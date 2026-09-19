import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Alert, App, Button, Empty, Input, InputNumber, Modal, Space, Table, Tag } from "antd";
import type { ColumnsType } from "antd/es/table";
import { FileUp, ListChecks, PencilLine, Sparkles, Upload } from "lucide-react";
import { useTranslation } from "react-i18next";

import {
    confirmChapters,
    extractChapterEventCandidates,
    importDocumentByChunks,
    isImportBindingsAvailable,
    listChapters,
    mergeChapter,
    splitChapter,
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
            // The precheck still crosses in one message, and that is a known
            // limit rather than an oversight: the transfer that feeds the IMPORT
            // is chunked, but the preview is a separate call and this one builds
            // the DTO's number array. For a 100,000-character novel that is about
            // 300,000 numbers, which the browser and the Go decoder each build
            // once.
            //
            // It is bounded by the domain's own input ceiling, so it cannot grow
            // without limit, and the acceptance criterion is stated about the
            // reader: the chapter panel pages the text through ReadDocumentRange,
            // whose response is capped by the core. This is recorded in the STATUS
            // record as a remaining cost rather than claimed to be free.
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
            // The chunked path, not the one-shot one: a 100,000-character novel is
            // about 300 KB, and `Array.from` over it would build a 300,000-element
            // array on the main thread — the opposite of the property the
            // acceptance criterion asks for. The chunk ceiling comes from the core.
            const result = await importDocumentByChunks({
                projectId,
                name: fileName,
                format: extensionOf(fileName),
                // The view is handed over as-is; nothing here builds the DTO's
                // number array.
                bytes: content,
                confirmDuplicate,
            });
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
    const [splitTarget, setSplitTarget] = useState<{ id: string; revision: number; at: number; title: string } | null>(null);

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

    /**
     * openPage reads one page, forwards or backwards from an anchor.
     *
     * The direction is explicit because the range API cannot express a backward
     * page: a non-positive endRune means "to the end of the document", so asking
     * for a page ending before an offset used to produce one running forward from
     * it. The previous-page control was therefore broken — it moved one rune back
     * and a whole page forward — and an independent review caught it in the UI.
     *
     * totalRunes is passed as a hint so a backward read does not measure the text
     * again; the core clamps it rather than trusting it.
     */
    const openPage = async (anchor: number, direction: "forward" | "backward" = "forward") => {
        setPageBusy(true);
        try {
            const result = await readDocumentRange({
                sourceDocumentVersionId: versionId,
                startRune: anchor,
                endRune: 0,
                direction,
                totalRunes: page?.totalRunes ?? 0,
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

    /**
     * splitAt divides a chapter at the rune offset the user chose.
     *
     * PRD FR-020 lists 手动合并/拆分 among the import flow's MUST items, so this is
     * required rather than a convenience. The offset is entered as a character
     * position inside the chapter, converted here to the version coordinate the
     * command expects.
     */
    const splitAt = async () => {
        if (!splitTarget) return;
        setBusy(true);
        try {
            await splitChapter({
                chapterId: splitTarget.id,
                splitAtOffset: splitTarget.at,
                secondTitle: splitTarget.title,
                revision: splitTarget.revision,
            } as desktop.SplitChapterRequest);
            setSplitTarget(null);
            message.success(t("studio.chapters.splitDone"));
            await load();
            onChanged?.();
        } catch (caught) {
            message.error(caught instanceof Error ? caught.message : t("studio.chapters.splitFailed"));
        } finally {
            setBusy(false);
        }
    };

    /** mergeWith absorbs the NEXT chapter into this one. */
    const mergeWith = async (first: string, second: string, revision: number) => {
        setBusy(true);
        try {
            await mergeChapter({ firstChapterId: first, secondChapterId: second, revision } as desktop.MergeChapterRequest);
            message.success(t("studio.chapters.mergeDone"));
            await load();
            onChanged?.();
        } catch (caught) {
            message.error(caught instanceof Error ? caught.message : t("studio.chapters.mergeFailed"));
        } finally {
            setBusy(false);
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
                    <Button
                        size="small"
                        data-testid={`studio-chapter-split-${record.id}`}
                        onClick={() => setSplitTarget({ id: record.id, revision: record.revision, at: record.startOffset + 1, title: "" })}
                    >
                        {t("studio.chapters.split")}
                    </Button>
                    {/* Merging needs a chapter to absorb, so the last one has none. */}
                    {rows.some((row) => row.ordinal === record.ordinal + 1) ? (
                        <Button
                            size="small"
                            disabled={busy}
                            data-testid={`studio-chapter-merge-${record.id}`}
                            onClick={() => {
                                const next = rows.find((row) => row.ordinal === record.ordinal + 1);
                                if (next) void mergeWith(record.id, next.id, record.revision);
                            }}
                        >
                            {t("studio.chapters.merge")}
                        </Button>
                    ) : null}
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

            {splitTarget ? (
                <section className="rounded-xl border border-stone-200 p-4 dark:border-stone-800" data-testid="studio-split-panel">
                    <h4 className="mb-2 text-sm font-medium">{t("studio.chapters.splitTitle")}</h4>
                    <p className="mb-3 text-xs text-stone-500">{t("studio.chapters.splitNote")}</p>
                    <div className="flex flex-wrap items-end gap-2">
                        <label>
                            <span className="mb-1 block text-xs">{t("studio.chapters.splitAt")}</span>
                            <InputNumber
                                value={splitTarget.at}
                                data-testid="studio-split-offset"
                                onChange={(value) => setSplitTarget((current) => (current ? { ...current, at: Number(value ?? 0) } : current))}
                            />
                        </label>
                        <label className="min-w-52 flex-1">
                            <span className="mb-1 block text-xs">{t("studio.chapters.secondTitle")}</span>
                            <Input
                                value={splitTarget.title}
                                maxLength={200}
                                data-testid="studio-split-title"
                                onChange={(event) => setSplitTarget((current) => (current ? { ...current, title: event.target.value } : current))}
                            />
                        </label>
                        <Button type="primary" size="small" loading={busy} data-testid="studio-split-submit" onClick={() => void splitAt()}>
                            {t("studio.chapters.split")}
                        </Button>
                        <Button size="small" type="text" onClick={() => setSplitTarget(null)}>
                            {t("common.cancel")}
                        </Button>
                    </div>
                </section>
            ) : null}

            {page ? (
                <section className="rounded-xl border border-stone-200 p-4 dark:border-stone-800" data-testid="studio-chapter-page">
                    <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
                        <h4 className="text-sm font-medium">
                            {t("studio.chapters.pageLabel", { from: page.startRune + 1, to: page.endRune, total: page.totalRunes })}
                        </h4>
                        <Space size="small">
                            <Button
                                size="small"
                                disabled={pageBusy || page.startRune === 0}
                                data-testid="studio-chapter-previous"
                                onClick={() => void openPage(page.startRune, "backward")}
                            >
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
