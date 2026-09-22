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

/**
 * The other direction: a window WITH a core.
 *
 * Every check above describes what happens when there is no binding, and an independent quality
 * review found that was the ONLY direction: three mutations survived the suite, and two of them were
 * the probe's own body — `isMediaBindingsAvailable` hardcoded to `false` (which would disable every
 * media section forever) and `REQUIRED_MEDIA_METHODS` emptied (because `[].every(...)` is `true`, so a
 * window with no binding at all would read as available). The probes are the sections' only gate, so
 * both directions have to be asserted.
 *
 * The fake installs the same shape Wails generates: `window.go.desktop.<Binding>.<Method>`. It is
 * installed and removed per test, and `globalThis` is restored afterwards so the absence tests above
 * cannot be affected by one that ran before them.
 */
type FakeBindings = Record<string, Record<string, (...args: never[]) => unknown>>;

/** withDesktopCore runs a body with a fake Wails binding surface installed. */
async function withDesktopCore(bindings: FakeBindings, body: () => Promise<void> | void) {
    const host = globalThis as { window?: unknown };
    const previous = host.window;
    host.window = { go: { desktop: bindings } };
    try {
        await body();
    } finally {
        if (previous === undefined) {
            delete host.window;
        } else {
            host.window = previous;
        }
    }
}

test("a window with a core reports the capabilities its binding satisfies", async () => {
    // The full surface, which is what a real build has.
    const complete: FakeBindings = {
        MediaBinding: {
            MediaCapability: () => undefined,
            ReadTimeline: () => undefined,
            RunExport: () => undefined,
            SaveExport: () => undefined,
        },
        JobsBinding: {
            SubmitVideoJob: () => undefined,
            SubmitAudioJob: () => undefined,
        },
    };
    await withDesktopCore(complete, () => {
        assert.equal(isMediaBindingsAvailable(), true, "a complete media binding must read as available");
        assert.equal(isMediaExportAvailable(), true, "a complete export binding must read as available");
        assert.equal(isMediaJobSubmissionAvailable(), true, "a complete job binding must read as available");
    });

    // A build whose media services exist but whose engine cannot compose: the two flags DIVERGE, and
    // that is the property the split exists for. A single flag would hide it.
    const noExport: FakeBindings = {
        MediaBinding: { MediaCapability: () => undefined, ReadTimeline: () => undefined },
        JobsBinding: {},
    };
    await withDesktopCore(noExport, () => {
        assert.equal(isMediaBindingsAvailable(), true, "the reads are reachable without an export");
        assert.equal(isMediaExportAvailable(), false, "a binding with no RunExport must not report export");
        assert.equal(isMediaJobSubmissionAvailable(), false, "a binding with no job submissions must say so");
    });

    // And ONE missing method is enough. This is the assertion that kills the empty-array mutation:
    // `[].every(...)` is true, so a probe whose required list was emptied would report a window with
    // NOTHING installed as available.
    await withDesktopCore({ MediaBinding: { MediaCapability: () => undefined } }, () => {
        assert.equal(isMediaBindingsAvailable(), false, "a binding missing ReadTimeline must not read as available");
    });
    await withDesktopCore({}, () => {
        assert.equal(isMediaBindingsAvailable(), false, "a window with no bindings must not read as available");
        assert.equal(isMediaExportAvailable(), false, "a window with no bindings must not report export");
    });
});

test("a reachable binding carries the call through rather than answering empty", async () => {
    // The QUERY/COMMAND split is asserted from the other side here: with a core present, a read calls
    // through and a command reaches the binding. A mutation that made `listSubtitleTracks` return `[]`
    // unconditionally, or that made a command resolve instead of calling, would pass every absence test
    // above and fail this one.
    const calls: string[] = [];
    const recording: FakeBindings = {
        MediaBinding: {
            MediaCapability: () => {
                calls.push("MediaCapability");
                return { exportAvailable: true, saveAvailable: true };
            },
            ReadTimeline: () => {
                calls.push("ReadTimeline");
                return { episodeId: "episode-9", shots: [], totalDurationMs: 0, missingMedia: 0 };
            },
            ListSubtitleTracks: () => {
                calls.push("ListSubtitleTracks");
                return [];
            },
            DraftSubtitles: () => {
                calls.push("DraftSubtitles");
                return { track: {}, cues: [] };
            },
        },
    };
    await withDesktopCore(recording, async () => {
        const timeline = await readTimeline({ episodeId: "episode-9" });
        assert.equal(timeline.episodeId, "episode-9", "the core's answer must be the one returned");
        await listSubtitleTracks("episode-9");
        await mediaCapability();
        await draftSubtitles({ episodeId: "episode-9", scriptVersionId: "s" } as never);
        assert.deepEqual(calls, ["ReadTimeline", "ListSubtitleTracks", "MediaCapability", "DraftSubtitles"]);
    });
});
