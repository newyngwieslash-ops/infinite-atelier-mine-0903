# Implementation Status

> Last updated: 2026-09-23
> Product: Infinite Atelier Core + Drama Production Pack
> Current work package: **WP-12 — 硬化、性能、打包与 Release Candidate, scope items 10, 12 and the
> release-blocker audit (headline acceptance)**
>
> # 0n. WP-12 items 10 and 12, and the release-blocker audit (2026-09-23)
>
> Items 10 (third-party notices/SBOM) and 12 (dependency vulnerability) are CLOSED, and PRD section
> 18's eleven release blockers were each checked against the running code rather than against a
> comment. **All eleven are CLEARED**, with one named caveat that is a capability gap rather than a
> blocker: there is no working video or audio generation in secure desktop mode, inherited from item
> 16.
>
> ## Item 10: SBOM and notices
>
> - **`scripts/gen-sbom.mjs` generates `sbom/cyclonedx.json` (CycloneDX 1.5) and
>   `sbom/licences.json`**, and supports `--check`. Both verify scripts now run the check, so the
>   document cannot drift from `go.mod`/`go.sum`/`web/package-lock.json` without failing a gate.
>   CycloneDX was chosen over SPDX because its `licenses[].license.id` carries an SPDX identifier
>   directly and both ecosystems publish CycloneDX tooling. **Neither `syft` nor `cyclonedx-gomod`
>   nor `cyclonedx-npm` is installed on this host, and this generator uses neither.**
> - **The generator never writes outside `sbom/`.** It does not run `go mod download`, because that
>   command appended 143 lines to `go.sum` when it was tried during development — a real
>   modification to a tracked file made by a tool whose job is to observe. A module whose licence
>   file is absent from the cache is recorded as `unknown`, never fetched.
> - **Licence identification was VERIFIED, not restated, and the verification found two WRONG
>   answers.** `modernc.org/memory`'s LICENSE is BSD-3-Clause but was reported BSD-2-Clause (its
>   third clause reads "Neither the names of the authors..."), and `hashicorp/golang-lru/v2` plus
>   `cyphar/filepath-securejoin` were reported **AGPL-3.0** — a false release blocker — because
>   MPL-2.0's own text mentions the AGPL in ordinary sentence case. Both were found by reading the
>   files. The GPL-family markers are now case-sensitive, and a negative control over synthetic
>   AGPL/SSPL/GPL-3/GPL-2/LGPL-2.1 texts caught a regression that the fix itself introduced.
> - **Result: no AGPL/GPL/LGPL/SSPL dependency anywhere, and no unidentified licence.** Across 130
>   Go modules: MIT 66, BSD-3-Clause 35, Apache-2.0 14, BSD-2-Clause 8, ISC 4, Unlicense OR MIT 1,
>   BSD-3-Clause AND MPL-2.0 1, MPL-2.0 1. Across 1,388 npm slots: MIT 1,270, ISC 63, BSD-3-Clause
>   16, Apache-2.0 12, MPL-2.0 12, BSD-2-Clause 5, BlueOak 2, 0BSD 2, four singletons, two
>   OR-expressions. The three MPL entries are in the graph tier, not in `go build ./...`'s closure.
> - **`THIRD_PARTY_NOTICES.md` gained 86 entries**, covering every module in the graph the file had
>   no entry for (its WP-01 caveat said toolchain-only modules were outside the inventory; that gap
>   is now closed). No licence entry was removed: `git diff` shows 5,653 insertions and 2 deletions,
>   and those two are the file's TITLE line and its opening paragraph, rewritten because both said
>   "WP-01" and "this is not a complete release SBOM" — claims this package made false. All 86
>   appended licence texts were verified against the module cache, which is how two formatting
>   defects were caught (a missing trailing newline merging two entries, and an embedded Markdown
>   fence truncating a 19 KB text to 1.2 KB). Eight lines then had trailing whitespace stripped
>   inside the fences — `git diff --check` is an AGENTS section 4.4 gate and eight upstream licence
>   files wrap their text that way. Trailing whitespace carries no legal meaning, every word is
>   unchanged, and the only 8 differing lines are ones where `rstrip()` matches.
>
> ## Item 12: dependency vulnerability
>
> - **Go: 25 findings, ALL in the standard library, ALL fixed in go1.25.13.** `go.mod` gained
>   `toolchain go1.25.13`. Re-scanned with the directive honoured: **"No vulnerabilities found."**
>   The findings were reachable, not theoretical — `net/url`, `crypto/tls`, `crypto/x509`,
>   `encoding/xml` and `encoding/asn1` all appear in traces through `providerhttp.Downloader` and
>   `importing.parseWordBody`.
> - **npm: 24 findings → 4, via in-major bumps only.** axios 1.16.0→1.20.0, nanoid 5.1.11→5.1.16,
>   react-router and react-router-dom 7.18.0→7.18.4. `package.json` is unchanged; only the lockfile
>   moved. Every bump stayed inside its existing major.
> - **The 4 remaining are moderate, and they are NOT in the shipped bundle.** They are the
>   `@ant-design/pro-components` → `react-syntax-highlighter` → `refractor` → `prismjs` chain. The
>   only import of that package is `ProConfigProvider` in `app-providers.tsx`, and the built
>   `dist/assets/index-*.js` contains zero occurrences of `react-syntax-highlighter`, `refractor`,
>   `prismjs` or `SyntaxHighlighter` — the vulnerable subtree is tree-shaken out. The available
>   "fix" is a **major downgrade** to 2.8.10, which is why it was NOT applied.
> - `shadcn` is a **production** dependency that pulls the whole `@modelcontextprotocol/sdk` →
>   express/hono/qs chain while contributing exactly one CSS `@import`. That chain's 10 findings are
>   now fixed by the lockfile update; moving `shadcn` to devDependencies would be the structurally
>   right change and is left as a recommendation, because it is a dependency-graph decision rather
>   than a vulnerability fix.
>
> ## The release-blocker audit
>
> All eleven CLEARED. Each verdict names the code, test or command it rests on. One row carries a
> caveat that is a capability gap rather than a blocker, and it is stated rather than softened:
>
> | # | Blocker | Verdict |
> |---|---|---|
> | 1 | 前端或普通备份可获取完整 API Key | **CLEARED** |
> | 2 | 仍存在任意模型 JavaScript 执行路径 | **CLEARED** |
> | 3 | API 代理可访问回环、私网或任意地址 | **CLEARED** |
> | 4 | 工作流运行状态只存在内存 | **CLEARED** |
> | 5 | 视频结果未验证即标记成功 | **CLEARED** |
> | 6 | 数据库迁移无备份或回滚/修复路径 | **CLEARED** |
> | 7 | Supervisor 可使用未授权写工具 | **CLEARED** |
> | 8 | 导入可路径穿越或 Zip Bomb | **CLEARED** |
> | 9 | 旧项目迁移存在静默丢失 | **CLEARED** |
> | 10 | 核心 E2E 测试未通过 | **CLEARED** |
> | 11 | 许可证和第三方声明缺失 | **CLEARED** — by this package |
>
> The evidence for each row, and the SECURITY section 19 / AGENT_CONTRACTS section 20 extras, are in
> `docs/implementation/RELEASE_BLOCKERS_WP12.md`.
>
> ## Verification, with results
>
> | Command | Result |
> |---|---|
> | `GOTOOLCHAIN=auto go test ./... -count=1` (go1.25.13) | **PASS** — 36 packages ok |
> | `go build ./...` / `go vet ./...` (go1.25.13) | **PASS** |
> | `govulncheck ./...` (go1.25.13) | **PASS** — No vulnerabilities found |
> | `bash scripts/verify.sh` | **PASS** — including the new "SBOM is current" step |
> | `node scripts/security-scan.mjs` | **PASS** — 607 files, 1 audited dynamic exception, 3 named legacy files |
> | `node scripts/gen-sbom.mjs --check` | **PASS** |
> | `web`: `npm run typecheck` / `npm test` (67) / `npm run build` | **PASS** |
> | `web`: `npx playwright test` | **PASS** — 25 passed, 1 pre-existing skip |
> | `npm audit --omit=dev` | 4 moderate, all in a tree-shaken-out subtree (was 24) |
> | `go test -race ./...` | **ENVIRONMENT FAILURE** — `cc1.exe: 64-bit mode not compiled in`. Not a pass. |
>
> No test was weakened, skipped or deleted.
>
> ## Known limits
>
> - **Secure desktop mode has no working video or audio generation**, on the canvas or in the
>   studio. Item 16 recorded this; the audit confirms it is a capability gap rather than a security
>   defect, and it is NOT one of PRD section 18's eleven blockers.
> - `THIRD_PARTY_NOTICES.md` now carries the whole module graph (~385 KB). The generated SBOM is the
>   machine-readable primary; the Markdown is the licence TEXT a distribution must carry.
> - The npm half of the SBOM reads `package-lock.json` and, for the two packages it omits a licence
>   for, the installed package. It does not read the network.

> # 0m1. WP-12 items 3 and 4: the scale cases are measured, and ONE FINDING IS OPEN (2026-09-23)
>
> ## What this closes
>
> - **Item 3 (1,000 node/2,000 edge 性能) is MEASURED.** `internal/infrastructure/database/scale_wp12_test.go`
>   builds the load through the real repositories over a migrated database;
>   `scale_canvas_wp12_test.go` holds five bound tests, two benchmarks per operation, a shape test and
>   a worst-case test at the binding's own 5,000-move ceiling. Every figure is in the file's own
>   comments beside the bound it set.
> - **Item 4 (10k asset/Memory benchmark) is MEASURED.**
>   `scale_records_wp12_test.go` holds the same for 10,000 assets, 10,000 embedded memories and
>   10,000 transcript rows.
>
> ## THE BLOCKING FINDING: three indexed reads sort the whole table
>
> **`idx_memory_items_embedding` does not cover the query's ORDER BY, so a memory search spends
> ~62 ms of which ~0.2 ms is the search's arithmetic.** Measured decomposition and
> `EXPLAIN QUERY PLAN` output are recorded in `scale_records_wp12_test.go`. The plan is
> `SEARCH memory_items USING INDEX idx_memory_items_embedding (scope_project=? AND embedding_model=?
> AND embedding_version=?)` followed by `USE TEMP B-TREE FOR ORDER BY`; the same query with the
> ORDER BY removed is 0.5 ms, a hundredfold difference.
>
> `memory_index.go`'s "The ceiling" section states that the cost of a search "must not be a function
> of how much a user has accumulated". **That holds for the SCORING, which is capped at
> `MaxCandidates`, and NOT for the READ, which is linear in the project's embedded rows.**
>
> **The same shape affects two more reads**, each confirmed by its own plan:
>
> | read | plan's second line | measured |
> |---|---|---|
> | `MemoryRepository.VectorCandidates` | `USE TEMP B-TREE FOR ORDER BY` | 62 ms |
> | `AssetRepository.ListAssets` | `USE TEMP B-TREE FOR ORDER BY` | 23 ms (1,000-row page) |
> | `MemoryRepository.ListItems`, no type filter | `USE TEMP B-TREE FOR ORDER BY` | 51 ms |
> | `MemoryRepository.ListItems`, one type | `USE TEMP B-TREE FOR LAST TERM OF ORDER BY` | not measured |
> | `AgentRepository.RecentMessages` (for contrast) | none — index satisfies the order | 62 µs |
>
> The two `ListItems` rows are the same statement with and without a type filter, and the
> plan's second line differs between them while the conclusion does not: `idx_memory_items_project_type`
> supplies `scope_project` and `memory_type`, and `created_at` is its fourth column, so the
> `id DESC` tie-breaker is what still needs a sort. Only the unfiltered variant — the one the
> benchmark makes — has a measured figure here.
>
> **The cause is the same in each case**: every one of these reads orders by
> `created_at DESC, id DESC` while its index ends at some other column, so SQLite materialises and
> sorts. `RecentMessages` is the proof that it is fixable — `idx_agent_messages_scope` is
> `(scope_project, scope_agent_key, created_at)`, which satisfies its ordering, and it is a
> thousand times faster for it.
>
> **The fix is a migration**: an index per affected table whose column list ends with
> `created_at DESC, id DESC`. That is a change to `docs/DOMAIN_MODEL.md` section 18's index list and
> belongs to a package with migration scope — WP-12 item 7 is "Database migration from previous
> package", which is the natural home. **It is NOT done here**, because a performance measurement
> must not quietly add schema, and no bound was loosened to hide it: the search's bound is 200 ms
> (3.2x the measurement) with the figure recorded beside it, and the file says explicitly that a
> loosened bound was not used to make this pass.
>
> ## The second finding: the semantic channel cannot reach a memory older than the newest 500
>
> `VectorCandidates` orders by `created_at DESC` and cuts to `MaxCandidates`, so a search can only
> return a memory among the newest 500 embedded rows of its scope. A memory older than that is not
> ranked lower — it is not a candidate, and no similarity can bring it back. In the 10,000-row load
> the window is `wp12-memory-09500..wp12-memory-09999` and `wp12-memory-00000` is unreachable.
>
> ADR-0014 rules on the index's COST ("A search is O(candidates) rather than O(log n), bounded by
> `MaxCandidates`") and says nothing about the candidate WINDOW being the newest rows by recency.
> This is a recall limit nothing documents. Whether it is acceptable is a **product decision** — the
> alternatives are a non-recency ordering, a wider window, or the sqlite-vec adapter the port's V1
> names — so it is recorded rather than changed. `TestWP12VectorCandidatesReachOnlyTheNewestRows`
> asserts the CURRENT behaviour so the limit is a tested number rather than an unchecked
> assumption.
>
> ## What is NOT measured, stated plainly
>
> **The React canvas's render cost at 1,000 nodes is not measured, and nothing in this repository
> currently can.** It is a browser measurement, and the Playwright suite runs the canvas in BROWSER
> mode where persistence is the legacy IndexedDB adapter rather than the Go core (ADR-BASE-004);
> its own header says the desktop shell is not launched because "a Wails window cannot be driven
> from a test runner". The benchmarks below support "the Go core's canvas reads and writes at 1,000
> nodes and 2,000 edges are bounded" and NOT "the canvas renders 1,000 nodes smoothly".
>
> # 0m. WP-12 item 16: the dangerous legacy paths are gone or fail closed (2026-09-23)
>
> ## What this closes
>
> - **PRD section 18's "仍存在任意模型 JavaScript 执行路径" is CLOSED.** `web/src/services/api/model-plugin.ts`
>   was the repository's only frontend `new Function`, and it is deleted, with its editor
>   (`model-script-editor.tsx`) and the accessor that fed it (`resolveModelScript`). The
>   scanner's `DYNAMIC_ALLOWLIST` no longer carries an entry for it, so a reintroduction
>   FAILS `node scripts/security-scan.mjs` rather than inheriting an exception.
> - **The stale `WP-13 (legacy media removal)` owner strings are gone.** WP-13 does not exist —
>   WP-12 is the last work package in ROADMAP — so those entries were corrected to
>   `WP-12 (legacy removal)` with reasons that state the CURRENT truth.
> - **No direct provider call is reachable in secure desktop mode.** `video-generation.ts` and
>   `audio-generation.ts` were added, mirroring `image-generation.ts`, and they THROW in secure
>   mode rather than falling back to a browser call. The three legacy files survive for browser
>   development mode only.
>
> ## The blocking finding, stated rather than worked around
>
> **The Go media job path cannot serve a free canvas node, so the canvas loses video and audio
> generation in secure desktop mode.** This is a capability loss and it is recorded here as one.
>
> `SubmitVideoJobRequest` requires `projectId`, `episodeId` and `shotId`, and
> `SubmitAudioJobRequest` requires `projectId`, `episodeId` and `dialogueLineId`;
> `internal/desktop/media_jobs.go` returns `bindingInvalidInput()` when any is blank. The canvas
> page addresses a PROJECT (`web/src/pages/canvas/project.tsx` reads `useParams<{id: string}>`),
> `CanvasNodeType` has no shot member, and a free-canvas node carries no `entityType`/`entityId`,
> so there is no honest value for `episodeId`, `shotId` or `dialogueLineId`. Inventing one would
> record a job against a shot or a line that does not exist, which is a fabricated domain fact
> rather than a failed request. Separately, the Go video and audio adapters are mocks reachable
> only through `mock_media`, which `IsUserConfigurableKind` refuses to persist and no UI offers,
> so a secure submission would not have reached a real provider either.
>
> **The alternative that was NOT taken, and why:** adding `episodeId`/`shotId` fields to the
> canvas node, or a canvas-scoped media submission, is a domain and binding design change that
> belongs to a work package with that scope. WP-12 item 16 is "remove or make unreachable", and
> expanding it into new binding surface would be the scope creep AGENTS section 4.1 forbids.
>
> **What a user can still do, and the part that does not work either:** the studio's video and
> audio sections (`web/src/components/studio/video-view.tsx`, `audio-view.tsx`) are the
> structurally correct route — they submit against a real shot and a real dialogue line through
> `submitVideoJob`/`submitAudioJob`, so they can fill every required field. **But this build has no
> real video or audio adapter.** `Registry.VideoPortFor` and `AudioPortFor` resolve an adapter only
> for `mock_media`, which `IsUserConfigurableKind` refuses to persist and no UI offers, and return
> "unsupported" for `openai_compatible` and `gemini_compatible`. A studio submission therefore
> reaches the queue and fails at provider resolution. WP-11's own section 0l already records this
> ("No real video provider", listed among what that package does not cover).
>
> **So the honest statement is: secure desktop mode has no working video or audio generation today,
> on the canvas or in the studio.** The canvas refusal arrives earlier and with a clearer message
> than the studio's job failure, which is all this scope item can improve. Closing it requires a
> real adapter, which is adapter work rather than a routing change, and it is left as an open item
> rather than hidden behind a notice that implies the studio route succeeds.
>
> ## Verification
>
> | Command | Result |
> |---|---|
> | `node scripts/security-scan.mjs` | **PASS** — `PASS: security scans clean (602 files scanned; 1 audited dynamic-execution exception, 3 audited legacy direct-call files with named owners).` |
> | `web`: `npm run typecheck` | **PASS** |
> | `web`: `npm run test` | **PASS** — 67 tests, 67 pass, 0 fail |
> | `web`: `npx playwright test` | **PASS** — 25 passed, 1 pre-existing skipped (26 total) |
> | `web`: `npm run build` | **PASS** — with the pre-existing >500 kB chunk warning |
>
> No test was weakened, skipped or deleted to reach these results. No test asserted the
> model-script editor or a legacy transport, so none needed changing.
>
> ## Known limits
>
> - The legacy files (`api/image.ts`, `api/video.ts`, `api/audio.ts`) still exist and still
>   build; they are unreachable in secure mode because their routers check
>   `isSecureProviderMode()` first. A future regression that calls them directly would put a key
>   back in the webview, which is why they remain named in the scanner allowlist rather than
>   deleted: a browser build has no other transport for these capabilities.
> - A model whose stored `script` was the only thing that made it work now falls through to the
>   standard OpenAI-compatible path and may fail at the provider. This is a deliberate capability
>   loss; there is no replacement, because a Go-side script runner would be the same
>   arbitrary-execution surface. The stored `script` field is left inert rather than deleted, so
>   no user configuration is destroyed during an upgrade.
>
> ---
>
> Previous: **WP-11 — 视频、音频、字幕、时间线与导出**
> Status: **COMPLETE for all 14 ROADMAP items, with two PARTIAL items named in section 0l.** The media half of the product is built: the one audited subprocess and its ffmpeg adapter, migration 000020's three tables, the media domain's timecodes and cue/manifest rules, the subtitle service with drafts that cite dialogue lines and an editor that reads back, the timeline as an ordered join over the board's own rows, the export service that composes a real playable MP4 from approved panel frames with audio and subtitles muxed in, the `SaveFile` path that writes to where a user points — the first such path this application has ever had — the video and audio job submissions with their reference-asset pipeline, the two `final_episode` agents and the deterministic Final Ruleset that precedes the supervisor, and the video, audio and timeline sections. Section 0l states what this package delivered, the defects TWO INDEPENDENT REVIEWS found (321 mutations, 149 killed; four blockers), the items closed after the first report, and — plainly — what remains PARTIAL. WP-01 through WP-10 remain COMPLETE for their recorded scopes.

WP-03 start baseline (2026-09-15): branch `codex/wp-01-desktop-foundation`, HEAD `a243891455ec17687dd54b5ac90d3bd64478a1a1`, empty index. Freshly re-run baseline: `go test ./... -count=1` PASS (15 packages at start), `go vet ./...` PASS, `web` `npm run typecheck` PASS, `npm test` PASS (15 tests), `npm run build` PASS. Go commands require `GOTOOLCHAIN=go1.25.0 GOSUMDB=sum.golang.org` on this host because the user-level `go env` sets `GOSUMDB=off`, which blocks toolchain verification. The working tree already contained the WP-01/WP-02 tracked and untracked work plus the user's brand rename; none of it was modified outside the WP-03 scope.


# 0k. WP-10 result: persistent memory, the deterministic checks and the Quality Center (2026-09-23)

## Scope completed

- **Status: COMPLETE for the 17 ROADMAP items.** Four acceptance clauses are PARTIAL and
  three verification limits are named at the end of this section; each is stated rather than
  glossed. Two independent reviews ran against the package and the defects they found are
  listed below with what was done about each.

- **Migration `000019_memory_and_consistency.sql`**: `memory_items` (DOMAIN_MODEL section
  14.1's fields), `memory_summary_sources` (section 14.2), `memory_entity_links` (section
  14.3), and `review_issues.source` — AGENT_CONTRACTS section 11.4's `source=deterministic|llm`
  mark, defaulted to `'llm'` so every finding written before it keeps its meaning. Forward
  only; no published migration was modified.

- **The memory aggregate** (`internal/domain/memory/`): the four shapes, the five memory
  types, section 14.5's invariants as functions (`CanModifyMemory` for the locked rule,
  `EmbeddingIsCurrent` for the index version), the float32 little-endian BLOB codec, and the
  fused score with section 12.2's own weights as named constants.

- **The store and its index** (`internal/infrastructure/database/memory.go`,
  `memory_index.go`): scoped, parameterised reads with the project in every WHERE clause; the
  exact-scan vector index over the BLOB column; a compile-time assertion that the store
  satisfies the port — which the first version did NOT, and nothing failed because the service
  takes an interface.

- **The recall** (`internal/application/memory/`): DOMAIN_MODEL section 14.4's scope with
  `ScopeFor` as the single run-to-scope mapping; `BuildMemoryContext` with section 12.1's four
  channels, the threshold applied to the semantic channel only, the pinned high-importance
  exception AC-MEM-003 names, the token budget, and `Truncated` reporting; `DeepRecall` with
  section 12.3's five steps and provenance per restored message.

- **FR-120's five user commands**: view, pin, edit, delete (with the summary-invalidation
  policy in both directions) and rebuild embeddings, each bound on `MemoryBinding` and each
  acting as the user, because section 14.5's locked rule turns on the actor.

- **The embedding capability**: `CapabilityEmbedding`, a deterministic feature-hash adapter,
  and an OpenAI-compatible `/v1/embeddings` client, both behind one `Embedder` port, with the
  provider resolution opt-in per project.

- **The runtime's memory layer**: the runtime now writes the USER's turn — which nothing did
  before, so every transcript was half a conversation — and recalls before it writes, in the
  order section 12.2 states, with the port on the runtime so a caller cannot invert the two.

- **The deterministic checks** (`internal/domain/consistency/`,
  `internal/application/consistency/`): six rules — costume continuity, prop continuity,
  location continuity, shot coverage and order, duration total, approved asset version — each
  reporting nothing when the data it would compare against is absent, and each with a positive
  and a negative test over the real schema.

- **The merge**: section 11.4's "合并两类证据，并标记 source". The findings are computed
  BEFORE the model runs, rendered into its task so it is not asked to re-derive a join, and
  merged with its own on (rule, entity, field) keeping the more severe statement.
  `ReviewPassed` makes a deterministic blocker overrule a happy verdict.

- **The Quality Center's findings pane** and the **Memory Center section**: the review read
  path's first caller, with each finding marked by which half produced it and a jump to the
  entity's section; and the memory list, the pin/edit/delete commands, the recall preview with
  each candidate's fused score beside its raw similarity, and a summary's sources as rows that
  open the source memory.

- **`MemoryCreated` is emitted.** ADR-0009 section 5 assigned it to this package and the name
  has been in migration 000013's closed vocabulary since WP-05; nothing emitted it until now.

- **`memory.deep_recall` deepens.** It was registered by WP-07 with a comment promising this
  package would deepen it, and the first version of this package did not: the tool still
  returned the recent window while its name, its manifest grant and section 12.3 all promised
  the walk. It now runs the flow when given a query and keeps the window when not.

## Defects found, by two independent reviews

The first review was a spec review; the second ran 167 mutations across the new code and
proved each survivor rather than asserting it.

**Blockers, all fixed:**

1. **The store did not satisfy its port.** `MemoryRepository` declared its own filter type and
   a `VectorCandidates` with a different signature, so `appmemory.Repository` was unsatisfied,
   `StorageAvailable` was false in every composed build, and every memory command returned "no
   memory store is configured" while the store sat implemented and tested. Nothing failed to
   compile because the service takes an interface and nothing asserted the implementation
   satisfied it. Fixed with the aliased types and a `var _ Repository = (*MemoryRepository)(nil)`
   assertion that immediately caught the drift, plus an integration test that drives the service
   the way the composition root does.

2. **Every summary row failed its own validation.** `Summarize` set `SourceType: SourceSummary`
   and no `SourceID`, and the domain demanded both-or-neither — so the summary table could never
   be written, and AC-MEM-004 and AC-MEM-005 had no runnable path. Fixed by stating the rule per
   type: a summary cites its sources through the relation table and the source columns stay empty.

3. **The context builder never applied its default threshold.** It clamped a negative to zero
   and passed zero through, so `PassesThreshold`'s `>= 0` admitted everything the search
   returned — literally FR-120's forbidden "无条件返回低相关结果". The constant existed and was
   read by nothing on that path.

4. **The vector search dropped the agent scope.** It filtered the project and the episode and
   discarded `scope.AgentKey`, while four comments asserted the scope filter ran in full. A
   decision agent could recall a supervisor's conversation by similarity.

5. **`memory.deep_recall` performed no deep recall** (see above), and **`RememberFact` had no
   production caller at all**, so the semantic type was unreachable, FR-120's semantic channel
   could only be empty, and AC-MEM-003's pinned exception could never fire in a real build.

6. **Three comments claimed behaviour the code did not have**: the memory binding's "UI 可跳
   原始消息", the quality centre's "jumpable reference", and the consistency checker's
   "compile-time proof" — which was `var _ = appconsistency.NewChecker`, a FUNCTION VALUE that
   proves nothing. All three are now true rather than reworded: the section navigates, the
   reference is a button, and the assertion is against a real interface.

**Majors, all addressed:**

7. **`LOCATION_CONTINUITY` was declared and never implemented** — a constant with a doc comment
   claiming scope item 14's location clause. It is now a rule with the two halves it needs.

8. **A pre-existing defect WP-09 shipped, found by this package's wiring test**:
   `agenttools.Build` requires `Deps.Gaps` and `composeAgents` never passed it, so the tool
   table refused and the whole agent stack was nil in every composed build. That is the third
   one-call-site omission of the same shape, and the first thing in the repository to assert
   the composed stack is non-nil.

9. **Two defects the acceptance tests themselves found**: the token budget did not apply to the
   store's recent window (the channel was appended after the loop that charges the budget), and
   level-one summaries picked up summary rows as their own sources, so the second summarise run
   condensed the first.

10. **`embedItems` silently discarded every vector**: it built a `VectorItem` without the model
    and version the index requires, and the index's refusal was swallowed by design.

11. **The merge and the deterministic pass had no coverage at all.** `grep -rn "Checks:"` across
    the test files returned nothing, and ten mutations survived — including dropping the blocker
    gate from the pass predicate, which is the whole point of the deterministic half.

12. **The desktop binding had no test**, and three project-scoped reads had no prefix-sharing
    neighbour, so a `LIKE 'project-1%'` mutation on the summary window, the pinned channel or the
    rebuild survived the suite.

## Acceptance

| Criterion | Verdict | Where |
|---|---|---|
| AC-MEM-001 Scope | **PASS** | `TestACME001ScopeIsolation`: two projects with identical content, the prefix-sharing pair, plus episode and agent scopes; three separate leak tests including the pinned channel and the deep-recall walk |
| AC-MEM-002 Self-hit | **PASS** | `TestTheRuntimeRecallsBeforeItWritesAndRemembersBothTurns` (the order, through a real run) and `TestACME002SelfHitExclusionAndOrder` (the exclusion, counted across all four channels) |
| AC-MEM-003 Threshold | **PASS** | `TestACME003ThresholdDropsBelowRelevance`: the semantic channel empty, no forced Top-K, the pinned fact present on its own rule; plus `TestACME003TheLockedRuleHoldsAcrossProjectsToo` |
| AC-MEM-004 Summary provenance | **PASS** | `TestACME004SummaryProvenanceAndPolicy`: sources linked and resolving, role/agent/time preserved, the deletion policy in both directions, the hole visible |
| AC-MEM-005 Deep Recall | **PASS** | `TestACME005DeepRecallWalksBackToTheMessages`: the summary found, reranked, the originals restored with provenance, the bound enforced, and the automatic path asserted not to walk |
| AC-E2E-004 Quality revision | **PASS** | `TestE2E004ACostumeFaultIsLocatedAndThenFixed` over the assembled production stack: located, blocked, a new version, the old one kept, the corrected board clean, other rows unchanged |
| AC-E2E-005 Deep memory | **PASS** | `TestACMME005TheCanaryScenarioRunsThroughTheWholeChain` over `testdata/canary-drama/memory-recall.json` |
| 跨项目泄露 0 | **PASS** | Five tests: the prefix pair, the deep-recall walk, the pinned channel, the store's own guards, and the summary window |
| Supervisor evidence | **PASS** | `TestE2E004TheFindingReachesTheReportShape`: the `source` mark and both version refs in the column the database holds |
| Memory 可查看/删除 | **PASS** | `MemoryBinding`'s thirteen methods, with the confirmation and actor rules asserted in `internal/desktop/memory_binding_wp10_test.go` |
| 低相关候选不强制返回 | **PASS** | `DefaultThreshold = 0.30` with the test that failed before it was applied |

## Verification

- `GOTOOLCHAIN=go1.25.0 GOSUMDB=sum.golang.org go test ./... -count=1` — **PASS**, 51 packages.
- `go vet ./... .` — **PASS**; `gofmt -l internal/ *.go` — clean; `git diff --check` — **PASS**.
- `node scripts/security-scan.mjs` — **PASS**, 552 files scanned.
- `node scripts/gen-canary-fixture.mjs --check` — **PASS** (6 fixtures);
  `gen-malicious-fixtures.mjs --check` — **PASS** (11);
  `gen-tool-schemas.mjs --check` — **PASS**; `gen-skill-packs.mjs --check` — **PASS**.
- `npm run typecheck` — **PASS**; `npm test` — **PASS**, 52 tests; `npm run build` — **PASS**;
  `npm run build:monoform` — **PASS**.
- `npx playwright test` — **PASS**, 25 passed / 1 skipped.
- `wails build` — **PASS**; `wails generate module` regenerated `MemoryBinding` and the models.

### Verification limits, stated

- **`go test -race` cannot run on this host**: the C toolchain reports
  `cc1.exe: 64-bit mode not compiled in`. This is an ENVIRONMENT FAILURE, not a pass, and no
  race evidence exists for this package.
- **`gen-tool-schemas.mjs --check` prints nothing on success** and exits 0; the exit code was
  captured explicitly rather than trusting the empty output.
- **No real provider was called.** Every provider test uses a double, and no embedding call left
  the process.

## Mutation testing

An independent review ran **167 mutations** across the new code: **52 killed, 81 survived, 34
unusable** (uncompilable or equivalent). The survivors are reported in three classes:

- **Reachable code with a missing test** — the merge and the deterministic pass (10 mutations,
  now covered by `merge_wp10_test.go`), the desktop binding (13 methods, now covered by
  `memory_binding_wp10_test.go`), the repository's own guards (6, now covered by
  `guard_wp10_test.go`), the three prefix-leak reads (3, now in the same file), and the rerank's
  internals.
- **Equivalent mutations** — a guard shadowed by a later check on the same path, or defence in
  depth that is genuinely redundant. These are correctly survivors.
- **Unreachable code**, which is the defect class this repository treats as a finding:
  `MockEmbeddingAdapter` and `OpenAITextEmbeddingAdapter` are executed by no test, and
  `Registry.WithMockEmbeddingAdapter` has no caller in any file. The consequence is recorded in
  ADR-0014 section 4 and in the limits below.

## What this package does NOT cover, stated plainly

1. **AC-MEM-001's "跨项目泄露 0" is asserted, not audited.** Five tests cover the paths that
   read a project, and every query in `memory.go` names the project in its WHERE clause — but
   there is no test that enumerates every read and asserts each one is scoped, so a NEW read
   added without a project guard would need a new test rather than failing an existing one.
2. **The deterministic rules cover storyboards only.** AGENT_CONTRACTS section 11.1's script
   rules and 11.2's asset rules have no mechanical half in this build: their artifacts have no
   structure the six rules' shape fits without inventing vocabulary the specification does not
   give. `CheckedStages` lists the one stage, and the checker returns no findings for others
   rather than guessing.
3. **FR-110's ten quality categories are partly covered.** The deterministic half covers
   Character (costume), Asset, Temporal (prop, duration) and Location. Narrative, Fidelity,
   Visual and Technical are the supervisor's skills. **Safety and Cost are covered by
   neither**, and nothing in the package says so — this is the honest gap this section exists
   to state.
4. **FR-110's `score` and `grade` are never populated.** The columns exist and the schema
   carries them; `ReviewFromOutcome` does not read them, so a report's score is always nil and
   its grade always empty. The report's `nextAction` is spelled `recommendedAction` in this
   build, which is what AGENT_CONTRACTS calls it and the PRD does not.
5. **AGENT_CONTRACTS section 12.1's `provenance` output field is absent** from
   `MemoryContext`. Provenance travels per item instead, which is the more useful shape for
   the four channels, and `SemanticSearched` is a field section 12.1 does not have.
6. **The keyword fallback is not reachable through configuration.** The deterministic embedding
   adapter exists and its kind is registered, but `IsUserConfigurableKind` refuses it and no
   composition root writes a provider row of that kind — the same three-guardrail treatment the
   other mocks have. ADR-0014 section 4 records the reasoning and the alternative.
7. **No third summary level.** FR-120's ladder is message → episode → project; this build
   implements the first two, and `summary.go` says why the third would need a trigger the
   specification does not give.
8. **`VectorIndex.Rebuild` refuses.** It needs an embedding provider the infrastructure layer
   has no registry for; the rebuild lives on the application service, which does have one, and
   the interface method says so rather than pretending.
9. **`memory.deep_recall`'s rerank is lexical, not semantic.** The bound that keeps it a
   rerank rather than a second search is asserted, but a summary whose wording differs from the
   query's will not be promoted by it. That is the cost ADR-0014 section 6 records.

## Git and data safety

- Existing user changes preserved: **yes**. The brand-rename files and the untracked
  `.zcode/plans/` scratch were left intact; `.zcode/` is now gitignored so a future `git add -A`
  cannot sweep it in, and a user-authored proposal document that a stray `git add` had tracked
  was removed from the INDEX only, with the file left on disk.
- Automatic commit/push/stash/reset/clean: **none** beyond the commits the user's instruction
  authorised.
- Secrets found or introduced: **none**. The embedding adapters reuse the controlled client, the
  secret resolution and the redacted audit; the deterministic adapter has no network path.
- Migrations/backups: `000019` is forward only, `000001` through `000018` were not modified, and
  no user database was touched.
- Real Provider calls: **none**.

# 0l. WP-11 result: video, audio, subtitles, timeline and export (2026-09-23)

## Scope completed

- **Status: COMPLETE for all 14 ROADMAP items, with two PARTIAL.** Items 11 and 14 were reported
  NOT BUILT in the first report and are now built; item 13's references case and the version pickers
  were closed after it. See "Item 11 and item 14, closed after the first report" and "What remains
  PARTIAL" below — the latter records, for each closure, what the first account of it got wrong.
  Every closure is stated rather than glossed. Two independent reviews ran against the
  package — a spec review and a mutation/quality review — and the defects they found are listed
  with what was done about each.

- **The one audited subprocess** (`internal/infrastructure/media/ffmpeg.go`): `os/exec` in exactly
  one file, structured `[]string` argv with no shell, `-`-prefixed paths refused, bounded output,
  timeouts, isolated scratch directories, and fail-soft when the machine has no ffmpeg. The
  repository's scanner gained a precise allowlist entry naming the file, the `os-exec` rule and
  ADR-0015; it refuses wildcards and fails on a stale entry, so removing the adapter without
  removing the entry is also a failure.

- **Migration `000020_media.sql`**: `subtitle_tracks` (versioned, eight statuses, one approved per
  episode), `subtitle_cues` (millisecond ranges with `end_ms > start_ms`, a `dialogue_line_id` that
  makes "which lines have no cue" a join, and a `status` column distinguishing a generated cue from
  an edited one), and `episode_exports` (the manifest, the output hash, the approval trace).
  Forward only; no published migration was modified.

- **The media domain** (`internal/domain/media/`): `Timecode` with SRT and VTT formatting and a
  parser, `Cue`/`Track` with `ValidateTrack` and `MissingLines`, and `Manifest` with the two
  completeness rules AC-MEDIA-003's traceability depends on.

- **The subtitle service** (`internal/application/media/subtitle_service.go`): drafts from a script
  version's SPOKEN lines (dialogue and narration; an action line is a direction to the production
  and gets no subtitle), an editor that replaces cues in one transaction, SRT/VTT rendering, and
  `Missing` as the same join the draft's filter uses so the two agree by construction.

- **The timeline** (`internal/application/media/timeline.go`): the ordered read model, joining the
  board's own rows to the media approved for each, the audio a line produced, and the cues that
  fall inside each shot's running span. A read model rather than a table, because the order already
  exists in `storyboard_items.ordinal` and a second copy would eventually disagree.

- **The export service** (`internal/application/media/export_service.go`): the manifest assembly,
  the three-pass compose through the engine, the output through the real content-addressed store,
  and the record. It composes from approved PANEL FRAMES rather than shot videos, which ADR-0015
  section 2 rules on and section 0l returns to below.

- **The save path** (`save_dialog.go`, `MediaBinding.SaveExport`): the first way this application
  has ever had to write a file to where a USER pointed. The destination comes from a native dialog
  and NEVER from a request parameter — a compromised frontend cannot name a write target — and the
  bytes stream from the store to the destination without passing through the browser, because
  `ReadResultFile`'s 64 MiB data-URL cap would reject any real episode.

- **The video and audio job submissions** (`internal/desktop/media_jobs.go`): `SubmitVideoJob` with
  first-frame, last-frame and reference assets (bounded at eight), `SubmitAudioJob` with the
  dialogue line as the job's entity, and both bounded by confirmation-shaped limits (60 seconds,
  2000 characters).

- **The Final Ruleset** (`internal/application/consistency/final.go`): AGENT_CONTRACTS section
  11.4's eight clauses as ten rules, dispatched from the same `StoryboardConsistencyChecker.Check`
  switch the storyboard rules use, so a build has both or neither.

- **The two `final_episode` agents** (`production.execution.final_episode` and
  `production.supervision.final_episode`): the execution agent states an export recipe and the
  supervisor reads whether the film the approved pieces assemble is the film the episode intended.
  Section 11.4's division is real: the deterministic pass runs FIRST and its findings travel into
  the supervisor's task, so a model is told what a join found rather than asked to notice it.

- **The three sections** (`web/src/components/studio/{video,audio,timeline}-view.tsx`) and their
  client (`web/src/services/desktop/media.ts`), with the query-empty/command-throws split every
  desktop client in this repository keeps. All three were `available: false` before this package.

- **The four documents** (`internal/domain/screenplay`, `internal/domain/shotlist`,
  `internal/application/media/documents.go`): a script in plain text or Fountain, a shot list in
  aligned text or CSV, SRT and VTT subtitles, and the export manifest as a `.json` a user can take
  away. Each goes through the same save dialog an MP4 uses. ROADMAP item 11.

- **The single-episode walk** (`acceptance_wp11_e2e_test.go`): script → board → subtitles → a video
  version and an audio version attached to the assets aggregate → timeline → export → Final Ruleset
  → all four documents, asserting on the database at each step. ROADMAP item 14, and the test that
  found the manifest-reference defect recorded below.

## Acceptance criteria

| Criterion | Result | Evidence |
|---|---|---|
| AC-MEDIA-001 (9 clauses) | PASS, with clause 7 PARTIAL | `internal/application/jobs` and `internal/infrastructure/jobs` cover submit, remote ID, poll, restart, fetch, validate, duplicate and cancel. **Clause 7, "asset version", is PARTIAL**: no test wires a video job's result to an `asset_version` row. The job's result is stored and referenced; the asset aggregate is not what holds it. |
| AC-MEDIA-002 (5 clauses) | PASS, with clauses 1 and 2 PARTIAL | Editable subtitles, valid SRT/VTT round-tripped through the parser, and missing-line detection are covered by `subtitle_wp11_test.go` and `domain/media/subtitle_test.go`. **Clause 1** (`dialogue line → voice job`) is implemented by `SubmitAudioJob` and asserted nowhere; **clause 2** (`audio linked to character/line`) has no read that enumerates a script version's dialogue lines, so the character half is carried by the subtitle cue's speaker rather than by the audio asset. |
| AC-MEDIA-003 (7 clauses) | PASS, with clause 2 PARTIAL | `acceptance_wp11_test.go` walks ordered shots, clip replacement, export, playability (ffprobe read-back) and manifest traceability over the real schema. **Clause 2, "audio/subtitle"**: the subtitle stream is asserted in that walk and the AUDIO composition is covered separately in `compose_test.go`, not in the same composition. |
| Final Review | PASS | The two agents and the deterministic ruleset are composed and dispatched; `final_reader_test.go` drives the adapter over the real schema and `TestTheCheckerDispatchesTheFinalStageToTheFinalRuleset` asserts the routing. |
| output playable | PASS | ffprobe read-back with stream count, duration and dimensions, in two suites. ffmpeg is present on this host, so these ran rather than skipped. |
| 无 Shell 注入 | PASS | Structured argv, no shell, `-`-prefixed paths refused, and a real composition with a crafted value. |
| 导出清单可追溯 | PASS | The manifest is decoded, each cited reference is read back from the database, and the traceability rule compares it against what is currently approved. |

## Item 11 and item 14, closed after the first report

**Both were reported NOT BUILT and both are now built.** They were closed in the same work package
rather than deferred, and the walk item 14 asks for is what found the defect below.

- **ROADMAP item 11, "Script/Storyboard/Subtitle/Manifest Export", is COMPLETE.** All four documents
  exist. The script renders in `txt` and Fountain (`internal/domain/screenplay`), the shot list in
  aligned text and CSV (`internal/domain/shotlist`), the subtitle in SRT and VTT (built earlier), and
  the manifest as a `.json` a user can take away — which had NO caller before: `save_dialog.go`'s
  `.json` filter was unreachable, and `ExportRecordDTO.ManifestJSON` was a block of text a user could
  read and not save.
  `internal/application/media/documents.go` assembles them,
  `MediaBinding.{ExportScript,ExportShotList,ExportManifestDocument,SaveDocument}` exposes them, and
  the timeline section offers all three with a preview before a save. Each document goes through the
  SAME save dialog an MP4 uses, so a document cannot be written anywhere the user did not point at.
  Evidence: `internal/infrastructure/database/documents_wp11_test.go` over the real schema, plus
  `internal/domain/{screenplay,shotlist}/*_test.go` for the two formats' own conventions.
- **ROADMAP item 14, "单集 E2E", is COMPLETE.** `acceptance_wp11_e2e_test.go` walks one episode
  through script → board → subtitle draft and approval → a video version attached with a
  `usage_role = 'video'` usage → an audio version attached → timeline → export composed and probed →
  Final Ruleset → all four documents, asserting on the DATABASE at every step rather than on a
  service's own account.
- **ROADMAP item 3, "Shot Video Version", is COMPLETE for its model.** The walk attaches a shot's
  video as an `assets` row of type `video` with an `asset_usages` row whose `usage_role` is `video`,
  which is ADR-0015 section 4's ruling, and asserts the usage exists. The submission path is
  `SubmitVideoJob`; the attachment is what the walk exercises. What remains absent is a real video
  PROVIDER, which item 1 permits.

### The defect the walk found, and no unit test could

`final_reader.go` read a manifest's per-shot references by appending the `asset_version` and `panel`
lists and keying both by shot. `ExportService` writes BOTH for every shot, so the panel OVERWROTE the
asset version — and the traceability rule then compared a panel version id against the media version a
shot currently approves and reported **every export as stale**. Each kind was correct on its own, which
is why the ruleset's own tests and the export's own tests were both green.

Fixed: the two kinds are keyed separately (`shot:` for the media, `panel:` for the panel). The walk is
what found it, which is the argument for item 14 existing at all.

## What remains PARTIAL

**Two of the five recorded here have been closed since the first report**, and each closure is worth
naming because the first account of it was wrong:

- **ROADMAP item 13, "中断恢复", is COMPLETE.** The restart test covered a job with no references, which
  proved the remote ID survives and said nothing about the frames — a resumed job is POLLED, and a poll
  is given no references at all. `TestVideoRestartKeepsTheReferencesAJobWasSubmittedWith` recovers a job
  carrying a reference, a first frame and a last frame and compares the input field by field, and
  `TestRunnerVideoCarriesReferencesAndFrames` grades the pipeline that carries them (which had NO test
  at all: the stub discarded its request, so it could have been deleted with the suite green).
- **ROADMAP item 4's "no picker" half is COMPLETE, and the reason first recorded here was FALSE.** It
  said "no read enumerates a script version's lines". `GetScriptStructure` has always listed them, with
  each line's id, type and character. What was genuinely missing was the version HISTORY —
  `ListScriptVersions` was on the repository and dispatched by `Service.ListVersions` since WP-08 with
  no binding, while the skeleton and strategy families both had one. It has one now, so the audio
  section and a subtitle draft resolve their version (and, for audio, their line) by picking, and the
  document exports offer a `versionId` picker for both script and board.

What genuinely remains:

1. **ROADMAP item 1's video provider is a complete Mock, which the item permits** ("或完整 Mock").
   Its payload is a twenty-four byte container header rather than decodable video, which is why
   ADR-0015 section 2 composes exports from panel frames instead. A real adapter is not built, and
   building one is a WP-12-or-later decision rather than an oversight.
2. **No read of a LINE's approved audio.** `SubmitAudioJob` attaches a job to a `dialogueLineId`, and
   the timeline's audio column is per SHOT ("whether audio is approved for any line in this row's
   scene"). So the audio section reports the shot's state and names the line it is voicing, and a
   per-line "which lines have audio" list would need a read that does not exist. ROADMAP item 4's
   mapping is otherwise complete.

## The two AGENT_CONTRACTS section 11.4 clauses this build does not fully answer

- **"所有必需 Shot 有批准视频"** is implemented as approved MEDIA, which in this build is usually a
  panel image. The divergence is deliberate and now recorded in the rule's own comment: a literal
  reading would report every shot of every episode, because the video adapter is a mock. The
  condition for closing it is stated — a build with a real video adapter would require
  `MediaKind == "video"` for the shots a director marked as needing motion.
- **"黑帧/空帧/静音异常"** is answered as far as a probe can: a placeholder's size, a type ffmpeg
  cannot compose, an audio file that is a container header. **A black frame inside a well-formed
  video and a silent passage inside well-formed audio are NOT caught**, and the rule says so in its
  own comment rather than reporting a clean result it did not earn.

## The licence clause, and why it is a reported gap

**"资源许可证元数据" has nowhere in this build to read a licence from.** Verified:
`grep -rni "license\|licence\|rights" --include="*.sql" internal/infrastructure/database/migrations/`
finds only two prose comments using the English word as a synonym for "justification"; there is no
column anywhere. PRD R9's mitigation is a THIRD_PARTY_NOTICES document rather than a per-asset
rights record.

A rule that looked and found nothing would either report every asset as unlicensed — a page of
findings a user learns to ignore — or report none and read as green. So `FinalFacts.LicensesChecked`
is `false` in this build and the rule **reports the gap itself**, once, as a minor finding that
names what is missing. Closing it means adding a licence column to the asset aggregate and a command
to record it.

## The defects the two independent reviews found

### Spec review — four blockers

1. **CRITICAL: the Final Ruleset was inert in every real build.** `final_reader.go` prepared four
   SQL statements against columns the schema does not have:
   `storyboard_versions.episode_id` (the episode is on `storyboards`),
   `dialogue_lines.script_version_id` (the line reaches its version through `scenes`),
   `artifact_staleness.id` (the primary key is the pair `(artifact_type, artifact_id)`), and
   `script_versions.episode_id` (it hangs off `scripts`). The FIRST of these runs unconditionally, so
   `FinalFacts` returned an error for every episode; `stagepipeline`'s `if err == nil` discarded it;
   and all eight of section 11.4's clauses contributed nothing while the ruleset's own tests stayed
   green over a struct-literal double. One of those tests asserted in a comment that the adapter "has
   its own tests over a real schema", and it did not.
   **Fixed**, and `internal/infrastructure/database/final_reader_test.go` is the test that would have
   caught it: it drives the real adapter over the real schema, and eight mutations that reintroduce
   the broken statements are all killed by it.
2. **HIGH: burned-in subtitles failed on every Windows machine.** `escapeFilterPath` escaped the
   drive letter's colon and returned the value UNQUOTED, which ffmpeg rejects — the filtergraph reads
   `C` as an option name and the rest of the path as its value. Escaping the colon was necessary and
   not sufficient; the single quotes are what make it an expression. The test covering the function
   asserted the string's SHAPE and never ran ffmpeg, and the only subtitle compose test used sidecar
   mode, which takes a different branch. **Fixed**, and `TestBurnedInSubtitlesComposeADecodableFilm`
   is the test that runs it — verified to fail against the unquoted form with ffmpeg's own
   "Unable to parse option value" error.
3. **HIGH: no command could move an artifact into REVIEW.** Both `ApproveSubtitleTrack` and
   `ApproveExport` refuse a row that is not `under_review`. For an export the only caller that ever
   wrote that status was an acceptance test reaching past the service to the concrete repository; for
   a subtitle track nothing wrote it at all — the value lived in the schema's CHECK and in the
   approval's WHERE clause and nowhere else. So in a real build neither approval was reachable.
   **Fixed**: `SubmitForReview` on both services, the repository methods moved onto the PORTS, binding
   methods for both, and the two fixtures rewritten to walk that path instead of bypassing it.
4. **The ADR's own correction.** It claimed the `final_episode` agent "has a write tool [that]
   writes a recipe". It has NO write tool — its five granted tools are all reads, and the recipe is
   the model's structured output recorded on the run. The paragraph now says so.

### Quality review — 321 mutations, 149 killed

The survivors are reported by class in section 0l's table below. The four with consequences:

1. **`internal/application/media` had NO test file at all**, and 34 of 47 mutations to its three
   services survived — including deleting the "shots with no approved media" refusal that
   `TestACMEDIA003AnExportRefusesShotsWithNoMedia` is named for. That test asserted `err == nil`
   alone, so a DIFFERENT refusal from a later rule satisfied it. **Fixed**: the assertion now names
   the refusal's message, and a mutation that deletes the guard is killed.
2. **`save_dialog.go` had 0.0% coverage**, and two of its nine surviving mutations were
   security-relevant (the traversal reduction and the Windows device-name refusal in
   `sanitizeSuggestedName`). **Fixed**: `save_dialog_test.go` covers the sanitiser, the filter, the
   atomic write sequence and the binding's storage-key guard.
3. **Three tests were written against the constants they were testing** — `DefaultMinMediaBytes`,
   `DefaultMaxWidth` and `DefaultMaxFPS` appeared on both sides of their comparisons, so a mutation
   that changed a bound moved the test with it. **Fixed**: the tests now use literals and
   `TestTheDefaultsAreTheNumbersTheTestsPin` states what the constants are, so a deliberate change
   fails that assertion once and is updated once.
4. **Two tests could not fail.** The audio-severity assertion compared a finding count against a
   filtered count over a list in which every element was already a blocker — equal by construction.
   And `TestAFrameOfTheWrongShapeIsPaddedRatherThanCropped` asserted OUTPUT DIMENSIONS, which a plain
   `scale` also produces, so removing `pad` from the filtergraph (stretching the picture instead of
   letterboxing it) survived. **Both fixed**: the severity is asserted directly, and the padding test
   now extracts a frame and checks the PIXELS — verified to fail with "the sides are {248 39 39 255},
   want the black bars padding draws" when `pad` is removed.

## Verification, with results

- `go test ./... -count=1` — **PASS** (62 packages; no failures).
- `go vet ./...` — **PASS**.
- `gofmt -l .` — **PASS** (no files).
- `node scripts/security-scan.mjs` — **PASS**: 591 files scanned, 2 audited dynamic-execution
  exceptions, 4 audited legacy direct-call files with named owners.
- The four `--check` generators — **PASS**.
- `npm run typecheck` — **PASS**. `npm run test` — **PASS**, 59 tests. `npm run build` — **PASS**.
  `npm run build:monoform` — **PASS**.
- `npx playwright test` — **PASS**, 25 passed / 1 skipped (a pre-existing canvas crop skip).
- `wails build` — **PASS**; `wails generate module` regenerated `MediaBinding` and the models.
- Real ffmpeg 5.1.1 composes a real MP4, reads it back with ffprobe, and the composed file carries
  the audio and subtitle streams that were asked for.

### Verification limits, stated

- **`go test -race` cannot run on this host**: the C toolchain reports
  `cc1.exe: 64-bit mode not compiled in`. This is an ENVIRONMENT FAILURE, not a pass, and no race
  evidence exists for this package. **Every concurrency claim in this package is from reading, not
  from a detector.** The two synchronised structures (`FFmpegEngine.versionOnce` and
  `MediaBinding`'s mutex) were read and are correct; `mediaWiring.saveFile` is written before
  `attach` reads it at startup and is not synchronised, which is safe in the current call order and
  would need a lock if a binding call could arrive earlier.
- **No real paid provider was called.** The video and audio providers are the complete Mock the
  roadmap permits; no generation left the process.
- **`wails build` requires the pinned CLI on PATH.** It is not on `scripts/verify.sh`'s PATH by
  default, so that gate SKIPS it and says so; it was run explicitly and separately.

## Mutation testing, in summary

Two rounds, both reported in full above. The spec review's round and the quality review's **321
mutations across eight areas: 149 KILLED, 157 SURVIVED, 9 NO-COMPILE, 6 NO-ANCHOR**, with the
survivors classified as reachable-with-a-missing-test, equivalent, or unreachable. The
reachable-with-a-missing-test survivors in `internal/application/media` (34), the root-level
adapters (`save_dialog.go`, `media_reader.go`, `media_wiring.go`'s two port adapters) and the
frontend probes are the ones the fixes above address; the remainder are recorded here rather than
silently dropped:

- **`internal/application/media`'s own refusals** — the cross-episode guards, the FPS and
  subtitle-mode checks, the empty-shot and engine-unavailable refusals, and the four cue-timing
  rules of `cuesFor` — have no unit test. They are exercised only through the acceptance walk's
  happy path. **Not fixed in this package**; the package's test file does not exist.
- **`media_reader.go` and `media_wiring.go`'s file-store and temp-dir adapters** are covered by the
  wiring test's construction path but not by an assertion about their behaviour.
- **`internal/desktop`'s DTO mapping layer** (twelve methods) has no Go test; the frontend calls
  them through Wails bindings that no Go test drives.
- **`ParseTimeRange` and `DecodeManifest`** had no production caller. (DecodeManifest's bypass is
  fixed: `final_reader.go` now reads the manifest through it, so its empty/`null`/missing-schema
  guards apply on the production path.)

## What this package does NOT cover, stated plainly

1. **No script export, no storyboard export.** ROADMAP item 11 asks for four artifacts and two are
   built.
2. **No single-episode E2E walk.** ROADMAP item 14.
3. **No real video provider.** The mock's bytes are a container header, which is why exports compose
   from panel frames.
4. **`video_generation` has no agent and no test.** The stage is configured in the policy table and
   driven by `SubmitVideoJob`; ADR-0015 section 5 records why, and the stage's policy entry is
   unreachable in practice.
5. **Black frames and silent passages inside well-formed media are not detected.** Section 11.4's
   fifth clause is answered only as far as a probe can.
6. **The licence clause is a reported gap**, not a check — see above.
7. **Staleness for an export is a deterministic finding** that compares the manifest against what is
   currently approved, not a node in the staleness chain: migration 000012's `artifact_type` CHECK is
   closed and published, and ADR-0015 section 7 records the ruling.
8. **`scripts/gen-canary-fixture.mjs` produces no media fixture.** The canary drama's subtitles and
   manifest are not generated, so a drift between the canary and the media code is not detected.
9. **No "FFmpeg 路径" setting, which PRD FR-180 lists.** The two program names are constants in the
   adapter, because a path from a settings field or a request would be a way to run an arbitrary
   program — see that file's comment for why the reasoning applies even though SECURITY section 11's
   "文件创建使用 exclusive" is about files. A machine whose ffmpeg is not on PATH gets the
   engine-unavailable diagnostic rather than a field to correct it.

## Git and data safety

- Existing user changes preserved: **yes**. The brand-rename files and the untracked
  `.zcode/plans/` scratch were left intact; a user-authored proposal document remains untracked and
  was not added to the index.
- Automatic commit/push/stash/reset/clean: **none** beyond the commits the user's instruction
  authorised.
- Secrets found or introduced: **none**. The media adapter has no network path, the save dialog
  writes only where the user pointed, and no provider key reaches the frontend.
- Migrations/backups: `000020` is forward only, `000001` through `000019` were not modified, and no
  user database was touched.
- Real Provider calls: **none**.
- Build products: `build/bin/` and `web/dist/` are gitignored; `web/dist/.gitkeep` was restored after
  a build removed it.

# 0j. WP-09 result: the production pipeline, the gap report and the MONOFORM bridge (2026-09-22)

## Scope completed

- **Status: COMPLETE for the 14 ROADMAP items.** Three acceptance clauses are PARTIAL and
  two verification limits are named at the end of this section; each is stated rather than
  glossed.

- **The mechanism, extracted**: `internal/application/stagepipeline/` holds the stage
  machine — the attempt, the FIX read-back, the gate's ordering, the manual edit's two-step
  — and `scriptpipeline` became a LAYER that states a stage map, an approval, a lock read
  and a manual-edit writer. `productionpipeline` is the second layer. WP-08's own reviews
  found four defects shaped "two places disagree about the same fact", so a second copy of
  the gate was not written.

- **Stage vocabulary**: `director_plan` added to `DocumentedStageNames` and to the engine's
  policy table with section 10.1's own settings (`supervision: conditional`, `userGate:
  required`). The reference list is now eleven where FR-100 lists ten, and its test says why.

- **Migration** `000018_production.sql`: `storyboard_items` gains FR-070's
  `first_frame_description`, `last_frame_description` and `video_motion_description`;
  `asset_gap_reports` and `asset_gap_items` are new, with an approved-per-episode unique
  index. Forward-only, no published content touched.

- **The gap report**: `internal/domain/asset/gap.go` states the one rule that matters —
  `satisfied` must name the asset that satisfies it and `missing` must not — and
  `application/assets/gap.go` implements create/list/get/approve plus
  `UnresolvedRequiredItems`, which REFUSES when no report is approved rather than reporting
  an empty gap list.

- **Provenance completed**: `asset.Version` gained the five columns migration 000009 had
  always had and the mapper dropped (`parent_asset_version_id`, `variant_type`, `seed`,
  `source_agent_run_id`, `created_by_id`), and `AttachJobResult` turns a succeeded job into a
  CANDIDATE version — a status nothing in the build wrote before, so a panel could never
  have had a candidate to approve.

- **The impact edge**: `storyboard_panel_version` now consumes the asset version it
  approved, so section 15.1's asset-version approval switch is a trigger that reaches
  something. It propagates from the version being REPLACED — the first version propagated
  from the newly approved one, and the wiring test caught it.

- **The batch**: `productionpipeline` with `CheckStoryboardGate`, `RunImageBatch`,
  `CollectBatchResults` and `ApproveCandidate`, reachable from the desktop binding and from
  the Storyboard Table section.

- **Tools**: `script.read_shots` (the stage had no way to list the shots it was boarding),
  `asset.create_gap_report`, `asset.read_gap_report`, and
  `storyboard.create_storyboard_version` now writes its ROWS.

- **Mocks**: `KindMockImage` with a real PNG renderer, five production execution branches
  and the supervision split in the text mock, so the whole pipeline runs in CI without a
  paid provider.

- **Skills**: all nine production documents are real prose. The assertion that required them
  to be skeletons is REVERSED, and now requires both packs to be prose.

- **MONOFORM**: a versioned envelope with origin, nonce, schema and size validation in one
  place (`web/src/services/desktop/monoform-bridge.ts`), `open_shot` and `shot_updated` as
  the two directions FR-060 asks for, the iframe's permission frame narrowed from
  `camera; microphone; clipboard-write; download; fullscreen` to `clipboard-write;
  fullscreen`, and the studio's output rebuilt.

- **The two sections**: Director and Storyboard Table are real, with the plan history, the
  previs studio opened from a shot, the board's rows, the single-row editor, the supervisor,
  the gate, and the asset versions drawer with the impact analysis shown BEFORE the switch.

## Defects found, by two independent reviews

A specification review and a quality review with 53 mutations ran against this package. The
findings were real and each is fixed in a commit of its own.

**The specification review found four blockers:**

1. **The batch had no caller.** `RunImageBatch` compiled, was tested, and nothing composed
   it — no binding, no composition root, no UI. Four of AC-BOARD-003's seven clauses and four
   ROADMAP items were unreachable from any build. Fixed by the bindings, the attachment, and
   the two commands it was missing (`CollectBatchResults`, `ApproveCandidate`).
2. **No candidate approval existed at all.** Fixed by `ApproveCandidate`, which delegates to
   the storyboard service so section 9.5's rule lives in one place.
3. **AC-ASSET-002's asset usage had no producing path.** The job-to-version command wrote the
   version and its files and no usage, so the candidate set section 9.5 reads was empty.
   The collection now writes it.
4. **AC-BOARD-002's supervisor clause was met by inert data.** A test read a fixture's own
   `mustLocateAt` and asserted it agreed with itself; no supervisor ever ran against the
   board. Findings now name the row, and two tests assert it — one faulted, one consistent.

**The quality review returned thirteen survivors.** The worst cluster was that NO TEST EVER
ATTACHED A PRODUCTION PIPELINE TO THE BINDING, so four routing mutations survived — one of
them the same `shot_ids` defect this package had already fixed once, whose regression test
lived on the wrong side of the layer boundary. `routing_wp09_test.go` closes it, and the
batch's concurrency limit and the collection's prompt/seed are now asserted too.

**Defects the fixes uncovered, which had shipped earlier:**

- **Every review's evidence was silently dropped since WP-08.** The review-report schema
  defines `evidence` as an array; `IssuesFromOutcome` read a field named `evidenceJson`,
  which the schema does not define. The struct decoded happily and the field was always
  empty, so section 7.6's rule that evidence points at what was read was unsatisfiable in
  principle.
- **A stage with no supervisor could never reach its gate.** The mechanism parked every
  attempt at `reviewing`, where `ApplyGate` refuses it, so the pipeline stopped at the first
  unsupervised stage — `asset_analysis`, which section 10.1 marks `supervision: false`.

## Acceptance

| Criterion | Status | Evidence |
|---|---|---|
| AC-ASSET-001 | **PASS** | `TestACAsset001TwoCandidatesThenAVersionSwitch` — all six clauses in order, including the last one read back |
| AC-ASSET-002 | **PARTIAL** | `TestAttachJobResultRecordsEveryProvenanceFact` covers physical file, hash, job, provider/model, prompt, parent refs and agent/run. **The asset-usage fact is asserted by `TestACCollectedJobBecomesACandidateVersion`, which is a different command**: the job-to-version write itself records no usage |
| AC-BOARD-001 | **PARTIAL** | Twelve shots, FR-070's fields field-by-field, the supervisor and the gate's block are asserted. **Generating from an APPROVED script is not ENFORCED**: the board stage's tool checks that the cited version exists and that the rows' shots belong to it, but nothing refuses a board built from an unapproved script |
| AC-BOARD-002 | **PASS** | `TestTheBadStoryboardFixtureNamesTheFaultTheCriterionDescribes`, `TestTheSupervisorLocatesTheRowAtFault` (the supervisor locates the row, with evidence naming two references), `TestTheSingleRowFixChangesOnlyThatRow` (every other row byte-identical, revision included) |
| AC-BOARD-003 | **PARTIAL** | Two candidates a shot, the concurrency limit, the collection, the approval and the restart-idempotency are asserted. **Cancel and retry are the job core's commands and are reachable by job id, but no test drives them from a batch**: the batch exposes no batch-scoped cancel or retry |

## Verification

- `go test ./... -count=1` — PASS (all packages)
- `go vet ./... .` — PASS
- `gofmt -l` — clean
- `git diff --check` — PASS
- `node scripts/security-scan.mjs` — PASS (513 files, 1 audited dynamic-execution exception, 4 audited legacy direct-call files)
- `gen-canary-fixture.mjs --check` — PASS (5 fixtures)
- `gen-malicious-fixtures.mjs --check` — PASS (11 fixtures)
- `gen-tool-schemas.mjs --check` — PASS
- `gen-skill-packs.mjs --check` — PASS
- `npm run typecheck` (web) — PASS
- `npm test` (web) — PASS, 52 tests
- `npm run build` (web) — PASS
- `npm run build:monoform` — PASS, output synced to `web/public/monoform/`
- `npx playwright test` — PASS, 24 passed / 1 skipped
- `wails build -s` — PASS; the bindings regenerated
- **`go test -race` — ENVIRONMENT FAILURE, not a pass.** The host's C toolchain reports
  `cc1.exe: 64-bit mode not compiled in`, so no race-aware run is possible here. Every
  concurrency-relevant test in this package runs without the detector.

## Mutation testing

53 mutations over the package's new code, run by an independent reviewer with a harness that
asserts each anchor is present before substituting and hash-checks its restore. Thirteen
survived; the clusters that matter are fixed and re-run:

- the binding's stage routing — 4 survivors, now 4 mutations killed by `routing_wp09_test.go`
- the batch's concurrency limit — 1 survivor, now killed by `TestTheBatchSubmitsNoMoreAtOnceThanItsLimit`
- the candidate ceiling — 1 survivor, now killed by `TestTheBatchRefusesMoreCandidatesThanTheCeiling`
- the collection's prompt and seed — 2 survivors, now killed by `TestACCollectedCandidateKeepsItsPromptAndSeed`

The remaining survivors are named in the review's own report; they are either equivalent
mutations (reordering two disjoint stage-set checks changes nothing, so no test can kill it)
or guards on test-only code.

## What this package does NOT cover, stated plainly

1. **Generating from an approved script is not enforced** (AC-BOARD-001, above). The
   materials are approved in the walk, but nothing REFUSES a board built from an unapproved
   script. The fix is a status check in the board stage's write path, which requires deciding
   whether a draft script may be boarded experimentally — a product question rather than a
   defect.
2. **A batch exposes no scoped cancel or retry** (AC-BOARD-003, above). The job core's
   commands work by id and a user can cancel the jobs they see; a batch-scoped command would
   need the batch to BE a row the database holds, which ruling 5 of ADR-0013 explains it
   deliberately is not.
3. **`go test -race` cannot run on this host.** Stated above; it is an environment failure and
   is not reported as a pass.
4. **Per-stage model policy remains per-layer**, which the user chose when the plan was set.
5. **The MONOFORM bridge is the BASIC form.** It opens a shot, reports a camera and exports a
   frame; FR-060's full two-way scene synchronisation would need the studio's object model to
   travel as well, and is not built.

## Git and data safety

- existing user changes preserved: **yes** — the user's brand rename and the untracked scratch
  files under `.zcode/plans/` were not touched or committed
- secrets found/introduced: **none** — the image mock is registered by no production
  composition (ADR-0013 ruling 6) and no credential is read on the test path
- migrations/backups: `000018_production.sql` is forward-only; no published migration was
  edited; no database was deleted or reset
- Automatic commit/push/stash/reset/clean: **none** (the commits were requested by the user)

---

# 0i. WP-08 result: the script pipeline, the locks and the stated stage map (2026-09-21)

## Scope completed

- **Status: COMPLETE for the 15 ROADMAP items.** Two acceptance items are PARTIAL and
  three verification limits are named at the end of this section; neither is cosmetic.

- **Domain**: `internal/domain/script/structure.go` and `draft.go` — `ScriptStructure`
  with its gapless-ordinal validation and explicit ceilings (200 scenes, 500 lines and
  200 shots per scene), the `LockableField` vocabulary PER FAMILY, `FieldLock`,
  `StrategyEventLink`, and the `ScriptStructureDraft` shape a model is given, which
  states no identifier, no ordinal and no duration.

- **Diff**: `internal/domain/script/diff.go` — pure functions over two versions,
  positional by ordinal, with the lock state carried on every item.

- **Migration**: `000017_script_field_locks.sql` — one lock table for all three version
  families, with `version_id` deliberately NOT a foreign key (three families, three
  tables) so the write path is what keeps it honest.

- **Migration 000008's two link tables gained writers**: a skeleton's selected events
  and a strategy's per-event treatments are written WITH the version they belong to, in
  one transaction, because §7.4 and §7.5 make those sets part of what the artifacts ARE.

- **Service**: whole-version writes, lock enforcement for all three families at the
  WRITE PATH, the event-link reads, the reference-existence check (§17's 「引用存在性」,
  which the schema cannot make — the citation columns have no foreign key), the version
  histories, `familyOfVersion`, and the two upstream approvals.

- **Pipeline**: `internal/application/scriptpipeline` — the stated stage→executor and
  stage→supervisor map, the workflow-state rendering, the FIX read-back from the
  decision row, the user's gate (which approves the ARTIFACT), the manual-edit path, and
  the run→version walk.

- **Runtime**: two prompt layers (`lockedRefs`, `fixIssueIds`) that the
  execution-request schema had carried since WP-07 with nothing populating them, and
  `Engine.Transition`.

- **Tools**: `script.create_script_structure` and `script.read_script_structure`, plus
  event-link fields on the two upstream create tools; `scripts/gen-tool-schemas.mjs`
  regenerated, with `--check` in the gate.

- **Mock**: the three script-stage branches, each asking for the write that stage OWES.

- **Skills**: EIGHT real documents (`skills/script/**`), replacing WP-05's generated
  skeletons. `scripts/gen-skill-packs.mjs` now writes a manifest always and a document
  only when MISSING — the line that makes the documents possible, since running it again
  would otherwise erase them.

- **Model policy**: §13 resolved per LAYER (`agent_wiring.choice`), with the table the
  schema already carried. FR-140's per-STAGE keys are DEFERRED (ADR-0012 §5).

- **UI**: the Script section renders the three stages, the version histories, the
  content, the locks, the diff, the projection and the gate.

## Defects found, by two independent reviews

The first review audited the implementation against the specification; the second ran
148 mutations and reported 35 survivors. Both were adversarial and both found real work.
Everything below is FIXED unless stated.

**The first review:**

1. **The Script UI was dead code.** `script-view.tsx` defined the section and nothing
   imported it; `sections.tsx` still exported its WP-05 namesake, so every WP-08
   capability was in a file no route rendered. Neither `tsc` nor the i18n spec could see
   it. ROADMAP scope item 12 was UNMET while the document describing it existed.
2. **The panel passed a script id where a workflow run id belongs** — a different table.
3. **Nothing creates an episode's workflow run**, so the first stage a user started
   would have failed on a foreign key.
4. **Three of the four things the panel claimed not to guess** never reached the
   service: no skeleton, no strategy, no events — and the state renderer OMITS an empty
   field, so a strategy stage would have been told nothing and handed nothing to decide
   about.
5. **`ItemChange.Locked` could never be true.** Declared with a comment about showing
   what the model was not allowed to touch, and set at none of its three construction
   sites.
6. **`scripts/gen-tool-schemas.mjs --check` reported PASS without checking** — the
   comment named a leftover check and the function returned one line later.
7. **`familyOfVersion` reported a storage failure as a not-found.**
8. **Two comments named callers that do not exist** (`LockedFieldsOf`,
   `ContentIsFrozen`), and one named an assertion that was a `t.Log` pair.
9. **`testdata/canary-drama/bad-script.json` was read by nothing** — five named faults
   whose only assertion was that the file existed.

**The second review:**

10. **The whole revision path was at 0% coverage** — `locksFor`, `versionOfDecision`,
    `runsForStage`, `revisionContext`, `StartRevision`, `ManualEdit`,
    `writeUserVersion`, `assertEpisodeInProject`, and `issuesFromOutcome`'s severe
    branch. `FixFromStageRunID` was set by NO test anywhere, so AC-SCRIPT-002's own
    scenario had never been driven end to end. `scriptpipeline` went from 19.9% to
    44.1%.
11. **Two assertions compared the code with itself.** `TestEveryStageArtifactTypeIsAWrite
    ToolTarget` compared the stage map against a literal in the same file; the stage-map
    test skipped the supervisor for the ONE stage the map exists to state. Both are
    replaced by tests that read `schemas/agent/tools/KEYS.txt` and
    `skills/script/manifest.json`.
12. **One survivor hid behind a test that looked like it covered it**:
    `MissingStoryEntityIDs` with a hard-coded project id passed, because the only entity
    assertion asked about another project's row from this one.

Each of the eight highest-value mutations was re-applied and watched to fail, one at a
time. A mutation that fails to compile or apply proves nothing and was redone rather
than counted.

## Acceptance

- **AC-SCRIPT-001 — PASS.** Workflow created, an independent AgentRun per stage naming
  its skill version, versions written, the supervisor's report STORED (read back from
  the database), the user's PASS, and 「approved 唯一」 counted in SQL.
- **AC-SCRIPT-002 — PARTIAL.** 「锁定字段不变」 is enforced by REFUSAL at the write path
  for all three families, with the positive and negative cases asserted, and 「原版本保留」
  holds. Two clauses are PARTIAL, both recorded in ADR-0012: (a) a revision REUSES its
  attempt row (§8 there — the domain's machine says so, and the alternative is a second
  active attempt the schema forbids), so the attempt NUMBER does not increase; (b) the
  canary's FIX scenario pins a field on a COMPLETE skeleton rather than constructing the
  missing-ending-hook skeleton the criterion describes, so the pre-seeded fixture is
  absent while the enforcement it tests is not.
- **AC-SCRIPT-003 — PASS.** Five entity kinds as rows, ordinals unique AND gapless (with
  the negative case), source-event references checked by the service, the
  original-adaptation flag, the duration derived as a sum and compared against the
  episode's target, the version diff, and the canvas projection writing real node rows.

## Verification

- `go test ./... -count=1` — PASS (all packages).
- `go vet ./...` — PASS.
- `gofmt -l` — clean.
- `scripts/verify.sh` — PASS end to end: frontend typecheck, 40 frontend tests,
  production build, Playwright (24 passed, 1 skipped), MONOFORM build, Go tests, Go vet,
  security scans (483 files), and the FIVE generator `--check` steps (canary fixtures,
  hostile fixtures, tool schemas, skill packs, plus the tool key list).
- `wails build -s` — PASS, `build/bin/InfiniteAtelier.exe` produced.
- **`go test -race` is an ENVIRONMENT FAILURE on this host** (`cc1.exe: 64-bit mode not
  compiled in`). It is reported as such, never as a pass. The race-sensitive paths this
  package adds (the mock's counters, the lock table) are exercised by the
  single-threaded suite only.

## What this package does NOT cover — stated plainly

1. **The desktop binding's request mapping.** `RunScriptStage`, `ApplyScriptGate`,
   `LockScriptField`, `SaveScriptStructure`, `ManualEditScript` and their neighbours each
   have exactly ONE call site in tests, against a nil binding, asserting only a refusal.
   A mutation that dropped a field from any of them survived. The Go services behind them
   are covered; the twenty-line mapping between is not.
2. **The composition root's attachment.** No test drives `App.Startup` far enough to
   observe that the pipeline is attached to the drama binding. The same defect class
   (composed but unreachable) has now been fixed twice in this package, and the defence
   is a wiring test that drives `composeDrama` — which covers the projector but not the
   pipeline's attachment.
3. **The frontend's Script view.** The frontend runner bundles only
   `web/src/services/__tests__/*.spec.ts`, so no spec covers the component. The e2e suite
   exercises it only in the no-core state. What is unverified is which field the component
   sends where, and what it renders for a report with findings.

## Git and data safety

- Existing user changes preserved throughout: the plan file under `.zcode/plans/` and an
  untracked user document at the repository root were never touched.
- No secrets introduced; the security scan is clean.
- Migration 000017 is forward-only; migrations 000001-000016 were not modified.
- No provider was contacted. The mock model is the only model path in every test.
- 15 commits for this package, none pushed.

# 0h. WP-07 result: the agent runtime, skills and the quality gate (2026-09-20)

## Scope completed

- **Status: COMPLETE for the 16 ROADMAP items, with one acceptance item PARTIAL.**
  The partial item is named in "Acceptance" below and is not cosmetic.

- **Domain**: `internal/domain/agent` — the three layers, the tool modes and the
  security core (`ToolAllowed`'s matrix), `Spec`/`Limits`/`ToolSpec` with their
  ceilings, the run/message/tool-call/skill records of DOMAIN_MODEL §13, the
  `ErrorCategory` taxonomy and its **separate** wire vocabulary with a
  parity-tested mapping between them (ADR-0011 §7).

- **Schemas**: the seven contracts of AGENT_CONTRACTS §7.1–7.7
  (`schemas/agent/*.json`) plus **nineteen generated tool input schemas**
  (`schemas/agent/tools/*.json`). The tool schemas are generated by
  `scripts/gen-tool-schemas.mjs` from the same key list the table registers, with
  `--check` in the gate, and three tests tie the two directions together.

- **Migration**: `000015_agent_runtime.sql` — four tables plus a **partial unique
  index** that turns §11.2's "at most one active attempt" from a promise into a
  constraint, after a deterministic reconciliation of any duplicate an existing
  database already holds. `000001`–`000014` were not modified.

- **Application**: `internal/application/agentruntime` (registry, tool table and
  authorizer, prompt assembler, the three runners, the workflow engine, the
  model bridge, the refusals, the Inspector), `internal/application/agenttools`
  (the nineteen real tool handlers), `internal/application/agentassembly` (pack
  loading, skill-version registration, registry assembly),
  `internal/application/memory` (the Recent-only port), `internal/application/skill`
  (the manifest loader and its validation). `internal/application/validation`
  gained `Against` — a schema-agnostic validator, because an agent declares its
  output contract by path.

- **Infrastructure**: `internal/infrastructure/providers/mock_text.go` (the
  deterministic Mock LLM, reachable only through `KindMockText`, a kind no
  persisted configuration may carry), `internal/infrastructure/database/agent_repository.go`
  and `artifact_verifier.go`.

- **Composition**: `agent_wiring.go` composes the stack AFTER the drama stack,
  because every tool's handler calls one of its services — the dependency
  direction made visible in one file. The runtime is the **only** implementation
  of WP-06's `Extractor` port in a production build, which closes the seam that
  package recorded.

- **Bindings**: `AgentBinding` (four reads), regenerated with
  `wails generate module`.

- **Frontend**: the `agents` section (the Agent Center) with run filtering, a run
  trace, and the agent inventory; i18n in both locales; the e2e suite's
  `SECTION_IDS` extended and a test asserting the section is reachable. The
  `quality` section needed no new content: its rows were WP-05's and are already
  there.

- **Docs**: ADR-0011, the ADR index, TRACEABILITY, README, this section.

## Commands executed and actual results

| Command | Result |
|---|---|
| `go test ./... -count=1` | **PASS** (53 packages: 46 with tests, 7 without) |
| `go vet ./...` | **PASS** |
| `gofmt -l internal/ schemas/ *.go` | **PASS** (no output) |
| `node scripts/security-scan.mjs` | **PASS** (451 files scanned; 1 audited dynamic-execution exception, 4 audited legacy direct-call files) |
| `node scripts/gen-skill-packs.mjs --check` | **PASS** (19 skill files current) |
| `node scripts/gen-tool-schemas.mjs --check` | **PASS** |
| `node scripts/gen-canary-fixture.mjs --check` | **PASS** |
| `web`: `npm run typecheck` | **PASS** |
| `web`: `npm test -- --run` | **PASS** (40 tests) |
| `web`: `npm run build` | **PASS** |
| `web`: `npx playwright test` | **PASS** (23 passed, 1 skipped) |
| `wails build -s` (v2.15.0) | **PASS**; `build/bin/InfiniteAtelier.exe` |
| `wails generate module` | **PASS**; `AgentBinding` added |
| `git diff --check` | **PASS**, with one note: the Wails-generated `models.ts` carries the generator's own trailing whitespace, unchanged from every earlier package |
| `go test -race ./...` | **ENVIRONMENT FAILURE, not a pass** — unchanged from sections 0e/0f/0g: the host's `cc1.exe` reports "64-bit mode not compiled in", so the race detector cannot build. Recorded as an environment failure rather than a skip. |

## Acceptance

- **AC-AGENT-001 (layer isolation)** — PASS. The ACL's matrix is asserted at
  registry construction AND per call; an illegal call returns
  `security.tool_not_allowed`, the run is recorded as `failed` with that code,
  and the tool call is recorded as `denied` with its reason (the schema's CHECK
  forces the reason). Covered by `agentruntime`'s unit tests and the canary.
- **AC-AGENT-002 (output schema)** — PASS. A valid document succeeds; a malformed
  one gets exactly one repair; two malformed answers fail the stage with
  `agent.output_schema_invalid`. **No business half-write** is asserted by
  counting the artifact rows before and after a failed run.
- **AC-AGENT-003 (artifact hallucination)** — PASS. `ArtifactVerifier` refuses an
  invented identifier, refuses an UNKNOWN entity type rather than assuming it
  exists, and reports a query failure as a storage error rather than a
  hallucination. The run is not marked successful and the workflow does not
  advance.
- **AC-AGENT-004 (prompt injection)** — PASS. The repository's own injection
  fixture travels as the untrusted task; the skill is unchanged, no tool is added
  (the grant comes from the registry on the other side of the prompt), no secret
  is read, the agent still processes the story content, and the prompt really
  carries the boundary.
- **AC-AGENT-005 (maximum loop)** — PASS. Thirteen tool calls against a budget of
  twelve are refused BEFORE any of them runs (zero calls recorded), and a failing
  review whose budget is spent moves the stage to `waiting_user` rather than
  revising again.
- **Canary (Decision → Execution → Supervisor → User Gate)** — PASS, and it is the
  package's central evidence: the whole chain over a real migrated database with
  the real packs, the real tool table and the real validator, driven by the
  deterministic mock. The audit trail records every transition the chain made.
- **Workflow DB state** — PASS. The stage's transitions are read back from
  `workflow_events`.
- **CI never calls a real model** — PASS. The mock is reachable only through
  `KindMockText`, which `IsUserConfigurableKind` refuses and the database CHECK
  rejects, so no persisted configuration can select it.
- **Supervisor read-only** — PASS, asserted three ways: the ACL's matrix, the
  registry's startup check (`Supervisor 无未批准写工具`), and a table test that no
  write tool is reachable from the supervision or decision layers.
- **Minimal Agent Center UI (scope item 14)** — **PARTIAL**, and the gap is named:
  there is **no user-facing way to start a run**. The binding surface is read-only by
  design (a run happens because an application service asks for one, which keeps "who may
  run an agent" a server-side question), so the Agent Center shows the runs that exist.
  The plan called for a "start canary run" action; what shipped instead is the canary as a
  TEST, which is where it is safest and where it can assert the whole chain. Adding a
  trigger is a product decision — which stage, under which policy, with which provider —
  and belongs to the package that owns that workflow (WP-08 for the script pack).
  Recorded here rather than papered over.

  The extraction path, by contrast, IS wired: an independent review found it was not, and
  that finding and its fix are recorded below.

## What was delivered against the 16 ROADMAP items

| # | Item | Where |
|---|---|---|
| 1 | Agent Registry/Spec | `agentruntime/registry.go`, `domain/agent` |
| 2 | Skill Manifest/Loader/Version | `application/skill`, `agentassembly` |
| 3 | Tool Registry/Authorizer | `agenttools`, `agentruntime/tools.go` |
| 4-6 | Decision/Execution/Supervisor runners | `agentruntime/runner.go` (one runner, three layers) |
| 7 | JSON Schema + one repair | `application/validation.Against`, `runner.go` |
| 8 | AgentRun/Message/ToolCall | `domain/agent`, `database/agent_repository.go` |
| 9 | Workflow State Machine | `agentruntime/engine.go` over WP-05's machines |
| 10 | StageRun/Review/UserGate | `engine.go`'s `ApplySupervision`/`ApplyGate` |
| 11 | Max Tool/Time/Retry | `domain/agent.Limits`, `runner.go`'s budget check |
| 12 | Cancellation | `runner.go`'s cancelled status, `reject.go` |
| 13 | Deterministic Mock LLM | `providers/mock_text.go`, section 18.3's eight scenarios |
| 14 | Agent Center / Quality Gate UI | the `agents` section; the gate itself is WP-05's `quality` section |
| 15 | Basic Memory Port (Recent) | `application/memory`, the repository's `RecentMessages` |
| 16 | Script/Production skeleton packs | `skills/`, 19 documents, generated and checked |

## Nine defects found in code that had already been written and reviewed

Each is recorded in ADR-0011's "Findings from implementation" with its evidence,
and each of the four the canary found has a regression test verified by
re-introducing the defect. The short version: the tool path was UNREACHABLE for
any schema-validated agent (tool calls cannot ride inside an
`additionalProperties: false` document); the workflow service had no `GetRun`, so
the engine could only ever have been driven by a test double; four more service
reads were missing, each the hop a project-boundary check needs; `ToolRequest` had
no agent-run id, so a version's author was recorded as a stage; `Runtime.finish`
discarded the revision it incremented; refusals discarded the run id; and the mock
had three defects of its own (empty tool arguments, counters that survived a
scenario change, and no supervision branch in its tool-call document).

## Independent reviews, and what they found

Two reviews ran against the delivered package: a specification review and a quality review
with mutation testing. Both found real defects, and the package was changed rather than the
findings being argued away. The specification review's report is worth reading in full; what
follows is what was DONE about each finding.

**Release-blocking: tool arguments were never schema-validated.** Section 6.1's per-call chain
is "JSON parse → Schema validation → Tool ACL → scope validation → execute", and the schema path
travelled into the prompt and was never applied to what came back. A model could hand a handler
any shape at all; only the handler's own struct decoding refused it — a decode error rather than
a contract refusal, unable to name the rule. Section 20 lists "Tool 参数未 Schema 校验" as a
release-blocking condition. Fixed: the runtime takes a `ToolArgumentValidator` and applies it
between the ACL and the handler, with three deliberate distinctions (no validator configured
REFUSES; a non-JSON document is a repairable refusal; a schema that will not compile is an error
because the model cannot fix a build defect). The refusal is a FAILURE rather than a denial, and
the code is `agent.tool_arguments_invalid`.

**The runtime had no call site in a production build.** `composeAgents` built the
runtime-backed extraction service and `app.go` never attached it, so the extraction binding kept
the drama stack's extractor-less service and every extraction refused with "unavailable". A
compile-time assertion proved the port was satisfied, not that anything used it — the "interface
with no real path" AGENTS section 12 refuses, and the finding was sharper for standing on
ADR-0011's own claim that the seam was closed. Fixed: `app.go` rebuilds the service with the
runtime as its Extractor and re-attaches it. That exposed a second gap — the mock could not
answer the extraction agent, whose contract is the event graph's rather than an ExecutionResult's
— so the mock now reads the agent from the skill layer and answers in that agent's shape. A new
canary test drives the extraction path end to end through the runtime, so the wiring is asserted
rather than assumed.

**The answering model was never recorded.** Section 16 requires "模型和 Provider", and section
13's "模型变更写入 Run" is about a run whose answering model differs from the requested one. Only
`model_config_id` was recorded. Fixed with a `ResponseModel` field, a forward migration
(`000016`) and a capture from the first reply.

**A long Chinese reply was silently dropped from the record.** The storage bound is BYTES and the
code counted RUNES against it, so a reply was clipped past the limit, the domain refused it, and
the caller discarded the refusal. Fixed: the clip counts bytes and walks back to a rune boundary,
and a refusal is recorded as `agent.message_not_stored` rather than swallowed. Writing that fix
produced the same bug one level down (subtracting the marker after the boundary walk), which the
new test's UTF-8 assertion caught.

**Two of section 7.4's artifact rules and section 7.6's critical rule did not exist.** A success
with no artifact passed for every stage, and a `critical` finding took the ordinary path rather
than forcing a human gate — a rule the DOMAIN names this package as the owner of. Fixed: the
no-op case is now the caller's to state (a new `Invocation` field, because the caller is what
chose the stage), a failed result may not carry artifacts, and `ApplySupervision` takes the
report's Severity and parks the stage on a critical finding, checked BEFORE the passed/failed
branch because it overrides both.

**The stage identity cross-check did not exist.** Nothing compared a result's `stageRunId` with
the invocation's, though two comments and a canary assertion claimed the check was there — the
canary's passed because the mock copies the value out of the prompt. Fixed.

**A comment described a `+ 1` the code does not contain.** `StartRevision` reads
`revisions > MaxAutoFix` while its comment said "hence the `+ 1`". A reader who followed it would
write `revisions+1 > MaxAutoFix`, permitting one extra automatic revision and breaking
AC-AGENT-005. Fixed: the comment now states the two predicates and why they differ.

**Two vacuous assertions** were found by reading rather than mutating: a `forbidden` slice built
and discarded (`_ = forbidden`) whose loop hardcoded three of its four entries, and an
empty-object test that derived its expectation from the same `required` array it was asserting
about. Both fixed; the second now names every tool and asserts its list length against the
table's.

**A status figure was wrong**: STATUS said 43 packages where `go list ./...` reports 53. Fixed.

**The quality review's mutation run** reported 29 of 81 surviving. The eight behaviourally
significant ones are now covered and each was verified by re-introducing its mutation: three
project-boundary checks (9 of `scope.go`'s 13 blocks had zero coverage and all 14 guard lines in
`handlers.go` were at zero or absent — an agent in project A could read and WRITE project B's
artifacts), two engine guards that their tests could not reach (the revision counter's
fail-closed arm needed a transitioner that does not implement the interface; the gate's status
guard was shadowed by the edge check below it), the artifact verifier's type table (whose own
comment promised a test, and whose `VerifiedTypes` had no caller anywhere), the mock's counter
reset (whose own comment says the canary found the defect), and the verifier's storage-fault
distinction. `inspector.go` had NO tests at all — 41 of 41 blocks at zero — and now has seven,
with five mutations verified caught.

Two survivors are recorded rather than chased, because they are equivalent mutations: a
redundant scheme guard that a later arm already refuses, and a defensive fallback no embedded
schema can reach. That is stated rather than counted as a pass.

## Mutation testing

The package's verification technique, used on every new file. What each pass found
is worth recording because three of them changed the code rather than the tests:

- **`IsRetriable`**: five branches were DEAD — every concrete type already
  satisfies the `Refusal` interface, so the interface check returned first. The
  branches were deleted rather than kept with tests that could not fail.
- **The tool table**: the first pass reported 12 of 18 surviving. Eight were in
  pure helpers with no test at all, which `helpers_test.go` fixed; the remaining
  five are handler-level project-boundary checks that need two projects in a real
  database, and the canary is what covers them.
- **The agent repository**: the first pass reported SEVEN surviving, and the
  harness was the defect — `python3` on this host is a Windows Store stub that
  exits 49 without running, so no mutation had been applied, and a backup path
  under Git Bash's `/tmp` was invisible to the real interpreter, so restores
  failed and mutations compounded. The corrected harness hashes the file after
  every restore and refuses to continue if it does not match. The real result is
  15 of 15 caught.

## Known limits and deferred work

- **No user-facing run trigger** (see Acceptance).
- **Memory is Recent-only.** §14.5's write path, the summary chain and semantic
  recall with a threshold and a rerank are WP-10's. The port's two invariants —
  self-exclusion and project isolation — ARE implemented, because a recall that
  broke either would leak.
- **The model policy is project-level, not per-layer.** §13's policy object allows
  a supervisor on a different provider from its executor; this build resolves the
  first enabled provider, ordered by id. The resolution point is one function
  (`textGenerator.choice`), so the per-layer object is a settings-UI change rather
  than a redesign.
- **Two of §6.2's example tools are absent** by ruling (ADR-0011 §6), not by
  omission: `story.create_event_candidates` would be a second write path for the
  extraction stage, and `provider.submit_image_job` is a Job rather than a tool.
- **The stage policies resolve "conditional" to supervised.** §10.1 marks several
  stages conditional; without a ruleset UI this build treats a conditional stage as
  supervised, because the review is read-only and the alternative is an unreviewed
  artifact. Recorded in ADR-0011 as refutable.

## Git and data safety

- Existing user changes preserved: **yes**. The user's untracked
  `Infinite-Atelier-OpenCode集成必要性评估与完整实施方案.md` was never modified, and no
  user file was reverted, stashed or reset.
- Secrets found or introduced: **none**. The Mock LLM's adapter is the only new
  outbound path and it opens no socket.
- Migrations: `000015_agent_runtime.sql` is forward-only; `000001`-`000014` were not
  modified. No backup or restore was performed.
- Real providers were not contacted; no paid API was called.
- `wails build -s` deleted the tracked `web/dist/.gitkeep` placeholder, which was
  restored with `git checkout --` before committing; no other build artifact was
  staged.

# 0g. WP-06 result: source documents, chapters and the event graph (2026-09-19)

## Scope completed

- **Status: COMPLETE for the 13 ROADMAP items, with the two scope exclusions
  named in ADR-0010 §8.**
- Fixtures: `testdata/canary-drama/` (32,736 Chinese characters, 5 chapters, one
  carrying a prompt-injection block) and `testdata/malicious-imports/` (11
  hostile inputs). Both are generated, both are checked by `verify.sh` /
  `verify.ps1` `--check` steps, and both are now consumed by real tests.
- Domain: `internal/domain/importing` (format, encoding, normalization, chapter
  detection as pure functions) and `internal/domain/extraction` (the contract's
  vocabulary and the refusal types).
- Schema: `schemas/agent/event_extraction.v1.json`, embedded through the
  `schemas` package. `internal/application/validation` compiles it and validates
  against it, so the file is the contract rather than a description of one.
- Application: `internal/application/importing` (precheck, import, confirm,
  paged reading) and `internal/application/extraction` (the `Extractor` port,
  reference resolution, candidate writes, evidence). `internal/application/story`
  gained the alias, participant and evidence writers ADR-0007 recorded as
  missing, the filtered graph queries, the conflict queue, and lock/unlock.
- Infrastructure: forward migration `000014_story_import.sql` (the entity
  vocabulary widened to eight values by rebuilding four tables; `source_hash`
  and `chapters.source_kind` added). `000001`–`000013` were not modified.
- Bindings: `ImportBinding` (precheck, import, paged read, confirm, extract),
  `ImportUploadBinding` (the chunked transfer), the five story-graph list
  queries, three conflict methods and four lock methods on `DramaBinding`. The
  Wails surface was regenerated.
- Frontend: the import flow with its precheck review and duplicate gate, the
  chapter panel with paged reading, the story-graph section with its review
  queue, evidence and participant panels, a conflict queue, an SVG reading aid,
  and lock controls. i18n in both locales at 1,181 leaves each.
- Docs: ADR-0010, the ADR index, TRACEABILITY, README, this section.

## Commands executed and actual results

| Command | Result |
|---|---|
| `go test ./... -count=1` | **PASS** |
| `go vet ./...` | **PASS** |
| `gofmt -l internal/ schemas/` | **PASS** (no output) |
| `node scripts/security-scan.mjs` | **PASS** (401 files scanned; 1 audited dynamic-execution exception, 4 audited legacy direct-call files) |
| `web`: `npm run typecheck` | **PASS** |
| `web`: `npm test` | **PASS** (40 tests, including four that compare the two locale key trees) |
| `web`: `npm run build` | **PASS** |
| `web`: `npx playwright test e2e/` | **PASS** (23 passed, 1 skipped) |
| `wails build -s` (v2.15.0) | **PASS**; `build/bin/InfiniteAtelier.exe` |
| `wails generate module` | **PASS**; `ImportBinding` and `ImportUploadBinding` added |
| `git diff --check` | **PASS** |
| `go test -race ./...` | **ENVIRONMENT FAILURE, not a pass** — unchanged from sections 0e/0f: the host's `cc1.exe` reports "64-bit mode not compiled in", so the race detector cannot build. Recorded as an environment failure rather than a skip. |

## Acceptance

| Item | Status | Evidence |
|---|---|---|
| AC-STORY-001 编码正确 | PASS | `DetectEncoding` covers UTF-8 with BOM, UTF-16 by BOM, GBK and GB18030; the GBK fixture decodes, and the lossy-decode guard rejects what it cannot decode without loss. |
| AC-STORY-001 章节检测 | PASS | The canary document's five chapters are detected and matched against its generated report; the boundaries are asserted to tile the document. |
| AC-STORY-001 用户调整 | PASS | `ReviseChapter` adjusts a boundary's title and offsets under a revision guard and marks it `manual`; `SplitChapter` and `MergeChapter` divide and join boundaries, renumbering under `UNIQUE (version, ordinal)` in one transaction, with the text still tiling afterwards. PRD FR-020 lists 手动合并/拆分 as a MUST item and this is where it is delivered. |
| AC-STORY-001 offsets 有效 | PASS | Evidence offsets are asserted to fall inside the chapter AND to index the VERSION's text, which is what §6.6's reader uses. Both assertions caught a real defect. |
| AC-STORY-001 原文定位 | PASS | `ReadDocumentRange` returns a version's text by offset, bounded by the core's page ceiling. |
| AC-STORY-001 duplicate hash 提示 | PASS | A repeat import of the same file is refused by default and reported with the document it was imported as; the UI requires an explicit confirmation. |
| AC-STORY-001 UI 不冻结 | PARTIAL, with the cost named | The reader's response is capped by the core (asserted), and the transfer that feeds an import is chunked at 64 KiB (asserted to cross a 100k-character document in more than one chunk). **No timing measurement exists**: the repository has no benchmark for this path, so the claim rests on the bounds rather than on a measured figure. The precheck still crosses in one message and is the remaining cost. |
| AC-STORY-002 结构校验 | PASS | The embedded schema is compiled and enforced; a document that violates it writes nothing. |
| AC-STORY-002 来源 chapter/offset | PASS | Evidence rows carry the version, the chapter and, when the name is found, its offsets. |
| AC-STORY-002 candidate 状态 | PASS | Every write goes through commands that hard-code `candidate`; the schema has no status field for a model to fill in. |
| AC-STORY-002 接受/拒绝/锁定 | PASS | Accept, reject, lock and unlock exist for entities and for events; the gates refuse a locked fact any of the others, and a rejected fact cannot be locked because unlocking it would confirm it. |
| AC-STORY-002 冲突记录 | PASS | Open, resolve and LIST; the list was the missing third of a loop whose commands existed with no way to find what they recorded. |
| AC-STORY-002 无跨项目污染 | PASS | Every query is scoped by project, and the propagation skips a dependent whose project does not match; a test asserts another project sees none of a conflict. |
| AC-STORY-002 一次 Schema 修复 | PASS | The repair round is implemented and asserted to reach the extractor with the violations, in exactly one round. |
| Prompt Injection 边界 | PASS | The canary's injection block and two hostile fixtures are asserted to survive as DATA: the text reaches the extractor unfiltered and nothing acts on it. |
| 恶意 DOCX | PASS | Eleven hostile fixtures, each with a test that reaches the refusal it exists for. Three fixtures were added because the first two never reached the container check at all. |
| ADR-0010 | PASS / ACCEPTED | The four rulings and the two exclusions recorded. |

## Independent review

An independent agent reviewed the 13 commits against ROADMAP, PRD FR-020/FR-030,
ACCEPTANCE §8, SECURITY §7.2/§7.5/§8.1/§8.2/§9/§17/§18 and AGENT_CONTRACTS
§2.2/§5/§6/§7/§14.3/§17/§18. It found eight issues. Two were blocking and are
fixed with mutation-verified tests:

1. **Evidence offsets were chapter-local, not version-absolute.** The reader
   sliced the chapter out of the version and the service searched that slice, so
   a span it found was relative to the chapter while the row named a VERSION. A
   citation in a chapter starting at offset 1000 pointed at the whole document's
   first characters. The suite was green because every fixture had one chapter at
   offset zero, where the two systems coincide. The fixtures now start at 1000;
   removing the shift fails three tests.
2. **Section 14.3's repair round did not exist**, and a validation refusal could
   not reach the caller at all — the error mapper recognised only the request
   error, so a malformed document arrived as "The drama request failed." with the
   violations discarded. Both are fixed, and the violations were made value-free
   before they could be sent anywhere.

The other six: the Conflict scope item had zero delivery (now implemented);
lock was missing from AC-STORY-002's list (now implemented, and "modify"
explained in ADR-0010 §8); "100k 不阻塞" had no measurement and the transport was
synchronous (the transport is now bounded and chunked, and the missing
measurement is recorded above rather than papered over); four comments described
code that did not do what they said; two fixtures were referenced by their
licence rows and read by no test; and a test-time dependency was missing from the
notices.

### Second review

A re-review confirmed three of those fixes by independent probe and mutation
(the offset arithmetic including the hash's inverse, the conflict chain, and the
comment/notice corrections), and found four things the first round had not
settled. A separate quality review ran 30 mutations and found 10 survivors. Both
sets are now fixed, each verified by a mutation that fails:

1. **Unlock reversed a rejection.** Lock was allowed from any status and unlock
   returns a fact to 'accepted', so reject → lock → unlock turned a rejection into
   a confirmation through two individually-legal commands, reachable in two
   clicks. A rejected fact can no longer be locked. This was a defect this package
   introduced, not a pre-existing one.
2. **ADR-0010 §8 was factually false about split and merge.** It claimed the
   specification "never names" them; PRD FR-020 lists 手动合并/拆分 as a MUST item
   and the acceptance criteria include 章节拆分可人工修正并保存. Both are now
   implemented (transactional renumbering under `UNIQUE (version, ordinal)`), and
   the false claim is replaced by the reasoning the section was missing. The
   reviewer was right to refuse the original wording: deferring is defensible,
   describing a MUST item as unspecified is not.
3. **The production chapter reader had no test.** It holds the one assignment that
   makes evidence offsets version-absolute, and a mutation setting it to zero left
   the suite green — the application tests use a fake that computes the same value
   independently, so the two could drift. The adapter now has tests, and the fake's
   comment claiming "the real one is covered by the desktop tests" was false when
   written and is true now.
4. **Four tests that could not fail**, found by mutation: the upload's streaming
   ceiling (its loop could never reach the threshold it asserted), the
   duplicate-import check (no test at all — inverting its condition left the whole
   suite green), the chapter confirmation (no test in either layer, which is how
   the next item survived), and the reference cross-check for participants and
   validFromEventRef. All four are now covered, and the mutations that survived
   them are caught.
5. **ConfirmChapters could record a confirmation that changed nothing.** The
   guard checked only that boundaries existed, not that any was confirmable, so a
   version whose boundaries were all 'edited' wrote `ChapterBoundariesConfirmed`
   for a decision that did not happen.
6. **The repair round covered only schema failures.** A reading whose shape is
   valid but whose references dangle — the clearest repairable case — failed
   without the model being asked. It now covers both checks.
7. **The reader's previous-page control moved forward.** `ReadRange` clamps a
   non-positive end to the end of the document, so asking for a page ending before
   an offset produced one running forward from it: the button advanced nearly a
   full page while moving one rune back. There is now an explicit direction, with
   the tiling property asserted.
8. **Three comment defects**: a special case that was unreachable (and whose
   removal changes no message, so it was deleted rather than kept with a test that
   could not fail), `MaxTitleRunes` claiming a CHECK on `chapters.title` that does
   not exist, and the "non-blocking" claim scoped to the wrong bound.

## Known limits and deferred work

- **No timing measurement for the 100k-character path.** The bounds are asserted;
  a figure is not. A benchmark belongs with the performance work.
- **The precheck still crosses in one Wails message.** Bounded by the domain's
  input ceiling, but it is the one remaining cost proportional to document size
  on the webview thread.
- **A generic "modify a fact" command does not exist.** The specification does not
  name one the way it names 合并/拆分, and a generic modify would be a second way to
  write the same row; ADR-0010 §8 records the reasoning.
- **The `chapterTextReader`-to-extraction path has no end-to-end test that starts
  from a real import.** The adapter is tested against a populated store and the
  extraction against a fake reader; the two are joined only in a desktop build.
- **`PropState` is still unmodelled** (ADR-0010 §3).
- **`go test -race` cannot run on this host** (no 64-bit C toolchain), so
  concurrency claims rest on design rather than on a race-detector run.
- **A WP-05 leftover is now observable**: creating a candidate entity or event
  emits `StoryFactAccepted`. The name contradicts the state, and extraction makes
  that path busy. It predates this package and is recorded rather than fixed
  here, because the event vocabulary is closed and pinned by a test.

## Git and data safety

- Existing user changes preserved: **yes**. The user's untracked
  `Infinite-Atelier-OpenCode集成必要性与完整实施方案.md` was briefly staged by a
  `git add -A` and unstaged immediately; the file was never modified and is
  untracked again.
- Automatic commit/push/stash/reset/clean: **none** beyond the `git add` mistake
  above, which was corrected in the same session.
- Migrations: `000014` added; `000001`–`000013` unmodified. No migration ran
  against user data; the upgrade-preservation test uses a temporary database.
- User databases, secrets and media changed: **no**.
- Real Provider calls: **none**.
- Dependencies added: `github.com/santhosh-tekuri/jsonschema/v6` (Apache-2.0, one
  dependency already direct). `THIRD_PARTY_NOTICES.md` records it and the
  test-only `dlclark/regexp2`.

---

# 0f. WP-05 follow-up: domain events and the missing approvals (2026-09-18)

## Why this exists

Section 0e closed WP-05 with two partial deliveries named as its largest gaps:
the §17 domain event stream was not implemented, and five of the eight version
families had no way to reach `approved`. This section records the work that
closed both, and what it found while doing so.

## Scope completed

- **Status: COMPLETE for this follow-up.** The two gaps section 0e named are
  closed to the extent WP-05's own commands can close them; what remains is
  named below per event.
- Schema: forward migration `000013_domain_events.sql`. `000001`–`000012` were
  not modified.
- Domain: `internal/domain/event` — §17's twenty-eight names and the envelope,
  both pinned by a test against the specification's list.
- Application: `internal/application/events` (record, build, list, count) plus
  the recorder ports on six services and the emissions from their commands.
- Infrastructure: the `domain_events` repository, and the shared §2.5 approval
  switch in `version_approval.go`.
- Bindings: `ListDomainEvents` and `CountDomainEvents` on `DramaBinding`, with
  the Wails surface regenerated.
- Frontend: `listDomainEvents` / `countDomainEvents` and `DOMAIN_EVENT_TYPES` in
  `services/desktop/drama.ts`.
- Docs: ADR-0009, the ADR index, TRACEABILITY, README, this section.

## Commands executed and actual results

| Command | Result |
|---|---|
| `go test ./... -count=1` | **PASS** (35 packages ok) |
| `go vet ./...` | **PASS** |
| `gofmt -l .` | **PASS** (no output) |
| `node scripts/security-scan.mjs` | **PASS** (365 files scanned) |
| `web`: `npm run typecheck` | **PASS** |
| `wails generate module` (v2.15.0) | **PASS**; `models.ts` 99 classes to 101, both added, none removed; `DramaBinding` now 48 methods |
| `git diff --check` | **PASS** |
| `go test -race ./...` | **ENVIRONMENT FAILURE, not a pass** — unchanged from section 0e |

## What was delivered

**The event stream.** `domain_events` stores the §17 envelope. It has no foreign
key to an event's subject and no revision: an event outlives what it describes,
and a stream that could be rewritten would not be a record of what happened. The
one reference it keeps is to the project, because every drama query is
project-scoped.

**Two emission paths, and the difference is deliberate.** An approval records its
event inside its own transaction and refuses without a recorder, because the
event is the governance record of a decision. Every other command announces
itself after its write succeeds and carries on if the announcement fails, because
the row is already committed. ADR-0009 draws the line; `RecordBestEffort`'s doc
comment says why its error is dropped.

**The five missing approvals.** Skeleton, adaptation strategy, director plan,
storyboard version and style guide version now have commands. All eight families
share one implementation of the switch, because the order of its two writes is
what keeps the schema's partial unique index satisfiable.

**Twenty of the twenty-eight events are emitted** by the commands that cause
them. The seven that are not belong to packages that do not exist yet, and each
is named below rather than left to be discovered.

## Defects found while doing the work

1. **`script_versions` had a second implementation of the §2.5 switch.** It
   predated the shared helper, took the supersede target as a caller-supplied id,
   and had its own transaction. Mutation testing found it: disabling the shared
   helper's supersede step left the suite green, because the script family never
   used it. Two implementations of one rule can disagree about the order, so the
   script repository now delegates to the shared switch and the whole repository
   has exactly one statement writing `'superseded'`.

2. **A nil recorder panicked.** The recorder is an interface, so a service
   composed without one holds a nil interface and calling a method on it panics.
   The existing test suite caught this when the first emit site was added. Each
   service now has one nil-checked `recordEvent`, so no emit site can forget.

3. **The same duplication appeared twice in this follow-up itself**: a first
   `approveVersion` helper was written without an event and left unreachable
   while every family used `approveVersionWithEvent`. It was removed, and the
   file header records that it existed rather than pretending it never did.

## Emissions: the twenty and the seven

| Emitted by WP-05 commands | Not emitted, and by whom |
|---|---|
| ProjectCreated, ProjectSettingsChanged, ProjectRuleLocked | ChapterBoundariesConfirmed — WP-06's chapter confirmation |
| SourceDocumentImported | GenerationJobQueued / Succeeded / Failed — the job core |
| StoryFactAccepted, StoryFactConflictOpened | MemoryCreated — WP-10 |
| EpisodeCreated, StorySkeletonApproved, AdaptationStrategyApproved | UpstreamVersionChanged — WP-07's impact analyzer. WP-05 emits `ArtifactMarkedStale`, the mark itself rather than the upstream change that caused it |
| ScriptVersionCreated, ScriptVersionApproved | BackupCompleted — the backup service |
| AssetVersionCreated, AssetVersionApproved | |
| DirectorPlanApproved, StoryboardVersionApproved | |
| CanvasProjectionCreated | |
| WorkflowStarted, WorkflowStageChanged, ReviewReportCreated, UserGateDecided | |
| ArtifactMarkedStale | |

Emitting the remaining seven from WP-05 would mean inventing their call sites, so
the vocabulary and the table are complete while the emissions are not.

## Known limits

- **The style-guide approval reports `ProjectSettingsChanged`.** §17 defines no
  style-guide event and the vocabulary is closed, so the approval uses the
  closest name §17 offers with a payload naming what changed. ADR-0009 records it
  and the alternative that was not taken.
- **A best-effort event can be lost** if the event write fails after the row
  commits. The row is still the authority on what exists; the stream is a
  projection, like the canvas. That is what ADR-0009's first decision buys and
  costs.
- **The stream has no subscription.** It is a query
  (`ListDomainEvents`/`CountDomainEvents`), not a push. The `core:event` channel
  carries job and provider events; wiring domain events onto it would be a
  transport decision for whichever package first needs live updates.
- **Only `script_versions`' approval path is covered end to end against the real
  database.** The other seven families are covered through the shared switch at
  the unit and repository level, plus the mutation that proves the switch's order
  matters. A dedicated integration test per family would be more convincing and
  was not written.
- Real paid providers were never contacted; every test uses synthetic fixtures
  and temporary databases.

## Git and data safety

- Existing user changes preserved: **yes**. An unrelated 78 KB report appeared in
  the working tree during this work; it is not WP-05's, it was not read into the
  package, and it was deliberately left untracked and uncommitted.
- Automatic commit/push/stash/reset/clean: **none**.
- Secrets found or introduced: **none**; the scanner passes over 365 files.
- Migrations executed against user data: **none**. `000013` ran only against
  temporary test databases.
- Every mutation probe was restored, and the tree was verified clean afterwards.

# 0e. WP-05 result (2026-09-18)

## Scope completed

- **Status: COMPLETE (WP-05 scope)**, with the limits listed below stated plainly rather than
  presented as passes. Every acceptance item below was executed on this host.
- Scope: drama project configuration (settings, rules, style guides, model policies); the
  source/chapter tables; the story fact layer (entities, aliases, events, participants, relations,
  evidence, conflicts, character state); episodes and the script pipeline (skeleton, strategy,
  script, scene, dialogue, shot); the asset aggregate rebuilt to the documented vocabularies plus
  lineage and usage; director plan, storyboard table and panels; workflow, review and user-gate
  tables; the canvas entity reference and relation registry; the studio navigation with honest
  empty states; the project creation wizard; domain commands, queries and the staleness
  propagation; and the unit tests for revision, version, approval and staleness.
- Schema: seven forward migrations, `000006`–`000012`. `000001`–`000005` were not modified.
- Domain: `versioning` (the shared §2.5 vocabulary), `story`, `script`, `asset` (extended),
  `storyboard`, `workflow`, `staleness`, and `project` (extended with the drama configuration and
  the §10.4 relation registry).
- Application: `story`, `script`, `projects` (extended with the drama configuration and the
  projection commands), `assets` (extended with lineage, usage and approval impact), `storyboard`,
  `workflow`, `staleness`.
- Infrastructure: `database/{story,script,storyboard,workflow,staleness,drama_settings,projection}`
  plus the canvas and asset repository extensions.
- Bindings: `DramaBinding` (52 methods across five services) and `AssetsBinding`, with
  `drama_wiring.go` composing them and `app.go` attaching only over a writable database.
  `CreateProjectRequest` gained the PRD FR-020 drama fields flat, and `ProjectsBinding` gained
  `GetProjectSettings`, `UpdateProjectSettings` and `ListProjectRules`.
- Frontend: the `/studio` list, the 14-section shell, the creation wizard, the studio service
  client, the UI-state store, and the section nav. The canvas persistence adapter was fixed to
  carry entity references and edge relation types in both directions.
- Docs: ADR-0007 (schema and vocabulary rulings), ADR-0008 (staleness, projection, approval),
  this section, the ADR index, TRACEABILITY and the README.

## Commands executed and actual results

| Command | Result |
|---|---|
| `go test ./... -count=1` | **PASS** (35 packages ok) |
| `go vet ./...` | **PASS** |
| `gofmt -l .` | **PASS** (no output) |
| `node scripts/security-scan.mjs` | **PASS** (365 files scanned; 1 audited dynamic-execution exception, 4 audited legacy direct-call files with named owners) |
| `web`: `npm run typecheck` | **PASS** |
| `web`: `npm test` | **PASS** (36 tests) |
| `web`: `npm run test:e2e` | **PASS** (22 passed, 1 skipped; the skip is the pre-existing crop dialog that needs node content a headless run cannot supply) |
| `web`: `npm run build` | **PASS** with the pre-existing over-500 kB chunk warning |
| `wails generate module` (v2.15.0) | **PASS**; produced `DramaBinding`, `AssetsBinding` and `BackupBinding` and extended `models.ts` |
| `git diff --check` | **PASS** |
| `go test -race ./... -count=1` | **ENVIRONMENT FAILURE, not a pass** — the host has no 64-bit CGO compiler, which reproduces on untouched packages. |

`models.ts` grew from 33 exported classes to 95. The change was checked to be purely additive by
comparing the class-name lists against the previous revision: 62 added, zero removed or renamed.

## Acceptance

| ID | Result | Evidence |
|---|---|---|
| Domain Model MVP tables and constraints | **PASS** | `TestWP05MigrationFreshDatabase` (43 tables), `TestWP05ConstraintsEnforceDocumentedValues` (each documented value accepted, each undocumented one rejected), and the vocabulary parity guard below. |
| approved 唯一 | **PASS** | A partial unique index in each of the eight version families plus `TestWP05ApprovedVersionIsUnique`, which asserts each index exists, is `UNIQUE` and is partial on the approved status. All eight were mutation-verified: weakening any one to a plain index fails the test. |
| locked rule | **PASS** | `Rule.CanModify` and `CanEscalate` gate the writer, and the service applies both against the stored rule. Two tests: one for the lock-change guard, one isolating `CanModify` by editing content with the lock flag unchanged. Both were mutation-verified. |
| stale 传播基础 | **PASS** | The §15.2 dependency graph with a schema-column justification per edge, `PropagateFrom` walking it, the waiver rules of §15.3, and an end-to-end test against a real database. Severity-against-the-original-change was mutation-verified from two directions. |
| 创建 Drama Project/Episode/Asset | **PASS** | `TestCreateDramaProjectWritesSettingsAndDramaCanvas` (real SQLite, every wizard field round-tripped) plus the episode and asset commands in the story/script/asset suites. The E2E suite covers the browser-side refusal only, because a browser has no core (see the limits below). |
| Canvas projection 基础 AC-CANVAS-001/002 子集 | **PASS** | `CreateCanvasProjection` (reference written and read back, idempotent, half a reference refused), `RemoveCanvasProjection` (node gone, entity untouched), `DeleteNodes` refusing a required reference, `CreateEdge` validating against the registry, and `FindEntityReferences` listing an entity's projections and blockers. Six tests, two of them mutation-verified. |

The vocabulary parity guard (`vocabulary_parity_wp05_test.go`) parses each closed vocabulary out of
the migrations per table and compares it against the Go list that must agree, in both directions. It
checks itself: it asserts the parser finds a known value, that per-table parsing keeps two `status`
columns apart, and that a mismatch, a duplicate and an unreadable column are each reported rather
than silently passing.

## The three documented vocabulary conflicts

All three were resolved in favour of the PRD's spellings with the reasoning recorded in the
migration headers, the package docs and ADR-0007, and each is pinned by a test that asserts the
rejected spellings stay rejected. The quality-gate stage keys are deliberately left unpinned
because choosing between the two documented lists belongs to WP-07.

## Defects found by review and fixed before this record

Two independent reviews ran. A spec review found four blockers; three were real gaps against
WP-05's own acceptance bullets.

1. **Projection had no writer.** Nodes carried entity-reference columns and nothing in the product
   ever filled them, so AC-CANVAS-001 was untestable. `CreateCanvasProjection`,
   `RemoveCanvasProjection` and `FindEntityReferences` were added.
2. **A required reference did not block a delete.** `canvas_edges.required` was stored and never
   read. `DeleteNodes` now refuses the batch and names the blocking edges.
3. **`CreateEdge` stored any registered relation without checking endpoints.** It now runs the
   registry, refuses an illegal edge, and writes the verdict to `validation_status`.
4. **The approved-uniqueness test covered 3 of 8 families**, so weakening five of the eight indexes
   left the suite green. All eight are covered now.

The spec review also found a correctness bug in the registry: `requires_version` demanded a version
on both endpoints, which made `uses_character` and `first_frame_of` unsatisfiable because a shot has
no version column. It now requires the version only on the versioned side.

A quality review then ran twelve mutation probes. Ten were caught. The two that were not:

- `ApproveVersion`'s supersede step had no test at all — the application assets package had no
  service test and there was no assets integration test. `assets_wp05_test.go` now covers it plus
  the approval preconditions, the impact list, lineage and usage.
- The locked-rule service test also exercised the lock-change guard, so removing `CanModify` left it
  passing. An isolating test was added.

Both reviews' remaining findings were also addressed: two dead functions were removed or their
comments corrected to say plainly that nothing calls them yet, and two hardcoded wizard
placeholders became i18n keys.

## Known limits and deferred work

- **The E2E suite cannot cover the with-core path.** A browser has no Go core, so creating a project,
  listing episodes and reading projections are verified at the application and database layers and
  by the binding tests, not end to end. The E2E spec says so rather than mocking the bindings, which
  would test a fiction. A desktop-driven suite is the only way to close this.
- **Domain events (§17) are not implemented.** The workflow service writes a `workflow_events` audit
  row per state change, which satisfies PRD FR-100, but the §17 event stream and its envelope are
  absent. Scope item 11 is therefore one third delivered: commands and queries are there, events are
  not. This is the largest single gap this package leaves.
- **Six of the eight version families have no approval command.** Only script versions, asset
  versions and storyboard panels can be approved. Skeleton, strategy, style guide, director plan and
  storyboard version rows can hold `approved` only if something else writes it, which nothing does.
- **§19's integrity checks are not implemented.** The code comments in the workflow domain point at
  a check that does not exist yet; §19 was not in WP-05's scope, but the reference is a promise the
  next package should keep or remove.
- **`IsContentFrozen` has no caller.** No version-edit command exists in WP-05, so §2.5's
  "批准后不可原地编辑" is not enforced anywhere. The predicate is in place for the first edit
  command and its comment says so.
- **Dialogue lines, story entity aliases and fact sources have tables, domain types and validation
  but no command.** Their writers are WP-06's extraction pipeline and WP-08's script path.
- **`asset_usages` other than the ones the asset commands write** are populated by nothing else yet;
  the shot and panel commands that would record a reference belong to WP-08/WP-09.
- **The studio's story-graph section cannot list entities**: `DramaBinding` exposes creation,
  acceptance and rejection but no list query, so the section states the gap rather than showing an
  empty table it cannot fill.
- `LoadCanvas` returns a project's oldest canvas document. For a wizard-created drama project that is
  its drama canvas; for a migrated project with pre-existing documents it may be another one.
- ADR-0002 remains **Proposed**, as WP-04 left it. WP-05 followed the migration contract it records
  but did not change its status.
- `docs/adr/README.md`'s index lists ADR-0002 as Accepted while the file itself says Proposed. The
  index was extended with 0007 and 0008 but this pre-existing discrepancy was not silently changed.
- Real paid providers were never contacted; every test uses synthetic fixtures and temporary
  databases.

## Git and data safety

- Existing user changes preserved: **yes**.
- Automatic commit/push/stash/reset/clean: **none**.
- Secrets found or introduced: **none**; the scanner passes over 365 files.
- Migrations executed against user data: **none**. `000006`–`000012` ran only against temporary test
  databases. The `000009` rebuild of the asset tables is covered by an upgrade test that seeds
  WP-04-shaped rows first and asserts every row, id and status mapping survives, that
  `PRAGMA foreign_key_check` is clean, and that a second run is a no-op.
- Every mutation probe run during review was restored, and the working tree was verified clean
  afterwards.

---

# 0d. WP-04 result (2026-09-17)

## Scope completed

- **Status: COMPLETE (WP-04 scope)**. Every acceptance item below was executed on this host;
  the limitations are listed explicitly and are not disguised as passes.
- Schema: forward-only `000004_projects_canvas.sql` adds workspaces, projects, canvas documents,
  nodes, edges, chat sessions, assets, asset versions, asset files, archived generation history
  and the legacy import bookkeeping. `000005_legacy_project_fingerprints.sql` adds the per-project
  fingerprint table the idempotency check needs. `000001`-`000003` were not modified.
- Domain: `internal/domain/project` (workspaces, projects, canvas kinds, the relation registry
  with the `generic` fallback) and `internal/domain/asset` (assets, versions, the approval
  preconditions). Neither mints or validates an identifier (ADR-0005).
- Application: `internal/application/projects`, `.../assets`, `.../legacy` (snapshot, fingerprint,
  transform, import) and `.../backup` (export and restore).
- Infrastructure: SQLite repositories with revision guards and cascades, the atomic import, the
  hardened ZIP reader/writer, and the backup store.
- Bindings: `ProjectsBinding` (projects, canvas, nodes, edges, chats, import),
  `LegacyUploadBinding` (the chunked base64 channel) and `BackupBinding` (export and restore
  preview). The upload surface was audited down to its five transfer methods: the configuration
  entry point and the media reader are package-level functions, so neither a filesystem path nor
  an internal port reaches the webview.
- Frontend: the canvas persistence adapter (Go and legacy implementations behind one mode
  decision), the migration dialog in both locales, and a Playwright regression for the canvas.

## Commands executed and actual results

| Command | Result |
|---|---|
| `go test ./... -count=1` | **PASS** (19 packages ok) |
| `go vet ./...` | **PASS** |
| `gofmt -l .` | **PASS** (no output) |
| `node scripts/security-scan.mjs` | **PASS** (296 files scanned; 1 audited dynamic-execution exception, 4 audited legacy direct-call files with named owners) |
| `web`: `npm run typecheck` | **PASS** |
| `web`: `npm test` | **PASS** (34 tests) |
| `web`: `npm run test:e2e` | **PASS** (13 passed, 1 skipped; the skip is the crop dialog, which needs node content a headless run cannot supply) |
| `web`: `npm run build` | **PASS** with the pre-existing over-500 kB chunk warning |
| `scripts/verify.sh` / `scripts/verify.ps1` | **PASS**; both now run the canvas regression, and each reports its own skip reason when Playwright is absent |
| `wails build -s` (v2.15.0) | **PASS**; produced `build/bin/InfiniteAtelier.exe` |
| `git diff --check` | **PASS** (exit 0; only pre-existing LF/CRLF advisory warnings) |
| `go test -race ./... -count=1` | **ENVIRONMENT FAILURE, not a pass** — the host 32-bit MinGW GCC cannot compile amd64 CGO. |

## Acceptance

| ID | Result | Evidence |
|---|---|---|
| AC-LEGACY-001 | **PASS** | `TestImportAllNodeTypesIsComplete` (counts, text, viewport, warnings, relations), `TestLegacyImportSnapshotIsAtomic` against the real store, `TestImportMissingMediaIsReportedNotFatal`, `TestImportMalformedMetadataIsRetained`, and the media checks against the fixtures in `testdata/old-projects/`. |
| AC-LEGACY-002 | **PASS** | `TestImportIsIdempotent` (the second import skips, copy mode makes a distinct project, media stays deduplicated) plus `TestLegacyProjectFingerprintIsPerProject` against the real store, which is the level an earlier revision failed at. |
| AC-LEGACY-003 | **PASS** | `TestLegacyImportRollsBackCompletely`, `TestLegacyProjectFingerprintSurvivesFailure`, `TestImportFailureLeavesNoProject` (the failing stage is named) and `TestImportDatabaseFailureIsReported`; the upload tests cover temporary-file cleanup. |
| AC-CANVAS-004 | **PASS** | `web/e2e/canvas-regression.spec.ts`: add, move (asserting a second node did not shift), delete, multi-select, box select, zoom, pan (asserting every node shifted by one delta), undo/redo, connections, the minimap toggle, the generation prompt surface, the image node's actions and a reload. Each was mutation-checked: breaking the behaviour fails its test. |
| AC-BACKUP-001 | **PASS** | `TestBackupRoundTrip`, `TestBackupContainsNoSecrets`, `TestRestoreRefusesSecretBearingArchive`, `TestRestoreRefusesCredentialHiddenInAnObject`, `TestBackupScanCoversAuthorizationAndCookie`, `TestRestoreRefusesTamperedArchive` and `TestBackupExportAgainstRealStorage`. |
| AC-BACKUP-002 | **PARTIAL** | Tamper detection and staged validation are tested; the atomic swap of a live database is not implemented, so a restore validates and stages and the promotion step belongs to WP-12. |
| AC-SEC-002 | **PASS** | The corpus in `internal/infrastructure/archive/archive_test.go`: `../`, absolute paths, drive letters, UNC, device names, trailing dot and space, dot elements, symlinks, duplicate normalised paths, entry-count and ratio bombs, size ceilings, corrupt input, a false manifest and a wrong hash. |

## Independent review

**Spec review (independent subagent): CHANGES REQUIRED → fixed.** It found six blockers, each
confirmed against the code and fixed:

- A node the canvas created was never stored: the binding treated an empty id as "create" while the
  adapter always sends the id it minted, so every new node took the update path and failed on a row
  that did not exist. The binding now decides by whether the row exists.
- The viewport was never persisted: the adapter sent revision 0 against a document whose revision
  starts at 1, so every write conflicted. The service now writes against the revision it just read.
- Idempotency could never fire: the store compared a per-project fingerprint, but the import
  recorded the whole run's, so a second import duplicated the project. A new table holds one row per
  imported project. `TestLegacyProjectFingerprintIsPerProject` pins it against the real store; the
  in-memory double had been recording whatever it was asked about, which is how the bug hid.
- The migration dialog read `undefined`: the Go structs carried no JSON tags, so the wire shape was
  PascalCase while the TypeScript types expect camelCase. Every shared struct is tagged, and the
  collections are never nil.
- The generation history was never converted: `bundle.History` was never populated, so an entire
  entity class was silently absent. `TransformHistory` archives it and the all-node-types test
  asserts the two records its fixture declares.
- The backup services were composed but unreachable: no binding exposed them. `BackupBinding` now
  exports an archive and previews a restore.

It also found majors, all fixed: the precheck committed media while the UI claimed nothing was
written; the media-hash assertion in the import test was vacuous; ADR-0006 described a snapshot and
a scheduler pause the code does not have, and now records both as gaps; the restore staging area was
never cleaned; the secret scan read only the manifest and the database and looked for no header
names; several DOMAIN_MODEL §8 fields were absent; project rename and delete bypassed the adapter;
the save diff recorded a write as done before it had succeeded; and four of the nine AC-CANVAS-004
tests could pass while the behaviour they named was broken.

## Canvas regression

The suite found a real regression while it was being written: a project reference added to a save
effect's dependency array made the renderer loop until React aborted with "Maximum update depth
exceeded". The page read the project through a store subscription inside an effect that also wrote
to that store, so the effect triggered itself. It now reads the project at call time.

## Known limits and deferred work

- A restore validates and stages but does not swap a live database; that promotion and its
  user-confirmation step belong to WP-12 (AC-BACKUP-002).
- Encrypted sensitive backup is not implemented and is out of WP-04 scope.
- `asset_relations` and `asset_usages` (DOMAIN_MODEL §8.5/§8.6) are not created, and the asset file
  roles are narrower than §8.4 lists: the canvas image tools that would produce a mask or a first
  frame are not migrated yet. This is a deliberate deviation, recorded here rather than claimed as
  conformance.
- The `assets` application service has no composition root: the import writes asset rows through the
  repository directly. It exists for WP-05.
- The generation history and the asset library are stored per browser profile, so both are attached
  to the first imported project and the report says so.
- MONOFORM's scene data, the prompt library and UI preferences are recorded and reported, not
  imported: they belong to another tool or to frontend state.
- The canvas regression runs in browser mode. A Wails window cannot be driven from a test runner, so
  the Go adapter's command shapes are covered by the Go binding tests rather than end to end.
- Real paid providers were never contacted; the import and backup evidence uses the synthetic
  fixtures in `testdata/` and temporary databases.

## Git and data safety

- Existing user changes preserved: **yes**.
- Automatic commit/push/stash/reset/clean: **none** (the commits were requested by the user).
- Secrets found or introduced: **none**; the scanner passes over 296 files, and the only fixture
  holding a key-shaped string is exempted by name with stale-exemption detection.
- Migrations executed against user data: **none**; `000004` and `000005` ran only against temporary
  test databases, and the upgrade tests assert every pre-existing row survives.

---

# 0c. WP-03 result (2026-09-15)

## Scope completed

- **Status: COMPLETE (WP-03 scope)**. All acceptance items below were executed on this host; limitations are listed explicitly and are not disguised as passes.
- Schema: forward-only `000003_jobs.sql` adds `generation_jobs`, `job_attempts`, `job_dependencies`, and rebuilds `provider_requests`/`provider_configs` to widen the capability/kind CHECKs while preserving existing rows. `000001`/`000002` were not modified.
- Domain: `internal/domain/job` owns the state set, transition table, retry policy, dependency conditions, and the stable error taxonomy; it has no infrastructure dependencies.
- Application: `internal/application/jobs` owns submit (idempotent), cancel (including a best-effort remote cancel and an orphan-candidate record), retry-failed-only, list/get, pause/resume, the priority scheduler, the worker pool with lease + revision guards, and the startup recovery scanner.
- Infrastructure: SQLite `JobRepository` (claim/CAS/idempotency/attempts/dependencies), the result pipeline (`ResultStore` → content-addressed FileStore → object metadata → `file_references`), a download-policy client for provider-supplied URLs, OpenAI- and Gemini-compatible image adapters, and deterministic async video / audio contract mocks.
- Bindings: narrow `JobsBinding` (list/get/attempts/submit/cancel/retry/pause/resume/summary/read-result-file). No execute, no resolve, no arbitrary path, no SQL. Bindings regenerated with the pinned Wails v2.15.0 CLI.
- Frontend: Job Center page (queue table, progress, batch cancel, retry-failed-only, pause/resume, accessible status text plus colour), desktop jobs client with event subscription, image generation routed through the Go job manager in secure mode with no fallback to the legacy path, and a migration notice stating that video/audio still use the browser-direct path.
- Scans: the security scanner now also refuses NEW browser-direct provider calls, with exact legacy-file exemptions and stale-exemption detection.

## Commands executed and actual results

| Command | Result |
|---|---|
| `go test ./... -count=1` | **PASS** (19 packages ok) |
| `go vet ./...` | **PASS** |
| `gofmt -l .` | **PASS** (no output) |
| `node scripts/security-scan.mjs` | **PASS** (247 files scanned; 1 audited dynamic-execution exception, 4 audited legacy direct-call files with named owners) |
| `web`: `npm run typecheck` | **PASS** |
| `web`: `npm test` | **PASS** (21 tests) |
| `web`: `npm run build` | **PASS** with the pre-existing >500 kB chunk warning |
| `scripts/verify.sh` | **PASS** (all steps; Wails SKIPped by the script because the CLI is not on PATH) |
| `scripts/verify.ps1` | **PASS** (same Wails caveat) |
| `wails build -s` (v2.15.0) | **PASS**; produced `build/bin/InfiniteAtelier.exe` |
| `git diff --check` | **PASS** (exit 0; only pre-existing LF/CRLF advisory warnings) |
| `go test -race ./... -count=1` | **ENVIRONMENT FAILURE, not a pass** — the host 32-bit MinGW GCC cannot compile amd64 CGO. Concurrency was reviewed by tracing and targeted probes only. |

Note on the Wails invocation: `wails build` runs the frontend `npm ci` itself, which fails with `EPERM` while the user's own Vite dev server holds locks on `web/node_modules`. The build is therefore run with `-s` after a successful frontend build, as in WP-02. On this host a `wails build` without `-s` was observed to leave `web/node_modules` half-uninstalled (`.bin` empty); `web/package.json` and `web/package-lock.json` were verified byte-identical afterwards (SHA-256) and the dependencies were repaired with the same `npm install --legacy-peer-deps --include=optional --package-lock=false` command WP-00 recorded.

## Acceptance

| ID | Result | Evidence |
|---|---|---|
| AC-FOUND-006 (job) | **PASS (WP-03 scope)** | queued→running→succeeded: `TestSchedulerRunsJobsToCompletion`; retry_wait: `TestExecuteRetriesTransientFailure`, `TestSchedulerRetriesThenSucceeds`, `TestJobRepositoryClaimableRespectsRetryWindow`; cancel: queued + in-flight + during-a-failing-attempt + during-a-poll + during-shutdown (`TestCancelQueuedJobIsImmediate`, `TestCancelDuringFailingAttemptStillTerminates`, `TestCancelDuringPollStillSettles`, `TestCancelSettlementSurvivesCancelledWorkerContext`); crash/restart: recovery tests incl. `TestRecoverDownloadingResumesWithoutReCallingProvider` and `TestCrashWithinLeaseStillRuns`; remote ID poll never re-submits and is paced: `TestRecoverResumesRemoteWithoutResubmitting`, `TestVideoRestartResumesPollingWithoutResubmitting`, `TestRemotePollIsPaced`, `TestParkedPollDoesNotConsumeAttemptBudget`; duplicate idempotency: `TestSubmitIsIdempotent`, `TestJobRepositoryDuplicateIdempotencyKeyReturnsConflict`; result commit: `TestResultPipelineWritesMetadataAndReference` against the real FileStore + repositories; interrupted writes release their claim: `TestShutdownDuringAttemptSettlesJob`, `TestShutdownReturnsClaimedJobToQueue`, `TestCancelDuringUnrecordableOutcomeStillSettles`. |
| AC-MEDIA-001 (async video, mock) | **PASS for the mock contract (8/9 items)** | submit/remote ID/poll/restart/fetch/validate/duplicate response/cancel are covered by `TestRunnerVideoFullAsyncFlow`, `TestVideoRestartResumesPollingWithoutResubmitting`, `TestVideoDuplicateResponseDoesNotCreateSecondAsset`, `TestVideoMockContractExercisesTheFullLifecycle`, `TestRunnerVideoRemoteOnlyOutcome`. The ninth item, "asset version", has no `AssetVersion` entity in this package: assets belong to WP-05, and the job pipeline records `file_references(owner_type='job')` instead. **This is an explicit gap, not a pass.** |
| AC-SEC-001 (SSRF) | **PASS (unchanged)** | WP-02 corpus still enforced; the new download policy adds https-only, all-addresses-public, per-hop redirect re-validation, and a streaming byte cap (`internal/infrastructure/providerhttp/download_test.go`). |

## Independent review

- **Spec review (independent subagent): CHANGES REQUIRED → fixed → re-review: CHANGES REQUIRED → fixed.** Round one found a blocker (every result commit failed against the real database because the pipeline wrote a `file_references` row without recording `file_objects` first — the FK then rejected it, so no job could ever reach `succeeded`), plus majors (cancel-while-running never terminated; the `downloading` phase never persisted; the async cancel contract never invoked; mock media adapters reachable in production; the legacy-media notice not rendered; idempotency scope ignoring the entity). Round two confirmed the blocker fixed and found that my first `downloading` fix introduced two new blockers: the runner's mid-attempt row write invalidated the worker's revision (wedging any job whose fetch failed), and nothing consumed the marker (so a reclaimed job called the provider twice). Both were re-fixed by removing the mid-attempt write entirely: the runner now returns the phase as an Outcome, the worker persists it under its own revision guard, and the next pass resumes the fetch from the persisted marker.
- **Quality review (independent subagent): CHANGES REQUIRED → fixed.** Findings included `AllowLocal` permitting cleartext HTTP to a public host, deltas published after a terminal stream event, and several tests that could not detect the defect they named.

### Quality review (WP-03)

An independent quality review found, and the fixes above cover: the mock video/audio payloads were rejected by the real content allowlist (Go sniffs the mock MP4 as `application/octet-stream` and WAV as `audio/wave`, which the allowlist omitted) — both are corrected and pinned by `TestMockPayloadsPassTheRealAllowlist`, which runs the payloads through the real FileStore and allowlist; a `count>1` image batch collapsed into one job because every request shared an idempotency key (the batch ordinal is now part of the scope, with behavioural tests); three image paths submitted unscoped keys (all now carry the node identity); aborting a canvas generation left the Go job running (abort now cancels it by id, and the wait is bounded); the result download could deadlock when the store stopped reading (the reader is now closed on that path); a claim abandoned at shutdown held its lease for the full TTL (it is handed back); `attempt_count` could regress and loop (it can no longer move backwards); and the batch of dead helpers/options was removed.

Subsequent adversarial rounds (each one run by an independent reviewer against the current code, with its findings then fixed and mutation-checked here) found and closed:

- **Unpaced remote polling (major).** A parked `waiting_remote` job was re-claimed as soon as a worker freed up, i.e. at provider round-trip speed: a real video job would be polled hundreds of times per minute, one attempt row per poll, and the attempt budget would be exhausted by polling. Polls are now paced through `next_retry_at` (both the SQL candidate query and the claim guard honour it) and a pure poll is not an attempt (no counter advance, no history row), while a failing poll and a completing pass remain real attempts. ADR-0004 §1b records the rule; `TestRemotePollIsPaced`, `TestParkedPollDoesNotConsumeAttemptBudget`, `TestRemotePollFailureStillRetries`, and `TestJobRepositoryParkedJobIsPacedByRetryWindow` pin it.
- **Crash-era attempt numbering (blocker).** A crash between `StartAttempt` and the settle leaves attempt row N while `attempt_count` still reads N-1. Recovery skips jobs whose lease looks live, so a restart inside the 30-minute lease window left the counter stale and every later `StartAttempt` collided with the orphaned row, wedging the job in a claim/record-failure/release loop. Attempt numbering is now derived from the stored rows (`nextAttemptNumber`), and `release` schedules a minimum delay so a storage failure cannot spin the scheduler.
- **Cancel could leave a job claimed as running (major).** Cancelling only *flags* a running job, so if the cancel raced the worker's own outcome write, the failed write went to `release`, which returned early on the cancel flag — leaving the row `running` on a live 30-minute lease with no worker to settle it, because recovery skips live leases. `release` now settles a cancel-flagged job itself.
- **Settlement writes depended on a live worker context (major).** `finish`, `finishOutcome`, `release`, and the attempt-history writes ran on the caller's context, so a worker cancelled by shutdown failed every read and write and stranded the job exactly as above. All settlement writes now run on a bounded `settleContext` (the caller keeps its values, only the cancellation is dropped), so scheduler state is persisted before exit as ARCHITECTURE §6.2 requires.
- **Cancel during a poll re-parked the job** instead of settling it; the `PollOnly` branch now settles a cancel-flagged job and preserves the durable orphan note.
- **Unvalidated provider progress.** The provider-reported percentage is written into a `CHECK (0..100)` column; a provider reporting 120 made the whole row update fail *after* the artifact was fetched, turning a reporting quirk into a retry loop. It is now clamped.
- **Test-double drift (twice).** The infrastructure test double reimplemented Go's content sniffer (accepting truncated MP4 boxes and "GIF8" that the real sniffer rejects), and the in-memory job repository ignored the poll window. Both doubles now use the real behaviour (`http.DetectContentType`; the same claim guard as the SQL), and the MP4 payloads used in tests are genuinely valid boxes.
- Several regression tests were found not to detect the defect they named (a cancellation injected before the claim never reached the poll branch; the settlement test failed every write including the recovery write). They were rewritten and each fix was verified by reverting it and observing the test fail.

## Known limits and deferred work

- The async video and audio adapters are **contracts with deterministic mocks**, not real provider integrations; those belong to the media work package. The mock kind is rejected by the configuration path so a real provider cannot resolve to it.
- `estimated_cost` is never populated: no pricing table exists, and ADR-0004 does not define one. Cost accounting belongs to the model-policy work package.
- Video/audio generation in the UI still uses the legacy browser-direct path; the user is told so at startup, and the scanner refuses new direct calls.
- No `AssetVersion` entity exists yet (WP-05); the pipeline records job-owned file references instead.
- macOS/Linux evidence, remote CI, and `-race` remain unavailable for the reasons recorded under WP-01.
- Real paid providers were never contacted; all provider evidence uses httptest, fakes, and the deterministic mocks.
- Mock media calls do not write an audit row (they are a local test seam, not provider calls).
- `Options.LeaseTTL`/`WorkerCount`/`PollInterval` were removed rather than wired: the scheduler uses the package defaults, and a configurable value with no reader was misleading. `RemotePollInterval` **is** wired (`job_wiring.go`, 5 s), because the poll cadence is a real product decision rather than an internal constant.
- A poll of an unused async image contract (`ImageOutcome.RemoteJobID`) is reported as `unsupported` rather than silently succeeding: no shipping image adapter returns a remote ID, and the runner has no image poll implementation, so accepting it would fabricate a result.
- The audio formats offered in the UI (MP3/WAV/Opus/AAC/FLAC/PCM) are wider than the content allowlist: FLAC, AAC and raw PCM have no signature Go's sniffer can recognise, so such a result is rejected as `response_invalid` rather than stored with an opaque type. Real adapter work belongs to the media package and must revisit this deliberately.

---

# 0b. WP-02 result (2026-09-15)

## Scope completed

- **Status: COMPLETE (WP-02 scope)**. All acceptance items below were executed on this host; limitations are listed explicitly and are not disguised as passes.
- SecretStore: Windows Credential Manager adapter (`advapi32` `CredReadW`/`CredWriteW`/`CredDeleteW`/`CredFree` via `windows.NewLazySystemDLL`), non-Windows `UnavailableStore` that fails closed, and a test-only in-memory fake. See ADR-0003.
- SQLite: forward-only migration `000002_provider_security.sql` adds `secret_references`, `provider_configs`, `provider_requests`; no secret-value column exists; `provider_configs.secret_ref` FKs to `secret_references` with `ON DELETE RESTRICT`. `000001_foundation.sql` was not modified.
- Controlled provider HTTP: scheme/host/port policy, DNS A/AAAA resolution with forbidden-range rejection (full SECURITY §6.2 CIDR list plus IPv4-mapped forms and cloud metadata), dial-the-validated-IP pinning, per-hop redirect re-validation, TLS verification that cannot be disabled, no environment proxy, and connect/TLS/header/idle/total timeouts with a bounded response body.
- First adapter: trusted registry + OpenAI-compatible text `Generate` and SSE `Stream` with Go-side `Authorization`, 429/5xx/4xx taxonomy, bad-JSON and truncated-stream handling, cancellation, redacted audit records (now including provider-reported token units), and a health probe that is also audited.
- Narrow Wails bindings: secret `Status`/`Set`/`Delete` (no Resolve) and provider `ListConfigs`/`SaveConfig`/`DeleteConfig`/`GenerateText`/`StreamText`/`CancelStream`/`CheckHealth`. Bindings were regenerated with the pinned Wails v2.15.0 CLI; the generated surface contains no `resolve`/generic fetch/arbitrary proxy.
- Frontend: desktop provider/secret client, secure secret field written through the Go binding, secure-mode guard that makes `new Function` script execution unreachable, URL-supplied keys ignored with an explicit notice, text generation routed through the Go gateway (encoded `channel::model` selections decoded before the call and routed to the matching provider), legacy-plaintext-key migration warning, and bilingual (zh-CN/en-US) copy.
- Ordinary config export/import and ZIP backup strip raw keys (removal, not masking) and sanitize legacy imports.
- Static scans: `scripts/security-scan.mjs` runs in both verify scripts and CI; it covers dynamic execution, high-confidence key patterns, and unguarded config serialization, with exact per-file exemptions and stale-exemption detection.

## Commands executed and actual results

| Command | Result |
|---|---|
| `go test ./... -count=1` | **PASS** (16 packages ok; includes live Windows Credential Manager round-trip) |
| `go vet ./...` | **PASS** |
| `gofmt -l .` | **PASS** (no output) |
| `node scripts/security-scan.mjs` | **PASS** (199 files scanned; 1 audited dynamic-execution exception owned by WP-12) |
| `web`: `npm run typecheck` | **PASS** |
| `web`: `npm test` (new) | **PASS** (15/15 security-focused unit tests) |
| `web`: `npm run build` | **PASS** with the pre-existing >500 kB chunk warning |
| `scripts/verify.ps1` | **PASS** (Wails step SKIPped because the CLI is not on PATH; executed separately) |
| `scripts/verify.sh` | **PASS** (same Wails caveat) |
| `wails build -s` (v2.15.0) | **PASS**; produced `build/bin/InfiniteAtelier.exe` |
| `git diff --check` | **PASS** (exit 0; only pre-existing LF/CRLF advisory warnings) |
| `go test -race ./... -count=1` | **ENVIRONMENT FAILURE, not a pass** — the host 32-bit MinGW GCC cannot compile amd64 CGO. Concurrency was reviewed by tracing and targeted probes only. |

## Acceptance

| ID | Result | Evidence |
|---|---|---|
| AC-FOUND-004 (secret) | **PASS (WP-02 scope)** | Native+fake SecretStore tests; schema test rejects value-shaped columns; generated bindings expose no Resolve; `provider_bindings_test.go` proves fail-closed and no leak; ordinary export/backup strip keys (frontend tests assert no key substring). |
| AC-FOUND-005 (text/security subset) | **PASS (text subset only)** | Go adds Authorization (`TestGenerateSendsAuthorizationFromGo`); timeout/429/5xx/bad JSON/truncated stream/cancel tested; allowlist and redirect-SSRF tested; stream cancel now also verified through the cancel-by-stream-ID path. Image/video/audio provider migration is **not** claimed. |
| AC-SEC-001 (SSRF corpus) | **PASS** | Full §6.2 CIDR corpus + IPv4-mapped + metadata; rebinding test asserts a single DNS lookup; redirect re-validation and TLS are structural and tested; the exact-local exception now requires *all* resolved addresses to be private and still blocks metadata. |

## Independent review

- **Spec review (independent subagent): CHANGES REQUIRED → all findings fixed.** Blocker: the production path pinned port 443 while building portless URLs, so every normal provider would have been rejected by its own policy (test seams hid it); fixed by normalizing the effective port, with a regression test that uses the real constructors. Majors fixed: encoded `channel::model` sent as the API model name; secure mode blocking generation on the legacy plaintext-key readiness check; a missing legacy-key migration warning; missing audit cost/unit fields. Minors fixed: empty provider ID accepted, metadata reachable under local approval, stale/too-wide scan exemptions, and missing test coverage for the production client/health/stream paths.
- **Quality review (independent subagent): CHANGES REQUIRED → all findings fixed.** Blocker: model encoding (same as above). Majors fixed: `AllowLocal` could permit cleartext HTTP to a *public* host (now requires all resolved addresses to be private; frontend no longer marks any `http:` URL as local); deltas could be published after the terminal stream event (now gated). Minors fixed: inconsistent error shape between secret and provider bindings, missing diagnostic ID on stream errors, a tautological size-limit test, a rebinding test that could not detect re-resolution, dead/misleading code (`trimTrailingSlash`, `readBounded` return value, `sanitizeLegacyConfig`), duplicate secret-ref namespace, unused `provider.SecretStatus`, and an audit test without read-back (now asserts the persisted columns).

## Known limits and deferred work

- macOS Keychain / Linux Secret Service backends are not implemented; those platforms run providers disabled (fail closed) until a later package.
- Image/video/audio/embedding provider calls, the persistent Job Manager, Agent runtime, Workflow, Memory, Drama domain, and the full backup/restore redesign remain later work packages. Legacy browser media paths still exist and are explicitly out of WP-02 scope; they have **not** been made secure, and no automatic fallback to them exists for text.
- Ordinary export/backup now exclude keys, but an already-written legacy config or backup on disk is not retroactively scrubbed.
- No remote CI run (no push authorized) and no macOS/Linux native evidence; `wails build` ran locally only.
- Real paid providers were never contacted; all provider evidence uses httptest/fakes.

---

# 0. Current WP-01 state

- User approval to begin WP-01: **YES — 2026-09-04**.
- Current phase: **WP-01 COMPLETE — Tasks 1–11 are implemented and approved with the evidence recorded below.**
- Plan: `docs/plans/2026-09-04-wp-01-secure-desktop-foundation.md`.
- Task 1–9 approval provenance and Task 9 production-native route proof remain in `docs/implementation/handoff-2026-09-08-153906+0800.md` and `docs/implementation/wp01-execution-2026-09-07.md`; the historical Task 9 binary fingerprint was `a52e7add9ea7959ee948466998400cffdc5ecdfe53b8d9c94c5c3e897519d1d5`.
- Task 10 (2026-09-08): `scripts/verify.ps1` and `scripts/verify.sh` now run root `go test ./... -count=1` and `go vet ./...`, invoke `wails build` only when the CLI is available, preserve nonzero executed-gate failures, and restore `web/dist/.gitkeep` after either success or failure. The existing local Wails v2.15.0 CLI was used to prove both script paths. CI retains the Ubuntu web job and adds a Windows Node 22 / Go 1.25 / Wails v2.15.0 build job; remote GitHub Actions was not run because no push was authorized. README now distinguishes loopback browser mode from desktop mode and documents the deferred Secret/Provider/legacy migration work. Independent spec review found a missing placeholder, which was corrected and re-reviewed **APPROVED**; independent quality review **APPROVED**.
- Task 11 (2026-09-08): final `go test ./... -count=1`, `go vet ./...`, both verification scripts, POSIX syntax check, and Wails v2.15.0 production builds passed. The final isolated native smoke used owned PID `5944` and redirected `APPDATA`, `LOCALAPPDATA`, `USERPROFILE`, `HOMEDRIVE`, `HOMEPATH`, and `WEBVIEW2_USER_DATA_FOLDER` under a fresh system-temp root. It created only the owned app DB/WebView/log paths, opened a native window, closed gracefully, retained schema migration `[(1,)]`, reported WAL from a post-close read-only connection, and produced a 130-byte log with no non-printing match for authorization/bearer/api-key/SQL indicator patterns. The owned root was removed after inspection. The app enables foreign keys per managed connection; the post-close Python connection reporting `foreign_keys=0` is SQLite's default for a new unrelated connection and is not an application-contract failure.
- `go test -race ./... -count=1` remains an **environment failure**, never a pass: the host 32-bit MinGW GCC cannot compile amd64 CGO (`cc1.exe: sorry, unimplemented: 64-bit mode not compiled in`).
- AC-FOUND-001, AC-FOUND-002, and AC-FOUND-003 are **PASS for the WP-01 Windows foundation scope**. Native packaging was proved on Windows only; macOS/Linux native packaging, remote CI execution, frontend automated tests/lint, Secret/Provider migration, and legacy browser-data migration remain future work.
- Active constraints: WP-01 is closed; preserve all WP-00/user changes; no commit/push/stash/reset/clean; no real Provider calls. WP-02 was implemented and closed on 2026-09-15 (section 0b); WP-03 was implemented and closed on 2026-09-15 (section 0c); WP-04 was implemented and closed on 2026-09-17 (section 0d); WP-05 has not started.

---

# 1. WP-00 result

- Status: **COMPLETE**
- Branch/commit: `main` / `a243891455ec17687dd54b5ac90d3bd64478a1a1`, tracking `origin/main`
- Product code changed: **no**
- User/specification changes preserved: **yes**
- Recommended next work package: **WP-01 — Wails/Go Core/SQLite/FileStore foundation**
- User approval required before WP-01: **YES**

WP-00 established an evidence-backed baseline, repository audit, requirements traceability, desktop-framework decision, proposed SQLite decision process, and executable verification entry points. It did not introduce Wails, Go product code, SQLite, migrations, Provider changes, canvas refactoring, data migration, Drama UI, or WP-01 implementation.

---

# 2. Deliverables

| Deliverable | Status | Purpose |
|---|---|---|
| `docs/implementation/BASELINE.md` | COMPLETE | Host/tool versions, Git baseline, install/typecheck/build/start results, skips and reproducibility failure. |
| `docs/implementation/REPO_AUDIT.md` | COMPLETE | Routes, nodes, canvas behavior, stores, Provider/Secret paths, backups, MONOFORM, CI/tests/licenses and risk inventory. |
| `docs/implementation/TRACEABILITY.md` | COMPLETE | PRD/NFR/security/acceptance mapping to current modules, gaps, and future work packages. |
| `docs/adr/0001-desktop-framework.md` | ACCEPTED | Wails v2 desktop decision and WP-01 verification contract. |
| `docs/adr/0002-sqlite-driver-and-migrations.md` | PROPOSED | Driver/migration options and the evidence required before acceptance. |
| `scripts/verify.ps1` | COMPLETE / PASS | Windows verification for real current gates with explicit SKIP reasons. |
| `scripts/verify.sh` | COMPLETE / PASS | POSIX/Git-Bash verification for real current gates with explicit SKIP reasons. |

---

# 3. WP-00 baseline repository facts (historical snapshot before WP-01 implementation)

- At the WP-00 baseline, the product was React 19/Vite 7/TypeScript under `web/`; there was no Go module, Wails project, SQLite schema, native binding, or FileStore. WP-01 implementation has since added those foundations; see section 0 and current execution evidence.
- npm is the repository package manager because the main app and separate MONOFORM app use npm lockfiles v3.
- The route table contains `/`, `/assets`, `/canvas`, `/canvas/:id`, `/director`, `/config`, and `*`.
- Built-in canvas node types are image, text, config, video, audio, group, and director; plugin types are open strings.
- Browser persistence is split across Zustand, localStorage, localForage/IndexedDB Blob stores, and in-memory generation Maps.
- Raw API keys and model scripts are stored in frontend configuration; backups/config exports include the full configuration.
- Provider requests are made in the browser; development may route through an arbitrary-target Vite proxy; production uses direct URLs.
- `services/api/model-plugin.ts:120` executes user-authored JavaScript using `new Function` and passes secrets/network helpers.
- Video polling/cancellation is in memory and has no durable Job/restart recovery.
- MONOFORM is a separate Vite package, embedded by iframe from tracked prebuilt output; it uses wildcard `postMessage` without origin/source validation.
- The main canvas page is 3,051 lines/approximately 171 KB; MONOFORM `App.jsx` is 2,261 lines/approximately 131 KB.
- No frontend test or lint command exists. Prettier checking exists and currently fails on 147 files.
- Root license is MIT; no third-party notices or SBOM were found. Toonflow is not a code dependency.

---

# 4. Commands executed and actual results

| Command | Result |
|---|---|
| `git status --short --branch` | PASS; `main...origin/main`, only pre-existing untracked specification-pack files. |
| `npm ci --legacy-peer-deps` in `web` | **FAIL**; main package/lock mismatch with missing optional platform packages. |
| `npm install --legacy-peer-deps --include=optional --package-lock=false --no-audit --no-fund` in `web` | PASS; changed 728 installed packages, lockfile unchanged. |
| `npm run typecheck` in `web` | PASS. |
| `npm run format:check` in `web` | **FAIL**; 147 existing files reported. No rewrite performed. |
| `npm run build` in `web` | PASS with chunk/dynamic-import warnings; 7,384 modules. |
| `npm ci --no-audit --no-fund` in `web/monoform-studio` | PASS after host-cache permission retry; 92 packages. |
| `npm run build` in `web/monoform-studio` | PASS with large-chunk warning; 2,407 modules. |
| loopback Vite smoke + HTTP `/` and `/monoform/index.html` | PASS; both HTTP 200. |
| `& ./scripts/verify.ps1` | PASS; typecheck/build/MONOFORM build pass; test/lint/Go explicitly skipped. |
| `C:\Program Files\Git\bin\bash.exe -n scripts/verify.sh` | PASS. |
| `C:\Program Files\Git\bin\bash.exe scripts/verify.sh` | PASS after adding Windows `npm.cmd` selection. |
| frontend tests | SKIP; no test script/files. |
| Go tests / Wails build | SKIP; no Go/Wails implementation in WP-00. |

---

# 5. WP-00 acceptance

| Item | Status | Evidence |
|---|---|---|
| 规格文件阅读 | PASS | Required files read in the mandated order before edits. |
| Git 状态记录 | PASS | `BASELINE.md` section 2. |
| 环境版本记录 | PASS | `BASELINE.md` section 3. |
| 依赖安装基线 | PASS WITH KNOWN FAILURE | Clean main `npm ci` failure and fallback are both recorded truthfully. |
| typecheck | PASS | `tsc --noEmit`, exit 0. |
| build | PASS | Main and MONOFORM source builds, with exact warnings/sizes recorded. |
| tests | SKIP / GAP RECORDED | No existing test command/files; verification scripts state the reason. |
| 路由/节点/Store 审计 | PASS | `REPO_AUDIT.md` sections 2–4. |
| Provider/Secret/脚本审计 | PASS | `REPO_AUDIT.md` section 5. |
| 备份/数据格式审计 | PASS | `REPO_AUDIT.md` section 6. |
| MONOFORM 审计 | PASS | `REPO_AUDIT.md` section 7 plus source build/smoke evidence. |
| License/Notices 审计 | PASS | `REPO_AUDIT.md` section 9. |
| verify scripts | PASS | Both scripts executed/syntax-checked on the host. |
| ADR-0001 | PASS / ACCEPTED | Desktop decision recorded. |
| ADR-0002 | PASS / PROPOSED | Options and acceptance evidence recorded; implementation deliberately deferred. |
| AC-BASE-001 | **PASS** | Baseline has real commands/versions/results; product code unchanged; failures are not disguised. |
| AC-BASE-002 | **PASS** | Required functional inventory is complete in `REPO_AUDIT.md`. |

---

# 6. Unresolved failures and risks

1. Main `npm ci --legacy-peer-deps` is not reproducible from the checked-in lockfile. The lockfile needs a narrowly scoped repair and CI proof; WP-00 did not rewrite it.
2. Raw Provider secrets, arbitrary JavaScript execution, direct browser Provider requests, the arbitrary-target proxy, secret-bearing backups, and wildcard iframe messaging violate the target security boundary.
3. Browser persistence remains the current source of truth and has no transactional migration/read-back safety.
4. No automated regression/security/accessibility test suite protects existing canvas behavior.
5. Prettier gate fails on 147 existing files; broad reformatting would be an unrelated WP-00 diff.
6. Main and MONOFORM bundles exceed the 500 kB chunk warning threshold.
7. ADR-0002 remains Proposed until the real Wails/SQLite cross-platform spike is complete.
8. The Go CLI printed a host permission error for its telemetry upload token after reporting its version; there is no Go project to test yet.

---

# 7. Git and data safety

- Existing user changes preserved: **yes**.
- Tracked product-code modifications: **none**.
- Automatic commit/push/stash/reset/clean: **none**.
- Lockfiles changed: **no**.
- User databases, browser profiles, secrets, and media changed: **no**.
- Real Provider calls: **none**.
- Secrets found or introduced by WP-00: **no actual secret value found; none introduced**. The insecure secret-handling code path is documented.
- Migrations/backups executed: **none**.
- Generated build output and installed dependencies stayed in ignored `dist`/`node_modules` locations.

---

# 8. Historical next-step record

The prior WP-00 recommendation to begin WP-01 was completed, and the WP-02 and WP-03 recommendations were executed on 2026-09-15 (sections 0b and 0c). The authoritative current state is: WP-01 COMPLETE (Windows foundation scope); WP-02 COMPLETE (its recorded scope); WP-03 COMPLETE (its recorded scope, with the gaps listed in section 0c); WP-04 was implemented and closed on 2026-09-17 (section 0d); WP-05 has not started and requires separate user authorization.

---

# 9. WP-02 Git and data safety (2026-09-15)

- Existing user changes preserved: **yes** (brand-rename files in `main.go`/`wails.json`/i18n and all WP-01 tracked/untracked work were left intact).
- Automatic commit/push/stash/reset/clean: **none**.
- User databases, browser profiles, secrets, and media changed: **no**. The application data directory held no `app.db` before or after this session.
- Real Provider calls: **none**.
- Migrations executed against user data: **none**; `000002` was applied only to temporary test databases, and a pre-migration snapshot test covers the v1→v2 upgrade path.
- Credential Manager: only uniquely-prefixed `InfiniteAtelier:test:<test name>:<pid>:*` entries were created and deleted by the Windows store tests; no existing user credential was read, enumerated, or removed.
- Secrets found or introduced: **none**; the new high-confidence secret scan passes over 199 files with exact, stale-checked exemptions.
- Dependencies added: **none** (the Credential Manager adapter uses the existing `golang.org/x/sys/windows` indirect dependency).
- Incidental environment note: a user-owned Vite dev server was running on port 3000 and held file locks on `web/node_modules`. A required dependency repair (`npm install`, no lockfile change) and the WP-02 builds were completed without stopping that user process; `web/package.json` was restored to its session-start content plus the new `test` script after `npm install` rewrote it.
