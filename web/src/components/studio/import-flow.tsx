import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Alert, App, Button, Empty, Input, Modal, Popconfirm, Select, Space, Table, Tag } from "antd";
import type { ColumnsType } from "antd/es/table";
import { FileUp, ListChecks, PencilLine, Sparkles, Upload } from "lucide-react";
import { useTranslation } from "react-i18next";

import {
    confirmChapters,
    extractChapterEventCandidates,
    importDocument,
    isImportBindingsAvailable,
    listChapters,
    precheckImport,
    readDocumentRange,
} from "@/services/desktop/drama";
import type { desktop } from "@/wailsjs/go/models";

/**
 * The document import flow: choose a file, review what it would do, confirm.
 *
 * Three rules this component keeps, all of them about not lying to the user:
 *
 * 1. The preview is a real preview. Every number shown comes from the core's own
 *    precheck, which reads the document and writes nothing. Nothing here guesses
 *    a chapter count from a byte length or a line count.
 * 2. A duplicate is reported before the write, not after. The core refuses a
 *    re-import by default, and the confirmation step is what turns that refusal
 *    into a decision the user makes rather than an error they hit.
 * 3. An unavailable binding says so. A browser session has no core, so the flow
 *    renders an explanation instead of a control that cannot work.
 *
 * The text is read a page at a time through the core's paged reader, so a
 * 100,000-character document does not arrive in one response. The page size is
 * the core's ceiling rather than a number chosen here.
 */

export type ImportFlowProps = {
    projectId: string;
    onImported: () => void;
};

/** ACCEPTED_EXTENSIONS is what the file picker offers; the bytes still decide. */
const ACCEPTED_EXTENSIONS = ".txt,.md,.markdown,.docx";

export function ImportFlow({ projectId, onImported }: ImportFlowProps) {
    const { t } = useTranslation();
    const { message } = App.useApp();
    const [open, setOpen] = useState(false);
    const [fileName, setFileName] = useState("");
    const [content, setContent] = useState<Uint8Array | null>(null);
    const [preview, setPreview] = useState<desktop.PrecheckImportResult | null>(null);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [confirmDuplicate, setConfirmDuplicate] = useState(false);
    const fileInput = useRef<HTMLInputElement | null>(null);

    const available = isImportBindingsAvailable();

    const reset = useCallback(() => {
        setFileName("");
        setContent(null);
        setPreview(null);
        setError(null);
        setConfirmDuplicate(false);
        if (fileInput.current) fileInput.current.value = "";
    }, []);

    const chooseFile = async (file: File) => {
        setBusy(true);
        setError(null);
        setPreview(null);
        setConfirmDuplicate(false);
        try {
            // arrayBuffer rather than text(): a DOCX is a ZIP, so decoding it as
            // text would corrupt it before the core ever sees the bytes.
            const buffer = await file.arrayBuffer();
            const bytes = new Uint8Array(buffer);
            setFileName(file.name);
            setContent(bytes);
            const result = await precheckImport({
                projectId,
                name: file.name,
                format: extensionOf(file.name),
                content: Array.from(bytes),
            } as desktop.PrecheckImportRequest);
            setPreview(result);
        } catch (caught) {
            setError(caught instanceof Error ? caught.message : t("studio.import.precheckFailed"));
        } finally {
            setBusy(false);
        }
    };

    const submit = async () => {
        if (!content) return;
        setBusy(true);
        setError(null);
        try {
            const result = await importDocument({
                projectId,
                name: fileName,
                format: extensionOf(fileName),
                content: Array.from(content),
                confirmDuplicate,
            } as desktop.ImportDocumentRequest);
            message.success(
                t("studio.import.stored", {
                    chapters: result.chapters.length,
                    characters: result.charCount,
                }),
            );
            setOpen(false);
            reset();
            onImported();
        } catch (caught) {
            setError(caught instanceof Error ? caught.message : t("studio.import.importFailed"));
        } finally {
            setBusy(false);
        }
    };

    if (!available) {
        return (
            <Alert
                type="info"
                showIcon
                message={t("studio.import.desktopOnlyTitle")}
                description={t("studio.import.desktopOnlyBody")}
                data-testid="studio-import-desktop-only"
            />
        );
    }

    return (
        <div className="space-y-4">
            <div className="flex flex-wrap items-center gap-3">
                <Button
                    type="primary"
                    icon={<Upload className="size-4" />}
                    data-testid="studio-import-open"
                    onClick={() => {
                        reset();
                        setOpen(true);
                    }}
                >
                    {t("studio.import.open")}
                </Button>
                <span className="text-xs text-stone-500">{t("studio.import.accepted")}</span>
            </div>

            <Modal
                open={open}
                title={t("studio.import.title")}
                onCancel={() => {
                    setOpen(false);
                    reset();
                }}
                onOk={() => void submit()}
                okButtonProps={{
                    disabled: !preview || busy || (preview.duplicate && !confirmDuplicate),
                    loading: busy,
                }}
                okText={t("studio.import.confirm")}
                cancelText={t("common.cancel")}
                width={720}
                destroyOnHidden
            >
                <div className="space-y-4">
                    <label className="flex flex-wrap items-center gap-3">
                        <input
                            ref={fileInput}
                            type="file"
                            accept={ACCEPTED_EXTENSIONS}
                            className="hidden"
                            data-testid="studio-import-file"
                            onChange={(event) => {
                                const file = event.target.files?.[0];
                                if (file) void chooseFile(file);
                            }}
                        />
                        <Button icon={<FileUp className="size-4" />} onClick={() => fileInput.current?.click()} data-testid="studio-import-choose">
                            {t("studio.import.choose")}
                        </Button>
                        <span className="text-sm text-stone-600 dark:text-stone-300" data-testid="studio-import-filename">
                            {fileName || t("studio.import.noFile")}
                        </span>
                    </label>

                    {error ? <Alert type="error" showIcon message={error} data-testid="studio-import-error" /> : null}

                    {preview ? (
                        <div className="space-y-3" data-testid="studio-import-preview">
                            <dl className="grid grid-cols-2 gap-x-6 gap-y-2 text-sm sm:grid-cols-4">
                                <PreviewField label={t("studio.import.format")} value={preview.format} testId="studio-import-format" />
                                <PreviewField label={t("studio.import.encoding")} value={preview.encoding} testId="studio-import-encoding" />
                                <PreviewField
                                    label={t("studio.import.characters")}
                                    value={String(preview.charCount)}
                                    testId="studio-import-charcount"
                                />
                                <PreviewField
                                    label={t("studio.import.chapters")}
                                    value={String(preview.chapterCount)}
                                    testId="studio-import-chaptercount"
                                />
                            </dl>

                            {preview.duplicate ? (
                                <Alert
                                    type="warning"
                                    showIcon
                                    message={t("studio.import.duplicateTitle")}
                                    description={t("studio.import.duplicateBody", {
                                        name: preview.duplicateDocumentName ?? preview.duplicateDocumentId ?? "",
                                    })}
                                    data-testid="studio-import-duplicate"
                                    action={
                                        <label className="flex items-center gap-2 whitespace-nowrap text-sm">
                                            <input
                                                type="checkbox"
                                                checked={confirmDuplicate}
                                                data-testid="studio-import-confirm-duplicate"
                                                onChange={(event) => setConfirmDuplicate(event.target.checked)}
                                            />
                                            {t("studio.import.duplicateConfirm")}
                                        </label>
                                    }
                                />
                            ) : null}

                            {preview.warnings.length > 0 ? (
                                <Alert
                                    type="info"
                                    showIcon
                                    message={t("studio.import.warnings")}
                                    description={
                                        <ul className="list-disc pl-4">
                                            {preview.warnings.map((warning, index) => (
                                                <li key={index}>{warning}</li>
                                            ))}
                                        </ul>
                                    }
                                    data-testid="studio-import-warnings"
                                />
                            ) : null}

                            {preview.chapters.length > 0 ? (
                                <div>
                                    <h4 className="mb-1 text-sm font-medium">{t("studio.import.boundaries")}</h4>
                                    <p className="mb-2 text-xs text-stone-500">
                                        {t("studio.import.boundariesNote", { shown: preview.chapters.length, total: preview.chapterCount })}
                                    </p>
                                    <ul className="max-h-48 space-y-1 overflow-y-auto text-sm" data-testid="studio-import-boundaries">
                                        {preview.chapters.map((boundary) => (
                                            <li key={boundary.ordinal} className="flex items-center gap-2">
                                                <span className="w-8 shrink-0 text-right text-stone-500">{boundary.ordinal}</span>
                                                <span className="truncate">{boundary.title || t("studio.import.untitled")}</span>
                                                <Tag className="shrink-0">{t(`studio.import.source.${boundary.source}`, { defaultValue: boundary.source })}</Tag>
                                                <span className="shrink-0 text-xs text-stone-400">
                                                    {boundary.startOffset}–{boundary.endOffset}
                                                </span>
                                            </li>
                                        ))}
                                    </ul>
                                </div>
                            ) : null}
                        </div>
                    ) : null}
                </div>
            </Modal>
        </div>
    );
}

/** PreviewField is one labelled value in the precheck summary. */
function PreviewField({ label, value, testId }: { label: string; value: string; testId: string }) {
    return (
        <div>
            <dt className="text-xs text-stone-500">{label}</dt>
            <dd className="font-medium" data-testid={testId}>
                {value || "—"}
            </dd>
        </div>
    );
}

/** extensionOf derives the format hint from a file name, lower-cased. */
function extensionOf(fileName: string): string {
    const index = fileName.lastIndexOf(".");
    return index < 0 ? "" : fileName.slice(index + 1).toLowerCase();
}

// ---------------------------------------------------------------------------
// The chapter panel: the boundaries of an imported version, readable in pages
// ---------------------------------------------------------------------------

export type ChapterPanelProps = {
    /** The version whose chapters are shown. Empty means nothing is selected. */
    versionId: string;
    /** Bump this to reload, so a caller does not have to own the reload logic. */
    reloadToken?: number;
    onChanged?: () => void;
};

/**
 * ChapterPanel lists a version's boundaries and confirms them.
 *
 * It also shows the text one page at a time, which is what makes a 100,000
 * character document readable without blocking: the core returns at most one
 * page per call and this component only ever holds the page it is showing.
 */
export function ChapterPanel({ versionId, reloadToken = 0, onChanged }: ChapterPanelProps) {
    const { t } = useTranslation();
    const { message } = App.useApp();
    const [rows, setRows] = useState<desktop.ChapterDTO[]>([]);
    const [loading, setLoading] = useState(false);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [page, setPage] = useState<{ text: string; startRune: number; endRune: number; totalRunes: number } | null>(null);
    const [pageBusy, setPageBusy] = useState(false);
    const [extracting, setExtracting] = useState<string | null>(null);

    const load = useCallback(async () => {
        if (!versionId) {
            setRows([]);
            return;
        }
        setLoading(true);
        setError(null);
        try {
            setRows(await listChapters(versionId));
        } catch (caught) {
            setError(caught instanceof Error ? caught.message : t("studio.shell.loadFailed"));
        } finally {
            setLoading(false);
        }
    }, [versionId, t]);

    useEffect(() => {
        void load();
    }, [load, reloadToken]);

    const confirm = async () => {
        setBusy(true);
        try {
            const updated = await confirmChapters({ sourceDocumentVersionId: versionId } as desktop.ConfirmChaptersRequestDTO);
            setRows(updated);
            message.success(t("studio.chapters.confirmed"));
            onChanged?.();
        } catch (caught) {
            message.error(caught instanceof Error ? caught.message : t("studio.chapters.confirmFailed"));
        } finally {
            setBusy(false);
        }
    };

    const openPage = async (startRune: number) => {
        setPageBusy(true);
        try {
            const result = await readDocumentRange({
                sourceDocumentVersionId: versionId,
                startRune,
                endRune: 0,
            } as desktop.ReadDocumentRangeRequest);
            setPage(result);
        } catch (caught) {
            message.error(caught instanceof Error ? caught.message : t("studio.chapters.readFailed"));
        } finally {
            setPageBusy(false);
        }
    };

    const extract = async (chapterID: string) => {
        setExtracting(chapterID);
        try {
            const result = await extractChapterEventCandidates({ chapterId: chapterID } as desktop.ExtractChapterRequest);
            message.success(
                t("studio.chapters.extracted", {
                    entities: result.entities,
                    events: result.events,
                    relations: result.relations,
                }),
            );
            onChanged?.();
        } catch (caught) {
            message.error(caught instanceof Error ? caught.message : t("studio.chapters.extractFailed"));
        } finally {
            setExtracting(null);
        }
    };

    const pendingConfirmation = useMemo(() => rows.some((row) => row.status === "detected"), [rows]);

    const columns: ColumnsType<desktop.ChapterDTO> = [
        { title: t("studio.chapters.ordinal"), dataIndex: "ordinal", key: "ordinal", width: 64 },
        { title: t("studio.chapters.titleLabel"), dataIndex: "title", key: "title", ellipsis: true },
        {
            title: t("studio.chapters.range"),
            key: "range",
            width: 160,
            render: (_, record) => (
                <span className="text-xs text-stone-500">
                    {record.startOffset}–{record.endOffset}
                </span>
            ),
        },
        {
            title: t("studio.source.statusLabel"),
            dataIndex: "status",
            key: "status",
            width: 110,
            render: (value: string) => <Tag>{t(`studio.status.${value}`, { defaultValue: value })}</Tag>,
        },
        {
            title: t("studio.chapters.actions"),
            key: "actions",
            width: 190,
            render: (_, record) => (
                <Space size="small">
                    <Button size="small" icon={<PencilLine className="size-3.5" />} onClick={() => void openPage(record.startOffset)}>
                        {t("studio.chapters.read")}
                    </Button>
                    <Button
                        size="small"
                        icon={<Sparkles className="size-3.5" />}
                        loading={extracting === record.id}
                        data-testid={`studio-chapter-extract-${record.id}`}
                        onClick={() => void extract(record.id)}
                    >
                        {t("studio.chapters.extract")}
                    </Button>
                </Space>
            ),
        },
    ];

    if (!versionId) {
        return <Empty description={t("studio.chapters.selectVersion")} />;
    }

    return (
        <div className="space-y-4">
            <div className="flex flex-wrap items-center justify-between gap-3">
                <h3 className="text-sm font-medium">{t("studio.source.chaptersLabel")}</h3>
                <Button
                    size="small"
                    type="primary"
                    icon={<ListChecks className="size-4" />}
                    loading={busy}
                    disabled={!pendingConfirmation}
                    data-testid="studio-chapters-confirm"
                    onClick={() => void confirm()}
                >
                    {t("studio.chapters.confirm")}
                </Button>
            </div>

            {error ? <Alert type="error" showIcon message={error} /> : null}

            {rows.length === 0 && !loading ? (
                <Empty description={t("studio.chapters.empty")} />
            ) : (
                <Table<desktop.ChapterDTO>
                    rowKey="id"
                    size="small"
                    loading={loading}
                    pagination={false}
                    columns={columns}
                    dataSource={rows}
                    data-testid="studio-chapters-table"
                />
            )}

            {page ? (
                <section className="rounded-xl border border-stone-200 p-4 dark:border-stone-800" data-testid="studio-chapter-page">
                    <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
                        <h4 className="text-sm font-medium">
                            {t("studio.chapters.pageLabel", { from: page.startRune + 1, to: page.endRune, total: page.totalRunes })}
                        </h4>
                        <Space size="small">
                            <Button size="small" disabled={pageBusy || page.startRune === 0} onClick={() => void openPage(Math.max(0, page.startRune - 1))}>
                                {t("studio.chapters.previous")}
                            </Button>
                            <Button
                                size="small"
                                disabled={pageBusy || page.endRune >= page.totalRunes}
                                data-testid="studio-chapter-next"
                                onClick={() => void openPage(page.endRune)}
                            >
                                {t("studio.chapters.next")}
                            </Button>
                            <Button size="small" type="text" onClick={() => setPage(null)}>
                                {t("common.cancel")}
                            </Button>
                        </Space>
                    </div>
                    <pre className="max-h-80 overflow-auto whitespace-pre-wrap break-words text-sm leading-6" data-testid="studio-chapter-text">
                        {page.text}
                    </pre>
                </section>
            ) : null}
        </div>
    );
}
