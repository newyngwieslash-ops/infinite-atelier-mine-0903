#!/usr/bin/env python
"""Apply each surviving mutation, run the zz_probe tests, and report whether a
small obvious probe kills it. Restores and hash-verifies like the main harness."""

import importlib.util
import os

ROOT = r"F:\AI_Movie_Things_202606\Infinite_Atelier_Drama_Studio_Spec_Pack_v1.0\infinite-atelier-mine-0903"
HARNESS = os.path.join(ROOT, ".zcode", "plans", "wp09_mutation_harness.py")

spec = importlib.util.spec_from_file_location("harness", HARNESS)
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)

# The probe that states each survivor's clause, and the package it runs in.
PROBES = {
    "B03": ("TestZZProbeTheCandidateBoundIsEnforced", "."),
    "B06": ("TestZZProbeSubmissionsAreBounded", "."),
    "B07": ("TestZZProbeTheGateRefusesAnEpisodeWithNoApprovedBoard", "."),
    "C01": ("TestZZProbeAnUnfinishedJobIsSkippedRatherThanWritten", "."),
    "C04": ("TestZZProbeASucceededJobWithNoFileIsRefused", "."),
    "B06-alt": ("TestTheBatchRespectsItsConcurrencyLimit", "."),
}

for key, (pattern, package) in PROBES.items():
    names = [m for m in mod.MUTATIONS if m["name"].startswith(key.split("-")[0])]
    if not names:
        print("no mutation named %s" % key)
        continue
    item = names[0]
    full = os.path.join(ROOT, item["path"])
    original = mod.read(full)
    before = mod.sha256(full)
    content = original
    apply_ok = True
    for anchor, replacement, expected in mod.adapt_edits(original, item["edits"]):
        if content.count(anchor) != expected:
            apply_ok = False
            print("  ANCHOR MISS for %s" % item["name"])
            break
        content = content.replace(anchor, replacement, expected)
    if not apply_ok or content == original:
        print("[%s] DID-NOT-APPLY" % item["name"])
        continue
    mod.write(full, content)
    try:
        code, out, secs = mod.run(
            ["go", "test", "-count=1", "-run", pattern, package], ROOT, timeout=600
        )
        verdict = "PROBE-KILLS" if code != 0 else "PROBE ALSO SURVIVES"
        print("[%-36s] %-20s %.1fs  probe=%s" % (item["name"], verdict, secs, pattern))
        if code != 0:
            for line in out.splitlines():
                if "--- FAIL" in line or "zz_probe" in line:
                    print("      " + line.strip()[:170])
    finally:
        mod.write(full, original)
        print("      restored=%s" % (mod.sha256(full) == before))
