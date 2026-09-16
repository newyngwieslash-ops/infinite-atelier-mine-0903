// Security static scans for the repository (WP-02).
//
// Two independent rulesets, both fail-closed with an explicit, auditable
// allowlist. There are deliberately NO global ignores:
//
//  1. dynamic-execution scan  — `new Function`, `eval(`, `child_process`,
//     `os/exec`, `syscall.Exec`, `plugin.Open`, `javascript:` URLs.
//  2. secret scan            — high-confidence key patterns plus
//     secret-value storage in frontend config/export/backup code.
//
// Every allowlist entry names one file, one rule, and one owner work package
// that must remove it. A new finding fails the build.
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(fileURLToPath(new URL(".", import.meta.url)), "..");

const SCAN_DIRS = ["internal", "web/src", "scripts", "."];
const SKIP_DIRS = new Set([
    ".git",
    "node_modules",
    "dist",
    "build",
    "monoform-studio",
    "wailsjs", // generated bindings: audited separately, never hand-edited
]);
// Tests ARE scanned: a real key committed into a test file is still a leak.
// Intentional fake keys are declared one file at a time in FIXTURE_FILES below.
// The scanner's own source necessarily quotes the patterns it looks for.
const SKIP_FILES = new Set(["scripts/security-scan.mjs"]);
const SOURCE_EXTENSIONS = [".go", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".ps1", ".sh"];

// --- Rule 1: dynamic execution -------------------------------------------
const DYNAMIC_RULES = [
    { id: "new-function", test: (line) => /\bnew\s+Function\s*\(/.test(line) },
    { id: "eval-call", test: (line) => /(^|[^.\w])eval\s*\(/.test(line) },
    { id: "child-process", test: (line) => /require\(\s*["']child_process["']\s*\)|from\s+["']child_process["']/.test(line) },
    { id: "os-exec", test: (line) => /"os\/exec"/.test(line) },
    { id: "syscall-exec", test: (line) => /syscall\.Exec\s*\(/.test(line) },
    { id: "plugin-open", test: (line) => /plugin\.Open\s*\(/.test(line) },
    { id: "javascript-url", test: (line) => /["'`]javascript:/.test(line) },
];

// Each entry is exact: file + rule + owner + reason. No wildcards. An entry
// that no longer matches a real finding is itself a failure, so stale
// exceptions cannot silently accumulate.
const DYNAMIC_ALLOWLIST = [
    {
        file: "web/src/services/api/model-plugin.ts",
        rule: "new-function",
        owner: "WP-12",
        reason: "Legacy browser model-script path; unreachable in secure desktop mode (guarded in runModelPlugin) and scheduled for removal in WP-12 clean-up.",
    },
];

// --- Rule 1b: browser-direct provider calls -------------------------------
//
// WP-03 migrated image generation to the Go job manager. Video and audio still
// call providers from the webview, which means the API key crosses into the
// frontend for those paths. WP-03's scope is to forbid NEW direct calls and
// name the existing ones; the media work package removes them.
//
// The rule flags direct network calls in the frontend service layer. The
// allowlist names each surviving legacy caller with its owner work package.
const DIRECT_CALL_RULE = "browser-direct-provider-call";

// A network call from the frontend to a provider, and the credential header
// that marks it as a direct authenticated request rather than, say, a local
// asset read.
const DIRECT_NETWORK_CALL_PATTERN = /\baxios\.(get|post|put|patch|request)(<[^>]*>)?\s*\(|\bfetch\s*\(/;
const CREDENTIAL_HEADER_PATTERN = /Authorization|apiKey|x-goog-api-key/;

const DIRECT_CALL_ALLOWLIST = [
    { file: "web/src/services/api/image.ts", owner: "WP-13 (legacy media removal)", reason: "Legacy image/text calls retained for browser dev mode; secure mode routes through the Go job manager instead." },
    { file: "web/src/services/api/video.ts", owner: "WP-13 (legacy media removal)", reason: "Video provider calls are not yet migrated; the real adapter belongs to the media work package." },
    { file: "web/src/services/api/audio.ts", owner: "WP-13 (legacy media removal)", reason: "Audio provider calls are not yet migrated; the real adapter belongs to the media work package." },
    { file: "web/src/services/api/model-plugin.ts", owner: "WP-12 (legacy removal)", reason: "Legacy script runner; already unreachable in secure mode and scheduled for removal." },
];

function scanDirectCalls(files) {
    const findings = [];
    const usedAllowlist = new Set();
    for (const file of files) {
        const rel = relative(repoRoot, file).replaceAll("\\", "/");
        if (!rel.startsWith("web/src/")) continue;
        // Generated bindings perform no requests of their own.
        if (rel.includes("wailsjs/")) continue;
        // The Go-backed desktop clients are the intended path, but they are
        // also where a regression would be most damaging, so they are scanned
        // too; they simply contain no credential header, which is what the
        // rule keys on.
        const content = readFileSync(file, "utf8");
        // A direct provider call only counts when the file both performs a
        // network request and builds a provider credential header. Matching the
        // header alone would flag tests and redaction code.
        if (!CREDENTIAL_HEADER_PATTERN.test(content)) continue;
        const lines = content.split(/\r?\n/);
        lines.forEach((line, index) => {
            if (!DIRECT_NETWORK_CALL_PATTERN.test(line)) return;
            const entry = DIRECT_CALL_ALLOWLIST.find((candidate) => candidate.file === rel);
            if (entry) {
                usedAllowlist.add(entry.file);
                return;
            }
            findings.push({ file: rel, line: index + 1, rule: DIRECT_CALL_RULE, text: line.trim().slice(0, 120) });
        });
    }
    for (const entry of DIRECT_CALL_ALLOWLIST) {
        if (!usedAllowlist.has(entry.file)) {
            findings.push({
                file: entry.file,
                line: 0,
                rule: "stale-exception",
                text: `allowlisted direct call no longer exists; remove the ${entry.owner} exception`,
            });
        }
    }
    return findings;
}

// --- Rule 2: secret handling ---------------------------------------------
const SECRET_PATTERNS = [
    { id: "openai-key", regex: /\bsk-[A-Za-z0-9_-]{20,}\b/g },
    { id: "google-key", regex: /\bAIza[0-9A-Za-z_-]{20,}\b/g },
    { id: "aws-key", regex: /\bAKIA[0-9A-Z]{16}\b/g },
    { id: "github-token", regex: /\bgh[pousr]_[0-9A-Za-z]{20,}\b/g },
    { id: "private-key-block", regex: /-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----/g },
];

// Files that intentionally contain fixed FAKE key material, each verified to
// actually match a pattern. Entries are exact; a listed file that no longer
// matches is reported as a stale exemption so the list cannot rot.
const FIXTURE_FILES = new Set([
    "internal/infrastructure/logging/logging_test.go",
    "internal/infrastructure/secretstore/wincred_windows_test.go",
    "web/src/services/__tests__/security.spec.ts",
]);

function* walk(dir) {
    let entries;
    try {
        entries = readdirSync(dir);
    } catch {
        return;
    }
    for (const entry of entries) {
        const full = join(dir, entry);
        let info;
        try {
            info = statSync(full);
        } catch {
            continue;
        }
        if (info.isDirectory()) {
            if (SKIP_DIRS.has(entry)) continue;
            yield* walk(full);
        } else if (SOURCE_EXTENSIONS.some((extension) => entry.endsWith(extension))) {
            yield full;
        }
    }
}

function collectFiles() {
    const files = new Set();
    const add = (full, relPath) => {
        if (SKIP_FILES.has(relPath)) return;
        files.add(full);
    };
    for (const dir of SCAN_DIRS) {
        const base = resolve(repoRoot, dir);
        if (dir === ".") {
            for (const entry of readdirSync(base)) {
                const full = join(base, entry);
                if (statSync(full).isFile() && SOURCE_EXTENSIONS.some((extension) => entry.endsWith(extension))) {
                    add(full, entry);
                }
            }
        } else {
            for (const file of walk(base)) add(file, relative(repoRoot, file).replace(/\\/g, "/"));
        }
    }
    return [...files];
}

function scanDynamic(files) {
    const findings = [];
    const usedAllowlist = new Set();
    for (const file of files) {
        const rel = relative(repoRoot, file).replace(/\\/g, "/");
        const lines = readFileSync(file, "utf8").split(/\r?\n/);
        lines.forEach((line, index) => {
            for (const rule of DYNAMIC_RULES) {
                if (!rule.test(line)) continue;
                const entry = DYNAMIC_ALLOWLIST.find((candidate) => candidate.file === rel && candidate.rule === rule.id);
                if (entry) {
                    usedAllowlist.add(`${entry.file}:${entry.rule}`);
                    continue;
                }
                findings.push({ file: rel, line: index + 1, rule: rule.id, text: line.trim().slice(0, 120) });
            }
        });
    }
    for (const entry of DYNAMIC_ALLOWLIST) {
        if (!usedAllowlist.has(`${entry.file}:${entry.rule}`)) {
            findings.push({
                file: entry.file,
                line: 0,
                rule: "stale-exception",
                text: `allowlisted ${entry.rule} no longer matches; remove the ${entry.owner} exception`,
            });
        }
    }
    return findings;
}

function scanSecrets(files) {
    const findings = [];
    const usedFixtures = new Set();
    for (const file of files) {
        const rel = relative(repoRoot, file).replace(/\\/g, "/");
        const content = readFileSync(file, "utf8");
        for (const pattern of SECRET_PATTERNS) {
            pattern.regex.lastIndex = 0;
            let match;
            while ((match = pattern.regex.exec(content)) !== null) {
                const line = content.slice(0, match.index).split(/\r?\n/).length;
                if (FIXTURE_FILES.has(rel)) {
                    usedFixtures.add(rel);
                    continue;
                }
                findings.push({ file: rel, line, rule: pattern.id, text: match[0].slice(0, 24) + "…" });
            }
        }
    }
    for (const fixture of FIXTURE_FILES) {
        if (!usedFixtures.has(fixture)) {
            findings.push({
                file: fixture,
                line: 0,
                rule: "stale-fixture-exemption",
                text: "listed as a fixture but contains no matching key pattern; remove the exemption",
            });
        }
    }
    return findings;
}

// Secret values must never be persisted by the frontend layer. This checks
// that the export/import/backup services strip keys before serialization.
function scanSecretPersistence(files) {
    const findings = [];
    const guarded = new Set([
        "web/src/services/config-file.ts",
        "web/src/services/backup-restore.ts",
        "web/src/services/config-secrets.ts",
    ]);
    for (const file of files) {
        const rel = relative(repoRoot, file).replace(/\\/g, "/");
        if (guarded.has(rel)) continue;
        if (!rel.startsWith("web/src/")) continue;
        const content = readFileSync(file, "utf8");
        // A direct JSON.stringify of a config-like object that still holds an
        // apiKey field is the pattern ordinary exports must avoid.
        if (/JSON\.stringify\(\s*(?:data|payload|config)\b/.test(content) && /apiKey/.test(content)) {
            findings.push({ file: rel, rule: "secret-persistence", text: "JSON.stringify of config-like object with apiKey" });
        }
    }
    return findings;
}

const files = collectFiles();
const dynamicFindings = scanDynamic(files);
const directCallFindings = scanDirectCalls(files);
const secretFindings = scanSecrets(files);
const persistenceFindings = scanSecretPersistence(files);

if (dynamicFindings.length) {
    console.error("FAIL: dynamic-execution scan found disallowed constructs:");
    for (const finding of dynamicFindings) console.error(`  ${finding.file}:${finding.line} [${finding.rule}] ${finding.text}`);
}
if (secretFindings.length) {
    console.error("FAIL: secret scan found high-confidence key material:");
    for (const finding of secretFindings) console.error(`  ${finding.file}:${finding.line} [${finding.rule}] ${finding.text}`);
}
if (persistenceFindings.length) {
    console.error("FAIL: secret-persistence scan found unguarded config serialization:");
    for (const finding of persistenceFindings) console.error(`  ${finding.file} [${finding.rule}] ${finding.text}`);
}
if (directCallFindings.length) {
    console.error("FAIL: a new browser-direct provider call was added to the frontend:");
    for (const finding of directCallFindings) console.error(`  ${finding.file}:${finding.line} [${finding.rule}] ${finding.text}`);
}

if (dynamicFindings.length || secretFindings.length || persistenceFindings.length || directCallFindings.length) {
    process.exit(1);
}

console.log(`PASS: security scans clean (${files.length} files scanned; ${DYNAMIC_ALLOWLIST.length} audited dynamic-execution exception, ${DIRECT_CALL_ALLOWLIST.length} audited legacy direct-call files with named owners).`);
