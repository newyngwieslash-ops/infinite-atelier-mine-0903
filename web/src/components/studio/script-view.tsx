import { useCallback, useEffect, useMemo, useState } from "react";
import { Alert, App, Button, Empty, Modal, Popconfirm, Segmented, Select, Space, Table, Tag, Tooltip } from "antd";
import type { ColumnsType } from "antd/es/table";
import { GitCompareArrows, Lock, Play, ShieldCheck, Unlock } from "lucide-react";
import { useTranslation } from "react-i18next";

import {
    applyScriptGate,
    approveAdaptationStrategyVersion,
    approveStorySkeletonVersion,
    createAdaptationStrategyVersion,
    createEpisode,
    createStorySkeletonVersion,
    createWorkflowRun,
    diffVersions,
    ensureScript,
    getScriptStructure,
    listAdaptationStrategyVersions,
    listScriptFieldLocks,
    listStorySkeletonVersions,
    listWorkflowRuns,
    lockScriptField,
    projectScriptVersion,
    runScriptStage,
    runScriptSupervision,
    startScriptRevision,
    unlockScriptField,
} from "@/services/desktop/drama";
import type { desktop } from "@/wailsjs/go/models";
// The generated classes as VALUES, because a Wails DTO carries a convertValues method and cannot be
// built from an object literal.
import { desktop as desktopModels } from "@/wailsjs/go/models";

/**
 * The Script section: the three stages of the drama pipeline over one episode.
 *
 * It is a separate component file rather than a function inside sections.tsx,
 * for the reason AGENTS section 9.1 gives about the canvas: one section growing
 * into a second file's worth of behaviour is a section that should have been its
 * own file. The shell passes the episode list down; everything else is read here.
 *
 * THREE THINGS IT REFUSES TO GUESS:
 *
 *  - An approval names the VERSION it approves. Moving a stage and approving an
 *    artifact are two acts, and the core enforces the difference — a stage that
 *    passed with no approved version would leave every later stage with nothing
 *    to read.
 *  - A gate decision that needs a reason says so. A skip without one is refused
 *    by the domain, so the dialog asks before the call rather than after the
 *    refusal.
 *  - A lock is read from the core, never cached locally. The list a user sees is
 *    the list that will be enforced on the next revision.
 */

export type ScriptSectionProps = {
    projectId: string;
    episodes: desktop.EpisodeDTO[];
    activeEpisodeId: string;
    onSelectEpisode: (episodeId: string) => void;
    onChanged: () => void;
};

/** lockedByRef is who the interface attributes a lock to. */
//
// A constant for now: the desktop build has no sign-in, and the core records the identifier on the lock
// row so an audit can tell a person's pin from an agent's. When accounts arrive this is where the
// signed-in user's id comes from.
function lockedByRef(): string {
    return "user-1";
}

/** TranslateFunction is what react-i18next's `t` is, for the helpers below. */
type TranslateFunction = (key: string, options?: Record<string, unknown>) => string;

/** The three stages, in the pipeline's order. */
const STAGES = ["story_skeleton", "adaptation_strategy", "script_generation"] as const;
type Stage = (typeof STAGES)[number];

/** Which version family each stage's artifact belongs to. */
const FAMILY_OF: Record<Stage, string> = {
    story_skeleton: "story_skeleton",
    adaptation_strategy: "adaptation_strategy",
    script_generation: "script",
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
    // The episode's workflow run. A stage belongs to a RUN, and none of the rows this section reads is
    // one: the script's id names a script, and the version ids name versions. The run is created on
    // demand by `ensureRun` and cached here, because a second one would be a second production of the
    // same episode.
    const [workflowRunId, setWorkflowRunId] = useState<string>("");

    /** ensureRun returns the episode's workflow run, creating it when there is none. */
    const ensureRun = useCallback(async (episodeId: string): Promise<string> => {
        if (!episodeId) return "";
        const existing = await listWorkflowRuns(projectId);
        const forEpisode = existing.find(
            (run) => run.episodeId === episodeId && run.workflowType === "episode_production",
        );
        if (forEpisode) {
            setWorkflowRunId(forEpisode.id);
            return forEpisode.id;
        }
        const created = await createWorkflowRun(
            desktopModels.CreateWorkflowRunRequest.createFrom({
                projectId,
                episodeId,
                workflowType: "episode_production",
                actorType: "user",
                actorId: lockedByRef(),
            }),
        );
        setWorkflowRunId(created.id);
        return created.id;
    }, [projectId]);
    // The three families' histories, newest first, as the core returns them.
    const [skeletons, setSkeletons] = useState<desktop.StorySkeletonVersionDTO[]>([]);
    const [strategies, setStrategies] = useState<desktop.AdaptationStrategyVersionDTO[]>([]);
    const [scriptVersions, setScriptVersions] = useState<desktop.ScriptVersionDTO[]>([]);
    const [structure, setStructure] = useState<desktop.ScriptStructureDTO | null>(null);
    const [locks, setLocks] = useState<desktop.FieldLockDTO[]>([]);
    const [diff, setDiff] = useState<desktop.VersionDiffDTO | null>(null);

    const create = async () => {
        setBusy(true);
        try {
            await createEpisode({
                projectId,
                seasonNumber: season,
                episodeNumber: number,
                title: title.trim(),
                targetDurationSeconds: duration ?? 0,
            });
            setTitle("");
            onChanged();
        } catch (error) {
            message.error(error instanceof Error ? error.message : t("studio.script.createFailed"));
        } finally {
            setBusy(false);
        }
    };

    /** refresh reads every family's history for the active episode. */
    const refresh = useCallback(async () => {
        const episodeId = activeEpisodeId;
        if (!episodeId) {
            setSkeletons([]);
            setStrategies([]);
            setScriptVersions([]);
            setScript(null);
            setStructure(null);
            setLocks([]);
            return;
        }
        try {
            // A query with no honest empty answer is what `ensureScript` is: the
            // core creates the script row when the episode has none, so the id it
            // returns is always the core's.
            const record = await ensureScript(episodeId);
            setScript(record);
            setSkeletons(await listStorySkeletonVersions(episodeId));
            setStrategies(await listAdaptationStrategyVersions(episodeId));
            // The run is resolved here rather than in the stage handler, so a caller cannot start a
            // stage before one exists.
            await ensureRun(episodeId);
        } catch (error) {
            message.error(error instanceof Error ? error.message : t("studio.script.loadFailed"));
        }
    }, [activeEpisodeId, ensureRun, message, t]);

    useEffect(() => {
        void refresh();
    }, [refresh]);

    const select = (episode: desktop.EpisodeDTO) => {
        onSelectEpisode(episode.id);
        setStructure(null);
        setLocks([]);
        setDiff(null);
    };

    /** loadStructure reads one script version's content and its locks. */
    const loadStructure = async (versionId: string) => {
        try {
            setStructure(await getScriptStructure(versionId));
            setLocks(await listScriptFieldLocks(versionId));
            setDiff(null);
        } catch (error) {
            message.error(error instanceof Error ? error.message : t("studio.script.loadFailed"));
        }
    };

    /** runStage starts one attempt and reports what it wrote. */
    const runStage = async (stage: Stage) => {
        if (!script) return;
        setBusy(true);
        try {
            const result = await runScriptStage(
                desktopModels.RunScriptStageRequest.createFrom({
                    workflowRunId,
                    stage,
                    projectId,
                    episodeId: activeEpisodeId,
                    // The upstream versions travel in the PROMPT's state layer, which is what the stage's
                    // tools name: a strategy reads the skeleton it adapts, and a generation stage reads
                    // both. Omitting them is not a harmless default — the state renderer omits an empty
                    // field, so the model would be told nothing about what it is writing from.
                    skeletonVersionId: approvedSkeletonId,
                    strategyVersionId: approvedStrategyId,
                    scriptVersionId: stage === "script_generation" ? latestScriptVersionId(scriptVersions) : "",
                    // The events an episode covers come from the approved skeleton's selection, which is
                    // §7.4's link set. A strategy gives each of them a treatment, so an empty list is a
                    // strategy that decided nothing — which the service would accept.
                    selectedEventIds: approvedSkeletonEvents,
                    task: t("studio.script.taskFor", { stage: t(`studio.script.stage.${stage}`) }),
                    providerId: "",
                    modelId: "",
                }),
            );
            message.success(
                t("studio.script.stageRan", {
                    stage: t(`studio.script.stage.${stage}`),
                    run: result.agentRunId || "—",
                }),
            );
            await refresh();
        } catch (error) {
            message.error(error instanceof Error ? error.message : t("studio.script.stageFailed"));
        } finally {
            setBusy(false);
        }
    };

    /** review runs the stage's supervisor and reports whether it passed. */
    const review = async (stageRunId: string, artifactVersionId: string) => {
        setBusy(true);
        try {
            const report = await runScriptSupervision({
                stageRunId,
                projectId,
                episodeId: activeEpisodeId,
                artifactVersionId,
            });
            if (report.passed) {
                message.success(t("studio.script.reviewPassed"));
            } else {
                // A failing report is not an error: it is the review doing its job.
                message.warning(t("studio.script.reviewFailed", { count: report.issues?.length ?? 0 }));
            }
            await refresh();
        } catch (error) {
            message.error(error instanceof Error ? error.message : t("studio.script.reviewErrored"));
        } finally {
            setBusy(false);
        }
    };

    /** approve makes one version the one in force for its family. */
    const approve = async (family: string, versionId: string) => {
        setBusy(true);
        try {
            if (family === FAMILY_OF.story_skeleton) {
                await approveStorySkeletonVersion(versionId);
            } else if (family === FAMILY_OF.adaptation_strategy) {
                await approveAdaptationStrategyVersion(versionId);
            } else {
                // The script family's approval is the user's GATE, so it needs a stage attempt by
                // definition: the gate is what moves a stage. A caller with no attempt has nothing to
                // approve, and the panel above disables the button in that state rather than sending an
                // empty identifier the core would refuse.
                return;
            }
            message.success(t("studio.script.approved"));
            await refresh();
        } catch (error) {
            message.error(error instanceof Error ? error.message : t("studio.script.approveFailed"));
        } finally {
            setBusy(false);
        }
    };

    /** toggleLock pins or releases one field of one version. */
    const toggleLock = async (versionId: string, field: string, locked: boolean) => {
        setBusy(true);
        try {
            // The core's signature is (versionId, field, lockedBy): the LOCKED_BY string is the
            // third argument, and the family is not an argument at all — the service reads it from the
            // version row, which is what keeps the lock table honest across three version families.
            const next = locked
                ? await unlockScriptField(versionId, field)
                : await lockScriptField(versionId, field, lockedByRef());
            setLocks(next);
            message.success(locked ? t("studio.script.unlocked") : t("studio.script.locked"));
        } catch (error) {
            message.error(error instanceof Error ? error.message : t("studio.script.lockFailed"));
        } finally {
            setBusy(false);
        }
    };

    /** compare asks the core for the diff between two versions of one family. */
    const compare = async (family: string, fromId: string, toId: string) => {
        setBusy(true);
        try {
            setDiff(await diffVersions(family, fromId, toId));
        } catch (error) {
            message.error(error instanceof Error ? error.message : t("studio.script.diffFailed"));
        } finally {
            setBusy(false);
        }
    };

    /** project writes the version's scenes onto the project's canvas. */
    const project = async (versionId: string) => {
        setBusy(true);
        try {
            const result = await projectScriptVersion({ projectId, scriptVersionId: versionId });
            message.success(t("studio.script.projected", { count: result.sceneNodeIds?.length ?? 0 }));
        } catch (error) {
            message.error(error instanceof Error ? error.message : t("studio.script.projectFailed"));
        } finally {
            setBusy(false);
        }
    };

    const episodeColumns: ColumnsType<desktop.EpisodeDTO> = [
        { title: t("studio.script.season"), key: "ordinal", width: 120,
          render: (_value, record) => `S${record.seasonNumber}E${record.episodeNumber}` },
        { title: t("studio.script.title"), dataIndex: "title", key: "title", render: (value: string) => value || "—" },
        { title: t("studio.script.statusLabel"), dataIndex: "status", key: "status", width: 140,
          render: (value: string) => <Tag>{t(`studio.episodeStatus.${value}`, value)}</Tag> },
        { title: t("studio.script.targetLabel"), dataIndex: "targetDurationSeconds", key: "duration", width: 140 },
        { title: "", key: "select", width: 100,
          render: (_value, record) => (
              <Button size="small" onClick={() => select(record)} data-testid={`studio-select-episode-${record.id}`}>
                  {t("studio.script.select")}
              </Button>
          ) },
    ];

    const approvedSkeletonId = useMemo(() => approvedIdOf(skeletons), [skeletons]);
    // §7.4's selection, read from the approved skeleton: this is what a strategy gives treatments to, and
    // passing it is what keeps "every event has a decision" true of a stage the interface started.
    const approvedSkeletonEvents = useMemo(
        () => skeletons.find((version) => version.versionId === approvedSkeletonId)?.selectedEventIds ?? [],
        [approvedSkeletonId, skeletons],
    );
    const approvedStrategyId = useMemo(() => approvedIdOf(strategies), [strategies]);
    // A script version's DTO names its identifier `id` while the two upstream families call it
    // `versionId`, which is the schema's own history rather than a choice made here. The adapter keeps
    // one shape for the helper below.
    const approvedScriptId = useMemo(
        () => scriptVersions.find((version) => version.status === "approved")?.id ?? "",
        [scriptVersions],
    );
    const activeScriptVersionId = useMemo(
        () => approvedScriptId || latestScriptVersionId(scriptVersions),
        [approvedScriptId, scriptVersions],
    );
    const durationTarget = episodes.find((episode) => episode.id === activeEpisodeId)?.targetDurationSeconds ?? 0;

    return (
        <div className="space-y-6" data-testid="studio-script">
            <section className="rounded-xl border border-stone-200 p-4 dark:border-stone-800">
                <h3 className="text-sm font-medium">{t("studio.script.newEpisode")}</h3>
                <div className="mt-3 flex flex-wrap items-end gap-3">
                    <label className="flex flex-col">
                        <span className="mb-1 text-sm">{t("studio.script.season")}</span>
                        <input type="number" min={1} className="w-20 rounded border border-stone-300 px-2 py-1 dark:border-stone-700 dark:bg-stone-900"
                               value={season} onChange={(event) => setSeason(Number(event.target.value))} />
                    </label>
                    <label className="flex flex-col">
                        <span className="mb-1 text-sm">{t("studio.script.episodeNumber")}</span>
                        <input type="number" min={1} className="w-20 rounded border border-stone-300 px-2 py-1 dark:border-stone-700 dark:bg-stone-900"
                               value={number} onChange={(event) => setNumber(Number(event.target.value))} />
                    </label>
                    <label className="flex flex-col">
                        <span className="mb-1 text-sm">{t("studio.script.title")}</span>
                        <input className="w-48 rounded border border-stone-300 px-2 py-1 dark:border-stone-700 dark:bg-stone-900"
                               value={title} onChange={(event) => setTitle(event.target.value)} />
                    </label>
                    <label className="flex flex-col">
                        <span className="mb-1 text-sm">{t("studio.script.targetDuration")}</span>
                        <input type="number" min={0} className="w-28 rounded border border-stone-300 px-2 py-1 dark:border-stone-700 dark:bg-stone-900"
                               value={duration ?? ""} onChange={(event) => setDuration(event.target.value === "" ? null : Number(event.target.value))} />
                    </label>
                    <Button type="primary" loading={busy} onClick={create} data-testid="studio-create-episode">
                        {t("studio.script.create")}
                    </Button>
                </div>
            </section>

            {episodes.length === 0 ? (
                <Empty description={t("studio.script.empty")} />
            ) : (
                <Table<desktop.EpisodeDTO>
                    size="small"
                    rowKey="id"
                    pagination={false}
                    columns={episodeColumns}
                    dataSource={episodes}
                    rowClassName={(record) => (record.id === activeEpisodeId ? "bg-stone-50 dark:bg-stone-900" : "")}
                    data-testid="studio-episode-table"
                />
            )}

            {script ? (
                <section className="rounded-xl border border-stone-200 p-4 dark:border-stone-800" data-testid="studio-script-detail">
                    <header className="flex flex-wrap items-center justify-between gap-3">
                        <div>
                            <h3 className="text-sm font-medium">{t("studio.script.pipeline")}</h3>
                            <p className="mt-1 text-xs text-stone-500">
                                {t("studio.script.scriptId")}: <code>{script.id}</code>
                            </p>
                        </div>
                        <Space>
                            {STAGES.map((stage) => (
                                <Tooltip key={stage} title={t(`studio.script.stageHint.${stage}`)}>
                                    <Button
                                        size="small"
                                        icon={<Play size={14} />}
                                        loading={busy}
                                        onClick={() => runStage(stage)}
                                        disabled={!stageReady(stage, approvedSkeletonId, approvedStrategyId)}
                                        data-testid={`studio-run-stage-${stage}`}
                                    >
                                        {t(`studio.script.stage.${stage}`)}
                                    </Button>
                                </Tooltip>
                            ))}
                        </Space>
                    </header>

                    {!stageReady("script_generation", approvedSkeletonId, approvedStrategyId) ? (
                        <Alert
                            className="mt-3"
                            type="info"
                            showIcon
                            message={t("studio.script.gateBlocked")}
                            description={t("studio.script.gateBlockedBody")}
                        />
                    ) : null}

                    <div className="mt-4 grid gap-4 lg:grid-cols-2">
                        <VersionTable
                            title={t("studio.script.skeletons")}
                            rows={skeletons.map((version) => ({
                                id: version.versionId,
                                number: version.versionNumber,
                                status: version.status,
                                detail: version.endingHook || version.openingHook || "—",
                                createdBy: version.createdByType,
                            }))}
                            approvedId={approvedSkeletonId}
                            onApprove={(id) => approve(FAMILY_OF.story_skeleton, id)}
                            busy={busy}
                            testId="studio-skeleton-versions"
                            t={t}
                        />
                        <VersionTable
                            title={t("studio.script.strategies")}
                            rows={strategies.map((version) => ({
                                id: version.versionId,
                                number: version.versionNumber,
                                status: version.status,
                                detail: version.strategySummary || "—",
                                createdBy: version.createdByType,
                            }))}
                            approvedId={approvedStrategyId}
                            onApprove={(id) => approve(FAMILY_OF.adaptation_strategy, id)}
                            busy={busy}
                            testId="studio-strategy-versions"
                            t={t}
                        />
                    </div>

                    <ScriptVersionPanel
                        versions={scriptVersions}
                        approvedId={approvedScriptId}
                        activeVersionId={activeScriptVersionId}
                        structure={structure}
                        locks={locks}
                        durationTarget={durationTarget}
                        diff={diff}
                        busy={busy}
                        onLoad={loadStructure}
                        onApprove={(id) => approve(FAMILY_OF.script_generation, id)}
                        onToggleLock={toggleLock}
                        onCompare={(fromId, toId) => compare(FAMILY_OF.script_generation, fromId, toId)}
                        onProject={project}
                        onVersions={setScriptVersions}
                        t={t}
                    />
                </section>
            ) : (
                <p className="text-sm text-stone-500">{t("studio.script.selectEpisode")}</p>
            )}

            <StageWorkflowPanel
                projectId={projectId}
                episodeId={activeEpisodeId}
                episodeTarget={durationTarget}
                latestSkeletonId={skeletons[0]?.versionId ?? ""}
                latestStrategyId={strategies[0]?.versionId ?? ""}
                latestScriptId={activeScriptVersionId}
                approvedScriptId={approvedScriptId}
                busy={busy}
                onReview={review}
                onChanged={refresh}
                t={t}
            />

            <CreateVersionPanel
                episodeId={activeEpisodeId}
                basedOnSkeletonId={skeletons[0]?.versionId ?? ""}
                basedOnStrategyId={strategies[0]?.versionId ?? ""}
                busy={busy}
                onCreated={async () => {
                    await refresh();
                    message.success(t("studio.script.versionCreated"));
                }}
                onError={(error) =>
                    message.error(error instanceof Error ? error.message : t("studio.script.createFailed"))
                }
                t={t}
            />
        </div>
    );
}

/**
 * approvedIdOf returns the id of the approved row, if there is one.
 *
 * It reads the STATUS rather than a "current version" field, because the status is what the schema's
 * partial unique index enforces: exactly one version per parent may be approved, so the status is the
 * honest answer to "which one is in force".
 */
function approvedIdOf(rows: Array<{ versionId: string; status: string }>): string {
    return rows.find((row) => row.status === "approved")?.versionId ?? "";
}

/** latestScriptVersionId returns the newest version's id, or "" when there is none. */
function latestScriptVersionId(versions: desktop.ScriptVersionDTO[]): string {
    return versions[0]?.id ?? "";
}

/**
 * stageReady reports whether a stage may run.
 *
 * A strategy needs an approved skeleton and a script needs an approved strategy, which is the pipeline's
 * own dependency order. The check exists so a person sees WHY the button is disabled rather than
 * discovering it from a core refusal.
 */
function stageReady(stage: Stage, approvedSkeletonId: string, approvedStrategyId: string): boolean {
    if (stage === "story_skeleton") return true;
    if (stage === "adaptation_strategy") return approvedSkeletonId !== "";
    return approvedSkeletonId !== "" && approvedStrategyId !== "";
}

/** VersionTable renders one family's history. */
function VersionTable({
    title,
    rows,
    approvedId,
    onApprove,
    busy,
    testId,
    t,
}: {
    title: string;
    rows: Array<{ id: string; number: number; status: string; detail: string; createdBy: string }>;
    approvedId: string;
    onApprove: (id: string) => void;
    busy: boolean;
    testId: string;
    t: TranslateFunction;
}) {
    return (
        <div data-testid={testId}>
            <h4 className="text-sm font-medium">{title}</h4>
            {rows.length === 0 ? (
                <p className="mt-1 text-sm text-stone-500">{t("studio.script.noVersions")}</p>
            ) : (
                <ul className="mt-2 space-y-2 text-sm">
                    {rows.map((row) => (
                        <li key={row.id} className="flex items-start justify-between gap-3 rounded border border-stone-200 p-2 dark:border-stone-800">
                            <div className="min-w-0">
                                <div className="flex items-center gap-2">
                                    <span className="text-stone-500">v{row.number}</span>
                                    <Tag color={row.status === "approved" ? "green" : undefined}>
                                        {t(`studio.versionStatus.${row.status}`)}
                                    </Tag>
                                    {row.createdBy ? <span className="text-xs text-stone-400">{row.createdBy}</span> : null}
                                </div>
                                <p className="mt-1 truncate text-stone-600 dark:text-stone-300">{row.detail}</p>
                            </div>
                            {row.id === approvedId ? (
                                <Tag icon={<ShieldCheck size={12} />} color="green">{t("studio.script.inForce")}</Tag>
                            ) : (
                                <Button size="small" loading={busy} onClick={() => onApprove(row.id)}>
                                    {t("studio.script.approve")}
                                </Button>
                            )}
                        </li>
                    ))}
                </ul>
            )}
        </div>
    );
}

/** ScriptVersionPanel renders the script family: history, content, locks, diff and projection. */
function ScriptVersionPanel({
    versions,
    approvedId,
    activeVersionId,
    structure,
    locks,
    durationTarget,
    diff,
    busy,
    onLoad,
    onApprove,
    onToggleLock,
    onCompare,
    onProject,
    onVersions,
    t,
}: {
    versions: desktop.ScriptVersionDTO[];
    approvedId: string;
    activeVersionId: string;
    structure: desktop.ScriptStructureDTO | null;
    locks: desktop.FieldLockDTO[];
    durationTarget: number;
    diff: desktop.VersionDiffDTO | null;
    busy: boolean;
    onLoad: (versionId: string) => void;
    onApprove: (versionId: string) => void;
    onToggleLock: (versionId: string, field: string, locked: boolean) => void;
    onCompare: (fromId: string, toId: string) => void;
    onProject: (versionId: string) => void;
    onVersions: (versions: desktop.ScriptVersionDTO[]) => void;
    t: TranslateFunction;
}) {
    const [selected, setSelected] = useState<string>("");
    useEffect(() => {
        setSelected(activeVersionId);
    }, [activeVersionId]);

    if (versions.length === 0) {
        return (
            <div className="mt-6">
                <h4 className="text-sm font-medium">{t("studio.script.versions")}</h4>
                <p className="mt-1 text-sm text-stone-500">{t("studio.script.noVersion")}</p>
            </div>
        );
    }

    const structureLocked = locks.some((lock) => lock.field === "structure");
    const summed = structure?.estimatedDurationSeconds ?? 0;

    return (
        <div className="mt-6 space-y-4">
            <h4 className="text-sm font-medium">{t("studio.script.versions")}</h4>
            <div className="flex flex-wrap items-center gap-3">
                <Select
                    className="min-w-64"
                    value={selected || undefined}
                    onChange={(value) => {
                        setSelected(value);
                        onLoad(value);
                    }}
                    options={versions.map((version) => ({
                        value: version.id,
                        label: `v${version.versionNumber} · ${t(`studio.versionStatus.${version.status}`)}`,
                    }))}
                    data-testid="studio-script-version-select"
                />
                <Button size="small" loading={busy} onClick={() => onLoad(selected)} disabled={!selected}>
                    {t("studio.script.loadContent")}
                </Button>
                <Button
                    size="small"
                    type="primary"
                    loading={busy}
                    onClick={() => onApprove(selected)}
                    disabled={!selected || selected === approvedId}
                    data-testid="studio-approve-script"
                >
                    {selected === approvedId ? t("studio.script.inForce") : t("studio.script.approve")}
                </Button>
                <Button size="small" icon={<GitCompareArrows size={14} />} loading={busy}
                        onClick={() => {
                            const previous = versions.find((version) => version.id !== selected);
                            if (previous) onCompare(previous.id, selected);
                        }}
                        disabled={versions.length < 2}
                        data-testid="studio-diff-script">
                    {t("studio.script.diff")}
                </Button>
                <Button size="small" loading={busy} onClick={() => onProject(selected)} disabled={!selected}
                        data-testid="studio-project-script">
                    {t("studio.script.project")}
                </Button>
                <Tooltip title={t("studio.script.structureLockHint")}>
                    <Button
                        size="small"
                        icon={structureLocked ? <Lock size={14} /> : <Unlock size={14} />}
                        loading={busy}
                        onClick={() => onToggleLock(selected, "structure", structureLocked)}
                        disabled={!selected}
                        data-testid="studio-lock-structure"
                    >
                        {structureLocked ? t("studio.script.unlockStructure") : t("studio.script.lockStructure")}
                    </Button>
                </Tooltip>
            </div>

            {structure ? (
                <>
                    <div className="text-sm">
                        <span className="text-stone-500">{t("studio.script.duration")}: </span>
                        <span data-testid="studio-script-duration">{summed}</span>
                        {durationTarget > 0 ? (
                            <span className="text-stone-500">
                                {" "}
                                / {t("studio.script.durationTarget", { target: durationTarget })}
                            </span>
                        ) : null}
                    </div>
                    {structure.scenes.length === 0 ? (
                        <p className="text-sm text-stone-500">{t("studio.script.scenesEmpty")}</p>
                    ) : (
                        <ul className="space-y-2 text-sm" data-testid="studio-script-scenes">
                            {structure.scenes.map((scene) => (
                                <li key={scene.sceneId} className="rounded border border-stone-200 p-2 dark:border-stone-800">
                                    <div className="flex items-center gap-2">
                                        <span className="text-stone-500">{scene.ordinal}</span>
                                        <span className="font-medium">{scene.slugline || scene.sceneNumber || "—"}</span>
                                        <span className="text-xs text-stone-400">{scene.estimatedDurationSeconds}s</span>
                                        {scene.isOriginalAdaptation ? (
                                            <Tag color="blue">{t("studio.script.original")}</Tag>
                                        ) : null}
                                    </div>
                                    {scene.summary ? <p className="mt-1 text-stone-600 dark:text-stone-300">{scene.summary}</p> : null}
                                    {scene.dialogueLines?.length ? (
                                        <ul className="mt-1 space-y-1">
                                            {scene.dialogueLines.map((line) => (
                                                <li key={line.lineId} className="flex items-start gap-2 text-xs">
                                                    <Tag>{t(`studio.lineType.${line.type}`)}</Tag>
                                                    <span className="flex-1 whitespace-pre-wrap">{line.text}</span>
                                                    {line.locked ? <Lock size={12} /> : null}
                                                </li>
                                            ))}
                                        </ul>
                                    ) : null}
                                </li>
                            ))}
                        </ul>
                    )}
                </>
            ) : null}

            {diff ? (
                <section className="rounded border border-stone-200 p-3 dark:border-stone-800" data-testid="studio-script-diff">
                    <h5 className="text-sm font-medium">
                        {t("studio.script.diffTitle", { from: diff.fromId, to: diff.toId })}
                    </h5>
                    <p className="mt-1 text-xs text-stone-500">
                        {t("studio.script.diffCounts", {
                            added: diff.added,
                            removed: diff.removed,
                            modified: diff.modified,
                            unchanged: diff.unchanged,
                        })}
                    </p>
                    {diff.items?.length ? (
                        <ul className="mt-2 space-y-1 text-xs">
                            {diff.items.map((item, index) => (
                                <li key={`${item.kind}-${item.ordinal}-${index}`} className="flex items-start gap-2">
                                    <Tag color={item.kind === "added" ? "green" : item.kind === "removed" ? "red" : "orange"}>
                                        {t(`studio.changeKind.${item.kind}`)}
                                    </Tag>
                                    <span className="text-stone-500">#{item.ordinal}</span>
                                    {item.locked ? <Lock size={12} /> : null}
                                    <span className="flex-1">
                                        {(item.fields ?? []).map((field) => (
                                            <span key={field.field} className="mr-2">
                                                {field.field}: {field.before || "—"} → {field.after || "—"}
                                            </span>
                                        ))}
                                    </span>
                                </li>
                            ))}
                        </ul>
                    ) : null}
                </section>
            ) : null}
        </div>
    );
}

/** StageWorkflowPanel renders the runs of the three stages and the user's gate. */
function StageWorkflowPanel({
    projectId,
    episodeId,
    episodeTarget,
    latestSkeletonId,
    latestStrategyId,
    latestScriptId,
    approvedScriptId,
    busy,
    onReview,
    onChanged,
    t,
}: {
    projectId: string;
    episodeId: string;
    episodeTarget: number;
    latestSkeletonId: string;
    latestStrategyId: string;
    latestScriptId: string;
    approvedScriptId: string;
    busy: boolean;
    onReview: (stageRunId: string, artifactVersionId: string) => void;
    onChanged: () => void;
    t: (key: string, options?: Record<string, unknown>) => string;
}) {
    const { message } = App.useApp();
    const [runs, setRuns] = useState<desktop.WorkflowRunDTO[]>([]);
    const [gate, setGate] = useState<{ stageRunId: string; versionId: string; decision: string } | null>(null);
    const [instruction, setInstruction] = useState("");
    const [reason, setReason] = useState("");

    const refresh = useCallback(async () => {
        if (!projectId) {
            setRuns([]);
            return;
        }
        try {
            setRuns(await listWorkflowRuns(projectId));
        } catch {
            // A query with no answer renders as empty, which is this module's rule.
            setRuns([]);
        }
    }, [projectId]);

    useEffect(() => {
        void refresh();
    }, [refresh]);

    const submitGate = async () => {
        if (!gate) return;
        try {
            await applyScriptGate(
                desktopModels.ApplyScriptGateRequest.createFrom({
                    stageRunId: gate.stageRunId,
                    decision: gate.decision,
                    artifactVersionId: gate.versionId,
                    instruction,
                    reason,
                    createdById: "user-1",
                }),
            );
            message.success(t("studio.script.gateApplied", { decision: t(`studio.gateDecision.${gate.decision}`) }));
            setGate(null);
            setInstruction("");
            setReason("");
            await onChanged();
        } catch (error) {
            message.error(error instanceof Error ? error.message : t("studio.script.gateFailed"));
        }
    };

    const latestRun = runs[0];

    return (
        <section className="rounded-xl border border-stone-200 p-4 dark:border-stone-800" data-testid="studio-script-workflow">
            <h3 className="text-sm font-medium">{t("studio.script.workflow")}</h3>
            {!latestRun ? (
                <p className="mt-1 text-sm text-stone-500">{t("studio.script.noRun")}</p>
            ) : (
                <div className="mt-2 space-y-3 text-sm">
                    <p className="text-xs text-stone-500">
                        {t("studio.script.runId")}: <code>{latestRun.id}</code>
                    </p>
                    <Space wrap data-testid="studio-gate-controls">
                        {["approve", "fix", "redo", "manual_edit", "skip"].map((decision) => {
                            const versionId =
                                decision === "approve" || decision === "skip" || decision === "manual_edit"
                                    ? latestScriptId
                                    : "";
                            const disabled =
                                busy ||
                                latestRun.id === "" ||
                                ((decision === "approve" || decision === "skip") && !versionId);
                            return (
                                <Popconfirm
                                    key={decision}
                                    title={t(`studio.gateDecision.${decision}`)}
                                    description={
                                        decision === "skip"
                                            ? t("studio.script.skipNeedsReason")
                                            : t("studio.script.gateHint")
                                    }
                                    okButtonProps={{ disabled: decision === "skip" && reason.trim() === "" }}
                                    onConfirm={() => {
                                        if (decision === "skip" && reason.trim() === "") {
                                            setGate({ stageRunId: latestRun.id, versionId, decision });
                                            return;
                                        }
                                        setGate({ stageRunId: latestRun.id, versionId, decision });
                                    }}
                                >
                                    <Button size="small" disabled={disabled} data-testid={`studio-gate-${decision}`}>
                                        {t(`studio.gateDecision.${decision}`)}
                                    </Button>
                                </Popconfirm>
                            );
                        })}
                        <Button
                            size="small"
                            loading={busy}
                            onClick={() => onReview(latestRun.id, latestScriptId)}
                            disabled={!latestScriptId}
                            data-testid="studio-run-supervision"
                        >
                            {t("studio.script.runSupervision")}
                        </Button>
                        <Button
                            size="small"
                            loading={busy}
                            onClick={async () => {
                                try {
                                    await startScriptRevision(latestRun.id);
                                    message.success(t("studio.script.revisionStarted"));
                                    await onChanged();
                                } catch (error) {
                                    message.error(error instanceof Error ? error.message : t("studio.script.revisionFailed"));
                                }
                            }}
                            data-testid="studio-start-revision"
                        >
                            {t("studio.script.startRevision")}
                        </Button>
                    </Space>
                    <p className="text-xs text-stone-500">
                        {t("studio.script.artifacts", {
                            skeleton: latestSkeletonId ? "✓" : "—",
                            strategy: latestStrategyId ? "✓" : "—",
                            script: latestScriptId ? "✓" : "—",
                        })}
                        {episodeTarget > 0 && approvedScriptId ? ` · ${t("studio.script.targetStated", { target: episodeTarget })}` : ""}
                    </p>
                </div>
            )}

            <Modal
                open={gate !== null}
                title={gate ? t(`studio.gateDecision.${gate.decision}`) : ""}
                onCancel={() => setGate(null)}
                onOk={submitGate}
                okText={t("studio.script.confirmGate")}
                cancelText={t("common.cancel")}
                data-testid="studio-gate-modal"
            >
                <div className="space-y-3">
                    <p className="text-sm">{t("studio.script.gateHint")}</p>
                    <label className="flex flex-col">
                        <span className="mb-1 text-sm">{t("studio.script.instruction")}</span>
                        <input
                            className="rounded border border-stone-300 px-2 py-1 dark:border-stone-700 dark:bg-stone-900"
                            value={instruction}
                            onChange={(event) => setInstruction(event.target.value)}
                            data-testid="studio-gate-instruction"
                        />
                    </label>
                    <label className="flex flex-col">
                        <span className="mb-1 text-sm">{t("studio.script.reason")}</span>
                        <input
                            className="rounded border border-stone-300 px-2 py-1 dark:border-stone-700 dark:bg-stone-900"
                            value={reason}
                            onChange={(event) => setReason(event.target.value)}
                            data-testid="studio-gate-reason"
                        />
                    </label>
                    {episodeId ? null : <Alert type="warning" showIcon message={t("studio.script.noEpisode")} />}
                </div>
            </Modal>
        </section>
    );
}

/** CreateVersionPanel creates a version of any family by hand, which is the user's own authoring path. */
function CreateVersionPanel({
    episodeId,
    basedOnSkeletonId,
    basedOnStrategyId,
    busy,
    onCreated,
    onError,
    t,
}: {
    episodeId: string;
    basedOnSkeletonId: string;
    basedOnStrategyId: string;
    busy: boolean;
    onCreated: () => Promise<void>;
    onError: (error: unknown) => void;
    t: TranslateFunction;
}) {
    const [family, setFamily] = useState<string>("story_skeleton");
    const [hook, setHook] = useState("");

    const create = async () => {
        try {
            if (family === "story_skeleton") {
                await createStorySkeletonVersion(
                    desktopModels.CreateStorySkeletonVersionRequest.createFrom({
                        episodeId,
                        openingHook: hook,
                        estimatedDurationSeconds: 0,
                        selectedEventIds: [],
                    }),
                );
            } else if (family === "adaptation_strategy") {
                await createAdaptationStrategyVersion(
                    desktopModels.CreateAdaptationStrategyVersionRequest.createFrom({
                        episodeId,
                        strategySummary: hook,
                        basedOnVersionId: "",
                        eventLinks: [],
                    }),
                );
            }
            setHook("");
            await onCreated();
        } catch (error) {
            onError(error);
        }
    };

    return (
        <section className="rounded-xl border border-stone-200 p-4 dark:border-stone-800" data-testid="studio-create-version">
            <h3 className="text-sm font-medium">{t("studio.script.createVersion")}</h3>
            <p className="mt-1 text-xs text-stone-500">
                {t("studio.script.createVersionHint", {
                    skeleton: basedOnSkeletonId ? "✓" : "—",
                    strategy: basedOnStrategyId ? "✓" : "—",
                })}
            </p>
            <div className="mt-3 flex flex-wrap items-end gap-3">
                <Segmented
                    value={family}
                    onChange={(value) => setFamily(String(value))}
                    options={[
                        { label: t("studio.script.stage.story_skeleton"), value: "story_skeleton" },
                        { label: t("studio.script.stage.adaptation_strategy"), value: "adaptation_strategy" },
                    ]}
                    data-testid="studio-version-family"
                />
                <input
                    className="w-80 rounded border border-stone-300 px-2 py-1 dark:border-stone-700 dark:bg-stone-900"
                    placeholder={t("studio.script.contentPlaceholder")}
                    value={hook}
                    onChange={(event) => setHook(event.target.value)}
                    data-testid="studio-version-content"
                />
                <Button loading={busy} onClick={create} disabled={!episodeId} data-testid="studio-create-version-submit">
                    {t("studio.script.createVersionSubmit")}
                </Button>
            </div>
        </section>
    );
}
