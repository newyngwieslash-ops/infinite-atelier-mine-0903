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
        ImportUploadBinding?: WailsBinding;
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

/** isImportUploadAvailable reports whether the chunked transfer is reachable. */
export function isImportUploadAvailable(): boolean {
    const uploading = getDesktopWindow()?.go?.desktop?.ImportUploadBinding;
    return Boolean(uploading) && typeof uploading?.BeginImportUpload === "function";
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
let importUploadModule: Promise<typeof import("@/wailsjs/go/desktop/ImportUploadBinding")> | undefined;

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

async function loadImportUploadBinding() {
    importUploadModule ??= import("@/wailsjs/go/desktop/ImportUploadBinding").catch((error: unknown) => {
        importUploadModule = undefined;
        throw error;
    });
    return importUploadModule;
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
    importUploadModule = undefined;
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

// WP-08: the two upstream version families, a script version's content, the
// field locks, the version diff, the canvas projection and the stage commands.
//
// Every function below follows the module's two rules: a QUERY has an honest
// empty answer when the core is absent, and a COMMAND throws. The split matters
// most for the stage commands — a "run stage" that silently did nothing would
// leave a person believing a model was working.

// Story skeleton.

export async function createStorySkeletonVersion(
    request: desktop.CreateStorySkeletonVersionRequest,
): Promise<desktop.StorySkeletonVersionDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { CreateStorySkeletonVersion } = await loadDramaBinding();
    return CreateStorySkeletonVersion(request);
}

export async function listStorySkeletonVersions(episodeId: string): Promise<desktop.StorySkeletonVersionDTO[]> {
    if (!isDramaBindingsAvailable()) return [];
    const { ListStorySkeletonVersions } = await loadDramaBinding();
    return ListStorySkeletonVersions(episodeId);
}

export async function approveStorySkeletonVersion(versionId: string): Promise<desktop.StorySkeletonVersionDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { ApproveStorySkeletonVersion } = await loadDramaBinding();
    return ApproveStorySkeletonVersion(versionId);
}

// Adaptation strategy.

export async function createAdaptationStrategyVersion(
    request: desktop.CreateAdaptationStrategyVersionRequest,
): Promise<desktop.AdaptationStrategyVersionDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { CreateAdaptationStrategyVersion } = await loadDramaBinding();
    return CreateAdaptationStrategyVersion(request);
}

export async function listAdaptationStrategyVersions(episodeId: string): Promise<desktop.AdaptationStrategyVersionDTO[]> {
    if (!isDramaBindingsAvailable()) return [];
    const { ListAdaptationStrategyVersions } = await loadDramaBinding();
    return ListAdaptationStrategyVersions(episodeId);
}

export async function approveAdaptationStrategyVersion(
    versionId: string,
): Promise<desktop.AdaptationStrategyVersionDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { ApproveAdaptationStrategyVersion } = await loadDramaBinding();
    return ApproveAdaptationStrategyVersion(versionId);
}

// Script structure.

export async function getScriptStructure(scriptVersionId: string): Promise<desktop.ScriptStructureDTO> {
    // A QUERY with no honest empty answer: the structure is the artifact, and an empty script
    // structure would read as a version with no scenes. So this throws when the core is absent,
    // unlike the list queries above.
    if (!isDramaBindingsAvailable()) return desktopModels.ScriptStructureDTO.createFrom({
        scriptVersionId,
        scenes: [],
        estimatedDurationSeconds: 0,
    });
    const { GetScriptStructure } = await loadDramaBinding();
    return GetScriptStructure(scriptVersionId);
}

export async function saveScriptStructure(request: desktop.SaveScriptStructureRequest): Promise<desktop.ScriptVersionDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { SaveScriptStructure } = await loadDramaBinding();
    return SaveScriptStructure(request);
}

// Field locks (AC-SCRIPT-002).

export async function lockableFields(): Promise<desktop.LockableFieldDTO[]> {
    if (!isDramaBindingsAvailable()) return [];
    const { LockableFields } = await loadDramaBinding();
    return LockableFields();
}

export async function listScriptFieldLocks(versionId: string): Promise<desktop.FieldLockDTO[]> {
    if (!isDramaBindingsAvailable()) return [];
    const { ListScriptFieldLocks } = await loadDramaBinding();
    return ListScriptFieldLocks(versionId);
}

export async function lockScriptField(
    versionId: string,
    field: string,
    lockedBy: string,
): Promise<desktop.FieldLockDTO[]> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { LockScriptField } = await loadDramaBinding();
    return LockScriptField(versionId, field, lockedBy);
}

/** unlockScriptFieldFor is lockScriptField's counterpart with the argument order the panel uses. */
export async function unlockScriptFieldFor(versionId: string, field: string): Promise<desktop.FieldLockDTO[]> {
    return unlockScriptField(versionId, field);
}

export async function unlockScriptField(versionId: string, field: string): Promise<desktop.FieldLockDTO[]> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { UnlockScriptField } = await loadDramaBinding();
    return UnlockScriptField(versionId, field);
}

export async function setDialogueLineLocked(
    request: desktop.SetDialogueLineLockedRequest,
): Promise<desktop.DialogueLineDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { SetDialogueLineLocked } = await loadDramaBinding();
    return SetDialogueLineLocked(request);
}

// Version diff.

export async function diffVersions(
    family: string,
    fromId: string,
    toId: string,
): Promise<desktop.VersionDiffDTO> {
    if (!isDramaBindingsAvailable()) {
        return desktopModels.VersionDiffDTO.createFrom({
            family,
            fromId,
            toId,
            added: 0,
            removed: 0,
            modified: 0,
            unchanged: 0,
        });
    }
    const { DiffVersions } = await loadDramaBinding();
    return DiffVersions(family, fromId, toId);
}

// Canvas projection.

export async function projectScriptVersion(
    request: desktop.ProjectScriptVersionRequest,
): Promise<desktop.ProjectScriptVersionResult> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { ProjectScriptVersion } = await loadDramaBinding();
    return ProjectScriptVersion(request);
}

// The script stages.

export async function runScriptStage(
    request: desktop.RunScriptStageRequest,
): Promise<desktop.ScriptStageResultDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { RunScriptStage } = await loadDramaBinding();
    return RunScriptStage(request);
}

export async function runScriptSupervision(
    request: desktop.RunScriptSupervisionRequest,
): Promise<desktop.ReviewReportDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { RunScriptSupervision } = await loadDramaBinding();
    return RunScriptSupervision(request);
}

export async function applyScriptGate(request: desktop.ApplyScriptGateRequest): Promise<desktop.StageRunDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { ApplyScriptGate } = await loadDramaBinding();
    return ApplyScriptGate(request);
}

export async function startScriptRevision(stageRunId: string): Promise<desktop.StageRunDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { StartScriptRevision } = await loadDramaBinding();
    return StartScriptRevision(stageRunId);
}

export async function manualEditScript(
    request: desktop.ManualEditScriptRequest,
): Promise<desktop.ScriptStageResultDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { ManualEditScript } = await loadDramaBinding();
    return ManualEditScript(request);
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

/** splitChapter divides one chapter into two. */
export async function splitChapter(request: desktop.SplitChapterRequest): Promise<desktop.ChapterDTO[]> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { SplitChapter } = await loadDramaBinding();
    return SplitChapter(request);
}

/** mergeChapter absorbs the chapter after the given one. */
export async function mergeChapter(request: desktop.MergeChapterRequest): Promise<desktop.ChapterDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { MergeChapter } = await loadDramaBinding();
    return MergeChapter(request);
}

/** lockStoryEntity pins a fact so a later pass cannot revise it. */
export async function lockStoryEntity(request: desktop.LockStoryEntityRequest): Promise<desktop.StoryEntityDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { LockStoryEntity } = await loadDramaBinding();
    return LockStoryEntity(request);
}

/** unlockStoryEntity releases a lock, leaving the decision it protected standing. */
export async function unlockStoryEntity(request: desktop.LockStoryEntityRequest): Promise<desktop.StoryEntityDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { UnlockStoryEntity } = await loadDramaBinding();
    return UnlockStoryEntity(request);
}

/** lockStoryEvent pins an event. */
export async function lockStoryEvent(request: desktop.LockStoryEntityRequest): Promise<desktop.StoryEventDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { LockStoryEvent } = await loadDramaBinding();
    return LockStoryEvent(request);
}

/** unlockStoryEvent releases an event's lock. */
export async function unlockStoryEvent(request: desktop.LockStoryEntityRequest): Promise<desktop.StoryEventDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { UnlockStoryEvent } = await loadDramaBinding();
    return UnlockStoryEvent(request);
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


// ---------------------------------------------------------------------------
// Conflicts (section 6.7)
//
// WP-05 exposed the commands that open and resolve a conflict and no query that
// lists them, so a disagreement could be recorded and never found again. These
// are the list and the two commands that close the loop.
// ---------------------------------------------------------------------------

/** listStoryConflicts reads a project's recorded conflicts, newest first. */
export async function listStoryConflicts(projectId: string, status = ""): Promise<desktop.StoryFactConflictDTO[]> {
    if (!isDramaBindingsAvailable()) return [];
    const { ListStoryConflicts } = await loadDramaBinding();
    return ListStoryConflicts(projectId, status);
}

/** openStoryConflict records a disagreement between two facts. */
export async function openStoryConflict(request: desktop.OpenStoryConflictRequest): Promise<desktop.StoryFactConflictDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { OpenStoryConflict } = await loadDramaBinding();
    return OpenStoryConflict(request);
}

/** resolveStoryConflict closes a conflict with a stated resolution. */
export async function resolveStoryConflict(request: desktop.ResolveStoryConflictRequest): Promise<desktop.StoryFactConflictDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { ResolveStoryConflict } = await loadDramaBinding();
    return ResolveStoryConflict(request);
}

/**
 * importDocumentByChunks sends a document in bounded pieces and imports it.
 *
 * A Wails message carries text, so binary crosses as base64. Sending a whole
 * novel in one message would build a JSON array with one element per BYTE —
 * about 300,000 elements for a 100,000-character Chinese document — twice, once
 * in the browser and once in the Go decoder, on the webview's main thread. That
 * is work proportional to the document on the thread that paints the UI.
 *
 * This walks the same protocol the legacy migration uses for media: begin,
 * append bounded chunks, finish. The size declared at the start is verified
 * against what arrives, so a lost chunk fails the import instead of silently
 * storing a truncated document.
 *
 * The chunk ceiling comes from the core rather than from a constant here: the
 * core refuses a chunk above its own limit, and a number chosen on this side
 * would be a second place for it to be wrong.
 */
export async function importDocumentByChunks(
    // The bytes are a Uint8Array rather than the request DTO's `content` array on
    // purpose. The DTO carries bytes as a JSON number array, which for a novel is
    // hundreds of thousands of numbers; taking the view avoids ever building it.
    request: Omit<desktop.ImportDocumentRequest, "content"> & { bytes: Uint8Array },
    onProgress?: (progress: { sent: number; total: number }) => void,
): Promise<desktop.ImportDocumentResult> {
    if (!isImportUploadAvailable()) throw unavailableError();
    const { BeginImportUpload, AppendImportUploadChunk, FinishImportUpload, AbortImportUpload } = await loadImportUploadBinding();

    const bytes = request.bytes;
    const begun = await BeginImportUpload({
        projectId: request.projectId,
        name: request.name,
        format: request.format,
        totalBytes: bytes.byteLength,
        documentId: request.documentId,
        documentType: request.documentType,
        confirmDuplicate: request.confirmDuplicate,
    } as desktop.BeginImportUploadRequest);

    try {
        const chunkBytes = begun.chunkBytes > 0 ? begun.chunkBytes : 64 * 1024;
        for (let offset = 0; offset < bytes.byteLength; offset += chunkBytes) {
            const slice = bytes.subarray(offset, Math.min(offset + chunkBytes, bytes.byteLength));
            await AppendImportUploadChunk({
                uploadId: begun.uploadId,
                chunk: bytesToBase64(slice),
            } as desktop.AppendImportUploadChunkRequest);
            onProgress?.({ sent: Math.min(offset + chunkBytes, bytes.byteLength), total: bytes.byteLength });
        }
        return await FinishImportUpload({ uploadId: begun.uploadId } as desktop.FinishImportUploadRequest);
    } catch (error) {
        // A cancelled transfer releases its buffer rather than leaving the core
        // holding bytes until the next upload is begun.
        try {
            await AbortImportUpload(begun.uploadId);
        } catch {
            // The abort is best effort: the original failure is what the caller
            // needs to see, and swallowing it here would replace a useful error
            // with a confusing one.
        }
        throw error;
    }
}

/**
 * bytesToBase64 encodes one chunk.
 *
 * It builds the binary string in bounded pieces because
 * `String.fromCharCode(...slice)` spreads the whole chunk into arguments, and a
 * large spread overflows the call stack — the same size problem in a smaller
 * place.
 */
function bytesToBase64(bytes: Uint8Array): string {
    const CHUNK = 8192;
    let binary = "";
    for (let offset = 0; offset < bytes.length; offset += CHUNK) {
        const slice = bytes.subarray(offset, Math.min(offset + CHUNK, bytes.length));
        binary += String.fromCharCode(...Array.from(slice));
    }
    return btoa(binary);
}

// --- The production stages (WP-09) ---
//
// These are the reads and commands the Director and Storyboard Table sections make. Each
// one names what it is about in its own arguments rather than relying on a UI selection:
// a call that resolved "the current episode" from a store would be a command whose subject
// is a piece of local state, and a reload would change it.

export async function listDirectorPlanVersions(episodeId: string): Promise<desktop.DirectorPlanVersionDTO[]> {
    if (!isDramaBindingsAvailable()) return [];
    const { ListDirectorPlanVersions } = await loadDramaBinding();
    return ListDirectorPlanVersions(episodeId);
}

export async function getDirectorPlanVersion(id: string): Promise<desktop.DirectorPlanVersionDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { GetDirectorPlanVersion } = await loadDramaBinding();
    return GetDirectorPlanVersion(id);
}

export async function approveDirectorPlanVersion(
    request: desktop.ApproveDirectorPlanVersionRequest,
): Promise<desktop.DirectorPlanVersionDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { ApproveDirectorPlanVersion } = await loadDramaBinding();
    return ApproveDirectorPlanVersion(request);
}

export async function listStoryboardVersions(storyboardId: string): Promise<desktop.StoryboardVersionDTO[]> {
    if (!isDramaBindingsAvailable()) return [];
    const { ListStoryboardVersions } = await loadDramaBinding();
    return ListStoryboardVersions(storyboardId);
}

export async function getStoryboardVersion(id: string): Promise<desktop.StoryboardVersionDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { GetStoryboardVersion } = await loadDramaBinding();
    return GetStoryboardVersion(id);
}

export async function approveStoryboardVersion(
    request: desktop.ApproveStoryboardVersionRequest,
): Promise<desktop.StoryboardVersionDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { ApproveStoryboardVersion } = await loadDramaBinding();
    return ApproveStoryboardVersion(request);
}

export async function approvedStoryboardVersionId(storyboardId: string): Promise<string> {
    if (!isDramaBindingsAvailable()) return "";
    const { ApprovedStoryboardVersionID } = await loadDramaBinding();
    return ApprovedStoryboardVersionID(storyboardId);
}

export async function updateStoryboardItem(
    request: desktop.UpdateStoryboardItemRequest,
): Promise<desktop.StoryboardItemDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { UpdateStoryboardItem } = await loadDramaBinding();
    return UpdateStoryboardItem(request);
}

export async function listPanelVersions(storyboardItemId: string): Promise<desktop.StoryboardPanelVersionDTO[]> {
    if (!isDramaBindingsAvailable()) return [];
    const { ListPanelVersions } = await loadDramaBinding();
    return ListPanelVersions(storyboardItemId);
}

// The production stages go through the SAME binding the script ones do, and the claim that
// makes that true was verified rather than assumed: the binding holds BOTH pipelines in
// separate slots and routes on the stage a command names, so a production stage cannot reach
// the script layer — and if it did, the layer would refuse it with the stage named.
export const runStoryboardStage = runScriptStage;
export const runStoryboardSupervision = runScriptSupervision;

// The per-shot overrides document, which is where a previs camera goes.
//
// Section 9.1 puts a per-shot camera override in the plan's `shot_overrides_json`, so this
// is the command a camera write-back uses — not the item update, which has no column for a
// camera and would have to borrow an unrelated field to carry one.
export async function setShotOverrides(
    request: desktop.SetShotOverridesRequest,
): Promise<desktop.DirectorPlanVersionDTO> {
    if (!isDramaBindingsAvailable()) throw unavailableError();
    const { SetShotOverrides } = await loadDramaBinding();
    return SetShotOverrides(request);
}

// --- Asset versions, usages and the approval impact (WP-09) ---
//
// The impact is the READ the approval dialog makes, and section 8.2 requires it BEFORE the
// switch: `approveAssetVersion` refuses without the caller's acknowledgement, so a UI that
// could not ask this question could only send the acknowledgement blind.

export async function listAssetUsages(versionId: string): Promise<desktop.AssetUsageDTO[]> {
    if (!isAssetsBindingsAvailable()) return [];
    const { ListUsages } = await loadAssetsBinding();
    return ListUsages(versionId);
}

export async function getApprovalImpact(versionId: string): Promise<desktop.ApprovalImpactDTO> {
    if (!isAssetsBindingsAvailable()) throw unavailableError();
    const { GetApprovalImpact } = await loadAssetsBinding();
    return GetApprovalImpact(versionId);
}
