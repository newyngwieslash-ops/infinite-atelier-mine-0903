# ADR-0008 Staleness Propagation, Projection Commands, and Version Approval

- Status: Accepted (WP-05 scope)
- Date: 2026-09-18
- Deciders: Repository engineering under approved WP-05 plan
- Related work package: WP-05 (Drama domain model and studio UI shell)

## Context

WP-05's roadmap scope names four things whose shape was not fixed by any
document, and getting them wrong would either overreach into WP-06/07/09's work
or leave an acceptance bullet unreachable.

**1. "stale 传播基础".** DOMAIN_MODEL §15.1 lists eight triggers, §15.2 gives a
ten-step arrow diagram and classifies the consequence as `breaking`,
`review_required` or `informational`, and §15.3 requires that keeping a stale
artifact be recorded as a `UserGateDecision` with a reason. What none of them
say is *which* artifacts a given change implicates, or how the classification
maps onto them. The roadmap puts "stale 传播基础" in WP-05 and the full
Impact Analyzer in WP-07/10.

**2. Canvas projections.** AC-CANVAS-001 requires "创建 Script Scene 后创建
Canvas Node", "Node 有 entity refs", "修改实体后 Node 更新", "移除 Node 不删除
Scene" and "删除 Scene 显示影响". DOMAIN_MODEL §16 names a
`CreateCanvasProjection` command. None of it existed: nodes carried entity
reference *columns*, and nothing in the product ever wrote them.

**3. "required ref 删除被阻止".** AC-CANVAS-002 lists it and PRD FR-130 repeats
it as "删除领域实体时列出所有引用并阻止破坏性删除". `canvas_edges.required` was
stored and never read.

**4. "approved 唯一" and the approval switch.** §2.5 requires one approved version
per parent, superseding the previous on a new approval, and §8.2 requires an
impact analysis before an asset approval switch. WP-04's `ApproveVersion`
refused an approval without an explicit acknowledgement precisely because the
shot and scene model it would analyse did not exist yet.

## Decision

### 1. Propagation walks the dependency graph, and severity is decided against the change

`internal/domain/staleness` holds the §15.2 chain as an ordered list for
reporting, and a **separate dependency graph** (`dependsOn`) as the thing that
actually decides propagation. The graph's edges are not aspirational: each one
corresponds to a column the schema enforces (a chapter holds
`source_document_version_id`, a story event holds `chapter_id`, a scene holds
`script_version_id` and `source_story_event_id`, and so on), and
`TestDirectDependentsAreTheSchemaEdges` names the column per edge.

The separation is the substance. A linear "next item in the chain" reading was
tried first and was wrong: it classified `chapter → story_event` as
*informational* even though `story_events.chapter_id` is a direct reference, and
PRD FR-030's acceptance explicitly requires the opposite ("删除或修改章节后，
受影响事实被标记为待复核"). Severity therefore follows the **dependency kind**:

- a direct consumer — an artifact holding a reference to the changed row — is
  `review_required`, which §15.2 glosses as "允许保留但必须重新审核";
- a transitive consumer is `informational`, because the change reaches it
  through intermediates and those going stale is what carries the signal.
  Marking the whole transitive closure `review_required` would make one chapter
  edit demand a re-review of every shot and panel in the project, which the
  specification does not ask for and which users would waive wholesale;
- `breaking` is reserved for a caller that knows a specific artifact is
  load-bearing. Nothing infers it.

`PropagateFrom` walks breadth-first from the changed artifact, resolves each hop
to concrete rows through a one-method port, skips rows belonging to another
project, de-duplicates by `(type, id)`, and writes every mark in one call so a
propagation records all of its findings or none. It is bounded by depth and by
mark count and reports `Truncated` rather than silently stopping.

Severity is classified against **the artifact that changed**, never against the
hop the walk took. Mutating this to a per-hop classification fails two
independent tests, which is how the rule is protected.

`orchestration types` (workflow runs, stage runs, canvas nodes) are valid mark
targets but are not reachable from a content change by following references: a
workflow run records that something executed rather than being derived from a
document, so it is marked directly by the code that knows why.

### 2. Re-review is enforced once, in the version vocabulary

§15.2's "must be re-reviewed" and §2.5's approval rules are the same rule seen
from two directions, so it lives in one place: `versioning.CanApprove` refuses
`stale → approved`, making the route back to approved run through
`under_review`. The staleness domain deliberately does **not** restate it; an
earlier duplicate was removed because two copies of one rule drift and the
second copy was never executed.

### 3. Projections are commands, and a projection never writes its entity

`CreateCanvasProjection` creates a node for a domain entity, or moves and
relabels the node that entity already has so re-running a workflow does not
accumulate duplicates. The entity reference is validated against the registry's
vocabulary and both halves are required, because the schema's CHECK rejects half
a reference and a half reference is indistinguishable from a decorative node.

`RemoveCanvasProjection` removes the node and leaves the entity — §10.2's
"删除 projection 默认不删除 entity". `FindEntityReferences` lists an entity's
projections and the required edges attached to them, which is what "删除 Scene
显示影响" and FR-130's "列出所有引用" both ask for. It reads only; refusing the
delete is the caller's decision, because whether a projected entity may be
removed is a domain question the canvas layer cannot answer.

### 4. A required reference blocks a delete, and a false "nothing depends" is refused

`DeleteNodes` refuses the **whole batch** when a required edge touches any node
being deleted, and the refusal names the blocking edges. Refusing the batch
rather than deleting the unblocked part is deliberate: a partial delete leaves
the canvas different from what the caller asked for. A failed lookup of the
edges is treated as a refusal rather than as "nothing depends on it", because
the second reading would let a destructive delete through on a read error.

Edges that are not required are deleted with their node, which the schema's
cascade already does.

### 5. Approval supersedes the previous version, and impact is computed rather than assumed

`ApproveVersion` supersedes the version it replaces **before** approving the new
one, so the schema's partial unique index is never asked to hold two approved
rows at once. The two status writes are separate statements; a failure between
them leaves the pre-switch state, which is retryable.

`ApprovalImpactOf` performs the analysis §8.2 requires by listing the consumers
of the version being replaced and separating out the required ones — the list
PRD FR-050 calls for ("替换批准版本时，系统列出受影响的分镜和镜头"). The
caller still has to acknowledge it, so the service never claims a check it did
not run.

## Consequences

- The staleness graph is a second place, besides the schema, that records what
  depends on what. It is kept honest by a test naming the column behind each
  edge, and a cycle test.
- WP-07 owns the analyzer that decides whether a *specific* edit is breaking
  versus informational, and the regeneration workflows. WP-05 provides the graph,
  the severity rule, the marks and the waiver — which is what "基础" means here.
- A propagation stops at `MaxPropagationDepth` edges or `MaxPropagationMarks`
  rows and reports `Truncated`. A caller that needs the whole closure can
  propagate again from a marked artifact.
- The canvas projection commands live in the projects service because they are
  canvas operations, not drama-aggregate operations. A caller wanting "project
  this scene" calls into the project service, which is the same place the canvas
  itself is served from.
- `FindEntityReferences` is read-only by design. A domain delete command that
  consults it and refuses on a non-empty `RequiredBlockers` belongs to the
  aggregate being deleted, and WP-06/08/09 introduce those deletes.
- Asset lineage (§8.5) and usage (§8.6) now have repositories and commands, which
  closes the two scope items that were previously tables without writers.
