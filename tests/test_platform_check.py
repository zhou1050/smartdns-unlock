#!/usr/bin/env python3
import importlib.util
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("platform_check", ROOT / "scripts" / "platform_check.py")
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)

assert module.classify_line("Netflix:\t\x1b[32mYes (Region: US)\x1b[0m") == "pass"
assert module.classify_line("Netflix:\t\x1b[31mNo\x1b[0m") == "fail"
assert module.classify_line("ChatGPT:\t\x1b[33mWeb Only\x1b[0m") == "unknown"

sample = "\n".join([
    "Netflix:\t\x1b[32mYes (Region: US)\x1b[0m",
    "Spotify Region:\t\x1b[32mJP\x1b[0m",
])
spotify_lines = module.match_platform_lines(sample, module.PROBES["spotify"]["labels"])
assert len(spotify_lines) == 1
assert module.summarize(spotify_lines)[0] == "pass"

registry = json.loads((ROOT / "config" / "platforms.json").read_text(encoding="utf-8"))
registry_ids = {item["id"] for item in registry["platforms"]}
assert set(module.PROBES).issubset(registry_ids)

print("platform checker tests OK")
