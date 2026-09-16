import i18n from "@/i18n";

import { isSecureProviderMode } from "./providers";
import { onJobChanged, submitImageJob, type JobEventPayload, type JobStatus } from "./jobs";
import { toProviderId } from "./provider-id";

/**
 * Reports whether this build still exposes legacy browser-direct provider
 * calls.
 *
 * WP-03 migrated image generation to the Go job manager but left the video and
 * audio calls on the direct browser path until the media work package provides
 * real adapters. The authoritative list of those files lives in the security
 * scanner (scripts/security-scan.mjs), which refuses NEW direct calls; this
 * flag only drives the user-facing migration notice, so it must not grow a
 * second copy of the list that could drift from the scanner's.
 */
export function hasLegacyDirectCalls(): boolean {
    // Still compiled in for compatibility. Removing them is the media work
    // package's job, so this reports the honest current state.
    return true;
}

export class SecureImageUnavailableError extends Error {
    constructor(message: string) {
        super(message);
        this.name = "SecureImageUnavailableError";
    }
}

export type SecureImageRequest = {
    projectId?: string;
    entityType?: string;
    /**
     * Identifies the canvas node this job belongs to. It participates in the
     * idempotency key, so two nodes with the same prompt are two jobs, and a
     * node can regenerate after a terminal failure.
     */
    entityId?: string;
    providerId?: string;
    channelId?: string;
    model: string;
    prompt: string;
    count?: number;
    size?: string;
    quality?: string;
    references?: string[];
    referenceMIMEs?: string[];
    mask?: string;
    maskMIME?: string;
    signal?: AbortSignal;
};

export type SecureImageResult = {
    jobId: string;
    files: { storageKey: string; mime: string; size: number }[];
};

/**
 * Submits an image job through the Go job manager and waits for it to finish.
 *
 * There is deliberately no fallback to the legacy browser path on failure:
 * a secure-mode error stays visible rather than silently degrading to a route
 * that would put the API key back in the webview.
 */
export async function submitSecureImageJob(request: SecureImageRequest, onProgress?: (status: JobStatus) => void): Promise<SecureImageResult> {
    if (!isSecureProviderMode()) {
        throw new SecureImageUnavailableError(i18n.t("secureJobs.unavailable"));
    }
    const providerId = request.providerId || (request.channelId ? toProviderId(request.channelId) : "");
    if (!providerId) {
        throw new SecureImageUnavailableError(i18n.t("secureJobs.noProvider"));
    }

    const job = await submitImageJob({
        projectId: request.projectId || "",
        entityType: request.entityType || "canvas_node",
        entityId: request.entityId || "",
        providerId,
        model: request.model,
        prompt: request.prompt,
        count: request.count ?? 1,
        size: request.size || "",
        quality: request.quality || "",
        references: request.references || [],
        referenceMIMEs: request.referenceMIMEs || [],
        mask: request.mask || "",
        maskMIME: request.maskMIME || "",
    } as never);

    const finalStatus = await waitForJob(job.id, request.signal, onProgress);
    if (finalStatus.status !== "succeeded") {
        throw new SecureImageUnavailableError(
            i18n.t("secureJobs.failed", {
                status: finalStatus.status,
                code: finalStatus.errorCode || i18n.t("secureJobs.errorUnavailable"),
            }),
        );
    }
    return {
        jobId: job.id,
        files: (finalStatus.files || []).map((file) => ({ storageKey: file.storageKey, mime: file.mime, size: file.size })),
    };
}

type TrackedJob = { status: JobStatus; errorCode?: string; files?: { storageKey: string; mime: string; size: number }[] };

/**
 * Waits for a job's terminal event. The Go side publishes terminal transitions
 * unconditionally, so this cannot miss a completion because of throttling.
 */
/** How long a canvas generation waits before giving up on the job. */
const JOB_WAIT_TIMEOUT_MS = 15 * 60 * 1000;

async function waitForJob(jobId: string, signal?: AbortSignal, onProgress?: (status: JobStatus) => void): Promise<TrackedJob> {
    const { getJob, isTerminalJobStatus: isTerminal } = await import("./jobs");
    return await new Promise<TrackedJob>((resolve, reject) => {
        let settled = false;
        let unsubscribe: () => void = () => undefined;
        // A paused queue or an unsettleable job must not hang the canvas
        // promise forever; the job keeps running and stays visible in the Job
        // Center.
        const timeout = setTimeout(() => finish(new Error("job wait timed out")), JOB_WAIT_TIMEOUT_MS);

        const finish = (error?: Error, result?: TrackedJob) => {
            if (settled) return;
            settled = true;
            clearTimeout(timeout);
            unsubscribe();
            signal?.removeEventListener("abort", abortHandler);
            if (error) reject(error);
            else resolve(result as TrackedJob);
        };

        const abortHandler = () => {
            // The job is persisted in Go: abandoning the local wait would leave
            // it running (and billing) while the canvas reports "cancelled".
            // Cancelling by id stops it at the next safe point.
            void import("./jobs")
                .then(({ cancelJobs }) => cancelJobs([jobId]))
                .catch(() => undefined);
            finish(new DOMException("Aborted", "AbortError"));
        };

        const handleEvent = (payload: JobEventPayload) => {
            if (payload.jobId !== jobId) return;
            onProgress?.(payload.status as JobStatus);
            if (!isTerminal(payload.status)) return;
            void getJob(jobId)
                .then((record) => {
                    finish(undefined, {
                        status: payload.status as JobStatus,
                        errorCode: payload.errorCode,
                        files: record?.resultFiles?.map((file) => ({ storageKey: file.storageKey, mime: file.mime, size: file.size })) || [],
                    });
                })
                .catch(() => finish(undefined, { status: payload.status as JobStatus, errorCode: payload.errorCode }));
        };

        unsubscribe = onJobChanged(handleEvent);

        if (signal) {
            if (signal.aborted) {
                abortHandler();
                return;
            }
            signal.addEventListener("abort", abortHandler, { once: true });
        }

        // A job may already be finished (submission returned an existing job for
        // an idempotent replay), so check the current state once.
        void getJob(jobId)
            .then((record) => {
                if (!record || settled) return;
                if (isTerminal(record.status)) {
                    finish(undefined, {
                        status: record.status as JobStatus,
                        errorCode: record.errorCode,
                        files: record.resultFiles?.map((file) => ({ storageKey: file.storageKey, mime: file.mime, size: file.size })) || [],
                    });
                }
            })
            .catch(() => undefined);
    });
}
