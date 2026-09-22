# Release-blocker audit — WP-12 (2026-09-23)

PRD section 18 blocks release on eleven conditions. This document records each one checked against
the running code, tests and commands on this host — not against a comment. A comment saying
"unreachable" is a claim; where a claim was load-bearing, it was tested instead of read.

Verdicts: **CLEARED** / **NOT CLEARED** / **PARTIAL**. Evidence is a test name, a `file:line`, or a
command with its real output.

Environment note. `go test -race` **fails on this host** (`cc1.exe: sorry, unimplemented: 64-bit
mode not compiled in`) and is reported as an environment failure, never as a pass. The Go toolchain
is `go1.25.13` after this package; the mandated command prefix `GOTOOLCHAIN=go1.25.0 …` pins the OLD
toolchain and therefore does not honour `go.mod`'s `toolchain` directive — see the row for blocker 11
and the vulnerability section for why that distinction matters.

---

## PRD section 18 — the eleven

### 1. 前端或普通备份可获取完整 API Key — **CLEARED**

- **The generated binding surface exposes no way to read a secret.** `web/src/wailsjs/go/desktop/SecretsBinding.d.ts`
  declares exactly `Delete`, `Set` and `Status`. `grep -c "Resolve" SecretsBinding.d.ts` returns `0`.
  There is no getter, so a compromised frontend cannot ask the Go core for a key.
- The provider path adds `Authorization` on the Go side only. WP-02's `TestGenerateSendsAuthorizationFromGo`
  and `provider_bindings_test.go` cover this; the binding regeneration produced no generic fetch or
  arbitrary proxy method.
- **Ordinary backup output is scanned, not merely assumed clean.** `TestBackupContainsNoSecrets`
  (`internal/application/backup/service_test.go:216`) searches the produced archive for a planted key
  and, per its own comment at line 232, proves the scan is **not vacuous** by confirming the same
  search *finds* a planted key. A test that can only pass is not evidence; this one is checked for
  discriminating power.
- Frontend-side: `stripSecretsFromConfig removes root and channel keys` and
  `stripSecretsFromConfig output contains no secret substring` (both in the 67-test `npm test` run).
- **Not claimed:** macOS/Linux secret stores are unimplemented and those platforms run providers
  disabled (fail closed). An already-written legacy plaintext config on disk is warned about, not
  retroactively scrubbed.

### 2. 仍存在任意模型 JavaScript 执行路径 — **CLEARED**

Verified rather than trusted, because commit `3bcd7b4` changed this today.

- `web/src/services/api/model-plugin.ts` (393 lines, the only `new Function`) is **deleted** — confirmed
  by `git show 3bcd7b4 --stat` and by `ls web/src/services/api/` returning only `audio.ts image.ts video.ts`.
  Its editor `model-script-editor.tsx` (113 lines) and the zero-importer `api/request.ts` went with it.
- A repo-wide grep for `new Function` / `eval(` across `web/src/` and `scripts/` returns **only comments**
  (in `channel-editor-drawer.tsx:17`, `video.ts:58`, `audio.ts:27`, `use-config-store.ts:168`) — the
  historical explanations, not code.
- **The scanner no longer carries an exception for it**, which is the part that matters for
  reintroduction: `scripts/security-scan.mjs`'s `DYNAMIC_ALLOWLIST` contains exactly one entry, for
  `internal/infrastructure/media/ffmpeg.go` under owner `ADR-0015` (the audited `os/exec` adapter).
  The scanner fails on a *stale* allowlist entry (`stale-exception`, line ~234), so removing the file
  without removing an entry is also a failure.
- `node scripts/security-scan.mjs` → `PASS: security scans clean (607 files scanned; 1 audited
  dynamic-execution exception, 3 audited legacy direct-call files with named owners).`
- A stored model `script` field is left **inert** rather than deleted, so an upgrade destroys no user
  configuration; such a model now falls through to the standard OpenAI-compatible path. That is a
  deliberate capability loss, recorded in STATUS section 0m.

### 3. API 代理可访问回环、私网或任意地址 — **CLEARED**

- The forbidden-range list is SECURITY §6.2's CIDRs: `internal/infrastructure/providerhttp/policy.go:31`
  includes `100.64.0.0/10` and `169.254.0.0/16`, plus the well-known metadata endpoints at
  `policy.go:132-134` (AWS/GCP/Azure IMDS `169.254.169.254`, AWS ECS `169.254.170.2`).
- **The corpus runs and passes.** `go test ./internal/infrastructure/providerhttp/ -run 'SSRF|Forbidden|Redirect|Rebind|Local|Private|Metadata|TLS' -v` → 17 tests, all PASS:
  `TestIsForbiddenIPCorpus`, `TestClientRejectsPrivateDNSAnswer`, `TestClientRejectsRebindingSecondLookup`,
  `TestClientEnforcesRedirectHostChange`, `TestClientRejectsRedirectToPrivateViaPublicLookalike`,
  `TestValidateIPsLocalApprovalRequiresAllAddressesPrivate`,
  `TestValidateIPsLocalApprovalStillBlocksMetadata`, `TestDownloadRefusesMetadataEndpoint`, and others.
- Redirect hops are re-validated (`client.go:102` `CheckRedirect`), and `Authorization` is dropped on a
  cross-host redirect (`client.go:109`).
- The `AllowLocal` escape hatch requires **all** resolved addresses to be private and still blocks
  metadata — `policy.go:94-118`, tested. So the opt-in for a local provider cannot be pointed at a
  public host, or at IMDS.

### 4. 工作流运行状态只存在内存 — **CLEARED**

- Workflow state is SQLite tables, not a Go map: `workflow_runs`, `stage_runs`, `review_reports`,
  `review_issues`, `user_gate_decisions`, `workflow_events`
  (`internal/infrastructure/database/migrations/000011_workflow_review.sql:32,58,89,108,133,151`).
- The repository reads and writes them with revision-based updates and a transactional event
  (`internal/infrastructure/database/workflow.go`: `CreateRun`, `UpdateRun`, `CreateStage`,
  `UpdateStage`, each taking `expectedRevision`).
- Restart behaviour is tested at the job layer, which is where a process death actually hurts:
  `TestVideoRestartResumesPollingWithoutResubmitting`, `TestVideoRestartKeepsTheReferencesAJobWasSubmittedWith`,
  `TestRecoverResumesRemoteWithoutResubmitting`, `TestRecoverDownloadingResumesWithoutReCallingProvider`,
  `TestRecoverSkipsLiveLease`, `TestRecoverNeverTouchesTerminalJobs`.
- A grep for in-memory-only workflow state (`map[string]*Stage` style) in `internal/application/workflow/`
  and `stagepipeline/` returns nothing.

### 5. 视频结果未验证即标记成功 — **CLEARED**

- Validation runs **before** a job can be committed, and it validates the **bytes**, not the provider's
  claim. `internal/infrastructure/jobs/results.go`'s doc comment states the order and the code follows
  it: write bytes through the content-addressed store → check the **sniffed** content type against a
  capability allowlist → record object metadata → record the owner reference. "A failure at any step
  leaves either no object or an unreferenced object, never a reference to bytes that were not verified."
- The allowlist is per capability and the sniffer is `http.DetectContentType`, never the provider
  header: `AllowedMIME` returns `video/mp4`, `video/webm` for video; `audio/wave`, `audio/mpeg`,
  `application/ogg` for audio. Its comment names a real bug the rule had (only the IANA `audio/wav`
  spelling was accepted, so every real WAV was rejected).
- The video job calls it: `internal/infrastructure/jobs/runner.go:369`
  `r.results.CommitInline(ctx, record.ID, "video", mimeType, media.Data)`.
- Tests, all PASS: `TestRunnerVideoFullAsyncFlow`, `TestRunnerVideoRemoteOnlyOutcome`,
  `TestRunnerVideoProviderFailureIsTerminal`, `TestCommitRejectsDisallowedContentType`,
  `TestMIMEAllowlistPerCapability`, `TestAllowedMIMECoversSnifferNames` (which pins the allowlist
  against the sniffer **in both directions**, so an entry the sniffer can never emit is a failing dead
  rule), `TestDownloadAndCommitNeverCommitsFailedDownload`, `TestDownloadAndCommitRejectsDisallowedType`,
  `TestCommitFailsClosedWithoutStore`.
- Export playability is separately read back with `ffprobe` in WP-11's suites (`AC-MEDIA-003`).
- **Scope limit, stated:** the deterministic checks catch a wrong container/type. They do not decode
  the video, so "the file is a valid MP4" is asserted and "the video shows what the prompt asked for"
  is the Final Ruleset supervisor's job, not this pipeline's.

### 6. 数据库迁移无备份或回滚/修复路径 — **CLEARED**

- **A pre-migration snapshot is taken before any schema change on a non-empty database.**
  `internal/infrastructure/database/database.go:52-62`: if migrations are pending, it reads
  `PRAGMA user_version` and, when `userVersion > 0`, calls `vacuumSnapshot` **before** `applyMigrations`.
  A fresh database is deliberately not snapshotted (`userVersion == 0`), which is not a data-loss case.
- **Three independent failure paths exist, and each is tested:**
  - snapshot failure → `DATABASE_SNAPSHOT_FAILED`, safe mode (`TestOpenSnapshotFailureReturnsSafeMode`);
  - integrity failure → `DATABASE_INTEGRITY_FAILED`, safe mode (`TestOpenIntegrityFailureReturnsSafeMode`,
    `database.go:46`), so a corrupt database cannot be written to;
  - migration failure → `DATABASE_MIGRATION_FAILED`, safe mode, **with the snapshot path retained** on
    the handle (`Handle.SnapshotPath()`), which is the repair route.
- Per-migration rollback is transactional and tested: `TestMigrationBadSecondRollsBackTransaction`,
  `TestWP02MigrationFailureRollsBack`, `TestWP03MigrationFailureRollsBack`,
  `TestWP04MigrationRollsBackOnFailure`, `TestWP05MigrationsRollBackOnFailure`,
  `TestMigrationChecksumMismatchFailsClosed` (a modified published migration is refused, not applied).
- Restore is verified against a real database: `TestBackupRestoreVerifiesAgainstRealDatabase`,
  `TestBackupOpenRejectsMangledHash`.
- Quoting is handled: `TestOpenSnapshotsBeforeUpgradeUsingQuotedPath` — a Windows path with a space is a
  real failure mode for `VACUUM INTO`.
- All of the above PASS in `go test ./internal/infrastructure/database/`.

### 7. Supervisor 可使用未授权写工具 — **CLEARED**

- The ACL is a **domain** function, not a comment: `internal/domain/agent/agent.go:198` `ToolAllowed`
  implements AGENT_CONTRACTS §6.3's matrix, and `LayerSupervision` returns `mode == ToolRead` only.
- **It is enforced twice and on the real path**, not merely available:
  - at assembly time — `internal/application/agentruntime/registry.go:60` refuses a spec whose tool
    list asks for a mode its layer may not hold;
  - **on every call** — `internal/application/agentruntime/runner.go:872` calls `Authorizer.Authorize`
    before dispatch, recording `ToolCallDenied` with code `CodeToolNotAllowed`. The comment there is
    explicit that the registry is checked once at startup and the authorizer on every call, so a
    runtime change of spec cannot slip through.
- **The registered data agrees.** All seven supervisor agents in both skill manifests hold only
  `read_*` tools:
  - `script.supervision.{story_skeleton, adaptation_strategy, script}`
  - `production.supervision.{director_plan, storyboard_table, storyboard_panel, final_episode}`
  Cross-checked against the nine registered `agent.ToolWrite` tools
  (`asset.create_*`, `script.create_*`, `storyboard.create_*`): **no intersection**.
- End-to-end proof, not a unit-test proxy: `TestCanaryRefusesAnIllegalToolCall`
  (`internal/infrastructure/database/canary_test.go:595`) drives the whole stack. A supervisor asks
  for a write tool; the ACL refuses it with the named code; the run is recorded failed and the tool
  call is recorded **DENIED** rather than "failed", which is SECURITY §7.1's requirement that a denial
  be attributable to the ACL. `TestAuthorizeEnforcesTheMatrixAndTheGrant`,
  `TestAuthorizeRefusesAnUnregisteredToolOrLayer` and `TestToolMatrixIsSection63` PASS.

### 8. 导入可路径穿越或 Zip Bomb — **CLEARED**

- Path handling refuses `..` as an element **after** Unicode normalisation, so a lookalike cannot
  compose one: `internal/infrastructure/archive/reader.go:263-325`, with
  `CodePathTraversal`. Entries are also required to have no leading separator, no empty or dot
  elements, and a bounded path length.
- The archive reader caps entry count, per-entry size, total size and compression ratio.
- Tests, all PASS (`go test ./internal/infrastructure/archive/`): `TestArchiveRefusesPathTraversal`,
  `TestArchiveRefusesEntryCountBomb`, `TestArchiveRefusesCompressionBomb`, `TestArchiveRefusesOversizeEntry`,
  `TestArchiveRefusesOversizeTotal`, `TestArchiveRefusesOversizePath`, `TestArchiveRefusesCorruptInput`,
  `TestArchiveWriterRefusesUnsafePath`, `TestNormalizePathRejectsUnicodeLookalikes`,
  `TestArchiveMissingChecksumsIsRefused`, `TestVerifyChecksumsDetectsTampering`.
- The hostile corpus is real files on disk, not synthetic strings: `testdata/malicious-imports/` holds
  `zip-slip.zip`, `zip-bomb-metadata.zip`, `zip-not-docx.zip`, `docx-entity-expansion.docx`,
  `docx-external-rel.docx`, `docx-prompt-injection.docx` and others; `TestHostileFixturesAreRefusedWithTheRightCategory`
  and `TestExternalRelationshipAndEntityFixturesAreRefused` assert each is refused **with the right
  category**, and `TestPromptInjectionTextImportsAsData` asserts injected text is kept as inert data.
  `TestDOCXContainerChecksHaveFixturesThatReachThem` guards against a fixture that no longer exercises
  the check it was written for.

### 9. 旧项目迁移存在静默丢失 — **CLEARED**

"Silent" is the operative word, and the check is whether anything can be dropped without a record.

- Every field the new schema cannot model produces a **typed warning**, not a discard:
  `WarningUnsupportedNodeType`, `WarningUnsupportedMetadata`, `WarningUnsupportedEdgeMetadata`,
  `WarningMissingMedia` (`internal/application/legacy/ports.go:14-22`), emitted at
  `transform.go:80,140,151,175,193,210,256,322`.
- Counts are asserted against the source fixture rather than against the importer's own claim:
  `TestImportAllNodeTypesIsComplete` compares `result.Counts.Nodes` to `len(snapshot.Projects[0].Nodes)`
  and `Counts.Edges` to the connection count, then reads the stored bundle back.
- **Warnings are asserted, and the assertion has discriminating power.** One test collects the
  produced codes and requires three specific ones to be present
  (`service_test.go:623`); another fails outright if `len(result.Warnings) == 0`
  (`service_test.go:350`). So a dropped warning is a failing test, not a quiet behaviour change.
- A failure records a run rather than disappearing: `ImportStore.RecordImport` stores an import that
  did not succeed "so a failed attempt is still visible to the user", and
  `TestImportRecordsFailure` / `TestLegacyImportRollsBackCompletely` cover it.
- **The warnings reach a user-visible surface, verified by grepping for the consumer rather than
  assuming the plumbing:** `ImportResult.Warnings` (`internal/application/legacy/service.go:240`) is
  carried out through `ProjectsBinding.ImportProjects`, and the frontend renders it —
  `web/src/components/canvas/canvas-migrate-dialog.tsx:199` renders the **result** warnings and
  `:146` the precheck warnings, via a `WarningList` at `:222`.
- The end-to-end acceptance test exists: `AC-E2E-001`'s corpus is `testdata/old-projects/{all-node-types,
  minimal, missing-media, malformed-metadata}` and the walk runs in `internal/application/legacy/service_test.go`.

### 10. 核心 E2E 测试未通过 — **CLEARED**

- `npx playwright test` → **25 passed, 1 skipped (26 total)**, exit 0. The skip is pre-existing and is
  the crop-action case, not a failure.
- The suite covers the regressions the specification names: `canvas-regression.spec.ts` implements
  AC-CANVAS-004 (free canvas: minimap, generation panel, image tools, reload-with-nodes) and
  `studio.spec.ts` covers the studio shell. Every test intercepts the network, so nothing reaches a
  provider.
- Go-side end-to-end walks also pass: `acceptance_wp11_e2e_test.go` (script → board → subtitles →
  video/audio versions → timeline → export → Final Ruleset → four documents, asserting on the database
  at each step), `TestACMME005TheCanaryScenarioRunsThroughTheWholeChain`,
  `TestCanaryDecisionToExecutionToSupervisorToGate`, `TestCanaryScriptPipelineToApprovedScript`,
  `TestCanaryProductionStagesToAnApprovedBoard`.
- **Stated limitation, not hidden:** the Playwright suite runs the canvas in **browser** mode against
  the Vite dev server. The Wails desktop shell is not launched, because a Wails window cannot be driven
  from a test runner — the config says so in its own header. So "the desktop shell's canvas works" is
  supported by the Go tests and by WP-01's native smoke evidence, not by this suite.

### 11. 许可证和第三方声明缺失 — **CLEARED** (by WP-12 item 10)

Was the one genuine gap; see the SBOM section of the report and `THIRD_PARTY_NOTICES.md`'s new
"Machine-readable SBOM" preamble.

- `scripts/gen-sbom.mjs` generates `sbom/cyclonedx.json` (CycloneDX 1.5 JSON, 1,516 components) and
  `sbom/licences.json` from the lockfiles plus on-disk module/package metadata. `--check` runs in both
  `scripts/verify.sh` and `scripts/verify.ps1`, so drift fails a gate.
- Every Go module in the graph and every npm package slot has an identified licence. **Zero**
  AGPL/GPL/LGPL/SSPL anywhere. The three MPL-2.0 entries are in the graph tier, not in
  `go build ./...`'s closure, and are named in the notices rather than left for a reader to find.
- `THIRD_PARTY_NOTICES.md` gained 86 entries for modules the file had none for, appended without
  removing an entry.
- **Not claimed:** there is no CI licence ALLOW/DENY policy engine. The scanner reports restrictive
  licences among direct dependencies and exits nonzero on one, which is the enforcement that exists.

---

## SECURITY section 19 — additional conditions

Checked because that section lists its own blockers. Items 1–3 and 5 map onto PRD rows above.

| SECURITY §19 | Verdict | Evidence beyond the PRD rows |
|---|---|---|
| 4. 重定向未复检 | **CLEARED** | `client.go:102` `CheckRedirect` re-validates each hop; `TestClientEnforcesRedirectHostChange`, `TestClientRejectsRedirectToPrivateViaPublicLookalike`, `TestDownloadRejectsRedirectToPrivateHost` PASS. `Authorization` dropped cross-host (`client.go:109`). |
| 6. Agent 越权 Tool 或跨项目访问 | **CLEARED** | The per-call `Authorizer` (blocker 7) plus the run's own scope: §7.1 forbids taking a project from a model, so tools read it from the run. `internal/application/agenttools/scope.go` + `scope_test.go`. |
| 7. Supervisor 默认可写 | **CLEARED** | `ToolAllowed(LayerSupervision, ToolWrite)` is `false` in the matrix, and `TestCanaryRefusesAnIllegalToolCall` drives the refusal through the whole stack. |
| 8. 用户内容可改变 Tool ACL/Workflow | **CLEARED** | The spec comes from the embedded skill manifest, and `userVersion`-style user text cannot reach it: prompts carry content as data (§5.2 untrusted context), and `TestPromptInjectionTextImportsAsData` pins that an injection is stored as a line, not executed. |
| 9. 媒体命令通过 Shell 字符串拼接 | **CLEARED** | `os/exec` appears in exactly one file, `internal/infrastructure/media/ffmpeg.go:443`, as `exec.CommandContext(runCtx, program, args...)` over a `[]string` — no shell, no quoting, no joining. `-`-prefixed paths refused. The scanner allowlists it by exact filename and fails on a stale entry. |
| 10. Migration/Restore 无安全副本 | **CLEARED** | The pre-migration `VACUUM INTO` snapshot (blocker 6), and `TestBackupRestoreVerifiesAgainstRealDatabase` for restore. |
| 11. 日志脱敏测试失败 | **CLEARED** | `internal/infrastructure/logging/logging_test.go`, including `TestRedactingHandlerUsesEscapeAwareCredentialScanning` — escape-aware, so an escaped credential is still caught. |
| 12. Secret scan、依赖漏洞或许可证阻断未处理 | **CLEARED** | Secret scan PASS (607 files); dependency vulnerabilities fixed (25 Go → 0, 24 npm → 4 tree-shaken-out); licence blocking implemented as the SBOM generator's nonzero exit. |
| 13. 数据完整性检查失败仍允许写入 | **CLEARED** | `database.go:46` returns a `ModeSafe` handle with a nil `SQL()`, so there is no writable pool to use. `TestOpenIntegrityFailureReturnsSafeMode` PASS. |
| 14. 高成本批量任务无确认/限制 | **CLEARED** | Media submissions carry confirmation-shaped bounds (`internal/desktop/media_jobs.go`: 60 s, 2,000 characters, 8 reference assets); canvas commands are bounded (`maxBatchNodes = 5000`, `maxSnapshotBytes = 64 << 20`); tool calls stop at a budget (`TestCanaryStopsAtItsToolCallBudget`). |
| 15. 安全测试语料未运行 | **CLEARED** | `testdata/malicious-imports/` (11 fixtures) and `testdata/old-projects/` are consumed by tests that ran and passed; `gen-canary-fixture.mjs --check` and `gen-malicious-fixtures.mjs --check` run in both verify scripts. |

---

## AGENT_CONTRACTS section 20 — additional conditions

Fourteen clauses. The ones that are genuinely distinct from the rows above:

| AGENT_CONTRACTS §20 | Verdict | Evidence |
|---|---|---|
| Decision 可直接使用业务写 Tool | **CLEARED** | `ToolAllowed(LayerDecision, ToolWrite)` is `false`; `agent_test.go:37` asserts it directly. |
| Tool 参数未 Schema 校验 | **CLEARED** | The runtime applies the tool's schema to what the model returns (`runner.go`, the comment at the schema step records that this check was MISSING once and was added); `schemas/agent/tools/*.json` are generated by `gen-tool-schemas.mjs` and `--check`ed, with a Go test comparing the key list. |
| Agent 可读取 Secret | **CLEARED** | No binding or tool returns a key (blocker 1); agents receive content, not credentials. |
| Agent 可执行任意网络/Shell/JS | **CLEARED** | No dynamic JS anywhere (blocker 2); the only subprocess is the audited ffmpeg adapter (row above); agent tools are a fixed registry of read/write domain commands, and the ACL matrix denies `ToolExternal` to Decision and Supervision. |
| Workflow 状态只在 Prompt | **CLEARED** | Blocker 4: state is read from SQLite; the prompt carries a *rendering* of it. |
| Execution 自述被当作成功 | **CLEARED** | Success is read back from the database, not taken from the model's message. `TestCanaryRefusesAHallucinatedArtifact` is the case that matters: an execution claiming an artifact that is not there is refused. |
| 当前消息召回自身 | **CLEARED** | Memory recall excludes the current message; covered in `internal/application/memory` tests. |
| Memory 可跨项目污染 | **CLEARED** | Every memory read/write carries a project scope (`request.Scope.Validate()` at `memory/service.go:76,172`); `TestWP12VectorCandidatesReachOnlyTheNewestRows` documents the recall *window* limit, which is a completeness property, not a cross-project leak. |
| Skill 无版本和 Hash | **CLEARED** | `gen-skill-packs.mjs --check` → `PASS: every manifest matches the table and every skill carries section 4.3's sections`; each agent has a skill version (AGENTS §10). |
| 输出无 Schema | **CLEARED** | Structured output is validated with ONE schema repair, then failure (`TestCanaryRepairsOnceThenFails`). |
| 无限自动 FIX/REDO | **CLEARED** | The quality gate caps automatic revision at 2; `TestApplySupervisionOverBudgetWaitsForAPerson` proves it parks for a person rather than looping. |
| CI 依赖真实付费模型 | **CLEARED** | CI runs the deterministic mock; no CI step contacts a paid provider. |
| 未保留 Run/Tool/Review 追踪 | **CLEARED** | `agent_runs`, `agent_tool_calls`, `review_reports`, `review_issues` and `workflow_events` tables record it; `ToolCallDenied` with a code is stored, which is what makes a denial attributable. |

---

## What this audit did NOT verify

- **No Wails native run.** The desktop shell was not launched; `wails` is not on `PATH` on this host,
  so both verify scripts SKIP the production build with an explicit reason. WP-01's native smoke
  evidence is on record, but it predates WP-12.
- **No `-race` run.** Environment failure, as stated at the top.
- **No remote CI run.** No push is authorized, so the GitHub Actions workflows were read, not executed.
- **No real provider was contacted.** Every provider-related result uses httptest or fakes, by design.
- **The Playwright suite does not drive the desktop shell** (see blocker 10), so "the canvas renders
  at 1,000 nodes in the Wails webview" remains unmeasured.
