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
p.add_argument("--bundle-id", default="net.scyde.streamero",
               help="which app of the source gets the release (Streamero or Streamero Manage)")
a = p.parse_args()


def build_number(version):
    """CFBundleVersion for a release: major*10000 + minor*100 + patch (same formula as ios.yml)."""
    core = version.removeprefix("v").split("-", 1)[0]
    major, minor, patch = (int(x) for x in core.split("."))
    return str(major * 10000 + minor * 100 + patch)


with open(a.source, encoding="utf-8") as f:
    src = json.load(f)
app = next((x for x in src["apps"] if x["bundleIdentifier"] == a.bundle_id), None)
if app is None:
    raise SystemExit(f"no app with bundleIdentifier {a.bundle_id} in {a.source}")
app["versions"] = [v for v in app["versions"] if v["version"] != a.version]
app["versions"].insert(0, {
    "version": a.version,
    "buildVersion": build_number(a.version),
    "date": datetime.date.today().isoformat(),
    "downloadURL": a.url,
    "size": a.size,
    "localizedDescription": f"{app['name']} {a.version}",
    "minOSVersion": "17.0",
})
with open(a.source, "w", encoding="utf-8") as f:
    json.dump(src, f, indent=2, ensure_ascii=False)
    f.write("\n")
