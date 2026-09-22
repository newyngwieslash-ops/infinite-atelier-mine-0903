import io

p = "scripts/gen-skill-packs.mjs"
s = io.open(p, encoding="utf-8").read()

# 1. The tool lists become the manifest's source of truth, matching what the packs now grant.
s = s.replace(
    'tools: ["story.read_events", "story.read_rules", "script.create_story_skeleton_version"]',
    'tools: ["story.read_events", "story.read_rules", "script.create_story_skeleton_version"]')
s = s.replace(
    'tools: ["script.read_story_skeleton", "script.read_adaptation_strategy", "story.read_rules", "script.create_script_version"]',
    'tools: ["script.read_story_skeleton", "script.read_adaptation_strategy", "script.read_script_version", "script.read_script_structure", "story.read_rules", "script.create_script_version", "script.create_script_structure"]')
s = s.replace(
    'tools: ["script.read_script_version", "script.read_story_skeleton", "story.read_rules"]',
    'tools: ["script.read_script_version", "script.read_script_structure", "script.read_story_skeleton", "script.read_adaptation_strategy", "story.read_rules"]')

# 2. The write and check paths: the manifest is generated, the documents are OURS once they exist.
old = '''function main() {
    const check = process.argv.includes("--check");
    let mismatched = 0;
    const files = [];
    for (const pack of PACKS) {
        files.push([join(skillsRoot, pack.name, "manifest.json"), JSON.stringify(manifestFor(pack), null, 2) + "\\n"]);
        for (const agent of pack.agents) {
            files.push([join(skillsRoot, pack.name, agent.skill), documentFor(pack, agent)]);
        }
    }
    for (const [path, content] of files) {
        if (check) {
            const current = existsSync(path) ? readFileSync(path, "utf8") : null;
            if (current !== content) {
                console.error(`FAIL: ${path} differs from what the generator produces`);
                mismatched++;
            }
            continue;
        }
        mkdirSync(dirname(path), { recursive: true });
        writeFileSync(path, content);
    }
    if (check) {
        if (mismatched > 0) {
            console.error(`FAIL: ${mismatched} skill file(s) are out of date`);
            process.exit(1);
        }
        console.log(`PASS: ${files.length} skill file(s) are current`);
        return;
    }
    console.log(`wrote ${files.length} skill file(s) under ${skillsRoot}`);
}'''
new = '''// main writes what is GENERATED and leaves alone what is AUTHORED.
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
        const manifest = JSON.stringify(manifestFor(pack), null, 2) + "\\n";
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
 * The heading is matched as a LINE rather than as a substring, so a document that MENTIONS "## Role" in
 * prose does not satisfy the check — and neither does one whose heading was renamed to "## Roles", which
 * is what a check on a substring would accept. */
function missingSections(document) {
    const headings = new Set(
        document
            .split("\\n")
            .map((line) => line.trim())
            .filter((line) => line.startsWith("## "))
            .map((line) => line.slice(3).trim()),
    );
    return SECTIONS.map(([heading]) => heading).filter((heading) => !headings.has(heading));
}'''
assert old in s, "main"
s = s.replace(old, new, 1)
io.open(p, "w", encoding="utf-8", newline="\n").write(s)
print("generator patched")
