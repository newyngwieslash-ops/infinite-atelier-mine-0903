import { useCallback, useEffect, useState } from "react";
import { Alert, Button, Drawer, Empty, Input, Modal, Select, Space, Table, Tag, Tooltip, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Lock, LockOpen, Sparkles, Trash2 } from "lucide-react";
import { useTranslation } from "react-i18next";

import {
    deleteMemory,
    getMemory,
    isMemoryBindingsAvailable,
    isMemoryCommandsAvailable,
    isMemoryRebuildAvailable,
    listMemories,
    listSummarySources,
    listMemoryEntityLinks,
    previewMemoryRecall,
    rebuildMemoryEmbedding,
    setMemoryLocked,
    summarizeMemory,
    updateMemoryContent,
} from "@/services/desktop/memory";
import type { desktop } from "@/wailsjs/go/models";

/**
 * MemoryCenterSection is WP-10's surface: the persistent memory of PRD FR-120.
 *
 * Four rules it keeps, and each is a rule the specification states rather than a
 * preference:
 *
 *  1. IT NEVER RENDERS A READ THAT FAILED AS EMPTY. "The core could not be reached" and
 *     "nothing is remembered" are different situations and only one of them is true.
 *  2. EVERY COMMAND SAYS WHAT IT WILL DO BEFORE IT DOES IT. Deleting invalidates summaries;
 *     editing clears the vector, so the memory stops being searchable until a rebuild. Both
 *     are consequences a user has to agree to, and both are confirmed rather than performed.
 *  3. THE RECALL PREVIEW SHOWS THE NUMBERS. FR-120 requires a threshold and forbids returning
 *     low-relevance results, so the preview shows each candidate's raw similarity beside its
 *     fused score — which is what makes "why did the agent not remember" answerable instead of
 *     mysterious.
 *  4. A SUMMARY'S SOURCES ARE FOLLOWABLE. AC-MEM-004 asks that a UI can jump to the original
 *     message, so a summary opens a drawer listing its sources with their own role, agent and
 *     time, and a source whose row is gone is shown as a hole rather than dropped.
 *
 * The section reads one project at a time, because a memory's scope names a project: showing
 * two projects' memories in one list is the cross-project leak DOMAIN_MODEL section 14.5
 * forbids, and a UI that showed them together would be that leak with a heading on it.
 */
export type MemoryCenterSectionProps = {
    projectId: string;
};

export function MemoryCenterSection({ projectId }: MemoryCenterSectionProps) {
    const { t } = useTranslation();
    const [memories, setMemories] = useState<desktop.MemoryDTO[] | null>(null);
    const [loadError, setLoadError] = useState("");
    const [typeFilter, setTypeFilter] = useState<string[]>([]);
    const [includeDeleted, setIncludeDeleted] = useState(false);
    const [busy, setBusy] = useState("");
    const [selected, setSelected] = useState<desktop.MemoryDTO | null>(null);
    const [sources, setSources] = useState<desktop.MemorySummarySourceDTO[] | null>(null);
    const [links, setLinks] = useState<desktop.MemoryEntityLinkDTO[]>([]);
    const [preview, setPreview] = useState<desktop.MemoryRecallPreviewDTO | null>(null);
    const [previewQuery, setPreviewQuery] = useState("");
    const [previewError, setPreviewError] = useState("");
    const [editing, setEditing] = useState<desktop.MemoryDTO | null>(null);
    const [editText, setEditText] = useState("");
    const [deleting, setDeleting] = useState<desktop.MemoryDTO | null>(null);
    const [deleteSummaries, setDeleteSummaries] = useState(false);
    const [notice, setNotice] = useState("");

    const bindingsAvailable = isMemoryBindingsAvailable();
    const commandsAvailable = isMemoryCommandsAvailable();

    const load = useCallback(async () => {
        if (!projectId) return;
        setLoadError("");
        try {
            const rows = await listMemories({
                projectId,
                types: typeFilter,
                includeDeleted,
                limit: 200,
            } as never);
            setMemories(rows);
        } catch (error) {
            setLoadError(error instanceof Error ? error.message : t("studio.memory.loadFailed"));
        }
    }, [projectId, typeFilter, includeDeleted, t]);

    useEffect(() => {
        void load();
    }, [load]);

    const openDetail = async (memory: desktop.MemoryDTO) => {
        setSelected(memory);
        setSources(null);
        setLinks([]);
        try {
            setLinks(await listMemoryEntityLinks(memory.id));
        } catch {
            // A link read that fails leaves the list empty; it is not worth an alert over a
            // secondary panel, and the memory itself is already on screen.
            setLinks([]);
        }
        if (memory.type === "summary") {
            try {
                setSources(await listSummarySources(memory.id));
            } catch {
                setSources([]);
            }
        }
    };

    /**
     * openDetailById opens one memory's detail pane from its identifier.
     *
     * It is what makes a summary's sources and its transcript citations FOLLOWABLE rather than merely
     * listed: the criterion AC-MEM-004 states is "UI 可跳原始消息", and a jump needs a handler. A read
     * that fails sets the notice rather than opening an empty drawer, so a gone row is visible as gone.
     */
    const openDetailById = async (memoryId: string) => {
        try {
            const memory = await getMemory(memoryId);
            if (!memory) {
                setNotice(t("studio.memory.sourceGone"));
                return;
            }
            await openDetail(memory);
        } catch (error) {
            setNotice(error instanceof Error ? error.message : t("studio.memory.commandFailed"));
        }
    };

    const runPreview = async () => {
        setPreviewError("");
        try {
            const result = await previewMemoryRecall({
                projectId,
                query: previewQuery,
            } as never);
            setPreview(result ?? null);
        } catch (error) {
            setPreviewError(error instanceof Error ? error.message : t("studio.memory.previewFailed"));
            setPreview(null);
        }
    };

    const togglePin = async (memory: desktop.MemoryDTO) => {
        setBusy(memory.id);
        try {
            await setMemoryLocked({ memoryId: memory.id, locked: !memory.locked } as never);
            setNotice(t(memory.locked ? "studio.memory.unpinned" : "studio.memory.pinned"));
            await load();
        } catch (error) {
            setNotice(error instanceof Error ? error.message : t("studio.memory.commandFailed"));
        } finally {
            setBusy("");
        }
    };

    const applyEdit = async () => {
        if (!editing) return;
        setBusy(editing.id);
        try {
            await updateMemoryContent({ memoryId: editing.id, content: editText, confirm: true } as never);
            setNotice(t("studio.memory.edited"));
            setEditing(null);
            await load();
        } catch (error) {
            setNotice(error instanceof Error ? error.message : t("studio.memory.commandFailed"));
        } finally {
            setBusy("");
        }
    };

    const applyDelete = async () => {
        if (!deleting) return;
        setBusy(deleting.id);
        try {
            const result = await deleteMemory({
                memoryId: deleting.id,
                invalidateSummaries: deleteSummaries,
                confirm: true,
            } as never);
            setNotice(
                result.invalidatedSummaries.length > 0
                    ? t("studio.memory.deletedWithSummaries", { count: result.invalidatedSummaries.length })
                    : t("studio.memory.deleted"),
            );
            setDeleting(null);
            setDeleteSummaries(false);
            await load();
        } catch (error) {
            setNotice(error instanceof Error ? error.message : t("studio.memory.commandFailed"));
        } finally {
            setBusy("");
        }
    };

    const rebuild = async () => {
        setBusy("rebuild");
        try {
            const result = await rebuildMemoryEmbedding({ projectId } as never);
            setNotice(
                t("studio.memory.rebuilt", {
                    rebuilt: result.rebuilt,
                    failed: result.failed,
                    model: result.model,
                    version: result.version,
                }),
            );
            await load();
        } catch (error) {
            setNotice(error instanceof Error ? error.message : t("studio.memory.commandFailed"));
        } finally {
            setBusy("");
        }
    };

    const condense = async () => {
        setBusy("summarize");
        try {
            const result = await summarizeMemory({ projectId, embed: true } as never);
            setNotice(result.created ? t("studio.memory.summarised") : t("studio.memory.nothingToSummarise"));
            await load();
        } catch (error) {
            setNotice(error instanceof Error ? error.message : t("studio.memory.commandFailed"));
        } finally {
            setBusy("");
        }
    };

    const columns: ColumnsType<desktop.MemoryDTO> = [
        {
            title: t("studio.memory.typeLabel"),
            dataIndex: "type",
            key: "type",
            width: 120,
            render: (value: string) => <Tag>{t(`studio.memory.type.${value}`, { defaultValue: value })}</Tag>,
        },
        {
            title: t("studio.memory.contentLabel"),
            dataIndex: "content",
            key: "content",
            render: (value: string, row) => (
                <div className="min-w-0">
                    <div className="truncate">{value}</div>
                    <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-stone-500">
                        {row.role ? <span>{t("studio.memory.roleLabel")}: {row.role}</span> : null}
                        {row.agentKey ? <span>{row.agentKey}</span> : null}
                        {row.scopeEpisode ? <span>{row.scopeEpisode}</span> : null}
                        {/* The two states a user has to be able to see: whether this memory is
                            searchable, and whether it is pinned against change. */}
                        {row.embedded ? (
                            <span data-memory-embedded={row.id}>
                                {t("studio.memory.embedded", { model: row.embeddingModel })}
                            </span>
                        ) : (
                            <span data-memory-unembedded={row.id}>{t("studio.memory.notEmbedded")}</span>
                        )}
                        {row.summarized ? <span>{t("studio.memory.summarisedTag")}</span> : null}
                        {row.deletedAt ? <Tag color="red">{t("studio.memory.deletedTag")}</Tag> : null}
                    </div>
                </div>
            ),
        },
        {
            title: t("studio.memory.importanceLabel"),
            dataIndex: "importance",
            key: "importance",
            width: 100,
            render: (value: number) => value.toFixed(2),
        },
        {
            title: t("studio.memory.actions"),
            key: "actions",
            width: 220,
            render: (_value, row) => (
                <Space size="small">
                    <Tooltip title={row.locked ? t("studio.memory.unpin") : t("studio.memory.pin")}>
                        <Button
                            size="small"
                            type="text"
                            loading={busy === row.id}
                            data-testid={`memory-pin-${row.id}`}
                            disabled={!commandsAvailable || Boolean(row.deletedAt)}
                            onClick={() => void togglePin(row)}
                        >
                            {row.locked ? <Lock className="size-4" /> : <LockOpen className="size-4" />}
                        </Button>
                    </Tooltip>
                    <Button
                        size="small"
                        type="text"
                        data-testid={`memory-edit-${row.id}`}
                        disabled={!commandsAvailable || Boolean(row.deletedAt)}
                        onClick={() => {
                            setEditing(row);
                            setEditText(row.content);
                        }}
                    >
                        {t("studio.memory.edit")}
                    </Button>
                    <Button
                        size="small"
                        type="text"
                        danger
                        data-testid={`memory-delete-${row.id}`}
                        disabled={!commandsAvailable || Boolean(row.deletedAt)}
                        onClick={() => {
                            setDeleting(row);
                            setDeleteSummaries(false);
                        }}
                    >
                        <Trash2 className="size-4" />
                    </Button>
                </Space>
            ),
        },
    ];

    if (!bindingsAvailable) {
        return (
            <Alert
                type="info"
                showIcon
                data-testid="studio-memory-no-core"
                message={t("studio.memory.noCoreTitle")}
                description={t("studio.memory.noCoreBody")}
            />
        );
    }

    return (
        <div className="space-y-6" data-testid="studio-memory">
            <section>
                <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
                    <h2 className="text-lg font-medium">{t("studio.memory.title")}</h2>
                    <Space wrap>
                        <Select
                            mode="multiple"
                            allowClear
                            placeholder={t("studio.memory.typeFilter")}
                            value={typeFilter}
                            style={{ minWidth: 200 }}
                            data-testid="memory-type-filter"
                            onChange={(value: string[]) => setTypeFilter(value)}
                            options={["episodic", "semantic", "procedural", "artifact", "summary"].map((value) => ({
                                value,
                                label: t(`studio.memory.type.${value}`, { defaultValue: value }),
                            }))}
                        />
                        <Button
                            size="small"
                            data-testid="memory-include-deleted"
                            type={includeDeleted ? "primary" : "default"}
                            onClick={() => setIncludeDeleted((current) => !current)}
                        >
                            {t("studio.memory.includeDeleted")}
                        </Button>
                        <Button size="small" loading={busy === "summarize"} data-testid="memory-summarize" onClick={() => void condense()}>
                            {t("studio.memory.summarise")}
                        </Button>
                        <Button
                            size="small"
                            loading={busy === "rebuild"}
                            disabled={!isMemoryRebuildAvailable()}
                            data-testid="memory-rebuild"
                            onClick={() => void rebuild()}
                        >
                            {t("studio.memory.rebuild")}
                        </Button>
                    </Space>
                </div>
                {notice ? <Alert className="mb-3" type="success" showIcon message={notice} data-testid="memory-notice" /> : null}
                {loadError ? (
                    <Alert className="mb-3" type="error" showIcon message={loadError} data-testid="memory-load-error" />
                ) : null}
                {memories === null ? (
                    <Empty description={t("studio.memory.loading")} />
                ) : memories.length === 0 ? (
                    <Empty description={t("studio.memory.empty")} />
                ) : (
                    <Table<desktop.MemoryDTO>
                        rowKey="id"
                        size="small"
                        pagination={{ pageSize: 20 }}
                        columns={columns}
                        dataSource={memories}
                        data-testid="studio-memory-table"
                        onRow={(row) => ({ onClick: () => void openDetail(row) })}
                    />
                )}
            </section>

            <section>
                <h2 className="mb-3 text-lg font-medium">{t("studio.memory.previewTitle")}</h2>
                <Typography.Paragraph className="text-xs text-stone-500">
                    {t("studio.memory.previewHint")}
                </Typography.Paragraph>
                <Space.Compact className="mb-3 w-full max-w-2xl">
                    <Input
                        value={previewQuery}
                        data-testid="memory-preview-query"
                        placeholder={t("studio.memory.previewPlaceholder")}
                        onChange={(event) => setPreviewQuery(event.target.value)}
                        onPressEnter={() => void runPreview()}
                    />
                    <Button type="primary" data-testid="memory-preview-run" onClick={() => void runPreview()}>
                        {t("studio.memory.previewRun")}
                    </Button>
                </Space.Compact>
                {previewError ? <Alert type="error" showIcon message={previewError} data-testid="memory-preview-error" /> : null}
                {preview ? (
                    <div className="space-y-3" data-testid="memory-preview">
                        <div className="text-xs text-stone-500">
                            {t("studio.memory.previewBudget", {
                                used: preview.usedTokens,
                                truncated: preview.truncated ? t("studio.memory.yes") : t("studio.memory.no"),
                            })}
                            {preview.semanticSearched ? null : ` · ${t("studio.memory.previewNoSemantic")}`}
                        </div>
                        {(["facts", "summaries", "semantic"] as const).map((channel) =>
                            preview[channel].length === 0 ? null : (
                                <div key={channel} data-memory-channel={channel}>
                                    <h3 className="mb-2 text-sm font-medium">
                                        {t(`studio.memory.channel.${channel}`)}
                                    </h3>
                                    <ul className="space-y-1">
                                        {preview[channel].map((candidate) => (
                                            <li key={candidate.memoryId} className="text-sm" data-memory-candidate={candidate.memoryId}>
                                                <span className="mr-2 font-mono text-xs text-stone-500">
                                                    {candidate.score.toFixed(3)} / {candidate.similarity.toFixed(3)}
                                                </span>
                                                {candidate.pinned ? <Tag color="gold">{t("studio.memory.pinnedTag")}</Tag> : null}
                                                <span className="truncate">{candidate.content}</span>
                                            </li>
                                        ))}
                                    </ul>
                                </div>
                            ),
                        )}
                        {preview.recent.length > 0 ? (
                            <div data-memory-channel="recent">
                                <h3 className="mb-2 text-sm font-medium">{t("studio.memory.channel.recent")}</h3>
                                <ul className="space-y-1">
                                    {preview.recent.map((item) => (
                                        <li key={item.messageId} className="text-sm">
                                            <span className="mr-2 text-xs text-stone-500">{item.role}</span>
                                            <span className="truncate">{item.content}</span>
                                        </li>
                                    ))}
                                </ul>
                            </div>
                        ) : null}
                    </div>
                ) : null}
            </section>

            <Drawer
                open={selected !== null}
                width={560}
                title={t("studio.memory.detailTitle")}
                data-testid="memory-detail"
                onClose={() => setSelected(null)}
            >
                {selected ? (
                    <div className="space-y-4 text-sm">
                        <div>
                            <div className="text-xs text-stone-500">{t("studio.memory.contentLabel")}</div>
                            <div className="whitespace-pre-wrap">{selected.content}</div>
                        </div>
                        <div className="grid grid-cols-2 gap-2">
                            <Detail label={t("studio.memory.typeLabel")} value={selected.type} />
                            <Detail label={t("studio.memory.roleLabel")} value={selected.role || "—"} />
                            <Detail label={t("studio.memory.importanceLabel")} value={selected.importance.toFixed(2)} />
                            <Detail label="confidence" value={selected.confidence.toFixed(2)} />
                            <Detail label="scope" value={selected.scopeProject} />
                            <Detail label="agent" value={selected.agentKey || "—"} />
                            <Detail
                                label="embedding"
                                value={selected.embedded ? `${selected.embeddingModel} / ${selected.embeddingVersion}` : t("studio.memory.notEmbedded")}
                            />
                            <Detail label="created" value={selected.createdAt} />
                        </div>
                        {/* AC-MEM-004's "UI 可跳原始消息": the transcript row an episodic memory came
                            from, shown as the identifier a reader can follow. */}
                        {selected.sourceId ? (
                            <div>
                                <div className="text-xs text-stone-500">{t("studio.memory.sourceLabel")}</div>
                                <code className="text-xs" data-memory-source={selected.sourceId}>
                                    {selected.sourceType}:{selected.sourceId}
                                </code>
                            </div>
                        ) : null}
                        {links.length > 0 ? (
                            <div>
                                <div className="text-xs text-stone-500">{t("studio.memory.linksLabel")}</div>
                                <ul className="mt-1 space-y-1">
                                    {links.map((link) => (
                                        <li key={`${link.entityType}:${link.entityId}:${link.relationType}`} className="text-xs">
                                            {link.relationType} · {link.entityType} <code>{link.entityId}</code>
                                        </li>
                                    ))}
                                </ul>
                            </div>
                        ) : null}
                        {selected.type === "summary" ? (
                            <div>
                                <div className="text-xs text-stone-500">{t("studio.memory.sourcesLabel")}</div>
                                {sources === null ? (
                                    <Empty description={t("studio.memory.loading")} />
                                ) : sources.length === 0 ? (
                                    <div className="text-xs text-stone-500">{t("studio.memory.noSources")}</div>
                                ) : (
                                    <ul className="mt-1 space-y-1" data-testid="memory-summary-sources">
                                        {sources.map((source) => (
                                            <li key={source.memoryId} className="text-xs" data-memory-source-row={source.memoryId}>
                                                <span className="mr-2 text-stone-500">{source.order}.</span>
                                                <span className="mr-2">{source.role || "—"}</span>
                                                {source.agentKey ? <span className="mr-2 text-stone-500">{source.agentKey}</span> : null}
                                                {source.missing ? (
                                                    <Tag color="red">{t("studio.memory.sourceMissing")}</Tag>
                                                ) : null}
                                                {/* AC-MEM-004's "UI 可跳原始消息": the row is a
                                                    BUTTON that opens the source memory, so a reader
                                                    follows it rather than reading an identifier and
                                                    looking it up. A source whose row is gone cannot be
                                                    opened, and says so instead of offering a control
                                                    that would fail. */}
                                                {source.missing ? (
                                                    <span className="text-stone-500">{t("studio.memory.sourceGone")}</span>
                                                ) : (
                                                    <Button
                                                        size="small"
                                                        type="link"
                                                        data-memory-source-row-jump={source.memoryId}
                                                        onClick={() => void openDetailById(source.memoryId)}
                                                    >
                                                        {t("studio.memory.openSource")}
                                                        {source.messageId ? <code className="ml-2">{source.messageId}</code> : null}
                                                    </Button>
                                                )}
                                            </li>
                                        ))}
                                    </ul>
                                )}
                            </div>
                        ) : null}
                    </div>
                ) : null}
            </Drawer>

            <Modal
                open={editing !== null}
                title={t("studio.memory.editTitle")}
                confirmLoading={busy === editing?.id}
                data-testid="memory-edit-modal"
                onCancel={() => setEditing(null)}
                onOk={() => void applyEdit()}
            >
                {/* The consequence is stated rather than discovered: an edit clears the vector, so
                    the memory stops being searchable until a rebuild. */}
                <Typography.Paragraph className="text-xs text-stone-500">
                    {t("studio.memory.editHint")}
                </Typography.Paragraph>
                <Input.TextArea
                    rows={5}
                    value={editText}
                    data-testid="memory-edit-content"
                    onChange={(event) => setEditText(event.target.value)}
                />
            </Modal>

            <Modal
                open={deleting !== null}
                title={t("studio.memory.deleteTitle")}
                confirmLoading={busy === deleting?.id}
                data-testid="memory-delete-modal"
                onCancel={() => setDeleting(null)}
                onOk={() => void applyDelete()}
                okButtonProps={{ danger: true }}
            >
                <Typography.Paragraph className="text-xs text-stone-500">
                    {t("studio.memory.deleteHint")}
                </Typography.Paragraph>
                <Space>
                    <input
                        type="checkbox"
                        checked={deleteSummaries}
                        data-testid="memory-delete-invalidate"
                        onChange={(event) => setDeleteSummaries(event.target.checked)}
                    />
                    <span className="text-sm">{t("studio.memory.deleteSummaries")}</span>
                </Space>
            </Modal>
        </div>
    );
}

function Detail({ label, value }: { label: string; value: string }) {
    return (
        <div>
            <div className="text-xs text-stone-500">{label}</div>
            <div className="truncate">{value}</div>
        </div>
    );
}

/** Sparkles is re-exported so the section's icon can be imported from one place. */
export const MemoryCenterIcon = Sparkles;
