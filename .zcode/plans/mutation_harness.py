#!/usr/bin/env python3
"""WP-08 independent mutation harness.

Applies one mutation at a time to a Go source file, runs that mutation's targeted test command,
restores the file, and verifies the restore by hash.

A mutation whose result does not COMPILE proves nothing and is reported as invalid rather than
counted as a kill.

A pristine copy of every mutated file is written to .zcode/plans/mutation-backups/ before the
mutation is applied, so an INTERRUPTED run can always be repaired with:

    python .zcode/plans/mutation_harness.py <spec> --restore-only

Usage:  python .zcode/plans/mutation_harness.py <mutation-spec.py> [--only SUBSTRING]
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

# Markers that mean "the mutant never ran": the toolchain refused it.
COMPILE_MARKERS = (
    "[build failed]",
    "undefined (type",
    "undefined:",
    "syntax error",
    "declared and not used",
    "imported and not used",
    "cannot use",
    "too many arguments",
    "not enough arguments",
    "missing return",
    "no statement",
    "invalid operation",
    "cannot range over",
    "assignment mismatch",
    "no new variables on left side",
    "is not a type",
    "has no field or method",
    "cannot be used as",
)


def sha256(path):
    with open(path, "rb") as handle:
        return hashlib.sha256(handle.read()).hexdigest()


def looks_like_compile_failure(output):
    return any(marker in output for marker in COMPILE_MARKERS)


def first_failure(output):
    for line in output.splitlines():
        if "--- FAIL" in line:
            return line.strip()
    for line in output.splitlines():
        if looks_like_compile_failure(line):
            return line.strip()[:180]
    return output.splitlines()[0][:180] if output else "(no output)"


def backup_name(path):
    return os.path.relpath(path, REPO).replace(os.sep, "__")


def save_original(path, payload):
    """Keep the pristine bytes outside the source tree, for recovery after an interrupt."""
    os.makedirs(BACKUP_DIR, exist_ok=True)
    with open(os.path.join(BACKUP_DIR, backup_name(path)), "wb") as handle:
        handle.write(payload)


def restore_only():
    if not os.path.isdir(BACKUP_DIR):
        print("no backups to restore")
        return 0
    for name in sorted(os.listdir(BACKUP_DIR)):
        relative = name.replace("__", os.sep)
        target = os.path.join(REPO, relative)
        with open(os.path.join(BACKUP_DIR, name), "rb") as handle:
            payload = handle.read()
        with open(target, "wb") as handle:
            handle.write(payload)
        print("restored %s (sha256 %s)" % (relative, sha256(target)[:16]))
    return 0


def main():
    spec_path = sys.argv[1]
    if "--restore-only" in sys.argv:
        return restore_only()
    only = None
    if "--only" in sys.argv:
        only = sys.argv[sys.argv.index("--only") + 1]

    spec = io.open(spec_path, encoding="utf-8-sig").read()
    namespace = {}
    exec(compile(spec, spec_path, "exec"), namespace)
    mutations = namespace["MUTATIONS"]

    killed, survived, invalid = [], [], []
    dirty = []

    for mutation in mutations:
        name = mutation["name"]
        if only and only.lower() not in name.lower():
            continue
        path = os.path.join(REPO, mutation["file"])
        original_hash = sha256(path)
        with open(path, "rb") as handle:
            original = handle.read()
        text = original.decode("utf-8")
        # The tree mixes LF and CRLF (git's autocrlf is on for this checkout), so a multi-line
        # pattern must be tried in whichever convention the file actually uses. The original bytes
        # are restored from the exact copy taken above, so the hash check is unaffected.
        newline = "\r\n" if "\r\n" in text else "\n"
        old, new = mutation["old"], mutation["new"]
        if newline != "\n":
            old = old.replace("\n", newline)
            new = new.replace("\n", newline)
        if old not in text:
            invalid.append((name, "pattern not found", ""))
            continue
        mutated = text.replace(old, new, 1)
        if mutated == text:
            invalid.append((name, "replacement changed nothing", ""))
            continue
        save_original(path, original)
        with open(path, "wb") as handle:
            handle.write(mutated.encode("utf-8"))
        try:
            result = subprocess.run(mutation["cmd"], cwd=REPO, env=ENV, capture_output=True,
                                    text=True, shell=False, timeout=1200)
            output = (result.stdout + result.stderr).strip()
            if result.returncode == 0:
                survived.append((name, output.splitlines()[-1] if output else "", output))
            elif looks_like_compile_failure(output):
                invalid.append((name, first_failure(output), output))
            else:
                killed.append((name, first_failure(output), output))
        except subprocess.TimeoutExpired:
            killed.append((name, "TIMEOUT: the mutation hung the suite, which is a kill", ""))
        finally:
            with open(path, "wb") as handle:
                handle.write(original)
            if sha256(path) != original_hash:
                dirty.append(path)
                raise SystemExit("RESTORE FAILED for %r: %s is modified" % (name, path))

    print("=== %d mutations run ===" % (len(killed) + len(survived) + len(invalid)))
    print("killed:   %d" % len(killed))
    print("SURVIVED: %d" % len(survived))
    print("invalid:  %d" % len(invalid))
    print()
    for name, _, evidence in killed:
        print("  killed:   %s\n              <- %s" % (name, evidence))
    print()
    for name, why, _ in survived:
        print("  SURVIVED: %s   (%s)" % (name, why))
    print()
    for name, why, _ in invalid:
        print("  INVALID:  %s   (%s)" % (name, why))
    print()
    if dirty:
        print("!!! FILES LEFT MODIFIED: %s" % dirty)
    else:
        print("all mutated files restored (hash-verified)")
    return 1 if (survived or invalid or dirty) else 0


if __name__ == "__main__":
    sys.exit(main())
