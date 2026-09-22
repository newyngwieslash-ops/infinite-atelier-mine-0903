import io

p = "docs/implementation/STATUS.md"
s = io.open(p, encoding="utf-8").read()

header_old = '''> Last updated: 2026-09-20
> Product: Infinite Atelier Core + Drama Production Pack
> Current work package: **WP-07 — Agent Runtime、Skill、Workflow 与 Quality Gate**
> Status: **COMPLETE for the 16 ROADMAP items, with one acceptance item marked PARTIAL and two scope notes in section 0h** (the three-layer agent runtime with its registry, tool table and per-call ACL, the skill manifest loader and its version binding, JSON Schema validation against each agent's own contract with section 14.3's single repair round, the workflow engine with PRD FR-100's ten stage policies and the FIX/REDO budget read from the audit trail, run/message/tool-call records, cancellation, the deterministic Mock LLM of section 18.3, the Agent Center section, the basic Recent-only Memory Port, and the two built-in skill packs as skeletons with no business content). WP-01 through WP-06 remain COMPLETE for their recorded scopes; section 0h states exactly what this package delivered, the nine defects implementation found in code that had already shipped, and what remains open.'''

header_new = '''> Last updated: 2026-09-21
> Product: Infinite Atelier Core + Drama Production Pack
> Current work package: **WP-08 — ScriptAgent：骨架、策略与剧本**
> Status: **COMPLETE for the 15 ROADMAP items, with the two acceptance items and three verification limits named in section 0i.** The Script Agent layer is built end to end: the three execution stages with their supervisors and the decision agent, eight real skill documents, the whole-content structure write with explicit ceilings, field locks enforced at the write path for all three version families, the version diff, the canvas projection, the FIX loop that reads a user's findings back from the decision row, the stage→agent map the registry's last-segment heuristic cannot derive, the per-layer model policy, the desktop bindings, and the Script UI. Section 0i states what this package delivered, the defects TWO INDEPENDENT REVIEWS found in it, and — plainly — the three things its tests do not cover. WP-01 through WP-07 remain COMPLETE for their recorded scopes.'''

assert header_old in s, "header"
s = s.replace(header_old, header_new, 1)

section = '''
# 0i. WP-08 result: the script pipeline, the locks and the stated stage map (2026-09-21)

## Scope completed

- **Status: COMPLETE for the 15 ROADMAP items.** Two acceptance items are PARTIAL and
  three verification limits are named at the end of this section; neither is cosmetic.

- **Domain**: `internal/domain/script/structure.go` and `draft.go` — `ScriptStructure`
  with its gapless-ordinal validation and explicit ceilings (200 scenes, 500 lines and
  200 shots per scene), the `LockableField` vocabulary PER FAMILY, `FieldLock`,
  `StrategyEventLink`, and the `ScriptStructureDraft` shape a model is given, which
  states no identifier, no ordinal and no duration.

- **Diff**: `internal/domain/script/diff.go` — pure functions over two versions,
  positional by ordinal, with the lock state carried on every item.

- **Migration**: `000017_script_field_locks.sql` — one lock table for all three version
  families, with `version_id` deliberately NOT a foreign key (three families, three
  tables) so the write path is what keeps it honest.

- **Migration 000008's two link tables gained writers**: a skeleton's selected events
  and a strategy's per-event treatments are written WITH the version they belong to, in
  one transaction, because §7.4 and §7.5 make those sets part of what the artifacts ARE.

- **Service**: whole-version writes, lock enforcement for all three families at the
  WRITE PATH, the event-link reads, the reference-existence check (§17's 「引用存在性」,
  which the schema cannot make — the citation columns have no foreign key), the version
  histories, `familyOfVersion`, and the two upstream approvals.

- **Pipeline**: `internal/application/scriptpipeline` — the stated stage→executor and
  stage→supervisor map, the workflow-state rendering, the FIX read-back from the
  decision row, the user's gate (which approves the ARTIFACT), the manual-edit path, and
  the run→version walk.

- **Runtime**: two prompt layers (`lockedRefs`, `fixIssueIds`) that the
  execution-request schema had carried since WP-07 with nothing populating them, and
  `Engine.Transition`.

- **Tools**: `script.create_script_structure` and `script.read_script_structure`, plus
  event-link fields on the two upstream create tools; `scripts/gen-tool-schemas.mjs`
  regenerated, with `--check` in the gate.

- **Mock**: the three script-stage branches, each asking for the write that stage OWES.

- **Skills**: EIGHT real documents (`skills/script/**`), replacing WP-05's generated
  skeletons. `scripts/gen-skill-packs.mjs` now writes a manifest always and a document
  only when MISSING — the line that makes the documents possible, since running it again
  would otherwise erase them.

- **Model policy**: §13 resolved per LAYER (`agent_wiring.choice`), with the table the
  schema already carried. FR-140's per-STAGE keys are DEFERRED (ADR-0012 §5).

- **UI**: the Script section renders the three stages, the version histories, the
  content, the locks, the diff, the projection and the gate.

## Defects found, by two independent reviews

The first review audited the implementation against the specification; the second ran
148 mutations and reported 35 survivors. Both were adversarial and both found real work.
Everything below is FIXED unless stated.

**The first review:**

1. **The Script UI was dead code.** `script-view.tsx` defined the section and nothing
   imported it; `sections.tsx` still exported its WP-05 namesake, so every WP-08
   capability was in a file no route rendered. Neither `tsc` nor the i18n spec could see
   it. ROADMAP scope item 12 was UNMET while the document describing it existed.
2. **The panel passed a script id where a workflow run id belongs** — a different table.
3. **Nothing creates an episode's workflow run**, so the first stage a user started
   would have failed on a foreign key.
4. **Three of the four things the panel claimed not to guess** never reached the
   service: no skeleton, no strategy, no events — and the state renderer OMITS an empty
   field, so a strategy stage would have been told nothing and handed nothing to decide
   about.
5. **`ItemChange.Locked` could never be true.** Declared with a comment about showing
   what the model was not allowed to touch, and set at none of its three construction
   sites.
6. **`scripts/gen-tool-schemas.mjs --check` reported PASS without checking** — the
   comment named a leftover check and the function returned one line later.
7. **`familyOfVersion` reported a storage failure as a not-found.**
8. **Two comments named callers that do not exist** (`LockedFieldsOf`,
   `ContentIsFrozen`), and one named an assertion that was a `t.Log` pair.
9. **`testdata/canary-drama/bad-script.json` was read by nothing** — five named faults
   whose only assertion was that the file existed.

**The second review:**

10. **The whole revision path was at 0% coverage** — `locksFor`, `versionOfDecision`,
    `runsForStage`, `revisionContext`, `StartRevision`, `ManualEdit`,
    `writeUserVersion`, `assertEpisodeInProject`, and `issuesFromOutcome`'s severe
    branch. `FixFromStageRunID` was set by NO test anywhere, so AC-SCRIPT-002's own
    scenario had never been driven end to end. `scriptpipeline` went from 19.9% to
    44.1%.
11. **Two assertions compared the code with itself.** `TestEveryStageArtifactTypeIsAWrite
    ToolTarget` compared the stage map against a literal in the same file; the stage-map
    test skipped the supervisor for the ONE stage the map exists to state. Both are
    replaced by tests that read `schemas/agent/tools/KEYS.txt` and
    `skills/script/manifest.json`.
12. **One survivor hid behind a test that looked like it covered it**:
    `MissingStoryEntityIDs` with a hard-coded project id passed, because the only entity
    assertion asked about another project's row from this one.

Each of the eight highest-value mutations was re-applied and watched to fail, one at a
time. A mutation that fails to compile or apply proves nothing and was redone rather
than counted.

## Acceptance

- **AC-SCRIPT-001 — PASS.** Workflow created, an independent AgentRun per stage naming
  its skill version, versions written, the supervisor's report STORED (read back from
  the database), the user's PASS, and 「approved 唯一」 counted in SQL.
- **AC-SCRIPT-002 — PARTIAL.** 「锁定字段不变」 is enforced by REFUSAL at the write path
  for all three families, with the positive and negative cases asserted, and 「原版本保留」
  holds. Two clauses are PARTIAL, both recorded in ADR-0012: (a) a revision REUSES its
  attempt row (§8 there — the domain's machine says so, and the alternative is a second
  active attempt the schema forbids), so the attempt NUMBER does not increase; (b) the
  canary's FIX scenario pins a field on a COMPLETE skeleton rather than constructing the
  missing-ending-hook skeleton the criterion describes, so the pre-seeded fixture is
  absent while the enforcement it tests is not.
- **AC-SCRIPT-003 — PASS.** Five entity kinds as rows, ordinals unique AND gapless (with
  the negative case), source-event references checked by the service, the
  original-adaptation flag, the duration derived as a sum and compared against the
  episode's target, the version diff, and the canvas projection writing real node rows.

## Verification

- `go test ./... -count=1` — PASS (all packages).
- `go vet ./...` — PASS.
- `gofmt -l` — clean.
- `scripts/verify.sh` — PASS end to end: frontend typecheck, 40 frontend tests,
  production build, Playwright (24 passed, 1 skipped), MONOFORM build, Go tests, Go vet,
  security scans (483 files), and the FIVE generator `--check` steps (canary fixtures,
  hostile fixtures, tool schemas, skill packs, plus the tool key list).
- `wails build -s` — PASS, `build/bin/InfiniteAtelier.exe` produced.
- **`go test -race` is an ENVIRONMENT FAILURE on this host** (`cc1.exe: 64-bit mode not
  compiled in`). It is reported as such, never as a pass. The race-sensitive paths this
  package adds (the mock's counters, the lock table) are exercised by the
  single-threaded suite only.

## What this package does NOT cover — stated plainly

1. **The desktop binding's request mapping.** `RunScriptStage`, `ApplyScriptGate`,
   `LockScriptField`, `SaveScriptStructure`, `ManualEditScript` and their neighbours each
   have exactly ONE call site in tests, against a nil binding, asserting only a refusal.
   A mutation that dropped a field from any of them survived. The Go services behind them
   are covered; the twenty-line mapping between is not.
2. **The composition root's attachment.** No test drives `App.Startup` far enough to
   observe that the pipeline is attached to the drama binding. The same defect class
   (composed but unreachable) has now been fixed twice in this package, and the defence
   is a wiring test that drives `composeDrama` — which covers the projector but not the
   pipeline's attachment.
3. **The frontend's Script view.** The frontend runner bundles only
   `web/src/services/__tests__/*.spec.ts`, so no spec covers the component. The e2e suite
   exercises it only in the no-core state. What is unverified is which field the component
   sends where, and what it renders for a report with findings.

## Git and data safety

- Existing user changes preserved throughout: the plan file under `.zcode/plans/` and an
  untracked user document at the repository root were never touched.
- No secrets introduced; the security scan is clean.
- Migration 000017 is forward-only; migrations 000001-000016 were not modified.
- No provider was contacted. The mock model is the only model path in every test.
- 15 commits for this package, none pushed.
'''

anchor = "# 0h. WP-07 result: the agent runtime, skills and the quality gate (2026-09-20)"
assert anchor in s, "anchor"
s = s.replace(anchor, section.strip() + "\n\n" + anchor, 1)
io.open(p, "w", encoding="utf-8", newline="\n").write(s)
print("STATUS updated")
