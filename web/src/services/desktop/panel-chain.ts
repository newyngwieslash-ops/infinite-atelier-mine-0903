import type { desktop } from "@/wailsjs/go/models";

/**
 * The panel-image chain's decisions, as pure functions (ADR-0017).
 *
 * They live here rather than inside the component for the reason this repository keeps
 * extracting modules like `job-scope.ts` and `model-selection.ts`: the suite is `node:test` with no
 * DOM, so a decision that only exists inside JSX cannot be asserted at all — and these are the
 * decisions worth asserting. Each one is a place the chain can silently do the wrong thing:
 *
 *  - the ASSET NAME decides whether a second batch appends versions to the rows' assets or
 *    creates a parallel set, which is a difference a user sees as duplicated panels;
 *  - the ASSET MAP is the argument `CollectBatchResults` refuses without;
 *  - the GATE reading decides whether the generate button is offered;
 *  - the FRESH filter decides what "collected 3 candidates" means, and a duplicate counted as
 *    fresh would report work that did not happen;
 *  - the CONFLICT reading decides whether a refused approval says "reload" or shows a
 *    transport error.
 */

/** The name one row's candidate versions hang off, derived so a re-run reuses the asset. */
export function assetNameFor(row: desktop.StoryboardItemDTO): string {
    return `分镜图 #${row.ordinal}`;
}

/**
 * assetByItem builds the map `CollectBatchResults` requires.
 *
 * It is keyed by the storyboard ITEM id because that is what the backend joins on, and a row
 * whose asset is missing is OMITTED rather than mapped to an empty string: the backend reads an
 * absent key as "this job's item has no asset", which is a refusal, while an empty string would
 * be looked up as an identifier and fail in a way that reads like a storage fault.
 */
export function assetByItem(
    items: desktop.StoryboardItemDTO[],
    assets: desktop.AssetDTO[],
): Record<string, string> {
    const map: Record<string, string> = {};
    for (const row of items) {
        const asset = assets.find((candidate) => candidate.name === assetNameFor(row));
        if (asset) map[row.id] = asset.id;
    }
    return map;
}

/**
 * rowsNeedingAssets returns the rows with no image asset yet.
 *
 * The caller creates one per returned row and REUSES the rest, which is what makes a second
 * batch append versions rather than fork the asset.
 */
export function rowsNeedingAssets(
    items: desktop.StoryboardItemDTO[],
    assets: desktop.AssetDTO[],
): desktop.StoryboardItemDTO[] {
    return items.filter((row) => !assets.some((candidate) => candidate.name === assetNameFor(row)));
}

/** rowsNeedingPanels returns the rows with no panel version, which §9.5's approval needs. */
export function rowsNeedingPanels(
    items: desktop.StoryboardItemDTO[],
    panelByItem: Record<string, desktop.StoryboardPanelVersionDTO>,
): desktop.StoryboardItemDTO[] {
    return items.filter((row) => !panelByItem[row.id]);
}

/**
 * gateBlocks reports whether the gate's reading forbids a batch.
 *
 * The gate REFUSES by throwing, so "blocked" is "the read produced a reason". A read that has
 * not happened yet (`checked` false) is NOT a block: the button would otherwise be disabled
 * during the first paint and look broken rather than pending.
 */
export function gateBlocks(checked: boolean, reason: string): boolean {
    return checked && reason.trim() !== "";
}

/** formatGateReason shapes the refusal for display, naming the gate as the source. */
export function formatGateReason(prefix: string, reason: string): string {
    return `${prefix} ${reason}`.trim();
}

/**
 * freshCandidates keeps the collected candidates that were actually created.
 *
 * A second collection of the same jobs reports them as duplicates with no version, and counting
 * one as fresh would tell a user that work happened when it did not.
 */
export function freshCandidates(collected: desktop.CollectedCandidateDTO[]): desktop.CollectedCandidateDTO[] {
    // `versionId` is OPTIONAL on the DTO and arrives as `undefined` for a job that was skipped
    // (not settled) or reported as a duplicate. Testing it against "" alone would count
    // `undefined` as a fresh candidate — the test for this caught exactly that, and the cost
    // would have been a user told that two versions appeared when none did.
    return collected.filter((candidate) => !candidate.duplicate && Boolean(candidate.versionId));
}

/** jobIdsOf returns the ids a batch's submissions carry, which is what polling and cancel use. */
export function jobIdsOf(submissions: desktop.BatchSubmissionDTO[]): string[] {
    return submissions.map((submission) => submission.jobId);
}

/**
 * jobCountFor states how many provider calls a batch will make, before it makes them.
 *
 * This build has no per-provider concurrency limit (handoff P0-2, FR-150), so the count is the
 * only warning a user gets about quota. It is computed rather than left implicit because the
 * number is not obvious: it is rows × candidates, and with eight candidates on a twelve-shot
 * board that is ninety-six calls.
 */
export function jobCountFor(rowCount: number, candidatesPerShot: number): number {
    return rowCount * candidatesPerShot;
}

/** itemRevisionOf reads the revision an approval guards on. */
export function itemRevisionOf(row: desktop.StoryboardItemDTO): number {
    return Number(row.revision ?? 0);
}

/**
 * isStaleConflict reports whether a refusal was the revision guard rather than a transport error.
 *
 * The one failure a user can act on is "this changed in another window", and it is acted on by
 * reloading. Everything else is shown as it came, because translating an unknown failure into a
 * known-sounding one hides what actually happened.
 */
export function isStaleConflict(message: string): boolean {
    return /another window/i.test(message);
}

/**
 * approvedImageOf reads the image in force for a row.
 *
 * From the PANEL version rather than from the row: §9.5 records the approved image there, and
 * the storyboard item DTO has no such field — which is a fact the compiler caught rather than a
 * design choice made up front.
 */
export function approvedImageOf(
    row: desktop.StoryboardItemDTO,
    panelByItem: Record<string, desktop.StoryboardPanelVersionDTO>,
): string {
    return panelByItem[row.id]?.approvedImageAssetVersionId ?? "";
}

/** approvedCount counts the rows that have an image in force, for the panel's progress line. */
export function approvedCount(
    items: desktop.StoryboardItemDTO[],
    panelByItem: Record<string, desktop.StoryboardPanelVersionDTO>,
): number {
    return items.filter((row) => approvedImageOf(row, panelByItem) !== "").length;
}

/** candidateVersionIds is the whole gallery an approval must carry (§9.5). */
export function candidateVersionIds(versions: desktop.AssetVersionDTO[]): string[] {
    return versions.map((version) => version.id);
}

/** wantsBatchEvent reports whether a job event concerns the batch this panel submitted. */
export function wantsBatchEvent(jobId: string | undefined, batchJobIds: string[]): boolean {
    return Boolean(jobId) && batchJobIds.includes(jobId as string);
}
