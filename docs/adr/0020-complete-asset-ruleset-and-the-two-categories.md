# ADR-0020 The Complete Asset Ruleset, and the Two Categories That Had No Emitter

Status: Accepted
Date: 2026-09-25
Work package: WP-16 (P3 item 20)
Supersedes: none
Related: ADR-0013 (the production pipeline's stage vocabulary), ADR-0019 (explicit stage
dependencies), AGENT_CONTRACTS §11.1–11.4, PRD FR-110 (quality categories), FR-030, FR-150,
FR-160

## Context

PRD §16's v1.0 list carries one item this package is accountable for: 完整资产一致性检查 —
"the deterministic half of every ruleset, plus Safety/Cost". A reconnaissance pass before any code
was written established four facts, each with evidence, and together they defined the work:

1. **`PROP_CONTINUITY` and `LOCATION_CONTINUITY` had implementations and no test that made them
   FIRE.** Both were dispatched by `Checker.CheckStoryboard` and both worked; every fixture in the
   repository cited `usage_role = "costume"` or `"reference"`, so neither rule's positive path had
   ever executed. A rule with no positive case cannot be told apart from a stub.
2. **`REVISION_BUDGET_SPENT` and `CONTENT_RATING` were category-map keys with nothing behind
   them.** One hit each in the whole repository, both in `ruleCategories`. No rule, no test, no
   caller, no storage. The classification was declared and nothing emitted it.
3. **`Finding.Category` was set by one ruleset and DROPPED by storage.** `review_issues` had no
   `category` column, so `CategoryOf` — the function that exists to answer exactly this question —
   had no production caller at all. Its only callers were three assertions in one test file.
4. **`TestCheckedStagesMatchTheSwitch` did not exist.** `consistency_checker.go`'s own comment
   promised it by name ("`TestCheckedStagesMatchTheSwitch` is what keeps the two from drifting").
   The list and the switch happened to agree, so nothing was broken yet; what was missing was the
   thing that would say so when they diverged.

AGENT_CONTRACTS §11.2 lists eight asset clauses. Three of them (派生关系, 文件存在和类型, and the
narrowed form of 引用范围) were answered by no code and no ruleset; four were already answered
elsewhere (`checkCostumeContinuity`, `checkAssetApproval`, `checkPropContinuity`) or have no
storage in this build at all (场景时间/天气 — STATUS §0k's recorded reason).

## Decision

**1. The Asset Ruleset's mechanical half is two NEW rules plus one repair, all over the versions a
board cites.**

They run from `CheckStoryboard` rather than from an asset stage, because `asset_usages` names the
consumer: a rule that ran at the asset stage would check versions nobody cites and miss the ones the
board depends on. Each rule reads one version per DISTINCT version id, and each finding's entity is
the board row a user can open.

- `ASSET_LINEAGE` (§11.2 派生关系) — `based_on_version_id` and `parent_asset_version_id` are TEXT
  with no foreign key, so a dangling parent is storable. `minor`, because a dangling citation does
  not stop a board from being produced; what it breaks is the lineage AC-ASSET-002 traces.
- `ASSET_FILE_PRESENT` (§11.2 文件存在和类型) — see ruling 3.

**2. The two category-only rules get an emitter each, and the shape of each is decided by what
storage can actually answer.**

- `CONTENT_POLICY_REFUSED` (Safety) reads `generation_jobs.error_code`, which the worker sets from
  the provider's own error category. It is the VENDOR half of 「内容与供应商规则」 — the half with a
  column behind it. Reported against the CONSUMER the job was submitted for, not the job, because a
  report's findings address things a user can open.
- `REVISION_BUDGET_SPENT` (Cost) is a FUNCTION, not a Checker method: it takes the revision count
  and the policy's budget, both of which the workflow engine already holds and now exposes through
  `Engine.RevisionBudget`. `minor`, and deliberately not a blocker — a spent budget is a fact a
  person needs, not a defect in the artifact.

**3. The file rule reports a version that CLAIMS a generation job and has no bytes; it does not
report a version with no file.** This is the ruling this ADR exists to record, because the first
version of the rule got it wrong and two existing tests proved it. See "Alternatives rejected".

**4. `review_issues.category` is added by migration 000024, ALTER rather than a rebuild, with an
EMPTY default and no CHECK.**

The empty default is the honest value for a row written before the column: those findings came from
a supervisor, which states no category, and reading one back as `technical` would be this migration
inventing a classification nobody made — the same distinction 000019's `source` column drew.

**5. The category is filled from the RULE, not copied from the finding.** `MergeIssues` prefers a
category the ruleset stated and falls back to `consistency.CategoryOf(finding.Rule)`. The fallback is
load-bearing: the six storyboard rules never set the field, so without it every finding they produce
would reach the database with an empty category while the classification map went unread — which was
the state of the build before this package.

**6. The classification reaches the UI as a tag, and an EMPTY one renders nothing.** The quality
centre shows `studio.quality.category.<value>`; an empty category shows no tag rather than the word
"technical".

**7. `TestCheckedStagesMatchTheSwitch` exists now**, and `StageChecker`'s doc comment is no longer a
promise about a test nobody wrote.

## Consequences

- Four of AGENT_CONTRACTS §11.2's eight clauses are now answered by code. The remaining ones are
  accounted for: three were already answered by other rules and 场景时间/天气 has no storage.
- FR-110's ten categories have a real vocabulary with a totality test, five of them have an emitter,
  and the classification survives to a reader. `CONTENT_RATING` stays category-only and its entry
  says why: `project_settings.content_rating` is free text, and a rule over free text would be
  checking the user's spelling.
- `generation_jobs.error_code` is now read by a rule, so its vocabulary is load-bearing beyond the
  job centre. A test asserts the string the asset rules look for equals `job.CategoryContentPolicy`,
  because the rules package spells it as a literal rather than importing the job domain.
- Two rules that had never fired have positive cases in the suite. The prop and location rules'
  negative cases are asserted in the same tests, so a rule that fired on everything fails too.
- The new rules read through one widened port (`AssetReader.ListFilesWithTypes`) and one new optional
  port (`JobFailureReader`), and a checker composed without the job read simply does not run the
  safety rule — asserted, so "goes quiet" cannot become "reports everything".

## Alternatives rejected

**Report every cited version with no file.** THIS IS WHAT THE FIRST VERSION DID, and
`TestConsistencyACleanBoardReportsNothing` and AC-E2E-004's repair walk both failed on the spot. Both
were right: an asset bible DEFINES an asset before any art exists, so a version with no file is the
ordinary state of a project that has written its bible and not yet generated its pictures. The rule
as written would have blocked every board in that state — which is most of them, including every
board the acceptance walk builds. The anchor that makes it sharp is `generation_job_id`: it is the
column that says "these bytes were produced".

**A branch for "the link exists but its object is gone".** Rejected as unreachable: `asset_files.file_hash`
has a FOREIGN KEY to `file_objects`, every connection enables `foreign_keys(1)`, and the garbage
collector's predicate spares any hash an asset references. Code no test can reach is the shape this
repository keeps deleting.

**A CHECK constraint on the category's values.** Rejected: the vocabulary is closed in
`internal/domain/consistency`, and a CHECK here would be a second copy of the list that drifts the
moment an eleventh category is added — with the failure surfacing as a constraint violation on a
user's write rather than as a failing vocabulary test.

**Default the category to `technical`.** Rejected for the reason the empty default exists: "nobody
classified this" and "classified as technical" are different facts, and the UI needs to tell them
apart. The empty string is a state, not an absence.

**Validate the category in the workflow service.** Rejected: that layer must not import the domain
package that owns the list, and a second copy of ten constants is a second list to keep in step. What
the service enforces is the SHAPE (a length bound, so a paragraph cannot be stored in a tag column);
what the vocabulary test enforces is membership.

**Put the cost rule in the checker's port surface.** Rejected: every other rule reads what the Checker
holds, but the revision count belongs to the engine that refuses the over-budget revision. A port
would have been a read pretending to be one, with the caller already having gone to the engine.

## Verification

- `go test ./... -count=1` — PASS, 57 packages.
- `sh scripts/verify.sh` — PASS, exit 0 (25 Playwright, security scans over 654 files, all fixture
  checks, SBOM).
- `wails build` — PASS (run explicitly, because this shell's PATH lacked the CLI; the verify script's
  SKIP line is a PATH artifact rather than a missing capability).
- **Twelve mutations, 12/12 killed**, each restored byte-identically and the restore verified. THREE
  SURVIVED THE FIRST RUN and each was a real gap: the merge dropped the category with every test green
  (nothing asserted it), the workflow service's row construction was untested because the fixture
  built rows itself and stepped over the line, and the budget rule had no unit test at all. The
  fixtures were changed to exercise the production path, and each gap got the assertion it lacked.
- Two mutations that broke EXISTING tests are recorded above rather than papered over.
