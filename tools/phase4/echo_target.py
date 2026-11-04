#!/usr/bin/env python3
import argparse
import selectors
import socket
import threading

parser = argparse.ArgumentParser()
parser.add_argument("--host", default="127.0.0.1")
parser.add_argument("--port", type=int, default=18081)
args = parser.parse_args()
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
            conn.sendall(payload)

try:
    while True:
        for key, _ in selector.select():
            if key.data == "udp":
                payload, peer = udp.recvfrom(65535)
                udp.sendto(payload, peer)
                continue
            if key.data == "tcp":
                conn, _ = tcp.accept()
                threading.Thread(target=echo_tcp, args=(conn,), daemon=True).start()
except KeyboardInterrupt:
    pass
finally:
    tcp.close()
    udp.close()

