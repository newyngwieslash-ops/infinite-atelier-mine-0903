import { useCallback, useEffect, useMemo, useState } from "react";
import { Alert, App, Button, Empty, Popconfirm, Select, Space, Table, Tag, Tooltip, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Box, CheckCircle2, Play } from "lucide-react";
import { useTranslation } from "react-i18next";

import { DirectorPanel } from "@/components/canvas/director-panel";
import { isMonoformSnapshotAvailable, storePrevisSnapshot } from "@/services/desktop/monoform";
import {
    approveDirectorPlanVersion,
    createWorkflowRun,
    listDirectorPlanVersions,
    listEpisodes,
    listStoryboardItems,
    listStoryboardVersions,
    listWorkflowRuns,
    runScriptStage,
    setShotOverrides,
} from "@/services/desktop/drama";
import type { desktop } from "@/wailsjs/go/models";

/**
 * The Director section: the plan an episode is shot from, and the previs studio.
 *
 * FR-060's acceptance is what this section is: "从一个 Shot 可打开导演预演并带入上下文" and
 * "保存后可在 Shot 中看到摄像机参数和预览图". The studio is the same MONOFORM build the free
 * canvas embeds — `web/public/monoform/` — and the bridge between them is
 * `services/desktop/monoform-bridge.ts` rather than anything this file parses.
 *
 * THREE THINGS IT DOES NOT GUESS:
 *
 *  - An approval names the VERSION. Moving a stage and approving an artifact are two acts,
 *    and the core enforces the difference: a stage that passed with no approved version
 *    would leave the storyboard stage with nothing to build from.
 *  - The workflow run is ENSURED, not assumed. `stage_runs.workflow_run_id` references
 *    `workflow_runs(id)`, so a stage started against an id from another table fails on a
 *    foreign key — which is the defect WP-08's review found in the Script section.
 *  - The camera write-back goes to the ROW the shot names, through the same update command
 *    the table uses, so one code path owns a row's camera whether it came from a form or
 *    from the studio.
 */

export type DirectorSectionProps = {
    projectId: string;
    episodes: desktop.EpisodeDTO[];
    activeEpisodeId: string;
    onSelectEpisode: (episodeId: string) => void;
    onChanged: () => void;
};

export function DirectorSection({ projectId, episodes, activeEpisodeId, onSelectEpisode, onChanged }: DirectorSectionProps) {
    const { t } = useTranslation();
    const { message } = App.useApp();
    const [plans, setPlans] = useState<desktop.DirectorPlanVersionDTO[]>([]);
    const [items, setItems] = useState<desktop.StoryboardItemDTO[]>([]);
    const [loading, setLoading] = useState(false);
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");
    const [studioShot, setStudioShot] = useState<desktop.StoryboardItemDTO | null>(null);

    const activeEpisode = useMemo(
        () => episodes.find((episode) => episode.id === activeEpisodeId) || null,
        [episodes, activeEpisodeId],
    );

    const reload = useCallback(async () => {
        if (!activeEpisodeId) {
            setPlans([]);
            setItems([]);
            return;
        }
        setLoading(true);
        setError("");
        try {
            const versions = await listDirectorPlanVersions(activeEpisodeId);
            setPlans(versions);
            // The shots this episode HAS, which is what the studio can be opened from. The
            // rows come from the episode's storyboard when one exists; a plan with no board
            // yet simply has no shots to open, and the section says so rather than showing
            // an empty picker.
            const storyboard = await listStoryboardVersions(activeEpisodeId).catch(() => []);
            if (storyboard.length > 0) {
                setItems(await listStoryboardItems(storyboard[0].id));
            } else {
                setItems([]);
            }
        } catch (failure) {
            setError(String(failure));
        } finally {
            setLoading(false);
        }
    }, [activeEpisodeId]);

    useEffect(() => {
        void reload();
    }, [reload]);

    const runPlanStage = useCallback(async () => {
        if (!activeEpisode || busy) return;
        setBusy(true);
        try {
            // The run is ENSURED first: a stage run references a workflow run, and passing
            // an episode id where a run id belongs is the foreign-key failure WP-08's review
            // found. The run is per episode, so this finds the one this episode already has.
            const runs = await listWorkflowRuns(projectId);
            const run = runs.find((candidate) => candidate.episodeId === activeEpisode.id)
                || (await createWorkflowRun({
                    projectId,
                    episodeId: activeEpisode.id,
                    workflowType: "episode_production",
                } as never));
            const scriptVersionId = plans.length > 0 ? plans[0].scriptVersionId : "";
            await runScriptStage({
                workflowRunId: run.id,
                stage: "director_plan",
                projectId,
                episodeId: activeEpisode.id,
                task: t("studio.shell.stageHint.director_plan"),
                scriptVersionId,
            } as never);
            message.success(t("studio.shell.stageStarted"));
            await reload();
            onChanged();
        } catch (failure) {
            message.error(String(failure));
        } finally {
            setBusy(false);
        }
    }, [activeEpisode, busy, projectId, plans, reload, onChanged, t, message]);

    const approve = useCallback(
        async (versionId: string) => {
            try {
                await approveDirectorPlanVersion({ versionId });
                message.success(t("studio.shell.approved"));
                await reload();
                onChanged();
            } catch (failure) {
                message.error(String(failure));
            }
        },
        [message, reload, onChanged, t],
    );

    const reportCamera = useCallback(
        async (update: {
            shotId: string;
            camera: {
                position: number[];
                rotation: number[];
                focalLength: number;
                aspectRatio: string;
                movement?: string;
                notes?: string;
            };
            // THE THUMBNAIL WAS BEING DROPPED HERE, and this parameter is the fix: the bridge carried
            // it, the panel forwarded it, and this handler's type named the shot and the camera and
            // nothing else — so FR-060's 「保存后可在 Shot 中看到摄像机参数和预览图」 was half-built
            // with no component even mentioning a thumbnail.
            thumbnail?: Blob;
        }) => {
            // THE CAMERA GOES INTO THE PLAN'S OVERRIDES DOCUMENT, which is where section 9.1
            // puts a per-shot override. It does NOT go through the item update: the item has
            // no camera column, and borrowing one of its fields would store a lens length as
            // a camera angle — a value a reader would then treat as the shot's angle.
            const plan = plans.find((candidate) => candidate.status === "approved") || plans[0];
            if (!plan) {
                message.warning(t("studio.director.noPlanForCamera"));
                return;
            }
            let document: Record<string, unknown> = {};
            try {
                if (plan.shotOverridesJson) {
                    const parsed = JSON.parse(plan.shotOverridesJson);
                    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
                        document = parsed as Record<string, unknown>;
                    }
                }
            } catch {
                // A stored document that is not JSON is a corrupt row rather than a user's
                // intent. It is REPLACED rather than merged into, because merging into a
                // document nobody can read would preserve the corruption.
                document = {};
            }
            // THE SNAPSHOT IS STORED BEFORE THE DOCUMENT IS WRITTEN, so a failure leaves the shot's
            // overrides untouched — 「失败时不影响主项目数据」 stated as an ordering rather than as a
            // promise. The version the snapshot attaches to is the shot's item, which is the asset
            // aggregate's own idea of "what files does this row have".
            let snapshot: { fileHash: string; storageKey: string; bytes: number } | null = null;
            if (update.thumbnail && update.thumbnail.size > 0 && isMonoformSnapshotAvailable()) {
                try {
                    snapshot = await storePrevisSnapshot(update.shotId, update.thumbnail);
                } catch (failure) {
                    // Reported, not swallowed, and the camera still saves: the parameters are what the
                    // shot is BUILT from, and losing them because a preview image would not store
                    // would be the wrong trade. The message names which half failed.
                    message.warning(
                        t("studio.director.snapshotFailed", {
                            reason: failure instanceof Error ? failure.message : String(failure),
                        }),
                    );
                }
            }
            document[update.shotId] = {
                camera: {
                    position: update.camera.position,
                    rotation: update.camera.rotation,
                    focalLength: update.camera.focalLength,
                    aspectRatio: update.camera.aspectRatio,
                    // The two optional fields ARCHITECTURE §17 names are carried when the studio sends
                    // them, and OMITTED rather than written as empty strings when it does not — an
                    // absent movement and a movement of "" are different facts about the shot.
                    ...(update.camera.movement ? { movement: update.camera.movement } : {}),
                    ...(update.camera.notes ? { notes: update.camera.notes } : {}),
                },
                // The snapshot travels with the camera because it is THAT framing's picture: a
                // preview stored without the parameters it was composed with is a picture of nothing
                // in particular.
                ...(snapshot ? { snapshot: { ...snapshot, savedAt: new Date().toISOString() } } : {}),
                savedAt: new Date().toISOString(),
            };
            try {
                await setShotOverrides({ versionId: plan.id, overridesJson: JSON.stringify(document) });
                message.success(t("studio.director.cameraSaved"));
                await reload();
            } catch (failure) {
                message.error(String(failure));
            }
        },
        [plans, message, reload, t],
    );

    const columns: ColumnsType<desktop.DirectorPlanVersionDTO> = [
        {
            title: t("studio.shell.version"),
            dataIndex: "versionNumber",
            width: 90,
            render: (value: number) => `v${value}`,
        },
        {
            title: t("studio.shell.status"),
            dataIndex: "status",
            width: 130,
            render: (value: string) =>
                value === "approved" ? <Tag color="green" icon={<CheckCircle2 className="size-3" />}>{t("studio.director.approved")}</Tag> : <Tag>{value}</Tag>,
        },
        {
            title: t("studio.director.field.cameraLanguage"),
            dataIndex: "cameraLanguage",
            ellipsis: true,
        },
        {
            title: t("studio.director.field.continuityRules"),
            dataIndex: "continuityRules",
            ellipsis: true,
        },
        {
            title: "",
            key: "actions",
            width: 160,
            render: (_: unknown, record: desktop.DirectorPlanVersionDTO) =>
                record.status === "approved" ? (
                    <Tooltip title={t("studio.director.approvedHint")}>
                        <CheckCircle2 className="size-4 text-green-600" />
                    </Tooltip>
                ) : (
                    <Popconfirm title={t("studio.director.approve")} onConfirm={() => void approve(record.id)}>
                        <Button size="small" type="primary" ghost>
                            {t("studio.director.approve")}
                        </Button>
                    </Popconfirm>
                ),
        },
    ];

    if (episodes.length === 0) {
        return <Empty description={t("studio.director.selectEpisode")} />;
    }

    return (
        <Space direction="vertical" size="middle" className="w-full">
            <Space wrap>
                <Select
                    className="min-w-56"
                    value={activeEpisodeId || undefined}
                    placeholder={t("studio.director.selectEpisode")}
                    onChange={onSelectEpisode}
                    options={episodes.map((episode) => ({
                        value: episode.id,
                        label: `S${episode.seasonNumber}E${episode.episodeNumber} · ${episode.title}`,
                    }))}
                />
                <Button type="primary" icon={<Play className="size-4" />} loading={busy} onClick={() => void runPlanStage()}>
                    {t("studio.shell.stage.director_plan")}
                </Button>
                <Select
                    className="min-w-56"
                    allowClear
                    value={studioShot?.id}
                    placeholder={t("studio.director.openStudio")}
                    disabled={items.length === 0}
                    onChange={(value) => setStudioShot(items.find((item) => item.id === value) || null)}
                    options={items.map((item) => ({
                        value: item.id,
                        label: `#${item.ordinal} ${item.shotSize || ""} ${item.visualDescription?.slice(0, 24) || ""}`.trim(),
                    }))}
                />
            </Space>
            {error !== "" ? <Alert type="error" showIcon message={error} /> : null}
            {plans.length === 0 && !loading ? (
                <Alert type="info" showIcon message={t("studio.director.noPlan")} />
            ) : (
                <Table
                    size="small"
                    rowKey="id"
                    loading={loading}
                    columns={columns}
                    dataSource={plans}
                    pagination={false}
                    title={() => <Typography.Text strong>{t("studio.director.planVersions")}</Typography.Text>}
                />
            )}
            {studioShot ? (
                <Typography.Text type="secondary">
                    <Box className="mr-1 inline size-3" />
                    {t("studio.director.studioHint")}
                </Typography.Text>
            ) : null}
            <DirectorPanel
                nodeId={studioShot?.id || activeEpisodeId}
                open={studioShot !== null}
                onClose={() => setStudioShot(null)}
                onExport={() => {
                    // The previs studio's export is a canvas-node path owned by the free
                    // canvas. From the drama side a frame is saved to the disk by the studio
                    // itself, and this handler exists because the panel requires one; it does
                    // not silently invent a node.
                    message.info(t("studio.director.exportFromCanvas"));
                }}
                shot={
                    studioShot
                        ? {
                              shotId: studioShot.id,
                              shotNumber: String(studioShot.ordinal),
                              shotSize: studioShot.shotSize || "",
                              cameraAngle: studioShot.cameraAngle || "",
                              cameraMovement: studioShot.cameraMovement || "",
                              durationSeconds: studioShot.durationSeconds || 0,
                              visualDescription: studioShot.visualDescription || "",
                              firstFrameDescription: studioShot.firstFrameDescription || "",
                              lastFrameDescription: studioShot.lastFrameDescription || "",
                              videoMotionDescription: studioShot.videoMotionDescription || "",
                          }
                        : null
                }
                onShotUpdated={(update) => void reportCamera(update)}
            />
        </Space>
    );
}
