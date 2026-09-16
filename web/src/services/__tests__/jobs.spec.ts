import assert from "node:assert/strict";
import { test } from "node:test";

/**
 * Job-status helpers that must stay in step with the Go domain set
 * (docs/adr/0004). The UI must never treat a non-terminal status as finished,
 * or a finished job would keep showing a spinner forever.
 */
import { isTerminalJobStatus, TERMINAL_JOB_STATUSES } from "../desktop/jobs";

test("terminal job statuses match the Go domain set", () => {
    const expected = ["succeeded", "remote_only", "failed", "cancelled", "orphaned"];
    assert.deepEqual([...TERMINAL_JOB_STATUSES].sort(), [...expected].sort());
});

test("isTerminalJobStatus accepts only terminal statuses", () => {
    for (const status of TERMINAL_JOB_STATUSES) {
        assert.equal(isTerminalJobStatus(status), true, `${status} should be terminal`);
    }
    const active = ["queued", "running", "waiting_remote", "downloading", "verifying", "retry_wait", "recovering"];
    for (const status of active) {
        assert.equal(isTerminalJobStatus(status), false, `${status} must not be terminal`);
    }
    // The PRD prose spelling is not a stored status, so it must not be treated
    // as terminal either.
    assert.equal(isTerminalJobStatus("awaiting_remote"), false);
    assert.equal(isTerminalJobStatus(""), false);
});

/**
 * The job scope decides the Go-side idempotency key, so these rules are
 * security-relevant behaviour rather than formatting: a wrong scope either
 * collapses distinct work into one job or makes a failed job impossible to
 * regenerate.
 */
import { buildJobScope } from "../desktop/job-scope";

test("job scope keeps distinct nodes apart", () => {
    const a = buildJobScope({ projectId: "p", entityId: "node-a" });
    const b = buildJobScope({ projectId: "p", entityId: "node-b" });
    assert.notEqual(a.entityId, b.entityId);
    assert.equal(a.projectId, "p");
});

test("job scope distinguishes the images of one batch", () => {
    const first = buildJobScope({ projectId: "p", entityId: "node-a", batchIndex: 0 });
    const second = buildJobScope({ projectId: "p", entityId: "node-a", batchIndex: 1 });
    // Without the ordinal every image in a count>1 batch would be a replay of
    // the first, so the canvas would show N copies of one result.
    assert.notEqual(first.entityId, second.entityId);
    assert.ok(first.entityId.startsWith("node-a"), "the node identity must remain recognisable");
    assert.ok(first.entityId.endsWith("#0"));
    assert.ok(second.entityId.endsWith("#1"));
});

test("job scope defaults the entity type and trims empty input", () => {
    const scope = buildJobScope(undefined);
    assert.equal(scope.entityType, "canvas_node");
    assert.equal(scope.entityId, "");
    assert.equal(scope.projectId, "");
    // A blank entity must not gain a batch suffix, otherwise "no entity" would
    // become a per-index identity.
    const blank = buildJobScope({ entityId: "   ", batchIndex: 3 });
    assert.equal(blank.entityId, "");
});

test("job scope never emits a suffix without an entity", () => {
    const scope = buildJobScope({ projectId: "p", batchIndex: 2 });
    assert.equal(scope.entityId, "");
});
