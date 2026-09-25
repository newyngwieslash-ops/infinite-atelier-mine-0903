# ADR-0022 The Summary Ladder's Third Rung, the Defect It Exposed, and the Recall Metrics

Status: Accepted
Date: 2026-09-25
Work package: WP-18 (P3 item 22)
Supersedes: none
Related: ADR-0014 (the memory store, summaries and vectors), AGENT_CONTRACTS §12.3 and §18.1–18.2,
PRD FR-120, DOMAIN_MODEL §14.2/§14.5

## Context

PRD §16's v1.0 list carries 层级摘要和记忆中心, and FR-120's necessary rules state the ladder
explicitly: 「支持层级摘要：message → episode/session → project」. AGENT_CONTRACTS §18.2 lists ten
metrics, two of which are about recall: 「Memory 跨项目泄露率」 and 「Deep Recall 命中率」.

A read-only reconnaissance pass established four facts, each with evidence, and together they defined
the work:

1. **The third rung was declared and unreachable.** `Scope.ProjectOnly()`'s own doc comment says it is
   "how a PROJECT-level summary is scoped", and it had ZERO production callers. The comment above the
   level constants said "There is no third level in this build".
2. **The second rung had never run.** Every `Level:` in the repository was `Level: 1` — six
   occurrences across two files. The middle rung of the ladder had no test of any kind.
3. **The second rung did not mark its children**, contradicting the comment two functions above it
   in the same file. `CreateSummaryWithSources` was called with `markSummarized = level == 1`, while
   `UnsummarisedItems` — the read that chooses a summary's sources — selects on `summarized = 0`.
4. **Neither recall metric existed.** The repository had a well-formed fixture
   (`testdata/canary-drama/memory-recall.json`, with the setting to bury, the question, two decoys
   each carrying the reason it is a decoy, and the message bound) and one integration test that
   asserted properties by hand. Nothing turned an observation into a NUMBER.

## Decision

**1. The second rung's marking is fixed: every rung marks its sources.**
`CreateSummaryWithSources(..., true)`. The `summarized` flag means "a parent covers this" rather than
"a level-one summary covers this", and the parameter's `level == 1` was reading a historical accident
as a rule. Nothing is lost on the recall side, which is what the old comment was protecting: the
semantic channel reads `VectorCandidates`, which filters on the embedding columns and NOT on
`summarized`, so a condensed summary is still searched.

**2. A rung is a COLUMN, not a derivation.** Migration 000025 adds `memory_items.summary_level`.
The rung was derivable from `memory_summary_sources` — a level-two summary's children are episodic,
a level-three summary's children are summaries — and the backfill does derive it, but as a QUERY the
derivation is recursive: telling rung 3 from rung 4 means asking what a summary's children's children
are, and a fourth rung would silently mean "whatever the query happened to match". The column states
the fact once, at the write, where the caller already knows it.

**3. Level three reads and writes the PROJECT scope, and the floor applies to every rung above the
first.** Two children, exactly as level two requires: a summary of one summary is a copy with an extra
hop.

**4. The window names its rung.** `UnsummarisedItems` became `UnsummarisedItemsForLevel`: the store
derives the type from the rung (the message rung is episodic rows; every rung above is summary rows)
and adds `summary_level = level - 1`. This is the fix for a defect that only three rungs expose — see
"Alternatives rejected".

**5. The project rung is reachable, and the widening that achieves it is a FILTERED second search.**
A project-level summary is stored with an empty episode, so `scopeClauses` (which filters on the parts
a scope NAMES) cannot match it from an episode-scoped question. `DeepRecall` therefore runs the
caller's own scope AND, when the caller named an episode, a project-wide search whose results pass
through `keepInRecallScope`. That filter keeps exactly two things: rows in the caller's own scope, and
rows belonging to the PROJECT itself (empty episode AND empty agent). Searching wide and answering
narrowly is deliberate — searching wide is how a memory leaks, and the filter is where the leak is
stopped.

**6. The recall metrics are pure functions over values.** `ScoreRecall` grades one result against one
expectation; `Aggregate` folds scores into `HitRate`, `SummaryHitRate` and `LeakRate`.
`ExpectationsFromFixture` reads the canary fixture's own claims. They are pure because the whole input
to a metric is (what a recall returned) plus (what was expected), and both are already values — a
metric that needed a database and a provider would be a second implementation of recall, grading
itself.

**7. `SummaryHitRate` is a separate rate from `HitRate`,** because 「Deep Recall 命中率」 is about the
history walk: a recall that answered from the recent window has not demonstrated it.

## Consequences

- FR-120's ladder has all three rungs, with the middle one tested for the first time.
- Section 18.2's two recall metrics are answerable. The other eight belong to the packages that own
  those behaviours, and this ADR says so rather than implying ten.
- The memory centre offers a rung picker and shows each summary's rung. Until WP-18 the section sent
  `{ projectId, embed: true }` — always level one — so the two rungs above it could only be reached
  from a test.
- `memory_items.summary_level` is `NOT NULL DEFAULT 0` with `CHECK (summary_level BETWEEN 0 AND 3)`.
  Zero means "not a summary", which is what every other type carries, and `MemoryItem.Validate`
  enforces the agreement in both directions: a summary must state a level, and a non-summary must not.

## Alternatives rejected

**Derive the rung from the source links instead of adding a column.** Rejected as recursive and
fragile: the derivation for rung 3 asks what a summary's children are, and for rung 4 it would ask
what its children's children are. The column is one integer written where the caller already knows the
answer.

**Search the project scope alone and let the scope clause do the filtering.** Rejected: that is a
project-wide vector search handed straight to the caller, which matches every episode and every agent
key in the project. The filter exists precisely because the SEARCH has to be wider than the ANSWER.

**Keep `EpisodeOnly()` in `DeepRecall` and leave the project rung unreachable.** Rejected: it is the
"interface with no real path" shape this repository has recorded five times. A rung that can be
written and never read is worse than no rung, because its rows and links look correct.

**Fold the two recall rates into one.** Rejected: they fail differently and have different owners. A
recall that restored the right message from the recent window is a different defect from one that
walked the history, and a leak is a different defect from both.

**Put the metrics in a new evaluation service.** Rejected: it would need a database and a provider to
re-derive what the recall already computed.

## The defect three rungs exposed, and how it was found

Building the third rung surfaced a defect that TWO rungs could not have: with three, both level two
and level three read summaries, so "the uncondensed summaries in this scope" identified no rung. The
consequence was concrete — once a project summary existed and another episode summary arrived, the
project rung's window held the PREVIOUS PROJECT SUMMARY plus the new child. Two rows, so the floor
was satisfied, and the run would condense its own earlier output as if the two were siblings.

The failure was not reasoned about; it was OBSERVED. `TestTheProjectRungDoesNotCondenseItsOwnOutput`
was written before the fix and its failure message showed a project summary whose text contained a
child project summary. The window now names its rung, and that test asserts the rule in both
directions: one new child creates nothing, two new children produce a summary citing those two and
not its predecessor.

## Verification

- `go test ./... -count=1` — PASS, 57 packages. `npm test` — PASS, 113 tests.
- `sh scripts/verify.sh` — PASS, exit 0 (25 Playwright, security scans over 662 files, all fixture
  checks, SBOM, Wails production build).
- **11 mutations, 11/11 killed**, each restored byte-identically.
- **Three mutations survived the first run and each was a real gap, recorded because the pattern is
  the point**: the leak filter could keep every candidate (nothing asserted its rejection — the
  end-to-end scenario filtered foreign rows by threshold before the filter was consulted, so the
  property was protected by ACCIDENT and the direct test was added); the level guard could accept
  anything (the store refuses the same input, so the test that only asserted "an error came back"
  could not tell the two layers apart, and it now asserts WHICH layer refuses); and the merge could
  keep the first score instead of the better one (nothing exercised a row present in both searches).
