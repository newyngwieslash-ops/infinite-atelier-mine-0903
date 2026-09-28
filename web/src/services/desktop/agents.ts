import type { agentruntime, desktop } from "@/wailsjs/go/models";

/**
 * Client for the agent bindings.
 *
 * The agent surface is one object (AgentBinding), and this module is the single
 * place the webview learns whether it exists. It keeps the same three rules the
 * drama client keeps, for the same reasons:
 *
 * 1. A browser with no Wails runtime gets an honest answer rather than an error.
 *    The Agent Center renders an explanation instead of a broken page, because a
 *    development browser is a supported way to run the app and every agent fact
 *    lives in the Go core.
 * 2. A query that cannot be answered returns an empty result; a command that
 *    cannot be performed throws. There is no command on this surface — every
 *    method reads — so every method here answers or reports.
 * 3. No method invents data. Every value comes back from the binding.
 */

type WailsBinding = Record<string, unknown>;

type WailsGo = {
    desktop?: {
        AgentBinding?: WailsBinding;
    };
};

type DesktopWindow = Window & { go?: WailsGo };

function getDesktopWindow(): DesktopWindow | null {
    return typeof window === "undefined" ? null : window;
}

/** Methods the agent surface must have for the Agent Center to be usable. */
const REQUIRED_AGENT_METHODS = ["ListAgentRuns", "GetAgentRunTrace"] as const;

/**
 * isAgentBindingsAvailable reports whether the Agent Center can talk to the core.
 *
 * It checks a representative set of methods rather than every one, so an older
 * build that has the binding but lacks a newer method is still treated as
 * available and fails on the specific call — a clearer error than a blanket
 * "unavailable".
 */
export function isAgentBindingsAvailable(): boolean {
    const binding = getDesktopWindow()?.go?.desktop?.AgentBinding;
    if (!binding) return false;
    return REQUIRED_AGENT_METHODS.every((method) => typeof binding[method] === "function");
}

/**
 * isAgentInventoryAvailable reports whether the inventory can be read.
 *
 * It is a separate question from isAgentBindingsAvailable, because the two have
 * different requirements in Go: the inventory reads the assembled registry and
 * the trace reads a database. A safe-mode build therefore lists its agents and
 * shows no runs, and the interface has to be able to say so rather than showing
 * one state for both.
 */
export function isAgentInventoryAvailable(): boolean {
    const binding = getDesktopWindow()?.go?.desktop?.AgentBinding;
    return !!binding && typeof binding["AgentInventory"] === "function";
}

/** listAgentRuns returns a project's runs newest first. */
export async function listAgentRuns(projectId: string): Promise<agentruntime.RunSummary[]> {
    const binding = getDesktopWindow()?.go?.desktop?.AgentBinding;
    if (!binding || typeof binding["ListAgentRuns"] !== "function") return [];
    const runs = (await (binding["ListAgentRuns"] as (id: string) => Promise<unknown>)(projectId)) as
        | agentruntime.RunSummary[]
        | null;
    return runs ?? [];
}

/** getAgentRunTrace returns one run with its messages and tool calls. */
export async function getAgentRunTrace(
    projectId: string,
    runId: string,
): Promise<agentruntime.RunTrace | null> {
    const binding = getDesktopWindow()?.go?.desktop?.AgentBinding;
    if (!binding || typeof binding["GetAgentRunTrace"] !== "function") return null;
    return (await (binding["GetAgentRunTrace"] as (p: string, r: string) => Promise<unknown>)(
        projectId,
        runId,
    )) as agentruntime.RunTrace;
}

/** runStatusCounts reports how many runs a project has in each status. */
export async function runStatusCounts(projectId: string): Promise<Record<string, number>> {
    const binding = getDesktopWindow()?.go?.desktop?.AgentBinding;
    if (!binding || typeof binding["RunStatusCounts"] !== "function") return {};
    const counts = (await (
        binding["RunStatusCounts"] as (id: string) => Promise<unknown>
    )(projectId)) as Record<string, number> | null;
    return counts ?? {};
}

/**
 * agentInventory returns the agents this build can run.
 *
 * A failure returns an empty inventory rather than throwing, because this is a
 * QUERY: the Agent Center's job is to show what exists, and a surface with no
 * agents is a legitimate answer for a build that composed none.
 */
export async function agentInventory(): Promise<desktop.AgentSpecDTO[]> {
    const binding = getDesktopWindow()?.go?.desktop?.AgentBinding;
    if (!binding || typeof binding["AgentInventory"] !== "function") return [];
    const inventory = (await (binding["AgentInventory"] as () => Promise<unknown>)()) as
        | desktop.AgentInventoryDTO
        | null;
    return inventory?.agents ?? [];
}

/**
 * setAgentEnabled starts or stops one agent — FR-090's management surface.
 *
 * The state is this build's in-process stop switch (T09): a disabled agent
 * refuses at the skill read, and a stopped agent resumes on the next app
 * start. An unknown key is refused by the core.
 */
export async function setAgentEnabled(agentKey: string, enabled: boolean): Promise<void> {
    const binding = getDesktopWindow()?.go?.desktop?.AgentBinding;
    if (!binding || typeof binding["SetAgentEnabled"] !== "function") {
        throw new Error("desktop agent bindings are unavailable");
    }
    await (binding["SetAgentEnabled"] as (key: string, enabled: boolean) => Promise<void>)(agentKey, enabled);
}

/**
 * agentSkillDocument returns one agent's skill text for read-only viewing.
 *
 * It is prompt material: the version a run cites is the pack's hash, so a
 * change is a new pack rather than an edit here.
 */
export async function agentSkillDocument(agentKey: string): Promise<string> {
    const binding = getDesktopWindow()?.go?.desktop?.AgentBinding;
    if (!binding || typeof binding["AgentSkillDocument"] !== "function") {
        throw new Error("desktop agent bindings are unavailable");
    }
    return (binding["AgentSkillDocument"] as (key: string) => Promise<string>)(agentKey);
}
