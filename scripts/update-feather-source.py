#!/usr/bin/env python3
"""Prepend a release to the AltStore/Feather source file (used by release.yml)."""
import argparse
import datetime
import json

p = argparse.ArgumentParser()
p.add_argument("source")
p.add_argument("--version", required=True)
p.add_argument("--url", required=True)
p.add_argument("--size", type=int, required=True)
a = p.parse_args()

with open(a.source, encoding="utf-8") as f:
    src = json.load(f)
app = src["apps"][0]
app["versions"] = [v for v in app["versions"] if v["version"] != a.version]
app["versions"].insert(0, {
    "version": a.version,
    "buildVersion": a.version,
    "date": datetime.date.today().isoformat(),
    "downloadURL": a.url,
    "size": a.size,
    "localizedDescription": f"Streamero {a.version}",
    "minOSVersion": "17.0",
})
with open(a.source, "w", encoding="utf-8") as f:
    json.dump(src, f, indent=2, ensure_ascii=False)
    f.write("\n")
