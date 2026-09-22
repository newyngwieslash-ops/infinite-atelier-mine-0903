import assert from "node:assert/strict";
import { test } from "node:test";

/**
 * The script-version read, from both sides.
 *
 * `listScriptVersions` is `drama.ts`'s QUERY that closes the one version family whose history had no
 * binding. Two sections depend on it — the audio section's dialogue-line picker and the timeline
 * section's subtitle-draft picker — and both call it before they render anything, so both of its
 * directions have to be asserted:
 *
 *  - with no core, an empty list, because a browser session renders each picker's empty state rather
 *    than an error;
 *  - with a core, the CORE'S OWN rows, because a picker built from an answer this client made up
 *    would offer versions that do not exist.
 *
 * The second direction is the one a mutation would survive: `return []` unconditionally passes every
 * absence check in the suite. It is also why the fake records the call rather than only its result —
 * the episode id has to reach the binding, since `ListScriptVersions` takes the EPISODE and resolves
 * the script from it.
 *
 * The fake installs the shape Wails generates, `window.go.desktop.<Binding>.<Method>`, exactly as
 * `media.spec.ts` does. `REQUIRED_DRAMA_METHODS` is installed alongside it because
 * `isDramaBindingsAvailable` gates the call: a fake with only `ListScriptVersions` would assert the
 * unavailable path instead of the one under test.
 */
import { isDramaBindingsAvailable, listScriptVersions, resetDramaClients } from "../desktop/drama";

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
        // The binding module is cached across calls, so it is released here rather than left pointing
        // at a window a later test has replaced.
        resetDramaClients();
    }
}

/** dramaSurface is the smallest binding `isDramaBindingsAvailable` accepts, plus the read. */
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
