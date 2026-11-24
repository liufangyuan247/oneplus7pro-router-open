"""Run under `unshare -n python FILE ORIGINAL_NETNS` as root.

Tests actual DNS client traffic, kernel NAT, fail-open and recovery in two
temporary namespaces. Refuses to mutate the caller's original namespace.
"""
import importlib.util
import json
import os
from pathlib import Path
import socket
import signal
import struct
import subprocess
import sys
import tempfile
import threading
import time

PREFIX = '/data/data/com.termux/files/usr/bin/'
PYTHON = PREFIX + 'python'
IP = PREFIX + 'ip'
assert len(sys.argv) == 2 and os.readlink('/proc/self/ns/net') != sys.argv[1], 'Must use an isolated network namespace'


def load(name, file):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(file))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


worker = load('dns_guard', 'dns-guard.py')
health = load('dns_health', 'dns-health.py')


def run(*args):
    result = subprocess.run(args, capture_output=True, text=True, timeout=8, check=False)
    if result.returncode:
        raise RuntimeError('Sandbox command failed: ' + result.stderr.strip())
    return result.stdout.strip()


stop = threading.Event()
respond = threading.Event()
respond.set()
sockets = []
threads = []


def dns_server(port, answer):
    server = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    server.bind(('0.0.0.0', port))
    server.settimeout(0.2)
    sockets.append(server)
    def loop():
        while not stop.is_set():
            try:
                query, peer = server.recvfrom(4096)
            except socket.timeout:
                continue
            if port == 5354 and not respond.is_set():
                continue
            reply = (query[:2] + struct.pack('!HHHHH', 0x8180, 1, 1, 0, 0)
                     + query[12:] + b'\xc0\x0c' + struct.pack('!HHIH', 1, 1, 1, 4)
                     + socket.inet_aton(answer))
            server.sendto(reply, peer)
    thread = threading.Thread(target=loop)
    thread.start()
    threads.append(thread)


query_code = '''import socket,struct,sys
q=b'ab'+struct.pack('!HHHHH',256,1,0,0,0)+b'\\x07example\\x03com\\0'+struct.pack('!HH',1,1)
with socket.socket(socket.AF_INET,socket.SOCK_DGRAM) as s:
 s.settimeout(2);s.connect((sys.argv[1],53));s.send(q);r=s.recv(4096)
 assert r[:2]==b'ab'
 print(socket.inet_ntoa(r[-4:]))
'''
client = subprocess.Popen(['/system/bin/unshare', '-n', '/system/bin/sleep', '120'])
try:
    original = os.readlink('/proc/self/ns/net')
    for _ in range(100):
        if os.readlink('/proc/' + str(client.pid) + '/ns/net') != original:
            break
        time.sleep(0.01)
    else:
        raise RuntimeError('Client namespace did not start')
    def peer(*args):
        return run('/system/bin/nsenter', '-t', str(client.pid), '-n', '--', *args)
    run(IP, 'link', 'set', 'lo', 'up')
    run(IP, 'link', 'add', 'lancheck0', 'type', 'veth', 'peer', 'name', 'clientcheck0')
    run(IP, 'link', 'set', 'clientcheck0', 'netns', str(client.pid))
    run(IP, 'addr', 'add', '192.168.2.1/24', 'dev', 'lancheck0')
    run(IP, 'link', 'set', 'lancheck0', 'up')
    peer(IP, 'link', 'set', 'lo', 'up')
    peer(IP, 'addr', 'add', '192.168.2.2/24', 'dev', 'clientcheck0')
    peer(IP, 'link', 'set', 'clientcheck0', 'up')
    dns_server(5354, '127.0.0.1')
    dns_server(53, '192.0.2.9')
    with tempfile.TemporaryDirectory(prefix='dns-guard-netns-') as temp:
        base = Path(temp)
        interfaces = base / 'interfaces'
        interfaces.write_text('wancheck0 lancheck0 ppp0\n')
        guard = worker.Guard(base=base, interfaces=interfaces, health=health.healthy)
        def query(address='192.168.2.1'):
            return peer(PYTHON, '-c', query_code, address)
        try:
            assert guard.lan_address() == ('lancheck0', '192.168.2.1')
            guard.tick()
            assert query() == '127.0.0.1'
            respond.clear()
            guard.tick()
            assert query() == '192.0.2.9'
            respond.set()
            guard.tick()
            assert query() == '127.0.0.1'
            assert guard.command('-D', 'PREROUTING', '-j', worker.CHAIN)
            guard.tick()
            assert query() == '127.0.0.1'
            assert guard.command('-D', worker.CHAIN, '-i', 'lancheck0', '-d', '192.168.2.1',
                                 '-p', 'tcp', '--dport', '53', '-j', 'REDIRECT', '--to-ports', '5354')
            guard.tick()
            assert guard.command('-C', worker.CHAIN, '-i', 'lancheck0', '-d', '192.168.2.1',
                                 '-p', 'tcp', '--dport', '53', '-j', 'REDIRECT', '--to-ports', '5354')
            run(IP, 'addr', 'del', '192.168.2.1/24', 'dev', 'lancheck0')
            run(IP, 'addr', 'add', '192.168.3.1/24', 'dev', 'lancheck0')
            peer(IP, 'addr', 'del', '192.168.2.2/24', 'dev', 'clientcheck0')
            peer(IP, 'addr', 'add', '192.168.3.2/24', 'dev', 'clientcheck0')
            guard.tick()
            assert query('192.168.3.1') == '127.0.0.1'
            guard.remove_rules()
            assert query('192.168.3.1') == '192.0.2.9'
            # Test the real entry point/stop path against private state files.
            source = Path(__file__).with_name('dns-guard.py').read_text()
            source = source.replace("BASE = Path('/data/adb/router-adguardhome')",
                                    'BASE = Path(' + repr(str(base)) + ')')
            source = source.replace("INTERFACES = Path('/data/data/com.termux/files/home/router-data/admin/active-interfaces')",
                                    'INTERFACES = Path(' + repr(str(interfaces)) + ')')
            script = base / 'dns-guard.py'
            script.write_text(source)
            (base / 'dns-health.py').write_text(Path(__file__).with_name('dns-health.py').read_text())
            daemon = subprocess.Popen([PYTHON, str(script), 'run'], stdout=subprocess.DEVNULL)
            try:
                for _ in range(100):
                    if (base / 'guard.pid').exists() and (base / 'state').read_text().strip() == 'active':
                        break
                    assert daemon.poll() is None
                    time.sleep(0.05)
                else:
                    raise RuntimeError('Isolated daemon failed to activate')
                assert query('192.168.3.1') == '127.0.0.1'
                duplicate = subprocess.run([PYTHON, str(script), 'run'], capture_output=True, timeout=5)
                assert duplicate.returncode != 0
                assert query('192.168.3.1') == '127.0.0.1'
                (base / 'guard.pid').unlink()
                run(PYTHON, str(script), 'stop')
                assert daemon.wait(timeout=5) == 0
                assert query('192.168.3.1') == '192.0.2.9'
            finally:
                if daemon.poll() is None:
                    daemon.send_signal(signal.SIGTERM)
                    daemon.wait(timeout=5)
            print(json.dumps({'dns_client_nat': True, 'failure_bypass': True, 'recovery': True,
                              'jump_repair': True, 'tcp_rule_repair': True,
                              'address_change': True, 'shutdown_bypass': True,
                              'daemon_singleton': True, 'stop_with_missing_pidfile': True}))
        finally:
            guard.remove_rules()
            if guard.address_socket is not None:
                guard.address_socket.close()
finally:
    stop.set()
    for thread in threads:
        thread.join(timeout=2)
    for server in sockets:
        server.close()
    client.terminate()
    client.wait(timeout=5)
