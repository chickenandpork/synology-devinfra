import contextlib
import copy
import io
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch

from provision import APIError, provision, read_secrets, reconcile


class FakeGrafana:
    url = "http://grafana.test"

    def __init__(self):
        self.users = {"allanc": {"id": 1, "login": "allanc", "isGrafanaAdmin": True}}
        self.roles = {1: "Admin"}
        self.writes = []
        self.lookup_status = 404

    def request(self, method, path, body=None):
        if method != "GET":
            self.writes.append((method, path, body))
        if path == "/api/user":
            return copy.deepcopy(self.users["allanc"])
        if path == "/api/dashboards/uid/home":
            return {"dashboard": {"uid": "home"}}
        if path.startswith("/api/users/lookup?"):
            login = path.split("=", 1)[1]
            if login not in self.users:
                raise APIError(self.lookup_status)
            return copy.deepcopy(self.users[login])
        if path == "/api/orgs/1/users":
            if method == "POST":
                self.roles[self.users[body["loginOrEmail"]]["id"]] = body["role"]
                return {}
            return [{"userId": uid, "role": role} for uid, role in self.roles.items()]
        if path == "/api/admin/users":
            uid = len(self.users) + 1
            self.users[body["login"]] = {"id": uid, "login": body["login"], "isGrafanaAdmin": False}
            self.roles[uid] = "Viewer"
            return {"id": uid}
        if path.startswith("/api/orgs/1/users/"):
            self.roles[int(path.rsplit("/", 1)[1])] = body["role"]
            return {}
        if path.startswith("/api/admin/users/"):
            uid = int(path.split("/")[4])
            next(user for user in self.users.values() if user["id"] == uid).update(body)
            return {}
        if path.startswith("/api/users/"):
            uid = int(path.split("/")[3])
            if path.endswith("/orgs"):
                return [{"orgId": 1, "role": self.roles[uid]}]
            return copy.deepcopy(next(user for user in self.users.values() if user["id"] == uid))
        raise AssertionError((method, path, body))


class ProvisionTest(unittest.TestCase):
    def setUp(self):
        self.api = FakeGrafana()
        self.config = {"users": [
            {"login": "allanc", "role": "Admin", "server_admin": True},
            {"login": "agent", "role": "Admin"},
            {"login": "screen1", "role": "Viewer"},
            {"login": "screen2", "role": "Viewer"},
        ]}
        self.secrets = {"username": "allanc", "password": "secret", "users": {
            "allanc": "secret", "agent": "secret-agent", "screen1": "secret-one", "screen2": "secret-two",
        }}

    def run_reconcile(self, apply):
        with contextlib.redirect_stdout(io.StringIO()) as output:
            reconcile(self.api, self.config, self.secrets, apply=apply)
        self.assertNotIn("secret", output.getvalue())

    def test_plan_has_no_writes(self):
        self.run_reconcile(False)
        self.assertEqual(self.api.writes, [])

    def test_apply_and_repeat(self):
        self.run_reconcile(True)
        self.assertEqual(self.api.roles, {1: "Admin", 2: "Admin", 3: "Viewer", 4: "Viewer"})
        self.assertFalse(self.api.users["agent"]["isGrafanaAdmin"])
        self.api.writes.clear()
        self.run_reconcile(True)
        self.assertEqual(self.api.writes, [])

    def test_repair_viewer_privileges(self):
        self.run_reconcile(True)
        self.api.users["screen1"]["isGrafanaAdmin"] = True
        self.api.roles[3] = "Admin"
        self.run_reconcile(True)
        self.assertFalse(self.api.users["screen1"]["isGrafanaAdmin"])
        self.assertEqual(self.api.roles[3], "Viewer")

    def test_missing_password_fails_before_writes(self):
        del self.secrets["users"]["screen2"]
        with self.assertRaises(ValueError):
            self.run_reconcile(True)
        self.assertEqual(self.api.writes, [])

    def test_lookup_failure_does_not_create_accounts(self):
        self.api.lookup_status = 403
        with self.assertRaises(APIError):
            self.run_reconcile(True)
        self.assertEqual(self.api.writes, [])

    def test_cannot_demote_caller(self):
        self.config["users"][0]["server_admin"] = False
        self.config["users"][1]["server_admin"] = True
        with self.assertRaises(ValueError):
            self.run_reconcile(True)
        self.assertEqual(self.api.writes, [])

    def test_home_dashboard_preserves_preferences_and_is_repeatable(self):
        self.run_reconcile(True)
        self.config["users"][2]["home_dashboard_uid"] = "home"
        preferences = {"theme": "dark", "timezone": "utc", "homeDashboardUID": ""}
        writes = []

        class Personal:
            def request(self, method, path, body=None):
                if path == "/api/user":
                    return {"id": 3, "orgId": 1}
                if path == "/api/dashboards/uid/home":
                    return {"dashboard": {"uid": "home"}}
                if path == "/api/user/preferences":
                    if method == "PATCH":
                        writes.append(body)
                        preferences.update(body)
                    return copy.deepcopy(preferences)
                raise AssertionError((method, path))

        with contextlib.redirect_stdout(io.StringIO()):
            for _ in range(2):
                reconcile(self.api, self.config, self.secrets, apply=True,
                          client=lambda *args: Personal())
        self.assertEqual(writes, [{"homeDashboardUID": "home"}])
        self.assertEqual(preferences, {"theme": "dark", "timezone": "utc", "homeDashboardUID": "home"})


class BootstrapTest(ProvisionTest):
    def setUp(self):
        super().setUp()
        self.api.users = {"admin": {"id": 1, "login": "admin", "isGrafanaAdmin": True}}
        self.passwords = {"admin": "admin"}

    def connect(self, url, username, password):
        backend = self.api
        passwords = self.passwords

        class Authenticated:
            def __init__(self):
                self.url = url

            def request(self, method, path, body=None):
                if username not in passwords or passwords[username] != password:
                    raise APIError(401)
                if path == "/api/user":
                    return copy.deepcopy(backend.users[username])
                if path == "/api/users?perpage=2":
                    return list(backend.users.values())[:2]
                if method == "PUT" and path == "/api/users/1":
                    backend.writes.append((method, path, body))
                    user = backend.users.pop(username)
                    user.update(body)
                    backend.users[body["login"]] = user
                    passwords[body["login"]] = passwords.pop(username)
                    return {}
                if path == "/api/user/password":
                    if body["oldPassword"] != passwords[username]:
                        raise APIError(400)
                    backend.writes.append((method, path, body))
                    passwords[username] = body["newPassword"]
                    return {}
                result = backend.request(method, path, body)
                if method == "POST" and path == "/api/admin/users":
                    passwords[body["login"]] = body["password"]
                return result

        return Authenticated()

    def run_bootstrap(self, apply=True):
        with contextlib.redirect_stdout(io.StringIO()) as output:
            provision(self.api.url, self.config, self.secrets, apply=apply,
                      bootstrap=True, client=self.connect)
        self.assertNotIn("secret", output.getvalue())

    # Override the base suite's entry point to exercise authenticated bootstrap too.
    def run_reconcile(self, apply):
        self.run_bootstrap(apply)

    def test_bootstrap_restores_saved_passwords(self):
        self.run_bootstrap()
        self.assertEqual(self.passwords, self.secrets["users"])
        self.assertNotIn("admin", self.api.users)
        self.assertEqual(self.api.roles, {1: "Admin", 2: "Admin", 3: "Viewer", 4: "Viewer"})
        self.api.writes.clear()
        self.run_bootstrap()
        self.assertEqual(self.api.writes, [])

    def test_bootstrap_resumes_after_rename(self):
        self.api.users["allanc"] = self.api.users.pop("admin")
        self.api.users["allanc"]["login"] = "allanc"
        self.passwords = {"allanc": "admin"}
        self.run_bootstrap()
        self.assertEqual(self.passwords, self.secrets["users"])

    def test_bootstrap_refuses_populated_database(self):
        self.api.users["someone"] = {"id": 2, "login": "someone"}
        with self.assertRaises(ValueError):
            self.run_bootstrap()
        self.assertEqual(self.api.writes, [])

    def test_no_implicit_bootstrap(self):
        with self.assertRaises(APIError):
            provision(self.api.url, self.config, self.secrets, apply=True, client=self.connect)
        self.assertEqual(self.api.writes, [])


class EncryptedSecretsTest(unittest.TestCase):
    @patch("provision.subprocess.run")
    def test_decrypts_in_memory(self, run):
        run.return_value = subprocess.CompletedProcess([], 0, b'{"password":"secret"}', b"")
        self.assertEqual(read_secrets(Path("credentials.age"), Path("identity")), {"password": "secret"})
        self.assertEqual(run.call_args.args[0], ["age", "--decrypt", "--identity", "identity", "credentials.age"])

    @patch("provision.subprocess.run")
    def test_failed_decryption_does_not_echo_output(self, run):
        run.return_value = subprocess.CompletedProcess([], 1, b"secret", b"secret")
        with self.assertRaisesRegex(ValueError, "Could not decrypt") as error:
            read_secrets(Path("credentials.age"), Path("identity"))
        self.assertNotIn("secret", str(error.exception))


if __name__ == "__main__":
    unittest.main()
