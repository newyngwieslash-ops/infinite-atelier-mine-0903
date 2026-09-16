# WP-01 Secure Desktop Foundation Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add the smallest production-capable Wails v2 shell, layered Go core, SQLite migration foundation, content-addressed FileStore, health binding, and event/logging contracts while preserving the existing React/Vite application and browser development mode.

**Architecture:** Keep `web/` intact as presentation and embed `web/dist` in a single Wails process. Go owns application directories, SQLite, files, health, logs, and desktop events through Domain → Application/Ports → Infrastructure/Desktop boundaries; the only WP-01 frontend binding is read-only health. SQLite is authoritative only for the new WP-01 foundation tables—legacy browser project data is not migrated in this package.

**Tech Stack:** Go 1.24, Wails v2.15.0, `database/sql`, `modernc.org/sqlite` v1.58.0 candidate, Go standard library (`embed`, `io`, `crypto/sha256`, `log/slog`, `net/http` MIME sniffing), React 19, TypeScript 5, Vite 7, npm lockfiles.

---

## Scope guardrails

- Use `@ponytail:ponytail` in full mode: reuse the current frontend, Wails native single-instance support, Go standard library, and existing React Query/i18n. Do not generate a replacement frontend or add an ORM, DI container, migration framework, logging framework, frontend test framework, or file-type dependency.
- Do not implement SecretStore, Provider Gateway, legacy import, durable Job, Agent, Workflow, Memory, Drama entities, or canvas refactoring.
- Do not delete, rewrite, or move browser data. Do not call a real Provider.
- Commit generated Wails TypeScript bindings because existing frontend typecheck must work without a locally installed Wails CLI. Do not hand-edit generated files.
- Every suggested commit below requires explicit user authorization under `AGENTS.md`. Without it, leave the reviewed changes uncommitted and report the proposed commit boundary.
- Execution should use a dedicated worktree. The current checkout contains untracked user specification/WP-00 files; preserve or commit them with user approval before creating that worktree. Never stash/reset/clean them.

## Dependency decisions to verify

- Pin Wails v2.15.0, the stable v2 line observed on 2026-09-04. Do not switch to v3 beta.
- Try `modernc.org/sqlite` v1.58.0 first. Accept it only after the tests in Task 4 pass with `CGO_ENABLED=0`, Windows Wails build, WAL, foreign keys, busy timeout, migration rollback, and consistent snapshot. Keep `mattn/go-sqlite3` v1.14.49 as the documented fallback; do not add both drivers to production.
- Keep the repository-owned migration runner bounded to embedded forward-only SQL, checksum verification, transactions, and `PRAGMA user_version`. If those requirements cannot be implemented compactly and tested, stop and update ADR-0002 before adding a migration framework.

### Task 1: Re-establish a portable, clean baseline

**Files:**

- Modify: `web/package.json`
- Modify: `web/package-lock.json`
- Verify: `docs/implementation/BASELINE.md`

**Step 1: Record the untouched worktree**

Run:

```powershell
git status --short --branch
git diff --check
```

Expected: the user-owned specification/WP-00 files are identified; there are no unknown tracked product changes. Stop if the execution worktree contains unexpected overlap.

**Step 2: Reproduce the lock failure**

In a clean worktree with no `web/node_modules`, run:

```powershell
Set-Location web
npm ci --legacy-peer-deps
```

Expected before the fix: FAIL with package/lock mismatch or platform-package errors recorded by WP-00.

**Step 3: Remove platform-specific packages from direct dependencies**

Delete only these four entries from `web/package.json`; their parent packages already manage platform binaries as optional dependencies:

```json
"@esbuild/win32-x64": "0.28.1",
"@rollup/rollup-win32-x64-msvc": "4.62.2",
"@tailwindcss/oxide-win32-x64-msvc": "4.2.4",
"lightningcss-win32-x64-msvc": "1.32.0"
```

Do not upgrade unrelated packages.

**Step 4: Regenerate only lock metadata**

Run:

```powershell
npm install --package-lock-only --legacy-peer-deps --include=optional --ignore-scripts
npm ci --legacy-peer-deps
npm run typecheck
npm run build
```

Expected: all PASS. Inspect `git diff -- web/package.json web/package-lock.json`; the lock diff must be limited to dependency reconciliation, not broad version upgrades.

**Step 5: Suggested commit boundary**

```powershell
git add web/package.json web/package-lock.json
git commit -m "build: restore portable npm lock install"
```

### Task 2: Add the minimal Wails v2 shell around the existing frontend

**Files:**

- Create: `go.mod`
- Create: `go.sum`
- Create: `main.go`
- Create: `app.go`
- Create: `wails.json`
- Create: `web/dist/.gitkeep`
- Modify: `.gitignore`
- Create: `internal/buildinfo/version.go`

**Step 1: Initialize and pin the Go module**

Run:

```powershell
go mod init github.com/newyngwieslash-ops/infinite-atelier-mine-0903
go get github.com/wailsapp/wails/v2@v2.15.0
go mod tidy
```

Expected: `go.mod` pins Wails v2.15.0; no Wails v3 module appears.

**Step 2: Keep the embed path valid before a frontend build**

Add the following final rules to `.gitignore` and create an empty `web/dist/.gitkeep`:

```gitignore
!web/dist/
web/dist/*
!web/dist/.gitkeep
```

This is the only committed file in `web/dist`; generated assets stay ignored.

**Step 3: Add the version source**

Create `internal/buildinfo/version.go`:

```go
package buildinfo

const Version = "1.0.0"
```

**Step 4: Add Wails configuration**

Create `wails.json` with this contract:

```json
{
  "$schema": "https://wails.io/schemas/config.v2.json",
  "name": "Infinite Atelier",
  "outputfilename": "InfiniteAtelier",
  "frontend:dir": "web",
  "frontend:install": "npm ci --legacy-peer-deps",
  "frontend:build": "npm run build",
  "frontend:dev:watcher": "npm run dev -- --host 127.0.0.1",
  "frontend:dev:serverUrl": "auto",
  "wailsjsdir": "./src/wailsjs",
  "author": {
    "name": "GuiYi-Xi"
  },
  "info": {
    "productName": "Infinite Atelier",
    "productVersion": "1.0.0",
    "copyright": "Copyright © 2026 GuiYi-Xi"
  }
}
```

Validate it with the installed v2.15.0 CLI rather than guessing at ignored/renamed fields.

**Step 5: Add composition without business logic**

`app.go` owns lifecycle composition but exports no Wails methods:

```go
package main

import "context"

type app struct {
    shutdown func(context.Context) error
}

func (a *app) startup(context.Context) {}

func (a *app) close(ctx context.Context) {
    if a.shutdown != nil {
        _ = a.shutdown(ctx)
    }
}
```

`main.go` must:

- use `//go:embed all:web/dist`;
- construct dependencies before `wails.Run`;
- embed assets through `assetserver.Options`;
- bind only the later `HealthBinding`, not repositories or infrastructure;
- use `OnStartup`, `OnDomReady`, and `OnShutdown` lifecycle callbacks;
- use Wails `options.SingleInstanceLock` with a fixed UUID;
- treat second-instance arguments as untrusted and only focus/show the existing window;
- use a normal framed 1280×800 window and close when the last window exits.

Do not add an HTTP server or expose the Vite server in production.

**Step 6: Verify shell compilation**

Run:

```powershell
go test ./...
go vet ./...
wails doctor
```

Expected: Go compilation PASS. `wails doctor` must record any host prerequisite warning; do not hide it.

**Step 7: Suggested commit boundary**

```powershell
git add .gitignore go.mod go.sum main.go app.go wails.json web/dist/.gitkeep internal/buildinfo/version.go
git commit -m "feat: add Wails v2 desktop shell"
```

### Task 3: Add application directories, stable errors, and redacted structured logs

**Files:**

- Create: `internal/domain/apperror/error.go`
- Create: `internal/infrastructure/appdirs/dirs.go`
- Create: `internal/infrastructure/appdirs/dirs_test.go`
- Create: `internal/infrastructure/logging/logging.go`
- Create: `internal/infrastructure/logging/logging_test.go`
- Modify: `app.go`
- Modify: `main.go`

**Step 1: Write failing directory tests**

Cover deterministic layout and permissions using `t.TempDir()`:

```go
func TestEnsureCreatesPrivateLayout(t *testing.T) {
    dirs, err := Ensure(t.TempDir())
    if err != nil { t.Fatal(err) }
    for _, path := range []string{dirs.Root, dirs.Files, dirs.Temp, dirs.Logs, dirs.Snapshots} {
        if info, err := os.Stat(path); err != nil || !info.IsDir() { t.Fatalf("missing directory %s: %v", path, err) }
    }
    if filepath.Dir(dirs.Database) != dirs.Root { t.Fatalf("database escaped root: %s", dirs.Database) }
}
```

Run `go test ./internal/infrastructure/appdirs -run TestEnsureCreatesPrivateLayout -v`; expected FAIL because `Ensure` is missing.

**Step 2: Implement the smallest directory resolver**

`Dirs` contains `Root`, `Database`, `Files`, `Temp`, `Logs`, and `Snapshots`. `Ensure(override string)` uses the supplied test root, otherwise `os.UserConfigDir()/InfiniteAtelier`; it creates directories with `0700` and never accepts a frontend-provided path.

Run the directory test; expected PASS.

**Step 3: Add stable application errors**

Implement only the fields required by the contracts:

```go
type Error struct {
    Code        string
    Category    string
    Retriable   bool
    SafeMessage string
    Diagnostic string
    Cause       error
}

func (e *Error) Error() string { return e.SafeMessage }
func (e *Error) Unwrap() error { return e.Cause }
```

Generate `Diagnostic` with 16 random bytes encoded as hex. Never include SQL, absolute paths, headers, URLs, or secrets in `SafeMessage`.

**Step 4: Write a failing redaction test**

Log attributes named `apiKey`, `authorization`, `token`, `secret`, and `cookie`, then assert the JSON output contains `[REDACTED]` and contains none of the input values. Also assert ordinary fields survive.

Run `go test ./internal/infrastructure/logging -v`; expected FAIL before implementation.

**Step 5: Implement `slog` redaction and file output**

Wrap `slog.NewJSONHandler` with one handler that replaces sensitive attribute values case-insensitively. Open `logs/app.jsonl` with `0600`, append mode, and close it during shutdown. Do not log request/response bodies or full provider URLs.

Run the logging tests; expected PASS.

**Step 6: Suggested commit boundary**

```powershell
git add internal/domain internal/infrastructure/appdirs internal/infrastructure/logging app.go main.go
git commit -m "feat: add safe desktop startup primitives"
```

### Task 4: Prove and accept one SQLite driver

**Files:**

- Create: `internal/infrastructure/database/driver_test.go`
- Create: `internal/infrastructure/database/driver.go`
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `docs/adr/0002-sqlite-driver-and-migrations.md`
- Create: `THIRD_PARTY_NOTICES.md`

**Step 1: Add the pure-Go candidate only**

Run:

```powershell
go get modernc.org/sqlite@v1.58.0
go mod tidy
```

Do not add GORM, `glebarez/go-sqlite`, an ORM, or a second SQLite driver.

**Step 2: Write the driver acceptance test**

Implement the DSN/open helper in `driver.go`; the test opens a path containing spaces through a `net/url`-built SQLite URI and asserts:

```go
var foreignKeys int
if err := db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil || foreignKeys != 1 { t.Fatalf(...) }

var journalMode string
if err := db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil || !strings.EqualFold(journalMode, "wal") { t.Fatalf(...) }

var busyTimeout int
if err := db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil || busyTimeout != 5000 { t.Fatalf(...) }
```

Also create a parent/child table and assert a missing parent insert fails. Cap connections explicitly; do not rely on default pooling.

**Step 3: Run the decision matrix**

Run:

```powershell
go test ./internal/infrastructure/database -run TestDriverContract -count=1 -v
$env:CGO_ENABLED='0'; go test ./internal/infrastructure/database -run TestDriverContract -count=1 -v
go test -race ./internal/infrastructure/database -run TestDriverContract -count=1
```

Expected: all PASS. Restore the caller's `CGO_ENABLED` value afterward. Cross-compile the test package for Windows, Linux, and Darwin with `go test -c` into a temporary directory; do not commit binaries.

If any required gate fails, stop and run the equivalent throwaway test with `mattn/go-sqlite3` v1.14.49 in a temporary branch/worktree. Do not keep both modules. Record build-tool, size, and failure evidence in ADR-0002.

**Step 4: Finalize ADR-0002**

Change it to Accepted only when the evidence passes. Record exact versions, Go/OS/arch, CGO result, SQLite version, license, WAL/foreign-key/busy-timeout result, Wails build result when available, and known limitations. If the evidence is incomplete, leave it Proposed and mark WP-01 PARTIAL.

**Step 5: Record direct notices**

Create `THIRD_PARTY_NOTICES.md` covering Wails (MIT), `modernc.org/sqlite` (BSD-3-Clause), and bundled SQLite (public domain), with exact versions and upstream links. Review `go list -m all`; record any license requiring notice. GPL/AGPL/restrictive dependencies block the package pending approval.

**Step 6: Suggested commit boundary**

```powershell
git add go.mod go.sum internal/infrastructure/database/driver_test.go docs/adr/0002-sqlite-driver-and-migrations.md THIRD_PARTY_NOTICES.md
git commit -m "build: select SQLite driver"
```

### Task 5: Implement migrations, snapshot, and safe mode

**Files:**

- Create: `internal/infrastructure/database/migrations/000001_foundation.sql`
- Create: `internal/infrastructure/database/migrate.go`
- Create: `internal/infrastructure/database/migrate_test.go`
- Create: `internal/infrastructure/database/database.go`
- Create: `internal/infrastructure/database/database_test.go`
- Modify: `app.go`

**Step 1: Write the foundation migration**

The SQL must create only foundation tables:

```sql
CREATE TABLE schema_migrations (
    version INTEGER PRIMARY KEY,
    checksum TEXT NOT NULL CHECK (length(checksum) = 64),
    applied_at TEXT NOT NULL
);

CREATE TABLE file_objects (
    hash TEXT PRIMARY KEY CHECK (length(hash) = 64),
    storage_key TEXT NOT NULL UNIQUE CHECK (length(storage_key) = 64),
    mime_type TEXT NOT NULL,
    size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
    created_at TEXT NOT NULL
);

CREATE TABLE file_references (
    owner_type TEXT NOT NULL,
    owner_id TEXT NOT NULL,
    file_hash TEXT NOT NULL REFERENCES file_objects(hash) ON DELETE RESTRICT,
    created_at TEXT NOT NULL,
    PRIMARY KEY (owner_type, owner_id, file_hash)
);
```

Do not add Drama, Agent, Job, Secret, Provider, or legacy project tables.

**Step 2: Write failing migration tests**

Tests must cover:

- fresh DB applies version 1 once;
- a second run is idempotent;
- checksum mismatch fails closed;
- a bad second migration rolls back its transaction;
- migration ordering is numeric and duplicate versions fail;
- `rows.Close()` and `rows.Err()` paths are exercised where rows are used.

Use `fstest.MapFS` for good and deliberately broken migration fixtures. Run `go test ./internal/infrastructure/database -run Migration -v`; expected FAIL before the runner exists.

**Step 3: Implement the embedded runner**

Use `//go:embed migrations/*.sql`, `fs.Glob`, `sort.Strings`, `crypto/sha256`, and `database/sql`. Validate the numeric filename before using it in `PRAGMA user_version = N`. Apply each migration in one transaction, insert the checksum, update `user_version`, and commit. Never edit a released migration.

Run migration tests; expected PASS.

**Step 4: Write failing snapshot and safe-mode tests**

Create an existing version-1 DB, introduce a test version-2 migration, open through the production manager, and assert a non-empty timestamped snapshot exists before version 2 is applied. Use a destination path containing an apostrophe to prove safe quoting. For a broken migration, assert:

- the manager returns `ModeSafe` and a stable `AppError`;
- no writable DB is exposed;
- the original database still reports version 1;
- the pre-migration snapshot can be opened and read.

**Step 5: Implement consistent pre-migration snapshot**

With the Wails single-instance lock already held, inspect `PRAGMA user_version`. When an existing non-empty DB needs migration, create a snapshot using SQLite `VACUUM INTO`. Obtain the SQL literal via `SELECT quote(?)` before concatenating it into `VACUUM INTO`; never concatenate a raw path. Fsync the completed snapshot. Fresh empty databases do not need a snapshot.

On any migration/snapshot failure, close the writable handle and return safe-mode health with the diagnostic ID. Do not silently continue, delete the DB, or restore automatically.

**Step 6: Verify database lifecycle**

Run:

```powershell
go test ./internal/infrastructure/database -count=1 -v
go test -race ./internal/infrastructure/database -count=1
```

Expected: PASS, including WAL, foreign keys, migration, failure, snapshot, and close/reopen behavior.

**Step 7: Suggested commit boundary**

```powershell
git add internal/infrastructure/database app.go
git commit -m "feat: add safe SQLite migrations"
```

### Task 6: Implement the content-addressed FileStore

**Files:**

- Create: `internal/application/files/ports.go`
- Create: `internal/application/files/service.go`
- Create: `internal/application/files/service_test.go`
- Create: `internal/infrastructure/filestore/store.go`
- Create: `internal/infrastructure/filestore/store_test.go`
- Create: `internal/infrastructure/database/files.go`
- Create: `internal/infrastructure/database/files_test.go`
- Modify: `app.go`

**Step 1: Define the minimum application contract**

```go
type Object struct {
    Hash       string
    StorageKey string
    MIME       string
    Size       int64
}

type Store interface {
    Put(context.Context, string, io.Reader) (Object, error)
    Open(context.Context, string) (io.ReadCloser, error)
}

type Repository interface {
    UpsertObject(context.Context, Object) error
}
```

`Service.Import` calls `Store.Put`, then `Repository.UpsertObject`, and returns the DTO. Keep interfaces in the consuming application package. Do not expose raw filesystem paths.

**Step 2: Write failing FileStore tests**

Use `t.TempDir()` and table-driven tests for:

- write through a temp file, SHA-256, detected MIME, size, and final read;
- duplicate content returns the same key and leaves one final object;
- `../x`, absolute paths, separators, colon/device names, empty names, and NUL are rejected;
- canceled context stops a large copy and removes the temp file;
- missing 64-hex key returns a diagnosable `FILE_NOT_FOUND` error;
- invalid storage key never reaches the filesystem;
- a failing reader leaves no committed or temporary file.

Run `go test ./internal/infrastructure/filestore -v`; expected FAIL before implementation.

**Step 3: Implement with the standard library**

`Put` must:

1. validate the display filename but never use it as a path;
2. create a random temp file under the managed temp directory;
3. read at most the first 512 bytes for `http.DetectContentType`, then stream the full content through `sha256.New()` and the temp file;
4. check context during copying;
5. `Sync`, close, and derive a lowercase 64-hex storage key;
6. create `<files>/<first-two-hex>/` and atomically rename the temp file to `<hash>`;
7. if the final object already exists, remove only the owned temp file and return the existing object;
8. clean the owned temp file on every failure.

`Open` accepts only `[0-9a-f]{64}`, opens the derived managed path, and maps missing files to a safe `AppError` with a diagnostic ID.

**Step 4: Implement metadata upsert**

Use one parameterized SQLite statement:

```sql
INSERT INTO file_objects(hash, storage_key, mime_type, size_bytes, created_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(hash) DO UPDATE SET
    mime_type = excluded.mime_type,
    size_bytes = excluded.size_bytes;
```

Reject a conflicting storage key/hash or size rather than masking corruption. Test foreign-key rejection for a `file_references` row whose object is missing.

**Step 5: Run all FileStore tests**

```powershell
go test ./internal/application/files ./internal/infrastructure/filestore ./internal/infrastructure/database -count=1 -v
go test -race ./internal/application/files ./internal/infrastructure/filestore ./internal/infrastructure/database -count=1
```

Expected: PASS. Inspect the temp directory after failure cases; it must be empty.

**Step 6: Suggested commit boundary**

```powershell
git add internal/application/files internal/infrastructure/filestore internal/infrastructure/database app.go
git commit -m "feat: add content-addressed file store"
```

### Task 7: Add health application service, Wails binding, and event envelope

**Files:**

- Create: `internal/application/health/service.go`
- Create: `internal/application/health/service_test.go`
- Create: `internal/desktop/health_binding.go`
- Create: `internal/desktop/health_binding_test.go`
- Create: `internal/desktop/events.go`
- Create: `internal/desktop/events_test.go`
- Modify: `app.go`
- Modify: `main.go`

**Step 1: Write failing health tests**

The returned JSON DTO is exactly:

```go
type Snapshot struct {
    Version       string `json:"version"`
    Database      string `json:"database"`
    DataDirectory string `json:"dataDirectory"`
    SafeMode      bool   `json:"safeMode"`
    Diagnostic    string `json:"diagnostic,omitempty"`
}
```

Test `ready` and `safe_mode` database states. The service depends on a tiny `Probe` interface defined beside the service; the binding stores lifecycle context and exposes only `Get() Snapshot`.

Run `go test ./internal/application/health ./internal/desktop -run Health -v`; expected FAIL before implementation.

**Step 2: Implement health and binding**

The application service calls `PingContext` when a DB exists and returns safe mode on startup/migration failure. `HealthBinding.Get` uses the Wails startup context, has no setter, and never returns a database file path, SQL, or error cause. `DataDirectory` is the approved app data root required by AC-FOUND-001.

**Step 3: Write and implement the event envelope**

```go
type Envelope struct {
    Version int    `json:"version"`
    ID      string `json:"id"`
    Type    string `json:"type"`
    Time    string `json:"time"`
    Payload any    `json:"payload"`
}
```

Use `crypto/rand` for ID and UTC RFC3339Nano time. Emit only the stable Wails event name `core:event`. On DOM ready emit `health.changed` with the health snapshot. Unit-test JSON keys, version 1, non-empty ID, UTC time, and payload. Do not add an event bus dependency.

**Step 4: Generate and inspect bindings**

Run:

```powershell
wails generate module
```

Expected: generated files under `web/src/wailsjs/` expose only the intended health method/models plus Wails runtime. Search generated output for `apiKey`, `secret`, `Resolve`, SQL, filesystem mutation, and arbitrary network methods; expected none. Never hand-edit these files.

**Step 5: Suggested commit boundary**

```powershell
git add internal/application/health internal/desktop app.go main.go web/src/wailsjs
git commit -m "feat: expose desktop health binding"
```

### Task 8: Connect health to React without breaking browser development

**Files:**

- Create: `web/src/services/desktop/health.ts`
- Create: `web/src/hooks/use-desktop-health.ts`
- Create: `web/src/components/layout/desktop-health-status.tsx`
- Modify: `web/src/components/layout/user-status-actions.tsx`
- Modify: `web/src/i18n/locales/en-US.ts`
- Modify: `web/src/i18n/locales/zh-CN.ts`

**Step 1: Add a runtime-safe adapter**

The adapter imports generated `Get` and Wails `EventsOn`, but first detects the injected Wails globals. Its public behavior is:

```ts
export async function getDesktopHealth(): Promise<desktop.Snapshot | null> {
    if (!("go" in window)) return null;
    return Get();
}

export function onHealthChanged(callback: () => void): () => void {
    if (!("runtime" in window)) return () => undefined;
    return EventsOn("core:event", (event) => {
        if (event?.version === 1 && event.type === "health.changed") callback();
    });
}
```

Validate the event shape before acting. Do not place health state in Zustand.

**Step 2: Reuse React Query**

`useDesktopHealth` uses the existing Query Client with key `['desktop-health']`, calls `getDesktopHealth`, and subscribes/unsubscribes in an effect. Browser/Vite mode returns `null` without an error or network call.

**Step 3: Render one accessible status**

`DesktopHealthStatus` returns `null` outside Wails. In Wails it renders a compact text+icon status in `UserStatusActions`:

- ready: localized “Local database ready”;
- safe mode: localized “Safe mode” with the diagnostic ID in an accessible tooltip;
- loading: `aria-label` only, no blocking overlay.

State must not rely on color alone. Do not add a new settings page.

**Step 4: Verify both frontend modes**

Run:

```powershell
Set-Location web
npm run typecheck
npm run build
npm run dev -- --host 127.0.0.1 --port 4173
```

Expected: typecheck/build PASS; browser mode opens existing routes with no Wails-global exception and no health badge. Stop the dev process cleanly.

**Step 5: Suggested commit boundary**

```powershell
git add web/src/services/desktop web/src/hooks/use-desktop-health.ts web/src/components/layout/desktop-health-status.tsx web/src/components/layout/user-status-actions.tsx web/src/i18n/locales/en-US.ts web/src/i18n/locales/zh-CN.ts
git commit -m "feat: show desktop core health"
```

### Task 9: Prove production desktop behavior and existing route compatibility

**Files:**

- Modify only if a verified defect requires it: `main.go`, `app.go`, `wails.json`, or health bridge files
- Record results: `docs/implementation/STATUS.md`

**Step 1: Run all automated gates**

From repository root:

```powershell
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
./scripts/verify.ps1
```

Expected: PASS, except an unavailable platform race detector must be recorded as an environment skip rather than disguised.

**Step 2: Build the desktop binary**

Run:

```powershell
wails build -clean
```

Expected: PASS; `build/bin/InfiniteAtelier.exe` exists and embeds the frontend. No external Vite server is listening or required.

**Step 3: Perform the Windows desktop smoke matrix**

Launch the binary without Provider credentials or network access and verify:

1. one native window opens;
2. health reports version, `ready`, and the managed data directory;
3. `/`, `/assets`, `/canvas`, `/director`, and `/config` render;
4. an existing browser-mode canvas remains untouched;
5. a second launch focuses the first instance and does not open a second writer;
6. closing the final window exits the process;
7. restart reopens the same database with migration version unchanged;
8. logs contain structured fields and no secrets/headers/SQL values.

Record source/manual evidence separately; do not call Provider endpoints.

**Step 4: Exercise safe mode**

Only in `t.TempDir()` integration tests—not the user data directory—inject a failing migration and verify the desktop composition can still return safe-mode health while exposing no writable database. Never corrupt the real app DB to demonstrate this.

**Step 5: Check artifacts and secrets**

Run repository secret scanning without printing matched values, inspect `git status --ignored`, and ensure only expected ignored `node_modules`, `dist`, database test temp files, and `build/bin` artifacts exist. No DB, logs, user media, or key material may be tracked.

### Task 10: Extend verification and CI with real WP-01 gates

**Files:**

- Modify: `scripts/verify.ps1`
- Modify: `scripts/verify.sh`
- Modify: `.github/workflows/desktop-build.yml`
- Modify: `README.md`

**Step 1: Update verification scripts**

After the existing frontend build, run these when `go.mod` exists:

```text
go test ./... -count=1
go vet ./...
```

If `wails` is installed, run `wails build`; otherwise print `SKIP: Wails build — install pinned CLI v2.15.0` without hiding the separately required local production-build evidence. Preserve nonzero exit on any executed gate failure. Never call Providers.

**Step 2: Add CI jobs without pretending cross-platform coverage**

- Keep the Ubuntu web job and make its repaired `npm ci` gate pass.
- Add a Windows job that installs Go 1.24 and Node 22, runs `npm ci`, `go test ./...`, `go vet ./...`, installs `wails@v2.15.0`, and runs `wails build`.
- Do not add macOS/Linux desktop packaging until their Wails prerequisites and runners are explicitly approved; record this as a matrix gap.
- Upload no database, logs, secrets, or user data.

**Step 3: Update README**

Document:

- browser mode: existing npm commands;
- desktop prerequisites and exact Wails CLI install command;
- `wails dev` and `wails build`;
- managed data location behavior;
- verification scripts;
- that Provider/Secret migration is not part of WP-01 yet.

**Step 4: Run both verification entry points**

```powershell
./scripts/verify.ps1
& 'C:\Program Files\Git\bin\bash.exe' -n scripts/verify.sh
& 'C:\Program Files\Git\bin\bash.exe' scripts/verify.sh
```

Expected: PASS on this host, with explicit SKIP reasons only for genuinely unavailable gates.

**Step 5: Suggested commit boundary**

```powershell
git add scripts/verify.ps1 scripts/verify.sh .github/workflows/desktop-build.yml README.md
git commit -m "ci: verify desktop foundation"
```

### Task 11: Close WP-01 with evidence, not assertions

**Files:**

- Modify: `docs/implementation/STATUS.md`
- Modify: `docs/implementation/TRACEABILITY.md`
- Modify if evidence changed: `docs/adr/0001-desktop-framework.md`
- Modify: `docs/adr/0002-sqlite-driver-and-migrations.md`

**Step 1: Run final checks**

```powershell
git diff --check
git status --short --branch
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
./scripts/verify.ps1
wails build -clean
```

Expected: all mandatory host gates PASS. Record exact versions, exit results, warnings, skips, binary path/size, and manual smoke evidence.

**Step 2: Evaluate acceptance literally**

- AC-FOUND-001: native production window, embedded React, health binding, no external Vite server, Windows process exits.
- AC-FOUND-002: empty create, ordered migrations, foreign keys, WAL, safe mode, pre-migration snapshot.
- AC-FOUND-003: temp write, SHA-256, MIME magic, atomic commit, deduplication, traversal rejection, diagnosable missing file.

Any missing bullet makes WP-01 PARTIAL or BLOCKED. Do not mark it COMPLETE because code merely compiles.

**Step 3: Update status and traceability**

Move only verified FR-010/020/030 and related NFR/SEC rows from Gap to Implemented/Partial. Preserve WP-00 history and current user changes. List cross-platform packaging, frontend test coverage, and Secret/Provider work as open risks.

**Step 4: Final safety inspection**

Confirm:

- no raw API key, DB, log, media, temp file, binary, or generated build output is tracked;
- released migration `000001_foundation.sql` has not changed after acceptance;
- existing browser data is untouched;
- no real Provider call occurred;
- `git diff --check` passes;
- product changes are limited to WP-01.

**Step 5: Stop**

Report WP-01 using the mandatory `AGENTS.md` format and recommend WP-02, but do not start it. Wait for explicit user approval.

## Expected final file map

```text
main.go
app.go
go.mod
go.sum
wails.json
THIRD_PARTY_NOTICES.md
internal/
  buildinfo/version.go
  domain/apperror/error.go
  application/files/{ports,service}.go
  application/health/service.go
  desktop/{events,health_binding}.go
  infrastructure/appdirs/dirs.go
  infrastructure/database/{database,driver,files,migrate}.go
  infrastructure/database/migrations/000001_foundation.sql
  infrastructure/filestore/store.go
  infrastructure/logging/logging.go
web/src/
  wailsjs/                     # generated, committed
  services/desktop/health.ts
  hooks/use-desktop-health.ts
  components/layout/desktop-health-status.tsx
```

Tests live beside the corresponding Go package. Add no generic `utils`, ORM models, Provider code, Secret code, or future domain scaffolding.
