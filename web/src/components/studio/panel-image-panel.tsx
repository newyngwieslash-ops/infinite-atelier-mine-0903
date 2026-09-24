import { useCallback, useEffect, useMemo, useState } from "react";
import { App, Button, Empty, InputNumber, Modal, Space, Table, Tag, Tooltip, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Image as ImageIcon, RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";

import {
    approvePanelImage,
    checkStoryboardGate,
    collectBatchResults,
    createAsset,
    createPanelVersion,
    listAssetVersions,
    listAssets,
    listPanels,
    runImageBatch,
} from "@/services/desktop/drama";
import { cancelJobs, isDesktopJobBindingsAvailable, listJobs, onJobChanged } from "@/services/desktop/jobs";
import { channelIdForModel, decodeModelSelection } from "@/services/desktop/model-selection";
import {
    approvedCount,
    approvedImageOf,
    assetByItem,
    assetNameFor,
    candidateVersionIds,
    formatGateReason,
    freshCandidates,
    gateBlocks,
    isStaleConflict,
    itemRevisionOf,
    jobCountFor,
    jobIdsOf,
    rowsNeedingAssets,
    rowsNeedingPanels,
    wantsBatchEvent,
} from "@/services/desktop/panel-chain";
import { useEffectiveConfig } from "@/stores/use-config-store";
import type { desktop } from "@/wailsjs/go/models";

/**
 * The panel-image chain: generate a board's panel images, collect them, and approve one
 * (ADR-0017, FR-070 and AC-BOARD-003's UI half).
 *
 * # Why this component exists at all
 *
 * `storyboard_panel_versions.approved_image_asset_version_id` is the column the MP4 export joins
 * on (`timeline.go`, `final_reader.go`) and the Final Ruleset's first rule is that every required
 * shot has approved media. Every command that writes it existed and was tested — and NOTHING in
 * the webview called any of them, so the export chain had no middle in the interface. This is
 * that middle.
 *
 * # The five steps, in the order the backend requires
 *
 * 1. **The gate is read first.** `CheckStoryboardGate` reads four facts (the episode's gap report
 *    must resolve with no unresolved required item, and the named board must be approved) and
 *    REFUSES by throwing. Reading before submitting is what lets the button say WHY rather than
 *    letting a user press a control that fails on the next call.
 * 2. **One asset per row.** `CollectBatchResults` takes `assetByItem` — the caller's own map of
 *    storyboard item id to asset id — because asset identity is the user's to decide, not the
 *    backend's to invent. Each row gets an image asset named from its ordinal, and a re-run
 *    REUSES it (found by name in the project's image assets) so a second batch appends versions
 *    rather than creating a parallel asset.
 * 3. **A panel version per row.** §9.5 approves a PANEL, and `CreatePanelVersion` had no
 *    production caller either. One is created per row that has none, carrying the row's own
 *    `visualDescription` as the visual prompt.
 * 4. **The batch submits, then collects.** `RunImageBatch` is idempotent per candidate, so a
 *    re-run reports duplicates rather than paying twice. Collection skips jobs that have not
 *    settled, which is why the page can collect while a batch is still running and collect again
 *    afterwards.
 * 5. **One candidate is approved.** The whole gallery travels with the approval because §9.5's
 *    rule — checked in the domain — is that the approved image must be one of the candidates the
 *    caller SUPPLIED. `expectedRevision` is the parent ITEM's revision, and a stale one is
 *    refused rather than merged.
 *
 * # Refreshing
 *
 * No timer. A provider-backed read on a clock is what `video-view.tsx` documents refusing, and
 * the batch's jobs already announce themselves: the component subscribes to the job-change event
 * and reloads when one arrives, offers a manual refresh, and unsubscribes on unmount (AGENTS
 * section 9's subscription rule).
 */

export type PanelImagePanelProps = {
    projectId: string;
    episodeId: string;
    /** The board the batch runs against. Generation is refused for an unapproved board. */
    storyboardVersionId: string;
    /** The board's rows, as the table has them, with the revisions the approval guards on. */
    items: desktop.StoryboardItemDTO[];
    onChanged: () => void;
};

export function PanelImagePanel({ projectId, episodeId, storyboardVersionId, items, onChanged }: PanelImagePanelProps) {
    const { t } = useTranslation();
    const { message } = App.useApp();
    // The provider selection is read the way every other generation surface reads it: the store
    // holds a `channel::model` string, and the channel is resolved from it. An image batch is a
    // provider call like any other, which is why the batch request refuses without both parts and
    // this component refuses to submit before it has them.
    const config = useEffectiveConfig();
    const modelValue = config.imageModel || config.model;

    const [busy, setBusy] = useState(false);
    const [gateReason, setGateReason] = useState("");
    const [gateChecked, setGateChecked] = useState(false);
    const [confirmOpen, setConfirmOpen] = useState(false);
    const [candidates, setCandidates] = useState(2);
    const [promptSuffix, setPromptSuffix] = useState("");
    const [batchJobIds, setBatchJobIds] = useState<string[]>([]);
    const [jobRows, setJobRows] = useState<desktop.JobDTO[]>([]);
    const [gallery, setGallery] = useState<Record<string, desktop.AssetVersionDTO[]>>({});
    const [panelByItem, setPanelByItem] = useState<Record<string, desktop.StoryboardPanelVersionDTO>>({});
    const [drawerItem, setDrawerItem] = useState<desktop.StoryboardItemDTO | null>(null);

    const model = useMemo(() => decodeModelSelection(modelValue).model, [modelValue]);
    const providerId = useMemo(() => channelIdForModel(config, modelValue), [config, modelValue]);

    // The gate is READ, not assumed: a button that is enabled on an unapproved board produces a
    // refusal on the next call, which is a worse experience than a disabled button with a reason.
    const readGate = useCallback(async () => {
        if (!episodeId || !storyboardVersionId) {
            setGateReason(t("studio.panelImages.gateNoBoard"));
            setGateChecked(true);
            return;
        }
        try {
            await checkStoryboardGate({ episodeId, storyboardVersionId });
            setGateReason("");
        } catch (failure) {
            setGateReason(String(failure));
        } finally {
            setGateChecked(true);
        }
    }, [episodeId, storyboardVersionId, t]);

    useEffect(() => {
        void readGate();
    }, [readGate]);

    // The gallery: one image asset per row, found by the name step 2 gives it, with its versions.
    // A row whose asset does not exist yet has no candidates, which the drawer says rather than
    // showing an empty gallery as if a generation had produced nothing.
    const reloadGallery = useCallback(async () => {
        if (!projectId) return;
        const assets = await listAssets({ projectId, types: ["image"] });
        const next: Record<string, desktop.AssetVersionDTO[]> = {};
        for (const row of items) {
            const asset = assets.find((candidate) => candidate.name === assetNameFor(row));
            next[row.id] = asset ? await listAssetVersions(asset.id) : [];
        }
        setGallery(next);
    }, [projectId, items]);

    const reloadPanels = useCallback(async () => {
        // The NEWEST panel version per row, because that is the one §9.5's approval is recorded
        // against and therefore the one whose `approvedImageAssetVersionId` is the current state.
        const next: Record<string, desktop.StoryboardPanelVersionDTO> = {};
        for (const row of items) {
            const panels = await listPanels(row.id);
            if (panels.length > 0) next[row.id] = panels[panels.length - 1];
        }
        setPanelByItem(next);
    }, [items]);

    useEffect(() => {
        void reloadGallery();
        void reloadPanels();
    }, [reloadGallery, reloadPanels]);

    const refreshJobs = useCallback(async () => {
        if (batchJobIds.length === 0) {
            setJobRows([]);
            return;
        }
        const all = await listJobs({ limit: 500 } as never);
        setJobRows(all.filter((job) => batchJobIds.includes(job.id)));
    }, [batchJobIds]);

    // The event subscription, with its cleanup. A payload's job id decides whether this panel
    // cares: another project's job moving must not cause a read here.
    useEffect(() => {
        if (!isDesktopJobBindingsAvailable()) return;
        // Only this batch's own jobs: another project's job moving must not cause a read here.
        const unsubscribe = onJobChanged((payload: { jobId?: string }) => {
            if (!wantsBatchEvent(payload.jobId, batchJobIds)) return;
            void refreshJobs();
        });
        return unsubscribe;
    }, [batchJobIds, refreshJobs]);

    useEffect(() => {
        void refreshJobs();
    }, [refreshJobs]);

    const runBatch = useCallback(async () => {
        if (!model || !providerId) {
            message.error(t("studio.panelImages.noModel"));
            return;
        }
        setBusy(true);
        try {
            // Step 2 and step 3, per row, BEFORE the batch: the collect call needs the asset map
            // and the approval needs a panel, both of which are the caller's to supply.
            const assets = await listAssets({ projectId, types: ["image"] });
            for (const row of rowsNeedingAssets(items, assets)) {
                await createAsset({ projectId, type: "image", name: assetNameFor(row) });
            }
            for (const row of rowsNeedingPanels(items, panelByItem)) {
                await createPanelVersion({
                    storyboardItemId: row.id,
                    visualPrompt: row.visualDescription ?? "",
                    changeReason: "panel image batch",
                });
            }
            const result = await runImageBatch({
                storyboardVersionId,
                episodeId,
                projectId,
                perShotCandidates: candidates,
                providerId,
                modelName: model,
                promptSuffix: promptSuffix || undefined,
            });
            const ids = jobIdsOf(result.submissions);
            setBatchJobIds(ids);
            // The count is reported because it is the number of provider calls this created, which
            // is the number a user cares about when a provider bills per call.
            message.success(t("studio.panelImages.submitted", { count: ids.length }));
            setConfirmOpen(false);
            await reloadPanels();
            await reloadGallery();
            onChanged();
        } catch (failure) {
            message.error(String(failure));
        } finally {
            setBusy(false);
        }
    }, [
        model, providerId, projectId, items, panelByItem, storyboardVersionId, episodeId, candidates,
        promptSuffix, message, t, reloadPanels, reloadGallery, onChanged,
    ]);

    const collect = useCallback(async () => {
        if (batchJobIds.length === 0) return;
        setBusy(true);
        try {
            const assets = await listAssets({ projectId, types: ["image"] });
            const collected = await collectBatchResults({ assetByItem: assetByItem(items, assets), jobIds: batchJobIds });
            // The number a user is told is the number of VERSIONS that appeared, not the number
            // of jobs asked about: a duplicate collection reports the jobs and no new versions.
            message.success(t("studio.panelImages.collected", { count: freshCandidates(collected).length }));
            await reloadGallery();
            onChanged();
        } catch (failure) {
            message.error(String(failure));
        } finally {
            setBusy(false);
        }
    }, [batchJobIds, projectId, items, message, t, reloadGallery, onChanged]);

    const approve = useCallback(
        async (row: desktop.StoryboardItemDTO, versionId: string) => {
            const panelVersionId = panelByItem[row.id]?.id;
            if (!panelVersionId) {
                message.error(t("studio.panelImages.noPanel"));
                return;
            }
            setBusy(true);
            try {
                // The WHOLE gallery travels: section 9.5 requires the approved image to be one of
                // the candidates the caller supplied, so sending only the chosen id would be
                // refused by the domain.
                await approvePanelImage({
                    panelVersionId,
                    approvedImageAssetVersionId: versionId,
                    candidateVersionIds: candidateVersionIds(gallery[row.id] ?? []),
                    expectedRevision: itemRevisionOf(row),
                });
                message.success(t("studio.panelImages.approved"));
                await reloadGallery();
                onChanged();
            } catch (failure) {
                const text = String(failure);
                // A stale revision is the one failure the user can act on, so it is named as what
                // it is rather than passed through as a transport error.
                message.error(isStaleConflict(text) ? t("studio.panelImages.approveConflict") : text);
            } finally {
                setBusy(false);
            }
        },
        [panelByItem, gallery, message, t, reloadGallery, onChanged],
    );

    const jobColumns: ColumnsType<desktop.JobDTO> = [
        { title: t("jobs.id"), dataIndex: "id", width: 120, render: (value: string) => value.slice(0, 8) },
        { title: t("jobs.statusLabel"), dataIndex: "status", width: 130, render: (value: string) => <Tag>{t(`jobs.status.${value}`, { defaultValue: value })}</Tag> },
        { title: t("studio.panelImages.progress"), dataIndex: "progress", width: 90, render: (value?: number) => `${value ?? 0}%` },
        { title: t("jobs.errorCode"), dataIndex: "errorCode", ellipsis: true, render: (value?: string) => value || "—" },
    ];

    // Both come from the rules module, so what the panel shows and what the tests assert are the
    // same code: the approved image is the PANEL's record (§9.5), not the row's.
    const approvedFor = (row: desktop.StoryboardItemDTO) => approvedImageOf(row, panelByItem);
    const approved = approvedCount(items, panelByItem);

    if (items.length === 0) {
        return <Empty description={t("studio.panelImages.noRows")} />;
    }

    return (
        <Space direction="vertical" size="middle" className="w-full" data-testid="studio-panel-images">
            <Space wrap>
                <Button
                    type="primary"
                    icon={<ImageIcon className="size-4" />}
                    loading={busy}
                    disabled={gateBlocks(gateChecked, gateReason)}
                    onClick={() => setConfirmOpen(true)}
                    data-testid="studio-panel-generate"
                >
                    {t("studio.panelImages.generate")}
                </Button>
                <Button loading={busy} disabled={batchJobIds.length === 0} onClick={() => void collect()} data-testid="studio-panel-collect">
                    {t("studio.panelImages.collect")}
                </Button>
                <Button
                    icon={<RefreshCw className="size-4" />}
                    onClick={() => {
                        void refreshJobs();
                        void reloadGallery();
                        void reloadPanels();
                    }}
                >
                    {t("studio.panelImages.refresh")}
                </Button>
                <Button
                    danger
                    disabled={batchJobIds.length === 0}
                    onClick={() => void cancelJobs(batchJobIds).then(() => refreshJobs())}
                >
                    {t("studio.panelImages.cancel")}
                </Button>
                <Typography.Text type="secondary">
                    {t("studio.panelImages.approvedCount", { approved, total: items.length })}
                </Typography.Text>
            </Space>

            {gateBlocks(gateChecked, gateReason) ? (
                <Typography.Text type="warning" data-testid="studio-panel-gate">
                    {formatGateReason(t("studio.panelImages.gateBlocked"), gateReason)}
                </Typography.Text>
            ) : null}

            <Table
                size="small"
                rowKey="id"
                columns={[
                    { title: t("studio.storyboardTable.shot"), dataIndex: "ordinal", width: 70, render: (value: number) => `#${value}` },
                    {
                        title: t("studio.panelImages.candidates"),
                        key: "candidates",
                        width: 120,
                        render: (_: unknown, row: desktop.StoryboardItemDTO) => {
                            const versions = gallery[row.id] ?? [];
                            return versions.length === 0 ? "—" : `${versions.length}`;
                        },
                    },
                    {
                        title: t("studio.panelImages.approvedImage"),
                        key: "approved",
                        width: 140,
                        render: (_: unknown, row: desktop.StoryboardItemDTO) =>
                            approvedFor(row) !== "" ? (
                                <Tag color="green" data-testid={`studio-panel-approved-${row.id}`}>
                                    {t("studio.panelImages.approved")}
                                </Tag>
                            ) : (
                                "—"
                            ),
                    },
                    {
                        title: "",
                        key: "actions",
                        width: 100,
                        render: (_: unknown, row: desktop.StoryboardItemDTO) => (
                            <Button size="small" type="link" onClick={() => setDrawerItem(row)} data-testid={`studio-panel-open-${row.id}`}>
                                {t("studio.panelImages.open")}
                            </Button>
                        ),
                    },
                ]}
                dataSource={items}
                pagination={false}
                data-testid="studio-panel-table"
            />

            {jobRows.length > 0 ? (
                <Table size="small" rowKey="id" columns={jobColumns} dataSource={jobRows} pagination={false} data-testid="studio-panel-jobs" />
            ) : null}

            <Modal
                open={confirmOpen}
                title={t("studio.panelImages.generateTitle")}
                onCancel={() => setConfirmOpen(false)}
                onOk={() => void runBatch()}
                okButtonProps={{ loading: busy }}
                data-testid="studio-panel-confirm"
            >
                <Space direction="vertical" className="w-full">
                    <Typography.Paragraph type="secondary">{t("studio.panelImages.generateHint")}</Typography.Paragraph>
                    <Space>
                        <Typography.Text>{t("studio.panelImages.perShot")}</Typography.Text>
                        <InputNumber min={1} max={8} value={candidates} onChange={(value) => setCandidates(Number(value ?? 1))} />
                    </Space>
                    <Space>
                        <Typography.Text>{t("studio.panelImages.promptSuffix")}</Typography.Text>
                        <input
                            className="rounded border border-stone-300 px-2 py-1 text-sm dark:border-stone-700 dark:bg-stone-900"
                            value={promptSuffix}
                            onChange={(event) => setPromptSuffix(event.target.value)}
                        />
                    </Space>
                    {/* The number of provider calls this is about to make, stated before it makes
                        them: with no per-provider concurrency limit in this build (handoff P0-2,
                        FR-150), the count is the only warning a user gets about quota. */}
                    <Typography.Text type="warning" data-testid="studio-panel-job-count">
                        {t("studio.panelImages.jobCount", { count: jobCountFor(items.length, candidates) })}
                    </Typography.Text>
                </Space>
            </Modal>

            <Modal
                open={drawerItem !== null}
                title={drawerItem ? `#${drawerItem.ordinal} · ${t("studio.panelImages.gallery")}` : ""}
                onCancel={() => setDrawerItem(null)}
                footer={null}
                width={720}
            >
                {drawerItem ? (
                    <PanelGallery
                        versions={gallery[drawerItem.id] ?? []}
                        approvedImageVersionId={approvedFor(drawerItem)}
                        busy={busy}
                        onApprove={(versionId) => void approve(drawerItem, versionId)}
                    />
                ) : null}
            </Modal>
        </Space>
    );
}

/** The candidate gallery for one row, with the approve control per candidate. */
function PanelGallery({
    versions,
    approvedImageVersionId,
    busy,
    onApprove,
}: {
    versions: desktop.AssetVersionDTO[];
    /** The image in force, read from the panel rather than from the row: §9.5 records it there. */
    approvedImageVersionId: string;
    busy: boolean;
    onApprove: (versionId: string) => void;
}) {
    const { t } = useTranslation();
    if (versions.length === 0) {
        return <Empty description={t("studio.panelImages.noCandidates")} />;
    }
    return (
        <Space direction="vertical" className="w-full" data-testid="studio-panel-gallery">
            {versions.map((version) => (
                <Space key={version.id} className="w-full justify-between">
                    <Typography.Text>
                        v{version.versionNumber} · {version.status}
                        {version.generationJobId ? ` · ${version.generationJobId.slice(0, 8)}` : ""}
                    </Typography.Text>
                    {version.id === approvedImageVersionId ? (
                        <Tag color="green">{t("studio.panelImages.approved")}</Tag>
                    ) : (
                        <Tooltip title={t("studio.panelImages.approveHint")}>
                            <Button
                                size="small"
                                type="primary"
                                ghost
                                loading={busy}
                                onClick={() => onApprove(version.id)}
                                data-testid={`studio-panel-approve-${version.id}`}
                            >
                                {t("studio.panelImages.approve")}
                            </Button>
                        </Tooltip>
                    )}
                </Space>
            ))}
        </Space>
    );
}
