export namespace desktop {
	
	export class AddSourceDocumentVersionRequest {
	    sourceDocumentId: string;
	    physicalFileId?: string;
	    normalizedTextFileId: string;
	    contentHash?: string;
	    mimeType?: string;
	    encoding?: string;
	    charCount?: number;
	    importMetadataJson?: string;
	    createdByType?: string;
	
	    static createFrom(source: any = {}) {
	        return new AddSourceDocumentVersionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourceDocumentId = source["sourceDocumentId"];
	        this.physicalFileId = source["physicalFileId"];
	        this.normalizedTextFileId = source["normalizedTextFileId"];
	        this.contentHash = source["contentHash"];
	        this.mimeType = source["mimeType"];
	        this.encoding = source["encoding"];
	        this.charCount = source["charCount"];
	        this.importMetadataJson = source["importMetadataJson"];
	        this.createdByType = source["createdByType"];
	    }
	}
	export class AddVersionRequest {
	    assetId: string;
	    basedOnVersionId?: string;
	    prompt?: string;
	    negativePrompt?: string;
	    providerConfigId?: string;
	    modelConfigId?: string;
	    modelParameters?: string;
	    generationJobId?: string;
	    metadata?: string;
	    createdByType?: string;
	    legacyMetadata?: string;
	
	    static createFrom(source: any = {}) {
	        return new AddVersionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.assetId = source["assetId"];
	        this.basedOnVersionId = source["basedOnVersionId"];
	        this.prompt = source["prompt"];
	        this.negativePrompt = source["negativePrompt"];
	        this.providerConfigId = source["providerConfigId"];
	        this.modelConfigId = source["modelConfigId"];
	        this.modelParameters = source["modelParameters"];
	        this.generationJobId = source["generationJobId"];
	        this.metadata = source["metadata"];
	        this.createdByType = source["createdByType"];
	        this.legacyMetadata = source["legacyMetadata"];
	    }
	}
	export class ApprovePanelImageRequest {
	    panelVersionId: string;
	    approvedImageAssetVersionId: string;
	    candidateVersionIds?: string[];
	    expectedRevision: number;
	
	    static createFrom(source: any = {}) {
	        return new ApprovePanelImageRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.panelVersionId = source["panelVersionId"];
	        this.approvedImageAssetVersionId = source["approvedImageAssetVersionId"];
	        this.candidateVersionIds = source["candidateVersionIds"];
	        this.expectedRevision = source["expectedRevision"];
	    }
	}
	export class ApproveScriptVersionRequest {
	    scriptVersionId: string;
	
	    static createFrom(source: any = {}) {
	        return new ApproveScriptVersionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scriptVersionId = source["scriptVersionId"];
	    }
	}
	export class ApproveVersionRequest {
	    versionId: string;
	    impactAcknowledged: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ApproveVersionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.versionId = source["versionId"];
	        this.impactAcknowledged = source["impactAcknowledged"];
	    }
	}
	export class AssetDTO {
	    id: string;
	    projectId: string;
	    type: string;
	    name: string;
	    description?: string;
	    storyEntityId?: string;
	    currentApprovedVersionId?: string;
	    status: string;
	    deletedAt?: string;
	    legacyMetadata?: string;
	    createdAt: string;
	    updatedAt: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new AssetDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.projectId = source["projectId"];
	        this.type = source["type"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.storyEntityId = source["storyEntityId"];
	        this.currentApprovedVersionId = source["currentApprovedVersionId"];
	        this.status = source["status"];
	        this.deletedAt = source["deletedAt"];
	        this.legacyMetadata = source["legacyMetadata"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.revision = source["revision"];
	    }
	}
	export class AssetFileDTO {
	    versionId: string;
	    fileHash: string;
	    role: string;
	    ordinal: number;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new AssetFileDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.versionId = source["versionId"];
	        this.fileHash = source["fileHash"];
	        this.role = source["role"];
	        this.ordinal = source["ordinal"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class AssetVersionDTO {
	    id: string;
	    assetId: string;
	    versionNumber: number;
	    status: string;
	    basedOnVersionId?: string;
	    prompt?: string;
	    negativePrompt?: string;
	    providerConfigId?: string;
	    modelConfigId?: string;
	    modelParameters?: string;
	    generationJobId?: string;
	    metadata?: string;
	    createdByType: string;
	    legacyMetadata?: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new AssetVersionDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.assetId = source["assetId"];
	        this.versionNumber = source["versionNumber"];
	        this.status = source["status"];
	        this.basedOnVersionId = source["basedOnVersionId"];
	        this.prompt = source["prompt"];
	        this.negativePrompt = source["negativePrompt"];
	        this.providerConfigId = source["providerConfigId"];
	        this.modelConfigId = source["modelConfigId"];
	        this.modelParameters = source["modelParameters"];
	        this.generationJobId = source["generationJobId"];
	        this.metadata = source["metadata"];
	        this.createdByType = source["createdByType"];
	        this.legacyMetadata = source["legacyMetadata"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class AttachFileRequest {
	    versionId: string;
	    fileHash: string;
	    role?: string;
	
	    static createFrom(source: any = {}) {
	        return new AttachFileRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.versionId = source["versionId"];
	        this.fileHash = source["fileHash"];
	        this.role = source["role"];
	    }
	}
	export class BackupPreview {
	    manifestVersion: number;
	    appVersion: string;
	    schemaVersion: number;
	    createdAt: string;
	    projects: number;
	    assets: number;
	    files: number;
	    databaseBytes: number;
	    fileBytes: number;
	    stagedFiles: number;
	    stagedDatabase: string;
	
	    static createFrom(source: any = {}) {
	        return new BackupPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.manifestVersion = source["manifestVersion"];
	        this.appVersion = source["appVersion"];
	        this.schemaVersion = source["schemaVersion"];
	        this.createdAt = source["createdAt"];
	        this.projects = source["projects"];
	        this.assets = source["assets"];
	        this.files = source["files"];
	        this.databaseBytes = source["databaseBytes"];
	        this.fileBytes = source["fileBytes"];
	        this.stagedFiles = source["stagedFiles"];
	        this.stagedDatabase = source["stagedDatabase"];
	    }
	}
	export class CanvasChatSessionDTO {
	    id: string;
	    title: string;
	    messagesJson: string;
	    createdAt: string;
	    updatedAt: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new CanvasChatSessionDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.messagesJson = source["messagesJson"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.revision = source["revision"];
	    }
	}
	export class CanvasEdgeDTO {
	    id: string;
	    fromNodeId: string;
	    toNodeId: string;
	    relationType: string;
	    fromPort?: string;
	    toPort?: string;
	    required: boolean;
	    validationStatus: string;
	    metadata?: string;
	    legacyMetadata?: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new CanvasEdgeDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.fromNodeId = source["fromNodeId"];
	        this.toNodeId = source["toNodeId"];
	        this.relationType = source["relationType"];
	        this.fromPort = source["fromPort"];
	        this.toPort = source["toPort"];
	        this.required = source["required"];
	        this.validationStatus = source["validationStatus"];
	        this.metadata = source["metadata"];
	        this.legacyMetadata = source["legacyMetadata"];
	        this.revision = source["revision"];
	    }
	}
	export class CanvasNodeDTO {
	    id: string;
	    nodeType: string;
	    title: string;
	    entityType?: string;
	    entityId?: string;
	    positionX: number;
	    positionY: number;
	    width: number;
	    height: number;
	    zIndex: number;
	    uiState?: string;
	    legacyMetadata?: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new CanvasNodeDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.nodeType = source["nodeType"];
	        this.title = source["title"];
	        this.entityType = source["entityType"];
	        this.entityId = source["entityId"];
	        this.positionX = source["positionX"];
	        this.positionY = source["positionY"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.zIndex = source["zIndex"];
	        this.uiState = source["uiState"];
	        this.legacyMetadata = source["legacyMetadata"];
	        this.revision = source["revision"];
	    }
	}
	export class ViewportDTO {
	    x: number;
	    y: number;
	    k: number;
	
	    static createFrom(source: any = {}) {
	        return new ViewportDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x = source["x"];
	        this.y = source["y"];
	        this.k = source["k"];
	    }
	}
	export class ProjectDTO {
	    id: string;
	    workspaceId: string;
	    projectType: string;
	    name: string;
	    description: string;
	    language: string;
	    status: string;
	    createdAt: string;
	    updatedAt: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new ProjectDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.workspaceId = source["workspaceId"];
	        this.projectType = source["projectType"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.language = source["language"];
	        this.status = source["status"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.revision = source["revision"];
	    }
	}
	export class CanvasSnapshotDTO {
	    project: ProjectDTO;
	    documentId: string;
	    canvasKind: string;
	    viewport: ViewportDTO;
	    background?: string;
	    nodes: CanvasNodeDTO[];
	    edges: CanvasEdgeDTO[];
	    chatSessions: CanvasChatSessionDTO[];
	
	    static createFrom(source: any = {}) {
	        return new CanvasSnapshotDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.project = this.convertValues(source["project"], ProjectDTO);
	        this.documentId = source["documentId"];
	        this.canvasKind = source["canvasKind"];
	        this.viewport = this.convertValues(source["viewport"], ViewportDTO);
	        this.background = source["background"];
	        this.nodes = this.convertValues(source["nodes"], CanvasNodeDTO);
	        this.edges = this.convertValues(source["edges"], CanvasEdgeDTO);
	        this.chatSessions = this.convertValues(source["chatSessions"], CanvasChatSessionDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ChapterDTO {
	    id: string;
	    sourceDocumentVersionId: string;
	    ordinal: number;
	    title: string;
	    startOffset: number;
	    endOffset: number;
	    contentHash?: string;
	    status: string;
	    createdAt: string;
	    updatedAt: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new ChapterDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.sourceDocumentVersionId = source["sourceDocumentVersionId"];
	        this.ordinal = source["ordinal"];
	        this.title = source["title"];
	        this.startOffset = source["startOffset"];
	        this.endOffset = source["endOffset"];
	        this.contentHash = source["contentHash"];
	        this.status = source["status"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.revision = source["revision"];
	    }
	}
	export class ClearStaleMarkRequest {
	    artifactType: string;
	    artifactId: string;
	
	    static createFrom(source: any = {}) {
	        return new ClearStaleMarkRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.artifactType = source["artifactType"];
	        this.artifactId = source["artifactId"];
	    }
	}
	export class CreateAssetRequest {
	    projectId: string;
	    type: string;
	    name: string;
	    description?: string;
	    legacyMetadata?: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateAssetRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.type = source["type"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.legacyMetadata = source["legacyMetadata"];
	    }
	}
	export class CreateChapterRequest {
	    sourceDocumentVersionId: string;
	    ordinal: number;
	    title: string;
	    startOffset: number;
	    endOffset: number;
	    contentHash?: string;
	    status?: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateChapterRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourceDocumentVersionId = source["sourceDocumentVersionId"];
	        this.ordinal = source["ordinal"];
	        this.title = source["title"];
	        this.startOffset = source["startOffset"];
	        this.endOffset = source["endOffset"];
	        this.contentHash = source["contentHash"];
	        this.status = source["status"];
	    }
	}
	export class CreateDirectorPlanVersionRequest {
	    episodeId: string;
	    scriptVersionId: string;
	    basedOnVersionId?: string;
	    visualRhythm?: string;
	    cameraLanguage?: string;
	    colorLighting?: string;
	    staging?: string;
	    continuityRules?: string;
	    audioDirection?: string;
	    shotOverridesJson?: string;
	    sourceAgentRunId?: string;
	    createdByType?: string;
	    createdById?: string;
	    changeReason?: string;
	    legacyMetadata?: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateDirectorPlanVersionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.episodeId = source["episodeId"];
	        this.scriptVersionId = source["scriptVersionId"];
	        this.basedOnVersionId = source["basedOnVersionId"];
	        this.visualRhythm = source["visualRhythm"];
	        this.cameraLanguage = source["cameraLanguage"];
	        this.colorLighting = source["colorLighting"];
	        this.staging = source["staging"];
	        this.continuityRules = source["continuityRules"];
	        this.audioDirection = source["audioDirection"];
	        this.shotOverridesJson = source["shotOverridesJson"];
	        this.sourceAgentRunId = source["sourceAgentRunId"];
	        this.createdByType = source["createdByType"];
	        this.createdById = source["createdById"];
	        this.changeReason = source["changeReason"];
	        this.legacyMetadata = source["legacyMetadata"];
	    }
	}
	export class CreateEdgeRequest {
	    documentId: string;
	    fromNodeId: string;
	    toNodeId: string;
	    relationType?: string;
	    fromPort?: string;
	    toPort?: string;
	    required?: boolean;
	    metadata?: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateEdgeRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.documentId = source["documentId"];
	        this.fromNodeId = source["fromNodeId"];
	        this.toNodeId = source["toNodeId"];
	        this.relationType = source["relationType"];
	        this.fromPort = source["fromPort"];
	        this.toPort = source["toPort"];
	        this.required = source["required"];
	        this.metadata = source["metadata"];
	    }
	}
	export class CreateEpisodeRequest {
	    projectId: string;
	    seasonNumber: number;
	    episodeNumber: number;
	    title?: string;
	    targetDurationSeconds?: number;
	
	    static createFrom(source: any = {}) {
	        return new CreateEpisodeRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.seasonNumber = source["seasonNumber"];
	        this.episodeNumber = source["episodeNumber"];
	        this.title = source["title"];
	        this.targetDurationSeconds = source["targetDurationSeconds"];
	    }
	}
	export class CreatePanelVersionRequest {
	    storyboardItemId: string;
	    basedOnVersionId?: string;
	    visualPrompt?: string;
	    negativePrompt?: string;
	    referencePolicyJson?: string;
	    sourceAgentRunId?: string;
	    createdByType?: string;
	    createdById?: string;
	    changeReason?: string;
	    legacyMetadata?: string;
	
	    static createFrom(source: any = {}) {
	        return new CreatePanelVersionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.storyboardItemId = source["storyboardItemId"];
	        this.basedOnVersionId = source["basedOnVersionId"];
	        this.visualPrompt = source["visualPrompt"];
	        this.negativePrompt = source["negativePrompt"];
	        this.referencePolicyJson = source["referencePolicyJson"];
	        this.sourceAgentRunId = source["sourceAgentRunId"];
	        this.createdByType = source["createdByType"];
	        this.createdById = source["createdById"];
	        this.changeReason = source["changeReason"];
	        this.legacyMetadata = source["legacyMetadata"];
	    }
	}
	export class CreateProjectRequest {
	    name: string;
	    description: string;
	    projectType: string;
	    language: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateProjectRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.projectType = source["projectType"];
	        this.language = source["language"];
	    }
	}
	export class CreateSceneRequest {
	    scriptVersionId: string;
	    ordinal: number;
	    sceneNumber?: string;
	    slugline?: string;
	    interiorExterior?: string;
	    locationEntityId?: string;
	    timeOfDay?: string;
	    summary?: string;
	    dramaticGoal?: string;
	    estimatedDurationSeconds?: number;
	    sourceStoryEventId?: string;
	    isOriginalAdaptation?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CreateSceneRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scriptVersionId = source["scriptVersionId"];
	        this.ordinal = source["ordinal"];
	        this.sceneNumber = source["sceneNumber"];
	        this.slugline = source["slugline"];
	        this.interiorExterior = source["interiorExterior"];
	        this.locationEntityId = source["locationEntityId"];
	        this.timeOfDay = source["timeOfDay"];
	        this.summary = source["summary"];
	        this.dramaticGoal = source["dramaticGoal"];
	        this.estimatedDurationSeconds = source["estimatedDurationSeconds"];
	        this.sourceStoryEventId = source["sourceStoryEventId"];
	        this.isOriginalAdaptation = source["isOriginalAdaptation"];
	    }
	}
	export class CreateScriptVersionRequest {
	    scriptId: string;
	    basedOnVersionId?: string;
	    storySkeletonVersionId: string;
	    adaptationStrategyVersionId: string;
	    estimatedDurationSeconds?: number;
	    summary?: string;
	    sourceAgentRunId?: string;
	    createdByType?: string;
	    createdById?: string;
	    changeReason?: string;
	    legacyMetadata?: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateScriptVersionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scriptId = source["scriptId"];
	        this.basedOnVersionId = source["basedOnVersionId"];
	        this.storySkeletonVersionId = source["storySkeletonVersionId"];
	        this.adaptationStrategyVersionId = source["adaptationStrategyVersionId"];
	        this.estimatedDurationSeconds = source["estimatedDurationSeconds"];
	        this.summary = source["summary"];
	        this.sourceAgentRunId = source["sourceAgentRunId"];
	        this.createdByType = source["createdByType"];
	        this.createdById = source["createdById"];
	        this.changeReason = source["changeReason"];
	        this.legacyMetadata = source["legacyMetadata"];
	    }
	}
	export class CreateShotRequest {
	    sceneId: string;
	    ordinal: number;
	    shotNumber?: string;
	    shotSize?: string;
	    cameraAngle?: string;
	    cameraMovement?: string;
	    estimatedDurationSeconds?: number;
	    visualDescription?: string;
	    actionDescription?: string;
	    audioIntent?: string;
	    continuityNotes?: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateShotRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sceneId = source["sceneId"];
	        this.ordinal = source["ordinal"];
	        this.shotNumber = source["shotNumber"];
	        this.shotSize = source["shotSize"];
	        this.cameraAngle = source["cameraAngle"];
	        this.cameraMovement = source["cameraMovement"];
	        this.estimatedDurationSeconds = source["estimatedDurationSeconds"];
	        this.visualDescription = source["visualDescription"];
	        this.actionDescription = source["actionDescription"];
	        this.audioIntent = source["audioIntent"];
	        this.continuityNotes = source["continuityNotes"];
	    }
	}
	export class CreateSourceDocumentRequest {
	    projectId: string;
	    type: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateSourceDocumentRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.type = source["type"];
	        this.name = source["name"];
	    }
	}
	export class CreateStageRunRequest {
	    workflowRunId: string;
	    stage: string;
	    attempt: number;
	    executionKey?: string;
	    inputJson?: string;
	    actorType?: string;
	    actorId?: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateStageRunRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.workflowRunId = source["workflowRunId"];
	        this.stage = source["stage"];
	        this.attempt = source["attempt"];
	        this.executionKey = source["executionKey"];
	        this.inputJson = source["inputJson"];
	        this.actorType = source["actorType"];
	        this.actorId = source["actorId"];
	    }
	}
	export class CreateStoryEntityRequest {
	    projectId: string;
	    type: string;
	    canonicalName: string;
	    sourceScope?: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateStoryEntityRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.type = source["type"];
	        this.canonicalName = source["canonicalName"];
	        this.sourceScope = source["sourceScope"];
	    }
	}
	export class CreateStoryEventRequest {
	    projectId: string;
	    chapterId?: string;
	    ordinal?: number;
	    name: string;
	    description?: string;
	    eventType?: string;
	    storyTimeText?: string;
	    storyTimeOrder?: number;
	    locationEntityId?: string;
	    causeSummary?: string;
	    resultSummary?: string;
	    importance?: string;
	    confidence?: number;
	    sourceScope?: string;
	    createdByAgentRunId?: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateStoryEventRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.chapterId = source["chapterId"];
	        this.ordinal = source["ordinal"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.eventType = source["eventType"];
	        this.storyTimeText = source["storyTimeText"];
	        this.storyTimeOrder = source["storyTimeOrder"];
	        this.locationEntityId = source["locationEntityId"];
	        this.causeSummary = source["causeSummary"];
	        this.resultSummary = source["resultSummary"];
	        this.importance = source["importance"];
	        this.confidence = source["confidence"];
	        this.sourceScope = source["sourceScope"];
	        this.createdByAgentRunId = source["createdByAgentRunId"];
	    }
	}
	export class CreateStoryRelationRequest {
	    projectId: string;
	    type: string;
	    sourceEntityType: string;
	    sourceEntityId: string;
	    targetEntityType: string;
	    targetEntityId: string;
	    validFromEventId?: string;
	    validToEventId?: string;
	    confidence?: number;
	    sourceScope?: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateStoryRelationRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.type = source["type"];
	        this.sourceEntityType = source["sourceEntityType"];
	        this.sourceEntityId = source["sourceEntityId"];
	        this.targetEntityType = source["targetEntityType"];
	        this.targetEntityId = source["targetEntityId"];
	        this.validFromEventId = source["validFromEventId"];
	        this.validToEventId = source["validToEventId"];
	        this.confidence = source["confidence"];
	        this.sourceScope = source["sourceScope"];
	    }
	}
	export class CreateStoryboardItemRequest {
	    storyboardVersionId: string;
	    shotId: string;
	    ordinal: number;
	    shotSize?: string;
	    cameraAngle?: string;
	    cameraMovement?: string;
	    durationSeconds?: number;
	    visualDescription?: string;
	    actionDescription?: string;
	    dialogueAudioSummary?: string;
	    continuityNotes?: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateStoryboardItemRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.storyboardVersionId = source["storyboardVersionId"];
	        this.shotId = source["shotId"];
	        this.ordinal = source["ordinal"];
	        this.shotSize = source["shotSize"];
	        this.cameraAngle = source["cameraAngle"];
	        this.cameraMovement = source["cameraMovement"];
	        this.durationSeconds = source["durationSeconds"];
	        this.visualDescription = source["visualDescription"];
	        this.actionDescription = source["actionDescription"];
	        this.dialogueAudioSummary = source["dialogueAudioSummary"];
	        this.continuityNotes = source["continuityNotes"];
	    }
	}
	export class CreateStoryboardVersionRequest {
	    storyboardId: string;
	    scriptVersionId: string;
	    directorPlanVersionId: string;
	    basedOnVersionId?: string;
	    sourceAgentRunId?: string;
	    createdByType?: string;
	    createdById?: string;
	    changeReason?: string;
	    legacyMetadata?: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateStoryboardVersionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.storyboardId = source["storyboardId"];
	        this.scriptVersionId = source["scriptVersionId"];
	        this.directorPlanVersionId = source["directorPlanVersionId"];
	        this.basedOnVersionId = source["basedOnVersionId"];
	        this.sourceAgentRunId = source["sourceAgentRunId"];
	        this.createdByType = source["createdByType"];
	        this.createdById = source["createdById"];
	        this.changeReason = source["changeReason"];
	        this.legacyMetadata = source["legacyMetadata"];
	    }
	}
	export class CreateWorkflowRunRequest {
	    projectId: string;
	    episodeId?: string;
	    workflowType: string;
	    configurationJson?: string;
	    actorType?: string;
	    actorId?: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateWorkflowRunRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.episodeId = source["episodeId"];
	        this.workflowType = source["workflowType"];
	        this.configurationJson = source["configurationJson"];
	        this.actorType = source["actorType"];
	        this.actorId = source["actorId"];
	    }
	}
	export class DecideStoryEntityRequest {
	    id: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new DecideStoryEntityRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.revision = source["revision"];
	    }
	}
	export class DecideStoryEventRequest {
	    id: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new DecideStoryEventRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.revision = source["revision"];
	    }
	}
	export class DirectorPlanVersionDTO {
	    id: string;
	    episodeId: string;
	    versionNumber: number;
	    status: string;
	    basedOnVersionId?: string;
	    scriptVersionId: string;
	    visualRhythm?: string;
	    cameraLanguage?: string;
	    colorLighting?: string;
	    staging?: string;
	    continuityRules?: string;
	    audioDirection?: string;
	    shotOverridesJson?: string;
	    sourceAgentRunId?: string;
	    createdByType: string;
	    createdById?: string;
	    changeReason?: string;
	    legacyMetadata?: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new DirectorPlanVersionDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.episodeId = source["episodeId"];
	        this.versionNumber = source["versionNumber"];
	        this.status = source["status"];
	        this.basedOnVersionId = source["basedOnVersionId"];
	        this.scriptVersionId = source["scriptVersionId"];
	        this.visualRhythm = source["visualRhythm"];
	        this.cameraLanguage = source["cameraLanguage"];
	        this.colorLighting = source["colorLighting"];
	        this.staging = source["staging"];
	        this.continuityRules = source["continuityRules"];
	        this.audioDirection = source["audioDirection"];
	        this.shotOverridesJson = source["shotOverridesJson"];
	        this.sourceAgentRunId = source["sourceAgentRunId"];
	        this.createdByType = source["createdByType"];
	        this.createdById = source["createdById"];
	        this.changeReason = source["changeReason"];
	        this.legacyMetadata = source["legacyMetadata"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class EpisodeDTO {
	    id: string;
	    key: string;
	    projectId: string;
	    seasonNumber: number;
	    episodeNumber: number;
	    title: string;
	    status: string;
	    sourceChapterStartId?: string;
	    sourceChapterEndId?: string;
	    targetDurationSeconds: number;
	    currentStorySkeletonVersionId?: string;
	    currentAdaptationStrategyVersionId?: string;
	    currentScriptVersionId?: string;
	    deletedAt?: string;
	    createdAt: string;
	    updatedAt: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new EpisodeDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.key = source["key"];
	        this.projectId = source["projectId"];
	        this.seasonNumber = source["seasonNumber"];
	        this.episodeNumber = source["episodeNumber"];
	        this.title = source["title"];
	        this.status = source["status"];
	        this.sourceChapterStartId = source["sourceChapterStartId"];
	        this.sourceChapterEndId = source["sourceChapterEndId"];
	        this.targetDurationSeconds = source["targetDurationSeconds"];
	        this.currentStorySkeletonVersionId = source["currentStorySkeletonVersionId"];
	        this.currentAdaptationStrategyVersionId = source["currentAdaptationStrategyVersionId"];
	        this.currentScriptVersionId = source["currentScriptVersionId"];
	        this.deletedAt = source["deletedAt"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.revision = source["revision"];
	    }
	}
	export class ImportProjectsRequest {
	    snapshotJson: string;
	    mode?: string;
	    sourceCase?: string;
	    legacyRoot?: string;
	
	    static createFrom(source: any = {}) {
	        return new ImportProjectsRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.snapshotJson = source["snapshotJson"];
	        this.mode = source["mode"];
	        this.sourceCase = source["sourceCase"];
	        this.legacyRoot = source["legacyRoot"];
	    }
	}
	export class JobAttemptDTO {
	    id: string;
	    attemptNumber: number;
	    status: string;
	    errorCode?: string;
	    startedAt: string;
	    finishedAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new JobAttemptDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.attemptNumber = source["attemptNumber"];
	        this.status = source["status"];
	        this.errorCode = source["errorCode"];
	        this.startedAt = source["startedAt"];
	        this.finishedAt = source["finishedAt"];
	    }
	}
	export class JobResultFileDTO {
	    storageKey: string;
	    mime: string;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new JobResultFileDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.storageKey = source["storageKey"];
	        this.mime = source["mime"];
	        this.size = source["size"];
	    }
	}
	export class JobDTO {
	    id: string;
	    projectId: string;
	    entityType: string;
	    entityId: string;
	    jobType: string;
	    status: string;
	    priority: number;
	    remoteJobId?: string;
	    progress: number;
	    errorCode?: string;
	    attemptCount: number;
	    maxAttempts: number;
	    createdAt: string;
	    updatedAt: string;
	    finishedAt?: string;
	    resultFiles?: JobResultFileDTO[];
	    resultRemoteOnly?: boolean;
	    cancelledRemoteUnconfirmed?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new JobDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.projectId = source["projectId"];
	        this.entityType = source["entityType"];
	        this.entityId = source["entityId"];
	        this.jobType = source["jobType"];
	        this.status = source["status"];
	        this.priority = source["priority"];
	        this.remoteJobId = source["remoteJobId"];
	        this.progress = source["progress"];
	        this.errorCode = source["errorCode"];
	        this.attemptCount = source["attemptCount"];
	        this.maxAttempts = source["maxAttempts"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.finishedAt = source["finishedAt"];
	        this.resultFiles = this.convertValues(source["resultFiles"], JobResultFileDTO);
	        this.resultRemoteOnly = source["resultRemoteOnly"];
	        this.cancelledRemoteUnconfirmed = source["cancelledRemoteUnconfirmed"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class JobResultFileContent {
	    mime: string;
	    dataUrl: string;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new JobResultFileContent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mime = source["mime"];
	        this.dataUrl = source["dataUrl"];
	        this.size = source["size"];
	    }
	}
	
	export class ListAssetsRequest {
	    projectId?: string;
	    types?: string[];
	    includeDeleted?: boolean;
	    limit?: number;
	    offset?: number;
	
	    static createFrom(source: any = {}) {
	        return new ListAssetsRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.types = source["types"];
	        this.includeDeleted = source["includeDeleted"];
	        this.limit = source["limit"];
	        this.offset = source["offset"];
	    }
	}
	export class ListJobsRequest {
	    statuses?: string[];
	    activeOnly?: boolean;
	    limit?: number;
	    offset?: number;
	
	    static createFrom(source: any = {}) {
	        return new ListJobsRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.statuses = source["statuses"];
	        this.activeOnly = source["activeOnly"];
	        this.limit = source["limit"];
	        this.offset = source["offset"];
	    }
	}
	export class ListProjectsRequest {
	    statuses?: string[];
	    limit?: number;
	    offset?: number;
	    includeTrashed?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ListProjectsRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.statuses = source["statuses"];
	        this.limit = source["limit"];
	        this.offset = source["offset"];
	        this.includeTrashed = source["includeTrashed"];
	    }
	}
	export class MarkStaleRequest {
	    artifactType: string;
	    artifactId: string;
	    projectId: string;
	    severity: string;
	    reason?: string;
	    upstreamType?: string;
	    upstreamId?: string;
	
	    static createFrom(source: any = {}) {
	        return new MarkStaleRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.artifactType = source["artifactType"];
	        this.artifactId = source["artifactId"];
	        this.projectId = source["projectId"];
	        this.severity = source["severity"];
	        this.reason = source["reason"];
	        this.upstreamType = source["upstreamType"];
	        this.upstreamId = source["upstreamId"];
	    }
	}
	export class NodePositionDTO {
	    id: string;
	    x: number;
	    y: number;
	    zIndex?: number;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new NodePositionDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.x = source["x"];
	        this.y = source["y"];
	        this.zIndex = source["zIndex"];
	        this.revision = source["revision"];
	    }
	}
	export class MoveNodesRequest {
	    documentId: string;
	    positions: NodePositionDTO[];
	
	    static createFrom(source: any = {}) {
	        return new MoveNodesRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.documentId = source["documentId"];
	        this.positions = this.convertValues(source["positions"], NodePositionDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	export class ProviderConfigRequest {
	    id: string;
	    kind: string;
	    displayName: string;
	    baseUrl: string;
	    localApprove: boolean;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProviderConfigRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.displayName = source["displayName"];
	        this.baseUrl = source["baseUrl"];
	        this.localApprove = source["localApprove"];
	        this.enabled = source["enabled"];
	    }
	}
	export class QueueSummaryDTO {
	    queued: number;
	    running: number;
	    waiting: number;
	    failed: number;
	    succeeded: number;
	    cancelled: number;
	    activeTotal: number;
	    paused: boolean;
	
	    static createFrom(source: any = {}) {
	        return new QueueSummaryDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.queued = source["queued"];
	        this.running = source["running"];
	        this.waiting = source["waiting"];
	        this.failed = source["failed"];
	        this.succeeded = source["succeeded"];
	        this.cancelled = source["cancelled"];
	        this.activeTotal = source["activeTotal"];
	        this.paused = source["paused"];
	    }
	}
	export class ReviewIssueInputRequest {
	    rule?: string;
	    severity: string;
	    entityType?: string;
	    entityId?: string;
	    location?: string;
	    field?: string;
	    problem?: string;
	    suggestion?: string;
	    evidenceJson?: string;
	    autoFixable?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ReviewIssueInputRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rule = source["rule"];
	        this.severity = source["severity"];
	        this.entityType = source["entityType"];
	        this.entityId = source["entityId"];
	        this.location = source["location"];
	        this.field = source["field"];
	        this.problem = source["problem"];
	        this.suggestion = source["suggestion"];
	        this.evidenceJson = source["evidenceJson"];
	        this.autoFixable = source["autoFixable"];
	    }
	}
	export class RecordReviewRequest {
	    stageRunId: string;
	    supervisorKey?: string;
	    rulesetVersion?: string;
	    score?: number;
	    grade?: string;
	    passed: boolean;
	    severity: string;
	    recommendedAction?: string;
	    summary?: string;
	    issues?: ReviewIssueInputRequest[];
	
	    static createFrom(source: any = {}) {
	        return new RecordReviewRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.stageRunId = source["stageRunId"];
	        this.supervisorKey = source["supervisorKey"];
	        this.rulesetVersion = source["rulesetVersion"];
	        this.score = source["score"];
	        this.grade = source["grade"];
	        this.passed = source["passed"];
	        this.severity = source["severity"];
	        this.recommendedAction = source["recommendedAction"];
	        this.summary = source["summary"];
	        this.issues = this.convertValues(source["issues"], ReviewIssueInputRequest);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class RenameProjectRequest {
	    id: string;
	    name: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new RenameProjectRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.revision = source["revision"];
	    }
	}
	export class ReviewIssueDTO {
	    id: string;
	    reviewReportId: string;
	    rule?: string;
	    severity: string;
	    entityType?: string;
	    entityId?: string;
	    location?: string;
	    field?: string;
	    problem?: string;
	    suggestion?: string;
	    evidenceJson?: string;
	    autoFixable: boolean;
	    status: string;
	    resolvedBy?: string;
	    resolvedAt?: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new ReviewIssueDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.reviewReportId = source["reviewReportId"];
	        this.rule = source["rule"];
	        this.severity = source["severity"];
	        this.entityType = source["entityType"];
	        this.entityId = source["entityId"];
	        this.location = source["location"];
	        this.field = source["field"];
	        this.problem = source["problem"];
	        this.suggestion = source["suggestion"];
	        this.evidenceJson = source["evidenceJson"];
	        this.autoFixable = source["autoFixable"];
	        this.status = source["status"];
	        this.resolvedBy = source["resolvedBy"];
	        this.resolvedAt = source["resolvedAt"];
	        this.createdAt = source["createdAt"];
	    }
	}
	
	export class ReviewReportDTO {
	    id: string;
	    stageRunId: string;
	    supervisorKey?: string;
	    rulesetVersion?: string;
	    score?: number;
	    grade?: string;
	    passed: boolean;
	    severity: string;
	    recommendedAction?: string;
	    summary?: string;
	    createdAt: string;
	    issues: ReviewIssueDTO[];
	
	    static createFrom(source: any = {}) {
	        return new ReviewReportDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.stageRunId = source["stageRunId"];
	        this.supervisorKey = source["supervisorKey"];
	        this.rulesetVersion = source["rulesetVersion"];
	        this.score = source["score"];
	        this.grade = source["grade"];
	        this.passed = source["passed"];
	        this.severity = source["severity"];
	        this.recommendedAction = source["recommendedAction"];
	        this.summary = source["summary"];
	        this.createdAt = source["createdAt"];
	        this.issues = this.convertValues(source["issues"], ReviewIssueDTO);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ReviseChapterRequest {
	    chapterId: string;
	    title: string;
	    startOffset: number;
	    endOffset: number;
	    contentHash?: string;
	    status?: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new ReviseChapterRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.chapterId = source["chapterId"];
	        this.title = source["title"];
	        this.startOffset = source["startOffset"];
	        this.endOffset = source["endOffset"];
	        this.contentHash = source["contentHash"];
	        this.status = source["status"];
	        this.revision = source["revision"];
	    }
	}
	export class SaveChatSessionRequest {
	    id?: string;
	    canvasDocumentId: string;
	    title: string;
	    messagesJson: string;
	    revision?: number;
	
	    static createFrom(source: any = {}) {
	        return new SaveChatSessionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.canvasDocumentId = source["canvasDocumentId"];
	        this.title = source["title"];
	        this.messagesJson = source["messagesJson"];
	        this.revision = source["revision"];
	    }
	}
	export class SceneDTO {
	    id: string;
	    scriptVersionId: string;
	    ordinal: number;
	    sceneNumber?: string;
	    slugline?: string;
	    interiorExterior: string;
	    locationEntityId?: string;
	    timeOfDay?: string;
	    summary?: string;
	    dramaticGoal?: string;
	    estimatedDurationSeconds: number;
	    sourceStoryEventId?: string;
	    isOriginalAdaptation: boolean;
	    createdAt: string;
	    updatedAt: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new SceneDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.scriptVersionId = source["scriptVersionId"];
	        this.ordinal = source["ordinal"];
	        this.sceneNumber = source["sceneNumber"];
	        this.slugline = source["slugline"];
	        this.interiorExterior = source["interiorExterior"];
	        this.locationEntityId = source["locationEntityId"];
	        this.timeOfDay = source["timeOfDay"];
	        this.summary = source["summary"];
	        this.dramaticGoal = source["dramaticGoal"];
	        this.estimatedDurationSeconds = source["estimatedDurationSeconds"];
	        this.sourceStoryEventId = source["sourceStoryEventId"];
	        this.isOriginalAdaptation = source["isOriginalAdaptation"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.revision = source["revision"];
	    }
	}
	export class ScriptDTO {
	    id: string;
	    episodeId: string;
	    currentVersionId?: string;
	    createdAt: string;
	    updatedAt: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new ScriptDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.episodeId = source["episodeId"];
	        this.currentVersionId = source["currentVersionId"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.revision = source["revision"];
	    }
	}
	export class ScriptVersionDTO {
	    id: string;
	    scriptId: string;
	    versionNumber: number;
	    status: string;
	    basedOnVersionId?: string;
	    storySkeletonVersionId: string;
	    adaptationStrategyVersionId: string;
	    estimatedDurationSeconds: number;
	    summary?: string;
	    sourceAgentRunId?: string;
	    createdByType: string;
	    createdById?: string;
	    changeReason?: string;
	    legacyMetadata?: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new ScriptVersionDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.scriptId = source["scriptId"];
	        this.versionNumber = source["versionNumber"];
	        this.status = source["status"];
	        this.basedOnVersionId = source["basedOnVersionId"];
	        this.storySkeletonVersionId = source["storySkeletonVersionId"];
	        this.adaptationStrategyVersionId = source["adaptationStrategyVersionId"];
	        this.estimatedDurationSeconds = source["estimatedDurationSeconds"];
	        this.summary = source["summary"];
	        this.sourceAgentRunId = source["sourceAgentRunId"];
	        this.createdByType = source["createdByType"];
	        this.createdById = source["createdById"];
	        this.changeReason = source["changeReason"];
	        this.legacyMetadata = source["legacyMetadata"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class SetProjectStatusRequest {
	    id: string;
	    status: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new SetProjectStatusRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.status = source["status"];
	        this.revision = source["revision"];
	    }
	}
	export class SetSecretRequest {
	    providerId: string;
	    value: string;
	
	    static createFrom(source: any = {}) {
	        return new SetSecretRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.providerId = source["providerId"];
	        this.value = source["value"];
	    }
	}
	export class ShotDTO {
	    id: string;
	    sceneId: string;
	    ordinal: number;
	    shotNumber?: string;
	    shotSize?: string;
	    cameraAngle?: string;
	    cameraMovement?: string;
	    estimatedDurationSeconds: number;
	    visualDescription?: string;
	    actionDescription?: string;
	    audioIntent?: string;
	    continuityNotes?: string;
	    status: string;
	    createdAt: string;
	    updatedAt: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new ShotDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.sceneId = source["sceneId"];
	        this.ordinal = source["ordinal"];
	        this.shotNumber = source["shotNumber"];
	        this.shotSize = source["shotSize"];
	        this.cameraAngle = source["cameraAngle"];
	        this.cameraMovement = source["cameraMovement"];
	        this.estimatedDurationSeconds = source["estimatedDurationSeconds"];
	        this.visualDescription = source["visualDescription"];
	        this.actionDescription = source["actionDescription"];
	        this.audioIntent = source["audioIntent"];
	        this.continuityNotes = source["continuityNotes"];
	        this.status = source["status"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.revision = source["revision"];
	    }
	}
	export class SourceDocumentDTO {
	    id: string;
	    projectId: string;
	    type: string;
	    name: string;
	    currentVersionId?: string;
	    status: string;
	    deletedAt?: string;
	    createdAt: string;
	    updatedAt: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new SourceDocumentDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.projectId = source["projectId"];
	        this.type = source["type"];
	        this.name = source["name"];
	        this.currentVersionId = source["currentVersionId"];
	        this.status = source["status"];
	        this.deletedAt = source["deletedAt"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.revision = source["revision"];
	    }
	}
	export class SourceDocumentVersionDTO {
	    id: string;
	    sourceDocumentId: string;
	    versionNumber: number;
	    physicalFileId?: string;
	    normalizedTextFileId: string;
	    contentHash?: string;
	    mimeType?: string;
	    encoding?: string;
	    charCount: number;
	    importMetadataJson?: string;
	    createdByType: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new SourceDocumentVersionDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.sourceDocumentId = source["sourceDocumentId"];
	        this.versionNumber = source["versionNumber"];
	        this.physicalFileId = source["physicalFileId"];
	        this.normalizedTextFileId = source["normalizedTextFileId"];
	        this.contentHash = source["contentHash"];
	        this.mimeType = source["mimeType"];
	        this.encoding = source["encoding"];
	        this.charCount = source["charCount"];
	        this.importMetadataJson = source["importMetadataJson"];
	        this.createdByType = source["createdByType"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class StageRunDTO {
	    id: string;
	    workflowRunId: string;
	    stage: string;
	    attempt: number;
	    executionAgentKey?: string;
	    status: string;
	    inputJson?: string;
	    validatedOutputJson?: string;
	    rawOutputFileId?: string;
	    errorCode?: string;
	    errorMessage?: string;
	    createdAt: string;
	    startedAt?: string;
	    finishedAt?: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new StageRunDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.workflowRunId = source["workflowRunId"];
	        this.stage = source["stage"];
	        this.attempt = source["attempt"];
	        this.executionAgentKey = source["executionAgentKey"];
	        this.status = source["status"];
	        this.inputJson = source["inputJson"];
	        this.validatedOutputJson = source["validatedOutputJson"];
	        this.rawOutputFileId = source["rawOutputFileId"];
	        this.errorCode = source["errorCode"];
	        this.errorMessage = source["errorMessage"];
	        this.createdAt = source["createdAt"];
	        this.startedAt = source["startedAt"];
	        this.finishedAt = source["finishedAt"];
	        this.revision = source["revision"];
	    }
	}
	export class StaleMarkDTO {
	    artifactType: string;
	    artifactId: string;
	    projectId: string;
	    severity: string;
	    reason?: string;
	    upstreamType?: string;
	    upstreamId?: string;
	    waived: boolean;
	    waivedByDecisionId?: string;
	    waivedReason?: string;
	    clearedAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new StaleMarkDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.artifactType = source["artifactType"];
	        this.artifactId = source["artifactId"];
	        this.projectId = source["projectId"];
	        this.severity = source["severity"];
	        this.reason = source["reason"];
	        this.upstreamType = source["upstreamType"];
	        this.upstreamId = source["upstreamId"];
	        this.waived = source["waived"];
	        this.waivedByDecisionId = source["waivedByDecisionId"];
	        this.waivedReason = source["waivedReason"];
	        this.clearedAt = source["clearedAt"];
	    }
	}
	export class StoryEntityDTO {
	    id: string;
	    projectId: string;
	    type: string;
	    canonicalName: string;
	    status: string;
	    sourceScope: string;
	    currentProfileVersionId?: string;
	    deletedAt?: string;
	    createdAt: string;
	    updatedAt: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new StoryEntityDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.projectId = source["projectId"];
	        this.type = source["type"];
	        this.canonicalName = source["canonicalName"];
	        this.status = source["status"];
	        this.sourceScope = source["sourceScope"];
	        this.currentProfileVersionId = source["currentProfileVersionId"];
	        this.deletedAt = source["deletedAt"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.revision = source["revision"];
	    }
	}
	export class StoryEventDTO {
	    id: string;
	    projectId: string;
	    chapterId?: string;
	    ordinal: number;
	    name: string;
	    description?: string;
	    eventType?: string;
	    storyTimeText?: string;
	    storyTimeOrder?: number;
	    locationEntityId?: string;
	    causeSummary?: string;
	    resultSummary?: string;
	    importance?: string;
	    confidence: number;
	    status: string;
	    sourceScope: string;
	    createdByAgentRunId?: string;
	    deletedAt?: string;
	    createdAt: string;
	    updatedAt: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new StoryEventDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.projectId = source["projectId"];
	        this.chapterId = source["chapterId"];
	        this.ordinal = source["ordinal"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.eventType = source["eventType"];
	        this.storyTimeText = source["storyTimeText"];
	        this.storyTimeOrder = source["storyTimeOrder"];
	        this.locationEntityId = source["locationEntityId"];
	        this.causeSummary = source["causeSummary"];
	        this.resultSummary = source["resultSummary"];
	        this.importance = source["importance"];
	        this.confidence = source["confidence"];
	        this.status = source["status"];
	        this.sourceScope = source["sourceScope"];
	        this.createdByAgentRunId = source["createdByAgentRunId"];
	        this.deletedAt = source["deletedAt"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.revision = source["revision"];
	    }
	}
	export class StoryRelationDTO {
	    id: string;
	    projectId: string;
	    type: string;
	    sourceEntityType: string;
	    sourceEntityId: string;
	    targetEntityType: string;
	    targetEntityId: string;
	    validFromEventId?: string;
	    validToEventId?: string;
	    confidence: number;
	    status: string;
	    sourceScope: string;
	    createdAt: string;
	    updatedAt: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new StoryRelationDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.projectId = source["projectId"];
	        this.type = source["type"];
	        this.sourceEntityType = source["sourceEntityType"];
	        this.sourceEntityId = source["sourceEntityId"];
	        this.targetEntityType = source["targetEntityType"];
	        this.targetEntityId = source["targetEntityId"];
	        this.validFromEventId = source["validFromEventId"];
	        this.validToEventId = source["validToEventId"];
	        this.confidence = source["confidence"];
	        this.status = source["status"];
	        this.sourceScope = source["sourceScope"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.revision = source["revision"];
	    }
	}
	export class StoryboardDTO {
	    id: string;
	    episodeId: string;
	    currentVersionId?: string;
	    createdAt: string;
	    updatedAt: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new StoryboardDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.episodeId = source["episodeId"];
	        this.currentVersionId = source["currentVersionId"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.revision = source["revision"];
	    }
	}
	export class StoryboardItemDTO {
	    id: string;
	    storyboardVersionId: string;
	    shotId: string;
	    ordinal: number;
	    shotSize?: string;
	    cameraAngle?: string;
	    cameraMovement?: string;
	    durationSeconds: number;
	    visualDescription?: string;
	    actionDescription?: string;
	    dialogueAudioSummary?: string;
	    continuityNotes?: string;
	    status: string;
	    createdAt: string;
	    updatedAt: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new StoryboardItemDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.storyboardVersionId = source["storyboardVersionId"];
	        this.shotId = source["shotId"];
	        this.ordinal = source["ordinal"];
	        this.shotSize = source["shotSize"];
	        this.cameraAngle = source["cameraAngle"];
	        this.cameraMovement = source["cameraMovement"];
	        this.durationSeconds = source["durationSeconds"];
	        this.visualDescription = source["visualDescription"];
	        this.actionDescription = source["actionDescription"];
	        this.dialogueAudioSummary = source["dialogueAudioSummary"];
	        this.continuityNotes = source["continuityNotes"];
	        this.status = source["status"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.revision = source["revision"];
	    }
	}
	export class StoryboardPanelVersionDTO {
	    id: string;
	    storyboardItemId: string;
	    versionNumber: number;
	    status: string;
	    basedOnVersionId?: string;
	    visualPrompt?: string;
	    negativePrompt?: string;
	    referencePolicyJson?: string;
	    approvedImageAssetVersionId?: string;
	    sourceAgentRunId?: string;
	    createdByType: string;
	    createdById?: string;
	    changeReason?: string;
	    legacyMetadata?: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new StoryboardPanelVersionDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.storyboardItemId = source["storyboardItemId"];
	        this.versionNumber = source["versionNumber"];
	        this.status = source["status"];
	        this.basedOnVersionId = source["basedOnVersionId"];
	        this.visualPrompt = source["visualPrompt"];
	        this.negativePrompt = source["negativePrompt"];
	        this.referencePolicyJson = source["referencePolicyJson"];
	        this.approvedImageAssetVersionId = source["approvedImageAssetVersionId"];
	        this.sourceAgentRunId = source["sourceAgentRunId"];
	        this.createdByType = source["createdByType"];
	        this.createdById = source["createdById"];
	        this.changeReason = source["changeReason"];
	        this.legacyMetadata = source["legacyMetadata"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class StoryboardVersionDTO {
	    id: string;
	    storyboardId: string;
	    versionNumber: number;
	    status: string;
	    scriptVersionId: string;
	    directorPlanVersionId: string;
	    basedOnVersionId?: string;
	    sourceAgentRunId?: string;
	    createdByType: string;
	    createdById?: string;
	    changeReason?: string;
	    legacyMetadata?: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new StoryboardVersionDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.storyboardId = source["storyboardId"];
	        this.versionNumber = source["versionNumber"];
	        this.status = source["status"];
	        this.scriptVersionId = source["scriptVersionId"];
	        this.directorPlanVersionId = source["directorPlanVersionId"];
	        this.basedOnVersionId = source["basedOnVersionId"];
	        this.sourceAgentRunId = source["sourceAgentRunId"];
	        this.createdByType = source["createdByType"];
	        this.createdById = source["createdById"];
	        this.changeReason = source["changeReason"];
	        this.legacyMetadata = source["legacyMetadata"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class SubmitGateDecisionRequest {
	    workflowRunId: string;
	    stageRunId?: string;
	    decision: string;
	    issueIdsJson?: string;
	    instruction?: string;
	    reason?: string;
	    lockedEntityRefsJson?: string;
	    createdByType?: string;
	    createdById?: string;
	
	    static createFrom(source: any = {}) {
	        return new SubmitGateDecisionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.workflowRunId = source["workflowRunId"];
	        this.stageRunId = source["stageRunId"];
	        this.decision = source["decision"];
	        this.issueIdsJson = source["issueIdsJson"];
	        this.instruction = source["instruction"];
	        this.reason = source["reason"];
	        this.lockedEntityRefsJson = source["lockedEntityRefsJson"];
	        this.createdByType = source["createdByType"];
	        this.createdById = source["createdById"];
	    }
	}
	export class SubmitImageJobRequest {
	    projectId: string;
	    entityType: string;
	    entityId: string;
	    providerId: string;
	    model: string;
	    prompt: string;
	    count?: number;
	    size?: string;
	    quality?: string;
	    references?: string[];
	    referenceMimes?: string[];
	    mask?: string;
	    maskMime?: string;
	    priority?: number;
	
	    static createFrom(source: any = {}) {
	        return new SubmitImageJobRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.entityType = source["entityType"];
	        this.entityId = source["entityId"];
	        this.providerId = source["providerId"];
	        this.model = source["model"];
	        this.prompt = source["prompt"];
	        this.count = source["count"];
	        this.size = source["size"];
	        this.quality = source["quality"];
	        this.references = source["references"];
	        this.referenceMimes = source["referenceMimes"];
	        this.mask = source["mask"];
	        this.maskMime = source["maskMime"];
	        this.priority = source["priority"];
	    }
	}
	export class TextRequestDTO {
	    providerId: string;
	    model: string;
	    messages: providers.TextMessage[];
	
	    static createFrom(source: any = {}) {
	        return new TextRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.providerId = source["providerId"];
	        this.model = source["model"];
	        this.messages = this.convertValues(source["messages"], providers.TextMessage);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TransitionStageRunRequest {
	    stageRunId: string;
	    status: string;
	    revision: number;
	    actorType?: string;
	    actorId?: string;
	
	    static createFrom(source: any = {}) {
	        return new TransitionStageRunRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.stageRunId = source["stageRunId"];
	        this.status = source["status"];
	        this.revision = source["revision"];
	        this.actorType = source["actorType"];
	        this.actorId = source["actorId"];
	    }
	}
	export class TransitionWorkflowRunRequest {
	    runId: string;
	    status: string;
	    revision: number;
	    actorType?: string;
	    actorId?: string;
	
	    static createFrom(source: any = {}) {
	        return new TransitionWorkflowRunRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.runId = source["runId"];
	        this.status = source["status"];
	        this.revision = source["revision"];
	        this.actorType = source["actorType"];
	        this.actorId = source["actorId"];
	    }
	}
	export class UpdateEpisodeStatusRequest {
	    episodeId: string;
	    status: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new UpdateEpisodeStatusRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.episodeId = source["episodeId"];
	        this.status = source["status"];
	        this.revision = source["revision"];
	    }
	}
	export class UpdateViewportRequest {
	    documentId: string;
	    viewport: ViewportDTO;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new UpdateViewportRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.documentId = source["documentId"];
	        this.viewport = this.convertValues(source["viewport"], ViewportDTO);
	        this.revision = source["revision"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class UpsertNodeRequest {
	    id?: string;
	    documentId: string;
	    nodeType: string;
	    title: string;
	    entityType?: string;
	    entityId?: string;
	    positionX: number;
	    positionY: number;
	    width: number;
	    height: number;
	    zIndex: number;
	    uiState?: string;
	    legacyMetadata?: string;
	    revision?: number;
	
	    static createFrom(source: any = {}) {
	        return new UpsertNodeRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.documentId = source["documentId"];
	        this.nodeType = source["nodeType"];
	        this.title = source["title"];
	        this.entityType = source["entityType"];
	        this.entityId = source["entityId"];
	        this.positionX = source["positionX"];
	        this.positionY = source["positionY"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.zIndex = source["zIndex"];
	        this.uiState = source["uiState"];
	        this.legacyMetadata = source["legacyMetadata"];
	        this.revision = source["revision"];
	    }
	}
	export class UserGateDecisionDTO {
	    id: string;
	    workflowRunId: string;
	    stageRunId?: string;
	    decision: string;
	    issueIdsJson?: string;
	    instruction?: string;
	    reason?: string;
	    lockedEntityRefsJson?: string;
	    createdByType: string;
	    createdById?: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new UserGateDecisionDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.workflowRunId = source["workflowRunId"];
	        this.stageRunId = source["stageRunId"];
	        this.decision = source["decision"];
	        this.issueIdsJson = source["issueIdsJson"];
	        this.instruction = source["instruction"];
	        this.reason = source["reason"];
	        this.lockedEntityRefsJson = source["lockedEntityRefsJson"];
	        this.createdByType = source["createdByType"];
	        this.createdById = source["createdById"];
	        this.createdAt = source["createdAt"];
	    }
	}
	
	export class WaiveStaleMarkRequest {
	    artifactType: string;
	    artifactId: string;
	    decisionId: string;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new WaiveStaleMarkRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.artifactType = source["artifactType"];
	        this.artifactId = source["artifactId"];
	        this.decisionId = source["decisionId"];
	        this.reason = source["reason"];
	    }
	}
	export class WorkflowEventDTO {
	    id: string;
	    workflowRunId: string;
	    stageRunId?: string;
	    eventType: string;
	    fromStatus?: string;
	    toStatus?: string;
	    payloadJson?: string;
	    actorType: string;
	    actorId?: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkflowEventDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.workflowRunId = source["workflowRunId"];
	        this.stageRunId = source["stageRunId"];
	        this.eventType = source["eventType"];
	        this.fromStatus = source["fromStatus"];
	        this.toStatus = source["toStatus"];
	        this.payloadJson = source["payloadJson"];
	        this.actorType = source["actorType"];
	        this.actorId = source["actorId"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class WorkflowRunDTO {
	    id: string;
	    projectId: string;
	    episodeId?: string;
	    workflowType: string;
	    currentStage?: string;
	    status: string;
	    activeStageRunId?: string;
	    configurationJson?: string;
	    retryCount: number;
	    createdAt: string;
	    updatedAt: string;
	    completedAt?: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new WorkflowRunDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.projectId = source["projectId"];
	        this.episodeId = source["episodeId"];
	        this.workflowType = source["workflowType"];
	        this.currentStage = source["currentStage"];
	        this.status = source["status"];
	        this.activeStageRunId = source["activeStageRunId"];
	        this.configurationJson = source["configurationJson"];
	        this.retryCount = source["retryCount"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.completedAt = source["completedAt"];
	        this.revision = source["revision"];
	    }
	}

}

export namespace health {
	
	export class Snapshot {
	    version: string;
	    database: string;
	    dataDirectory: string;
	    safeMode: boolean;
	    diagnostic?: string;
	
	    static createFrom(source: any = {}) {
	        return new Snapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.database = source["database"];
	        this.dataDirectory = source["dataDirectory"];
	        this.safeMode = source["safeMode"];
	        this.diagnostic = source["diagnostic"];
	    }
	}

}

export namespace provider {
	
	export class HealthState {
	    ProviderID: string;
	    Healthy: boolean;
	    // Go type: time
	    CheckedAt: any;
	    Detail: string;
	
	    static createFrom(source: any = {}) {
	        return new HealthState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ProviderID = source["ProviderID"];
	        this.Healthy = source["Healthy"];
	        this.CheckedAt = this.convertValues(source["CheckedAt"], null);
	        this.Detail = source["Detail"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace providers {
	
	export class ConfigDTO {
	    id: string;
	    kind: string;
	    displayName: string;
	    baseUrl: string;
	    secretRef: string;
	    localApproved: boolean;
	    enabled: boolean;
	    revision: number;
	    updatedAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new ConfigDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.displayName = source["displayName"];
	        this.baseUrl = source["baseUrl"];
	        this.secretRef = source["secretRef"];
	        this.localApproved = source["localApproved"];
	        this.enabled = source["enabled"];
	        this.revision = source["revision"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class TextMessage {
	    role: string;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new TextMessage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.role = source["role"];
	        this.content = source["content"];
	    }
	}
	export class TextResult {
	    content: string;
	    finishReason?: string;
	    model?: string;
	
	    static createFrom(source: any = {}) {
	        return new TextResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.content = source["content"];
	        this.finishReason = source["finishReason"];
	        this.model = source["model"];
	    }
	}

}

export namespace secrets {
	
	export class Status {
	    providerId: string;
	    configured: boolean;
	    displayHint: string;
	    updatedAt?: string;
	    available: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.providerId = source["providerId"];
	        this.configured = source["configured"];
	        this.displayHint = source["displayHint"];
	        this.updatedAt = source["updatedAt"];
	        this.available = source["available"];
	    }
	}

}

