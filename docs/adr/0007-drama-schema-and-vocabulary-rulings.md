# ADR-0007 Drama Schema and Vocabulary Rulings

- Status: Accepted (WP-05 scope)
- Date: 2026-09-18
- Deciders: Repository engineering under approved WP-05 plan
- Related work package: WP-05 (Drama domain model and studio UI shell)

## Context

WP-05 creates the drama domain: seven forward migrations (`000006`–`000012`) adding
the project configuration, the source/chapter and story-fact layer, episodes and
the script pipeline, the storyboard aggregate, the workflow and review tables, and
the artifact-staleness marker. `docs/DOMAIN_MODEL.md` §2–§11 defines the fields and
§18 the indexes; `PRD.md` defines the product behaviour.

Three problems had to be settled before a line of SQL could be written.

**1. The PRD and the domain model contradict each other in three places.**

- StageRun statuses. PRD FR-100 lists `pending, running, execution_succeeded,
  reviewing, passed, needs_fix, needs_redo, waiting_user, failed, cancelled`.
  DOMAIN_MODEL §11.2 lists `ready, running, executed, under_review, waiting_user,
  passed, failed, cancelled, superseded`. Four names differ and one
  (`superseded`) exists on only one side.
- Default quality-gate stage keys. PRD FR-100 lists ten
  (`chapter_event_extraction, story_skeleton, adaptation_strategy,
  script_generation, asset_gap_analysis, asset_generation, storyboard_table,
  storyboard_panel_generation, video_generation, final_episode`).
  `docs/AGENT_CONTRACTS.md` §10.1 lists ten different ones.
- UserGateDecision values. DOMAIN_MODEL §11.5 lists `pass, fix, redo, manual_edit,
  cancel, waive`. PRD §12.2 lists `approve, fix, redo, manual_edit, skip, cancel`.

**2. `asset_type` and two other WP-04 constraints contradict the specification.**
Migration `000004` shipped `assets.asset_type` with nine values, missing PRD
FR-050's `vehicle`, `creature`, `style_reference` and `derived_asset`;
`asset_versions.status` spelling one value `review` where §2.5 says
`under_review` and lacking `candidate` and `deprecated`; and `asset_files.role`
with four of §8.4's seven roles. SQLite cannot alter a CHECK constraint.

**3. The relation registry's source lists were wrong on the first attempt.**
PRD FR-130's example puts a character on the *from* side and a shot on the *to*
(`fromNodeId: character_1, toNodeId: shot_12`), while §10.4's prose reads the
other way ("Shot 使用的角色"), and §10.4 and FR-130 each list relation names the
other omits.

## Decision

### 1. Where the documents disagree, the PRD's spellings win, and every ruling is recorded

`AGENTS.md` §3 puts the PRD's required behaviour above the domain model, and
between them the PRD is the behavioural specification. So:

- `stage_runs.status` stores FR-100's spellings **plus `superseded`**, which
  §11.2's own invariant ("passed 后不可改写，只能 supersede") cannot be expressed
  without. The three remaining domain names are the same states under different
  spellings (`ready` = `pending`, `executed` = `execution_succeeded`,
  `under_review` = `reviewing`) and are **deliberately not accepted**: storing one
  state two ways would make every query guess which spelling it holds. The ruling
  and its reasoning are in `migrations/000011_workflow_review.sql` and mirrored in
  `internal/domain/workflow/workflow.go`; `TestStageStatusVocabularyMatchesMigration`
  asserts the rejected spellings stay rejected.
- `user_gate_decisions.decision` stores §12.2's set **plus `waive`**, which §15.3
  requires whenever a stale artifact is kept.
- The **stage-name vocabulary is left open** (`TEXT` with a length bound) and is
  deliberately not pinned to either list. Choosing between them is a question
  about the quality gate's behaviour, which belongs to WP-07, and pinning one
  would answer it by accident. `DocumentedStageNames` in the workflow domain
  carries FR-100's keys as a reference list, with a comment saying it is not a
  constraint.

### 2. `assets`, `asset_versions` and `asset_files` are rebuilt in `000009`

The three constraints are widened to the documented sets: asset types to the
union of §8.1's nine and FR-050's eight, version statuses to §2.5's eight, and
file roles to §8.4's seven. The rebuild also adds the §8.2 and §8.4 columns
`000004` never created (`parent_asset_version_id`, `variant_type`, `seed`,
`source_agent_run_id`, `created_by_id`, `change_reason`, `asset_files.id` and
`asset_files.ordinal`).

`style` and `style_reference` are kept as **two** entries: §8.1's `style` is a
generic style asset, FR-050's StyleReference is a production reference, and
collapsing them would lose the distinction the asset bible is built on.

The mechanism is staging copies plus children-first drops, not
`ALTER TABLE ... RENAME`, because a rename does not rename the implicit index
behind a UNIQUE constraint and the old autoindex name would collide with the one
the new table creates. `PRAGMA foreign_keys` is not toggled: it is a no-op inside
a transaction, which is why correctness comes from the drop order. The runner
applies the file in one transaction and the application snapshots the database
before any pending migration, so a failure leaves the previous database intact.

### 3. The relation registry is the union of §10.4 and FR-130, with `references` open between content artifacts

Each relation is registered with all seven §10.4 fields. The relation *names* are
the union, because each list has names the other lacks: FR-130 has
`uses_character`, `uses_location` and `uses_prop`; §10.4 has `appears_in`,
`located_in` and `uses_asset`.

`references` is allowed between content artifacts in **both** directions and
excluded from pointing at orchestration records. FR-130's own example reads
one way and §10.4's prose the other, so allowing only one would contradict one of
them; what is excluded is pointing at a workflow run or a stage run, which is
what `generated_by` and `reviewed_by` exist for.

### 4. `requires_version` requires a version on the versioned side only

§10.2's rule is "version_id 必须属于 entity": naming a version is only possible
for an entity that *has* versions. Requiring it of both endpoints made the
registry's flagship relations unsatisfiable — a shot has no version column
(§7.8), so `uses_character` from a shot to a costume version could never
validate, and neither could `first_frame_of` from a frame version to a shot. The
check now consults a whitelist of versioned entity types.

### 5. §18's index list is met, and the one vocabulary the parity guard cannot reach is covered separately

Every index §18 names for WP-05's tables exists. The guard in
`internal/infrastructure/database/vocabulary_parity_wp05_test.go` parses each
`CHECK (column IN (...))` out of the WP-05 migrations per table and compares it
against the Go list that must agree, so a value Go accepts and the schema rejects
fails at test time instead of inside a user's database. `canvas_edges.relation_type`
lives in the WP-04 migration and so is covered by
`TestWP04CanvasEdgeRelationVocabulary` in the same file.

Per-family approved-version uniqueness is enforced by a partial unique index in
each of the eight version families §2.5 defines, and
`TestWP05ApprovedVersionIsUnique` covers all eight — asserting each index exists,
is `UNIQUE` and is partial on the approved status. An earlier revision covered
three, and mutating the other five to plain indexes left the suite green.

## Consequences

- A reader comparing `domain/workflow` to DOMAIN_MODEL §11.2 will find statuses
  the model does not list. The migration header and the package doc both explain
  why, and the rejected spellings are asserted to stay rejected.
- ADR-0002 remains **Proposed** and is not superseded by this one; the migration
  contract it records (forward-only, unique numbering, checksum-pinned, one
  transaction per file, published files immutable) is what WP-05 followed.
- The rebuild of the asset tables is the highest-risk step in WP-05. Its evidence
  is the upgrade test that seeds WP-04-shaped rows first, the `foreign_key_check`
  assertion, the rollback test, the second-run idempotence test, and the
  `id.Valid` assertion over the UUIDv7 values the `asset_files` backfill mints in
  SQL.
- `assets.asset_type` now accepts thirteen values. A future reader wanting
  "the FR-050 production set" must filter on the eight production kinds rather
  than assume the column is that set.
- The quality-gate stage keys remain unpinned until WP-07. A WP-07 author must
  choose between the two documented lists and record it then.
