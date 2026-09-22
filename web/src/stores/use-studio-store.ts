import { create } from "zustand";

import type { desktop } from "@/wailsjs/go/models";

/**
 * Studio UI state.
 *
 * ADR-BASE-004 draws the line this store sits behind: Zustand keeps temporary UI
 * state and a read-only projection of what the Go core answered, and never
 * becomes the place a domain fact lives. So there is no persistence here at all
 * — no localForage, no localStorage — and every field below is either a
 * selection, a loaded page of results, or a loading flag.
 *
 * The projection fields are named `…_loaded` rather than being a cache: they are
 * replaced by a query result, never edited in place, and the studio's commands
 * go to Go and then re-query.
 */

/** Which section of the studio shell is showing. */
export type StudioSection =
    | "overview"
    | "source"
    | "story-graph"
    | "script"
    | "characters"
    | "locations"
    | "props"
    | "director"
    | "storyboard-table"
    | "storyboard-canvas"
    | "video"
    | "audio"
    | "timeline"
    | "quality"
    | "agents"
    | "memory";

export type StudioState = {
    /** The section the shell is showing. */
    section: StudioSection;
    /** The episode whose detail the script and storyboard sections follow. */
    activeEpisodeId: string;
    /** The source document whose chapters the source section follows. */
    activeSourceDocumentId: string;

    // Projections. Each is the last answer a query gave, replaced wholesale.
    episodes: desktop.EpisodeDTO[];
    sourceDocuments: desktop.SourceDocumentDTO[];
    staleMarks: desktop.StaleMarkDTO[];
    workflowRuns: desktop.WorkflowRunDTO[];
    assets: desktop.AssetDTO[];
    /** True once a load has finished, so an empty list is not shown before it. */
    episodesLoaded: boolean;
    /**
     * The error from the most recent load, already human-readable, or empty.
     * The studio shows it rather than an empty state, because "could not read"
     * and "nothing there" are different situations.
     */
    loadError: string;

    setSection: (section: StudioSection) => void;
    setActiveEpisode: (episodeId: string) => void;
    setActiveSourceDocument: (documentId: string) => void;
    setEpisodes: (episodes: desktop.EpisodeDTO[]) => void;
    setSourceDocuments: (documents: desktop.SourceDocumentDTO[]) => void;
    setStaleMarks: (marks: desktop.StaleMarkDTO[]) => void;
    setWorkflowRuns: (runs: desktop.WorkflowRunDTO[]) => void;
    setAssets: (assets: desktop.AssetDTO[]) => void;
    setLoadError: (message: string) => void;
    reset: () => void;
};

const INITIAL_STATE = {
    section: "overview" as StudioSection,
    activeEpisodeId: "",
    activeSourceDocumentId: "",
    episodes: [] as desktop.EpisodeDTO[],
    sourceDocuments: [] as desktop.SourceDocumentDTO[],
    staleMarks: [] as desktop.StaleMarkDTO[],
    workflowRuns: [] as desktop.WorkflowRunDTO[],
    assets: [] as desktop.AssetDTO[],
    episodesLoaded: false,
    loadError: "",
};

export const useStudioStore = create<StudioState>((set) => ({
    ...INITIAL_STATE,

    setSection: (section) => set({ section }),
    setActiveEpisode: (episodeId) => set({ activeEpisodeId: episodeId }),
    setActiveSourceDocument: (documentId) => set({ activeSourceDocumentId: documentId }),
    setEpisodes: (episodes) => set({ episodes, episodesLoaded: true }),
    setSourceDocuments: (sourceDocuments) => set({ sourceDocuments }),
    setStaleMarks: (staleMarks) => set({ staleMarks }),
    setWorkflowRuns: (workflowRuns) => set({ workflowRuns }),
    setAssets: (assets) => set({ assets }),
    setLoadError: (loadError) => set({ loadError }),
    // Switching projects clears the projection so one project's rows are never
    // shown under another's heading while the new query is in flight.
    reset: () => set({ ...INITIAL_STATE }),
}));

/**
 * STUDIO_SECTIONS lists the shell's navigation in the order PRD §8 gives for
 * the drama studio, with the work package each section's content belongs to.
 *
 * A section whose content is not built yet still appears, because the shell's
 * job in this work package is the navigation and the honest empty states. The
 * `available` flag is what the section renders as its status, so a viewer is
 * told which package will fill it rather than being shown a blank page.
 */
export type StudioSectionSpec = {
    id: StudioSection;
    /** i18n key for the section's title. */
    titleKey: string;
    /** i18n key for the one-line description of what the section holds. */
    descriptionKey: string;
    /**
     * True when this work package backs the section with real commands. False
     * means the section is an empty state naming the package that will fill it.
     */
    available: boolean;
    /** The work package that owns the section's content. */
    workPackage: string;
};

export const STUDIO_SECTIONS: StudioSectionSpec[] = [
    { id: "overview", titleKey: "studio.sections.overview.title", descriptionKey: "studio.sections.overview.description", available: true, workPackage: "WP-05" },
    { id: "source", titleKey: "studio.sections.source.title", descriptionKey: "studio.sections.source.description", available: true, workPackage: "WP-05" },
    { id: "story-graph", titleKey: "studio.sections.storyGraph.title", descriptionKey: "studio.sections.storyGraph.description", available: true, workPackage: "WP-05" },
    { id: "script", titleKey: "studio.sections.script.title", descriptionKey: "studio.sections.script.description", available: true, workPackage: "WP-05" },
    { id: "characters", titleKey: "studio.sections.characters.title", descriptionKey: "studio.sections.characters.description", available: true, workPackage: "WP-05" },
    { id: "locations", titleKey: "studio.sections.locations.title", descriptionKey: "studio.sections.locations.description", available: true, workPackage: "WP-05" },
    { id: "props", titleKey: "studio.sections.props.title", descriptionKey: "studio.sections.props.description", available: true, workPackage: "WP-05" },
    { id: "director", titleKey: "studio.sections.director.title", descriptionKey: "studio.sections.director.description", available: true, workPackage: "WP-09" },
    { id: "storyboard-table", titleKey: "studio.sections.storyboardTable.title", descriptionKey: "studio.sections.storyboardTable.description", available: true, workPackage: "WP-09" },
    { id: "storyboard-canvas", titleKey: "studio.sections.storyboardCanvas.title", descriptionKey: "studio.sections.storyboardCanvas.description", available: true, workPackage: "WP-05" },
    { id: "video", titleKey: "studio.sections.video.title", descriptionKey: "studio.sections.video.description", available: false, workPackage: "WP-11" },
    { id: "audio", titleKey: "studio.sections.audio.title", descriptionKey: "studio.sections.audio.description", available: false, workPackage: "WP-11" },
    { id: "timeline", titleKey: "studio.sections.timeline.title", descriptionKey: "studio.sections.timeline.description", available: false, workPackage: "WP-11" },
    { id: "quality", titleKey: "studio.sections.quality.title", descriptionKey: "studio.sections.quality.description", available: true, workPackage: "WP-05" },
    // The Agent Center is WP-07's surface: the runs the three-layer runtime produced, with
    // their messages and tool calls, and the inventory of agents this build can run.
    { id: "agents", titleKey: "studio.sections.agents.title", descriptionKey: "studio.sections.agents.description", available: true, workPackage: "WP-07" },
    // The Memory Center is WP-10's surface. PRD section 8 lists 记忆中心 among the GLOBAL modules
    // rather than among a studio's sections, and this build places it here anyway — for the reason
    // the Agent Center is here: it is reached per project, a memory's scope names a project, and the
    // studio is where a project's surface lives. ADR-0014 records the ruling.
    { id: "memory", titleKey: "studio.sections.memory.title", descriptionKey: "studio.sections.memory.description", available: true, workPackage: "WP-10" },
];
