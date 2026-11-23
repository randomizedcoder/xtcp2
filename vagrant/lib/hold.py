#!/usr/bin/env python3
"""hold.py — one scrape-slot holder: open a scrape path, shrink SO_RCVBUF,
never read.

The server's response write then blocks once the receive window fills, so the
promhttp handler stays in flight (a MaxRequestsInFlight slot is held) until
this process exits or reads. Usage: hold.py <url> <bearer> <seconds>.
"""
import socket
import sys
import time
from urllib.parse import urlsplit

url, bearer, secs = sys.argv[1], sys.argv[2], float(sys.argv[3])
u = urlsplit(url)
host = u.hostname
port = u.port or 80
path = u.path or "/"

s = socket.create_connection((host, port), timeout=secs)
s.setsockopt(socket.SOL_SOCKET, socket.SO_RCVBUF, 2048)
s.sendall(
    (
        f"GET {path} HTTP/1.1\r\nHost: {host}\r\n"
        f"Authorization: Bearer {bearer}\r\nConnection: keep-alive\r\n\r\n"
    ).encode()
)
time.sleep(secs)
