import assert from "node:assert/strict";
import { test } from "node:test";
import { readFileSync } from "node:fs";
import { join, resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

// verify-contract.test.mjs is RP-10.2's alignment contract: verify.sh and
// verify.ps1 must carry the SAME set of gates (a platform difference in gate
// semantics is how "green on my shell" stops meaning the same thing), the sh
// script must not kill strangers' processes by port, and the NSIS template
// must have a tracked source that the build syncs from.

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const sh = readFileSync(join(repoRoot, "scripts", "verify.sh"), "utf8");
const ps1 = readFileSync(join(repoRoot, "scripts", "verify.ps1"), "utf8");

const GATES_BOTH = [
    ["frontend typecheck", /frontend typecheck/],
    ["frontend tests", /frontend tests/],
    ["frontend lint", /frontend lint/],
    ["frontend production build", /frontend production build/],
    ["canvas regression", /canvas regression/],
    ["MONOFORM build", /MONOFORM source build/],
    ["Go tests", /Go tests/],
    ["Go vet", /Go vet/],
    ["Go formatting", /gofmt/],
    ["security scans", /security-scan\.mjs/],
    ["canary fixture", /gen-canary-fixture\.mjs/],
    ["hostile fixtures", /gen-malicious-fixtures\.mjs/],
    ["tool schemas", /gen-tool-schemas\.mjs/],
    ["skill packs", /gen-skill-packs\.mjs/],
    ["SBOM", /gen-sbom\.mjs/],
    ["release prerequisites", /check-release-prerequisites\.mjs/],
    ["NSIS template sync", /sync-nsis-template\.mjs/],
];

test("every shared gate appears in BOTH verify scripts", () => {
    for (const [name, pattern] of GATES_BOTH) {
        assert.match(sh, pattern, `verify.sh is missing the ${name} gate`);
        assert.match(ps1, pattern, `verify.ps1 is missing the ${name} gate`);
    }
});

test("verify.sh cleanup is no longer kill-by-port", () => {
    // The old kill_stray_dev_servers killed whatever listened on 5173 —
    // a user's own dev server included. The cleanup now only restores the
    // embed placeholder.
    assert.doesNotMatch(sh, /fuser -k|lsof -ti/);
    assert.match(sh, /restore_embed_placeholder/);
});

test("strict mode machinery exists in the sh script", () => {
    assert.match(sh, /STRICT_VERIFY/);
    assert.match(sh, /strict_fail/);
});

test("the NSIS template has a tracked source that syncs before build", () => {
    const source = readFileSync(join(repoRoot, "packaging", "windows", "project.nsi"), "utf8");
    assert.match(source, /THIRD_PARTY_NOTICES/, "the tracked template must keep T30's declaration files");
    assert.match(source, /sbom/, "the tracked template must keep the SBOM files");
    // The sync script exists and refuses drift in check mode.
    const sync = readFileSync(join(repoRoot, "scripts", "sync-nsis-template.mjs"), "utf8");
    assert.match(sync, /--check/);
    assert.match(sync, /drifted/);
});

test("playwright e2e binds loopback explicitly", () => {
    const config = readFileSync(join(repoRoot, "web", "playwright.config.ts"), "utf8");
    assert.match(config, /--host 127\.0\.0\.1/);
});
