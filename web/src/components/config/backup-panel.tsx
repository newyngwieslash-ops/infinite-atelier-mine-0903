import { Alert, App, Button, Descriptions, Modal, Space, Typography } from "antd";
import { Download, FileUp, Trash2 } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { saveAs } from "file-saver";

import type { desktop } from "@/wailsjs/go/models";
import {
    backupArchiveName,
    backupStateHeld,
    base64ToBytes,
    discardBackupState,
    exportBackup,
    isBackupBindingsAvailable,
    previewBackup,
    readArchiveAsBase64,
    restoreBackup,
} from "@/services/desktop/backup";
import { formatBytes } from "@/lib/image-utils";

/**
 * The project data backup panel.
 *
 * # Why it stands beside the browser-local backup rather than replacing it
 *
 * `ConfigBackupTab` already offers a backup, and that one packages the canvas
 * projects, assets and settings the BROWSER holds, in localForage and IndexedDB.
 * Its own text says "数据仅保存在浏览器本地" and that sentence is true of it. The
 * Go core's ordinary backup is a different thing entirely — the project database
 * and the object store — and the two archives are not interchangeable in either
 * direction. So this panel is a second section with its own heading that says
 * which data it covers, not a rework of the first one: conflating them would
 * tell a user their core data was protected by a browser download that never
 * touched it.
 *
 * # The order the restore keeps, and why
 *
 * `PreviewBackup` → the user reads what the archive holds → they confirm →
 * `RestoreBackup(archive, true)`. The core requires the confirmation flag and
 * refuses a false one, so the only honest way to reach it is to have shown the
 * user the preview first. That is why the picker leads to a modal rather than to
 * a confirm box: a confirm box with nothing in it asks the user to agree to
 * something they have not seen, over work they cannot get back.
 *
 * # What it must not swallow
 *
 * Three results are the user's, not the log's. `restartRequired` is always true
 * because the core closes the database to move its file, so the panel keeps it in
 * view until dismissed — a restore the user thinks took effect is a restore they
 * will report as a missing project. `previousDatabasePath` names the only copy of
 * what the restore displaced, and `BackupStateHeld` is what still reports it on a
 * later visit. And `rolledBack` says the promotion failed and the previous state
 * was put back, which must NOT be shown as a success.
 *
 * Every failure from every call lands in `error` and is rendered. A call whose
 * message went only to the console would leave a user pressing a button that
 * appeared to do nothing.
 */
export function ProjectBackupPanel() {
    const { message, modal } = App.useApp();
    const { t } = useTranslation();
    const inputRef = useRef<HTMLInputElement>(null);
    const [busy, setBusy] = useState<"export" | "preview" | "restore" | "discard" | "">("");
    const [error, setError] = useState("");
    /** The chosen archive and its preview, held across the confirmation. */
    const [archive, setArchive] = useState<{ name: string; base64: string; preview: desktop.BackupPreview } | null>(null);
    /** Whether the preview modal is open. Separate from `archive` on purpose — see handleRestore. */
    const [previewOpen, setPreviewOpen] = useState(false);
    const [restored, setRestored] = useState<desktop.RestoreResult | null>(null);
    const [stateHeld, setStateHeld] = useState(false);

    const available = isBackupBindingsAvailable();

    const refreshStateHeld = useCallback(() => {
        if (!isBackupBindingsAvailable()) {
            setStateHeld(false);
            return;
        }
        // A failure here is reported rather than hidden: the question decides
        // whether a user is told their previous projects are still recoverable,
        // and a silent `false` would answer it wrongly.
        void backupStateHeld().then(setStateHeld, (cause: unknown) => setError(describe(cause, t("config.projectBackup.stateHeldFailed"))));
    }, [t]);

    useEffect(refreshStateHeld, [refreshStateHeld]);

    const handleExport = async () => {
        setBusy("export");
        setError("");
        try {
            const archive = await exportBackup();
            // The binding returns base64 because a Wails call carries text; the
            // file the user gets is the decoded bytes, under a name carrying the
            // moment it was written.
            saveAs(new Blob([base64ToBytes(archive)], { type: "application/zip" }), backupArchiveName(new Date()));
            message.success(t("config.projectBackup.exported"));
        } catch (cause) {
            setError(describe(cause, t("config.projectBackup.exportFailed")));
        } finally {
            setBusy("");
        }
    };

    const handleChoose = async (file?: File) => {
        if (!file) return;
        setBusy("preview");
        setError("");
        try {
            const base64 = await readArchiveAsBase64(file);
            const preview = await previewBackup(base64);
            setArchive({ name: file.name, base64, preview });
            setPreviewOpen(true);
        } catch (cause) {
            setError(describe(cause, t("config.projectBackup.previewFailed")));
        } finally {
            setBusy("");
            if (inputRef.current) inputRef.current.value = "";
        }
    };

    const handleRestore = () => {
        if (!archive) return;
        // The preview closes as the confirmation opens rather than sitting under
        // it: a failure has to land where the user can SEE it, and the panel's
        // error Alert is behind anything still open. The chosen archive is NOT
        // cleared, so a failure can put the preview back rather than making the
        // user pick their file a second time.
        setPreviewOpen(false);
        modal.confirm({
            title: t("config.projectBackup.confirmTitle"),
            content: t("config.projectBackup.confirmDescription"),
            okText: t("config.projectBackup.confirm"),
            cancelText: t("common.cancel"),
            okButtonProps: { danger: true },
            onOk: async () => {
                setError("");
                try {
                    const result = await restoreBackup(archive.base64, true);
                    setRestored(result);
                    setArchive(null);
                    // The result, not this handler, is what says the restore worked:
                    // a rolled-back promotion arrives here too and put the previous
                    // state back, so `RestoreOutcome` is what tells them apart.
                    refreshStateHeld();
                } catch (cause) {
                    // Recorded rather than rethrown, so the confirmation closes and
                    // the message is on screen. Reopening the preview keeps the
                    // archive and its contents in front of the user with the reason
                    // it failed, which is more use than an empty panel and a file
                    // picker.
                    setError(describe(cause, t("config.projectBackup.restoreFailed")));
                    setPreviewOpen(true);
                }
            },
        });
    };

    const handleDiscard = () => {
        modal.confirm({
            title: t("config.projectBackup.discardConfirmTitle"),
            content: t("config.projectBackup.discardConfirmDescription"),
            okText: t("config.projectBackup.discard"),
            cancelText: t("common.cancel"),
            okButtonProps: { danger: true },
            onOk: async () => {
                setBusy("discard");
                setError("");
                try {
                    await discardBackupState();
                    message.success(t("config.projectBackup.discarded"));
                    setStateHeld(false);
                } catch (cause) {
                    setError(describe(cause, t("config.projectBackup.discardFailed")));
                } finally {
                    setBusy("");
                }
            },
        });
    };

    if (!available) {
        return (
            <section className="rounded-lg border border-stone-200 p-3 dark:border-stone-800" data-testid="project-backup">
                <div className="mb-1 text-sm font-semibold">{t("config.projectBackup.title")}</div>
                <Alert
                    className="mt-2"
                    type="info"
                    showIcon
                    message={t("config.projectBackup.noCoreTitle")}
                    description={t("config.projectBackup.noCoreBody")}
                    data-testid="project-backup-no-core"
                />
            </section>
        );
    }

    return (
        <section className="rounded-lg border border-stone-200 p-3 dark:border-stone-800" data-testid="project-backup">
            <div className="mb-1 text-sm font-semibold">{t("config.projectBackup.title")}</div>
            <div className="mb-3 text-xs text-stone-500">{t("config.projectBackup.description")}</div>

            {stateHeld ? (
                <Alert
                    className="mb-3"
                    type="warning"
                    showIcon
                    message={t("config.projectBackup.stateHeldTitle")}
                    description={
                        <div className="space-y-2">
                            <div>{t("config.projectBackup.stateHeldBody")}</div>
                            <Button size="small" danger icon={<Trash2 className="size-3.5" />} loading={busy === "discard"} onClick={handleDiscard} data-testid="project-backup-discard">
                                {t("config.projectBackup.discard")}
                            </Button>
                        </div>
                    }
                    data-testid="project-backup-state-held"
                />
            ) : null}

            {error !== "" ? <Alert className="mb-3" type="error" showIcon message={error} data-testid="project-backup-error" /> : null}

            {restored !== null ? <RestoreOutcome result={restored} onDismiss={() => setRestored(null)} /> : null}

            <Space wrap>
                <Button icon={<Download className="size-4" />} loading={busy === "export"} onClick={() => void handleExport()} data-testid="project-backup-export">
                    {busy === "export" ? t("config.projectBackup.exporting") : t("config.projectBackup.export")}
                </Button>
                <Button icon={<FileUp className="size-4" />} loading={busy === "preview"} onClick={() => inputRef.current?.click()} data-testid="project-backup-choose">
                    {busy === "preview" ? t("config.projectBackup.reading") : t("config.projectBackup.restore")}
                </Button>
            </Space>
            <input ref={inputRef} type="file" accept="application/zip,.zip" className="hidden" onChange={(event) => void handleChoose(event.target.files?.[0])} />

            <Modal
                open={previewOpen}
                title={t("config.projectBackup.previewTitle")}
                okText={t("config.projectBackup.previewContinue")}
                cancelText={t("common.cancel")}
                onOk={() => handleRestore()}
                // Cancelling the preview drops the archive with it: the next
                // press of the restore button should start from a file the user
                // chose, not from one they had already decided against.
                onCancel={() => {
                    setPreviewOpen(false);
                    setArchive(null);
                }}
            >
                {archive ? <PreviewBody name={archive.name} preview={archive.preview} /> : null}
            </Modal>
        </section>
    );
}

/**
 * PreviewBody shows what an archive holds, which is what the confirmation is FOR.
 *
 * The three counts the user is asked to weigh — projects, assets, files — come
 * first, and the archive's own identity (when it was made, by which build, on
 * which database version) follows: an archive from a much older application
 * version is exactly the thing a user should notice BEFORE agreeing to replace
 * their work with it.
 */
function PreviewBody({ name, preview }: { name: string; preview: desktop.BackupPreview }) {
    const { t } = useTranslation();
    // `formatBytes` answers "" for a zero or unreadable size, which would leave a
    // label with nothing beside it. An archive with no stored media is a real
    // case — a database-only backup — so the zero is spelled out rather than
    // dropped.
    const size = (bytes: number) => formatBytes(bytes) || t("config.projectBackup.bytesZero");
    const rows: Array<[string, string]> = [
        [t("config.projectBackup.createdAt"), preview.createdAt],
        [t("config.projectBackup.appVersion"), preview.appVersion],
        [t("config.projectBackup.schemaVersion"), String(preview.schemaVersion)],
        [t("config.projectBackup.databaseBytes"), size(preview.databaseBytes)],
        [t("config.projectBackup.fileBytes"), size(preview.fileBytes)],
    ];
    return (
        <div className="space-y-3" data-testid="project-backup-preview">
            <Typography.Paragraph className="text-sm text-stone-500">{t("config.projectBackup.previewDescription")}</Typography.Paragraph>
            <div className="flex flex-wrap gap-4 rounded-lg border border-stone-200 px-4 py-3 dark:border-stone-800">
                <PreviewCount field="projects" value={preview.projects} />
                <PreviewCount field="assets" value={preview.assets} />
                <PreviewCount field="files" value={preview.files} />
            </div>
            <Descriptions size="small" column={1} items={rows.map(([label, value]) => ({ key: label, label, children: value }))} />
            {/* The chosen file's own name, so a user who picked the wrong archive
                out of a folder of them can see that before confirming. */}
            <Typography.Text type="secondary" className="text-xs">
                {name}
            </Typography.Text>
        </div>
    );
}

function PreviewCount({ field, value }: { field: "projects" | "assets" | "files"; value: number }) {
    const { t } = useTranslation();
    return (
        <div>
            <div className="text-xs text-stone-500">{t(`config.projectBackup.${field}`)}</div>
            {/* Keyed by the FIELD rather than by its translated label: a test id
                built from a label would change with the language and stop
                identifying the same number. */}
            <div className="text-lg font-semibold" data-testid={`project-backup-count-${field}`}>
                {value}
            </div>
        </div>
    );
}

/**
 * RestoreOutcome reports what a restore did, including the parts it would be
 * dishonest to leave out.
 *
 * `rolledBack` decides the tone, not the mere arrival of a result: the promotion
 * failing and the previous state being put back is a FAILURE, and showing it in a
 * success colour would be the panel telling a user their data had been replaced
 * when it had not. `restartRequired` is rendered whenever the core reports it
 * rather than assumed, and `previousDatabasePath` is shown because it names the
 * only copy of what the restore displaced.
 */
function RestoreOutcome({ result, onDismiss }: { result: desktop.RestoreResult; onDismiss: () => void }) {
    const { t } = useTranslation();
    return (
        <Alert
            className="mb-3"
            type={result.rolledBack ? "error" : "success"}
            showIcon
            message={result.rolledBack ? t("config.projectBackup.restoreFailed") : t("config.projectBackup.restored")}
            data-testid="project-backup-outcome"
            description={
                <div className="space-y-2" data-testid="project-backup-restore-result">
                    {result.rolledBack ? <div>{t("config.projectBackup.rolledBack")}</div> : null}
                    <div>
                        {t("config.projectBackup.restoreCounts", {
                            projects: result.projects,
                            assets: result.assets,
                            files: result.files,
                        })}
                    </div>
                    {result.restartRequired ? <div data-testid="project-backup-restart">{t("config.projectBackup.restartRequired")}</div> : null}
                    {result.previousDatabasePath ? (
                        <div className="break-all" data-testid="project-backup-previous-path">
                            {t("config.projectBackup.previousKept", { path: result.previousDatabasePath })}
                        </div>
                    ) : null}
                    {!result.rolledBack ? (
                        <Button size="small" onClick={onDismiss}>
                            {t("common.done")}
                        </Button>
                    ) : null}
                </div>
            }
        />
    );
}

/** describe turns any thrown value into a message the user can read. */
function describe(cause: unknown, fallback: string): string {
    return cause instanceof Error && cause.message !== "" ? cause.message : fallback;
}

export default ProjectBackupPanel;
