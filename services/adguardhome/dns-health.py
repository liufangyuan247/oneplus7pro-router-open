"""Local DNS liveness check; requires the dedicated rewrite configured at install."""
import secrets
import socket
import struct

try:
    ident = secrets.token_bytes(2)
    name = b"".join(bytes([len(x)]) + x for x in b"adguard-health.invalid".split(b".")) + b"\0"
    query = ident + struct.pack("!HHHHH", 0x100, 1, 0, 0, 0) + name + struct.pack("!HH", 1, 1)
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
        sock.settimeout(1)
        sock.connect(("127.0.0.1", 5354))
        sock.send(query)
        reply = sock.recv(4096)
    assert reply[:2] == ident and len(reply) >= 16
    assert reply[3] & 15 == 0 and struct.unpack("!H", reply[6:8])[0] > 0
    assert reply[-4:] == socket.inet_aton("127.0.0.1")
except Exception:
    raise SystemExit(1)
