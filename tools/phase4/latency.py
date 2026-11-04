#!/usr/bin/env python3
"""Fresh-connection client-connect to complete first echo, with raw samples."""
import argparse
import json
import socket
import statistics
import time
from pathlib import Path

p = argparse.ArgumentParser()
p.add_argument("--host", default="127.0.0.1")
p.add_argument("--port", type=int, default=18080)
p.add_argument("--samples", type=int, default=50)
p.add_argument("--output", required=True)
a = p.parse_args()
samples = []
for i in range(a.samples + 5):
    payload = ("latency-%04d" % i).encode()
    start = time.perf_counter_ns()
    with socket.create_connection((a.host, a.port), timeout=5) as conn:
        conn.sendall(payload)
        response = conn.recv(len(payload))
        first_byte_ns = time.perf_counter_ns()
        while len(response) < len(payload):
            part = conn.recv(len(payload) - len(response))
            if not part:
                raise SystemExit("latency echo truncated")
            response += part
        if response != payload:
            raise SystemExit("latency echo mismatch")
        conn.shutdown(socket.SHUT_WR)
        if conn.recv(1):
            raise SystemExit("unexpected latency response data")
    if i >= 5:
        samples.append((first_byte_ns - start) / 1e6)
Path(a.output).write_text(json.dumps({
    "schema": 1, "measurement": "fresh-client-connect-to-first-byte",
    "samples_ms": samples, "median_ms": statistics.median(samples),
    "warmup_connections": 5,
    "scope": "runtime actual connection policy; no assertion that pool and mux modes are both implemented",
}, indent=2, sort_keys=True) + "\n")
