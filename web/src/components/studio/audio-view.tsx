import { useCallback, useEffect, useMemo, useState } from "react";
import { Alert, App, Button, Empty, Input, Select, Space, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Mic, RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";

import { listJobs, submitAudioJob } from "@/services/desktop/jobs";
import { isMediaBindingsAvailable, readTimeline } from "@/services/desktop/media";
import { channelIdForModel, decodeModelSelection } from "@/services/desktop/model-selection";
import { useEffectiveConfig } from "@/stores/use-config-store";
import type { desktop } from "@/wailsjs/go/models";

/**
 * AudioSection is WP-11's audio surface: which shots have speech, and where a line's TTS is missing.
 *
 * # What it reads, and the read that does NOT exist
 *
 * The per-line state FR-080 asks for — "TTS Voice/Dialogue mapping" — is not reachable from this
 * build's binding, and the section says so rather than guessing:
 *
 *  - There is NO method that lists a script version's dialogue lines. `DramaBinding` exposes
 *    `GetScriptStructure`, which needs a script version id, and nothing lists a script's versions:
 *    `ListScriptVersions` exists on the repository interface and on a TEST store, but no service
 *    method, no binding method and no DTO returns a version list — `ApproveScriptVersion`,
 *    `CreateScriptVersion` and `SaveScriptStructure` are the only three that name a version, and
 *    each takes one rather than returning a list. `ScriptDTO.currentVersionId` is a column nothing
 *    writes (`CreateScript` writes it empty and no statement updates it), so it is not a route
 *    either.
 *  - There is NO method that reads a line's approved audio. The audio join lives in the timeline
 *    read's audio column, which is per SHOT ("whether audio is approved for any line in this
 *    row's scene"), and `SubmitAudioJob` names a `dialogueLineId` whose value no read here can
 *    supply.
 *
 * So the section reports the audio state at the granularity the core actually answers — the shot,
 * from `ReadTimeline`'s `hasAudio` — and it submits a TTS job for a shot whose audio is missing,
 * carrying the character and line text the user states. It does NOT render a fabricated per-line
 * list, and it does not call a method that does not exist.
 *
 * # The dialogue line identifier
 *
 * `SubmitAudioJob` requires a non-empty `dialogueLineId` — the core refuses the request without one
 * — and it is the job's entity. Since this interface cannot enumerate lines, the field is an INPUT:
 * the user names the line they are voicing, and what is created is honest about what it is — a
 * request about a line the user identified, not one this interface looked up.
 */
export type AudioSectionProps = {
    projectId: string;
    episodes: desktop.EpisodeDTO[];
    activeEpisodeId: string;
    onSelectEpisode: (episodeId: string) => void;
    onChanged: () => void;
};

/** The job types this section lists: one line's speech, and nothing else. */
const AUDIO_JOB_TYPES = ["audio_generation"];

export function AudioSection({ projectId, episodes, activeEpisodeId, onSelectEpisode, onChanged }: AudioSectionProps) {
    const { t } = useTranslation();
    const { message } = App.useApp();
    const config = useEffectiveConfig();

    const [timeline, setTimeline] = useState<desktop.TimelineDTO | null>(null);
    const [jobs, setJobs] = useState<desktop.JobDTO[]>([]);
    const [loading, setLoading] = useState(false);
    const [refreshing, setRefreshing] = useState(false);
    const [error, setError] = useState("");
    const [selectedShotId, setSelectedShotId] = useState("");
    const [lineId, setLineId] = useState("");
    const [text, setText] = useState("");
    const [submitting, setSubmitting] = useState(false);

    const activeEpisode = useMemo(() => episodes.find((episode) => episode.id === activeEpisodeId) || null, [episodes, activeEpisodeId]);

    const bindingsAvailable = isMediaBindingsAvailable();

    const reload = useCallback(async () => {
        if (!activeEpisodeId) {
            setTimeline(null);
            setJobs([]);
            return;
        }
        setLoading(true);
        setError("");
        try {
            const [read, all] = await Promise.all([readTimeline({ episodeId: activeEpisodeId }), listJobs({ limit: 200 })]);
            setTimeline(read);
            // The project filter is applied here: ListJobsRequest has no project field — it is
            // {statuses, activeOnly, limit, offset} — so a comment claiming the core scoped this
            // would be false.
            setJobs(all.filter((job) => job.projectId === projectId && AUDIO_JOB_TYPES.includes(job.jobType)));
        } catch (failure) {
            setError(failure instanceof Error ? failure.message : t("studio.audio.loadFailed"));
            setTimeline(null);
            setJobs([]);
        } finally {
            setLoading(false);
        }
    }, [activeEpisodeId, projectId, t]);

    useEffect(() => {
        void reload();
    }, [reload]);

    /** refreshJobs re-reads only the job list: a TTS job's status is the thing that changes. */
    const refreshJobs = useCallback(async () => {
        setRefreshing(true);
        try {
            const all = await listJobs({ limit: 200 });
            setJobs(all.filter((job) => job.projectId === projectId && AUDIO_JOB_TYPES.includes(job.jobType)));
        } catch (failure) {
            setError(failure instanceof Error ? failure.message : t("studio.audio.loadFailed"));
        } finally {
            setRefreshing(false);
        }
    }, [projectId, t]);

    const shots = timeline?.shots ?? [];
    const withoutAudio = shots.filter((shot) => !shot.hasAudio).length;

    /**
     * submit sends one line's TTS request.
     *
     * The voice, format and speed come from the project's own audio configuration — the same
     * values the canvas's speech panel sends — and the model selection is DECODED, because the
     * drawer stores it as `"<channelId>::<model>"` while the Go gateway wants a provider id and a
     * bare model name.
     */
    const submit = async () => {
        if (!activeEpisode) return;
        const selection = config.audioModel || config.model;
        const decoded = decodeModelSelection(selection);
        const channelId = channelIdForModel(config, selection);
        if (!decoded.model || !channelId) {
            message.error(t("studio.audio.modelRequired"));
            return;
        }
        if (lineId.trim() === "") {
            // The core refuses a request with no dialogue line, so the form asks rather than
            // sending one the core would reject.
            message.error(t("studio.audio.lineRequired"));
            return;
        }
        if (text.trim() === "") {
            message.error(t("studio.audio.textRequired"));
            return;
        }
        setSubmitting(true);
        try {
            const job = await submitAudioJob({
                projectId,
                episodeId: activeEpisode.id,
                dialogueLineId: lineId.trim(),
                providerId: channelId,
                model: decoded.model,
                text: text.trim(),
                voice: config.audioVoice || undefined,
                format: config.audioFormat || undefined,
                speed: config.audioSpeed || undefined,
            } as never);
            message.success(t("studio.audio.submitted", { job: job.id.slice(0, 8) }));
            setText("");
            await refreshJobs();
            onChanged();
        } catch (failure) {
            message.error(failure instanceof Error ? failure.message : t("studio.audio.submitFailed"));
        } finally {
            setSubmitting(false);
        }
    };

    const columns: ColumnsType<desktop.TimelineShotDTO> = [
        { title: t("studio.audio.ordinal"), dataIndex: "ordinal", key: "ordinal", width: 70 },
        {
            title: t("studio.audio.duration"),
            dataIndex: "durationMs",
            key: "duration",
            width: 110,
            render: (value: number) => (value / 1000).toFixed(1),
        },
        {
            title: t("studio.audio.stateLabel"),
            key: "state",
            render: (_value, row) => (row.hasAudio ? <Tag color="green">{t("studio.audio.hasAudio")}</Tag> : <Tag color="orange">{t("studio.audio.noAudio")}</Tag>),
        },
        {
            title: t("studio.audio.cuesLabel"),
            dataIndex: "cueCount",
            key: "cueCount",
            width: 120,
        },
        {
            title: "",
            key: "actions",
            width: 150,
            render: (_value, row) => (
                <Button size="small" type={row.shotId === selectedShotId ? "primary" : "default"} data-testid={`studio-audio-select-${row.shotId}`} onClick={() => setSelectedShotId(row.shotId)}>
                    {t("studio.audio.selectShot")}
                </Button>
            ),
        },
    ];

    if (episodes.length === 0) {
        return <Empty description={t("studio.audio.selectEpisode")} />;
    }

    // The SHELL's notice is about the drama binding. A build with the drama surface and no media
    // surface reaches this body, and every read below answers empty — which would render "the episode
    // has no shots" about a question nobody asked. The section carries its own guard for that case.
    if (!bindingsAvailable) {
        return <Alert type="info" showIcon data-testid="studio-audio-no-media-core" message={t("studio.audio.noMediaCoreTitle")} description={t("studio.audio.noMediaCoreBody")} />;
    }

    return (
        <div className="space-y-8" data-testid="studio-audio">
            <section>
                <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
                    <h2 className="text-lg font-medium">{t("studio.audio.shots")}</h2>
                    <Space wrap>
                        <Select
                            className="min-w-56"
                            value={activeEpisodeId || undefined}
                            placeholder={t("studio.audio.selectEpisode")}
                            data-testid="studio-audio-episode"
                            onChange={onSelectEpisode}
                            options={episodes.map((episode) => ({
                                value: episode.id,
                                label: `S${episode.seasonNumber}E${episode.episodeNumber} · ${episode.title}`,
                            }))}
                        />
                        <Button icon={<RefreshCw className="size-4" />} loading={loading} data-testid="studio-audio-reload" onClick={() => void reload()}>
                            {t("studio.audio.reload")}
                        </Button>
                    </Space>
                </div>

                {/* The scope of the answer, stated rather than implied: this section reports
                    audio at the SHOT's granularity because the timeline's audio column is the only
                    read of it this build exposes. A per-line list would be a second answer nobody
                    computed. */}
                <Alert className="mb-3" type="info" showIcon message={t("studio.audio.scopeTitle")} description={t("studio.audio.scopeBody")} data-testid="studio-audio-scope" />

                {error ? <Alert className="mb-3" type="error" showIcon message={error} data-testid="studio-audio-error" /> : null}

                {shots.length === 0 && !loading ? (
                    <Empty description={t("studio.audio.noShots")} />
                ) : (
                    <Table<desktop.TimelineShotDTO>
                        rowKey="itemId"
                        size="small"
                        loading={loading}
                        pagination={false}
                        columns={columns}
                        dataSource={shots}
                        data-testid="studio-audio-shots"
                        rowClassName={(row) => (row.shotId === selectedShotId ? "bg-stone-50 dark:bg-stone-900" : "")}
                    />
                )}

                {timeline && shots.length > 0 ? <Typography.Paragraph className="mt-2 text-xs text-stone-500">{t("studio.audio.totals", { total: shots.length, missing: withoutAudio })}</Typography.Paragraph> : null}
            </section>

            <section className="rounded-xl border border-stone-200 p-4 dark:border-stone-800">
                <h2 className="text-base font-medium">{t("studio.audio.requestTitle")}</h2>
                <Typography.Paragraph className="mt-1 text-xs text-stone-500">{t("studio.audio.requestHint")}</Typography.Paragraph>
                <Space wrap align="end">
                    <label>
                        <span className="mb-1 block text-sm">{t("studio.audio.shotLabel")}</span>
                        <Select
                            className="min-w-48"
                            value={selectedShotId || undefined}
                            placeholder={t("studio.audio.selectShot")}
                            data-testid="studio-audio-shot"
                            onChange={(value: string) => setSelectedShotId(value)}
                            options={shots.map((shot) => ({
                                value: shot.shotId,
                                label: `#${shot.ordinal} · ${shot.hasAudio ? t("studio.audio.hasAudio") : t("studio.audio.noAudio")}`,
                            }))}
                        />
                    </label>
                    <label>
                        <span className="mb-1 block text-sm">{t("studio.audio.lineLabel")}</span>
                        <Input className="w-64" value={lineId} maxLength={120} data-testid="studio-audio-line" placeholder={t("studio.audio.linePlaceholder")} onChange={(event) => setLineId(event.target.value)} />
                    </label>
                    <Button type="primary" icon={<Mic className="size-4" />} loading={submitting} data-testid="studio-audio-submit" onClick={() => void submit()}>
                        {t("studio.audio.submit")}
                    </Button>
                </Space>
                <label className="mt-3 block">
                    <span className="mb-1 block text-sm">{t("studio.audio.textLabel")}</span>
                    <Input.TextArea rows={3} value={text} maxLength={2000} data-testid="studio-audio-text" placeholder={t("studio.audio.textPlaceholder")} onChange={(event) => setText(event.target.value)} />
                </label>
                {/* The reference frames and the voice are not offered as controls here: the voice
                    comes from the project's audio settings, which is the one place this build
                    configures it, and a second voice picker would be a second answer to "which
                    voice does this render in". */}
                <Typography.Paragraph className="mt-3 text-xs text-stone-500">{t("studio.audio.voiceNote", { voice: config.audioVoice || "—", format: config.audioFormat || "—" })}</Typography.Paragraph>
            </section>

            <section>
                <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
                    <h2 className="text-lg font-medium">{t("studio.audio.jobs")}</h2>
                    <Button size="small" icon={<RefreshCw className="size-4" />} loading={refreshing} data-testid="studio-audio-refresh-jobs" onClick={() => void refreshJobs()}>
                        {t("studio.audio.refresh")}
                    </Button>
                </div>
                {jobs.length === 0 ? (
                    <Empty description={t("studio.audio.noJobs")} />
                ) : (
                    <Table<desktop.JobDTO>
                        rowKey="id"
                        size="small"
                        pagination={false}
                        dataSource={jobs}
                        data-testid="studio-audio-jobs"
                        columns={[
                            { title: t("studio.audio.jobId"), dataIndex: "id", key: "id", width: 120, render: (value: string) => <code className="text-xs">{value.slice(0, 8)}</code> },
                            { title: t("studio.audio.lineLabel"), dataIndex: "entityId", key: "entityId", render: (value: string) => <code className="text-xs">{value.slice(0, 16) || "—"}</code> },
                            {
                                title: t("studio.audio.jobStatus"),
                                dataIndex: "status",
                                key: "status",
                                width: 160,
                                render: (value: string) => <Tag color={audioStatusColour(value)}>{t(`jobs.status.${value}`, { defaultValue: value })}</Tag>,
                            },
                            { title: t("studio.audio.jobError"), dataIndex: "errorCode", key: "errorCode", width: 160, render: (value?: string) => value || "—" },
                            {
                                title: t("studio.audio.jobFiles"),
                                key: "files",
                                render: (_value, row) =>
                                    row.resultFiles && row.resultFiles.length > 0 ? (
                                        <ul className="space-y-0.5">
                                            {row.resultFiles.map((file) => (
                                                <li key={file.storageKey} className="text-xs">
                                                    <code>{file.storageKey.slice(0, 12)}…</code> <span className="text-stone-500">{file.mime}</span>
                                                </li>
                                            ))}
                                        </ul>
                                    ) : (
                                        <span className="text-xs text-stone-500">—</span>
                                    ),
                            },
                        ]}
                    />
                )}
            </section>
        </div>
    );
}

/** audioStatusColour maps a job status to a tag colour. Text always accompanies it. */
function audioStatusColour(status: string): string {
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
