# ADR-0011 The Agent Runtime, its Tool Table, and the Stage-Key Rulings

- Status: Accepted (WP-07 scope)
- Date: 2026-09-20
- Deciders: Repository engineering under the approved WP-07 plan
- Related work package: WP-07 (Agent Runtime、Skill、Workflow 与 Quality Gate)

## Context

WP-07 had to build a three-layer agent runtime, a skill loader, a tool registry with
an authorization matrix, a workflow engine with quality gates, a deterministic mock
model and a basic memory port — sixteen scope items over `docs/ROADMAP.md:374-407`.
Four places in the specification disagreed with itself or with the code WP-05 and
WP-06 had already shipped. Each is recorded here so the next package can disagree
with a decision rather than with an accident.

Three were resolved while planning and are stated first; the fourth and the
findings that follow it came out of implementation and a canary run, and are stated
with the evidence that produced them.

## Decision

### 1. The stage key list: ten, from PRD FR-100, not eleven from AGENT_CONTRACTS §10.1

PRD FR-100's default quality gate lists ten stages. AGENT_CONTRACTS §10.1 lists
eleven, with `event_extraction` where the PRD has `chapter_event_extraction` and
splitting `storyboard_panel_generation` differently.

**Ruling.** The PRD's ten, which is what `agentruntime.StagePolicyFor` implements.

Three reasons, in order of weight:

1. Migration 000011 pinned `stage_runs`' status vocabulary from the same PRD, so the
   two documents already agree about the lifecycle a stage has. Taking the keys from
   the other document would leave the keys and their states sourced from two texts.
2. `chapter_event_extraction` is the name WP-06's delivered extraction path uses
   (its service, its schema and its manifest entry all say so). Renaming the stage
   to match §10.1 would have made the stage a different thing from the code that
   serves it.
3. WP-08/09/11's package boundaries line up with FR-100's list.

The eleventh key is a deliberate omission rather than an oversight: a stage the
engine has no policy for gets `SupervisionNone` and `UserGateRequired` (see
`StagePolicyFor`'s fallback), so a stage someone adds later stops for a person
rather than passing silently.

### 2. The manifest is JSON, not the YAML §4.1 shows

§4.1 writes `manifest.yaml` and the repository has no YAML dependency.

**Ruling.** `manifest.json`, with §4.2's fields exactly.

Adding a YAML parser for one file is a dependency for syntax nobody needs, and YAML's
alias mechanism is a real attack surface: a manifest is the file that grants an agent
its tools, and a format that can expand one node into many is a format that can be made
to expand past what a reviewer read. JSON with `DisallowUnknownFields` is stricter in
the direction that matters — a field the loader does not know is refused rather than
silently accepted.

The loader reads either name if a future pack ships YAML; today's built-in packs are
JSON and the generator writes them that way.

### 3. "At most one active attempt" is a partial unique index

Migration 000011 says the invariant "cannot be a plain unique index because attempts are
historical". That is true of a plain index and false of a partial one.

**Ruling.** `CREATE UNIQUE INDEX idx_stage_runs_single_active ON stage_runs(workflow_run_id, stage)
WHERE status IN (<the seven active statuses>)`, added by migration 000015 after a
deterministic reconciliation.

A probe against this SQLite (3.53.4) confirmed the index refuses a second active attempt
and permits a new one once the previous is terminal. The predicate's status list is
exactly the set `workflow.StageRun.IsActive` reports, so the constraint and the code
cannot disagree about which attempts are in play. The reconciliation closes any duplicate
an existing database already holds, keeping the highest attempt number and recording
`agent.multiple_active_attempts_reconciled` — refusing to migrate would leave a database
that cannot be opened, which is worse than a row whose status is corrected with a code
saying why.

### 4. A FIX or REDO reuses the attempt row

§10.2 says REDO creates "新版本 + 新 Attempt". The domain WP-05 already shipped says
otherwise in three places at once, and they agree with each other:

- `workflow.StageRun.IsActive` counts `needs_fix` and `needs_redo` as ACTIVE.
- The only edge out of `needs_fix`/`needs_redo` is `→ running` (and `→ cancelled`), so an
  attempt that is revised in place is the only reading the machine permits.
- The domain's own comment on `StageNeedsFix` says "revised in place".

**Ruling.** A revision reuses the attempt, and a NEW attempt is created only once the
previous one is terminal.

This reading has a consequence the implementation had to handle rather than assume:
the attempt COUNT cannot answer "how many revisions has this had", so a revision budget
keyed on it could never trip — an unbounded loop. The budget therefore reads the
workflow_events trail (`agentruntime.RevisionCounter`), which records every transition
into `needs_fix`/`needs_redo` against its stage run. The alternative reading — a new
attempt per revision — would need both `IsActive` and migration 000015's index changed,
and is recorded here as the way to disagree with this decision.

### 5. A tool call travels beside the validated document, not inside it

§7's output schemas have no field for a tool call, and every one of them declares
`additionalProperties: false`. The wire protocols agree: an OpenAI-compatible response
carries `tool_calls` as a SIBLING of `content`.

**Ruling.** `providers.TextToolCall` travels beside the document on `TextResult`, and the
runner reads the calls from the reply rather than parsing them out of the validated output.

This was a DEFECT before it was a ruling. The runner's first version parsed a `toolCalls`
member out of the validated output, and its own tests put one there — because those tests
used a validator that accepted anything. The deterministic mock's first real document
travelled through the path and the schema refused it immediately, which is how the
unreachable branch was found: the entire tool layer — the ACL, the budget, the denial
record — could never have run for any agent whose output was actually validated.

### 6. Two of §6.2's example tools are absent, not stubbed

`story.create_event_candidates` and `provider.submit_image_job` are named in §6.2's
example list and are NOT in this build's tool table.

**Ruling.** They are absent, and the reason is different for each.

- `story.create_event_candidates`: the extraction stage's write path is the EXTRACTION
  SERVICE, and §6.1's registry is not the only way to serve a stage. WP-06 built its seam
  for the runtime to plug into, and the runtime now implements that package's `Extractor`
  port while the service does the validating and the writing. A tool doing the same thing
  would be a second write path for one stage, and the two would disagree about which rows
  are candidates.
- `provider.submit_image_job`: media generation is a Job, not an agent tool. §19 says so
  outright ("媒体生成本身由 Job/Provider Service 执行，不让 LLM 阻塞等待大文件") and WP-11 owns it.

Both are absent from the generated schemas as well as from the table, so there is nothing
to register by accident. A test asserts the table and the generated key list agree.

### 7. The domain error taxonomy is not §7.7's wire vocabulary

Writing the provider classifier exposed a drift: `agent.ErrorCategory` used
`invalid_input|not_found|conflict|storage|security|unavailable|model|tool|cancelled`, while
`schemas/agent/agent-error.v1.json`'s enum is §7.7's list
(`configuration|input|model|tool|timeout|cancelled|security|storage|internal`) — the THIRD
instance of vocabulary drift in this repository, after the two `validation.go` records.

**Ruling.** The two sets stay different, and a mapping closes the gap.

They are different on purpose: the domain taxonomy distinguishes states the domain acts on
(a not-found, a conflict, "the runtime is not composed") while the wire form is what a
caller outside the process branches on. Merging them would lose the first. So
`ErrorCategory.Wire()` is the mapping, `agent.WireCategories()` is the closed set, and a
parity test asserts every domain category maps into it. The mappings that LOSE information
are asserted individually — `not_found` becomes `input`, `conflict` becomes `input`,
`unavailable` becomes `configuration` — because each is a decision a reviewer should have
to disagree with explicitly.

### 8. The extraction request carries a project id

WP-06's `extraction.Request` documented itself as carrying "no project identifier", and
WP-07 added `ProjectID`.

**Ruling.** Additive, and the earlier comment's intent is preserved.

That comment was about CAPABILITY: it forbids an extractor that can READ the project's
other chapters and facts, which is what would let it correlate across chapters. A project
id is the opposite — it is the scope a run must be filed under, and `agent_runs` has a
foreign key to `projects`, so a run without one cannot be created at all. The reader
already walked chapter → version → document and reported the project it found, so the value
is a fact about the chapter rather than anything the extractor supplies.

## Findings from implementation, with their evidence

These are recorded because each was a defect in code that had already been written and
reviewed, found by a test that did not exist when the code was.

1. **The tool path was unreachable** (§5 above). Found by the mock's first real reply.
2. **`workflow.Service` had no `GetRun`.** The engine's `StageTransitioner` needs it, so
   before this method the engine could only ever have been driven by a test double — the
   "interface with no real path" AGENTS §12 refuses. WP-07's own engine tests used one,
   which is why nothing failed. Found by writing the compile-time assertion that the REAL
   service satisfies the interface.
3. **Four more service reads were missing**: `script.GetScript`, `storyboard.GetStoryboard`,
   `storyboard.GetStoryboardItem`, and the getters for skeleton, strategy and script
   versions. Each is the hop a project-boundary check needs (a version names a script, a
   script names an episode, an episode names a project), so the checks the tools perform
   could not have been written without them. Found by the compiler while building the table.
4. **`ToolRequest` had no `AgentRunID`.** The write tools filled `SourceAgentRunID` with the
   STAGE id, and DOMAIN_MODEL §13.4 says the field names the author — a run superseded and
   re-run in the same stage is a different author. Found by the canary asserting the field
   against the run it had just made.
5. **`Runtime.finish` discarded the revision it incremented.** It took its record by value,
   so the increment was lost. Invisible while a run writes once, which is every path except
   one. Found by the canary.
6. **Refusals discarded the run id**, so a caller could not look up what happened. The
   canary's assertion "the run records the refusal" could not be written without it.
7. **The mock's tool-call scenario sent `{}`** for every tool, which the tool's own input
   schema refused — so a test of the write path was testing the failure path. Found by the
   canary, which saw "a tool this step needed did not complete".
8. **The mock's `SetScenario` kept its per-conversation counters**, so a test that ran one
   scenario and then another got a valid reply where it expected an invalid one: the second
   scenario's first call was counted as the first scenario's second. Found by the canary.
9. **The mock's tool-call document had no supervision branch**, so a supervisor received an
   execution-shaped document, which the review-report schema refused — and the ACL was
   therefore never reached. Found by the canary, which was asserting about a denial.

## Findings from the independent reviews

Two reviews ran against the delivered package — a specification review and a quality review with
mutation testing — and both found real defects in code that had already been written, tested and
reviewed once. They are recorded here because each is a lesson about the KIND of thing a test
suite of this shape misses.

1. **Tool arguments were never schema-validated.** Section 6.1's chain names the step and section
   20 lists its absence as release-blocking, and the schema path travelled into the prompt and was
   never applied to what came back. The reviewer found it by READING, not by mutating: the field
   appeared in exactly one place. The lesson is that a prompt layer and a validation step can be
   described by the same string and only one of them implemented.
2. **The runtime had no call site in a production build.** `composeAgents` built the
   runtime-backed extraction service and `app.go` never attached it. A compile-time assertion
   proved the port was SATISFIED, not that anything USED it — and ADR-0011's own text claimed the
   seam was closed. The lesson is that a `var _ Port = (*Impl)(nil)` line is evidence about a type,
   never about a build.
3. **`ToolRequest` had no agent-run id**, so a version's author was recorded as a stage id.
4. **`Runtime.finish` discarded the revision it incremented**, so a second write reused the first
   revision and the repository's guard reported a not-found.
5. **Refusals discarded the run id**, so a caller could not look up what happened — the canary
   could not be written without it.
6. **The answering model was never recorded**, though section 13 is specifically about the case
   where it differs from the requested one.
7. **The message bound counted runes against a byte limit**, so a long CJK reply was refused and
   the refusal discarded: a silent drop, on a field nothing asserted.
8. **Section 7.4's two artifact rules and section 7.6's critical rule did not exist.** Both are
   relations between fields, which JSON Schema cannot express — the same class as the
   `passed`/`severity` rule WP-06 left to the domain, and a reminder that a schema file is not a
   specification.
9. **The stage identity cross-check did not exist**, though two comments and a canary assertion
   claimed it did. The canary's assertion passed because the mock copied the value out of the
   prompt: a test whose fixture does the work the production code should be doing proves nothing
   about the production code.

Three findings stand out because they were about the TESTS rather than the code: the mock's
`SetScenario` kept its counters across a scenario change (so a test saw a success where it
expected a refusal); the mock's tool-call scenario sent `{}` (so the tool's own schema refused it
and a test of the write path tested the failure path); and the repair round's two-write sequence
could not be reached through `Run` at all, which is why the revision defect needed a test of
`finish`'s contract rather than of a run.

## Consequences

- The stage-key list is the PRD's, and a stage outside it stops for a person rather than
  passing silently.
- A revision budget is counted from the audit trail. A future package that adds a new
  revision path must record it as a transition into `needs_fix`/`needs_redo`, or the count
  will not see it.
- The tool table and its schemas are generated from one key list and compared by tests in
  both directions, so a tool cannot be registered without a schema or generated without a
  registration.
- `agent.Wire()` is the only place the two error vocabularies meet, and the parity test is
  what keeps a third taxonomy from appearing.
- The extraction path has ONE implementation in a production build, and it is the runtime.
  A build that fails to compose the agent stack refuses extraction with a reason rather than
  falling back to an adapter — which is the state WP-06 shipped deliberately.
