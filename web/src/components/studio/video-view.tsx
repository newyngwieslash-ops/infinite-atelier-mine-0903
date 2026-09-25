import { useCallback, useEffect, useMemo, useState } from "react";
import { Alert, App, Button, Empty, Input, InputNumber, Select, Space, Switch, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Layers, Play, RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";

import { isVideoBatchAvailable, listJobs, submitVideoBatch, submitVideoJob } from "@/services/desktop/jobs";
import { loadShotFrame, type ShotFrame } from "@/services/desktop/frames";
import { isMediaBindingsAvailable, mediaCapability, readTimeline } from "@/services/desktop/media";
import { channelIdForModel, decodeModelSelection } from "@/services/desktop/model-selection";
import { useEffectiveConfig } from "@/stores/use-config-store";
import type { desktop } from "@/wailsjs/go/models";

/**
 * VideoSection is WP-11's video surface: the shots a video can be generated for.
 *
 * FR-080's acceptance is what this section shows — "分镜到视频的生成、挑选与版本管理" — and it is
 * built out of three reads that already existed and nothing invented:
 *
 *  1. `ReadTimeline` is what "which shots exist" means: the ordered rows of the episode's approved
 *     storyboard, each with the media approved for it and whether it has audio. A section that
 *     listed shots from anywhere else would be a second answer to a question the timeline already
 *     answers.
 *  2. `MediaCapability` is the MACHINE's answer, and it is shown VERBATIM when it says export is
 *     unavailable. ARCHITECTURE's "媒体引擎不可用：禁用相关能力并显示诊断" gives the diagnostic a home:
 *     this section renders the core's own sentence rather than a translation of it, because the
 *     sentence names what to install and a translation would paraphrase a fact.
 *  3. The job list is the JobsBinding's, filtered to this project in the client. The binding's
 *     `ListJobsRequest` carries no project field — its four fields are statuses, activeOnly, limit
 *     and offset — so the filter is applied here and the comment says so rather than implying the
 *     core scoped the query.
 *
 * WHAT IT DOES NOT OFFER, and why: there is no per-shot "pick this version" control and no
 * version list for a shot's video. The binding exposes no command that enumerates or selects a
 * shot's video versions — `ListExports` and `ListSubtitleTracks` are per episode, and a shot's
 * generated media is reachable only as the job's `resultFiles`. Adding a picker would mean
 * inventing a call, so the section shows the job's outcome and its stored files instead.
 */
export type VideoSectionProps = {
    projectId: string;
    episodes: desktop.EpisodeDTO[];
    activeEpisodeId: string;
    onSelectEpisode: (episodeId: string) => void;
    onChanged: () => void;
};

/** The job types this section lists: a shot's video, and nothing else's audio work. */
const VIDEO_JOB_TYPES = ["video_generation"];

export function VideoSection({ projectId, episodes, activeEpisodeId, onSelectEpisode, onChanged }: VideoSectionProps) {
    const { t } = useTranslation();
    const { message } = App.useApp();
    const config = useEffectiveConfig();

    const [timeline, setTimeline] = useState<desktop.TimelineDTO | null>(null);
    const [capability, setCapability] = useState<desktop.MediaCapabilityDTO | null>(null);
    const [jobs, setJobs] = useState<desktop.JobDTO[]>([]);
    const [loading, setLoading] = useState(false);
    const [refreshing, setRefreshing] = useState(false);
    const [error, setError] = useState("");
    const [selectedShotId, setSelectedShotId] = useState("");
    const [prompt, setPrompt] = useState("");
    const [seconds, setSeconds] = useState<number>(4);
    const [submitting, setSubmitting] = useState(false);
    /**
     * The frames the user chose for the NEXT submission, and the bytes once they are loaded.
     *
     * Two pieces of state rather than one, because "chosen" and "loaded" are different facts and the
     * gap between them is where a submission would otherwise go wrong: a request built while the load
     * was still running would carry no frame, and a request built after a FAILED load would carry an
     * empty one. `firstFrame`/`lastFrame` hold the loaded frames, and a null there with a non-empty
     * choice is a load that failed — which is stated rather than sent.
     */
    const [firstFrameChoice, setFirstFrameChoice] = useState(false);
    const [lastFrameChoice, setLastFrameChoice] = useState(false);
    const [frame, setFrame] = useState<ShotFrame | null>(null);
    const [frameError, setFrameError] = useState("");
    const [loadingFrame, setLoadingFrame] = useState(false);
    /**
     * The shots a batch would generate for, keyed by `shotId`.
     *
     * A MAP rather than an array so adding and removing is not a scan, and keyed by the SHOT rather
     * than the board row: the row's `itemId` is a board's row identity and the shot is what the job's
     * entity is, so a selection keyed on the row would break when the board is re-versioned.
     */
    const [batchSelection, setBatchSelection] = useState<Record<string, boolean>>({});
    const [batching, setBatching] = useState(false);
    const [batchResult, setBatchResult] = useState<desktop.SubmitVideoBatchResultDTO | null>(null);

    const activeEpisode = useMemo(() => episodes.find((episode) => episode.id === activeEpisodeId) || null, [episodes, activeEpisodeId]);

    const bindingsAvailable = isMediaBindingsAvailable();
    /**
     * Whether this build can submit a batch, asked as its own question.
     *
     * A build whose binding predates the batch still submits one shot at a time, so the multi-select is
     * offered only when there is something to do with it.
     */
    const batchAvailable = isVideoBatchAvailable();
    /**
     * batchLimit mirrors the core's `maxVideoBatch`.
     *
     * IT IS A SECOND COPY OF A NUMBER, which is normally the shape this codebase refuses — but the
     * alternative is worse here: the constant is unexported Go, and the only way to learn it would be to
     * submit an oversized batch and read the refusal. What keeps the copy honest is that the core
     * REFUSES an oversized request rather than truncating it, so a drifted value produces a refusal the
     * user sees rather than a silently shortened batch. Six matches the core's, and this note is where
     * to look when one moves.
     */
    const batchLimit = 6;

    /**
     * reload reads the timeline, the machine's capability and the project's video jobs.
     *
     * The three are independent and each failure is recorded rather than aborting the pass, for
     * the reason the shell's own load gives: a user must still see the shots when the job query is
     * the one that failed.
     */
    const reload = useCallback(async () => {
        if (!activeEpisodeId) {
            setTimeline(null);
            setJobs([]);
            return;
        }
        setLoading(true);
        setError("");
        try {
            const [read, machine, all] = await Promise.all([readTimeline({ episodeId: activeEpisodeId }), mediaCapability(), listJobs({ limit: 200 })]);
            setTimeline(read);
            setCapability(machine);
            // The scope filter is applied HERE because the binding's request has no project
            // field: ListJobsRequest is {statuses, activeOnly, limit, offset}. Saying "this
            // project's jobs" about an unfiltered list would be a claim the query did not make.
            setJobs(all.filter((job) => job.projectId === projectId && VIDEO_JOB_TYPES.includes(job.jobType)));
        } catch (failure) {
            setError(failure instanceof Error ? failure.message : t("studio.video.loadFailed"));
            setTimeline(null);
            setJobs([]);
        } finally {
            setLoading(false);
        }
    }, [activeEpisodeId, projectId, t]);

    useEffect(() => {
        void reload();
    }, [reload]);

    /**
     * refreshJobs re-reads only the job list.
     *
     * It is the read the refresh control makes: a video job runs for minutes and its status is the
     * one thing that changes, while the timeline and the machine's capability do not. A full reload
     * would re-read two answers nobody asked about.
     *
     * It is a MANUAL refresh rather than a timer or a subscription, and that is stated rather than
     * left implicit. A timer would issue provider-backed reads on a clock the user did not ask for,
     * and the job event stream (`onJobChanged`, which the Job Center uses) delivers canvas-side
     * convenience notifications whose absence this section must not depend on — so a user who wants
     * the current state asks for it. The status column is a read of stored rows either way, so a
     * stale row shows a stale status rather than a wrong one.
     */
    const refreshJobs = useCallback(async () => {
        setRefreshing(true);
        try {
            const all = await listJobs({ limit: 200 });
            setJobs(all.filter((job) => job.projectId === projectId && VIDEO_JOB_TYPES.includes(job.jobType)));
        } catch (failure) {
            setError(failure instanceof Error ? failure.message : t("studio.video.loadFailed"));
        } finally {
            setRefreshing(false);
        }
    }, [projectId, t]);

    const shots = timeline?.shots ?? [];
    const selectedShot = shots.find((shot) => shot.shotId === selectedShotId) || null;

    /**
     * The selected shot's approved frame, loaded once per shot.
     *
     * The bytes come from the shot's own `mediaHash`, which the timeline already carries: there is no
     * second read of the board and no new call, and a shot whose media is missing produces a null frame
     * with a reason rather than a submission without one.
     */
    useEffect(() => {
        const hash = selectedShot?.mediaHash ?? "";
        if (!hash) {
            setFrame(null);
            setFrameError("");
            return;
        }
        let cancelled = false;
        setLoadingFrame(true);
        setFrameError("");
        void loadShotFrame(hash)
            .then((loaded) => {
                if (cancelled) return;
                setFrame(loaded);
                if (!loaded) {
                    // The reason is stated because the alternative — a picker whose switch does nothing —
                    // is what a user would otherwise have to interpret. It is not an error the section
                    // reports: a frame that cannot be read disables the frame controls and the shot can
                    // still be generated without one.
                    setFrameError(t("studio.video.frameUnreadable"));
                }
            })
            .catch(() => {
                if (!cancelled) {
                    setFrame(null);
                    setFrameError(t("studio.video.frameUnreadable"));
                }
            })
            .finally(() => {
                if (!cancelled) setLoadingFrame(false);
            });
        return () => {
            cancelled = true;
        };
    }, [selectedShot?.mediaHash, t]);

    /**
     * The two frame choices are reset when the shot changes.
     *
     * Carrying them over would send one shot's frame for another: the choice names THIS shot's frame,
     * and a shot with no approved media has none to send.
     */
    useEffect(() => {
        setFirstFrameChoice(false);
        setLastFrameChoice(false);
    }, [selectedShotId]);

    /**
     * submit sends the shot's video request.
     *
     * THE MODEL AND PROVIDER COME FROM THE PROJECT'S OWN CONFIGURATION, which is the only place
     * they are configured in this build: the settings drawer's video model is stored as
     * `"<channelId>::<model>"`, and the Go gateway wants a provider id and a bare model name —
     * sending the encoded string as the API model name would ask the provider for a model
     * literally named `default::grok-imagine-video`. So the selection is DECODED here through the
     * same shared helper the secure image path uses.
     */
    const submit = async () => {
        if (!selectedShot || !activeEpisode) return;
        const selection = config.videoModel || config.model;
        const decoded = decodeModelSelection(selection);
        const channelId = channelIdForModel(config, selection);
        if (!decoded.model || !channelId) {
            message.error(t("studio.video.modelRequired"));
            return;
        }
        if (prompt.trim() === "") {
            // The core refuses an empty prompt too; not sending one turns a form mistake into a
            // round trip.
            message.error(t("studio.video.promptRequired"));
            return;
        }
        setSubmitting(true);
        try {
            // THE FRAMES ARE SENT ONLY WHEN THEY LOADED. A choice whose bytes are absent is refused
            // here rather than sent: an empty `firstFrame` and an absent one are different requests, and
            // the provider reads the first as a zero-byte image — the defect WP-28 found inside the Go
            // adapter, in its other form.
            const wantsFirst = firstFrameChoice && frame !== null;
            const wantsLast = lastFrameChoice && frame !== null;
            if ((firstFrameChoice || lastFrameChoice) && frame === null) {
                message.error(t("studio.video.frameUnreadable"));
                setSubmitting(false);
                return;
            }
            const job = await submitVideoJob({
                projectId,
                episodeId: activeEpisode.id,
                shotId: selectedShot.shotId,
                providerId: channelId,
                model: decoded.model,
                prompt: prompt.trim(),
                seconds,
                // The fields are OMITTED when a frame was not chosen, because `omitempty` on the Go side
                // means an empty string never travels — so a caller that sent `firstFrame: ""` would be
                // sending the same request as one that sent nothing, and the comment would claim a
                // frame that is not there.
                firstFrame: wantsFirst ? frame!.dataUrl : undefined,
                firstFrameMime: wantsFirst ? frame!.mime : undefined,
                lastFrame: wantsLast ? frame!.dataUrl : undefined,
                lastFrameMime: wantsLast ? frame!.mime : undefined,
            } as never);
            message.success(t("studio.video.submitted", { job: job.id.slice(0, 8) }));
            setPrompt("");
            await refreshJobs();
            onChanged();
        } catch (failure) {
            message.error(failure instanceof Error ? failure.message : t("studio.video.submitFailed"));
        } finally {
            setSubmitting(false);
        }
    };

    /**
     * submitBatch generates for every selected shot.
     *
     * The result is SHOWN rather than toasted: a batch can half-succeed, and a message that said
     * "submitted" for a run whose third shot was refused would be the report lying about what
     * happened. The per-shot outcome is rendered below the button.
     */
    const submitBatch = async () => {
        if (!activeEpisode) return;
        const shotIDs = Object.keys(batchSelection).filter((shotId) => batchSelection[shotId]);
        if (shotIDs.length === 0) return;
        const selection = config.videoModel || config.model;
        const decoded = decodeModelSelection(selection);
        const channelId = channelIdForModel(config, selection);
        if (!decoded.model || !channelId) {
            message.error(t("studio.video.modelRequired"));
            return;
        }
        setBatching(true);
        try {
            const result = await submitVideoBatch({
                projectId,
                episodeId: activeEpisode.id,
                providerId: channelId,
                model: decoded.model,
                // The shared prompt is optional for a batch: one prompt cannot describe six different
                // shots, and the core's own default says what the request is rather than sending none.
                prompt: prompt.trim() || undefined,
                seconds,
                shotIds: shotIDs,
            } as never);
            setBatchResult(result);
            // The selection is cleared only on a submission that reported something: a request refused
            // WHOLESALE throws, and losing the user's selection to a failure they have to fix would make
            // them pick six shots again.
            if (result.submitted.length > 0) {
                setBatchSelection({});
                await refreshJobs();
                onChanged();
            }
        } catch (failure) {
            message.error(failure instanceof Error ? failure.message : t("studio.video.batchFailed"));
        } finally {
            setBatching(false);
        }
    };

    const selectedCount = Object.values(batchSelection).filter(Boolean).length;

    const shotColumns: ColumnsType<desktop.TimelineShotDTO> = [
        { title: t("studio.video.ordinal"), dataIndex: "ordinal", key: "ordinal", width: 70 },
        {
            title: t("studio.video.duration"),
            dataIndex: "durationMs",
            key: "duration",
            width: 120,
            // Seconds with one decimal, because a shot's length is stated in seconds everywhere
            // else in this build and the timeline stores milliseconds.
            render: (value: number) => (value / 1000).toFixed(1),
        },
        {
            title: t("studio.video.mediaLabel"),
            key: "media",
            render: (_value, row) =>
                row.mediaVersionId ? (
                    <Space size="small">
                        <Tag color="green">{t("studio.video.mediaApproved")}</Tag>
                        {/* The kind is the asset's own type: a frame and a clip are different
                            things to compose, and the export reads the same column. */}
                        {row.mediaKind ? <Tag>{t(`studio.assetKind.${row.mediaKind}`, { defaultValue: row.mediaKind })}</Tag> : null}
                    </Space>
                ) : (
                    // No approved media is the ordinary state of a board under construction, and
                    // it is what refuses an export. Saying "none" is not a failure.
                    <span className="text-xs text-stone-500">{t("studio.video.mediaMissing")}</span>
                ),
        },
        {
            title: t("studio.video.audioLabel"),
            key: "audio",
            width: 140,
            render: (_value, row) => (row.hasAudio ? <Tag color="green">{t("studio.video.audioApproved")}</Tag> : <span className="text-xs text-stone-500">{t("studio.video.audioMissing")}</span>),
        },
        {
            title: t("studio.video.submit"),
            key: "actions",
            width: 160,
            render: (_value, row) => (
                <Button size="small" type={row.shotId === selectedShotId ? "primary" : "default"} data-testid={`studio-video-select-${row.shotId}`} onClick={() => setSelectedShotId(row.shotId)}>
                    {t("studio.video.selectShot")}
                </Button>
            ),
        },
    ];

    const jobColumns: ColumnsType<desktop.JobDTO> = [
        { title: t("studio.video.jobId"), dataIndex: "id", key: "id", width: 120, render: (value: string) => <code className="text-xs">{value.slice(0, 8)}</code> },
        { title: t("studio.video.jobEntity"), dataIndex: "entityId", key: "entityId", width: 160, render: (value: string) => <code className="text-xs">{value.slice(0, 12) || "—"}</code> },
        {
            title: t("studio.video.jobStatus"),
            dataIndex: "status",
            key: "status",
            width: 160,
            render: (value: string, row) => (
                <Space size="small">
                    <Tag color={jobStatusColour(value)}>{t(`jobs.status.${value}`, { defaultValue: value })}</Tag>
                    {row.progress > 0 && !isFinished(value) ? <span className="text-xs text-stone-500">{row.progress}%</span> : null}
                </Space>
            ),
        },
        { title: t("studio.video.jobAttempts"), key: "attempts", width: 110, render: (_value, row) => `${row.attemptCount}/${row.maxAttempts}` },
        { title: t("studio.video.jobError"), dataIndex: "errorCode", key: "errorCode", width: 160, render: (value?: string) => value || "—" },
        {
            title: t("studio.video.jobFiles"),
            key: "files",
            render: (_value, row) =>
                row.resultFiles && row.resultFiles.length > 0 ? (
                    // The storage keys are shown as hashes: they are content addresses, and this
                    // section must not present them as paths, because they are not.
                    <ul className="space-y-0.5">
                        {row.resultFiles.map((file) => (
                            <li key={file.storageKey} className="text-xs">
                                <code>{file.storageKey.slice(0, 12)}…</code> <span className="text-stone-500">{file.mime}</span>
                            </li>
                        ))}
                    </ul>
                ) : row.resultRemoteOnly ? (
                    <span className="text-xs text-stone-500">{t("studio.video.resultRemoteOnly")}</span>
                ) : (
                    <span className="text-xs text-stone-500">—</span>
                ),
        },
    ];

    if (episodes.length === 0) {
        return <Empty description={t("studio.video.selectEpisode")} />;
    }

    // The SHELL's no-core notice is about the drama binding. A build with the drama surface and no
    // media surface reaches this body, and every read below would then answer with an empty result —
    // which would render "the episode has no shots" about a question nobody asked. So the section
    // carries its own guard for that partial case, exactly as the memory center does.
    if (!bindingsAvailable) {
        return <Alert type="info" showIcon data-testid="studio-video-no-media-core" message={t("studio.video.noMediaCoreTitle")} description={t("studio.video.noMediaCoreBody")} />;
    }

    return (
        <div className="space-y-8" data-testid="studio-video">
            <section>
                <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
                    <h2 className="text-lg font-medium">{t("studio.video.shots")}</h2>
                    <Space wrap>
                        <Select
                            className="min-w-56"
                            value={activeEpisodeId || undefined}
                            placeholder={t("studio.video.selectEpisode")}
                            data-testid="studio-video-episode"
                            onChange={onSelectEpisode}
                            options={episodes.map((episode) => ({
                                value: episode.id,
                                label: `S${episode.seasonNumber}E${episode.episodeNumber} · ${episode.title}`,
                            }))}
                        />
                        <Button icon={<RefreshCw className="size-4" />} loading={loading} data-testid="studio-video-reload" onClick={() => void reload()}>
                            {t("studio.video.reload")}
                        </Button>
                    </Space>
                </div>

                {/* The diagnostic is the CORE's own sentence, shown verbatim. It names what is
                    missing on this machine, and a translation would paraphrase a fact about the
                    machine into a claim about the interface. */}
                {capability && !capability.exportAvailable ? (
                    <Alert className="mb-3" type="warning" showIcon message={t("studio.video.exportUnavailable")} description={capability.diagnostic || t("studio.video.exportUnavailableNoDiagnostic")} data-testid="studio-video-capability" />
                ) : null}

                {error ? <Alert className="mb-3" type="error" showIcon message={error} data-testid="studio-video-error" /> : null}

                {shots.length === 0 && !loading ? (
                    // An empty timeline is not "no shots": the core REFUSES to answer an episode
                    // with no approved storyboard, and that refusal arrives as an error. So this
                    // state is only reached when a board exists and holds no rows, or when the
                    // browser has no core to ask.
                    <Empty description={t("studio.video.noShots")} />
                ) : (
                    <Table<desktop.TimelineShotDTO>
                        rowKey="itemId"
                        size="small"
                        loading={loading}
                        pagination={false}
                        columns={shotColumns}
                        dataSource={shots}
                        data-testid="studio-video-shots"
                        rowClassName={(row) => (row.shotId === selectedShotId ? "bg-stone-50 dark:bg-stone-900" : "")}
                        // The selection column appears ONLY when this build has the batch command: a
                        // checkbox that could not act would be a control offering work it cannot do.
                        rowSelection={
                            batchAvailable
                                ? {
                                      selectedRowKeys: shots.filter((shot) => batchSelection[shot.shotId]).map((shot) => shot.itemId),
                                      onChange: (_keys, rows) => {
                                          const next: Record<string, boolean> = {};
                                          for (const row of rows) next[row.shotId] = true;
                                          setBatchSelection(next);
                                      },
                                      getCheckboxProps: (row) => ({ disabled: false, name: row.shotId }),
                                  }
                                : undefined
                        }
                    />
                )}

                {/* THE BATCH, beside the single-shot form rather than replacing it: submitting one shot
                    is the ordinary act, and a batch is what a user does once the board is settled. */}
                {batchAvailable && shots.length > 0 ? (
                    <div className="mt-3" data-testid="studio-video-batch">
                        <Space wrap>
                            <Button icon={<Layers className="size-4" />} loading={batching} disabled={selectedCount === 0} data-testid="studio-video-batch-submit" onClick={() => void submitBatch()}>
                                {t("studio.video.batchSubmit", { count: selectedCount })}
                            </Button>
                            <Typography.Text type="secondary">{t("studio.video.batchHint", { max: batchLimit })}</Typography.Text>
                        </Space>
                        {batchResult ? (
                            <div className="mt-2 text-xs" data-testid="studio-video-batch-result">
                                {/* The two lists are rendered SEPARATELY and neither is hidden when
                                    empty: a run that submitted four and refused one must not look like
                                    a run that submitted four. */}
                                {batchResult.submitted.map((item) => (
                                    <div key={item.shotId} className="text-stone-600 dark:text-stone-400">
                                        {t(item.duplicate ? "studio.video.batchAlready" : "studio.video.batchQueued", { shot: item.shotId.slice(0, 8), job: (item.jobId ?? "").slice(0, 8) })}
                                    </div>
                                ))}
                                {batchResult.refused.map((item) => (
                                    <div key={item.shotId} className="text-red-600 dark:text-red-400">
                                        {t("studio.video.batchRefused", { shot: item.shotId.slice(0, 8), reason: item.refused })}
                                    </div>
                                ))}
                            </div>
                        ) : null}
                    </div>
                ) : null}

                {timeline && shots.length > 0 ? (
                    <Typography.Paragraph className="mt-2 text-xs text-stone-500">
                        {t("studio.video.totals", {
                            seconds: (timeline.totalDurationMs / 1000).toFixed(1),
                            missing: timeline.missingMedia,
                            cues: timeline.cueCount,
                        })}
                    </Typography.Paragraph>
                ) : null}
            </section>

            <section className="rounded-xl border border-stone-200 p-4 dark:border-stone-800">
                <h2 className="text-base font-medium">{t("studio.video.requestTitle")}</h2>
                <Typography.Paragraph className="mt-1 text-xs text-stone-500">{t("studio.video.requestHint")}</Typography.Paragraph>
                <Space wrap align="end">
                    <label>
                        <span className="mb-1 block text-sm">{t("studio.video.shotLabel")}</span>
                        <Select
                            className="min-w-56"
                            value={selectedShotId || undefined}
                            placeholder={t("studio.video.selectShot")}
                            data-testid="studio-video-shot"
                            onChange={(value: string) => setSelectedShotId(value)}
                            options={shots.map((shot) => ({
                                value: shot.shotId,
                                label: `#${shot.ordinal} · ${(shot.durationMs / 1000).toFixed(1)}s${shot.mediaVersionId ? ` · ${t("studio.video.mediaApproved")}` : ""}`,
                            }))}
                        />
                    </label>
                    <label>
                        <span className="mb-1 block text-sm">{t("studio.video.secondsLabel")}</span>
                        <InputNumber min={1} max={60} value={seconds} data-testid="studio-video-seconds" onChange={(value) => setSeconds(typeof value === "number" ? value : 4)} />
                    </label>
                    <Button type="primary" icon={<Play className="size-4" />} loading={submitting} disabled={!selectedShot} data-testid="studio-video-submit" onClick={() => void submit()}>
                        {t("studio.video.submit")}
                    </Button>
                </Space>
                <label className="mt-3 block">
                    <span className="mb-1 block text-sm">{t("studio.video.promptLabel")}</span>
                    <Input.TextArea rows={3} value={prompt} maxLength={2000} data-testid="studio-video-prompt" placeholder={t("studio.video.promptPlaceholder")} onChange={(event) => setPrompt(event.target.value)} />
                </label>
                {/* THE FIRST AND LAST FRAME, from the shot's OWN approved panel image.
                    This is the picker WP-26's comment said was missing — and finding it missing is what
                    exposed that the pipe underneath was broken too: the adapter read `ImageInput.Bytes`
                    while the runner fills `Data`, so every reference travelled as an empty data URL.
                    The pipe is fixed and this control is what uses it.
                    The source is the approved frame rather than a file chooser: a frame in this build
                    has an exact source, and "any image at all" is a different capability. */}
                <div className="mt-3" data-testid="studio-video-frames">
                    <span className="mb-1 block text-sm">{t("studio.video.framesLabel")}</span>
                    {!selectedShot ? (
                        <Typography.Text type="secondary">{t("studio.video.framesPickShot")}</Typography.Text>
                    ) : !selectedShot.mediaHash ? (
                        // No approved media is the ordinary state of a board under construction, and the
                        // control says THAT rather than offering a switch that cannot do anything.
                        <Typography.Text type="secondary">{t("studio.video.framesNoMedia")}</Typography.Text>
                    ) : loadingFrame ? (
                        <Typography.Text type="secondary">{t("studio.video.framesLoading")}</Typography.Text>
                    ) : frame === null ? (
                        <Typography.Text type="secondary">{frameError || t("studio.video.frameUnreadable")}</Typography.Text>
                    ) : (
                        <Space wrap align="start">
                            {/* The thumbnail IS the choice: a user picking a first frame should see the
                                frame, and this one is the exact image the request will carry. */}
                            <img src={frame.dataUrl} alt={t("studio.video.framesLabel")} className="h-16 w-auto rounded border border-stone-200 dark:border-stone-700" data-testid="studio-video-frame-thumb" />
                            <Space direction="vertical" size={4}>
                                <label className="flex items-center gap-2 text-sm">
                                    <Switch size="small" checked={firstFrameChoice} data-testid="studio-video-first-frame" onChange={(checked) => setFirstFrameChoice(checked)} />
                                    {t("studio.video.useFirstFrame")}
                                </label>
                                <label className="flex items-center gap-2 text-sm">
                                    <Switch size="small" checked={lastFrameChoice} data-testid="studio-video-last-frame" onChange={(checked) => setLastFrameChoice(checked)} />
                                    {t("studio.video.useLastFrame")}
                                </label>
                            </Space>
                        </Space>
                    )}
                </div>
                {/* What is NOT offered, said rather than left to be discovered: `references` stays
                    unsent because there is no picker for a style reference, and the size field has no
                    control either. Both are the binding's to accept and this section's to leave out. */}
                <Typography.Paragraph className="mt-3 text-xs text-stone-500">{t("studio.video.framesNote")}</Typography.Paragraph>
            </section>

            <section>
                <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
                    <h2 className="text-lg font-medium">{t("studio.video.jobs")}</h2>
                    <Button size="small" icon={<RefreshCw className="size-4" />} loading={refreshing} data-testid="studio-video-refresh-jobs" onClick={() => void refreshJobs()}>
                        {t("studio.video.refresh")}
                    </Button>
                </div>
                {jobs.length === 0 ? <Empty description={t("studio.video.noJobs")} /> : <Table<desktop.JobDTO> rowKey="id" size="small" pagination={false} columns={jobColumns} dataSource={jobs} data-testid="studio-video-jobs" />}
            </section>
        </div>
    );
}

/** isFinished reports whether a job status ends a job's life. */
function isFinished(status: string): boolean {
    return ["succeeded", "remote_only", "failed", "cancelled", "orphaned"].includes(status);
}

/** jobStatusColour maps a job status to a tag colour. Text always accompanies it. */
function jobStatusColour(status: string): string {
    switch (status) {
        case "succeeded":
            return "green";
        case "failed":
        case "orphaned":
            return "red";
        case "cancelled":
            return "default";
        case "remote_only":
            return "gold";
        default:
            return "blue";
    }
}
