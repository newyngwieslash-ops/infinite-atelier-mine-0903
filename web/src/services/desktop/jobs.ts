import type { desktop } from "@/wailsjs/go/models";

type WailsBinding = Record<string, unknown>;

type WailsGo = {
    desktop?: {
        JobsBinding?: WailsBinding;
    };
};

type WailsRuntime = {
    EventsOn?: unknown;
};

type DesktopWindow = Window & { go?: WailsGo; runtime?: WailsRuntime };

function getDesktopWindow(): DesktopWindow | null {
    return typeof window === "undefined" ? null : window;
}

/**
 * Job status values, mirroring the Go domain set documented in
 * docs/adr/0004. Kept as a union so the UI must handle every state instead of
 * falling through to a generic label.
 */
export type JobStatus =
    | "queued"
    | "running"
    | "waiting_remote"
    | "downloading"
    | "verifying"
    | "retry_wait"
    | "recovering"
    | "succeeded"
    | "remote_only"
    | "failed"
    | "cancelled"
    | "orphaned";

/** Statuses that end a job's life. */
export const TERMINAL_JOB_STATUSES: JobStatus[] = ["succeeded", "remote_only", "failed", "cancelled", "orphaned"];

export function isTerminalJobStatus(status: string): boolean {
    return (TERMINAL_JOB_STATUSES as string[]).includes(status);
}

/**
 * Returns true when the Wails job bindings are available. The binding exposes
 * queue queries and user actions only; there is deliberately no execute,
 * resolve, or arbitrary fetch method.
 */
export function isDesktopJobBindingsAvailable(): boolean {
    const jobs = getDesktopWindow()?.go?.desktop?.JobsBinding;
    return (
        typeof jobs?.ListJobs === "function" &&
        typeof jobs?.GetJob === "function" &&
        typeof jobs?.SubmitImageJob === "function" &&
        typeof jobs?.CancelJobs === "function" &&
        typeof jobs?.RetryFailedJobs === "function" &&
        typeof jobs?.PauseQueue === "function" &&
        typeof jobs?.ResumeQueue === "function" &&
        typeof jobs?.GetQueueStatus === "function"
    );
}

/**
 * isMediaJobSubmissionAvailable reports whether the two media job submissions
 * are reachable.
 *
 * It is a separate probe from `isDesktopJobBindingsAvailable`, and the
 * separation is deliberate: the required set there is what the Job Center
 * needs, and adding a method to it would make an older build's whole queue
 * surface read as unavailable. A build with ListJobs and no SubmitVideoJob can
 * still show the queue; it simply cannot start a shot's video, and this says so
 * on its own.
 */
export function isMediaJobSubmissionAvailable(): boolean {
    const jobs = getDesktopWindow()?.go?.desktop?.JobsBinding;
    return typeof jobs?.SubmitVideoJob === "function" && typeof jobs?.SubmitAudioJob === "function";
}

let jobsModule: Promise<typeof import("@/wailsjs/go/desktop/JobsBinding")> | undefined;

async function loadJobsBinding() {
    jobsModule ??= import("@/wailsjs/go/desktop/JobsBinding").catch((error: unknown) => {
        jobsModule = undefined;
        throw error;
    });
    return jobsModule;
}

export async function listJobs(request: desktop.ListJobsRequest): Promise<desktop.JobDTO[]> {
    if (!isDesktopJobBindingsAvailable()) return [];
    const { ListJobs } = await loadJobsBinding();
    return ListJobs(request);
}

export async function getJob(id: string): Promise<desktop.JobDTO | null> {
    if (!isDesktopJobBindingsAvailable()) return null;
    const { GetJob } = await loadJobsBinding();
    return GetJob(id);
}

export async function jobAttempts(id: string): Promise<desktop.JobAttemptDTO[]> {
    if (!isDesktopJobBindingsAvailable()) return [];
    const { JobAttempts } = await loadJobsBinding();
    return JobAttempts(id);
}

export async function submitImageJob(request: desktop.SubmitImageJobRequest): Promise<desktop.JobDTO> {
    if (!isDesktopJobBindingsAvailable()) throw new Error("desktop job bindings are unavailable");
    const { SubmitImageJob } = await loadJobsBinding();
    return SubmitImageJob(request);
}

/**
 * submitVideoJob enqueues one shot's video generation.
 *
 * It lives here rather than in `media.ts` because it is a JobsBinding method:
 * that module is the client for MediaBinding, and this module is the one place
 * the webview learns whether the job surface exists. A video job is minutes of
 * provider polling — ADR-0011 section 6's "媒体生成本身由 Job/Provider Service
 * 执行，不让 LLM 阻塞等待大文件" — so the command only puts it on the queue and
 * returns the row a section then polls.
 *
 * The returned DTO is the EXISTING job when an identical request was already
 * submitted, so a double click produces one job rather than two.
 */
export async function submitVideoJob(request: desktop.SubmitVideoJobRequest): Promise<desktop.JobDTO> {
    if (!isDesktopJobBindingsAvailable()) throw new Error("desktop job bindings are unavailable");
    const { SubmitVideoJob } = await loadJobsBinding();
    return SubmitVideoJob(request);
}

/**
 * submitAudioJob enqueues one dialogue line's speech.
 *
 * The line is the job's entity, which is what AC-MEDIA-002's "audio linked to
 * character/line" reads: the job names the line, and the line names the
 * character. The text travels on the request because TTS is billed by the
 * character and the bound on it belongs to the command, not to a runner's
 * memory of a script.
 */
export async function submitAudioJob(request: desktop.SubmitAudioJobRequest): Promise<desktop.JobDTO> {
    if (!isDesktopJobBindingsAvailable()) throw new Error("desktop job bindings are unavailable");
    const { SubmitAudioJob } = await loadJobsBinding();
    return SubmitAudioJob(request);
}

export async function cancelJobs(ids: string[]): Promise<number> {
    if (!isDesktopJobBindingsAvailable()) return 0;
    const { CancelJobs } = await loadJobsBinding();
    return CancelJobs(ids);
}

export async function retryFailedJobs(ids: string[]): Promise<number> {
    if (!isDesktopJobBindingsAvailable()) return 0;
    const { RetryFailedJobs } = await loadJobsBinding();
    return RetryFailedJobs(ids);
}

export async function pauseQueue(): Promise<void> {
    if (!isDesktopJobBindingsAvailable()) return;
    const { PauseQueue } = await loadJobsBinding();
    await PauseQueue();
}

export async function resumeQueue(): Promise<void> {
    if (!isDesktopJobBindingsAvailable()) return;
    const { ResumeQueue } = await loadJobsBinding();
    await ResumeQueue();
}

export async function getQueueStatus(): Promise<desktop.QueueSummaryDTO | null> {
    if (!isDesktopJobBindingsAvailable()) return null;
    const { GetQueueStatus } = await loadJobsBinding();
    return GetQueueStatus();
}

export type JobEventPayload = {
    jobId: string;
    status: string;
    progress: number;
    errorCode?: string;
    updatedAt: string;
};

/**
 * Subscribes to job change events. Returns an unsubscribe function; the caller
 * owns cleanup on unmount.
 */
export function onJobChanged(callback: (payload: JobEventPayload) => void): () => void {
    const runtime = getDesktopWindow()?.runtime as { EventsOn?: unknown } | undefined;
    if (typeof runtime?.EventsOn !== "function") return () => undefined;

    let cancelled = false;
    let cleanup: () => void = () => undefined;

    void import("@/wailsjs/runtime/runtime")
        .then(({ EventsOn }) => {
            if (cancelled) return;
            const unsubscribe = EventsOn("core:event", (event: unknown) => {
                if (typeof event !== "object" || event === null) return;
                const candidate = event as Record<string, unknown>;
                if (candidate.type !== "job.changed") return;
                const payload = candidate.payload as JobEventPayload | undefined;
                if (!payload || typeof payload.jobId !== "string" || typeof payload.status !== "string") return;
                callback(payload);
            });
            cleanup = typeof unsubscribe === "function" ? unsubscribe : () => undefined;
        })
        .catch(() => undefined);

    return () => {
        cancelled = true;
        cleanup();
    };
}

/**
 * Reads one stored job artifact by its content-addressed hash.
 *
 * The hash shape is validated on both sides; there is no path parameter, so
 * this cannot become an arbitrary file reader.
 */
export async function readResultFile(storageKey: string): Promise<desktop.JobResultFileContent | null> {
    if (!isDesktopJobBindingsAvailable()) return null;
    if (!/^[0-9a-f]{64}$/.test(storageKey)) return null;
    const { ReadResultFile } = await loadJobsBinding();
    return ReadResultFile(storageKey);
}
