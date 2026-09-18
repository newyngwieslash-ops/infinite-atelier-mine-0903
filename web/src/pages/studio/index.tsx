import { useCallback, useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { Alert, Button, Empty, Spin, Tag } from "antd";
import { BookOpen, Maximize2, Plus } from "lucide-react";
import { useTranslation } from "react-i18next";

import { DramaCreateWizard } from "@/components/studio/drama-create-wizard";
import { isDramaBindingsAvailable } from "@/services/desktop/drama";
import type { desktop } from "@/wailsjs/go/models";

/**
 * StudioPage lists the drama projects.
 *
 * The list is read from the `ListProjects` binding rather than through
 * `resolveCanvasAdapter()`. The adapter's `CanvasProject` carries what the canvas
 * needs — a title, geometry, timestamps — and drops the project type on the way
 * through (`emptyProject` in canvas-adapter-go.ts builds one from id, name and
 * dates), so filtering the adapter's rows by type is not possible without
 * changing the adapter. This page therefore reads the same binding the adapter
 * reads and keeps the field the adapter discards.
 *
 * The project type is the only filter applied, and it is applied to what the core
 * returned: a project appears here because its row says `projectType === "drama"`,
 * never because this page guessed.
 */

// The binding is imported lazily through a cached module promise, the same
// pattern `drama.ts` uses: a browser build has no Wails runtime, and an eager
// import would fail there.
let projectsModule: Promise<typeof import("@/wailsjs/go/desktop/ProjectsBinding")> | undefined;

async function loadProjectsBinding() {
    projectsModule ??= import("@/wailsjs/go/desktop/ProjectsBinding").catch((error: unknown) => {
        projectsModule = undefined;
        throw error;
    });
    return projectsModule;
}

export default function StudioPage() {
    const { t } = useTranslation();
    const navigate = useNavigate();
    const [projects, setProjects] = useState<desktop.ProjectDTO[]>([]);
    const [loading, setLoading] = useState(true);
    const [loadError, setLoadError] = useState("");
    /** True once a read has finished, so the empty state is not shown early. */
    const [loaded, setLoaded] = useState(false);
    const [wizardOpen, setWizardOpen] = useState(false);

    const bindingsAvailable = isDramaBindingsAvailable();

    const load = useCallback(async () => {
        if (!bindingsAvailable) return;
        setLoading(true);
        setLoadError("");
        try {
            const { ListProjects } = await loadProjectsBinding();
            const listed = await ListProjects({ limit: 1000 });
            setProjects(listed.filter((project) => project.projectType === "drama"));
        } catch (error) {
            // A failed read is reported as a failure. Showing an empty list here
            // would claim the project has no drama projects, which is not known.
            setLoadError(error instanceof Error ? error.message : t("studio.loadFailed"));
            setProjects([]);
        } finally {
            setLoading(false);
            setLoaded(true);
        }
    }, [bindingsAvailable, t]);

    useEffect(() => {
        if (!bindingsAvailable) {
            // No core means no read to attempt; the notice below says why.
            setLoading(false);
            setLoaded(true);
            return;
        }
        void load();
    }, [bindingsAvailable, load]);

    return (
        <main className="h-full overflow-auto bg-background text-stone-950 dark:text-stone-100">
            <div className="mx-auto flex w-full max-w-6xl flex-col gap-8 px-6 py-10">
                <header className="flex flex-wrap items-end justify-between gap-4 border-b border-stone-200 pb-6 dark:border-stone-800">
                    <div>
                        <p className="text-xs text-stone-500">{t("studio.library")}</p>
                        <h1 className="mt-3 text-3xl font-semibold">{t("studio.title")}</h1>
                        <p className="mt-2 max-w-2xl text-sm text-stone-500">{t("studio.subtitle")}</p>
                    </div>
                    <div className="flex items-center gap-2">
                        {/* Without a core the wizard cannot create anything, so the
                            entry is disabled rather than opening a form whose submit
                            would fail. The notice below explains why. */}
                        <Button
                            type="primary"
                            icon={<Plus className="size-4" />}
                            disabled={!bindingsAvailable}
                            data-testid="studio-new-project"
                            onClick={() => setWizardOpen(true)}
                        >
                            {t("studio.newProject")}
                        </Button>
                    </div>
                </header>

                {!bindingsAvailable ? (
                    <Alert type="info" showIcon message={t("studio.desktopOnly.title")} description={t("studio.desktopOnly.body")} data-testid="studio-desktop-only" />
                ) : null}

                {loadError !== "" ? <Alert type="error" showIcon message={t("studio.loadFailed")} description={loadError} data-testid="studio-list-error" /> : null}

                {!loaded ? (
                    <section className="flex min-h-[360px] items-center justify-center border-y border-stone-200 text-sm text-stone-500 dark:border-stone-800" data-testid="studio-list-loading">
                        <Spin className="mr-3" />
                        {t("studio.loading")}
                    </section>
                ) : bindingsAvailable && projects.length > 0 ? (
                    <div className="grid gap-5 sm:grid-cols-2 xl:grid-cols-3" data-testid="studio-project-grid">
                        {projects.map((project) => (
                            <article
                                key={project.id}
                                data-project-id={project.id}
                                data-project-type={project.projectType}
                                className="group flex min-h-44 cursor-pointer flex-col justify-between rounded-2xl bg-[#f1eee8] p-5 transition hover:bg-[#ebe6dc] dark:bg-white/5 dark:hover:bg-white/10"
                                onClick={() => navigate(`/studio/${project.id}`)}
                            >
                                <div className="flex items-start gap-3">
                                    <BookOpen className="mt-1 size-5 shrink-0 text-stone-500" aria-hidden="true" />
                                    <div className="min-w-0">
                                        <h2 className="truncate text-xl font-semibold">{project.name}</h2>
                                        <Tag className="mt-3">{t("studio.typeLabel")}</Tag>
                                    </div>
                                </div>
                                <div className="mt-8 flex items-end justify-between gap-3">
                                    <p className="text-xs text-stone-500">{t("studio.updated", { date: formatWhen(project.updatedAt) })}</p>
                                    <Link to={`/studio/${project.id}`} onClick={(event) => event.stopPropagation()} className="text-xs text-stone-600 underline-offset-2 hover:underline dark:text-stone-300">
                                        {t("studio.open")}
                                    </Link>
                                </div>
                            </article>
                        ))}
                    </div>
                ) : (
                    <section className="flex min-h-[360px] flex-col items-center justify-center border-y border-stone-200 text-center dark:border-stone-800" data-testid="studio-empty">
                        <h2 className="text-xl font-medium">{t("studio.empty.title")}</h2>
                        <p className="mt-3 max-w-xl text-sm text-stone-500">{t("studio.empty.description")}</p>
                        <div className="mt-6 flex items-center gap-2">
                            <Link to="/canvas">
                                <Button icon={<Maximize2 className="size-4" />} data-testid="studio-empty-open-canvas">
                                    {t("studio.empty.openCanvas")}
                                </Button>
                            </Link>
                            <Button type="primary" icon={<Plus className="size-4" />} disabled={!bindingsAvailable} data-testid="studio-empty-create" onClick={() => setWizardOpen(true)}>
                                {t("studio.empty.create")}
                            </Button>
                        </div>
                    </section>
                )}
            </div>

            {bindingsAvailable ? <DramaCreateWizard open={wizardOpen} onClose={() => setWizardOpen(false)} onCreated={(projectId) => navigate(`/studio/${projectId}`)} /> : null}
        </main>
    );
}

/**
 * formatWhen renders a timestamp the core produced.
 *
 * The value is an RFC3339 string; an unparseable one is shown verbatim rather
 * than replaced with a time this page invented.
 */
function formatWhen(value: string): string {
    const parsed = new Date(value);
    if (Number.isNaN(parsed.getTime())) return value;
    return parsed.toLocaleString();
}
