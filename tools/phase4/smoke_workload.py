#!/usr/bin/env python3
import argparse
import socket
from pathlib import Path

p = argparse.ArgumentParser()
p.add_argument("output")
p.add_argument("--host", default="127.0.0.1")
a = p.parse_args()
with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as client:
    client.settimeout(10)
    client.sendto(b"vortex-three-relay-udp-smoke", (a.host, 18082))
    response, _ = client.recvfrom(4096)
if response != b"vortex-three-relay-udp-smoke":
    raise SystemExit("UDP smoke payload mismatch")
Path(a.output, "udp-client.out").write_bytes(response)
with socket.create_connection((a.host, 18080), 10) as client:
    client.sendall(b"vortex-three-relay-smoke")
    client.shutdown(socket.SHUT_WR)
    data = b""
    while True:
        part = client.recv(4096)
        if not part:
            break
        data += part
if data != b"vortex-three-relay-smoke":
    raise SystemExit("TCP smoke payload/half-close mismatch")
Path(a.output, "client.out").write_bytes(data)
