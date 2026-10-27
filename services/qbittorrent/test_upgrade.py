"""Run on Termux: python -m unittest discover -s services/qbittorrent."""
import importlib.util
import io
import json
import os
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("upgrader", Path(__file__).with_name("upgrade-qbittorrent.py"))
upgrader = importlib.util.module_from_spec(spec)
spec.loader.exec_module(upgrader)


class PackageRecoveryTest(unittest.TestCase):
    def test_program_version_handles_debian_epoch_and_revision(self):
        self.assertEqual(upgrader.program_version("1:5.3.0-2"), "5.3.0")
        self.assertEqual(upgrader.program_version("5.3.0"), "5.3.0")

    def test_repack_uses_actual_version_and_preserves_symlinks(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            prefix = root / "prefix"
            (prefix / "bin").mkdir(parents=True)
            binary = prefix / "bin/qbittorrent-nox"
            binary.write_bytes(b"old-working-program")
            os.chmod(binary, 0o700)
            link = prefix / "bin/alias"
            link.symlink_to("qbittorrent-nox")
            dest = root / "recovery"
            dest.mkdir()
            def command(args, **kwargs):
                if "-s" in args:
                    return "Package: qbittorrent-nox\nStatus: install ok installed\nVersion: 5.3.0\nArchitecture: aarch64\nDescription: test\n"
                if "-L" in args:
                    return "\n".join(map(str, (prefix, prefix / "bin", binary, link)))
                package_root = Path(args[-2])
                control = package_root / "DEBIAN/control"
                self.assertIn("Version: 5.2.4", control.read_text())
                self.assertNotIn("Status:", control.read_text())
                self.assertEqual((control.parent.stat().st_mode & 0o777), 0o755)
                saved = package_root / str(binary).lstrip("/")
                self.assertEqual(saved.read_bytes(), b"old-working-program")
                self.assertEqual(saved.stat().st_mode & 0o777, 0o700)
                self.assertEqual(os.readlink(package_root / str(link).lstrip("/")), "qbittorrent-nox")
                return ""
            with patch.object(upgrader, "PREFIX", prefix), patch.object(upgrader, "command", command):
                upgrader.repack("qbittorrent-nox", dest, "5.2.4", io.BytesIO())

    def test_repack_rejects_payload_outside_termux_prefix(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            with patch.object(upgrader, "PREFIX", root / "prefix"), patch.object(
                    upgrader, "command", side_effect=["Package: test\nVersion: 1\n", "/etc/shadow\n"]):
                with self.assertRaisesRegex(RuntimeError, "outside Termux"):
                    upgrader.repack("test", root / "backup", "1", io.BytesIO())

    def test_restore_keeps_download_payload_and_recovers_task_metadata(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            data = root / "data"
            profile = data / "qbittorrent"
            cfg = profile / "qBittorrent/config/settings.ini"
            resume = profile / "qBittorrent/data/BT_backup/task.fastresume"
            for file in (cfg, resume):
                file.parent.mkdir(parents=True, exist_ok=True)
                file.write_text("old-state")
            media = profile / "downloads/keep.bin"
            media.parent.mkdir()
            media.write_bytes(b"keep-download")
            backup = root / "backup"
            backup.mkdir()
            (backup / "apt-packages.json").write_text(json.dumps({"version": "5.2.4"}))
            with tarfile.open(backup / "profile.tar.gz", "w:gz") as archive:
                for name in ("qBittorrent/config", "qBittorrent/data/BT_backup"):
                    archive.add(profile / name, arcname=name)
            cfg.write_text("new-state")
            resume.write_text("new-state")
            with patch.object(upgrader, "DATA", data), patch.object(upgrader, "command") as command, patch.object(upgrader, "wait_healthy") as healthy:
                self.assertEqual(upgrader.restore(backup, io.BytesIO()), "5.2.4")
                install = command.call_args_list[1].args[0]
                self.assertEqual(str(install[0]), str(upgrader.PREFIX / "bin/dpkg"))
                self.assertIn("--force-downgrade", install)
                self.assertEqual(len([x for x in install if str(x).endswith(".deb")]), 4)
                healthy.assert_called_once_with("5.2.4")
            self.assertEqual(cfg.read_text(), "old-state")
            self.assertEqual(resume.read_text(), "old-state")
            self.assertEqual(media.read_bytes(), b"keep-download")


if __name__ == "__main__":
    unittest.main()
