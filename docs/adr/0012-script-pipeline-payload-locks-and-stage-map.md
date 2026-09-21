# ADR-0012 The Script Pipeline: One Payload, Field Locks, Derived Duration, and the Stated Stage Map

- Status: Accepted (WP-08 scope)
- Date: 2026-09-21
- Deciders: Repository engineering under the approved WP-08 plan
- Related work package: WP-08 (ScriptAgent：骨架、策略与剧本)

## Context

WP-08 had to build the Script Agent layer — three execution stages, three supervisors,
a decision agent, their skills and tools, the Script UI, the version diff, the field
locks and the canvas projection — over `docs/ROADMAP.md:419-455`. Eleven places in the
specification were silent, disagreed with itself, or disagreed with the code WP-05 to
WP-07 had shipped.

Six were resolved while planning and are the ones a later package is most likely to
want to disagree with, so they are stated first and each names what it COSTS. Five more
came out of implementation and two independent reviews, and are stated with the evidence
that produced them.

The user was asked to choose between the alternatives for the first six and did not
answer. Per the plan's own procedure they were implemented on the recorded
recommendation, so each one is a **refutable ruling** rather than a user decision: a
later package that disagrees should say so here rather than work around it.

## Decision

### 1. One whole-content payload per script version, not three commands

A script version's scenes, their dialogue lines and their shots are written by ONE tool
call, `script.create_script_structure`, in ONE transaction, with explicit ceilings
(200 scenes, 500 lines and 200 shots per scene) that REFUSE rather than truncate.

**Ruling.** One payload. A version is an immutable artifact, and writing it in three
commands would leave it observably half-written between them — with a duration that
could not be summed until the last piece arrived. One payload makes both properties true
by construction: a version either exists whole or does not exist.

**Cost, stated.** A very long episode needs a different transport (a FileRef plus a
streaming write), which is out of scope here. The ceilings are the guardrail: a payload
over one is refused with a message naming the bound, so the failure is visible rather
than a partial artifact.

### 2. Field locks in a new table, enforced at the write path

`script_version_field_locks (version_id, field, locked_by, created_at)`, added by
migration 000017, alongside the `dialogue_lines.locked` column migration 000008 already
carried. `version_id` deliberately has NO foreign key, because the three version
families live in three tables and the lock vocabulary has to be validated against the
right one.

**Ruling.** A table plus write-path enforcement. AC-SCRIPT-002's scenario is a FIX on a
story skeleton whose ending hook is missing, so the lock it turns on is a skeleton
FIELD — not a line. `dialogue_lines.locked` therefore covers only part of the
requirement. Three families share one lock table rather than three columns, because a
lock is the same fact about all of them.

**Enforcement is a refusal at the write path, not a request in a prompt.** A model that
rewrote a pinned field fails the stage; nothing about the check depends on the model
having understood anything.

**Cost, stated.** The write path is what keeps the table honest, since no constraint can.
`familyOfVersion` answers "which family is this version" with three lookups, and a lock
row whose version was deleted is inert rather than cleaned up.

### 3. Duration is derived, never accepted

The version's `estimated_duration_seconds` is the SUM of its scenes' own estimates, and
the tool has no field for it.

**Ruling.** Derived. AGENT_CONTRACTS §17 puts 「时长求和」 in the code's column, and a
declared total and a computed one can disagree while only one of them is checkable.

**Cost, stated.** A model cannot state a total that its scenes do not support, which is
the point — and it means a stage that wants to hit a target duration must plan its
scenes' lengths.

### 4. An explicit stage → agent map, not the last-segment heuristic

`scriptpipeline` states which executor and which supervisor serve each stage.
`Registry.SupervisionFor` matches a stage name against an agent key's LAST SEGMENT,
which works for every stage in the inventory except `script_generation`: its supervisor
is `script.supervision.script`, and `script` is not the stage's name.

**Ruling.** A stated map. The gap was known — WP-07 recorded it — and the fix is not to
widen the heuristic, because for this pair there is nothing to match on. The mapping
cannot be derived; it can only be stated.

The map is the COMPLETE statement and the registry's lookup is the partial one, which is
why the test asserts agreement wherever the registry CAN answer and asserts the
DISAGREEMENT is exactly the one gap. If a future manifest makes the registry resolve
`script_generation`'s supervisor, that test fails and sends the reader here to decide
whether the entry is still load-bearing.

### 5. Model policy per LAYER, not per stage

`agent_wiring.choice` resolves a provider in three steps: a named provider, then the
project's policy for the agent's LAYER, then the `default` row, then the first enabled
provider.

**Ruling.** By layer, which is what §13 states ("不同层可使用不同模型"). The policy table
`project_provider_policies` has carried a `layer` CHECK since migration 000006, so no
migration was needed.

**Deferred, and recorded rather than silently dropped.** PRD FR-140's per-STAGE keys
(`script_execution_model` and its siblings) are not implemented. A stage is not a layer,
and the specification gives those keys no shape — no table, no column, no vocabulary —
so writing one now would be inventing a design. The deferred surface is: a stage cannot
currently be pointed at a different model than its layer.

**A policy naming a DISABLED provider is refused rather than skipped.** Falling through
would send this project's prompts to a provider its owner did not choose, which is the
one thing a per-layer policy is about.

### 6. `scriptpipeline` owns the stage-driving orchestration

A new package drives the three script stages; `agentruntime` stays generic.

**Ruling.** The orchestration lives in the script layer, which is what makes ROADMAP's
「Script Agent 是独立层」 a property rather than a claim: the layer is runnable and
testable on its own, without a Production stage beside it and without the runtime
knowing what a story skeleton is.

**Cost, stated.** Two packages rather than one. `agentruntime` gained exactly three
things for this: the `lockedRefs` and `fixIssueIds` prompt layers (both of which the
execution-request SCHEMA already carried and nothing populated), and `Engine.Transition`
— a single entry point for the "the run finished, now review it" move, which reads its
own revision so a caller cannot pass a stale one.

## Findings from implementation and review

### 7. A gate decision approves an ARTIFACT, and that is a separate act from moving the stage

The canary found this. `ApplyUserGate` moved the stage to `passed` and never approved
the version, so a workflow could report every stage passed while the project had nothing
to approve — and every later stage reads the APPROVED version. AC-SCRIPT-001's
「approved 唯一」 is about an artifact's status, not a stage's.

**Ruling.** An approving decision (`approve`, `manual_edit`, `skip`) names the version it
puts in force, and the pipeline approves the artifact BEFORE moving the stage. That
order, so a failure leaves a state a user can retry from rather than a passed stage with
an unapproved version.

### 8. A revision reuses its attempt row, which deviates from AC-SCRIPT-002's 「新 StageRun attempt」

ADR-0011 ruling #4 already recorded that `needs_fix` is "an attempt the review wants
revised in place, reusing the attempt row", and the domain's machine permits
`needs_fix → running` rather than creating a new row. AC-SCRIPT-002 asks for a new
attempt.

**Ruling.** The domain's reuse stands, because the alternative is a second active
attempt for one stage — which migration 000015's partial unique index forbids, and which
would make a stage's revision history a list of attempts rather than one attempt's
revisions. The criterion's intent (a re-run, a new version, the old version preserved,
the locks honoured) is satisfied by the reuse plus `revisionCount`.

**What this costs, stated plainly.** A stage whose FIX is re-run **reuses the same
`stage_runs` row**, so the attempt NUMBER does not increase. `RevisionCount` is what
tracks the revisions, and a caller reading "attempt" for "how many times has this been
tried" reads the wrong field. Two independent reviews raised this; it is a deviation
from the criterion's letter, and a later package that wants the letter should change the
domain's machine and this ADR together.

### 9. The lock carries forward by POSITION across a revision

A revision's dialogue lines inherit their `locked` flags from the base version by
(scene ordinal, line ordinal).

**Ruling.** Positional, because it is the only relation two versions of a script are
guaranteed to share: there is no line identity across versions, and content matching
would be a guess presented as a fact. A revision that reorders its scenes moves the pins
with the positions, which is the same reading `DiffScriptStructure` reports.

### 10. A script version's citation of an event is checked by the SERVICE, not the schema

`scenes.source_story_event_id` and `dialogue_lines.source_story_event_id` are TEXT
columns with no foreign key, because a citation is provenance and outlives the row it
names.

**Ruling.** The service refuses a citation of an event the project does not have, and
the refusal NAMES the missing identifiers so a model can act rather than guess. Same for
the two link tables' `story_event_id`, for the same reason.

### 11. The frontend's `script` section is REAL but not exhaustively verified

The section renders the three stages, the version histories, the review, the diff and
the gate. Its TESTS are the gap: the frontend runner bundles only
`web/src/services/__tests__/*.spec.ts`, so no spec covers the Script view, and the
e2e suite exercises it only in the no-core state.

**Ruling.** Recorded as a known limit rather than left implicit. The Go-side binding
tests cover the same commands' arguments and refusals, and the canary covers the chain
end to end through the pipeline; what is unverified is the component's own behaviour —
which field it sends where, and what it renders for a report with findings.

## Consequences

- `go test ./... -count=1` and `go vet ./...` are clean; `scripts/verify.sh` runs the
  whole available gate, including three generator `--check` steps this package added.
- Two independent reviews ran. The first found a dead UI component, a wrong identifier
  (a script id where a workflow run id belongs), three missing state fields, a boolean
  that could never be true, a generator check that reported PASS without checking, and
  three comments describing behaviour the code did not have. The second ran 148 mutations
  and found the whole revision path at zero coverage, two assertions that compared the
  code with itself, and one survivor hiding behind a test that looked like it covered it.
  All are fixed; the survivors that remain are named in `STATUS.md`.
- The honest remaining bound: the desktop binding's request mapping, the composition
  root's attachment, and the frontend's Script view have no test that observes their
  content. Each is named where it lives.
