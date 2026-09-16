import { requestEdit as legacyRequestEdit, requestGeneration as legacyRequestGeneration } from "@/services/api/image";
import type { ReferenceImage } from "@/types/image";
import { buildJobScope } from "@/services/desktop/job-scope";
import { submitSecureImageJob } from "@/services/desktop/image-jobs";
import { readResultFile } from "@/services/desktop/jobs";
import { isSecureProviderMode } from "@/services/desktop/providers";
import { decodeModelSelection, channelIdForModel } from "@/services/desktop/model-selection";
import { imageToDataUrl } from "@/services/image-storage";
import type { AiConfig } from "@/stores/use-config-store";

/**
 * Mirrors the legacy service option shape, extended with the identity the job
 * needs.
 *
 * `jobScope` carries the project and canvas node so the Go job's idempotency
 * key identifies the work rather than just the prompt text: two nodes with the
 * same prompt are two jobs, and a node can regenerate after a terminal failure.
 */
type RequestOptions = {
    signal?: AbortSignal;
    jobScope?: {
        projectId?: string;
        entityId?: string;
        entityType?: string;
        /**
         * Distinguishes the images of one batch. Without it, every request in a
         * count>1 batch would carry the same idempotency key and all but the
         * first would be treated as a replay of a single job.
         */
        batchIndex?: number;
    };
};

/**
 * Image generation entry point used by the canvas.
 *
 * In secure desktop mode the request goes through the Go job manager: the key
 * stays in Go, the job is persisted, and a failure is reported rather than
 * silently retried through the browser. In browser development mode the legacy
 * direct call remains, because there is no Go core to route to.
 *
 * The routing lives here, not at each call site, so the canvas code keeps its
 * existing shape and there is exactly one place that decides which path runs.
 */
export type GeneratedImage = { id: string; dataUrl: string };

export async function requestGeneration(config: AiConfig, prompt: string, options?: RequestOptions): Promise<GeneratedImage[]> {
    if (!isSecureProviderMode()) {
        return legacyRequestGeneration(config, prompt, options);
    }
    return await runSecureImageJob(config, prompt, [], undefined, options);
}

export async function requestEdit(config: AiConfig, prompt: string, references: ReferenceImage[], mask?: ReferenceImage, options?: RequestOptions): Promise<GeneratedImage[]> {
    if (!isSecureProviderMode()) {
        return legacyRequestEdit(config, prompt, references, mask, options);
    }
    return await runSecureImageJob(config, prompt, references, mask, options);
}

/**
 * Submits the job and converts the stored artifacts back into the data URLs the
 * canvas renders. Each file is read through the content-addressed reader, so
 * the webview receives bytes only for hashes the job actually produced.
 */
async function runSecureImageJob(config: AiConfig, prompt: string, references: ReferenceImage[], mask: ReferenceImage | undefined, options?: RequestOptions): Promise<GeneratedImage[]> {
    const selection = config.model || config.imageModel;
    const decoded = decodeModelSelection(selection);
    const channelId = channelIdForModel(config, selection);
    const channel = config.channels.find((candidate) => candidate.id === channelId);

    const referenceData: string[] = [];
    const referenceMIMEs: string[] = [];
    for (const reference of references) {
        const dataUrl = reference.dataUrl || (await imageToDataUrl(reference));
        referenceData.push(dataUrl);
        referenceMIMEs.push(reference.type || "image/png");
    }
    let maskData = "";
    let maskMIME = "";
    if (mask) {
        maskData = mask.dataUrl || (await imageToDataUrl(mask));
        maskMIME = mask.type || "image/png";
    }

    const scope = buildJobScope(options?.jobScope);
    const result = await submitSecureImageJob({
        projectId: scope.projectId,
        entityType: scope.entityType,
        entityId: scope.entityId,
        channelId: channelId || channel?.id,
        model: decoded.model,
        prompt,
        count: 1,
        size: config.size,
        quality: config.quality,
        references: referenceData,
        referenceMIMEs,
        mask: maskData,
        maskMIME,
        signal: options?.signal,
    });

    const images: GeneratedImage[] = [];
    for (const file of result.files) {
        const content = await readResultFile(file.storageKey);
        if (!content?.dataUrl) continue;
        // The identifier is the content hash, so the same bytes always map to
        // the same id and a duplicate submission cannot create a second asset.
        images.push({ id: file.storageKey, dataUrl: content.dataUrl });
    }
    if (images.length === 0) {
        throw new Error("secure image job produced no readable result");
    }
    return images;
}
