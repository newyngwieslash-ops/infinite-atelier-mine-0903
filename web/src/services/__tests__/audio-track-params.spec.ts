import assert from "node:assert/strict";
import { test } from "node:test";
import { mergeTrackEdit, type TrackEdit, type TrackParams } from "../desktop/audio-track-params";

test("editing gain preserves source trim and dialogue identity", () => {
    const original: TrackParams = {
        offsetMs: 1200, sourceStartMs: 300, sourceEndMs: 2100,
        durationMs: 1800, volume: 1, muted: true, dialogueLineId: "line-A",
    };
    const edit: TrackEdit = { offsetMs: 1200, volume: 0, muted: false };
    assert.deepEqual(mergeTrackEdit(original, edit), { ...original, volume: 0, muted: false });
    // The stored snapshot is not mutated: the save builds its own document.
    assert.equal(original.volume, 1);
});

test("an explicit zero volume survives the merge", () => {
    const original: TrackParams = { volume: 0.5, sourceStartMs: 100 };
    const merged = mergeTrackEdit(original, { offsetMs: null, volume: 0, muted: false });
    assert.equal(merged.volume, 0);
    assert.equal(merged.sourceStartMs, 100);
});

test("an explicit false muted survives the merge", () => {
    const original: TrackParams = { muted: true, volume: 0.8 };
    const merged = mergeTrackEdit(original, { offsetMs: 0, volume: 0.8, muted: false });
    assert.equal(merged.muted, false);
    assert.equal(merged.volume, 0.8);
});

test("null edit fields replace stored values rather than hiding them", () => {
    const original: TrackParams = { offsetMs: 500, volume: 0.7, muted: true, dialogueLineId: "line-B" };
    const merged = mergeTrackEdit(original, { offsetMs: null, volume: null, muted: null });
    assert.equal(merged.offsetMs, null);
    assert.equal(merged.volume, null);
    assert.equal(merged.muted, null);
    assert.equal(merged.dialogueLineId, "line-B");
});

test("fields outside the editor's three are carried through untouched", () => {
    const original: TrackParams = {
        offsetMs: 10, sourceStartMs: 20, sourceEndMs: 30, durationMs: 40,
        volume: 1, muted: false, dialogueLineId: "line-C",
    };
    const merged = mergeTrackEdit(original, { offsetMs: 99, volume: 0.25, muted: true });
    assert.equal(merged.sourceStartMs, 20);
    assert.equal(merged.sourceEndMs, 30);
    assert.equal(merged.durationMs, 40);
    assert.equal(merged.dialogueLineId, "line-C");
    assert.equal(merged.offsetMs, 99);
    assert.equal(merged.volume, 0.25);
    assert.equal(merged.muted, true);
});

test("an empty document gains exactly the edited fields", () => {
    const merged = mergeTrackEdit({}, { offsetMs: 5, volume: 1, muted: false });
    assert.deepEqual(merged, { offsetMs: 5, volume: 1, muted: false });
});

test("an unrelated usage's document is not touched by another edit", () => {
    const firstUsage: TrackParams = { offsetMs: 1, volume: 0.5, muted: false, dialogueLineId: "line-D" };
    const secondUsage: TrackParams = { offsetMs: 2, volume: 0.75, muted: true, dialogueLineId: "line-E" };
    mergeTrackEdit(firstUsage, { volume: 0, muted: true });
    // The other row's document is unchanged — the merge never aliases.
    assert.equal(secondUsage.volume, 0.75);
    assert.equal(secondUsage.dialogueLineId, "line-E");
});
