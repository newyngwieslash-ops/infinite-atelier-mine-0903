import type { desktop } from "@/wailsjs/go/models";

/**
 * The story graph's layout decisions, as pure functions (ADR-0021).
 *
 * They live here rather than inside the JSX for the reason this repository keeps extracting modules
 * like `panel-chain.ts` and `job-selection`: the suite is `node:test` with no DOM, so a decision that
 * only exists inside a component cannot be asserted at all — and these are the decisions worth
 * asserting, because each one is a place the drawing can silently show the wrong thing:
 *
 *  - WHAT IS A NODE decides whether the picture is an event graph or an entity graph. The section is
 *    called 事件图谱 and FR-030 lists StoryEvent among the node types; the first version of the view
 *    drew entities only, so the events were read, listed in a table, and absent from the drawing.
 *  - THE DROP RULE decides what happens to an edge whose endpoint is not a node. It must be dropped
 *    rather than drawn to nowhere, and the drop must be COUNTED, because a silently missing edge
 *    reads as "these facts are unrelated" when the truth is "this view did not draw them".
 *  - THE LAYOUT decides whether two nodes can land on the same point. A pure grid cannot collide; a
 *    ring can when the counts differ, so the ring is what this build had and the collision is what
 *    the grid fixes.
 *  - THE PARTICIPATION EDGES decide whether a character and the events they act in are connected at
 *    all. Participation is NOT a `story_relations` row (`participates_in` is in the vocabulary and
 *    nothing writes it) — it lives in `story_event_participants`, so a view that read relations alone
 *    would draw every event as an isolated dot.
 */

/** What a node is. The kind decides its colour and which id space its `id` belongs to. */
export type GraphNodeKind = "entity" | "event";

export type GraphNode = {
    id: string;
    label: string;
    /** The entity's type or the event's type, shown in the tooltip. */
    detail: string;
    kind: GraphNodeKind;
    /** The fact's status, so a candidate is distinguishable from an accepted fact. */
    status: string;
};

export type GraphEdge = {
    id: string;
    from: string;
    to: string;
    label: string;
    /** True when the edge came from participation rather than from a stored relation. */
    participation: boolean;
};

export type Graph = {
    nodes: GraphNode[];
    edges: GraphEdge[];
    /** How many edges were DROPPED because an endpoint was not a node. */
    droppedEdges: number;
};

/** The option lists the view offers, and the ids they filter on. */
export function entityNodeId(entityId: string): string {
    return `entity:${entityId}`;
}

export function eventNodeId(eventId: string): string {
    return `event:${eventId}`;
}

/**
 * buildGraph turns the four reads into nodes and edges the SVG can draw.
 *
 * # Why events are nodes now
 *
 * The section is 事件图谱 — an EVENT graph — and FR-030 lists `StoryEvent` first among the entity
 * types the fact layer supports. The first version of this view built its nodes from the entity list
 * alone, so every event was read, shown in its own table, and absent from the drawing: a user looking
 * at "the graph" saw characters connected to characters and could not see which events they act in.
 *
 * # Why participation is an edge rather than a relation
 *
 * `participates_in` is in the relation vocabulary and NOTHING WRITES IT: participation is stored in
 * `story_event_participants`, which is a table with a role and a state before/after. So the edges
 * from an entity to an event are built from the participants read, and their label is the ROLE
 * ("actor", "witness"), because that is the fact the row holds and a generic "participates_in" would
 * throw away which part the character played.
 *
 * # Why a dropped edge is COUNTED
 *
 * The four lists are read separately, so an edge can name a node this view did not build — a relation
 * to a soft-deleted entity, an event filtered out by status. Drawing it would be a line to nowhere.
 * Dropping it silently would be worse: the picture would say "these two facts are unrelated" when the
 * truth is that this view did not draw the relation. The count travels with the graph and the view
 * reports it.
 */
export type StoryGraphInput = {
    entities: desktop.StoryEntityDTO[];
    events: desktop.StoryEventDTO[];
    relations: desktop.StoryRelationDTO[];
    participants: desktop.StoryEventParticipantDTO[];
};

export function buildGraph(input: StoryGraphInput): Graph {
    const nodes: GraphNode[] = [];
    // The id a node is KEYED by is prefixed by its kind, because the two id spaces are separate
    // tables: an entity and an event could in principle share a uuid, and a collision would silently
    // draw two nodes as one. The prefix also makes the endpoint translation below total.
    for (const entity of input.entities) {
        nodes.push({
            id: entityNodeId(entity.id),
            label: entity.canonicalName,
            detail: entity.type,
            kind: "entity",
            status: entity.status,
        });
    }
    for (const event of input.events) {
        nodes.push({
            id: eventNodeId(event.id),
            label: event.name,
            detail: event.eventType ?? "",
            kind: "event",
            status: event.status,
        });
    }
    const present = new Set(nodes.map((node) => node.id));
    const edges: GraphEdge[] = [];
    let dropped = 0;

    // One edge per stored relation. Both endpoints are entity ids: `CreateStoryRelation` is called
    // with entity references only (the extraction path writes `EntityConcept` on both sides), so a
    // relation between two events is not a shape this build produces. A relation whose endpoint the
    // entity list does not have is dropped and counted.
    for (const relation of input.relations) {
        const from = entityNodeId(relation.sourceEntityId);
        const to = entityNodeId(relation.targetEntityId);
        if (!present.has(from) || !present.has(to)) {
            dropped += 1;
            continue;
        }
        edges.push({ id: relation.id, from, to, label: relation.type, participation: false });
    }

    // One edge per participation: the entity to the event it takes part in.
    //
    // The edge id is derived rather than taken from the row, because a participation's identity is
    // the (event, entity, role) TRIPLE and it has no id column — so the triple is what makes the key
    // unique, and using the pair alone would collide for a character who is both the actor and the
    // owner in one event.
    for (const participant of input.participants) {
        const from = entityNodeId(participant.storyEntityId);
        const to = eventNodeId(participant.storyEventId);
        if (!present.has(from) || !present.has(to)) {
            dropped += 1;
            continue;
        }
        edges.push({
            id: `participation:${participant.storyEventId}:${participant.storyEntityId}:${participant.role}`,
            from,
            to,
            label: participant.role,
            participation: true,
        });
    }

    return { nodes, edges, droppedEdges: dropped };
}

export type Point = { x: number; y: number };

/**
 * layoutGraph places the nodes on a grid, deterministically.
 *
 * # Why a grid and not a ring
 *
 * The first version placed nodes on a circle, which is compact for a handful and unreadable for a
 * real project: twenty events and thirty entities put a hundred edges through the middle, and the
 * labels overlapped at the top of the ring. A grid gives every node a cell, so two nodes can never
 * land on the same point at any count — which is the property `positionsAreDistinct` asserts.
 *
 * # Why the columns are derived from the count
 *
 * A fixed column count gives a very wide picture for a small graph and a very tall one for a large
 * graph. The square root keeps it roughly square, and the clamp keeps a two-node graph from being a
 * two-column strip.
 *
 * # Why this is not a force simulation
 *
 * A force layout produces a better picture and a NON-DETERMINISTIC one: the same graph would render
 * differently on two loads, which makes a screenshot comparison useless and a user's spatial memory
 * of their own story worthless. The canvas is where a user arranges things; this is a reading aid,
 * and a stable reading aid beats a pretty unstable one.
 */
export function layoutGraph(nodes: GraphNode[], width: number, height: number): Map<string, Point> {
    const positions = new Map<string, Point>();
    if (nodes.length === 0) return positions;
    const columns = Math.max(2, Math.ceil(Math.sqrt(nodes.length)));
    const rows = Math.max(1, Math.ceil(nodes.length / columns));
    // The margin keeps a label, which is drawn above its node, inside the viewBox.
    const marginX = 70;
    const marginY = 40;
    const usableWidth = Math.max(1, width - marginX * 2);
    const usableHeight = Math.max(1, height - marginY * 2);
    nodes.forEach((node, index) => {
        const column = index % columns;
        const row = Math.floor(index / columns);
        // A single row or column has nothing to spread across, so it is centred rather than divided
        // by zero — the division below is guarded by `max(1, ...)` on the counts.
        const x = marginX + (columns === 1 ? usableWidth / 2 : (usableWidth * column) / (columns - 1));
        const y = marginY + (rows === 1 ? usableHeight / 2 : (usableHeight * row) / (rows - 1));
        positions.set(node.id, { x, y });
    });
    return positions;
}

/**
 * graphCounts is the sentence the view shows above the drawing.
 *
 * It reports the node and edge counts AND the dropped edges, because a picture that omitted an edge
 * without saying so is the failure mode this whole module is written against: the count line is what
 * turns "the graph looks sparse" into "four relations are not shown here".
 */
export function graphCounts(graph: Graph): { nodes: number; edges: number; dropped: number; entities: number; events: number } {
    let entities = 0;
    let events = 0;
    for (const node of graph.nodes) {
        if (node.kind === "event") events += 1;
        else entities += 1;
    }
    return { nodes: graph.nodes.length, edges: graph.edges.length, dropped: graph.droppedEdges, entities, events };
}

/** The colour a node is drawn in, by kind. Kept here so the test can assert the two differ. */
export function nodeColour(kind: GraphNodeKind): { fill: string; stroke: string } {
    if (kind === "event") {
        return { fill: "var(--color-amber-100, #fef3c7)", stroke: "var(--color-amber-500, #f59e0b)" };
    }
    return { fill: "var(--color-sky-100, #e0f2fe)", stroke: "var(--color-sky-600, #0284c7)" };
}

/** A label shortened to fit a grid cell, with the full text kept for the tooltip. */
export function shortenLabel(label: string, max = 10): string {
    const trimmed = label.trim();
    if (trimmed.length <= max) return trimmed;
    return `${trimmed.slice(0, max)}…`;
}
