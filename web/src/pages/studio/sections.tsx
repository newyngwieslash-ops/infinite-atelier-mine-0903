import { useCallback, useEffect, useState } from "react";
import { Alert, App, Button, Drawer, Empty, Input, InputNumber, Modal, Popconfirm, Select, Space, Table, Tag, Tooltip, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { FilePlus2, Plus, ShieldAlert, UserPlus } from "lucide-react";
import { useTranslation } from "react-i18next";

import {
    getReviewReport,
    approveAssetVersion,
    clearStaleMark,
    createAsset,
    createSourceDocument,
    getApprovalImpact,
    listAssetUsages,
    listAssetVersions,
    waiveStaleMark,
} from "@/services/desktop/drama";
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

export { ScriptSection } from "@/components/studio/script-view";
export type { ScriptSectionProps } from "@/components/studio/script-view";

// ---------------------------------------------------------------------------
// Production (WP-09)
// ---------------------------------------------------------------------------

// The two production sections, re-exported the way the script and graph ones are: the
// shell keeps a single import surface, and each section's body lives in its own file
// because one section growing past a file's worth of behaviour should be its own file.

export { DirectorSection } from "@/components/studio/director-view";
export type { DirectorSectionProps } from "@/components/studio/director-view";
export { StoryboardTableSection } from "@/components/studio/storyboard-table-view";
export type { StoryboardTableSectionProps } from "@/components/studio/storyboard-table-view";

// ---------------------------------------------------------------------------
// Media and export (WP-11)
// ---------------------------------------------------------------------------

// The three media sections, re-exported the way the production ones above are: the shell keeps a
// single import surface, and each section lives in its own file because one section growing past a
// file's worth of behaviour should be its own file. These three are the largest in the studio — the
// timeline in particular owns subtitles, an export and the manifest — and keeping them out of here
// is what the file-per-section rule is for.

export { VideoSection } from "@/components/studio/video-view";
export type { VideoSectionProps } from "@/components/studio/video-view";
export { AudioSection } from "@/components/studio/audio-view";
export type { AudioSectionProps } from "@/components/studio/audio-view";
export { TimelineSection } from "@/components/studio/timeline-view";
export type { TimelineSectionProps } from "@/components/studio/timeline-view";

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

    // The version drawer's state. It is per-asset rather than a page: opening one asset's
    // versions must not disturb the list a user is reading.
    const [versionedAsset, setVersionedAsset] = useState<desktop.AssetDTO | null>(null);

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
        {
            title: "",
            key: "versions",
            width: 120,
            render: (_, record) => (
                <Button
                    size="small"
                    type="link"
                    data-testid={`studio-asset-versions-${record.id}`}
                    onClick={() => setVersionedAsset(record)}
                >
                    {t("studio.assets.versions")}
                </Button>
            ),
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
            <AssetVersionsDrawer
                asset={versionedAsset}
                onClose={() => setVersionedAsset(null)}
                onChanged={onChanged}
            />
        </div>
    );
}

/**
 * AssetVersionsDrawer is one asset's versions, its usages, and the approval switch.
 *
 * AC-ASSET-001's scenario runs through this: two candidate versions, approve v1, approve v2
 * and v1 becomes superseded, a shot using v1 triggers the impact analysis, and v1 is NOT
 * deleted. The impact is shown BEFORE the approval because DOMAIN_MODEL section 8.2 requires
 * it before the switch — and because the core refuses an unacknowledged approval, so a UI
 * that could not ask the question could only send the acknowledgement blind.
 *
 * The usages are READ rather than inferred: they are what the impact list is built from, and
 * a UI that guessed at them would show a user a list nobody computed.
 */
function AssetVersionsDrawer({
    asset,
    onClose,
    onChanged,
}: {
    asset: desktop.AssetDTO | null;
    onClose: () => void;
    onChanged: () => void;
}) {
    const { t } = useTranslation();
    const { message } = App.useApp();
    const [versions, setVersions] = useState<desktop.AssetVersionDTO[]>([]);
    const [impact, setImpact] = useState<desktop.ApprovalImpactDTO | null>(null);
    const [impactFor, setImpactFor] = useState("");
    const [usages, setUsages] = useState<desktop.AssetUsageDTO[]>([]);
    const [loading, setLoading] = useState(false);

    const load = useCallback(async () => {
        if (!asset) {
            setVersions([]);
            setUsages([]);
            return;
        }
        setLoading(true);
        try {
            const found = await listAssetVersions(asset.id);
            setVersions(found);
            // The usages of the version IN FORCE are what an impact switch disturbs, which is
            // why they are read for that one rather than for the whole asset.
            if (asset.currentApprovedVersionId) {
                setUsages(await listAssetUsages(asset.currentApprovedVersionId));
            } else {
                setUsages([]);
            }
        } catch (failure) {
            message.error(String(failure));
        } finally {
            setLoading(false);
        }
    }, [asset, message]);

    useEffect(() => {
        void load();
    }, [load]);

    const askImpact = useCallback(
        async (versionId: string) => {
            try {
                setImpact(await getApprovalImpact(versionId));
                setImpactFor(versionId);
            } catch (failure) {
                message.error(String(failure));
            }
        },
        [message],
    );

    const approve = useCallback(
        async (versionId: string) => {
            try {
                await approveAssetVersion({ versionId, impactAcknowledged: true });
                message.success(t("studio.assets.approved"));
                await load();
                onChanged();
            } catch (failure) {
                message.error(String(failure));
            } finally {
                setImpact(null);
                setImpactFor("");
            }
        },
        [load, onChanged, t, message],
    );

    const columns: ColumnsType<desktop.AssetVersionDTO> = [
        { title: "#", dataIndex: "versionNumber", width: 60, render: (value: number) => `v${value}` },
        {
            title: t("studio.assets.statusLabel"),
            dataIndex: "status",
            width: 130,
            render: (value: string) =>
                value === "approved" ? <Tag color="green">{value}</Tag> : <Tag>{value}</Tag>,
        },
        {
            title: t("studio.assets.lineage"),
            key: "lineage",
            ellipsis: true,
            render: (_, row) =>
                [row.generationJobId ? `${t("studio.assets.job")}: ${row.generationJobId.slice(0, 8)}` : "", row.seed ? `seed ${row.seed}` : ""]
                    .filter(Boolean)
                    .join(" · ") || "—",
        },
        {
            title: "",
            key: "actions",
            width: 200,
            render: (_, row) =>
                row.status === "approved" ? (
                    <Tag color="green">{t("studio.assets.approved")}</Tag>
                ) : row.status === "superseded" ? (
                    // A superseded version is KEPT rather than deleted: AC-ASSET-001's last
                    // clause is that v1 survives the switch to v2.
                    <Tooltip title={t("studio.assets.supersededHint")}>
                        <Tag>{t("studio.assets.superseded")}</Tag>
                    </Tooltip>
                ) : (
                    <Button size="small" type="primary" ghost onClick={() => void askImpact(row.id)}>
                        {t("studio.assets.approve")}
                    </Button>
                ),
        },
    ];

    return (
        <Drawer
            open={asset !== null}
            onClose={onClose}
            width="min(94vw, 760px)"
            title={asset ? asset.name : ""}
            destroyOnHidden
        >
            <Space direction="vertical" size="middle" className="w-full">
                <Table rowKey="id" size="small" loading={loading} pagination={false} columns={columns} dataSource={versions} />
                {usages.length > 0 ? (
                    <div>
                        <Typography.Text strong>{t("studio.assets.usedBy")}</Typography.Text>
                        <ul className="mt-1 list-disc pl-5 text-sm">
                            {usages.map((usage) => (
                                <li key={`${usage.consumerType}-${usage.consumerId}-${usage.usageRole}`}>
                                    {usage.consumerType} · {usage.consumerId}
                                    {usage.required ? ` (${t("studio.assets.required")})` : ""}
                                </li>
                            ))}
                        </ul>
                    </div>
                ) : null}
                <Modal
                    open={impact !== null}
                    title={t("studio.assets.impactTitle")}
                    onCancel={() => {
                        setImpact(null);
                        setImpactFor("");
                    }}
                    onOk={() => void approve(impactFor)}
                    okText={t("studio.assets.approveAnyway")}
                    width="min(92vw, 560px)"
                    destroyOnHidden
                >
                    {impact && impact.replaces ? (
                        <Space direction="vertical" size="small" className="w-full">
                            <Typography.Text>
                                {t("studio.assets.impactReplaces", { version: impact.replaces })}
                            </Typography.Text>
                            {impact.consumers && impact.consumers.length > 0 ? (
                                <ul className="list-disc pl-5 text-sm">
                                    {impact.consumers.map((consumer) => (
                                        <li key={`${consumer.consumerType}-${consumer.consumerId}-${consumer.usageRole}`}>
                                            {consumer.consumerType} · {consumer.consumerId}
                                            {consumer.required ? ` (${t("studio.assets.required")})` : ""}
                                        </li>
                                    ))}
                                </ul>
                            ) : (
                                <Typography.Text type="secondary">{t("studio.assets.impactNone")}</Typography.Text>
                            )}
                        </Space>
                    ) : (
                        <Typography.Text type="secondary">{t("studio.assets.impactFirst")}</Typography.Text>
                    )}
                </Modal>
            </Space>
        </Drawer>
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
    /**
     * onNavigate moves the shell to another section.
     *
     * It is a PROP rather than a store import because this file is a collection of section bodies and
     * the shell owns which one is showing: a body that reached into the store would make every section
     * able to navigate, which is the shell's decision and not theirs. PRD FR-110's "报告问题可以在 UI
     * 中跳转到实体" is the one place a body has a reason to ask.
     */
    onNavigate: (section: string) => void;
};

export function QualitySection({ projectId, runs, marks, onChanged, onNavigate }: QualitySectionProps) {
    const { t } = useTranslation();
    const { message } = App.useApp();
    const [busyKey, setBusyKey] = useState("");
    const [waiving, setWaiving] = useState<desktop.StaleMarkDTO | null>(null);
    const [reason, setReason] = useState("");
    const [decisionId, setDecisionId] = useState("");
    const [findings, setFindings] = useState<desktop.ReviewReportDTO | null>(null);
    const [findingsKey, setFindingsKey] = useState("");
    const [findingsError, setFindingsError] = useState("");
    const [stageRunId, setStageRunId] = useState("");

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

    /**
     * findingsFor loads a stage run's review report.
     *
     * It is the read WP-07 built and nothing called until now: `getReviewReport` has existed since
     * the review tables did, with the evidence column WP-09 repaired, and no component fetched it. A
     * quality centre that showed only the runs and the stale marks would be showing the workflow's
     * bookkeeping and not its JUDGEMENT, which is what PRD FR-110's report shape is for.
     */
    const findingsFor = async (stageRunId: string) => {
        setFindingsKey(stageRunId);
        setFindingsError("");
        try {
            const report = await getReviewReport(stageRunId);
            setFindings(report);
        } catch (error) {
            setFindings(null);
            setFindingsError(error instanceof Error ? error.message : t("studio.quality.findingsFailed"));
        } finally {
            setFindingsKey("");
        }
    };

    /**
     * evidenceOf renders a finding's references.
     *
     * The column holds the review schema's array of {type, ref}, and it is parsed here rather than
     * sent as a nested document because that is the shape the supervisor's evidence has always been
     * stored in. A parse that fails shows the raw text rather than nothing: a reader is better served
     * by an unparsed column than by a finding whose evidence silently disappeared, which is the exact
     * defect WP-09 found in the writer.
     */
    const evidenceOf = (finding: desktop.ReviewIssueDTO): string[] => {
        if (!finding.evidenceJson) return [];
        try {
            const parsed = JSON.parse(finding.evidenceJson) as { type?: string; ref?: string }[];
            if (!Array.isArray(parsed)) return [finding.evidenceJson];
            return parsed.map((entry) => (entry.ref ? `${entry.type ?? "ref"}:${entry.ref}` : JSON.stringify(entry)));
        } catch {
            return [finding.evidenceJson];
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
                <h2 className="text-lg font-medium">{t("studio.quality.findings")}</h2>
                <Typography.Paragraph className="text-xs text-stone-500">
                    {t("studio.quality.findingsHint")}
                </Typography.Paragraph>
                <Space.Compact className="mb-3 w-full max-w-2xl">
                    <Input
                        value={stageRunId}
                        data-testid="studio-findings-stage-run"
                        placeholder={t("studio.quality.findingsStageRun")}
                        onChange={(event) => setStageRunId(event.target.value)}
                        onPressEnter={() => void findingsFor(stageRunId.trim())}
                    />
                    <Button
                        type="primary"
                        loading={findingsKey !== ""}
                        data-testid="studio-findings-load"
                        onClick={() => void findingsFor(stageRunId.trim())}
                    >
                        {t("studio.quality.findingsLoad")}
                    </Button>
                </Space.Compact>
                {findingsError ? (
                    <Alert className="mb-3" type="error" showIcon message={findingsError} data-testid="studio-findings-error" />
                ) : null}
                {findings === null ? null : (
                    <div data-testid="studio-findings" data-findings-passed={findings.passed ? "true" : "false"}>
                        <div className="mb-2 flex flex-wrap items-center gap-2 text-sm">
                            <Tag color={findings.passed ? "green" : "red"}>
                                {findings.passed ? t("studio.quality.passed") : t("studio.quality.failed")}
                            </Tag>
                            <span>{t("studio.quality.severityLabel")}: {findings.severity || "—"}</span>
                            <span className="text-stone-500">{findings.rulesetVersion}</span>
                        </div>
                        {findings.summary ? <p className="mb-3 text-sm">{findings.summary}</p> : null}
                        {findings.issues.length === 0 ? (
                            <Empty description={t("studio.quality.noFindings")} />
                        ) : (
                            <ul className="space-y-2" data-testid="studio-findings-list">
                                {findings.issues.map((finding) => (
                                    <li
                                        key={finding.id}
                                        data-finding={finding.id}
                                        data-finding-rule={finding.rule}
                                        data-finding-source={finding.source}
                                        data-finding-entity={finding.entityId}
                                        data-finding-field={finding.field}
                                        className="rounded-xl border border-stone-200 p-4 dark:border-stone-800"
                                    >
                                        <div className="flex flex-wrap items-center gap-2">
                                            <Tag color={severityColour(finding.severity)}>
                                                {t(`studio.severity.${finding.severity}`, { defaultValue: finding.severity })}
                                            </Tag>
                                            {/* The mark AGENT_CONTRACTS section 11.4 requires: which
                                                half of the review found this. A reader deciding what
                                                to do about a finding needs to know whether it is a
                                                computation over stored rows or a model's reading. */}
                                            <Tag color={finding.source === "deterministic" ? "blue" : "purple"}>
                                                {t(`studio.quality.source.${finding.source}`, { defaultValue: finding.source })}
                                            </Tag>
                                            <code className="text-xs">{finding.rule}</code>
                                            {/* FR-110's quality-rule classification. An EMPTY
                                                category renders nothing rather than a tag: a
                                                supervisor's finding states none, and every row
                                                written before the column existed is empty, so
                                                showing "technical" here would be the UI inventing
                                                a classification nobody made. */}
                                            {finding.category ? (
                                                <Tag
                                                    data-finding-category={finding.category}
                                                    color="gold"
                                                >
                                                    {t(`studio.quality.category.${finding.category}`, {
                                                        defaultValue: finding.category,
                                                    })}
                                                </Tag>
                                            ) : null}
                                            {finding.autoFixable ? (
                                                <Tag color="cyan">{t("studio.quality.autoFixable")}</Tag>
                                            ) : null}
                                        </div>
                                        <p className="mt-2 text-sm">{finding.problem}</p>
                                        {finding.suggestion ? (
                                            <p className="mt-1 text-sm text-stone-600 dark:text-stone-400">
                                                {t("studio.quality.suggestion")}: {finding.suggestion}
                                            </p>
                                        ) : null}
                                        {/* PRD FR-110's "报告问题可以在 UI 中跳转到实体": the entity a
                                            finding is about is shown as a jumpable reference, and
                                            the location is shown when there is no entity. */}
                                        {finding.entityId ? (
                                            <p className="mt-1 flex flex-wrap items-center gap-2 text-xs">
                                                <span>{t("studio.quality.entityLabel")}:</span>
                                                {/* PRD FR-110's "报告问题可以在 UI 中跳转到实体".
                                                    The button navigates to the section that owns the
                                                    entity — a storyboard row goes to the board, a
                                                    shot to the script, an asset to its section — or
                                                    reports that it has no section when the entity is
                                                    one this shell does not show. */}
                                                <Button
                                                    size="small"
                                                    type="link"
                                                    data-finding-target={finding.entityId}
                                                    data-finding-section={sectionForEntity(finding.entityType ?? "")}
                                                    onClick={() => jumpToEntity(finding.entityType ?? "", onNavigate)}
                                                >
                                                    {finding.entityType}:{finding.entityId}
                                                </Button>
                                                {finding.field ? <span className="text-stone-500">{finding.field}</span> : null}
                                            </p>
                                        ) : null}
                                        {finding.location ? (
                                            <p className="mt-1 text-xs text-stone-500">{finding.location}</p>
                                        ) : null}
                                        {evidenceOf(finding).length > 0 ? (
                                            <ul className="mt-2 space-y-1" data-testid="studio-finding-evidence">
                                                {evidenceOf(finding).map((entry) => (
                                                    <li key={entry} className="text-xs text-stone-500">
                                                        {t("studio.quality.evidence")}: <code>{entry}</code>
                                                    </li>
                                                ))}
                                            </ul>
                                        ) : null}
                                    </li>
                                ))}
                            </ul>
                        )}
                    </div>
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

/**
 * jumpToEntity switches the shell to the section that owns a finding's entity.
 *
 * It is the second half of FR-110's "报告问题可以在 UI 中跳转到实体": the button knows which entity
 * the finding is about, this knows where that entity is shown, and the callback's `setSection` is what
 * moves there. An entity with no section does nothing, and the call site renders it as a plain
 * reference rather than as a control — a button that went nowhere would be worse than a label.
 *
 * The navigation is deliberately SECTION-level rather than row-level. The shell carries no "selected
 * entity" projection, and inventing one here would make the quality centre responsible for the
 * board's or the asset list's scroll position — which is a different feature from "take me to where
 * this lives", and is what the section's own search and filters are for.
 */
function jumpToEntity(entityType: string, onNavigate: (section: string) => void) {
    const section = sectionForEntity(entityType);
    if (section === "") return;
    onNavigate(section);
}

/**
 * sectionForEntity maps a finding's entity type to the studio section that owns it.
 *
 * The mapping is the shell's own vocabulary rather than the domain's, which is why it is a table here
 * and not a field on the Go DTO: a section is a place in this UI, and a domain entity that has no
 * section (a workflow run, a staleness mark) answers with the empty string — which the call site
 * renders as a reference without a jump rather than as a button that goes nowhere.
 */
function sectionForEntity(entityType: string): string {
    switch (entityType) {
        case "storyboard_item":
        case "storyboard_version":
            return "storyboard-table";
        case "shot":
        case "scene":
        case "script_version":
            return "script";
        case "asset":
        case "asset_version":
        case "character":
        case "location":
        case "prop":
            return "characters";
        case "director_plan_version":
            return "director";
        case "story_entity":
        case "story_event":
            return "story-graph";
        default:
            return "";
    }
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
