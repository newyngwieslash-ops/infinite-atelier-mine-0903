import assert from "node:assert/strict";
import { test } from "node:test";

/**
 * The panel-image chain's client contract (ADR-0017).
 *
 * `drama.ts` keeps the split every desktop client here keeps — a QUERY answers empty in a browser
 * session, a COMMAND throws — and this chain has one piece that is neither: `checkStoryboardGate`
 * is a read whose ANSWER is its refusal, so it throws with the gate's reason in the message. That
 * asymmetry is the thing worth asserting, because a caller that swallowed it would show a button
 * that silently does nothing.
 *
 * The tests below drive the real wrapper functions against a fake Wails surface. They are about
 * the REQUESTS that reach the binding and the RESULTS that come back, not about which function
 * was imported: a wrapper that forgot to forward a field would fail the argument assertions, and a
 * wrapper that answered a query by throwing would fail the empty-result ones.
 */
import {
    addRelation,
    addUsage,
    addVersion,
    approvePanelImage,
    attachFile,
    attachJobResult,
    checkStoryboardGate,
    collectBatchResults,
    createPanelVersion,
    isAssetsBindingsAvailable,
    isDramaBindingsAvailable,
    listPanels,
    runImageBatch,
} from "../desktop/drama";
import { withDesktopCore, type FakeBindings } from "./desktop-core";

/** The representative methods the two probes check, so the fake reads as available. */
function availableBindings(overrides: FakeBindings = {}): FakeBindings {
    return {
        DramaBinding: {
            ListEpisodes: () => [],
            CreateEpisode: () => ({}),
            ListSourceDocuments: () => [],
            ListStaleMarks: () => [],
            ListWorkflowRuns: () => [],
            ListPanels: () => [],
            ...(overrides.DramaBinding ?? {}),
        },
        AssetsBinding: {
            ListAssets: () => [],
            CreateAsset: () => ({}),
            ...(overrides.AssetsBinding ?? {}),
        },
    };
}

test("every panel-chain wrapper throws when the core is absent", async () => {
    // Commands, so each must refuse rather than silently succeed. The list is exhaustive on
    // purpose: a wrapper added later without its absence branch is the exact defect this catches.
    await assert.rejects(() => checkStoryboardGate({ episodeId: "e1" }));
    await assert.rejects(() => runImageBatch({} as never));
    await assert.rejects(() => collectBatchResults({} as never));
    await assert.rejects(() => createPanelVersion({} as never));
    await assert.rejects(() => approvePanelImage({} as never));
    await assert.rejects(() => attachFile({} as never));
    await assert.rejects(() => addVersion({} as never));
    await assert.rejects(() => addUsage({} as never));
    await assert.rejects(() => addRelation({} as never));
    await assert.rejects(() => attachJobResult({} as never));
    assert.equal(isDramaBindingsAvailable(), false);
    assert.equal(isAssetsBindingsAvailable(), false);
});

test("listPanels is a query and answers empty rather than throwing", async () => {
    assert.deepEqual(await listPanels("item-1"), []);
});

test("the gate's refusal travels as the thrown message", async () => {
    // The binding returns void and reports refusal by throwing, so the wrapper must not translate
    // it: the message IS the four facts' verdict ("no approved storyboard version", "the gap
    // report has required assets missing"), and a caller shows it verbatim.
    await withDesktopCore(
        availableBindings({
            DramaBinding: {
                CheckStoryboardGate: () => {
                    throw new Error("This episode has no approved gap report.");
                },
            },
        }),
        async () => {
            await assert.rejects(
                () => checkStoryboardGate({ episodeId: "e1", storyboardVersionId: "v1" }),
                /no approved gap report/,
            );
        },
    );
});

test("the batch request reaches the binding with every field intact", async () => {
    // The four required fields are the ones the backend refuses without, and `perShotCandidates`
    // is the one the submit modal exists to set: a wrapper that dropped it would silently submit
    // one candidate per shot where the user asked for eight.
    let seen: unknown;
    await withDesktopCore(
        availableBindings({
            DramaBinding: {
                RunImageBatch: (request: unknown) => {
                    seen = request;
                    return { submissions: [{ shotId: "s1", itemId: "i1", candidateIndex: 1, jobId: "j1", duplicate: false }], duplicate: 0 };
                },
            },
        }),
        async () => {
            const result = await runImageBatch({
                storyboardVersionId: "board-v1",
                episodeId: "ep-1",
                projectId: "pr-1",
                perShotCandidates: 8,
                providerId: "channel-1",
                modelName: "image-model",
                promptSuffix: "night",
                seed: "42",
            });
            assert.equal(result.submissions.length, 1);
            assert.equal(result.duplicate, 0);
        },
    );
    assert.deepEqual(seen, {
        storyboardVersionId: "board-v1",
        episodeId: "ep-1",
        projectId: "pr-1",
        perShotCandidates: 8,
        providerId: "channel-1",
        modelName: "image-model",
        promptSuffix: "night",
        seed: "42",
    });
});

test("collect forwards the caller's asset map and job list", async () => {
    // `assetByItem` is the argument the backend deliberately does not invent — the caller creates
    // one asset per item and hands the map over — so a wrapper that dropped or renamed it would
    // break the chain in a way the backend reports as "no asset for this item".
    let seen: unknown;
    await withDesktopCore(
        availableBindings({
            DramaBinding: {
                CollectBatchResults: (request: unknown) => {
                    seen = request;
                    return [{ jobId: "j1", itemId: "i1", assetId: "a1", versionId: "v1", versionNumber: 1, duplicate: false }];
                },
            },
        }),
        async () => {
            const collected = await collectBatchResults({
                assetByItem: { "item-1": "asset-1" },
                jobIds: ["job-1", "job-2"],
            });
            assert.equal(collected.length, 1);
            assert.equal(collected[0].versionId, "v1");
        },
    );
    assert.deepEqual(seen, { assetByItem: { "item-1": "asset-1" }, jobIds: ["job-1", "job-2"] });
});

test("the approve request carries the whole gallery and the item revision", async () => {
    // Section 9.5's rule is enforced in the domain: the approved image must be one of the
    // candidates the caller SUPPLIED. A wrapper that sent only the chosen id would be refused by
    // the service, and one that dropped `expectedRevision` would fail to compile — which is the
    // reason this asserts the array rather than only the chosen id.
    let seen: unknown;
    await withDesktopCore(
        availableBindings({
            DramaBinding: {
                ApprovePanelImage: (request: unknown) => {
                    seen = request;
                    return { id: "panel-v1", storyboardItemId: "i1", versionNumber: 1, status: "approved", approvedImageAssetVersionId: "av-2" };
                },
            },
        }),
        async () => {
            const approved = await approvePanelImage({
                panelVersionId: "panel-v1",
                approvedImageAssetVersionId: "av-2",
                candidateVersionIds: ["av-1", "av-2"],
                expectedRevision: 3,
            });
            // The column the export joins on comes back from the binding, so the caller can show
            // the state it just wrote rather than assuming it.
            assert.equal(approved.approvedImageAssetVersionId, "av-2");
        },
    );
    assert.deepEqual(seen, {
        panelVersionId: "panel-v1",
        approvedImageAssetVersionId: "av-2",
        candidateVersionIds: ["av-1", "av-2"],
        expectedRevision: 3,
    });
});

test("the panel-version creation forwards its prompt and reason", async () => {
    let seen: unknown;
    await withDesktopCore(
        availableBindings({
            DramaBinding: {
                CreatePanelVersion: (request: unknown) => {
                    seen = request;
                    return { id: "panel-v1", storyboardItemId: "i1", versionNumber: 1, status: "draft" };
                },
            },
        }),
        async () => {
            await createPanelVersion({
                storyboardItemId: "i1",
                visualPrompt: "a lantern in the rain",
                changeReason: "batch panel generation",
            });
        },
    );
    assert.deepEqual(seen, {
        storyboardItemId: "i1",
        visualPrompt: "a lantern in the rain",
        changeReason: "batch panel generation",
    });
});

test("a query that reaches an available core returns the binding's rows unchanged", async () => {
    // The presence direction of the split: with a core, `listPanels` answers what it was given.
    // Without this the absence tests above would pass for a wrapper that always returned [].
    await withDesktopCore(
        availableBindings({
            DramaBinding: {
                ListPanels: () => [{ id: "panel-1", storyboardItemId: "item-1", versionNumber: 2, status: "candidate" }],
            },
        }),
        async () => {
            const panels = await listPanels("item-1");
            assert.equal(panels.length, 1);
            assert.equal(panels[0].id, "panel-1");
            assert.equal(panels[0].versionNumber, 2);
        },
    );
});

test("the asset writes forward their requests", async () => {
    const seen: string[] = [];
    await withDesktopCore(
        availableBindings({
            AssetsBinding: {
                AttachFile: (request: never) => {
                    seen.push("AttachFile:" + (request as { fileHash: string }).fileHash);
                    return { versionId: "v1", fileHash: "hash-1", role: "primary", ordinal: 0, createdAt: "" };
                },
                AddVersion: (request: never) => {
                    seen.push("AddVersion:" + (request as { assetId: string }).assetId);
                    return { id: "v1", assetId: "a1", versionNumber: 1, status: "draft" };
                },
                AddUsage: (request: never) => {
                    seen.push("AddUsage:" + (request as { consumerType: string }).consumerType);
                    return { assetVersionId: "v1", consumerType: "shot", consumerId: "s1" };
                },
                AddRelation: (request: never) => {
                    seen.push("AddRelation:" + (request as { type: string }).type);
                    return { sourceAssetVersionId: "v1", targetAssetVersionId: "v2", type: "variants" };
                },
                AttachJobResult: (request: never) => {
                    seen.push("AttachJobResult:" + (request as { jobId: string }).jobId);
                    return { id: "v1", assetId: "a1", versionNumber: 1, status: "candidate" };
                },
            },
        }),
        async () => {
            await attachFile({ versionId: "v1", fileHash: "hash-1" });
            await addVersion({ assetId: "a1" });
            await addUsage({ assetVersionId: "v1", consumerType: "shot", consumerId: "s1" });
            await addRelation({ sourceAssetVersionId: "v1", targetAssetVersionId: "v2", type: "variants" });
            await attachJobResult({ assetId: "a1", jobId: "job-9", files: [{ fileHash: "hash-1" }] } as never);
        },
    );
    assert.deepEqual(seen, [
        "AttachFile:hash-1",
        "AddVersion:a1",
        "AddUsage:shot",
        "AddRelation:variants",
        "AttachJobResult:job-9",
    ]);
});
