import { useCallback, useEffect, useMemo, useState } from "react";
import { Alert, App, Badge, Button, Empty, Progress, Space, Table, Tag, Tooltip } from "antd";
import type { ColumnsType } from "antd/es/table";
import { PauseCircle, PlayCircle, RotateCcw, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";

import {
    cancelJobs,
    getQueueStatus,
    isDesktopJobBindingsAvailable,
    isTerminalJobStatus,
    listJobs,
    onJobChanged,
    pauseQueue,
    resumeQueue,
    retryFailedJobs,
    type JobStatus,
} from "@/services/desktop/jobs";
import type { desktop } from "@/wailsjs/go/models";

/**
 * Job Center: the queue view and the batch actions for persistent jobs.
 *
 * Status is expressed with text plus a tag colour, never colour alone, so the
 * table stays readable for colour-blind users and in greyscale.
 */
export default function JobsPage() {
    const { t } = useTranslation();
    const { message } = App.useApp();
    const [jobs, setJobs] = useState<desktop.JobDTO[]>([]);
    const [summary, setSummary] = useState<desktop.QueueSummaryDTO | null>(null);
    const [selected, setSelected] = useState<string[]>([]);
    const [busy, setBusy] = useState(false);
    const [loading, setLoading] = useState(false);
    const available = isDesktopJobBindingsAvailable();

    const refresh = useCallback(async () => {
        if (!available) return;
        setLoading(true);
        try {
            const [records, status] = await Promise.all([listJobs({ limit: 200 } as never), getQueueStatus()]);
            setJobs(records);
            setSummary(status);
        } catch {
            message.error(t("jobs.loadFailed"));
        } finally {
            setLoading(false);
        }
    }, [available, message, t]);

    useEffect(() => {
        void refresh();
    }, [refresh]);

    // Live updates: the Go side publishes terminal transitions unconditionally,
    // so a finished job always appears even when progress events are coalesced.
    useEffect(() => {
        if (!available) return;
        return onJobChanged(() => {
            void refresh();
        });
    }, [available, refresh]);

    const runBatch = useCallback(
        async (ids: string[], action: (targets: string[]) => Promise<number>, successKey: string) => {
            if (ids.length === 0) return;
            setBusy(true);
            try {
                const count = await action(ids);
                message.success(t(successKey, { count }));
                await refresh();
            } catch {
                message.error(t("jobs.actionFailed"));
            } finally {
                setBusy(false);
            }
        },
        [message, refresh, t],
    );

    const togglePause = useCallback(async () => {
        setBusy(true);
        try {
            if (summary?.paused) await resumeQueue();
            else await pauseQueue();
            await refresh();
        } catch {
            message.error(t("jobs.actionFailed"));
        } finally {
            setBusy(false);
        }
    }, [message, refresh, summary?.paused, t]);

    const selectedActive = useMemo(() => jobs.filter((job) => selected.includes(job.id) && !isTerminalJobStatus(job.status)).map((job) => job.id), [jobs, selected]);
    const selectedRetryable = useMemo(() => jobs.filter((job) => selected.includes(job.id) && (job.status === "failed" || job.status === "orphaned")).map((job) => job.id), [jobs, selected]);

    const columns: ColumnsType<desktop.JobDTO> = [
        {
            title: t("jobs.columns.type"),
            dataIndex: "jobType",
            key: "jobType",
            render: (value: string) => <span className="text-sm">{t(`jobs.types.${value}`, { defaultValue: value })}</span>,
        },
        {
            title: t("jobs.columns.status"),
            dataIndex: "status",
            key: "status",
            render: (value: string, record) => (
                <Space direction="vertical" size={2}>
                    <Tag color={statusColor(value as JobStatus)}>
                        {t(`jobs.status.${value}`, { defaultValue: value })}
                    </Tag>
                    {!isTerminalJobStatus(value) ? <Progress percent={record.progress} size="small" className="w-32" /> : null}
                </Space>
            ),
        },
        {
            title: t("jobs.columns.attempts"),
            key: "attempts",
            render: (_, record) => (
                <span className="text-sm text-stone-600 dark:text-stone-400">
                    {record.attemptCount}/{record.maxAttempts}
                </span>
            ),
        },
        {
            title: t("jobs.columns.error"),
            dataIndex: "errorCode",
            key: "errorCode",
            render: (value: string, record) =>
                value ? (
                    <Tooltip title={t("jobs.errorHint")}>
                        <span className="text-sm">{t(`jobs.errorCodes.${value}`, { defaultValue: value })}</span>
                        {record.resultRemoteOnly ? <Tag className="ml-1">{t("jobs.remoteOnlyTag")}</Tag> : null}
                    </Tooltip>
                ) : record.resultRemoteOnly ? (
                    <Tag>{t("jobs.remoteOnlyTag")}</Tag>
                ) : (
                    <span className="text-stone-400">—</span>
                ),
        },
        {
            title: t("jobs.columns.updated"),
            dataIndex: "updatedAt",
            key: "updatedAt",
            render: (value: string) => <span className="text-xs text-stone-500">{value ? new Date(value).toLocaleString() : "—"}</span>,
        },
    ];

    if (!available) {
        return (
            <div className="mx-auto w-full max-w-5xl p-6">
                <Alert type="info" showIcon message={t("jobs.desktopOnlyTitle")} description={t("jobs.desktopOnlyBody")} />
            </div>
        );
    }

    return (
        <div className="mx-auto w-full max-w-6xl p-6">
            <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
                <div>
                    <h1 className="text-lg font-semibold">{t("jobs.title")}</h1>
                    <p className="mt-0.5 text-xs text-stone-500">{t("jobs.subtitle")}</p>
                </div>
                <Space wrap>
                    <Badge status="processing" text={t("jobs.summary.active", { count: summary?.activeTotal ?? 0 })} />
                    <Badge status="success" text={t("jobs.summary.succeeded", { count: summary?.succeeded ?? 0 })} />
                    <Badge status="error" text={t("jobs.summary.failed", { count: summary?.failed ?? 0 })} />
                    <Button icon={summary?.paused ? <PlayCircle className="size-4" /> : <PauseCircle className="size-4" />} onClick={() => void togglePause()} loading={busy}>
                        {summary?.paused ? t("jobs.resume") : t("jobs.pause")}
                    </Button>
                    <Button
                        icon={<Trash2 className="size-4" />}
                        disabled={selectedActive.length === 0}
                        loading={busy}
                        onClick={() => void runBatch(selectedActive, cancelJobs, "jobs.cancelledCount")}
                    >
                        {t("jobs.cancelSelected")}
                    </Button>
                    <Button
                        icon={<RotateCcw className="size-4" />}
                        disabled={selectedRetryable.length === 0}
                        loading={busy}
                        onClick={() => void runBatch(selectedRetryable, retryFailedJobs, "jobs.retriedCount")}
                    >
                        {t("jobs.retryFailed")}
                    </Button>
                </Space>
            </div>

            {summary?.paused ? <Alert className="mb-3" type="warning" showIcon message={t("jobs.pausedNotice")} /> : null}

            <Table<desktop.JobDTO>
                rowKey="id"
                size="small"
                loading={loading}
                columns={columns}
                dataSource={jobs}
                rowSelection={{
                    selectedRowKeys: selected,
                    onChange: (keys) => setSelected(keys as string[]),
                }}
                pagination={{ pageSize: 25, showSizeChanger: false, showTotal: (total) => t("jobs.totalCount", { total }) }}
                locale={{
                    emptyText: <Empty description={t("jobs.empty")} />,
                }}
            />

            <p className="mt-3 text-xs text-stone-500">{t("jobs.footnote")}</p>
        </div>
    );
}

/** Maps a job status to a tag colour. Text always accompanies the colour. */
function statusColor(status: JobStatus): string {
    switch (status) {
        case "succeeded":
            return "green";
        case "remote_only":
            return "cyan";
        case "failed":
        case "orphaned":
            return "red";
        case "cancelled":
            return "default";
        case "retry_wait":
            return "orange";
        case "waiting_remote":
        case "downloading":
        case "verifying":
            return "blue";
        case "running":
        case "recovering":
            return "processing";
        default:
            return "default";
    }
}
