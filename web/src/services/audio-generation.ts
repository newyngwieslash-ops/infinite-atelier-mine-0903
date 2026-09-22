import { requestAudioGeneration as legacyRequestAudioGeneration, storeGeneratedAudio } from "@/services/api/audio";
import type { AiConfig } from "@/stores/use-config-store";
import { isSecureProviderMode } from "@/services/desktop/providers";

/**
 * Audio (TTS) generation entry point used by the canvas.
 *
 * It is the ONE place that decides which transport a canvas audio generation
 * uses, so the canvas code keeps its existing shape. It mirrors
 * `image-generation.ts` and `video-generation.ts`.
 *
 * # The secure branch fails closed, and why that is not a stub
 *
 * `SubmitAudioJob` (`internal/desktop/media_jobs.go`) requires `projectId`,
 * `episodeId` and `dialogueLineId`, and rejects the request when any is blank:
 * its entity is a DIALOGUE LINE, because AC-MEDIA-002's "audio linked to
 * character/line" reads as the job naming a line and the line naming the
 * character.
 *
 * A free canvas node is none of those. The canvas page addresses a project,
 * not an episode, and a free-canvas node is not a dialogue line, so there is no
 * honest value for `episodeId` or `dialogueLineId`. Sending a made-up one would
 * record speech against a line that does not exist — a fabricated fact in the
 * database rather than a failed request — and AGENTS section 14 says such a
 * decision is listed, not invented.
 *
 * There is deliberately no fallback to the legacy browser call on failure: that
 * route carries the API key in the webview, which PRD section 18 forbids.
 *
 * # The honest total, including the part that is not this module's fault
 *
 * The studio's audio section (`components/studio/audio-view.tsx`) IS the
 * structurally correct route: it submits against a real dialogue line through
 * `SubmitAudioJob`, so it can fill every required field. But **no real audio
 * adapter exists in this build** — `Registry.AudioPortFor` resolves an adapter
 * only for `mock_media`, which no configuration can carry, and returns
 * "unsupported" for `openai_compatible` and `gemini_compatible`. A studio
 * submission therefore reaches the queue and fails at provider resolution.
 *
 * The consequence, stated rather than glossed: SECURE DESKTOP MODE HAS NO
 * WORKING SPEECH GENERATION TODAY, on the canvas or in the studio. The canvas
 * refusal arrives earlier and with a clearer message than the studio's job
 * failure, which is all this module can honestly improve. Closing it needs a
 * real TTS adapter, which is adapter work rather than a routing change.
 * `docs/implementation/STATUS.md` section 0m records the same finding.
 */
export async function requestAudioGeneration(config: AiConfig, prompt: string, options?: { signal?: AbortSignal }): Promise<Blob> {
    if (isSecureProviderMode()) {
        throw new Error("Speech generation is not available in the desktop build. The Go audio job is keyed to a dialogue line and no real audio adapter exists yet; the canvas cannot submit one, and the Drama Studio's audio section submits correctly but fails at the provider. Track a real TTS adapter as the outstanding work.");
    }
    return await legacyRequestAudioGeneration(config, prompt, options);
}

/**
 * Stores a generated audio blob and returns the canvas's uploaded-file shape.
 *
 * It is re-exported here for the same reason `storeGeneratedVideo` is: a caller
 * imports the whole audio capability from one module, and in secure mode the
 * request above never returns a blob to store.
 */
export { storeGeneratedAudio };
