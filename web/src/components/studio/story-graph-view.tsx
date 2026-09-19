import { useCallback, useEffect, useMemo, useState } from "react";
import { Alert, App, Button, Empty, Input, Select, Space, Table, Tag } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Check, GitBranch, RefreshCw, X } from "lucide-react";
import { useTranslation } from "react-i18next";

import {
    acceptStoryEntity,
    createStoryEntity,
    isDramaBindingsAvailable,
    listStoryConflicts,
    listStoryEntities,
    listStoryEventParticipants,
    listStoryEvents,
    listStoryFactSources,
    listStoryRelations,
    lockStoryEntity,
    lockStoryEvent,
    rejectStoryEntity,
    resolveStoryConflict,
    unlockStoryEntity,
    unlockStoryEvent,
} from "@/services/desktop/drama";
import { ENTITY_TYPES } from "@/services/desktop/drama";
import type { desktop } from "@/wailsjs/go/models";

/**
 * The story graph: entities, events, relations, and where each fact came from.
 *
 * WP-05 shipped this section with a stated gap — the binding had the fact layer's
 * commands and no query that lists it, so the section showed only what the create
 * command returned in the current session and said why. The queries now exist, so
 * the gap notice is gone and the lists are real.
 *
 * What this component will NOT do is pretend. Three properties it keeps:
 *
 * 1. An empty list is the core's answer, not a placeholder. Each list is read
 *    from the binding and rendered as it comes; the empty state names what would
 *    be there rather than showing a fabricated row.
 * 2. The review queue is separate from the graph. A candidate is a proposal and
 *    an accepted fact is a decision, so they are two filters over the same list
 *    rather than one undifferentiated table.
 * 3. Evidence is shown as a reference, never as copied text. DOMAIN_MODEL
 *    section 6.6 keeps the passage in the document and the row names where to
 *    look, so the panel shows the version, the chapter and the offsets.
 *
 * The visualization is deliberately minimal: entities as nodes, relations as
 * edges, drawn with the primitives the canvas already uses. It is a reading aid
 * rather than the canvas itself — WP-04 owns the canvas projection, and
 * duplicating it here would give the same data two renderers.
 */

export type StoryGraphSectionProps = { projectId: string };

/** STATUS_FILTERS are the review states the list can be narrowed to. */
const STATUS_FILTERS = ["", "candidate", "accepted", "rejected", "locked"] as const;

export function StoryGraphSection({ projectId }: StoryGraphSectionProps) {
    const { t } = useTranslation();
    const { message } = App.useApp();
    const [entities, setEntities] = useState<desktop.StoryEntityDTO[]>([]);
    const [events, setEvents] = useState<desktop.StoryEventDTO[]>([]);
    const [relations, setRelations] = useState<desktop.StoryRelationDTO[]>([]);
    const [conflicts, setConflicts] = useState<desktop.StoryFactConflictDTO[]>([]);
    const [resolving, setResolving] = useState<{ id: string; text: string } | null>(null);
    const [status, setStatus] = useState<string>("");
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [evidence, setEvidence] = useState<{ factId: string; rows: desktop.StoryFactSourceDTO[] } | null>(null);
    const [participants, setParticipants] = useState<{ eventId: string; rows: desktop.StoryEventParticipantDTO[] } | null>(null);
    const [busy, setBusy] = useState<string | null>(null);

    const [type, setType] = useState<string>("character");
    const [name, setName] = useState("");
    const [creating, setCreating] = useState(false);

    const available = isDramaBindingsAvailable();

    const load = useCallback(async () => {
        if (!projectId) return;
        setLoading(true);
        setError(null);
        try {
            // Three reads rather than one composite call, because the core exposes
            // three lists and a combined query would be a fourth thing to keep in
            // step with them.
            const [entityRows, eventRows, relationRows, conflictRows] = await Promise.all([
                listStoryEntities(projectId, status),
                listStoryEvents(projectId, "", status),
                listStoryRelations(projectId, status),
                // Conflicts are read with NO status filter whatever the list
                // filter says. The queue is what needs a decision, but hiding a
                // resolved one behind "candidates only" would make the record
                // unfindable — which is the gap this panel closes.
                listStoryConflicts(projectId, ""),
            ]);
            setEntities(entityRows);
            setEvents(eventRows);
            setRelations(relationRows);
            setConflicts(conflictRows);
        } catch (caught) {
            setError(caught instanceof Error ? caught.message : t("studio.shell.loadFailed"));
        } finally {
            setLoading(false);
        }
    }, [projectId, status, t]);

    useEffect(() => {
        void load();
    }, [load]);

    const create = async () => {
        if (name.trim() === "") {
            message.error(t("studio.storyGraph.nameRequired"));
            return;
        }
        setCreating(true);
        try {
            await createStoryEntity({ projectId, type, canonicalName: name.trim() });
            setName("");
            message.success(t("studio.storyGraph.created"));
            await load();
        } catch (caught) {
            message.error(caught instanceof Error ? caught.message : t("studio.storyGraph.createFailed"));
        } finally {
            setCreating(false);
        }
    };

    const decide = async (entity: desktop.StoryEntityDTO, accept: boolean) => {
        setBusy(entity.id);
        try {
            const request = { id: entity.id, revision: entity.revision } as desktop.DecideStoryEntityRequest;
            if (accept) {
                await acceptStoryEntity(request);
            } else {
                await rejectStoryEntity(request);
            }
            await load();
        } catch (caught) {
            // A refusal here is usually a conflict: the row moved in another
            // window. The reload shows the state that actually exists.
            message.error(caught instanceof Error ? caught.message : t("studio.storyGraph.decideFailed"));
            await load();
        } finally {
            setBusy(null);
        }
    };

    /**
     * toggleLock pins or releases a fact.
     *
     * AC-STORY-002 lists 接受/拒绝/锁定 as the three decisions a candidate may
     * receive, and this is the third. The lock protects a decision rather than
     * making one: releasing it returns the fact to 'accepted', which is the state
     * it was in before it was pinned.
     */
    const toggleLock = async (id: string, revision: number, locked: boolean) => {
        setBusy(id);
        try {
            const request = { id, revision } as desktop.LockStoryEntityRequest;
            if (locked) {
                await unlockStoryEntity(request);
            } else {
                await lockStoryEntity(request);
            }
            await load();
        } catch (caught) {
            // A refusal is usually a conflict: the row moved. The reload shows
            // the state that actually exists.
            message.error(caught instanceof Error ? caught.message : t("studio.storyGraph.lockFailed"));
            await load();
        } finally {
            setBusy(null);
        }
    };

    /** toggleEventLock is the same decision for an event, which is a separate command. */
    const toggleEventLock = async (id: string, revision: number, locked: boolean) => {
        setBusy(id);
        try {
            const request = { id, revision } as desktop.LockStoryEntityRequest;
            if (locked) {
                await unlockStoryEvent(request);
            } else {
                await lockStoryEvent(request);
            }
            await load();
        } catch (caught) {
            message.error(caught instanceof Error ? caught.message : t("studio.storyGraph.lockFailed"));
            await load();
        } finally {
            setBusy(null);
        }
    };

    const showEvidence = async (factType: string, factId: string) => {
        try {
            setEvidence({ factId, rows: await listStoryFactSources(factType, factId) });
        } catch (caught) {
            message.error(caught instanceof Error ? caught.message : t("studio.storyGraph.evidenceFailed"));
        }
    };

    const showParticipants = async (eventId: string) => {
        try {
            setParticipants({ eventId, rows: await listStoryEventParticipants(eventId) });
        } catch (caught) {
            message.error(caught instanceof Error ? caught.message : t("studio.storyGraph.participantsFailed"));
        }
    };

    const entityColumns: ColumnsType<desktop.StoryEntityDTO> = [
        {
            title: t("studio.storyGraph.typeLabel"),
            dataIndex: "type",
            key: "type",
            width: 130,
            render: (value: string) => <Tag>{t(`studio.entityType.${value}`, { defaultValue: value })}</Tag>,
        },
        { title: t("studio.storyGraph.nameLabel"), dataIndex: "canonicalName", key: "canonicalName", ellipsis: true },
        {
            title: t("studio.source.statusLabel"),
            dataIndex: "status",
            key: "status",
            width: 110,
            render: (value: string) => <Tag>{t(`studio.status.${value}`, { defaultValue: value })}</Tag>,
        },
        {
            title: t("studio.storyGraph.actions"),
            key: "actions",
            width: 230,
            render: (_, record) => (
                <Space size="small">
                    <Button
                        size="small"
                        icon={<Check className="size-3.5" />}
                        loading={busy === record.id}
                        disabled={record.status !== "candidate"}
                        data-testid={`studio-entity-accept-${record.id}`}
                        onClick={() => void decide(record, true)}
                    >
                        {t("studio.storyGraph.accept")}
                    </Button>
                    <Button
                        size="small"
                        icon={<X className="size-3.5" />}
                        loading={busy === record.id}
                        disabled={record.status !== "candidate"}
                        data-testid={`studio-entity-reject-${record.id}`}
                        onClick={() => void decide(record, false)}
                    >
                        {t("studio.storyGraph.reject")}
                    </Button>
                    <Button
                        size="small"
                        loading={busy === record.id}
                        data-testid={`studio-entity-lock-${record.id}`}
                        onClick={() => void toggleLock(record.id, record.revision, record.status === "locked")}
                    >
                        {record.status === "locked" ? t("studio.storyGraph.unlock") : t("studio.storyGraph.lock")}
                    </Button>
                    <Button size="small" type="text" onClick={() => void showEvidence("entity", record.id)}>
                        {t("studio.storyGraph.evidence")}
                    </Button>
                </Space>
            ),
        },
    ];

    const eventColumns: ColumnsType<desktop.StoryEventDTO> = [
        { title: t("studio.chapters.ordinal"), dataIndex: "ordinal", key: "ordinal", width: 64 },
        { title: t("studio.storyGraph.eventName"), dataIndex: "name", key: "name", ellipsis: true },
        {
            title: t("studio.storyGraph.storyTime"),
            dataIndex: "storyTimeText",
            key: "storyTimeText",
            width: 150,
            render: (value: string | undefined) => value || "—",
        },
        {
            title: t("studio.source.statusLabel"),
            dataIndex: "status",
            key: "status",
            width: 110,
            render: (value: string) => <Tag>{t(`studio.status.${value}`, { defaultValue: value })}</Tag>,
        },
        {
            title: t("studio.storyGraph.actions"),
            key: "actions",
            width: 150,
            render: (_, record) => (
                <Space size="small">
                    <Button
                        size="small"
                        loading={busy === record.id}
                        data-testid={`studio-event-lock-${record.id}`}
                        onClick={() => void toggleEventLock(record.id, record.revision, record.status === "locked")}
                    >
                        {record.status === "locked" ? t("studio.storyGraph.unlock") : t("studio.storyGraph.lock")}
                    </Button>
                    <Button size="small" type="text" onClick={() => void showParticipants(record.id)}>
                        {t("studio.storyGraph.participants")}
                    </Button>
                    <Button size="small" type="text" onClick={() => void showEvidence("event", record.id)}>
                        {t("studio.storyGraph.evidence")}
                    </Button>
                </Space>
            ),
        },
    ];

    const relationColumns: ColumnsType<desktop.StoryRelationDTO> = [
        {
            title: t("studio.storyGraph.relationType"),
            dataIndex: "type",
            key: "type",
            width: 150,
            render: (value: string) => <Tag>{t(`studio.relationType.${value}`, { defaultValue: value })}</Tag>,
        },
        {
            title: t("studio.storyGraph.relationEnds"),
            key: "ends",
            render: (_, record) => (
                <span className="truncate text-sm">
                    {shortId(record.sourceEntityId)} → {shortId(record.targetEntityId)}
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
            title: t("studio.storyGraph.actions"),
            key: "actions",
            width: 110,
            render: (_, record) => (
                <Button size="small" type="text" onClick={() => void showEvidence("relation", record.id)}>
                    {t("studio.storyGraph.evidence")}
                </Button>
            ),
        },
    ];

    const submitResolution = async (conflictId: string) => {
        const text = resolving?.text.trim() ?? "";
        if (text === "") {
            // The domain refuses a blank resolution too — section 6.7 keeps the
            // resolution as the decision that closed the conflict, so an empty one
            // would close it having recorded nothing.
            message.error(t("studio.storyGraph.resolutionRequired"));
            return;
        }
        try {
            await resolveStoryConflict({
                conflictId,
                resolution: text,
                // The resolver is a person at this surface. A rule that closed the
                // conflict would name the rule instead.
                resolvedBy: "user",
            } as desktop.ResolveStoryConflictRequest);
            setResolving(null);
            message.success(t("studio.storyGraph.resolved"));
            await load();
        } catch (caught) {
            // A refusal here is usually a second resolution: the row moved, so
            // the reload below shows the state that actually exists.
            message.error(caught instanceof Error ? caught.message : t("studio.storyGraph.resolveFailed"));
            await load();
        }
    };

    const graph = useMemo(() => buildGraph(entities, relations), [entities, relations]);

    if (!available) {
        return (
            <Alert
                type="info"
                showIcon
                message={t("studio.storyGraph.desktopOnlyTitle")}
                description={t("studio.storyGraph.desktopOnlyBody")}
                data-testid="studio-story-graph-desktop-only"
            />
        );
    }

    return (
        <div className="space-y-6">
            <div className="flex flex-wrap items-end gap-3 rounded-xl border border-stone-200 p-4 dark:border-stone-800">
                <label className="min-w-52 flex-1">
                    <span className="mb-1 block text-sm">{t("studio.storyGraph.nameLabel")}</span>
                    <Input
                        value={name}
                        maxLength={200}
                        data-testid="studio-entity-name"
                        onChange={(event) => setName(event.target.value)}
                        onPressEnter={() => void create()}
                    />
                </label>
                <label>
                    <span className="mb-1 block text-sm">{t("studio.storyGraph.typeLabel")}</span>
                    <Select
                        className="w-44"
                        value={type}
                        data-testid="studio-entity-type"
                        onChange={(value: string) => setType(value)}
                        options={ENTITY_TYPES.map((value) => ({ value, label: t(`studio.entityType.${value}`) }))}
                    />
                </label>
                <Button type="primary" loading={creating} data-testid="studio-entity-create" onClick={() => void create()}>
                    {t("studio.storyGraph.create")}
                </Button>
            </div>

            <div className="flex flex-wrap items-center gap-3">
                <label className="flex items-center gap-2 text-sm">
                    {t("studio.storyGraph.statusFilter")}
                    <Select
                        className="w-40"
                        value={status}
                        data-testid="studio-story-status-filter"
                        onChange={(value: string) => setStatus(value)}
                        options={STATUS_FILTERS.map((value) => ({
                            value,
                            label: value === "" ? t("studio.storyGraph.statusAny") : t(`studio.status.${value}`),
                        }))}
                    />
                </label>
                <Button icon={<RefreshCw className="size-4" />} loading={loading} data-testid="studio-story-reload" onClick={() => void load()}>
                    {t("studio.storyGraph.reload")}
                </Button>
                <span className="text-xs text-stone-500" data-testid="studio-story-counts">
                    {t("studio.storyGraph.counts", { entities: entities.length, events: events.length, relations: relations.length })}
                </span>
            </div>

            {error ? <Alert type="error" showIcon message={error} data-testid="studio-story-error" /> : null}

            <section className="space-y-2">
                <h3 className="text-sm font-medium">{t("studio.storyGraph.entitiesTitle")}</h3>
                {entities.length === 0 && !loading ? (
                    <Empty description={t("studio.storyGraph.entitiesEmpty")} />
                ) : (
                    <Table<desktop.StoryEntityDTO>
                        rowKey="id"
                        size="small"
                        loading={loading}
                        pagination={false}
                        columns={entityColumns}
                        dataSource={entities}
                        data-testid="studio-entities-table"
                    />
                )}
            </section>

            <section className="space-y-2">
                <h3 className="text-sm font-medium">{t("studio.storyGraph.eventsTitle")}</h3>
                {events.length === 0 && !loading ? (
                    <Empty description={t("studio.storyGraph.eventsEmpty")} />
                ) : (
                    <Table<desktop.StoryEventDTO>
                        rowKey="id"
                        size="small"
                        loading={loading}
                        pagination={false}
                        columns={eventColumns}
                        dataSource={events}
                        data-testid="studio-events-table"
                    />
                )}
            </section>

            <section className="space-y-2">
                <h3 className="text-sm font-medium">{t("studio.storyGraph.relationsTitle")}</h3>
                {relations.length === 0 && !loading ? (
                    <Empty description={t("studio.storyGraph.relationsEmpty")} />
                ) : (
                    <Table<desktop.StoryRelationDTO>
                        rowKey="id"
                        size="small"
                        loading={loading}
                        pagination={false}
                        columns={relationColumns}
                        dataSource={relations}
                        data-testid="studio-relations-table"
                    />
                )}
            </section>

            <section className="space-y-2">
                <h3 className="text-sm font-medium">{t("studio.storyGraph.conflictsTitle")}</h3>
                {conflicts.length === 0 ? (
                    <Empty description={t("studio.storyGraph.conflictsEmpty")} />
                ) : (
                    <ul className="space-y-2" data-testid="studio-conflicts-list">
                        {conflicts.map((conflict) => (
                            <li key={conflict.id} className="rounded-lg border border-stone-200 p-3 text-sm dark:border-stone-800">
                                <div className="flex flex-wrap items-center gap-2">
                                    <Tag color={conflict.status === "open" ? "orange" : "default"}>
                                        {t(`studio.conflictStatus.${conflict.status}`, { defaultValue: conflict.status })}
                                    </Tag>
                                    <span className="text-xs text-stone-500">
                                        {t(`studio.factType.${conflict.leftFactType}`, { defaultValue: conflict.leftFactType })}
                                    </span>
                                    <code className="text-xs">{shortId(conflict.leftFactId)}</code>
                                    <span className="text-stone-400">↔</span>
                                    <span className="text-xs text-stone-500">
                                        {t(`studio.factType.${conflict.rightFactType}`, { defaultValue: conflict.rightFactType })}
                                    </span>
                                    <code className="text-xs">{shortId(conflict.rightFactId)}</code>
                                    {conflict.conflictType ? <Tag>{conflict.conflictType}</Tag> : null}
                                </div>
                                {conflict.status === "open" ? (
                                    resolving?.id === conflict.id ? (
                                        <div className="mt-2 flex flex-wrap items-end gap-2">
                                            <Input
                                                className="min-w-52 flex-1"
                                                value={resolving.text}
                                                maxLength={2000}
                                                placeholder={t("studio.storyGraph.resolutionPlaceholder")}
                                                data-testid={`studio-conflict-resolution-${conflict.id}`}
                                                onChange={(event) => setResolving({ id: conflict.id, text: event.target.value })}
                                            />
                                            <Button
                                                size="small"
                                                type="primary"
                                                data-testid={`studio-conflict-resolve-submit-${conflict.id}`}
                                                onClick={() => void submitResolution(conflict.id)}
                                            >
                                                {t("studio.storyGraph.resolve")}
                                            </Button>
                                            <Button size="small" type="text" onClick={() => setResolving(null)}>
                                                {t("common.cancel")}
                                            </Button>
                                        </div>
                                    ) : (
                                        <Button
                                            className="mt-2"
                                            size="small"
                                            data-testid={`studio-conflict-resolve-${conflict.id}`}
                                            onClick={() => setResolving({ id: conflict.id, text: "" })}
                                        >
                                            {t("studio.storyGraph.resolve")}
                                        </Button>
                                    )
                                ) : (
                                    <p className="mt-2 text-xs text-stone-500">
                                        {t("studio.storyGraph.resolvedBy", { by: conflict.resolvedBy || "—" })}: {conflict.resolution}
                                    </p>
                                )}
                            </li>
                        ))}
                    </ul>
                )}
            </section>

            {graph.nodes.length > 0 ? (
                <section className="space-y-2">
                    <h3 className="flex items-center gap-2 text-sm font-medium">
                        <GitBranch className="size-4" />
                        {t("studio.storyGraph.graphTitle")}
                    </h3>
                    <p className="text-xs text-stone-500">{t("studio.storyGraph.graphNote")}</p>
                    <StoryGraphView graph={graph} />
                </section>
            ) : null}

            {participants ? (
                <section className="rounded-xl border border-stone-200 p-4 dark:border-stone-800" data-testid="studio-participants-panel">
                    <div className="mb-2 flex items-center justify-between">
                        <h4 className="text-sm font-medium">{t("studio.storyGraph.participants")}</h4>
                        <Button size="small" type="text" onClick={() => setParticipants(null)}>
                            {t("common.cancel")}
                        </Button>
                    </div>
                    {participants.rows.length === 0 ? (
                        <p className="text-sm text-stone-500">{t("studio.storyGraph.participantsEmpty")}</p>
                    ) : (
                        <ul className="space-y-1 text-sm">
                            {participants.rows.map((row) => (
                                <li key={`${row.storyEntityId}-${row.role}`} className="flex items-center gap-2">
                                    <Tag>{t(`studio.participantRole.${row.role}`, { defaultValue: row.role })}</Tag>
                                    <span>{shortId(row.storyEntityId)}</span>
                                    {row.stateBefore || row.stateAfter ? (
                                        <span className="text-xs text-stone-500">
                                            {row.stateBefore || "?"} → {row.stateAfter || "?"}
                                        </span>
                                    ) : null}
                                </li>
                            ))}
                        </ul>
                    )}
                </section>
            ) : null}

            {evidence ? (
                <section className="rounded-xl border border-stone-200 p-4 dark:border-stone-800" data-testid="studio-evidence-panel">
                    <div className="mb-2 flex items-center justify-between">
                        <h4 className="text-sm font-medium">{t("studio.storyGraph.evidence")}</h4>
                        <Button size="small" type="text" onClick={() => setEvidence(null)}>
                            {t("common.cancel")}
                        </Button>
                    </div>
                    {evidence.rows.length === 0 ? (
                        <p className="text-sm text-stone-500">{t("studio.storyGraph.evidenceEmpty")}</p>
                    ) : (
                        <ul className="space-y-1 text-sm" data-testid="studio-evidence-list">
                            {evidence.rows.map((row) => (
                                <li key={row.id} className="flex flex-wrap items-center gap-2">
                                    <Tag>{t(`studio.evidenceKind.${row.sourceKind}`, { defaultValue: row.sourceKind })}</Tag>
                                    <span className="text-xs text-stone-500">{t("studio.storyGraph.evidenceVersion")}</span>
                                    <code className="text-xs">{shortId(row.sourceDocumentVersionId)}</code>
                                    {row.startOffset !== undefined && row.endOffset !== undefined ? (
                                        // The offsets are shown as a reference rather than as
                                        // text: section 6.6 keeps the passage in the document.
                                        <span className="text-xs text-stone-500">
                                            {t("studio.storyGraph.evidenceRange", { from: row.startOffset, to: row.endOffset })}
                                        </span>
                                    ) : (
                                        <span className="text-xs text-stone-400">{t("studio.storyGraph.evidenceNoRange")}</span>
                                    )}
                                </li>
                            ))}
                        </ul>
                    )}
                </section>
            ) : null}
        </div>
    );
}

/** shortId trims an identifier for display without pretending it is a name. */
function shortId(value: string): string {
    return value.length <= 12 ? value : `${value.slice(0, 8)}…`;
}

type GraphNode = { id: string; label: string; type: string; status: string };
type GraphEdge = { id: string; from: string; to: string; label: string };
type Graph = { nodes: GraphNode[]; edges: GraphEdge[] };

/**
 * buildGraph turns the two lists into a layout the SVG below can draw.
 *
 * A relation whose endpoint is not in the entity list is DROPPED rather than
 * drawn to nowhere: the two lists are read separately, so a relation can name an
 * entity the filter excluded, and an edge to a missing node would be a line the
 * user cannot follow. The count line above says how many relations exist, so a
 * dropped edge is not hidden by silence.
 */
function buildGraph(entities: desktop.StoryEntityDTO[], relations: desktop.StoryRelationDTO[]): Graph {
    const nodes: GraphNode[] = entities.map((entity) => ({
        id: entity.id,
        label: entity.canonicalName,
        type: entity.type,
        status: entity.status,
    }));
    const present = new Set(nodes.map((node) => node.id));
    const edges: GraphEdge[] = [];
    for (const relation of relations) {
        if (!present.has(relation.sourceEntityId) || !present.has(relation.targetEntityId)) continue;
        edges.push({
            id: relation.id,
            from: relation.sourceEntityId,
            to: relation.targetEntityId,
            label: relation.type,
        });
    }
    return { nodes, edges };
}

/**
 * StoryGraphView draws the graph with plain SVG.
 *
 * It is a reading aid, not the canvas: a fixed circular layout, no dragging and no
 * zoom, because the canvas has those and two draggable surfaces over the same data
 * would be two things to keep in step. The layout is deterministic, so the same
 * graph renders the same way twice.
 */
function StoryGraphView({ graph }: { graph: Graph }) {
    const { t } = useTranslation();
    const width = 640;
    const height = 360;
    const radius = Math.min(width, height) / 2 - 60;
    const centre = { x: width / 2, y: height / 2 };
    const positions = new Map<string, { x: number; y: number }>();
    graph.nodes.forEach((node, index) => {
        const angle = (2 * Math.PI * index) / Math.max(1, graph.nodes.length) - Math.PI / 2;
        positions.set(node.id, {
            x: centre.x + radius * Math.cos(angle),
            y: centre.y + radius * Math.sin(angle),
        });
    });

    return (
        <svg
            viewBox={`0 0 ${width} ${height}`}
            className="w-full max-w-3xl rounded-xl border border-stone-200 bg-white dark:border-stone-800 dark:bg-stone-950"
            role="img"
            aria-label={t("studio.storyGraph.graphLabel")}
            data-testid="studio-story-graph-view"
        >
            {graph.edges.map((edge) => {
                const from = positions.get(edge.from);
                const to = positions.get(edge.to);
                if (!from || !to) return null;
                return (
                    <g key={edge.id}>
                        <line x1={from.x} y1={from.y} x2={to.x} y2={to.y} stroke="currentColor" strokeWidth={1} className="text-stone-300 dark:text-stone-700" />
                        <text x={(from.x + to.x) / 2} y={(from.y + to.y) / 2} fontSize={9} textAnchor="middle" className="fill-stone-500">
                            {edge.label}
                        </text>
                    </g>
                );
            })}
            {graph.nodes.map((node) => {
                const position = positions.get(node.id);
                if (!position) return null;
                return (
                    <g key={node.id}>
                        <circle cx={position.x} cy={position.y} r={8} className="fill-stone-400 dark:fill-stone-500" />
                        <text x={position.x} y={position.y - 14} fontSize={11} textAnchor="middle" className="fill-stone-700 dark:fill-stone-200">
                            {node.label.length > 8 ? `${node.label.slice(0, 8)}…` : node.label}
                        </text>
                    </g>
                );
            })}
        </svg>
    );
}
