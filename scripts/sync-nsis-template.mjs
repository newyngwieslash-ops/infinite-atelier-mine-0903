#!/usr/bin/env node
// sync-nsis-template.mjs — RP-10.2: the NSIS template's VERSION-CONTROLLED
// source is packaging/windows/project.nsi; build/windows/installer/project.nsi
// is a BUILD INPUT that wails reads (and that directory is ignored, so the
// template cannot live there without being lost to every fresh checkout).
//
// This script copies the tracked source to the build location. It refuses
// when the build copy has been edited WITHOUT the source being updated
// (edit-the-build-input is the exact drift the source-of-truth move exists
// to prevent): a mismatch with the tracked source is a failure, not a
// silent overwrite.
//
// Usage:
//   node scripts/sync-nsis-template.mjs            # copy + verify
//   node scripts/sync-nsis-template.mjs --check    # verify only (CI)
//
// Exit 0 = build template matches the tracked source; 1 = drift or missing.

import { readFileSync, writeFileSync, mkdirSync, statSync } from "node:fs";
import { join, resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const source = join(repoRoot, "packaging", "windows", "project.nsi");
const build = join(repoRoot, "build", "windows", "installer", "project.nsi");
const check = process.argv.includes("--check");

let sourceText;
try {
    sourceText = readFileSync(source, "utf8");
} catch (err) {
    console.error(`FAIL: the tracked NSIS source ${source} is missing: ${err.message}`);
    process.exit(1);
}

let buildExists = false;
try {
    statSync(build);
    buildExists = true;
} catch { /* absent */ }
if (!buildExists) {
    if (check) {
        console.error("FAIL: build/windows/installer/project.nsi is absent; run the sync without --check before building");
        process.exit(1);
    }
    writeIt(build, sourceText);
    console.log("synced NSIS template (build copy created)");
    process.exit(0);
}

const buildText = readFileSync(build, "utf8");
if (buildText === sourceText) {
    console.log("NSIS template is in sync");
    process.exit(0);
}
if (check) {
    console.error("FAIL: build/windows/installer/project.nsi drifted from packaging/windows/project.nsi; the tracked source is the only authority");
    process.exit(1);
}
writeIt(build, sourceText);
console.log("synced NSIS template (build copy was drifted; overwritten from the tracked source)");
process.exit(0);

function writeIt(path, text) {
    mkdirSync(dirname(path), { recursive: true });
    writeFileSync(path, text);
}
