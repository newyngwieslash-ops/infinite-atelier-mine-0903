# WP-00 Repository Audit

> Audited: 2026-09-04
> Scope: source, configuration, persistence, Provider paths, backup formats, MONOFORM, CI, tests, licensing, and compatibility risks
> Rule: verified repository facts are separated from future architecture requirements.

## 1. Repository shape

The current product is a browser-hosted React/Vite application under `web/`. There is no Go module, Wails app, SQLite database, migration directory, desktop binding, or native FileStore yet. `web/monoform-studio/` is a second standalone Vite application whose generated bundle is committed under `web/public/monoform/` and embedded by iframe.

Top-level operational inputs are the MIT `LICENSE`, `README.md`, `start.bat`, `scripts/start-windows.ps1`, npm application files, GitHub CI, and the newly supplied specification pack. Build outputs (`web/dist`, nested `dist`) and `node_modules` are ignored.

## 2. Routes and pages

`web/src/router.tsx:12-29` declares the complete route table:

| Route | Page | Current role |
|---|---|---|
| `/` | `pages/home` | Home / project entry. |
| `/assets` | `pages/assets` | Local asset library and transfer. |
| `/canvas` | `pages/canvas/index` | Canvas project list. |
| `/canvas/:id` | `pages/canvas/project` | Main infinite-canvas editor. |
| `/director` | `pages/director` | Standalone MONOFORM iframe. |
| `/config` | `pages/config` | Provider/model configuration. |
| `*` | `pages/not-found` | Not-found fallback. |

There are no current routes for Drama Project, Source, Chapter, Character, Scene, Event, Script, Workflow, Agent, Memory, Consistency, Quality Center, or Export Center. Those are future work packages, not hidden current capabilities.

## 3. Canvas inventory and behavior

`web/src/types/canvas.ts:12-23` defines built-in node types `image`, `text`, `config`, `video`, `audio`, `group`, and `director`, while allowing a plugin node type as an arbitrary string. `web/src/components/canvas/nodes/builtin-nodes.tsx:22-40` registers the seven built-ins in `web/src/lib/canvas/node-registry.ts`. Unknown node types fall back to a generic definition rather than failing.

The current connection contract (`web/src/types/canvas.ts:89-93`) contains only `id`, `from`, and `to`; it has no semantic relation type, evidence, revision, or domain validation. Project data includes nodes, connections, chat sessions, active chat, appearance flags, and viewport.

Source inspection confirms these existing canvas behaviors:

- pan/zoom transform with zoom clamped to 0.05–5 (`atelier-canvas.tsx`);
- node selection, marquee selection, drag, resize, context menus, keyboard deletion, undo/redo;
- 180 ms debounced history snapshots capped at 50 states (`project.tsx:358-390`);
- node viewport culling with 280 px padding (`project.tsx:559-573`), but all connections still render (`project.tsx:2823-2850`);
- minimap and viewport navigation;
- image crop, split, mask edit, browser-canvas upscale, and angle generation;
- text, image, video, and audio generation paths, stop controls, generation history, and derived-node connections;
- image “AI super resolution” opens an explicit not-implemented modal (`project.tsx:3012-3014`).

The audit validated route availability and builds, not every interactive behavior. There is no automated regression suite, so the above is source-based evidence and must be protected by later tests before canvas extraction.

## 4. Stores and persistence map

The database is not yet the source of truth. Current business state is split across Zustand, localStorage, localForage/IndexedDB, in-memory Maps, and blob/object URLs.

| State / key | Location | Schema / behavior | Risk |
|---|---|---|---|
| `infinite-canvas:canvas_store` | localForage DB `infinite-canvas`, store `app_state` | Project id/title/timestamps, nodes, connections, chat sessions, UI flags, viewport; writes debounced 400 ms. | Browser storage is current project truth; no revision/transaction/migration contract. |
| `infinite-canvas:generation_history` | same localForage store | Up to 200 generation records. | History is not a durable Job model. |
| `infinite-canvas:asset_store` | same localForage store | Text/image/video asset metadata; resolves stored blobs. | Asset metadata and files can diverge. |
| `infinite-canvas:prompt_library_store` | same localForage store | Prompt library entries. | No schema version. |
| `infinite-canvas:ai_config_store` | Zustand persist / localStorage | Full `AiConfig`, channels, model scripts, and raw `apiKey` values (`use-config-store.ts:22-30,58,175-230`). | Critical: secrets and executable scripts are frontend-readable. |
| `media_files` | localForage DB `infinite-canvas` | Blobs keyed by generated storage key (`file-storage.ts`). | Object URL cache is revoked on deletion only. |
| `image_files` | localForage DB `infinite-canvas` | Image blobs and remote download cache (`image-storage.ts`). | Browser quota and lifecycle limits; remote fetch uses current proxy path. |
| canvas UI preferences | localStorage/Zustand | quick tools, side-panel dimensions/open state, theme/locale and similar UI choices. | Appropriate only as UI state; names need inventory before migration. |
| MONOFORM projects/poses | MONOFORM localStorage | Project data scoped using the iframe `?key=<nodeId>` plus legacy/custom-pose keys. | Separate schema and lifecycle from canvas; no typed host transaction. |
| active generation requests | in-memory `Map` in `project.tsx` | Abort controllers and transient request identity. | Lost on reload/restart. |
| plugin video results | in-memory `Map` in `video.ts` | Completed plugin response cache. | Lost on reload/restart. |

`localforage-storage.ts` falls back to localStorage if IndexedDB/localForage operations fail, which changes capacity and serialization behavior without a durable migration record. Object URLs are process/session projections and must not become persistent identifiers.

## 5. Provider, Secret, polling, and cancellation paths

All current model execution originates in the frontend:

1. UI components resolve an `AiConfig`/channel from `use-config-store.ts`.
2. `services/api/image.ts`, `audio.ts`, and `video.ts` construct OpenAI-compatible or Gemini requests in the browser.
3. During Vite development, `lib/api-proxy.ts` rewrites full URLs to `/api-proxy?target=...`; in production it returns the direct Provider URL.
4. Model-plugin paths execute user-authored JavaScript through `new Function` at `services/api/model-plugin.ts:120`, passing `apiKey`, URLs, prompts, images/messages, HTTP helpers, `fetch`, Axios, polling helpers, and AbortSignal.

No `eval(` occurrence was found in tracked application source, but `new Function` is sufficient to violate the target security contract. Plugin helpers accept absolute HTTP(S) URLs, add bearer credentials by default, and can override headers. Secrets are also accepted from URL query parameters in `client-root-init.ts:20-44` and then persisted to config after the parameters are removed from the displayed URL.

The Vite serve-only proxy in `web/vite.config.ts` accepts an arbitrary `target` URL, forwards nearly all incoming headers, rewrites Host, buffers request bodies without a size limit, follows platform fetch behavior, allows a 300-second request, and returns the raw exception message. It has no scheme/host/IP allowlist, DNS rebinding defense, redirect revalidation, TLS policy boundary, response limit, or log redaction contract. Combined with the package scripts' `0.0.0.0` binding, this is a high-priority WP-02 security boundary.

Cancellation is present for many active browser requests through AbortSignal/AbortController. Video uses up to 120 in-memory poll iterations at 2.5 seconds (`video.ts:37-47`). There is no persisted Job, lease, retry schedule, checkpoint, startup recovery, or read-back verification. Reload marks interrupted UI generation state rather than resuming work. A failed video download may retain a remote Provider URL with an empty storage key and zero bytes (`video.ts:108-117`).

## 6. Backup, import, blobs, and URLs

`services/backup-restore.ts` creates a synchronous in-memory ZIP containing `backup.json` plus media/image blobs. `backup.json` includes the complete `AiConfig`; therefore ordinary backups currently contain raw API keys and model scripts. Import replaces config, projects, and assets after only shallow parsing. Missing media is skipped, and there are no manifest hashes, per-entry/total size limits, schema migration plan, duplicate/conflict policy, path safety layer, or atomic rollback.

`services/config-file.ts` separately exports/imports the entire `AiConfig`, again including raw keys and scripts. `lib/zip.ts` uses `fflate` `zipSync`/`unzipSync`, loading whole archives into memory. Canvas export and asset transfer also bundle blobs client-side with limited validation. No SQLite-consistent backup exists because SQLite is not present.

File and image stores generate internal keys rather than trusting user filenames, which is a useful behavior to retain. However, the target FileStore must add staging, streaming limits, hashes, atomic commit, path/symlink/device protection, and database/file consistency.

## 7. MONOFORM integration

MONOFORM source is copied into `web/monoform-studio` and built separately. `web/scripts/build-monoform.mjs` may run a nested npm install, builds to nested `dist`, then replaces the tracked `web/public/monoform` bundle. The main Vite build does not rebuild it; CI therefore packages whatever prebuilt bundle is committed.

The canvas `DirectorPanel` and standalone `/director` page use an iframe pointing to `monoform/index.html`. Canvas scoping is sent as `?key=<nodeId>`. The iframe allows camera, microphone, clipboard-write, download, and fullscreen, but has no `sandbox` attribute. The host message handler checks only the payload's `source`, `type`, `kind`, and Blob; it does not verify `event.origin`, `event.source`, a nonce, or a versioned schema (`director-panel.tsx:19-28`). MONOFORM posts image/video exports with wildcard target origin (`App.jsx:1966,2046`).

This is an untyped, non-transactional bridge. Exported Blob data can be ingested by the canvas, but project state, persistence, errors, and cancellation are separate. WP-04/WP-09 must preserve capability while replacing the trust boundary deliberately; WP-00 does not alter it.

## 8. Large files and coupling hotspots

| File | Approx. lines / size | Finding |
|---|---:|---|
| `web/src/pages/canvas/project.tsx` | 3,051 / 171 KB | Main God Component: projection, selection, viewport, history, drag/resize, media tools, Provider execution, polling, dialogs, persistence, and rendering. |
| `web/monoform-studio/src/App.jsx` | 2,261 / 131 KB | MONOFORM application orchestration, persistence, capture/export, UI, and bridge. |
| `web/src/styles/globals.css` | 1,403 lines | Broad global styling surface. |
| `web/monoform-studio/src/Viewport.jsx` | 1,336 / 65 KB | 3D viewport and interaction concentration. |
| `web/src/services/api/image.ts` | 946 / 43 KB | Multiple Provider formats and model operations in one browser service. |
| `web/src/components/canvas/canvas-node.tsx` | 914 / 46 KB | Many node presentation/interaction responsibilities. |

These are quantified refactoring inputs, not authorization for WP-00 restructuring.

## 9. Tests, CI, release, and licenses

- No unit, integration, E2E, migration, security, or accessibility test files were found; neither package declares a test command.
- No lint script exists. Prettier check exists but currently fails across 147 files.
- CI performs install, typecheck, and main web build only. Its lock install is expected to encounter the audited main-lock mismatch.
- There is no Wails packaging, signing, installer, SBOM, vulnerability scan, deterministic Mock LLM, or release smoke matrix.
- Root `LICENSE` is MIT, copyright 2026 GuiYi-Xi. No `THIRD_PARTY_NOTICES`, NOTICE, or SBOM exists.
- Toonflow is present only in reference documentation; it is not a code dependency or submodule. Its source, prompts, branding, icons, text, and distinctive implementation must remain excluded under the clean-room rule.

## 10. Prioritized gaps

| Priority | Verified gap | Target package |
|---|---|---|
| P0 | Raw API keys/scripts in frontend persistence and ordinary backups; arbitrary `new Function`; open dev proxy; browser Provider traffic. | WP-02, with foundation in WP-01 |
| P0 | Browser stores are business truth; no SQLite schema/migrations/revisions/FileStore. | WP-01, WP-04 |
| P0 | No durable Job/Workflow/Agent/Memory runtime or restart recovery. | WP-03, WP-07, WP-10 |
| P1 | Main lockfile cannot complete `npm ci`; CI/reproducibility compromised. | WP-01 or narrowly approved maintenance before its implementation |
| P1 | No automated tests/lint; format gate fails; canvas lacks regression protection. | Starts WP-01, expands WP-04/WP-12 |
| P1 | Backup/import is memory-heavy, includes secrets, and lacks validation/atomicity. | WP-02, WP-12 |
| P1 | MONOFORM wildcard messaging and permissive iframe trust boundary. | WP-04/WP-09, security hardening WP-12 |
| P1 | 2.57 MB main JS and 1.28 MB MONOFORM JS chunks; ineffective code splitting. | WP-12 |
| P2 | Explicitly unimplemented super-resolution action and provider-format capability gaps. | Relevant media package, not WP-00 |

## 11. Compatibility guardrails for subsequent work

Before migration or extraction, capture fixtures for existing canvas projects, config (with synthetic keys only), assets/blobs, generation history, prompt library, canvas export, backup ZIP, asset transfer, and MONOFORM project storage. Preserve built-in node rendering, unknown-node fallback, pan/zoom, selection, drag/resize, connections, minimap, undo/redo, media tools, stop behavior, and project reopening. The database must become authoritative without treating canvas projection, Zustand, model text, Provider responses, or Memory as domain truth.
