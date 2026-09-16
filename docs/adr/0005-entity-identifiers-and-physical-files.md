# ADR-0005 Entity Identifiers and the Physical File Table Name

- Status: Accepted (WP-04 scope)
- Date: 2026-09-16
- Deciders: Repository engineering under approved WP-04 plan
- Related work package: WP-04 (Legacy project migration and canvas backend adaptation)

## Context

`docs/DOMAIN_MODEL.md` §2.1 states the identifier rule and defers the concrete choice:

```text
- 外部可见 ID 使用 UUIDv7 或 ULID；
- 具体选择在 WP-01 ADR 固化；
- ID 在 Application 层生成；
- 任何实体不得依赖数据库自增 ID 作为跨表业务引用；
- 文件内容使用 SHA-256；
- 幂等键采用命令作用域 + 业务输入哈希。
```

No ADR ever made that choice. `docs/adr/0001` fixes the desktop framework, `0002` the SQLite
driver and migration process, `0003` the secret backend, `0004` the job lifecycle. The only
identifier generator in the repository is the job package's private helper
(`id_generator.go`: `<prefix>-<6-byte hex>-<counter>`), which is neither UUID nor ULID, is not
time-sortable, and was never intended to be an entity-ID standard.

WP-04 is the first package that creates durable user-visible entities (projects, canvas
documents, nodes, edges, assets, asset versions). It must therefore fix the identifier format
before writing the first row, because `docs/DOMAIN_MODEL.md` requires IDs to be generated in the
application layer, exposed externally, and stable across tables.

The second decision in this ADR is a naming collision. `docs/DOMAIN_MODEL.md` §16 defines
`PhysicalFile`:

```text
id
sha256 (unique)
relative_path
size_bytes
mime_type
magic_type
width/height/duration_ms nullable
created_at
verified_at
status ∈ temp|ready|quarantined|missing
```

WP-01 already ships `file_objects` (`000001_foundation.sql`), which holds the same fact for the
same purpose:

```text
hash TEXT PRIMARY KEY          -- SHA-256, CHECK(length(hash)=64)
storage_key TEXT UNIQUE        -- also the hash
mime_type TEXT
size_bytes INTEGER
created_at TEXT
```

`docs/DOMAIN_MODEL.md` was written before WP-01's schema existed. WP-04 must decide whether to
create `physical_files` alongside `file_objects` or to treat them as one table under two names.

## Decision

### 1. External identifiers are UUIDv7

- Format: RFC 9562 UUID version 7 — 48-bit big-endian Unix milliseconds, 4-bit version, 12 bits of
  randomness, 2-bit variant, 62 bits of randomness, rendered as the canonical 36-character
  hyphenated lowercase hex string.
- Generation: `internal/platform/id` generates them with `crypto/rand` and no third-party
  dependency (the UUID is assembled by hand; no library is added to `go.mod`).
- Generation point: the application layer, per `docs/DOMAIN_MODEL.md` §2.1. Repository
  implementations accept the ID; they never mint one.
- Why UUIDv7 over ULID: both satisfy the spec, but UUIDv7 is an IETF standard (RFC 9562), needs no
  new dependency or custom Base32 codec, sorts lexicographically in the same order as the
  canonical string form, and interoperates with the wider tooling ecosystem that already speaks
  UUID (SQLite tooling, future integrations, request tracing). ULID's only advantage here is 26
  characters instead of 36, which does not matter for this product's data volumes.
- Why time-sortable at all: every new table has a `created_at` index or a `(parent, created_at)`
  read pattern. A time-ordered primary key keeps B-tree inserts append-mostly and makes
  "list newest first" cheap without a secondary index scan.
- Uniqueness expectation: 62 random bits per millisecond. The application treats collisions as
  impossible in practice but still relies on the primary key to reject a duplicate rather than
  trusting the generator.

### 2. The job package's existing IDs are not rewritten

`generation_jobs.id`, `job_attempts.id` and the provider audit rows keep their current
prefix-based format. Rewriting them would change already-persisted identifiers for no functional
gain, and `docs/DOMAIN_MODEL.md` does not require retroactive renumbering. The two formats
coexist; the job tables' identifiers stay internal to the job pipeline and are never used as a
cross-table business reference to the new entities (`generation_jobs.project_id` is a plain text
column with no foreign key — see decision 4).

### 3. `file_objects` is the physical file table; `physical_files` is not created

The WP-01 table already stores content-addressed bytes exactly as `PhysicalFile` requires, and
`docs/ARCHITECTURE.md` §21 requires that file paths are reachable only through the FileStore.
Creating a second table for the same fact would produce two truths about one file and force every
reader to know which one is authoritative.

- `docs/DOMAIN_MODEL.md`'s `PhysicalFile` maps onto `file_objects` as follows: `sha256` → `hash`,
  `relative_path` → derived from the hash by the FileStore (never stored, because the store owns
  the layout), `size_bytes` → `size_bytes`, `mime_type` → `mime_type`, `created_at` → `created_at`.
- Fields with no current producer — `magic_type`, `width`, `height`, `duration_ms`, `verified_at`,
  `status` — are **not** added speculatively. WP-04 needs none of them: dimensions already live in
  the canvas node metadata that a migration carries across, and a verification state with no
  writer and no reader would be dead schema.
- If a later package needs those fields it adds them with a forward migration, as `000003` widened
  its CHECK constraints.
- Owner edges stay in `file_references` (`owner_type`, `owner_id`, `file_hash`). WP-04 introduces
  the `asset` owner type; the existing `job` owner type keeps working unchanged.

### 4. `generation_jobs.project_id` gains no foreign key

The column exists from WP-03 with `DEFAULT ''`, and rows already written by the job pipeline can
carry an empty value or a project id from the legacy browser store. Adding a foreign key in
`000004` would either reject those rows or require inventing a project for them, which is exactly
the kind of silent fabrication `docs/DOMAIN_MODEL.md` §20 forbids. The column stays a plain text
scope value for the idempotency key; reconciliation with the new `projects` table is not required
by any acceptance item and is deliberately left alone.

## Consequences

- New entities carry lexicographically sortable identifiers, so `ORDER BY id` and `ORDER BY
  created_at` agree for rows created in the same clock tick.
- `go.mod` gains no dependency: the generator is ~30 lines of `crypto/rand` plus bit layout, and it
  is unit-testable against the RFC's field layout (version nibble, variant bits, timestamp
  round-trip).
- Reviewers must not "unify" the job IDs into UUIDv7 as a refactor: that would rewrite persisted
  identifiers and is out of scope.
- Reviewers must not create `physical_files`: any new physical-file field belongs on
  `file_objects` through a forward migration.
- The `asset` owner type becomes the second real writer to `file_references`; the commit order
  (bytes → `file_objects` → `file_references`) stays mandatory because the FK enforces it.

## Verification

- `internal/platform/id` tests assert: the version and variant nibbles, a 36-character canonical
  form, monotonic ordering for identifiers generated in sequence, uniqueness across a large burst,
  and rejection of no input (the generator takes no caller data).
- `internal/domain/project` and `internal/domain/asset` tests assert IDs are validated, not minted,
  by the domain.
- Database tests assert that a new project row's `id` is exactly the UUIDv7 the application
  supplied, and that a duplicate id is rejected by the primary key.
- The migration test for `000004` asserts the path-free `file_objects` shape is unchanged and that
  no `physical_files` table exists.

## References

- `docs/DOMAIN_MODEL.md` §2.1 (ID), §16 (PhysicalFile), §18 (indexes)
- `docs/ARCHITECTURE.md` §21 (architecture guards), §8.3 (file storage)
- `internal/infrastructure/database/migrations/000001_foundation.sql`
- `docs/adr/0002-sqlite-driver-and-migrations.md`
