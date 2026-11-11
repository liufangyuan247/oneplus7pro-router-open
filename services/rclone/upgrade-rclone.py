#!/data/data/com.termux/files/usr/bin/python
"""Preflight and upgrade Termux rclone, with package-aware rollback at idle."""
import argparse
import fcntl
import json
import os
from pathlib import Path
import re
import shutil
import signal
import socket
import subprocess
import tempfile
import time
import urllib.request

PREFIX = Path(os.environ.get("PREFIX", "/data/data/com.termux/files/usr"))
HOME = Path(os.environ["HOME"])
DATA = HOME / "router-data"
SERVICE = HOME / "router-stack/services/rclone"
BINARY = PREFIX / "bin/rclone"
BACKUPS = DATA / "backups/services/rclone"
GUI = HOME / ".cache/rclone/webgui/current/build"
HTTP = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def command(args, *, log=None, cwd=None, timeout=180):
    result = subprocess.run([str(x) for x in args], cwd=cwd, timeout=timeout,
                            stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    if log:
        log.write(result.stdout)
        log.flush()
    if result.returncode:
        raise RuntimeError(f"{Path(str(args[0])).name} failed (exit {result.returncode}); see upgrade.log")
    return result.stdout.decode(errors="replace")


def program_version(value):
    match = re.search(r"(?:^|\brclone\s+)(?:\d+:)?v?(\d+\.\d+\.\d+)", value)
    if not match:
        raise RuntimeError("Cannot identify rclone version")
    return match[1]


def version(binary=BINARY):
    return program_version(command([binary, "version"], timeout=15))


def rc(endpoint, body=None, port=5572):
    req = urllib.request.Request(f"http://127.0.0.1:{port}/{endpoint}",
        data=json.dumps(body or {}).encode(), headers={"Content-Type": "application/json"})
    with HTTP.open(req, timeout=3) as response:
        return json.load(response)


def require_idle():
    for job in rc("job/list").get("jobids", []):
        if not rc("job/status", {"jobid": job}).get("finished", False):
            raise RuntimeError("Rclone 有未完成任务，请等待任务结束后再升级")
    stats = rc("core/stats")
    if stats.get("transferring") or stats.get("checking"):
        raise RuntimeError("Rclone 正在传输或校验，请等待空闲后再升级")


def healthy(expected, port=5572):
    try:
        if program_version(rc("core/version", port=port)["version"]) != expected:
            return False
        with HTTP.open(f"http://127.0.0.1:{port}/", timeout=2) as response:
            return response.status == 200
    except Exception:
        return False


def wait_healthy(expected):
    for _ in range(20):
        if healthy(expected) and version() == expected:
            text = command([PREFIX / "bin/sv", "status", SERVICE], timeout=5)
            match = re.search(r"^run:[^;\n]*?\(pid (\d+)\)", text)
            if match and os.path.samefile("/proc/" + match[1] + "/exe", BINARY):
                return
        time.sleep(1)
    raise RuntimeError("Rclone 版本、RC 或 WebUI 健康检查失败")


def repack(dest, old_version, log):
    """Recover the actual binary even after an older binary-only rollback."""
    root = dest / "rclone.files"
    control = root / "DEBIAN"
    control.mkdir(parents=True)
    os.chmod(control, 0o755)
    fields = re.split(r"\n(?=\S)", command([PREFIX / "bin/dpkg-query", "-s", "rclone"]).strip())
    fields = [f for f in fields if f.split(":", 1)[0] not in ("Status", "Conffiles", "Config-Version")]
    fields = ["Version: " + old_version
              if f.startswith("Version:") and program_version(f.split(":", 1)[1].strip()) != old_version
              else f for f in fields]
    (control / "control").write_text("\n".join(fields) + "\n")
    for name in ("preinst", "postinst", "prerm", "postrm", "triggers", "conffiles"):
        src = PREFIX / "var/lib/dpkg/info" / ("rclone." + name)
        if src.exists():
            shutil.copy2(src, control / name)
    for name in command([PREFIX / "bin/dpkg-query", "-L", "rclone"]).splitlines():
        src = Path(name)
        if not src.is_absolute() or src in PREFIX.parents:
            continue
        if not src.is_relative_to(PREFIX):
            raise RuntimeError("Package file outside Termux prefix")
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
    deb = dest / "rclone.deb"
    command([PREFIX / "bin/dpkg-deb", "-Zgzip", "-z1", "--build", root, deb], log=log)
    shutil.rmtree(root)
    return deb


def preflight(binary, expected, work, log):
    if version(binary) != expected or not (GUI / "index.html").is_file():
        raise RuntimeError("候选版本或现有 WebUI 静态文件无效")
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        port = listener.getsockname()[1]
    config = work / "empty.conf"
    config.write_text("")
    proc = subprocess.Popen([str(binary), "rcd", "--config=" + str(config), "--disable=copy",
        "--rc-addr=127.0.0.1:" + str(port), "--rc-files=" + str(GUI), "--rc-serve", "--rc-no-auth"],
        stdout=log, stderr=log)
    try:
        for _ in range(15):
            if proc.poll() is not None:
                break
            if healthy(expected, port):
                return
            time.sleep(0.5)
        raise RuntimeError("候选 Rclone 未通过隔离 RC / WebUI 启动检查")
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait()


def restore(backup, log):
    old = json.loads((backup / "apt-packages.json").read_text())["version"]
    command([PREFIX / "bin/sv", "-w", "30", "down", SERVICE], log=log)
    command([PREFIX / "bin/dpkg", "--force-downgrade", "-i", backup / "rclone.deb"], log=log)
    shutil.copy2(backup / "run", SERVICE / "run")
    command([PREFIX / "bin/sv", "up", SERVICE], log=log)
    wait_healthy(old)
    installed = command([PREFIX / "bin/dpkg-query", "-W", "-f=${Version}", "rclone"]).strip()
    if program_version(installed) != old:
        raise RuntimeError("回滚后软件包记录与程序版本不一致")
    return old


def upgrade():
    require_idle()
    old = version()
    policy = command([PREFIX / "bin/apt-cache", "policy", "rclone"])
    match = re.search(r"^\s*Candidate:\s+(\S+)", policy, re.M)
    if not match or match[1] == "(none)":
        raise RuntimeError("找不到 Rclone 软件包候选版本")
    target = match[1]
    expected = program_version(target)
    installed = command([PREFIX / "bin/dpkg-query", "-W", "-f=${Version}", "rclone"]).strip()
    if old == expected and installed == target and healthy(expected):
        return {"ok": True, "version": old, "message": "已经是最新版本"}
    backup = BACKUPS / (time.strftime("%Y%m%d-%H%M%S") + "-apt")
    backup.mkdir(parents=True, mode=0o700)
    with tempfile.TemporaryDirectory(prefix="rclone-", dir=DATA / ".updates") as temp, (backup / "upgrade.log").open("wb") as log:
        work = Path(temp)
        command([PREFIX / "bin/apt-get", "download", "rclone=" + target], cwd=work, log=log)
        deb = next(work.glob("rclone*.deb"))
        command([PREFIX / "bin/dpkg-deb", "-x", deb, work / "root"], log=log)
        preflight(work / "root" / str(BINARY).lstrip("/"), expected, work, log)
        repack(backup, old, log)
        shutil.copy2(BINARY, backup / "rclone")
        shutil.copy2(SERVICE / "run", backup / "run")
        (backup / "apt-packages.json").write_text(json.dumps({"version": old, "target": target}))
        (backup / "meta.json").write_text(json.dumps({"Service": "rclone", "Version": "v" + old,
            "Binary": str(backup / "rclone"), "Timestamp": time.strftime("%Y-%m-%dT%H:%M:%S%z")}))
        require_idle()  # Recheck immediately before the planned service restart.
        (BACKUPS / "last-backup").write_text(str(backup))
        changed = False
        try:
            changed = True
            command([PREFIX / "bin/sv", "-w", "30", "down", SERVICE], log=log)
            # Explicit install also repairs dpkg-new / binary-old mismatches.
            command([PREFIX / "bin/dpkg", "-i", deb], log=log)
            if version() != expected:
                raise RuntimeError("安装后的程序版本与候选版本不一致")
            command([PREFIX / "bin/sv", "up", SERVICE], log=log)
            wait_healthy(expected)
            if command([PREFIX / "bin/dpkg-query", "-W", "-f=${Version}", "rclone"]).strip() != target:
                raise RuntimeError("软件包记录与候选版本不一致")
        except BaseException as error:
            if not changed:
                raise
            try:
                restored = restore(backup, log)
                return {"ok": False, "rolledBack": True, "error": str(error) + "; 已完整恢复至 " + restored}
            except Exception:
                return {"ok": False, "rolledBack": False, "error": "升级及回滚失败，请查看 " + str(backup / "upgrade.log")}
    return {"ok": True, "version": expected, "backupDir": str(backup), "message": "Rclone 已升级并通过版本、RC 和 WebUI 校验"}


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
            require_idle()
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
