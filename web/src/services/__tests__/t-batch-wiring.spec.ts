import assert from "node:assert/strict";
import { test } from "node:test";
import fs from "node:fs";
import path from "node:path";

/**
 * T-batch audit remediation surface assertions (T02/T03/T05/T09/T18).
 *
 * These assert the wiring CONTRACT of the new surfaces — that the frontend
 * services call the right binding methods and probe availability before use —
 * by reading the shipped source, which is the strongest assertion a no-DOM
 * test can make about Wails IPC wiring. Each assertion maps to an audit
 * requirement that a silent rename or drift would break.
 */

// The test runner bundles specs into a temp dir, so __dirname is unusable.
// Resolve the repo root from the module URL: <tmp>/x.mjs -> web/src/services/
// __tests__/ -> walk up to web/, then one more to the repo.
import { fileURLToPath } from "node:url";
const here = path.dirname(fileURLToPath(import.meta.url));
// Bundled path is <temp>/t-batch-wiring.spec.mjs — no directory relationship
// to the repo survives. Instead locate via the process working directory,
// which run-tests.mjs keeps at web/.
const repoRoot = path.resolve(process.cwd(), "..");
const read = (rel: string) => fs.readFileSync(path.join(repoRoot, rel), "utf-8");

test("T02: video collection probes availability and calls the right binding", () => {
    const src = read("web/src/services/desktop/drama.ts");
    assert.match(src, /export function isVideoCollectionAvailable\(\)/);
    assert.match(src, /CollectVideoJobResults/);
    // The probe must gate on the binding method existing.
    assert.match(src, /typeof drama\?\.CollectVideoJobResults === "function"/);
});

test("T02: video-view gates adoption controls on collection availability", () => {
    const src = read("web/src/components/studio/video-view.tsx");
    // Collect button only rendered when the command exists.
    assert.match(src, /isVideoCollectionAvailable\(\) && row\.status === "succeeded"/);
    // Adopt writes a candidate panel version then runs the approval switch.
    assert.match(src, /createPanelVersion\(/);
    assert.match(src, /approvePanelImage\(/);
    // Candidate list travels whole — §9.5 requires the approved one among them.
    assert.match(src, /candidateVersionIds: takes\.map\(\(take\) => take\.id\)/);
});

test("T03: effect jobs are submitted through the dedicated command, not TTS", () => {
    const timeline = read("web/src/components/studio/timeline-view.tsx");
    // The TTS masquerade is gone: acceptEffect uses submitEffectJob.
    assert.match(timeline, /submitEffectJob\(/);
    assert.match(timeline, /description: matched/);
    // The old field that carried the effect word as TTS text must not reappear
    // in the effect path.
    assert.doesNotMatch(timeline, /dialogueLineId: shot\.shotId/);
    const jobs = read("web/src/services/desktop/jobs.ts");
    assert.match(jobs, /SubmitEffectJob/);
});

test("T03: audio section lists both audio capability job types", () => {
    const audio = read("web/src/components/studio/audio-view.tsx");
    assert.match(audio, /AUDIO_JOB_TYPES = \["audio_generation", "effect_generation"\]/);
});

test("T05: track editor writes and reads the placement document", () => {
    const audio = read("web/src/components/studio/audio-view.tsx");
    assert.match(audio, /setUsageParams\(/);
    assert.match(audio, /listUsagesOfConsumer\("shot", selectedShotId\)/);
    const drama = read("web/src/services/desktop/drama.ts");
    assert.match(drama, /SetUsageParams/);
    assert.match(drama, /ListUsagesOfConsumer/);
});

test("T09: agent management probes and calls SetAgentEnabled", () => {
    const agents = read("web/src/services/desktop/agents.ts");
    assert.match(agents, /SetAgentEnabled/);
    assert.match(agents, /typeof binding\["SetAgentEnabled"\] !== "function"/);
    // Skill document read-back is also exposed, read-only.
    assert.match(agents, /AgentSkillDocument/);
    const center = read("web/src/components/studio/agent-center.tsx");
    assert.match(center, /checked=\{agent\.enabled\}/);
});

test("T18: health snapshot carries the embedding state", () => {
    const health = read("internal/application/health/service.go".replace(/\//g, path.sep));
    assert.match(health, /Embedding\s+string `json:"embedding"`/);
    assert.match(health, /EmbeddingUnavailable/);
});

test("T27: the perf spec drives the real canvas surfaces", () => {
    const spec = read("web/e2e/render-perf-t27.spec.ts");
    // Injection through the real toolbar, measurement via the browser clock.
    assert.match(spec, /data-tool='tool-text'/);
    assert.match(spec, /performance\.now\(\)/);
    assert.match(spec, /data-connection-id/);
});
