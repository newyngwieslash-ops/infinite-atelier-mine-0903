import { useCallback, useEffect, useMemo, useState } from "react";
import { Alert, App, Button, Collapse, Empty, Input, InputNumber, Popconfirm, Select, Space, Switch, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Download, Music, Play, RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";

import {
    approveExport,
    approveSubtitleTrack,
    draftSubtitles,
    editSubtitleCues,
    exportManifestDocument,
    exportScript,
    exportShotList,
    exportSubtitles,
    isDocumentExportAvailable,
    isMediaBindingsAvailable,
    isMediaExportAvailable,
    listExports,
    listSubtitleCues,
    listSubtitleTracks,
    mediaCapability,
    missingSubtitleLines,
    readTimeline,
    runExport,
    saveDocument,
    saveExport,
    submitExportForReview,
    submitSubtitleTrackForReview,
} from "@/services/desktop/media";
import { ensureStoryboard, getScriptStructure, isDramaBindingsAvailable, listScriptVersions, listStoryboardVersions } from "@/services/desktop/drama";
import { importBackgroundMusic, isMusicImportAvailable } from "@/services/desktop/music";
import { isVoiceSurfaceAvailable, suggestShotEffects } from "@/services/desktop/voices";
import { submitAudioJob, submitEffectJob } from "@/services/desktop/jobs";
import { channelIdForModel, decodeModelSelection } from "@/services/desktop/model-selection";
import { useEffectiveConfig } from "@/stores/use-config-store";
import type { desktop } from "@/wailsjs/go/models";

/**
 * TimelineSection is WP-11's assembly and export surface, and it is the largest of the three
 * because it holds the parts AC-MEDIA-003 names:
 *
 *	ordered Shots；audio/subtitle；replace clip；export；Final Supervisor；output playable；
 *	manifest traceability。
 *
 * This file is the UI half of the first two, the export, and the last one. "replace clip" and the
 * Final Supervisor are reached elsewhere — the former through a storyboard row's own image
 * approval, the latter through the production stage machine — and this section offers no button for
 * either, because the binding it talks to has no command for them.
 *
 * FIVE THINGS IT KEEPS TRUE, and each is a rule the rest of the studio keeps too:
 *
 *  1. AN EMPTY STATE AND A FAILED READ ARE DIFFERENT. Every list starts as `null` — "not read" —
 *     and a failed read leaves it there while showing the failure, so a subtitle area that could
 *     not be read never renders as "no tracks".
 *  2. THE SUBTITLE DOCUMENT IS SHOWN BEFORE IT IS SAVED. `ExportSubtitles` returns the TEXT rather
 *     than writing a file — the core's own choice — so the document goes into a read-only area and
 *     the writing is a separate act.
 *  3. THE MANIFEST IS VISIBLE. AC-MEDIA-003's "manifest traceability" is a requirement, and
 *     `ExportRecordDTO.manifestJson` travels with the row, so each export expands to its own
 *     manifest rather than needing a second call.
 *  4. THE APPROVAL IS TWO STEPS, AND BOTH ARE OFFERED. An approval requires a row in `under_review`,
 *     which is a step a person takes: this section offers "submit for review" and then "approve", in
 *     that order, because the core refuses an approval on a draft. The first version of this file
 *     rendered both approve controls DISABLED with an explanation that no command could reach the
 *     review state — a real finding, and the fix was in the Go core rather than here: nobody can
 *     approve a film whose review step does not exist. `ExportRepository.MarkUnderReview` had existed
 *     all along and was on no port, and the subtitle side had nothing; both are now service commands
 *     with binding methods, so the controls work.
 *  5. NOTHING IS SENT THAT THE CORE CANNOT ATTRIBUTE. A subtitle draft needs a script version id,
 *     and the id comes from a PICKER over the episode's own versions — `ListScriptVersions` was the
 *     missing binding, not the read: the repository method existed and was dispatched the whole time,
 *     and two sections worked around its absence by asking the user to type an identifier. The
 *     picker pre-selects the APPROVED version when there is one, because a subtitle track is part of
 *     an episode in force and drafting against a draft script is how a caption ends up disagreeing
 *     with the picture. The version's status is shown beside every option, so the choice a user makes
 *     is the one they can see.
 *  6. A DOCUMENT IS READ BEFORE IT IS WRITTEN. ROADMAP item 11's script, shot list and manifest are
 *     the same shape as the subtitle document: the core returns the TEXT and `SaveDocument` is the
 *     separate act that writes it, so each of the three is previewed here and saved from the preview.
 *  7. A DOCUMENT NAMES THE VERSION IT RENDERS. `ExportScript` and `ExportShotList` each take an
 *     optional `versionId` and the core honours it, so the script and shot-list controls carry a
 *     picker over the episode's own history: without one a user could export the version in force and
 *     nothing else, which was the PARTIAL this closes. Each pre-selects the APPROVED version when
 *     there is one, because that is the version in force and the one "my script" means. Changing a
 *     picker CLEARS that document's preview, because text rendered from the version that WAS selected
 *     is a preview that silently belongs to a different version the moment the picker moves — the
 *     defect this task exists to remove. The manifest has no picker: it names no version. It is the
 *     newest export's own document (`ExportManifestDocument` takes only the episode), so a control
 *     there would be one with nothing behind it.
 */
export type TimelineSectionProps = {
    /**
     * No `projectId`, unlike its two sibling sections, and that is a fact about this section's
     * commands rather than an oversight: every read and write here is EPISODE-scoped. `ReadTimeline`,
     * `ListSubtitleTracks`, `ListExports`, `DraftSubtitles`, `RunExport` and `SaveExport` each name
     * an episode or a track, and none has a project parameter — so a `projectId` prop would be one
     * this component could only ignore. The shell passes it to the other two because their job
     * filters need it; taking it here to look uniform would be a prop nothing reads.
     */
    episodes: desktop.EpisodeDTO[];
    activeEpisodeId: string;
    onSelectEpisode: (episodeId: string) => void;
    onChanged: () => void;
};

/** The export qualities the domain accepts (`domain/media.QualityPreview|QualityFinal`). */
const QUALITIES = ["preview", "final"] as const;

/**
 * The subtitle modes the domain accepts.
 *
 * The empty string is `SubtitleNone` — the schema's own spelling for "no subtitles in this export"
 * — and it is the default because an export that burned in subtitles nobody asked for would be a
 * picture the user did not choose.
 */
const SUBTITLE_MODES = [
    { value: "", labelKey: "modeNone" },
    { value: "sidecar", labelKey: "modeSidecar" },
    { value: "burn", labelKey: "modeBurn" },
] as const;

/** The two formats `domain/media.SubtitleFormats` documents. */
const FORMATS = ["srt", "vtt"] as const;

/** The two formats `screenplay.Formats` documents for a script document. */
const SCRIPT_FORMATS = ["txt", "fountain"] as const;

/**
 * The two formats `shotlist.Formats` documents for a shot list.
 *
 * CSV is listed first because it is the one a schedule is read in: a spreadsheet is what a shot list
 * is for, and the text form is the same rows for a person reading them.
 */
const SHOT_LIST_FORMATS = ["csv", "txt"] as const;

/**
 * One rendered document as this section holds it: exactly what a save needs, and nothing else.
 *
 * The read-only preview is an `Input.TextArea`, so a document is never inserted as markup.
 */
type RenderedDocument = {
    text: string;
    suggestedName: string;
    /**
     * The EPISODE and VERSION the text was rendered from, which is what makes a stale preview
     * detectable. Both are recorded at render time rather than read back off the current state later,
     * because the two differ exactly when it matters: a render that is in flight while the user moves
     * the picker — or switches episode — answers with the OLD subject's text, and a preview compared
     * against the CURRENT selection is the only way to notice.
     *
     * The episode is carried for the reason the version is. Switching episode does not immediately
     * change a picker either: the new version history takes a round trip to arrive, so for that window
     * both sides still hold the previous episode's id and a version-only comparison would call the
     * preview current. The document on screen would then belong to another episode while the save
     * control beside it wrote it out under this one's name.
     *
     * `versionId` is empty for a document whose request names no version, which today is the manifest.
     */
    episodeId: string;
    versionId: string;
};

/**
 * The three fields every version picker in this section reads.
 *
 * `ScriptVersionDTO` and `StoryboardVersionDTO` both carry them, and a picker only ever needs the id
 * to send, the number to show and the status to label the option with. Naming the shape once is what
 * lets the label and the empty-state notice be built in one place for both families rather than
 * twice in near-identical copies.
 */
type VersionRow = {
    id: string;
    versionNumber: number;
    status: string;
};

/** The three documents this section can render, which is also the busy key of each export. */
type DocumentKind = "script" | "shotList" | "manifest";

/** One cue as the editor holds it. Times stay in the milliseconds the schema stores. */
type CueDraft = {
    id: string;
    startMs: number;
    endMs: number;
    text: string;
};

export function TimelineSection({ episodes, activeEpisodeId, onSelectEpisode, onChanged }: TimelineSectionProps) {
    const { t } = useTranslation();
    const { message } = App.useApp();
    // The effect's generation reads the same audio configuration the audio section submits with, so a
    // provider and voice chosen once serve both surfaces.
    const config = useEffectiveConfig();

    const [timeline, setTimeline] = useState<desktop.TimelineDTO | null>(null);
    const [capability, setCapability] = useState<desktop.MediaCapabilityDTO | null>(null);
    const [tracks, setTracks] = useState<desktop.SubtitleTrackDTO[] | null>(null);
    const [missing, setMissing] = useState<desktop.MissingLineDTO[]>([]);
    const [exports, setExports] = useState<desktop.ExportRecordDTO[] | null>(null);
    const [selectedTrackId, setSelectedTrackId] = useState("");
    /**
     * The episode's script versions, and the one a subtitle draft will render from.
     *
     * `scriptVersions` is `null` until the read has answered, so "could not read" stays
     * distinguishable from "this episode has no script version yet" — the same distinction the
     * timeline, track and export lists keep in this file.
     *
     * ONE LIST, TWO CHOICES. The list is read once and feeds both this section's script-version
     * controls — the subtitle draft's and the document export's below — because it is one question
     * ("which script versions does this episode have") and a second read would be a second answer that
     * could disagree. The two SELECTIONS are separate fields, because the two controls ask different
     * questions of that list: `scriptVersionId` is the script a subtitle DRAFT renders from, and
     * `scriptDocumentVersionId` is the script an exported FILE contains. One shared field would make
     * moving either picker silently move the other and discard the other's preview, which is the class
     * of surprise this section's version handling exists to remove.
     */
    const [scriptVersions, setScriptVersions] = useState<desktop.ScriptVersionDTO[] | null>(null);
    const [scriptVersionId, setScriptVersionId] = useState("");
    const [format, setFormat] = useState<string>("srt");
    const [document, setDocument] = useState("");
    const [quality, setQuality] = useState<string>("preview");
    const [width, setWidth] = useState<number | null>(null);
    const [height, setHeight] = useState<number | null>(null);
    const [subtitleMode, setSubtitleMode] = useState<string>("");
    const [drafts, setDrafts] = useState<CueDraft[]>([]);
    const [loading, setLoading] = useState(false);
    /**
     * The effect each shot's own `audio_intent` suggests, keyed by shot id.
     *
     * It is a MAP rather than a column on the row because the suggestion is a PROJECTION: it is a pure
     * function of the script, it is not stored, and writing it into the timeline's rows would make it
     * look like a fact about the board. A shot with no match is absent from the map, which renders as
     * nothing rather than as "no effect" — the two are different claims.
     */
    const [effectSuggestions, setEffectSuggestions] = useState<Record<string, desktop.EffectSuggestionDTO>>({});
    /**
     * The background-music import: its own upload state, and the last file's outcome.
     *
     * The outcome is kept rather than toasted alone, so a user can see which track was imported and how
     * large it was after the message that announced it has gone.
     */
    const [importingMusic, setImportingMusic] = useState(false);
    const [importedMusic, setImportedMusic] = useState<{ name: string; bytes: number } | null>(null);
    /** generatingEffect names the shot whose effect job is being submitted, so one row spins and not all. */
    const [generatingEffect, setGeneratingEffect] = useState("");
    const [busy, setBusy] = useState("");
    const [error, setError] = useState("");
    const [notice, setNotice] = useState("");

    /**
     * The document area's own state, kept beside the subtitle document's.
     *
     * `document` above is the SUBTITLE track's rendered file and these are the episode's script, shot
     * list and manifest. They are separate fields rather than one "current document" because the three
     * are different artifacts: a user reads the shot list while the script is still on screen, and one
     * shared slot would make the second export erase the first.
     */
    const [scriptFormat, setScriptFormat] = useState<string>("txt");
    const [shotListFormat, setShotListFormat] = useState<string>("csv");
    const [includeShots, setIncludeShots] = useState(false);
    const [documents, setDocuments] = useState<Record<DocumentKind, RenderedDocument | null>>({ script: null, shotList: null, manifest: null });
    /**
     * The two document VERSION PICKERS' selections: the script a script document contains and the
     * board a shot list contains.
     *
     * Both default by the same rule (see the effect below) and both are fed by the reads in `reload`:
     * `scriptVersions` above is the shared list, and `shotListVersions` is the board's own history,
     * which needs the extra `EnsureStoryboard` step because `ListStoryboardVersions` takes a BOARD
     * rather than an episode. That is the pair `StoryboardTableSection` calls, in the same order, and
     * reusing it is what keeps the two sections describing the same board.
     *
     * `shotListVersions` is `null` until its read has answered, for the reason the script list is:
     * a failed read must not render as "this episode has no board", a claim about its content drawn
     * from a call that never returned.
     */
    const [scriptDocumentVersionId, setScriptDocumentVersionId] = useState("");
    const [shotListVersions, setShotListVersions] = useState<VersionRow[] | null>(null);
    const [shotListVersionId, setShotListVersionId] = useState("");
    const activeEpisode = useMemo(() => episodes.find((episode) => episode.id === activeEpisodeId) || null, [episodes, activeEpisodeId]);

    const bindingsAvailable = isMediaBindingsAvailable();
    const exportAvailable = isMediaExportAvailable();
    /**
     * The documents are ROADMAP item 11's other half, and a build can carry the media binding without
     * them. The probe is separate from `exportAvailable` for that reason, and the controls below are
     * disabled rather than left to fail on a press — the same ruling the export button follows.
     */
    const documentExportAvailable = isDocumentExportAvailable();
    /**
     * The script version read goes through the DRAMA binding, which is a surface of its own.
     *
     * The question is separate from `bindingsAvailable` because the answers differ: a build with both
     * surfaces is the normal case, and a build with the media surface alone still reads the timeline,
     * drafts and exports. In that build the version picker has nothing to fill from, and the section
     * says WHICH read is missing rather than reporting "no script version" — a claim about the
     * episode's content drawn from an absent binding.
     */
    const scriptBindingsAvailable = isDramaBindingsAvailable();

    /**
     * The two selections are cleared when the EPISODE changes, because the ids they hold name rows of
     * the history that is about to be replaced. Clearing them here rather than waiting for the read
     * makes the picker empty — `undefined` on a Select — instead of holding a value that belongs to
     * another episode for as long as the round trip takes; `documentIsStale` covers the preview in the
     * same window, and the two together are what stop an id crossing episodes.
     *
     * The SUBTITLE picker's own field is cleared by the defaulting effect below, which is enough there
     * because nothing is rendered from it until a draft is asked for.
     */
    useEffect(() => {
        setScriptDocumentVersionId("");
        setShotListVersionId("");
    }, [activeEpisodeId]);

    /**
     * reload reads the timeline, the machine's capability, the tracks and the exports.
     *
     * Each failure is recorded once and the unread lists are set back to `null`, which is what makes
     * "could not read" distinguishable from "nothing there": `null` renders as the failure, `[]` as
     * the empty state.
     */
    const reload = useCallback(async () => {
        if (!activeEpisodeId) {
            setTimeline(null);
            setTracks(null);
            setExports(null);
            setScriptVersions(null);
            setShotListVersions(null);
            setMissing([]);
            setDrafts([]);
            return;
        }
        setLoading(true);
        setError("");
        try {
            setTimeline(await readTimeline({ episodeId: activeEpisodeId }));
            setCapability(await mediaCapability());
            setTracks(await listSubtitleTracks(activeEpisodeId));
            setExports(await listExports(activeEpisodeId));
        } catch (failure) {
            setError(failure instanceof Error ? failure.message : t("studio.timeline.loadFailed"));
            setTimeline(null);
            setTracks(null);
            setExports(null);
        }
        // The version history is read OUTSIDE the block above, and on its own binding's probe. Both
        // halves of that matter: this section's other four reads are media reads that a media-only
        // build can answer, so a failure from a different surface must not null them — a user would
        // lose a working timeline to a picker that could not load. `null` is what renders as "could
        // not read", which is the distinction the picker's empty state depends on.
        if (!scriptBindingsAvailable) {
            setScriptVersions(null);
            setShotListVersions(null);
        } else {
            try {
                setScriptVersions(await listScriptVersions(activeEpisodeId));
            } catch (failure) {
                setScriptVersions(null);
                // The first failure on screen wins: the media block above has already reported the
                // more consequential problem if there was one, and a second message would replace it
                // with a detail about a picker.
                setError((current) => current || (failure instanceof Error ? failure.message : t("studio.timeline.scriptVersionsUnread")));
            }
            // The board history is a SECOND read with a step in front of it, and it is wrapped on its
            // own so a board that cannot be ensured still leaves the script picker filled: the two
            // document exports are independent, and one failing read must not empty the other's list.
            try {
                const board = await ensureStoryboard(activeEpisodeId);
                setShotListVersions(await listStoryboardVersions(board.id));
            } catch (failure) {
                setShotListVersions(null);
                setError((current) => current || (failure instanceof Error ? failure.message : t("studio.timeline.shotListVersionsUnread")));
            }
        }
        setLoading(false);
    }, [activeEpisodeId, scriptBindingsAvailable, t]);

    useEffect(() => {
        void reload();
    }, [reload]);

    /**
     * The script version the subtitle draft form starts on, resolved once per list of versions.
     *
     * APPROVED first, and the newest otherwise. The two rules are different answers to "which script
     * does this caption describe": an approved version is the one in force, so a caption drafted from it
     * describes the picture the export will render, while a draft version is the only script there is
     * on an episode nobody has approved yet — where refusing to pre-select would leave a form the
     * user has to fill before it can do anything.
     *
     * A user's own choice is kept for as long as it names a version in the list, so a reload does not
     * overwrite what they picked; a version that has gone (or an episode switch) is replaced rather
     * than left pointing at a row the list no longer carries.
     */
    useEffect(() => {
        if (scriptVersions === null) return;
        if (scriptVersionId && scriptVersions.some((version) => version.id === scriptVersionId)) return;
        const preferred = scriptVersions.find((version) => version.status === "approved") ?? scriptVersions[0];
        setScriptVersionId(preferred?.id ?? "");
    }, [scriptVersions, scriptVersionId]);

    /**
     * The two DOCUMENT pickers default by the same rule, out of the same two lists.
     *
     * Written out rather than folded into a helper because each is one line and a helper would have to
     * take the setter, the current value and the list — a signature longer than the rule it carries.
     * The rule itself is the subtitle picker's above and is stated there: approved first, newest
     * otherwise, a user's own choice kept while it names a version the list still holds. An approved
     * board or script is the version in force, so a document rendered from the pre-selected row is the
     * one a user means by "my script" and "my shot list"; the newest is the fallback for an episode
     * whose history has no approval in it yet.
     */
    useEffect(() => {
        if (scriptVersions === null) return;
        if (scriptDocumentVersionId && scriptVersions.some((version) => version.id === scriptDocumentVersionId)) return;
        const preferred = scriptVersions.find((version) => version.status === "approved") ?? scriptVersions[0];
        setScriptDocumentVersionId(preferred?.id ?? "");
    }, [scriptVersions, scriptDocumentVersionId]);

    useEffect(() => {
        if (shotListVersions === null) return;
        if (shotListVersionId && shotListVersions.some((version) => version.id === shotListVersionId)) return;
        const preferred = shotListVersions.find((version) => version.status === "approved") ?? shotListVersions[0];
        setShotListVersionId(preferred?.id ?? "");
    }, [shotListVersions, shotListVersionId]);

    /**
     * loadTrack reads one track's cues and the spoken lines it does not cover.
     *
     * The missing-line read is a read of its OWN rather than a field of the track, because the core
     * computes it by joining the track's cues against its script version's spoken lines: the answer
     * belongs to the pair.
     */
    const loadTrack = useCallback(
        async (trackId: string) => {
            if (!trackId) {
                setMissing([]);
                setDrafts([]);
                return;
            }
            try {
                // The cue read is exactly what the editor table holds, so it is NOT stored twice:
                // `drafts` is the single copy the table renders and the save sends.
                const [foundCues, foundMissing] = await Promise.all([listSubtitleCues(trackId), missingSubtitleLines(trackId)]);
                setMissing(foundMissing);
                setDrafts(foundCues.map((cue) => ({ id: cue.id, startMs: cue.startMs, endMs: cue.endMs, text: cue.text })));
            } catch (failure) {
                setError(failure instanceof Error ? failure.message : t("studio.timeline.loadFailed"));
                setMissing([]);
                setDrafts([]);
            }
        },
        [t],
    );

    // The first track is selected once the list arrives, so a user does not have to pick one to see
    // the cues of the only track there is.
    useEffect(() => {
        if (tracks === null) return;
        const keep = selectedTrackId && tracks.some((track) => track.id === selectedTrackId) ? selectedTrackId : tracks[0]?.id || "";
        if (keep !== selectedTrackId) setSelectedTrackId(keep);
    }, [tracks, selectedTrackId]);

    useEffect(() => {
        void loadTrack(selectedTrackId);
    }, [selectedTrackId, loadTrack]);

    const selectedTrack = useMemo(() => (tracks ?? []).find((track) => track.id === selectedTrackId) || null, [tracks, selectedTrackId]);

    /**
     * trackInReview reports whether the selected track is in the state an approval requires.
     *
     * The two conditions are the core's own: `ApproveTrack` updates `WHERE status =
     * 'under_review'`, and `MarkTrackUnderReview` updates `WHERE status = 'draft'`. So the buttons
     * enable exactly when the core would accept them, which is why the submit button is offered for
     * a draft and the approve button for a reviewed track.
     */
    const trackInReview = selectedTrack?.status === "under_review";
    const trackDraft = selectedTrack?.status === "draft";

    /** draft asks the core for a starting track from a script version. */
    const draft = async () => {
        if (!activeEpisode) return;
        // The core requires BOTH ids and refuses a draft with either empty. The script version comes
        // from the picker above, which reads the episode's own history through `ListScriptVersions`
        // and pre-selects the approved version; this guard is what remains of the typed field, kept
        // because a picker with no versions in it leaves the id empty and the core would refuse the
        // request rather than explain why.
        if (scriptVersionId.trim() === "") {
            message.error(t("studio.timeline.scriptVersionRequired"));
            return;
        }
        setBusy("draft");
        try {
            const result = await draftSubtitles({ episodeId: activeEpisode.id, scriptVersionId: scriptVersionId.trim() });
            setNotice(t("studio.timeline.drafted", { version: result.track.versionNumber, cues: result.cues.length }));
            setTracks(await listSubtitleTracks(activeEpisode.id));
            // The new draft is selected whether or not it is the first row: a user who just made one
            // is looking at it, not at whatever was selected before.
            setSelectedTrackId(result.track.id);
            onChanged();
        } catch (failure) {
            message.error(failure instanceof Error ? failure.message : t("studio.timeline.draftFailed"));
        } finally {
            setBusy("");
        }
    };

    /**
     * saveCues writes the edited table back.
     *
     * The whole list is sent because the core's command REPLACES a track's cues: a row removed from
     * this table is a cue the user removed, and a row added is one they wrote. The core renumbers
     * positions from the array order and refuses an overlap or a non-positive duration, so this
     * pre-checks the two rules a person can see — and lets the core be the authority on the rest.
     */
    const saveCues = async () => {
        if (!selectedTrackId) return;
        const problem = firstCueProblem(drafts);
        if (problem !== "") {
            message.error(t(`studio.timeline.cueProblem.${problem}`));
            return;
        }
        setBusy("cues");
        try {
            const saved = await editSubtitleCues(selectedTrackId, drafts);
            // The SAVED rows become the editor's state, because the core renumbers positions and may
            // have assigned ids to cues the user added; keeping the pre-save list would show ordinals
            // the database does not have.
            setDrafts(saved.map((cue) => ({ id: cue.id, startMs: cue.startMs, endMs: cue.endMs, text: cue.text })));
            setMissing(await missingSubtitleLines(selectedTrackId));
            message.success(t("studio.timeline.cuesSaved"));
            onChanged();
        } catch (failure) {
            message.error(failure instanceof Error ? failure.message : t("studio.timeline.cuesSaveFailed"));
        } finally {
            setBusy("");
        }
    };

    /** render asks the core for the track's document in one format. */
    const render = async () => {
        if (!selectedTrackId) return;
        setBusy("render");
        try {
            setDocument(await exportSubtitles({ trackId: selectedTrackId, format }));
        } catch (failure) {
            message.error(failure instanceof Error ? failure.message : t("studio.timeline.subtitleExportFailed"));
            setDocument("");
        } finally {
            setBusy("");
        }
    };

    /**
     * submitTrack moves the selected track to review, which is what an approval requires.
     *
     * The user presses this after editing the cues, and it is the act the core calls
     * `under_review`: "I have looked at this and it may go in force". Approving without it is
     * refused, so the two buttons are a sequence rather than a choice.
     */
    const submitTrack = async () => {
        if (!selectedTrackId || !activeEpisode) return;
        setBusy("submit-track");
        try {
            const submitted = await submitSubtitleTrackForReview({ trackId: selectedTrackId, episodeId: activeEpisode.id });
            setNotice(t("studio.timeline.trackSubmitted", { version: submitted.versionNumber }));
            setTracks(await listSubtitleTracks(activeEpisode.id));
            onChanged();
        } catch (failure) {
            message.error(failure instanceof Error ? failure.message : t("studio.timeline.submitFailed"));
        } finally {
            setBusy("");
        }
    };

    /** approveTrack approves the selected track, which the core accepts only from review. */
    const approveTrack = async () => {
        if (!selectedTrackId || !activeEpisode) return;
        setBusy("approve-track");
        try {
            const approved = await approveSubtitleTrack({ trackId: selectedTrackId, episodeId: activeEpisode.id });
            setNotice(t("studio.timeline.trackApproved", { version: approved.versionNumber }));
            setTracks(await listSubtitleTracks(activeEpisode.id));
            onChanged();
        } catch (failure) {
            message.error(failure instanceof Error ? failure.message : t("studio.timeline.approveFailed"));
        } finally {
            setBusy("");
        }
    };

    /**
     * compose runs one export.
     *
     * Zero is the schema's "unset" for a frame dimension, so a blank field is sent as 0 and the
     * service then derives the size from the quality — which is its own rule rather than a default
     * invented here.
     */
    const compose = async () => {
        if (!activeEpisode) return;
        setBusy("export");
        try {
            const record = await runExport({
                episodeId: activeEpisode.id,
                subtitleTrackId: selectedTrackId,
                quality,
                width: width ?? 0,
                height: height ?? 0,
                subtitleMode,
            } as never);
            setNotice(t("studio.timeline.exported", { version: record.versionNumber }));
            setExports(await listExports(activeEpisode.id));
            onChanged();
        } catch (failure) {
            message.error(failure instanceof Error ? failure.message : t("studio.timeline.exportFailed"));
        } finally {
            setBusy("");
        }
    };

    /**
     * writeToDisk opens the save dialog and reports what happened.
     *
     * The storage key is the export's `outputFileHash`: the store is content-addressed and its keys
     * ARE the sha-256 of the bytes, so the hash on the row is the key `SaveExport` accepts. A row
     * with no hash has no stored file, which is said rather than sent as a request the core would
     * refuse for the key's shape.
     *
     * `written: false` is the user having cancelled the dialog. The core returns that as a VALUE
     * rather than as an error, and this handler keeps the distinction: a cancellation is an ordinary
     * act, reported as one.
     */
    const writeToDisk = async (record: desktop.ExportRecordDTO) => {
        const key = record.outputFileHash ?? "";
        if (key === "") {
            message.warning(t("studio.timeline.noOutputFile"));
            return;
        }
        setBusy(`save-${record.id}`);
        try {
            const result = await saveExport({ storageKey: key, suggestedName: `episode-v${record.versionNumber}.mp4` });
            setNotice(result.written ? t("studio.timeline.savedTo", { path: result.path ?? "" }) : t("studio.timeline.saveCancelled"));
        } catch (failure) {
            message.error(failure instanceof Error ? failure.message : t("studio.timeline.saveFailed"));
        } finally {
            setBusy("");
        }
    };

    /**
     * renderDocument asks the core for one of the episode's three documents.
     *
     * The three arrive through one handler because the ACT is the same — render, hold the text, let a
     * person read it — and only the request differs. Each keeps its own slot in `documents`, so a user
     * can compare a shot list against the script they just rendered.
     *
     * A failed render CLEARS that document rather than leaving the previous one on screen: text that
     * came from an earlier, successful call would be read as this call's answer.
     *
     * The version is CAPTURED before the call rather than read after it. A render is a round trip, and
     * the picker can move while it is in flight: reading the state on the way back would stamp the
     * answer with the version the user just chose rather than the one that was actually rendered, which
     * is precisely the mismatch `RenderedDocument.versionId` exists to catch.
     */
    const renderDocument = async (kind: DocumentKind) => {
        if (!activeEpisode) return;
        // The episode and version are read ONCE and used for both the request and the stamp, so the
        // text a user reads and the subject recorded beside it are the same fact by construction
        // rather than by two reads that could straddle a picker change.
        const versionId = documentVersionId(kind);
        const episodeId = activeEpisode.id;
        setBusy(`document-${kind}`);
        try {
            const rendered = await renderByKind(kind, episodeId, { scriptFormat, shotListFormat, includeShots, versionId });
            setDocuments((current) => ({ ...current, [kind]: { text: rendered.text, suggestedName: rendered.suggestedName, episodeId, versionId } }));
        } catch (failure) {
            message.error(failure instanceof Error ? failure.message : t(`studio.timeline.documentFailed.${kind}`));
            setDocuments((current) => ({ ...current, [kind]: null }));
        } finally {
            setBusy("");
        }
    };

    /**
     * documentVersionId is the version one document's picker currently names.
     *
     * The manifest has no picker and answers the empty string, which is what its request carries:
     * `ExportManifestDocument` names the episode alone. Returning the picker's value rather than a copy
     * held beside it is what keeps "what is selected" and "what a render would send" one fact.
     */
    const documentVersionId = (kind: DocumentKind): string => {
        switch (kind) {
            case "script":
                return scriptDocumentVersionId;
            case "shotList":
                return shotListVersionId;
            case "manifest":
                // Empty because the manifest's request HAS no version field — not because one is
                // unselected. `documentIsStale` therefore compares `""` against `""` and a manifest
                // preview is never stale, which is the truth about a document that names no version.
                return "";
        }
    };

    /**
     * documentIsStale reports whether a held document belongs to a subject the section has left.
     *
     * The comparison is made when the preview renders rather than in an effect that clears the slot,
     * and that is the whole point: a render that was IN FLIGHT when the picker moved lands afterwards,
     * and an effect watching only the picker would have already run and would not fire again — leaving
     * the old version's text on screen under the new version's control. Deriving it here catches that
     * case, and also the harmless one of a user changing their mind: putting the picker back on the
     * version the text came from makes the same text valid again, and it comes back rather than being
     * lost to a slot that was cleared.
     *
     * BOTH the episode and the version are compared. The episode is not redundant: a switch does not
     * move a picker until the new history arrives, so a version-only check reads the previous episode's
     * preview as current for exactly as long as the read takes.
     */
    const documentIsStale = (kind: DocumentKind): boolean => {
        const rendered = documents[kind];
        if (!rendered) return false;
        return rendered.episodeId !== activeEpisodeId || rendered.versionId !== documentVersionId(kind);
    };

    /**
     * saveRenderedDocument writes one rendered document to where the user points.
     *
     * The text travels BACK rather than being re-rendered, which is the core's own reasoning: a second
     * render could pick up a version approved in between, so what a user read would not be what they
     * saved. `suggestedName` is the core's own suggestion — a hint to a human, not a destination — and
     * the dialog is still the only thing that decides where the file goes.
     *
     * A STALE document is refused here as well as hidden, because this is the handler that would
     * otherwise do the damage: the text it sends is the text on screen, so writing a preview whose
     * picker has moved would put a version on disk that the user did not choose. The check is a guard
     * rather than the mechanism — the control is not rendered for a stale document — and it is here
     * because a guard at the write is the one that cannot be bypassed by a later render change.
     *
     * `written: false` is the user having cancelled the dialog, and this handler reports it the way
     * `writeToDisk` does: as an ordinary act, not a failure.
     */
    const saveRenderedDocument = async (kind: DocumentKind) => {
        const rendered = documents[kind];
        if (!rendered) return;
        if (documentIsStale(kind)) {
            message.warning(
                rendered.episodeId !== activeEpisodeId
                    ? t("studio.timeline.documentEpisodeChanged")
                    : t("studio.timeline.documentVersionChanged", { version: documentVersionLabel(kind, rendered.versionId) }),
            );
            return;
        }
        setBusy(`document-save-${kind}`);
        try {
            const result = await saveDocument({ text: rendered.text, suggestedName: rendered.suggestedName });
            setNotice(result.written ? t("studio.timeline.savedTo", { path: result.path ?? "" }) : t("studio.timeline.saveCancelled"));
        } catch (failure) {
            message.error(failure instanceof Error ? failure.message : t("studio.timeline.documentSaveFailed"));
        } finally {
            setBusy("");
        }
    };

    /** submitOne moves one export to review, which is what its approval requires. */
    const submitOne = async (record: desktop.ExportRecordDTO) => {
        setBusy(`submit-${record.id}`);
        try {
            const submitted = await submitExportForReview({ exportId: record.id, episodeId: record.episodeId });
            setNotice(t("studio.timeline.exportSubmitted", { version: submitted.versionNumber }));
            setExports(await listExports(activeEpisodeId));
            onChanged();
        } catch (failure) {
            message.error(failure instanceof Error ? failure.message : t("studio.timeline.submitFailed"));
        } finally {
            setBusy("");
        }
    };

    /** approveOne approves one export. */
    const approveOne = async (record: desktop.ExportRecordDTO) => {
        setBusy(`approve-${record.id}`);
        try {
            const approved = await approveExport({ exportId: record.id, episodeId: record.episodeId });
            setNotice(t("studio.timeline.exportApproved", { version: approved.versionNumber }));
            setExports(await listExports(activeEpisodeId));
            onChanged();
        } catch (failure) {
            message.error(failure instanceof Error ? failure.message : t("studio.timeline.approveFailed"));
        } finally {
            setBusy("");
        }
    };

    /** updateDraft replaces one cue row's fields. */
    const updateDraft = (index: number, patch: Partial<CueDraft>) => {
        setDrafts((current) => current.map((cue, position) => (position === index ? { ...cue, ...patch } : cue)));
    };

    /**
     * documentPreview renders one rendered document with its save control, or nothing.
     *
     * It is a render helper rather than a component because it reads four pieces of this section's own
     * state, and lifting them into props would be a component boundary drawn around a text area. The
     * area is read-only for the same reason the subtitle document's is — the core returns the DOCUMENT
     * rather than writing it — and the save control appears only beside a document, because there is
     * nothing to write until one has been rendered.
     *
     * A STALE document is shown rather than hidden, with no save control and a notice saying which
     * version it came from. Hiding it would be the simpler code and the worse answer: a user who
     * pressed "render", then moved the picker, then looked down would see an empty space and could not
     * tell whether the render had failed, was still running, or had produced something they must not
     * use. What is on screen is real text from a real version, so it stays, labelled, and the only
     * thing taken away is the write — which is the act that would put the wrong version on disk.
     */
    const documentPreview = (kind: DocumentKind, rows: number) => {
        const rendered = documents[kind];
        if (!rendered) return null;
        const stale = documentIsStale(kind);
        return (
            <div className="mt-3">
                <Space wrap className="mb-2">
                    {stale ? (
                        // WHY the preview went out of date, said in the terms the user moved in. The two
                        // causes are named separately because they are different sentences: a version the
                        // picker left still has a number worth quoting, while a document belonging to the
                        // previous episode has no meaningful version label HERE — its id would resolve
                        // against a history the section no longer holds, and naming it would be a claim
                        // about this episode's versions drawn from another episode's row.
                        <Typography.Text className="text-xs" data-testid={`studio-timeline-document-stale-${kind}`}>
                            {rendered.episodeId !== activeEpisodeId
                                ? t("studio.timeline.documentEpisodeChanged")
                                : t("studio.timeline.documentVersionChanged", { version: documentVersionLabel(kind, rendered.versionId) })}
                        </Typography.Text>
                    ) : (
                        <>
                            <Button
                                size="small"
                                icon={<Download className="size-3" />}
                                loading={busy === `document-save-${kind}`}
                                disabled={!documentExportAvailable}
                                data-testid={`studio-timeline-save-document-${kind}`}
                                onClick={() => void saveRenderedDocument(kind)}
                            >
                                {t("studio.timeline.save")}
                            </Button>
                            <Typography.Text className="text-xs text-stone-500">{t("studio.timeline.suggestedName", { name: rendered.suggestedName })}</Typography.Text>
                        </>
                    )}
                </Space>
                <Input.TextArea readOnly rows={rows} value={rendered.text} data-testid={`studio-timeline-document-${kind}`} className="font-mono text-xs" />
            </div>
        );
    };

    /**
     * documentVersionLabel names one version id for a person.
     *
     * The id is what the request carried, and a picker option is what a user recognises, so the two are
     * joined through the lists this section already holds. A version that is no longer in its list —
     * superseded and filtered out, or the read failed — falls back to the raw id rather than to a
     * number this interface would be inventing.
     */
    const documentVersionLabel = (kind: DocumentKind, versionId: string): string => {
        const rows: VersionRow[] = kind === "script" ? scriptVersions ?? [] : kind === "shotList" ? shotListVersions ?? [] : [];
        const found = rows.find((version) => version.id === versionId);
        return found ? versionOptionLabel(found, t) : versionId;
    };

    const cueColumns: ColumnsType<CueDraft> = [
        { title: "#", key: "ordinal", width: 60, render: (_value, _row, index) => index + 1 },
        {
            title: t("studio.timeline.startLabel"),
            key: "start",
            width: 130,
            render: (_value, _row, index) => (
                <InputNumber<number> min={0} value={drafts[index]?.startMs ?? 0} data-testid={`studio-timeline-cue-start-${index}`} onChange={(value) => updateDraft(index, { startMs: typeof value === "number" ? value : 0 })} />
            ),
        },
        {
            title: t("studio.timeline.endLabel"),
            key: "end",
            width: 130,
            render: (_value, _row, index) => <InputNumber<number> min={0} value={drafts[index]?.endMs ?? 0} data-testid={`studio-timeline-cue-end-${index}`} onChange={(value) => updateDraft(index, { endMs: typeof value === "number" ? value : 0 })} />,
        },
        {
            title: t("studio.timeline.textLabel"),
            key: "text",
            render: (_value, _row, index) => <Input value={drafts[index]?.text ?? ""} maxLength={500} data-testid={`studio-timeline-cue-text-${index}`} onChange={(event) => updateDraft(index, { text: event.target.value })} />,
        },
        {
            title: "",
            key: "remove",
            width: 90,
            render: (_value, _row, index) => (
                <Button size="small" type="text" danger data-testid={`studio-timeline-cue-remove-${index}`} onClick={() => setDrafts((current) => current.filter((_cue, position) => position !== index))}>
                    {t("studio.timeline.removeCue")}
                </Button>
            ),
        },
    ];

    const shots = timeline?.shots ?? [];

    /**
     * Whether this build can import a music file, asked as its own question.
     *
     * A build whose binding predates the import still reads a timeline and runs an export, so the
     * control is offered only when there is something behind it.
     */
    const musicImportAvailable = isMusicImportAvailable();

    /**
     * The project the music import belongs to, read off the ACTIVE EPISODE.
     *
     * This section deliberately takes no `projectId` prop — see the note on `TimelineSectionProps` —
     * because every other read and write here is episode-scoped and a prop it could only ignore would
     * be noise. The import is the section's first command that needs a project, and the episode
     * already names one: deriving it is exact rather than a guess, and adding a prop for this single
     * caller would put a second answer to "which project is this episode in" beside the episode's own.
     */
    const activeEpisodeForMusic = episodes.find((episode) => episode.id === activeEpisodeId) || null;
    const projectId = activeEpisodeForMusic?.projectId ?? "";

    /**
     * The effect suggestions, read from the script version the timeline names.
     *
     * `audioIntent` lives on the SCRIPT's shots, and the timeline's own rows carry `shotId` — so the
     * two are joined on that identifier rather than on the ordinal, which a board may reorder. The
     * read is best-effort and its failure is silent: a suggestion is a convenience, and a project whose
     * script cannot be read still has a timeline, a subtitle editor and an export.
     */
    useEffect(() => {
        const scriptVersionId = timeline?.scriptVersionId ?? "";
        if (!scriptVersionId || !isVoiceSurfaceAvailable()) {
            setEffectSuggestions({});
            return;
        }
        let cancelled = false;
        void getScriptStructure(scriptVersionId)
            .then(async (structure) => {
                // Every shot of every scene, flattened in script order — the suggestion re-sorts by
                // ordinal itself, so the order here is only about not losing a row.
                const inputs: desktop.ShotEffectInputDTO[] = [];
                for (const scene of structure.scenes) {
                    for (const shot of scene.shots) {
                        inputs.push({ shotId: shot.shotId, ordinal: shot.ordinal, audioIntent: shot.audioIntent ?? "" });
                    }
                }
                const suggestions = await suggestShotEffects(inputs);
                if (cancelled) return;
                const byShot: Record<string, desktop.EffectSuggestionDTO> = {};
                for (const suggestion of suggestions) {
                    byShot[suggestion.shotId] = suggestion;
                }
                setEffectSuggestions(byShot);
            })
            .catch(() => {
                if (!cancelled) setEffectSuggestions({});
            });
        return () => {
            cancelled = true;
        };
    }, [timeline?.scriptVersionId, timeline?.boardVersionId]);

    if (episodes.length === 0) {
        return <Empty description={t("studio.timeline.selectEpisode")} />;
    }

    // The SHELL's notice is about the drama binding. A build with the drama surface and no media
    // surface reaches this body, and every read below answers empty — which would render "no shots"
    // and "no tracks" about questions nobody asked. The section carries its own guard for that case.
    if (!bindingsAvailable) {
        return <Alert type="info" showIcon data-testid="studio-timeline-no-media-core" message={t("studio.timeline.noMediaCoreTitle")} description={t("studio.timeline.noMediaCoreBody")} />;
    }

    /**
     * acceptEffect suggests and GENERATES: it submits a speech job for the shot's suggested effect.
     *
     * # What FR-080's 生成适配 half means here
     *
     * The suggestion itself is a projection and is stored nowhere — that is WP-27's ruling, and it is
     * what keeps a suggestion from going stale when the intent is edited. ACCEPTING one is the act that
     * creates a fact, and the fact is an audio job: its bytes are spoken from the effect's own words and
     * its result is attached to the shot as an `audio_effect` version, which the mix layers as
     * punctuation rather than as a line of speech.
     *
     * The prompt is built from the MATCHED term rather than from the effect's name: a provider asked for
     * "footsteps" produces generic steps, while one asked for 「脚步」 in the language the script is
     * written in produces what the scene describes.
     */
    const acceptEffect = async (shot: desktop.TimelineShotDTO, effect: string, matched: string) => {
        const selection = config.audioModel || config.model;
        const decoded = decodeModelSelection(selection);
        const channelId = channelIdForModel(config, selection);
        if (!decoded.model || !channelId) {
            message.error(t("studio.video.modelRequired"));
            return;
        }
        setGeneratingEffect(shot.shotId);
        try {
            // An effect is an EFFECT job (T03), not a speech job with the
            // matched word in its text: a speech endpoint would read the word
            // aloud, which is the failure mode the audit named. The dedicated
            // command carries the description to the effect capability, and a
            // channel without one refuses honestly.
            const job = await submitEffectJob({
                projectId,
                episodeId: activeEpisodeId,
                shotId: shot.shotId,
                providerId: channelId,
                model: decoded.model,
                description: matched,
                format: config.audioFormat || undefined,
            } as never);
            message.success(t("studio.timeline.effectSubmitted", { effect, job: job.id.slice(0, 8) }));
            onChanged();
        } catch (failure) {
            message.error(failure instanceof Error ? failure.message : t("studio.timeline.effectFailed"));
        } finally {
            setGeneratingEffect("");
        }
    };

    /**
     * importMusic carries the chosen file into the library as a bed.
     *
     * # The shot it is attached to
     *
     * The episode's FIRST shot, and the choice is about which mix carries the music rather than about
     * when it begins: the core starts a bed at zero wherever it was attached, and the mix's join is
     * shot-keyed, so a usage recorded any other way would be a row nothing reads. A board with no rows
     * has no shot to attach to, and the refusal says so.
     */
    const importMusic = async (file: File) => {
        const firstShot = shots[0];
        if (!activeEpisodeId || !firstShot) {
            message.error(t("studio.timeline.musicNoShot"));
            return;
        }
        setImportingMusic(true);
        try {
            const imported = await importBackgroundMusic(firstShot.shotId, projectId, file);
            setImportedMusic({ name: file.name, bytes: imported.bytes });
            message.success(t("studio.timeline.musicImported", { name: file.name }));
            // The timeline is re-read rather than patched: the bed is an approved audio version, and the
            // audio column is what says whether the episode has any. A local patch would be a second
            // answer to a question the read already answers.
            await reload();
        } catch (failure) {
            message.error(failure instanceof Error ? failure.message : t("studio.timeline.musicFailed"));
        } finally {
            setImportingMusic(false);
        }
    };

    return (
        <div className="space-y-8" data-testid="studio-timeline">
            <section>
                <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
                    <h2 className="text-lg font-medium">{t("studio.timeline.shots")}</h2>
                    <Space wrap>
                        <Select
                            className="min-w-56"
                            value={activeEpisodeId || undefined}
                            placeholder={t("studio.timeline.selectEpisode")}
                            data-testid="studio-timeline-episode"
                            onChange={onSelectEpisode}
                            options={episodes.map((episode) => ({
                                value: episode.id,
                                label: `S${episode.seasonNumber}E${episode.episodeNumber} · ${episode.title}`,
                            }))}
                        />
                        {/* THE BACKGROUND MUSIC, which FR-080 lists as a V1 clause and which had no
                            control: `AudioRoleMusic` mixed and a user could not point at a file.
                            The label is a control rather than a styled button because the platform's own
                            file chooser is what a user expects here, and its `accept` is a HINT rather than
                            a check — the core checks the type its sniffer decides, so a renamed file is
                            stored as what it actually is. */}
                        {musicImportAvailable ? (
                            <label className={`inline-flex items-center gap-2 rounded border border-stone-300 px-3 py-1 text-sm dark:border-stone-600 ${importingMusic ? "opacity-60" : "cursor-pointer"}`} data-testid="studio-timeline-music-label">
                                <Music className="size-4" />
                                {importingMusic ? t("studio.timeline.musicImporting") : t("studio.timeline.musicImport")}
                                <input
                                    type="file"
                                    accept="audio/*,.mp3,.wav,.ogg,.m4a"
                                    className="hidden"
                                    disabled={importingMusic}
                                    data-testid="studio-timeline-music-input"
                                    onChange={(event) => {
                                        const chosen = event.target.files?.[0];
                                        // The input is cleared so choosing the SAME file twice fires again:
                                        // a user who fixed something on disk and re-picked it would
                                        // otherwise get no reaction at all.
                                        event.target.value = "";
                                        if (chosen) void importMusic(chosen);
                                    }}
                                />
                            </label>
                        ) : null}
                        <Button icon={<RefreshCw className="size-4" />} loading={loading} data-testid="studio-timeline-reload" onClick={() => void reload()}>
                            {t("studio.timeline.reload")}
                        </Button>
                    </Space>
                </div>
                {importedMusic ? (
                    <Typography.Paragraph className="mt-2 text-xs text-stone-500" data-testid="studio-timeline-music-imported">
                        {t("studio.timeline.musicImportedDetail", { name: importedMusic.name, mb: (importedMusic.bytes / (1024 * 1024)).toFixed(1) })}
                    </Typography.Paragraph>
                ) : null}

                {capability && !capability.exportAvailable ? (
                    // The core's own sentence, verbatim: it names what this machine is missing, and
                    // a translation would paraphrase a fact about the machine into a claim about
                    // the interface. ARCHITECTURE: "媒体引擎不可用：禁用相关能力并显示诊断".
                    <Alert className="mb-3" type="warning" showIcon message={t("studio.timeline.exportUnavailable")} description={capability.diagnostic || t("studio.timeline.exportUnavailableNoDiagnostic")} data-testid="studio-timeline-capability" />
                ) : null}

                {error ? <Alert className="mb-3" type="error" showIcon message={error} data-testid="studio-timeline-error" /> : null}
                {notice ? <Alert className="mb-3" type="success" showIcon message={notice} data-testid="studio-timeline-notice" /> : null}

                {shots.length === 0 && !loading ? (
                    // An episode with no APPROVED storyboard is refused by the core rather than
                    // answered with an empty list — "no board is approved" and "the board is empty"
                    // are different situations — so a plain empty state here means the board exists
                    // and holds no rows, or this browser has no core to ask.
                    <Empty description={t("studio.timeline.noShots")} />
                ) : (
                    <>
                        <Table<desktop.TimelineShotDTO>
                            rowKey="itemId"
                            size="small"
                            loading={loading}
                            pagination={false}
                            dataSource={shots}
                            data-testid="studio-timeline-shots"
                            columns={[
                                { title: t("studio.timeline.ordinal"), dataIndex: "ordinal", key: "ordinal", width: 70 },
                                { title: t("studio.timeline.duration"), dataIndex: "durationMs", key: "duration", width: 110, render: (value: number) => (value / 1000).toFixed(1) },
                                {
                                    title: t("studio.timeline.mediaLabel"),
                                    key: "media",
                                    render: (_value, row) =>
                                        row.mediaVersionId ? (
                                            <Space size="small">
                                                <Tag color="green">{t("studio.timeline.mediaApproved")}</Tag>
                                                {row.mediaKind ? <Tag>{t(`studio.assetKind.${row.mediaKind}`, { defaultValue: row.mediaKind })}</Tag> : null}
                                            </Space>
                                        ) : (
                                            <Tag color="red">{t("studio.timeline.mediaMissing")}</Tag>
                                        ),
                                },
                                {
                                    title: t("studio.timeline.audioLabel"),
                                    key: "audio",
                                    width: 130,
                                    render: (_value, row) => (row.hasAudio ? <Tag color="green">{t("studio.timeline.audioApproved")}</Tag> : <Tag color="orange">{t("studio.timeline.audioMissing")}</Tag>),
                                },
                                { title: t("studio.timeline.cueCountLabel"), dataIndex: "cueCount", key: "cueCount", width: 110 },
                                {
                                    // THE SUGGESTION, WITH ITS EVIDENCE. The matched term is shown beside
                                    // the effect because that is what makes it checkable: a user reading
                                    // "rain ← 雨" can see immediately whether this application understood
                                    // the shot, and a wrong suggestion becomes a phrase to add rather than
                                    // a mystery. A suggestion with no matched term is not rendered at all.
                                    title: t("studio.timeline.effectLabel"),
                                    key: "effect",
                                    width: 190,
                                    render: (_value, row) => {
                                        const suggestion = effectSuggestions[row.shotId];
                                        if (!suggestion) {
                                            return <Typography.Text type="secondary">{t("studio.timeline.effectNone")}</Typography.Text>;
                                        }
                                        return (
                                            <Space size="small" data-testid={`studio-timeline-effect-${row.shotId}`}>
                                                <Tag color="blue">{t(`studio.effect.${suggestion.effect}`, { defaultValue: suggestion.effect })}</Tag>
                                                <Typography.Text type="secondary">← {suggestion.matched}</Typography.Text>
                                                {/* ACCEPTING IS WHAT CREATES THE FACT. A suggestion is a
                                                    projection and is stored nowhere; this button submits the
                                                    job whose result becomes the shot's effect version. */}
                                                <Button size="small" loading={generatingEffect === row.shotId} data-testid={`studio-timeline-effect-generate-${row.shotId}`} onClick={() => void acceptEffect(row, suggestion.effect, suggestion.matched)}>
                                                    {t("studio.timeline.effectGenerate")}
                                                </Button>
                                            </Space>
                                        );
                                    },
                                },
                            ]}
                        />
                        <Typography.Paragraph className="mt-2 text-xs text-stone-500" data-testid="studio-timeline-total">
                            {t("studio.timeline.total", {
                                seconds: (timeline?.totalDurationMs ?? 0) / 1000,
                                missing: timeline?.missingMedia ?? 0,
                                cues: timeline?.cueCount ?? 0,
                                lines: timeline?.missingLines ?? 0,
                            })}
                        </Typography.Paragraph>
                    </>
                )}
            </section>

            <section>
                <h2 className="mb-3 text-lg font-medium">{t("studio.timeline.subtitles")}</h2>

                <Space wrap align="end" className="mb-3">
                    <Select
                        className="min-w-56"
                        value={selectedTrackId || undefined}
                        placeholder={t("studio.timeline.noTracks")}
                        disabled={!tracks || tracks.length === 0}
                        data-testid="studio-timeline-track"
                        onChange={(value: string) => setSelectedTrackId(value)}
                        options={(tracks ?? []).map((track) => ({
                            value: track.id,
                            label: `v${track.versionNumber} · ${t(`studio.versionStatus.${track.status}`, { defaultValue: track.status })}`,
                        }))}
                    />
                    <label>
                        <span className="mb-1 block text-sm">{t("studio.timeline.scriptVersionLabel")}</span>
                        <Select
                            className="min-w-56"
                            value={scriptVersionId || undefined}
                            placeholder={scriptBindingsAvailable ? t("studio.timeline.scriptVersionPlaceholder") : t("studio.timeline.scriptCoreMissing")}
                            loading={loading}
                            disabled={!scriptBindingsAvailable || scriptVersions === null || scriptVersions.length === 0}
                            data-testid="studio-timeline-script-version"
                            onChange={(value: string) => setScriptVersionId(value)}
                            options={(scriptVersions ?? []).map((version) => ({
                                value: version.id,
                                // The status is part of the label rather than decoration around it: a
                                // draft and an approved version can carry the same number, and which
                                // one a caption is drafted from is the choice being made here.
                                label: `v${version.versionNumber} · ${t(`studio.versionStatus.${version.status}`, { defaultValue: version.status })}`,
                            }))}
                        />
                    </label>
                    <Button loading={busy === "draft"} data-testid="studio-timeline-draft" onClick={() => void draft()}>
                        {t("studio.timeline.draft")}
                    </Button>
                    {/* The two steps of an approval, in the order the core accepts them: a draft
                        goes to review, and a reviewed track goes in force. Each is disabled exactly
                        when the core would refuse it, so a press never produces a conflict about a
                        state the user could not reach. */}
                    <Popconfirm title={t("studio.timeline.confirmSubmitTrack")} disabled={!trackDraft} onConfirm={() => void submitTrack()}>
                        <Button loading={busy === "submit-track"} disabled={!trackDraft} data-testid="studio-timeline-submit-track">
                            {t("studio.timeline.submitTrack")}
                        </Button>
                    </Popconfirm>
                    <Popconfirm title={t("studio.timeline.confirmApproveTrack")} disabled={!trackInReview} onConfirm={() => void approveTrack()}>
                        <Button loading={busy === "approve-track"} disabled={!trackInReview} data-testid="studio-timeline-approve-track">
                            {t("studio.timeline.approveTrack")}
                        </Button>
                    </Popconfirm>
                    <Select className="w-28" value={format} data-testid="studio-timeline-format" onChange={(value: string) => setFormat(value)} options={FORMATS.map((value) => ({ value, label: value.toUpperCase() }))} />
                    <Button loading={busy === "render"} disabled={!selectedTrackId} data-testid="studio-timeline-render-subtitles" onClick={() => void render()}>
                        {t("studio.timeline.renderSubtitles")}
                    </Button>
                </Space>

                {/* Why the version picker has nothing to offer, and which of the two reasons it is.
                    A disabled Select would otherwise be the only signal: "this episode has no script
                    version" is a state a user can act on — the script section's generation stage
                    creates one — while "this build cannot read the script" is not, and reporting the
                    second as the first would be a claim about the episode drawn from an absent read. */}
                {!scriptBindingsAvailable ? (
                    <Alert className="mb-3" type="info" showIcon data-testid="studio-timeline-no-script-core" message={t("studio.timeline.noScriptCoreTitle")} description={t("studio.timeline.noScriptCoreBody")} />
                ) : scriptVersions !== null && scriptVersions.length === 0 ? (
                    <Alert className="mb-3" type="info" showIcon data-testid="studio-timeline-no-script-version" message={t("studio.timeline.noScriptVersionTitle")} description={t("studio.timeline.noScriptVersionBody")} />
                ) : null}

                {/* The selected track's status, said out loud, so a disabled button is never the
                    only clue about which step comes next. */}
                <Typography.Paragraph className="mb-3 text-xs text-stone-500">{t("studio.timeline.trackReviewNote")}</Typography.Paragraph>

                {tracks === null ? <Empty description={t("studio.timeline.subtitlesUnread")} /> : tracks.length === 0 ? <Empty description={t("studio.timeline.noTracks")} /> : null}

                {missing.length > 0 ? (
                    <Alert
                        className="mb-3"
                        type="warning"
                        showIcon
                        data-testid="studio-timeline-missing"
                        message={t("studio.timeline.missingLines", { count: missing.length })}
                        description={
                            <ul className="mt-1 list-disc pl-5 text-xs">
                                {missing.map((line) => (
                                    <li key={line.lineId} data-missing-line={line.lineId}>
                                        <Tag>{t(`studio.lineType.${line.type}`, { defaultValue: line.type })}</Tag>
                                        <span>{line.text}</span>
                                    </li>
                                ))}
                            </ul>
                        }
                    />
                ) : null}

                {selectedTrackId ? (
                    <>
                        <Table<CueDraft> rowKey="id" size="small" pagination={false} dataSource={drafts} data-testid="studio-timeline-cues" columns={cueColumns} />
                        <Space className="mt-3">
                            <Button type="primary" loading={busy === "cues"} data-testid="studio-timeline-save-cues" onClick={() => void saveCues()}>
                                {t("studio.timeline.saveCues")}
                            </Button>
                            <Button
                                data-testid="studio-timeline-add-cue"
                                onClick={() =>
                                    setDrafts((current) => {
                                        // A new cue starts where the previous one ended, so an
                                        // appended line does not overlap the one before it — the rule
                                        // the core refuses a save over.
                                        const last = current[current.length - 1];
                                        const start = last ? last.endMs : 0;
                                        return [...current, { id: `new-${current.length}-${Date.now()}`, startMs: start, endMs: start + 2000, text: "" }];
                                    })
                                }
                            >
                                {t("studio.timeline.addCue")}
                            </Button>
                        </Space>
                    </>
                ) : null}

                {document !== "" ? (
                    <div className="mt-4">
                        <h3 className="mb-2 text-sm font-medium">{t("studio.timeline.documentTitle", { format: format.toUpperCase() })}</h3>
                        {/* Read-only on purpose: the core returns the DOCUMENT rather than writing
                            it, so a user can read what they are about to save. Saving it is the
                            export row's own control below. */}
                        <Input.TextArea readOnly rows={12} value={document} data-testid="studio-timeline-document" className="font-mono text-xs" />
                    </div>
                ) : null}
            </section>

            <section>
                <h2 className="mb-3 text-lg font-medium">{t("studio.timeline.exportTitle")}</h2>
                <Space wrap align="end" className="mb-3">
                    <label>
                        <span className="mb-1 block text-sm">{t("studio.timeline.qualityLabel")}</span>
                        <Select className="w-32" value={quality} data-testid="studio-timeline-quality" onChange={(value: string) => setQuality(value)} options={QUALITIES.map((value) => ({ value, label: t(`studio.timeline.quality.${value}`) }))} />
                    </label>
                    <label>
                        <span className="mb-1 block text-sm">{t("studio.timeline.widthLabel")}</span>
                        <InputNumber min={64} max={7680} value={width} data-testid="studio-timeline-width" onChange={(value) => setWidth(typeof value === "number" ? value : null)} />
                    </label>
                    <label>
                        <span className="mb-1 block text-sm">{t("studio.timeline.heightLabel")}</span>
                        <InputNumber min={64} max={4320} value={height} data-testid="studio-timeline-height" onChange={(value) => setHeight(typeof value === "number" ? value : null)} />
                    </label>
                    <label>
                        <span className="mb-1 block text-sm">{t("studio.timeline.subtitleModeLabel")}</span>
                        <Select
                            className="w-40"
                            value={subtitleMode}
                            data-testid="studio-timeline-subtitle-mode"
                            onChange={(value: string) => setSubtitleMode(value)}
                            options={SUBTITLE_MODES.map((mode) => ({ value: mode.value, label: t(`studio.timeline.${mode.labelKey}`) }))}
                        />
                    </label>
                    <Button
                        type="primary"
                        icon={<Play className="size-4" />}
                        loading={busy === "export"}
                        // The core refuses an export on a machine with no engine and says why in
                        // `diagnostic`; disabling the control is ARCHITECTURE's own ruling —
                        // "禁用相关能力并显示诊断" — so the reason is on screen instead of arriving
                        // after a press.
                        disabled={!exportAvailable || (capability !== null && !capability.exportAvailable)}
                        data-testid="studio-timeline-run-export"
                        onClick={() => void compose()}
                    >
                        {t("studio.timeline.runExport")}
                    </Button>
                </Space>
                <Typography.Paragraph className="text-xs text-stone-500">{t("studio.timeline.exportHint")}</Typography.Paragraph>

                {exports === null ? (
                    <Empty description={t("studio.timeline.exportsUnread")} />
                ) : exports.length === 0 ? (
                    <Empty description={t("studio.timeline.noExports")} />
                ) : (
                    <Table<desktop.ExportRecordDTO>
                        rowKey="id"
                        size="small"
                        pagination={false}
                        dataSource={exports}
                        data-testid="studio-timeline-exports"
                        columns={[
                            { title: t("studio.timeline.versionLabel"), dataIndex: "versionNumber", key: "versionNumber", width: 90, render: (value: number) => `v${value}` },
                            {
                                title: t("studio.timeline.statusLabel"),
                                dataIndex: "status",
                                key: "status",
                                width: 150,
                                render: (value: string) => <Tag color={exportStatusColour(value)}>{t(`studio.versionStatus.${value}`, { defaultValue: value })}</Tag>,
                            },
                            { title: t("studio.timeline.qualityLabel"), dataIndex: "quality", key: "quality", width: 110, render: (value: string) => t(`studio.timeline.quality.${value}`, { defaultValue: value }) },
                            { title: t("studio.timeline.framesLabel"), key: "frames", width: 130, render: (_value, row) => `${row.width}×${row.height}` },
                            { title: t("studio.timeline.duration"), dataIndex: "durationMs", key: "duration", width: 110, render: (value: number) => (value / 1000).toFixed(1) },
                            {
                                title: t("studio.timeline.fileLabel"),
                                key: "hash",
                                render: (_value, row) => (row.outputFileHash ? <code className="text-xs">{row.outputFileHash.slice(0, 12)}…</code> : <span className="text-xs text-stone-500">{t("studio.timeline.noOutputFile")}</span>),
                            },
                            {
                                title: "",
                                key: "actions",
                                width: 240,
                                render: (_value, row) => (
                                    <Space size="small">
                                        <Button
                                            size="small"
                                            icon={<Download className="size-3" />}
                                            loading={busy === `save-${row.id}`}
                                            disabled={!row.outputFileHash || !exportAvailable}
                                            data-testid={`studio-timeline-save-${row.id}`}
                                            onClick={() => void writeToDisk(row)}
                                        >
                                            {t("studio.timeline.save")}
                                        </Button>
                                        <Button
                                            size="small"
                                            loading={busy === `submit-${row.id}`}
                                            // A draft goes to review before it can be approved, which
                                            // is the core's rule and the row's own status says so.
                                            disabled={row.status !== "draft"}
                                            data-testid={`studio-timeline-submit-${row.id}`}
                                            onClick={() => void submitOne(row)}
                                        >
                                            {t("studio.timeline.submitExport")}
                                        </Button>
                                        <Button
                                            size="small"
                                            loading={busy === `approve-${row.id}`}
                                            // Enabled exactly when the core would accept it: an
                                            // approval updates a row whose status is `under_review`.
                                            disabled={row.status !== "under_review"}
                                            data-testid={`studio-timeline-approve-${row.id}`}
                                            onClick={() => void approveOne(row)}
                                        >
                                            {t("studio.timeline.approveExport")}
                                        </Button>
                                    </Space>
                                ),
                            },
                        ]}
                        expandable={{
                            // AC-MEDIA-003's "manifest traceability": the manifest travels with the
                            // row, so it is shown here rather than behind a second call.
                            expandedRowRender: (row) =>
                                row.manifestJson ? (
                                    <Input.TextArea readOnly rows={10} value={row.manifestJson} data-testid={`studio-timeline-manifest-${row.id}`} className="font-mono text-xs" />
                                ) : (
                                    <Typography.Text type="secondary">{t("studio.timeline.noManifest")}</Typography.Text>
                                ),
                            rowExpandable: (row) => Boolean(row.manifestJson),
                        }}
                    />
                )}
            </section>

            {/* ROADMAP item 11's other half: the episode's three documents, each rendered here to be
                READ and saved from what was read rather than written on the first press — the same
                shape the subtitle document above keeps, and the core's own decision in both cases
                (`ExportScript`, `ExportShotList` and `ExportManifestDocument` return the text). */}
            <section>
                <h2 className="mb-3 text-lg font-medium">{t("studio.timeline.documentsTitle")}</h2>
                <Typography.Paragraph className="mb-3 text-xs text-stone-500">{t("studio.timeline.documentsHint")}</Typography.Paragraph>

                {!documentExportAvailable ? (
                    // The documents are a later addition to the media binding, so a build can carry the
                    // timeline without them. The controls stay on screen and are disabled, which is the
                    // same answer the export button gives a machine with no engine: a visible control
                    // that says why, rather than a press that fails.
                    <Alert className="mb-3" type="warning" showIcon data-testid="studio-timeline-no-document-core" message={t("studio.timeline.noDocumentCoreTitle")} description={t("studio.timeline.noDocumentCoreBody")} />
                ) : null}

                <div className="space-y-6">
                    <div>
                        <h3 className="mb-2 text-sm font-medium">{t("studio.timeline.documentKind.script")}</h3>
                        <Space wrap align="end">
                            <label>
                                <span className="mb-1 block text-sm">{t("studio.timeline.documentVersionLabel")}</span>
                                {/* The version this document CONTAINS, which the request names as
                                    `versionId` and the core honours. It is a different choice from the
                                    subtitle picker above — that one is the script a draft renders
                                    from — so the two are separate fields over one list. */}
                                <Select
                                    className="min-w-56"
                                    value={scriptDocumentVersionId || undefined}
                                    placeholder={scriptBindingsAvailable ? t("studio.timeline.documentVersionPlaceholder") : t("studio.timeline.scriptCoreMissing")}
                                    loading={loading}
                                    disabled={!scriptBindingsAvailable || scriptVersions === null || scriptVersions.length === 0}
                                    data-testid="studio-timeline-script-document-version"
                                    onChange={(value: string) => setScriptDocumentVersionId(value)}
                                    options={(scriptVersions ?? []).map((version) => ({
                                        value: version.id,
                                        // See `versionOptionLabel`: the status is what tells two versions
                                        // with the same number apart.
                                        label: versionOptionLabel(version, t),
                                    }))}
                                />
                            </label>
                            <label>
                                <span className="mb-1 block text-sm">{t("studio.timeline.scriptFormatLabel")}</span>
                                <Select
                                    className="w-32"
                                    value={scriptFormat}
                                    data-testid="studio-timeline-script-format"
                                    onChange={(value: string) => setScriptFormat(value)}
                                    options={SCRIPT_FORMATS.map((value) => ({ value, label: value.toUpperCase() }))}
                                />
                            </label>
                            <label>
                                <span className="mb-1 block text-sm">{t("studio.timeline.includeShotsLabel")}</span>
                                <div>
                                    {/* The switch is the request's own field, forwarded rather than
                                        decided here: whether the document carries the camera setups
                                        is the user's choice and the section does not make it. */}
                                    <Switch checked={includeShots} data-testid="studio-timeline-include-shots" onChange={(checked: boolean) => setIncludeShots(checked)} />
                                </div>
                            </label>
                            <Button
                                loading={busy === "document-script"}
                                disabled={!documentExportAvailable}
                                data-testid="studio-timeline-render-script"
                                onClick={() => void renderDocument("script")}
                            >
                                {t("studio.timeline.renderScript")}
                            </Button>
                        </Space>
                        {/* WHY THE PICKER IS EMPTY, and which of the two reasons it is — the same
                            distinction the subtitle picker draws above. "No script version" is a state
                            a user can act on and the notice names the act; "this build cannot read
                            script versions" is not, and reporting it as the first would be a claim
                            about the episode drawn from an absent binding. A disabled Select on its
                            own would say neither. */}
                        {!scriptBindingsAvailable ? (
                            <Typography.Paragraph className="mt-2 text-xs text-stone-500" data-testid="studio-timeline-script-document-no-core">
                                {t("studio.timeline.noScriptCoreBody")}
                            </Typography.Paragraph>
                        ) : scriptVersions !== null && scriptVersions.length === 0 ? (
                            <Typography.Paragraph className="mt-2 text-xs text-stone-500" data-testid="studio-timeline-script-document-no-version">
                                {t("studio.timeline.documentNoScriptVersionBody")}
                            </Typography.Paragraph>
                        ) : null}
                        {documentPreview("script", 16)}
                    </div>

                    <div>
                        <h3 className="mb-2 text-sm font-medium">{t("studio.timeline.documentKind.shotList")}</h3>
                        <Space wrap align="end">
                            <label>
                                <span className="mb-1 block text-sm">{t("studio.timeline.documentVersionLabel")}</span>
                                {/* The board this shot list contains. `listStoryboardVersions` reads it
                                    through the episode's own board identity, which is the pair
                                    `StoryboardTableSection` calls — so the versions offered here are
                                    the ones the storyboard table is showing. */}
                                <Select
                                    className="min-w-56"
                                    value={shotListVersionId || undefined}
                                    placeholder={scriptBindingsAvailable ? t("studio.timeline.documentVersionPlaceholder") : t("studio.timeline.scriptCoreMissing")}
                                    loading={loading}
                                    disabled={!scriptBindingsAvailable || shotListVersions === null || shotListVersions.length === 0}
                                    data-testid="studio-timeline-shot-list-version"
                                    onChange={(value: string) => setShotListVersionId(value)}
                                    options={(shotListVersions ?? []).map((version) => ({ value: version.id, label: versionOptionLabel(version, t) }))}
                                />
                            </label>
                            <label>
                                <span className="mb-1 block text-sm">{t("studio.timeline.shotListFormatLabel")}</span>
                                <Select
                                    className="w-32"
                                    value={shotListFormat}
                                    data-testid="studio-timeline-shot-list-format"
                                    onChange={(value: string) => setShotListFormat(value)}
                                    options={SHOT_LIST_FORMATS.map((value) => ({ value, label: value.toUpperCase() }))}
                                />
                            </label>
                            <Button
                                loading={busy === "document-shotList"}
                                disabled={!documentExportAvailable}
                                data-testid="studio-timeline-render-shot-list"
                                onClick={() => void renderDocument("shotList")}
                            >
                                {t("studio.timeline.renderShotList")}
                            </Button>
                        </Space>
                        {!scriptBindingsAvailable ? (
                            <Typography.Paragraph className="mt-2 text-xs text-stone-500" data-testid="studio-timeline-shot-list-no-core">
                                {t("studio.timeline.noScriptCoreBody")}
                            </Typography.Paragraph>
                        ) : shotListVersions !== null && shotListVersions.length === 0 ? (
                            <Typography.Paragraph className="mt-2 text-xs text-stone-500" data-testid="studio-timeline-shot-list-no-version">
                                {t("studio.timeline.documentNoShotListVersionBody")}
                            </Typography.Paragraph>
                        ) : null}
                        {documentPreview("shotList", 12)}
                    </div>

                    <div>
                        <h3 className="mb-2 text-sm font-medium">{t("studio.timeline.documentKind.manifest")}</h3>
                        <Space wrap align="end">
                            <Button
                                loading={busy === "document-manifest"}
                                disabled={!documentExportAvailable}
                                data-testid="studio-timeline-render-manifest"
                                onClick={() => void renderDocument("manifest")}
                            >
                                {t("studio.timeline.renderManifest")}
                            </Button>
                        </Space>
                        {/* NO VERSION PICKER HERE, deliberately. `ExportManifestDocumentRequest`
                            carries the episode and nothing else: the manifest is the NEWEST EXPORT'S
                            own record of what that film was made from, not a rendering of a version a
                            user selects. A picker would be a control that changes nothing. */}
                        <Typography.Paragraph className="mt-2 text-xs text-stone-500">{t("studio.timeline.manifestNoVersionNote")}</Typography.Paragraph>
                        {documentPreview("manifest", 12)}
                    </div>
                </div>
            </section>

            <Collapse
                items={[
                    {
                        key: "final-review",
                        // The Final Supervisor is a STAGE, not a control here: `final_episode` runs
                        // through the production pipeline — it is one of the six stages
                        // `productionpipeline.Stages()` lists — and its report is read in the
                        // quality section. This panel says where, rather than offering a second
                        // button for a command this binding does not have.
                        label: t("studio.timeline.finalReviewTitle"),
                        children: <Typography.Paragraph className="text-sm">{t("studio.timeline.finalReviewBody")}</Typography.Paragraph>,
                    },
                ]}
            />
        </div>
    );
}

/**
 * renderByKind calls the one document method that matches the kind.
 *
 * The three requests are shaped here rather than inside `media.ts` because this is the layer that knows
 * WHICH control a user pressed; the client's job is to name the binding method and keep the absence
 * rule, and it would have to take a kind to do this instead — a union in a client whose other exports
 * each take their own request.
 *
 * `versionId` carries the picking user's choice for the two kinds that HAVE a picker, and it is empty
 * for the manifest because that request has no such field to fill. Empty is meaningful rather than a
 * gap for the other two: the core reads it as "the version in force", which is the honest request
 * exactly when the picker holds nothing — an episode whose history this build could not read — and
 * `documents[kind].versionId` records the value sent, so a preview rendered under one version and read
 * under another is detectable rather than silent.
 */
async function renderByKind(
    kind: DocumentKind,
    episodeId: string,
    request: { scriptFormat: string; shotListFormat: string; includeShots: boolean; versionId: string },
): Promise<desktop.DocumentDTO> {
    switch (kind) {
        case "script":
            return exportScript({
                episodeId,
                versionId: request.versionId || undefined,
                format: request.scriptFormat,
                includeShots: request.includeShots,
            } as never);
        case "shotList":
            return exportShotList({ episodeId, versionId: request.versionId || undefined, format: request.shotListFormat } as never);
        case "manifest":
            // No `versionId`: the manifest is the NEWEST EXPORT'S own document, not a rendering of a
            // version a user could pick. `ExportManifestDocumentRequest` carries the episode alone, so
            // a picker beside this control would have nothing to put in the request.
            return exportManifestDocument({ episodeId } as never);
    }
}

/**
 * versionOptionLabel is how every version picker in this section names one option.
 *
 * `v{n} · {status}` is the shape the timeline's subtitle picker and the audio section's picker already
 * use, and keeping it identical here is what makes the controls read as one: the status is part of the
 * label rather than decoration around it, because a draft and an approved version can carry the same
 * number and which one a document contains is the choice being made. The status goes through `t` with
 * the RAW value as its fallback, so a status a locale has not caught up with shows as itself rather
 * than as a missing key.
 */
function versionOptionLabel(version: VersionRow, translate: Translate): string {
    return `v${version.versionNumber} · ${translate(`studio.versionStatus.${version.status}`, { defaultValue: version.status })}`;
}

/**
 * Translate is the one shape this file needs from i18next.
 *
 * `t` itself is a heavily overloaded generic, and naming the slice used here keeps the two helpers
 * below readable; the alternative — threading `t` through every call site's own inline expression —
 * is what the three near-identical labels would otherwise become.
 */
type Translate = (key: string, options: { defaultValue: string }) => string;

/**
 * firstCueProblem reports the first cue problem a user can act on, or the empty string.
 *
 * It mirrors the two rules `domainmedia.ValidateTrack` and `Cue.Validate` enforce — a cue must last
 * a positive time, and two cues may not be on screen together — so a mistake is named in the editor
 * rather than arriving as a core refusal after a write. The core remains the authority: this checks
 * only the pair of conditions a person can see on screen.
 */
function firstCueProblem(cues: CueDraft[]): string {
    for (let index = 0; index < cues.length; index += 1) {
        const cue = cues[index];
        if (cue.endMs <= cue.startMs) return "endBeforeStart";
        if (index > 0 && cue.startMs < cues[index - 1].endMs) return "overlap";
    }
    return "";
}

/** exportStatusColour maps an export status to a tag colour. Text always accompanies it. */
function exportStatusColour(status: string): string {
    switch (status) {
        case "approved":
            return "green";
        case "under_review":
            return "blue";
        case "superseded":
            return "default";
        default:
            return "orange";
    }
}
