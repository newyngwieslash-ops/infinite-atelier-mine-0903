# ADR-0009 Domain Event Stream and Approval Semantics

- Status: Accepted (WP-05 scope)
- Date: 2026-09-18
- Deciders: Repository engineering under approved WP-05 plan
- Related work package: WP-05 (Drama domain model and studio UI shell)

## Context

`docs/DOMAIN_MODEL.md` §16 requires every command to implement "event" alongside
its DTO, validation, revision guard, idempotency, authorization and transaction.
§17 fixes the vocabulary — twenty-eight names — and the envelope every event
carries. Neither says where an event is written relative to the change it
describes, which is the question that decides whether the stream can be trusted.

WP-05 also had to finish a job WP-04 left: five of the eight version families §2.5
defines had no way to reach `approved`. The script-version family already had a
switch, written before this package, taking the supersede target as a
caller-supplied id.

## Decision

### 1. Two emission paths, and the difference is whether the event gates the change

- **Transactional.** A command whose event is the governance record of a decision
  builds the event first and passes it into the repository call that performs the
  change, so the change and its record commit together or neither does. A service
  composed without a recorder **refuses** the command. All eight version-family
  approvals are here. An approval nobody can audit is worse than an approval that
  did not happen: the record is what a later review reads to learn who decided
  what, and the schema's own `user_gate_decisions` comment says a user decision
  cannot be forged by an agent.
- **Best effort.** Every other command writes its event after its own write has
  succeeded and carries on if the event cannot be written. The row is already
  committed, so telling the caller the command failed because a log line did not
  land would trade a real success for a cosmetic failure. `RecordBestEffort`
  drops the error deliberately, and its doc comment says so.

The line is per command, not per package: `CreateScriptVersion` announces itself
best-effort while `ApproveScriptVersion` records transactionally, though both
live in the same service.

### 2. One nil-checked emit helper per service

The recorder is a Go interface. A `Service` composed without one holds a nil
interface, and calling a method on it panics. That is not hypothetical: adding
the first emit site made an existing test panic, which is how the problem was
found. So each service has a single `recordEvent` that checks for nil, and no
emit site can forget the check because none of them performs it.

### 3. One supersede implementation for all eight families

`internal/infrastructure/database/version_approval.go` owns the §2.5 switch:
supersede the current approval, then approve the target, and (on the
event-carrying path) write the event, all in one transaction. The order is
load-bearing and the schema proves it: `approveVersionWithEvent`'s supersede step
matched to nothing fails the approval with the partial unique index reporting
"This parent already has an approved version".

The `script_versions` family had its own copy of this switch, predating the
shared helper and differing from it: it took the supersede target as a
caller-supplied id rather than deriving it. Two implementations of one rule can
disagree about the order, so the script repository now delegates to the shared
one. The whole repository has exactly one statement that writes `'superseded'`.

Two consequences, recorded rather than smoothed over:

- A conflict raised by the shared switch is a `versioning.Error`, not a
  `script.Error`. The category a caller acts on is unchanged and the binding maps
  both, so this is a type change rather than a behaviour change.
- The old signature let a caller name a supersede target that was not the current
  approval, and refused it. The shared switch derives the target itself, so
  approving a third version now correctly retires the second. The integration
  test that asserted the old refusal was updated to assert the new behaviour, and
  its approved-row-count assertion still proves the pair is atomic.

### 4. One event name is mapped rather than invented

§17 defines no style-guide event, and the vocabulary is closed (a test pins it to
the specification's list). A guide approval therefore reports
`ProjectSettingsChanged`, the closest name §17 offers, with a payload naming what
changed. §3 groups `ProjectStyleGuide` inside the `ProjectAggregate`, so a
configuration change is a true description rather than a convenience.

The alternative — adding a name to §17's list — was not taken, because §17 is a
specification artifact and a work package should not widen it to suit itself. If
a future package needs a distinct style-guide event, that is a specification
change and belongs in a decision record of its own.

### 5. Where the stream stops

Twenty of the twenty-eight events are emitted by the commands WP-05 owns. The
remaining seven belong to packages that do not exist yet, and `STATUS.md` §0f
names each: `ChapterBoundariesConfirmed` (WP-06's chapter confirmation),
`GenerationJobQueued`/`Succeeded`/`Failed` (the job core), `MemoryCreated`
(WP-10), `UpstreamVersionChanged` (WP-07's impact analyzer — WP-05 emits
`ArtifactMarkedStale`, which is the mark itself rather than the upstream change
that caused it), and `BackupCompleted` (the backup service).

Emitting them from WP-05 would mean inventing the call sites, so the vocabulary
and the table are complete while the emissions are not.

## Consequences

- A consumer reading the stream sees an approval only if it was recorded, because
  an unrecorded approval is refused. It may miss a creation if a database write
  to the event table failed after the row committed; the row is still the
  authority on what exists, which is what §7.1's "数据库是事实来源、Canvas 是投影"
  says of every projection, and the stream is a projection too.
- The event table has no foreign key to its subject and no revision. An event
  outlives what it describes, and a stream that could be rewritten would not be a
  record of what happened. The one reference it keeps is to the project, because
  every drama query is project-scoped and an event with no project could not be
  listed.
- A page that wants the stream filters by project, aggregate or type; the three
  indexes serve those queries. `Limit` is clamped, because a stream read is a
  window and a full export is the backup service's job.
- A command that needs its event to gate the change must use the transactional
  path. A future command that calls `RecordBestEffort` and then acts on the
  result would be a defect, and the two methods' names and doc comments are what
  a reviewer checks against.
