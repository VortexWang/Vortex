#!/usr/bin/env python3
import argparse
import json
import selectors
import socket
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

parser = argparse.ArgumentParser()
parser.add_argument("--host", default="127.0.0.1")
parser.add_argument("--port", type=int, default=18081)
parser.add_argument("--metrics-host", default=None)
parser.add_argument("--metrics-port", type=int, default=18083)
args = parser.parse_args()
stats = {"tcp_bytes_received": 0, "tcp_bytes_sent": 0, "udp_datagrams_received": 0,
         "udp_datagrams_sent": 0, "udp_bytes_received": 0, "udp_bytes_sent": 0}
stats_lock = threading.Lock()

class MetricsHandler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path != "/stats":
            self.send_error(404)
            return
        with stats_lock:
            body = json.dumps(stats, sort_keys=True).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *_):
        pass

metrics = ThreadingHTTPServer((args.metrics_host or args.host, args.metrics_port), MetricsHandler)
threading.Thread(target=metrics.serve_forever, daemon=True).start()
selector = selectors.DefaultSelector()
tcp = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
tcp.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
tcp.bind((args.host, args.port))
tcp.listen()
tcp.setblocking(False)
udp = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
udp.bind((args.host, args.port))
udp.setblocking(False)
selector.register(tcp, selectors.EVENT_READ, "tcp")
selector.register(udp, selectors.EVENT_READ, "udp")

def echo_tcp(conn):
    conn.setblocking(True)
    with conn:
        while True:
            payload = conn.recv(65535)
            if not payload:
                return
            with stats_lock:
                stats["tcp_bytes_received"] += len(payload)
            conn.sendall(payload)
            with stats_lock:
                stats["tcp_bytes_sent"] += len(payload)

try:
    while True:
        for key, _ in selector.select():
            if key.data == "udp":
                payload, peer = udp.recvfrom(65535)
                sent = udp.sendto(payload, peer)
                with stats_lock:
                    stats["udp_datagrams_received"] += 1
                    stats["udp_datagrams_sent"] += 1
                    stats["udp_bytes_received"] += len(payload)
                    stats["udp_bytes_sent"] += sent
                continue
            if key.data == "tcp":
                conn, _ = tcp.accept()
                threading.Thread(target=echo_tcp, args=(conn,), daemon=True).start()
except KeyboardInterrupt:
    pass
finally:
    tcp.close()
    udp.close()
    metrics.shutdown()
    metrics.server_close()

