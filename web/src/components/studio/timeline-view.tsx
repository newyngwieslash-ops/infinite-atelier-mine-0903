import { useCallback, useEffect, useMemo, useState } from "react";
import { Alert, App, Button, Collapse, Empty, Input, InputNumber, Popconfirm, Select, Space, Switch, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Download, Play, RefreshCw } from "lucide-react";
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
 *  5. NOTHING IS SENT THAT THE CORE CANNOT ATTRIBUTE. A subtitle draft needs a script version id
 *     and no read here can enumerate one (see `scriptVersionId` below), so the field is an INPUT the
 *     user fills rather than a value this interface guessed.
 *  6. A DOCUMENT IS READ BEFORE IT IS WRITTEN. ROADMAP item 11's script, shot list and manifest are
 *     the same shape as the subtitle document: the core returns the TEXT and `SaveDocument` is the
 *     separate act that writes it, so each of the three is previewed here and saved from the preview.
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

    const [timeline, setTimeline] = useState<desktop.TimelineDTO | null>(null);
    const [capability, setCapability] = useState<desktop.MediaCapabilityDTO | null>(null);
    const [tracks, setTracks] = useState<desktop.SubtitleTrackDTO[] | null>(null);
    const [missing, setMissing] = useState<desktop.MissingLineDTO[]>([]);
    const [exports, setExports] = useState<desktop.ExportRecordDTO[] | null>(null);
    const [selectedTrackId, setSelectedTrackId] = useState("");
    const [scriptVersionId, setScriptVersionId] = useState("");
    const [format, setFormat] = useState<string>("srt");
    const [document, setDocument] = useState("");
    const [quality, setQuality] = useState<string>("preview");
    const [width, setWidth] = useState<number | null>(null);
    const [height, setHeight] = useState<number | null>(null);
    const [subtitleMode, setSubtitleMode] = useState<string>("");
    const [drafts, setDrafts] = useState<CueDraft[]>([]);
    const [loading, setLoading] = useState(false);
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
        } finally {
            setLoading(false);
        }
    }, [activeEpisodeId, t]);

    useEffect(() => {
        void reload();
    }, [reload]);

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
        // The core requires BOTH ids and refuses a draft with either empty. The script version is
        // typed by the user because no read in this build enumerates a script's versions: the
        // repository has `ListScriptVersions`, but it is not on any service method, binding method
        // or DTO — `ApproveScriptVersion`, `CreateScriptVersion` and `SaveScriptStructure` each take
        // a version id and none returns a list, and `ScriptDTO.currentVersionId` is a column no
        // statement writes. Asking is the honest form of an unavailable read.
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
     */
    const renderDocument = async (kind: DocumentKind) => {
        if (!activeEpisode) return;
        setBusy(`document-${kind}`);
        try {
            const rendered = await renderByKind(kind, activeEpisode.id, { scriptFormat, shotListFormat, includeShots });
            setDocuments((current) => ({ ...current, [kind]: { text: rendered.text, suggestedName: rendered.suggestedName } }));
        } catch (failure) {
            message.error(failure instanceof Error ? failure.message : t(`studio.timeline.documentFailed.${kind}`));
            setDocuments((current) => ({ ...current, [kind]: null }));
        } finally {
            setBusy("");
        }
    };

    /**
     * saveRenderedDocument writes one rendered document to where the user points.
     *
     * The text travels BACK rather than being re-rendered, which is the core's own reasoning: a second
     * render could pick up a version approved in between, so what a user read would not be what they
     * saved. `suggestedName` is the core's own suggestion — a hint to a human, not a destination — and
     * the dialog is still the only thing that decides where the file goes.
     *
     * `written: false` is the user having cancelled the dialog, and this handler reports it the way
     * `writeToDisk` does: as an ordinary act, not a failure.
     */
    const saveRenderedDocument = async (kind: DocumentKind) => {
        const rendered = documents[kind];
        if (!rendered) return;
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
     */
    const documentPreview = (kind: DocumentKind, rows: number) => {
        const rendered = documents[kind];
        if (!rendered) return null;
        return (
            <div className="mt-3">
                <Space wrap className="mb-2">
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
                </Space>
                <Input.TextArea readOnly rows={rows} value={rendered.text} data-testid={`studio-timeline-document-${kind}`} className="font-mono text-xs" />
            </div>
        );
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

    if (episodes.length === 0) {
        return <Empty description={t("studio.timeline.selectEpisode")} />;
    }

    // The SHELL's notice is about the drama binding. A build with the drama surface and no media
    // surface reaches this body, and every read below answers empty — which would render "no shots"
    // and "no tracks" about questions nobody asked. The section carries its own guard for that case.
    if (!bindingsAvailable) {
        return <Alert type="info" showIcon data-testid="studio-timeline-no-media-core" message={t("studio.timeline.noMediaCoreTitle")} description={t("studio.timeline.noMediaCoreBody")} />;
    }

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
                        <Button icon={<RefreshCw className="size-4" />} loading={loading} data-testid="studio-timeline-reload" onClick={() => void reload()}>
                            {t("studio.timeline.reload")}
                        </Button>
                    </Space>
                </div>

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
                        <Input
                            className="w-56"
                            value={scriptVersionId}
                            maxLength={120}
                            data-testid="studio-timeline-script-version"
                            placeholder={t("studio.timeline.scriptVersionPlaceholder")}
                            onChange={(event) => setScriptVersionId(event.target.value)}
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
                        {documentPreview("script", 16)}
                    </div>

                    <div>
                        <h3 className="mb-2 text-sm font-medium">{t("studio.timeline.documentKind.shotList")}</h3>
                        <Space wrap align="end">
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
 * No `versionId` is sent: the core renders the version IN FORCE, and a version id this interface
 * invented would render someone else's draft as "my script". The subtitle engine's known gap —
 * `scriptVersionId` is a field a user fills because no read enumerates versions — does not apply here,
 * because these three documents are defined as the approved versions.
 */
async function renderByKind(
    kind: DocumentKind,
    episodeId: string,
    formats: { scriptFormat: string; shotListFormat: string; includeShots: boolean },
): Promise<desktop.DocumentDTO> {
    switch (kind) {
        case "script":
            return exportScript({ episodeId, format: formats.scriptFormat, includeShots: formats.includeShots } as never);
        case "shotList":
            return exportShotList({ episodeId, format: formats.shotListFormat } as never);
        case "manifest":
            return exportManifestDocument({ episodeId } as never);
    }
}

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
