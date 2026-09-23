# ADR-0016 Encrypted Sensitive Backups Are Not in v1

- Status: Accepted (WP-12 scope)
- Date: 2026-09-23
- Deciders: Repository engineering under the approved WP-12 plan
- Related work package: WP-12 (硬化、性能、打包与 Release Candidate), scope item 6

## Context

ROADMAP item 6 is conditional in as many words: **"加密敏感备份（若 ADR 批准进入 v1）"** — an
encrypted sensitive backup, *if an ADR approves it for v1*. This is that ADR, and it decides not to
build one.

The requirement it would satisfy is `PRD.md:1228-1236` ("加密敏感备份": password-derived key,
authenticated encryption, an explicit inventory of what is sensitive, no stored password, risks shown
before import) and `docs/SECURITY.md:137-146` (its own separate entry point, a strong warning,
authenticated encryption, KDF parameters in the manifest, an error that does not reveal entries, and
plaintext temporaries cleaned up afterwards). `docs/SECURITY.md:144` delegates the parameters
explicitly — "具体 Argon2id/AEAD 参数由 ADR 决定并有测试向量" — so the decision this record makes is
the one the security document asked for, not a decision taken around it.

### What is already true, verified rather than assumed

The case for encryption rests on what an ordinary backup contains, so that was checked in the code
before this record was written.

**An ordinary backup carries no Secret, and that is structural rather than filtered.** Four
independent facts hold it:

- The exporter never reads the credential store. `internal/application/backup/service.go` imports
  only `bytes`, `context`, `crypto/sha256`, `encoding/hex`, `encoding/json`, `fmt`, `os`, `strings`,
  `time`, `apperror` and `archive` — there is no import of `internal/infrastructure/secretstore` and
  no credential read anywhere in the package.
- The provider metadata is a query over non-secret columns only:
  `SELECT id, kind, display_name, base_url, secret_ref, local_approved, enabled FROM provider_configs`
  (`internal/infrastructure/database/backup.go:156-159`). `secret_ref` is the *name* of a credential,
  not its value, and its own comment says so. The serialised payload carries a note saying credential
  values live in the OS store.
- `Manifest.HasSecrets` is written as `false` (`internal/application/backup/service.go:106-108`), and
  the reader treats a `true` value as a refusal rather than as a hint
  (`internal/application/backup/service.go:294-298`: "That is a sensitive backup, which this build
  does not restore").
- The reader scans **every** archive entry for credential shapes and refuses an archive that carries
  one (`scanArchiveForSecretShapes`, `internal/application/backup/service.go:325-354`, over the ten
  shapes at `:309-312`, which include the `Authorization` and `Cookie` names `AC-BACKUP-001` lists).

`TestBackupContainsNoSecrets` (`internal/application/backup/service_test.go:216`) is the criterion's
test, and `:232` proves the scan is **not vacuous** by confirming the same search *finds* a planted
key. `TestRestoreRefusesSecretBearingArchive` (`:241`) covers the other direction — a planted key
fails the restore instead of being imported — and `TestRestoreRefusesCredentialHiddenInAnObject`
(`:575`) covers a credential buried inside a stored object rather than in the database.

**So what would encryption actually protect?** Project *content*: the scripts, storyboards, prompt
text, the database, and every stored media object. Not credentials. That is a real asset and the
distinction is the whole of this decision.

### The starting point for a future implementation, measured

None of it exists, and that was verified rather than assumed:

- There is no sensitive-backup code path anywhere. `grep -rni "sensitive backup"` over `internal/`
  and `web/src/` finds exactly one hit, and it is the *refusal* quoted above. There is no crypto
  import for this purpose, no password prompt, no second entry point.
- The archive is a ZIP (`internal/infrastructure/archive/writer.go:50,80` — `archive/zip`,
  `zip.Deflate`), read through a hardened reader that enforces `MaxEntries: 50_000`,
  `MaxEntryBytes: 4<<30`, `MaxTotalBytes: 20<<30`, `MaxCompressionRatio: 10_000` and
  `MaxPathLength: 512` (`internal/infrastructure/archive/reader.go:79-83`). Encryption would wrap
  this format, not replace it.
- The restore path is fully built and atomic: staging, checksum verification, a promotion that keeps
  what it displaced, a `Rollback`, and a `DiscardPrevious` that is the only irreversible act
  (`internal/application/backup/ports.go:104-176`; `internal/infrastructure/database/backup_promote.go`).
- **No sensitive-backup entry point exists in the UI, and neither does an ordinary one.** That is a
  separate gap recorded below rather than something encryption would fix.

## Decision drivers

1. **A backup exists to be openable.** The failure a backup protects against is losing the data; the
   failure encryption introduces is losing the ability to read the data. For a single-user desktop
   build with no support channel and no key escrow, a forgotten password is unrecoverable.
2. **The ordinary backup is already Secret-free by construction**, so encryption is an *additional*
   protection for content rather than the closure of a credential leak.
3. **Item 6 is conditional, and the condition is a decision rather than a schedule.** It is not a
   release blocker: `PRD.md:1734-1748` lists eleven blockers and this is not one of them, and neither
   is it among `docs/SECURITY.md:706-724`'s fifteen.
4. **The cost is not the cipher.** Argon2id plus an AEAD is a bounded amount of code. The cost is the
   password lifecycle — prompting, confirming, warning, storing nothing, and answering a user who
   has forgotten it — and that lifecycle is a product surface, not a function.

## Options considered

**A. Ship an encrypted sensitive backup in v1.** Rejected. It adds a password a user can lose, on top
of a backup that already cannot leak a credential. The PRD's own acceptance for this area is
"普通备份中搜索不到已配置 API Key" (`PRD.md:1246-1249`), which the ordinary path already meets.

**B. Do nothing and say nothing.** Rejected outright. Item 6 asks a question, and an unrecorded
silence is the failure mode AGENTS section 4.1 forbids — the next reader would find a ROADMAP item
with no answer and either re-decide it or assume it shipped.

**C. Decide against, record what a v1 would need, and name the accepted risk.** **Chosen.**

## Decision

### 1. v1 ships no encrypted sensitive backup, and item 6 is closed as a decision

The ordinary backup is the v1 feature. Item 6's condition is not met, and this record is the reason.
ROADMAP item 6 is **closed by this ADR**, not deferred.

### 2. The reason is stated in one sentence, and it is about the password

A password-protected archive has a password to lose, and a user who forgets it holds a backup they
cannot open — which is worse than the exposure it prevents, *for the case a backup exists for*. That
trade is the entire argument, and it is why the answer does not change with a stronger cipher.

### 3. The cost of NOT doing it is named, and it is accepted for this build

**A backup file on a shared drive is readable by anyone who can read the file.** An ordinary backup
contains the project's scripts, storyboards, prompt text, database and media: all of the user's
creative work, in the clear, inside a ZIP. Anyone with the file can open it with any unzip tool and
read the database with any SQLite tool. There is no obfuscation in the format (`zip.Deflate`
compression only) and none is claimed.

**This is accepted for a single-user desktop build**, on three grounds that are worth stating rather
than leaving to a reader's inference:

- The application stores its data in a per-user private directory (`internal/infrastructure/appdirs/dirs.go:11,32`
  — `os.UserConfigDir()/InfiniteAtelier`, created `0o700`), so the default posture is already
  "readable by this user".
- An attacker who can read a file in the user's own profile can usually read the application's own
  data directory directly, which the backup is a copy of. Encryption of the backup alone would not
  change what that attacker can reach.
- The exposure that is *genuinely* created is the backup leaving the machine — a cloud folder, a USB
  stick, an emailed file. That is a real risk and it is the user's to manage; this record says so
  instead of implying the format handles it.

### 4. What a future v1 would have to decide, listed so the work is not re-discovered

If a later package builds this, each of these is a decision it must make and record, not an
implementation detail:

- **Key derivation.** `docs/SECURITY.md:145` names Argon2id and asks for **test vectors**. The
  parameters have to be chosen for a desktop machine (memory and iteration counts), versioned, and
  pinned by a vector test — otherwise a parameter change silently makes old archives unopenable.
- **AEAD and its framing.** Which AEAD, how the archive streams, and whether the whole file is one
  authenticated message or a chunked sequence. The 4 GiB entry ceiling
  (`api/limits`/`archive.reader.go:80`) rules out "read the whole archive into one message"
  unconditionally.
- **KDF parameters in the manifest.** `docs/SECURITY.md:143` requires it, which means the manifest
  must be readable *before* decryption — a plaintext header, which is itself a decision about what
  that header reveals.
- **A recovery story for a lost password.** The requirement this build has no answer for. Options
  are a printed recovery key, an OS-protected key alongside the password, or an explicit
  "unrecoverable, confirmed twice" warning. Doing nothing is also an option, but it must be a
  recorded one, and the PRD's "密码不保存" (`PRD.md:1235`) rules out storing it outright.
- **Whole archive or selected entries.** `PRD.md:1234` asks for an explicit inventory of what is
  sensitive. Since the ordinary backup holds no Secret, the honest split is by *content class* —
  database and media, say — and the choice changes the format rather than only the UI.
- **The error path.** `docs/SECURITY.md:144` requires a wrong password not to reveal entries, which
  constrains how decryption failures are reported and rules out per-entry errors leaking.
- **Plaintext temporaries.** `docs/SECURITY.md:146` requires cleaning up plaintext temporary data,
  and the current exporter already writes a database snapshot into a private work directory
  (`readDatabaseBytes`, `internal/application/backup/service.go:412-418`). An encrypted variant must
  not leave that behind, which the current path does not.
- **The existing refusal stays.** `Manifest.HasSecrets` already refuses a sensitive archive on
  restore (`internal/application/backup/service.go:294-298`). A future implementation must lift that
  refusal for its own format deliberately, not by accident.

### 5. Nothing in the existing backup path changes

No format version bump, no new field, no new refusal. `SupportedManifestVersion` stays `1`
(`internal/application/backup/ports.go:33`), and the archive layout stays `manifest.json`,
`app.sqlite`, `files/`, `checksums.txt`, `provider.json` (ADR-0006 section 9, `docs/ARCHITECTURE.md`
section 16). A user's existing backups remain readable, which is the point of deciding against rather
than adding a flag.

## Consequences

- **ROADMAP item 6 is closed as "decided against"**, with this record as its evidence. It does not
  appear in the release checklist as outstanding work, because it is not work: it is a decision.
- **`docs/SECURITY.md` section 4.4's "敏感备份" list describes a feature this build does not have.**
  That section is not amended by this ADR — the requirement stands for a build that ships one — but
  the divergence is now recorded rather than implied, and the release checklist names it.
- **No new dependency, no new migration, no new binding method.** The smallest possible footprint
  for a decision, which is the point.
- **The accepted risk is written down in one place.** A reader asking "is my backup file safe on a
  shared drive?" gets section 3 above rather than a search through the code.
- **A future implementation re-reads section 4 first.** It is the list of decisions, not of tasks.
- **One thing this ADR does NOT touch:** the ordinary backup has no user-interface entry point. That
  is a real gap in the shipping build and it is recorded in `docs/implementation/STATUS.md` and named
  in `docs/RELEASE_CHECKLIST.md` as a known-outstanding item. It is not fixed here — this ADR's
  business is the encrypted variant, and a documentation package cannot add a Wails call site
  (AGENTS section 4.1: one package at a time).

## Verification

- `TestBackupContainsNoSecrets` (`internal/application/backup/service_test.go:216`) — the archive
  written from a realistic state contains no credential shape, and the scan is proven
  non-vacuous at `:232`.
- `TestRestoreRefusesSecretBearingArchive` (`:241`) — a planted key fails the restore.
- `TestRestoreRefusesCredentialHiddenInAnObject` (`:575`) — a credential buried in a stored object
  is found, which is what makes "every entry is scanned" a tested claim rather than a comment.
- `TestBackupScanCoversAuthorizationAndCookie` (`:624`) — the two header names `AC-BACKUP-001`
  explicitly lists are in the shape list.
- `TestBackupExportAgainstRealStorage` and `TestBackupRestoreVerifiesAgainstRealDatabase`
  (`internal/infrastructure/database/backup_test.go:35,108`) — the export and restore run against
  the real schema, not a fake.
- `TestManifestRecordsWhatTheReaderNeeds` (`internal/application/backup/service_test.go:532`) — the
  manifest's fields are the ones a reader needs, which is what a future encrypted variant's
  plaintext header would have to preserve.
- The claims about imports and column lists were verified by reading
  `internal/application/backup/service.go` and `internal/infrastructure/database/backup.go:151-200`
  rather than by trusting the package comment that states them.

## References

- `PRD.md:1228-1236` (加密敏感备份), `PRD.md:1246-1256` (验收), `PRD.md:1734-1748` (the eleven
  release blockers, which do not include this).
- `docs/SECURITY.md:133-146` (§4.4 导出, including the delegation of the KDF/AEAD decision and the
  test-vector requirement), `docs/SECURITY.md:84-89` (the OS credential backends), `:706-724` (§19).
- `docs/ARCHITECTURE.md` section 16 (backup/restore layout); `docs/ACCEPTANCE.md:604-622`
  (AC-BACKUP-001, AC-BACKUP-002); `docs/ACCEPTANCE.md:635-652` (AC-SEC-002, AC-SEC-003).
- `docs/ROADMAP.md:573-600` (WP-12 scope; item 6 is the conditional one), `:601-614` (关键验收:
  "普通备份无 Secret").
- ADR-0006 section 9 (ordinary backup v1, the ZIP layout, and "Secrets are structurally absent");
  ADR-0003 (Windows Credential Manager as the secret backend); ADR-0002 (SQLite driver and
  migrations).
- AGENTS.md §6 (licence and clean-room rules — no new third-party crypto dependency is taken by this
  decision), §13 (documentation duties), §14 (how an unresolved question is handled: decide the
  safe, reversible, minimal option and record it).
