import type { desktop } from "@/wailsjs/go/models";
import { desktop as desktopModels } from "@/wailsjs/go/models";

/**
 * voices.ts is the client for FR-080's two V1 audio clauses WP-27 built:
 * 多角色声线映射 and 音效建议.
 *
 * # Why it is separate from media.ts
 *
 * `media.ts` wraps the video, audio and timeline sections' reads — the timeline,
 * the subtitles, the export. This module wraps two things those do not: which
 * voice renders which character, and which sound a shot's own intent suggests.
 * Both are about a LINE or a SHOT rather than about the film, and the audio and
 * timeline sections reach them at different times, so folding them into the media
 * client would make one section's import carry the other's surface.
 *
 * # The unavailable case
 *
 * The same split the other clients keep: QUERIES answer empty, COMMANDS throw. A
 * browser-mode preview renders an empty casting list rather than an error, while
 * pressing "assign" says why nothing happened.
 */

/** Methods this module needs before either feature is usable. */
const REQUIRED_VOICE_METHODS = ["ListCharacterVoices", "ResolveCharacterVoice", "SuggestShotEffects"] as const;

type DesktopWindow = Window & {
    go?: {
        desktop?: Record<string, Record<string, (...args: never[]) => unknown>>;
    };
};

function getDesktopWindow(): DesktopWindow | undefined {
    if (typeof window === "undefined") return undefined;
    return window as DesktopWindow;
}

function unavailableError(): Error {
    return new Error("The desktop core is not available in this window.");
}

let voiceModule: Promise<typeof import("@/wailsjs/go/desktop/MediaBinding")> | undefined;

async function loadMediaBinding() {
    voiceModule ??= import("@/wailsjs/go/desktop/MediaBinding").catch((error: unknown) => {
        voiceModule = undefined;
        throw error;
    });
    return voiceModule;
}

/**
 * isVoiceSurfaceAvailable reports whether the casting and suggestion reads can be made.
 *
 * It probes the READS rather than the commands, because a panel's first act is to
 * list: a build that could not list would show an empty casting area, and a check
 * that passed on the commands alone would let it claim to be available while
 * showing nothing.
 */
export function isVoiceSurfaceAvailable(): boolean {
    const media = getDesktopWindow()?.go?.desktop?.MediaBinding;
    if (!media) return false;
    return REQUIRED_VOICE_METHODS.every((method) => typeof media[method] === "function");
}

/**
 * listCharacterVoices returns a project's casting decisions, with character names.
 *
 * Empty when the core is absent, which is the query rule: the panel shows no cast
 * rather than a failure a user cannot act on.
 */
export async function listCharacterVoices(projectId: string): Promise<desktop.CharacterVoiceDTO[]> {
    if (!isVoiceSurfaceAvailable()) {
        return [];
    }
    const binding = await loadMediaBinding();
    return binding.ListCharacterVoices(projectId);
}

/**
 * assignCharacterVoice casts a character's voice.
 *
 * `expectedRevision` is the revision the caller last saw, or zero for a first
 * assignment. A stale value is REFUSED by the core as a conflict rather than
 * overwriting a change somebody else made, so a panel that saved from an old tab
 * gets an error it can act on instead of silently winning.
 */
export async function assignCharacterVoice(request: desktop.AssignVoiceRequest): Promise<desktop.CharacterVoiceDTO> {
    if (!isVoiceSurfaceAvailable()) {
        throw unavailableError();
    }
    const binding = await loadMediaBinding();
    return binding.AssignCharacterVoice(request);
}

/**
 * clearCharacterVoice removes a character's casting decision.
 *
 * It reports whether there was one, so a panel can tell "cleared" from "there was
 * nothing there" without a second read.
 */
export async function clearCharacterVoice(request: desktop.ClearVoiceRequest): Promise<boolean> {
    if (!isVoiceSurfaceAvailable()) {
        throw unavailableError();
    }
    const binding = await loadMediaBinding();
    return binding.ClearCharacterVoice(request);
}

/**
 * resolveCharacterVoice answers what a character's line should be rendered with.
 *
 * The answer carries WHERE the voice came from — the character's own cast, the
 * project's setting, or nothing — and the audio section shows it, because the
 * three are indistinguishable by sound alone. A user who hears the project
 * default on a character they cast needs to see that the cast did not decide.
 *
 * The project's own configuration travels in the request rather than being read by
 * the core, because it is a preference the desktop layer holds, not a fact about
 * the story.
 */
export async function resolveCharacterVoice(request: desktop.ResolveVoiceRequest): Promise<desktop.VoiceChoiceDTO> {
    if (!isVoiceSurfaceAvailable()) {
        // The degraded answer uses the caller's own configuration, which is what the
        // audio section did before casting existed. It is NOT an error: a build
        // without the drama store can still render speech with the voice the user
        // chose in settings.
        const voice = (request.projectVoice ?? "").trim();
        return desktopModels.VoiceChoiceDTO.createFrom({
            providerConfigId: voice ? request.projectProvider ?? "" : "",
            model: voice ? request.projectModel ?? "" : "",
            voice,
            source: voice ? "project" : "unset",
            isSet: voice !== "",
        });
    }
    const binding = await loadMediaBinding();
    return binding.ResolveCharacterVoice(request);
}

/**
 * suggestShotEffects proposes at most one effect per shot, with the term it matched.
 *
 * The shots come from the caller because their `audioIntent` lives in the script,
 * which the media surface does not read. Passing what the section has already
 * listed is what keeps this from being a second reader of the script with a second
 * answer to "what does this shot say".
 */
export async function suggestShotEffects(shots: desktop.ShotEffectInputDTO[]): Promise<desktop.EffectSuggestionDTO[]> {
    if (!isVoiceSurfaceAvailable()) {
        return [];
    }
    const binding = await loadMediaBinding();
    return binding.SuggestShotEffects(shots);
}

/**
 * effectVocabulary returns the sounds this build can suggest.
 *
 * A panel shows it so a user can see what the suggestion recognises instead of
 * guessing at phrasing — which is the whole reason the suggestion names its matched
 * term rather than a score.
 */
export async function effectVocabulary(): Promise<string[]> {
    if (!isVoiceSurfaceAvailable()) {
        return [];
    }
    const binding = await loadMediaBinding();
    return binding.EffectVocabulary();
}
