import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

SCRIPT = Path(__file__).with_name("enable_android_cgo.py")
OLD = "//go:build (linux && !android) || (android && !cgo)"
NEW = "//go:build (linux && !android) || android"


class AndroidCgoPatchTests(unittest.TestCase):
    def test_patch_and_idempotence(self):
        with tempfile.TemporaryDirectory() as temp:
            module = Path(temp)
            target = module / "feature/androidbin/register.go"
            target.parent.mkdir(parents=True)
            target.write_text(OLD + "\npackage androidbin\n")
            for _ in range(2):
                result = subprocess.run([sys.executable, str(SCRIPT), str(module)], capture_output=True, text=True)
                self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(target.read_text(), NEW + "\npackage androidbin\n")

    def test_no_broken_anet_getter(self):
        # A registered getter bypasses androidbin's fallback entirely.
        project = Path(__file__).resolve().parents[1]
        source = (project / "tsproxy/setup.go").read_text()
        self.assertNotIn("netmon.RegisterInterfaceGetter", source)
        self.assertNotIn("anet.Interfaces()", source)

    def test_auth_url_not_leaked_to_diagnostic_log(self):
        project = Path(__file__).resolve().parents[1]
        source = (project / "mobile/tsproxy.go").read_text()
        self.assertNotIn('logBuf.write("LOGIN URL: %s"', source)
        self.assertIn('loginURL = *url', source)  # UI still gets the auth link

    def test_unknown_version_refused(self):
        with tempfile.TemporaryDirectory() as temp:
            module = Path(temp)
            target = module / "feature/androidbin/register.go"
            target.parent.mkdir(parents=True)
            target.write_text("//go:build android\npackage androidbin\n")
            result = subprocess.run([sys.executable, str(SCRIPT), str(module)], capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("Unexpected Tailscale build constraint", result.stderr)
            self.assertEqual(target.read_text(), "//go:build android\npackage androidbin\n")


if __name__ == "__main__":
    unittest.main()
