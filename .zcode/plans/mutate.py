#!/usr/bin/env python3
"""Mutation harness for WP-08's enforcement and link code.

Applies one mutation at a time to a Go source file, runs a targeted test command, restores the
file from a hash-verified copy, and reports whether the mutation was KILLED (the test failed) or
SURVIVED (the test passed, so the mutation proved nothing).

A mutation that fails to COMPILE or APPLY proves nothing and is reported as such rather than
counted as a kill.
"""
import hashlib
import io
import os
import shutil
import subprocess
import sys
import tempfile

ENV = dict(os.environ)
ENV["GOTOOLCHAIN"] = "go1.25.0"
ENV["GOSUMDB"] = "sum.golang.org"


def run(cmd, cwd):
    return subprocess.run(cmd, cwd=cwd, env=ENV, capture_output=True, text=True, shell=False)


def hash_of(path):
    with open(path, "rb") as handle:
        return hashlib.sha256(handle.read()).hexdigest()


def main():
    spec_path = sys.argv[1]
    repo = sys.argv[2]
    spec = io.open(spec_path, encoding="utf-8-sig").read()
    namespace = {}
    exec(compile(spec, spec_path, "exec"), namespace)
    mutations = namespace["MUTATIONS"]
    test_cmd = namespace["TEST_CMD"]

    killed = []
    survived = []
    invalid = []
    for index, mutation in enumerate(mutations):
        name = mutation["name"]
        rel = mutation["file"]
        old = mutation["old"]
        new = mutation["new"]
        path = os.path.join(repo, rel)
        original_hash = hash_of(path)
        with open(path, "rb") as handle:
            original = handle.read()
        text = original.decode("utf-8")
        if old not in text:
            invalid.append((name, "pattern not found"))
            continue
        mutated = text.replace(old, new, 1)
        if mutated == text:
            invalid.append((name, "replacement changed nothing"))
            continue
        with open(path, "wb") as handle:
            handle.write(mutated.encode("utf-8"))
        try:
            result = run(test_cmd, repo)
            output = result.stdout + result.stderr
            if result.returncode != 0:
                if "build failed" in output or "cannot use" in output or "undefined" in output or "syntax error" in output:
                    invalid.append((name, "did not compile"))
                else:
                    killed.append((name, ""))
            else:
                survived.append((name, ""))
        finally:
            with open(path, "wb") as handle:
                handle.write(original)
            if hash_of(path) != original_hash:
                raise SystemExit("RESTORE FAILED for " + name)

    print("=== %d mutations ===" % len(mutations))
    print("killed:   %d" % len(killed))
    print("SURVIVED: %d" % len(survived))
    print("invalid:  %d" % len(invalid))
    for name, why in survived:
        print("  SURVIVED: %s" % name)
    for name, why in invalid:
        print("  invalid:  %s (%s)" % (name, why))
    return 1 if survived or invalid else 0


if __name__ == "__main__":
    sys.exit(main())
