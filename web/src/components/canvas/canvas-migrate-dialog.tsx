import { useState } from "react";
import { Alert, Button, Modal, Progress, Space, Table, Tag, Typography } from "antd";
import { useTranslation } from "react-i18next";

import { precheckMigration, runMigration, uploadSnapshotMedia, type ImportResult, type MigrationProgress, type PrecheckResult } from "@/services/desktop/migration";
import type { LegacySnapshot } from "@/services/desktop/legacy-extract";
import { resolveCanvasAdapter } from "@/services/desktop/canvas-adapter";
import { useCanvasStore } from "@/stores/canvas/use-canvas-store";

/**
 * The legacy migration dialog.
 *
 * It walks the four steps AC-LEGACY-001..003 name: precheck, review, import,
 * report. Nothing is written before the user confirms, and the report survives a
 * failure so a partial or refused import is explained rather than swallowed.
 *
 * The dialog never claims more than happened: a run that imported nothing
 * because everything was already there says so, and a run that stopped names the
 * stage it stopped at.
 */
type Stage = "idle" | "prechecking" | "review" | "running" | "done" | "failed";

export function CanvasMigrateDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
    const { t } = useTranslation();
    const [stage, setStage] = useState<Stage>("idle");
    const [snapshot, setSnapshot] = useState<LegacySnapshot | null>(null);
    const [precheck, setPrecheck] = useState<PrecheckResult | null>(null);
    const [result, setResult] = useState<ImportResult | null>(null);
    const [failure, setFailure] = useState<string>("");
    const [progress, setProgress] = useState<MigrationProgress | null>(null);

    const reset = () => {
        setStage("idle");
        setSnapshot(null);
        setPrecheck(null);
        setResult(null);
        setFailure("");
        setProgress(null);
    };

    const close = () => {
        reset();
        onClose();
    };

    const startPrecheck = async () => {
        setStage("prechecking");
        setFailure("");
        try {
            const { snapshot: extracted, result: reported } = await precheckMigration();
            setSnapshot(extracted);
            setPrecheck(reported);
            setStage("review");
        } catch (error) {
            setFailure(error instanceof Error ? error.message : String(error));
            setStage("failed");
        }
    };

    const startImport = async (mode: "initial" | "copy") => {
        if (!snapshot) return;
        setStage("running");
        setFailure("");
        try {
            // The media is uploaded before the import starts: the Go side resolves
            // a legacy key to committed bytes, so an upload that happened during
            // the import would make the result depend on timing (ADR-0006 §2).
            const upload = await uploadSnapshotMedia(snapshot, setProgress);
            const imported = await runMigration(snapshot, { mode, sourceCase: "browser", onProgress: setProgress });
            setResult(imported);
            setStage("done");
            // The canvas list is refreshed from the core, so the migrated projects
            // appear as the database holds them.
            void resolveCanvasAdapter()
                .then((adapter) => adapter.listProjects())
                .then((projects) => useCanvasStore.getState().replaceProjects(projects))
                .catch(() => {
                    // The report below already tells the user the import finished;
                    // a failed refresh only means they should reopen the list.
                });
            if (upload.skipped > 0) {
                setFailure(t("canvas.migrate.skippedUploads", { count: upload.skipped }));
            }
        } catch (error) {
            setFailure(error instanceof Error ? error.message : String(error));
            setStage("failed");
        }
    };

    const pending = precheck?.projects.filter((project) => !project.alreadyImported) ?? [];
    const alreadyImported = precheck?.projects.filter((project) => project.alreadyImported) ?? [];

    return (
        <Modal
            title={t("canvas.migrate.title")}
            open={open}
            centered
            width={720}
            onCancel={stage === "running" ? undefined : close}
            maskClosable={stage !== "running"}
            footer={
                <Space>
                    <Button onClick={close} disabled={stage === "running"}>
                        {t("common.cancel")}
                    </Button>
                    {stage === "idle" ? (
                        <Button type="primary" onClick={() => void startPrecheck()}>
                            {t("canvas.migrate.startPrecheck")}
                        </Button>
                    ) : null}
                    {stage === "review" ? (
                        <>
                            <Button onClick={() => void startImport("copy")}>{t("canvas.migrate.importCopy")}</Button>
                            <Button type="primary" disabled={pending.length === 0} onClick={() => void startImport("initial")}>
                                {t("canvas.migrate.importSelected", { count: pending.length })}
                            </Button>
                        </>
                    ) : null}
                    {stage === "done" || stage === "failed" ? <Button type="primary" onClick={close}>{t("common.confirm")}</Button> : null}
                </Space>
            }
        >
            <div className="space-y-4">
                {stage === "idle" ? <Typography.Paragraph className="text-sm text-stone-500">{t("canvas.migrate.intro")}</Typography.Paragraph> : null}

                {stage === "prechecking" ? <Typography.Paragraph className="text-sm">{t("canvas.migrate.prechecking")}</Typography.Paragraph> : null}

                {stage === "review" && precheck ? (
                    <>
                        <Alert
                            type={pending.length > 0 ? "info" : "warning"}
                            showIcon
                            message={t("canvas.migrate.scope", {
                                projects: precheck.projects.length,
                                nodes: precheck.totalNodes,
                                edges: precheck.totalEdges,
                                media: precheck.mediaFiles,
                            })}
                        />
                        {alreadyImported.length > 0 ? (
                            <Alert type="warning" showIcon message={t("canvas.migrate.alreadyImported", { count: alreadyImported.length })} description={t("canvas.migrate.alreadyImportedHint")} />
                        ) : null}
                        {precheck.missingMedia.length > 0 ? (
                            <Alert type="warning" showIcon message={t("canvas.migrate.missingMedia", { count: precheck.missingMedia.length })} description={t("canvas.migrate.missingMediaHint")} />
                        ) : null}
                        {precheck.warnings.length > 0 ? <WarningList warnings={precheck.warnings} /> : null}
                        <Table
                            size="small"
                            pagination={false}
                            rowKey="legacyId"
                            dataSource={precheck.projects}
                            columns={[
                                { title: t("canvas.migrate.columnTitle"), dataIndex: "title" },
                                { title: t("canvas.migrate.columnNodes"), dataIndex: "nodes", width: 80 },
                                { title: t("canvas.migrate.columnEdges"), dataIndex: "edges", width: 80 },
                                {
                                    title: t("canvas.migrate.columnState"),
                                    dataIndex: "alreadyImported",
                                    width: 140,
                                    render: (imported: boolean) =>
                                        imported ? <Tag color="gold">{t("canvas.migrate.stateImported")}</Tag> : <Tag color="green">{t("canvas.migrate.stateNew")}</Tag>,
                                },
                            ]}
                        />
                    </>
                ) : null}

                {stage === "running" ? (
                    <>
                        <Typography.Paragraph className="text-sm">
                            {progress?.phase === "upload" ? t("canvas.migrate.uploading") : t("canvas.migrate.importing")}
                        </Typography.Paragraph>
                        <Progress
                            percent={progress && progress.total > 0 ? Math.round((progress.done / progress.total) * 100) : 0}
                            status="active"
                        />
                        {progress?.detail ? <Typography.Text type="secondary" className="text-xs">{progress.detail}</Typography.Text> : null}
                    </>
                ) : null}

                {stage === "done" && result ? (
                    <>
                        <Alert
                            type={result.imported.length > 0 ? "success" : "info"}
                            showIcon
                            message={
                                result.imported.length > 0
                                    ? t("canvas.migrate.resultImported", { count: result.imported.length })
                                    : t("canvas.migrate.resultNothing")
                            }
                            description={t("canvas.migrate.resultCounts", {
                                nodes: result.counts.nodes,
                                edges: result.counts.edges,
                                assets: result.counts.assets,
                                media: result.counts.media,
                            })}
                        />
                        {result.skipped.length > 0 ? <Alert type="info" showIcon message={t("canvas.migrate.resultSkipped", { count: result.skipped.length })} /> : null}
                        {result.warnings.length > 0 ? <WarningList warnings={result.warnings} /> : null}
                        <Typography.Paragraph className="text-xs text-stone-500">{t("canvas.migrate.legacyKept")}</Typography.Paragraph>
                    </>
                ) : null}

                {stage === "failed" ? (
                    <>
                        <Alert type="error" showIcon message={t("canvas.migrate.failed")} description={failure} />
                        <Typography.Paragraph className="text-xs text-stone-500">{t("canvas.migrate.failedHint")}</Typography.Paragraph>
                    </>
                ) : null}
            </div>
        </Modal>
    );
}

/**
 * WarningList shows what a migration could not model.
 *
 * Each entry names the case in the user's language and falls back to the code
 * when a translation is missing, so a new warning kind is visible rather than
 * blank.
 */
function WarningList({ warnings }: { warnings: { code: string; detail?: string; occurrences: number }[] }) {
    const { t } = useTranslation();
    // The same code can be reported for many rows; one line per code keeps the
    // list readable while the count preserves the scale.
    const byCode = new Map<string, { code: string; detail?: string; occurrences: number }>();
    for (const warning of warnings) {
        const existing = byCode.get(warning.code);
        if (existing) {
            existing.occurrences += warning.occurrences;
        } else {
            byCode.set(warning.code, { ...warning });
        }
    }
    return (
        <div className="space-y-1">
            {[...byCode.values()].map((warning) => (
                <div key={warning.code} className="flex items-start gap-2 text-xs">
                    <Tag color="orange">{warning.occurrences}</Tag>
                    <span>
                        <strong>{t(`canvas.migrate.warning.${warning.code}`, { defaultValue: warning.code })}</strong>
                        {warning.detail ? <span className="text-stone-500"> — {warning.detail}</span> : null}
                    </span>
                </div>
            ))}
        </div>
    );
}
