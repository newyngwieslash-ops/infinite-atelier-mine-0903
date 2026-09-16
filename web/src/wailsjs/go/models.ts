export namespace desktop {
	
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

