import { useCallback, useEffect, useState } from "react";
import { Alert, App, Button, Empty, Input, InputNumber, Modal, Popconfirm, Select, Space, Table, Tag } from "antd";
import type { ColumnsType } from "antd/es/table";
import { FilePlus2, Plus, ShieldAlert, UserPlus } from "lucide-react";
import { useTranslation } from "react-i18next";

import { clearStaleMark, createAsset, createEpisode, createSourceDocument, ensureScript, listScenes, waiveStaleMark } from "@/services/desktop/drama";
import { ChapterPanel, ImportFlow } from "@/components/studio/import-flow";
import type { desktop } from "@/wailsjs/go/models";

/**
 * The studio shell's section bodies.
 *
 * Every component here reads its rows from the Go core and says so when there is
 * nothing to read. None of them invents a row, a count or a placeholder: a
 * section whose binding does not exist states the gap (see StoryGraphSection),
 * and a section whose query returns nothing renders an empty state that names
 * what would be there.
 *
 * Each component owns only the state it alone needs — an inline form's draft, a
 * selected row's detail — while the lists the shell shares across sections live
 * in `useStudioStore`.
 */

// The project reads that drama.ts does not wrap. The import is lazy and cached
// for the same reason drama.ts caches its own: a browser build has no Wails
// runtime, and an eager import would fail before the page could explain itself.
let projectsModule: Promise<typeof import("@/wailsjs/go/desktop/ProjectsBinding")> | undefined;

async function loadProjectsBinding() {
    projectsModule ??= import("@/wailsjs/go/desktop/ProjectsBinding").catch((error: unknown) => {
        projectsModule = undefined;
        throw error;
    });
    return projectsModule;
}

/** loadCanvasSnapshot reads a project's canvas through the Go binding. */
export async function loadCanvasSnapshot(projectId: string): Promise<desktop.CanvasSnapshotDTO> {
    const { LoadCanvas } = await loadProjectsBinding();
    return LoadCanvas(projectId);
}

/** loadProjectRules reads a project's rules, so the overview can count them. */
export async function loadProjectRules(projectId: string): Promise<desktop.ProjectRuleDTO[]> {
    const { ListProjectRules } = await loadProjectsBinding();
    return ListProjectRules({ projectId });
}

/** getProjectSettings reads a project's drama settings. */
export async function getProjectSettings(projectId: string): Promise<desktop.ProjectSettingsDTO> {
    const { GetProjectSettings } = await loadProjectsBinding();
    return GetProjectSettings(projectId);
}

/** getProjectName reads a project's row, for the shell header. */
export async function getProjectName(projectId: string): Promise<desktop.ProjectDTO> {
    const { GetProject } = await loadProjectsBinding();
    return GetProject(projectId);
}

// ---------------------------------------------------------------------------
// Overview
// ---------------------------------------------------------------------------

export type OverviewSectionProps = {
    settings: desktop.ProjectSettingsDTO | null;
    /** Every count below is the length of a list the shell read from the core. */
    episodeCount: number;
    sourceDocumentCount: number;
    /** Null when the rule query failed, so the UI says "unread" and not "zero". */
    ruleCount: number | null;
    openStaleMarkCount: number | null;
};

export function OverviewSection({ settings, episodeCount, sourceDocumentCount, ruleCount, openStaleMarkCount }: OverviewSectionProps) {
    const { t } = useTranslation();
    // A blank settings field is the core saying "not stated", which is not the
    // same as a field this interface failed to read.
    const text = (value: string | undefined) => (value && value.trim() !== "" ? value : "—");
    const count = (value: number | null) => (value === null ? "—" : String(value));

    return (
        <div className="space-y-8">
            <section>
                <h2 className="text-lg font-medium">{t("studio.overview.settings")}</h2>
                {settings ? (
                    <dl className="mt-4 grid gap-4 text-sm sm:grid-cols-2" data-testid="studio-overview-settings">
                        <Setting label={t("studio.overview.platform")} value={text(settings.targetPlatform)} />
                        <Setting label={t("studio.overview.aspectRatio")} value={text(settings.aspectRatio)} />
                        <Setting label={t("studio.overview.resolution")} value={text(settings.resolution)} />
                        <Setting label={t("studio.overview.expectedEpisodes")} value={String(settings.expectedEpisodeCount)} />
                        <Setting label={t("studio.overview.defaultDuration")} value={String(settings.defaultEpisodeDurationSecs)} />
                        <Setting label={t("studio.overview.audience")} value={text(settings.audience)} />
                        <Setting label={t("studio.overview.contentRating")} value={text(settings.contentRating)} />
                        <Setting label={t("studio.overview.adaptationMode")} value={t(`studio.wizard.mode${capitalise(settings.adaptationMode)}`, { defaultValue: settings.adaptationMode })} />
                        <Setting label={t("studio.overview.language")} value={text(settings.language)} />
                        <Setting label={t("studio.overview.timezone")} value={text(settings.timezone)} />
                    </dl>
                ) : (
                    <p className="mt-3 text-sm text-stone-500">{t("studio.overview.noSettings")}</p>
                )}
            </section>

            <section>
                <h2 className="text-lg font-medium">{t("studio.overview.counts")}</h2>
                <dl className="mt-4 grid gap-4 text-sm sm:grid-cols-2 xl:grid-cols-4">
                    <Setting label={t("studio.overview.episodes")} value={String(episodeCount)} />
                    <Setting label={t("studio.overview.sourceDocuments")} value={String(sourceDocumentCount)} />
                    <Setting label={t("studio.overview.rules")} value={count(ruleCount)} />
                    <Setting label={t("studio.overview.openStaleMarks")} value={count(openStaleMarkCount)} />
                </dl>
            </section>
        </div>
    );
}

function Setting({ label, value }: { label: string; value: string }) {
    return (
        <div className="min-w-0">
            <dt className="text-xs text-stone-500">{label}</dt>
            <dd className="mt-1 truncate">{value}</dd>
        </div>
    );
}

/** capitalise turns a stored enum value into the suffix of its i18n key. */
function capitalise(value: string): string {
    if (value === "") return "";
    return value.charAt(0).toUpperCase() + value.slice(1);
}

// ---------------------------------------------------------------------------
// Source documents
// ---------------------------------------------------------------------------

const DOCUMENT_TYPES = ["novel", "story", "screenplay", "outline", "notes"] as const;

export type SourceSectionProps = {
    projectId: string;
    documents: desktop.SourceDocumentDTO[];
    onChanged: () => void;
};

/**
 * SourceSection registers documents, imports their text, and shows the chapters
 * that came out of it.
 *
 * The import flow and the chapter panel live in components/studio/import-flow.tsx
 * because they own a flow of their own — choose, preview, confirm, review — and a
 * section that also held it inline would be the page-sized component AGENTS warns
 * against. What stays here is the list of documents and which one is expanded.
 */
export function SourceSection({ projectId, documents, onChanged }: SourceSectionProps) {
    const { t } = useTranslation();
    const { message } = App.useApp();
    const [name, setName] = useState("");
    const [type, setType] = useState<string>("novel");
    const [busy, setBusy] = useState(false);
    const [selected, setSelected] = useState<{ documentId: string; versionId: string } | null>(null);
    const [reloadToken, setReloadToken] = useState(0);

    const create = async () => {
        if (name.trim() === "") {
            // The core refuses a blank name too; not submitting one turns a form
            // mistake into a round trip.
            message.error(t("studio.source.nameRequired"));
            return;
        }
        setBusy(true);
        try {
            await createSourceDocument({ projectId, type, name: name.trim() });
            message.success(t("studio.source.created"));
            setName("");
            onChanged();
        } catch (error) {
            message.error(error instanceof Error ? error.message : t("studio.source.createFailed"));
        } finally {
            setBusy(false);
        }
    };

    const columns: ColumnsType<desktop.SourceDocumentDTO> = [
        { title: t("studio.source.nameLabel"), dataIndex: "name", key: "name" },
        {
            title: t("studio.source.typeLabel"),
            dataIndex: "type",
            key: "type",
            width: 140,
            render: (value: string) => <Tag>{t(`studio.source.type${capitalise(value)}`, { defaultValue: value })}</Tag>,
        },
        {
            title: t("studio.source.statusLabel"),
            dataIndex: "status",
            key: "status",
            width: 120,
            render: (value: string) => <span className="text-sm">{t(`studio.status.${value}`, { defaultValue: value })}</span>,
        },
        {
            title: t("studio.source.versionLabel"),
            key: "version",
            render: (_, record) =>
                record.currentVersionId ? (
                    <Button
                        size="small"
                        data-testid={`studio-source-chapters-${record.id}`}
                        onClick={() => setSelected({ documentId: record.id, versionId: record.currentVersionId ?? "" })}
                    >
                        {t("studio.source.chaptersLabel")}
                    </Button>
                ) : (
                    // No version means no imported text yet. The import control is
                    // above this table, so the answer is where to go rather than
                    // what is missing.
                    <span className="text-xs text-stone-500">{t("studio.source.noVersion")}</span>
                ),
        },
    ];

    return (
        <div className="space-y-6">
            <div className="flex flex-wrap items-end gap-3 rounded-xl border border-stone-200 p-4 dark:border-stone-800">
                <label className="min-w-52 flex-1">
                    <span className="mb-1 block text-sm">{t("studio.source.nameLabel")}</span>
                    <Input value={name} maxLength={200} data-testid="studio-source-name" onChange={(event) => setName(event.target.value)} onPressEnter={() => void create()} />
                </label>
                <label>
                    <span className="mb-1 block text-sm">{t("studio.source.typeLabel")}</span>
                    <Select
                        className="w-44"
                        value={type}
                        data-testid="studio-source-type"
                        onChange={(value: string) => setType(value)}
                        options={DOCUMENT_TYPES.map((value) => ({ value, label: t(`studio.source.type${capitalise(value)}`) }))}
                    />
                </label>
                <Button type="primary" icon={<FilePlus2 className="size-4" />} loading={busy} data-testid="studio-source-create" onClick={() => void create()}>
                    {t("studio.source.add")}
                </Button>
            </div>

            <ImportFlow
                projectId={projectId}
                onImported={() => {
                    onChanged();
                    setReloadToken((current) => current + 1);
                }}
            />

            {documents.length === 0 ? (
                <Empty description={t("studio.source.empty")} />
            ) : (
                <Table<desktop.SourceDocumentDTO> rowKey="id" size="small" pagination={false} columns={columns} dataSource={documents} data-testid="studio-source-table" />
            )}

            {selected ? (
                <section className="rounded-xl border border-stone-200 p-4 dark:border-stone-800">
                    <div className="mb-2 flex items-center justify-between">
                        <h3 className="text-sm font-medium">{t("studio.source.chaptersLabel")}</h3>
                        <Button size="small" type="text" onClick={() => setSelected(null)}>
                            {t("common.cancel")}
                        </Button>
                    </div>
                    <ChapterPanel versionId={selected.versionId} reloadToken={reloadToken} onChanged={onChanged} />
                </section>
            ) : null}
        </div>
    );
}

// ---------------------------------------------------------------------------
// Story graph
// ---------------------------------------------------------------------------

// The graph section moved to components/studio/story-graph-view.tsx when the
// list queries landed: it now owns an entity review queue, an event list, a
// relation list, an evidence panel and an SVG view, which is four concerns and
// too many for one section body. The shell re-exports it so the section registry
// keeps a single import surface.
export { StoryGraphSection } from "@/components/studio/story-graph-view";
export type { StoryGraphSectionProps } from "@/components/studio/story-graph-view";

// ---------------------------------------------------------------------------
// Script
// ---------------------------------------------------------------------------

export type ScriptSectionProps = {
    projectId: string;
    episodes: desktop.EpisodeDTO[];
    activeEpisodeId: string;
    onSelectEpisode: (episodeId: string) => void;
    onChanged: () => void;
};

export function ScriptSection({ projectId, episodes, activeEpisodeId, onSelectEpisode, onChanged }: ScriptSectionProps) {
    const { t } = useTranslation();
    const { message } = App.useApp();
    const [season, setSeason] = useState(1);
    const [number, setNumber] = useState(1);
    const [title, setTitle] = useState("");
    const [duration, setDuration] = useState<number | null>(null);
    const [busy, setBusy] = useState(false);
    const [script, setScript] = useState<desktop.ScriptDTO | null>(null);
    const [scenes, setScenes] = useState<desktop.SceneDTO[]>([]);

    const create = async () => {
        setBusy(true);
        try {
            await createEpisode({ projectId, seasonNumber: season, episodeNumber: number, title: title.trim(), targetDurationSeconds: duration ?? 0 });
            setTitle("");
            onChanged();
        } catch (error) {
            message.error(error instanceof Error ? error.message : t("studio.script.createFailed"));
        } finally {
            setBusy(false);
        }
    };

    // Selecting an episode asks the core for its script: EnsureScript creates the
    // script row when the episode has none, so the id below is always the core's.
    const select = async (episode: desktop.EpisodeDTO) => {
        onSelectEpisode(episode.id);
        setScript(null);
        setScenes([]);
        try {
            const record = await ensureScript(episode.id);
            setScript(record);
            if (record.currentVersionId) {
                setScenes(await listScenes(record.currentVersionId));
            }
        } catch (error) {
            message.error(error instanceof Error ? error.message : t("studio.script.loadFailed"));
        }
    };

    const columns: ColumnsType<desktop.EpisodeDTO> = [
        {
            title: t("studio.script.season"),
            key: "ordinal",
            width: 120,
            render: (_, record) => <span className="text-sm">{`S${record.seasonNumber}E${record.episodeNumber}`}</span>,
        },
        { title: t("studio.script.title"), dataIndex: "title", key: "title", render: (value: string) => value || "—" },
        {
            title: t("studio.script.statusLabel"),
            dataIndex: "status",
            key: "status",
            width: 140,
            render: (value: string) => <Tag>{t(`studio.status.${value}`, { defaultValue: value })}</Tag>,
        },
        { title: t("studio.script.targetLabel"), dataIndex: "targetDurationSeconds", key: "duration", width: 140 },
        {
            title: "",
            key: "select",
            width: 110,
            render: (_, record) => (
                <Button size="small" data-testid={`studio-episode-select-${record.id}`} onClick={() => void select(record)}>
                    {t("studio.script.select")}
                </Button>
            ),
        },
    ];

    return (
        <div className="space-y-6">
            <div className="flex flex-wrap items-end gap-3 rounded-xl border border-stone-200 p-4 dark:border-stone-800">
                <label className="w-20">
                    <span className="mb-1 block text-sm">{t("studio.script.season")}</span>
                    <InputNumber className="w-full" min={0} precision={0} value={season} onChange={(value) => setSeason(typeof value === "number" ? value : 1)} />
                </label>
                <label className="w-20">
                    <span className="mb-1 block text-sm">{t("studio.script.episodeNumber")}</span>
                    <InputNumber className="w-full" min={0} precision={0} value={number} onChange={(value) => setNumber(typeof value === "number" ? value : 1)} />
                </label>
                <label className="min-w-48 flex-1">
                    <span className="mb-1 block text-sm">{t("studio.script.title")}</span>
                    <Input value={title} maxLength={200} data-testid="studio-episode-title" onChange={(event) => setTitle(event.target.value)} />
                </label>
                <label className="w-40">
                    <span className="mb-1 block text-sm">{t("studio.script.targetDuration")}</span>
                    <InputNumber className="w-full" min={0} precision={0} value={duration} onChange={(value) => setDuration(typeof value === "number" ? value : null)} />
                </label>
                <Button type="primary" icon={<Plus className="size-4" />} loading={busy} data-testid="studio-episode-create" onClick={() => void create()}>
                    {t("studio.script.create")}
                </Button>
            </div>

            {episodes.length === 0 ? (
                <Empty description={t("studio.script.empty")} />
            ) : (
                <Table<desktop.EpisodeDTO>
                    rowKey="id"
                    size="small"
                    pagination={false}
                    columns={columns}
                    dataSource={episodes}
                    rowClassName={(record) => (record.id === activeEpisodeId ? "bg-stone-50 dark:bg-stone-900" : "")}
                    data-testid="studio-episode-table"
                />
            )}

            {script ? (
                <section className="rounded-xl border border-stone-200 p-4 dark:border-stone-800" data-testid="studio-script-detail">
                    <p className="text-sm">
                        {t("studio.script.scriptId")}: <code>{script.id}</code>
                    </p>
                    {script.currentVersionId ? (
                        <>
                            <h3 className="mt-4 text-sm font-medium">{t("studio.script.scenes")}</h3>
                            {scenes.length === 0 ? (
                                <p className="mt-1 text-sm text-stone-500">{t("studio.script.scenesEmpty")}</p>
                            ) : (
                                <ul className="mt-2 space-y-1 text-sm">
                                    {scenes.map((scene) => (
                                        <li key={scene.id} className="flex items-center gap-2">
                                            <span className="text-stone-500">{scene.ordinal}</span>
                                            <span className="truncate">{scene.slugline ?? scene.summary ?? "—"}</span>
                                        </li>
                                    ))}
                                </ul>
                            )}
                        </>
                    ) : (
                        <p className="mt-2 text-sm text-stone-500">{t("studio.script.noVersion")}</p>
                    )}
                </section>
            ) : null}
        </div>
    );
}

// ---------------------------------------------------------------------------
// Assets
// ---------------------------------------------------------------------------

export type AssetsSectionProps = {
    projectId: string;
    /** The asset type this section lists: "character", "location" or "prop". */
    type: string;
    assets: desktop.AssetDTO[];
    onChanged: () => void;
};

export function AssetsSection({ projectId, type, assets, onChanged }: AssetsSectionProps) {
    const { t } = useTranslation();
    const { message } = App.useApp();
    const [name, setName] = useState("");
    const [busy, setBusy] = useState(false);
    const typeLabel = t(`studio.sections.${sectionKeyForAssetType(type)}.title`);

    const create = async () => {
        if (name.trim() === "") {
            message.error(t("studio.assets.nameRequired"));
            return;
        }
        setBusy(true);
        try {
            await createAsset({ projectId, type, name: name.trim() });
            message.success(t("studio.assets.created"));
            setName("");
            onChanged();
        } catch (error) {
            message.error(error instanceof Error ? error.message : t("studio.assets.createFailed"));
        } finally {
            setBusy(false);
        }
    };

    const columns: ColumnsType<desktop.AssetDTO> = [
        { title: t("studio.assets.nameLabel"), dataIndex: "name", key: "name" },
        {
            title: t("studio.assets.statusLabel"),
            dataIndex: "status",
            key: "status",
            width: 130,
            render: (value: string) => <Tag>{t(`studio.status.${value}`, { defaultValue: value })}</Tag>,
        },
        {
            title: t("studio.assets.versionLabel"),
            key: "version",
            render: (_, record) =>
                record.currentApprovedVersionId ? <code className="text-xs">{record.currentApprovedVersionId}</code> : <span className="text-xs text-stone-500">{t("studio.assets.noVersion")}</span>,
        },
    ];

    return (
        <div className="space-y-6">
            <div className="flex flex-wrap items-end gap-3 rounded-xl border border-stone-200 p-4 dark:border-stone-800">
                <label className="min-w-52 flex-1">
                    <span className="mb-1 block text-sm">{t("studio.assets.nameLabel")}</span>
                    <Input value={name} maxLength={200} data-testid="studio-asset-name" onChange={(event) => setName(event.target.value)} onPressEnter={() => void create()} />
                </label>
                <Button type="primary" icon={<Plus className="size-4" />} loading={busy} data-testid="studio-asset-create" onClick={() => void create()}>
                    {t("studio.assets.create", { type: typeLabel })}
                </Button>
            </div>

            {assets.length === 0 ? (
                <Empty description={t("studio.assets.empty", { type: typeLabel })} />
            ) : (
                <Table<desktop.AssetDTO> rowKey="id" size="small" pagination={false} columns={columns} dataSource={assets} data-testid="studio-asset-table" />
            )}
        </div>
    );
}

/** sectionKeyForAssetType maps an asset type to its studio section key. */
function sectionKeyForAssetType(type: string): string {
    if (type === "character") return "characters";
    if (type === "location") return "locations";
    return "props";
}

// ---------------------------------------------------------------------------
// Storyboard canvas (projections)
// ---------------------------------------------------------------------------

export type StoryboardCanvasSectionProps = { projectId: string };

/**
 * StoryboardCanvasSection reads the projections the drama commands wrote.
 *
 * It reads `LoadCanvas` from the binding rather than through
 * `resolveCanvasAdapter()`, because the adapter's read path converts a node to
 * `CanvasNodeData` and keeps only the display metadata: `entityType` and
 * `entityId` are dropped on the way through (`toCanvasNode` in
 * canvas-adapter-go.ts). Reading through the adapter would therefore always
 * report "no projected nodes", which is a false statement about a canvas that has
 * them.
 *
 * It also writes nothing. Nodes appear here because a drama command projected
 * them; this section never creates a node to fill the view.
 */
export function StoryboardCanvasSection({ projectId }: StoryboardCanvasSectionProps) {
    const { t } = useTranslation();
    const [snapshot, setSnapshot] = useState<desktop.CanvasSnapshotDTO | null>(null);
    const [error, setError] = useState("");
    const [loading, setLoading] = useState(true);

    const load = useCallback(async () => {
        setLoading(true);
        setError("");
        try {
            setSnapshot(await loadCanvasSnapshot(projectId));
        } catch (cause) {
            // A failed read is reported as a failure; an empty canvas and an
            // unreadable one must not look the same.
            setError(cause instanceof Error ? cause.message : t("studio.canvas.loadFailed"));
            setSnapshot(null);
        } finally {
            setLoading(false);
        }
    }, [projectId, t]);

    useEffect(() => {
        void load();
    }, [load]);

    if (loading) return <p className="text-sm text-stone-500">{t("studio.shell.loading")}</p>;
    if (error) return <Alert type="error" showIcon message={t("studio.canvas.loadFailed")} description={error} />;

    const nodes = (snapshot?.nodes ?? []).filter((node) => (node.entityType ?? "") !== "" && (node.entityId ?? "") !== "");
    const edges = (snapshot?.edges ?? []).filter((edge) => edge.relationType !== "");

    return (
        <div className="space-y-6">
            <section>
                <h2 className="text-lg font-medium">{t("studio.canvas.projectedNodes")}</h2>
                {nodes.length === 0 ? (
                    <div className="mt-3 rounded-xl border border-dashed border-stone-300 px-6 py-8 dark:border-stone-700" data-testid="studio-canvas-empty">
                        <p className="text-sm">{t("studio.canvas.empty")}</p>
                        <p className="mt-2 text-xs text-stone-500">{t("studio.canvas.emptyHint")}</p>
                    </div>
                ) : (
                    <ul className="mt-3 space-y-2 text-sm" data-testid="studio-canvas-nodes">
                        {nodes.map((node) => (
                            <li key={node.id} className="flex flex-wrap items-center gap-2" data-projected-node={node.id}>
                                <Tag>{node.entityType}</Tag>
                                <span className="truncate">{node.title}</span>
                                <code className="text-xs text-stone-500">{node.entityId}</code>
                            </li>
                        ))}
                    </ul>
                )}
            </section>

            <section>
                <h2 className="text-lg font-medium">{t("studio.canvas.edgeList")}</h2>
                {edges.length === 0 ? (
                    <p className="mt-2 text-sm text-stone-500" data-testid="studio-canvas-edges-empty">
                        {t("studio.canvas.empty")}
                    </p>
                ) : (
                    <ul className="mt-3 space-y-2 text-sm" data-testid="studio-canvas-edges">
                        {edges.map((edge) => (
                            <li key={edge.id} className="flex flex-wrap items-center gap-2" data-semantic-edge={edge.id}>
                                <Tag>{edge.relationType}</Tag>
                                <span>{t("studio.canvas.validationStatus")}:</span>
                                <span className="text-stone-500">{edge.validationStatus}</span>
                                <code className="text-xs text-stone-500">
                                    {edge.fromNodeId} → {edge.toNodeId}
                                </code>
                            </li>
                        ))}
                    </ul>
                )}
            </section>
        </div>
    );
}

// ---------------------------------------------------------------------------
// Quality
// ---------------------------------------------------------------------------

export type QualitySectionProps = {
    projectId: string;
    runs: desktop.WorkflowRunDTO[];
    marks: desktop.StaleMarkDTO[];
    onChanged: () => void;
};

export function QualitySection({ projectId, runs, marks, onChanged }: QualitySectionProps) {
    const { t } = useTranslation();
    const { message } = App.useApp();
    const [busyKey, setBusyKey] = useState("");
    const [waiving, setWaiving] = useState<desktop.StaleMarkDTO | null>(null);
    const [reason, setReason] = useState("");
    const [decisionId, setDecisionId] = useState("");

    const clear = async (mark: desktop.StaleMarkDTO) => {
        setBusyKey(`${mark.artifactType}:${mark.artifactId}`);
        try {
            await clearStaleMark({ artifactType: mark.artifactType, artifactId: mark.artifactId });
            message.success(t("studio.quality.cleared"));
            onChanged();
        } catch (error) {
            message.error(error instanceof Error ? error.message : t("studio.quality.actionFailed"));
        } finally {
            setBusyKey("");
        }
    };

    const waive = async () => {
        if (!waiving) return;
        // The core refuses a waiver without a decision and a reason (§15.3), so
        // the dialog asks for both rather than sending a request it knows fails.
        if (reason.trim() === "" || decisionId.trim() === "") {
            message.error(t("studio.quality.waiveRequired"));
            return;
        }
        setBusyKey(`${waiving.artifactType}:${waiving.artifactId}`);
        try {
            await waiveStaleMark({ artifactType: waiving.artifactType, artifactId: waiving.artifactId, decisionId: decisionId.trim(), reason: reason.trim() });
            message.success(t("studio.quality.waived"));
            setWaiving(null);
            setReason("");
            setDecisionId("");
            onChanged();
        } catch (error) {
            message.error(error instanceof Error ? error.message : t("studio.quality.actionFailed"));
        } finally {
            setBusyKey("");
        }
    };

    const runColumns: ColumnsType<desktop.WorkflowRunDTO> = [
        { title: t("studio.quality.workflowLabel"), dataIndex: "workflowType", key: "workflowType" },
        {
            title: t("studio.quality.statusLabel"),
            dataIndex: "status",
            key: "status",
            width: 140,
            render: (value: string) => <Tag>{t(`studio.status.${value}`, { defaultValue: value })}</Tag>,
        },
        { title: t("studio.quality.stageLabel"), dataIndex: "currentStage", key: "currentStage", width: 160, render: (value?: string) => value || "—" },
        { title: t("studio.quality.updatedAt"), dataIndex: "updatedAt", key: "updatedAt", width: 200, render: (value: string) => new Date(value).toLocaleString() },
    ];

    return (
        <div className="space-y-8">
            <section>
                <h2 className="text-lg font-medium">{t("studio.quality.runs")}</h2>
                {runs.length === 0 ? (
                    <Empty description={t("studio.quality.noRuns")} />
                ) : (
                    <Table<desktop.WorkflowRunDTO> rowKey="id" size="small" pagination={false} columns={runColumns} dataSource={runs} data-testid="studio-runs-table" />
                )}
            </section>

            <section>
                <h2 className="text-lg font-medium">{t("studio.quality.marks")}</h2>
                {marks.length === 0 ? (
                    <Empty description={t("studio.quality.noMarks")} />
                ) : (
                    <ul className="mt-3 space-y-3" data-testid="studio-marks">
                        {marks.map((mark) => {
                            const key = `${mark.artifactType}:${mark.artifactId}`;
                            return (
                                <li
                                    key={key}
                                    data-stale-mark={key}
                                    data-stale-mark-severity={mark.severity}
                                    className="flex flex-wrap items-center gap-3 rounded-xl border border-stone-200 p-4 dark:border-stone-800"
                                >
                                    <ShieldAlert className="size-4 shrink-0 text-stone-500" aria-hidden="true" />
                                    <Tag color={severityColour(mark.severity)}>{t(`studio.severity.${mark.severity}`, { defaultValue: mark.severity })}</Tag>
                                    <span className="text-sm">
                                        {t("studio.quality.artifactTypeLabel")}: {mark.artifactType}
                                    </span>
                                    <code className="text-xs text-stone-500">{mark.artifactId}</code>
                                    {mark.reason ? <span className="min-w-40 flex-1 text-sm text-stone-600 dark:text-stone-400">{mark.reason}</span> : null}
                                    {mark.waived ? <Tag color="gold">{t("studio.quality.waivedTag")}</Tag> : null}
                                    <Space>
                                        <Popconfirm title={t("studio.quality.confirmClear")} onConfirm={() => void clear(mark)}>
                                            <Button size="small" loading={busyKey === key} data-testid={`studio-mark-clear-${key}`}>
                                                {t("studio.quality.clear")}
                                            </Button>
                                        </Popconfirm>
                                        <Button
                                            size="small"
                                            data-testid={`studio-mark-waive-${key}`}
                                            onClick={() => {
                                                setWaiving(mark);
                                                setReason("");
                                                setDecisionId("");
                                            }}
                                        >
                                            {t("studio.quality.waive")}
                                        </Button>
                                    </Space>
                                </li>
                            );
                        })}
                    </ul>
                )}
            </section>

            <Modal
                title={t("studio.quality.waiveTitle")}
                open={waiving !== null}
                centered
                width={520}
                onCancel={() => setWaiving(null)}
                footer={
                    <Space>
                        <Button onClick={() => setWaiving(null)}>{t("common.cancel")}</Button>
                        <Button type="primary" loading={busyKey !== ""} data-testid="studio-mark-waive-submit" onClick={() => void waive()}>
                            {t("studio.quality.waive")}
                        </Button>
                    </Space>
                }
            >
                <p className="mb-4 text-xs text-stone-500">{t("studio.quality.waiveRequired")}</p>
                <label className="mb-4 block">
                    <span className="mb-1 block text-sm">{t("studio.quality.waiveReason")}</span>
                    <Input.TextArea value={reason} rows={3} data-testid="studio-mark-waive-reason" onChange={(event) => setReason(event.target.value)} />
                </label>
                <label className="block">
                    <span className="mb-1 block text-sm">{t("studio.quality.waiveDecisionId")}</span>
                    <Input value={decisionId} data-testid="studio-mark-waive-decision" onChange={(event) => setDecisionId(event.target.value)} />
                </label>
            </Modal>
        </div>
    );
}

/** severityColour maps a severity to a tag colour. Text always accompanies it. */
function severityColour(severity: string): string {
    switch (severity) {
        case "breaking":
            return "red";
        case "review_required":
            return "orange";
        default:
            return "default";
    }
}
