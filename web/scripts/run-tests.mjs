// Frontend security and behaviour unit tests without adding a test framework
// dependency.
//
// The repository intentionally has no test runner. Rather than introduce a
// large new dependency, this script bundles the focused specs with the
// already-installed esbuild and runs them on Node's built-in test runner.
//
// Scope: pure, security-relevant logic (secret stripping, provider-ID
// normalization, message shaping, job status handling). DOM/binding behaviour
// is covered by Go-side binding tests, which can drive the real Wails surface.
import { mkdtempSync, readdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const webDir = resolve(fileURLToPath(new URL(".", import.meta.url)), "..");
const specDir = resolve(webDir, "src/services/__tests__");

// Bundle every spec in the directory so adding a spec file does not require
// editing this runner.
const specs = readdirSync(specDir)
    .filter((entry) => entry.endsWith(".spec.ts"))
    .sort()
    .map((entry) => join(specDir, entry));

if (specs.length === 0) {
    console.error("FAIL: no spec files found in src/services/__tests__");
    process.exit(1);
}

const outDir = mkdtempSync(join(tmpdir(), "ia-web-tests-"));
let exitCode = 0;
try {
    const esbuild = resolve(webDir, "node_modules", ".bin", process.platform === "win32" ? "esbuild.cmd" : "esbuild");
    const build = spawnSync(
        esbuild,
        [...specs, "--bundle", "--platform=node", "--format=esm", "--target=node20", `--outdir=${outDir}`, "--out-extension:.js=.mjs", "--external:node:*"],
        {
            cwd: webDir,
            stdio: "inherit",
            shell: process.platform === "win32",
        },
    );
    if (build.status !== 0) {
        exitCode = build.status ?? 1;
    } else {
        const bundles = readdirSync(outDir)
            .filter((entry) => entry.endsWith(".mjs"))
            .sort()
            .map((entry) => join(outDir, entry));
        const test = spawnSync(process.execPath, ["--test", ...bundles], { cwd: webDir, stdio: "inherit", env: { ...process.env, IA_WEB_ROOT: webDir } });
        exitCode = test.status ?? 1;
    }
} finally {
    rmSync(outDir, { recursive: true, force: true });
}
process.exit(exitCode);
