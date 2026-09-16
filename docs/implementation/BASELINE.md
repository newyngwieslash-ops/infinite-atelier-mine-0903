# WP-00 Repository Baseline

> Captured: 2026-09-04 (Asia/Shanghai)
> Work package: WP-00 — 仓库审计、基线与实施契约
> Source commit: `a243891455ec17687dd54b5ac90d3bd64478a1a1` on `main` tracking `origin/main`

## 1. Scope and method

This baseline was taken before product-code changes. It covers the checked-out repository, current toolchain, lockfiles, existing build commands, local startup, MONOFORM source build, and known failures. It does not contact a real Provider, alter user data, introduce Wails/Go code, or start WP-01.

The required specification set was read in the repository-mandated order: `PRD.md`, `AGENTS.md`, `docs/implementation/STATUS.md`, the WP-00 section of `docs/ROADMAP.md`, `docs/ARCHITECTURE.md`, `docs/DOMAIN_MODEL.md`, `docs/AGENT_CONTRACTS.md`, `docs/SECURITY.md`, `docs/ACCEPTANCE.md`, both reference analyses, then repository source/configuration/CI.

## 2. Git baseline

Initial command: `git status --short --branch`.

```text
## main...origin/main
?? AGENTS.md
?? CODEX_FIRST_RUN_PROMPT.txt
?? CODEX_MASTER_PROMPT(1) (1).md
?? CODEX_MASTER_PROMPT(1).md
?? CODEX_MASTER_PROMPT.md
?? PRD.md
?? SHA256SUMS.txt
?? START_HERE.md
?? docs/
```

There were no staged changes and no tracked-file modifications. The listed untracked files are the user-supplied specification pack and were preserved. Git also reported that the per-user global ignore file under `C:\Users\Administrator\.config\git\ignore` could not be read; repository-local status still completed.

## 3. Host and tools

| Item | Observed value | Notes |
|---|---|---|
| OS | Microsoft Windows `10.0.22631`, x64 | Build 22631 corresponds to the Windows 11 release family. |
| PowerShell | `7.6.5` | Baseline shell. |
| Git | `2.48.1.windows.1` | Current branch `main`. |
| Node.js | `v24.13.0` | Newer than README minimum (20.19+ or 22.12+). |
| npm | `11.6.2` | Selected package manager because both applications use `package-lock.json`. |
| pnpm | `10.30.3` | Installed globally, but no `pnpm-lock.yaml`; not used. |
| Yarn | unavailable | Not used. |
| Go | `go1.24.1 windows/amd64` | Version printed, followed by a host permission error creating the Go telemetry upload token. No `go.mod` exists. |
| Wails | unavailable | Expected at WP-00; Wails is not yet introduced. |
| ripgrep | unusable | The WinGet execution alias was broken; audit searches fell back to `git grep`, `git ls-files`, and PowerShell. |

## 4. Package and lockfile baseline

The product frontend is `web/package.json` (`infinite-atelier@1.0.0`) with npm lockfile v3. Locked core versions include React/React DOM `19.2.5`, Vite `7.3.6`, TypeScript `5.9.3`, and `@vitejs/plugin-react` `5.2.0`.

MONOFORM is a separate package at `web/monoform-studio/package.json` (`monoform-previs-studio@0.6.0`) with its own npm lockfile v3. Its lock resolves React `19.2.8` and Vite `8.2.0`. It is not declared as an npm workspace.

## 5. Commands and results

| Command | Result | Evidence / interpretation |
|---|---|---|
| `npm ci --legacy-peer-deps` (`web`) | **FAIL** | Main `package.json` and lockfile are out of sync. npm reported missing optional platform packages including Tailwind Oxide, esbuild, Rollup, and Lightning CSS variants. This is the clean-lock reproducibility defect; it is not hidden by the fallback install. |
| `npm install --legacy-peer-deps --include=optional --package-lock=false --no-audit --no-fund` (`web`) | **PASS** | Completed after external-cache permission was allowed: `changed 728 packages in 5m`. `package-lock.json` was not modified. |
| `npm run typecheck` (`web`) | **PASS** | `tsc --noEmit`, exit 0. |
| `npm run format:check` (`web`) | **FAIL** | Prettier reported style issues in 147 existing files. No formatting rewrite was performed because it is outside WP-00. |
| `npm run build` (`web`) | **PASS with warnings** | Vite transformed 7,384 modules. Main JS was 2,573.46 kB (832.60 kB gzip); Rollup warned about a chunk over 500 kB and ineffective dynamic imports for two statically imported stores/services. |
| `npm ci --no-audit --no-fund` (`web/monoform-studio`) | **PASS after permission retry** | First sandboxed attempt failed writing the user npm cache; the same lockfile install succeeded outside that restriction with 92 packages. |
| `npm run build` (`web/monoform-studio`) | **PASS with warning** | Vite transformed 2,407 modules. Main JS was 1,283.22 kB (356.71 kB gzip), over the 500 kB warning threshold. |
| `npm run dev -- --host 127.0.0.1 --port 4173` (`web`) | **PASS** | Vite became ready in 464 ms. Explicit loopback override avoided the repository's `0.0.0.0` default during the audit. |
| HTTP GET `/` and `/monoform/index.html` | **PASS** | Both returned HTTP 200 with HTML content types. This is an availability smoke check, not full UI behavior coverage. |
| frontend tests | **SKIP** | No `test` script and no test files were found. |
| frontend lint | **SKIP** | No `lint` script/configured lint gate was found. |
| Go tests | **SKIP** | No `go.mod` or Go production source exists at WP-00. |
| Wails build | **SKIP** | Wails is not installed and no Wails project exists at WP-00. |

## 6. Existing command surface

The main package exposes `dev`, `build`, `build:monoform`, `typecheck`, `start`, `format`, and `format:check`. Both `dev` and `start` bind to `0.0.0.0`; this is a documented security gap, not changed in WP-00. `build:monoform` runs `web/scripts/build-monoform.mjs`, can install nested dependencies when absent, builds MONOFORM, and replaces tracked files under `web/public/monoform`.

CI (`.github/workflows/desktop-build.yml`, despite its name) runs on Ubuntu with Node 22 and executes only `npm ci --legacy-peer-deps`, `npm run typecheck`, and `npm run build`. It does not build desktop artifacts, run tests/lint, or rebuild MONOFORM from source.

The Windows launcher `start.bat` delegates to `scripts/start-windows.ps1`; the audit did not use it because it manages background processes and browser launch. The equivalent Vite server was smoke-tested directly on loopback.

## 7. Reproduction instructions

From an environment matching the README:

```powershell
Set-Location web
npm ci --legacy-peer-deps
npm run typecheck
npm run build
```

At this commit, the first command is expected to fail for the lock mismatch above. To reproduce the remaining baseline without rewriting the lockfile:

```powershell
npm install --legacy-peer-deps --include=optional --package-lock=false --no-audit --no-fund
Set-Location ..
./scripts/verify.ps1
```

On POSIX shells, after dependencies are present, run `./scripts/verify.sh`. Verification scripts never call a Provider and clearly print why unavailable test/lint/Go gates are skipped.

## 8. Baseline conclusion

The checked-out application is buildable and locally servable after a non-lockfile fallback install, and both TypeScript and MONOFORM source builds pass. A clean, lockfile-only main install is not currently reproducible; automated tests and lint are absent; formatting and bundle-size warnings are existing debt. These failures are preserved as explicit WP-01/WP-12 inputs rather than repaired in WP-00.
