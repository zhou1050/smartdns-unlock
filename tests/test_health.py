#!/usr/bin/env python3
import json
import socket
import struct
import subprocess
import sys
import tempfile
import threading
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
CHECKER = ROOT / "scripts/check_upstreams.py"


class FakeDns:
    def __init__(self, port=0):
        self.sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        self.sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        self.sock.bind(("127.0.0.1", port))
        self.port = self.sock.getsockname()[1]
        self.thread = threading.Thread(target=self.run, daemon=True)
        self.thread.start()

    def run(self):
        while True:
            try:
                query, peer = self.sock.recvfrom(4096)
            except OSError:
                return
            if len(query) < 12:
                continue
            header = query[:2] + struct.pack("!HHHHH", 0x8180, 1, 1, 0, 0)
            answer = b"\xc0\x0c\x00\x01\x00\x01\x00\x00\x00\x3c\x00\x04\x01\x01\x01\x01"
            try:
                self.sock.sendto(header + query[12:] + answer, peer)
            except OSError:
                return

    def close(self):
        self.sock.close()
        self.thread.join(timeout=1)


with tempfile.TemporaryDirectory() as directory:
    directory = Path(directory)
    state = directory / "state.json"
    fallback = directory / "fallback.groups"
    upstreams = directory / "upstreams.tsv"
    server = FakeDns()
    port = server.port
    upstreams.write_text(f"default|primary|udp|127.0.0.1:{port}|\n", encoding="utf-8")

    def check():
        output = subprocess.check_output([
            sys.executable, str(CHECKER),
            "--upstreams", str(upstreams),
            "--state", str(state),
            "--fallback-groups", str(fallback),
            "--fail-threshold", "2",
            "--recover-threshold", "2",
            "--cooldown", "0",
            "--timeout", "0.2",
        ], text=True)
        return json.loads(output)

    assert check()["groups"]["default"]["mode"] == "unlock"
    server.close()
    check()
    assert fallback.read_text() == ""
    assert check()["groups"]["default"]["mode"] == "public"
    assert fallback.read_text() == "default\n"

    server = FakeDns(port)
    check()
    assert fallback.read_text() == "default\n"
    assert check()["groups"]["default"]["mode"] == "unlock"
    assert fallback.read_text() == ""
    server.close()

print("health failover hysteresis OK")
