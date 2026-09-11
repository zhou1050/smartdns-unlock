#!/usr/bin/env python3
import argparse
import json
import os
import re
import subprocess
import sys
from datetime import datetime, timezone
from pathlib import Path

ANSI_RE = re.compile(r"\x1b\[[0-9;]*m")
GREEN = "\x1b[32m"
RED = "\x1b[31m"
YELLOW = "\x1b[33m"

# Only platforms with a real availability/region probe are auto-managed.
# Unlisted platforms stay on the configured unlock DNS by default.
PROBES = {
    "netflix": {"function": "MediaUnlockTest_Netflix", "labels": ["Netflix:"]},
    "disney": {"function": "MediaUnlockTest_DisneyPlus", "labels": ["Disney+:"]},
    "youtube": {"function": "MediaUnlockTest_YouTube_Premium", "labels": ["YouTube Premium:"]},
    "primevideo": {"function": "MediaUnlockTest_PrimeVideo_Region", "labels": ["Amazon Prime Video:"]},
    "max": {"function": "MediaUnlockTest_HBOMax", "labels": ["HBO Max:"]},
    "hulu": {"function": "MediaUnlockTest_HuluUS", "labels": ["Hulu:"]},
    "spotify": {"function": "MediaUnlockTest_Spotify", "labels": ["Spotify Region:", "Spotify:"]},
    "tiktok": {"function": "MediaUnlockTest_Tiktok", "labels": ["Tiktok:", "TikTok:"]},
    "dazn": {"function": "MediaUnlockTest_Dazn", "labels": ["Dazn:", "DAZN:"]},
    "bbciplayer": {"function": "MediaUnlockTest_BBCiPLAYER", "labels": ["BBC iPLAYER:", "BBC iPlayer:"]},
    "paramount": {"function": "MediaUnlockTest_ParamountPlus", "labels": ["Paramount+:"]},
    "peacock": {"function": "MediaUnlockTest_PeacockTV", "labels": ["Peacock TV:"]},
    "crunchyroll": {"function": "MediaUnlockTest_Crunchyroll", "labels": ["Crunchyroll:"]},
    "abema": {"function": "MediaUnlockTest_AbemaTV_IPTest", "labels": ["Abema.TV:"]},
    "bahamut": {"function": "MediaUnlockTest_BahamutAnime", "labels": ["Bahamut Anime:"]},
    "bilibili": {"function": "MediaUnlockTest_BilibiliAnimeNew", "labels": ["Bilibili Anime:"]},
    "iqiyi": {"function": "MediaUnlockTest_iQYI_Region", "labels": ["iQyi Oversea:", "iQIYI:"]},
    "viu": {"function": "MediaUnlockTest_Viu.com", "labels": ["Viu.com:"]},
    "tvb": {"function": "MediaUnlockTest_TVBAnywhere", "labels": ["TVBAnywhere+:"]},
    "openai": {"function": "MediaUnlockTest_ChatGPT", "labels": ["ChatGPT:"]},
    "claude": {"function": "AIUnlockTest_Claude", "labels": ["Claude:"]},
    "microsoftcopilot": {"function": "AIUnlockTest_Copilot", "labels": ["Microsoft Copilot:", "Copilot:"]},
}


def strip_ansi(text: str) -> str:
    return ANSI_RE.sub("", text).replace("\r", "")


def classify_line(raw: str) -> str:
    if GREEN in raw:
        return "pass"
    if RED in raw:
        return "fail"
    if YELLOW in raw:
        return "unknown"
    clean = strip_ansi(raw).lower()
    if re.search(r"\b(no|failed|blocked|unsupported|originals only|not available)\b", clean):
        return "fail"
    if re.search(r"\b(yes|available)\b", clean):
        return "pass"
    return "unknown"


def match_platform_lines(output: str, labels):
    lines = output.splitlines()
    found = []
    for raw in lines:
        clean = strip_ansi(raw).strip()
        for label in labels:
            if clean.lower().startswith(label.lower()):
                found.append(raw)
                break
    return found


def summarize(lines):
    statuses = [classify_line(line) for line in lines]
    if "pass" in statuses:
        status = "pass"
    elif "fail" in statuses:
        status = "fail"
    else:
        status = "unknown"
    detail = " | ".join(strip_ansi(x).strip() for x in lines if strip_ansi(x).strip())
    return status, detail[:500]


def run_rrc(rrc: Path, platform=None, timeout=300):
    cmd = ["bash", str(rrc), "-M", "4", "-E"]
    input_text = "\n"
    if platform:
        probe = PROBES[platform]
        cmd += ["-F", probe["function"]]
        input_text = None
    env = os.environ.copy()
    env.setdefault("TERM", "xterm")
    try:
        proc = subprocess.run(
            cmd,
            input=input_text,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            timeout=timeout,
            env=env,
            check=False,
        )
        output = proc.stdout or ""
    except subprocess.TimeoutExpired as exc:
        output = (exc.stdout or "") if isinstance(exc.stdout, str) else ""
        return 124, output, "timeout"
    if platform and "IPv4:" in strip_ansi(output) and "IPv6:" in strip_ansi(output):
        # Function mode invokes both families; only use IPv4 for auto-routing decisions.
        parts = re.split(r"IPv6:\s*", output, maxsplit=1)
        output = parts[0]
    return proc.returncode, output, ""


def load_registry(path: Path):
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
        return {p["id"]: p.get("name", p["id"]) for p in data.get("platforms", [])}
    except Exception:
        return {}


def main():
    ap = argparse.ArgumentParser(description="Probe actual streaming/AI platform availability")
    ap.add_argument("--rrc", required=True, help="RegionRestrictionCheck check.sh path")
    ap.add_argument("--registry", required=True, help="platforms.json path")
    ap.add_argument("--platform", choices=sorted(PROBES), help="check one platform")
    ap.add_argument("--output", help="write JSON report")
    ap.add_argument("--timeout", type=int, default=300)
    args = ap.parse_args()

    rrc = Path(args.rrc)
    registry = Path(args.registry)
    if not rrc.is_file():
        print(f"probe script missing: {rrc}", file=sys.stderr)
        return 2

    names = load_registry(registry)
    rc, output, error = run_rrc(rrc, args.platform, args.timeout if not args.platform else min(args.timeout, 60))
    ids = [args.platform] if args.platform else sorted(PROBES)
    results = {}
    for pid in ids:
        probe = PROBES[pid]
        lines = match_platform_lines(output, probe["labels"])
        status, detail = summarize(lines)
        if not lines and (error or rc not in (0, 1)):
            status = "unknown"
            detail = error or f"probe exit={rc}"
        results[pid] = {
            "name": names.get(pid, pid),
            "status": status,
            "detail": detail or "no result",
            "function": probe["function"],
        }

    report = {
        "checked_at": datetime.now(timezone.utc).isoformat(),
        "mode": "single" if args.platform else "all",
        "probe_source": "1-stream/RegionRestrictionCheck",
        "exit_code": rc,
        "platforms": results,
    }
    payload = json.dumps(report, ensure_ascii=False, indent=2)
    if args.output:
        tmp = Path(args.output + ".tmp")
        tmp.write_text(payload + "\n", encoding="utf-8")
        os.replace(tmp, args.output)
    print(payload)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
