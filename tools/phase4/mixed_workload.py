#!/usr/bin/env python3
"""Run a same-route TCP baseline followed by concurrent TCP and paced UDP."""
import argparse
import json
import multiprocessing
import selectors
import socket
import struct
import threading
import time
import urllib.request
from pathlib import Path


parser = argparse.ArgumentParser()
parser.add_argument("--host", default="127.0.0.1")
parser.add_argument("--tcp-port", type=int, default=18080)
parser.add_argument("--udp-port", type=int, default=18082)
parser.add_argument("--metrics", default="http://127.0.0.1:18083/stats")
parser.add_argument("--duration", type=float, default=5)
parser.add_argument("--tcp-streams", type=int, default=4)
parser.add_argument("--udp-mbps", type=float, default=100)
parser.add_argument("--output", required=True)
args = parser.parse_args()


def target_stats():
    with urllib.request.urlopen(args.metrics, timeout=3) as response:
        return json.load(response)


def kernel_udp_stats():
    path = Path("/proc/net/snmp")
    if not path.exists():
        return {}
    lines = path.read_text().splitlines()
    for index, line in enumerate(lines[:-1]):
        if line.startswith("Udp:"):
            return dict(zip(line.split()[1:], map(int, lines[index + 1].split()[1:])))
    return {}


def tcp_phase(seconds):
    totals = {"sent_bytes": 0, "received_bytes": 0, "connections": 0, "errors": [], "first_byte_latency_ms": []}
    payload = b"v" * 65536
    max_inflight = 256 * 1024
    selector = selectors.DefaultSelector()
    states = []
    start = time.monotonic()
    for _ in range(args.tcp_streams):
        try:
            conn = socket.create_connection((args.host, args.tcp_port), timeout=5)
            conn.setblocking(False)
            state = {"socket": conn, "outstanding": 0, "pending": payload,
                     "write_closed": False, "eof": False, "first_sent": None, "first_received": False}
            selector.register(conn, selectors.EVENT_READ | selectors.EVENT_WRITE, state)
            states.append(state)
            totals["connections"] += 1
        except OSError as error:
            totals["errors"].append("TCP connect: " + str(error))
    stop_at = start + seconds
    drain_deadline = stop_at + 30
    stopped = False
    while any(not state["eof"] for state in states):
        now = time.monotonic()
        if not stopped and now >= stop_at:
            stopped = True
            for state in states:
                try:
                    state["socket"].shutdown(socket.SHUT_WR)
                    state["write_closed"] = True
                except OSError as error:
                    totals["errors"].append("TCP half-close: " + str(error))
                    state["eof"] = True
        if stopped and now >= drain_deadline:
            totals["errors"].append("TCP response drain timed out")
            break
        for key, mask in selector.select(0.01):
            state = key.data
            conn = state["socket"]
            if mask & selectors.EVENT_READ:
                while True:
                    try:
                        block = conn.recv(65536)
                    except BlockingIOError:
                        break
                    except OSError as error:
                        totals["errors"].append("TCP recv: " + str(error))
                        state["eof"] = True
                        break
                    if not block:
                        state["eof"] = True
                        break
                    if block != b"v" * len(block):
                        totals["errors"].append("TCP echo payload mismatch")
                    if not state["first_received"] and state["first_sent"] is not None:
                        totals["first_byte_latency_ms"].append((time.monotonic() - state["first_sent"]) * 1000)
                        state["first_received"] = True
                    totals["received_bytes"] += len(block)
                    state["outstanding"] -= len(block)
            if not state["eof"] and not state["write_closed"] and mask & selectors.EVENT_WRITE:
                while state["outstanding"] < max_inflight:
                    try:
                        sent = conn.send(state["pending"])
                    except BlockingIOError:
                        break
                    except OSError as error:
                        totals["errors"].append("TCP send: " + str(error))
                        state["eof"] = True
                        break
                    if sent <= 0:
                        break
                    if state["first_sent"] is None:
                        state["first_sent"] = time.monotonic()
                    totals["sent_bytes"] += sent
                    state["outstanding"] += sent
                    state["pending"] = state["pending"][sent:]
                    if not state["pending"]:
                        state["pending"] = payload
            if state["eof"]:
                selector.unregister(conn)
                conn.close()
            else:
                events = selectors.EVENT_READ
                if not state["write_closed"] and state["outstanding"] < max_inflight:
                    events |= selectors.EVENT_WRITE
                selector.modify(conn, events, state)
    selector.close()
    for state in states:
        if not state["eof"]:
            state["socket"].close()
    elapsed = time.monotonic() - start
    totals["duration_seconds"] = elapsed
    totals["throughput_mbps"] = totals["received_bytes"] * 8 / elapsed / 1_000_000
    return totals


def mixed_phase(seconds):
    stop = threading.Event()
    totals = {"udp_sent": 0, "udp_received": 0, "udp_unique_received": 0,
              "udp_bytes_received": 0, "tcp": None, "errors": []}
    received_ids = set()
    lock = threading.Lock()
    udp = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    udp.setsockopt(socket.SOL_SOCKET, socket.SO_RCVBUF, 4 * 1024 * 1024)
    totals["udp_socket_receive_buffer_bytes"] = udp.getsockopt(socket.SOL_SOCKET, socket.SO_RCVBUF)
    udp.settimeout(0.1)
    end = time.monotonic() + seconds
    packet_size = 1200
    interval = packet_size * 8 / (args.udp_mbps * 1_000_000)

    def receive_udp():
        while time.monotonic() < end + 2:
            try:
                data, _ = udp.recvfrom(2048)
            except socket.timeout:
                continue
            except OSError:
                return
            if len(data) != packet_size:
                with lock:
                    totals["errors"].append("UDP echo size mismatch")
                continue
            sequence = struct.unpack("!Q", data[:8])[0]
            if data[8:] != bytes([sequence & 0xff]) * (packet_size - 8):
                with lock:
                    totals["errors"].append("UDP echo payload mismatch")
                continue
            with lock:
                totals["udp_received"] += 1
                totals["udp_bytes_received"] += len(data)
                received_ids.add(sequence)

    receiver = threading.Thread(target=receive_udp, daemon=True)
    tcp_result = {}
    process_context = multiprocessing.get_context("fork")
    parent_pipe, child_pipe = process_context.Pipe(duplex=False)

    def run_tcp():
        child_pipe.send(tcp_phase(seconds))
        child_pipe.close()

    tcp_process = process_context.Process(target=run_tcp)
    tcp_process.start()
    child_pipe.close()
    receiver.start()
    next_send = time.monotonic()
    sequence = 0
    while time.monotonic() < end:
        body = struct.pack("!Q", sequence) + bytes([sequence & 0xff]) * (packet_size - 8)
        try:
            udp.sendto(body, (args.host, args.udp_port))
            totals["udp_sent"] += 1
        except OSError as error:
            totals["errors"].append(str(error))
        sequence += 1
        next_send += interval
        delay = next_send - time.monotonic()
        if delay > 0:
            time.sleep(delay)
    stop.set()
    if parent_pipe.poll(40):
        tcp_result = parent_pipe.recv()
    else:
        totals["errors"].append("TCP workload process did not finish drain")
        tcp_result = {"throughput_mbps": 0, "received_bytes": 0, "sent_bytes": 0,
                      "connections": 0, "errors": ["TCP workload did not finish"]}
        tcp_process.terminate()
    parent_pipe.close()
    tcp_process.join(timeout=3)
    receiver.join(timeout=4)
    udp.close()
    totals["tcp"] = tcp_result
    totals["udp_unique_received"] = len(received_ids)
    totals["udp_loss"] = (totals["udp_sent"] - totals["udp_unique_received"]) / max(1, totals["udp_sent"])
    totals["udp_payload_mbps"] = totals["udp_bytes_received"] * 8 / seconds / 1_000_000
    return totals


kernel_before = kernel_udp_stats()
before_baseline = target_stats()
baseline = tcp_phase(args.duration)
after_baseline = target_stats()
before_mixed = after_baseline
mixed = mixed_phase(args.duration)
after_mixed = target_stats()
kernel_after = kernel_udp_stats()

def diff(after, before):
    return {key: after[key] - before[key] for key in after}

report = {
    "schema": 1,
    "host": args.host,
    "duration_seconds_per_phase": args.duration,
    "tcp_streams": args.tcp_streams,
    "udp_target_mbps": args.udp_mbps,
    "baseline": baseline,
    "mixed": mixed,
    "tcp_retention": mixed["tcp"]["throughput_mbps"] / max(0.001, baseline["throughput_mbps"]),
    "target_baseline": diff(after_baseline, before_baseline),
    "target_mixed": diff(after_mixed, before_mixed),
    "target_total": diff(after_mixed, before_baseline),
    "kernel_udp_host_delta": {key: kernel_after[key] - kernel_before[key] for key in kernel_before},
}
with open(args.output, "w", encoding="utf-8") as result:
    json.dump(report, result, indent=2, sort_keys=True)
    result.write("\n")
print(json.dumps(report, sort_keys=True))
if baseline["errors"] or mixed["errors"] or mixed["tcp"]["errors"]:
    raise SystemExit("mixed workload contains transport errors")
if baseline["connections"] != args.tcp_streams or mixed["tcp"]["connections"] != args.tcp_streams:
    raise SystemExit("TCP workload did not establish all requested flows")
