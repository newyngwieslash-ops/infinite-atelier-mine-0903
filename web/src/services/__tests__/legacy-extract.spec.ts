import assert from "node:assert/strict";
import test from "node:test";

import { collectMediaKeys, SUPPORTED_MANIFEST_VERSION } from "../desktop/legacy-extract";

/**
 * The legacy extractor is pure logic: given the stored project shape, it must
 * produce the snapshot envelope the Go importer consumes (ADR-0006 §1).
 *
 * What matters here is the reference walk, because that is what decides which
 * media the migration carries. A key the walk misses is a node whose image is
 * reported missing; a key it invents is a wasted upload. The walk therefore has
 * to find every place the legacy shape keeps one, and only those.
 *
 * The import itself cannot be driven from this layer: it needs the Go core, which
 * a browser test does not have. The Go tests cover it against the real fixtures
 * (testdata/old-projects) and a real database.
 */

function project(nodes: unknown[], sessions: unknown[] = []) {
    return { id: "p1", title: "T", nodes, connections: [], chatSessions: sessions } as never;
}

test("the reference walk finds every media key a node can carry", () => {
    const keys = collectMediaKeys(
        [
            project([
                { id: "n1", type: "image", metadata: { content: "image:one", storageKey: "image:one" } },
                { id: "n2", type: "video", metadata: { content: "video:two" } },
                {
                    id: "n3",
                    type: "image",
                    metadata: {
                        images: [
                            { storageKey: "image:three", content: "image:three" },
                            { storageKey: "image:four" },
                        ],
                    },
                },
                { id: "n4", type: "image", metadata: { references: ["image:five"] } },
                { id: "n5", type: "text", metadata: { coverUrl: "image:six" } },
            ]),
        ],
        [],
    );
    for (const key of ["image:one", "video:two", "image:three", "image:four", "image:five", "image:six"]) {
        assert.ok(keys.has(key), `the walk missed ${key}`);
    }
});

test("the reference walk ignores what is not a stored key", () => {
    const keys = collectMediaKeys(
        [
            project([
                // A data URL is inline content, an object URL is session-local and a
                // remote address is not in the store: none is a key to migrate.
                { id: "n1", type: "image", metadata: { content: "data:image/png;base64,AAAA" } },
                { id: "n2", type: "image", metadata: { content: "blob:http://localhost/abc" } },
                { id: "n3", type: "image", metadata: { content: "https://example.com/a.png" } },
                { id: "n4", type: "text", metadata: { content: "just text" } },
                { id: "n5", type: "text", metadata: { content: "" } },
                { id: "n6", type: "text", metadata: {} },
                { id: "n7", type: "image", metadata: { content: "   " } },
            ]),
        ],
        [],
    );
    // "just text" is not a store key either: a plain string in a node's content is
    // the node's own text, and the importer would report it as missing media.
    assert.equal(keys.size, 0, `unexpected keys: ${[...keys].join(", ")}`);
});

test("the reference walk reads chat references and assets", () => {
    const keys = collectMediaKeys(
        [
            project([{ id: "n1", type: "text", metadata: {} }], [
                {
                    id: "s1",
                    messages: [{ references: [{ storageKey: "image:chat-ref" }] }, { references: [{ id: "x" }] }],
                },
            ]),
        ],
        [
            { id: "a1", kind: "image", coverUrl: "image:asset-cover", data: { storageKey: "image:asset-data" } },
            { id: "a2", kind: "image", data: { dataUrl: "data:image/png;base64,BBBB" } },
        ] as never,
    );
    assert.ok(keys.has("image:chat-ref"), "a chat reference was missed");
    assert.ok(keys.has("image:asset-cover"), "an asset cover was missed");
    assert.ok(keys.has("image:asset-data"), "an asset's stored file was missed");
});

test("the reference walk is idempotent across repeated references", () => {
    const keys = collectMediaKeys(
        [
            project([
                { id: "n1", type: "image", metadata: { content: "image:same", storageKey: "image:same" } },
                { id: "n2", type: "image", metadata: { content: "image:same", storageKey: "image:same" } },
            ]),
        ],
        [],
    );
    // One key, because the same media referenced twice is one object to upload and
    // the importer must not store it twice (AC-LEGACY-002's "不重复媒体").
    assert.equal(keys.size, 1);
});

test("the envelope version this build writes is the one it accepts", () => {
    // The Go importer refuses any other version, so the two constants must agree;
    // this pins the frontend half of that contract.
    assert.equal(SUPPORTED_MANIFEST_VERSION, 1);
});

test("the reference walk accepts every prefix the legacy store uses", () => {
    // The prefixes come from the call sites that create keys: uploadImage uses
    // image:, and uploadMediaFile is called with video, audio, director and file.
    const keys = collectMediaKeys(
        [
            project([
                { id: "n1", type: "image", metadata: { storageKey: "image:abc123" } },
                { id: "n2", type: "video", metadata: { storageKey: "video:def456" } },
                { id: "n3", type: "audio", metadata: { storageKey: "audio:ghi789" } },
                { id: "n4", type: "director", metadata: { storageKey: "director:jkl012" } },
                { id: "n5", type: "text", metadata: { storageKey: "file:mno345" } },
            ]),
        ],
        [],
    );
    assert.equal(keys.size, 5, `expected five keys, got ${[...keys].join(", ")}`);
});

test("a sentence that looks like a key prefix is still not a key", () => {
    // Guards the shape check: the prefix alone is not enough, the body must look
    // like an identifier rather than prose.
    const keys = collectMediaKeys([project([{ id: "n1", type: "text", metadata: { content: "image: this is a caption" } }])], []);
    assert.equal(keys.size, 0, `unexpected keys: ${[...keys].join(", ")}`);
});
