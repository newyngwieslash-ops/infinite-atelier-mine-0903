import type { desktop } from "@/wailsjs/go/models";

/**
 * memory.ts is the client for the memory center's binding.
 *
 * # Why it is a separate module from drama.ts
 *
 * The memory center is a GLOBAL module rather than a section of one drama project (PRD FR-120's UI
 * placement puts 记忆中心 beside 素材库 and Agent 中心), and its reads take a project identifier
 * rather than an episode: a memory's scope names the project, and the section above it chooses which
 * one to show. Keeping the client separate means the memory section does not import the drama
 * surface's module, which is the shape that would make one feature's failure take the other with it.
 *
 * # The unavailable case
 *
 * Every command THROWS when the core is absent, and the queries return an empty result. That is the
 * same split drama.ts uses and for the same reason: a browser-mode preview should render an empty
 * memory list rather than an error, while a button a user pressed must say why nothing happened.
 */

/** Methods the memory surface must have for the section to be usable. */
const REQUIRED_MEMORY_METHODS = ["ListMemories", "GetMemory", "PreviewMemoryRecall"] as const;

/**
 * isMemoryBindingsAvailable reports whether the memory center can talk to the Go core.
 *
 * It checks a representative set rather than every method, so an older build with the binding but
 * without a newly added method is still available and fails on the specific call — which produces a
 * clearer message than a blanket "unavailable".
 */
export function isMemoryBindingsAvailable(): boolean {
    const memory = getDesktopWindow()?.go?.desktop?.MemoryBinding;
    if (!memory) return false;
    return REQUIRED_MEMORY_METHODS.every((method) => typeof memory[method] === "function");
}

/** isMemoryCommandsAvailable reports whether the binding's commands are reachable. */
export function isMemoryCommandsAvailable(): boolean {
    const memory = getDesktopWindow()?.go?.desktop?.MemoryBinding;
    return Boolean(memory) && typeof memory?.DeleteMemory === "function" && typeof memory?.SetMemoryLocked === "function";
}

/** isMemoryRebuildAvailable reports whether the embedding rebuild is reachable. */
export function isMemoryRebuildAvailable(): boolean {
    const memory = getDesktopWindow()?.go?.desktop?.MemoryBinding;
    return Boolean(memory) && typeof memory?.RebuildMemoryEmbedding === "function";
}

type DesktopWindow = Window & {
    go?: {
        desktop?: Record<string, Record<string, (...args: never[]) => unknown>>;
    };
};

function getDesktopWindow(): DesktopWindow | undefined {
    if (typeof window === "undefined") return undefined;
    return window as DesktopWindow;
}

function unavailableError(): Error {
    return new Error("The desktop core is not available in this window.");
}

let memoryModule: Promise<typeof import("@/wailsjs/go/desktop/MemoryBinding")> | undefined;

async function loadMemoryBinding() {
    memoryModule ??= import("@/wailsjs/go/desktop/MemoryBinding").catch((error: unknown) => {
        memoryModule = undefined;
        throw error;
    });
    return memoryModule;
}

/**
 * listMemories returns a project's memories.
 *
 * A query in browser mode answers with an empty list rather than throwing, because a list is a
 * READ: the section renders its empty state, which is what a user with no core sees everywhere else.
 */
export async function listMemories(query: desktop.MemoryQuery): Promise<desktop.MemoryDTO[]> {
    if (!isMemoryBindingsAvailable()) return [];
    const binding = await loadMemoryBinding();
    return binding.ListMemories(query as never);
}

/** getMemory returns one memory. */
export async function getMemory(id: string): Promise<desktop.MemoryDTO | undefined> {
    if (!isMemoryBindingsAvailable()) return undefined;
    const binding = await loadMemoryBinding();
    return binding.GetMemory(id);
}

/** listMemoryEntityLinks returns a memory's links to domain entities. */
export async function listMemoryEntityLinks(memoryId: string): Promise<desktop.MemoryEntityLinkDTO[]> {
    if (!isMemoryBindingsAvailable()) return [];
    const binding = await loadMemoryBinding();
    return binding.ListMemoryEntityLinks(memoryId);
}

/**
 * listSummarySources returns what a summary cites.
 *
 * This is the read AC-MEM-004's "UI 可跳原始消息" is built on: each source carries the memory's own
 * role, agent, time and transcript citation.
 */
export async function listSummarySources(summaryId: string): Promise<desktop.MemorySummarySourceDTO[]> {
    if (!isMemoryBindingsAvailable()) return [];
    const binding = await loadMemoryBinding();
    return binding.ListSummarySources(summaryId);
}

/**
 * previewMemoryRecall returns what a query WOULD recall, without writing anything.
 *
 * The preview is the read that makes the recall explainable: it carries each scored candidate's raw
 * similarity beside its fused score, so a user asking "why did the agent not remember" can see
 * whether the candidate was below the threshold rather than absent.
 */
export async function previewMemoryRecall(request: desktop.RecallPreviewRequest): Promise<desktop.MemoryRecallPreviewDTO | undefined> {
    if (!isMemoryBindingsAvailable()) return undefined;
    const binding = await loadMemoryBinding();
    return binding.PreviewMemoryRecall(request as never);
}

/** deleteMemory removes a memory, optionally with the summaries that cite it. */
export async function deleteMemory(request: desktop.DeleteMemoryRequest): Promise<desktop.MemoryDeleteResultDTO> {
    if (!isMemoryCommandsAvailable()) throw unavailableError();
    const binding = await loadMemoryBinding();
    return binding.DeleteMemory(request as never);
}

/** setMemoryLocked pins or unpins a memory. */
export async function setMemoryLocked(request: desktop.MemoryPinRequest): Promise<boolean> {
    if (!isMemoryCommandsAvailable()) throw unavailableError();
    const binding = await loadMemoryBinding();
    return binding.SetMemoryLocked(request as never);
}

/**
 * updateMemoryContent replaces a memory's text.
 *
 * The caller must set `confirm`, because the edit CLEARS the memory's vector: the old vector
 * described the old text, and leaving it would make the memory match queries about something it no
 * longer says. The memory stops being searchable until a rebuild, and the confirmation is how the
 * user says they meant that.
 */
export async function updateMemoryContent(request: desktop.MemoryEditRequest): Promise<boolean> {
    if (!isMemoryCommandsAvailable()) throw unavailableError();
    const binding = await loadMemoryBinding();
    return binding.UpdateMemoryContent(request as never);
}

/** summarizeMemory condenses a scope's unsummarised memories. */
export async function summarizeMemory(request: desktop.SummarizeMemoryRequest): Promise<desktop.MemorySummarizeResultDTO> {
    if (!isMemoryCommandsAvailable()) throw unavailableError();
    const binding = await loadMemoryBinding();
    return binding.SummarizeMemory(request as never);
}

/** rebuildMemoryEmbedding re-embeds the memories that are on an older model or version. */
export async function rebuildMemoryEmbedding(request: desktop.RebuildMemoryEmbeddingRequest): Promise<desktop.MemoryRebuildResultDTO> {
    if (!isMemoryRebuildAvailable()) throw unavailableError();
    const binding = await loadMemoryBinding();
    return binding.RebuildMemoryEmbedding(request as never);
}
