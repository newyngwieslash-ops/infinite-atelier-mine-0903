import { useCallback, useEffect, useState } from "react";
import { Alert, App, Button, Empty, Popconfirm, Space, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { RefreshCw, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";

import {
    cancelGarbageCollection,
    isGarbageCollectionAvailable,
    previewGarbageListing,
    runGarbageCollection,
} from "@/services/desktop/backup";
import type { desktop } from "@/wailsjs/go/models";

/**
 * GarbageCollectionPanel is FR-160's 「垃圾回收执行前显示将删除内容并支持取消」.
 *
 * # The three clauses, and where each one is
 *
 *   - 执行前显示将删除内容 — the list below IS the preview, rendered before anything is removed.
 *     The list is produced by a read that removes nothing, so opening the panel is safe.
 *   - 确认 — collecting requires the confirmation below, and the binding refuses without it at the
 *     boundary rather than after reading anything. The confirmation is a `Popconfirm` whose text
 *     states what will be removed rather than asking "are you sure" — a user agreeing to a number
 *     and a size can judge, one agreeing to a question cannot.
 *   - 支持取消 — a collection that is running offers a cancel, and the result says whether it was
 *     stopped so a caller does not report a partial run as complete.
 *
 * # What the panel deliberately shows
 *
 * Each candidate's size and MIME, because a user deciding what to delete needs to recognise the
 * objects rather than trust a count. And the TOTAL, because that is the number the decision is
 * actually about: "free 240 MB" is a reason to collect, "remove 31 objects" is not.
 *
 * It does not show project names or paths for the candidates. An object nothing references has no
 * owner to name — that is what "unreferenced" means — so an invented label would be worse than the
 * hash, which at least identifies the row to somebody looking at the database.
 */
export function GarbageCollectionPanel() {
    const { t } = useTranslation();
    const { message } = App.useApp();
    const [available] = useState(() => isGarbageCollectionAvailable());
    const [preview, setPreview] = useState<desktop.GarbagePreviewDTO | null>(null);
    const [busy, setBusy] = useState(false);
    const [running, setRunning] = useState(false);
    const [error, setError] = useState("");

    const reload = useCallback(async () => {
        if (!available) return;
        setBusy(true);
        setError("");
        try {
            const next = await previewGarbageListing();
            setPreview(next);
            setRunning(next.collecting === true);
        } catch (failure) {
            setError(String(failure));
        } finally {
            setBusy(false);
        }
    }, [available]);

    useEffect(() => {
        void reload();
    }, [reload]);

    const collect = useCallback(async () => {
        setBusy(true);
        try {
            const result = await runGarbageCollection(true);
            // The report names both halves: the objects removed and whether the bytes went with them.
            // A collector that could not tell them apart would report freeing space it did not free.
            if (result.cancelled) {
                message.warning(t("config.garbage.cancelled", { count: result.removed?.length ?? 0 }));
            } else {
                message.success(t("config.garbage.collected", {
                    count: result.removed?.length ?? 0,
                    bytes: formatBytes(result.freedBytes ?? 0),
                }));
            }
            await reload();
        } catch (failure) {
            message.error(String(failure));
        } finally {
            setBusy(false);
            setRunning(false);
        }
    }, [message, t, reload]);

    const cancel = useCallback(async () => {
        try {
            // The boolean is what tells "stopped" from "there was nothing to stop", which a button's
            // feedback depends on.
            const stopped = await cancelGarbageCollection();
            if (stopped) {
                message.info(t("config.garbage.cancelling"));
            }
        } catch (failure) {
            message.error(String(failure));
        }
    }, [message, t]);

    if (!available) {
        return (
            <Alert
                type="info"
                showIcon
                data-testid="garbage-unavailable"
                message={t("config.garbage.unavailable")}
            />
        );
    }

    const candidates = preview?.candidates ?? [];
    const columns: ColumnsType<desktop.GarbageCandidateDTO> = [
        {
            title: t("config.garbage.hash"),
            dataIndex: "hash",
            width: 160,
            render: (value: string) => <Typography.Text code>{value.slice(0, 16)}…</Typography.Text>,
        },
        { title: t("config.garbage.type"), dataIndex: "mimeType", width: 140, render: (value?: string) => value || "—" },
        {
            title: t("config.garbage.size"),
            dataIndex: "sizeBytes",
            width: 110,
            render: (value: number) => formatBytes(value ?? 0),
        },
    ];

    return (
        <Space direction="vertical" size="middle" className="w-full" data-testid="garbage-collection">
            <Space wrap>
                <Button
                    icon={<RefreshCw className="size-4" />}
                    loading={busy}
                    onClick={() => void reload()}
                    data-testid="garbage-refresh"
                >
                    {t("config.garbage.refresh")}
                </Button>
                {running ? (
                    <Button danger onClick={() => void cancel()} data-testid="garbage-cancel">
                        {t("config.garbage.cancel")}
                    </Button>
                ) : (
                    <Popconfirm
                        title={t("config.garbage.confirmTitle")}
                        description={t("config.garbage.confirmBody", {
                            count: candidates.length,
                            bytes: formatBytes(preview?.totalBytes ?? 0),
                        })}
                        okButtonProps={{ danger: true }}
                        onConfirm={() => void collect()}
                        disabled={candidates.length === 0}
                    >
                        <Button
                            type="primary"
                            danger
                            icon={<Trash2 className="size-4" />}
                            disabled={candidates.length === 0}
                            data-testid="garbage-collect"
                        >
                            {t("config.garbage.collect")}
                        </Button>
                    </Popconfirm>
                )}
                <Tag data-testid="garbage-total">
                    {t("config.garbage.total", { bytes: formatBytes(preview?.totalBytes ?? 0) })}
                </Tag>
            </Space>

            <Typography.Paragraph type="secondary" className="text-xs">
                {t("config.garbage.hint")}
            </Typography.Paragraph>

            {error !== "" ? <Alert type="error" showIcon message={error} /> : null}
            {candidates.length === 0 ? (
                <Empty description={t("config.garbage.empty")} />
            ) : (
                <Table
                    size="small"
                    rowKey="hash"
                    columns={columns}
                    dataSource={candidates}
                    pagination={false}
                    data-testid="garbage-list"
                />
            )}
        </Space>
    );
}

/**
 * formatBytes renders a size the way a person reads one.
 *
 * It is local rather than shared because the two other callers in this codebase are in the canvas's
 * storage display and memoising it there would couple a settings panel to a canvas module. A size a
 * user decides on has to be readable at a glance, so the unit is chosen rather than always shown in
 * bytes.
 */
function formatBytes(bytes: number): string {
    if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
    const units = ["B", "KB", "MB", "GB"];
    let value = bytes;
    let unit = 0;
    while (value >= 1024 && unit < units.length - 1) {
        value /= 1024;
        unit += 1;
    }
    return `${value.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`;
}
