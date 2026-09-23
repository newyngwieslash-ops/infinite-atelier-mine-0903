# Release Checklist — WP-12 Item 15

The document a person runs through before shipping. Every gate below is a **command with an expected
result**, not an intention. Where a gate cannot run in an environment, it says so and says why,
rather than being quietly omitted.

**How to read the status column.** `DONE` means it was run and passed for this build. `NOT RUN`
means this work package did not run it. `BLOCKED-BY-ENVIRONMENT` means the tooling cannot run on the
host available. `NOT-DONE` means the work does not exist. A gate that is `NOT RUN` is not a pass.

---

## 1. Preconditions

Run from the repository root, with the toolchain the repository pins.

```bash
# Go: the module requires go 1.25.0 and pins toolchain go1.25.13 in go.mod.
# GOTOOLCHAIN must NOT be pinned to the older version — go.mod's `toolchain` directive
# is what honours the standard-library vulnerability fix (see §4.2).
export GOSUMDB=sum.golang.org
go version                       # expect: go1.25.13 (or the toolchain go.mod selects)

node --version                   # expect: v22 LTS or newer for CI parity
cd web && npm ci --legacy-peer-deps && cd ..
which wails || echo "wails missing — §3 gate will SKIP"
which ffmpeg ffprobe || echo "ffmpeg missing — media export tests will SKIP"
```

**Do not set `GOTOOLCHAIN=go1.25.0`.** That pins the older toolchain and overrides `go.mod`'s
`toolchain go1.25.13` directive, which is the directive that clears the 25 standard-library
vulnerability findings of WP-12 item 12. The WP-03-era instruction to set it predates that fix;
`docs/implementation/STATUS.md` §0n records the change.

---

## 2. The four generator checks

These catch a checked-in artifact that has drifted from the code that generates it. Each supports
`--check` and each is a gate in both `scripts/verify.sh` and `scripts/verify.ps1`.

| # | Command | Expected | Status (2026-09-23) |
|---|---|---|---|
| 2.1 | `node scripts/gen-canary-fixture.mjs --check` | `PASS: fixtures are current (…, 5 chapters, 7 fixtures)` | **DONE — PASS** |
| 2.2 | `node scripts/gen-malicious-fixtures.mjs --check` | `PASS: 11 fixtures are current` | **DONE — PASS** |
| 2.3 | `node scripts/gen-tool-schemas.mjs --check` | **silent, exit 0** | **DONE — PASS** (exit code captured explicitly; see the note) |
| 2.4 | `node scripts/gen-skill-packs.mjs --check` | `PASS: every manifest matches the table and every skill carries section 4.3's sections` | **DONE — PASS** |

**Note on 2.3.** `gen-tool-schemas.mjs --check` **prints nothing on success** and exits 0. An empty
output is a pass here, and `echo $?` after it is what confirms it. This is recorded because an empty
output is exactly what a crashed script also produces.

---

## 3. The verification scripts

| # | Command | Expected | Status |
|---|---|---|---|
| 3.1 | `bash scripts/verify.sh` | `PASS: available verification gates completed.` | **DONE — PASS** |
| 3.2 | `powershell -File scripts/verify.ps1` | same, on Windows PowerShell | **NOT RUN** — the POSIX script is the one exercised here |

`verify.sh` runs, in order: frontend typecheck → frontend tests (if a `test` script exists — it does)
→ frontend lint (**SKIP: no lint script in `web/package.json`**) → frontend production build →
canvas regression (Playwright) → MONOFORM source build (**SKIP** unless
`web/monoform-studio/node_modules` exists; the main build uses the tracked `web/public/monoform`
output) → Go tests → `go vet` → security scan → the four generator checks → the SBOM check → the
Wails production build.

**Two SKIPs in that list are permanent and expected**: frontend lint (no script exists) and the
MONOFORM source build (its separate `node_modules` is absent). One more, the Wails build, SKIPs on
this host — see §3.1 below. **A SKIP is not a FAIL, and it is also not evidence.**

### 3.1 The Wails build gate SKIPs when `wails` is not on `PATH`

`scripts/verify.sh:144-149`:

```sh
if command -v wails >/dev/null 2>&1; then
  run_step "Wails production build" wails build
else
  printf 'SKIP: Wails production build — install pinned CLI v2.15.0 with: …\n'
fi
```

On this host `which wails` returns nothing, so the SKIP prints and **`verify.sh` still exits 0 and
prints PASS**. The desktop artifact is therefore not covered by a green run of the verification
script on a machine without the CLI. Install it and re-run:

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0
$env:PATH = "$(go env GOPATH)\bin;$env:PATH"
wails build                       # expect: build/bin/InfiniteAtelier.exe
```

**This step is a release gate, not optional.** `docs/implementation/RELEASE_BLOCKERS_WP12.md` lists
"No Wails native run" among what its audit did not verify, and `docs/INSTALL_AND_SIGNING.md` §2
records the same.

---

## 4. Security, secrets, dependencies and licences

| # | Command | Expected | Status |
|---|---|---|---|
| 4.1 | `node scripts/security-scan.mjs` | `PASS: security scans clean (…; 1 audited dynamic-execution exception, 3 audited legacy direct-call files with named owners).` | **DONE — PASS** (614 files scanned) |
| 4.2 | `govulncheck ./...` | `No vulnerabilities found.` | **DONE — PASS** by WP-12 item 12; **NOT RE-RUN** here (`govulncheck` is not installed on this host) |
| 4.3 | `npm audit --omit=dev` | 4 moderate, all in a tree-shaken-out subtree | **DONE** by item 12; **NOT RE-RUN** here |
| 4.4 | `node scripts/gen-sbom.mjs --check` | exit 0, no output | **DONE — PASS** |
| 4.5 | `THIRD_PARTY_NOTICES.md` present and complete | every module in the graph has an entry | **DONE** by item 10 |

**4.1's expected line is specific on purpose.** The scan reports exactly **one** audited
dynamic-execution exception (`internal/infrastructure/media/ffmpeg.go` under owner `ADR-0015`) and
**three** audited legacy direct-call files. A run reporting a *different* count is a change that
needs reading, not a pass. The scanner also fails on a **stale** allowlist entry, so removing the
ffmpeg adapter without removing its entry is a failure too — that is the guard against an exception
outliving the code it excused.

**4.2 and 4.3 are genuinely not re-run here.** They need `govulncheck` (not installed) and a network
call to the npm advisory database. They passed when item 12 did the work; whether they still pass is
**not verified** by this checklist run, and a release run must execute them.

**No `LICENSE_ALLOWLIST` policy engine exists.** `scripts/gen-sbom.mjs` reports restrictive licences
among direct dependencies and exits nonzero on one, and that is the enforcement that exists.
`RELEASE_BLOCKERS_WP12.md` states this too. Treat §4.4 as the licence gate.

---

## 5. Go: build, test, vet, format

| # | Command | Expected | Status |
|---|---|---|---|
| 5.1 | `go build ./...` | exit 0 | **DONE — PASS** (reported by item 12; not re-run here) |
| 5.2 | `go test ./... -count=1` | all packages `ok` | **DONE — PASS** |
| 5.3 | `go vet ./...` | exit 0, no output | **DONE — PASS** |
| 5.4 | `gofmt -l internal/ *.go` | **no output** | **DONE — PASS** (no files listed) |
| 5.5 | `git diff --check` | no output (no whitespace errors) | **NOT RUN** in this work package — run it, it is an AGENTS §4.4 gate |
| 5.6 | `go test -race ./...` | all packages `ok` under the race detector | **BLOCKED-BY-ENVIRONMENT** — see below |

### 5.6 `go test -race` cannot run on this host

The C toolchain reports:

```text
cc1.exe: sorry, unimplemented: 64-bit mode not compiled in
```

This is an **environment failure, never a pass**. There is **no race-detector evidence for this
build.** AGENTS §8.6 requires race-aware testing for concurrency (`go test -race` "平台可用时" — where
the platform allows it). It does not here. A release run must either repair the host's C toolchain or
accept, in writing, that concurrency has no race evidence.

---

## 6. Frontend

Run from `web/`.

| # | Command | Expected | Status |
|---|---|---|---|
| 6.1 | `npm run typecheck` | exit 0 | **DONE — PASS** |
| 6.2 | `npm test` | 67 tests, 67 pass, 0 fail | **DONE — PASS** |
| 6.3 | `npm run build` | exit 0, with the pre-existing >500 kB chunk warning | **DONE — PASS** |
| 6.4 | `npx playwright test` | 25 passed, 1 skipped (26 total), exit 0 | **DONE — PASS** |
| 6.5 | `npm run format:check` | exit 0 | **NOT RUN** — not wired into `verify.sh`; run it or say it was skipped |

**6.4's skip is pre-existing** and is the crop-action case, not a failure. **6.4 runs the canvas in
BROWSER mode against the Vite dev server.** The Wails desktop shell is not launched, because a Wails
window cannot be driven from a test runner — the Playwright config says so in its own header. So
"the desktop shell's canvas works" is **not** covered by this suite; it rests on the Go tests and on
WP-01's native smoke evidence, which predates WP-12.

**6.5 is a real hole.** `web/package.json` has `format` and `format:check` scripts and neither runs
in either verify script. A release run should execute it or record that it did not.

---

## 7. The PRD section 18 release blockers

`PRD.md:1734-1748` blocks release on eleven conditions. **Each was checked against the running code
rather than against a comment, and all eleven are CLEARED with one named caveat.** The evidence row
by row — the test name, the `file:line`, the command and its real output — is in
**`docs/implementation/RELEASE_BLOCKERS_WP12.md`**, which is the authority. It is deliberately **not
restated here**: two copies of the same evidence drift, and the second one is always the one nobody
re-reads.

| # | Blocker | Verdict | Evidence |
|---|---|---|---|
| 1 | 前端或普通备份可获取完整 API Key | **CLEARED** | `RELEASE_BLOCKERS_WP12.md` §1 |
| 2 | 仍存在任意模型 JavaScript 执行路径 | **CLEARED** | §2 |
| 3 | API 代理可访问回环、私网或任意地址 | **CLEARED** | §3 |
| 4 | 工作流运行状态只存在内存 | **CLEARED** | §4 |
| 5 | 视频结果未验证即标记成功 | **CLEARED** | §5 |
| 6 | 数据库迁移无备份或回滚/修复路径 | **CLEARED** | §6 |
| 7 | Supervisor 可使用未授权写工具 | **CLEARED** | §7 |
| 8 | 导入可路径穿越或 Zip Bomb | **CLEARED** | §8 |
| 9 | 旧项目迁移存在静默丢失 | **CLEARED** | §9 |
| 10 | 核心 E2E 测试未通过 | **CLEARED** | §10 — with the stated limitation that Playwright drives browser mode, not the desktop shell |
| 11 | 许可证和第三方声明缺失 | **CLEARED** | §11 — closed by WP-12 item 10 |

The same document also audits `docs/SECURITY.md` §19's fifteen extra conditions and
`docs/AGENT_CONTRACTS.md` §20's fourteen clauses. **Read it before declaring a release.**

**A green §7 table is not a substitute for running §2 through §6 of this checklist.** The blockers
are *conditions*; the commands are the *evidence that they still hold*.

---

## 8. Known outstanding items

Each is stated with its real status. None is implied, and none is left to be inferred.

### 8.1 BLOCKED-BY-ENVIRONMENT — cannot be verified here

| Item | Why it cannot be verified | What it costs |
|---|---|---|
| `go test -race ./...` | `cc1.exe: 64-bit mode not compiled in`. Not a pass. | **No race-detector evidence exists for this build.** |
| Windows clean-VM run | No clean Windows VM available to this work package. | Nothing here is evidence the binary runs on a machine other than the one that built it — including WebView2 availability, first-run behaviour, and the per-user data path. |
| Code signing | No certificate and no signing identity; a certificate is a purchase, not a task. | Windows shows an unknown-publisher warning. See `docs/INSTALL_AND_SIGNING.md` §3. |
| Remote CI run (`.github/workflows/desktop-build.yml`) | No push is authorized (AGENTS §5), so the workflow was read, not executed. | The CI desktop build has **never run**. |
| `govulncheck ./...` and `npm audit --omit=dev` | `govulncheck` not installed; npm audit needs the advisory network. | The item-12 results are not re-confirmed. |
| React canvas render at 1,000 nodes | A browser measurement, and the Playwright suite runs canvas in browser mode with the legacy IndexedDB adapter, not the Go core. | "The canvas renders 1,000 nodes smoothly" is **unmeasured**. `STATUS.md` §0m1 states this. |

### 8.2 NOT-DONE — the work does not exist

| Item | Status | Where it is recorded |
|---|---|---|
| **No encrypted sensitive backup** | **Decided against for v1**, not deferred. An ordinary backup holds no Secret, so encryption would protect content rather than credentials, and a password is a thing to lose. The accepted risk — *a backup file on a shared drive is readable by anyone who can read the file* — is stated in the ADR. | `docs/adr/0016-encrypted-sensitive-backups-not-in-v1.md` |
| **No installer** | No `.msi`, no installer script, no NSIS/Inno/WiX template. The artifact is a portable executable. The per-user vs per-machine question, the WebView2 strategy choice and the uninstall behaviour are analysed but **undecided**. | `docs/INSTALL_AND_SIGNING.md` §4 |
| **No user interface for the core's backup/restore** | `BackupBinding` exposes `ExportBackup`, `PreviewBackup`, `RestoreBackup`, `DiscardBackupState` and `BackupStateHeld`, and they are bound into the desktop app — but **no component in `web/src` calls any of them**. The 数据备份 tab in 配置 is a *different* feature that exports browser-local data. Restore atomicity (ROADMAP item 5) is implemented and tested in the core; it is simply unreachable from a screen. | This entry; `docs/USER_GUIDE.md` §15.3 |
| **No working video or audio generation in secure desktop mode** | Not a PRD §18 blocker and not a security defect — a **capability gap**. `Registry.VideoPortFor`/`AudioPortFor` resolve an adapter only for `mock_media`, which `IsUserConfigurableKind` refuses to persist and no UI offers. The canvas refuses earlier (a free node is not a shot or a dialogue line). | `STATUS.md` §0m; `docs/adr/0015-*.md`; `docs/USER_GUIDE.md` §13 |
| **No per-line audio read** | The timeline reports audio per **shot**; a "which lines have audio" list needs a read that does not exist. | `STATUS.md` §0l item 3 |
| **No screen generates or approves a panel image** | Five core commands have **zero frontend callers**: `RunImageBatch` (submit the image jobs), `CheckStoryboardGate` (refuse a batch before the gate passes), `CollectBatchResults` (turn finished jobs into candidate versions), `ApproveCandidate`, `ApprovePanelImage` (make one candidate a shot's canonical panel). `ListPanels` and `ListPanelVersions` exist as wrappers in `web/src/services/desktop/drama.ts:185,924` and **no component calls either**. `approved_image_asset_version_id` is where a shot's approved frame lives (`internal/infrastructure/database/timeline.go:21`) and the export reads it — so **the export composes from approved panels, and no part of the interface can set one.** Established by counting references, and it is not in `STATUS.md`'s outstanding lists. | This entry; `docs/USER_GUIDE.md` §11.2 |
| **No screen creates an asset version** | `AddVersion`, `AttachFile`, `AttachJobResult`, `AddUsage` and `AddRelation` have **zero frontend callers**. `ApproveVersion` does (through the asset drawer). So the asset sections can **approve** a version and cannot **create** one: an asset created through the interface has no versions, and the batch that was meant to produce candidate versions has no UI (row above). Same shape as the row above, and part of the same chain. | This entry; `docs/USER_GUIDE.md` §10 |
| **No per-asset licence/rights metadata** | No column in any migration stores one. The Final Ruleset **reports this gap itself** as a single minor finding rather than pretending to check it. | `STATUS.md` §0l; `PRD.md` §R9 |
| **FR-110's Safety and Cost categories covered by neither half** | No deterministic rule and no supervisor skill checks them, in a build whose entire point is a quality gate. | `STATUS.md` §0k item 3 |
| **No SQLite index satisfies three `ORDER BY` clauses** | `MemoryRepository.VectorCandidates` (~62 ms/10k rows), `AssetRepository.ListAssets` (~23 ms/1k rows) and `MemoryRepository.ListItems` (~51 ms/10k rows) sort the whole table. The fix is a migration and was deliberately **not** taken in a measurement package. | `STATUS.md` §0m1 |
| **Semantic search reaches only the newest 500 embedded rows** | A recall limit nothing documents. Changing it is a product decision, so it is recorded and asserted by a test rather than changed. | `STATUS.md` §0m1 |
| **`shadcn` is a production dependency** pulling a large chain for one CSS `@import` | Moving it to `devDependencies` is the structurally right change and is left as a recommendation, because it is a dependency-graph decision rather than a vulnerability fix. | `STATUS.md` §0n |
| **No legacy-data deletion** | WP-12 item 16 made the dangerous legacy paths unreachable and deleted the model-script executor, but browser data and the legacy transport files remain. | `STATUS.md` §0m |

### 8.3 PARTIAL — built, with a named limit

| Item | What is partial |
|---|---|
| Restore of a database needing more than one disk operation | The promotion's `os.Rename` is atomic within a filesystem and **refuses** across two rather than attempting it. Untested at that size. |
| AGENT_CONTRACTS §11.4 "所有必需 Shot 有批准视频" | Implemented as approved **media**, which in this build is usually a panel image. A literal reading would report every shot of every episode, because the video adapter is a mock. |
| AGENT_CONTRACTS §11.4 "黑帧/空帧/静音异常" | A probe catches a placeholder's size, a type ffmpeg cannot compose, and an audio file that is a container header. **A black frame inside a well-formed video, and a silent passage inside well-formed audio, are NOT caught** — and the rule says so rather than reporting a clean result it did not earn. |
| Export output | A real, playable MP4 with real timing, audio and subtitles — composed from **approved panel images**, so a slideshow of frames rather than moving footage. |
| Playwright E2E | 25 passing tests, but in **browser mode**. The desktop shell is not covered. |

---

## 9. What a Release Candidate means here

Given §8.1, §8.2 and §8.3, "Release Candidate" in this repository means a specific and bounded claim.
It is worth stating what it is **not**: it is not "these items were fixed", and it is not "these
items do not matter".

### 9.1 What this RC claims

**A security-hardened, functionally incomplete desktop build with a complete and audited
security posture, a working image and text pipeline, a working export, and no working video or audio
generation.**

Specifically:

- **All eleven PRD §18 release blockers are cleared**, with evidence per row in
  `RELEASE_BLOCKERS_WP12.md`. This is the headline claim and it is the one that was audited against
  the running code.
- **All fifteen of `docs/SECURITY.md` §19's conditions are cleared**, and all fourteen of
  `AGENT_CONTRACTS.md` §20's clauses.
- **The dangerous legacy execution paths are gone or fail closed** (§8.2's item 16 work): the model
  JavaScript executor is deleted, no direct provider call is reachable in secure desktop mode, and
  no test was weakened, skipped or deleted to reach any of this.
- **The gates in §2 through §6 pass**, except where §8.1 says they could not be run.

### 9.2 What is DEFERRABLE, and why

These do not block an RC **provided the release states them**:

| Item | Why it is deferrable |
|---|---|
| **Code signing** | A trust and distribution concern, not a security property. No §19 or §18 condition depends on it. It needs a purchase and an identity, neither of which an engineering package can supply. The cost is an unknown-publisher warning, disclosed. |
| **Installer and per-user/per-machine packaging** | The build is a portable executable. Installing it is a copy. The WebView2 prerequisite is handled at first launch by Wails' `download` strategy with a real message box. The decisions are analysed in `docs/INSTALL_AND_SIGNING.md` §4 and are the right work for a packaging package. |
| **Video and audio generation** | A **capability gap**, explicitly not one of the eleven blockers. `docs/ROADMAP.md` permits a complete mock for the video provider, and the mock is complete. The export composes from approved frames and is genuinely playable — the gap is that there is no moving footage and no synthesised speech. |
| **The backup/restore UI gap** | The core implementation is complete and tested (atomicity, checksums, secret scan, rollback, discard). What is missing is a screen. A user can copy the data directory; `docs/USER_GUIDE.md` §15.3 says how and states the caveat. |
| **The three unindexed `ORDER BY` reads** | Measured, bounded and recorded, with the fix identified as a migration. They are a performance characteristic at 10k rows, not a correctness defect. |
| **The semantic-search 500-row window** | A recall limit. Changing it is a product decision, which is why it was recorded rather than changed. |
| **No encrypted backup** | Decided against, with the reasoning and the accepted risk in ADR-0016. It is a **closed decision**, not an open item. |
| **Per-asset licence metadata, FR-110 Safety/Cost, the 1,000-node render measurement** | Named gaps in `STATUS.md`. Each is reported by the product rather than hidden by it. |

### 9.3 What is NOT deferrable

These must be resolved, or the release must state in writing that it shipped without them:

| Item | Why it is not deferrable |
|---|---|
| **Any of the eleven PRD §18 blockers regressing** | They are release *blockers*, in as many words. `RELEASE_BLOCKERS_WP12.md` is the evidence; re-run §2 through §6 of this checklist to confirm it still holds. |
| **`go test -race` evidence** | Currently absent for an environment reason. Concurrency is everywhere in this build — the job runner, the queue, the agent runtime. Shipping with **zero** race evidence is a decision a release must make explicitly and record; it cannot be inherited by default. |
| **A Wails build that has actually run** | §3.1's SKIP means the desktop artifact can be unbuilt and the verification script still says PASS. **Someone must run `wails build` and confirm the artifact before shipping.** WP-01's smoke evidence predates every media change in WP-11. |
| **A run on a machine other than the build host** | The WebView2 first-run path, the per-user data directory, and the whole desktop shell are untested off this machine. Nothing in this checklist substitutes for it. |
| **Re-running `govulncheck` and `npm audit`** | Item 12's results are a **point-in-time** claim about a dependency graph that has moved since (the SBOM regenerated at 07:14 today). Re-run §4.2 and §4.3, or record that the results are stale. |
| **The desktop E2E gap** | Playwright proves the canvas works in a browser. It does not prove it works in the Wails webview, which is the only place users will run it. |

### 9.4 The one-line version

**This is an RC whose security posture is audited and whose feature set is incomplete in a way that
is documented, reported in the product, and permitted by the specification — with four verification
gaps (race detector, clean VM, code signing, desktop-shell E2E) that a shipping decision must name
rather than inherit.**

---

## 10. The checklist, in run order

Copy-paste order for a release run. Each line's expected result is in its section above.

```bash
# Preconditions
export GOSUMDB=sum.golang.org
go version && node --version
cd web && npm ci --legacy-peer-deps && cd ..

# 2. Generators
node scripts/gen-canary-fixture.mjs --check
node scripts/gen-malicious-fixtures.mjs --check
node scripts/gen-tool-schemas.mjs --check; echo "exit=$?"   # silent on success
node scripts/gen-skill-packs.mjs --check
node scripts/gen-sbom.mjs --check

# 3. Verification
bash scripts/verify.sh

# 4. Security and dependencies
node scripts/security-scan.mjs
govulncheck ./...
cd web && npm audit --omit=dev && cd ..

# 5. Go
go build ./...
go test ./... -count=1
go vet ./...
gofmt -l internal/ *.go            # expect NO output
git diff --check                   # expect NO output
go test -race ./...                # known to fail on this host; must be resolved or recorded

# 6. Frontend
cd web
npm run typecheck
npm test
npm run build
npx playwright test
npm run format:check
cd ..

# 3.1 / 9.3  The desktop build — a RELEASE GATE, and it SKIPs above without the CLI
wails build
ls -la build/bin/InfiniteAtelier.exe
```

---

## References

- `PRD.md:1734-1748` — the eleven release blockers; `PRD.md:1750-1800` — AC-E2E-001 through
  AC-E2E-005.
- `docs/implementation/RELEASE_BLOCKERS_WP12.md` — the per-blocker evidence. **The authority for §7.**
- `docs/implementation/STATUS.md` — §0n (items 10 and 12, the blocker audit), §0m1 (scale cases and
  the open performance finding), §0m (item 16), §0l (WP-11, including what remains PARTIAL), §0k
  (WP-10, including the nine things it does not cover).
- `docs/ACCEPTANCE.md` — AC-BACKUP-001/002, AC-SEC-001..003, AC-MEDIA-001..003, AC-CANVAS-004, the
  AC-MEM series, AC-E2E-001..005.
- `docs/SECURITY.md` §19 (its own release blockers); `docs/AGENT_CONTRACTS.md` §20.
- `docs/ROADMAP.md:573-614` — WP-12's scope and 关键验收.
- `docs/adr/0015-*.md` and `docs/adr/0016-*.md` — the two ADRs this package's scope sits beside.
- `docs/INSTALL_AND_SIGNING.md` — build, signing and installer status.
- `docs/USER_GUIDE.md` — the user-facing statement of what works.
- `THIRD_PARTY_NOTICES.md`, `sbom/cyclonedx.json`, `sbom/licences.json` — the distribution's licence
  obligations (AGENTS §6).
- `scripts/verify.sh`, `scripts/verify.ps1`, `.github/workflows/desktop-build.yml` — the gates
  themselves, as code.
