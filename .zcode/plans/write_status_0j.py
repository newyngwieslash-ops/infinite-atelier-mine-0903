import io

p = 'docs/implementation/STATUS.md'
s = io.open(p, encoding='utf-8').read()

SECTION = '''# 0j. WP-09 result: the production pipeline, the gap report and the MONOFORM bridge (2026-09-22)

## Scope completed

- **Status: COMPLETE for the 14 ROADMAP items.** Three acceptance clauses are PARTIAL and
  two verification limits are named at the end of this section; each is stated rather than
  glossed.

- **The mechanism, extracted**: `internal/application/stagepipeline/` holds the stage
  machine — the attempt, the FIX read-back, the gate's ordering, the manual edit's two-step
  — and `scriptpipeline` became a LAYER that states a stage map, an approval, a lock read
  and a manual-edit writer. `productionpipeline` is the second layer. WP-08's own reviews
  found four defects shaped "two places disagree about the same fact", so a second copy of
  the gate was not written.

- **Stage vocabulary**: `director_plan` added to `DocumentedStageNames` and to the engine's
  policy table with section 10.1's own settings (`supervision: conditional`, `userGate:
  required`). The reference list is now eleven where FR-100 lists ten, and its test says why.

- **Migration** `000018_production.sql`: `storyboard_items` gains FR-070's
  `first_frame_description`, `last_frame_description` and `video_motion_description`;
  `asset_gap_reports` and `asset_gap_items` are new, with an approved-per-episode unique
  index. Forward-only, no published content touched.

- **The gap report**: `internal/domain/asset/gap.go` states the one rule that matters —
  `satisfied` must name the asset that satisfies it and `missing` must not — and
  `application/assets/gap.go` implements create/list/get/approve plus
  `UnresolvedRequiredItems`, which REFUSES when no report is approved rather than reporting
  an empty gap list.

- **Provenance completed**: `asset.Version` gained the five columns migration 000009 had
  always had and the mapper dropped (`parent_asset_version_id`, `variant_type`, `seed`,
  `source_agent_run_id`, `created_by_id`), and `AttachJobResult` turns a succeeded job into a
  CANDIDATE version — a status nothing in the build wrote before, so a panel could never
  have had a candidate to approve.

- **The impact edge**: `storyboard_panel_version` now consumes the asset version it
  approved, so section 15.1's asset-version approval switch is a trigger that reaches
  something. It propagates from the version being REPLACED — the first version propagated
  from the newly approved one, and the wiring test caught it.

- **The batch**: `productionpipeline` with `CheckStoryboardGate`, `RunImageBatch`,
  `CollectBatchResults` and `ApproveCandidate`, reachable from the desktop binding and from
  the Storyboard Table section.

- **Tools**: `script.read_shots` (the stage had no way to list the shots it was boarding),
  `asset.create_gap_report`, `asset.read_gap_report`, and
  `storyboard.create_storyboard_version` now writes its ROWS.

- **Mocks**: `KindMockImage` with a real PNG renderer, five production execution branches
  and the supervision split in the text mock, so the whole pipeline runs in CI without a
  paid provider.

- **Skills**: all nine production documents are real prose. The assertion that required them
  to be skeletons is REVERSED, and now requires both packs to be prose.

- **MONOFORM**: a versioned envelope with origin, nonce, schema and size validation in one
  place (`web/src/services/desktop/monoform-bridge.ts`), `open_shot` and `shot_updated` as
  the two directions FR-060 asks for, the iframe's permission frame narrowed from
  `camera; microphone; clipboard-write; download; fullscreen` to `clipboard-write;
  fullscreen`, and the studio's output rebuilt.

- **The two sections**: Director and Storyboard Table are real, with the plan history, the
  previs studio opened from a shot, the board's rows, the single-row editor, the supervisor,
  the gate, and the asset versions drawer with the impact analysis shown BEFORE the switch.

## Defects found, by two independent reviews

A specification review and a quality review with 53 mutations ran against this package. The
findings were real and each is fixed in a commit of its own.

**The specification review found four blockers:**

1. **The batch had no caller.** `RunImageBatch` compiled, was tested, and nothing composed
   it — no binding, no composition root, no UI. Four of AC-BOARD-003's seven clauses and four
   ROADMAP items were unreachable from any build. Fixed by the bindings, the attachment, and
   the two commands it was missing (`CollectBatchResults`, `ApproveCandidate`).
2. **No candidate approval existed at all.** Fixed by `ApproveCandidate`, which delegates to
   the storyboard service so section 9.5's rule lives in one place.
3. **AC-ASSET-002's asset usage had no producing path.** The job-to-version command wrote the
   version and its files and no usage, so the candidate set section 9.5 reads was empty.
   The collection now writes it.
4. **AC-BOARD-002's supervisor clause was met by inert data.** A test read a fixture's own
   `mustLocateAt` and asserted it agreed with itself; no supervisor ever ran against the
   board. Findings now name the row, and two tests assert it — one faulted, one consistent.

**The quality review returned thirteen survivors.** The worst cluster was that NO TEST EVER
ATTACHED A PRODUCTION PIPELINE TO THE BINDING, so four routing mutations survived — one of
them the same `shot_ids` defect this package had already fixed once, whose regression test
lived on the wrong side of the layer boundary. `routing_wp09_test.go` closes it, and the
batch's concurrency limit and the collection's prompt/seed are now asserted too.

**Defects the fixes uncovered, which had shipped earlier:**

- **Every review's evidence was silently dropped since WP-08.** The review-report schema
  defines `evidence` as an array; `IssuesFromOutcome` read a field named `evidenceJson`,
  which the schema does not define. The struct decoded happily and the field was always
  empty, so section 7.6's rule that evidence points at what was read was unsatisfiable in
  principle.
- **A stage with no supervisor could never reach its gate.** The mechanism parked every
  attempt at `reviewing`, where `ApplyGate` refuses it, so the pipeline stopped at the first
  unsupervised stage — `asset_analysis`, which section 10.1 marks `supervision: false`.

## Acceptance

| Criterion | Status | Evidence |
|---|---|---|
| AC-ASSET-001 | **PASS** | `TestACAsset001TwoCandidatesThenAVersionSwitch` — all six clauses in order, including the last one read back |
| AC-ASSET-002 | **PARTIAL** | `TestAttachJobResultRecordsEveryProvenanceFact` covers physical file, hash, job, provider/model, prompt, parent refs and agent/run. **The asset-usage fact is asserted by `TestACCollectedJobBecomesACandidateVersion`, which is a different command**: the job-to-version write itself records no usage |
| AC-BOARD-001 | **PARTIAL** | Twelve shots, FR-070's fields field-by-field, the supervisor and the gate's block are asserted. **Generating from an APPROVED script is not ENFORCED**: the board stage's tool checks that the cited version exists and that the rows' shots belong to it, but nothing refuses a board built from an unapproved script |
| AC-BOARD-002 | **PASS** | `TestTheBadStoryboardFixtureNamesTheFaultTheCriterionDescribes`, `TestTheSupervisorLocatesTheRowAtFault` (the supervisor locates the row, with evidence naming two references), `TestTheSingleRowFixChangesOnlyThatRow` (every other row byte-identical, revision included) |
| AC-BOARD-003 | **PARTIAL** | Two candidates a shot, the concurrency limit, the collection, the approval and the restart-idempotency are asserted. **Cancel and retry are the job core's commands and are reachable by job id, but no test drives them from a batch**: the batch exposes no batch-scoped cancel or retry |

## Verification

- `go test ./... -count=1` — PASS (all packages)
- `go vet ./... .` — PASS
- `gofmt -l` — clean
- `git diff --check` — PASS
- `node scripts/security-scan.mjs` — PASS (513 files, 1 audited dynamic-execution exception, 4 audited legacy direct-call files)
- `gen-canary-fixture.mjs --check` — PASS (5 fixtures)
- `gen-malicious-fixtures.mjs --check` — PASS (11 fixtures)
- `gen-tool-schemas.mjs --check` — PASS
- `gen-skill-packs.mjs --check` — PASS
- `npm run typecheck` (web) — PASS
- `npm test` (web) — PASS, 52 tests
- `npm run build` (web) — PASS
- `npm run build:monoform` — PASS, output synced to `web/public/monoform/`
- `npx playwright test` — PASS, 24 passed / 1 skipped
- `wails build -s` — PASS; the bindings regenerated
- **`go test -race` — ENVIRONMENT FAILURE, not a pass.** The host's C toolchain reports
  `cc1.exe: 64-bit mode not compiled in`, so no race-aware run is possible here. Every
  concurrency-relevant test in this package runs without the detector.

## Mutation testing

53 mutations over the package's new code, run by an independent reviewer with a harness that
asserts each anchor is present before substituting and hash-checks its restore. Thirteen
survived; the clusters that matter are fixed and re-run:

- the binding's stage routing — 4 survivors, now 4 mutations killed by `routing_wp09_test.go`
- the batch's concurrency limit — 1 survivor, now killed by `TestTheBatchSubmitsNoMoreAtOnceThanItsLimit`
- the candidate ceiling — 1 survivor, now killed by `TestTheBatchRefusesMoreCandidatesThanTheCeiling`
- the collection's prompt and seed — 2 survivors, now killed by `TestACCollectedCandidateKeepsItsPromptAndSeed`

The remaining survivors are named in the review's own report; they are either equivalent
mutations (reordering two disjoint stage-set checks changes nothing, so no test can kill it)
or guards on test-only code.

## What this package does NOT cover, stated plainly

1. **Generating from an approved script is not enforced** (AC-BOARD-001, above). The
   materials are approved in the walk, but nothing REFUSES a board built from an unapproved
   script. The fix is a status check in the board stage's write path, which requires deciding
   whether a draft script may be boarded experimentally — a product question rather than a
   defect.
2. **A batch exposes no scoped cancel or retry** (AC-BOARD-003, above). The job core's
   commands work by id and a user can cancel the jobs they see; a batch-scoped command would
   need the batch to BE a row the database holds, which ruling 5 of ADR-0013 explains it
   deliberately is not.
3. **`go test -race` cannot run on this host.** Stated above; it is an environment failure and
   is not reported as a pass.
4. **Per-stage model policy remains per-layer**, which the user chose when the plan was set.
5. **The MONOFORM bridge is the BASIC form.** It opens a shot, reports a camera and exports a
   frame; FR-060's full two-way scene synchronisation would need the studio's object model to
   travel as well, and is not built.

## Git and data safety

- existing user changes preserved: **yes** — the user's brand rename and the untracked scratch
  files under `.zcode/plans/` were not touched or committed
- secrets found/introduced: **none** — the image mock is registered by no production
  composition (ADR-0013 ruling 6) and no credential is read on the test path
- migrations/backups: `000018_production.sql` is forward-only; no published migration was
  edited; no database was deleted or reset
- Automatic commit/push/stash/reset/clean: **none** (the commits were requested by the user)

---

# 0i. WP-08 result: the script pipeline, the locks and the stated stage map (2026-09-21)'''

anchor = '# 0i. WP-08 result: the script pipeline, the locks and the stated stage map (2026-09-21)'
assert anchor in s, "0i anchor"
s = s.replace(anchor, SECTION, 1)

old_header = '''> Last updated: 2026-09-21
> Product: Infinite Atelier Core + Drama Production Pack
> Current work package: **WP-08'''
new_header = '''> Last updated: 2026-09-22
> Product: Infinite Atelier Core + Drama Production Pack
> Current work package: **WP-09'''
assert old_header in s, "header anchor"
s = s.replace(old_header, new_header, 1)
io.open(p, 'w', encoding='utf-8').write(s)
print("STATUS section 0j written")
