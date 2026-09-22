#!/usr/bin/env python
"""Dry-run the WP-09 mutation anchors: report any anchor that is missing or
occurs a different number of times than the mutation expects. Applies nothing."""

import importlib.util
import os
import sys

HARNESS = os.path.join(
    r"F:\AI_Movie_Things_202606\Infinite_Atelier_Drama_Studio_Spec_Pack_v1.0\infinite-atelier-mine-0903",
    ".zcode", "plans", "wp09_mutation_harness.py",
)

spec = importlib.util.spec_from_file_location("harness", HARNESS)
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)

bad = 0
for item in mod.MUTATIONS:
    full = os.path.join(mod.ROOT, item["path"])
    if not os.path.exists(full):
        print("MISSING FILE %s (%s)" % (item["path"], item["name"]))
        bad += 1
        continue
    content = mod.read(full)
    for index, (anchor, replacement, expected) in enumerate(mod.adapt_edits(content, item["edits"])):
        found = content.count(anchor)
        status = "ok" if found == expected else "BAD"
        if found != expected:
            bad += 1
        if found != expected:
            print(
                "%-38s edit %d %s: found %d, expected %d"
                % (item["name"], index, status, found, expected)
            )
            print("    anchor head: %r" % anchor[:120])
        # a replacement that equals the anchor is a no-op
        if anchor == replacement:
            print("%-38s edit %d is a NO-OP (anchor == replacement)" % (item["name"], index))
            bad += 1
print("checked %d mutations, %d problems" % (len(mod.MUTATIONS), bad))
sys.exit(1 if bad else 0)
