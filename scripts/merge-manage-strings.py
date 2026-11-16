#!/usr/bin/env python3
"""Merge Streamero Manage's UI strings (scripts/manage-strings.tsv:
key, English, Croatian) into the shared Localizable.xcstrings, and check that every
`manage.*` key used in Swift has a translation. Exits 1 when one is missing.

    python3 scripts/merge-manage-strings.py
"""
import json
import pathlib
import re
import sys

root = pathlib.Path(__file__).resolve().parent.parent / "ios"
tsv = pathlib.Path(__file__).resolve().parent / "manage-strings.tsv"
catalog = root / "Streamero/Resources/Localizable.xcstrings"

entries = {}
for line in tsv.read_text(encoding="utf-8").splitlines():
    if not line.strip():
        continue
    key, en, hr = line.split("\t")
    entries[key] = (en, hr)

# Keys used in code: plain literals, plus interpolated Text("manage.x \(n)") → "manage.x %lld"/"%@".
used = set()
for swift in list((root / "StreameroManage").rglob("*.swift")) + list((root / "Streamero").rglob("*.swift")):
    text = swift.read_text(encoding="utf-8")
    for m in re.finditer(r'"(manage\.[A-Za-z0-9_.]+)( \\\()?', text):
        used.add(m.group(1) + (" *" if m.group(2) else ""))

missing = []
for key in sorted(used):
    if key.endswith(" *"):
        base = key[:-2]
        if not any(k.startswith(base + " %") for k in entries):
            missing.append(base + " %…")
    elif key not in entries:
        missing.append(key)

data = json.loads(catalog.read_text(encoding="utf-8"))
for key, (en, hr) in entries.items():
    data["strings"][key] = {
        "localizations": {
            "en": {"stringUnit": {"state": "translated", "value": en}},
            "hr": {"stringUnit": {"state": "translated", "value": hr}},
        }
    }
catalog.write_text(json.dumps(data, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
print(f"merged {len(entries)} strings")
if missing:
    print("missing translations:", ", ".join(missing))
    sys.exit(1)
