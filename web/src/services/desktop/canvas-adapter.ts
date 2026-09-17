import type { CanvasAssistantSession, CanvasConnection, CanvasNodeData, ViewportTransform } from "@/types/canvas";
import type { CanvasProject } from "@/stores/canvas/use-canvas-store";
import type { CanvasBackgroundMode } from "@/lib/canvas-theme";

/**
 * The canvas persistence seam.
 *
 * ARCHITECTURE §9.2 calls this the CanvasPersistenceAdapter: the canvas reads and
 * writes its document through one interface, so the component tree does not know
 * whether the facts live in the Go core or in the browser's IndexedDB.
 *
 * Two implementations exist because the application runs in two modes:
 *
 *  - desktop (secure) mode: the Go core owns the facts, and the browser stores
 *    receive nothing new (ADR-BASE-004: no new domain data in localForage);
 *  - browser development mode: there is no Go core, so the existing store
 *    remains the authority.
 *
 * The interface is deliberately narrow: it carries exactly what a canvas
 * document holds, so neither implementation can smuggle a domain fact through a
 * side channel.
 */
export type CanvasDocument = {
    id: string;
    title: string;
    createdAt: string;
    updatedAt: string;
    nodes: CanvasNodeData[];
    connections: CanvasConnection[];
    chatSessions: CanvasAssistantSession[];
    activeChatId: string | null;
    backgroundMode: CanvasBackgroundMode;
    showImageInfo: boolean;
    viewport: ViewportTransform;
    /** The Go document id, when one exists. */
    documentId?: string;
};

/** A node as the adapter receives it, with the revision a Go write needs. */
export type CanvasNodeWrite = {
    id: string;
    nodeType: string;
    title: string;
    entityType?: string;
    entityId?: string;
    positionX: number;
    positionY: number;
    width: number;
    height: number;
    zIndex: number;
    /** The canvas display fields the domain does not model. */
    uiState?: string;
    /** Migrated fields the schema does not model, preserved verbatim. */
    legacyMetadata?: string;
    revision?: number;
};

export type CanvasEdgeWrite = {
    id: string;
    fromNodeId: string;
    toNodeId: string;
    relationType?: string;
    fromPort?: string;
    toPort?: string;
    required?: boolean;
    metadata?: string;
    legacyMetadata?: string;
    revision?: number;
};

/**
 * CanvasPersistenceAdapter is what the page and the store depend on.
 *
 * Every method returns a promise: the Go implementation crosses a binding, and
 * making the legacy implementation synchronous would let callers accidentally
 * depend on ordering that only holds in one mode.
 */
export type CanvasPersistenceAdapter = {
    /** Which backend is in use, for diagnostics and tests. */
    readonly kind: "go" | "legacy";
    /** Loads every project, newest first. */
    listProjects(): Promise<CanvasProject[]>;
    /** Loads one project with its full canvas document, or null when absent. */
    loadCanvas(projectId: string): Promise<CanvasDocument | null>;
    /** Creates a project and its default canvas, returning the new id. */
    createProject(title: string): Promise<string>;
    /** Persists the canvas document's layout and projections. */
    saveCanvas(projectId: string, document: CanvasDocument): Promise<void>;
    /** Persists the viewport alone, which is a canvas-only change. */
    saveViewport(projectId: string, viewport: ViewportTransform): Promise<void>;
    /** Renames a project. */
    renameProject(projectId: string, title: string): Promise<void>;
    /** Removes projects and everything under them. */
    deleteProjects(ids: string[]): Promise<void>;
};

/**
 * isSecureCanvasMode reports whether the Go core owns the facts.
 *
 * It mirrors the provider gateway's mode check: the desktop shell injects the
 * bindings into `window.go`, and their absence means the page is running in a
 * browser, where there is nothing to route to.
 */
export function isSecureCanvasMode(): boolean {
    if (typeof window === "undefined") return false;
    const scope = window as unknown as { go?: { desktop?: Record<string, unknown> } };
    return Boolean(scope.go?.desktop?.ProjectsBinding);
}

/**
 * resolveCanvasAdapter picks the implementation for the current mode.
 *
 * One decision point, evaluated once and cached, so no caller can end up with
 * half its writes in each backend. The Go adapter is imported lazily: a browser
 * build has no bindings, and importing them eagerly would fail there.
 */
let resolved: CanvasPersistenceAdapter | undefined;
let resolving: Promise<CanvasPersistenceAdapter> | undefined;

export async function resolveCanvasAdapter(): Promise<CanvasPersistenceAdapter> {
    if (resolved) return resolved;
    if (!resolving) {
        resolving = (async () => {
            if (isSecureCanvasMode()) {
                const { GoCanvasAdapter } = await import("@/services/desktop/canvas-adapter-go");
                resolved = new GoCanvasAdapter();
            } else {
                const { LegacyCanvasAdapter } = await import("@/services/desktop/canvas-adapter-legacy");
                resolved = new LegacyCanvasAdapter();
            }
            return resolved;
        })();
    }
    return resolving;
}

/** canvasAdapterKind reports the resolved backend, for diagnostics and tests. */
export function canvasAdapterKind(): "go" | "legacy" | "unresolved" {
    return resolved?.kind ?? "unresolved";
}

/** resetCanvasAdapter clears the cached decision. It exists for tests. */
export function resetCanvasAdapter(): void {
    resolved = undefined;
    resolving = undefined;
}
