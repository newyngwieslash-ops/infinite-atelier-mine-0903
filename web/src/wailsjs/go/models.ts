export namespace agentruntime {
	
	export class MessageView {
	    id: string;
	    role: string;
	    content: string;
	    createdAt: string;
	    scopeKey: string;
	
	    static createFrom(source: any = {}) {
	        return new MessageView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.role = source["role"];
	        this.content = source["content"];
	        this.createdAt = source["createdAt"];
	        this.scopeKey = source["scopeKey"];
	    }
	}
	export class RunSummary {
	    id: string;
	    agentKey: string;
	    layer: string;
	    status: string;
	    skillVersionId: string;
	    modelConfigId: string;
	    responseModel: string;
	    workflowRunId: string;
	    stageRunId: string;
	    inputSummary: string;
	    errorCode: string;
	    startedAt: string;
	    finishedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new RunSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.agentKey = source["agentKey"];
	        this.layer = source["layer"];
	        this.status = source["status"];
	        this.skillVersionId = source["skillVersionId"];
	        this.modelConfigId = source["modelConfigId"];
	        this.responseModel = source["responseModel"];
	        this.workflowRunId = source["workflowRunId"];
	        this.stageRunId = source["stageRunId"];
	        this.inputSummary = source["inputSummary"];
	        this.errorCode = source["errorCode"];
	        this.startedAt = source["startedAt"];
	        this.finishedAt = source["finishedAt"];
	    }
	}
	export class ToolCallView {
	    id: string;
	    sequence: number;
	    toolKey: string;
	    status: string;
	    errorCode: string;
	    inputJson: string;
	    outputJson: string;
	    startedAt: string;
	    finishedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new ToolCallView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.sequence = source["sequence"];
	        this.toolKey = source["toolKey"];
	        this.status = source["status"];
	        this.errorCode = source["errorCode"];
	        this.inputJson = source["inputJson"];
	        this.outputJson = source["outputJson"];
	        this.startedAt = source["startedAt"];
	        this.finishedAt = source["finishedAt"];
	    }
	}
	export class RunTrace {
	    run: RunSummary;
	    messages: MessageView[];
	    toolCalls: ToolCallView[];
	    output: string;
	
	    static createFrom(source: any = {}) {
	        return new RunTrace(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.run = this.convertValues(source["run"], RunSummary);
	        this.messages = this.convertValues(source["messages"], MessageView);
	        this.toolCalls = this.convertValues(source["toolCalls"], ToolCallView);
	        this.output = source["output"];
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

export namespace desktop {
	
	export class StrategyEventLinkDTO {
	    storyEventId: string;
	    treatment: string;
	    ordinal: number;
	
	    static createFrom(source: any = {}) {
	        return new StrategyEventLinkDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.storyEventId = source["storyEventId"];
	        this.treatment = source["treatment"];
	        this.ordinal = source["ordinal"];
	    }
	}
	export class AdaptationStrategyVersionDTO {
	    versionId: string;
	    episodeId: string;
	    versionNumber: number;
	    status: string;
	    basedOnVersionId?: string;
	    strategySummary: string;
	    adaptationMode: string;
	    mergedEventGroupsJson?: string;
	    originalAdditions?: string;
	    rationale?: string;
	    risks?: string;
	    eventLinks: StrategyEventLinkDTO[];
	    sourceAgentRunId?: string;
	    createdByType: string;
	    createdById?: string;
	    changeReason?: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new AdaptationStrategyVersionDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.versionId = source["versionId"];
	        this.episodeId = source["episodeId"];
	        this.versionNumber = source["versionNumber"];
	        this.status = source["status"];
	        this.basedOnVersionId = source["basedOnVersionId"];
	        this.strategySummary = source["strategySummary"];
	        this.adaptationMode = source["adaptationMode"];
	        this.mergedEventGroupsJson = source["mergedEventGroupsJson"];
	        this.originalAdditions = source["originalAdditions"];
	        this.rationale = source["rationale"];
	        this.risks = source["risks"];
	        this.eventLinks = this.convertValues(source["eventLinks"], StrategyEventLinkDTO);
	        this.sourceAgentRunId = source["sourceAgentRunId"];
	        this.createdByType = source["createdByType"];
	        this.createdById = source["createdById"];
	        this.changeReason = source["changeReason"];
	        this.createdAt = source["createdAt"];
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
	export class AddRelationRequest {
	    sourceAssetVersionId: string;
	    targetAssetVersionId: string;
	    type: string;
	
	    static createFrom(source: any = {}) {
	        return new AddRelationRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourceAssetVersionId = source["sourceAssetVersionId"];
	        this.targetAssetVersionId = source["targetAssetVersionId"];
	        this.type = source["type"];
	    }
	}
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
	export class AddUsageRequest {
	    assetVersionId: string;
	    consumerType: string;
	    consumerId: string;
	    usageRole?: string;
	    required?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AddUsageRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.assetVersionId = source["assetVersionId"];
	        this.consumerType = source["consumerType"];
	        this.consumerId = source["consumerId"];
	        this.usageRole = source["usageRole"];
	        this.required = source["required"];
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
	export class AgentSpecDTO {
	    key: string;
	    layer: string;
	    skill: string;
	    allowedTools: string[];
	    maxToolCalls: number;
	    maxDurationSeconds: number;
	    policyLayer: string;
	
	    static createFrom(source: any = {}) {
	        return new AgentSpecDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.layer = source["layer"];
	        this.skill = source["skill"];
	        this.allowedTools = source["allowedTools"];
	        this.maxToolCalls = source["maxToolCalls"];
	        this.maxDurationSeconds = source["maxDurationSeconds"];
	        this.policyLayer = source["policyLayer"];
	    }
	}
	export class AgentInventoryDTO {
	    agents: AgentSpecDTO[];
	
	    static createFrom(source: any = {}) {
	        return new AgentInventoryDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.agents = this.convertValues(source["agents"], AgentSpecDTO);
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
	
	export class AppendImportUploadChunkRequest {
	    uploadId: string;
	    chunk: string;
	
	    static createFrom(source: any = {}) {
	        return new AppendImportUploadChunkRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.uploadId = source["uploadId"];
	        this.chunk = source["chunk"];
	    }
	}
	export class ApplyScriptGateRequest {
	    stageRunId: string;
	    decision: string;
	    artifactVersionId?: string;
	    issueIdsJson?: string;
	    lockedEntityRefsJson?: string;
	    instruction?: string;
	    reason?: string;
	    createdById?: string;
	
	    static createFrom(source: any = {}) {
	        return new ApplyScriptGateRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.stageRunId = source["stageRunId"];
	        this.decision = source["decision"];
	        this.artifactVersionId = source["artifactVersionId"];
	        this.issueIdsJson = source["issueIdsJson"];
	        this.lockedEntityRefsJson = source["lockedEntityRefsJson"];
	        this.instruction = source["instruction"];
	        this.reason = source["reason"];
	        this.createdById = source["createdById"];
	    }
	}
	export class AssetUsageDTO {
	    assetVersionId: string;
	    consumerType: string;
	    consumerId: string;
	    usageRole: string;
	    required: boolean;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new AssetUsageDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.assetVersionId = source["assetVersionId"];
	        this.consumerType = source["consumerType"];
	        this.consumerId = source["consumerId"];
	        this.usageRole = source["usageRole"];
	        this.required = source["required"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class ApprovalImpactDTO {
	    versionId: string;
	    replaces?: string;
	    consumers: AssetUsageDTO[];
	    requiredConsumers: AssetUsageDTO[];
	
	    static createFrom(source: any = {}) {
	        return new ApprovalImpactDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.versionId = source["versionId"];
	        this.replaces = source["replaces"];
	        this.consumers = this.convertValues(source["consumers"], AssetUsageDTO);
	        this.requiredConsumers = this.convertValues(source["requiredConsumers"], AssetUsageDTO);
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
	export class ApproveCandidateRequest {
	    panelVersionId: string;
	    approvedImageAssetVersionId: string;
	    candidateVersionIds: string[];
	    expectedRevision: number;
	
	    static createFrom(source: any = {}) {
	        return new ApproveCandidateRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.panelVersionId = source["panelVersionId"];
	        this.approvedImageAssetVersionId = source["approvedImageAssetVersionId"];
	        this.candidateVersionIds = source["candidateVersionIds"];
	        this.expectedRevision = source["expectedRevision"];
	    }
	}
	export class ApproveDirectorPlanVersionRequest {
	    versionId: string;
	    traceId?: string;
	
	    static createFrom(source: any = {}) {
	        return new ApproveDirectorPlanVersionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.versionId = source["versionId"];
	        this.traceId = source["traceId"];
	    }
	}
	export class ApproveExportRequest {
	    exportId: string;
	    episodeId?: string;
	    traceId?: string;
	
	    static createFrom(source: any = {}) {
	        return new ApproveExportRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.exportId = source["exportId"];
	        this.episodeId = source["episodeId"];
	        this.traceId = source["traceId"];
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
	export class ApproveStoryboardVersionRequest {
	    versionId: string;
	    traceId?: string;
	
	    static createFrom(source: any = {}) {
	        return new ApproveStoryboardVersionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.versionId = source["versionId"];
	        this.traceId = source["traceId"];
	    }
	}
	export class ApproveSubtitleTrackRequest {
	    trackId: string;
	    episodeId?: string;
	    traceId?: string;
	
	    static createFrom(source: any = {}) {
	        return new ApproveSubtitleTrackRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.trackId = source["trackId"];
	        this.episodeId = source["episodeId"];
	        this.traceId = source["traceId"];
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
	export class AssetRelationDTO {
	    id: string;
	    sourceAssetVersionId: string;
	    targetAssetVersionId: string;
	    type: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new AssetRelationDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.sourceAssetVersionId = source["sourceAssetVersionId"];
	        this.targetAssetVersionId = source["targetAssetVersionId"];
	        this.type = source["type"];
	        this.createdAt = source["createdAt"];
	    }
	}
	
	export class AssetVersionDTO {
	    id: string;
	    assetId: string;
	    versionNumber: number;
	    status: string;
	    basedOnVersionId?: string;
	    parentAssetVersionId?: string;
	    variantType?: string;
	    prompt?: string;
	    negativePrompt?: string;
	    providerConfigId?: string;
	    modelConfigId?: string;
	    modelParameters?: string;
	    seed?: string;
	    generationJobId?: string;
	    sourceAgentRunId?: string;
	    metadata?: string;
	    createdByType: string;
	    createdById?: string;
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
	        this.parentAssetVersionId = source["parentAssetVersionId"];
	        this.variantType = source["variantType"];
	        this.prompt = source["prompt"];
	        this.negativePrompt = source["negativePrompt"];
	        this.providerConfigId = source["providerConfigId"];
	        this.modelConfigId = source["modelConfigId"];
	        this.modelParameters = source["modelParameters"];
	        this.seed = source["seed"];
	        this.generationJobId = source["generationJobId"];
	        this.sourceAgentRunId = source["sourceAgentRunId"];
	        this.metadata = source["metadata"];
	        this.createdByType = source["createdByType"];
	        this.createdById = source["createdById"];
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
	export class AttachJobResultFile {
	    fileHash: string;
	    role?: string;
	    ordinal?: number;
	
	    static createFrom(source: any = {}) {
	        return new AttachJobResultFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fileHash = source["fileHash"];
	        this.role = source["role"];
	        this.ordinal = source["ordinal"];
	    }
	}
	export class AttachJobResultRequest {
	    assetId: string;
	    jobId: string;
	    files: AttachJobResultFile[];
	    prompt?: string;
	    providerConfigId?: string;
	    modelConfigId?: string;
	    modelParameters?: string;
	    seed?: string;
	    basedOnVersionId?: string;
	    parentAssetVersionId?: string;
	    variantType?: string;
	    sourceAgentRunId?: string;
	    createdById?: string;
	    metadata?: string;
	
	    static createFrom(source: any = {}) {
	        return new AttachJobResultRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.assetId = source["assetId"];
	        this.jobId = source["jobId"];
	        this.files = this.convertValues(source["files"], AttachJobResultFile);
	        this.prompt = source["prompt"];
	        this.providerConfigId = source["providerConfigId"];
	        this.modelConfigId = source["modelConfigId"];
	        this.modelParameters = source["modelParameters"];
	        this.seed = source["seed"];
	        this.basedOnVersionId = source["basedOnVersionId"];
	        this.parentAssetVersionId = source["parentAssetVersionId"];
	        this.variantType = source["variantType"];
	        this.sourceAgentRunId = source["sourceAgentRunId"];
	        this.createdById = source["createdById"];
	        this.metadata = source["metadata"];
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
	export class BatchSubmissionDTO {
	    shotId: string;
	    itemId: string;
	    candidateIndex: number;
	    jobId: string;
	    duplicate: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BatchSubmissionDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.shotId = source["shotId"];
	        this.itemId = source["itemId"];
	        this.candidateIndex = source["candidateIndex"];
	        this.jobId = source["jobId"];
	        this.duplicate = source["duplicate"];
	    }
	}
	export class BeginImportUploadRequest {
	    projectId: string;
	    name?: string;
	    format?: string;
	    totalBytes: number;
	    documentId?: string;
	    documentType?: string;
	    confirmDuplicate?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BeginImportUploadRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.name = source["name"];
	        this.format = source["format"];
	        this.totalBytes = source["totalBytes"];
	        this.documentId = source["documentId"];
	        this.documentType = source["documentType"];
	        this.confirmDuplicate = source["confirmDuplicate"];
	    }
	}
	export class BeginImportUploadResult {
	    uploadId: string;
	    chunkBytes: number;
	
	    static createFrom(source: any = {}) {
	        return new BeginImportUploadResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.uploadId = source["uploadId"];
	        this.chunkBytes = source["chunkBytes"];
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
	export class ChapterBoundaryDTO {
	    ordinal: number;
	    title: string;
	    startOffset: number;
	    titleEndOffset: number;
	    endOffset: number;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new ChapterBoundaryDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ordinal = source["ordinal"];
	        this.title = source["title"];
	        this.startOffset = source["startOffset"];
	        this.titleEndOffset = source["titleEndOffset"];
	        this.endOffset = source["endOffset"];
	        this.source = source["source"];
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
	export class CheckStoryboardGateRequest {
	    episodeId: string;
	    storyboardVersionId?: string;
	    storyboardId?: string;
	
	    static createFrom(source: any = {}) {
	        return new CheckStoryboardGateRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.episodeId = source["episodeId"];
	        this.storyboardVersionId = source["storyboardVersionId"];
	        this.storyboardId = source["storyboardId"];
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
	export class CollectBatchResultsRequest {
	    assetByItem: Record<string, string>;
	    jobIds: string[];
	    usageRole?: string;
	
	    static createFrom(source: any = {}) {
	        return new CollectBatchResultsRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.assetByItem = source["assetByItem"];
	        this.jobIds = source["jobIds"];
	        this.usageRole = source["usageRole"];
	    }
	}
	export class CollectedCandidateDTO {
	    jobId: string;
	    itemId: string;
	    assetId: string;
	    versionId?: string;
	    versionNumber?: number;
	    duplicate: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CollectedCandidateDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.jobId = source["jobId"];
	        this.itemId = source["itemId"];
	        this.assetId = source["assetId"];
	        this.versionId = source["versionId"];
	        this.versionNumber = source["versionNumber"];
	        this.duplicate = source["duplicate"];
	    }
	}
	export class ConfirmChaptersRequestDTO {
	    sourceDocumentVersionId: string;
	    traceId?: string;
	
	    static createFrom(source: any = {}) {
	        return new ConfirmChaptersRequestDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourceDocumentVersionId = source["sourceDocumentVersionId"];
	        this.traceId = source["traceId"];
	    }
	}
	export class CreateAdaptationStrategyVersionRequest {
	    episodeId: string;
	    basedOnVersionId?: string;
	    strategySummary?: string;
	    adaptationMode?: string;
	    mergedEventGroupsJson?: string;
	    originalAdditions?: string;
	    rationale?: string;
	    risks?: string;
	    eventLinks?: StrategyEventLinkDTO[];
	    changeReason?: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateAdaptationStrategyVersionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.episodeId = source["episodeId"];
	        this.basedOnVersionId = source["basedOnVersionId"];
	        this.strategySummary = source["strategySummary"];
	        this.adaptationMode = source["adaptationMode"];
	        this.mergedEventGroupsJson = source["mergedEventGroupsJson"];
	        this.originalAdditions = source["originalAdditions"];
	        this.rationale = source["rationale"];
	        this.risks = source["risks"];
	        this.eventLinks = this.convertValues(source["eventLinks"], StrategyEventLinkDTO);
	        this.changeReason = source["changeReason"];
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
	    targetPlatform?: string;
	    aspectRatio?: string;
	    resolution?: string;
	    expectedEpisodeCount?: number;
	    defaultEpisodeDurationSecs?: number;
	    audience?: string;
	    contentRating?: string;
	    adaptationMode?: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateProjectRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.projectType = source["projectType"];
	        this.language = source["language"];
	        this.targetPlatform = source["targetPlatform"];
	        this.aspectRatio = source["aspectRatio"];
	        this.resolution = source["resolution"];
	        this.expectedEpisodeCount = source["expectedEpisodeCount"];
	        this.defaultEpisodeDurationSecs = source["defaultEpisodeDurationSecs"];
	        this.audience = source["audience"];
	        this.contentRating = source["contentRating"];
	        this.adaptationMode = source["adaptationMode"];
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
	export class CreateStorySkeletonVersionRequest {
	    episodeId: string;
	    basedOnVersionId?: string;
	    openingHook?: string;
	    coreConflict?: string;
	    turningPointsJson?: string;
	    climax?: string;
	    endingHook?: string;
	    estimatedDurationSeconds?: number;
	    selectedEventIds?: string[];
	    changeReason?: string;
	
	    static createFrom(source: any = {}) {
	        return new CreateStorySkeletonVersionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.episodeId = source["episodeId"];
	        this.basedOnVersionId = source["basedOnVersionId"];
	        this.openingHook = source["openingHook"];
	        this.coreConflict = source["coreConflict"];
	        this.turningPointsJson = source["turningPointsJson"];
	        this.climax = source["climax"];
	        this.endingHook = source["endingHook"];
	        this.estimatedDurationSeconds = source["estimatedDurationSeconds"];
	        this.selectedEventIds = source["selectedEventIds"];
	        this.changeReason = source["changeReason"];
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
	export class DeleteMemoryRequest {
	    memoryId: string;
	    invalidateSummaries?: boolean;
	    confirm: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DeleteMemoryRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.memoryId = source["memoryId"];
	        this.invalidateSummaries = source["invalidateSummaries"];
	        this.confirm = source["confirm"];
	    }
	}
	export class DiagnosticsSectionDTO {
	    section: string;
	    bytes: number;
	    note: string;
	    included: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DiagnosticsSectionDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.section = source["section"];
	        this.bytes = source["bytes"];
	        this.note = source["note"];
	        this.included = source["included"];
	    }
	}
	export class DiagnosticsBundleDTO {
	    sections: DiagnosticsSectionDTO[];
	    files: Record<string, string>;
	    totalBytes: number;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new DiagnosticsBundleDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sections = this.convertValues(source["sections"], DiagnosticsSectionDTO);
	        this.files = source["files"];
	        this.totalBytes = source["totalBytes"];
	        this.createdAt = source["createdAt"];
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
	export class DiagnosticsPlanDTO {
	    sections: DiagnosticsSectionDTO[];
	    totalBytes: number;
	
	    static createFrom(source: any = {}) {
	        return new DiagnosticsPlanDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sections = this.convertValues(source["sections"], DiagnosticsSectionDTO);
	        this.totalBytes = source["totalBytes"];
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
	
	export class DialogueLineDTO {
	    lineId: string;
	    sceneId: string;
	    ordinal: number;
	    type: string;
	    characterEntityId?: string;
	    text: string;
	    emotion?: string;
	    performanceNote?: string;
	    sourceStoryEventId?: string;
	    locked: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DialogueLineDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.lineId = source["lineId"];
	        this.sceneId = source["sceneId"];
	        this.ordinal = source["ordinal"];
	        this.type = source["type"];
	        this.characterEntityId = source["characterEntityId"];
	        this.text = source["text"];
	        this.emotion = source["emotion"];
	        this.performanceNote = source["performanceNote"];
	        this.sourceStoryEventId = source["sourceStoryEventId"];
	        this.locked = source["locked"];
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
	export class DocumentDTO {
	    name: string;
	    text: string;
	    extension: string;
	    suggestedName: string;
	
	    static createFrom(source: any = {}) {
	        return new DocumentDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.text = source["text"];
	        this.extension = source["extension"];
	        this.suggestedName = source["suggestedName"];
	    }
	}
	export class DocumentRangeDTO {
	    text: string;
	    startRune: number;
	    endRune: number;
	    totalRunes: number;
	
	    static createFrom(source: any = {}) {
	        return new DocumentRangeDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.startRune = source["startRune"];
	        this.endRune = source["endRune"];
	        this.totalRunes = source["totalRunes"];
	    }
	}
	export class DomainEventDTO {
	    eventId: string;
	    eventType: string;
	    schemaVersion: number;
	    aggregateType: string;
	    aggregateId: string;
	    projectId: string;
	    occurredAt: string;
	    traceId: string;
	    payload: string;
	
	    static createFrom(source: any = {}) {
	        return new DomainEventDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.eventId = source["eventId"];
	        this.eventType = source["eventType"];
	        this.schemaVersion = source["schemaVersion"];
	        this.aggregateType = source["aggregateType"];
	        this.aggregateId = source["aggregateId"];
	        this.projectId = source["projectId"];
	        this.occurredAt = source["occurredAt"];
	        this.traceId = source["traceId"];
	        this.payload = source["payload"];
	    }
	}
	export class DraftSubtitlesRequest {
	    episodeId: string;
	    scriptVersionId: string;
	
	    static createFrom(source: any = {}) {
	        return new DraftSubtitlesRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.episodeId = source["episodeId"];
	        this.scriptVersionId = source["scriptVersionId"];
	    }
	}
	export class EditSkeletonInput {
	    openingHook?: string;
	    coreConflict?: string;
	    turningPointsJson?: string;
	    climax?: string;
	    endingHook?: string;
	    estimatedDurationSeconds?: number;
	    selectedEventIds?: string[];
	
	    static createFrom(source: any = {}) {
	        return new EditSkeletonInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.openingHook = source["openingHook"];
	        this.coreConflict = source["coreConflict"];
	        this.turningPointsJson = source["turningPointsJson"];
	        this.climax = source["climax"];
	        this.endingHook = source["endingHook"];
	        this.estimatedDurationSeconds = source["estimatedDurationSeconds"];
	        this.selectedEventIds = source["selectedEventIds"];
	    }
	}
	export class EditStrategyInput {
	    strategySummary?: string;
	    adaptationMode?: string;
	    mergedEventGroupsJson?: string;
	    originalAdditions?: string;
	    rationale?: string;
	    risks?: string;
	    eventLinks?: StrategyEventLinkDTO[];
	
	    static createFrom(source: any = {}) {
	        return new EditStrategyInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.strategySummary = source["strategySummary"];
	        this.adaptationMode = source["adaptationMode"];
	        this.mergedEventGroupsJson = source["mergedEventGroupsJson"];
	        this.originalAdditions = source["originalAdditions"];
	        this.rationale = source["rationale"];
	        this.risks = source["risks"];
	        this.eventLinks = this.convertValues(source["eventLinks"], StrategyEventLinkDTO);
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
	export class ExportManifestDocumentRequest {
	    episodeId: string;
	
	    static createFrom(source: any = {}) {
	        return new ExportManifestDocumentRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.episodeId = source["episodeId"];
	    }
	}
	export class ExportRecordDTO {
	    id: string;
	    episodeId: string;
	    versionNumber: number;
	    status: string;
	    quality: string;
	    width: number;
	    height: number;
	    durationMs: number;
	    outputFileHash?: string;
	    subtitleTrackId?: string;
	    manifestJson?: string;
	    approvalTraceId?: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new ExportRecordDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.episodeId = source["episodeId"];
	        this.versionNumber = source["versionNumber"];
	        this.status = source["status"];
	        this.quality = source["quality"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.durationMs = source["durationMs"];
	        this.outputFileHash = source["outputFileHash"];
	        this.subtitleTrackId = source["subtitleTrackId"];
	        this.manifestJson = source["manifestJson"];
	        this.approvalTraceId = source["approvalTraceId"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class ExportScriptRequest {
	    episodeId: string;
	    versionId?: string;
	    format: string;
	    includeShots?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ExportScriptRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.episodeId = source["episodeId"];
	        this.versionId = source["versionId"];
	        this.format = source["format"];
	        this.includeShots = source["includeShots"];
	    }
	}
	export class ExportShotListRequest {
	    episodeId: string;
	    versionId?: string;
	    format: string;
	
	    static createFrom(source: any = {}) {
	        return new ExportShotListRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.episodeId = source["episodeId"];
	        this.versionId = source["versionId"];
	        this.format = source["format"];
	    }
	}
	export class ExportSubtitlesRequest {
	    trackId: string;
	    format: string;
	
	    static createFrom(source: any = {}) {
	        return new ExportSubtitlesRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.trackId = source["trackId"];
	        this.format = source["format"];
	    }
	}
	export class ExtractChapterRequest {
	    chapterId: string;
	
	    static createFrom(source: any = {}) {
	        return new ExtractChapterRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.chapterId = source["chapterId"];
	    }
	}
	export class ExtractionResultDTO {
	    chapterId: string;
	    extracted: number;
	    entities: number;
	    events: number;
	    relations: number;
	    aliases: number;
	    participants: number;
	    evidence: number;
	    summary?: string;
	
	    static createFrom(source: any = {}) {
	        return new ExtractionResultDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.chapterId = source["chapterId"];
	        this.extracted = source["extracted"];
	        this.entities = source["entities"];
	        this.events = source["events"];
	        this.relations = source["relations"];
	        this.aliases = source["aliases"];
	        this.participants = source["participants"];
	        this.evidence = source["evidence"];
	        this.summary = source["summary"];
	    }
	}
	export class FieldChangeDTO {
	    field: string;
	    before: string;
	    after: string;
	
	    static createFrom(source: any = {}) {
	        return new FieldChangeDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.field = source["field"];
	        this.before = source["before"];
	        this.after = source["after"];
	    }
	}
	export class FieldLockDTO {
	    versionId: string;
	    family: string;
	    field: string;
	    lockedBy?: string;
	    createdAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new FieldLockDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.versionId = source["versionId"];
	        this.family = source["family"];
	        this.field = source["field"];
	        this.lockedBy = source["lockedBy"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class FinishImportUploadRequest {
	    uploadId: string;
	
	    static createFrom(source: any = {}) {
	        return new FinishImportUploadRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.uploadId = source["uploadId"];
	    }
	}
	export class GarbageCandidateDTO {
	    hash: string;
	    storageKey: string;
	    mimeType: string;
	    sizeBytes: number;
	
	    static createFrom(source: any = {}) {
	        return new GarbageCandidateDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hash = source["hash"];
	        this.storageKey = source["storageKey"];
	        this.mimeType = source["mimeType"];
	        this.sizeBytes = source["sizeBytes"];
	    }
	}
	export class GarbageCollectResultDTO {
	    removed: GarbageCandidateDTO[];
	    skipped: string[];
	    freedBytes: number;
	    cancelled: boolean;
	    bytesRemoved: number;
	
	    static createFrom(source: any = {}) {
	        return new GarbageCollectResultDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.removed = this.convertValues(source["removed"], GarbageCandidateDTO);
	        this.skipped = source["skipped"];
	        this.freedBytes = source["freedBytes"];
	        this.cancelled = source["cancelled"];
	        this.bytesRemoved = source["bytesRemoved"];
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
	export class GarbagePreviewDTO {
	    candidates: GarbageCandidateDTO[];
	    totalBytes: number;
	    collecting: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GarbagePreviewDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.candidates = this.convertValues(source["candidates"], GarbageCandidateDTO);
	        this.totalBytes = source["totalBytes"];
	        this.collecting = source["collecting"];
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
	export class ImportDocumentRequest {
	    projectId: string;
	    documentId?: string;
	    documentType?: string;
	    name?: string;
	    format?: string;
	    content: number[];
	    confirmDuplicate?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ImportDocumentRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.documentId = source["documentId"];
	        this.documentType = source["documentType"];
	        this.name = source["name"];
	        this.format = source["format"];
	        this.content = source["content"];
	        this.confirmDuplicate = source["confirmDuplicate"];
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
	export class ImportDocumentResult {
	    document: SourceDocumentDTO;
	    version: SourceDocumentVersionDTO;
	    chapters: ChapterDTO[];
	    charCount: number;
	    encoding: string;
	    format: string;
	
	    static createFrom(source: any = {}) {
	        return new ImportDocumentResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.document = this.convertValues(source["document"], SourceDocumentDTO);
	        this.version = this.convertValues(source["version"], SourceDocumentVersionDTO);
	        this.chapters = this.convertValues(source["chapters"], ChapterDTO);
	        this.charCount = source["charCount"];
	        this.encoding = source["encoding"];
	        this.format = source["format"];
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
	export class ItemChangeDTO {
	    kind: string;
	    ordinal: number;
	    itemId?: string;
	    fields?: FieldChangeDTO[];
	    locked: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ItemChangeDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.ordinal = source["ordinal"];
	        this.itemId = source["itemId"];
	        this.fields = this.convertValues(source["fields"], FieldChangeDTO);
	        this.locked = source["locked"];
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
	
	export class LineageDTO {
	    from: AssetRelationDTO[];
	    to: AssetRelationDTO[];
	
	    static createFrom(source: any = {}) {
	        return new LineageDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.from = this.convertValues(source["from"], AssetRelationDTO);
	        this.to = this.convertValues(source["to"], AssetRelationDTO);
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
	export class ListDomainEventsRequest {
	    projectId: string;
	    aggregateType?: string;
	    aggregateId?: string;
	    eventType?: string;
	    traceId?: string;
	    limit?: number;
	
	    static createFrom(source: any = {}) {
	        return new ListDomainEventsRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.aggregateType = source["aggregateType"];
	        this.aggregateId = source["aggregateId"];
	        this.eventType = source["eventType"];
	        this.traceId = source["traceId"];
	        this.limit = source["limit"];
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
	export class ListProjectEventParticipantsRequest {
	    projectId: string;
	    status?: string;
	
	    static createFrom(source: any = {}) {
	        return new ListProjectEventParticipantsRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.status = source["status"];
	    }
	}
	export class ListProjectRulesRequest {
	    projectId: string;
	    includeDeleted?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ListProjectRulesRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.includeDeleted = source["includeDeleted"];
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
	export class LockStoryEntityRequest {
	    id: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new LockStoryEntityRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.revision = source["revision"];
	    }
	}
	export class LockableFieldDTO {
	    family: string;
	    field: string;
	
	    static createFrom(source: any = {}) {
	        return new LockableFieldDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.family = source["family"];
	        this.field = source["field"];
	    }
	}
	export class ScriptStructureShotInput {
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
	        return new ScriptStructureShotInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
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
	export class ScriptStructureLineInput {
	    type?: string;
	    characterEntityId?: string;
	    text?: string;
	    emotion?: string;
	    performanceNote?: string;
	    sourceStoryEventId?: string;
	
	    static createFrom(source: any = {}) {
	        return new ScriptStructureLineInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.characterEntityId = source["characterEntityId"];
	        this.text = source["text"];
	        this.emotion = source["emotion"];
	        this.performanceNote = source["performanceNote"];
	        this.sourceStoryEventId = source["sourceStoryEventId"];
	    }
	}
	export class ScriptStructureSceneInput {
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
	    dialogueLines?: ScriptStructureLineInput[];
	    shots?: ScriptStructureShotInput[];
	
	    static createFrom(source: any = {}) {
	        return new ScriptStructureSceneInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
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
	        this.dialogueLines = this.convertValues(source["dialogueLines"], ScriptStructureLineInput);
	        this.shots = this.convertValues(source["shots"], ScriptStructureShotInput);
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
	export class ManualEditScriptRequest {
	    stageRunId: string;
	    stage: string;
	    projectId: string;
	    episodeId: string;
	    basedOnVersionId?: string;
	    skeletonVersionId?: string;
	    strategyVersionId?: string;
	    skeleton?: EditSkeletonInput;
	    strategy?: EditStrategyInput;
	    scenes?: ScriptStructureSceneInput[];
	    summary?: string;
	    changeReason?: string;
	    createdById?: string;
	
	    static createFrom(source: any = {}) {
	        return new ManualEditScriptRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.stageRunId = source["stageRunId"];
	        this.stage = source["stage"];
	        this.projectId = source["projectId"];
	        this.episodeId = source["episodeId"];
	        this.basedOnVersionId = source["basedOnVersionId"];
	        this.skeletonVersionId = source["skeletonVersionId"];
	        this.strategyVersionId = source["strategyVersionId"];
	        this.skeleton = this.convertValues(source["skeleton"], EditSkeletonInput);
	        this.strategy = this.convertValues(source["strategy"], EditStrategyInput);
	        this.scenes = this.convertValues(source["scenes"], ScriptStructureSceneInput);
	        this.summary = source["summary"];
	        this.changeReason = source["changeReason"];
	        this.createdById = source["createdById"];
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
	export class MediaCapabilityDTO {
	    exportAvailable: boolean;
	    diagnostic?: string;
	    saveAvailable: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MediaCapabilityDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.exportAvailable = source["exportAvailable"];
	        this.diagnostic = source["diagnostic"];
	        this.saveAvailable = source["saveAvailable"];
	    }
	}
	export class MemoryDTO {
	    id: string;
	    type: string;
	    scopeProject: string;
	    scopeEpisode?: string;
	    scopeAgent?: string;
	    role?: string;
	    agentKey?: string;
	    content: string;
	    importance: number;
	    confidence: number;
	    embedded: boolean;
	    embeddingModel?: string;
	    embeddingVersion?: string;
	    embeddedAt?: string;
	    summarized: boolean;
	    summaryLevel?: number;
	    locked: boolean;
	    sourceType?: string;
	    sourceId?: string;
	    deletedAt?: string;
	    createdAt: string;
	    updatedAt: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new MemoryDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.type = source["type"];
	        this.scopeProject = source["scopeProject"];
	        this.scopeEpisode = source["scopeEpisode"];
	        this.scopeAgent = source["scopeAgent"];
	        this.role = source["role"];
	        this.agentKey = source["agentKey"];
	        this.content = source["content"];
	        this.importance = source["importance"];
	        this.confidence = source["confidence"];
	        this.embedded = source["embedded"];
	        this.embeddingModel = source["embeddingModel"];
	        this.embeddingVersion = source["embeddingVersion"];
	        this.embeddedAt = source["embeddedAt"];
	        this.summarized = source["summarized"];
	        this.summaryLevel = source["summaryLevel"];
	        this.locked = source["locked"];
	        this.sourceType = source["sourceType"];
	        this.sourceId = source["sourceId"];
	        this.deletedAt = source["deletedAt"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.revision = source["revision"];
	    }
	}
	export class MemoryDeleteResultDTO {
	    deleted: boolean;
	    invalidatedSummaries: string[];
	    keptSummaries: string[];
	
	    static createFrom(source: any = {}) {
	        return new MemoryDeleteResultDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.deleted = source["deleted"];
	        this.invalidatedSummaries = source["invalidatedSummaries"];
	        this.keptSummaries = source["keptSummaries"];
	    }
	}
	export class MemoryEditRequest {
	    memoryId: string;
	    content: string;
	    confirm: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MemoryEditRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.memoryId = source["memoryId"];
	        this.content = source["content"];
	        this.confirm = source["confirm"];
	    }
	}
	export class MemoryEntityLinkDTO {
	    entityType: string;
	    entityId: string;
	    relationType: string;
	
	    static createFrom(source: any = {}) {
	        return new MemoryEntityLinkDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.entityType = source["entityType"];
	        this.entityId = source["entityId"];
	        this.relationType = source["relationType"];
	    }
	}
	export class MemoryLinkRequest {
	    memoryId: string;
	    entityType: string;
	    entityId: string;
	
	    static createFrom(source: any = {}) {
	        return new MemoryLinkRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.memoryId = source["memoryId"];
	        this.entityType = source["entityType"];
	        this.entityId = source["entityId"];
	    }
	}
	export class MemoryPinRequest {
	    memoryId: string;
	    locked: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MemoryPinRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.memoryId = source["memoryId"];
	        this.locked = source["locked"];
	    }
	}
	export class MemoryQuery {
	    projectId: string;
	    types?: string[];
	    includeDeleted?: boolean;
	    limit?: number;
	
	    static createFrom(source: any = {}) {
	        return new MemoryQuery(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.types = source["types"];
	        this.includeDeleted = source["includeDeleted"];
	        this.limit = source["limit"];
	    }
	}
	export class MemoryRebuildResultDTO {
	    model: string;
	    version: string;
	    rebuilt: number;
	    failed: number;
	    remaining: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MemoryRebuildResultDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.model = source["model"];
	        this.version = source["version"];
	        this.rebuilt = source["rebuilt"];
	        this.failed = source["failed"];
	        this.remaining = source["remaining"];
	    }
	}
	export class MemoryRecallItemDTO {
	    messageId: string;
	    role?: string;
	    content: string;
	    provenance?: string;
	    createdAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new MemoryRecallItemDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.messageId = source["messageId"];
	        this.role = source["role"];
	        this.content = source["content"];
	        this.provenance = source["provenance"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class MemoryScoredItemDTO {
	    memoryId: string;
	    type: string;
	    content: string;
	    score: number;
	    similarity: number;
	    channel: string;
	    pinned: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MemoryScoredItemDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.memoryId = source["memoryId"];
	        this.type = source["type"];
	        this.content = source["content"];
	        this.score = source["score"];
	        this.similarity = source["similarity"];
	        this.channel = source["channel"];
	        this.pinned = source["pinned"];
	    }
	}
	export class MemoryRecallPreviewDTO {
	    recent: MemoryRecallItemDTO[];
	    facts: MemoryScoredItemDTO[];
	    summaries: MemoryScoredItemDTO[];
	    semantic: MemoryScoredItemDTO[];
	    usedTokens: number;
	    truncated: boolean;
	    semanticSearched: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MemoryRecallPreviewDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.recent = this.convertValues(source["recent"], MemoryRecallItemDTO);
	        this.facts = this.convertValues(source["facts"], MemoryScoredItemDTO);
	        this.summaries = this.convertValues(source["summaries"], MemoryScoredItemDTO);
	        this.semantic = this.convertValues(source["semantic"], MemoryScoredItemDTO);
	        this.usedTokens = source["usedTokens"];
	        this.truncated = source["truncated"];
	        this.semanticSearched = source["semanticSearched"];
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
	
	export class MemorySummarizeResultDTO {
	    created: boolean;
	    summary?: MemoryDTO;
	
	    static createFrom(source: any = {}) {
	        return new MemorySummarizeResultDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.created = source["created"];
	        this.summary = this.convertValues(source["summary"], MemoryDTO);
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
	export class MemorySummarySourceDTO {
	    memoryId: string;
	    order: number;
	    role?: string;
	    agentKey?: string;
	    createdAt?: string;
	    messageId?: string;
	    missing?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MemorySummarySourceDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.memoryId = source["memoryId"];
	        this.order = source["order"];
	        this.role = source["role"];
	        this.agentKey = source["agentKey"];
	        this.createdAt = source["createdAt"];
	        this.messageId = source["messageId"];
	        this.missing = source["missing"];
	    }
	}
	export class MergeChapterRequest {
	    firstChapterId: string;
	    secondChapterId: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new MergeChapterRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.firstChapterId = source["firstChapterId"];
	        this.secondChapterId = source["secondChapterId"];
	        this.revision = source["revision"];
	    }
	}
	export class MergeStoryEntityRequest {
	    survivorId: string;
	    absorbedId: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new MergeStoryEntityRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.survivorId = source["survivorId"];
	        this.absorbedId = source["absorbedId"];
	        this.revision = source["revision"];
	    }
	}
	export class MergeStoryEntityResultDTO {
	    aliasesMoved: number;
	    aliasesDropped: number;
	    participantsMoved: number;
	    characterStatesMoved: number;
	    factSourcesMoved: number;
	    absorbedName?: string;
	
	    static createFrom(source: any = {}) {
	        return new MergeStoryEntityResultDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.aliasesMoved = source["aliasesMoved"];
	        this.aliasesDropped = source["aliasesDropped"];
	        this.participantsMoved = source["participantsMoved"];
	        this.characterStatesMoved = source["characterStatesMoved"];
	        this.factSourcesMoved = source["factSourcesMoved"];
	        this.absorbedName = source["absorbedName"];
	    }
	}
	export class MissingLineDTO {
	    lineId: string;
	    type: string;
	    characterEntityId?: string;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new MissingLineDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.lineId = source["lineId"];
	        this.type = source["type"];
	        this.characterEntityId = source["characterEntityId"];
	        this.text = source["text"];
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
	
	export class OpenStoryConflictRequest {
	    projectId: string;
	    leftFactType: string;
	    leftFactId: string;
	    rightFactType: string;
	    rightFactId: string;
	    conflictType?: string;
	
	    static createFrom(source: any = {}) {
	        return new OpenStoryConflictRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.leftFactType = source["leftFactType"];
	        this.leftFactId = source["leftFactId"];
	        this.rightFactType = source["rightFactType"];
	        this.rightFactId = source["rightFactId"];
	        this.conflictType = source["conflictType"];
	    }
	}
	export class PrecheckImportRequest {
	    projectId: string;
	    format?: string;
	    name?: string;
	    content: number[];
	
	    static createFrom(source: any = {}) {
	        return new PrecheckImportRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.format = source["format"];
	        this.name = source["name"];
	        this.content = source["content"];
	    }
	}
	export class PrecheckImportResult {
	    format: string;
	    encoding: string;
	    charCount: number;
	    chapterCount: number;
	    chapters: ChapterBoundaryDTO[];
	    duplicate: boolean;
	    duplicateDocumentId?: string;
	    duplicateDocumentName?: string;
	    warnings: string[];
	
	    static createFrom(source: any = {}) {
	        return new PrecheckImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.format = source["format"];
	        this.encoding = source["encoding"];
	        this.charCount = source["charCount"];
	        this.chapterCount = source["chapterCount"];
	        this.chapters = this.convertValues(source["chapters"], ChapterBoundaryDTO);
	        this.duplicate = source["duplicate"];
	        this.duplicateDocumentId = source["duplicateDocumentId"];
	        this.duplicateDocumentName = source["duplicateDocumentName"];
	        this.warnings = source["warnings"];
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
	
	export class ProjectRuleDTO {
	    id: string;
	    projectId: string;
	    category: string;
	    name: string;
	    content: string;
	    strength: string;
	    status: string;
	    sourceType: string;
	    sourceId: string;
	    lockedByUser: boolean;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new ProjectRuleDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.projectId = source["projectId"];
	        this.category = source["category"];
	        this.name = source["name"];
	        this.content = source["content"];
	        this.strength = source["strength"];
	        this.status = source["status"];
	        this.sourceType = source["sourceType"];
	        this.sourceId = source["sourceId"];
	        this.lockedByUser = source["lockedByUser"];
	        this.revision = source["revision"];
	    }
	}
	export class ProjectScriptVersionRequest {
	    projectId: string;
	    scriptVersionId: string;
	
	    static createFrom(source: any = {}) {
	        return new ProjectScriptVersionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.scriptVersionId = source["scriptVersionId"];
	    }
	}
	export class ProjectScriptVersionResult {
	    sceneNodeIds: string[];
	
	    static createFrom(source: any = {}) {
	        return new ProjectScriptVersionResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sceneNodeIds = source["sceneNodeIds"];
	    }
	}
	export class ProjectSettingsDTO {
	    projectId: string;
	    targetPlatform: string;
	    aspectRatio: string;
	    resolution: string;
	    expectedEpisodeCount: number;
	    defaultEpisodeDurationSecs: number;
	    audience: string;
	    contentRating: string;
	    adaptationMode: string;
	    language: string;
	    timezone: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new ProjectSettingsDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.targetPlatform = source["targetPlatform"];
	        this.aspectRatio = source["aspectRatio"];
	        this.resolution = source["resolution"];
	        this.expectedEpisodeCount = source["expectedEpisodeCount"];
	        this.defaultEpisodeDurationSecs = source["defaultEpisodeDurationSecs"];
	        this.audience = source["audience"];
	        this.contentRating = source["contentRating"];
	        this.adaptationMode = source["adaptationMode"];
	        this.language = source["language"];
	        this.timezone = source["timezone"];
	        this.revision = source["revision"];
	    }
	}
	export class ProviderConfigRequest {
	    id: string;
	    kind: string;
	    displayName: string;
	    baseUrl: string;
	    maxConcurrency: number;
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
	        this.maxConcurrency = source["maxConcurrency"];
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
	export class ReadDocumentRangeRequest {
	    sourceDocumentVersionId: string;
	    startRune: number;
	    endRune: number;
	    direction?: string;
	    totalRunes?: number;
	
	    static createFrom(source: any = {}) {
	        return new ReadDocumentRangeRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sourceDocumentVersionId = source["sourceDocumentVersionId"];
	        this.startRune = source["startRune"];
	        this.endRune = source["endRune"];
	        this.direction = source["direction"];
	        this.totalRunes = source["totalRunes"];
	    }
	}
	export class RebuildMemoryEmbeddingRequest {
	    projectId: string;
	    limit?: number;
	
	    static createFrom(source: any = {}) {
	        return new RebuildMemoryEmbeddingRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.limit = source["limit"];
	    }
	}
	export class RecallPreviewRequest {
	    projectId: string;
	    episodeId?: string;
	    agentKey?: string;
	    query?: string;
	    threshold?: number;
	    tokenBudget?: number;
	
	    static createFrom(source: any = {}) {
	        return new RecallPreviewRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.episodeId = source["episodeId"];
	        this.agentKey = source["agentKey"];
	        this.query = source["query"];
	        this.threshold = source["threshold"];
	        this.tokenBudget = source["tokenBudget"];
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
	export class RememberFactRequest {
	    projectId: string;
	    episodeId?: string;
	    content: string;
	    importance?: number;
	    entityType?: string;
	    entityId?: string;
	    createdById?: string;
	    embed?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RememberFactRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.episodeId = source["episodeId"];
	        this.content = source["content"];
	        this.importance = source["importance"];
	        this.entityType = source["entityType"];
	        this.entityId = source["entityId"];
	        this.createdById = source["createdById"];
	        this.embed = source["embed"];
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
	export class ReorderStoryboardItemRequest {
	    itemId: string;
	    toOrdinal: number;
	
	    static createFrom(source: any = {}) {
	        return new ReorderStoryboardItemRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.itemId = source["itemId"];
	        this.toOrdinal = source["toOrdinal"];
	    }
	}
	export class ReorderStoryboardItemResultDTO {
	    moved: number;
	    order: number[];
	
	    static createFrom(source: any = {}) {
	        return new ReorderStoryboardItemResultDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.moved = source["moved"];
	        this.order = source["order"];
	    }
	}
	export class ResolveStoryConflictRequest {
	    conflictId: string;
	    resolution: string;
	    resolvedBy: string;
	
	    static createFrom(source: any = {}) {
	        return new ResolveStoryConflictRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.conflictId = source["conflictId"];
	        this.resolution = source["resolution"];
	        this.resolvedBy = source["resolvedBy"];
	    }
	}
	export class RestoreResult {
	    manifestVersion: number;
	    projects: number;
	    assets: number;
	    files: number;
	    databasePath: string;
	    previousDatabasePath?: string;
	    rolledBack: boolean;
	    restartRequired: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RestoreResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.manifestVersion = source["manifestVersion"];
	        this.projects = source["projects"];
	        this.assets = source["assets"];
	        this.files = source["files"];
	        this.databasePath = source["databasePath"];
	        this.previousDatabasePath = source["previousDatabasePath"];
	        this.rolledBack = source["rolledBack"];
	        this.restartRequired = source["restartRequired"];
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
	    source: string;
	    status: string;
	    resolvedBy?: string;
	    resolvedAt?: string;
	    createdAt: string;
	    category?: string;
	
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
	        this.source = source["source"];
	        this.status = source["status"];
	        this.resolvedBy = source["resolvedBy"];
	        this.resolvedAt = source["resolvedAt"];
	        this.createdAt = source["createdAt"];
	        this.category = source["category"];
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
	export class RunExportRequest {
	    episodeId: string;
	    boardVersionId?: string;
	    subtitleTrackId?: string;
	    quality: string;
	    width?: number;
	    height?: number;
	    fps?: number;
	    subtitleMode?: string;
	
	    static createFrom(source: any = {}) {
	        return new RunExportRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.episodeId = source["episodeId"];
	        this.boardVersionId = source["boardVersionId"];
	        this.subtitleTrackId = source["subtitleTrackId"];
	        this.quality = source["quality"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.fps = source["fps"];
	        this.subtitleMode = source["subtitleMode"];
	    }
	}
	export class RunImageBatchRequest {
	    storyboardVersionId: string;
	    episodeId: string;
	    projectId: string;
	    perShotCandidates: number;
	    shotIds?: string[];
	    providerId: string;
	    modelName: string;
	    promptSuffix?: string;
	    seed?: string;
	
	    static createFrom(source: any = {}) {
	        return new RunImageBatchRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.storyboardVersionId = source["storyboardVersionId"];
	        this.episodeId = source["episodeId"];
	        this.projectId = source["projectId"];
	        this.perShotCandidates = source["perShotCandidates"];
	        this.shotIds = source["shotIds"];
	        this.providerId = source["providerId"];
	        this.modelName = source["modelName"];
	        this.promptSuffix = source["promptSuffix"];
	        this.seed = source["seed"];
	    }
	}
	export class RunImageBatchResultDTO {
	    submissions: BatchSubmissionDTO[];
	    duplicate: number;
	
	    static createFrom(source: any = {}) {
	        return new RunImageBatchResultDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.submissions = this.convertValues(source["submissions"], BatchSubmissionDTO);
	        this.duplicate = source["duplicate"];
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
	export class RunScriptStageRequest {
	    workflowRunId: string;
	    stage: string;
	    projectId: string;
	    episodeId: string;
	    task?: string;
	    taskIsUntrusted?: boolean;
	    userMessage?: string;
	    fixFromStageRunId?: string;
	    skeletonVersionId?: string;
	    strategyVersionId?: string;
	    scriptVersionId?: string;
	    selectedEventIds?: string[];
	    directorPlanVersionId?: string;
	    storyboardVersionId?: string;
	    storyboardId?: string;
	    storyboardItemId?: string;
	    assetGapReportId?: string;
	    shotIds?: string[];
	    modelId?: string;
	    providerId?: string;
	
	    static createFrom(source: any = {}) {
	        return new RunScriptStageRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.workflowRunId = source["workflowRunId"];
	        this.stage = source["stage"];
	        this.projectId = source["projectId"];
	        this.episodeId = source["episodeId"];
	        this.task = source["task"];
	        this.taskIsUntrusted = source["taskIsUntrusted"];
	        this.userMessage = source["userMessage"];
	        this.fixFromStageRunId = source["fixFromStageRunId"];
	        this.skeletonVersionId = source["skeletonVersionId"];
	        this.strategyVersionId = source["strategyVersionId"];
	        this.scriptVersionId = source["scriptVersionId"];
	        this.selectedEventIds = source["selectedEventIds"];
	        this.directorPlanVersionId = source["directorPlanVersionId"];
	        this.storyboardVersionId = source["storyboardVersionId"];
	        this.storyboardId = source["storyboardId"];
	        this.storyboardItemId = source["storyboardItemId"];
	        this.assetGapReportId = source["assetGapReportId"];
	        this.shotIds = source["shotIds"];
	        this.modelId = source["modelId"];
	        this.providerId = source["providerId"];
	    }
	}
	export class RunScriptSupervisionRequest {
	    stageRunId: string;
	    projectId: string;
	    episodeId: string;
	    artifactVersionId?: string;
	    modelId?: string;
	    providerId?: string;
	
	    static createFrom(source: any = {}) {
	        return new RunScriptSupervisionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.stageRunId = source["stageRunId"];
	        this.projectId = source["projectId"];
	        this.episodeId = source["episodeId"];
	        this.artifactVersionId = source["artifactVersionId"];
	        this.modelId = source["modelId"];
	        this.providerId = source["providerId"];
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
	export class SaveDocumentRequest {
	    text: string;
	    suggestedName: string;
	
	    static createFrom(source: any = {}) {
	        return new SaveDocumentRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	        this.suggestedName = source["suggestedName"];
	    }
	}
	export class SaveExportRequest {
	    storageKey: string;
	    suggestedName: string;
	
	    static createFrom(source: any = {}) {
	        return new SaveExportRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.storageKey = source["storageKey"];
	        this.suggestedName = source["suggestedName"];
	    }
	}
	export class SaveFileResultDTO {
	    written: boolean;
	    path?: string;
	
	    static createFrom(source: any = {}) {
	        return new SaveFileResultDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.written = source["written"];
	        this.path = source["path"];
	    }
	}
	export class SaveScriptStructureRequest {
	    scriptVersionId: string;
	    projectId?: string;
	    scenes: ScriptStructureSceneInput[];
	    summary?: string;
	    changeReason?: string;
	
	    static createFrom(source: any = {}) {
	        return new SaveScriptStructureRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scriptVersionId = source["scriptVersionId"];
	        this.projectId = source["projectId"];
	        this.scenes = this.convertValues(source["scenes"], ScriptStructureSceneInput);
	        this.summary = source["summary"];
	        this.changeReason = source["changeReason"];
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
	export class SceneStructureShotDTO {
	    shotId: string;
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
	
	    static createFrom(source: any = {}) {
	        return new SceneStructureShotDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.shotId = source["shotId"];
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
	    }
	}
	export class SceneStructureDTO {
	    sceneId: string;
	    scriptVersionId: string;
	    ordinal: number;
	    sceneNumber?: string;
	    slugline: string;
	    interiorExterior: string;
	    locationEntityId?: string;
	    timeOfDay?: string;
	    summary?: string;
	    dramaticGoal?: string;
	    estimatedDurationSeconds: number;
	    sourceStoryEventId?: string;
	    isOriginalAdaptation: boolean;
	    dialogueLines: DialogueLineDTO[];
	    shots: SceneStructureShotDTO[];
	
	    static createFrom(source: any = {}) {
	        return new SceneStructureDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sceneId = source["sceneId"];
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
	        this.dialogueLines = this.convertValues(source["dialogueLines"], DialogueLineDTO);
	        this.shots = this.convertValues(source["shots"], SceneStructureShotDTO);
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
	export class ScriptStageResultDTO {
	    stageRunId: string;
	    status: string;
	    attempt: number;
	    agentRunId: string;
	    artifactIds: string[];
	    repaired: boolean;
	    summary?: string;
	
	    static createFrom(source: any = {}) {
	        return new ScriptStageResultDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.stageRunId = source["stageRunId"];
	        this.status = source["status"];
	        this.attempt = source["attempt"];
	        this.agentRunId = source["agentRunId"];
	        this.artifactIds = source["artifactIds"];
	        this.repaired = source["repaired"];
	        this.summary = source["summary"];
	    }
	}
	export class ScriptStructureDTO {
	    scriptVersionId: string;
	    scenes: SceneStructureDTO[];
	    estimatedDurationSeconds: number;
	
	    static createFrom(source: any = {}) {
	        return new ScriptStructureDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.scriptVersionId = source["scriptVersionId"];
	        this.scenes = this.convertValues(source["scenes"], SceneStructureDTO);
	        this.estimatedDurationSeconds = source["estimatedDurationSeconds"];
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
	export class SetDialogueLineLockedRequest {
	    lineId: string;
	    locked: boolean;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new SetDialogueLineLockedRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.lineId = source["lineId"];
	        this.locked = source["locked"];
	        this.revision = source["revision"];
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
	export class SetShotOverridesRequest {
	    versionId: string;
	    overridesJson: string;
	
	    static createFrom(source: any = {}) {
	        return new SetShotOverridesRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.versionId = source["versionId"];
	        this.overridesJson = source["overridesJson"];
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
	
	
	export class SplitChapterRequest {
	    chapterId: string;
	    splitAtOffset: number;
	    secondTitle?: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new SplitChapterRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.chapterId = source["chapterId"];
	        this.splitAtOffset = source["splitAtOffset"];
	        this.secondTitle = source["secondTitle"];
	        this.revision = source["revision"];
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
	export class StoryEntityAliasDTO {
	    id: string;
	    storyEntityId: string;
	    alias: string;
	    sourceChapterId?: string;
	    sourceStart?: number;
	    sourceEnd?: number;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new StoryEntityAliasDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.storyEntityId = source["storyEntityId"];
	        this.alias = source["alias"];
	        this.sourceChapterId = source["sourceChapterId"];
	        this.sourceStart = source["sourceStart"];
	        this.sourceEnd = source["sourceEnd"];
	        this.createdAt = source["createdAt"];
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
	export class StoryEventParticipantDTO {
	    storyEventId: string;
	    storyEntityId: string;
	    role: string;
	    stateBefore?: string;
	    stateAfter?: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new StoryEventParticipantDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.storyEventId = source["storyEventId"];
	        this.storyEntityId = source["storyEntityId"];
	        this.role = source["role"];
	        this.stateBefore = source["stateBefore"];
	        this.stateAfter = source["stateAfter"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class StoryFactConflictDTO {
	    id: string;
	    projectId: string;
	    leftFactType: string;
	    leftFactId: string;
	    rightFactType: string;
	    rightFactId: string;
	    conflictType?: string;
	    status: string;
	    resolution?: string;
	    resolvedBy?: string;
	    createdAt: string;
	    resolvedAt?: string;
	
	    static createFrom(source: any = {}) {
	        return new StoryFactConflictDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.projectId = source["projectId"];
	        this.leftFactType = source["leftFactType"];
	        this.leftFactId = source["leftFactId"];
	        this.rightFactType = source["rightFactType"];
	        this.rightFactId = source["rightFactId"];
	        this.conflictType = source["conflictType"];
	        this.status = source["status"];
	        this.resolution = source["resolution"];
	        this.resolvedBy = source["resolvedBy"];
	        this.createdAt = source["createdAt"];
	        this.resolvedAt = source["resolvedAt"];
	    }
	}
	export class StoryFactSourceDTO {
	    id: string;
	    factType: string;
	    factId: string;
	    chapterId?: string;
	    sourceDocumentVersionId: string;
	    startOffset?: number;
	    endOffset?: number;
	    quoteHash?: string;
	    sourceKind: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new StoryFactSourceDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.factType = source["factType"];
	        this.factId = source["factId"];
	        this.chapterId = source["chapterId"];
	        this.sourceDocumentVersionId = source["sourceDocumentVersionId"];
	        this.startOffset = source["startOffset"];
	        this.endOffset = source["endOffset"];
	        this.quoteHash = source["quoteHash"];
	        this.sourceKind = source["sourceKind"];
	        this.createdAt = source["createdAt"];
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
	export class StorySkeletonVersionDTO {
	    versionId: string;
	    episodeId: string;
	    versionNumber: number;
	    status: string;
	    basedOnVersionId?: string;
	    openingHook: string;
	    coreConflict: string;
	    turningPoints: string;
	    climax: string;
	    endingHook: string;
	    estimatedDurationSeconds: number;
	    selectedEventIds: string[];
	    sourceAgentRunId?: string;
	    createdByType: string;
	    createdById?: string;
	    changeReason?: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new StorySkeletonVersionDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.versionId = source["versionId"];
	        this.episodeId = source["episodeId"];
	        this.versionNumber = source["versionNumber"];
	        this.status = source["status"];
	        this.basedOnVersionId = source["basedOnVersionId"];
	        this.openingHook = source["openingHook"];
	        this.coreConflict = source["coreConflict"];
	        this.turningPoints = source["turningPoints"];
	        this.climax = source["climax"];
	        this.endingHook = source["endingHook"];
	        this.estimatedDurationSeconds = source["estimatedDurationSeconds"];
	        this.selectedEventIds = source["selectedEventIds"];
	        this.sourceAgentRunId = source["sourceAgentRunId"];
	        this.createdByType = source["createdByType"];
	        this.createdById = source["createdById"];
	        this.changeReason = source["changeReason"];
	        this.createdAt = source["createdAt"];
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
	    firstFrameDescription?: string;
	    lastFrameDescription?: string;
	    videoMotionDescription?: string;
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
	        this.firstFrameDescription = source["firstFrameDescription"];
	        this.lastFrameDescription = source["lastFrameDescription"];
	        this.videoMotionDescription = source["videoMotionDescription"];
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
	
	export class SubmitAudioJobRequest {
	    projectId: string;
	    episodeId: string;
	    dialogueLineId: string;
	    providerId: string;
	    model: string;
	    text: string;
	    voice?: string;
	    format?: string;
	    speed?: string;
	    priority?: number;
	
	    static createFrom(source: any = {}) {
	        return new SubmitAudioJobRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.episodeId = source["episodeId"];
	        this.dialogueLineId = source["dialogueLineId"];
	        this.providerId = source["providerId"];
	        this.model = source["model"];
	        this.text = source["text"];
	        this.voice = source["voice"];
	        this.format = source["format"];
	        this.speed = source["speed"];
	        this.priority = source["priority"];
	    }
	}
	export class SubmitExportForReviewRequest {
	    exportId: string;
	    episodeId?: string;
	
	    static createFrom(source: any = {}) {
	        return new SubmitExportForReviewRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.exportId = source["exportId"];
	        this.episodeId = source["episodeId"];
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
	export class SubmitSubtitleTrackForReviewRequest {
	    trackId: string;
	    episodeId?: string;
	
	    static createFrom(source: any = {}) {
	        return new SubmitSubtitleTrackForReviewRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.trackId = source["trackId"];
	        this.episodeId = source["episodeId"];
	    }
	}
	export class SubmitVideoJobRequest {
	    projectId: string;
	    episodeId: string;
	    shotId: string;
	    providerId: string;
	    model: string;
	    prompt: string;
	    seconds?: number;
	    size?: string;
	    references?: string[];
	    referenceMimes?: string[];
	    firstFrame?: string;
	    firstFrameMime?: string;
	    lastFrame?: string;
	    lastFrameMime?: string;
	    priority?: number;
	
	    static createFrom(source: any = {}) {
	        return new SubmitVideoJobRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.episodeId = source["episodeId"];
	        this.shotId = source["shotId"];
	        this.providerId = source["providerId"];
	        this.model = source["model"];
	        this.prompt = source["prompt"];
	        this.seconds = source["seconds"];
	        this.size = source["size"];
	        this.references = source["references"];
	        this.referenceMimes = source["referenceMimes"];
	        this.firstFrame = source["firstFrame"];
	        this.firstFrameMime = source["firstFrameMime"];
	        this.lastFrame = source["lastFrame"];
	        this.lastFrameMime = source["lastFrameMime"];
	        this.priority = source["priority"];
	    }
	}
	export class SubtitleCueDTO {
	    id: string;
	    ordinal: number;
	    startMs: number;
	    endMs: number;
	    text: string;
	    characterEntityId?: string;
	    dialogueLineId?: string;
	    status: string;
	
	    static createFrom(source: any = {}) {
	        return new SubtitleCueDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.ordinal = source["ordinal"];
	        this.startMs = source["startMs"];
	        this.endMs = source["endMs"];
	        this.text = source["text"];
	        this.characterEntityId = source["characterEntityId"];
	        this.dialogueLineId = source["dialogueLineId"];
	        this.status = source["status"];
	    }
	}
	export class SubtitleCueEdit {
	    id?: string;
	    startMs: number;
	    endMs: number;
	    text: string;
	    characterEntityId?: string;
	    dialogueLineId?: string;
	
	    static createFrom(source: any = {}) {
	        return new SubtitleCueEdit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.startMs = source["startMs"];
	        this.endMs = source["endMs"];
	        this.text = source["text"];
	        this.characterEntityId = source["characterEntityId"];
	        this.dialogueLineId = source["dialogueLineId"];
	    }
	}
	export class SubtitleTrackDTO {
	    id: string;
	    episodeId: string;
	    scriptVersionId: string;
	    versionNumber: number;
	    status: string;
	    createdAt: string;
	
	    static createFrom(source: any = {}) {
	        return new SubtitleTrackDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.episodeId = source["episodeId"];
	        this.scriptVersionId = source["scriptVersionId"];
	        this.versionNumber = source["versionNumber"];
	        this.status = source["status"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class SubtitleDraftDTO {
	    track: SubtitleTrackDTO;
	    cues: SubtitleCueDTO[];
	
	    static createFrom(source: any = {}) {
	        return new SubtitleDraftDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.track = this.convertValues(source["track"], SubtitleTrackDTO);
	        this.cues = this.convertValues(source["cues"], SubtitleCueDTO);
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
	
	export class SummarizeMemoryRequest {
	    projectId: string;
	    episodeId?: string;
	    agentKey?: string;
	    level?: number;
	    embed?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SummarizeMemoryRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.episodeId = source["episodeId"];
	        this.agentKey = source["agentKey"];
	        this.level = source["level"];
	        this.embed = source["embed"];
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
	export class TimelineShotDTO {
	    ordinal: number;
	    itemId: string;
	    shotId: string;
	    durationMs: number;
	    mediaVersionId?: string;
	    mediaHash?: string;
	    mediaKind?: string;
	    panelVersionId?: string;
	    hasAudio: boolean;
	    cueCount: number;
	
	    static createFrom(source: any = {}) {
	        return new TimelineShotDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ordinal = source["ordinal"];
	        this.itemId = source["itemId"];
	        this.shotId = source["shotId"];
	        this.durationMs = source["durationMs"];
	        this.mediaVersionId = source["mediaVersionId"];
	        this.mediaHash = source["mediaHash"];
	        this.mediaKind = source["mediaKind"];
	        this.panelVersionId = source["panelVersionId"];
	        this.hasAudio = source["hasAudio"];
	        this.cueCount = source["cueCount"];
	    }
	}
	export class TimelineDTO {
	    episodeId: string;
	    boardVersionId: string;
	    scriptVersionId: string;
	    shots: TimelineShotDTO[];
	    totalDurationMs: number;
	    missingMedia: number;
	    cueCount: number;
	    missingLines: number;
	
	    static createFrom(source: any = {}) {
	        return new TimelineDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.episodeId = source["episodeId"];
	        this.boardVersionId = source["boardVersionId"];
	        this.scriptVersionId = source["scriptVersionId"];
	        this.shots = this.convertValues(source["shots"], TimelineShotDTO);
	        this.totalDurationMs = source["totalDurationMs"];
	        this.missingMedia = source["missingMedia"];
	        this.cueCount = source["cueCount"];
	        this.missingLines = source["missingLines"];
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
	export class TimelineRequest {
	    episodeId: string;
	    boardVersionId?: string;
	
	    static createFrom(source: any = {}) {
	        return new TimelineRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.episodeId = source["episodeId"];
	        this.boardVersionId = source["boardVersionId"];
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
	export class UpdateProjectSettingsRequest {
	    projectId: string;
	    targetPlatform: string;
	    aspectRatio: string;
	    resolution: string;
	    expectedEpisodeCount: number;
	    defaultEpisodeDurationSecs: number;
	    audience: string;
	    contentRating: string;
	    adaptationMode: string;
	    revision: number;
	
	    static createFrom(source: any = {}) {
	        return new UpdateProjectSettingsRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.targetPlatform = source["targetPlatform"];
	        this.aspectRatio = source["aspectRatio"];
	        this.resolution = source["resolution"];
	        this.expectedEpisodeCount = source["expectedEpisodeCount"];
	        this.defaultEpisodeDurationSecs = source["defaultEpisodeDurationSecs"];
	        this.audience = source["audience"];
	        this.contentRating = source["contentRating"];
	        this.adaptationMode = source["adaptationMode"];
	        this.revision = source["revision"];
	    }
	}
	export class UpdateStoryboardItemRequest {
	    itemId: string;
	    expectedRevision: number;
	    shotSize?: string;
	    cameraAngle?: string;
	    cameraMovement?: string;
	    durationSeconds?: number;
	    visualDescription?: string;
	    actionDescription?: string;
	    dialogueAudioSummary?: string;
	    continuityNotes?: string;
	    firstFrameDescription?: string;
	    lastFrameDescription?: string;
	    videoMotionDescription?: string;
	    status?: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateStoryboardItemRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.itemId = source["itemId"];
	        this.expectedRevision = source["expectedRevision"];
	        this.shotSize = source["shotSize"];
	        this.cameraAngle = source["cameraAngle"];
	        this.cameraMovement = source["cameraMovement"];
	        this.durationSeconds = source["durationSeconds"];
	        this.visualDescription = source["visualDescription"];
	        this.actionDescription = source["actionDescription"];
	        this.dialogueAudioSummary = source["dialogueAudioSummary"];
	        this.continuityNotes = source["continuityNotes"];
	        this.firstFrameDescription = source["firstFrameDescription"];
	        this.lastFrameDescription = source["lastFrameDescription"];
	        this.videoMotionDescription = source["videoMotionDescription"];
	        this.status = source["status"];
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
	export class VersionDiffDTO {
	    family: string;
	    fromId: string;
	    toId: string;
	    fields?: FieldChangeDTO[];
	    items?: ItemChangeDTO[];
	    added: number;
	    removed: number;
	    modified: number;
	    unchanged: number;
	
	    static createFrom(source: any = {}) {
	        return new VersionDiffDTO(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.family = source["family"];
	        this.fromId = source["fromId"];
	        this.toId = source["toId"];
	        this.fields = this.convertValues(source["fields"], FieldChangeDTO);
	        this.items = this.convertValues(source["items"], ItemChangeDTO);
	        this.added = source["added"];
	        this.removed = source["removed"];
	        this.modified = source["modified"];
	        this.unchanged = source["unchanged"];
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
	    maxConcurrency: number;
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
	        this.maxConcurrency = source["maxConcurrency"];
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
	export class TextToolCall {
	    key: string;
	    arguments?: number[];
	
	    static createFrom(source: any = {}) {
	        return new TextToolCall(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.arguments = source["arguments"];
	    }
	}
	export class TextResult {
	    content: string;
	    finishReason?: string;
	    model?: string;
	    toolCalls?: TextToolCall[];
	
	    static createFrom(source: any = {}) {
	        return new TextResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.content = source["content"];
	        this.finishReason = source["finishReason"];
	        this.model = source["model"];
	        this.toolCalls = this.convertValues(source["toolCalls"], TextToolCall);
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

