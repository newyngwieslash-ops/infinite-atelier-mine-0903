#!/usr/bin/env python3
"""Second pass: re-run named survivors against the FULL Go suite.

A survivor in the targeted pass means only "the command I chose did not notice". The honest
question is whether ANY test in the repository notices, so each survivor is applied again and the
whole suite (every package plus the root) is run. A survivor the full suite also accepts is a
genuine gap (or an equivalent mutant) and is reported as one.

Usage: python .zcode/plans/mutation_second_pass.py <spec.py> <names-file>
"""
import hashlib
import io
import os
import subprocess
import sys

REPO = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
BACKUP_DIR = os.path.join(REPO, ".zcode", "plans", "mutation-backups")
ENV = dict(os.environ)
ENV["GOTOOLCHAIN"] = "go1.25.0"
ENV["GOSUMDB"] = "sum.golang.org"

FULL = ["go", "test", "./...", ".", "-count=1"]

COMPILE_MARKERS = ("[build failed]", "undefined (type", "undefined:", "syntax error",
                   "declared and not used", "imported and not used", "cannot use",
                   "too many arguments", "not enough arguments", "missing return",
                   "no statement", "invalid operation", "cannot range over",
                   "assignment mismatch", "no new variables on left side", "is not a type",
                   "has no field or method", "cannot be used as")


def sha256(path):
    with open(path, "rb") as handle:
        return hashlib.sha256(handle.read()).hexdigest()


def first_failure(output):
    for line in output.splitlines():
        if "--- FAIL" in line:
            return line.strip()
    for line in output.splitlines():
        if any(marker in line for marker in COMPILE_MARKERS):
            return line.strip()[:180]
    return output.splitlines()[0][:180] if output else "(no output)"


def main():
    spec_path, names_path = sys.argv[1], sys.argv[2]
    spec = io.open(spec_path, encoding="utf-8-sig").read()
    namespace = {}
    exec(compile(spec, spec_path, "exec"), namespace)
    wanted = set()
    for line in io.open(names_path, encoding="utf-8"):
        line = line.strip()
        if line:
            wanted.add(line.split()[0])

    still_alive, killed_full, invalid = [], [], []
    for mutation in namespace["MUTATIONS"]:
        ident = mutation["name"].split()[0]
        if ident not in wanted:
            continue
        path = os.path.join(REPO, mutation["file"])
        original_hash = sha256(path)
        with open(path, "rb") as handle:
            original = handle.read()
        text = original.decode("utf-8")
        # The tree mixes LF and CRLF, so a multi-line pattern must match the file's own convention.
        newline = "\r\n" if "\r\n" in text else "\n"
        old, new = mutation["old"], mutation["new"]
        if newline != "\n":
            old = old.replace("\n", newline)
            new = new.replace("\n", newline)
        if old not in text:
            invalid.append((mutation["name"], "pattern not found"))
            continue
        os.makedirs(BACKUP_DIR, exist_ok=True)
        backup = os.path.join(BACKUP_DIR, os.path.relpath(path, REPO).replace(os.sep, "__"))
        with open(backup, "wb") as handle:
            handle.write(original)
        with open(path, "wb") as handle:
            handle.write(text.replace(old, new, 1).encode("utf-8"))
        try:
            result = subprocess.run(FULL, cwd=REPO, env=ENV, capture_output=True, text=True,
                                    shell=False, timeout=1800)
            output = (result.stdout + result.stderr).strip()
            if result.returncode == 0:
                still_alive.append((mutation["name"], output))
            elif "[build failed]" in output:
                invalid.append((mutation["name"], first_failure(output)))
            else:
                killed_full.append((mutation["name"], first_failure(output)))
        except subprocess.TimeoutExpired:
            killed_full.append((mutation["name"], "TIMEOUT: the full suite hung, which is a kill"))
        finally:
            with open(path, "wb") as handle:
                handle.write(original)
            if sha256(path) != original_hash:
                raise SystemExit("RESTORE FAILED for %r" % mutation["name"])

    total = len(still_alive) + len(killed_full) + len(invalid)
    print("=== full-suite second pass over %d mutations ===" % total)
    print("killed by the FULL suite: %d" % len(killed_full))
    print("STILL ALIVE:              %d" % len(still_alive))
    print("invalid:                  %d" % len(invalid))
    print()
    for name, evidence in killed_full:
        print("  killed-by-full: %s\n                  <- %s" % (name, evidence))
    print()
    for name, _ in still_alive:
        print("  STILL ALIVE: %s" % name)
    print()
    for name, why in invalid:
        print("  INVALID: %s (%s)" % (name, why))
    print()
    print("all files restored (hash-verified)")


if __name__ == "__main__":
    main()
