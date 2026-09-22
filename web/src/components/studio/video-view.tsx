import { useCallback, useEffect, useMemo, useState } from "react";
import { Alert, App, Button, Empty, Input, InputNumber, Select, Space, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Play, RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";

import { listJobs, submitVideoJob } from "@/services/desktop/jobs";
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

    const activeEpisode = useMemo(() => episodes.find((episode) => episode.id === activeEpisodeId) || null, [episodes, activeEpisodeId]);

    const bindingsAvailable = isMediaBindingsAvailable();

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
            const job = await submitVideoJob({
                projectId,
                episodeId: activeEpisode.id,
                shotId: selectedShot.shotId,
                providerId: channelId,
                model: decoded.model,
                prompt: prompt.trim(),
                seconds,
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
                    />
                )}

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
                {/* The size and provider fields the binding accepts but this section does NOT
                    send: `references`, `firstFrame`/`lastFrame` and their MIME pairs carry base64
                    image bytes, and this section has no picker that could produce them. Sending an
                    empty array would be indistinguishable from a request that meant "no references",
                    so the fields are left out and this note says why. */}
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
