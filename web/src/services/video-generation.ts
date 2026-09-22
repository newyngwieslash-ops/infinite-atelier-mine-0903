import { requestVideoGeneration as legacyRequestVideoGeneration, storeGeneratedVideo, type VideoGenerationResult } from "@/services/api/video";
import type { ReferenceImage } from "@/types/image";
import type { AiConfig } from "@/stores/use-config-store";
import { isSecureProviderMode } from "@/services/desktop/providers";

export type { VideoGenerationResult };

/**
 * Video generation entry point used by the canvas.
 *
 * # Why this module exists at all
 *
 * It is the ONE place that decides which transport a canvas video generation
 * uses, so the canvas code keeps its existing shape and there is a single
 * answer to "where did this request go". It mirrors `image-generation.ts`,
 * which the image path already uses.
 *
 * # The secure branch fails closed, and why that is not a stub
 *
 * `image-generation.ts` routes a secure-mode request through the Go job
 * manager. Video has no equivalent route for a FREE CANVAS NODE, and the
 * absence is structural rather than a gap in wiring — it is recorded here
 * because a reader will otherwise expect symmetry with images:
 *
 *  - `SubmitVideoJob` (`internal/desktop/media_jobs.go`) requires `projectId`,
 *    `episodeId` and `shotId`, and rejects the request when any is blank. Its
 *    entity is a storyboard SHOT, because FR-080's "同一 Shot 可保留多个视频版本"
 *    makes the shot the unit.
 *  - A free canvas node is none of those. `CanvasNodeType` has no shot type,
 *    a free-canvas node carries no `entityType`/`entityId`, and the canvas page
 *    addresses a project — not an episode — so there is no honest value to send
 *    for `episodeId` or `shotId`. Inventing one would record a video job against
 *    a shot that does not exist, which is the fabrication the repository
 *    refuses (AGENTS section 14: a blocking decision is listed, never invented).
 *  - The Go media adapters are also mocks reachable only through a `mock_media`
 *    provider kind that the configuration UI never offers and the application
 *    layer refuses to persist.
 *
 * So a secure-mode request stops here with a message naming where the
 * capability actually lives, and there is deliberately NO fallback to the
 * legacy browser call: falling back would put the API key back in the webview,
 * which is exactly what PRD section 18's "前端或普通备份可获取完整 API Key"
 * forbids.
 *
 * # The honest total, including the part that is not this module's fault
 *
 * The studio's video section (`components/studio/video-view.tsx`) IS the
 * structurally correct route: it submits against a real shot through
 * `SubmitVideoJob`, so it can fill every required field. But **no real video
 * adapter exists in this build** — `Registry.VideoPortFor` resolves an adapter
 * only for `mock_media`, which no configuration can carry, and returns
 * "unsupported" for `openai_compatible` and `gemini_compatible`. A studio
 * submission therefore reaches the queue and fails at provider resolution.
 *
 * The consequence, stated rather than glossed: SECURE DESKTOP MODE HAS NO
 * WORKING VIDEO GENERATION TODAY, on the canvas or in the studio. The canvas
 * refusal arrives earlier and with a clearer message than the studio's job
 * failure, which is all this module can honestly improve. Closing it needs a
 * real video adapter, which is adapter work rather than a routing change.
 * `docs/implementation/STATUS.md` section 0m records the same finding.
 *
 * `isSecureProviderMode()` is false only when the Wails provider bindings are
 * absent, which in practice means the browser development server, where there
 * is no Go core to route to and the legacy direct call is the only transport.
 */
export async function requestVideoGeneration(config: AiConfig, prompt: string, references: ReferenceImage[] = [], options?: { signal?: AbortSignal }): Promise<VideoGenerationResult> {
    if (isSecureProviderMode()) {
        throw new Error("Video generation is not available in the desktop build. The Go video job is keyed to a storyboard shot and no real video adapter exists yet; the canvas cannot submit one, and the Drama Studio's video section submits correctly but fails at the provider. Track a real video adapter as the outstanding work.");
    }
    return await legacyRequestVideoGeneration(config, prompt, references, options);
}

/**
 * Stores a generated video and returns the canvas's uploaded-file shape.
 *
 * It is re-exported here so a caller imports the whole video capability from
 * one module and cannot reach the legacy storage helper while believing it is
 * on the secure path: in secure mode the request above never returns a result
 * to store.
 */
export { storeGeneratedVideo };
