#!/usr/bin/env sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
WEB_DIR="$ROOT_DIR/web"
EMBED_PLACEHOLDER="$WEB_DIR/dist/.gitkeep"

restore_embed_placeholder() {
  mkdir -p "$(dirname -- "$EMBED_PLACEHOLDER")"
  : > "$EMBED_PLACEHOLDER"
}

trap restore_embed_placeholder EXIT

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
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
  printf '\nSKIP: frontend tests — web/package.json has no test script.\n'
fi

if node -e "process.exit(require('./package.json').scripts?.lint ? 0 : 1)"; then
  run_step "frontend lint" "$NPM" run lint
else
  printf 'SKIP: frontend lint — web/package.json has no lint script.\n'
fi

run_step "frontend production build" "$NPM" run build

# The canvas regression (AC-CANVAS-004) runs against the dev server, so it needs
# both the Playwright package and a browser it can launch. Each absence is
# reported with its own reason; the step is never silently passed.
if node -e "process.exit(require('./package.json').scripts?.['test:e2e'] ? 0 : 1)"; then
  if [ -d "$WEB_DIR/node_modules/@playwright/test" ]; then
    run_step "canvas regression (AC-CANVAS-004)" "$NPM" run test:e2e
  else
    printf 'SKIP: canvas regression — @playwright/test is absent from node_modules; run npm install.\n'
  fi
else
  printf 'SKIP: canvas regression — web/package.json has no test:e2e script.\n'
fi

if [ -f "$WEB_DIR/monoform-studio/package.json" ] && [ -d "$WEB_DIR/monoform-studio/node_modules" ]; then
  run_step "MONOFORM source build" "$NPM" --prefix "$WEB_DIR/monoform-studio" run build
else
  printf 'SKIP: MONOFORM source build — its separate node_modules is absent; the main build uses tracked web/public/monoform output.\n'
fi

if [ -f "$ROOT_DIR/go.mod" ]; then
  command -v go >/dev/null 2>&1 || fail "go.mod exists but go is unavailable"
  cd "$ROOT_DIR"
  run_step "Go tests" go test ./... -count=1
  run_step "Go vet" go vet ./...
else
  printf 'SKIP: Go tests and vet — go.mod is unavailable.\n'
fi

if [ -f "$ROOT_DIR/scripts/security-scan.mjs" ]; then
  cd "$ROOT_DIR"
  run_step "security scans (dynamic execution, secrets, persistence, direct calls)" node scripts/security-scan.mjs
else
  printf 'SKIP: security scans — scripts/security-scan.mjs is missing.\n'
fi

if command -v wails >/dev/null 2>&1; then
  cd "$ROOT_DIR"
  run_step "Wails production build" wails build
else
  printf 'SKIP: Wails production build — install pinned CLI v2.15.0 with: go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0. Production build evidence is required for the desktop acceptance gates.\n'
fi

printf '\nPASS: available verification gates completed.\n'
