import io

p = 'docs/implementation/TRACEABILITY.md'
s = io.open(p, encoding='utf-8').read()

ROWS = [
 (
  '| FR-050 | Production assets | The asset bible exists: all thirteen asset types (the FR-050 production kinds plus section 8.1\'s general ones), versions with the documented status set and per-asset approved-version uniqueness, file roles, lineage and usage, and the impact analysis an approval switch requires. Generation and the bible UI are WP-09. | Partial | WP-05 owns the model; WP-09 owns generation and the bible UI; AC-ASSET group |',
  '| FR-050 | Production assets | **Delivered for WP-09\'s scope.** The bible gained what it was missing: the five provenance columns migration 000009 had always had and the mapper dropped (`parent_asset_version_id`, `variant_type`, `seed`, `source_agent_run_id`, `created_by_id`), a job-to-CANDIDATE-version command (`AttachJobResult`) — the status nothing in the build wrote before, so a panel could never have had a candidate — `usage` and `lineage` write paths with bindings, the impact analysis exposed as a READ (`GetApprovalImpact`) so the acknowledgement `ApproveVersion` requires can be given meaningfully, and the propagation that makes an approval switch reach the storyboard panel holding the replaced version. The versioned Asset Gap Report exists with its own domain rule, service, migration and tools. **Per-stage model policy, the full bible UI and real generation against a paid provider are not here**: the first was deferred by the user\'s choice, the UI lists assets with their versions, usages and approval, and generation runs against a configured provider or the deterministic mock in tests. | **Delivered (WP-09 scope), gaps named** | WP-05 built the model; WP-09 built the provenance, the generation path and the gap report; AC-ASSET-001 PASS, AC-ASSET-002 PARTIAL (STATUS 0j) |',
 ),
 (
  '| FR-060 | Director planning and MONOFORM | MONOFORM previs exists, but its bridge is untyped and unrelated to durable Shots. | Partial | WP-09; AC-BOARD group |',
  '| FR-060 | Director planning and MONOFORM | **Delivered for WP-09\'s scope, as the BASIC bridge the user chose.** `director_plan` is a stage with section 10.1\'s settings and a version carrying all ten of the FR\'s fields; the MONOFORM bridge is versioned and validated — a source field, a schema version, a per-mount nonce, an ORIGIN check and a size bound, in that order, in one place — with `open_shot` carrying a shot\'s framing and camera INTO the studio and `shot_updated` reporting the camera back to the plan\'s `shot_overrides_json`. The iframe\'s permission frame narrowed from `camera; microphone; clipboard-write; download; fullscreen` to `clipboard-write; fullscreen`, because nothing in the studio opens a camera, a microphone or a download prompt. **NOT built: the full two-way scene synchronisation** the FR describes — sending staging, scene references and saving preview snapshots would need the studio\'s object model to travel as well. A shot opens, a camera returns, a frame exports. | **Delivered (WP-09 scope), basic bridge only** | WP-09; ADR-0013 ruling 7; AC-BOARD group |',
 ),
 (
  '| FR-070 | Storyboard table, panels, and images | Generic canvas/media nodes lack structured storyboard entities and bidirectional projection. | Gap | WP-09; AC-BOARD group |',
  '| FR-070 | Storyboard table, panels, and images | **Delivered for WP-09\'s scope.** The five storyboard entities exist with their repository and both approve commands; the table\'s write tool now writes ROWS with every per-shot field the FR lists, including the three migration 000018 added (`first_frame_description`, `last_frame_description`, `video_motion_description`) — WP-05 read those onto the Shot, and they are SHOOTING decisions rather than script facts, so there was nowhere to store them. A stage with no way to list the shots it was boarding gained `script.read_shots`. The panel\'s candidates come from `asset_usages(consumer_type=\'storyboard_panel\')` plus the job that produced them rather than from a new table (ADR-0013 ruling 4); the batch submits one job per candidate per shot within a concurrency bound, collects them into candidate versions, and approves one as the panel\'s canonical image through section 9.5\'s own rule. The supervisor locates a fault at a specific ROW with evidence naming two references, and a single-row fix leaves every other row byte-identical. **Bidirectional table-canvas sync is not built**: shots are PROJECTED to the canvas (item 11) and the table reads and writes the database, but dragging a canvas node does not reorder the board. | **Delivered (WP-09 scope), one clause NOT built** | WP-09; AC-BOARD-001 PARTIAL, AC-BOARD-002 PASS, AC-BOARD-003 PARTIAL (STATUS 0j) |',
 ),
 (
  '| FR-100 | Durable Workflow engine | WP-05\'s tables and state machines plus WP-07\'s engine: PRD FR-100\'s ten stage policies with their supervision and gate settings, `StartStage` (with the partial unique index making "at most one active attempt" a constraint), `ApplySupervision` (PASS to the gate or to passed, FIX/REDO to a revision), the FIX budget counted from the **audit trail** because a revision reuses the attempt row, `ApplyGate` for every gate decision, and cancellation recorded as `cancelled` rather than `failed`. A run and a stage transition emit their section 17 domain events. Checkpoints and restart recovery remain open — the state is durable and readable, but nothing resumes a run automatically after a restart. | Partial | WP-05 built the structure; WP-07 built the engine (ADR-0011 section 4 for the revision reading); recovery is deferred and named; AC-WORKFLOW group |',
  '| FR-100 | Durable Workflow engine | WP-05\'s tables and state machines plus WP-07\'s engine: the stage policies with their supervision and gate settings, `StartStage` (with the partial unique index making "at most one active attempt" a constraint), `ApplySupervision` (PASS to the gate or to passed, FIX/REDO to a revision), the FIX budget counted from the **audit trail** because a revision reuses the attempt row, `ApplyGate` for every gate decision, and cancellation recorded as `cancelled` rather than `failed`. A run and a stage transition emit their section 17 domain events. WP-09 added the eleventh policy (`director_plan`, section 10.1\'s settings) and, in the shared mechanism, the rule that a stage whose layer names NO supervisor parks at `waiting_user` rather than at `reviewing` — without it the pipeline stopped at the first unsupervised stage, which `asset_analysis` is. Checkpoints and restart recovery remain open. | Partial | WP-05 built the structure; WP-07 built the engine (ADR-0011 section 4); WP-09 added `director_plan` and the unsupervised-stage parking; recovery is deferred and named; AC-WORKFLOW group |',
 ),
]

for old, new in ROWS:
    assert old in s, "row not found: " + old[:60]
    s = s.replace(old, new, 1)

# The contract rows the package also moved.
CONTRACTS = [
 (
  '| NFR? |',
  '| NFR? |',
 ),
]
io.open(p, 'w', encoding='utf-8').write(s)
print("traceability rows updated")
