import type { desktop } from "@/wailsjs/go/models";
// The query placeholders need the generated classes as VALUES rather than as
// types, because a Wails DTO carries a convertValues method and so cannot be
// built from an object literal.
import { desktop as desktopModels } from "@/wailsjs/go/models";

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
        ImportBinding?: WailsBinding;
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

/**
 * isImportBindingsAvailable reports whether document import can be reached.
 *
 * It is a separate question from isDramaBindingsAvailable, because the two
 * surfaces can be composited independently: a build with no file store has an
 * import binding that fails closed on every call, and the source section should
 * say so rather than offering a control that cannot work.
 */
export function isImportBindingsAvailable(): boolean {
    const importing = getDesktopWindow()?.go?.desktop?.ImportBinding;
    if (!importing) return false;
    return typeof importing.PrecheckImport === "function" && typeof importing.ImportDocument === "function";
}

/** isExtractionAvailable reports whether an extractor is configured. */
export function isExtractionAvailable(): boolean {
    const importing = getDesktopWindow()?.go?.desktop?.ImportBinding;
    return Boolean(importing) && typeof importing?.ExtractChapterEventCandidates === "function";
}

/** isAssetsBindingsAvailable reports whether the asset bible can be reached. */
export function isAssetsBindingsAvailable(): boolean {
    const assets = getDesktopWindow()?.go?.desktop?.AssetsBinding;
    return Boolean(assets) && typeof assets?.ListAssets === "function" && typeof assets?.CreateAsset === "function";
}

let dramaModule: Promise<typeof import("@/wailsjs/go/desktop/DramaBinding")> | undefined;
let assetsModule: Promise<typeof import("@/wailsjs/go/desktop/AssetsBinding")> | undefined;
let importingModule: Promise<typeof import("@/wailsjs/go/desktop/ImportBinding")> | undefined;

async function loadDramaBinding() {
    dramaModule ??= import("@/wailsjs/go/desktop/DramaBinding").catch((error: unknown) => {
        dramaModule = undefined;
        throw error;
    });
    return dramaModule;
}

async function loadImportBinding() {
    importingModule ??= import("@/wailsjs/go/desktop/ImportBinding").catch((error: unknown) => {
        importingModule = undefined;
        throw error;
    });
    return importingModule;
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
    importingModule = undefined;
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

// The domain event stream. It is a query, so a browser session gets an empty
// answer rather than an error: a studio page renders its empty state instead of
// a failure toast.

export async function listDomainEvents(request: desktop.ListDomainEventsRequest): Promise<desktop.DomainEventDTO[]> {
    if (!isDramaBindingsAvailable()) return [];
    const { ListDomainEvents } = await loadDramaBinding();
    return ListDomainEvents(request);
}

export async function countDomainEvents(request: desktop.ListDomainEventsRequest): Promise<number> {
    if (!isDramaBindingsAvailable()) return 0;
    const { CountDomainEvents } = await loadDramaBinding();
    return CountDomainEvents(request);
}

/**
 * DOMAIN_EVENT_TYPES mirrors the section 17 vocabulary on the Go side.
 *
 * It is exported so a section can offer a filter without hardcoding a list that
 * would drift from the one the core validates against. The core refuses an
 * unknown type, so a stale entry here fails loudly rather than silently
 * filtering everything out.
 */
export const DOMAIN_EVENT_TYPES = [
    "ProjectCreated",
    "ProjectSettingsChanged",
    "ProjectRuleLocked",
    "SourceDocumentImported",
    "ChapterBoundariesConfirmed",
    "StoryFactAccepted",
    "StoryFactConflictOpened",
    "EpisodeCreated",
    "StorySkeletonApproved",
    "AdaptationStrategyApproved",
    "ScriptVersionCreated",
    "ScriptVersionApproved",
    "AssetVersionCreated",
    "AssetVersionApproved",
    "DirectorPlanApproved",
    "StoryboardVersionApproved",
    "CanvasProjectionCreated",
    "WorkflowStarted",
    "WorkflowStageChanged",
    "ReviewReportCreated",
    "UserGateDecided",
    "GenerationJobQueued",
    "GenerationJobSucceeded",
    "GenerationJobFailed",
    "MemoryCreated",
    "UpstreamVersionChanged",
    "ArtifactMarkedStale",
    "BackupCompleted",
] as const;

// ---------------------------------------------------------------------------
// Document import and event extraction (WP-06)
//
// These talk to ImportBinding rather than DramaBinding. Precheck and the paged
// read are QUERIES: a browser session gets an honest empty answer so the page
// renders an explanation. Import, confirmation and extraction are COMMANDS: a
// browser session throws, because silently doing nothing would be a lie about
// the project's state.
// ---------------------------------------------------------------------------

/** precheckImport inspects a document without storing it. */
export async function precheckImport(request: desktop.PrecheckImportRequest): Promise<desktop.PrecheckImportResult> {
    if (!isImportBindingsAvailable()) {
        // The generated DTOs carry a convertValues method, so an empty answer is
        // built through the constructor rather than as an object literal.
        return desktopModels.PrecheckImportResult.createFrom({});
    }
    const { PrecheckImport } = await loadImportBinding();
    return PrecheckImport(request);
}

/** importDocument stores a document, its version and its chapters. */
export async function importDocument(request: desktop.ImportDocumentRequest): Promise<desktop.ImportDocumentResult> {
    if (!isImportBindingsAvailable()) throw unavailableError();
    const { ImportDocument } = await loadImportBinding();
    return ImportDocument(request);
}

/** readDocumentRange reads one page of a version's normalized text. */
export async function readDocumentRange(request: desktop.ReadDocumentRangeRequest): Promise<desktop.DocumentRangeDTO> {
    if (!isImportBindingsAvailable()) {
        return desktopModels.DocumentRangeDTO.createFrom({});
    }
    const { ReadDocumentRange } = await loadImportBinding();
    return ReadDocumentRange(request);
}

/** confirmChapters confirms a version's detected boundaries. */
export async function confirmChapters(request: desktop.ConfirmChaptersRequestDTO): Promise<desktop.ChapterDTO[]> {
    if (!isImportBindingsAvailable()) throw unavailableError();
    const { ConfirmChapters } = await loadImportBinding();
    return ConfirmChapters(request);
}

/**
 * extractChapterEventCandidates reads one chapter and stores what it proposes.
 *
 * Everything it writes is a candidate, so the caller's next step is the review
 * queue rather than anything this function decides.
 */
export async function extractChapterEventCandidates(request: desktop.ExtractChapterRequest): Promise<desktop.ExtractionResultDTO> {
    if (!isImportBindingsAvailable()) throw unavailableError();
    const { ExtractChapterEventCandidates } = await loadImportBinding();
    return ExtractChapterEventCandidates(request);
}

// ---------------------------------------------------------------------------
// The story-graph reads WP-05 left open
//
// WP-05's binding exposed the fact layer's COMMANDS and no query that lists it,
// which the story-graph section recorded as a gap. These are the lists that
// close it. They are queries, so a browser session gets an empty list.
// ---------------------------------------------------------------------------

/** listStoryEntities reads a project's entities. An empty status means any. */
export async function listStoryEntities(projectId: string, status = ""): Promise<desktop.StoryEntityDTO[]> {
    if (!isDramaBindingsAvailable()) return [];
    const { ListStoryEntities } = await loadDramaBinding();
    return ListStoryEntities(projectId, status);
}

/** listStoryEvents reads a project's events in story order. */
export async function listStoryEvents(projectId: string, chapterId = "", status = ""): Promise<desktop.StoryEventDTO[]> {
    if (!isDramaBindingsAvailable()) return [];
    const { ListStoryEvents } = await loadDramaBinding();
    return ListStoryEvents(projectId, chapterId, status);
}

/** listStoryRelations reads a project's relations. */
export async function listStoryRelations(projectId: string, status = ""): Promise<desktop.StoryRelationDTO[]> {
    if (!isDramaBindingsAvailable()) return [];
    const { ListStoryRelations } = await loadDramaBinding();
    return ListStoryRelations(projectId, status);
}

/** listStoryEntityAliases reads one entity's alternative names. */
export async function listStoryEntityAliases(storyEntityId: string): Promise<desktop.StoryEntityAliasDTO[]> {
    if (!isDramaBindingsAvailable()) return [];
    const { ListStoryEntityAliases } = await loadDramaBinding();
    return ListStoryEntityAliases(storyEntityId);
}

/** listStoryEventParticipants reads one event's participants. */
export async function listStoryEventParticipants(storyEventId: string): Promise<desktop.StoryEventParticipantDTO[]> {
    if (!isDramaBindingsAvailable()) return [];
    const { ListStoryEventParticipants } = await loadDramaBinding();
    return ListStoryEventParticipants(storyEventId);
}

/** listStoryFactSources reads the evidence one fact cites. */
export async function listStoryFactSources(factType: string, factId: string): Promise<desktop.StoryFactSourceDTO[]> {
    if (!isDramaBindingsAvailable()) return [];
    const { ListStoryFactSources } = await loadDramaBinding();
    return ListStoryFactSources(factType, factId);
}

/** acceptStoryEntity confirms a candidate entity. */
export async function acceptStoryEntity(request: desktop.DecideStoryEntityRequest): Promise<desktop.StoryEntityDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { AcceptStoryEntity } = await loadDramaBinding();
    return AcceptStoryEntity(request);
}

/** rejectStoryEntity refuses a candidate entity. */
export async function rejectStoryEntity(request: desktop.DecideStoryEntityRequest): Promise<desktop.StoryEntityDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { RejectStoryEntity } = await loadDramaBinding();
    return RejectStoryEntity(request);
}

/**
 * ENTITY_TYPES mirrors the entity vocabulary the schema's CHECK constrains.
 *
 * It is exported so a section offers a filter without hardcoding a list that
 * would drift from the one the core validates against. The list has eight
 * values, not the six DOMAIN_MODEL section 6.1 names: migration 000014 widened it
 * to PRD FR-030's kinds, and the parity guard keeps Go, SQL and the JSON schema
 * agreeing. A value missing here would simply not be offered; a value present
 * that the core refuses fails loudly.
 */
export const ENTITY_TYPES = [
    "character",
    "location",
    "organization",
    "prop",
    "concept",
    "time",
    "relationship",
    "timeline_marker",
] as const;

