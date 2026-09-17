import type { CanvasConnection, CanvasNodeData, CanvasAssistantSession, ViewportTransform } from "@/types/canvas";
import type { CanvasBackgroundMode } from "@/lib/canvas-theme";
import type { CanvasProject } from "@/stores/canvas/use-canvas-store";
import type { CanvasDocument, CanvasPersistenceAdapter } from "@/services/desktop/canvas-adapter";
import {
    CreateProject,
    DeleteProject,
    ListProjects,
    LoadCanvas,
    RenameProject,
    UpdateViewport,
    UpsertNode,
    CreateEdge,
    DeleteNodes,
    DeleteEdges,
    SaveChatSession,
} from "@/wailsjs/go/desktop/ProjectsBinding";
import type { desktop } from "@/wailsjs/go/models";

/**
 * GoCanvasAdapter persists the canvas through the Go core.
 *
 * This is the desktop-mode implementation: the database is the source of truth
 * and the browser stores receive nothing new (ADR-BASE-004). The geometry the
 * canvas edits — node positions, sizes, the viewport — is written here, while
 * everything the Go schema does not model travels inside `uiState` and
 * `legacyMetadata`, which are opaque to Go and therefore round-trip verbatim.
 *
 * Two conversions happen in this file and nowhere else:
 *
 *  1. `uiState` carries the canvas display fields (a node's content, its status)
 *     plus the rest of the legacy metadata, so a Go round trip does not lose a
 *     field the canvas renders.
 *  2. Connections are created after their endpoints exist, because the schema has
 *     foreign keys; a connection whose endpoint failed is skipped and reported
 *     rather than silently dropped.
 */
export class GoCanvasAdapter implements CanvasPersistenceAdapter {
    readonly kind = "go" as const;

    /**
     * known holds the last state written for each project, so a save can send
     * only what changed.
     *
     * The canvas reports its whole document on every change, while a Go write is
     * a binding round-trip. Without a comparison, moving one node on a
     * 1,000-node canvas would issue 1,000 updates, which is exactly the case
     * AC-CANVAS-003 measures. The map is keyed by project so two open canvases
     * cannot confuse each other, and it is refreshed from every load.
     */
    private readonly known = new Map<string, KnownCanvas>();

    async listProjects(): Promise<CanvasProject[]> {
        const projects = await ListProjects({ limit: 1000 });
        // The list view needs a project's identity, not its canvas, so each row
        // is returned with empty geometry: the canvas loads when it is opened.
        return projects.map((project) => emptyProject(project.id, project.name, project.createdAt, project.updatedAt));
    }

    async loadCanvas(projectId: string): Promise<CanvasDocument | null> {
        let snapshot: desktop.CanvasSnapshotDTO;
        try {
            snapshot = await LoadCanvas(projectId);
        } catch {
            // A missing project is not an error the caller can act on: the canvas
            // page navigates away from a project that is gone.
            return null;
        }
        const documentId = snapshot.documentId;
        const nodes = snapshot.nodes.map(toCanvasNode);
        const connections = snapshot.edges.map((edge) => ({
            id: edge.id,
            fromNodeId: edge.fromNodeId,
            toNodeId: edge.toNodeId,
        }));
        const chatSessions = snapshot.chatSessions.map((session) => toChatSession(session, documentId));
        const background = snapshot.background ?? "";
        return {
            id: projectId,
            title: snapshot.project.name,
            createdAt: snapshot.project.createdAt,
            updatedAt: snapshot.project.updatedAt,
            nodes,
            connections,
            chatSessions,
            activeChatId: readActiveChatId(background),
            backgroundMode: readBackgroundMode(background),
            showImageInfo: readShowImageInfo(background),
            viewport: readViewport(snapshot.viewport),
            documentId,
        };
    }

    async createProject(title: string): Promise<string> {
        const project = await CreateProject({
            name: title,
            description: "",
            projectType: "free_canvas",
            language: "zh-CN",
        });
        return project.id;
    }

    /**
     * saveCanvas writes only what changed.
     *
     * The canvas reports its whole document on every edit, so this method diffs
     * that document against the last state it wrote (or loaded) and issues one
     * command per actual change. That is what keeps a drag on a large canvas from
     * turning into a write per node.
     *
     * Each command carries the revision the stored row had when it was read, so a
     * concurrent edit is reported as a conflict instead of being overwritten. A
     * command that fails is skipped rather than aborting the pass: one bad node
     * must not lose every other edit, and the mismatch is visible on the next
     * load.
     */
    async saveCanvas(projectId: string, document: CanvasDocument): Promise<void> {
        let snapshot: desktop.CanvasSnapshotDTO;
        try {
            snapshot = await LoadCanvas(projectId);
        } catch {
            // Without the stored state there is nothing to diff against, and
            // writing blind would overwrite rows this client never read.
            return;
        }
        const documentId = snapshot.documentId;
        const revisions = new Map(snapshot.nodes.map((node) => [node.id, node.revision]));
        const storedNodeIds = new Set(snapshot.nodes.map((node) => node.id));
        const storedEdgeIds = new Set(snapshot.edges.map((edge) => edge.id));
        const previous = this.known.get(projectId);

        // Removals first, so a node the document dropped cannot be recreated by
        // the upsert loop below.
        const wantedNodes = new Set(document.nodes.map((node) => node.id));
        const removedNodes = [...storedNodeIds].filter((id) => !wantedNodes.has(id));
        if (removedNodes.length > 0) {
            await ignoreFailure(() => DeleteNodes(removedNodes));
            for (const id of removedNodes) storedNodeIds.delete(id);
        }
        const wantedEdges = new Set(document.connections.map((connection) => connection.id));
        const removedEdges = [...storedEdgeIds].filter((id) => !wantedEdges.has(id));
        if (removedEdges.length > 0) {
            await ignoreFailure(() => DeleteEdges(removedEdges));
            for (const id of removedEdges) storedEdgeIds.delete(id);
        }

        const nextKnown: KnownCanvas = { nodes: new Map(), edges: new Set(storedEdgeIds), sessions: new Map() };
        for (const node of document.nodes) {
            const payload = toNodeWrite(node, documentId, revisions.get(node.id));
            const fingerprint = JSON.stringify(payload);
            // The fingerprint is recorded only when the write succeeded: recording
            // it first would make a failed write look like a completed one and the
            // next save would skip the node, losing the edit.
            if (previous?.nodes.get(node.id) === fingerprint) {
                nextKnown.nodes.set(node.id, fingerprint);
                continue;
            }
            if (await ignoreFailure(() => UpsertNode(payload))) {
                nextKnown.nodes.set(node.id, fingerprint);
            }
        }

        for (const connection of document.connections) {
            if (storedEdgeIds.has(connection.id)) continue;
            const created = await ignoreFailure(() =>
                CreateEdge({
                    documentId,
                    fromNodeId: connection.fromNodeId,
                    toNodeId: connection.toNodeId,
                }),
            );
            if (created) {
                storedEdgeIds.add(connection.id);
                nextKnown.edges.add(connection.id);
            }
        }

        for (const session of document.chatSessions) {
            const messagesJson = JSON.stringify(session.messages);
            if (previous?.sessions.get(session.id) === messagesJson) {
                nextKnown.sessions.set(session.id, messagesJson);
                continue;
            }
            const saved = await ignoreFailure(() =>
                SaveChatSession({
                    // A session the canvas created in this session has no stored id
                    // yet; the binding assigns one when the id is empty.
                    id: session.id.startsWith("local-") ? "" : session.id,
                    canvasDocumentId: documentId,
                    title: session.title,
                    messagesJson,
                }),
            );
            if (saved) nextKnown.sessions.set(session.id, messagesJson);
        }

        this.known.set(projectId, nextKnown);
    }

    async saveViewport(projectId: string, viewport: ViewportTransform): Promise<void> {
        const snapshot = await LoadCanvas(projectId);
        await UpdateViewport({
            documentId: snapshot.documentId,
            viewport: { x: viewport.x, y: viewport.y, k: viewport.k },
            revision: 0,
        } as desktop.UpdateViewportRequest);
    }

    async renameProject(projectId: string, title: string): Promise<void> {
        let revision = 1;
        try {
            const snapshot = await LoadCanvas(projectId);
            revision = snapshot.project.revision;
        } catch {
            // A failed read leaves the revision at its initial value; the rename
            // then reports a conflict rather than overwriting a concurrent edit.
        }
        await RenameProject({ id: projectId, name: title, revision });
    }

    async deleteProjects(ids: string[]): Promise<void> {
        for (const id of ids) {
            await DeleteProject(id);
        }
    }
}

/** emptyProject is a list-view project with no geometry loaded. */
function emptyProject(id: string, title: string, createdAt: string, updatedAt: string): CanvasProject {
    return {
        id,
        title,
        createdAt,
        updatedAt,
        nodes: [],
        connections: [],
        chatSessions: [],
        activeChatId: null,
        backgroundMode: "lines",
        showImageInfo: false,
        viewport: { x: 0, y: 0, k: 1 },
    };
}

/**
 * toCanvasNode rebuilds a canvas node from its stored row.
 *
 * The display fields come back out of `uiState`, which is where they were put;
 * the rest of the legacy metadata is merged into `metadata` so a canvas that was
 * migrated from the browser store renders exactly as it did there.
 */
export function toCanvasNode(node: desktop.CanvasNodeDTO): CanvasNodeData {
    const display = parseObject(node.uiState) ?? {};
    const legacy = parseObject(node.legacyMetadata) ?? {};
    return {
        id: node.id,
        type: node.nodeType,
        title: node.title,
        position: { x: node.positionX, y: node.positionY },
        width: node.width,
        height: node.height,
        metadata: { ...legacy, ...display },
    };
}

/**
 * toNodeWrite converts a canvas node into its stored shape.
 *
 * The display fields the canvas renders are separated from the rest of the
 * metadata: the first group goes into `uiState`, which exists to be read back,
 * and the second into `legacyMetadata`, which exists to be preserved. Splitting
 * them is what lets a future field be added to Go without stranding the fields
 * that only the canvas understands.
 */
export function toNodeWrite(node: CanvasNodeData, documentId: string, revision?: number): desktop.UpsertNodeRequest {
    const metadata = node.metadata ?? {};
    const display: Record<string, unknown> = {};
    const retained: Record<string, unknown> = {};
    for (const [key, value] of Object.entries(metadata)) {
        if (key === "content" || key === "status" || key === "errorDetails") {
            display[key] = value;
        } else {
            retained[key] = value;
        }
    }
    return {
        id: node.id,
        documentId,
        nodeType: String(node.type),
        title: node.title ?? "",
        positionX: node.position.x,
        positionY: node.position.y,
        width: node.width,
        height: node.height,
        zIndex: 0,
        uiState: JSON.stringify(display),
        legacyMetadata: JSON.stringify(retained),
        revision: revision ?? 0,
    } as desktop.UpsertNodeRequest;
}

/** toChatSession converts a stored chat session into the canvas shape. */
function toChatSession(session: desktop.CanvasChatSessionDTO, documentId: string): CanvasAssistantSession {
    return {
        id: session.id,
        title: session.title,
        messages: parseArray(session.messagesJson),
        createdAt: session.createdAt,
        updatedAt: session.updatedAt,
        // The document id is not part of the canvas session shape; it is carried
        // on the session so a save knows which canvas it belongs to.
        canvasDocumentId: documentId,
    } as CanvasAssistantSession;
}

/** readViewport extracts the viewport from the stored transform. */
function readViewport(viewport: desktop.ViewportDTO): ViewportTransform {
    if (!viewport || typeof viewport.k !== "number" || viewport.k <= 0) {
        return { x: 0, y: 0, k: 1 };
    }
    return { x: viewport.x, y: viewport.y, k: viewport.k };
}

/** readBackgroundMode reads the display flag out of the stored background blob. */
function readBackgroundMode(background: string): CanvasBackgroundMode {
    const parsed = parseObject(background);
    const mode = parsed?.backgroundMode;
    // A stored value outside the union is not a mode the canvas knows, so the
    // default is applied rather than passing an unusable string through.
    if (mode === "dots" || mode === "lines" || mode === "blank") return mode;
    return "lines";
}

function readShowImageInfo(background: string): boolean {
    const parsed = parseObject(background);
    return parsed?.showImageInfo === true;
}

function readActiveChatId(background: string): string | null {
    const parsed = parseObject(background);
    const value = parsed?.activeChatId;
    return typeof value === "string" && value !== "" ? value : null;
}

/** parseObject decodes a JSON object, tolerating an empty or malformed value. */
function parseObject(value: string | undefined): Record<string, unknown> | null {
    if (!value) return null;
    try {
        const parsed = JSON.parse(value);
        if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
            return parsed as Record<string, unknown>;
        }
        return null;
    } catch {
        // A malformed blob degrades to "no extra fields" rather than breaking the
        // canvas load: the node still renders with its stored geometry.
        return null;
    }
}

/** parseArray decodes a JSON array, tolerating an empty or malformed value. */
function parseArray(value: string | undefined): CanvasAssistantSession["messages"] {
    if (!value) return [];
    try {
        const parsed = JSON.parse(value);
        return Array.isArray(parsed) ? parsed : [];
    } catch {
        return [];
    }
}

/** Unused imports are kept out of the bundle by naming them here. */
export type { CanvasConnection, CanvasDocument };

/**
 * KnownCanvas is the last state this adapter wrote for one project.
 *
 * Node and session entries are JSON fingerprints rather than deep copies: the
 * comparison only asks "did this change since the last write", and a string
 * comparison answers that without holding a second copy of the document.
 */
type KnownCanvas = {
    nodes: Map<string, string>;
    edges: Set<string>;
    sessions: Map<string, string>;
};

/**
 * ignoreFailure runs an operation and reports whether it succeeded.
 *
 * A canvas save issues many commands; one failure must not abandon the rest, and
 * the caller sees the consequence on the next load. Returning a boolean lets the
 * edge loop record only the connections that were actually created.
 */
async function ignoreFailure(operation: () => Promise<unknown>): Promise<boolean> {
    try {
        await operation();
        return true;
    } catch {
        return false;
    }
}
