import type { desktop } from "@/wailsjs/go/models";

/**
 * Client for the drama bindings.
 *
 * The binding surface is one object (DramaBinding) carrying five services, so
 * this module is the single place the webview learns whether that surface
 * exists. Three rules it keeps:
 *
 * 1. A browser that has no Wails runtime gets an honest answer rather than an
 *    error. The studio shell renders an explanation instead of a broken page,
 *    because a development browser is a supported way to run the app and the
 *    drama facts live only in the Go core (ADR-BASE-004).
 * 2. A query that cannot be answered returns an empty result; a command that
 *    cannot be performed throws. A query has an honest "nothing yet" answer and
 *    a silent empty list is not a lie, while a command that silently does
 *    nothing is.
 * 3. No method here invents data. Every value comes back from the binding.
 */

type WailsBinding = Record<string, unknown>;

type WailsGo = {
    desktop?: {
        DramaBinding?: WailsBinding;
        AssetsBinding?: WailsBinding;
    };
};

type DesktopWindow = Window & { go?: WailsGo };

function getDesktopWindow(): DesktopWindow | null {
    return typeof window === "undefined" ? null : window;
}

/** Methods the drama surface must have for the studio to be usable. */
const REQUIRED_DRAMA_METHODS = [
    "ListEpisodes",
    "CreateEpisode",
    "ListSourceDocuments",
    "ListStaleMarks",
    "ListWorkflowRuns",
] as const;

/**
 * isDramaBindingsAvailable reports whether the studio can talk to the Go core.
 *
 * It checks a representative set of methods rather than every one, so an older
 * build that has the binding but lacks a newly added method is still treated as
 * available and fails on the specific call, which produces a clearer error than
 * a blanket "unavailable".
 */
export function isDramaBindingsAvailable(): boolean {
    const drama = getDesktopWindow()?.go?.desktop?.DramaBinding;
    if (!drama) return false;
    return REQUIRED_DRAMA_METHODS.every((method) => typeof drama[method] === "function");
}

/** isAssetsBindingsAvailable reports whether the asset bible can be reached. */
export function isAssetsBindingsAvailable(): boolean {
    const assets = getDesktopWindow()?.go?.desktop?.AssetsBinding;
    return Boolean(assets) && typeof assets?.ListAssets === "function" && typeof assets?.CreateAsset === "function";
}

let dramaModule: Promise<typeof import("@/wailsjs/go/desktop/DramaBinding")> | undefined;
let assetsModule: Promise<typeof import("@/wailsjs/go/desktop/AssetsBinding")> | undefined;

async function loadDramaBinding() {
    dramaModule ??= import("@/wailsjs/go/desktop/DramaBinding").catch((error: unknown) => {
        dramaModule = undefined;
        throw error;
    });
    return dramaModule;
}

async function loadAssetsBinding() {
    assetsModule ??= import("@/wailsjs/go/desktop/AssetsBinding").catch((error: unknown) => {
        assetsModule = undefined;
        throw error;
    });
    return assetsModule;
}

/** resetDramaClients clears the cached imports so tests can re-resolve them. */
export function resetDramaClients(): void {
    dramaModule = undefined;
    assetsModule = undefined;
}

function unavailableError(): Error {
    return new Error("The studio needs the desktop application; this browser session has no core to read from.");
}

// Queries. Each returns an empty result in a browser session rather than
// throwing, so a page renders its empty state instead of an error toast.

export async function listEpisodes(projectId: string): Promise<desktop.EpisodeDTO[]> {
    if (!isDramaBindingsAvailable()) return [];
    const { ListEpisodes } = await loadDramaBinding();
    return ListEpisodes(projectId);
}

export async function listSourceDocuments(projectId: string): Promise<desktop.SourceDocumentDTO[]> {
    if (!isDramaBindingsAvailable()) return [];
    const { ListSourceDocuments } = await loadDramaBinding();
    return ListSourceDocuments(projectId);
}

export async function listChapters(sourceDocumentVersionId: string): Promise<desktop.ChapterDTO[]> {
    if (!isDramaBindingsAvailable()) return [];
    const { ListChapters } = await loadDramaBinding();
    return ListChapters(sourceDocumentVersionId);
}

export async function listScenes(scriptVersionId: string): Promise<desktop.SceneDTO[]> {
    if (!isDramaBindingsAvailable()) return [];
    const { ListScenes } = await loadDramaBinding();
    return ListScenes(scriptVersionId);
}

export async function listShots(sceneId: string): Promise<desktop.ShotDTO[]> {
    if (!isDramaBindingsAvailable()) return [];
    const { ListShots } = await loadDramaBinding();
    return ListShots(sceneId);
}

export async function listStoryboardItems(storyboardVersionId: string): Promise<desktop.StoryboardItemDTO[]> {
    if (!isDramaBindingsAvailable()) return [];
    const { ListStoryboardItems } = await loadDramaBinding();
    return ListStoryboardItems(storyboardVersionId);
}

export async function listPanels(storyboardItemId: string): Promise<desktop.StoryboardPanelVersionDTO[]> {
    if (!isDramaBindingsAvailable()) return [];
    const { ListPanels } = await loadDramaBinding();
    return ListPanels(storyboardItemId);
}

export async function listWorkflowRuns(projectId: string): Promise<desktop.WorkflowRunDTO[]> {
    if (!isDramaBindingsAvailable()) return [];
    const { ListWorkflowRuns } = await loadDramaBinding();
    return ListWorkflowRuns(projectId);
}

export async function listStageRuns(workflowRunId: string): Promise<desktop.StageRunDTO[]> {
    if (!isDramaBindingsAvailable()) return [];
    const { ListStageRuns } = await loadDramaBinding();
    return ListStageRuns(workflowRunId);
}

export async function listWorkflowEvents(workflowRunId: string): Promise<desktop.WorkflowEventDTO[]> {
    if (!isDramaBindingsAvailable()) return [];
    const { ListWorkflowEvents } = await loadDramaBinding();
    return ListWorkflowEvents(workflowRunId);
}

export async function getReviewReport(stageRunId: string): Promise<desktop.ReviewReportDTO | null> {
    if (!isDramaBindingsAvailable()) return null;
    const { GetReviewReport } = await loadDramaBinding();
    return GetReviewReport(stageRunId);
}

export async function listStaleMarks(projectId: string): Promise<desktop.StaleMarkDTO[]> {
    if (!isDramaBindingsAvailable()) return [];
    const { ListStaleMarks } = await loadDramaBinding();
    return ListStaleMarks(projectId);
}

export async function listOpenStaleMarks(projectId: string): Promise<desktop.StaleMarkDTO[]> {
    if (!isDramaBindingsAvailable()) return [];
    const { ListOpenStaleMarks } = await loadDramaBinding();
    return ListOpenStaleMarks(projectId);
}

// Commands. Each throws when the core is absent, because a command that
// silently does nothing would leave the user believing it happened.

export async function createEpisode(request: desktop.CreateEpisodeRequest): Promise<desktop.EpisodeDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { CreateEpisode } = await loadDramaBinding();
    return CreateEpisode(request);
}

export async function updateEpisodeStatus(request: desktop.UpdateEpisodeStatusRequest): Promise<desktop.EpisodeDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { UpdateEpisodeStatus } = await loadDramaBinding();
    return UpdateEpisodeStatus(request);
}

export async function createSourceDocument(request: desktop.CreateSourceDocumentRequest): Promise<desktop.SourceDocumentDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { CreateSourceDocument } = await loadDramaBinding();
    return CreateSourceDocument(request);
}

export async function createStoryEntity(request: desktop.CreateStoryEntityRequest): Promise<desktop.StoryEntityDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { CreateStoryEntity } = await loadDramaBinding();
    return CreateStoryEntity(request);
}

export async function clearStaleMark(request: desktop.ClearStaleMarkRequest): Promise<boolean> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { ClearStaleMark } = await loadDramaBinding();
    return ClearStaleMark(request);
}

export async function waiveStaleMark(request: desktop.WaiveStaleMarkRequest): Promise<boolean> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { WaiveStaleMark } = await loadDramaBinding();
    return WaiveStaleMark(request);
}

export async function createWorkflowRun(request: desktop.CreateWorkflowRunRequest): Promise<desktop.WorkflowRunDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { CreateWorkflowRun } = await loadDramaBinding();
    return CreateWorkflowRun(request);
}

export async function ensureStoryboard(episodeId: string): Promise<desktop.StoryboardDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { EnsureStoryboard } = await loadDramaBinding();
    return EnsureStoryboard(episodeId);
}

export async function ensureScript(episodeId: string): Promise<desktop.ScriptDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { EnsureScript } = await loadDramaBinding();
    return EnsureScript(episodeId);
}

// Assets.

export async function listAssets(request: desktop.ListAssetsRequest): Promise<desktop.AssetDTO[]> {
    if (!isAssetsBindingsAvailable()) return [];
    const { ListAssets } = await loadAssetsBinding();
    return ListAssets(request);
}

export async function createAsset(request: desktop.CreateAssetRequest): Promise<desktop.AssetDTO> {
    if (!isAssetsBindingsAvailable()) throw unavailableError();
    const { CreateAsset } = await loadAssetsBinding();
    return CreateAsset(request);
}

export async function listAssetVersions(assetId: string): Promise<desktop.AssetVersionDTO[]> {
    if (!isAssetsBindingsAvailable()) return [];
    const { ListVersions } = await loadAssetsBinding();
    return ListVersions(assetId);
}

export async function approveAssetVersion(request: desktop.ApproveVersionRequest): Promise<desktop.AssetVersionDTO> {
    if (!isAssetsBindingsAvailable()) throw unavailableError();
    const { ApproveVersion } = await loadAssetsBinding();
    return ApproveVersion(request);
}
