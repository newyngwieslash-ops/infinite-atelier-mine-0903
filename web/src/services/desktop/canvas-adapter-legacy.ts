import type { CanvasProject } from "@/stores/canvas/use-canvas-store";
import type { CanvasDocument, CanvasPersistenceAdapter } from "@/services/desktop/canvas-adapter";
import type { ViewportTransform } from "@/types/canvas";
import { useCanvasStore } from "@/stores/canvas/use-canvas-store";

/**
 * LegacyCanvasAdapter keeps the browser store authoritative.
 *
 * It exists for browser development mode, where there is no Go core to route to.
 * The store already persists through localForage, so this adapter is a thin
 * translation layer and deliberately contains no new persistence of its own: a
 * second writer would be a second truth.
 *
 * In desktop mode this adapter is never constructed (see resolveCanvasAdapter),
 * which is what stops new canvas facts reaching localForage (ADR-BASE-004).
 */
export class LegacyCanvasAdapter implements CanvasPersistenceAdapter {
    readonly kind = "legacy" as const;

    async listProjects(): Promise<CanvasProject[]> {
        return useCanvasStore.getState().projects;
    }

    async loadCanvas(projectId: string): Promise<CanvasDocument | null> {
        const project = useCanvasStore.getState().openProject(projectId);
        if (!project) return null;
        return {
            id: project.id,
            title: project.title,
            createdAt: project.createdAt,
            updatedAt: project.updatedAt,
            nodes: project.nodes,
            connections: project.connections,
            chatSessions: project.chatSessions,
            activeChatId: project.activeChatId,
            backgroundMode: project.backgroundMode,
            showImageInfo: project.showImageInfo,
            viewport: project.viewport,
        };
    }

    async createProject(title: string): Promise<string> {
        return useCanvasStore.getState().createProject(title);
    }

    async saveCanvas(projectId: string, document: CanvasDocument): Promise<void> {
        // The store's update path also stamps updatedAt and triggers the debounced
        // localForage write, so the document is not written here.
        useCanvasStore.getState().updateProject(projectId, {
            nodes: document.nodes,
            connections: document.connections,
            chatSessions: document.chatSessions,
            activeChatId: document.activeChatId ?? null,
            backgroundMode: document.backgroundMode as CanvasProject["backgroundMode"],
            showImageInfo: document.showImageInfo,
        });
    }

    async saveViewport(projectId: string, viewport: ViewportTransform): Promise<void> {
        useCanvasStore.getState().updateProject(projectId, { viewport });
    }

    async renameProject(projectId: string, title: string): Promise<void> {
        useCanvasStore.getState().renameProject(projectId, title);
    }

    async deleteProjects(ids: string[]): Promise<void> {
        useCanvasStore.getState().deleteProjects(ids);
    }
}
