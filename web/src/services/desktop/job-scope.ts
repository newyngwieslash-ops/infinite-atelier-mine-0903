/**
 * Builds the identity part of a job submission.
 *
 * The Go job's idempotency key is derived from the scope plus the input, so this
 * scope is what prevents two unrelated requests from collapsing into one job
 * and what lets a failed node regenerate.
 *
 * It is a pure function so the rules are testable without a DOM: the batch
 * ordinal must be part of the identity (otherwise every image in a count>1
 * batch would be a replay of the first), and the entity must be trimmed so an
 * absent id does not create a distinct key from an empty one.
 */
export type JobScopeInput = {
    projectId?: string;
    entityId?: string;
    entityType?: string;
    batchIndex?: number;
};

export type JobScope = {
    projectId: string;
    entityId: string;
    entityType: string;
};

export function buildJobScope(input: JobScopeInput | undefined): JobScope {
    const projectId = (input?.projectId ?? "").trim();
    const entityId = (input?.entityId ?? "").trim();
    const entityType = (input?.entityType ?? "canvas_node").trim() || "canvas_node";
    if (entityId === "") {
        return { projectId, entityId: "", entityType };
    }
    // A batch shares one node, so the ordinal is appended to keep the images
    // distinct while still identifying the node they belong to.
    const suffix = input?.batchIndex === undefined ? "" : `#${input.batchIndex}`;
    return { projectId, entityId: `${entityId}${suffix}`, entityType };
}
