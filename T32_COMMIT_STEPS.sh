#!/usr/bin/env bash
# T32 execution steps — paste-ready. Run from the repo root.
# Prereq verified: 77 tracked modifications + 33 batch new files, secret scan clean,
# EXE gitignored, user's untracked doc preserved.
set -eu

# 1. Stage everything EXCEPT the user's own untracked document and the
#    machine-specific toolchain env script (local paths, not repo material).
git add -A
# Exclusions: the user's own untracked analysis doc and this script itself.
# (The machine-local toolchain script is excluded via .gitignore.)
# INCLUDED batch deliverables: docs/SETTINGS_INVENTORY_T10_T11.md,
# docs/T25_VM_ACCEPTANCE_CHECKLIST.md, docs/T29_SIGNING_STRATEGY.md,
# docs/VENDOR_PROTOCOL_CROSSCHECK.md, docs/EMBEDDING_MODEL_DISTRIBUTION.md,
# web/e2e/render-perf-t27.spec.ts, and every migration/test/source file the
# batch added.
git reset -q -- "Infinite-Atelier-OpenCode集成必要性评估与完整实施方案.md"
git reset -q -- T32_COMMIT_STEPS.sh
# Verify exclusions took effect (should print nothing):
git diff --cached --name-only | grep -E "OpenCode|T32_COMMIT_STEPS" && {
  echo "ERROR: excluded files still staged"; exit 1;
}

# 2. Commit with the drafted message.
git commit -m "T-batch: audit remediation T01-T24

- T01/T04: per-line audio asset isolation + atomic collection (migrations 000027)
- T02: video adoption chain (collect/adopt/timeline)
- T03: effect_generation capability split from TTS (migration 000028)
- T05: per-usage track params + editor (migration 000029)
- T06: local job handlers (thumbnail/import/export/migration)
- T07: per-provider rate limits + window ledger (migration 000030)
- T08: declarative provider manifest (closed-set validation)
- T09: agent enable/disable + skill document surface
- T13/T14/T15: FIX contract wording, storyboard citation approval checks,
  final-ruleset video tightening + per-line dialogue coverage
- T16/T17: content analysis (blackdetect/silencedetect), asset license record
  (migration 000031)
- T12/T18/T19/T20: backup scheduler, embedding diagnostics, distribution doc,
  strict ONNX release gate
- T22/T23/T24: CI gates (fixtures/race/NSIS/artifacts), STRICT_VERIFY,
  race suite green with perf tests separated
- T28/T30/T31: vendor contract cross-check + enum/speed tightening,
  NSIS declaration files, full doc sync (STATUS/TRACEABILITY/ADR-0032)

BLOCKED (user-gated): T25 clean VM, T29 signing cert, T32 push authorization."

# 3. Record the clean-baseline build (audit T21 requirement: push-time rebuild
#    must produce hashes that correspond to a CLEAN tree, not the dirty one
#    recorded in STATUS). Run after commit, before push:
TC="D:\GoWorks1.18\pkg\mod\golang.org\toolchain@v0.0.1-go1.25.13.windows-amd64"
export GOROOT="$TC" GOTOOLCHAIN=local CC="C:/msys64/mingw64/bin/gcc.exe" CGO_ENABLED=1
export PATH="$(cygpath -u "$TC")/bin:/c/msys64/mingw64/bin:/d/GoWorks1.18/bin:/c/Program Files (x86)/NSIS:$PATH"
wails build -nsis -skipbindings && {
  sha256sum build/bin/InfiniteAtelier.exe build/bin/*-installer.exe > /tmp/t32-clean-hashes.txt
  echo "=== CLEAN BASELINE HASHES (record in STATUS after push) ==="
  cat /tmp/t32-clean-hashes.txt
}

# 4. Push (uncomment when authorized):
# git push origin codex/wp-01-desktop-foundation

# 5. CI trigger (after push): the branch is not in the workflow's push filter;
#    trigger via workflow_dispatch (added by T22):
#   gh workflow run desktop-build.yml --ref codex/wp-01-desktop-foundation
#    or open a PR to main (the pull_request filter runs all jobs).
#   Watch: gh run watch --interval 30

# 4. CI trigger: push to the feature branch + workflow_dispatch. The workflow
#    now accepts workflow_dispatch (T22), so either:
#    a) push alone does NOT run CI (branch filter is main/PR) — trigger via:
#       gh workflow run desktop-build.yml --ref codex/wp-01-desktop-foundation
#    b) or open a PR to main; the pull_request filter runs all jobs.
#    Watch: gh run watch --interval 30
