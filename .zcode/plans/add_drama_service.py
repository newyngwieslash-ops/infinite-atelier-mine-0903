import io

p = "web/src/services/desktop/drama.ts"
s = io.open(p, encoding="utf-8").read()

anchor = "// Assets.\n\nexport async function listAssets"
new = '''// WP-08: the two upstream version families, a script version's content, the
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

export async function listAssets'''
assert anchor in s, "anchor"
s = s.replace(anchor, new, 1)
io.open(p, "w", encoding="utf-8", newline="\n").write(s)
print("ok")
