#!/usr/bin/env python3
"""Live middle-hop termination: established and new flows cannot bypass it."""
import json
import socket
import sys
import time
from pathlib import Path

out = Path(sys.argv[1])
tcp = socket.create_connection(("127.0.0.1", 18080), timeout=5)
udp = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
udp.settimeout(5)
tcp.sendall(b"before-fault")
if tcp.recv(12) != b"before-fault":
    raise SystemExit("pre-fault TCP route is unavailable")
udp.sendto(b"before-fault", ("127.0.0.1", 18082))
if udp.recv(100) != b"before-fault":
    raise SystemExit("pre-fault UDP route is unavailable")
(out / "fault-ready").touch()
deadline = time.monotonic() + 20
while not (out / "fault-injected").exists():
    if time.monotonic() >= deadline:
        raise SystemExit("fault injection timed out")
    time.sleep(.05)
time.sleep(1)
tcp.settimeout(3)
try:
    tcp.sendall(b"after-fault")
    data = tcp.recv(100)
    if data:
        raise SystemExit("established TCP bypassed failed relay")
except (BrokenPipeError, ConnectionResetError):
    pass
except socket.timeout:
    raise SystemExit("established TCP did not terminate after link loss")
finally:
    tcp.close()
udp.settimeout(.5)
for i in range(3):
    udp.sendto(b"after-fault", ("127.0.0.1", 18082))
    try:
        if udp.recv(100):
            raise SystemExit("UDP bypassed failed relay")
    except socket.timeout:
        pass
udp.close()
with socket.create_connection(("127.0.0.1", 18080), timeout=3) as fresh:
    fresh.settimeout(3)
    fresh.sendall(b"new-after-fault")
    try:
        if fresh.recv(100):
            raise SystemExit("new TCP bypassed failed relay")
    except (ConnectionResetError, socket.timeout):
        pass
(out / "middle-hop-fault.json").write_text(json.dumps({
    "status": "pass", "fault": "relay-2 process terminated",
    "established_tcp": "terminated", "udp": "no forwarding",
    "new_tcp": "no forwarding", "automatic_replay": False,
    "scope": "single failure, configured route; no alternate authorized path configured",
}, indent=2) + "\n")
