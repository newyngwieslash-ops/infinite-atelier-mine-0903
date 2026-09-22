import assert from "node:assert/strict";
import { test } from "node:test";

/**
 * The version reads `drama.ts` exposes, from both sides.
 *
 * `listScriptVersions` is the QUERY that closed the one version family whose history had no binding.
 * `listStoryboardVersions` is its BOARD counterpart, which the timeline's new shot-list version picker
 * reads: `ListStoryboardVersions` takes a storyboard id rather than an episode, so the picker resolves
 * the episode's board through `EnsureStoryboard` first and the id it passes here is the BOARD's. That
 * difference is why the second case asserts the id's passage rather than only the rows: a client that
 * forwarded an episode id to this method would be asking the wrong question, and the core would answer
 * "no versions" for a board that has several.
 *
 * Both directions are asserted for each:
 *
 *  - with no core, an empty list, because a browser session renders each picker's empty state rather
 *    than an error;
 *  - with a core, the CORE'S OWN rows, because a picker built from an answer this client made up
 *    would offer versions that do not exist.
 *
 * The second direction is the one a mutation would survive: `return []` unconditionally passes every
 * absence check in the suite. It is also why the fake records the call rather than only its result.
 *
 * The fake installs the shape Wails generates, `window.go.desktop.<Binding>.<Method>`, exactly as
 * `media.spec.ts` does, and reuses that file's `withDesktopCore` helper pattern rather than growing a
 * third copy of it. `REQUIRED_DRAMA_METHODS` is installed alongside because `isDramaBindingsAvailable`
 * gates the calls: a fake with only the read under test would assert the unavailable path instead.
 */
import { isDramaBindingsAvailable, listScriptVersions, listStoryboardVersions } from "../desktop/drama";
import { withDesktopCore, type FakeBindings } from "./desktop-core";

/** dramaSurface is the smallest binding `isDramaBindingsAvailable` accepts, plus the reads. */
function dramaSurface(overrides: Record<string, (...args: never[]) => unknown>): FakeBindings {
    return {
        DramaBinding: {
            // The probe's own set, which is what makes the surface reachable at all.
            ListEpisodes: () => [],
            CreateEpisode: () => undefined,
            ListSourceDocuments: () => [],
            ListStaleMarks: () => [],
            ListWorkflowRuns: () => [],
            ...overrides,
        },
    };
}

test("the script version read answers empty when the core is absent", async () => {
    // A QUERY, so a browser session gets the honest "nothing yet" answer the two pickers render as
    // their empty state. An exception here would turn a development browser into an error toast.
    assert.deepEqual(await listScriptVersions("episode-1"), []);
});

test("a reachable binding carries the script version read through and returns its rows", async () => {
    // The rows are deliberately NOT the shape a client would synthesize: `status` and
    // `versionNumber` are what both pickers label their options with, and `id` is what they send
    // back as `scriptVersionId` / the dialogue-line lookup's subject.
    const rows = [
        { id: "ver-3", scriptId: "script-1", versionNumber: 3, status: "draft", estimatedDurationSeconds: 120 },
        { id: "ver-2", scriptId: "script-1", versionNumber: 2, status: "approved", estimatedDurationSeconds: 118 },
        { id: "ver-1", scriptId: "script-1", versionNumber: 1, status: "superseded", estimatedDurationSeconds: 100 },
    ];
    const calls: Array<[string, unknown]> = [];
    const recording = dramaSurface({
        ListScriptVersions: (episodeId: unknown) => {
            calls.push(["ListScriptVersions", episodeId]);
            return rows;
        },
    });

    await withDesktopCore(recording, async () => {
        assert.equal(isDramaBindingsAvailable(), true, "the fake surface must read as available");
        const versions = await listScriptVersions("episode-7");
        assert.deepEqual(versions, rows, "the core's own rows must be the ones answered");
        // The ORDER is the core's — newest first — and the client must not re-sort it: both pickers
        // read the first entry as "the newest", and the timeline picker scans for the approved one.
        assert.deepEqual(
            versions.map((version) => version.id),
            ["ver-3", "ver-2", "ver-1"],
        );
    });

    assert.deepEqual(calls, [["ListScriptVersions", "episode-7"]], "the episode id must reach the binding");
});

test("a binding that predates the read is treated as present and fails on the call", async () => {
    // The probe checks a representative set rather than every method, which is deliberate: an older
    // build with the drama surface and without `ListScriptVersions` still renders the studio, and the
    // specific call is what fails. Asserting it here keeps that trade explicit — a picker in that
    // build shows its empty state because the call rejected, not because a query lied.
    const withoutTheRead = dramaSurface({});
    await withDesktopCore(withoutTheRead, async () => {
        assert.equal(isDramaBindingsAvailable(), true, "a surface without the new read is still available");
        await assert.rejects(
            listScriptVersions("episode-7"),
            (error: unknown) => {
                assert.ok(error instanceof Error, "the failure must be an Error");
                return true;
            },
            "a build with no ListScriptVersions must fail on the call rather than answer empty",
        );
    });
});

test("the storyboard version read answers empty when the core is absent", async () => {
    // The BOARD family's read, which the timeline's shot-list version picker calls. It is a QUERY like
    // the script one above, so a browser session gets `[]` and the picker renders its empty state
    // rather than an error toast — and a mutation that made this method return a fixed row, or throw,
    // would fail here.
    assert.deepEqual(await listStoryboardVersions("storyboard-1"), []);
});

test("a reachable binding carries the storyboard version read through and returns its rows", async () => {
    // The ids and numbers are deliberately NOT a sequence a client could synthesize: the picker labels
    // each option `v{versionNumber} · {status}` and sends the `id` back as the shot list's `versionId`,
    // so a client that fabricated either would offer versions the core cannot render.
    const rows = [
        { id: "board-5", storyboardId: "board-1", versionNumber: 5, status: "draft" },
        { id: "board-4", storyboardId: "board-1", versionNumber: 4, status: "approved" },
        { id: "board-3", storyboardId: "board-1", versionNumber: 3, status: "superseded" },
    ];
    const calls: Array<[string, unknown]> = [];
    const recording = dramaSurface({
        ListStoryboardVersions: (storyboardId: unknown) => {
            calls.push(["ListStoryboardVersions", storyboardId]);
            return rows;
        },
    });

    await withDesktopCore(recording, async () => {
        assert.equal(isDramaBindingsAvailable(), true, "the fake surface must read as available");
        const versions = await listStoryboardVersions("board-1");
        assert.deepEqual(versions, rows, "the core's own rows must be the ones answered");
        // The order is the CORE'S — `ORDER BY version_number DESC`, newest first — and the client must
        // not re-sort it: the picker reads the first entry as "the newest" when it finds no approved
        // version to prefer, and the storyboard table reads it the same way.
        assert.deepEqual(
            versions.map((version) => version.id),
            ["board-5", "board-4", "board-3"],
        );
    });

    // The STORYBOARD id, not an episode's: `ListStoryboardVersions` takes the board the versions belong
    // to, which is why the caller resolves the episode's board first. A client that forwarded an
    // episode id here would ask the wrong question and be told, truthfully, that it has no versions.
    assert.deepEqual(calls, [["ListStoryboardVersions", "board-1"]], "the storyboard id must reach the binding");
});
