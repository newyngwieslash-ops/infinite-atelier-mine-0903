import { useCallback, useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { Alert, Button, Spin } from "antd";
import { ArrowLeft, ExternalLink } from "lucide-react";
import { useTranslation } from "react-i18next";

import { StudioSectionNav } from "@/components/studio/studio-section-nav";
import { StudioEmptySection } from "@/components/studio/studio-empty-section";
import { AssetsSection, OverviewSection, QualitySection, ScriptSection, SourceSection, StoryboardCanvasSection, StoryGraphSection, getProjectName, getProjectSettings, loadProjectRules } from "@/pages/studio/sections";
import { isDramaBindingsAvailable, listAssets, listEpisodes, listOpenStaleMarks, listSourceDocuments, listWorkflowRuns } from "@/services/desktop/drama";
import { STUDIO_SECTIONS, useStudioStore, type StudioSection } from "@/stores/use-studio-store";
import type { desktop } from "@/wailsjs/go/models";

/**
 * StudioProjectPage is the drama studio's shell for one project.
 *
 * The shell owns the project's shared reads: it loads episodes, source
 * documents, workflow runs, open staleness marks and the asset buckets once per
 * project, puts them in the projection fields `use-studio-store.ts` declares, and
 * hands each section the slice it shows. A section therefore never issues the
 * same query the shell already answered, and switching sections does not refetch.
 *
 * Three behaviours the shell is responsible for:
 *
 *  1. `reset()` runs when the project id changes, so rows read for one project
 *     are never painted under another project's heading while the new query is
 *     in flight.
 *  2. A failed load is shown as an error, not as an empty state: the store's
 *     `loadError` distinguishes "could not read" from "nothing there".
 *  3. The first load shows a spinner. Before it finishes, no count and no list is
 *     rendered, because a zero rendered before the answer arrives is a claim this
 *     interface cannot yet make.
 */
export default function StudioProjectPage() {
    const { t } = useTranslation();
    const { id: projectId = "" } = useParams<{ id: string }>();

    // Store values are read with selectors so a section change does not re-render
    // the shell, and `getState()` is used inside effects where subscribing would
    // re-trigger the effect that wrote the value.
    const section = useStudioStore((state) => state.section);
    const episodes = useStudioStore((state) => state.episodes);
    const sourceDocuments = useStudioStore((state) => state.sourceDocuments);
    const staleMarks = useStudioStore((state) => state.staleMarks);
    const workflowRuns = useStudioStore((state) => state.workflowRuns);
    const assets = useStudioStore((state) => state.assets);
    const loadError = useStudioStore((state) => state.loadError);
    const activeEpisodeId = useStudioStore((state) => state.activeEpisodeId);

    const [project, setProject] = useState<desktop.ProjectDTO | null>(null);
    const [settings, setSettings] = useState<desktop.ProjectSettingsDTO | null>(null);
    const [ruleCount, setRuleCount] = useState<number | null>(null);
    const [loading, setLoading] = useState(true);
    /** True once the shell has asked the core, so an empty list is not rendered early. */
    const [loaded, setLoaded] = useState(false);

    const bindingsAvailable = isDramaBindingsAvailable();

    /**
     * load reads everything the shell owns for one project.
     *
     * The queries are independent and a failure in one must not hide the others,
     * so each is awaited separately and a failure is recorded in `loadError`
     * rather than aborting the pass: a user can still see the episodes when the
     * staleness query is the one that failed.
     *
     * `isCancelled` is consulted before every write, because a read that resolves
     * after the project changed or the page unmounted must not put its rows in
     * the store.
     */
    const load = useCallback(
        async (isCancelled: () => boolean) => {
            setLoading(true);
            setLoaded(false);
            useStudioStore.getState().setLoadError("");
            try {
                const [record, projectSettings, rules, episodeRows, documents, runs, marks, characters, locations, props] = await Promise.all([
                    getProjectName(projectId),
                    getProjectSettings(projectId),
                    loadProjectRules(projectId),
                    listEpisodes(projectId),
                    listSourceDocuments(projectId),
                    listWorkflowRuns(projectId),
                    listOpenStaleMarks(projectId),
                    listAssets({ projectId, types: ["character"] }),
                    listAssets({ projectId, types: ["location"] }),
                    listAssets({ projectId, types: ["prop"] }),
                ]);
                if (isCancelled()) return;
                // The store is written through getState() because this callback
                // must not depend on the store's own values; subscribing here
                // would make the effect that calls `load` re-run on every write.
                const current = useStudioStore.getState();
                current.setEpisodes(episodeRows);
                current.setSourceDocuments(documents);
                current.setWorkflowRuns(runs);
                current.setStaleMarks(marks);
                current.setAssets([...characters, ...locations, ...props]);
                setProject(record);
                setSettings(projectSettings);
                setRuleCount(rules.length);
            } catch (error) {
                // A failed read is an error, never an empty state.
                if (!isCancelled()) useStudioStore.getState().setLoadError(error instanceof Error ? error.message : t("studio.shell.loadFailed"));
            } finally {
                if (!isCancelled()) {
                    setLoading(false);
                    setLoaded(true);
                }
            }
        },
        [projectId, t],
    );

    // One project's rows must never appear under another's heading, so the
    // projection is cleared before the new project's query starts.
    useEffect(() => {
        let cancelled = false;
        useStudioStore.getState().reset();
        setProject(null);
        setSettings(null);
        setRuleCount(null);
        if (!bindingsAvailable) {
            // A browser session has no core, so there is no read to attempt and no
            // failure to report. Marking the load finished lets the shell render,
            // and the notice above explains why the data sections are empty.
            setLoading(false);
            setLoaded(true);
            return;
        }
        void load(() => cancelled);
        return () => {
            cancelled = true;
        };
    }, [bindingsAvailable, load]);

    // The asset list is one query per type, so the buckets are re-read whenever a
    // section command added one.
    const reloadAssets = useCallback(async () => {
        try {
            const [characters, locations, props] = await Promise.all([
                listAssets({ projectId, types: ["character"] }),
                listAssets({ projectId, types: ["location"] }),
                listAssets({ projectId, types: ["prop"] }),
            ]);
            useStudioStore.getState().setAssets([...characters, ...locations, ...props]);
        } catch (error) {
            useStudioStore.getState().setLoadError(error instanceof Error ? error.message : t("studio.shell.loadFailed"));
        }
    }, [projectId, t]);

    const reloadDramaData = useCallback(async () => {
        try {
            const [episodeRows, documents, runs, marks] = await Promise.all([listEpisodes(projectId), listSourceDocuments(projectId), listWorkflowRuns(projectId), listOpenStaleMarks(projectId)]);
            const current = useStudioStore.getState();
            current.setEpisodes(episodeRows);
            current.setSourceDocuments(documents);
            current.setWorkflowRuns(runs);
            current.setStaleMarks(marks);
        } catch (error) {
            useStudioStore.getState().setLoadError(error instanceof Error ? error.message : t("studio.shell.loadFailed"));
        }
    }, [projectId, t]);

    const spec = STUDIO_SECTIONS.find((candidate) => candidate.id === section);
    const activeSection: StudioSection = spec?.id ?? "overview";

    return (
        <main className="h-full overflow-auto bg-background text-stone-950 dark:text-stone-100">
            <div className="mx-auto flex w-full max-w-6xl flex-col gap-6 px-6 py-10">
                <header className="flex flex-wrap items-end justify-between gap-4 border-b border-stone-200 pb-6 dark:border-stone-800">
                    <div className="min-w-0">
                        <p className="text-xs text-stone-500">{t("studio.typeLabel")}</p>
                        <h1 className="mt-3 truncate text-3xl font-semibold">{project?.name ?? t("studio.projectFallback")}</h1>
                    </div>
                    <div className="flex items-center gap-2">
                        <Link to="/studio">
                            <Button icon={<ArrowLeft className="size-4" />} data-testid="studio-back-to-list">
                                {t("studio.shell.backToList")}
                            </Button>
                        </Link>
                        <Link to={`/canvas/${projectId}`}>
                            <Button icon={<ExternalLink className="size-4" />} data-testid="studio-open-canvas">
                                {t("studio.shell.openCanvas")}
                            </Button>
                        </Link>
                    </div>
                </header>

                {/* Without the core there is no project row to read, so the name
                    stays generic. The notice below explains why nothing loads, and
                    the navigation stays usable because the sections that are not
                    built yet state that fact rather than depending on a query. */}
                {!bindingsAvailable ? (
                    <Alert type="info" showIcon message={t("studio.desktopOnly.title")} description={t("studio.desktopOnly.body")} data-testid="studio-desktop-only" />
                ) : null}

                {/* The read failure is rendered inside the section body below, so
                    it replaces the data it failed to read rather than sitting
                    above an empty table. */}

                <div className="flex flex-col gap-8 lg:flex-row">
                    <aside className="w-full shrink-0 lg:w-56">
                        <StudioSectionNav active={activeSection} onSelect={(next) => useStudioStore.getState().setSection(next)} />
                    </aside>

                    <section className="min-w-0 flex-1" data-active-section={activeSection}>
                        {!loaded ? (
                            <div className="flex min-h-[280px] items-center justify-center border-y border-stone-200 text-sm text-stone-500 dark:border-stone-800" data-testid="studio-section-loading">
                                <Spin className="mr-3" />
                                {t("studio.shell.loading")}
                            </div>
                        ) : (
                            <SectionBody
                                section={activeSection}
                                projectId={projectId}
                                bindingsAvailable={bindingsAvailable}
                                loadError={loadError}
                                settings={settings}
                                ruleCount={ruleCount}
                                episodes={episodes}
                                sourceDocuments={sourceDocuments}
                                staleMarks={staleMarks}
                                workflowRuns={workflowRuns}
                                assets={assets}
                                activeEpisodeId={activeEpisodeId}
                                onDramaChanged={() => void reloadDramaData()}
                                onAssetsChanged={() => void reloadAssets()}
                            />
                        )}
                    </section>
                </div>
            </div>
        </main>
    );
}

type SectionBodyProps = {
    section: StudioSection;
    projectId: string;
    bindingsAvailable: boolean;
    /** The last read failure, already human-readable, or empty. */
    loadError: string;
    settings: desktop.ProjectSettingsDTO | null;
    ruleCount: number | null;
    episodes: desktop.EpisodeDTO[];
    sourceDocuments: desktop.SourceDocumentDTO[];
    staleMarks: desktop.StaleMarkDTO[];
    workflowRuns: desktop.WorkflowRunDTO[];
    assets: desktop.AssetDTO[];
    activeEpisodeId: string;
    onDramaChanged: () => void;
    onAssetsChanged: () => void;
};

/**
 * SectionBody renders the active section.
 *
 * Three things it will not do:
 *
 *  - It never renders a data section as empty when the data was not read. In a
 *    browser session every drama query answers with an empty list, so a table
 *    here would say "no episodes" about a project the interface never asked
 *    about. A section that needs the core states that instead.
 *  - It never follows a failed read with an empty list. A section whose data
 *    could not be read shows the failure, because "could not read" and "nothing
 *    there" are different situations and only one of them is true.
 *  - It never renders something plausible for a section the studio does not back
 *    with commands. Those render `StudioEmptySection` with the owning package,
 *    taken from the same `STUDIO_SECTIONS` entry the navigation shows.
 *
 * A section that needs no core data — every unbuilt one — renders regardless of
 * either condition, because a failed query for other sections says nothing about
 * whether its content exists.
 */
function SectionBody(props: SectionBodyProps) {
    const { t } = useTranslation();
    const { section, projectId, bindingsAvailable, loadError } = props;
    const spec = STUDIO_SECTIONS.find((candidate) => candidate.id === section);

    if (!spec) {
        // Reached only if STUDIO_SECTIONS loses an id the store can still hold.
        // Naming the section is more useful than rendering nothing.
        return <Alert type="warning" showIcon message={section} description={t("studio.shell.sectionUnavailable")} />;
    }
    if (!spec.available) {
        return <StudioEmptySection titleKey={spec.titleKey} descriptionKey={spec.descriptionKey} workPackage={spec.workPackage} />;
    }
    if (!bindingsAvailable) {
        return <Alert type="info" showIcon message={t("studio.desktopOnly.title")} description={t("studio.shell.noCore")} data-testid="studio-section-no-core" />;
    }
    if (loadError !== "") {
        return <Alert type="error" showIcon message={t("studio.shell.loadFailed")} description={loadError} data-testid="studio-section-load-error" />;
    }

    switch (section) {
        case "overview":
            return (
                <OverviewSection
                    settings={props.settings}
                    episodeCount={props.episodes.length}
                    sourceDocumentCount={props.sourceDocuments.length}
                    ruleCount={props.ruleCount}
                    openStaleMarkCount={props.staleMarks.length}
                />
            );
        case "source":
            return <SourceSection projectId={projectId} documents={props.sourceDocuments} onChanged={props.onDramaChanged} />;
        case "story-graph":
            return <StoryGraphSection projectId={projectId} />;
        case "script":
            return (
                <ScriptSection
                    projectId={projectId}
                    episodes={props.episodes}
                    activeEpisodeId={props.activeEpisodeId}
                    onSelectEpisode={(episodeId) => useStudioStore.getState().setActiveEpisode(episodeId)}
                    onChanged={props.onDramaChanged}
                />
            );
        case "characters":
        case "locations":
        case "props":
            return (
                <AssetsSection
                    projectId={projectId}
                    type={assetTypeForSection(section)}
                    assets={props.assets.filter((asset) => asset.type === assetTypeForSection(section))}
                    onChanged={props.onAssetsChanged}
                />
            );
        case "storyboard-canvas":
            return <StoryboardCanvasSection projectId={projectId} />;
        case "quality":
            return <QualitySection projectId={projectId} runs={props.workflowRuns} marks={props.staleMarks} onChanged={props.onDramaChanged} />;
        default:
            return <StudioEmptySection titleKey={spec.titleKey} descriptionKey={spec.descriptionKey} workPackage={spec.workPackage} />;
    }
}

/** assetTypeForSection maps the three asset sections to the asset types they list. */
function assetTypeForSection(section: StudioSection): string {
    if (section === "characters") return "character";
    if (section === "locations") return "location";
    return "prop";
}
