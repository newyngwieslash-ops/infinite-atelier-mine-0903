# ADR-0006 Legacy Import Pipeline, Idempotency, and Ordinary Backup v1

- Status: Accepted (WP-04 scope)
- Date: 2026-09-16
- Deciders: Repository engineering under approved WP-04 plan
- Related work package: WP-04 (Legacy project migration and canvas backend adaptation)

## Context

`docs/ARCHITECTURE.md` §8.4 fixes the shape of the migration:

```text
Scan legacy IndexedDB/localForage
→ Export legacy snapshot
→ Transform to import manifest
→ Validate counts and hashes
→ Import into Go Core transactionally
→ Compare node/edge/media counts
→ Mark migrated
→ Keep legacy data read-only until user confirms
```

with the rule `迁移必须幂等。失败时可重新执行，不覆盖已成功迁移数据。`

`docs/DOMAIN_MODEL.md` §20.1 adds the mapping table and the retention rule:

```text
legacy_project_id -> project_id
legacy_node_id -> canvas_node_id
legacy_asset_id -> asset_id/asset_version_id
legacy_media_key -> physical_file_id
legacy_generation_history_id -> provider_request/job
```

`无法映射的 metadata 原样放入 legacy_metadata_json，并写迁移告警；不得静默丢弃。`

Three facts about the starting point decide most of this ADR:

1. **The Go core cannot read the legacy data.** It lives in the webview's IndexedDB (`infinite-canvas/app_state`, `image_files`, `media_files`) and in `localStorage`. The frontend must hand it over. Binary blobs cannot cross a Wails binding except as base64, and the legacy blob stores legitimately hold videos up to hundreds of megabytes, so a whole-file transfer would materialise the entire payload several times in memory.
2. **Legacy media keys are not content hashes.** They are `${prefix}:${nanoid()}` (`image:`, `video:`, `audio:`, `director:`, `file:`), so the same bytes may exist under several keys and the same key may be referenced by a node, an asset, a chat message and the generation history at once.
3. **A project id in the legacy store is not a project id in the new schema.** The legacy canvas store assigns `nanoid()` ids and its import path mints a new id on every call, so nothing today is idempotent.

`docs/ACCEPTANCE.md` §7 fixes the three migration criteria this ADR must satisfy (AC-LEGACY-001 completeness, AC-LEGACY-002 idempotency, AC-LEGACY-003 rollback), and §13 plus `docs/ARCHITECTURE.md` §16 fix the backup shape.

## Decision

### 1. The transfer format is a versioned legacy snapshot plus chunked media

The frontend produces a **legacy snapshot** (`manifest.json`) and streams each media blob separately.

```json
{
  "app": "infinite-canvas",
  "manifestVersion": 1,
  "exportedAt": "RFC3339",
  "projects": ["<CanvasProject, verbatim from the canvas store>"],
  "assets": ["<Asset, verbatim from the asset store>"],
  "generationHistory": ["<GenerationHistoryRecord, verbatim>"],
  "promptLibrary": {"builtInCovers": {}},
  "uiPreferences": {},
  "media": [
    {"legacyKey": "image:abc", "mimeType": "image/png", "bytes": 1234, "sha256": "optional"}
  ],
  "unsupported": {}
}
```

- The arrays carry the **legacy rows verbatim**. The frontend does not normalise, rename, or drop fields: transformation is a Go concern, so the same input always produces the same output and the legacy shapes stay auditable.
- `manifestVersion` versions this envelope. An unknown version is refused with a stable error before anything is written.
- Go never trusts the envelope's own counts: it recomputes every count and hash it needs, and compares (`AC-LEGACY-001`).
- `media` is a manifest of what should arrive, not proof that it did. A blob listed but never uploaded makes the import report a missing-file warning rather than silently producing a project with broken nodes.

### 2. Media crosses the boundary in bounded chunks

The frontend uploads each blob through:

```text
BeginLegacyFile(legacyKey, mimeType, declaredBytes) -> uploadID
AppendLegacyFile(uploadID, base64Chunk)             -> appended bytes
FinishLegacyFile(uploadID, sha256)                  -> stored: bool
AbortLegacyFile(uploadID)                           -> cleaned up
```

- **Chunk size is bounded** (4 MiB decoded) so one binding call cannot allocate an unbounded buffer, and the binding enforces it plus a per-file ceiling and a cap on concurrent transfers. A per-session byte total is **not** enforced.
- Go writes the chunk as it arrives: each chunk is decoded, bounds-checked against a per-file ceiling, and handed to the FileStore, which streams it through SHA-256 and commits it content-addressed with an atomic rename. `Finish` verifies the declared size and the optional declared hash before the object is linked to anything. No temporary file survives a failed transfer: an aborted or over-limit upload is discarded in place.
- This is why no local HTTP server is introduced: an HTTP surface would be a new network attack surface for a desktop app that otherwise makes no inbound connections. `docs/SECURITY.md` §11 requires all file paths to go through the FileStore, and this path does.
- An interrupted upload leaves nothing behind: `Abort` discards it, and the over-limit path discards it too. `AC-LEGACY-003` is tested against exactly this window.

### 3. Import is one transaction over metadata, with files committed first

Order of operations, each stage named in the error report so `AC-LEGACY-003` can point at the failing stage:

```text
snapshot      -> parse and validate the envelope, refuse unknown versions
precheck      -> recompute counts, reject oversize/malformed input
files         -> commit every media blob into the FileStore (outside the DB transaction)
transform     -> build domain rows; unknown fields and node types go to legacy_metadata_json
db            -> one transaction: workspaces, projects, canvas docs/nodes/edges/chats,
                 assets/versions/files, history, id map, import record
verify        -> re-read inside the transaction and compare counts and hashes
report        -> persist the report and warnings
```

- **Files first, metadata second.** If the DB transaction fails, the worst outcome is unreferenced objects in the content-addressed store — garbage, never a broken reference, because `file_references.file_hash` has a foreign key to `file_objects` and the reference row is only written inside the transaction. The reverse order would let a project reference bytes that do not exist.
- **The whole DB write is one transaction**, so a failure cannot leave half a project. `AC-LEGACY-003`. Verification re-reads the row counts inside the transaction; the media hashes are verified when the bytes are committed, before the transaction opens, because a content-addressed store returns the hash of what it actually stored.
- The database runs with `MaxOpenConns=1`, so an import transaction holds the only connection for its duration and job workers wait for it. The import writes a bounded number of rows per project and does not perform network I/O inside the transaction, so the wait is short; a scheduler pause was considered and not implemented, because pausing would require reaching across the job service and the import is not long enough to justify it. This is a deliberate, recorded choice rather than an oversight.
- Counts and hashes are compared **inside** the transaction by re-reading the rows just written, so the verification is of persisted state, not of the in-memory plan.
- The pre-import snapshot that `PRD.md` §NFR-002 asks for ("任何迁移先备份") is **not** taken by this path: the migration runner snapshots before a *schema* migration, and a data import applies no schema change. A user who wants a copy first can export an ordinary backup, which is the same mechanism the backup binding exposes. This is recorded as a gap against NFR-002 rather than claimed as met.

### 4. Idempotency is a fingerprint, and the default is "do not import again"

Each project's fingerprint is the legacy project id plus the counts and media hashes that identify its content:

```text
fingerprint = sha256(legacy_project_id | node count | edge count | sorted media hashes)
```

- A completed import whose fingerprint matches is reported as `already_imported` and **writes nothing**. `AC-LEGACY-002`'s "第二次检测已导入；默认不覆盖".
- The user may explicitly choose **copy mode**, which imports again with fresh identifiers for every entity (including a new project id) and records the mapping as a separate import. That is the criterion's "用户可选择新副本".
- Overwrite mode **does not exist** in WP-04. Overwriting would mean deleting rows a user may have since edited; `AC-LEGACY-002` says "默认不覆盖", and offering a destructive path without an impact analysis contradicts `docs/ARCHITECTURE.md` §9.1. If it is ever needed it arrives with impact checking, not here.
- A fingerprint is only recorded as completed when the whole import succeeded. A failed import leaves no fingerprint, so a retry is a retry, not a duplicate.

### 5. Legacy identifiers map through a dedicated table

`legacy_id_map(kind, legacy_id, new_id, import_id)` covers exactly the five mappings of §20.1:

```text
project, node, asset, media, generation_history
```

- The table is read-only after import and is what makes a second import recognise its own work.
- `legacy_media_key -> physical_file_id` uses the FileStore hash as `new_id`, because content addressing means two legacy keys can legitimately map to one stored object. That is the desired outcome (`AC-LEGACY-002`'s "不重复媒体"), not a collapse to be avoided.

### 6. Unsupported fields are retained, never dropped

Fields the new schema does not model are written into `legacy_metadata_json` on the entity, and each occurrence produces a warning in the import report. This includes:

- unknown/plugin `node_type` values (the legacy type is an open string),
- unknown keys inside node `metadata` and inside connection rows,
- prompt-library covers and UI preferences (they belong to browser-scoped state that WP-04 does not move into the domain),
- MONOFORM iframe state, which lives in a separate application's `localStorage` keyed by canvas node id and is **not imported**: it is recorded as a warning naming the node, so a user can see it was found and left alone rather than lost silently.

`docs/DOMAIN_MODEL.md` §20.1 forbids silent loss, and `PRD.md` §18 lists "旧项目迁移存在静默丢失" as a release blocker, so the report is part of the deliverable, not a nicety.

### 7. Generation history does not become provider audit rows

`docs/DOMAIN_MODEL.md` §20.1 maps `legacy_generation_history_id -> provider_request/job`. That mapping is **not** followed literally: `provider_requests` and `generation_jobs` are audit and execution records, and the legacy history is a UI convenience list of prompts with image counts and no call metadata. Writing fabricated audit rows would corrupt the audit surface that `docs/SECURITY.md` §10 relies on.

Instead the history is imported into its own table (`generation_history`) with the legacy id preserved in the mapping table, and the deviation is recorded here and in the import report. The acceptance items name history conversion, not audit fidelity, so nothing in `AC-LEGACY-*` is weakened by this.

### 8. Legacy data stays where it is; WP-04 never deletes it

ROADMAP item 12 requires "旧数据只读保留策略". The import therefore reads the legacy stores and writes nothing back to them:

- no localForage key is modified or removed by the migration,
- the legacy plaintext API keys are **not** copied into any new store by this package (they stay a WP-12 concern, as recorded in `STATUS.md`),
- once a project is imported and the user confirms, the frontend stops offering it as an import source and marks it migrated in its own display state — but the underlying browser data is untouched,
- deleting legacy data is WP-12's job after the whole regression suite passes.

### 9. Ordinary backup v1 is a Go-authored archive with a manifest and checksums

`docs/ARCHITECTURE.md` §16 fixes the layout:

```text
manifest.json      -- schema version, app version, created_at, counts, db snapshot name
app.sqlite         -- checkpointed database snapshot taken via VACUUM INTO
files/             -- content-addressed objects referenced by the snapshot
checksums.txt      -- sha256 per entry, including the database
provider.json      -- provider metadata without secret material
```

- Export takes the database snapshot first (`wal_checkpoint(TRUNCATE)` then `VACUUM INTO` through the existing snapshot helper), then copies exactly the objects the snapshot references. A referenced object that is missing from disk fails the export rather than producing an archive that restores into broken references.
- **Secrets are structurally absent.** `secret_references` stores no value, and the export never reads the OS credential store, so a key cannot be in the archive. `AC-BACKUP-001`'s scan is therefore a check that the mechanism holds rather than a filter that could miss a case, and it reads every entry of the archive — the manifest, the database, the provider metadata and each stored object — looking for credential shapes including the `Authorization` and `Cookie` names the criterion lists.
- Restore validates the manifest schema, every path (no traversal, no absolute paths, no device names, no symlinks, no duplicate normalised paths), every entry size, the total size and the compression ratio, and every checksum **before** touching live data; the extraction happens in a temporary directory and the swap is atomic. `AC-BACKUP-002` (atomicity) is WP-12's full criterion, but the archive reader it needs is built here and the WP-04 acceptance is the basic round trip plus the secret scan.
- The archive is a ZIP read and written through the hardened reader/writer in `internal/infrastructure/archive`, which enforces the limits of `docs/SECURITY.md` §9 and is tested against the corpus in §18 (`../`, absolute path, device path, symlink, duplicate normalised path, entry count, compression ratio, false manifest, wrong hash, corrupt SQLite).

## Consequences

- A migration is re-runnable by construction: fingerprint first, fresh ids only in copy mode, files content-addressed.
- The Go side owns all transformation logic, so the frontend's migration code is a thin extractor that can be unit-tested on its own and cannot silently "help" by dropping fields.
- The import cannot be cancelled mid-transaction; it can be abandoned between blobs, which cleans up its temporary files. A cancelled mid-transaction import would need to abort the transaction, and the acceptance items ask for atomic failure, not cancellation.
- Reviewers must not add an "overwrite" mode without impact analysis, must not move history into the audit tables, and must not write to the legacy stores.
- The backup format is versioned from its first release (`manifestVersion`), because `PRD.md` FR-170 requires old versions to have migration tests.

## Verification

- `testdata/old-projects/{minimal,all-node-types,missing-media,malformed-metadata}` are synthetic fixtures (declared in `testdata/LICENSES.md`) that exercise the four cases; the importer tests run them through the real service against a temporary database and a temporary FileStore.
- `AC-LEGACY-001`: counts of projects/nodes/edges, text equality, media hash equality, viewport round-trip, and the presence of a warning plus retained metadata for the malformed fixture.
- `AC-LEGACY-002`: importing `all-node-types` twice reports the second as already imported, stores no second copy, and copy mode produces a distinct project with a distinct id while the stored bytes stay deduplicated.
- `AC-LEGACY-003`: an injected failure (a file whose bytes do not match its declared size, and a forced database error) leaves no project and no canvas rows, removes the temporary files, and reports the failing stage.
- Backup: a round trip through the real archive writer and reader, plus a scan of every archive entry for the synthetic key patterns named in `docs/SECURITY.md` §18.
- Archive limits: the `docs/SECURITY.md` §18 corpus, each case asserted to be refused with the documented error code.

## References

- `docs/ARCHITECTURE.md` §8.4 (migration), §9.1 (canvas source of truth), §16 (backup/restore), §20 phase C
- `docs/DOMAIN_MODEL.md` §20.1/§20.2, §12 (canvas), §16 (files)
- `docs/SECURITY.md` §8, §9, §10, §11, §17, §18
- `docs/ACCEPTANCE.md` §7 (AC-LEGACY-001..003), §12 (AC-CANVAS-004), §13 (AC-BACKUP-001/002, AC-SEC-002/003)
- `PRD.md` FR-001, FR-130, FR-160, FR-170, §18
- `docs/adr/0004-job-lifecycle-and-download-policy.md` (job/project id relationship)
- `docs/adr/0005-entity-identifiers-and-physical-files.md`
