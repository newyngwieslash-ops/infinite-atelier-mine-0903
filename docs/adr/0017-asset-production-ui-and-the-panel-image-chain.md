# ADR-0017 Asset-Production UI: the Panel-Image Chain, Its Approve Control, and the AC-E2E-002 Walk

- Status: Accepted (WP-13 scope)
- Date: 2026-09-24
- Deciders: Repository engineering under the user's ruling that P0-1 of
  `docs/implementation/project-progress-and-remaining-tasks-2026-09-23.md` is a standalone package
- Related work package: WP-13 (资产生产 UI：分镜图批量生成、候选批准与 AC-E2E-002 整场景串测)

## Context

`storyboard_panel_versions.approved_image_asset_version_id` is the column the MP4 export joins on:
`internal/infrastructure/database/timeline.go:92,103` and `final_reader.go:192,204` both read it, and
the Final Ruleset's first rule is that every required shot has approved media
(`internal/application/consistency/final.go`). **Nothing in the interface writes it.**

That is not a missing backend. Every command exists and is tested:

- `productionpipeline.Service.CheckStoryboardGate` (`batch.go:130`) — the pre-flight read.
- `productionpipeline.Service.RunImageBatch` (`batch.go:167`) — submits one image job per candidate
  per shot, 1–8 candidates, idempotency keyed on the candidate index, and on a half-failure it
  returns the successful submissions *together with* the error.
- `productionpipeline.Service.CollectBatchResults` (`candidate.go:73`) — turns succeeded jobs into
  CANDIDATE asset versions, skipping non-terminal ones and refusing a succeeded job with no readable
  hash; it calls `AttachJobResult` and `AddUsage(consumer_type='storyboard_panel')` itself.
- `storyboard.Service.ApprovePanelImage` (`storyboard/service.go:439`) — §9.5's rule through the
  domain's `CanApproveImage` (`domain/storyboard/storyboard.go:329`): the approved image must be one
  of the candidate versions the caller supplied.

Ten desktop bindings wrap these and have **zero** callers in `web/src` outside the generated
`wailsjs/`: `CheckStoryboardGate`, `RunImageBatch`, `CollectBatchResults`, `ApproveCandidate`,
`ApprovePanelImage`, `AddVersion`, `AttachFile`, `AddUsage`, `AddRelation`, `AttachJobResult`
(verified by grep, per method, in the WP-13 plan's survey).

Two further facts shape this record. First, **`CreatePanelVersion` has no production caller either**
(`drama_binding.go:1858,1882,2329`): panels are the object §9.5 approves, and nothing creates one.
Second, the interface *already promises this feature in its own copy*:
`studio.storyboardTable.approvedHint` reads 「当前生效的分镜，批量生成图片针对它运行。」
(`web/src/i18n/locales/zh-CN.ts:1920`) and `studio.storyboardTable.panels` is 「面板」
(`:1932`) — strings with no control behind them.

## Decision drivers

1. AGENTS section 12's "interface with no real path" — the defect shape this repository's reviews
   keep finding, and the one WP-12's section 0n2 named as its largest open item.
2. Minimal surface: the chain has five write steps and the UI must supply ids the backend
   deliberately does not invent (per-item assets, panel versions, the parent item's revision).
3. Existing precedent over new invention: this repository has established patterns for every piece
   (provider/model resolution, job tables, refreshing, lazy binding access, i18n), and a new pattern
   would need a reason.
4. The acceptance criterion is PRD §19's AC-E2E-002 — one walk from a novel to approved panel
   images — because that is the scenario the gap breaks.

## Options considered

- **A new top-level studio section for panels.** Rejected: it would re-list the same shots the
  storyboard table already renders, and the i18n keys that anticipate the feature live under
  `studio.storyboardTable`. A separate section also invites a second shot-listing code path.
- **A row-level control only, no batch entry.** Rejected: AC-BOARD-003 is about batch image jobs and
  `RunImageBatch` is the batch command; a per-row-only UI would never exercise the candidate counts
  and the idempotency the command exists for.
- **Make `CollectBatchResults` create the per-item assets itself.** Rejected: it changes a tested
  application-service contract to spare the UI a step, which inverts the dependency the repository
  keeps (the backend states its requirements; the caller meets them). `candidate_wiring_test.go:83`
  already establishes what meeting them looks like.
- **Wire `ApproveCandidate` alongside `ApprovePanelImage`.** Rejected: they are the same act —
  `ApproveCandidate` delegates to `storyboard.Service.ApprovePanelImage` (`candidate.go:171`) and its
  DTO carries *fewer* fields (the app-service request's `ItemID` is not populated from it). Two
  entry points for one act, differing in completeness, is how a caller picks the wrong one.
- **Wire all five AssetsBinding methods into the UI.** Rejected: the chain needs `AttachFile` for
  user-supplied images and the other four either run inside `CollectBatchResults`
  (`AttachJobResult`, `AddUsage`) or serve flows this package does not build (`AddRelation`,
  `AddVersion`). Wiring a control with no scenario is the same defect as a binding with no caller.
- **Drive the AC-E2E-002 walk through the browser (Playwright).** Rejected: the walk needs a real Go
  core, a migrated database and a mock provider; this repository's canaries are all Go-level for
  exactly that reason, and the browser suite asserts the UI's behaviour against a missing core.

## Decision

**Ruling 1 — the UI lives inside the storyboard table view.** A batch entry in the header beside the
approval control, and a per-row panel drawer. No new section.

**Ruling 2 — panel versions are created by an explicit step before collection.** For each item with
no panel version, the UI calls `CreatePanelVersion` with `VisualPrompt` taken from the item's own
`visualDescription`. §9.5's approval needs a panel version to approve, and nothing else creates one.

**Ruling 3 — one image asset per item, named from the shot's ordinal.** The UI calls
`CreateAsset({type:'image'})` per item and the resulting map *is* the `assetByItem` argument
`CollectBatchResults` requires. This is the pattern `candidate_wiring_test.go:83-95` demonstrates. A
re-run of the batch reuses the same asset and appends versions to it.

**Ruling 4 — refreshing is manual, plus an event-driven reload.** The section does not put a timer on
a provider-backed read (the principle `video-view.tsx:105-118` states). It subscribes to the job
change event and reloads when one arrives, offers a manual refresh, and **unsubscribes on unmount**.

**Ruling 5 — `ApproveCandidate` keeps its binding and gets no UI.** Recorded here so the zero-caller
count falls for the right reason: the act has one control, and it is `ApprovePanelImage`.

**Ruling 6 — of the five AssetsBinding methods, only `AttachFile` reaches the UI.** The other four
are wrapped in the service layer (so tests and later packages have a typed path) but have no control.
Each unwired one is recorded with what would unlock it: `AttachJobResult` and `AddUsage` already run
inside `CollectBatchResults`; `AddVersion` would serve "register an externally produced image as a
version"; `AddRelation` would serve an explicit lineage editor.

**Ruling 7 — the AC-E2E-002 walk is Go-level**, `internal/infrastructure/database/acceptance_e2e002_test.go`,
and its input is the existing canary fixture (32,736 Chinese characters, 5 chapters,
`testdata/canary-drama/`). The walk proves the CHAIN; the fixture's size is asserted, not re-authored.

**Ruling 8 — "创建 3 集规划" is data, not a new agent stage.** The walk creates three episodes with
`CreateEpisode`. PRD §19 does not describe a planning artifact for the episode split, and inventing
an agent stage to produce one would be this package writing a requirement the spec does not state.
**This ruling is the most rebuttable in this record**: if the product intent is a model-produced
three-episode plan, the change is additive and touches the walk, not the product code.

**Ruling 9 — the `asset_generation` stage runs in the walk** (2+ characters, 2+ scenes), driven the
way `canary_production_test.go` drives its stages. The stage is already in the policy table
(`agentruntime/engine.go:158`); no canary exercised it.

**Ruling 10 — the walk closes on a traceability read.** `GetTimeline(episodeId)` must show every
shot with a non-empty `approvedImageAssetVersionId` that resolves to an existing version, and the
job that produced it must resolve through `GenerationJobID`. That read is exactly what the export
performs, so "所有结果可追溯到输入、模型、任务和版本" is asserted against the same join the
product uses rather than against the tables behind it.

## Consequences

- The export chain becomes reachable in the interface: `approved_image_asset_version_id` gains a
  writer, and AC-E2E-002's last clauses stop being backend-only.
- **The batch's job volume becomes a user-visible number.** 12 shots × 8 candidates is 96 jobs; the
  submit modal states the count it is about to create. The per-provider concurrency limit (handoff
  P0-2, FR-150) remains unimplemented, so against a real paid provider this UI can exceed a quota —
  a dependency this record states rather than hides.
- `CreatePanelVersion` is used in production for the first time, with the minimum field set
  (`VisualPrompt`, `ChangeReason`). Any validation the UI does not satisfy surfaces in the walk
  before it surfaces for a user.
- Four AssetsBinding methods remain without UI callers *by decision*, and this record is the place
  that says so — the difference between "nobody called it" and "we decided not to".
- The i18n keys that already promised the panel batch now have controls behind them.

## Verification

- `acceptance_e2e002_test.go` — the nine-step walk, PRD §19's clauses asserted individually.
- Frontend: `drama.ts` wrapper tests against a mocked binding boundary; the storyboard view's tests
  for gate-blocked disabling, the batch request's parameter passing, the approve-conflict path and
  subscriber cleanup.
- Playwright: the batch entry renders and is disabled without a core (the full chain's evidence is
  the Go walk, matching how this repository splits the two suites).
- Mutations: the traceability assertion, the chapter-size assertion and the job read-back each get
  mutations and the survivors are reported.
- The full gate: `sh scripts/verify.sh`.

## References

- `docs/implementation/project-progress-and-remaining-tasks-2026-09-23.md` — the handoff whose P0-1
  this package is.
- `docs/implementation/STATUS.md` section 0n2 — where the ten zero-caller bindings were found.
- `docs/implementation/plans/plan-wp13-asset-production-ui.md` — the survey this record's facts come
  from, with file:line references.
- `PRD.md:1765-1778` (AC-E2E-002), `PRD.md:647-747` (FR-070, FR-080), `docs/ACCEPTANCE.md:440-468`
  (AC-BOARD-003, AC-ASSET-002).
- ADR-0013 (the production stage vocabulary and the panel's candidate source).
