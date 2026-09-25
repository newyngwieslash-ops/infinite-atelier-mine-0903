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
    exportManifestDocument,
    exportScript,
    exportShotList,
    exportSubtitles,
    isDocumentExportAvailable,
    isMediaBindingsAvailable,
    isMediaExportAvailable,
    listExports,
    listSubtitleCues,
    listSubtitleTracks,
    mediaCapability,
    missingSubtitleLines,
    readTimeline,
    runExport,
    saveDocument,
    saveExport,
} from "../desktop/media";
import { isMediaJobSubmissionAvailable, isVideoBatchAvailable, submitAudioJob, submitVideoJob } from "../desktop/jobs";
import { isFrameLoadingAvailable, loadShotFrame } from "../desktop/frames";
import { withDesktopCore, type FakeBindings } from "./desktop-core";

test("the frame loader answers null rather than throwing when nothing can be read", async () => {
    // The absence direction for the frame picker, which is WP-28's own rule at the UI's end: a frame
    // that cannot be loaded is a state the section RENDERS — "this shot has no frame to send" — and
    // never an empty `firstFrame`, which a provider reads as a zero-byte image.
    assert.equal(await loadShotFrame(""), null);
    // A key that is not the store's shape is refused before any call, so a malformed value never looks
    // like a read failure.
    assert.equal(await loadShotFrame("not-a-hash"), null);
    assert.equal(await loadShotFrame("A".repeat(64)), null);
    assert.equal(await loadShotFrame("a".repeat(63)), null);
});

test("a window with no desktop core offers no frame loading", () => {
    assert.equal(isFrameLoadingAvailable(), false);
});

test("a window with no desktop core offers no video batch", () => {
    // A build whose binding predates the batch still submits one shot at a time, and the section hides
    // its multi-select rather than offering a control that cannot act.
    assert.equal(isVideoBatchAvailable(), false);
});

test("a window with no desktop core reports both media capabilities unsatisfied", () => {
    assert.equal(isMediaBindingsAvailable(), false);
    assert.equal(isMediaExportAvailable(), false);
    assert.equal(isMediaJobSubmissionAvailable(), false);
    // The documents are a probe of their own, and the absence direction has to be asserted for it too:
    // a build whose media binding predates them reads a timeline and runs an export, and a probe that
    // answered `true` here would offer a user three buttons that cannot reach anything.
    assert.equal(isDocumentExportAvailable(), false);
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
        // ROADMAP item 11's documents, which are commands of the same kind: a user pressed "render" and
        // a silent no-op would leave them believing a document was produced.
        ["exportScript", () => exportScript({ episodeId: "episode-1", format: "txt" } as never)],
        ["exportShotList", () => exportShotList({ episodeId: "episode-1", format: "csv" } as never)],
        ["exportManifestDocument", () => exportManifestDocument({ episodeId: "episode-1" } as never)],
        ["saveDocument", () => saveDocument({ text: "INT. ROOM - DAY", suggestedName: "script-ep1.txt" } as never)],
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
 * The fake installs the same shape Wails generates, `window.go.desktop.<Binding>.<Method>`. It is
 * installed and removed per test, and `globalThis` is restored afterwards so the absence tests above
 * cannot be affected by one that ran before them. The helper itself lives in `./desktop-core` now that
 * three specs need it; this file kept its own copy until the third arrived.
 */
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

/**
 * The document probe, from both sides.
 *
 * `isDocumentExportAvailable` is the only gate on ROADMAP item 11's document controls, so it carries
 * the same mutation risk `isMediaBindingsAvailable` did: hardcoded to `true` it offers three buttons
 * that reach nothing, and hardcoded to `false` it hides a working feature forever. The pairs below fix
 * both directions, and the second pair is the one that matters for the split it draws: `ExportScript`
 * without `SaveDocument` must read as UNAVAILABLE, because a user can render a document they can never
 * write out — which is worse than the notice that says the build cannot do this.
 */
test("the document probe reads available only when the binding has both a renderer and a writer", async () => {
    const both: FakeBindings = {
        MediaBinding: {
            ExportScript: () => undefined,
            SaveDocument: () => undefined,
        },
    };
    await withDesktopCore(both, () => {
        assert.equal(isDocumentExportAvailable(), true, "a binding with both methods must report documents available");
    });

    // A renderer with no writer, which is the divergence the probe exists for: the text would arrive
    // and the save button beside it would fail on every press.
    const renderOnly: FakeBindings = { MediaBinding: { ExportScript: () => undefined } };
    await withDesktopCore(renderOnly, () => {
        assert.equal(isDocumentExportAvailable(), false, "a binding that cannot write a document must not report documents available");
    });

    // The mirror: a writer with no renderer reaches nothing, since there is no text to send it.
    const saveOnly: FakeBindings = { MediaBinding: { SaveDocument: () => undefined } };
    await withDesktopCore(saveOnly, () => {
        assert.equal(isDocumentExportAvailable(), false, "a binding that cannot render a document must not report documents available");
    });

    // And a binding with none of them, which is the state every older build is in.
    await withDesktopCore({ MediaBinding: {} }, () => {
        assert.equal(isDocumentExportAvailable(), false, "a media binding without the document methods must not report documents available");
    });
});

test("a reachable document binding carries the call through rather than answering empty", async () => {
    // The same property the media probe's positive test asserts, for the four document methods: each
    // call must REACH the binding and return what it answered. A client mutated to return a fixed
    // document, or to throw while the probe reports available, fails here — and these are the only
    // checks in a browser-mode run that can see the calls at all, since the sections' own bodies are
    // answered by the shell's no-core notice.
    const calls: Array<[string, unknown]> = [];
    const rendering: FakeBindings = {
        MediaBinding: {
            ExportScript: (request: unknown) => {
                calls.push(["ExportScript", request]);
                return { name: "script", text: "INT. ROOM - DAY", extension: ".txt", suggestedName: "script-ep1.txt" };
            },
            ExportShotList: (request: unknown) => {
                calls.push(["ExportShotList", request]);
                return { name: "shot list", text: "ordinal,shot_id", extension: ".csv", suggestedName: "shotlist-ep1.csv" };
            },
            ExportManifestDocument: (request: unknown) => {
                calls.push(["ExportManifestDocument", request]);
                return { name: "manifest", text: "{\"schemaVersion\":1}", extension: ".json", suggestedName: "manifest-ep1.json" };
            },
            SaveDocument: (request: unknown) => {
                calls.push(["SaveDocument", request]);
                return { written: true, path: "C:/exports/shotlist-ep1.csv" };
            },
        },
    };
    await withDesktopCore(rendering, async () => {
        assert.equal(isDocumentExportAvailable(), true, "the probe must read the documents as available");

        const script = await exportScript({ episodeId: "episode-1", format: "fountain", includeShots: true } as never);
        assert.equal(script.text, "INT. ROOM - DAY", "the script the core returned must be the one answered");
        assert.equal(script.suggestedName, "script-ep1.txt");

        const shotList = await exportShotList({ episodeId: "episode-1", format: "csv" } as never);
        assert.equal(shotList.text, "ordinal,shot_id");
        assert.equal(shotList.extension, ".csv", "the extension decides what the dialog writes");

        const manifest = await exportManifestDocument({ episodeId: "episode-1" } as never);
        assert.equal(manifest.name, "manifest");

        const saved = await saveDocument({ text: shotList.text, suggestedName: shotList.suggestedName } as never);
        assert.equal(saved.written, true, "a written answer must travel back as written");
        assert.equal(saved.path, "C:/exports/shotlist-ep1.csv");

        assert.deepEqual(
            calls.map(([name]) => name),
            ["ExportScript", "ExportShotList", "ExportManifestDocument", "SaveDocument"],
        );
        // The requests must reach the binding AS GIVEN, `includeShots` included: the switch is the
        // user's choice and the client forwards it rather than deciding it.
        assert.deepEqual(calls[0][1], { episodeId: "episode-1", format: "fountain", includeShots: true });
        assert.deepEqual(calls[1][1], { episodeId: "episode-1", format: "csv" });
        assert.deepEqual(calls[3][1], { text: "ordinal,shot_id", suggestedName: "shotlist-ep1.csv" });
    });
});

test("a cancelled document save is a value rather than an error", async () => {
    // `written: false` is the user closing the save dialog, which `media.ts` documents as a returned
    // value and NOT a failure — the same distinction `saveExport` keeps. A client that turned it into
    // a rejection would make a cancellation look like a defect on a screen that did nothing wrong.
    const cancelling: FakeBindings = {
        MediaBinding: {
            // The probe's own pair, which is what makes the binding reachable at all: it reads
            // `ExportScript` and `SaveDocument`, so a fake that installed a different renderer would
            // assert the unavailable path rather than the cancelled one.
            ExportScript: () => undefined,
            SaveDocument: () => ({ written: false }),
        },
    };
    await withDesktopCore(cancelling, async () => {
        const result = await saveDocument({ text: "ordinal,shot_id", suggestedName: "shotlist-ep1.csv" } as never);
        assert.equal(result.written, false, "a cancellation must come back as written: false");
        assert.equal(result.path ?? "", "", "a cancellation names no path");
    });
});
