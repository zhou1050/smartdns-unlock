#!/usr/bin/env python3
import importlib.util
import json
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("builder", ROOT / "scripts/build_rules.py")
builder = importlib.util.module_from_spec(spec)
assert spec.loader
spec.loader.exec_module(builder)

assert builder.normalize("*.Netflix.COM.") == "netflix.com"
assert builder.normalize("bad domain") is None
assert builder.normalize("example") is None

registry = json.loads((ROOT / "config/platforms.json").read_text())
ids = [entry["id"] for entry in registry["platforms"]]
assert len(ids) == len(set(ids))
assert {"netflix", "disney", "openai", "claude", "gemini"}.issubset(ids)
assert all(entry.get("seeds") for entry in registry["platforms"])
print(f"registry OK: {len(ids)} platforms")
