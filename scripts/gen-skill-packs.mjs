#!/usr/bin/env node
// Generates the built-in Agent Pack skeletons under skills/.
//
// WP-07 scope item 16 asks for "Script/Production 空 Skill 骨架，不实现业务内容" —
// empty skeletons, no business content. So each document carries the thirteen
// sections AGENT_CONTRACTS section 4.3 requires and a one-line statement of what
// belongs in each, and nothing else. The later work packages (WP-08 for the script
// pack, WP-11 for the remaining production stages) fill them in.
//
// The section list is written here ONCE, and internal/application/skill also
// requires it. A test compares the generated documents against the loader's own
// list, so the two cannot drift: a document missing a section the loader demands
// would make the built-in packs fail to load.
//
// Usage: node scripts/gen-skill-packs.mjs
//        node scripts/gen-skill-packs.mjs --check

import { readFileSync, writeFileSync, mkdirSync, existsSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(fileURLToPath(new URL(".", import.meta.url)), "..");
const skillsRoot = join(repoRoot, "skills");

/** The thirteen sections of AGENT_CONTRACTS section 4.3, in its order. */
const SECTIONS = [
    ["Role", "Which layer this agent is, and the one narrow job it does. One or two sentences: a skill that describes several jobs is several agents."],
    ["Goal", "What a successful invocation produces. Name the artifact, so \"success\" is checkable against the database rather than against the agent's own account of it."],
    ["Trusted Context", "What may be treated as instruction. Runtime policy, this skill, tool schemas, workflow state and approved facts — the first four layers of section 5."],
    ["Untrusted Input", "What must be treated as data. Imported documents, provider text, asset metadata and model output. Say explicitly which of them this agent reads."],
    ["Workflow State", "The stages this agent reads and the state it may run in. The workflow engine decides when this agent runs; this section says what it may assume."],
    ["Input Contract", "The request schema this agent is given, and what each field means. Name the schema file."],
    ["Allowed Tools", "The tool keys this agent may call, exactly as the manifest lists them. A skill may not name a tool the manifest does not grant."],
    ["Required Procedure", "The ordered steps of the work. Keep it to what changes the outcome: a procedure that restates the schema is noise."],
    ["Domain Constraints", "Rules this stage must respect. Locked versions and approved facts belong here."],
    ["Quality Rules", "What a reviewer checks. These become the findings a supervisor reports, so each should be observable in the artifact."],
    ["Failure Conditions", "When to stop and say so rather than continue. A stage that cannot proceed must fail rather than produce a partial artifact."],
    ["Output Contract", "The result schema, and the rule that success is what the database shows rather than what this document claims."],
    ["Examples", "One worked input and its expected output shape. Examples are prompt material, so they must not contain real project data."],
];

/** The packs and their agents, from AGENT_CONTRACTS section 19. */
const PACKS = [
    {
        name: "script",
        agents: [
            { key: "script.decision", layer: "decision", skill: "decision.md", input: "schemas/agent/decision-request.v1.json", output: "schemas/agent/decision-result.v1.json", maxToolCalls: 6, timeoutSeconds: 180, tools: ["workflow.read_state", "workflow.request_user_gate", "memory.deep_recall"] },
            { key: "script.execution.event_extraction", layer: "execution", skill: "execution/event_extraction.md", input: "schemas/agent/execution-request.v1.json", output: "schemas/agent/event_extraction.v1.json", maxToolCalls: 8, timeoutSeconds: 300, tools: ["story.read_chapter_text", "story.read_events"] },
            { key: "script.execution.story_skeleton", layer: "execution", skill: "execution/story_skeleton.md", input: "schemas/agent/execution-request.v1.json", output: "schemas/agent/execution-result.v1.json", maxToolCalls: 8, timeoutSeconds: 300, tools: ["story.read_events", "story.read_rules", "script.create_story_skeleton_version"] },
            { key: "script.execution.adaptation_strategy", layer: "execution", skill: "execution/adaptation_strategy.md", input: "schemas/agent/execution-request.v1.json", output: "schemas/agent/execution-result.v1.json", maxToolCalls: 8, timeoutSeconds: 300, tools: ["story.read_events", "story.read_rules", "script.read_story_skeleton", "script.create_adaptation_strategy_version"] },
            { key: "script.execution.script_generation", layer: "execution", skill: "execution/script_generation.md", input: "schemas/agent/execution-request.v1.json", output: "schemas/agent/execution-result.v1.json", maxToolCalls: 8, timeoutSeconds: 300, tools: ["script.read_story_skeleton", "script.read_adaptation_strategy", "script.read_script_version", "script.read_script_structure", "story.read_rules", "script.create_script_version", "script.create_script_structure"] },
            { key: "script.supervision.story_skeleton", layer: "supervision", skill: "supervision/story_skeleton.md", input: "schemas/agent/supervision-request.v1.json", output: "schemas/agent/review-report.v1.json", maxToolCalls: 12, timeoutSeconds: 300, tools: ["story.read_events", "story.read_rules", "script.read_story_skeleton"] },
            { key: "script.supervision.adaptation_strategy", layer: "supervision", skill: "supervision/adaptation_strategy.md", input: "schemas/agent/supervision-request.v1.json", output: "schemas/agent/review-report.v1.json", maxToolCalls: 12, timeoutSeconds: 300, tools: ["story.read_events", "story.read_rules", "script.read_adaptation_strategy"] },
            { key: "script.supervision.script", layer: "supervision", skill: "supervision/script.md", input: "schemas/agent/supervision-request.v1.json", output: "schemas/agent/review-report.v1.json", maxToolCalls: 12, timeoutSeconds: 300, tools: ["script.read_script_version", "script.read_script_structure", "script.read_story_skeleton", "script.read_adaptation_strategy", "story.read_rules"] },
        ],
    },
    {
        name: "production",
        agents: [
            { key: "production.decision", layer: "decision", skill: "decision.md", input: "schemas/agent/decision-request.v1.json", output: "schemas/agent/decision-result.v1.json", maxToolCalls: 6, timeoutSeconds: 180, tools: ["workflow.read_state", "workflow.request_user_gate", "memory.deep_recall"] },
            { key: "production.execution.director_plan", layer: "execution", skill: "execution/director_plan.md", input: "schemas/agent/execution-request.v1.json", output: "schemas/agent/execution-result.v1.json", maxToolCalls: 10, timeoutSeconds: 300, tools: ["script.read_script_version", "script.read_shots", "storyboard.read_director_plan", "storyboard.create_director_plan_version"] },
            { key: "production.execution.asset_analysis", layer: "execution", skill: "execution/asset_analysis.md", input: "schemas/agent/execution-request.v1.json", output: "schemas/agent/execution-result.v1.json", maxToolCalls: 12, timeoutSeconds: 300, tools: ["story.read_events", "asset.read_approved_assets", "asset.read_gap_report", "asset.create_gap_report"] },
            { key: "production.execution.asset_generation_plan", layer: "execution", skill: "execution/asset_generation_plan.md", input: "schemas/agent/execution-request.v1.json", output: "schemas/agent/execution-result.v1.json", maxToolCalls: 8, timeoutSeconds: 300, tools: ["asset.read_approved_assets", "asset.create_candidate_version"] },
            { key: "production.execution.storyboard_table", layer: "execution", skill: "execution/storyboard_table.md", input: "schemas/agent/execution-request.v1.json", output: "schemas/agent/execution-result.v1.json", maxToolCalls: 12, timeoutSeconds: 300, tools: ["script.read_script_version", "script.read_shots", "storyboard.read_director_plan", "asset.read_approved_assets", "asset.read_gap_report", "storyboard.create_storyboard_version"] },
            { key: "production.execution.storyboard_panel", layer: "execution", skill: "execution/storyboard_panel.md", input: "schemas/agent/execution-request.v1.json", output: "schemas/agent/execution-result.v1.json", maxToolCalls: 12, timeoutSeconds: 300, tools: ["storyboard.read_storyboard", "asset.read_approved_assets", "asset.read_gap_report", "storyboard.create_storyboard_panel_version"] },
            { key: "production.supervision.director_plan", layer: "supervision", skill: "supervision/director_plan.md", input: "schemas/agent/supervision-request.v1.json", output: "schemas/agent/review-report.v1.json", maxToolCalls: 14, timeoutSeconds: 300, tools: ["script.read_script_version", "script.read_shots", "storyboard.read_director_plan"] },
            { key: "production.supervision.storyboard_table", layer: "supervision", skill: "supervision/storyboard_table.md", input: "schemas/agent/supervision-request.v1.json", output: "schemas/agent/review-report.v1.json", maxToolCalls: 16, timeoutSeconds: 300, tools: ["storyboard.read_storyboard", "script.read_script_version", "script.read_shots", "asset.read_approved_assets", "asset.read_gap_report"] },
            { key: "production.supervision.storyboard_panel", layer: "supervision", skill: "supervision/storyboard_panel.md", input: "schemas/agent/supervision-request.v1.json", output: "schemas/agent/review-report.v1.json", maxToolCalls: 16, timeoutSeconds: 300, tools: ["storyboard.read_storyboard", "script.read_shots", "asset.read_approved_assets", "asset.read_gap_report"] },
        ],
    },
];

/** One skeleton document. */
function documentFor(pack, agent) {
    const lines = [
        `# ${pack.name}/${agent.key}`,
        "",
        "<!-- Generated by scripts/gen-skill-packs.mjs. WP-0X fills this in. -->",
        "<!-- The sections below are the ones AGENT_CONTRACTS section 4.3 requires. -->",
        "",
    ];
    for (const [heading, guidance] of SECTIONS) {
        lines.push(`# ${heading}`, "", guidance, "");
    }
    return lines.join("\n");
}

function manifestFor(pack) {
    return {
        apiVersion: "atelier.agent/v1",
        kind: "AgentPack",
        metadata: { name: pack.name, version: "1.0.0" },
        agents: pack.agents.map((agent) => ({
            key: agent.key,
            layer: agent.layer,
            skill: agent.skill,
            inputSchema: agent.input,
            outputSchema: agent.output,
            allowedTools: agent.tools,
            limits: { maxToolCalls: agent.maxToolCalls, timeoutSeconds: agent.timeoutSeconds },
        })),
    };
}

// main writes what is GENERATED and leaves alone what is AUTHORED.
//
// THE TWO KINDS OF FILE ARE TREATED DIFFERENTLY, and the difference is the whole point of this function:
//
//   - A MANIFEST is generated. It is a mechanical projection of the table above — keys, layers, schemas,
//     grants, limits — and a hand edit to one would be reverted by the next run, so comparing it byte for
//     byte in `--check` is what keeps the table and the file from drifting.
//   - A SKILL DOCUMENT is AUTHORED. Section 4.3's thirteen sections are guidance for a writer, not
//     content a generator can produce: the first version of this script wrote them out as headings with
//     the guidance beneath and a "WP-0X fills this in" marker, and running it again would have erased
//     every real document. So a document is written only when it is MISSING, and `--check` asks only
//     whether it exists and carries the thirteen headings.
//
// The check is deliberately weaker than byte equality for documents, and that is not a concession: the
// thing worth failing a build over is a document that lost a required section, not one whose prose was
// improved. A byte comparison would make every edit to a skill a change to this script.
function main() {
    const check = process.argv.includes("--check");
    let problems = 0;
    let written = 0;
    let kept = 0;
    for (const pack of PACKS) {
        const manifestPath = join(skillsRoot, pack.name, "manifest.json");
        const manifest = JSON.stringify(manifestFor(pack), null, 2) + "\n";
        if (check) {
            const current = existsSync(manifestPath) ? readFileSync(manifestPath, "utf8") : null;
            if (current !== manifest) {
                console.error(`FAIL: ${manifestPath} differs from what the generator produces`);
                problems++;
            }
        } else {
            mkdirSync(dirname(manifestPath), { recursive: true });
            writeFileSync(manifestPath, manifest);
        }
        for (const agent of pack.agents) {
            const path = join(skillsRoot, pack.name, agent.skill);
            if (check) {
                const current = existsSync(path) ? readFileSync(path, "utf8") : null;
                if (current === null) {
                    console.error(`FAIL: ${path} is missing`);
                    problems++;
                    continue;
                }
                const missing = missingSections(current);
                if (missing.length > 0) {
                    console.error(`FAIL: ${path} is missing section(s): ${missing.join(", ")}`);
                    problems++;
                }
                continue;
            }
            if (existsSync(path)) {
                // AN AUTHORED DOCUMENT IS NEVER OVERWRITTEN. This is the line that makes the eight real
                // skills possible: without it, running the generator to refresh a manifest would wipe
                // them.
                kept++;
                continue;
            }
            mkdirSync(dirname(path), { recursive: true });
            writeFileSync(path, documentFor(pack, agent));
            written++;
        }
    }
    if (check) {
        if (problems > 0) {
            console.error(`FAIL: ${problems} skill file(s) are out of date`);
            process.exit(1);
        }
        console.log(`PASS: every manifest matches the table and every skill carries section 4.3's sections`);
        return;
    }
    console.log(`wrote every manifest; kept ${kept} authored skill document(s); wrote ${written} skeleton(s)`);
}

/** missingSections returns the section headings a document does not carry.
 *
 * The heading is matched as a LINE rather than as a substring, so a document that MENTIONS "# Role" in
 * prose does not satisfy the check — and neither does one whose heading was renamed to "# Roles", which
 * is what a check on a substring would accept.
 *
 * The level is a single `#`, because that is what this generator's own skeleton writes and what the
 * authored documents follow. The first version of this check looked for `##` and reported EVERY file as
 * missing all thirteen sections, including the ones written correctly — and a check that fails everything
 * is indistinguishable from one that works, which is why this was worth fixing rather than relaxing. */
function missingSections(document) {
    const headings = new Set(
        document
            .split("\n")
            .map((line) => line.trim())
            .filter((line) => line.startsWith("# ") && !line.startsWith("## "))
            .map((line) => line.slice(2).trim()),
    );
    return SECTIONS.map(([heading]) => heading).filter((heading) => !headings.has(heading));
}

main();
