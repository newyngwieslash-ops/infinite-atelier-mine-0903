"""Mutation harness for WP-09's batch bounds and the collection's lineage facts.

Anchors that contain indentation are built with explicit escapes (`\t`, `\n`) so a reader can
see exactly what is being matched, and each is asserted present before substituting — a
str.replace whose anchor does not match is a silent no-op.
"""
import hashlib
import io
import os
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
ENV = dict(os.environ, GOTOOLCHAIN="go1.25.0", GOSUMDB="sum.golang.org")

PKG = "."
TESTS = "TestTheBatchRefusesMoreCandidates|TestTheBatchSubmitsNoMore|TestTheCollectedCandidateKeeps"

# The blocks, written as escape sequences rather than literal tabs so this file is readable.
SEMAPHORE = "\t\t\twait.Add(1)\n\t\t\tslots <- struct{}{}"
SEMAPHORE_MUTATED = "\t\t\twait.Add(1)"

PROMPT_LINE = "\t\t\tPrompt:           input.Prompt,\n"
SEED_LINE = "\t\t\tSeed:             input.Seed,\n"

MUTATIONS = [
    (
        "internal/application/productionpipeline/batch.go",
        SEMAPHORE,
        SEMAPHORE_MUTATED,
        "the semaphore is removed, so submissions are unbounded",
    ),
    (
        "internal/application/productionpipeline/candidate.go",
        PROMPT_LINE,
        "",
        "the candidate's prompt is dropped",
    ),
    (
        "internal/application/productionpipeline/candidate.go",
        SEED_LINE,
        "",
        "the candidate's seed is dropped",
    ),
]


def digest(path):
    with open(path, "rb") as handle:
        return hashlib.sha256(handle.read()).hexdigest()


def run(args):
    return subprocess.run(args, cwd=ROOT, env=ENV, capture_output=True, text=True)


def main():
    survivors = []
    for index, (rel, old, new, label) in enumerate(MUTATIONS, 1):
        path = os.path.join(ROOT, rel.replace("/", os.sep))
        original = io.open(path, encoding="utf-8", newline="").read()
        before = digest(path)
        # THE FILE'S OWN LINE ENDING, which is not always LF: batch.go is CRLF while most of the
        # tree is LF, so an anchor written with a bare newline silently fails to match and the
        # mutation reports DID NOT APPLY. The quality reviewer hit the same trap on a .ts file,
        # and this is the harness learning it: anchors are rewritten to the file's own ending.
        if "\r\n" in original:
            old = old.replace("\n", "\r\n")
            new = new.replace("\n", "\r\n")
        if old not in original:
            print("%d. %s -- DID NOT APPLY" % (index, label))
            continue
        io.open(path, "w", encoding="utf-8", newline="").write(original.replace(old, new, 1))
        try:
            vet = run(["go", "vet", PKG])
            if vet.returncode != 0:
                print("%d. %s -- DID NOT COMPILE" % (index, label))
                continue
            test = run(["go", "test", PKG, "-count=1", "-run", TESTS])
            if test.returncode != 0:
                first = [line for line in test.stdout.splitlines() if line.startswith("--- FAIL")]
                print("%d. %s -- KILLED (%s)" % (index, label, first[0] if first else "failed"))
            else:
                print("%d. %s -- SURVIVED" % (index, label))
                survivors.append(label)
        finally:
            io.open(path, "w", encoding="utf-8", newline="").write(original)
            assert digest(path) == before, "restore failed for " + rel
    print()
    if survivors:
        print("SURVIVORS (%d):" % len(survivors))
        for label in survivors:
            print("  - " + label)
        return 1
    print("no survivors")
    return 0


if __name__ == "__main__":
    sys.exit(main())
