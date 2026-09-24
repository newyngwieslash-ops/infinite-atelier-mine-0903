# ADR-0018 Per-Provider Concurrency Limits, and the Rate Limit This Record Does Not Build

- Status: Accepted (WP-14 scope)
- Date: 2026-09-24
- Deciders: Repository engineering under the user's approval of the WP-14 plan
- Related work package: WP-14 (FR-150's per-provider concurrency limit — handoff P0-2)

## Context

`PRD.md:1161` lists 「Provider 并发和速率限制」 among FR-150's capabilities, and `PRD.md:1168`
makes the concurrency half an acceptance criterion in as many words: 「同供应商并发不超过配置上限」.
`docs/SECURITY.md:323` lists 最大并发 in its DoS boundary.

**The data to enforce it already exists; nothing reads it.** `generation_jobs` has carried
`provider_config_id` since migration 000003, and every submission writes it. What did not exist is
any reader that used it for admission control:

- `Service.Start(ctx, workerCount, interval)` launches N identical goroutines (`worker.go:13-40`).
- `dispatch` claims by `ORDER BY priority DESC, created_at ASC` with no provider dimension
  (`worker.go:111-150`), and `ClaimableCandidates`' SQL has none either (`jobs.go:139-177`).
- `provider_configs` has no limit column (migration 000003).

The batch command has a semaphore (`DefaultMaxImageConcurrency = 2`, `batch.go:43`), and it bounds
the wrong thing: how fast jobs are SUBMITTED, not how many EXECUTE. Once submitted, the worker pool
runs them at the pool's size regardless of which provider serves them.

WP-13 made this consequential rather than theoretical: its panel-image batch submits one job per
candidate per shot, so a 12-shot board at 8 candidates is **96 provider calls from one command**.
Against the deterministic mock that is harmless; against a real provider it is the quota exhaustion
FR-150's clause exists to prevent.

## Decision drivers

1. The criterion is about a CONFIGURED limit, so there has to be a place to configure it.
2. A limit must not starve the rest of the queue: one provider at its ceiling must not stop another
   provider's work from running.
3. A restart must not multiply the concurrency. This repository's whole job design exists because an
   application may be force-closed mid-flight (AC-E2E-003), and a limit that lived only in memory
   would be exactly the state that does not survive.
4. The scope must be stated. "并发和速率限制" is two things and the acceptance criterion grades one.

## Options considered

- **A global concurrency setting instead of a per-provider one.** Rejected: 「同供应商」 is the
  criterion's own word. A single global number cannot express "this provider allows two, that one
  allows one", which is what provider documentation states.
- **Enforce in `Claim` by refusing a job whose provider is full.** Rejected: a refusal inside the
  claim loop makes the scheduler spend its passes on jobs it cannot run, and the candidates are read
  in priority order — so one provider's full queue would occupy the head of every batch and starve
  every other provider. Skipping in `dispatch` keeps the other providers moving.
- **In-memory counters incremented on claim and decremented on settle.** Rejected on its own: a
  force-closed process leaves its leases behind, and a counter that starts at zero on restart would
  count those ghosts as available capacity, doubling the real concurrency for one lease TTL. The
  counter still has a role — within one dispatch pass — but it is not the source of truth.
- **A rate limit (tokens per minute) as well.** Rejected for this package: it needs a token bucket
  and per-window state, and the acceptance criterion grades only concurrency. Building it would be
  inventing scope; **not building it is recorded below rather than left implicit**.
- **Enforcing through the provider registry rather than the scheduler.** Rejected: the registry
  resolves an adapter per call and knows nothing about how many calls are in flight, and giving it
  that state would put a queue's policy in an adapter's hands.

## Decision

**Ruling 1 — the limit is a column on `provider_configs`.** `max_concurrency INTEGER NOT NULL
DEFAULT 0`, where **0 means unlimited**. Migration 000022 adds it, and the default is what keeps
every existing configuration behaving exactly as it did. A `CHECK (max_concurrency >= 0)` refuses a
negative.

**Ruling 2 — admission happens in `dispatch`, not in `Claim`.** Each pass reads the in-flight counts
and the limits for the providers its candidates name, then claims only the candidates that fit,
skipping the rest. A job that is skipped is NOT failed and NOT claimed: it stays queued and the next
pass offers it again. This is what keeps a full provider from starving the others.

**Ruling 3 — the count comes from the database, not from memory.** A provider's in-flight jobs are:

```sql
SELECT COUNT(*) FROM generation_jobs
WHERE provider_config_id = ? AND lease_owner <> '' AND lease_expires_at > ?
  AND status IN ('running', 'waiting_remote', 'downloading', 'verifying', 'retry_wait', 'recovering')
```

The `lease_expires_at > ?` clause is Ruling 4, and the lease condition is what makes the count
survive a restart: a force-closed process's jobs keep their status and lease until the lease
expires, and a restarted scheduler counts them exactly as a running one would.

**Ruling 4 — a job abandoned by an expired lease does not count.** Its holder is gone, so it is not
consuming anyone's quota, and `ClaimableCandidates` already treats it as runnable. Counting it would
permanently reduce a provider's throughput after a single crash.

**Ruling 5 — a local counter covers one dispatch pass.** The database snapshot is taken once per
pass, so two candidates for the same provider would both look admissible within it. The dispatch
loop increments its own map on each successful claim, which closes that window. The window THAT
remains open — two processes dispatching at once — is named under Consequences rather than papered
over.

**Ruling 6 — two read-only ports, implemented by `JobRepository`.** `ProviderConcurrency` and
`ActiveProviderCounts`. The scheduler keeps no SQL (AGENTS section 7.2).

**Ruling 7 — the limit is configurable from the interface.** A field on the existing channel form.
An unconfigurable limit is not 「配置上限」.

**Ruling 8 — the RATE limit is NOT built, and that is recorded.** FR-150's clause names both; the
acceptance criterion grades concurrency. A token bucket with per-window state is a different
mechanism with its own failure modes, and inventing it here would put unrequested policy in the
scheduler. It is named as undone.

## Consequences

- **The batch becomes safe against a real provider**, which is the whole point: 96 calls are
  admitted at the provider's own pace instead of all at once.
- **A full provider's jobs wait rather than fail.** A limit set too low degrades throughput; it does
  not stop the queue, because the skipped job keeps its place and its priority.
- **Two scheduler processes on one database can still over-admit by one dispatch pass each.** This
  build is a single-process desktop application (AGENTS section 7.3) and the window is one pass, but
  it is a real window and it is recorded here rather than claimed away. A future multi-process build
  would need the admission and the claim in one transaction.
- **`max_concurrency = 0` is the upgrade path's default**, so this migration changes no existing
  behaviour. A user who wants a limit sets one.
- The rate-limit half of FR-150's clause remains open and is now named in TRACEABILITY rather than
  only in this record.

## Verification

- `TestSchedulerHonoursPerProviderConcurrency` — three providers (limits 1, 2, unlimited) with 3, 4
  and 4 jobs; asserts each stays at or under its limit AND that the unlimited one runs more than
  either, which is what proves the others were not stalled.
- A job whose lease expired does not count (Ruling 4).
- A restarted service still honours the limit (Ruling 3).
- Limit 0 and an empty `provider_config_id` are both unlimited.
- The migration's default, and a round trip of the column.
- Mutations over the admission check, the lease clause and the per-pass counter.
- `sh scripts/verify.sh`.

## References

- `PRD.md:1161,1168` (FR-150), `PRD.md:1260` (the settings list), `docs/SECURITY.md:323`.
- `docs/implementation/project-progress-and-remaining-tasks-2026-09-23.md` — P0-2, this package.
- ADR-0004 (job lifecycle and download policy), ADR-0017 (the batch that made this urgent).
