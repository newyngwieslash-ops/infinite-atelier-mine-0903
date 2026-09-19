# ADR-0010 Document Import, the Event Extraction Contract, and the Story Graph

- Status: Accepted (WP-06 scope)
- Date: 2026-09-19
- Deciders: Repository engineering under approved WP-06 plan
- Related work package: WP-06 (原始文档、章节与事件图谱)

## Context

WP-06 had to make three things real that no document fully specified: a document
import path (PRD FR-020), an event-extraction contract (ROADMAP scope item 6, which
says only "EventExtraction 输出 Schema 和 Mock Agent/Service"), and the candidate
workflow over the story fact layer (PRD FR-030). Where the specification was
silent or self-contradictory, this record states what was chosen and why, so the
next package can disagree with a decision rather than with an accident.

Four gaps needed a ruling, and one existing vocabulary needed widening.

## Decision

### 1. The EventExtraction contract, which the specification did not define

No document in the repository defined the shape an extractor must return.
AGENT_CONTRACTS §4.2 refers to a schema by path (`schemas/agent/...`), §7.3/§7.4
define an ExecutionRequest and ExecutionResult for the RUNTIME, and neither is the
shape of "what this chapter contains". The schema's job here is narrower than the
runtime's: one chapter in, facts out.

**Ruling.** `schemas/agent/event_extraction.v1.json` is that contract, and it is
AUTHORITATIVE: `internal/application/validation` compiles the embedded file and
validates against it rather than describing the same shape a second time in Go
tags. Two properties are deliberate:

- **It is closed.** `additionalProperties: false` everywhere, so a field a model
  invents is a refusal rather than something silently dropped.
- **It has no status field.** PRD FR-030 keeps a user or rule gate in front of
  every confirmed fact, and a schema with nowhere to write `accepted` makes that
  structural rather than a convention a prompt has to remember.

The `ref` fields are local keys rather than database identifiers, because a model
cannot know an id the repository has not minted. The service mints ids and
resolves keys, and refuses a document naming a ref nothing defines — see §4.

**If WP-07 introduces a general agent schema, that schema wins** and this one
becomes its specialisation. The service reads it through one function
(`validation.EventExtraction`), so the seam is one call rather than a shape
threaded through the package.

### 2. Section 14.3's repair round is an optional interface

AGENT_CONTRACTS §14.3 allows exactly one repair: send compact validation errors
back to the same agent, validate again, fail if still invalid.

**Ruling.** `RepairingExtractor` is a separate OPTIONAL interface embedding
`Extractor`. A caller that composes an extractor without it gets the validation
failure reported as final, which is honest for a single-shot implementation; what
must not happen is a silent claim that a repair was attempted.

The violations sent back name the RULE and the JSON Pointer and never quote the
document. That is a security decision rather than a formatting one: §14.3's round
puts text into a model's next prompt, and a model's output quotes a chapter,
which is untrusted input. The library's detailed messages render the offending
value, so they are not used; the rule name comes from the schema's own keyword
vocabulary instead. `additionalProperties` is the one keyword whose message names
an INSTANCE-chosen value, so its property names are dropped; `required` is the one
whose name comes from the SCHEMA, so it is kept, because "does not satisfy
required" cannot be acted on.

### 3. The entity vocabulary widens to FR-030's list, with a mapping

PRD FR-030 names nine entity kinds; migration 000007's CHECK pinned six. The
column is widened to eight by rebuilding the table (SQLite cannot alter a CHECK;
migration 000014 stages the four affected tables, drops children before parents,
and backfills).

| FR-030 | Where it lives | Why |
|---|---|---|
| Character, Location, Organization, Object/Prop | `story_entities.entity_type` | Already present. |
| **Relationship** | `story_relations` normally; `entity_type = 'relationship'` for a relationship the TEXT names as a thing | A relation between two entities is a `story_relations` row and always was. The entity value exists for a named bond — a sworn brotherhood — which a user can describe, alias and attach facts to. Both homes are needed because the two are different facts. |
| **TimelineMarker** | `story_entities.entity_type = 'timeline_marker'` | A named point the story refers back to. An entity rather than an event because it is REFERRED to, not narrated. |
| **CharacterState** | `character_states` (migration 000007 §6.8) | Already modelled, as a sparse controlled-JSON row rather than an entity. |
| **StoryEvent** | `story_events` | Already modelled. An event is not an entity kind; folding it in would give one concept two homes. |
| **PropState** | **Not added.** | §6.8 already models character state with a `prop` entity for the object itself and a controlled-JSON state row, and the PRD gives no fields for a separate prop-state table. Adding one would create a second representation of "what a thing was like at a moment" with no specification to fill it. If a later package needs prop state, it extends `character_states` — the schema's own comment there says the JSON columns are the licence for sparse state. |

The three parity guards (Go list, SQL CHECK, JSON schema enum) are kept in step by
a test that derives all three, which is what closed the drift this widening
introduced and a pinned file did not notice — see ADR-0007's record of the same
class of failure.

### 4. Where the offsets point, and why the alias offsets are different

DOMAIN_MODEL §6.6 gives evidence a `source_document_version_id` and two offsets,
and says the passage is read back "通过 offset 从规范化文本读取" — the VERSION's text.
§6.2 gives an alias a `source_chapter_id` and two offsets and no version reference,
so its offsets index the CHAPTER.

**Ruling.** Both are honoured as written, and the difference is deliberate rather
than an inconsistency: the presence of the version field is what decides. A
`ChapterText` therefore carries a `BaseOffset` — where the chapter's slice begins
in the version — and evidence offsets are shifted by it while alias offsets are
not. Conflating them was a defect that survived a green suite because every
fixture had one chapter at offset zero; the fixtures now start at 1000.

### 5. What extraction does NOT search for

Section 6.6 permits a limited excerpt for verification. Extraction records the
offsets of a proposed NAME when it can find it in the chapter text, and records
the chapter with no range when it cannot.

**Ruling.** No offsets are computed from a model's arithmetic, and none are
guessed. A range that points at the wrong passage is worse than no range, because
a reader following the citation is misled rather than merely uninformed. The kind
is `agent_inference`, not `text`: a fact a model proposed is an inference from the
text, and calling it `text` would tell a later reader that the passage states what
the fact states.

### 6. The DOCX reader takes text and nothing else

**Ruling.** Two entries are read: `[Content_Types].xml` to confirm the container
is an OOXML Word document, and `word/document.xml` for the body. `<w:t>` runs and
the paragraph, line-break and cell boundaries are extracted; styles, headings,
footnotes, comments and field codes are ignored. No relationship is resolved and
no URL is fetched, which is what SECURITY §8.2's "禁止外部关系自动访问" asks for.

One consequence is deliberate: a DOCX and a TXT of the same prose produce the same
normalized text and therefore the same chapter boundaries, because all three
formats go through one text-based detector. That is what makes a DOCX chapter's
offset comparable with a TXT chapter's.

The three container refusals each needed a fixture that ONLY that refusal can
produce, which is a finding in its own right: `zip-slip.zip` is stopped by the
archive reader's traversal guard and `bad-docx.docx` never opens, so neither
reaches the container check at all. `zip-not-docx.zip`, `xlsx-named-docx.docx` and
`docx-missing-body.docx` exist for that reason, and a mutation disabling the check
is caught by them and was NOT caught before they existed.

### 7. Extraction is a synchronous command, with no workflow rows

ROADMAP's WP-06 note says: if the Agent Runtime is not finished, extraction goes
through an explicit Application Service plus a Mock/Provider adapter, and WP-07
wires it to the unified runtime — "不创建临时不可迁移架构".

**Ruling.** `ExtractChapterEventCandidates` is one command. It creates no
`WorkflowRun` and no `StageRun`, because the stage keys are WP-07's to define
(ADR-0007 §1 records that the two documents disagree about them, and this package
does not choose between them). The `Extractor` port is the seam WP-07 implements.

No domain event is emitted either. DOMAIN_MODEL §17's list is closed and pinned by
a test; it has `StoryFactAccepted` and `StoryFactConflictOpened` and nothing for a
proposal, which is coherent — an extraction writes CANDIDATES, and a candidate is
not yet a fact. Announcing one would put a row in the stream saying a fact changed
when what happened is that a model suggested something.

### 8. Scope not delivered, and why

Recorded here rather than left to be discovered:

- **Chapter split and merge have no command.** `ReviseChapter` adjusts a
  boundary's title and offsets under a revision guard, which is the "调整" that
  ROADMAP item 3 and AC-STORY-001's "用户调整" ask for. Splitting one chapter into
  two or merging two into one are specific forms the specification never names,
  and each raises questions it does not answer (which ordinal does the new row
  take, what happens to facts citing the old chapter, does the merge keep the
  first title). They are a later package's work, not an oversight.
- **A "modify" command beyond the boundary edit does not exist.** ROADMAP item 8
  says 接受/拒绝/修改/锁定. Accept, reject and lock exist as their own commands;
  "modify" is served by the create and revise commands of the aggregate in
  question, and a generic "modify a fact" would be a second way to write the same
  row.

### 9. The Mock is a test file

AGENTS forbids a Mock on the production path. `Mock` lives in `mock_test.go`, and
a `Service` composed without an extractor reports itself UNAVAILABLE: a production
build fails closed with a reason (`DRAMA_UNAVAILABLE`, "No document reader is
configured") instead of inventing facts. Its modes are AGENT_CONTRACTS §18.3's
scenarios, including `MockInvalidOnce`, which is what exercises the repair round.

## Consequences

- The EventExtraction schema is a second place the entity and relation
  vocabularies are written. A test derives all three and compares; it caught the
  drift the widening introduced.
- Section 14.3's repair round doubles the calls a malformed reading costs. It is
  bounded at one round, and the alternative — failing a reading a model could
  have fixed — is what the contract exists to avoid.
- The chunked transfer adds three binding methods where one would do, because a
  Wails message carrying a whole novel as a byte array is work proportional to the
  document on the thread that paints the UI.
- The precheck still crosses in one message. It is bounded by the domain's input
  ceiling and recorded as a remaining cost rather than claimed to be free.
- `PropState` remains unmodelled. A later package that needs it should extend
  `character_states` rather than add a fourth representation.
