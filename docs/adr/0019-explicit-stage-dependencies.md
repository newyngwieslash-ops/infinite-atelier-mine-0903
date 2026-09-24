# ADR-0019 Explicit Stage Dependencies: A Declared Graph, Checked Before the Attempt Exists

- Status: Accepted (WP-15 scope)
- Date: 2026-09-24
- Deciders: Repository engineering, resolving handoff P1-3's product question
- Related work package: WP-15 (the P1 backlog), item 3

## Context

`PRD.md:817` lists 「显式阶段依赖」 among FR-100's necessary capabilities, and `PRD.md:860` makes it
an acceptance criterion: 「状态迁移不允许跳过未满足依赖的阶段」.

**Neither was implemented.** Verified by the WP-15 survey:

- `Layer.Stages()` returns each layer's stages "in pipeline order" and had **no non-test caller at
  all** — its only consumers were the two packages' own tests.
- `Engine.StartStage` checked four things before creating an attempt: the engine is composed, the
  stage name is non-empty and short enough, the stage has no active attempt, and its newest attempt
  is not `passed`. **Nothing read another stage's state**, although `Engine.Load` had already read
  every stage of the run into the `RunState` it receives.
- `stagepipeline.RunStage` checked that the layer serves the stage and that a FIX names a decision.
  It did not check what came before.
- The engine's own file header claimed "WHICH STAGES MAY RUN NOW. A stage's dependencies must be
  satisfied." — a comment describing code that did not exist.

Dependencies were enforced **per artifact at the point of use**: `CheckStoryboardGate` refuses a
batch without an approved gap report, `assets/gap.go` refuses a report read without one, the media
document services refuse without an approved script or board, and `domain/script` requires both
upstream version ids on a `ScriptVersion`. That is real enforcement, and it is why the product works
— but it is not the criterion's shape, and it leaves the gap the criterion names open: a stage can be
STARTED with nothing behind it.

Handoff P1-3 recorded this as a **product decision** with two options, and it is the reason this ADR
exists rather than a bare commit: (a) accept per-artifact enforcement as the design and reword the
PRD, or (b) build the declared graph. This record chooses (b).

## Decision drivers

1. The PRD asks for 「显式」 — explicit. Inferring a predecessor from a list's position would make
   the dependency an accident of ordering, and it would drift the first time the list moved for an
   unrelated reason (a display order, say).
2. A refusal must be actionable. The caller's next step is either to run the missing stage or to
   understand why the pipeline stopped, so the message names the stages that are missing.
3. A gate written too strictly is worse than none: it breaks work that is fine. The graph therefore
   has to be derived from what each stage actually READS, not from a gate list's presentation order.
4. The engine must keep enforcing what is its own — one active attempt per stage, and no restart of a
   passed one — while the LAYER owns the graph, because `Engine.StartStage` is handed a stage name
   and knows nothing about which pipeline declared it.

## Options considered

- **Infer predecessors from `Stages()` order** (stage *n* requires stage *n−1*). Rejected: it
  conflates a sequence with a graph, and the two differ here — `asset_gap_analysis` does not read the
  plan, and `storyboard_table` does not read the gap report. An inferred linear chain would refuse
  both, which is the "too strict" failure driver 3 names. **This rejection was measured, not
  argued**: the first implementation of this ADR declared the linear chain, and the AC-E2E-002 walk
  and two WP-10 acceptance tests failed immediately — all three drive a stage the chain would
  forbid, and all three are legitimate.
- **Enforce in `Engine.StartStage`.** Rejected: the graph is the layer's knowledge and the engine
  serves a bare stage name. It would also make the engine's port surface carry a concept its callers
  cannot answer.
- **Enforce in `workflow.Service.CreateStage`.** Rejected for the same reason, plus one more: the
  workflow service is the persistence layer for attempts, and giving it pipeline vocabulary would
  invert the dependency direction AGENTS section 7.2 fixes.
- **Reject the stage at the GATE (`ApplyUserGate`) instead of at its start.** Rejected: the
  criterion is about 状态迁移 and a stage that starts is a state. Refusing only at the end would let
  an hour of model work run against a stage whose inputs were never approved.
- **Accept per-artifact enforcement and reword the PRD** (handoff option (a)). Rejected: the
  per-artifact checks are valuable and stay, but they are downstream guards on USE. The criterion
  asks for a guard on START, and a user can start any stage from the studio's stage buttons — which
  is exactly where an unmet dependency currently produces work rather than a refusal.

## Decision

**Ruling 1 — a layer DECLARES its graph.** `Layer.DependsOn(stage) []Stage` returns the stages that
must have passed before `stage` may start. An empty list is a real answer: a layer's first stage
depends on nothing.

**Ruling 2 — the graph is derived from what each stage READS, not from anyone's list order.** The
production layer's declarations name the artifact each stage loads, and the ADR's Context records why
the linear reading was rejected — with the test failures that rejected it.

**Ruling 3 — the check happens in `stagepipeline.RunStage`, before the attempt exists.** The same
position the revision read uses, and for the same reason: an attempt created and then refused is a
stage in play that nothing can run, which a caller cannot distinguish from a slow model.

**Ruling 4 — the refusal names the missing stages.** It is the caller's next action.

**Ruling 5 — "passed" means ANY attempt of that stage has passed**, not the newest one. A stage that
passed and was later superseded by a revision still produced the approved artifact its successor
reads — `IsStageTerminal` refuses to call `passed` final for exactly this reason — so a gate that
insisted on the newest attempt would refuse a stage whose input is sitting there approved.

**Ruling 6 — the state read is a named port.** `StageStateReader` with one method, satisfied by
`*agentruntime.Engine` in production. It exists so the gate can be exercised without a runtime, a
database and a model; the gate's entire input is "which stages have passed".

**Ruling 7 — the gate is fail-closed on an unwired reader.** A build whose reader is missing refuses
rather than admits, because "I could not check" is not "they are satisfied".

**Ruling 8 — the script chain is enforced within its own layer, and the two layers do not name each
other's stages.** `script_generation` requires the skeleton and the strategy; `director_plan` and
`asset_gap_analysis` declare nothing, because the layer cannot name a script stage it does not drive.
The script's prerequisite for a plan is enforced where the plan is written: the stage needs an
approved script version to cite, and `StateFields.ScriptVersionID` is empty without one.

## Consequences

- **Three real stage-skips were found and fixed by building this**, which is the strongest evidence
  that the criterion was open: the AC-E2E-002 walk drove `asset_gap_analysis` without a plan, and two
  WP-10 acceptance tests boarded from a plan that had never left `waiting_user`. All three are
  fixtures rather than product code, and all three now walk the chain the way the chain is meant to
  be walked (supervise the plan, then gate it, then board).
- A stage with an unmet dependency now fails at its START with a message naming what is missing,
  instead of producing an artifact nobody asked for.
- The per-artifact checks remain, and they remain the guard on USE. The two are complementary: this
  one stops a stage from starting, and those stop a command from acting on absent inputs.
- **`Layer.DependsOn` is now a method every layer must implement**, so a third layer cannot be added
  without stating its graph. That is the point.
- The engine's file-header comment claimed a capability that did not exist; it now describes the
  boundary correctly (the layer owns the graph, the engine owns the attempt rules).

## Verification

- `dependency_gate_test.go` — four cases, one per way the gate can be wrong: a dependency that has
  not passed (seven statuses, each refused with the stage named), a dependency that HAS passed
  (admitted), two dependencies where only one has passed (refused, naming only the missing one), and
  a stage declaring nothing (admitted). Plus the superseded-reading test for Ruling 5.
- The three acceptance walks above, which failed when the graph was too strict and pass when it is
  derived from what the stages read.
- Mutations over the gate, the `Passed` predicate and both layers' declarations.

## References

- `PRD.md:817,860` (FR-100), `docs/AGENT_CONTRACTS.md` section 10.1's stage list.
- `docs/implementation/TRACEABILITY.md`'s FR-100 row, which carried the "NOT DELIVERED" verdict this
  record resolves.
- `docs/implementation/project-progress-and-remaining-tasks-2026-09-23.md` P1 item 3.
- ADR-0011 (the agent runtime and its stage keys), ADR-0012 and ADR-0013 (the two layers).
