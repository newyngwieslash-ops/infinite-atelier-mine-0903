# Implementation Status

> Last updated: 2026-09-18
> Product: Infinite Atelier Core + Drama Production Pack
> Current work package: **WP-05 — 短剧领域模型与工作室 UI Shell**
> Status: **COMPLETE for the WP-05 scope recorded in section 0e, and for the follow-up recorded in section 0f** (the drama schema, domain vocabulary, relation registry, application services and repositories, the drama and assets bindings, the studio shell and creation wizard, the projection and required-reference commands AC-CANVAS-001/002 need, the section 17 domain event stream, and the five version families that had no approval command). WP-01 through WP-04 remain COMPLETE for their recorded scopes; sections 0e and 0f state exactly which earlier gaps this package closed and which it left open.

WP-03 start baseline (2026-09-15): branch `codex/wp-01-desktop-foundation`, HEAD `a243891455ec17687dd54b5ac90d3bd64478a1a1`, empty index. Freshly re-run baseline: `go test ./... -count=1` PASS (15 packages at start), `go vet ./...` PASS, `web` `npm run typecheck` PASS, `npm test` PASS (15 tests), `npm run build` PASS. Go commands require `GOTOOLCHAIN=go1.25.0 GOSUMDB=sum.golang.org` on this host because the user-level `go env` sets `GOSUMDB=off`, which blocks toolchain verification. The working tree already contained the WP-01/WP-02 tracked and untracked work plus the user's brand rename; none of it was modified outside the WP-03 scope.

# 0f. WP-05 follow-up: domain events and the missing approvals (2026-09-18)

## Why this exists

Section 0e closed WP-05 with two partial deliveries named as its largest gaps:
the §17 domain event stream was not implemented, and five of the eight version
families had no way to reach `approved`. This section records the work that
closed both, and what it found while doing so.

## Scope completed

- **Status: COMPLETE for this follow-up.** The two gaps section 0e named are
  closed to the extent WP-05's own commands can close them; what remains is
  named below per event.
- Schema: forward migration `000013_domain_events.sql`. `000001`–`000012` were
  not modified.
- Domain: `internal/domain/event` — §17's twenty-eight names and the envelope,
  both pinned by a test against the specification's list.
- Application: `internal/application/events` (record, build, list, count) plus
  the recorder ports on six services and the emissions from their commands.
- Infrastructure: the `domain_events` repository, and the shared §2.5 approval
  switch in `version_approval.go`.
- Bindings: `ListDomainEvents` and `CountDomainEvents` on `DramaBinding`, with
  the Wails surface regenerated.
- Frontend: `listDomainEvents` / `countDomainEvents` and `DOMAIN_EVENT_TYPES` in
  `services/desktop/drama.ts`.
- Docs: ADR-0009, the ADR index, TRACEABILITY, README, this section.

## Commands executed and actual results

| Command | Result |
|---|---|
| `go test ./... -count=1` | **PASS** (35 packages ok) |
| `go vet ./...` | **PASS** |
| `gofmt -l .` | **PASS** (no output) |
| `node scripts/security-scan.mjs` | **PASS** (365 files scanned) |
| `web`: `npm run typecheck` | **PASS** |
| `wails generate module` (v2.15.0) | **PASS**; `models.ts` 99 classes to 101, both added, none removed; `DramaBinding` now 48 methods |
| `git diff --check` | **PASS** |
| `go test -race ./...` | **ENVIRONMENT FAILURE, not a pass** — unchanged from section 0e |

## What was delivered

**The event stream.** `domain_events` stores the §17 envelope. It has no foreign
key to an event's subject and no revision: an event outlives what it describes,
and a stream that could be rewritten would not be a record of what happened. The
one reference it keeps is to the project, because every drama query is
project-scoped.

**Two emission paths, and the difference is deliberate.** An approval records its
event inside its own transaction and refuses without a recorder, because the
event is the governance record of a decision. Every other command announces
itself after its write succeeds and carries on if the announcement fails, because
the row is already committed. ADR-0009 draws the line; `RecordBestEffort`'s doc
comment says why its error is dropped.

**The five missing approvals.** Skeleton, adaptation strategy, director plan,
storyboard version and style guide version now have commands. All eight families
share one implementation of the switch, because the order of its two writes is
what keeps the schema's partial unique index satisfiable.

**Twenty of the twenty-eight events are emitted** by the commands that cause
them. The seven that are not belong to packages that do not exist yet, and each
is named below rather than left to be discovered.

## Defects found while doing the work

1. **`script_versions` had a second implementation of the §2.5 switch.** It
   predated the shared helper, took the supersede target as a caller-supplied id,
   and had its own transaction. Mutation testing found it: disabling the shared
   helper's supersede step left the suite green, because the script family never
   used it. Two implementations of one rule can disagree about the order, so the
   script repository now delegates to the shared switch and the whole repository
   has exactly one statement writing `'superseded'`.

2. **A nil recorder panicked.** The recorder is an interface, so a service
   composed without one holds a nil interface and calling a method on it panics.
   The existing test suite caught this when the first emit site was added. Each
   service now has one nil-checked `recordEvent`, so no emit site can forget.

3. **The same duplication appeared twice in this follow-up itself**: a first
   `approveVersion` helper was written without an event and left unreachable
   while every family used `approveVersionWithEvent`. It was removed, and the
   file header records that it existed rather than pretending it never did.

## Emissions: the twenty and the seven

| Emitted by WP-05 commands | Not emitted, and by whom |
|---|---|
| ProjectCreated, ProjectSettingsChanged, ProjectRuleLocked | ChapterBoundariesConfirmed — WP-06's chapter confirmation |
| SourceDocumentImported | GenerationJobQueued / Succeeded / Failed — the job core |
| StoryFactAccepted, StoryFactConflictOpened | MemoryCreated — WP-10 |
| EpisodeCreated, StorySkeletonApproved, AdaptationStrategyApproved | UpstreamVersionChanged — WP-07's impact analyzer. WP-05 emits `ArtifactMarkedStale`, the mark itself rather than the upstream change that caused it |
| ScriptVersionCreated, ScriptVersionApproved | BackupCompleted — the backup service |
| AssetVersionCreated, AssetVersionApproved | |
| DirectorPlanApproved, StoryboardVersionApproved | |
| CanvasProjectionCreated | |
| WorkflowStarted, WorkflowStageChanged, ReviewReportCreated, UserGateDecided | |
| ArtifactMarkedStale | |

Emitting the remaining seven from WP-05 would mean inventing their call sites, so
the vocabulary and the table are complete while the emissions are not.

## Known limits

- **The style-guide approval reports `ProjectSettingsChanged`.** §17 defines no
  style-guide event and the vocabulary is closed, so the approval uses the
  closest name §17 offers with a payload naming what changed. ADR-0009 records it
  and the alternative that was not taken.
- **A best-effort event can be lost** if the event write fails after the row
  commits. The row is still the authority on what exists; the stream is a
  projection, like the canvas. That is what ADR-0009's first decision buys and
  costs.
- **The stream has no subscription.** It is a query
  (`ListDomainEvents`/`CountDomainEvents`), not a push. The `core:event` channel
  carries job and provider events; wiring domain events onto it would be a
  transport decision for whichever package first needs live updates.
- **Only `script_versions`' approval path is covered end to end against the real
  database.** The other seven families are covered through the shared switch at
  the unit and repository level, plus the mutation that proves the switch's order
  matters. A dedicated integration test per family would be more convincing and
  was not written.
- Real paid providers were never contacted; every test uses synthetic fixtures
  and temporary databases.

## Git and data safety

- Existing user changes preserved: **yes**. An unrelated 78 KB report appeared in
  the working tree during this work; it is not WP-05's, it was not read into the
  package, and it was deliberately left untracked and uncommitted.
- Automatic commit/push/stash/reset/clean: **none**.
- Secrets found or introduced: **none**; the scanner passes over 365 files.
- Migrations executed against user data: **none**. `000013` ran only against
  temporary test databases.
- Every mutation probe was restored, and the tree was verified clean afterwards.

# 0e. WP-05 result (2026-09-18)

## Scope completed

- **Status: COMPLETE (WP-05 scope)**, with the limits listed below stated plainly rather than
  presented as passes. Every acceptance item below was executed on this host.
- Scope: drama project configuration (settings, rules, style guides, model policies); the
  source/chapter tables; the story fact layer (entities, aliases, events, participants, relations,
  evidence, conflicts, character state); episodes and the script pipeline (skeleton, strategy,
  script, scene, dialogue, shot); the asset aggregate rebuilt to the documented vocabularies plus
  lineage and usage; director plan, storyboard table and panels; workflow, review and user-gate
  tables; the canvas entity reference and relation registry; the studio navigation with honest
  empty states; the project creation wizard; domain commands, queries and the staleness
  propagation; and the unit tests for revision, version, approval and staleness.
- Schema: seven forward migrations, `000006`–`000012`. `000001`–`000005` were not modified.
- Domain: `versioning` (the shared §2.5 vocabulary), `story`, `script`, `asset` (extended),
  `storyboard`, `workflow`, `staleness`, and `project` (extended with the drama configuration and
  the §10.4 relation registry).
- Application: `story`, `script`, `projects` (extended with the drama configuration and the
  projection commands), `assets` (extended with lineage, usage and approval impact), `storyboard`,
  `workflow`, `staleness`.
- Infrastructure: `database/{story,script,storyboard,workflow,staleness,drama_settings,projection}`
  plus the canvas and asset repository extensions.
- Bindings: `DramaBinding` (52 methods across five services) and `AssetsBinding`, with
  `drama_wiring.go` composing them and `app.go` attaching only over a writable database.
  `CreateProjectRequest` gained the PRD FR-020 drama fields flat, and `ProjectsBinding` gained
  `GetProjectSettings`, `UpdateProjectSettings` and `ListProjectRules`.
- Frontend: the `/studio` list, the 14-section shell, the creation wizard, the studio service
  client, the UI-state store, and the section nav. The canvas persistence adapter was fixed to
  carry entity references and edge relation types in both directions.
- Docs: ADR-0007 (schema and vocabulary rulings), ADR-0008 (staleness, projection, approval),
  this section, the ADR index, TRACEABILITY and the README.

## Commands executed and actual results

| Command | Result |
|---|---|
| `go test ./... -count=1` | **PASS** (35 packages ok) |
| `go vet ./...` | **PASS** |
| `gofmt -l .` | **PASS** (no output) |
| `node scripts/security-scan.mjs` | **PASS** (365 files scanned; 1 audited dynamic-execution exception, 4 audited legacy direct-call files with named owners) |
| `web`: `npm run typecheck` | **PASS** |
| `web`: `npm test` | **PASS** (36 tests) |
| `web`: `npm run test:e2e` | **PASS** (22 passed, 1 skipped; the skip is the pre-existing crop dialog that needs node content a headless run cannot supply) |
| `web`: `npm run build` | **PASS** with the pre-existing over-500 kB chunk warning |
| `wails generate module` (v2.15.0) | **PASS**; produced `DramaBinding`, `AssetsBinding` and `BackupBinding` and extended `models.ts` |
| `git diff --check` | **PASS** |
| `go test -race ./... -count=1` | **ENVIRONMENT FAILURE, not a pass** — the host has no 64-bit CGO compiler, which reproduces on untouched packages. |

`models.ts` grew from 33 exported classes to 95. The change was checked to be purely additive by
comparing the class-name lists against the previous revision: 62 added, zero removed or renamed.

## Acceptance

| ID | Result | Evidence |
|---|---|---|
| Domain Model MVP tables and constraints | **PASS** | `TestWP05MigrationFreshDatabase` (43 tables), `TestWP05ConstraintsEnforceDocumentedValues` (each documented value accepted, each undocumented one rejected), and the vocabulary parity guard below. |
| approved 唯一 | **PASS** | A partial unique index in each of the eight version families plus `TestWP05ApprovedVersionIsUnique`, which asserts each index exists, is `UNIQUE` and is partial on the approved status. All eight were mutation-verified: weakening any one to a plain index fails the test. |
| locked rule | **PASS** | `Rule.CanModify` and `CanEscalate` gate the writer, and the service applies both against the stored rule. Two tests: one for the lock-change guard, one isolating `CanModify` by editing content with the lock flag unchanged. Both were mutation-verified. |
| stale 传播基础 | **PASS** | The §15.2 dependency graph with a schema-column justification per edge, `PropagateFrom` walking it, the waiver rules of §15.3, and an end-to-end test against a real database. Severity-against-the-original-change was mutation-verified from two directions. |
| 创建 Drama Project/Episode/Asset | **PASS** | `TestCreateDramaProjectWritesSettingsAndDramaCanvas` (real SQLite, every wizard field round-tripped) plus the episode and asset commands in the story/script/asset suites. The E2E suite covers the browser-side refusal only, because a browser has no core (see the limits below). |
| Canvas projection 基础 AC-CANVAS-001/002 子集 | **PASS** | `CreateCanvasProjection` (reference written and read back, idempotent, half a reference refused), `RemoveCanvasProjection` (node gone, entity untouched), `DeleteNodes` refusing a required reference, `CreateEdge` validating against the registry, and `FindEntityReferences` listing an entity's projections and blockers. Six tests, two of them mutation-verified. |

The vocabulary parity guard (`vocabulary_parity_wp05_test.go`) parses each closed vocabulary out of
the migrations per table and compares it against the Go list that must agree, in both directions. It
checks itself: it asserts the parser finds a known value, that per-table parsing keeps two `status`
columns apart, and that a mismatch, a duplicate and an unreadable column are each reported rather
than silently passing.

## The three documented vocabulary conflicts

All three were resolved in favour of the PRD's spellings with the reasoning recorded in the
migration headers, the package docs and ADR-0007, and each is pinned by a test that asserts the
rejected spellings stay rejected. The quality-gate stage keys are deliberately left unpinned
because choosing between the two documented lists belongs to WP-07.

## Defects found by review and fixed before this record

Two independent reviews ran. A spec review found four blockers; three were real gaps against
WP-05's own acceptance bullets.

1. **Projection had no writer.** Nodes carried entity-reference columns and nothing in the product
   ever filled them, so AC-CANVAS-001 was untestable. `CreateCanvasProjection`,
   `RemoveCanvasProjection` and `FindEntityReferences` were added.
2. **A required reference did not block a delete.** `canvas_edges.required` was stored and never
   read. `DeleteNodes` now refuses the batch and names the blocking edges.
3. **`CreateEdge` stored any registered relation without checking endpoints.** It now runs the
   registry, refuses an illegal edge, and writes the verdict to `validation_status`.
4. **The approved-uniqueness test covered 3 of 8 families**, so weakening five of the eight indexes
   left the suite green. All eight are covered now.

The spec review also found a correctness bug in the registry: `requires_version` demanded a version
on both endpoints, which made `uses_character` and `first_frame_of` unsatisfiable because a shot has
no version column. It now requires the version only on the versioned side.

A quality review then ran twelve mutation probes. Ten were caught. The two that were not:

- `ApproveVersion`'s supersede step had no test at all — the application assets package had no
  service test and there was no assets integration test. `assets_wp05_test.go` now covers it plus
  the approval preconditions, the impact list, lineage and usage.
- The locked-rule service test also exercised the lock-change guard, so removing `CanModify` left it
  passing. An isolating test was added.

Both reviews' remaining findings were also addressed: two dead functions were removed or their
comments corrected to say plainly that nothing calls them yet, and two hardcoded wizard
placeholders became i18n keys.

## Known limits and deferred work

- **The E2E suite cannot cover the with-core path.** A browser has no Go core, so creating a project,
  listing episodes and reading projections are verified at the application and database layers and
  by the binding tests, not end to end. The E2E spec says so rather than mocking the bindings, which
  would test a fiction. A desktop-driven suite is the only way to close this.
- **Domain events (§17) are not implemented.** The workflow service writes a `workflow_events` audit
  row per state change, which satisfies PRD FR-100, but the §17 event stream and its envelope are
  absent. Scope item 11 is therefore one third delivered: commands and queries are there, events are
  not. This is the largest single gap this package leaves.
- **Six of the eight version families have no approval command.** Only script versions, asset
  versions and storyboard panels can be approved. Skeleton, strategy, style guide, director plan and
  storyboard version rows can hold `approved` only if something else writes it, which nothing does.
- **§19's integrity checks are not implemented.** The code comments in the workflow domain point at
  a check that does not exist yet; §19 was not in WP-05's scope, but the reference is a promise the
  next package should keep or remove.
- **`IsContentFrozen` has no caller.** No version-edit command exists in WP-05, so §2.5's
  "批准后不可原地编辑" is not enforced anywhere. The predicate is in place for the first edit
  command and its comment says so.
- **Dialogue lines, story entity aliases and fact sources have tables, domain types and validation
  but no command.** Their writers are WP-06's extraction pipeline and WP-08's script path.
- **`asset_usages` other than the ones the asset commands write** are populated by nothing else yet;
  the shot and panel commands that would record a reference belong to WP-08/WP-09.
- **The studio's story-graph section cannot list entities**: `DramaBinding` exposes creation,
  acceptance and rejection but no list query, so the section states the gap rather than showing an
  empty table it cannot fill.
- `LoadCanvas` returns a project's oldest canvas document. For a wizard-created drama project that is
  its drama canvas; for a migrated project with pre-existing documents it may be another one.
- ADR-0002 remains **Proposed**, as WP-04 left it. WP-05 followed the migration contract it records
  but did not change its status.
- `docs/adr/README.md`'s index lists ADR-0002 as Accepted while the file itself says Proposed. The
  index was extended with 0007 and 0008 but this pre-existing discrepancy was not silently changed.
- Real paid providers were never contacted; every test uses synthetic fixtures and temporary
  databases.

## Git and data safety

- Existing user changes preserved: **yes**.
- Automatic commit/push/stash/reset/clean: **none**.
- Secrets found or introduced: **none**; the scanner passes over 365 files.
- Migrations executed against user data: **none**. `000006`–`000012` ran only against temporary test
  databases. The `000009` rebuild of the asset tables is covered by an upgrade test that seeds
  WP-04-shaped rows first and asserts every row, id and status mapping survives, that
  `PRAGMA foreign_key_check` is clean, and that a second run is a no-op.
- Every mutation probe run during review was restored, and the working tree was verified clean
  afterwards.

---

# 0d. WP-04 result (2026-09-17)

## Scope completed

- **Status: COMPLETE (WP-04 scope)**. Every acceptance item below was executed on this host;
  the limitations are listed explicitly and are not disguised as passes.
- Schema: forward-only `000004_projects_canvas.sql` adds workspaces, projects, canvas documents,
  nodes, edges, chat sessions, assets, asset versions, asset files, archived generation history
  and the legacy import bookkeeping. `000005_legacy_project_fingerprints.sql` adds the per-project
  fingerprint table the idempotency check needs. `000001`-`000003` were not modified.
- Domain: `internal/domain/project` (workspaces, projects, canvas kinds, the relation registry
  with the `generic` fallback) and `internal/domain/asset` (assets, versions, the approval
  preconditions). Neither mints or validates an identifier (ADR-0005).
- Application: `internal/application/projects`, `.../assets`, `.../legacy` (snapshot, fingerprint,
  transform, import) and `.../backup` (export and restore).
- Infrastructure: SQLite repositories with revision guards and cascades, the atomic import, the
  hardened ZIP reader/writer, and the backup store.
- Bindings: `ProjectsBinding` (projects, canvas, nodes, edges, chats, import),
  `LegacyUploadBinding` (the chunked base64 channel) and `BackupBinding` (export and restore
  preview). The upload surface was audited down to its five transfer methods: the configuration
  entry point and the media reader are package-level functions, so neither a filesystem path nor
  an internal port reaches the webview.
- Frontend: the canvas persistence adapter (Go and legacy implementations behind one mode
  decision), the migration dialog in both locales, and a Playwright regression for the canvas.

## Commands executed and actual results

| Command | Result |
|---|---|
| `go test ./... -count=1` | **PASS** (19 packages ok) |
| `go vet ./...` | **PASS** |
| `gofmt -l .` | **PASS** (no output) |
| `node scripts/security-scan.mjs` | **PASS** (296 files scanned; 1 audited dynamic-execution exception, 4 audited legacy direct-call files with named owners) |
| `web`: `npm run typecheck` | **PASS** |
| `web`: `npm test` | **PASS** (34 tests) |
| `web`: `npm run test:e2e` | **PASS** (13 passed, 1 skipped; the skip is the crop dialog, which needs node content a headless run cannot supply) |
| `web`: `npm run build` | **PASS** with the pre-existing over-500 kB chunk warning |
| `scripts/verify.sh` / `scripts/verify.ps1` | **PASS**; both now run the canvas regression, and each reports its own skip reason when Playwright is absent |
| `wails build -s` (v2.15.0) | **PASS**; produced `build/bin/InfiniteAtelier.exe` |
| `git diff --check` | **PASS** (exit 0; only pre-existing LF/CRLF advisory warnings) |
| `go test -race ./... -count=1` | **ENVIRONMENT FAILURE, not a pass** — the host 32-bit MinGW GCC cannot compile amd64 CGO. |

## Acceptance

| ID | Result | Evidence |
|---|---|---|
| AC-LEGACY-001 | **PASS** | `TestImportAllNodeTypesIsComplete` (counts, text, viewport, warnings, relations), `TestLegacyImportSnapshotIsAtomic` against the real store, `TestImportMissingMediaIsReportedNotFatal`, `TestImportMalformedMetadataIsRetained`, and the media checks against the fixtures in `testdata/old-projects/`. |
| AC-LEGACY-002 | **PASS** | `TestImportIsIdempotent` (the second import skips, copy mode makes a distinct project, media stays deduplicated) plus `TestLegacyProjectFingerprintIsPerProject` against the real store, which is the level an earlier revision failed at. |
| AC-LEGACY-003 | **PASS** | `TestLegacyImportRollsBackCompletely`, `TestLegacyProjectFingerprintSurvivesFailure`, `TestImportFailureLeavesNoProject` (the failing stage is named) and `TestImportDatabaseFailureIsReported`; the upload tests cover temporary-file cleanup. |
| AC-CANVAS-004 | **PASS** | `web/e2e/canvas-regression.spec.ts`: add, move (asserting a second node did not shift), delete, multi-select, box select, zoom, pan (asserting every node shifted by one delta), undo/redo, connections, the minimap toggle, the generation prompt surface, the image node's actions and a reload. Each was mutation-checked: breaking the behaviour fails its test. |
| AC-BACKUP-001 | **PASS** | `TestBackupRoundTrip`, `TestBackupContainsNoSecrets`, `TestRestoreRefusesSecretBearingArchive`, `TestRestoreRefusesCredentialHiddenInAnObject`, `TestBackupScanCoversAuthorizationAndCookie`, `TestRestoreRefusesTamperedArchive` and `TestBackupExportAgainstRealStorage`. |
| AC-BACKUP-002 | **PARTIAL** | Tamper detection and staged validation are tested; the atomic swap of a live database is not implemented, so a restore validates and stages and the promotion step belongs to WP-12. |
| AC-SEC-002 | **PASS** | The corpus in `internal/infrastructure/archive/archive_test.go`: `../`, absolute paths, drive letters, UNC, device names, trailing dot and space, dot elements, symlinks, duplicate normalised paths, entry-count and ratio bombs, size ceilings, corrupt input, a false manifest and a wrong hash. |

## Independent review

**Spec review (independent subagent): CHANGES REQUIRED → fixed.** It found six blockers, each
confirmed against the code and fixed:

- A node the canvas created was never stored: the binding treated an empty id as "create" while the
  adapter always sends the id it minted, so every new node took the update path and failed on a row
  that did not exist. The binding now decides by whether the row exists.
- The viewport was never persisted: the adapter sent revision 0 against a document whose revision
  starts at 1, so every write conflicted. The service now writes against the revision it just read.
- Idempotency could never fire: the store compared a per-project fingerprint, but the import
  recorded the whole run's, so a second import duplicated the project. A new table holds one row per
  imported project. `TestLegacyProjectFingerprintIsPerProject` pins it against the real store; the
  in-memory double had been recording whatever it was asked about, which is how the bug hid.
- The migration dialog read `undefined`: the Go structs carried no JSON tags, so the wire shape was
  PascalCase while the TypeScript types expect camelCase. Every shared struct is tagged, and the
  collections are never nil.
- The generation history was never converted: `bundle.History` was never populated, so an entire
  entity class was silently absent. `TransformHistory` archives it and the all-node-types test
  asserts the two records its fixture declares.
- The backup services were composed but unreachable: no binding exposed them. `BackupBinding` now
  exports an archive and previews a restore.

It also found majors, all fixed: the precheck committed media while the UI claimed nothing was
written; the media-hash assertion in the import test was vacuous; ADR-0006 described a snapshot and
a scheduler pause the code does not have, and now records both as gaps; the restore staging area was
never cleaned; the secret scan read only the manifest and the database and looked for no header
names; several DOMAIN_MODEL §8 fields were absent; project rename and delete bypassed the adapter;
the save diff recorded a write as done before it had succeeded; and four of the nine AC-CANVAS-004
tests could pass while the behaviour they named was broken.

## Canvas regression

The suite found a real regression while it was being written: a project reference added to a save
effect's dependency array made the renderer loop until React aborted with "Maximum update depth
exceeded". The page read the project through a store subscription inside an effect that also wrote
to that store, so the effect triggered itself. It now reads the project at call time.

## Known limits and deferred work

- A restore validates and stages but does not swap a live database; that promotion and its
  user-confirmation step belong to WP-12 (AC-BACKUP-002).
- Encrypted sensitive backup is not implemented and is out of WP-04 scope.
- `asset_relations` and `asset_usages` (DOMAIN_MODEL §8.5/§8.6) are not created, and the asset file
  roles are narrower than §8.4 lists: the canvas image tools that would produce a mask or a first
  frame are not migrated yet. This is a deliberate deviation, recorded here rather than claimed as
  conformance.
- The `assets` application service has no composition root: the import writes asset rows through the
  repository directly. It exists for WP-05.
- The generation history and the asset library are stored per browser profile, so both are attached
  to the first imported project and the report says so.
- MONOFORM's scene data, the prompt library and UI preferences are recorded and reported, not
  imported: they belong to another tool or to frontend state.
- The canvas regression runs in browser mode. A Wails window cannot be driven from a test runner, so
  the Go adapter's command shapes are covered by the Go binding tests rather than end to end.
- Real paid providers were never contacted; the import and backup evidence uses the synthetic
  fixtures in `testdata/` and temporary databases.

## Git and data safety

- Existing user changes preserved: **yes**.
- Automatic commit/push/stash/reset/clean: **none** (the commits were requested by the user).
- Secrets found or introduced: **none**; the scanner passes over 296 files, and the only fixture
  holding a key-shaped string is exempted by name with stale-exemption detection.
- Migrations executed against user data: **none**; `000004` and `000005` ran only against temporary
  test databases, and the upgrade tests assert every pre-existing row survives.

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
- Active constraints: WP-01 is closed; preserve all WP-00/user changes; no commit/push/stash/reset/clean; no real Provider calls. WP-02 was implemented and closed on 2026-09-15 (section 0b); WP-03 was implemented and closed on 2026-09-15 (section 0c); WP-04 was implemented and closed on 2026-09-17 (section 0d); WP-05 has not started.

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

The prior WP-00 recommendation to begin WP-01 was completed, and the WP-02 and WP-03 recommendations were executed on 2026-09-15 (sections 0b and 0c). The authoritative current state is: WP-01 COMPLETE (Windows foundation scope); WP-02 COMPLETE (its recorded scope); WP-03 COMPLETE (its recorded scope, with the gaps listed in section 0c); WP-04 was implemented and closed on 2026-09-17 (section 0d); WP-05 has not started and requires separate user authorization.

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
