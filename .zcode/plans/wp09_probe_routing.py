#!/usr/bin/env python
"""Apply each routing mutation, run the zz_probe tests, and report whether the
probe kills it. Restores and hash-verifies like the main harness."""

import hashlib
import importlib.util
import os
import subprocess
import sys

ROOT = r"F:\AI_Movie_Things_202606\Infinite_Atelier_Drama_Studio_Spec_Pack_v1.0\infinite-atelier-mine-0903"
HARNESS = os.path.join(ROOT, ".zcode", "plans", "wp09_mutation_harness.py")

spec = importlib.util.spec_from_file_location("harness", HARNESS)
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)

WANT = ["R01", "R02", "R03", "B03", "B06", "B07", "C01", "C04"]
selected = [m for m in mod.MUTATIONS if any(m["name"].startswith(w) for w in WANT)]

PROBE_PACKAGES = {
    "R01": "./internal/desktop/",
    "R02": "./internal/desktop/",
    "R03": "./internal/desktop/",
}


def run_probe(pattern, package):
    return mod.run(
        ["go", "test", "-count=1", "-run", pattern, package], ROOT, timeout=600
    )


for item in selected:
    full = os.path.join(ROOT, item["path"])
    original = mod.read(full)
    before = mod.sha256(full)
    content = original
    ok = True
    for anchor, replacement, expected in mod.adapt_edits(original, item["edits"]):
        if content.count(anchor) != expected:
            ok = False
            break
        content = content.replace(anchor, replacement, expected)
    if not ok or content == original:
        print("[%s] DID-NOT-APPLY" % item["name"])
        continue
    mod.write(full, content)
    try:
        pattern = "TestZZProbe" if item["name"].startswith("R") else "."
        package = PROBE_PACKAGES.get(item["name"][:3], "./internal/desktop/")
        code, out, secs = run_probe(pattern, package)
        verdict = "PROBE-KILLS" if code != 0 else "probe survived too"
        print("[%-38s] %s (%.1fs)" % (item["name"], verdict, secs))
        if code != 0:
            for line in out.splitlines():
                if "--- FAIL" in line or "zz_probe" in line:
                    print("      " + line.strip()[:160])
    finally:
        mod.write(full, original)
        after = mod.sha256(full)
        print("      restored=%s" % (after == before))
