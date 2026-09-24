import assert from "node:assert/strict";
import { test } from "node:test";

/**
 * The panel-image chain's decisions (ADR-0017), asserted as pure functions.
 *
 * These are the places the chain can silently do the wrong thing, and each one costs something
 * real if it is wrong: a changed asset name forks a row's versions into two assets, a duplicate
 * counted as fresh reports work that did not happen, a mis-read gate offers a button that cannot
 * work, and an unreadable conflict tells a user to retry what will fail again.
 *
 * The suite is `node:test` with no DOM, which is why the decisions are a module rather than
 * JSX — the same reason `job-scope.ts` and `model-selection.ts` exist beside it.
 */
import {
    approvedCount,
    approvedImageOf,
    assetByItem,
    assetNameFor,
    candidateVersionIds,
    formatGateReason,
    freshCandidates,
    gateBlocks,
    isStaleConflict,
    itemRevisionOf,
    jobCountFor,
    jobIdsOf,
    rowsNeedingAssets,
    rowsNeedingPanels,
    wantsBatchEvent,
} from "../desktop/panel-chain";
import type { desktop } from "@/wailsjs/go/models";

/** A storyboard row, as the table has it. */
function row(id: string, ordinal: number, revision = 1): desktop.StoryboardItemDTO {
    return { id, ordinal, revision, storyboardVersionId: "board-1", shotId: `shot-${ordinal}`, durationSeconds: 3, status: "draft", createdAt: "", updatedAt: "" };
}

/** An image asset, as `ListAssets` returns it. */
function asset(id: string, name: string): desktop.AssetDTO {
    return { id, name, projectId: "p1", type: "image", status: "active", createdAt: "", updatedAt: "", revision: 1 };
}

function panel(id: string, approvedImageAssetVersionId?: string): desktop.StoryboardPanelVersionDTO {
    return { id, storyboardItemId: "item-1", versionNumber: 1, status: "candidate", createdByType: "user", createdAt: "", approvedImageAssetVersionId };
}

function version(id: string, versionNumber: number): desktop.AssetVersionDTO {
    return { id, assetId: "a1", versionNumber, status: "candidate", createdByType: "agent", createdAt: "" };
}

test("the asset name is stable, so a second batch appends rather than forks", () => {
    // The name is the ONLY thing that ties a row to its asset across sessions. If it changed
    // shape between runs, the second batch would create a second asset per row and the gallery
    // would show two half-populated sets — a defect a user sees rather than one the backend
    // reports.
    assert.equal(assetNameFor(row("i1", 1)), "分镜图 #1");
    assert.equal(assetNameFor(row("i2", 12)), "分镜图 #12");
    // Derived from the ORDINAL and not from the id: an id is regenerated on a re-import, while
    // the ordinal is the shot's position, which is what a person reads on screen.
    assert.notEqual(assetNameFor(row("i1", 1)), assetNameFor(row("i9", 2)));
});

test("rowsNeedingAssets returns only the rows with no asset, and assetByItem maps the rest", () => {
    const items = [row("i1", 1), row("i2", 2), row("i3", 3)];
    const assets = [asset("a1", "分镜图 #1"), asset("a3", "分镜图 #3")];

    const missing = rowsNeedingAssets(items, assets);
    assert.deepEqual(missing.map((r) => r.id), ["i2"], "only the un-served row needs an asset");

    // The map is the argument the collect call refuses without, and it is keyed by ITEM id.
    assert.deepEqual(assetByItem(items, assets), { i1: "a1", i3: "a3" });
});

test("a row with no asset is OMITTED from the map rather than mapped to an empty string", () => {
    // An empty string would be looked up as an identifier and fail like a storage fault; an
    // absent key is the backend's own "no asset for this item" refusal, which names the cause.
    const map = assetByItem([row("i1", 1), row("i2", 2)], [asset("a1", "分镜图 #1")]);
    assert.deepEqual(Object.keys(map), ["i1"]);
    assert.equal(map.i2, undefined);
});

test("rowsNeedingPanels returns the rows §9.5 has nothing to approve for", () => {
    const items = [row("i1", 1), row("i2", 2)];
    const panels = { i1: panel("panel-1") };
    assert.deepEqual(rowsNeedingPanels(items, panels).map((r) => r.id), ["i2"]);
    // A row with a panel is NOT listed even when that panel has no approved image: the panel is
    // what an approval is recorded against, and creating a second one would orphan the first.
    assert.deepEqual(rowsNeedingPanels([row("i1", 1)], panels), []);
});

test("the gate blocks only once it has been READ", () => {
    // Before the read the button must be enabled rather than disabled: a control that starts
    // disabled and becomes enabled looks broken for the first frame, while one that starts
    // enabled and becomes disabled with a reason reads as a verdict.
    assert.equal(gateBlocks(false, ""), false);
    assert.equal(gateBlocks(false, "not read yet"), false);
    assert.equal(gateBlocks(true, ""), false, "a read gate with no reason does not block");
    assert.equal(gateBlocks(true, "   "), false, "whitespace is not a reason");
    assert.equal(gateBlocks(true, "This episode has no approved gap report."), true);
});

test("the gate's reason is shown with its source named", () => {
    assert.equal(formatGateReason("The quality gate refused:", "no approved gap report"), "The quality gate refused: no approved gap report");
    assert.equal(formatGateReason("The quality gate refused:", ""), "The quality gate refused:");
});

test("freshCandidates excludes duplicates AND rows with no version", () => {
    const collected: desktop.CollectedCandidateDTO[] = [
        { jobId: "j1", itemId: "i1", assetId: "a1", versionId: "v1", versionNumber: 1, duplicate: false },
        // A second collection of the same job: it reports the job, not a new version.
        { jobId: "j1", itemId: "i1", assetId: "a1", duplicate: true },
        // A job that has not settled produces neither, which the service skips rather than refuses.
        { jobId: "j2", itemId: "i2", assetId: "a2", duplicate: false },
    ];
    const fresh = freshCandidates(collected);
    assert.equal(fresh.length, 1);
    assert.equal(fresh[0].versionId, "v1");
    // The count a user is told is therefore the number of versions that appeared, not the number
    // of jobs that were asked about.
});

test("job identities come from the submissions, which is what polling and cancel use", () => {
    const submissions: desktop.BatchSubmissionDTO[] = [
        { shotId: "s1", itemId: "i1", candidateIndex: 1, jobId: "j1", duplicate: false },
        { shotId: "s1", itemId: "i1", candidateIndex: 2, jobId: "j2", duplicate: false },
        { shotId: "s2", itemId: "i2", candidateIndex: 1, jobId: "j3", duplicate: true },
    ];
    // A duplicate is INCLUDED: it names a job that already exists, and a cancel or a refresh that
    // dropped it would silently stop tracking work the user paid for.
    assert.deepEqual(jobIdsOf(submissions), ["j1", "j2", "j3"]);
});

test("the job count is rows times candidates, stated before the calls are made", () => {
    // No per-provider concurrency limit exists in this build, so this number is the only warning
    // about quota. The twelve-shot case with eight candidates is the one worth naming.
    assert.equal(jobCountFor(12, 8), 96);
    assert.equal(jobCountFor(12, 1), 12);
    assert.equal(jobCountFor(0, 8), 0);
});

test("the revision an approval guards on is the ITEM's", () => {
    // Not the panel version's: the backend checks the parent storyboard item, and sending the
    // panel's revision would be refused as stale on the first approval and on every one after.
    assert.equal(itemRevisionOf(row("i1", 1, 7)), 7);
    assert.equal(itemRevisionOf({ ...row("i1", 1), revision: undefined as never }), 0);
});

test("a stale conflict is told apart from a transport failure", () => {
    // The one refusal a user can act on is "changed in another window", and the action is reload.
    // An unknown failure keeps its own text, because renaming it would hide what happened.
    assert.equal(isStaleConflict("This row changed in another window. Reload and try again."), true);
    assert.equal(isStaleConflict("that panel version changed in ANOTHER WINDOW"), true);
    assert.equal(isStaleConflict("The provider refused the request."), false);
    assert.equal(isStaleConflict(""), false);
});

test("the approved image is read from the PANEL, and its absence is empty rather than a throw", () => {
    // §9.5 records the approved image on the panel version. The storyboard row has no such field,
    // so a component reading it from the row would show "—" for every approved shot.
    const panels = { i1: panel("panel-1", "v-approved") };
    assert.equal(approvedImageOf(row("i1", 1), panels), "v-approved");
    assert.equal(approvedImageOf(row("i2", 2), panels), "", "a row with no panel has no approved image");

    assert.equal(approvedCount([row("i1", 1), row("i2", 2), row("i3", 3)], { i1: panel("p1", "v1"), i3: panel("p3") }), 1);
});

test("an approval carries the WHOLE gallery the panel is showing", () => {
    // Section 9.5's rule — checked in the domain — is that the approved image must be one of the
    // candidates the caller SUPPLIED. Sending only the chosen id is refused, so the gallery is
    // what travels.
    const versions = [version("v1", 1), version("v2", 2)];
    assert.deepEqual(candidateVersionIds(versions), ["v1", "v2"]);
    assert.deepEqual(candidateVersionIds([]), []);
});

test("a job event is wanted only when it names a job this batch submitted", () => {
    // Another project's job moving must not cause a read here: the panel would re-read on every
    // event in the application, which is a provider-backed read on a foreign clock.
    assert.equal(wantsBatchEvent("j1", ["j1", "j2"]), true);
    assert.equal(wantsBatchEvent("j9", ["j1", "j2"]), false);
    assert.equal(wantsBatchEvent(undefined, ["j1"]), false);
    assert.equal(wantsBatchEvent("j1", []), false);
});
