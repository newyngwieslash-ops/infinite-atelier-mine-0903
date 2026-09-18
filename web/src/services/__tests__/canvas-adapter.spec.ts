import assert from "node:assert/strict";
import test from "node:test";

import { buildJobScope } from "../desktop/job-scope";

/**
 * The canvas adapter's diffing and mode selection are pure logic, so they are
 * testable without a browser. What matters here is the property AC-CANVAS-003
 * depends on, plus the mode decision that keeps new facts out of localForage.
 *
 * The Go adapter itself needs the Wails bindings, which do not exist in Node, so
 * it is exercised through the exported conversion helpers instead: those are
 * where a field could be silently dropped on the way to the database.
 */

test("a node's display fields are separated from the fields Go does not model", async () => {
    const { toNodeWrite } = await import("../desktop/canvas-adapter-go");
    const payload = toNodeWrite(
        {
            id: "node-1",
            type: "image",
            title: "Hero",
            position: { x: 12, y: 34 },
            width: 100,
            height: 200,
            // A migrated node carries keys the canvas type does not name, which
            // is exactly what the retained-fields path exists for.
            metadata: {
                content: "image:abc",
                status: "success",
                errorDetails: "why",
                prompt: "a lighthouse",
                vendorField: { nested: true },
            } as never,
        },
        "doc-1",
        undefined,
    );
    const uiState = JSON.parse(payload.uiState ?? "{}");
    const legacy = JSON.parse(payload.legacyMetadata ?? "{}");

    // The fields the canvas renders round-trip through uiState.
    assert.equal(uiState.content, "image:abc");
    assert.equal(uiState.status, "success");
    assert.equal(uiState.errorDetails, "why");
    // Everything else is preserved verbatim rather than dropped.
    assert.equal(legacy.prompt, "a lighthouse");
    assert.deepEqual(legacy.vendorField, { nested: true });
    // Geometry and identity travel in their own columns.
    assert.equal(payload.positionX, 12);
    assert.equal(payload.positionY, 34);
    assert.equal(payload.width, 100);
    assert.equal(payload.height, 200);
    assert.equal(payload.nodeType, "image");
    assert.equal(payload.documentId, "doc-1");
});

test("a node with no metadata still produces valid JSON columns", async () => {
    const { toNodeWrite } = await import("../desktop/canvas-adapter-go");
    const payload = toNodeWrite(
        {
            id: "node-2",
            type: "text",
            title: "T",
            position: { x: 0, y: 0 },
            width: 10,
            height: 10,
        },
        "doc-1",
        4,
    );
    assert.deepEqual(JSON.parse(payload.uiState ?? "{}"), {});
    assert.deepEqual(JSON.parse(payload.legacyMetadata ?? "{}"), {});
    // The revision is carried so the write is guarded.
    assert.equal(payload.revision, 4);
});

test("a stored node is rebuilt with its display fields merged back", async () => {
    const { toCanvasNode } = await import("../desktop/canvas-adapter-go");
    const node = toCanvasNode({
        id: "node-3",
        nodeType: "image",
        title: "T",
        positionX: 5,
        positionY: 6,
        width: 7,
        height: 8,
        zIndex: 0,
        uiState: JSON.stringify({ content: "image:x", status: "success" }),
        legacyMetadata: JSON.stringify({ prompt: "kept" }),
        revision: 2,
    } as never);
    assert.equal(node.id, "node-3");
    assert.equal(node.metadata?.content, "image:x");
    assert.equal(node.metadata?.status, "success");
    // The retained field is merged back, so the canvas sees what it wrote.
    assert.equal(node.metadata?.prompt, "kept");
    assert.deepEqual(node.position, { x: 5, y: 6 });
});

test("a malformed stored column degrades instead of breaking the load", async () => {
    const { toCanvasNode } = await import("../desktop/canvas-adapter-go");
    const node = toCanvasNode({
        id: "node-4",
        nodeType: "text",
        title: "T",
        positionX: 0,
        positionY: 0,
        width: 1,
        height: 1,
        zIndex: 0,
        uiState: "{not json",
        legacyMetadata: "[]",
        revision: 1,
    } as never);
    // The node still renders with its geometry; only the extra fields are absent.
    assert.deepEqual(node.metadata, {});
    assert.equal(node.width, 1);
});

test("the mode decision follows the injected bindings", async () => {
    const { isSecureCanvasMode, resetCanvasAdapter, canvasAdapterKind } = await import("../desktop/canvas-adapter");
    const scope = globalThis as unknown as { window?: unknown };
    const original = scope.window;

    resetCanvasAdapter();
    // No window at all: a Node context is not a desktop shell.
    delete scope.window;
    assert.equal(isSecureCanvasMode(), false);

    // A window without the project binding is a browser.
    scope.window = {};
    assert.equal(isSecureCanvasMode(), false);

    // The desktop shell injects the binding namespace.
    scope.window = { go: { desktop: { ProjectsBinding: {} } } };
    assert.equal(isSecureCanvasMode(), true);
    // Nothing has resolved yet, so the kind reports that honestly.
    assert.equal(canvasAdapterKind(), "unresolved");

    scope.window = original;
    resetCanvasAdapter();
});

test("job identity still scopes a batch write", () => {
    // The canvas and the job manager share the identity convention; a regression
    // here would collapse a batch into one job again.
    const scope = buildJobScope({ projectId: "p", entityId: "n", batchIndex: 3 });
    assert.equal(scope.entityId, "n#3");
    assert.equal(scope.projectId, "p");
});

test("a node's entity reference survives the write and the read", async () => {
    // AC-CANVAS-001: "Node 有 entity refs". Domain projection nodes carry the
    // entity they project, and the schema's CHECK requires both halves together,
    // so a write that dropped them would make a projection indistinguishable
    // from a decorative node.
    const { toNodeWrite, toCanvasNode } = await import("../desktop/canvas-adapter-go");

    const projection = {
        id: "node-1",
        type: "scene" as const,
        title: "Scene 1",
        position: { x: 10, y: 20 },
        width: 300,
        height: 200,
        entityType: "scene",
        entityId: "scene-42",
    };
    const written = toNodeWrite(projection, "doc-1", 3);
    assert.equal(written.entityType, "scene");
    assert.equal(written.entityId, "scene-42");

    // The reference comes back out of its own columns, not out of metadata.
    const read = toCanvasNode({
        id: "node-1",
        nodeType: "scene",
        title: "Scene 1",
        entityType: "scene",
        entityId: "scene-42",
        positionX: 10,
        positionY: 20,
        width: 300,
        height: 200,
        zIndex: 0,
        revision: 4,
    } as never);
    assert.equal(read.entityType, "scene");
    assert.equal(read.entityId, "scene-42");

    // A free canvas node projects nothing, and must not acquire a reference.
    const free = toNodeWrite(
        { id: "node-2", type: "text", title: "note", position: { x: 0, y: 0 }, width: 100, height: 50 },
        "doc-1",
    );
    assert.equal(free.entityType, undefined);
    assert.equal(free.entityId, undefined);

    // A half reference is refused here rather than sent to fail the CHECK.
    const half = toNodeWrite(
        { id: "node-3", type: "scene", title: "half", position: { x: 0, y: 0 }, width: 100, height: 50, entityType: "scene" },
        "doc-1",
    );
    assert.equal(half.entityType, undefined, "a reference without an id must not be sent");
    assert.equal(half.entityId, undefined);
});

test("an edge keeps its relation type and the registry's verdict", async () => {
    // AC-CANVAS-002: "Edge 不是仅 UI 线". The meaning and the validation status
    // are what make it more than a line, so both must survive the round trip.
    const { toCanvasConnection } = await import("../desktop/canvas-adapter-go");

    const edge = toCanvasConnection({
        id: "edge-1",
        fromNodeId: "node-1",
        toNodeId: "node-2",
        relationType: "references",
        fromPort: "character_reference",
        toPort: "",
        required: true,
        validationStatus: "valid",
        revision: 2,
    } as never);
    assert.equal(edge.relationType, "references");
    assert.equal(edge.fromPort, "character_reference");
    assert.equal(edge.required, true);
    assert.equal(edge.validationStatus, "valid");

    // An imported untyped link stays generic rather than being upgraded: PRD
    // FR-130 requires it to survive as generic, and calling it valid would
    // claim a check that never ran.
    const legacy = toCanvasConnection({
        id: "edge-2",
        fromNodeId: "a",
        toNodeId: "b",
        relationType: "generic",
        required: false,
        validationStatus: "unknown",
        revision: 1,
    } as never);
    assert.equal(legacy.relationType, "generic");
    assert.equal(legacy.validationStatus, "unknown");
});
