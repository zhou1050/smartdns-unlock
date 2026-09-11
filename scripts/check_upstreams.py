#!/usr/bin/env python3
import argparse
import concurrent.futures
import json
import os
import random
import socket
import ssl
import struct
import tempfile
import time
import urllib.parse
import urllib.request
from pathlib import Path


PROBE_NAME = "www.cloudflare.com"


def dns_query():
    query_id = random.randrange(0, 65536)
    labels = b"".join(bytes([len(x)]) + x.encode("ascii") for x in PROBE_NAME.split(".")) + b"\0"
    packet = struct.pack("!HHHHHH", query_id, 0x0100, 1, 0, 0, 0) + labels + struct.pack("!HH", 1, 1)
    return query_id, packet


def valid_response(data, query_id):
    if len(data) < 12:
        return False
    response_id, flags, _, answers, _, _ = struct.unpack("!HHHHHH", data[:12])
    return response_id == query_id and flags & 0x8000 and flags & 0x000F == 0 and answers > 0


def recv_exact(sock, size):
    chunks = []
    remaining = size
    while remaining:
        chunk = sock.recv(remaining)
        if not chunk:
            raise OSError("连接提前关闭")
        chunks.append(chunk)
        remaining -= len(chunk)
    return b"".join(chunks)


def host_port(endpoint, default_port):
    value = endpoint
    if "://" not in value:
        value = "//" + value
    parsed = urllib.parse.urlparse(value)
    host = parsed.hostname
    if not host:
        raise ValueError("无法识别服务器地址")
    return host, parsed.port or default_port


def probe_doh(endpoint, timeout):
    query_id, packet = dns_query()
    request = urllib.request.Request(
        endpoint,
        data=packet,
        method="POST",
        headers={"Accept": "application/dns-message", "Content-Type": "application/dns-message"},
    )
    with urllib.request.urlopen(request, timeout=timeout) as response:
        data = response.read(65536)
    if not valid_response(data, query_id):
        raise OSError("DoH 返回无效 DNS 响应")


def probe_dot(endpoint, timeout):
    host, port = host_port(endpoint, 853)
    query_id, packet = dns_query()
    context = ssl.create_default_context()
    with socket.create_connection((host, port), timeout=timeout) as raw:
        with context.wrap_socket(raw, server_hostname=host) as conn:
            conn.sendall(struct.pack("!H", len(packet)) + packet)
            length = struct.unpack("!H", recv_exact(conn, 2))[0]
            data = recv_exact(conn, length)
    if not valid_response(data, query_id):
        raise OSError("DoT 返回无效 DNS 响应")


def probe_tcp(endpoint, timeout):
    host, port = host_port(endpoint, 53)
    query_id, packet = dns_query()
    with socket.create_connection((host, port), timeout=timeout) as conn:
        conn.sendall(struct.pack("!H", len(packet)) + packet)
        length = struct.unpack("!H", recv_exact(conn, 2))[0]
        data = recv_exact(conn, length)
    if not valid_response(data, query_id):
        raise OSError("TCP DNS 返回无效响应")


def probe_udp(endpoint, timeout):
    host, port = host_port(endpoint, 53)
    query_id, packet = dns_query()
    addresses = socket.getaddrinfo(host, port, type=socket.SOCK_DGRAM)
    family, socktype, proto, _, address = addresses[0]
    with socket.socket(family, socktype, proto) as conn:
        conn.settimeout(timeout)
        conn.sendto(packet, address)
        data, _ = conn.recvfrom(65536)
    if not valid_response(data, query_id):
        raise OSError("UDP DNS 返回无效响应")


def probe_one(item, timeout=5):
    group, priority, proto, endpoint = item
    started = time.monotonic()
    try:
        for attempt in range(2):
            try:
                if proto == "doh":
                    probe_doh(endpoint, timeout)
                elif proto == "dot":
                    probe_dot(endpoint, timeout)
                elif proto == "tcp":
                    probe_tcp(endpoint, timeout)
                elif proto == "udp":
                    probe_udp(endpoint, timeout)
                else:
                    raise ValueError(f"健康检测暂不支持协议 {proto}")
                return {"group": group, "priority": priority, "proto": proto, "ok": True,
                        "latency_ms": round((time.monotonic() - started) * 1000), "error": ""}
            except Exception:
                if attempt:
                    raise
        raise RuntimeError("检测失败")
    except Exception as exc:
        return {"group": group, "priority": priority, "proto": proto, "ok": False,
                "latency_ms": round((time.monotonic() - started) * 1000), "error": str(exc)[:200]}


def atomic_write(path, content, mode=0o600):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, name = tempfile.mkstemp(prefix=f".{path.name}.", dir=path.parent)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            handle.write(content)
        os.chmod(name, mode)
        os.replace(name, path)
    finally:
        if os.path.exists(name):
            os.unlink(name)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--upstreams", required=True)
    parser.add_argument("--state", required=True)
    parser.add_argument("--fallback-groups", required=True)
    parser.add_argument("--fail-threshold", type=int, default=3)
    parser.add_argument("--recover-threshold", type=int, default=3)
    parser.add_argument("--cooldown", type=int, default=300)
    parser.add_argument("--timeout", type=float, default=5)
    args = parser.parse_args()

    items = []
    for raw in Path(args.upstreams).read_text(encoding="utf-8").splitlines():
        if not raw.strip() or raw.lstrip().startswith("#"):
            continue
        parts = raw.split("|", 4)
        if len(parts) >= 4:
            items.append(tuple(parts[:4]))

    with concurrent.futures.ThreadPoolExecutor(max_workers=max(1, min(8, len(items)))) as pool:
        probes = list(pool.map(lambda item: probe_one(item, args.timeout), items))

    try:
        state = json.loads(Path(args.state).read_text(encoding="utf-8"))
    except (FileNotFoundError, json.JSONDecodeError):
        state = {"groups": {}}

    now = int(time.time())
    groups = {}
    transitions = []
    for group in sorted({item[0] for item in items}):
        group_probes = [probe for probe in probes if probe["group"] == group]
        healthy = any(probe["ok"] for probe in group_probes)
        previous = state.get("groups", {}).get(group, {})
        fallback = bool(previous.get("fallback", False))
        failed = int(previous.get("failed", 0))
        recovered = int(previous.get("recovered", 0))
        changed_at = int(previous.get("changed_at", 0))

        if healthy:
            failed = 0
            recovered += 1
            if fallback and recovered >= args.recover_threshold and now - changed_at >= args.cooldown:
                fallback = False
                recovered = 0
                changed_at = now
                transitions.append({"group": group, "mode": "unlock"})
        else:
            recovered = 0
            failed += 1
            if not fallback and failed >= args.fail_threshold:
                fallback = True
                failed = 0
                changed_at = now
                transitions.append({"group": group, "mode": "public"})

        groups[group] = {
            "fallback": fallback,
            "failed": failed,
            "recovered": recovered,
            "changed_at": changed_at,
            "healthy": healthy,
            "mode": "public" if fallback else "unlock",
            "probes": group_probes,
        }

    new_state = {"updated_at": now, "groups": groups}
    atomic_write(args.state, json.dumps(new_state, ensure_ascii=False, indent=2) + "\n")
    fallback_content = "".join(f"{group}\n" for group, value in groups.items() if value["fallback"])
    atomic_write(args.fallback_groups, fallback_content)
    print(json.dumps({"groups": groups, "transitions": transitions}, ensure_ascii=False))


if __name__ == "__main__":
    main()
