# ADR-0014 Persistent Memory: the Store, the Summary Chain, the Vector Index, the Embedding Port, and the Deterministic Checks

- Status: Accepted (WP-10 scope)
- Date: 2026-09-23
- Deciders: Repository engineering under the approved WP-10 plan
- Related work package: WP-10 (Persistent Memory、Consistency 与 Quality Center)

## Context

WP-10 had to build the persistent memory DOMAIN_MODEL section 14 describes, the recall of
AGENT_CONTRACTS section 12, the embedding capability PRD FR-120 requires to be replaceable,
the deterministic half of AGENT_CONTRACTS section 11.4's review, and the Quality Center —
over `docs/ROADMAP.md:495-533` (seventeen scope items), AC-MEM-001 through AC-MEM-005 in
`docs/ACCEPTANCE.md:516-560`, AC-E2E-004 and AC-E2E-005 in `PRD.md:1788-1809`, FR-120 and
FR-110, and ARCHITECTURE section 12.

Eleven places in that material were silent, disagreed with itself, or disagreed with what
WP-05 to WP-09 had shipped. The user was asked to choose between the alternatives for three
of them and answered: a REAL PROVIDER ADAPTER PLUS A DETERMINISTIC FAKE for embeddings, a
DETERMINISTIC EXTRACTIVE SUMMARISER rather than a model-driven summary agent, and the
deterministic checks RUNNING BEFORE the LLM supervisor and MERGING into one report. The rest
were implemented on the recorded recommendation, so each ruling below is **refutable**: a
later package that disagrees should supersede this record rather than work around it.

## Decision

### 1. `memory_items` is the store, and `agent_messages` stays the runtime's transcript

The memory aggregate is a new table (migration 000019). It is deliberately NOT a set of
columns on `agent_messages`, and it does not reuse that table's rows.

**Ruling.** The two have different lifecycles and different owners. An `agent_messages` row
is the agent RUNTIME's record of a turn: it is cascade-deleted with the run that produced it
(migration 000015), and nothing outside the runtime writes one. A memory is the USER's
record: DOMAIN_MODEL section 14.5 gives them rights over it that a transcript row cannot
have — to pin it so no agent may change it, to edit it, to delete it, to rebuild its vector.
Storing `locked` and `embedding_blob` on the transcript would make "delete this memory" a
write to the runtime's own record, and the two would then have to agree about a lifecycle
they do not share.

**Cost, stated.** One copy of each episodic turn's text, and a citation to reconcile. The
copy is why an episodic memory survives the run it came from, which is the point of a
PERSISTENT memory rather than a window over a log.

**What keeps them from drifting.** The runtime writes both in the same call, and the memory
cites the transcript through section 14.1's `source_type`/`source_id`, so a reader can always
walk from a recalled memory to the message behind it. `TestTheRuntimeRecallsBeforeItWrites-
AndRemembersBothTurns` drives a real run and asserts the order and the citation.

### 2. A summary's sources are MESSAGES, and the hierarchy is an entity link

`memory_summary_sources` joins a summary to the memories it covered; a level-two summary
records its children through `memory_entity_links` with `relation_type = 'summarizes'`.

**Ruling.** AC-MEM-004's own words are "Summary 关联源消息表", and the episodic memory IS a
message's memory-side twin — so one hop reaches the message and the message carries the role,
the agent and the time. A second, summary-to-summary table for the hierarchy would be a
second place for "which summaries does this cover" to be stated; section 14.3's entity link
already expresses a relation between a memory and an entity, and a summary is one.

**Cost, stated.** Two hops for a hierarchy question rather than one, and the reader has to
know that `summarizes` links run child-to-parent while `source_order` runs parent-to-child.

### 3. The Recent channel keeps reading the transcript

`Store.RecentMessages` over `agent_messages` is unchanged, and `BuildMemoryContext` ALSO reads
the store's own recent unsummarised episodic rows.

**Ruling.** The transcript is what the runtime definitely wrote and what the WP-07 port's six
tests cover, including AC-MEM-001's core. The store's window is what section 12.2 calls "Recent
未摘要消息", and its ABSENCE made the store unreachable for the ordinary case: a memory that is
not pinned, not summarised and not embedded appeared in no other channel, so a project with no
embedding provider — which is every project until somebody configures one — could store a
conversation and recall none of it. The two are deduped by message identifier, so a prompt
never carries the same sentence twice.

**Cost, stated.** Two reads on the recall path, and a dedupe that depends on the citation
being present. An episodic memory always cites one, which the domain's validation enforces.

### 4. Embedding is a capability with two adapters and no third

`CapabilityEmbedding` joins the provider vocabulary, and two adapters implement the port: an
OpenAI-compatible `/v1/embeddings` client and a deterministic feature-hash adapter.

**Ruling.** FR-120 requires the provider to be REPLACEABLE and forbids uploading project text
without authorisation; ROADMAP item 17 asks for an ADR that may accept "Provider/Fake". A port
with one implementation cannot demonstrate replaceability, and a feature-hash adapter is not
only a test double — it is FR-120's KEYWORD FALLBACK, computing vectors on the user's own
machine with nothing leaving the process.

**Cost, stated, and this is the one to read.** The deterministic adapter is registered by
`Registry.WithMockEmbeddingAdapter` and `IsUserConfigurableKind` refuses its kind, so **no
provider configuration can carry it**: a composed build embeds only through a provider the
user configured, and `embedder.go` says so in full. What serves the keyword fallback in a real
deployment is a local provider reached through the `openai_compatible` arm. The offline path
for CI is the mock, which a harness registers the way the batch tests register the image mock.
The first version of this code CLAIMED the fallback was reachable and it was not; an
independent review found the contradiction.

### 5. The vector index is an exact scan, and the scope filter is SQL

`VectorIndex`'s four methods are ARCHITECTURE section 12.3's. The implementation stores
little-endian float32 in a BLOB and scores with a normalised dot product in Go.

**Ruling.** ADR-0002 section 68 denies dynamic extension loading, so `sqlite-vec` needs its own
compatibility and security evidence that persistent memory does not justify; section 12.3's
MVP is exactly this, and its V1 names the adapter it would replace. The SCOPE FILTER runs in
the query rather than after it, because section 12.2 requires it before scoring and a filter
in Go would be a leak waiting for a mistake.

**Cost, stated.** A search is O(candidates) rather than O(log n), bounded by
`MaxCandidates`. At a desktop project's scale that is a scan of a few hundred rows.

### 6. Summaries are extractive and deterministic, with no model call

`Summarize` renders its sources as `role: content` lines, clipped and marked. The ruleset
version `extractive/v1` is stored on every row.

**Ruling.** The user chose this. It buys three things the criteria actually grade: AC-MEM-004's
provenance is CHECKABLE rather than promised (a reader can compare the summary against its
sources line by line), AC-MEM-005's "恢复原始消息" is a mechanical walk rather than a model's
attempt, and a build with no embedding provider can still summarise because nothing leaves the
process.

**Cost, stated, and it is real.** The text reads mechanically and cannot paraphrase: a source
whose wording differs from the query's will not be found by the rerank's lexical term. A
model-written summary is the V1 the roadmap anticipates, and it would need a new ruleset
version rather than a change to this one.

### 7. The token estimator is a bound, not a tokeniser

`EstimateTokens` counts Han characters as one each and other runes at three per token, with a
floor of one per non-empty string.

**Ruling.** FR-120 grades "记忆构建符合 Token Budget" and section 5.3 puts memory seventh of
nine priorities, so the failure that matters is EXCEEDING the budget. An over-estimate is the
safe direction; an under-estimate would spend tokens the layer was not allowed and would be
invisible until a provider refused the prompt. This build has no dependency that carries a
tokeniser.

**Cost, stated.** A budget of N may fit fewer items than a provider's own count would, so a
context is smaller than it could be. That is the direction a bound should err in.

### 8. The deterministic checks run BEFORE the supervisor and MERGE into its report

`stagepipeline.RunSupervision` asks a `StageChecker` for findings, renders them into the
supervisor's task, then merges both kinds into one report. `review_issues` gains a `source`
column.

**Ruling.** The user chose this, and it is AGENT_CONTRACTS section 11.4's literal text:
"硬规则应尽量用确定性代码先检查，LLM Supervisor 负责语义质量。ReviewReport 合并两类证据，并标记
source=deterministic|llm". The merge dedupes on (rule, entity type, entity id, field) and keeps
the more severe statement, and `ReviewPassed` makes a deterministic blocker overrule a happy
verdict — which is what AC-E2E-004's FIX step depends on.

**Cost, stated.** A stage with a checker pays for the checks even when the supervisor would
have found the same faults, and the report's severity is now the worse of two sources, so a
model that rated a problem minor can see its rating overridden.

**Why the `source` column defaults to `'llm'`.** Every finding written before migration 000019
came from a supervisor, so an old row keeps exactly the meaning it had. The default is applied
at three boundaries — the column, the service and the repository — because the column's
DEFAULT only fires when an INSERT omits the column and the repository names every one.

### 9. The six rules, and the false-positive discipline they share

Costume continuity, prop continuity, location continuity, shot coverage and order, duration
total, and the approved-asset rule. Every one of them reports NOTHING when the data it would
compare against is absent.

**Ruling.** A deterministic check that fired on missing data would be one a user learns to
ignore, and the six are each capable of that failure in their own way: a coverage rule that
mis-read the script, a continuity rule that could not tell "no state recorded" from "wrong
costume", a duration rule with no tolerance. The discipline is asserted:
`TestConsistencyACleanBoardReportsNothing` and `TestConsistencyDoesNotFireWithoutAStoryState`.

**Cost, stated.** A project that has not filled in its character states gets no continuity
checking at all, and the rule cannot tell an incomplete project from a correct one. The
Quality Center's findings pane is where a user sees that nothing was checked.

### 10. The Memory Center is a Studio section

PRD section 8 lists 记忆中心 among the GLOBAL modules, beside 素材库 and Agent 中心.

**Ruling.** It is a section of the project's Studio shell, for the reason the Agent Center is:
a memory's scope names a PROJECT, so the surface that shows one is the surface that has one.
The alternative — a top-level route that picks a project from a list — would put a project
selector in front of a list whose every read needs one anyway.

**Cost, stated.** The IA's grouping is not reproduced literally, and a reader comparing the
PRD's module list against the navigation will not find a 记忆中心 entry beside the studio.

### 11. Memory writes have no agent-facing surface

`RememberFact` and `RememberMessage` are called by the desktop binding and by the runtime's own
per-turn write; `memory.deep_recall` is the ONLY memory tool, and it reads.

**Ruling.** AGENT_CONTRACTS section 12.4 lists what must not become a high-confidence fact
automatically — unapproved candidates, supervisor suggestions, agent guesses, rejected
versions, provider error text — and every item is something a MODEL produces. The strongest
form the rule can take is for the write path to have no tool at all.

**Cost, stated.** A user must type a fact for it to become semantic memory, and an agent that
learns something useful cannot record it. That is the intended trade: section 12.4's list is
about the agent's judgement, and there is no version of this rule that both keeps an agent's
guesses out and lets an agent write one.

## Consequences

- Three tables and one column are added by migration 000019; no published migration changes.
- The runtime gains an optional `MemoryPort`. A build without one recalls nothing and writes
  no memories, which is exactly what WP-07 shipped, and nothing invents an empty result to
  stand in for it.
- `stagepipeline` gains two optional ports (`Memory`, `Checks`). A build without a checker
  runs the supervisor alone.
- `review_issues.source` has a default, so every pre-WP-10 finding keeps its meaning.
- `MemoryCreated` is emitted, which ADR-0009 section 5 assigned to this package.
- The agent tool table's `memory.deep_recall` deepens from the recent window to section 12.3's
  walk, and its schema gains `query`, `maxSummaries` and `maxRawMessages`.

## Verification

- `go test ./... -count=1` — the AC-MEM criteria, the AC-E2E-004 walk, the canary scenario,
  the store's own refusals and the merge all have tests over the real schema.
- `internal/infrastructure/database/acceptance_wp10_test.go` — AC-MEM-001 through AC-MEM-005.
- `internal/infrastructure/database/acceptance_wp10_e2e_test.go` — AC-E2E-004's five clauses.
- `internal/application/stagepipeline/merge_wp10_test.go` — the deterministic pass and the
  merge, which an independent mutation review found entirely uncovered.
- `internal/infrastructure/database/canary_memory_wp10_test.go` — AC-E2E-005 over
  AGENT_CONTRACTS section 18.1's memory-recall fixture.

## References

- `docs/ROADMAP.md:495-533`; `docs/ACCEPTANCE.md:516-560`; `PRD.md` FR-110, FR-120,
  AC-E2E-004, AC-E2E-005; `docs/DOMAIN_MODEL.md` sections 14-17; `docs/ARCHITECTURE.md`
  section 12; `docs/AGENT_CONTRACTS.md` sections 11.4, 12, 13, 16, 18.
- ADR-0002 section 68 (extension loading); ADR-0005 (identifiers); ADR-0009 section 5
  (`MemoryCreated`'s assignment); ADR-0011 (the tool table).
