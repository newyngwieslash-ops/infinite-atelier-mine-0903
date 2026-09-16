# Implementation Status

> Last updated: 2026-09-15
> Product: Infinite Atelier Core + Drama Production Pack
> Current work package: **WP-03 — Persistent Job Manager 与多模态 Provider**
> Status: **COMPLETE for the WP-03 scope recorded in section 0c** (job tables and state machine, scheduler/worker/lease/retry, startup recovery, OpenAI- and Gemini-compatible image adapters, async video and audio contracts with mocks, result pipeline into the FileStore, narrow job bindings, Job Center UI, image generation routed through Go, scans). WP-01 and WP-02 remain COMPLETE for their recorded scopes.

WP-03 start baseline (2026-09-15): branch `codex/wp-01-desktop-foundation`, HEAD `a243891455ec17687dd54b5ac90d3bd64478a1a1`, empty index. Freshly re-run baseline: `go test ./... -count=1` PASS (15 packages at start), `go vet ./...` PASS, `web` `npm run typecheck` PASS, `npm test` PASS (15 tests), `npm run build` PASS. Go commands require `GOTOOLCHAIN=go1.25.0 GOSUMDB=sum.golang.org` on this host because the user-level `go env` sets `GOSUMDB=off`, which blocks toolchain verification. The working tree already contained the WP-01/WP-02 tracked and untracked work plus the user's brand rename; none of it was modified outside the WP-03 scope.

---

# 0c. WP-03 result (2026-09-15)

## Scope completed

- **Status: COMPLETE (WP-03 scope)**. All acceptance items below were executed on this host; limitations are listed explicitly and are not disguised as passes.
- Schema: forward-only `000003_jobs.sql` adds `generation_jobs`, `job_attempts`, `job_dependencies`, and rebuilds `provider_requests`/`provider_configs` to widen the capability/kind CHECKs while preserving existing rows. `000001`/`000002` were not modified.
- Domain: `internal/domain/job` owns the state set, transition table, retry policy, dependency conditions, and the stable error taxonomy; it has no infrastructure dependencies.
- Application: `internal/application/jobs` owns submit (idempotent), cancel (including a best-effort remote cancel and an orphan-candidate record), retry-failed-only, list/get, pause/resume, the priority scheduler, the worker pool with lease + revision guards, and the startup recovery scanner.
- Infrastructure: SQLite `JobRepository` (claim/CAS/idempotency/attempts/dependencies), the result pipeline (`ResultStore` → content-addressed FileStore → object metadata → `file_references`), a download-policy client for provider-supplied URLs, OpenAI- and Gemini-compatible image adapters, and deterministic async video / audio contract mocks.
- Bindings: narrow `JobsBinding` (list/get/attempts/submit/cancel/retry/pause/resume/summary/read-result-file). No execute, no resolve, no arbitrary path, no SQL. Bindings regenerated with the pinned Wails v2.15.0 CLI.
- Frontend: Job Center page (queue table, progress, batch cancel, retry-failed-only, pause/resume, accessible status text plus colour), desktop jobs client with event subscription, image generation routed through the Go job manager in secure mode with no fallback to the legacy path, and a migration notice stating that video/audio still use the browser-direct path.
- Scans: the security scanner now also refuses NEW browser-direct provider calls, with exact legacy-file exemptions and stale-exemption detection.

## Commands executed and actual results

| Command | Result |
|---|---|
| `go test ./... -count=1` | **PASS** (19 packages ok) |
| `go vet ./...` | **PASS** |
| `gofmt -l .` | **PASS** (no output) |
| `node scripts/security-scan.mjs` | **PASS** (247 files scanned; 1 audited dynamic-execution exception, 4 audited legacy direct-call files with named owners) |
| `web`: `npm run typecheck` | **PASS** |
| `web`: `npm test` | **PASS** (21 tests) |
| `web`: `npm run build` | **PASS** with the pre-existing >500 kB chunk warning |
| `scripts/verify.sh` | **PASS** (all steps; Wails SKIPped by the script because the CLI is not on PATH) |
| `scripts/verify.ps1` | **PASS** (same Wails caveat) |
| `wails build -s` (v2.15.0) | **PASS**; produced `build/bin/InfiniteAtelier.exe` |
| `git diff --check` | **PASS** (exit 0; only pre-existing LF/CRLF advisory warnings) |
| `go test -race ./... -count=1` | **ENVIRONMENT FAILURE, not a pass** — the host 32-bit MinGW GCC cannot compile amd64 CGO. Concurrency was reviewed by tracing and targeted probes only. |

Note on the Wails invocation: `wails build` runs the frontend `npm ci` itself, which fails with `EPERM` while the user's own Vite dev server holds locks on `web/node_modules`. The build is therefore run with `-s` after a successful frontend build, as in WP-02. On this host a `wails build` without `-s` was observed to leave `web/node_modules` half-uninstalled (`.bin` empty); `web/package.json` and `web/package-lock.json` were verified byte-identical afterwards (SHA-256) and the dependencies were repaired with the same `npm install --legacy-peer-deps --include=optional --package-lock=false` command WP-00 recorded.

## Acceptance

| ID | Result | Evidence |
|---|---|---|
| AC-FOUND-006 (job) | **PASS (WP-03 scope)** | queued→running→succeeded: `TestSchedulerRunsJobsToCompletion`; retry_wait: `TestExecuteRetriesTransientFailure`, `TestSchedulerRetriesThenSucceeds`, `TestJobRepositoryClaimableRespectsRetryWindow`; cancel: queued + in-flight + during-a-failing-attempt + during-a-poll + during-shutdown (`TestCancelQueuedJobIsImmediate`, `TestCancelDuringFailingAttemptStillTerminates`, `TestCancelDuringPollStillSettles`, `TestCancelSettlementSurvivesCancelledWorkerContext`); crash/restart: recovery tests incl. `TestRecoverDownloadingResumesWithoutReCallingProvider` and `TestCrashWithinLeaseStillRuns`; remote ID poll never re-submits and is paced: `TestRecoverResumesRemoteWithoutResubmitting`, `TestVideoRestartResumesPollingWithoutResubmitting`, `TestRemotePollIsPaced`, `TestParkedPollDoesNotConsumeAttemptBudget`; duplicate idempotency: `TestSubmitIsIdempotent`, `TestJobRepositoryDuplicateIdempotencyKeyReturnsConflict`; result commit: `TestResultPipelineWritesMetadataAndReference` against the real FileStore + repositories; interrupted writes release their claim: `TestShutdownDuringAttemptSettlesJob`, `TestShutdownReturnsClaimedJobToQueue`, `TestCancelDuringUnrecordableOutcomeStillSettles`. |
| AC-MEDIA-001 (async video, mock) | **PASS for the mock contract (8/9 items)** | submit/remote ID/poll/restart/fetch/validate/duplicate response/cancel are covered by `TestRunnerVideoFullAsyncFlow`, `TestVideoRestartResumesPollingWithoutResubmitting`, `TestVideoDuplicateResponseDoesNotCreateSecondAsset`, `TestVideoMockContractExercisesTheFullLifecycle`, `TestRunnerVideoRemoteOnlyOutcome`. The ninth item, "asset version", has no `AssetVersion` entity in this package: assets belong to WP-05, and the job pipeline records `file_references(owner_type='job')` instead. **This is an explicit gap, not a pass.** |
| AC-SEC-001 (SSRF) | **PASS (unchanged)** | WP-02 corpus still enforced; the new download policy adds https-only, all-addresses-public, per-hop redirect re-validation, and a streaming byte cap (`internal/infrastructure/providerhttp/download_test.go`). |

## Independent review

- **Spec review (independent subagent): CHANGES REQUIRED → fixed → re-review: CHANGES REQUIRED → fixed.** Round one found a blocker (every result commit failed against the real database because the pipeline wrote a `file_references` row without recording `file_objects` first — the FK then rejected it, so no job could ever reach `succeeded`), plus majors (cancel-while-running never terminated; the `downloading` phase never persisted; the async cancel contract never invoked; mock media adapters reachable in production; the legacy-media notice not rendered; idempotency scope ignoring the entity). Round two confirmed the blocker fixed and found that my first `downloading` fix introduced two new blockers: the runner's mid-attempt row write invalidated the worker's revision (wedging any job whose fetch failed), and nothing consumed the marker (so a reclaimed job called the provider twice). Both were re-fixed by removing the mid-attempt write entirely: the runner now returns the phase as an Outcome, the worker persists it under its own revision guard, and the next pass resumes the fetch from the persisted marker.
- **Quality review (independent subagent): CHANGES REQUIRED → fixed.** Findings included `AllowLocal` permitting cleartext HTTP to a public host, deltas published after a terminal stream event, and several tests that could not detect the defect they named.

### Quality review (WP-03)

An independent quality review found, and the fixes above cover: the mock video/audio payloads were rejected by the real content allowlist (Go sniffs the mock MP4 as `application/octet-stream` and WAV as `audio/wave`, which the allowlist omitted) — both are corrected and pinned by `TestMockPayloadsPassTheRealAllowlist`, which runs the payloads through the real FileStore and allowlist; a `count>1` image batch collapsed into one job because every request shared an idempotency key (the batch ordinal is now part of the scope, with behavioural tests); three image paths submitted unscoped keys (all now carry the node identity); aborting a canvas generation left the Go job running (abort now cancels it by id, and the wait is bounded); the result download could deadlock when the store stopped reading (the reader is now closed on that path); a claim abandoned at shutdown held its lease for the full TTL (it is handed back); `attempt_count` could regress and loop (it can no longer move backwards); and the batch of dead helpers/options was removed.

Subsequent adversarial rounds (each one run by an independent reviewer against the current code, with its findings then fixed and mutation-checked here) found and closed:

- **Unpaced remote polling (major).** A parked `waiting_remote` job was re-claimed as soon as a worker freed up, i.e. at provider round-trip speed: a real video job would be polled hundreds of times per minute, one attempt row per poll, and the attempt budget would be exhausted by polling. Polls are now paced through `next_retry_at` (both the SQL candidate query and the claim guard honour it) and a pure poll is not an attempt (no counter advance, no history row), while a failing poll and a completing pass remain real attempts. ADR-0004 §1b records the rule; `TestRemotePollIsPaced`, `TestParkedPollDoesNotConsumeAttemptBudget`, `TestRemotePollFailureStillRetries`, and `TestJobRepositoryParkedJobIsPacedByRetryWindow` pin it.
- **Crash-era attempt numbering (blocker).** A crash between `StartAttempt` and the settle leaves attempt row N while `attempt_count` still reads N-1. Recovery skips jobs whose lease looks live, so a restart inside the 30-minute lease window left the counter stale and every later `StartAttempt` collided with the orphaned row, wedging the job in a claim/record-failure/release loop. Attempt numbering is now derived from the stored rows (`nextAttemptNumber`), and `release` schedules a minimum delay so a storage failure cannot spin the scheduler.
- **Cancel could leave a job claimed as running (major).** Cancelling only *flags* a running job, so if the cancel raced the worker's own outcome write, the failed write went to `release`, which returned early on the cancel flag — leaving the row `running` on a live 30-minute lease with no worker to settle it, because recovery skips live leases. `release` now settles a cancel-flagged job itself.
- **Settlement writes depended on a live worker context (major).** `finish`, `finishOutcome`, `release`, and the attempt-history writes ran on the caller's context, so a worker cancelled by shutdown failed every read and write and stranded the job exactly as above. All settlement writes now run on a bounded `settleContext` (the caller keeps its values, only the cancellation is dropped), so scheduler state is persisted before exit as ARCHITECTURE §6.2 requires.
- **Cancel during a poll re-parked the job** instead of settling it; the `PollOnly` branch now settles a cancel-flagged job and preserves the durable orphan note.
- **Unvalidated provider progress.** The provider-reported percentage is written into a `CHECK (0..100)` column; a provider reporting 120 made the whole row update fail *after* the artifact was fetched, turning a reporting quirk into a retry loop. It is now clamped.
- **Test-double drift (twice).** The infrastructure test double reimplemented Go's content sniffer (accepting truncated MP4 boxes and "GIF8" that the real sniffer rejects), and the in-memory job repository ignored the poll window. Both doubles now use the real behaviour (`http.DetectContentType`; the same claim guard as the SQL), and the MP4 payloads used in tests are genuinely valid boxes.
- Several regression tests were found not to detect the defect they named (a cancellation injected before the claim never reached the poll branch; the settlement test failed every write including the recovery write). They were rewritten and each fix was verified by reverting it and observing the test fail.

## Known limits and deferred work

- The async video and audio adapters are **contracts with deterministic mocks**, not real provider integrations; those belong to the media work package. The mock kind is rejected by the configuration path so a real provider cannot resolve to it.
- `estimated_cost` is never populated: no pricing table exists, and ADR-0004 does not define one. Cost accounting belongs to the model-policy work package.
- Video/audio generation in the UI still uses the legacy browser-direct path; the user is told so at startup, and the scanner refuses new direct calls.
- No `AssetVersion` entity exists yet (WP-05); the pipeline records job-owned file references instead.
- macOS/Linux evidence, remote CI, and `-race` remain unavailable for the reasons recorded under WP-01.
- Real paid providers were never contacted; all provider evidence uses httptest, fakes, and the deterministic mocks.
- Mock media calls do not write an audit row (they are a local test seam, not provider calls).
- `Options.LeaseTTL`/`WorkerCount`/`PollInterval` were removed rather than wired: the scheduler uses the package defaults, and a configurable value with no reader was misleading. `RemotePollInterval` **is** wired (`job_wiring.go`, 5 s), because the poll cadence is a real product decision rather than an internal constant.
- A poll of an unused async image contract (`ImageOutcome.RemoteJobID`) is reported as `unsupported` rather than silently succeeding: no shipping image adapter returns a remote ID, and the runner has no image poll implementation, so accepting it would fabricate a result.
- The audio formats offered in the UI (MP3/WAV/Opus/AAC/FLAC/PCM) are wider than the content allowlist: FLAC, AAC and raw PCM have no signature Go's sniffer can recognise, so such a result is rejected as `response_invalid` rather than stored with an opaque type. Real adapter work belongs to the media package and must revisit this deliberately.

---

# 0b. WP-02 result (2026-09-15)

## Scope completed

- **Status: COMPLETE (WP-02 scope)**. All acceptance items below were executed on this host; limitations are listed explicitly and are not disguised as passes.
- SecretStore: Windows Credential Manager adapter (`advapi32` `CredReadW`/`CredWriteW`/`CredDeleteW`/`CredFree` via `windows.NewLazySystemDLL`), non-Windows `UnavailableStore` that fails closed, and a test-only in-memory fake. See ADR-0003.
- SQLite: forward-only migration `000002_provider_security.sql` adds `secret_references`, `provider_configs`, `provider_requests`; no secret-value column exists; `provider_configs.secret_ref` FKs to `secret_references` with `ON DELETE RESTRICT`. `000001_foundation.sql` was not modified.
- Controlled provider HTTP: scheme/host/port policy, DNS A/AAAA resolution with forbidden-range rejection (full SECURITY §6.2 CIDR list plus IPv4-mapped forms and cloud metadata), dial-the-validated-IP pinning, per-hop redirect re-validation, TLS verification that cannot be disabled, no environment proxy, and connect/TLS/header/idle/total timeouts with a bounded response body.
- First adapter: trusted registry + OpenAI-compatible text `Generate` and SSE `Stream` with Go-side `Authorization`, 429/5xx/4xx taxonomy, bad-JSON and truncated-stream handling, cancellation, redacted audit records (now including provider-reported token units), and a health probe that is also audited.
- Narrow Wails bindings: secret `Status`/`Set`/`Delete` (no Resolve) and provider `ListConfigs`/`SaveConfig`/`DeleteConfig`/`GenerateText`/`StreamText`/`CancelStream`/`CheckHealth`. Bindings were regenerated with the pinned Wails v2.15.0 CLI; the generated surface contains no `resolve`/generic fetch/arbitrary proxy.
- Frontend: desktop provider/secret client, secure secret field written through the Go binding, secure-mode guard that makes `new Function` script execution unreachable, URL-supplied keys ignored with an explicit notice, text generation routed through the Go gateway (encoded `channel::model` selections decoded before the call and routed to the matching provider), legacy-plaintext-key migration warning, and bilingual (zh-CN/en-US) copy.
- Ordinary config export/import and ZIP backup strip raw keys (removal, not masking) and sanitize legacy imports.
- Static scans: `scripts/security-scan.mjs` runs in both verify scripts and CI; it covers dynamic execution, high-confidence key patterns, and unguarded config serialization, with exact per-file exemptions and stale-exemption detection.

## Commands executed and actual results

| Command | Result |
|---|---|
| `go test ./... -count=1` | **PASS** (16 packages ok; includes live Windows Credential Manager round-trip) |
| `go vet ./...` | **PASS** |
| `gofmt -l .` | **PASS** (no output) |
| `node scripts/security-scan.mjs` | **PASS** (199 files scanned; 1 audited dynamic-execution exception owned by WP-12) |
| `web`: `npm run typecheck` | **PASS** |
| `web`: `npm test` (new) | **PASS** (15/15 security-focused unit tests) |
| `web`: `npm run build` | **PASS** with the pre-existing >500 kB chunk warning |
| `scripts/verify.ps1` | **PASS** (Wails step SKIPped because the CLI is not on PATH; executed separately) |
| `scripts/verify.sh` | **PASS** (same Wails caveat) |
| `wails build -s` (v2.15.0) | **PASS**; produced `build/bin/InfiniteAtelier.exe` |
| `git diff --check` | **PASS** (exit 0; only pre-existing LF/CRLF advisory warnings) |
| `go test -race ./... -count=1` | **ENVIRONMENT FAILURE, not a pass** — the host 32-bit MinGW GCC cannot compile amd64 CGO. Concurrency was reviewed by tracing and targeted probes only. |

## Acceptance

| ID | Result | Evidence |
|---|---|---|
| AC-FOUND-004 (secret) | **PASS (WP-02 scope)** | Native+fake SecretStore tests; schema test rejects value-shaped columns; generated bindings expose no Resolve; `provider_bindings_test.go` proves fail-closed and no leak; ordinary export/backup strip keys (frontend tests assert no key substring). |
| AC-FOUND-005 (text/security subset) | **PASS (text subset only)** | Go adds Authorization (`TestGenerateSendsAuthorizationFromGo`); timeout/429/5xx/bad JSON/truncated stream/cancel tested; allowlist and redirect-SSRF tested; stream cancel now also verified through the cancel-by-stream-ID path. Image/video/audio provider migration is **not** claimed. |
| AC-SEC-001 (SSRF corpus) | **PASS** | Full §6.2 CIDR corpus + IPv4-mapped + metadata; rebinding test asserts a single DNS lookup; redirect re-validation and TLS are structural and tested; the exact-local exception now requires *all* resolved addresses to be private and still blocks metadata. |

## Independent review

- **Spec review (independent subagent): CHANGES REQUIRED → all findings fixed.** Blocker: the production path pinned port 443 while building portless URLs, so every normal provider would have been rejected by its own policy (test seams hid it); fixed by normalizing the effective port, with a regression test that uses the real constructors. Majors fixed: encoded `channel::model` sent as the API model name; secure mode blocking generation on the legacy plaintext-key readiness check; a missing legacy-key migration warning; missing audit cost/unit fields. Minors fixed: empty provider ID accepted, metadata reachable under local approval, stale/too-wide scan exemptions, and missing test coverage for the production client/health/stream paths.
- **Quality review (independent subagent): CHANGES REQUIRED → all findings fixed.** Blocker: model encoding (same as above). Majors fixed: `AllowLocal` could permit cleartext HTTP to a *public* host (now requires all resolved addresses to be private; frontend no longer marks any `http:` URL as local); deltas could be published after the terminal stream event (now gated). Minors fixed: inconsistent error shape between secret and provider bindings, missing diagnostic ID on stream errors, a tautological size-limit test, a rebinding test that could not detect re-resolution, dead/misleading code (`trimTrailingSlash`, `readBounded` return value, `sanitizeLegacyConfig`), duplicate secret-ref namespace, unused `provider.SecretStatus`, and an audit test without read-back (now asserts the persisted columns).

## Known limits and deferred work

- macOS Keychain / Linux Secret Service backends are not implemented; those platforms run providers disabled (fail closed) until a later package.
- Image/video/audio/embedding provider calls, the persistent Job Manager, Agent runtime, Workflow, Memory, Drama domain, and the full backup/restore redesign remain later work packages. Legacy browser media paths still exist and are explicitly out of WP-02 scope; they have **not** been made secure, and no automatic fallback to them exists for text.
- Ordinary export/backup now exclude keys, but an already-written legacy config or backup on disk is not retroactively scrubbed.
- No remote CI run (no push authorized) and no macOS/Linux native evidence; `wails build` ran locally only.
- Real paid providers were never contacted; all provider evidence uses httptest/fakes.

---

# 0. Current WP-01 state

- User approval to begin WP-01: **YES — 2026-09-04**.
- Current phase: **WP-01 COMPLETE — Tasks 1–11 are implemented and approved with the evidence recorded below.**
- Plan: `docs/plans/2026-09-04-wp-01-secure-desktop-foundation.md`.
- Task 1–9 approval provenance and Task 9 production-native route proof remain in `docs/implementation/handoff-2026-09-08-153906+0800.md` and `docs/implementation/wp01-execution-2026-09-07.md`; the historical Task 9 binary fingerprint was `a52e7add9ea7959ee948466998400cffdc5ecdfe53b8d9c94c5c3e897519d1d5`.
- Task 10 (2026-09-08): `scripts/verify.ps1` and `scripts/verify.sh` now run root `go test ./... -count=1` and `go vet ./...`, invoke `wails build` only when the CLI is available, preserve nonzero executed-gate failures, and restore `web/dist/.gitkeep` after either success or failure. The existing local Wails v2.15.0 CLI was used to prove both script paths. CI retains the Ubuntu web job and adds a Windows Node 22 / Go 1.25 / Wails v2.15.0 build job; remote GitHub Actions was not run because no push was authorized. README now distinguishes loopback browser mode from desktop mode and documents the deferred Secret/Provider/legacy migration work. Independent spec review found a missing placeholder, which was corrected and re-reviewed **APPROVED**; independent quality review **APPROVED**.
- Task 11 (2026-09-08): final `go test ./... -count=1`, `go vet ./...`, both verification scripts, POSIX syntax check, and Wails v2.15.0 production builds passed. The final isolated native smoke used owned PID `5944` and redirected `APPDATA`, `LOCALAPPDATA`, `USERPROFILE`, `HOMEDRIVE`, `HOMEPATH`, and `WEBVIEW2_USER_DATA_FOLDER` under a fresh system-temp root. It created only the owned app DB/WebView/log paths, opened a native window, closed gracefully, retained schema migration `[(1,)]`, reported WAL from a post-close read-only connection, and produced a 130-byte log with no non-printing match for authorization/bearer/api-key/SQL indicator patterns. The owned root was removed after inspection. The app enables foreign keys per managed connection; the post-close Python connection reporting `foreign_keys=0` is SQLite's default for a new unrelated connection and is not an application-contract failure.
- `go test -race ./... -count=1` remains an **environment failure**, never a pass: the host 32-bit MinGW GCC cannot compile amd64 CGO (`cc1.exe: sorry, unimplemented: 64-bit mode not compiled in`).
- AC-FOUND-001, AC-FOUND-002, and AC-FOUND-003 are **PASS for the WP-01 Windows foundation scope**. Native packaging was proved on Windows only; macOS/Linux native packaging, remote CI execution, frontend automated tests/lint, Secret/Provider migration, and legacy browser-data migration remain future work.
- Active constraints: WP-01 is closed; preserve all WP-00/user changes; no commit/push/stash/reset/clean; no real Provider calls. WP-02 was implemented and closed on 2026-09-15 (section 0b); WP-03 was implemented and closed on 2026-09-15 (section 0c); WP-04 has not started.

---

# 1. WP-00 result

- Status: **COMPLETE**
- Branch/commit: `main` / `a243891455ec17687dd54b5ac90d3bd64478a1a1`, tracking `origin/main`
- Product code changed: **no**
- User/specification changes preserved: **yes**
- Recommended next work package: **WP-01 — Wails/Go Core/SQLite/FileStore foundation**
- User approval required before WP-01: **YES**

WP-00 established an evidence-backed baseline, repository audit, requirements traceability, desktop-framework decision, proposed SQLite decision process, and executable verification entry points. It did not introduce Wails, Go product code, SQLite, migrations, Provider changes, canvas refactoring, data migration, Drama UI, or WP-01 implementation.

---

# 2. Deliverables

| Deliverable | Status | Purpose |
|---|---|---|
| `docs/implementation/BASELINE.md` | COMPLETE | Host/tool versions, Git baseline, install/typecheck/build/start results, skips and reproducibility failure. |
| `docs/implementation/REPO_AUDIT.md` | COMPLETE | Routes, nodes, canvas behavior, stores, Provider/Secret paths, backups, MONOFORM, CI/tests/licenses and risk inventory. |
| `docs/implementation/TRACEABILITY.md` | COMPLETE | PRD/NFR/security/acceptance mapping to current modules, gaps, and future work packages. |
| `docs/adr/0001-desktop-framework.md` | ACCEPTED | Wails v2 desktop decision and WP-01 verification contract. |
| `docs/adr/0002-sqlite-driver-and-migrations.md` | PROPOSED | Driver/migration options and the evidence required before acceptance. |
| `scripts/verify.ps1` | COMPLETE / PASS | Windows verification for real current gates with explicit SKIP reasons. |
| `scripts/verify.sh` | COMPLETE / PASS | POSIX/Git-Bash verification for real current gates with explicit SKIP reasons. |

---

# 3. WP-00 baseline repository facts (historical snapshot before WP-01 implementation)

- At the WP-00 baseline, the product was React 19/Vite 7/TypeScript under `web/`; there was no Go module, Wails project, SQLite schema, native binding, or FileStore. WP-01 implementation has since added those foundations; see section 0 and current execution evidence.
- npm is the repository package manager because the main app and separate MONOFORM app use npm lockfiles v3.
- The route table contains `/`, `/assets`, `/canvas`, `/canvas/:id`, `/director`, `/config`, and `*`.
- Built-in canvas node types are image, text, config, video, audio, group, and director; plugin types are open strings.
- Browser persistence is split across Zustand, localStorage, localForage/IndexedDB Blob stores, and in-memory generation Maps.
- Raw API keys and model scripts are stored in frontend configuration; backups/config exports include the full configuration.
- Provider requests are made in the browser; development may route through an arbitrary-target Vite proxy; production uses direct URLs.
- `services/api/model-plugin.ts:120` executes user-authored JavaScript using `new Function` and passes secrets/network helpers.
- Video polling/cancellation is in memory and has no durable Job/restart recovery.
- MONOFORM is a separate Vite package, embedded by iframe from tracked prebuilt output; it uses wildcard `postMessage` without origin/source validation.
- The main canvas page is 3,051 lines/approximately 171 KB; MONOFORM `App.jsx` is 2,261 lines/approximately 131 KB.
- No frontend test or lint command exists. Prettier checking exists and currently fails on 147 files.
- Root license is MIT; no third-party notices or SBOM were found. Toonflow is not a code dependency.

---

# 4. Commands executed and actual results

| Command | Result |
|---|---|
| `git status --short --branch` | PASS; `main...origin/main`, only pre-existing untracked specification-pack files. |
| `npm ci --legacy-peer-deps` in `web` | **FAIL**; main package/lock mismatch with missing optional platform packages. |
| `npm install --legacy-peer-deps --include=optional --package-lock=false --no-audit --no-fund` in `web` | PASS; changed 728 installed packages, lockfile unchanged. |
| `npm run typecheck` in `web` | PASS. |
| `npm run format:check` in `web` | **FAIL**; 147 existing files reported. No rewrite performed. |
| `npm run build` in `web` | PASS with chunk/dynamic-import warnings; 7,384 modules. |
| `npm ci --no-audit --no-fund` in `web/monoform-studio` | PASS after host-cache permission retry; 92 packages. |
| `npm run build` in `web/monoform-studio` | PASS with large-chunk warning; 2,407 modules. |
| loopback Vite smoke + HTTP `/` and `/monoform/index.html` | PASS; both HTTP 200. |
| `& ./scripts/verify.ps1` | PASS; typecheck/build/MONOFORM build pass; test/lint/Go explicitly skipped. |
| `C:\Program Files\Git\bin\bash.exe -n scripts/verify.sh` | PASS. |
| `C:\Program Files\Git\bin\bash.exe scripts/verify.sh` | PASS after adding Windows `npm.cmd` selection. |
| frontend tests | SKIP; no test script/files. |
| Go tests / Wails build | SKIP; no Go/Wails implementation in WP-00. |

---

# 5. WP-00 acceptance

| Item | Status | Evidence |
|---|---|---|
| 规格文件阅读 | PASS | Required files read in the mandated order before edits. |
| Git 状态记录 | PASS | `BASELINE.md` section 2. |
| 环境版本记录 | PASS | `BASELINE.md` section 3. |
| 依赖安装基线 | PASS WITH KNOWN FAILURE | Clean main `npm ci` failure and fallback are both recorded truthfully. |
| typecheck | PASS | `tsc --noEmit`, exit 0. |
| build | PASS | Main and MONOFORM source builds, with exact warnings/sizes recorded. |
| tests | SKIP / GAP RECORDED | No existing test command/files; verification scripts state the reason. |
| 路由/节点/Store 审计 | PASS | `REPO_AUDIT.md` sections 2–4. |
| Provider/Secret/脚本审计 | PASS | `REPO_AUDIT.md` section 5. |
| 备份/数据格式审计 | PASS | `REPO_AUDIT.md` section 6. |
| MONOFORM 审计 | PASS | `REPO_AUDIT.md` section 7 plus source build/smoke evidence. |
| License/Notices 审计 | PASS | `REPO_AUDIT.md` section 9. |
| verify scripts | PASS | Both scripts executed/syntax-checked on the host. |
| ADR-0001 | PASS / ACCEPTED | Desktop decision recorded. |
| ADR-0002 | PASS / PROPOSED | Options and acceptance evidence recorded; implementation deliberately deferred. |
| AC-BASE-001 | **PASS** | Baseline has real commands/versions/results; product code unchanged; failures are not disguised. |
| AC-BASE-002 | **PASS** | Required functional inventory is complete in `REPO_AUDIT.md`. |

---

# 6. Unresolved failures and risks

1. Main `npm ci --legacy-peer-deps` is not reproducible from the checked-in lockfile. The lockfile needs a narrowly scoped repair and CI proof; WP-00 did not rewrite it.
2. Raw Provider secrets, arbitrary JavaScript execution, direct browser Provider requests, the arbitrary-target proxy, secret-bearing backups, and wildcard iframe messaging violate the target security boundary.
3. Browser persistence remains the current source of truth and has no transactional migration/read-back safety.
4. No automated regression/security/accessibility test suite protects existing canvas behavior.
5. Prettier gate fails on 147 existing files; broad reformatting would be an unrelated WP-00 diff.
6. Main and MONOFORM bundles exceed the 500 kB chunk warning threshold.
7. ADR-0002 remains Proposed until the real Wails/SQLite cross-platform spike is complete.
8. The Go CLI printed a host permission error for its telemetry upload token after reporting its version; there is no Go project to test yet.

---

# 7. Git and data safety

- Existing user changes preserved: **yes**.
- Tracked product-code modifications: **none**.
- Automatic commit/push/stash/reset/clean: **none**.
- Lockfiles changed: **no**.
- User databases, browser profiles, secrets, and media changed: **no**.
- Real Provider calls: **none**.
- Secrets found or introduced by WP-00: **no actual secret value found; none introduced**. The insecure secret-handling code path is documented.
- Migrations/backups executed: **none**.
- Generated build output and installed dependencies stayed in ignored `dist`/`node_modules` locations.

---

# 8. Historical next-step record

The prior WP-00 recommendation to begin WP-01 was completed, and the WP-02 and WP-03 recommendations were executed on 2026-09-15 (sections 0b and 0c). The authoritative current state is: WP-01 COMPLETE (Windows foundation scope); WP-02 COMPLETE (its recorded scope); WP-03 COMPLETE (its recorded scope, with the gaps listed in section 0c); WP-04 has not started and requires separate user authorization.

---

# 9. WP-02 Git and data safety (2026-09-15)

- Existing user changes preserved: **yes** (brand-rename files in `main.go`/`wails.json`/i18n and all WP-01 tracked/untracked work were left intact).
- Automatic commit/push/stash/reset/clean: **none**.
- User databases, browser profiles, secrets, and media changed: **no**. The application data directory held no `app.db` before or after this session.
- Real Provider calls: **none**.
- Migrations executed against user data: **none**; `000002` was applied only to temporary test databases, and a pre-migration snapshot test covers the v1→v2 upgrade path.
- Credential Manager: only uniquely-prefixed `InfiniteAtelier:test:<test name>:<pid>:*` entries were created and deleted by the Windows store tests; no existing user credential was read, enumerated, or removed.
- Secrets found or introduced: **none**; the new high-confidence secret scan passes over 199 files with exact, stale-checked exemptions.
- Dependencies added: **none** (the Credential Manager adapter uses the existing `golang.org/x/sys/windows` indirect dependency).
- Incidental environment note: a user-owned Vite dev server was running on port 3000 and held file locks on `web/node_modules`. A required dependency repair (`npm install`, no lockfile change) and the WP-02 builds were completed without stopping that user process; `web/package.json` was restored to its session-start content plus the new `test` script after `npm install` rewrote it.
