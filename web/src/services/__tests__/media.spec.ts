import assert from "node:assert/strict";
import { test } from "node:test";

/**
 * The media client's absence contract, which is the part of WP-11's sections that can be tested
 * without a core.
 *
 * `media.ts` keeps the split every desktop client in this repository keeps: a QUERY answers with an
 * empty result in a browser session, and a COMMAND throws. The split is behaviour rather than
 * formatting — a browser-mode preview must render an empty shot list, while a button a user pressed
 * must say why nothing happened — so it is asserted here rather than left to the e2e suite, which
 * runs with no core and therefore cannot reach the sections' own bodies.
 *
 * The checks are deliberately about the RESULT and the THROW, not about which method was called: a
 * client that reached the binding would fail these tests in a browser session, which is the point.
 */
import {
    approveExport,
    approveSubtitleTrack,
    draftSubtitles,
    editSubtitleCues,
    exportSubtitles,
    isMediaBindingsAvailable,
    isMediaExportAvailable,
    listExports,
    listSubtitleCues,
    listSubtitleTracks,
    mediaCapability,
    missingSubtitleLines,
    readTimeline,
    runExport,
    saveExport,
} from "../desktop/media";
import { isMediaJobSubmissionAvailable, submitAudioJob, submitVideoJob } from "../desktop/jobs";

test("a window with no desktop core reports both media capabilities unsatisfied", () => {
    assert.equal(isMediaBindingsAvailable(), false);
    assert.equal(isMediaExportAvailable(), false);
    assert.equal(isMediaJobSubmissionAvailable(), false);
});

test("the media reads answer empty rather than throwing when the core is absent", async () => {
    // Every one of these is a READ. A section renders its empty state from each of them, and an
    // exception here would render an error about a machine the interface never asked.
    assert.deepEqual(await listSubtitleTracks("episode-1"), []);
    assert.deepEqual(await listSubtitleCues("track-1"), []);
    assert.deepEqual(await missingSubtitleLines("track-1"), []);
    assert.deepEqual(await listExports("episode-1"), []);
});

test("the timeline read echoes the episode it was asked about", async () => {
    // The empty answer must still describe the QUESTION: a caller reading `episodeId` off the result
    // uses it to tell which episode's shots these are, and a blank one would make an empty answer
    // look like an answer about no episode at all.
    const timeline = await readTimeline({ episodeId: "episode-7" });
    assert.equal(timeline.episodeId, "episode-7");
    assert.deepEqual(timeline.shots, []);
    assert.equal(timeline.totalDurationMs, 0);
    assert.equal(timeline.missingMedia, 0);
});

test("the capability read reports nothing available and claims no diagnostic", async () => {
    const capability = await mediaCapability();
    assert.equal(capability.exportAvailable, false);
    assert.equal(capability.saveAvailable, false);
    // A diagnostic is a statement about the MACHINE. This interface asked this machine nothing, and
    // inventing a reason would be a claim about why — which is worse than letting the section state
    // that it needs the core.
    assert.equal(capability.diagnostic ?? "", "");
});

test("every media command throws when the core is absent", async () => {
    // Each entry is a COMMAND: a user pressed a button, and a silent no-op would leave them believing
    // something happened.
    const commands: Array<[string, () => Promise<unknown>]> = [
        ["draftSubtitles", () => draftSubtitles({ episodeId: "episode-1", scriptVersionId: "script-1" } as never)],
        ["editSubtitleCues", () => editSubtitleCues("track-1", [] as never)],
        ["exportSubtitles", () => exportSubtitles({ trackId: "track-1", format: "srt" } as never)],
        ["approveSubtitleTrack", () => approveSubtitleTrack({ trackId: "track-1" } as never)],
        ["runExport", () => runExport({ episodeId: "episode-1", quality: "preview" } as never)],
        ["approveExport", () => approveExport({ exportId: "export-1" } as never)],
        ["saveExport", () => saveExport({ storageKey: "a".repeat(64), suggestedName: "episode.mp4" } as never)],
        ["submitVideoJob", () => submitVideoJob({ projectId: "p", episodeId: "e", shotId: "s", providerId: "pr", model: "m", prompt: "x" } as never)],
        ["submitAudioJob", () => submitAudioJob({ projectId: "p", episodeId: "e", dialogueLineId: "l", providerId: "pr", model: "m", text: "x" } as never)],
    ];
    for (const [name, run] of commands) {
        await assert.rejects(
            run(),
            (error: unknown) => {
                assert.ok(error instanceof Error, `${name} must reject with an Error`);
                assert.ok(error.message.length > 0, `${name}'s message must say something`);
                return true;
            },
            `${name} did not throw`,
        );
    }
});
