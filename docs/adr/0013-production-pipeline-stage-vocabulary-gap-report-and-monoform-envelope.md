# ADR-0013 The Production Pipeline: Stage Vocabulary, a Shared Mechanism, the Gap Report, the Batch, and the MONOFORM Envelope

- Status: Accepted (WP-09 scope)
- Date: 2026-09-22
- Deciders: Repository engineering under the approved WP-09 plan
- Related work package: WP-09 (ProductionAgent：资产、导演与分镜)

## Context

WP-09 had to build the Production Agent layer — the director plan, the asset gap
analysis, the asset generation plan, the storyboard table and the storyboard panel,
their supervisors, their skills and tools, the image batch and the panel approval, the
Director and Storyboard Table sections of the Studio, the canvas projection for shots,
and the MONOFORM bridge — over `docs/ROADMAP.md:455-492` and AC-ASSET-001/002 and
AC-BOARD-001/002/003 in `docs/ACCEPTANCE.md:414-468`.

Eleven places in the specification were silent, disagreed with itself, or disagreed with
the code WP-05 to WP-08 had shipped, and each needed a decision before work could
proceed. The user was asked to choose between the alternatives for three of them and
answered: a BASIC MONOFORM bridge, per-stage model policy deferred, and a versioned gap
report. The rest were implemented on the recorded recommendation, so each one below is a
**refutable ruling** rather than a user decision: a later package that disagrees should
say so here rather than work around it.

## Decision

### 1. `director_plan` becomes a stage name

`director_plan` is added to `workflow.DocumentedStageNames` and to
`agentruntime.defaultStagePolicies`, with AGENT_CONTRACTS section 10.1's own settings for
it: `supervision: conditional`, `userGate: required`.

**Ruling.** Section 10.1 CONFIGURES the stage and FR-060 requires the artifact, while
neither FR-100's list nor the engine's policy table named it. A stage the specification
describes but the vocabulary omits gets the DEFAULT policy, and the settings the document
gives it are silently lost — the failure mode is a stage that runs with the wrong
supervision and reports nothing wrong.

**Cost, stated.** `DocumentedStageNames` is now eleven entries where FR-100 lists ten, and
its own test says why. A reader comparing the two documents will find the extra entry and
has to read the reason rather than assume a typo.

### 2. The stage machine is one mechanism with two layers, not two pipelines

The attempt, the FIX read-back, the gate's ordering and the manual edit's two-step moved
from `application/scriptpipeline` to a new `application/stagepipeline`, and
`scriptpipeline` became a LAYER that supplies a stage map, an approval, a lock read and a
manual-edit writer. `productionpipeline` is the second layer.

**Ruling.** WP-08's own reviews found four defects whose shape was "two places disagree
about the same fact". A second copy of the gate would have been a fifth. The mechanism
knows nothing about scripts or storyboards; what each layer states is what only it can.

**Cost, stated.** WP-09 touched code WP-08's reviewers had already read and approved. The
tests that covered it moved with it — eighteen test functions before the split and twenty
after, each traced — and the extraction's own first version silently disabled the manual
edit's project-scope check, which a new test now pins. That is the cost, and it is
recorded rather than presented as free.

### 3. FR-070's three descriptions are storyboard item columns

Migration 000018 adds `first_frame_description`, `last_frame_description` and
`video_motion_description` to `storyboard_items`.

**Ruling.** WP-05's comment pushed these onto the Shot ("belong to the shot the storyboard
item cites"), and that reading is wrong for the reason the two things are: a Shot is a line
of the SCRIPT — what happens, in what order, said by whom — written before anybody decided
what the camera does. "The first frame shows X" is a SHOOTING decision, and until these
columns existed it had nowhere to live. FR-070's "每个镜头至少包含" is a MUST.

**Cost, stated.** The same three fields now exist in two vocabularies: a Shot draft carries
its own visual description, and the item carries the frame descriptions. A reader has to
keep the distinction in mind, which is why the domain's comment states it at the field.

### 4. A panel's candidates come from `asset_usages`, not a new table

There is no panel-candidate table. A candidate is an `asset_usages` row with
`consumer_type = 'storyboard_panel'` and the panel's item id, plus the
`asset_versions.generation_job_id` that produced it.

**Ruling.** Section 9.5 says the approved image is an asset version and must come from the
panel's candidates or an explicit link; migration 000009 already carries the consumer type
and the job column. A second table would be a second answer to "which images belong to this
panel", which is the shape four of this repository's reviews have already found.

**Cost, stated.** The candidate set is a QUERY rather than a stored list, so nothing
enforces at write time that a panel's candidates are images. A future package that wants
that constraint will need the table this ruling avoids.

### 5. The image batch is an orchestrator, not a stage

`productionpipeline.RunImageBatch` submits one job per candidate per shot, bounded by a
concurrency limit, and `CollectBatchResults` turns a succeeded job into a candidate
version. `storyboard_image` is NOT driven through the stage machine, though section 10.1
lists it.

**Ruling.** A stage runs one agent and produces one artifact to review; a batch submits N
jobs and then collects their results. Driving it through the mechanism would mean an
"attempt" that runs no model and a "candidate artifact" that is a set of job ids — a shape
the review report cannot describe and the gate cannot approve. The agent half is
`storyboard_panel_generation`, which writes the PROMPT; the batch executes it.

**Cost, stated.** `storyboard_image` therefore has no stage policy, no supervisor and no
gate of its own: a batch is approved by approving its RESULTS, one panel image at a time.
A user who wants to reject a whole batch rejects it by not approving anything.

### 6. `KindMockImage` is a third mock kind, and it is NOT registered in production

`provider.KindMockImage` joins `KindMockText` and `KindMockMedia`, reachable only through
the registry's kind switch and refused by `IsUserConfigurableKind` and by the
`provider_configs.kind` CHECK.

**Ruling.** AGENT_CONTRACTS section 18.3 forbids CI from calling a paid provider, and
AC-BOARD-003 is a test about several image jobs — two candidates a shot, a concurrency
limit, a cancellation, a retry. None of it can run against a real adapter in CI.

**Cost, stated, and this one was learned twice.** The mock is registered by NO production
composition, because it could never be returned: `ImagePortFor` dispatches on a config's
kind, and no config can carry `mock_image`. The first version of the fix registered it
anyway — an arm nothing can reach, which is the "interface with no real path" shape one
level down. A harness that wants the mock supplies it through the runner's `AdapterSource`
port, and a real build uses a configured `openai_compatible` or `gemini_compatible`
provider. The consequence is that the deterministic image path is reachable ONLY from a
test, and a production build has no offline image generation at all.

### 7. The MONOFORM envelope is versioned and origin-checked, and `export` keeps its shape

The bridge carries `{source, schemaVersion, nonce, type, ...}` in both directions. The host
validates the source, the version, the nonce, the ORIGIN and a size bound, in that order,
before reading anything in the payload. `open_shot` and `shot_updated` are new; `export`
keeps its `{kind, blob}` shape and gains the envelope.

**Ruling.** ARCHITECTURE section 17 and SECURITY section 12 require source, type and schema
validation, and the state WP-09 found was the opposite: a `postMessage(..., '*')` and an
acceptance rule that read a `source` field ANY frame in the page can set. `event.origin` is
the browser's own record and the part a sender cannot forge, so the checks are built on it
plus a nonce the host mints per panel mount.

**Cost, stated.** The `export` direction is a breaking change: an older studio build in
`web/public/monoform/` would have its messages refused by the new host, so the two must be
rebuilt together — which is why the studio's output is TRACKED in this repository and the
`build:monoform` script regenerates it. The `allow` attribute also shrank from
`camera; microphone; clipboard-write; download; fullscreen` to `clipboard-write;
fullscreen`, because nothing in the studio opens a camera, a microphone or a download
prompt. Fullscreen is kept: the studio has a maximise control, and removing it would break
a visible feature.

### 8. No event type is invented for the gap report; the decision's trace is stored instead

`asset_gap_reports` has an `approval_trace_id` column and writes NO `domain_events` row,
though every other approval in this build does.

**Ruling.** DOMAIN_MODEL section 17's event list is CLOSED — `event.IsValidType` enforces
it and the type's own comment says so — and it carries `AssetVersionCreated`,
`AssetVersionApproved`, `DirectorPlanApproved` and `StoryboardVersionApproved` but nothing
for a gap report. A consumer filtering the stream by type would have to know a vocabulary
the specification does not define, and the closed list exists precisely so that a typo is a
failure rather than a silent hole.

**Cost, stated.** The gap report's approval is governed where the OTHER user gates are:
section 10.1 gives `asset_analysis` a required user gate, so the decision is a
`UserGateDecided` row on the workflow's stream. `approval_trace_id` is the LINK, so a
reader can find the decision rather than match timestamps — but a consumer that filters
`domain_events` for "the gap report was approved" finds nothing, and has to join through
the workflow stream instead. A later package that adds the event to section 17 should
retrofit this column's readers.

## Consequences

**What this enables.** The production half of the pipeline has a real path from an
approved script to an approved storyboard image: composable pipelines, the five agent
stages, the batch with its gate, the candidate collection and the panel approval, the two
Studio sections, and the shot projection. AC-ASSET-001, AC-ASSET-002, AC-BOARD-001,
AC-BOARD-002 and AC-BOARD-003 each have an assertion.

**What is deferred, and to what.** The `storyboard_image` stage has no gate of its own
(ruling 5). No event type exists for the gap report (ruling 8). Per-stage model policy
remains per-layer, which the user chose. The MONOFORM bridge is the BASIC form: open a
shot, report a camera, export a frame — not the full two-way scene synchronisation FR-060
describes, which would need the studio's object model to travel as well.

**What a later package should watch.** Two rulings have a cost that shows up only in
operation: the candidate set is a query rather than a stored list (ruling 4), and the gap
report's approval is invisible to an event-stream filter (ruling 8). Both are stated where
a reader will meet them.
