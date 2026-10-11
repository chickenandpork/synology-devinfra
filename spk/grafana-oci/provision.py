"""Reconcile Grafana users and their home dashboards through Grafana's API."""

import argparse
import base64
import json
import os
from pathlib import Path
import subprocess
import sys
from urllib.error import HTTPError, URLError
from urllib.parse import quote, urlsplit
from urllib.request import HTTPRedirectHandler, Request, build_opener


class APIError(Exception):
    def __init__(self, status):
        self.status = status
        super().__init__(f"Grafana API returned HTTP {status}")


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class Grafana:
    def __init__(self, url, username, password):
        parsed = urlsplit(url)
        if (parsed.scheme not in ("http", "https") or not parsed.hostname
                or parsed.username or parsed.password or parsed.query or parsed.fragment):
            raise ValueError("Grafana URL must be an HTTP(S) URL without credentials or query")
        self.url = url.rstrip("/")
        self.auth = base64.b64encode(f"{username}:{password}".encode()).decode()
        self.opener = build_opener(NoRedirect())

    def request(self, method, path, body=None):
        request = Request(
            self.url + path,
            data=None if body is None else json.dumps(body).encode(),
            method=method,
            headers={"Authorization": "Basic " + self.auth, "Content-Type": "application/json"},
        )
        try:
            with self.opener.open(request, timeout=30) as response:
                return json.load(response)
        except HTTPError as error:
            # Do not print response bodies or request credentials.
            raise APIError(error.code) from None
        except URLError:
            raise RuntimeError("Could not connect to Grafana") from None


def validate(config, secrets):
    users = config["users"]
    names = [user["login"] for user in users]
    if len(set(names)) != len(names) or not all(names):
        raise ValueError("User logins must be nonempty and unique")
    if not secrets.get("username") or not secrets.get("password"):
        raise ValueError("An existing Grafana server administrator credential is required")
    for user in users:
        if user["role"] not in ("Admin", "Editor", "Viewer"):
            raise ValueError("User role must be Admin, Editor, or Viewer")
        if not isinstance(user.get("server_admin", False), bool):
            raise ValueError("server_admin must be a boolean")
        if user.get("server_admin") and user["role"] != "Admin":
            raise ValueError("Server administrators must also have the Admin organization role")
        if not isinstance(user.get("home_dashboard_uid", ""), str):
            raise ValueError("home_dashboard_uid must be a string")
    if not any(user.get("server_admin") for user in users):
        raise ValueError("Keep at least one managed server administrator")


def reconcile(api, config, secrets, apply=False, client=Grafana):
    validate(config, secrets)
    me = api.request("GET", "/api/user")
    if not me.get("isGrafanaAdmin"):
        raise ValueError("Provisioning requires a Grafana server administrator")
    org_id = config.get("org_id", 1)
    members = {user["userId"]: user["role"] for user in
               api.request("GET", f"/api/orgs/{org_id}/users")}
    planned = []
    # Validate every account and dashboard before making any changes.
    for user in config["users"]:
        login = user["login"]
        password = secrets.get("users", {}).get(login)
        try:
            existing = api.request("GET", "/api/users/lookup?loginOrEmail=" + quote(login, safe=""))
        except APIError as error:
            if error.status != 404:
                raise
            existing = None
        if existing and existing["login"] != login:
            raise ValueError(f"Login {login} matched a different account's email")
        if existing and existing["id"] == me["id"] and not user.get("server_admin", False):
            raise ValueError("Cannot demote the administrator running provisioning")
        if not existing and not password:
            raise ValueError(f"A creation password is required for {login}")
        if "home_dashboard_uid" in user:
            if not password:
                raise ValueError(f"The user's credential is required to set {login}'s home dashboard")
            if existing:
                identity = client(api.url, login, password).request("GET", "/api/user")
                if identity["id"] != existing["id"]:
                    raise ValueError(f"Unexpected identity for {login}")
            uid = user["home_dashboard_uid"]
            if uid:
                api.request("GET", "/api/dashboards/uid/" + quote(uid, safe=""))
        planned.append((user, existing, password))

    for user, existing, password in planned:
        login = user["login"]
        print(f"{'Apply' if apply else 'Plan'}: {login}: role={user['role']}, "
              f"server_admin={user.get('server_admin', False)}, "
              f"account={'existing' if existing else 'create'}")
        if not apply:
            continue
        if existing is None:
            created = api.request("POST", "/api/admin/users", {
                "login": login, "name": user.get("name", login),
                "email": user.get("email", ""), "password": password, "OrgId": org_id,
            })
            user_id = created["id"]
            existing = api.request("GET", f"/api/users/{user_id}")
            # Grafana automatically joins new users to their initial organization.
            members = {entry["userId"]: entry["role"] for entry in
                       api.request("GET", f"/api/orgs/{org_id}/users")}
        user_id = existing["id"]
        if user_id not in members:
            api.request("POST", f"/api/orgs/{org_id}/users",
                        {"loginOrEmail": login, "role": user["role"]})
        elif members[user_id] != user["role"]:
            api.request("PATCH", f"/api/orgs/{org_id}/users/{user_id}", {"role": user["role"]})
        if existing.get("isGrafanaAdmin", False) != user.get("server_admin", False):
            api.request("PUT", f"/api/admin/users/{user_id}/permissions",
                        {"isGrafanaAdmin": user.get("server_admin", False)})
        if "home_dashboard_uid" in user:
            personal = client(api.url, login, password)
            identity = personal.request("GET", "/api/user")
            if identity["orgId"] != org_id:
                personal.request("POST", f"/api/user/using/{org_id}")
            uid = user["home_dashboard_uid"]
            if uid:
                # Verify that the account can actually read the requested dashboard.
                personal.request("GET", "/api/dashboards/uid/" + quote(uid, safe=""))
            preferences = personal.request("GET", "/api/user/preferences")
            if preferences.get("homeDashboardUID", "") != uid:
                personal.request("PATCH", "/api/user/preferences", {"homeDashboardUID": uid})
            if personal.request("GET", "/api/user/preferences").get("homeDashboardUID", "") != uid:
                raise RuntimeError(f"Home dashboard verification failed for {login}")
        actual = api.request("GET", f"/api/users/{user_id}")
        roles = {entry["orgId"]: entry["role"] for entry in
                 api.request("GET", f"/api/users/{user_id}/orgs")}
        if (actual.get("isGrafanaAdmin", False) != user.get("server_admin", False)
                or roles.get(org_id) != user["role"]):
            raise RuntimeError(f"Role verification failed for {login}")


def provision(url, config, secrets, apply=False, bootstrap=False, client=Grafana):
    validate(config, secrets)
    owner = secrets["username"]
    api = client(url, owner, secrets["password"])
    try:
        api.request("GET", "/api/user")
    except APIError as error:
        if not bootstrap or error.status != 401:
            raise
        if (not any(user["login"] == owner and user.get("server_admin") for user in config["users"])
                or secrets.get("users", {}).get(owner) != secrets["password"]):
            raise ValueError("Bootstrap requires a managed owner with matching saved passwords")
        # Also recover if an earlier bootstrap stopped between rename and password change.
        for login in dict.fromkeys(("admin", owner)):
            initial = client(url, login, "admin")
            try:
                identity = initial.request("GET", "/api/user")
                break
            except APIError as initial_error:
                if initial_error.status != 401:
                    raise
        else:
            raise ValueError("Initial administrator login unavailable; restore the database or recover administrator access")
        users = initial.request("GET", "/api/users?perpage=2")
        if (not identity.get("isGrafanaAdmin") or len(users) != 1
                or users[0]["id"] != identity["id"] or identity["login"] != login):
            raise ValueError("Bootstrap only supports a database with a single initial administrator")
        # Preflight all account inputs and required dashboards before mutating the initial account.
        reconcile(initial, config, secrets, apply=False, client=client)
        print(f"{'Apply' if apply else 'Plan'}: bootstrap initial administrator as {owner}")
        if not apply:
            return
        initial.request("PUT", f"/api/users/{identity['id']}", {
            "login": owner, "name": owner, "email": identity.get("email", ""),
        })
        initial = client(url, owner, "admin")
        initial.request("PUT", "/api/user/password", {
            "oldPassword": "admin", "newPassword": secrets["password"],
            "confirmNew": secrets["password"],
        })
        api = client(url, owner, secrets["password"])
    reconcile(api, config, secrets, apply=apply, client=client)


def read_secrets(path, identity=None):
    if identity is None:
        return json.loads(path.read_text())
    # age writes plaintext only to this process's memory, not a temporary file.
    decrypted = subprocess.run(
        ["age", "--decrypt", "--identity", str(identity), str(path)],
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False,
    )
    if decrypted.returncode:
        raise ValueError("Could not decrypt credentials with the supplied age identity")
    return json.loads(decrypted.stdout)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--url", required=True)
    parser.add_argument("--config", type=Path, required=True)
    parser.add_argument("--secrets", type=Path, required=True)
    parser.add_argument("--age-identity", type=Path, help="Decrypt an age-encrypted secrets file using this identity")
    parser.add_argument("--bootstrap", action="store_true", help="Allow initial admin/admin setup on an empty Grafana database")
    parser.add_argument("--apply", action="store_true", help="Apply changes; otherwise only show a plan")
    args = parser.parse_args()
    # Bazel run starts in runfiles; resolve user-supplied paths from the invoking directory.
    os.chdir(os.environ.get("BUILD_WORKING_DIRECTORY", os.getcwd()))
    try:
        config = json.loads(args.config.read_text())
        secrets = read_secrets(args.secrets, args.age_identity)
        provision(args.url, config, secrets, apply=args.apply, bootstrap=args.bootstrap)
    except (APIError, ValueError, RuntimeError, KeyError, OSError) as error:
        print(f"Provisioning failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
