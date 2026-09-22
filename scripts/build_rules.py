#!/usr/bin/env python3
"""Build one-domain-per-line SmartDNS domain sets from maintained sources."""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import sys
import urllib.error
import urllib.request
from pathlib import Path

V2FLY_BASE = "https://raw.githubusercontent.com/v2fly/domain-list-community/master/data/"
DOMAIN_RE = re.compile(r"^(?=.{1,253}$)(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$")


def normalize(value: str) -> str | None:
    value = value.strip().lower().rstrip(".")
    for prefix in ("||", "*.", "."):
        if value.startswith(prefix):
            value = value[len(prefix):]
    value = value.split("^")[0].split("/")[0]
    if not value or "*" in value:
        return None
    try:
        value = value.encode("idna").decode("ascii")
    except UnicodeError:
        return None
    return value if DOMAIN_RE.fullmatch(value) else None


def fetch(url: str) -> str:
    req = urllib.request.Request(url, headers={"User-Agent": "smartdns-unlock-rule-builder/1"})
    with urllib.request.urlopen(req, timeout=30) as response:
        return response.read().decode("utf-8", errors="replace")


def parse_v2fly(name: str, seen: set[str], warnings: list[str]) -> set[str]:
    if name in seen:
        return set()
    seen.add(name)
    try:
        text = fetch(V2FLY_BASE + name)
    except (urllib.error.URLError, TimeoutError) as exc:
        warnings.append(f"source {name}: {exc}")
        return set()

    result: set[str] = set()
    for raw in text.splitlines():
        line = raw.split("#", 1)[0].strip()
        if not line:
            continue
        token = line.split()[0]
        if token.startswith("include:"):
            result.update(parse_v2fly(token.partition(":")[2], seen, warnings))
            continue
        if token.startswith(("domain:", "full:")):
            domain = normalize(token.partition(":")[2])
            if domain:
                result.add(domain)
            continue
        # domain-list-community primarily stores ordinary suffix rules as bare
        # domains (for example, "nflxvideo.net").  Ignoring those lines leaves
        # generated platform sets containing only our small seed list and can
        # bypass the selected unlock resolver for API/search/playback hosts.
        # Regex/keyword rules cannot be represented safely by a SmartDNS
        # domain-set and remain intentionally unsupported.
        if ":" not in token:
            domain = normalize(token)
            if domain:
                result.add(domain)
    return result


def load_local(path: Path) -> set[str]:
    if not path.exists():
        return set()
    result: set[str] = set()
    for raw in path.read_text(encoding="utf-8").splitlines():
        line = raw.split("#", 1)[0].strip()
        domain = normalize(line)
        if domain:
            result.add(domain)
    return result


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--registry", default="config/platforms.json")
    parser.add_argument("--custom-dir", default="rules/custom")
    parser.add_argument("--output", default="rules/generated")
    parser.add_argument("--offline", action="store_true", help="use seeds and local additions only")
    args = parser.parse_args()

    registry_path = Path(args.registry)
    registry = json.loads(registry_path.read_text(encoding="utf-8"))
    output = Path(args.output)
    custom = Path(args.custom_dir)
    output.mkdir(parents=True, exist_ok=True)
    expected: set[str] = set()
    metadata: dict[str, object] = {"schema": 1, "platforms": {}}
    failed = False

    for platform in registry["platforms"]:
        platform_id = platform["id"]
        warnings: list[str] = []
        domains = {d for seed in platform.get("seeds", []) if (d := normalize(seed))}
        source_failed = False
        if (source := platform.get("source")) and not args.offline:
            domains.update(parse_v2fly(source, set(), warnings))
            source_failed = bool(warnings)
        domains.update(load_local(custom / f"{platform_id}.txt"))
        for excluded in platform.get("exclude", []):
            normalized = normalize(excluded)
            if normalized:
                domains.discard(normalized)

        destination = output / f"{platform_id}.txt"
        expected.add(destination.name)
        # A temporary upstream outage must not replace a healthy list with only seeds.
        if source_failed and destination.exists():
            previous = load_local(destination)
            if len(previous) > len(domains):
                domains.update(previous)
                warnings.append("kept previous generated list because upstream failed")
        if not domains:
            print(f"ERROR: {platform_id} produced no domains", file=sys.stderr)
            failed = True
            continue
        body = "\n".join(sorted(domains)) + "\n"
        destination.write_text(body, encoding="utf-8")
        metadata["platforms"][platform_id] = {
            "count": len(domains),
            "sha256": hashlib.sha256(body.encode()).hexdigest(),
            "warnings": warnings,
        }
        print(f"{platform_id:20s} {len(domains):5d} domains" + (" (source warning)" if warnings else ""))

    for stale in output.glob("*.txt"):
        if stale.name not in expected:
            stale.unlink()
    (output / "manifest.json").write_text(json.dumps(metadata, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    return 1 if failed else 0


if __name__ == "__main__":
    raise SystemExit(main())
