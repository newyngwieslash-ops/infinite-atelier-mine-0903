import type { agent } from "@/wailsjs/go/models";

/**
 * agent-skills.ts is RP-06.2's frontend service for skill version
 * management: the version history read, the derive command, and the
 * rollback/switch. All three are narrow commands over the binding; a build
 * without the versioning store surfaces its refusal verbatim.
 */

let bindingModule: Promise<typeof import("@/wailsjs/go/desktop/AgentBinding")> | undefined;

function skillSurfaceAvailable(): boolean {
    const bindings = (window as unknown as {
        go?: { desktop?: { AgentBinding?: Record<string, unknown> } };
    }).go?.desktop ?? null;
    return typeof bindings?.AgentBinding?.ListSkillVersions === "function";
}

async function loadBinding() {
    bindingModule ??= import("@/wailsjs/go/desktop/AgentBinding").catch((error: unknown) => {
        bindingModule = undefined;
        throw error;
    });
    return bindingModule;
}

/** listSkillVersions returns an agent's version history, newest first. */
export async function listSkillVersions(agentKey: string): Promise<agent.SkillVersion[]> {
    if (!skillSurfaceAvailable()) throw new Error("the skill version surface is unavailable in this build");
    const { ListSkillVersions } = await loadBinding();
    return ListSkillVersions(agentKey);
}

/** createSkillVersion derives a NEW version; it does not activate it. */
export async function createSkillVersion(request: {
    agentKey: string;
    basedOnVersionId: string;
    newVersionLabel: string;
    document: string;
}): Promise<agent.SkillVersion> {
    if (!skillSurfaceAvailable()) throw new Error("the skill version surface is unavailable in this build");
    const { CreateSkillVersion } = await loadBinding();
    return CreateSkillVersion(request);
}

/** activateSkillVersion rolls the active pointer — affects only later runs. */
export async function activateSkillVersion(agentKey: string, versionId: string): Promise<void> {
    if (!skillSurfaceAvailable()) throw new Error("the skill version surface is unavailable in this build");
    const { ActivateSkillVersion } = await loadBinding();
    return ActivateSkillVersion(agentKey, versionId);
}
