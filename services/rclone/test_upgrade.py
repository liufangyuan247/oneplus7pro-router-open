"""Run on Termux: python -m unittest discover -s services/rclone."""
import importlib.util
import io
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("upgrader", Path(__file__).with_name("upgrade-rclone.py"))
upgrader = importlib.util.module_from_spec(spec)
spec.loader.exec_module(upgrader)


class RcloneUpgradeTest(unittest.TestCase):
    def test_health_checks_service_pid_instead_of_log_process(self):
        status = "run: /services/rclone: (pid 123) 2s; run: log: (pid 456) 50s\n"
        with patch.object(upgrader, "healthy", return_value=True), \
             patch.object(upgrader, "version", return_value="1.76.0"), \
             patch.object(upgrader, "command", return_value=status), \
             patch.object(upgrader.os.path, "samefile", return_value=True) as samefile:
            upgrader.wait_healthy("1.76.0")
            samefile.assert_called_once_with("/proc/123/exe", upgrader.BINARY)

    def test_versions(self):
        for text in ("1.76.0", "1:1.76.0-2", "rclone v1.76.0-termux\n- os/arch: arm64"):
            self.assertEqual(upgrader.program_version(text), "1.76.0")
        with self.assertRaises(RuntimeError):
            upgrader.program_version("linker failed")

    def test_running_job_blocks_before_package_or_service_changes(self):
        def rc(endpoint, body=None):
            if endpoint == "job/list":
                return {"jobids": [1]}
            return {"finished": False}
        with patch.object(upgrader, "rc", rc), patch.object(upgrader, "command") as command:
            with self.assertRaisesRegex(RuntimeError, "未完成任务"):
                upgrader.upgrade()
            command.assert_not_called()

    def test_transfers_and_checks_block_upgrade(self):
        for activity in ("transferring", "checking"):
            with patch.object(upgrader, "rc", side_effect=[{"jobids": []}, {activity: [{}]}]):
                with self.assertRaisesRegex(RuntimeError, "传输或校验"):
                    upgrader.require_idle()

    def test_unreachable_rc_does_not_count_as_idle(self):
        with patch.object(upgrader, "rc", side_effect=OSError("not reachable")), patch.object(upgrader, "command") as command:
            with self.assertRaises(OSError):
                upgrader.upgrade()
            command.assert_not_called()

    def test_recovery_package_uses_actual_binary_version(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            prefix = root / "prefix"
            binary = prefix / "bin/rclone"
            binary.parent.mkdir(parents=True)
            binary.write_bytes(b"old binary")
            dest = root / "backup"
            dest.mkdir()
            def command(args, **kwargs):
                if "-s" in args:
                    return "Package: rclone\nVersion: 1.76.0\nStatus: install ok installed\nArchitecture: aarch64\nDescription: Test\n"
                if "-L" in args:
                    return "\n".join(map(str, [prefix, prefix / "bin", binary]))
                staged = Path(args[-2])
                control = staged / "DEBIAN/control"
                self.assertIn("Version: 1.75.2", control.read_text())
                self.assertNotIn("Status:", control.read_text())
                self.assertEqual(control.parent.stat().st_mode & 0o777, 0o755)
                self.assertEqual((staged / str(binary).lstrip("/")).read_bytes(), b"old binary")
                return ""
            with patch.object(upgrader, "PREFIX", prefix), patch.object(upgrader, "command", command):
                upgrader.repack(dest, "1.75.2", io.BytesIO())

    def test_recovery_rejects_files_outside_prefix(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            with patch.object(upgrader, "PREFIX", root / "prefix"), patch.object(upgrader, "command", side_effect=[
                    "Package: rclone\nVersion: 1.76.0\n", "/etc/shadow\n"]):
                with self.assertRaisesRegex(RuntimeError, "outside Termux"):
                    upgrader.repack(root / "backup", "1.75.2", io.BytesIO())


if __name__ == "__main__":
    unittest.main()
