# Requirements Traceability

> Updated: 2026-09-15
> Meanings: **Existing** = verified current behavior; **Partial** = related behavior exists but does not satisfy the target contract; **Gap** = no implementation found. WP-01 evidence covers the desktop foundation; WP-02 evidence covers the Secret/Provider-Gateway foundation (section 6). Future package allocation does not imply that work has started.

## 1. Product requirement groups

| Requirement | Target capability | Current evidence | State | Planned package / acceptance |
|---|---|---|---|---|
| FR-001 | Existing Infinite Atelier compatibility | React/Vite app, routes, seven node types, canvas tools, Provider UI, assets and MONOFORM exist. No regression tests. | Partial | WP-04; AC-COMPAT group |
| FR-010 | Desktop runtime and Go Core | Wails v2.15.0 embeds the React build; Windows production build and isolated native route/lifecycle smoke are recorded; Health/Secrets/Providers bindings are the current narrow frontend-to-Go paths. | Partial | WP-01 foundation complete; later packages migrate production operations; AC-FOUND-001 PASS (Windows). |
| FR-020 | Project, source, and chapter import | SQLite foundation is authoritative only for migrations and FileStore metadata; existing browser project/source data is not migrated. | Partial | WP-04/WP-06; AC-FOUND-002 PASS only for foundation DB, not project import. |
| FR-030 | Chapter event graph | No StoryEntity/StoryEvent/StoryRelation tables, extraction, or graph UI. | Gap | WP-05/WP-06; consumed read-only by WP-08/09/10. |
| FR-160 | Managed local FileStore and lineage | Go content-addressed FileStore provides temporary write, SHA-256, MIME detection, atomic commit, deduplication, reference foundation, traversal rejection, and missing-file diagnostics; browser Blob stores remain for legacy data. | Partial | WP-01 foundation complete; WP-03/WP-04 add production lineage and legacy migration; AC-FOUND-003 PASS. |
| FR-040 | Script pipeline | Canvas text/chat is not an immutable screenplay aggregate. | Gap | WP-08; AC-SCRIPT group |
| FR-050 | Production assets | Image/video/audio nodes exist but no production-domain asset/version lifecycle. | Partial | WP-09; AC-ASSET group |
| FR-060 | Director planning and MONOFORM | MONOFORM previs exists, but its bridge is untyped and unrelated to durable Shots. | Partial | WP-09; AC-BOARD group |
| FR-070 | Storyboard table, panels, and images | Generic canvas/media nodes lack structured storyboard entities and bidirectional projection. | Gap | WP-09; AC-BOARD group |
| FR-080 | Video, audio, subtitles, and export | The async video contract (submit/poll/fetch/cancel), `remote_only` semantics, restart-resume-by-poll, and duplicate-response handling are implemented and tested against deterministic mocks; real video/audio adapters, TTS, subtitles, and export are not implemented. | Partial | WP-03 contracts; WP-11 real media and export. |
| FR-090 | Agent center | No registered Decision/Execution/Supervision inventory or Agent run UI. | Gap | WP-07; AC-AGENT group |
| FR-100 | Durable Workflow engine | No workflow tables, state machine, or checkpoints. | Gap | WP-07; AC-WORKFLOW group |
| FR-110 | Supervisor and quality center | No supervisor read model, findings, or quality gates. | Gap | WP-07/WP-10; AC-QUALITY group |
| FR-120 | Persistent Memory | Prompt library/chat/history are not scoped Agent Memory or evidence-based recall. | Gap | WP-10; AC-MEM group |
| FR-130 | Semantic canvas | Existing nodes/connections lack domain references and semantic relation validation. | Gap | WP-04/WP-05; AC-CANVAS group |
| FR-140 | Provider gateway and model policy | Go gateway now covers text **and image**: OS-backed SecretStore (Windows), DB stores references only, controlled SSRF-guarded HTTP client, OpenAI- and Gemini-compatible image adapters, a separate download policy for provider-supplied result URLs, audited calls (capability + job linkage), narrow bindings. Video/audio are contracts with deterministic mocks reachable only through a code-registered kind; embedding, the declarative Manifest, and per-project model policy are not implemented; legacy browser video/audio paths still carry raw keys and are disclosed to the user. | Partial | WP-02/WP-03 foundation complete; WP-07–11 policy; WP-11 real media adapters; WP-12 removal of legacy paths. |
| FR-150 | Persistent Job Manager | Durable job tables, state machine, priority scheduler, worker pool with lease + revision guards, exponential backoff, cancel (including best-effort remote cancel with an orphan candidate), retry-failed-only, dependency gate, startup recovery scanner, and a Job Center UI. Thumbnail/import/export/migration job types and provider concurrency limits per provider are not implemented; asset versions arrive with WP-05. | Partial | WP-03 complete for its AC; later packages add the remaining job types. |
| FR-170 | Backup, restore, and migration | Ordinary config export/import and ZIP backup now strip raw keys (removal, not masking) and sanitize legacy imports; full backup restore validation/atomicity is still absent. | Partial/unsafe | WP-02 began secret exclusion; WP-04/WP-12 complete the format. |
| FR-180 | Settings, logs, diagnostics, and privacy | WP-01 has managed directories and redacted structured-log foundation; WP-02 adds per-call redacted provider audit (category, latency, request ID, units) and diagnostic IDs on stream errors; user settings, diagnostics export, privacy controls, and release hardening remain absent. | Partial | WP-02/WP-12; AC-RELEASE/AC-SEC |

## 2. Non-functional requirements

| Requirement | Current state | Evidence / gap | Planned verification |
|---|---|---|---|
| NFR-001 Performance | Partial | Viewport node culling exists; all edges render; builds warn at 2.54 MB and 1.28 MB chunks; no WP-01 benchmark claim. | WP-04/WP-12; benchmark and large-project tests |
| NFR-002 Reliability | Partial | WP-01 adds transactional SQLite foundation, WAL, migration safety, snapshots, and FileStore atomicity; WP-03 adds durable jobs with lease/revision guards, deterministic backoff, and a startup recovery scanner (including resume-without-resubmit). Workflows remain absent; no process-level crash test exists (recovery is exercised by state injection). | WP-07; workflow recovery tests |
| NFR-003 Security | Fail overall | WP-01 FileStore/logging boundaries pass their focused tests, but raw frontend keys, secret-bearing backups, `new Function`, arbitrary proxy, and wildcard iframe messaging remain. | WP-02/WP-12; negative security suite |
| NFR-004 Maintainability | Partial | Go layered foundation, typed health binding, tests, verification scripts, CI Windows build definition, and ADR evidence exist; major frontend components and broader architecture checks remain. | All packages; architectural tests/ADR |
| NFR-005 Testing | Partial | WP-01 Go unit/integration tests and Windows local build evidence pass; race is host-environment blocked and frontend test/lint scripts do not exist. | WP-02/WP-12; test matrix |
| NFR-006 Accessibility/i18n | Partial | i18next/bilingual health status exists; no automated accessibility suite or complete target UI. | UI packages/WP-12; keyboard/ARIA tests |

## 3. Security requirements

| Security contract | Current status | Evidence | Owner package |
|---|---|---|---|
| Secrets never enter frontend/Agent/log/ordinary backup | **Partial — secure path PASS, legacy store not yet scrubbed** | New provider secrets live only in the OS credential store; bindings expose status/set/delete with no resolve; ordinary export/backup strip keys; scans cover key patterns and config serialization. **Existing installs still hold legacy plaintext keys** in the persisted browser config until the user re-enters them (a migration warning is shown). | WP-02 (secure path), WP-12 (legacy removal) |
| No arbitrary model script execution | **Partial** | `runModelPlugin` fails closed in secure mode; one audited `new Function` remains, unreachable in secure mode, owned by WP-12 for removal. | WP-02 (guard), WP-12 (removal) |
| Provider traffic only through controlled Go client | **Partial — text and image** | Text, image generation/editing, and health go through the Go gateway. Video and audio still call providers from the webview on the legacy path; the user is notified at startup, and the scanner refuses NEW direct calls. | WP-02/WP-03 (text+image), WP-11 (media), WP-12 (legacy removal) |
| URL/SSRF/TLS/redirect/header/body controls | **PASS for the Go gateway** | Full SECURITY §6.2 CIDR corpus, IPv4-mapped forms, metadata endpoints, DNS-pinning (single-lookup assertion), per-hop redirect re-validation, no environment proxy, TLS verification not disableable, bounded body and layered timeouts. | WP-02 |
| Agent tools are allowlisted and schema-validated | Gap | No Agent runtime. | WP-07 |
| Supervisor is read-only | Gap | No Supervisor. | WP-07/WP-10 |
| File access only through safe FileStore | Partial | New Go FileStore validates managed content-addressed files; legacy browser Blob access remains until WP-04 migration. | WP-01 complete; WP-04 |
| Import/ZIP is bounded and fail-closed | Fail | Synchronous whole-ZIP parsing, shallow checks, no size/hash/atomic contract. | WP-12 |
| Backups exclude secrets and are consistent | **Partial — key exclusion PASS, format hardening pending** | Ordinary export/import and ZIP backup strip keys (removal, not masking) and legacy imports are sanitized; DB consistency/manifest/checksum work remains. | WP-02 (key exclusion), WP-04/WP-12 (format) |
| Auditable errors/actions without chain-of-thought | Partial | `apperror` diagnostic IDs plus redacted provider audit records (category, latency, request ID, units, no headers); full audit/UI surfacing lands with later packages. | WP-02/WP-07 |

## 4. Acceptance traceability

| Acceptance family | WP-00 state | Evidence / next package |
|---|---|---|
| AC-BASE-001 Repository reproducibility | **PASS for WP-00 audit contract** | `BASELINE.md` records clean-lock failure, fallback, tool versions, typecheck/build/start, skips, and preserves product code. The underlying lock defect remains open. |
| AC-BASE-002 Functional inventory | **PASS** | `REPO_AUDIT.md` records routes, node types, persistence, Provider paths, backup, MONOFORM, dynamic scripts, proxy, and artifacts. |
| AC-FOUND-001 Wails/Go Core | **PASS (Windows foundation)** | Wails v2.15.0 production build, embedded React, Health Binding, no external Vite requirement, owned native window and graceful exit; remote CI and non-Windows packaging remain unrun. |
| AC-FOUND-002 SQLite | **PASS (foundation)** | Empty DB, ordered migration/checksum/safe-mode/snapshot tests; isolated production DB read-back reports schema version 1 and WAL. Foreign keys are verified by managed-connection contract tests. |
| AC-FOUND-003 FileStore | **PASS (foundation)** | Go tests cover temporary writes, SHA-256, MIME/magic, atomic commit, deduplication, traversal rejection, and diagnosable missing files. |

| AC-FOUND-004 Secret | **PASS (WP-02 scope)** | Native Windows Credential Manager + fail-closed non-Windows + test fake; schema test rejects value-shaped columns; bindings expose no resolve; ordinary export/backup strip keys. Legacy browser keys are warned about, not yet scrubbed. |
| AC-FOUND-005 Provider (text subset) | **PASS (text only)** | Authorization added by Go; timeout/429/5xx/invalid JSON/stream-cancel/URL-allowlist/redirect-SSRF tested through the adapter and the real production client. Image/video/audio are not claimed. |
| AC-SEC-001 SSRF | **PASS** | Full §6.2 corpus incl. IPv4-mapped and metadata; exact-local exception requires all resolved addresses to be private; single-DNS-lookup rebinding assertion; per-hop redirect re-validation; TLS cannot be disabled. |

| AC-FOUND-006 Job | **PASS (WP-03 scope)** | Durable queue, state machine, retry_wait, cancel (queued/in-flight/during-failure/during-poll/during-shutdown), restart recovery including a crash inside the lease window (when the startup scan must not touch the job), paced remote polling that is not charged to the attempt budget, remote-ID poll without re-submit, duplicate idempotency, and a verified result commit against the real FileStore + repositories. All settlement writes run on a bounded settling context, so a shutdown persists scheduler state before exit (ARCHITECTURE §6.2). |
| AC-MEDIA-001 Async video (mock) | **PASS 8/9 for the mock contract** | submit/remote ID/poll/restart/fetch/validate/duplicate response/cancel are tested. "asset version" has no `AssetVersion` entity in this package (WP-05); the pipeline records job-owned file references instead — an explicit gap, not a pass. |
| AC-LEGACY-001 legacy import completeness | **PASS (WP-04)** | The importer preserves project, node and edge counts, text, the viewport and the media, and retains every field the schema does not model in `legacy_metadata_json` with a warning. Fixtures live in `testdata/old-projects/` (declared in `testdata/LICENSES.md`); the evidence is `TestImportAllNodeTypesIsComplete`, `TestLegacyImportSnapshotIsAtomic`, `TestImportMissingMediaIsReportedNotFatal`, `TestImportMalformedMetadataIsRetained`. The old browser data is never written to. |
| AC-LEGACY-002 legacy import idempotency | **PASS (WP-04)** | `TestImportIsIdempotent` and `TestLegacyProjectFingerprintIsPerProject` (against the real store): a second import detects the project, stores no media twice, and writes nothing; copy mode imports again with fresh identifiers; there is no overwrite mode. |
| AC-LEGACY-003 legacy import rollback | **PASS (WP-04)** | One transaction covers every table and its verification re-reads what it wrote, so a failure leaves nothing: `TestLegacyImportRollsBackCompletely`, `TestLegacyImportVerificationCoversChatSessions`, `TestLegacyProjectFingerprintSurvivesFailure`, `TestImportFailureLeavesNoProject` (the failing stage is named). An abandoned upload removes its temporary file. |
| AC-CANVAS-004 free canvas regression | **PASS (WP-04)** | `web/e2e/canvas-regression.spec.ts` (Playwright, run by both verify scripts): add, move, delete, multi-select, box select, zoom, pan, undo/redo, connections, the minimap, the generation prompt surface, the image node actions and a reload. The move, pan, minimap and image-tools tests were rebuilt after a review showed they could pass while the behaviour was broken. |
| AC-DRAMA / AC-STORY | Not started | WP-05/WP-06. |
| AC-AGENT / AC-WORKFLOW | Not started | WP-07. |
| AC-SCRIPT | Not started | WP-08. |
| AC-PROD / AC-STORYBOARD | Not started; MONOFORM bridge partial | WP-09. |
| AC-MEM / AC-QUALITY | Not started | WP-10. |
| AC-MEDIA / AC-EXPORT | Not started; legacy generation partial | WP-11. |
| AC-BACKUP-001 ordinary backup | **PASS (WP-04)** | A Go-authored archive with a manifest, a `VACUUM INTO` database snapshot, the referenced objects and per-entry checksums; the restore validates the manifest, the checksums, the staged database's integrity and each object's hash before staging anything. An ordinary backup cannot contain a credential (the database holds references only), and the reader scans every entry for credential shapes including `Authorization` and `Cookie`. Evidence: `TestBackupRoundTrip`, `TestBackupExportAgainstRealStorage`, `TestRestoreRefusesTamperedArchive`, `TestRestoreRefusesCredentialHiddenInAnObject`, `TestBackupScanCoversAuthorizationAndCookie`. |
| AC-BACKUP-002 restore atomicity | **PARTIAL (WP-04)** | Tamper detection, staged validation and staging cleanup are implemented and tested; the atomic swap of a live database and the user-confirmation step belong to WP-12. |
| AC-SEC-002 archive limits | **PASS (WP-04)** | The SECURITY §18 corpus: traversal, absolute paths, drive letters, UNC, device names, trailing dot and space, dot elements, symlinks, duplicate normalised paths, entry-count and compression bombs, size ceilings, corrupt input, a false manifest and a wrong hash (`internal/infrastructure/archive/archive_test.go`). |

## 5. Contract-to-module map

| Boundary | Current module(s) to adapt, not copy as authority | Guardrail |
|---|---|---|
| Desktop bindings | `web/src/services/desktop/*` (health, providers, text) | React calls typed application ports only; generated bindings are never hand-edited. |
| Project repository | `use-canvas-store.ts` current schema | Preserve projects and unknown nodes; add revision/read-back semantics. |
| FileStore | `file-storage.ts`, `image-storage.ts` | Preserve generated-key behavior; add staging/hash/atomic commit/path defenses. |
| Legacy importer | canvas store, asset store, generation history, prompt library, exports/backups | Synthetic fixtures first; never move/delete legacy data before verified read-back and backup. |
| SecretStore | **Implemented (WP-02)**: `internal/application/secrets`, `internal/infrastructure/secretstore`, `desktop.SecretsBinding`, `web/src/components/layout/secure-secret-field.tsx` | Value only crosses the binding on write; reads are Go-internal; legacy keys still need user-driven migration. |
| Provider gateway | **Implemented for text (WP-02) and image (WP-03)**: `internal/infrastructure/providerhttp` (API policy + download policy), `internal/infrastructure/providers` (text, OpenAI image, Gemini image, media contracts), `desktop.ProvidersBinding`, `web/src/services/desktop/{text,image-jobs}.ts` | Fail closed; bounded client, URL/IP policy, redaction, cancellation. Legacy video/audio calls remain for WP-11/12. |
| Job manager | **Implemented (WP-03)**: `internal/domain/job`, `internal/application/jobs`, `internal/infrastructure/jobs`, `internal/infrastructure/database/jobs.go`, `desktop.JobsBinding`, `web/src/pages/jobs` | The runner performs no database writes: phase changes travel through the worker's Outcome so the revision guard stays authoritative. Job state is the source of truth; the Job Center is a projection. |
| Canvas projection | `project.tsx`, node registry, connection types | DB is truth; Zustand remains ephemeral UI state; preserve current behavior through tests. |

## 6. Open decisions and evidence required

- ADR-0002 remains Proposed: WP-01 proved the selected driver, migration/snapshot behavior, and Windows Wails build; accepted status still needs supported-platform race evidence, measured performance, and native macOS/Linux packaging evidence. The host race command is an environment failure from its 32-bit MinGW GCC, not a pass.
- The repaired npm lock passes local `npm ci --legacy-peer-deps` paths through Wails/verify; remote GitHub Actions has not executed because no push was authorized.
- Existing browser storage must be sampled into sanitized, deterministic fixtures before importer implementation.
- The exact MONOFORM host protocol needs a versioned schema, origin/source validation strategy, and compatibility test before the bridge changes.
- **WP-02 (new):** ADR-0003 accepted the Windows Credential Manager backend. macOS/Linux secret backends remain unspecified; those platforms currently disable external providers (fail closed). A future package must add Keychain/Secret Service support or an approved encrypted vault before cross-platform provider use.
- **WP-02 (new):** the persisted legacy `AiConfig.apiKey` in browser storage is detected and warned about but not removed, because removing it without a verified migration would lose user data. The user-driven migration path (re-enter through the secure field, then clear) is documented in the UI; bulk scrub/migration belongs to WP-12.
