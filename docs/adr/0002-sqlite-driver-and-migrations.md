# ADR-0002 SQLite Driver and Migration Strategy

- Status: Proposed
- Date: 2026-09-04; Task 4 evidence updated 2026-09-07; Task 5/9/11 evidence updated 2026-09-08
- Deciders: Repository engineering; final selection deferred to WP-01 evidence
- Related work package: WP-00 (option record), WP-01 (spike and decision)

## Context

The approved architecture makes SQLite the source of truth for metadata and local files the source of media bytes. At WP-00 the repository had neither Go nor SQLite; browser localForage/localStorage was authoritative. WP-01 Tasks 2–4 now add the Go/Wails shell and a SQLite driver helper; no browser data has been migrated. WP-01 must introduce `database/sql`, foreign keys, WAL, versioned migrations, repositories, revision-safe updates, deterministic tests, and a backup-safe foundation across Wails desktop targets.

Choosing a driver affects CGO, cross-compilation, binary size, native extension support, platform packaging, race/sanitizer options, and CI. Choosing a migration strategy affects crash recovery, released-schema immutability, checksums, downgrade expectations, and test isolation. Task 4 pins the first candidate; Proposed status retains the unverified migration, snapshot, and desktop release gates below.

## Decision drivers

- Windows/macOS/Linux Wails v2 packaging and CI reproducibility.
- `database/sql` compatibility and context-aware operations.
- SQLite foreign keys, WAL, transactions, busy timeout, JSON/FTS requirements, and future vector-extension feasibility.
- Predictable native library/version ownership and security patching.
- Deterministic temporary-database tests and migration-from-every-released-version fixtures.
- Forward-only, immutable released migrations with observable schema version.
- Permissive license, active maintenance, and bounded dependency surface.
- A consistent online-backup/export path without copying a live WAL database incorrectly.

## Options considered: SQLite driver

| Option | Advantages | Costs / risks | Required spike evidence |
|---|---|---|---|
| `modernc.org/sqlite` | Pure Go toolchain; avoids application CGO compiler dependency; direct `database/sql` use. | Larger generated/runtime surface; performance and memory need measurement; SQLite build features/extensions and Wails packaging must be verified. | CRUD/transactions, WAL/foreign keys/busy handling, race-aware concurrency, binary size/startup, all target builds, backup path, required extension feasibility. |
| `github.com/mattn/go-sqlite3` | Mature and widely used; direct native SQLite integration; familiar extension/build-tag behavior. | Requires CGO and platform C toolchains; cross-compilation/signing/CI are more complex; SQLite compilation options must be controlled. | Same functional suite plus CGO toolchain matrix, cross-build reproducibility, DLL/static-link behavior, binary provenance and extension loading policy. |
| Alternate wrappers/ORMs | May add convenience or generated queries. | Can obscure SQL/transaction behavior and expand dependencies; does not remove the underlying driver decision. | Only reconsider through a separate ADR after the driver/ports work; ORM adoption is not part of WP-01. |

## Options considered: migrations

| Option | Advantages | Costs / risks |
|---|---|---|
| Small repository-owned runner using `//go:embed` SQL and `database/sql` | Minimal runtime dependency; exact transaction/checksum/version behavior; easy to keep SQL in Infrastructure. | Must correctly implement locking, dirty-state detection, checksums, error diagnostics, and tests; home-grown code is a maintenance commitment. |
| `golang-migrate/migrate` library | Established migration model and tooling. | Additional driver/glue/dependency surface; dirty-state and SQLite locking/packaging behavior must be aligned with the desktop lifecycle. |
| `pressly/goose` library/CLI | Convenient SQL/Go migrations and library usage. | Additional CLI/library policy, dependency and release surface; Go migrations can blur deterministic SQL history if unrestricted. |

## Proposed decision

Use `database/sql` behind Infrastructure repositories. Task 4 pins `modernc.org/sqlite` v1.58.0 as the only candidate in the production module graph. `mattn/go-sqlite3` v1.14.49 remains a documented fallback only if actual required driver behavior fails; it is not installed. A host race-compiler limitation alone does not establish a SQLite defect. Final acceptance still requires measured packaging, migration, and consistent snapshot evidence. Vector functionality belongs to a later work package and cannot expand WP-01.

The helper builds a file URI with `net/url` from an application-owned path. Driver DSN pragmas apply `foreign_keys=1`, `journal_mode=WAL`, and `busy_timeout=5000` on every connection. A one-connection pool bounds local access; callers must finish transactions/close rows before acquiring another connection. Expanding the pool requires measured contention and new transaction/concurrency evidence. The helper owns cleanup after failed `PingContext`, returns stable `DATABASE_OPEN_FAILED` diagnostics, and does not expose a Wails binding or create application tables.

`go mod tidy` completed on 2026-09-07 and classifies `modernc.org/sqlite` v1.58.0 as a direct module requirement.

The driver version was published on 2026-09-01 according to [its version documentation](https://pkg.go.dev/modernc.org/sqlite@v1.58.0); the canonical repository is [cznic/sqlite](https://gitlab.com/cznic/sqlite), with [modernc-org/sqlite](https://github.com/modernc-org/sqlite) as its official GitHub mirror. Its local module license is BSD-3-Clause; the bundled SQLite engine reports 3.53.4 and is [public domain](https://www.sqlite.org/copyright.html). These are maintenance and provenance signals, not an assertion of a security audit. Exact notices, including nested libc/memory/SQLite notices, are preserved in `THIRD_PARTY_NOTICES.md`.

Both Wails v2.15.0 and modernc SQLite v1.58.0 require Go 1.25.0. The plan's Go 1.24 text is stale; use Go 1.25 with `GOTOOLCHAIN=auto`. The candidate adds libc v1.75.6, mathutil v1.7.1, memory v1.12.1, go-humanize v1.0.1, go-strftime v1.0.0 and bigfft v0.0.0-20230129092748-24d4a6f8daec. Minimal version selection raises go-isatty from v0.0.20 to v0.0.24 and x/sys from v0.46.0 to v0.47.0. No ORM or second SQLite driver is present.

For migrations, prefer embedded, numbered, forward SQL files applied by a small application-owned runner only if WP-01 implements and tests all safeguards below. If that runner grows beyond the bounded feature set, select one reviewed migration library and record the exact license/version in a superseding or Accepted revision.

Required migration contract:

- immutable released files named with monotonically increasing versions;
- a `schema_migrations` table containing version, applied timestamp, and content checksum;
- exclusive startup migration ownership and explicit dirty/failure state;
- each compatible migration transactional where SQLite permits;
- foreign keys enabled and verified per connection; WAL/busy timeout configured centrally;
- no external network calls inside database transactions;
- upgrade fixtures from every released schema; fresh-database and interrupted/failing negative tests;
- forward-only production policy; recovery through a verified backup rather than untested down migrations;
- stable safe errors/diagnostic IDs without paths, SQL values, or secrets;
- consistent backup through a SQLite-supported snapshot/backup operation, including WAL state.

Driver-specific build flags, dynamic extension loading, and user-supplied extensions are denied by default. `sqlite-vec` or another vector mechanism requires its own compatibility/security evidence; persistent Memory does not justify enabling arbitrary extension loading.

## Consequences

- WP-01 cannot treat “opens a database” as completion; it needs a platform and failure-mode matrix.
- Repository/domain ports remain insulated from a later driver change.
- No published migration may be edited after release; fixes are new migrations.
- Pure-Go convenience is not assumed to be smaller/faster, and CGO maturity is not assumed to be operationally free.
- Backup, migration, and legacy import must be tested together before browser data is moved or deleted.

## Verification required before Accepted

### Task 4 evidence recorded on 2026-09-07

- TDD: `go test ./internal/infrastructure/database -run TestDriver -count=1 -v` first failed because `openDriver` did not exist, then passed all three tests after implementation. Root-agent independent execution also passed.
- The Windows/amd64 test reports SQLite 3.53.4. It verifies the exact filename with spaces, Unicode and URI-reserved characters; WAL, foreign keys and 5000 ms busy timeout on replacement connections; a missing-parent insert rejection; a bounded pool with context cancellation; safe open-failure diagnostics; and canceled open creating no file.
- `go mod tidy` completed with host module-cache write permission. `go.mod` now requires `modernc.org/sqlite` v1.58.0 directly with Wails v2.15.0. `go list -m all` and `go list -deps` for the database package show no ORM, wrapper, or second SQLite driver. `mattn/go-colorable`/`go-isatty`/`go-runewidth` remain Wails UI dependencies, not SQLite drivers.
- `go test ./internal/infrastructure/database -run TestDriver -count=20 -v` PASS, 4.234s, SQLite 3.53.4.
- Caller `CGO_ENABLED` was unset (`go env CGO_ENABLED` still reports the default `1`). `CGO_ENABLED=0 go test ./internal/infrastructure/database -run TestDriver -count=1 -v` PASS, 3.190s. The caller environment was restored to unset afterward.
- `CGO_ENABLED=0 go test -c` produced host-temp binaries for `windows/amd64` (11054592 bytes), `linux/amd64` (10737657 bytes), and `darwin/amd64` (10897840 bytes). Those binaries were not committed and were deleted after size recording. Cross-compilation is not a native runtime or Wails packaging result.
- `go test ./... -count=1`, `go vet ./...`, `go build ./...`, `gofmt -l .`, and `git diff --check` PASS. Host: Go 1.25.0, `GOTOOLCHAIN=auto`, windows/amd64.
- `go test -race ./internal/infrastructure/database -run TestDriver -count=1` FAIL / environment limitation: `cc1.exe: sorry, unimplemented: 64-bit mode not compiled in`. Host MinGW.org GCC 6.3.0 is 32-bit and cannot compile amd64 CGO. This is not a SQLite defect and did not add `mattn/go-sqlite3`.
- The Task 4 helper is not yet wired into desktop composition; a shell-only build would not prove a linked production database path.
- No migration runner, schema, consistent snapshot, rollback, safe-mode composition or native runtime smoke is claimed by this task. Those are Task 5/9 evidence gates.

### Task 5/9/11 evidence recorded on 2026-09-08

- The bounded repository-owned runner is now implemented with embedded forward-only SQL, numeric migration ordering, per-file SHA-256 checksums, per-migration transactions, `schema_migrations`, and `PRAGMA user_version`. The only released migration creates the three foundation tables `schema_migrations`, `file_objects`, and `file_references`; no Drama, Secret, Provider, Agent, Job, or legacy-project table was added.
- Database integration tests cover fresh creation, idempotence, numeric order, duplicate/checksum failure, transactional rollback on a bad migration, safe mode, pre-migration snapshot, close/reopen, WAL, managed-connection foreign keys, busy timeout, and FileStore reference constraints. The production migration strategy therefore intentionally remains a small bounded runner rather than adding a migration framework; this is the evidence-based resolution of the Architecture document's more general mature-tool wording.
- Production Windows evidence: Wails v2.15.0 `wails build -clean` and later Task 10/11 `wails build` passes produced `build/bin/InfiniteAtelier.exe`. The final Task 11 build was exercised in a fresh owned temporary environment with all AppData/WebView variables redirected. It created the managed database, opened a native window, closed gracefully, and a post-close read-only SQLite connection reported migration version `[(1,)]` and journal mode `wal`.
- A post-close third-party Python SQLite connection reported `foreign_keys=0`, which is that new connection's SQLite default rather than the managed application connection's configuration. The application's own driver contract tests verify `foreign_keys=1` on managed connections and a missing-parent rejection.
- The final host command `go test -race ./... -count=1` remains an environment failure: the host `C:\MinGW\bin\gcc.exe` invokes a 32-bit GCC and cannot compile amd64 CGO (`cc1.exe: sorry, unimplemented: 64-bit mode not compiled in`). No second driver was introduced to evade it.

### Remaining acceptance gates before Accepted

1. Obtain `go test -race` evidence on a supported native compiler/toolchain.
2. Establish native Wails build-and-test evidence for macOS and Linux; the Windows CI job is defined locally but remote GitHub Actions has not run because no push was authorized.
3. Measure binary size, startup, representative CRUD/batch operations, and memory under a defined workload.
4. Prove a full backup/restore flow with WAL enabled and read-back; WP-01's pre-migration snapshot is not a user backup feature.
5. Update this ADR to Accepted (or add a superseding ADR) with the remaining captured results before release.

## References

- `PRD.md`
- `docs/ARCHITECTURE.md`
- `docs/DOMAIN_MODEL.md`
- `docs/SECURITY.md`
- `docs/ACCEPTANCE.md`
- `docs/implementation/REPO_AUDIT.md`
