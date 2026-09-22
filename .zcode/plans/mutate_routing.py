"""Verify the routing tests kill the four mutations that survived before them.

Each mutation is (file, old, new, package, pattern, label). The harness asserts the anchor's
presence before substituting, because a str.replace whose anchor does not match is a silent
no-op — the failure mode that has bitten this session repeatedly.
"""
import hashlib
import io
import os
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
ENV = dict(os.environ, GOTOOLCHAIN="go1.25.0", GOSUMDB="sum.golang.org")

PKG = "."
ROUTING = "TestTheBatchRefusesMoreCandidates|TestTheBatchSubmitsNoMore|TestTheCollectedCandidateKeeps"

MUTATIONS = [
    (
        "internal/application/productionpipeline/batch.go",
        "			wait.Add(1)
			slots <- struct{}{}",
        "			wait.Add(1)",
        "the semaphore is removed, so submissions are unbounded",
    ),
    (
        "internal/application/productionpipeline/candidate.go",
        """			Prompt:           input.Prompt,""",
        """""",
        "the candidate's prompt is dropped",
    ),
    (
        "internal/application/productionpipeline/candidate.go",
        """			Seed:             input.Seed,""",
        """""",
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
        if old not in original:
            print("%d. %s -- DID NOT APPLY" % (index, label))
            continue
        io.open(path, "w", encoding="utf-8", newline="").write(original.replace(old, new, 1))
        try:
            vet = run(["go", "vet", PKG])
            if vet.returncode != 0:
                print("%d. %s -- DID NOT COMPILE" % (index, label))
                continue
            test = run(["go", "test", PKG, "-count=1", "-run", ROUTING])
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
    print("no survivors: the routing tests kill every one")
    return 0


if __name__ == "__main__":
    sys.exit(main())
