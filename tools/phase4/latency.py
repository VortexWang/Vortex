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
timeline = []
for i in range(a.samples + 5):
    payload = ("latency-%04d" % i).encode()
    start = time.perf_counter_ns()
    with socket.create_connection((a.host, a.port), timeout=5) as conn:
        connected = time.perf_counter_ns()
        conn.sendall(payload)
        sent = time.perf_counter_ns()
        response = conn.recv(len(payload))
        first_byte_ns = time.perf_counter_ns()
        while len(response) < len(payload):
            part = conn.recv(len(payload) - len(response))
            if not part:
                raise SystemExit("latency echo truncated")
            response += part
        complete = time.perf_counter_ns()
        if response != payload:
            raise SystemExit("latency echo mismatch")
        conn.shutdown(socket.SHUT_WR)
        if conn.recv(1):
            raise SystemExit("unexpected latency response data")
    closed = time.perf_counter_ns()
    if i >= 5:
        samples.append((first_byte_ns - start) / 1e6)
        timeline.append({
            "sample": i - 5,
            "connect_ms": (connected - start) / 1e6,
            "send_ms": (sent - connected) / 1e6,
            "first_byte_ms": (first_byte_ns - start) / 1e6,
            "complete_ms": (complete - start) / 1e6,
            "close_ms": (closed - start) / 1e6,
        })
Path(a.output).write_text(json.dumps({
    "schema": 1, "measurement": "fresh-client-connect-to-first-byte",
    "samples_ms": samples, "median_ms": statistics.median(samples),
    "warmup_connections": 5,
    "timeline": timeline,
    "timeline_clock": "monotonic userspace timestamps; intervals are relative to connect attempt",
    "scope": "runtime actual connection policy; no assertion that pool and mux modes are both implemented",
}, indent=2, sort_keys=True) + "\n")
