#!/data/data/com.termux/files/usr/bin/python
"""Upgrade Termux qBittorrent with its ABI dependencies and package rollback."""
import argparse
import fcntl
import json
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import tarfile
import tempfile
import time
import urllib.request

PACKAGES = ("libc++", "libtorrent-rasterbar", "qt6-qtbase", "qbittorrent-nox")
PREFIX = Path(os.environ.get("PREFIX", "/data/data/com.termux/files/usr"))
HOME = Path(os.environ["HOME"])
DATA = HOME / "router-data"
SERVICE = HOME / "router-stack/services/qbittorrent"
BINARY = PREFIX / "bin/qbittorrent-nox"
BACKUPS = DATA / "backups/services/qbittorrent"


def program_version(package_version):
    match = re.match(r"^(?:\d+:)?v?(\d+(?:\.\d+)+)", package_version)
    if not match:
        raise RuntimeError("Cannot identify APT program version")
    return match[1]


def command(args, *, log=None, env=None, cwd=None, timeout=180):
    result = subprocess.run([str(x) for x in args], env=env, cwd=cwd, timeout=timeout,
                            stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    if log:
        log.write(result.stdout)
        log.flush()
    if result.returncode:
        raise RuntimeError(f"{Path(str(args[0])).name} failed (exit {result.returncode}); see upgrade.log")
    return result.stdout.decode(errors="replace")


def version(binary=BINARY, env=None):
    text = command([binary, "--version"], env=env, timeout=15)
    match = re.search(r"qBittorrent\s+v?([0-9.]+)", text)
    if not match:
        raise RuntimeError("Cannot identify qBittorrent version")
    return match[1]


def candidate(package):
    text = command([PREFIX / "bin/apt-cache", "policy", package])
    match = re.search(r"^\s*Candidate:\s+(\S+)", text, re.M)
    if not match or match[1] == "(none)":
        raise RuntimeError(f"No APT candidate for {package}")
    return match[1]


def healthy(expected):
    try:
        text = command([PREFIX / "bin/sv", "status", SERVICE], timeout=10)
        if not text.startswith("run:") or version() != expected:
            return False
        match = re.search(r"\(pid (\d+)\)", text)
        if not match or not os.path.samefile("/proc/" + match[1] + "/exe", BINARY):
            return False
        with urllib.request.urlopen("http://127.0.0.1:8080/", timeout=2) as response:
            return response.status == 200
    except Exception:
        return False


def wait_healthy(expected):
    for _ in range(20):
        if healthy(expected):
            return
        time.sleep(1)
    raise RuntimeError("qBittorrent version / WebUI health check failed")


def repack(package, dest, old_version, log):
    """Recreate an installed package from actual files, including symlinks."""
    root = dest / (package + ".files")
    control = root / "DEBIAN"
    control.mkdir(parents=True)
    os.chmod(control, 0o755)
    text = command([PREFIX / "bin/dpkg-query", "-s", package])
    fields = re.split(r"\n(?=\S)", text.strip())
    fields = [f for f in fields if f.split(":", 1)[0] not in ("Status", "Conffiles", "Config-Version")]
    if package == "qbittorrent-nox":
        # Old binary-only rollback left dpkg at the newer version. Record the
        # actual program version in the recovery package, repairing that state.
        fields = ["Version: " + old_version
                  if f.startswith("Version:") and program_version(f.split(":", 1)[1].strip()) != old_version
                  else f for f in fields]
    (control / "control").write_text("\n".join(fields) + "\n")
    for name in ("preinst", "postinst", "prerm", "postrm", "triggers", "conffiles"):
        src = PREFIX / "var/lib/dpkg/info" / (package + "." + name)
        if src.exists():
            shutil.copy2(src, control / name)
    for item in command([PREFIX / "bin/dpkg-query", "-L", package]).splitlines():
        src = Path(item)
        if not src.is_absolute() or src in PREFIX.parents:
            continue
        if not src.is_relative_to(PREFIX):
            raise RuntimeError("Package file outside Termux prefix: " + package)
        target = root / str(src).lstrip("/")
        if src.is_symlink():
            target.parent.mkdir(parents=True, exist_ok=True)
            target.symlink_to(os.readlink(src))
        elif src.is_dir():
            target.mkdir(parents=True, exist_ok=True)
            shutil.copystat(src, target)
        elif src.is_file():
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(src, target)
    output = dest / (package + ".deb")
    command([PREFIX / "bin/dpkg-deb", "--build", root, output], log=log)
    shutil.rmtree(root)
    return output


def smoke_test(binary, env, work, expected, log):
    if version(binary, env) != expected:
        raise RuntimeError("Staged binary version does not match APT candidate")
    profile = work / "preflight-profile"
    config = profile / "qBittorrent/config/qBittorrent.conf"
    config.parent.mkdir(parents=True)
    config.write_text("[Preferences]\nWebUI\\Address=127.0.0.1\nWebUI\\LocalHostAuth=false\n"
                      "[BitTorrent]\nSession\\DHTEnabled=false\nSession\\LSDEnabled=false\nSession\\PeXEnabled=false\n")
    proc = subprocess.Popen([str(binary), "--profile=" + str(profile), "--webui-port=18080",
                             "--confirm-legal-notice"], env=env, stdout=log, stderr=log)
    try:
        for _ in range(20):
            if proc.poll() is not None:
                break
            try:
                with urllib.request.urlopen("http://127.0.0.1:18080/api/v2/app/version", timeout=1) as r:
                    if r.read().decode().strip().lstrip("v") == expected:
                        return
            except Exception:
                pass
            time.sleep(1)
        raise RuntimeError("Staged qBittorrent failed isolated WebUI startup")
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=10)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait()


def restore(backup, log):
    meta = json.loads((backup / "apt-packages.json").read_text())
    command([PREFIX / "bin/sv", "-w", "30", "down", SERVICE], log=log)
    command([PREFIX / "bin/dpkg", "--force-downgrade", "-i",
             *[backup / "packages" / (p + ".deb") for p in PACKAGES]], log=log)
    for name in ("config", "data/BT_backup"):
        folder = DATA / "qbittorrent/qBittorrent" / name
        if folder.exists():
            shutil.rmtree(folder)
    with tarfile.open(backup / "profile.tar.gz") as archive:
        archive.extractall(DATA / "qbittorrent", filter="data")
    command([PREFIX / "bin/sv", "up", SERVICE], log=log)
    wait_healthy(meta["version"])
    return meta["version"]


def upgrade():
    old = version()
    target = candidate("qbittorrent-nox")
    expected = program_version(target)
    installed = command([PREFIX / "bin/dpkg-query", "-W", "-f=${Version}", "qbittorrent-nox"]).strip()
    if old == expected and installed == target and healthy(expected):
        return {"ok": True, "version": old, "message": "已经是最新版本"}
    stamp = time.strftime("%Y%m%d-%H%M%S")
    backup = BACKUPS / (stamp + "-apt")
    backup.mkdir(parents=True, mode=0o700)
    with tempfile.TemporaryDirectory(prefix="qbittorrent-", dir=DATA / ".updates") as tmp, (backup / "upgrade.log").open("wb") as log:
        work = Path(tmp)
        packages = work / "packages"
        packages.mkdir()
        versions = {p: candidate(p) for p in PACKAGES}
        command([PREFIX / "bin/apt-get", "download", *[p + "=" + v for p, v in versions.items()]], cwd=packages, log=log)
        root = work / "root"
        root.mkdir()
        for deb in packages.glob("*.deb"):
            command([PREFIX / "bin/dpkg-deb", "-x", deb, root], log=log)
        staged_prefix = root / str(PREFIX).lstrip("/")
        env = dict(os.environ, LD_LIBRARY_PATH=str(staged_prefix / "lib") + ":" + str(PREFIX / "lib"),
                   QT_PLUGIN_PATH=str(staged_prefix / "lib/qt-6/plugins"), QT_QPA_PLATFORM="offscreen")
        smoke_test(staged_prefix / "bin/qbittorrent-nox", env, work, expected, log)
        rollback_packages = backup / "packages"
        rollback_packages.mkdir()
        for package in PACKAGES:
            repack(package, rollback_packages, old, log)
        shutil.copy2(BINARY, backup / BINARY.name)
        changed = False
        try:
            command([PREFIX / "bin/sv", "-w", "30", "down", SERVICE], log=log)
            with tarfile.open(backup / "profile.tar.gz", "w:gz") as archive:
                for name in ("qBittorrent/config", "qBittorrent/data/BT_backup"):
                    path = DATA / "qbittorrent" / name
                    if path.exists():
                        archive.add(path, arcname=name)
            (backup / "apt-packages.json").write_text(json.dumps({"version": old, "target": target}))
            (backup / "meta.json").write_text(json.dumps({"Service": "qbittorrent", "Version": "v" + old,
                "Binary": str(backup / BINARY.name), "Timestamp": time.strftime("%Y-%m-%dT%H:%M:%S%z")}))
            (BACKUPS / "last-backup").write_text(str(backup))
            changed = True
            # Already downloaded and independently tested this exact package set.
            command([PREFIX / "bin/dpkg", "-i", *sorted(packages.glob("*.deb"))], log=log)
            if version() != expected:
                raise RuntimeError("Installed program version does not match candidate")
            command([PREFIX / "bin/sv", "up", SERVICE], log=log)
            wait_healthy(expected)
            installed = command([PREFIX / "bin/dpkg-query", "-W", "-f=${Version}", "qbittorrent-nox"]).strip()
            if installed != target:
                raise RuntimeError("Package database version does not match installed program")
        except BaseException as error:
            if not changed:
                command([PREFIX / "bin/sv", "up", SERVICE], log=log)
                raise
            try:
                restored = restore(backup, log)
                return {"ok": False, "rolledBack": True, "error": str(error) + "; 已恢复程序、依赖和配置至 " + restored}
            except Exception:
                return {"ok": False, "rolledBack": False, "error": "升级及完整回滚失败，请查看 " + str(backup / "upgrade.log")}
    return {"ok": True, "version": expected, "backupDir": str(backup), "message": "qBittorrent 及配套依赖已升级并通过版本、WebUI 校验"}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--rollback", type=Path)
    args = parser.parse_args()
    os.umask(0o077)
    (DATA / ".updates").mkdir(exist_ok=True)
    with (DATA / ".updates/apt-services.lock").open("w") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise RuntimeError("另一个服务升级正在使用软件包管理器") from None
        if args.rollback:
            backup = args.rollback.resolve()
            if not backup.is_relative_to(BACKUPS.resolve()):
                raise RuntimeError("Invalid rollback directory")
            with (backup / "rollback.log").open("ab") as log:
                return {"ok": True, "version": restore(backup, log)}
        return upgrade()


if __name__ == "__main__":
    signal.signal(signal.SIGTERM, lambda *_: (_ for _ in ()).throw(KeyboardInterrupt("升级被终止")))
    try:
        result = main()
    except BaseException as error:
        result = {"ok": False, "rolledBack": False, "error": str(error) or type(error).__name__}
    print(json.dumps(result, ensure_ascii=False))
    raise SystemExit(0 if result.get("ok") else 1)
