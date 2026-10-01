"""Validate the installable archive and its persistent Compose deployment."""

import io
import json
import re
import sys
import tarfile
import unittest


SPK = sys.argv.pop(1)


def contents(archive):
    return {
        (member.name[2:] if member.name.startswith("./") else member.name):
        archive.extractfile(member).read()
        for member in archive.getmembers()
        if member.isfile()
    }


class PackageTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        with tarfile.open(SPK) as archive:
            cls.files = contents(archive)
        with tarfile.open(fileobj=io.BytesIO(cls.files["package.tgz"])) as archive:
            cls.payload = contents(archive)
        cls.compose = cls.payload["grafana-oci/compose.yaml"].decode()

    def test_dsm_registration(self):
        info = self.files["INFO"].decode()
        self.assertIn('package="grafana-oci"', info)
        version = re.search(r'^version="([^"]+)"', info, re.MULTILINE).group(1)
        self.assertRegex(
            self.compose,
            r"image: grafana/grafana:" + re.escape(version.rsplit("-", 1)[0])
            + r"@sha256:[a-f0-9]{64}\s",
        )
        resource = json.loads(self.files["conf/resource"])
        self.assertEqual(resource["docker-project"]["projects"], [
            {"name": "grafana-oci", "path": "grafana-oci"},
        ])
        for script in (
            "preinst", "postinst", "preuninst", "postuninst", "preupgrade",
            "postupgrade", "start-stop-status",
        ):
            self.assertIn("scripts/" + script, self.files)

    def test_unprivileged_runtime(self):
        privilege = json.loads(self.files["conf/privilege"])
        self.assertEqual(privilege["defaults"]["run-as"], "package")
        self.assertEqual(privilege["username"], "sc-grafana-oci")
        for script in privilege.get("ctrl-script", []):
            self.assertEqual(script["run-as"], "package")
        self.assertIn('user: "472"', self.compose)
        self.assertNotIn("privileged:", self.compose)
        self.assertIn('"3000:3000"', self.compose)
        self.assertNotIn("GF_SECURITY_ADMIN_PASSWORD", self.compose)
        self.assertNotIn("{{", self.compose)

    def test_storage_survives_package_lifecycle(self):
        self.assertIn("grafana-oci: {external: true}", self.compose)
        self.assertIn("grafana-oci:/var/lib/grafana", self.compose)
        self.assertIn("GF_PATHS_PLUGINS: /var/lib/grafana/plugins", self.compose)
        start_stop = self.files["scripts/start-stop-status"].decode()
        self.assertIn('docker volume create "grafana-oci"', start_stop)
        self.assertIn('docker_inspect "grafana-oci"', start_stop)
        self.assertIn("|| exit 1", start_stop)
        for name, data in self.files.items():
            if name.startswith("scripts/"):
                script = data.decode()
                self.assertNotRegex(script, r"docker\s+volume\s+(rm|prune)\b")
                self.assertNotRegex(script, r"\bdown\s+.*(-v|--volumes)\b")


if __name__ == "__main__":
    unittest.main()
