import assert from "node:assert/strict";
import { test } from "node:test";

/**
 * The story graph's layout decisions (ADR-0021), asserted as pure functions.
 *
 * Each one is a place the drawing can show the wrong thing, and the first two are defects the view
 * actually had:
 *
 *  - EVENTS AS NODES. The section is 事件图谱 and the first version built its nodes from the entity
 *    list alone, so every event was read, listed in its own table, and absent from the drawing.
 *  - THE DROPPED-EDGE COUNT. The four lists are read separately, so an edge can name a node that is
 *    not there. Dropping it silently says "these facts are unrelated" when the truth is "this view
 *    did not draw them".
 *  - A PURE GRID, because the ring it replaced could put nodes on top of one another and its picture
 *    got worse as the project grew.
 *  - PARTICIPATION AS EDGES, because `participates_in` is in the vocabulary and nothing writes it —
 *    the fact lives in `story_event_participants`, so a view reading relations alone would draw every
 *    event as an isolated dot.
 */
import {
    buildGraph,
    entityNodeId,
    eventNodeId,
    graphCounts,
    layoutGraph,
    nodeColour,
    shortenLabel,
    type GraphNode,
} from "../desktop/story-graph";

function entity(id: string, name: string, status = "accepted") {
    return { id, canonicalName: name, type: "character", status } as never;
}

function event(id: string, name: string, status = "accepted") {
    return { id, name, eventType: "conflict", status } as never;
}

function relation(id: string, source: string, target: string, type = "knows") {
    return { id, sourceEntityId: source, targetEntityId: target, type } as never;
}

function participant(eventId: string, entityId: string, role: string) {
    return { storyEventId: eventId, storyEntityId: entityId, role } as never;
}

test("events are nodes, which the first version of the view got wrong", () => {
    const graph = buildGraph({
        entities: [entity("e1", "沈砚")],
        events: [event("v1", "渡口相遇")],
        relations: [],
        participants: [],
    });
    assert.equal(graph.nodes.length, 2, "an entity and an event must be two nodes");
    const kinds = graph.nodes.map((node) => node.kind).sort();
    assert.deepEqual(kinds, ["entity", "event"]);
});

test("participation becomes an edge labelled by role", () => {
    const graph = buildGraph({
        entities: [entity("e1", "沈砚")],
        events: [event("v1", "渡口相遇")],
        relations: [],
        participants: [participant("v1", "e1", "actor")],
    });
    assert.equal(graph.edges.length, 1);
    assert.equal(graph.edges[0].label, "actor", "the edge label is the role, not a generic verb");
    assert.equal(graph.edges[0].participation, true);
    assert.equal(graph.edges[0].from, entityNodeId("e1"));
    assert.equal(graph.edges[0].to, eventNodeId("v1"));
});

test("one entity holding two roles in one event is two edges, not one", () => {
    // The row's identity is the (event, entity, role) triple and it has no id column, so an
    // implementation keying on the pair alone would collapse these two into one and lose a fact.
    const graph = buildGraph({
        entities: [entity("e1", "沈砚")],
        events: [event("v1", "渡口相遇")],
        relations: [],
        participants: [participant("v1", "e1", "actor"), participant("v1", "e1", "owner")],
    });
    assert.equal(graph.edges.length, 2);
    const ids = new Set(graph.edges.map((edge) => edge.id));
    assert.equal(ids.size, 2, "the edge ids must be distinct");
});

test("an edge to a node that is not there is dropped AND counted", () => {
    // A relation to an entity the filter excluded. Drawing it would be a line to nowhere; dropping
    // it silently would report "unrelated" where the truth is "not drawn here".
    const graph = buildGraph({
        entities: [entity("e1", "沈砚")],
        events: [],
        relations: [relation("r1", "e1", "e-missing")],
        participants: [],
    });
    assert.equal(graph.edges.length, 0);
    assert.equal(graph.droppedEdges, 1, "the drop must be counted so the view can report it");

    const counts = graphCounts(graph);
    assert.equal(counts.dropped, 1);
    assert.equal(counts.edges, 0);
});

test("a participation whose event was filtered out is dropped and counted", () => {
    const graph = buildGraph({
        entities: [entity("e1", "沈砚")],
        events: [],
        relations: [],
        participants: [participant("v-gone", "e1", "actor")],
    });
    assert.equal(graph.edges.length, 0);
    assert.equal(graph.droppedEdges, 1);
});

test("an entity id and an event id are separate spaces", () => {
    // The two ids come from separate tables, so an implementation keyed on the raw id would merge two
    // nodes into one if a uuid ever collided — and would translate a relation's endpoint wrongly.
    const graph = buildGraph({
        entities: [entity("same", "沈砚")],
        events: [event("same", "渡口相遇")],
        relations: [],
        participants: [],
    });
    assert.equal(graph.nodes.length, 2, "same raw id, two nodes");
    assert.notEqual(entityNodeId("same"), eventNodeId("same"));
});

test("the layout is deterministic and puts no two nodes on one point", () => {
    const nodes: GraphNode[] = Array.from({ length: 23 }, (_, index) => ({
        id: `n${index}`,
        label: `节点 ${index}`,
        detail: "",
        kind: index % 2 === 0 ? "entity" : "event",
        status: "accepted",
    }));
    const first = layoutGraph(nodes, 900, 520);
    const second = layoutGraph(nodes, 900, 520);
    assert.equal(first.size, nodes.length);
    // Determinism: the same input twice is the same picture, which is what makes a screenshot
    // comparison meaningful and a user's memory of where their story sits worth having.
    for (const node of nodes) {
        assert.deepEqual(first.get(node.id), second.get(node.id));
    }
    // Distinctness: the property the ring layout could not promise.
    const seen = new Set<string>();
    for (const point of first.values()) {
        const key = `${point.x},${point.y}`;
        assert.equal(seen.has(key), false, `two nodes landed on ${key}`);
        seen.add(key);
    }
    // And every point is inside the box, so no node is drawn off screen.
    for (const point of first.values()) {
        assert.ok(point.x >= 0 && point.x <= 900, `x out of range: ${point.x}`);
        assert.ok(point.y >= 0 && point.y <= 520, `y out of range: ${point.y}`);
    }
});

test("the layout handles the degenerate counts without producing NaN", () => {
    assert.equal(layoutGraph([], 900, 520).size, 0);
    const single = layoutGraph(
        [{ id: "only", label: "only", detail: "", kind: "entity", status: "accepted" }],
        900,
        520,
    );
    const point = single.get("only");
    assert.ok(point && Number.isFinite(point.x) && Number.isFinite(point.y), "a one-node graph must place its node");
    const two = layoutGraph(
        [
            { id: "a", label: "a", detail: "", kind: "entity", status: "accepted" },
            { id: "b", label: "b", detail: "", kind: "event", status: "accepted" },
        ],
        900,
        520,
    );
    for (const value of two.values()) {
        assert.ok(Number.isFinite(value.x) && Number.isFinite(value.y));
    }
});

test("an entity and an event are drawn in different colours", () => {
    // Not a style assertion for its own sake: the two kinds are otherwise indistinguishable dots, and
    // a viewer cannot tell an event from a character by its label alone.
    const entityColour = nodeColour("entity");
    const eventColour = nodeColour("event");
    assert.notEqual(entityColour.stroke, eventColour.stroke);
    assert.notEqual(entityColour.fill, eventColour.fill);
});

test("a long label is shortened but an exact-length one is not", () => {
    assert.equal(shortenLabel("沈砚"), "沈砚");
    assert.equal(shortenLabel("十刚刚好十个字"), "十刚刚好十个字");
    assert.equal(shortenLabel("这是一个非常长的角色名字").length, 11, "ten characters plus the ellipsis");
    assert.ok(shortenLabel("这是一个非常长的角色名字").endsWith("…"));
});

test("the counts sentence reports each kind separately", () => {
    const graph = buildGraph({
        entities: [entity("e1", "沈砚"), entity("e2", "陆行舟")],
        events: [event("v1", "渡口相遇")],
        relations: [relation("r1", "e1", "e2")],
        participants: [participant("v1", "e1", "actor")],
    });
    const counts = graphCounts(graph);
    assert.equal(counts.entities, 2);
    assert.equal(counts.events, 1);
    assert.equal(counts.nodes, 3);
    assert.equal(counts.edges, 2);
    assert.equal(counts.dropped, 0);
});
