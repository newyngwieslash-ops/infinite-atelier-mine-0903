import type { desktop } from "@/wailsjs/go/models";
// The two absence answers below are DTOs rather than plain objects, because a
// Wails DTO carries a convertValues method and so cannot be built from an
// object literal. Same reason `drama.ts` imports the module for its
// `getScriptStructure` placeholder.
import { desktop as desktopModels } from "@/wailsjs/go/models";

/**
 * media.ts is the client for the video, audio and timeline sections' binding.
 *
 * # Why it is a separate module from drama.ts
 *
 * The three WP-11 sections read and write a surface the drama module does not
 * wrap: `MediaBinding` composes an episode, drafts and edits its subtitles and
 * writes the exported file, and `JobsBinding` is where a shot's video and a
 * line's speech are submitted. Keeping the client separate means the media
 * sections do not import the drama surface's module — the shape that would make
 * one feature's failure take the other with it — and it is the same reason
 * `memory.ts` is separate from `drama.ts`.
 *
 * # The unavailable case
 *
 * Every COMMAND throws when the core is absent, and the queries answer with an
 * empty result. That is the split `drama.ts` and `memory.ts` both keep and for
 * the same reason: a browser-mode preview should render an empty list rather
 * than an error, while a button a user pressed must say why nothing happened.
 *
 * # The two availability questions, and why they are two
 *
 * `isMediaBindingsAvailable` asks whether the sections can read at all — a
 * build with no timeline service can answer neither "what are the shots" nor
 * "what is the diagnostics". `isMediaExportAvailable` asks the narrower
 * question of whether an export can be composed and written, because those two
 * methods are the ones that touch an engine and a save dialog: a build whose
 * media services are composed but whose engine is missing can still draft,
 * edit and read subtitles, and a single flag for both would hide that a
 * subtitle can be produced on a machine that cannot render a film.
 */

/** Methods the three media sections must have before any of them is usable. */
const REQUIRED_MEDIA_METHODS = ["MediaCapability", "ReadTimeline"] as const;

/** Methods an export needs: composing it, and writing the composed file out. */
const REQUIRED_EXPORT_METHODS = ["RunExport", "SaveExport"] as const;

/**
 * isMediaBindingsAvailable reports whether the media sections can talk to the Go core.
 *
 * It checks a representative set rather than every method, so an older build
 * with the binding but without a newly added method is still available and
 * fails on the specific call — which produces a clearer message than a blanket
 * "unavailable".
 */
export function isMediaBindingsAvailable(): boolean {
    const media = getDesktopWindow()?.go?.desktop?.MediaBinding;
    if (!media) return false;
    return REQUIRED_MEDIA_METHODS.every((method) => typeof media[method] === "function");
}

/** isMediaExportAvailable reports whether composing and saving an export are reachable. */
export function isMediaExportAvailable(): boolean {
    const media = getDesktopWindow()?.go?.desktop?.MediaBinding;
    if (!media) return false;
    return REQUIRED_EXPORT_METHODS.every((method) => typeof media[method] === "function");
}

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

let mediaModule: Promise<typeof import("@/wailsjs/go/desktop/MediaBinding")> | undefined;

async function loadMediaBinding() {
    mediaModule ??= import("@/wailsjs/go/desktop/MediaBinding").catch((error: unknown) => {
        mediaModule = undefined;
        throw error;
    });
    return mediaModule;
}

/**
 * mediaCapability reports what this machine can do.
 *
 * It is a READ rather than a failure, which is ARCHITECTURE's own ruling: "媒体
 * 引擎不可用：禁用相关能力并显示诊断". So a machine with no engine renders a
 * disabled export control and the diagnostic beside it, instead of an error
 * that arrives only when somebody presses the button.
 *
 * # What the absent-core answer deliberately omits
 *
 * The empty answer reports both capabilities false and NO diagnostic. A
 * diagnostic string is a statement about the machine — "ffmpeg is not on this
 * path" — and this interface never asked this machine anything; inventing one
 * would be a claim about why, which is worse than saying nothing and letting
 * the section state that it needs the core.
 */
export async function mediaCapability(): Promise<desktop.MediaCapabilityDTO> {
    if (!isMediaBindingsAvailable()) {
        return desktopModels.MediaCapabilityDTO.createFrom({ exportAvailable: false, saveAvailable: false });
    }
    const binding = await loadMediaBinding();
    return binding.MediaCapability();
}

/**
 * readTimeline returns an episode's ordered shots with the media approved for each.
 *
 * The empty answer names the episode it was asked about and carries no shots,
 * so a caller's `episodeId` field still describes the question rather than
 * being replaced by a blank one.
 */
export async function readTimeline(request: desktop.TimelineRequest): Promise<desktop.TimelineDTO> {
    if (!isMediaBindingsAvailable()) {
        return desktopModels.TimelineDTO.createFrom({
            episodeId: request.episodeId,
            shots: [],
            totalDurationMs: 0,
            missingMedia: 0,
            cueCount: 0,
            missingLines: 0,
        });
    }
    const binding = await loadMediaBinding();
    return binding.ReadTimeline(request as never);
}

/** listSubtitleTracks returns an episode's subtitle tracks, newest version first. */
export async function listSubtitleTracks(episodeId: string): Promise<desktop.SubtitleTrackDTO[]> {
    if (!isMediaBindingsAvailable()) return [];
    const binding = await loadMediaBinding();
    return binding.ListSubtitleTracks(episodeId);
}

/** listSubtitleCues returns one track's cues in order. */
export async function listSubtitleCues(trackId: string): Promise<desktop.SubtitleCueDTO[]> {
    if (!isMediaBindingsAvailable()) return [];
    const binding = await loadMediaBinding();
    return binding.ListSubtitleCues(trackId);
}

/**
 * missingSubtitleLines returns the spoken lines a track has no cue for.
 *
 * It is a read of its own rather than a field of the track, because the core
 * computes it by joining the track's cues against the script's spoken lines:
 * the answer belongs to the PAIR, and a section that asked only for the track
 * would show a completeness claim nobody made.
 */
export async function missingSubtitleLines(trackId: string): Promise<desktop.MissingLineDTO[]> {
    if (!isMediaBindingsAvailable()) return [];
    const binding = await loadMediaBinding();
    return binding.MissingSubtitleLines(trackId);
}

/** listExports returns an episode's exports, newest version first. */
export async function listExports(episodeId: string): Promise<desktop.ExportRecordDTO[]> {
    if (!isMediaExportAvailable()) return [];
    const binding = await loadMediaBinding();
    return binding.ListExports(episodeId);
}

/**
 * draftSubtitles builds a starting subtitle track from a script version's spoken lines.
 *
 * The script version is required by the core rather than defaulted here,
 * because which version a track renders is the one thing a subtitle cannot
 * recover from its own rows: a draft against the wrong version places every cue
 * against another draft's timings.
 */
export async function draftSubtitles(request: desktop.DraftSubtitlesRequest): Promise<desktop.SubtitleDraftDTO> {
    if (!isMediaBindingsAvailable()) throw unavailableError();
    const binding = await loadMediaBinding();
    return binding.DraftSubtitles(request as never);
}

/**
 * editSubtitleCues replaces a track's cues.
 *
 * It REPLACES rather than patches, which is the core's own shape ("subtitle
 * editable", AC-MEDIA-002): the editor sends the whole list, so a cue that
 * disappears from the list is a cue the user removed. An id-less edit is a cue
 * the service mints one for, which is how a user adds a line the draft missed.
 */
export async function editSubtitleCues(trackId: string, edits: desktop.SubtitleCueEdit[]): Promise<desktop.SubtitleCueDTO[]> {
    if (!isMediaBindingsAvailable()) throw unavailableError();
    const binding = await loadMediaBinding();
    return binding.EditSubtitleCues(trackId, edits as never);
}

/**
 * exportSubtitles renders a track as SRT or VTT and returns the DOCUMENT.
 *
 * It returns text rather than a stored file, and that is the core's own
 * decision: the section shows the document so a user can read what they are
 * about to save. A method that wrote on the first press would make "check my
 * subtitles" impossible, and this client does not paper over it by pretending
 * the text is a file.
 */
export async function exportSubtitles(request: desktop.ExportSubtitlesRequest): Promise<string> {
    if (!isMediaBindingsAvailable()) throw unavailableError();
    const binding = await loadMediaBinding();
    return binding.ExportSubtitles(request as never);
}

/** approveSubtitleTrack puts one track in force for its episode. */
export async function approveSubtitleTrack(request: desktop.ApproveSubtitleTrackRequest): Promise<desktop.SubtitleTrackDTO> {
    if (!isMediaBindingsAvailable()) throw unavailableError();
    const binding = await loadMediaBinding();
    return binding.ApproveSubtitleTrack(request as never);
}

/**
 * submitSubtitleTrackForReview moves a draft track to the state an approval requires.
 *
 * It is a separate step from approving rather than a convenience wrapper around
 * it, because the core refuses an approval on a track that is not
 * `under_review`: the review is the user saying "look at this", and the approval
 * is the user saying "I looked". A section that offered only the approve button
 * would offer a control whose every press failed with a conflict about a state
 * the user could not reach — which is how this method came to exist.
 */
export async function submitSubtitleTrackForReview(request: desktop.SubmitSubtitleTrackForReviewRequest): Promise<desktop.SubtitleTrackDTO> {
    if (!isMediaBindingsAvailable()) throw unavailableError();
    const binding = await loadMediaBinding();
    return binding.SubmitSubtitleTrackForReview(request as never);
}

/**
 * submitExportForReview moves a draft export to the state an approval requires.
 *
 * The same two-step as the subtitle track's, for the same reason: composing a
 * film produces a draft, and putting it in force is a person's decision taken
 * after reading what the Final Ruleset and the supervisor reported.
 */
export async function submitExportForReview(request: desktop.SubmitExportForReviewRequest): Promise<desktop.ExportRecordDTO> {
    if (!isMediaBindingsAvailable()) throw unavailableError();
    const binding = await loadMediaBinding();
    return binding.SubmitExportForReview(request as never);
}

/**
 * runExport composes an episode and records what it was made from.
 *
 * The record carries `manifestJson`, which is where AC-MEDIA-003's "manifest
 * traceability" lives: the row names the recipe and the references it composed,
 * so a reader can tell what an export was made from without a second call.
 */
export async function runExport(request: desktop.RunExportRequest): Promise<desktop.ExportRecordDTO> {
    if (!isMediaExportAvailable()) throw unavailableError();
    const binding = await loadMediaBinding();
    return binding.RunExport(request as never);
}

/** approveExport puts one export in force, superseding the episode's previous one. */
export async function approveExport(request: desktop.ApproveExportRequest): Promise<desktop.ExportRecordDTO> {
    if (!isMediaExportAvailable()) throw unavailableError();
    const binding = await loadMediaBinding();
    return binding.ApproveExport(request as never);
}

/**
 * saveExport writes a stored export to a location the user picks.
 *
 * The request names a STORAGE KEY and a suggested filename; it cannot name a
 * destination. The path is whatever the save dialog returned, which is the user
 * pointing at it — SECURITY section 11's "用户选择导出目录时只写明确目标". A
 * `destination` parameter would be the opposite: a frontend that had been
 * compromised could write anywhere.
 *
 * `written: false` means the user cancelled the dialog. It is a returned value
 * and NOT an error, so a caller must not report it as a failure.
 */
export async function saveExport(request: desktop.SaveExportRequest): Promise<desktop.SaveFileResultDTO> {
    if (!isMediaExportAvailable()) throw unavailableError();
    const binding = await loadMediaBinding();
    return binding.SaveExport(request as never);
}
