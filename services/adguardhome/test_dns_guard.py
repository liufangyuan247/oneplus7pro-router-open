"""Fault/recovery tests; no production interfaces or firewall are touched."""
import importlib.util
from pathlib import Path
import socket
import struct
import tempfile
import threading
import unittest
from unittest.mock import patch


def load(name, file):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(file))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


guard = load('dns_guard', 'dns-guard.py')
health = load('dns_health', 'dns-health.py')


class Firewall:
    def __init__(self):
        self.exists = False
        self.rules = []
        self.jumps = 0
        self.fail_add = False
        self.mutations = 0

    def command(self, op, chain, *args):
        if op == '-C':
            return bool(self.jumps) if chain == 'PREROUTING' else tuple(args) in self.rules
        self.mutations += 1
        if op == '-N':
            self.exists = True
        elif op == '-F':
            self.rules.clear()
            return self.exists
        elif op == '-X':
            self.exists = False
        elif op == '-A':
            if self.fail_add:
                return False
            self.rules.append(tuple(args))
        elif op == '-I':
            self.jumps += 1
        elif op == '-D':
            self.jumps -= 1
        else:
            raise AssertionError(op)
        return True


class GuardTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.firewall = Firewall()
        self.ok = True
        self.address = ('eth1', '192.168.2.1')
        self.worker = guard.Guard(base=Path(self.temp.name), health=lambda: self.ok)
        self.worker.command = self.firewall.command
        self.worker.lan_address = lambda: self.address

    def test_healthy_installs_both_protocols(self):
        self.worker.tick()
        self.assertEqual(self.firewall.jumps, 1)
        self.assertEqual(len(self.firewall.rules), 2)
        self.assertEqual(self.worker.state.read_text().strip(), 'active')

    def test_steady_state_does_not_rewrite_rules_or_state(self):
        self.worker.tick()
        before = self.firewall.mutations
        stamp = self.worker.state.stat().st_mtime_ns
        self.worker.tick()
        self.assertEqual(self.firewall.mutations, before)
        self.assertEqual(self.worker.state.stat().st_mtime_ns, stamp)

    def test_dns_failure_removes_interception_then_recovers(self):
        self.worker.tick()
        self.ok = False
        self.worker.tick()
        self.assertEqual(self.firewall.jumps, 0)
        self.assertEqual(self.worker.state.read_text().strip(), 'bypass')
        self.ok = True
        self.worker.tick()
        self.assertEqual(self.firewall.jumps, 1)

    def test_lost_jump_is_repaired_next_cycle(self):
        self.worker.tick()
        self.firewall.jumps = 0
        self.worker.tick()
        self.assertEqual(self.firewall.jumps, 1)

    def test_lost_tcp_rule_is_repaired_next_cycle(self):
        self.worker.tick()
        self.firewall.rules.pop()
        self.worker.tick()
        self.assertEqual(len(self.firewall.rules), 2)

    def test_address_change_replaces_old_targets(self):
        self.worker.tick()
        self.address = ('eth2', '192.168.4.1')
        self.worker.tick()
        self.assertTrue(all('eth2' in x and '192.168.4.1' in x for x in self.firewall.rules))

    def test_interface_disappearance_fails_open(self):
        self.worker.tick()
        self.address = ('', '')
        self.worker.tick()
        self.assertEqual(self.firewall.jumps, 0)

    def test_partial_install_fails_open(self):
        self.worker.tick()
        self.firewall.rules.pop()
        self.firewall.fail_add = True
        original = self.worker.publish
        def stop_after_bypass(state):
            original(state)
            if state == 'bypass':
                self.worker.stopping.set()
        self.worker.publish = stop_after_bypass
        self.worker.run()
        self.assertEqual(self.firewall.jumps, 0)

    def test_duplicate_jumps_are_removed(self):
        self.worker.tick()
        self.firewall.jumps = 3
        self.worker.remove_rules()
        self.assertEqual(self.firewall.jumps, 0)

    def test_no_interface_does_not_probe_dns(self):
        self.address = ('', '')
        self.worker.health = lambda: self.fail('Must not probe without LAN')
        self.worker.tick()

    def test_stop_never_signals_unrelated_pid(self):
        (Path(self.temp.name) / 'guard.pid').write_text('12345')
        with patch.object(guard.subprocess, 'run') as run, patch.object(guard.os, 'kill') as kill:
            run.return_value.returncode = 0
            run.return_value.stdout = b'python\0/unrelated.py\0run\0'
            guard.stop_existing(Path(self.temp.name))
            kill.assert_not_called()


class HealthTests(unittest.TestCase):
    def query(self, transform):
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as server:
            server.bind(('127.0.0.1', 0))
            port = server.getsockname()[1]
            def respond():
                query, peer = server.recvfrom(4096)
                reply = (query[:2] + struct.pack('!HHHHH', 0x8180, 1, 1, 0, 0)
                         + query[12:] + b'\xc0\x0c' + struct.pack('!HHIH', 1, 1, 1, 4)
                         + socket.inet_aton('127.0.0.1'))
                server.sendto(transform(reply), peer)
            thread = threading.Thread(target=respond)
            thread.start()
            try:
                return health.healthy(port=port)
            finally:
                thread.join(timeout=2)

    def test_valid_answer(self):
        self.assertTrue(self.query(lambda x: x))

    def test_wrong_transaction(self):
        self.assertFalse(self.query(lambda x: bytes([x[0] ^ 1]) + x[1:]))

    def test_nxdomain(self):
        self.assertFalse(self.query(lambda x: x[:3] + b'\x83' + x[4:]))

    def test_no_answers(self):
        self.assertFalse(self.query(lambda x: x[:6] + b'\0\0' + x[8:]))

    def test_wrong_rewrite_address(self):
        self.assertFalse(self.query(lambda x: x[:-4] + socket.inet_aton('192.168.2.1')))

    def test_truncated_answer(self):
        self.assertFalse(self.query(lambda x: x[:8]))

    def test_unavailable_server(self):
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as server:
            server.bind(('127.0.0.1', 0))
            port = server.getsockname()[1]
        self.assertFalse(health.healthy(port=port))


if __name__ == '__main__':
    unittest.main()
