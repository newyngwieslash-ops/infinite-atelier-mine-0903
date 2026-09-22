import { useCallback, useEffect, useMemo, useState } from "react";
import { Alert, App, Button, Empty, Form, Input, InputNumber, Modal, Popconfirm, Select, Space, Table, Tag, Tooltip, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { CheckCircle2, Pencil, Play } from "lucide-react";
import { useTranslation } from "react-i18next";

import {
    approveStoryboardVersion,
    createWorkflowRun,
    ensureStoryboard,
    listStoryboardItems,
    listStoryboardVersions,
    listStageRuns,
    listWorkflowRuns,
    runScriptStage,
    runScriptSupervision,
    updateStoryboardItem,
} from "@/services/desktop/drama";
import type { desktop } from "@/wailsjs/go/models";

/**
 * The Storyboard Table section: FR-070's rows, the supervisor's verdict, and the gate.
 *
 * FR-070's acceptance is what this section is: a board generated from an approved script,
 * the director plan and the approved assets; every per-shot field present; a supervisor that
 * can locate a problem at a specific shot; and a FIX that changes that shot alone.
 *
 * THE EDIT IS THE INTERESTING PART, and it is a single-row edit on purpose: AC-BOARD-002
 * requires that a FIX to one shot leaves the other shots unchanged, and the strongest form
 * of that is what `updateStoryboardItem` does — it writes THAT ROW, so the others are never
 * touched. A version-rewrite path would have to copy them, and "unchanged" would rest on a
 * copy being faithful.
 *
 * THREE THINGS IT DOES NOT GUESS:
 *
 *  - The workflow run is ENSURED before a stage starts. `stage_runs.workflow_run_id`
 *    references `workflow_runs(id)`, so an episode id in that position fails on a foreign
 *    key — the defect WP-08's review found in the Script section.
 *  - An approval names the VERSION it approves, because moving a stage and approving an
 *    artifact are two acts the core keeps separate.
 *  - A stale row is REFUSED rather than overwritten. The revision the table read travels
 *    with the edit, and a conflict says so instead of silently replacing another window's
 *    change.
 */

export type StoryboardTableSectionProps = {
    projectId: string;
    episodes: desktop.EpisodeDTO[];
    activeEpisodeId: string;
    onSelectEpisode: (episodeId: string) => void;
    onChanged: () => void;
};

/** The row's editable fields, which is exactly what the update command accepts. */
type RowDraft = {
    shotSize: string;
    cameraAngle: string;
    cameraMovement: string;
    durationSeconds: number;
    visualDescription: string;
    actionDescription: string;
    dialogueAudioSummary: string;
    continuityNotes: string;
    firstFrameDescription: string;
    lastFrameDescription: string;
    videoMotionDescription: string;
};

const EMPTY_DRAFT: RowDraft = {
    shotSize: "",
    cameraAngle: "",
    cameraMovement: "",
    durationSeconds: 0,
    visualDescription: "",
    actionDescription: "",
    dialogueAudioSummary: "",
    continuityNotes: "",
    firstFrameDescription: "",
    lastFrameDescription: "",
    videoMotionDescription: "",
};

export function StoryboardTableSection({ projectId, episodes, activeEpisodeId, onSelectEpisode, onChanged }: StoryboardTableSectionProps) {
    const { t } = useTranslation();
    const { message } = App.useApp();
    const [versions, setVersions] = useState<desktop.StoryboardVersionDTO[]>([]);
    const [activeVersionId, setActiveVersionId] = useState("");
    const [items, setItems] = useState<desktop.StoryboardItemDTO[]>([]);
    const [loading, setLoading] = useState(false);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");
    const [editing, setEditing] = useState<desktop.StoryboardItemDTO | null>(null);
    const [form] = Form.useForm<RowDraft>();

    const activeEpisode = useMemo(
        () => episodes.find((episode) => episode.id === activeEpisodeId) || null,
        [episodes, activeEpisodeId],
    );

    const reload = useCallback(async () => {
        if (!activeEpisodeId) {
            setVersions([]);
            setItems([]);
            setActiveVersionId("");
            return;
        }
        setLoading(true);
        setError("");
        try {
            const board = await ensureStoryboard(activeEpisodeId);
            const found = await listStoryboardVersions(board.id);
            setVersions(found);
            const versionId = activeVersionId && found.some((candidate) => candidate.id === activeVersionId)
                ? activeVersionId
                : found[0]?.id || "";
            setActiveVersionId(versionId);
            setItems(versionId ? await listStoryboardItems(versionId) : []);
        } catch (failure) {
            setError(String(failure));
        } finally {
            setLoading(false);
        }
    }, [activeEpisodeId, activeVersionId]);

    useEffect(() => {
        void reload();
        // The reload depends on the episode and the SELECTED VERSION, and re-running it when
        // the selection changes is what keeps the rows and the picker in step.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [activeEpisodeId, activeVersionId]);

    const runStage = useCallback(async (stage: string) => {
        if (!activeEpisode || busy) return;
        setBusy(true);
        try {
            const runs = await listWorkflowRuns(projectId);
            const run = runs.find((candidate) => candidate.episodeId === activeEpisode.id)
                || (await createWorkflowRun({
                    projectId,
                    episodeId: activeEpisode.id,
                    workflowType: "episode_production",
                } as never));
            await runScriptStage({
                workflowRunId: run.id,
                stage,
                projectId,
                episodeId: activeEpisode.id,
                task: stage,
                storyboardVersionId: activeVersionId,
            } as never);
            message.success(t("studio.shell.stageStarted"));
            await reload();
            onChanged();
        } catch (failure) {
            message.error(String(failure));
        } finally {
            setBusy(false);
        }
    }, [activeEpisode, busy, projectId, activeVersionId, reload, onChanged, t, message]);

    const supervise = useCallback(async () => {
        if (!activeEpisode || busy) return;
        setBusy(true);
        try {
            // The supervisor reviews an ATTEMPT, so the attempt is FOUND rather than
            // assumed: a report attaches to a stage run, and supervising a version with no
            // attempt would have nothing to attach it to. The newest storyboard_table
            // attempt is the one the version came from.
            const runs = await listWorkflowRuns(projectId);
            const run = runs.find((candidate) => candidate.episodeId === activeEpisode.id);
            if (!run) {
                message.warning(t("studio.storyboardTable.noAttempt"));
                return;
            }
            const attempts = await listStageRuns(run.id);
            const attempt = attempts
                .filter((candidate) => candidate.stage === "storyboard_table")
                .sort((left, right) => right.attempt - left.attempt)[0];
            if (!attempt) {
                message.warning(t("studio.storyboardTable.noAttempt"));
                return;
            }
            await runScriptSupervision({
                stageRunId: attempt.id,
                projectId,
                episodeId: activeEpisode.id,
                artifactVersionId: activeVersionId,
            } as never);
            message.success(t("studio.storyboardTable.reviewed"));
            await reload();
            onChanged();
        } catch (failure) {
            message.error(String(failure));
        } finally {
            setBusy(false);
        }
    }, [activeEpisode, busy, projectId, activeVersionId, message, reload, onChanged, t]);

    const approve = useCallback(async () => {
        if (!activeVersionId) return;
        try {
            await approveStoryboardVersion({ versionId: activeVersionId });
            message.success(t("studio.shell.approved"));
            await reload();
            onChanged();
        } catch (failure) {
            message.error(String(failure));
        }
    }, [activeVersionId, message, reload, onChanged, t]);

    const openEditor = useCallback(
        (row: desktop.StoryboardItemDTO) => {
            setEditing(row);
            form.setFieldsValue({
                shotSize: row.shotSize || "",
                cameraAngle: row.cameraAngle || "",
                cameraMovement: row.cameraMovement || "",
                durationSeconds: row.durationSeconds || 0,
                visualDescription: row.visualDescription || "",
                actionDescription: row.actionDescription || "",
                dialogueAudioSummary: row.dialogueAudioSummary || "",
                continuityNotes: row.continuityNotes || "",
                firstFrameDescription: row.firstFrameDescription || "",
                lastFrameDescription: row.lastFrameDescription || "",
                videoMotionDescription: row.videoMotionDescription || "",
            });
        },
        [form],
    );

    const saveRow = useCallback(async () => {
        if (!editing) return;
        const values = await form.validateFields();
        try {
            await updateStoryboardItem({
                itemId: editing.id,
                // The revision the TABLE read travels with the edit, so a row another window
                // changed is refused rather than silently replaced.
                expectedRevision: editing.revision,
                ...values,
            } as never);
            message.success(t("studio.storyboardTable.saved"));
            setEditing(null);
            await reload();
            onChanged();
        } catch (failure) {
            const text = String(failure);
            // A conflict is the one failure the user can act on, so it is named as what it is
            // rather than passed through as a transport error.
            message.error(text.includes("another window") ? t("studio.storyboardTable.conflict") : text);
        }
    }, [editing, form, message, reload, onChanged, t]);

    const activeVersion = useMemo(
        () => versions.find((candidate) => candidate.id === activeVersionId) || null,
        [versions, activeVersionId],
    );

    const columns: ColumnsType<desktop.StoryboardItemDTO> = [
        { title: t("studio.storyboardTable.shot"), dataIndex: "ordinal", width: 70, render: (value: number) => `#${value}` },
        { title: t("studio.storyboardTable.size"), dataIndex: "shotSize", width: 90 },
        { title: t("studio.storyboardTable.movement"), dataIndex: "cameraMovement", width: 110 },
        { title: t("studio.storyboardTable.seconds"), dataIndex: "durationSeconds", width: 70 },
        { title: t("studio.storyboardTable.visual"), dataIndex: "visualDescription", ellipsis: true },
        { title: t("studio.storyboardTable.continuity"), dataIndex: "continuityNotes", ellipsis: true },
        {
            title: t("studio.storyboardTable.frames"),
            key: "frames",
            ellipsis: true,
            render: (_: unknown, row: desktop.StoryboardItemDTO) =>
                [row.firstFrameDescription, row.lastFrameDescription].filter(Boolean).join(" → ") || "—",
        },
        {
            title: "",
            key: "actions",
            width: 60,
            render: (_: unknown, row: desktop.StoryboardItemDTO) => (
                <Tooltip title={t("studio.storyboardTable.editHint")}>
                    <Button size="small" type="text" icon={<Pencil className="size-4" />} onClick={() => openEditor(row)} />
                </Tooltip>
            ),
        },
    ];

    if (episodes.length === 0) {
        return <Empty description={t("studio.storyboardTable.selectEpisode")} />;
    }

    return (
        <Space direction="vertical" size="middle" className="w-full">
            <Space wrap>
                <Select
                    className="min-w-56"
                    value={activeEpisodeId || undefined}
                    placeholder={t("studio.storyboardTable.selectEpisode")}
                    onChange={onSelectEpisode}
                    options={episodes.map((episode) => ({
                        value: episode.id,
                        label: `S${episode.seasonNumber}E${episode.episodeNumber} · ${episode.title}`,
                    }))}
                />
                <Select
                    className="min-w-40"
                    value={activeVersionId || undefined}
                    placeholder={t("studio.storyboardTable.versions")}
                    onChange={setActiveVersionId}
                    options={versions.map((version) => ({
                        value: version.id,
                        label: `v${version.versionNumber} · ${version.status}`,
                    }))}
                />
                <Button type="primary" icon={<Play className="size-4" />} loading={busy} onClick={() => void runStage("storyboard_table")}>
                    {t("studio.shell.stage.storyboard_table")}
                </Button>
                <Button loading={busy} onClick={() => void supervise()}>
                    {t("studio.shell.review")}
                </Button>
                {activeVersion?.status === "approved" ? (
                    <Tag color="green" icon={<CheckCircle2 className="size-3" />}>
                        {t("studio.storyboardTable.approved")}
                    </Tag>
                ) : (
                    <Popconfirm title={t("studio.storyboardTable.approve")} onConfirm={() => void approve()} disabled={!activeVersionId}>
                        <Button disabled={!activeVersionId} type="primary" ghost>
                            {t("studio.storyboardTable.approve")}
                        </Button>
                    </Popconfirm>
                )}
            </Space>
            {activeVersion?.status === "approved" ? (
                <Typography.Text type="secondary">{t("studio.storyboardTable.approvedHint")}</Typography.Text>
            ) : null}
            {error !== "" ? <Alert type="error" showIcon message={error} /> : null}
            {items.length === 0 && !loading ? (
                <Alert type="info" showIcon message={t("studio.storyboardTable.noBoard")} />
            ) : (
                <Table
                    size="small"
                    rowKey="id"
                    loading={loading}
                    columns={columns}
                    dataSource={items}
                    pagination={false}
                    title={() => (
                        <Typography.Text strong>
                            {t("studio.storyboardTable.rows", { count: items.length })}
                        </Typography.Text>
                    )}
                />
            )}
            <Modal
                open={editing !== null}
                title={editing ? `#${editing.ordinal} · ${editing.shotId}` : ""}
                onCancel={() => setEditing(null)}
                onOk={() => void saveRow()}
                okText={t("studio.storyboardTable.save")}
                width="min(92vw, 720px)"
                destroyOnHidden
            >
                <Alert type="info" showIcon message={t("studio.storyboardTable.editHint")} className="mb-3" />
                <Form form={form} layout="vertical" initialValues={EMPTY_DRAFT}>
                    <Space size="middle" wrap>
                        <Form.Item name="shotSize" label={t("studio.storyboardTable.shotSize")}>
                            <Input className="w-32" />
                        </Form.Item>
                        <Form.Item name="cameraAngle" label={t("studio.storyboardTable.cameraAngle")}>
                            <Input className="w-40" />
                        </Form.Item>
                        <Form.Item name="cameraMovement" label={t("studio.storyboardTable.cameraMovement")}>
                            <Input className="w-40" />
                        </Form.Item>
                        <Form.Item name="durationSeconds" label={t("studio.storyboardTable.duration")}>
                            <InputNumber min={0} max={3600} className="w-32" />
                        </Form.Item>
                    </Space>
                    <Form.Item name="visualDescription" label={t("studio.storyboardTable.visual")}>
                        <Input.TextArea rows={2} />
                    </Form.Item>
                    <Form.Item name="actionDescription" label={t("studio.storyboardTable.action")}>
                        <Input.TextArea rows={2} />
                    </Form.Item>
                    <Form.Item name="dialogueAudioSummary" label={t("studio.storyboardTable.dialogue")}>
                        <Input.TextArea rows={2} />
                    </Form.Item>
                    <Form.Item name="continuityNotes" label={t("studio.storyboardTable.continuity")}>
                        <Input.TextArea rows={2} />
                    </Form.Item>
                    <Form.Item name="firstFrameDescription" label={t("studio.storyboardTable.firstFrame")}>
                        <Input.TextArea rows={2} />
                    </Form.Item>
                    <Form.Item name="lastFrameDescription" label={t("studio.storyboardTable.lastFrame")}>
                        <Input.TextArea rows={2} />
                    </Form.Item>
                    <Form.Item name="videoMotionDescription" label={t("studio.storyboardTable.videoMotion")}>
                        <Input.TextArea rows={2} />
                    </Form.Item>
                </Form>
            </Modal>
        </Space>
    );
}
