#!/usr/bin/env node
// check-release-prerequisites.mjs — RP-10.3's strict-mode environment gate.
//
// # Why this script exists
//
// `go test ./...` reports `[no test files]` for the ONNX package on a machine
// where CGO is off, because the cgo-tagged test file is excluded from the
// build — and a green "no tests" is not evidence that inference works. The
// same trap exists for FFmpeg (media tests SKIP without it), race (needs cgo
// + gcc) and NSIS (packaging). A RELEASE run must distinguish "passed" from
// "could not run": this script checks each prerequisite BEFORE the test
// phase, so a missing one is a loud FAIL in strict mode, never a green skip.
//
// Usage:
//   node scripts/check-release-prerequisites.mjs            # report mode
//   node scripts/check-release-prerequisites.mjs --strict   # release mode
//
// Exit codes: 0 = every prerequisite satisfied (strict) or report printed
// (non-strict); 1 = at least one REQUIRED prerequisite missing in strict mode.
//
// The script reads the environment; it does not modify anything.

import { execFileSync, spawnSync } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { join, resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const strict = process.argv.includes("--strict");

function run(program, args) {
    try {
        return { ok: true, out: execFileSync(program, args, { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }).trim() };
    } catch (err) {
        return { ok: false, out: err?.stderr?.toString?.() || err?.message || String(err) };
    }
}

// Version probes use each tool's OWN flag: ffmpeg/ffprobe take `-version`
// (single dash) while gcc/makensis take `--version`. A probe that guesses one
// flag for all reports a present tool as missing — the false negative this
// script exists to prevent.
const versionArgs = {
    ffmpeg: ["-version"],
    ffprobe: ["-version"],
};
function whichOnPath(program) {
    const args = versionArgs[program] || ["--version"];
    const result = spawnSync(program, args, { encoding: "utf8", shell: process.platform === "win32" });
    return result.status === 0;
}

// --- The prerequisite checks -------------------------------------------------

const checks = [];
const note = (name, state, detail) => checks.push({ name, state, detail });

// 1. GO TOOLCHAIN: must match go.mod's toolchain directive exactly. A build
//    against a different patch version is a different binary than the one the
//    SBOM and the status records describe.
const goMod = readFileSync(join(repoRoot, "go.mod"), "utf8");
const toolchainMatch = goMod.match(/^toolchain\s+(go\S+)\s*$/m);
const wantedToolchain = toolchainMatch ? toolchainMatch[1] : "";
const goVersion = run("go", ["version"]);
if (!goVersion.ok) {
    note("go toolchain", "missing", "go is not on PATH; the Go test phase cannot run at all");
} else if (wantedToolchain && !goVersion.out.includes(wantedToolchain)) {
    note("go toolchain", "fail", `go.mod requires ${wantedToolchain}, found: ${goVersion.out}`);
} else {
    note("go toolchain", "pass", goVersion.out);
}

// 2. CGO + GCC: the ONNX embedding package and `go test -race` are cgo builds.
//    Without a C compiler their tests are excluded (the `[no test files]` trap)
//    or cannot run — which strict mode must not let pass as green.
const cgoEnv = (process.env.CGO_ENABLED || "").trim();
const gcc = whichOnPath("gcc") ? "gcc" : (whichOnPath("cc") ? "cc" : "");
if (!gcc) {
    note("cgo compiler", "missing", "no gcc/cc on PATH: the ONNX (cgo) tests do not compile, and -race cannot run; `[no test files]` for onnxemb is NOT a pass");
} else if (cgoEnv === "0") {
    note("cgo compiler", "fail", `found ${gcc}, but CGO_ENABLED=0 disables cgo — enable CGO for the release test phase`);
} else {
    note("cgo compiler", "pass", `${gcc} present, CGO enabled`);
}

// 3. ONNX RUNTIME + MODEL: the local embedding adapter needs the runtime
//    library and the model/tokenizer files the user installs (they are NOT in
//    Git — docs/EMBEDDING_MODEL_DISTRIBUTION.md). Strict mode requires them so
//    the embedding tests run rather than skip.
const onnxRuntime = (process.env.IA_ONNX_RUNTIME || "").trim();
if (!onnxRuntime) {
    note("onnx runtime", "missing", "IA_ONNX_RUNTIME is not set; the embedding tests would skip, and skipping is not a pass for a release gate");
} else if (!existsSync(onnxRuntime)) {
    note("onnx runtime", "fail", `IA_ONNX_RUNTIME points at a missing file: ${onnxRuntime}`);
} else {
    note("onnx runtime", "pass", onnxRuntime);
}

// 4. FFMPEG / FFPROBE: the media engine's tests SKIP without the binaries. A
//    release whose export/compose tests never ran cannot claim playable output.
for (const program of ["ffmpeg", "ffprobe"]) {
    note(program, whichOnPath(program) ? "pass" : (strict ? "fail" : "missing"),
        whichOnPath(program) ? "on PATH" : "not on PATH: media tests would skip");
}

// 5. NSIS: packaging. Strict release mode requires the installer to be
//    buildable on this machine.
const makensis = whichOnPath("makensis");
note("makensis", makensis ? "pass" : (strict ? "fail" : "missing"),
    makensis ? "on PATH" : "not on PATH: the Windows installer cannot be built");

// 6. GO SUM FRESHNESS: the SBOM is generated from go.sum; a go.sum modified
//    after the SBOM means the checked-in document describes dependencies the
//    build no longer uses (the drift RP-10.1 exists to catch).
const goSumPath = join(repoRoot, "go.sum");
const bomPath = join(repoRoot, "sbom", "cyclonedx.json");
if (!existsSync(goSumPath) || !existsSync(bomPath)) {
    note("sbom inputs", "fail", "go.sum or sbom/cyclonedx.json is missing");
} else {
    const sumTime = goSumPath && (await import("node:fs")).statSync(goSumPath).mtimeMs;
    const bomTime = (await import("node:fs")).statSync(bomPath).mtimeMs;
    // Informational only — the authoritative check is `gen-sbom.mjs --check`.
    note("sbom inputs", "pass", `go.sum mtime ${sumTime > bomTime ? "is NEWER than" : "not newer than"} the SBOM (authoritative check: gen-sbom --check)`);
}

// --- Report -------------------------------------------------------------------

let failures = 0;
for (const check of checks) {
    const marker = check.state === "pass" ? "PASS" : check.state === "fail" ? "FAIL" : "MISSING";
    if (check.state === "fail" || (strict && check.state === "missing")) failures++;
    console.error(`${marker}: ${check.name} — ${check.detail}`);
}

if (failures > 0) {
    console.error(`\nrelease prerequisites: ${failures} unmet (strict=${strict})`);
    process.exit(1);
}
console.error(`\nrelease prerequisites: all satisfied (strict=${strict})`);
process.exit(0);
