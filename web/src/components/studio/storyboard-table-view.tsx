import { useCallback, useEffect, useMemo, useState } from "react";
import { Alert, App, Button, Empty, Form, Input, InputNumber, Modal, Popconfirm, Select, Space, Table, Tag, Tooltip, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { CheckCircle2, Download, Pencil, Play } from "lucide-react";
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
import { exportShotList, isDocumentExportAvailable, saveDocument } from "@/services/desktop/media";
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
 *
 * A FOURTH THING, ADDED WITH THE SHOT LIST: the board can leave as a DOCUMENT. ROADMAP item
 * 11 asks for the storyboard's export, and this section is where the board is, so the control
 * lives beside the rows rather than only in the timeline. The document is rendered to be READ
 * and saved from what was read — `ExportShotList` returns the text and `SaveDocument` writes
 * it — which is the same two-step the timeline's own document area keeps.
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
    /**
     * The shot list's own state.
     *
     * `busy` above is the section's boolean for the stage and the supervisor, and the shot list export
     * gets a separate key rather than sharing it: the two are independent actions, and a shared flag
     * would put a spinner on the stage button while a document renders.
     */
    const [shotList, setShotList] = useState<{ text: string; suggestedName: string } | null>(null);
    const [shotListBusy, setShotListBusy] = useState("");
    /** Whether this build can render and write a document at all. */
    const shotListExportAvailable = isDocumentExportAvailable();

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

    /**
     * renderShotList renders the board as a shot list document.
     *
     * THE EPISODE IS THE SECTION'S `activeEpisodeId` PROP, and the version is the one the table is
     * SHOWING. Those are the two facts a user is looking at, and the core takes both: `episodeId` is
     * required, and a `versionId` overrides the version in force — so naming the selected version is
     * what makes the exported document the board on screen rather than a different approved board.
     * When the picker holds no version (an episode whose board was never read), the id is left empty
     * and the core falls back to its own approved version, which is the honest answer to "export what
     * this episode has".
     *
     * CSV is the format sent, and it is the one a shot list is worked from: a schedule is read in a
     * spreadsheet. The core writes `txt` as well, and this control does not offer the choice because a
     * second format selector beside a table that is already CSV-shaped would be a control for its own
     * sake — the timeline's document area is where the formats are chosen side by side.
     */
    const renderShotList = useCallback(async () => {
        if (!activeEpisodeId) return;
        setShotListBusy("render");
        try {
            const rendered = await exportShotList({ episodeId: activeEpisodeId, versionId: activeVersionId || undefined, format: "csv" } as never);
            setShotList({ text: rendered.text, suggestedName: rendered.suggestedName });
        } catch (failure) {
            // A failed render CLEARS the document: text from an earlier successful call would be read
            // as this call's answer, which is the one thing a preview must never do.
            message.error(failure instanceof Error ? failure.message : t("studio.storyboardTable.shotListFailed"));
            setShotList(null);
        } finally {
            setShotListBusy("");
        }
    }, [activeEpisodeId, activeVersionId, message, t]);

    /**
     * saveShotList writes the rendered document to where the user points.
     *
     * The text travels BACK rather than being re-rendered, which is the core's own reasoning: a second
     * render could pick up a version approved in between, so what the user read would not be what they
     * saved. `written: false` is the user having cancelled the dialog — a returned value, not an error,
     * reported with the same wording the timeline's save handlers use.
     */
    const saveShotList = useCallback(async () => {
        if (!shotList) return;
        setShotListBusy("save");
        try {
            const result = await saveDocument({ text: shotList.text, suggestedName: shotList.suggestedName });
            // The three strings are the TIMELINE's, read from here on purpose: they are the wording
            // `writeToDisk` already uses for "the dialog wrote your file" and "you cancelled, which is
            // not a failure", and a second copy of them would be two sentences that must stay in step.
            // `assetKind` is a family two sections already share this way.
            message.success(result.written ? t("studio.timeline.savedTo", { path: result.path ?? "" }) : t("studio.timeline.saveCancelled"));
        } catch (failure) {
            message.error(failure instanceof Error ? failure.message : t("studio.storyboardTable.shotListSaveFailed"));
        } finally {
            setShotListBusy("");
        }
    }, [shotList, message, t]);

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
            {/* The board as a document: rendered here to be READ, and written only from what was read.
                The control is disabled when the build carries no document export rather than left to
                fail on a press, which is the same answer the timeline's document area gives. */}
            <Space wrap align="end">
                <Button
                    icon={<Download className="size-4" />}
                    loading={shotListBusy === "render"}
                    disabled={!shotListExportAvailable || !activeEpisodeId}
                    data-testid="studio-storyboard-table-render-shot-list"
                    onClick={() => void renderShotList()}
                >
                    {t("studio.storyboardTable.exportShotList")}
                </Button>
                {shotList ? (
                    <Button type="primary" loading={shotListBusy === "save"} disabled={!shotListExportAvailable} data-testid="studio-storyboard-table-save-shot-list" onClick={() => void saveShotList()}>
                        {t("studio.timeline.save")}
                    </Button>
                ) : null}
            </Space>
            {shotList ? (
                <div>
                    <Typography.Text className="mb-2 block text-xs text-stone-500">{t("studio.timeline.suggestedName", { name: shotList.suggestedName })}</Typography.Text>
                    {/* Read-only on purpose, and the same shape the timeline's previews use: the core
                        returns the DOCUMENT rather than writing it, so saving is a separate act. */}
                    <Input.TextArea readOnly rows={12} value={shotList.text} data-testid="studio-storyboard-table-shot-list" className="font-mono text-xs" />
                </div>
            ) : null}
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
