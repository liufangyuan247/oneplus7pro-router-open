"""Persistent DNS fail-open guard; retains per-cycle rule verification."""
import fcntl
import importlib.util
import os
from pathlib import Path
import re
import signal
import socket
import struct
import subprocess
import sys
import threading
import time

BASE = Path('/data/adb/router-adguardhome')
PREFIX = Path('/data/data/com.termux/files/usr')
INTERFACES = Path('/data/data/com.termux/files/home/router-data/admin/active-interfaces')
CHAIN = 'OP7_ADGUARD'
INTERVAL = 5


class Guard:
    def __init__(self, base=BASE, interfaces=INTERFACES, iptables=None, health=None):
        self.base = base
        self.interfaces = interfaces
        self.iptables = str(iptables or PREFIX / 'bin/iptables')
        self.state = base / 'state'
        self.health = health
        self.stopping = threading.Event()
        self.address_socket = None

    def command(self, *args):
        result = subprocess.run([self.iptables, '-w', '2', '-t', 'nat', *args],
                                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                                timeout=4, check=False)
        return result.returncode == 0

    def publish(self, state):
        try:
            previous = self.state.read_text().strip()
        except FileNotFoundError:
            previous = None
        if previous != state:
            self.state.write_text(state + '\n')
            print('AdGuard DNS ' + state, flush=True)

    def remove_rules(self):
        # Remove every stale jump before destroying the dedicated chain.
        for _ in range(32):
            if not self.command('-C', 'PREROUTING', '-j', CHAIN):
                break
            if not self.command('-D', 'PREROUTING', '-j', CHAIN):
                raise RuntimeError('Cannot remove DNS interception jump')
        else:
            raise RuntimeError('Too many DNS interception jumps')
        self.command('-F', CHAIN)
        self.command('-X', CHAIN)
        self.publish('bypass')

    def lan_address(self):
        try:
            fields = self.interfaces.read_text().split()
            lan = fields[1] if len(fields) >= 2 else ''
            if not re.fullmatch(r'[a-zA-Z0-9_.:-]{1,15}', lan):
                return '', ''
            if self.address_socket is None:
                self.address_socket = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
            # Read the primary IPv4 address directly, without spawning ip/awk.
            request = struct.pack('256s', lan.encode())
            response = fcntl.ioctl(self.address_socket.fileno(), 0x8915, request)
            addr = socket.inet_ntoa(response[20:24])
            if addr.startswith(('127.', '169.254.')) or addr == '0.0.0.0':
                return '', ''
            return lan, addr
        except (OSError, ValueError):
            return '', ''

    def tick(self):
        lan, addr = self.lan_address()
        if not lan or not addr or not self.health():
            self.remove_rules()
            return
        def rule(protocol):
            return ('-i', lan, '-d', addr, '-p', protocol, '--dport', '53',
                    '-j', 'REDIRECT', '--to-ports', '5354')
        if not all(self.command('-C', CHAIN, *rule(protocol)) for protocol in ('udp', 'tcp')):
            self.command('-N', CHAIN)
            if not self.command('-F', CHAIN):
                raise RuntimeError('Cannot clear DNS interception chain')
            for protocol in ('udp', 'tcp'):
                if not self.command('-A', CHAIN, *rule(protocol)):
                    raise RuntimeError('Cannot install DNS interception rule')
        if not self.command('-C', 'PREROUTING', '-j', CHAIN):
            if not self.command('-I', 'PREROUTING', '1', '-j', CHAIN):
                raise RuntimeError('Cannot attach DNS interception chain')
        self.publish('active')

    def run(self):
        try:
            while not self.stopping.is_set():
                started = time.monotonic()
                try:
                    self.tick()
                except (OSError, RuntimeError, subprocess.SubprocessError):
                    # A partial rule install must not become a DNS black hole.
                    self.remove_rules()
                self.stopping.wait(max(0, INTERVAL - (time.monotonic() - started)))
        finally:
            try:
                self.remove_rules()
            finally:
                if self.address_socket is not None:
                    self.address_socket.close()


def stop_existing(base):
    try:
        pidfile = base / 'guard.pid'
        # The locked file is also a recovery identity if a stale controller
        # removed the pidfile during a live upgrade.
        identity = pidfile if pidfile.exists() else base / 'guard.lock'
        pid = int(identity.read_text().strip())
        if pid <= 1:
            return
        # Keep the bounded procfs read used by the old shell controller.
        result = subprocess.run(['/system/bin/timeout', '2', '/system/bin/cat',
                                 '/proc/' + str(pid) + '/cmdline'],
                                capture_output=True, timeout=3, check=False)
        args = result.stdout.split(b'\0')
        expected = os.fsencode(base / 'dns-guard.py')
        if result.returncode == 0 and expected in args and b'run' in args:
            os.kill(pid, signal.SIGTERM)
            deadline = time.monotonic() + 5
            while time.monotonic() < deadline:
                try:
                    stat = Path('/proc/' + str(pid) + '/stat').read_text()
                except FileNotFoundError:
                    break
                if stat.rsplit(') ', 1)[-1].split()[0] == 'Z':
                    break
                time.sleep(0.05)
    except (OSError, ValueError, subprocess.SubprocessError):
        return


def main():
    os.umask(0o077)
    action = sys.argv[1] if len(sys.argv) > 1 else 'run'
    if action == 'stop':
        stop_existing(BASE)
        # Also clean up after a previous SIGKILL/crash left a stale pidfile.
        Guard().remove_rules()
        return
    if action != 'run':
        raise SystemExit(2)
    BASE.mkdir(mode=0o700, parents=True, exist_ok=True)
    with (BASE / 'guard.lock').open('a') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise SystemExit('AdGuard DNS guard is already running')
        lock.seek(0)
        lock.truncate()
        lock.write(str(os.getpid()) + '\n')
        lock.flush()
        spec = importlib.util.spec_from_file_location('adguard_dns_health', BASE / 'dns-health.py')
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        guard = Guard(health=module.healthy)
        for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
            signal.signal(sig, lambda *_: guard.stopping.set())
        pidfile = BASE / 'guard.pid'
        pidfile.write_text(str(os.getpid()) + '\n')
        try:
            guard.run()
        finally:
            if pidfile.exists() and pidfile.read_text().strip() == str(os.getpid()):
                pidfile.unlink()


if __name__ == '__main__':
    main()
