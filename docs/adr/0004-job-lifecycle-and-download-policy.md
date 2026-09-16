# ADR-0004 Job Lifecycle Names, Recovery Semantics, and Outbound Download Policy

- Status: Accepted (WP-03 scope)
- Date: 2026-09-15
- Deciders: Repository engineering under approved WP-03 plan
- Related work package: WP-03 (Persistent Job Manager and multimodal providers)

## Context

The specification set describes the same job lifecycle with different vocabularies, and
`docs/implementation/handoff-2026-09-08-181352+0800.md` (section "需要在对应包开始前记录的规格差异")
explicitly requires WP-03 to resolve them rather than infer behaviour from prose:

| Concept | PRD.md | DOMAIN_MODEL.md | ARCHITECTURE.md |
|---|---|---|---|
| Waiting on a remote provider job | `awaiting_remote` (§12.3, line 1502) | `waiting_remote` (§12.1, line 1137) | `waiting_remote` (§14.2, line 875) |
| Verifying a downloaded result | `verifying` (§12.3, line 1504) | absent | absent |
| Restart recovery state | `recovering` (FR-150 line 1170, §12.3 line 1511) | absent | absent |
| Remote-URL-only result | `remote_only` (FR-080 line 706) | absent | absent |
| Unrecoverable job | absent | `orphaned` (line 1143) | `orphaned` (§14.2, line 877) |
| Retry backoff | `retry_wait` (§12.3 line 1509) | `retry_wait` (line 1139) | — |
| Stage execution (StageRun) | `execution_succeeded` (§12.1 line 805) | `executed` (line 1032) | `executed` (§10.2 line 643) |

Two further gaps: DOMAIN_MODEL §12 invariant "idempotency_key 在作用域内唯一" does not fix what
"scope" is, and PRD FR-080's `remote_only` has no home in the DOMAIN_MODEL enum even though it is
an accepted terminal outcome for a successfully fetched-but-not-downloaded media result.

A second decision is needed because WP-03 acquires the ability to fetch **provider-supplied URLs**:
until now every outbound request went to a user-configured Provider base URL, where the policy can
pin one exact host/port. Image results may be returned as a URL on a different host (CDN, signed
storage). That URL is untrusted input from a remote service, so the existing "exact matching host"
policy cannot be reused for it.

## Decision

### 1. Vocabulary (behaviour per PRD, field names per DOMAIN_MODEL)

The persisted `status` column is a superset that keeps both the DOMAIN_MODEL names and the PRD
states that have real behaviour. The authoritative set is:

```text
queued          -> accepted, not yet started
running         -> a worker holds the lease and is executing (local work, including submit)
waiting_remote  -> a remote provider job was accepted; polling for completion
downloading     -> a remote result URL is being fetched into the FileStore
verifying       -> bytes are present; MIME/size/hash validation in progress
retry_wait      -> failed with retries remaining; waiting until next_retry_at
succeeded       -> result committed and verified (terminal)
remote_only     -> remote result acknowledged but deliberately not downloaded (terminal)
failed          -> no retries remain, or a non-retryable failure (terminal)
cancelled       -> user cancelled; remote side may still be running (terminal)
orphaned        -> state could not be recovered after restart (terminal)
recovering      -> restart marked the job for the recovery scanner (non-terminal, transient)
```

Naming rules that follow from this:

- `awaiting_remote` in PRD prose is the same state as `waiting_remote`; the database and code use
  `waiting_remote` (DOMAIN_MODEL spelling) and no separate `awaiting_remote` value exists.
- `recovering` is a persisted value with no writer in the current implementation: the scanner reads
  it (so a previous interrupted scan is recognised) and writes the disposition directly. It exists so
  that a future scan which must mark progress before deciding can do so without a schema change; no
  job rests there today.
- `orphaned` exists in addition to PRD's list; it is the DOMAIN_MODEL/ARCHITECTURE outcome for a job
  that could not be recovered. `failed` stays reserved for failures with diagnostics.
- `remote_only` is a terminal success-with-caveat: the provider returned a usable remote URL and no
  bytes were committed locally (because the adapter reported nothing fetchable). It is never reported
  as a normal `succeeded` result.
- `downloading` is written by the worker when the runner reports that provider work finished but the
  bytes are still to be fetched. The persisted marker (`pending_download`) is what lets the next
  attempt resume the fetch instead of calling the provider again.
- `executed` vs `execution_succeeded` is a StageRun concern owned by WP-07, not a Job status; WP-03
  does not introduce either.

### 1b. Attempt accounting and poll pacing

Two rules keep a long-running remote job honest:

- **A poll is not an attempt.** The pass that *submits* remote work is an attempt (it caused a
  provider side effect and is recorded in `job_attempts`), and the pass that *fetches* the finished
  artifact is an attempt again. Each intervening poll that finds the job unfinished only reads
  progress, so it neither advances `attempt_count` nor writes an attempt row. Without this a video
  job needing hundreds of polls would exhaust a three-attempt budget while behaving exactly as
  designed, and the Job Center would report attempt numbers that never existed.
- **A parked remote job is paced through `next_retry_at`**, the same column `retry_wait` uses. Both
  the scheduler's `waiting_remote` candidate query and the `Claim` guard require that timestamp to
  have elapsed, so the poll cadence is set by `Service.RemotePollInterval` (5 s in production,
  `DefaultRemotePollInterval` when unset) and not by the 250 ms scheduler pass. A poll that *fails*
  is real work: it is counted, retried, and reported like any other failure.

### 2. Idempotency scope

`idempotency_key` is unique per `(project_id, idempotency_key)`; for jobs with no project the
project column stores the empty string and uniqueness therefore applies globally. The key is derived
as `command-scope + business-input-hash` per DOMAIN_MODEL §2.3, computed by the caller (application
service) rather than the database.

### 3. Provider-supplied result URLs (untrusted egress)

Two distinct egress policies exist and MUST NOT be conflated:

| Policy | Applies to | Rule |
|---|---|---|
| `Policy` (WP-02, unchanged) | Provider **API** calls: the base URL the user configured | Exact host + port match, DNS pinned to validated IPs, env proxy disabled, TLS verified |
| `DownloadPolicy` (new) | Provider **result** URLs discovered in responses | https only; never a local/private/link-local/metadata address for `any` resolved IP; every redirect hop re-validated; per-download byte cap; total deadline; env proxy disabled; TLS verified |

`DownloadPolicy` deliberately does not require host equality — the value comes from a provider we
already trust for this call — but it must refuse every private/link-local/metadata destination,
because otherwise a compromised or malicious provider response could turn the desktop app into an
SSRF pivot into the user's own network. Local-Provider approval does **not** extend to downloads:
an `AllowLocal` provider may be reached over http, but its returned result URLs still have to be
public https, or the result is reported as `remote_only`/failed rather than fetched.

### 4. Failure and result rules (restated as test obligations)

- A failed or cancelled job never produces a usable asset: no `succeeded`, no committed file
  reference presented as a result.
- `succeeded` requires a committed file reference that was verified (MIME allowlist + size cap).
- `cancelled` does not claim the remote job stopped; if a remote job existed it is recorded as an
  orphan candidate for the user to inspect.
- On restart, a job with a `remote_job_id` is polled, never re-submitted; only jobs with no remote id
  are re-queued.

## Consequences

- The enum is wider than either source document alone; the schema, the Go domain type, and the tests
  all use this one list, and a migration-level CHECK enforces it.
- `verifying` has no writer in WP-03: the result pipeline decides the content type from the bytes
  during the commit, so the state is reserved for a future multi-step validation and the UI must
  tolerate it without relying on it appearing. `recovering` is also currently read-only: the
  scanner writes the disposition directly, and the state exists so that a scan interrupted part-way
  can be recognised on the next start. Neither state should grow a UI action other than cancel.
- Reviewers must not "simplify" the extra states away: each exists because a PRD acceptance item
  or an ARCHITECTURE recovery rule requires it.
- Adding a future state requires a new forward migration to widen the CHECK constraint (SQLite
  cannot alter a CHECK in place).

## Verification

- `internal/domain/job` unit tests enumerate the status set, assert terminal states reject further
  transitions, and assert `awaiting_remote` is not an accepted value.
- `internal/infrastructure/database` migration tests assert the CHECK accepts every documented value
  and rejects an unknown one, and that a v2 database with existing provider rows upgrades to v3
  without data loss.
- `internal/infrastructure/providerhttp` tests assert `DownloadPolicy` refuses loopback, private,
  link-local, metadata, and non-https destinations, and that redirects to those are refused too.
- Job recovery tests assert `running` → re-queue, `waiting_remote` → poll without re-submit, and
  unrecoverable → `orphaned`.
- Poll rules are pinned twice, at both layers they live in:
  `TestJobRepositoryParkedJobIsPacedByRetryWindow` proves the SQL candidate query *and* the claim
  guard refuse a parked job before its interval elapses (and accept it after);
  `TestRemotePollIsPaced` proves the running scheduler issues at most one poll per interval, and
  `TestParkedPollDoesNotConsumeAttemptBudget` / `TestRemotePollFailureStillRetries` prove a pure poll
  leaves `attempt_count` and `job_attempts` untouched while a failing poll is retried as real work.

## References

- `PRD.md` FR-080, FR-140, FR-150, §12.3
- `docs/DOMAIN_MODEL.md` §12, §18 indexes
- `docs/ARCHITECTURE.md` §13.1, §14
- `docs/ACCEPTANCE.md` AC-FOUND-006, AC-MEDIA-001
- `docs/SECURITY.md` §6 (HTTP limits), §9 (archive/download limits)
- `handoff-2026-09-08-181352+0800.md` (state-name discrepancy note)
