#!/usr/bin/env python3
"""Live middle-hop termination: established and new flows cannot bypass it."""
import argparse
import hashlib
import random
import threading
import json
import socket
import sys
import time
from pathlib import Path

parser = argparse.ArgumentParser()
parser.add_argument("output")
parser.add_argument("--host", default="127.0.0.1")
args = parser.parse_args()
out = Path(args.output)
# Binary integrity over the actual route before fault injection. Randomized
# chunks make reordering, duplication and truncation visible to SHA-256.
file_bytes = 64 * 1024 * 1024
expected_hash, received_hash = hashlib.sha256(), hashlib.sha256()
errors = []
with socket.create_connection((args.host, 18080), timeout=30) as transfer:
    def send_file():
        try:
            random_bytes = random.Random(0x564f5254)
            for _ in range(file_bytes // 65536):
                block = random_bytes.randbytes(65536)
                expected_hash.update(block)
                transfer.sendall(block)
            transfer.shutdown(socket.SHUT_WR)
        except OSError as error:
            errors.append(str(error))
    sender = threading.Thread(target=send_file)
    sender.start()
    received_bytes = 0
    while True:
        data = transfer.recv(65536)
        if not data:
            break
        received_bytes += len(data)
        received_hash.update(data)
    sender.join(timeout=30)
    if sender.is_alive() or errors or received_bytes != file_bytes or expected_hash.digest() != received_hash.digest():
        raise SystemExit("64 MiB binary integrity/half-close failed")
(out / "binary-integrity.json").write_text(json.dumps({"status": "pass", "bytes": received_bytes,
    "sha256": received_hash.hexdigest(), "pattern": "deterministic pseudorandom 64 MiB"}, indent=2) + "\n")

tcp = socket.create_connection((args.host, 18080), timeout=5)
udp = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
udp.settimeout(5)
tcp.sendall(b"before-fault")
if tcp.recv(12) != b"before-fault":
    raise SystemExit("pre-fault TCP route is unavailable")
udp.sendto(b"before-fault", (args.host, 18082))
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
    udp.sendto(b"after-fault", (args.host, 18082))
    try:
        if udp.recv(100):
            raise SystemExit("UDP bypassed failed relay")
    except socket.timeout:
        pass
udp.close()
with socket.create_connection((args.host, 18080), timeout=3) as fresh:
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
