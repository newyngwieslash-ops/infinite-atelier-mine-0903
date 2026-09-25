# ADR-0021 The Event Graph Drawing: What Is a Node, What Is an Edge, and What Is Not Drawn

Status: Accepted
Date: 2026-09-25
Work package: WP-17 (P3 item 18)
Supersedes: none
Related: ADR-0007 (the story fact layer's relation vocabulary), ADR-0013 (the production
pipeline), AGENT_CONTRACTS §11, PRD FR-030, DOMAIN_MODEL §6.5

## Context

PRD §16's v1.0 list carries 事件图谱可视化 — "event-graph VISUALISATION" — as an item because what
WP-06 shipped is a reading aid built from tables plus a small SVG. A reconnaissance pass established
three facts, each with evidence:

1. **THE GRAPH DREW NO EVENTS.** `buildGraph(entities, relations)` took two lists, and the component's
   `events` state was never passed to it. The section is 事件图谱, FR-030 lists `StoryEvent` first
   among the node types the fact layer supports, and every event was read, rendered in its own table,
   and absent from the drawing. A user looking at "the graph" saw characters connected to characters.
2. **PARTICIPATION HAD NO PROJECT-WIDE READ.** `participates_in` is in `story_relations`'s CHECK
   vocabulary and NOTHING WRITES IT: participation is stored in `story_event_participants`, a table
   with a role and a state before/after. The only read was per event. So drawing the edges between
   characters and the events they act in would have required one call per event — the N+1 shape this
   repository's reads avoid — and even then the fact was not in the graph's input at all.
3. **AN E2E ASSERTION HAD BEEN VACUOUS SINCE WP-06.** `web/e2e/studio.spec.ts` asserted that
   `studio-story-graph-gap` had a count of 0. That testid lived in `pages/studio/sections.tsx`; WP-06
   moved the section into `components/studio/story-graph-view.tsx` and deleted the notice, and the
   i18n strings went with it. From then on the assertion checked the absence of an element no file
   could produce — a negative assertion against a testid that cannot exist can never fail. Eleven work
   packages passed with a test that asserted nothing.

## Decision

**1. A node is an entity OR an event, and the two id spaces are kept apart by a prefix.**
`entityNodeId` and `eventNodeId` wrap the raw ids, because the two come from separate tables and a raw
uuid collision would silently draw two nodes as one — and because a relation's endpoint has to be
translated to the right space, which the prefix makes total.

**2. Participation is an edge, built from `story_event_participants`, labelled by ROLE.**
The label is the row's own role ("actor", "witness") rather than a generic `participates_in`, because
the role is the fact the row holds and a generic verb would throw away which part the character
played. The edge id is the (event, entity, role) TRIPLE: a participation has no id column, and keying
on the pair alone would collapse a character who is both the actor and the owner in one event.

**3. A project-wide participation read exists, and its status filter applies to the EVENT.**
`ListProjectEventParticipants` joins through `story_events` to reach the project. Two filters have to
agree with the events list the same view reads — the status filter and the soft delete — because a
view that filtered one way and not the other would draw edges to nodes it did not show, and the
drawing code DROPS those silently, so the bug would read as "the graph is missing an edge" rather
than as a filter mismatch. Both filters therefore live in the SQL, in one place.

**4. An edge whose endpoint is not a node is dropped AND COUNTED, and the count is on the screen.**
The four lists are read separately, so an edge can name a node this view did not build. Drawing it
would be a line to nowhere; dropping it silently would be worse — the picture would say "these two
facts are unrelated" when the truth is that this view did not draw the relation. `droppedEdges`
travels with the graph and the count line reports it.

**5. The layout is a deterministic grid, not a ring and not a force simulation.**
The ring it replaces was compact for a handful of nodes and unreadable for a project: twenty events
and thirty entities put a hundred edges through the middle with the labels overlapping at the top. A
grid gives every node a cell, so two nodes can never land on the same point at any count. A force
simulation would produce a prettier picture and a NON-DETERMINISTIC one: the same graph would render
differently on two loads, which makes a screenshot comparison useless and a user's spatial memory of
their own story worthless. The canvas is where a user arranges things; this is a reading aid.

**6. Events and entities are distinguishable by SHAPE as well as colour** — an event is a rounded
rect, an entity a circle — so a reader with a colour deficiency still sees which dots are events.

**7. The vacuous e2e assertion is REPLACED BY NOTHING, with the reason written where it stood.**
It cannot be repaired from that surface: `pages/studio/project.tsx` returns the no-core notice BEFORE
the section switch runs, so `StoryGraphSection` is never mounted in browser mode and no assertion
there can observe anything inside it. The facts that replaced the old gap ARE asserted — the section
reports itself available and the shell says why it cannot render — and the section's own states are
covered by the desktop build's tests. **What would be worse than deleting it is keeping a look-alike**:
an assertion that some new testid is absent would be exactly as vacuous for exactly the same reason.

**8. The layout decisions live in a pure module** (`web/src/services/desktop/story-graph.ts`), not in
the JSX, because the frontend suite is `node:test` with no DOM: a decision that only exists inside a
component cannot be asserted at all. This is the same ruling ADR-0017 made for the panel chain.

## Consequences

- The section is now an EVENT graph: entities and events as nodes, relations and participations as
  edges, with participation dashed so "this character is IN this event" is distinguishable from
  "these two entities are related".
- The graph has a real backend read for the first time, with the same filters as the events list.
- `StoryEventParticipantDTO` gained a second reader, and the desktop double in
  `internal/desktop/drama_binding_test.go` had to implement the new port method — which is the port
  doing its job rather than an inconvenience.
- The component's `graph` memo now takes all four reads, so a change that dropped one would fail the
  type check rather than silently producing a thinner picture.

## Alternatives rejected

**Draw relations only, and treat participation as "not a relation".** Rejected: it is the state the
package exists to fix. `participates_in` is in the vocabulary the extraction contract names, so a user
who reads FR-030 expects to see it; a view that showed every event as an isolated dot while the data
held the connections would be a picture arguing with its own tables.

**A force-directed layout.** Rejected for the determinism reason above, and because it needs physics
tuning that would be a second thing to review. The non-goal is stated: this is not the canvas.

**Draw the dropped edges to a placeholder node.** Rejected as the worst of the three options: it
would put a node on screen that is not a fact, and a reader would have to learn what it means. The
count is the honest instrument.

**Read participations per event.** Rejected: N+1 on the first render of a page, and it would also
have to apply the same filter in the caller, in a second place that could disagree.

**Keep the undirected `studio-story-graph-gap` assertion and point it at the new file.** Rejected
because the element does not exist there either, so the repaired assertion would be vacuous in a new
way. The ruling is stated in the test file rather than silently dropped, because a reader who greps
for the old testid should find why it is gone.

## Verification

- `go test ./... -count=1` — PASS, 57 packages. `npm test` — PASS, 113 tests (11 new).
- `sh scripts/verify.sh` — PASS, exit 0 (25 Playwright, security scans over 657 files, all fixture
  checks, SBOM, Wails production build).
- **13 mutations, 13/13 killed** (6 Go over the new read and its service guards, 7 over the frontend
  layout module), each restored byte-identically.
- **Two things the mutation run itself got wrong and the run exposed**: the first m3 was a no-op
  (`AND 1 = 1`), which proves nothing; and the first m6 removed the project check from
  `ListStoryEntities` rather than from the method under test, because the anchor matched four
  methods. Both were redone with method-specific anchors and both then killed. A mutation that
  cannot apply is not a survivor, and the distinction is recorded here because the runner cannot
  make it.
