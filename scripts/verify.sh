#!/usr/bin/env sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
WEB_DIR="$ROOT_DIR/web"
EMBED_PLACEHOLDER="$WEB_DIR/dist/.gitkeep"

restore_embed_placeholder() {
  mkdir -p "$(dirname -- "$EMBED_PLACEHOLDER")"
  : > "$EMBED_PLACEHOLDER"
}

kill_stray_dev_servers() {
  restore_embed_placeholder
  if command -v fuser >/dev/null 2>&1; then
    fuser -k 5173/tcp >/dev/null 2>&1 || true
  elif command -v lsof >/dev/null 2>&1; then
    lsof -ti tcp:5173 | xargs -r kill >/dev/null 2>&1 || true
  fi
}

trap kill_stray_dev_servers EXIT

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}

# STRICT MODE (T23, 2026-09-26 audit): a release-verification run sets
# STRICT_VERIFY=1, which turns the environmental SKIPs below into FAILURES.
# The distinction is the audit's completion standard: PASS / SKIP / BLOCKED
# must be distinguishable, and a release run must exit 0 with every gate
# PASS - not with gates silently skipped. The default mode keeps the script
# usable on machines that legitimately lack one tool.
STRICT=${STRICT_VERIFY:-0}
strict_fail() {
  # $1 = what was skipped, $2 = how to make it available.
  if [ "$STRICT" = "1" ]; then
    fail "$1 is REQUIRED in strict mode: $2"
  fi
  printf 'SKIP: %s\n' "$1"
}

run_step() {
  label=$1
  shift
  printf '\n==> %s\n' "$label"
  "$@"
}

command -v node >/dev/null 2>&1 || fail "node is required"
if command -v npm.cmd >/dev/null 2>&1; then
  NPM=npm.cmd
elif command -v npm >/dev/null 2>&1; then
  NPM=npm
else
  fail "npm is required"
fi
[ -f "$WEB_DIR/package.json" ] || fail "web/package.json is missing"
[ -f "$WEB_DIR/package-lock.json" ] || fail "web/package-lock.json is missing"
[ -d "$WEB_DIR/node_modules" ] || fail "web dependencies are missing; follow README installation instructions first"

cd "$WEB_DIR"
printf 'workspace: %s\n' "$ROOT_DIR"
printf 'node: %s\n' "$(node --version)"
printf 'npm: %s\n' "$($NPM --version)"

run_step "frontend typecheck" "$NPM" run typecheck

if node -e "process.exit(require('./package.json').scripts?.test ? 0 : 1)"; then
  run_step "frontend tests" "$NPM" test
else
  strict_fail "frontend tests — web/package.json has no test script" "install the missing tool or dependency, then re-run."
fi

if node -e "process.exit(require('./package.json').scripts?.lint ? 0 : 1)"; then
  run_step "frontend lint" "$NPM" run lint
else
  strict_fail "frontend lint — web/package.json has no lint script" "install the missing tool or dependency, then re-run."
fi

run_step "frontend production build" "$NPM" run build

# The canvas regression (AC-CANVAS-004) runs against the dev server, so it needs
# both the Playwright package and a browser it can launch. Each absence is
# reported with its own reason; the step is never silently passed.
if node -e "process.exit(require('./package.json').scripts?.['test:e2e'] ? 0 : 1)"; then
  if [ -d "$WEB_DIR/node_modules/@playwright/test" ]; then
    run_step "canvas regression (AC-CANVAS-004)" "$NPM" run test:e2e
  else
    strict_fail "canvas regression — @playwright/test is absent from node_modules; run npm install" "install the missing tool or dependency, then re-run."
  fi
else
  strict_fail "canvas regression — web/package.json has no test:e2e script" "install the missing tool or dependency, then re-run."
fi

if [ -f "$WEB_DIR/monoform-studio/package.json" ] && [ -d "$WEB_DIR/monoform-studio/node_modules" ]; then
  run_step "MONOFORM source build" "$NPM" --prefix "$WEB_DIR/monoform-studio" run build
else
  strict_fail "MONOFORM source build — its separate node_modules is absent; the main build uses tracked web/public/monoform output" "install the missing tool or dependency, then re-run."
fi

if [ -f "$ROOT_DIR/go.mod" ]; then
  command -v go >/dev/null 2>&1 || fail "go.mod exists but go is unavailable"
  cd "$ROOT_DIR"
  run_step "Go tests" go test ./... -count=1
  run_step "Go vet" go vet ./...
else
  strict_fail "Go tests and vet — go.mod is unavailable" "install the missing tool or dependency, then re-run."
fi

if [ -f "$ROOT_DIR/scripts/security-scan.mjs" ]; then
  cd "$ROOT_DIR"
  run_step "security scans (dynamic execution, secrets, persistence, direct calls)" node scripts/security-scan.mjs
else
  strict_fail "security scans — scripts/security-scan.mjs is missing" "install the missing tool or dependency, then re-run."
fi

# Both fixture generators can regenerate what they produced, so a fixture that
# drifted from its generator — or a generator edited without regenerating — is
# detectable. They support --check for exactly this, and without a gate that check
# exists and never runs, which is how a generated fixture silently stops matching
# the code that reads it.
if [ -f "$ROOT_DIR/scripts/gen-canary-fixture.mjs" ]; then
  cd "$ROOT_DIR"
  run_step "canary fixture is current" node scripts/gen-canary-fixture.mjs --check
else
  strict_fail "canary fixture check — the generator is missing" "install the missing tool or dependency, then re-run."
fi

if [ -f "$ROOT_DIR/scripts/gen-malicious-fixtures.mjs" ]; then
  cd "$ROOT_DIR"
  run_step "hostile-input fixtures are current" node scripts/gen-malicious-fixtures.mjs --check
else
  strict_fail "hostile-input fixture check — the generator is missing" "install the missing tool or dependency, then re-run."
fi

if [ -f "$ROOT_DIR/scripts/gen-tool-schemas.mjs" ]; then
  cd "$ROOT_DIR"
  run_step "tool schemas are current" node scripts/gen-tool-schemas.mjs --check
else
  strict_fail "tool schema check — the generator is missing" "install the missing tool or dependency, then re-run."
fi

# The skill packs' check has two halves since WP-08: a MANIFEST is generated and must match the table
# byte for byte, while a skill DOCUMENT is authored and must carry section 4.3's thirteen headings. The
# distinction is the generator's own — see its comment. Without this step a manifest edited by hand
# would silently diverge from the key list the build loads.
if [ -f "$ROOT_DIR/scripts/gen-skill-packs.mjs" ]; then
  cd "$ROOT_DIR"
  run_step "skill packs are current" node scripts/gen-skill-packs.mjs --check
else
  strict_fail "skill pack check — the generator is missing" "install the missing tool or dependency, then re-run."
fi

# The SBOM is generated from go.sum/go.mod/web-package-lock.json and is a release artifact:
# PRD section 18 blocks release while "许可证和第三方声明缺失" and SECURITY section 15 lists
# SBOM as CI. Without this step the checked-in document would keep describing a dependency
# set that no longer exists — the exact failure mode that made it generated rather than
# written. LICENSE_ALLOWLIST is not implemented and is NOT claimed here.
if [ -f "$ROOT_DIR/scripts/gen-sbom.mjs" ]; then
  cd "$ROOT_DIR"
  run_step "SBOM is current" node scripts/gen-sbom.mjs --check
else
  strict_fail "SBOM check — the generator is missing" "install the missing tool or dependency, then re-run."
fi

if command -v wails >/dev/null 2>&1; then
  cd "$ROOT_DIR"
  run_step "Wails production build" wails build
else
  strict_fail "Wails production build — install pinned CLI v2.15.0 with: go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0. Production build evidence is required for the desktop acceptance gates" "install the missing tool or dependency, then re-run."
fi

if [ "$STRICT" = "1" ]; then
  printf '\nPASS: strict verification completed - every gate ran and passed.\n'
else
  printf '\nPASS: available verification gates completed.\n'
fi
