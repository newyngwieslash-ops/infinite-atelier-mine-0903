package consistency

import (
	"context"
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/consistency"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/storyboard"
)

// asset_rules.go is AGENT_CONTRACTS section 11.2's mechanical half.
//
//	## 11.2 Asset Ruleset
//	- 角色身份和外观；
//	- 服装状态；
//	- 场景时间/天气；
//	- 道具所有权与状态；
//	- Approved Version；
//	- 派生关系；
//	- 文件存在和类型；
//	- 引用范围。
//
// # Which clauses are here and which are elsewhere
//
// Three of the eight are answered over the ASSETS themselves and they are the three that were
// missing: 派生关系 (a version must name a parent that exists), 文件存在和类型 (a version must have
// bytes, of the kind its asset claims), and 引用范围 — the last narrowed to what a join can actually
// decide, which this file explains under its own rule.
//
// Four were already answered somewhere a reader can find them, and repeating them here would make the
// merge's dedupe hide a duplicate rather than prevent one:
//
//   - 服装状态 is `checkCostumeContinuity`, over the board (STATUS section 0k's ruling).
//   - Approved Version is `checkAssetApproval`.
//   - 道具所有权与状态 is `checkPropContinuity`.
//   - 角色身份和外观 and 场景时间/天气 are judgement or have no column: the identity clause is what a
//     supervisor reads the picture for, and 场景时间/天气 has no storage in this build at all —
//     STATUS section 0k records that as the reason section 11.2's rules were deferred.
//
// # Why these run over the BOARD rather than over an asset stage
//
// The asset stage writes a VERSION, not a report about one, and the ruleset needs a set of rows to be
// about: `CheckStoryboard` is where the assets a project actually USES are known, because
// `asset_usages` names the consumer. Running these rules at the asset stage would check versions
// nobody cites and miss the ones the board depends on. So they read the board's usages and check each
// cited version — which is also what makes the findings actionable: the entity is the row, and the
// row is what a user can open.

// checkAssetFiles runs section 11.2's "文件存在和类型" over every version the board cites.
//
// # The rule, stated so it can be argued with
//
// A version that came from a GENERATION JOB must have a file, and every file a version has must be of
// a kind the asset's own `asset_type` is compatible with. Two states are reported:
//
//  1. GENERATED, NO BYTES. `generation_job_id` is set — this version is a job's output — and there are
//     no file rows. The collect path REFUSES to create such a version ("A generated version needs at
//     least one committed file"), so if one exists the bytes went away after the fact, or a restore
//     produced it. `critical`, because nothing can render it.
//  2. A TYPE MISMATCH. A `character` asset whose only file is an MP4 is a mismatch a user has to fix
//     by re-generating, and it is decidable because BOTH vocabularies are closed.
//
// # What the first version of this rule got wrong, and why the tests were right
//
// It reported EVERY cited version with no files as critical. Two existing tests failed immediately —
// `TestConsistencyACleanBoardReportsNothing` and AC-E2E-004's repair walk — and both were correct to:
// an asset bible DEFINES an asset before any art exists, so a costume version with no file is the
// ordinary state of a project that has written its bible and not yet generated its pictures. The rule
// as first written would have blocked every board in that state, which is most of them.
//
// The anchor that makes the rule sharp is `generation_job_id`: it is the column that says "these bytes
// were produced", and a version that says so with nothing behind it is a fault rather than a project
// that has not got there yet.
//
// # Why there is no "the link has no object" branch
//
// `asset_files.file_hash` has a FOREIGN KEY to `file_objects` and every connection enables
// `foreign_keys(1)`, so a link to a missing object cannot be committed — and the garbage collector's
// predicate spares any hash an asset references, so it cannot be removed afterwards either. A branch
// reporting that state would be code no test can reach, which is the shape this repository keeps
// deleting rather than adding.
//
// # What it deliberately does not do
//
// It does not require a version to have bytes at all, per the correction above, and it does not judge
// whether the bytes are GOOD — "is this PNG a picture of the right person" is not a join.
func (c *Checker) checkAssetFiles(ctx context.Context, items []storyboard.StoryboardItem) []consistency.Finding {
	if c.assets == nil {
		return nil
	}
	// One read per VERSION rather than per row: a costume used in twenty rows is one version, and a
	// rule that re-read its files per row would report the same fault twenty times.
	checked := map[string]bool{}
	findings := []consistency.Finding{}
	for _, item := range items {
		usages, err := c.assets.UsagesForConsumer(ctx, asset.ConsumerShot, item.ID)
		if err != nil {
			continue
		}
		for _, usage := range usages {
			if checked[usage.AssetVersionID] {
				continue
			}
			checked[usage.AssetVersionID] = true
			findings = append(findings, c.inspectVersionFiles(ctx, item, usage)...)
		}
	}
	return findings
}

// inspectVersionFiles reports what is wrong with one cited version's files.
//
// It is split from the loop above so the rule reads as one decision per version, which is what the
// findings are about.
func (c *Checker) inspectVersionFiles(ctx context.Context, item storyboard.StoryboardItem, usage asset.Usage) []consistency.Finding {
	record, err := c.assets.GetVersion(ctx, usage.AssetVersionID)
	if err != nil {
		// A version that cannot be read is not a version with no files. `checkAssetApproval` reports
		// the citation problems it can see, and inventing a finding from a read failure would be the
		// false positive this package's rules are written to avoid.
		return nil
	}
	owner, err := c.assets.GetAsset(ctx, record.AssetID)
	if err != nil {
		return nil
	}
	files, err := c.assets.ListFilesWithTypes(ctx, usage.AssetVersionID)
	if err != nil {
		return nil
	}
	evidence := []consistency.Evidence{
		{Type: "entity_ref", Ref: item.ID},
		{Type: "entity_ref", Ref: usage.AssetVersionID},
	}
	if len(files) == 0 {
		// NOT a finding in itself: see the note above on the asset bible. Only a version that claims
		// a job is reporting the fault this rule is about.
		if strings.TrimSpace(record.GenerationJobID) == "" {
			return nil
		}
		return []consistency.Finding{{
			Rule:       consistency.RuleAssetFilePresent,
			Category:   consistency.CategoryAsset,
			Severity:   consistency.SeverityCritical,
			EntityType: "storyboard_item",
			EntityID:   item.ID,
			Field:      "assetVersionId",
			Problem: "This row uses " + owner.Name + " version " + itoa(record.VersionNumber) +
				", which was produced by a generation job and has no file. Nothing can render it.",
			Suggestion: "Re-run the generation that produced it, or point the row at a version whose " +
				"file still exists.",
			Evidence: append(evidence, consistency.Evidence{Type: "job", Ref: record.GenerationJobID}),
			// The fix is a new generation, which is a decision about which picture belongs here.
			AutoFixable: false,
		}}
	}
	findings := []consistency.Finding{}
	for _, file := range files {
		if !mimeMatchesAssetType(owner.Type, file.MIMEType) {
			findings = append(findings, consistency.Finding{
				Rule:       consistency.RuleAssetFilePresent,
				Category:   consistency.CategoryAsset,
				Severity:   consistency.SeverityMajor,
				EntityType: "storyboard_item",
				EntityID:   item.ID,
				Field:      "assetVersionId",
				Problem: "This row uses " + owner.Name + " (" + string(owner.Type) + ") version " +
					itoa(record.VersionNumber) + ", whose file is " + file.MIMEType + ".",
				Suggestion: "Attach a file of the kind this asset is, or change the asset's type if " +
					"the file is right and the type is wrong.",
				Evidence:    append(evidence, consistency.Evidence{Type: "file", Ref: file.Hash}),
				AutoFixable: false,
			})
		}
	}
	return findings
}

// checkAssetLineage runs section 11.2's "派生关系" over every version the board cites.
//
// # The rule, stated so it can be argued with
//
// A version that names the version it was revised from (`based_on_version_id`) or derived from
// (`parent_asset_version_id`) must name one that EXISTS. Both columns are TEXT with no foreign key,
// so a dangling identifier is storable, and the two columns mean different things: a revision follows
// a version of the same asset, while a derivation may cross assets — a prop extracted from a costume
// is derived from it without being a revision of it. A row that gets this wrong loses the history a
// reader needs to answer "where did this come from", which is what AC-ASSET-002's parent refs are.
//
// # Why it is `minor` rather than blocking
//
// A dangling parent does not stop a board from being produced: the bytes are there and the row can be
// shot. What it breaks is the LINEAGE — the answer to "what was this derived from" — and a user who
// does not care about that today should not have their board blocked over it. A rule that made it
// major would be trading a production for a citation, which is the wrong way round.
//
// # Why it does not check that the parent is APPROVED
//
// Because that is not what the clause says. A derivation commonly names a version that was never
// approved — the whole point of deriving is to make a variant of something under review — and
// requiring approval would report every legitimate derivation in the project.
func (c *Checker) checkAssetLineage(ctx context.Context, items []storyboard.StoryboardItem) []consistency.Finding {
	if c.assets == nil {
		return nil
	}
	checked := map[string]bool{}
	findings := []consistency.Finding{}
	for _, item := range items {
		usages, err := c.assets.UsagesForConsumer(ctx, asset.ConsumerShot, item.ID)
		if err != nil {
			continue
		}
		for _, usage := range usages {
			if checked[usage.AssetVersionID] {
				continue
			}
			checked[usage.AssetVersionID] = true
			record, err := c.assets.GetVersion(ctx, usage.AssetVersionID)
			if err != nil {
				continue
			}
			for _, parent := range []struct {
				column string
				value  string
				// relation names what the column MEANS, because the two are different claims and a
				// finding that said "parent" for both would leave a user guessing which link broke.
				relation string
			}{
				{"basedOnVersionId", record.BasedOnVersionID, "revised from"},
				{"parentAssetVersionId", record.ParentAssetVersionID, "derived from"},
			} {
				if strings.TrimSpace(parent.value) == "" {
					continue
				}
				if _, err := c.assets.GetVersion(ctx, parent.value); err == nil {
					continue
				}
				findings = append(findings, consistency.Finding{
					Rule:       consistency.RuleAssetLineage,
					Category:   consistency.CategoryAsset,
					Severity:   consistency.SeverityMinor,
					EntityType: "storyboard_item",
					EntityID:   item.ID,
					Field:      parent.column,
					Problem: "This row uses a version that says it was " + parent.relation +
						" " + parent.value + ", which does not exist.",
					Suggestion: "Correct the version's lineage, or restore the version it names. The " +
						"history is what AC-ASSET-002 traces.",
					Evidence: []consistency.Evidence{
						{Type: "entity_ref", Ref: usage.AssetVersionID},
						{Type: "entity_ref", Ref: parent.value},
					},
					AutoFixable: false,
				})
			}
		}
	}
	return findings
}

// mimeMatchesAssetType reports whether a file of this MIME type is plausible for an asset of this
// type.
//
// # Why a table rather than a predicate
//
// The two vocabularies are both closed — `assets.asset_type` is a CHECK with thirteen values, and a
// file object's MIME is what a job stored — so the relation between them is a finite fact rather
// than a rule, and stating it as a table is what makes it reviewable. A reader can see that a
// costume may be an image and disagree; a reader of `strings.HasPrefix` could not.
//
// # Why the image types are one family
//
// `character`, `location`, `prop`, `costume`, `vehicle`, `creature`, `style`, `style_reference`,
// `derived_asset` and `image` are all things a picture is made of, and this build generates them as
// images (WP-09's mock image produces a real PNG). Splitting them would be inventing a finer
// distinction than any column carries: `asset_type` says what the asset IS, not what a file of it
// must be, and a costume rendered as a 3D reference is not an error this table should invent.
//
// # Why an unknown type is not a finding
//
// A file type this build does not classify falls through to `true`, which is the fail-open direction
// — and it is the right one HERE, because the alternative is a build that reports every file of a
// type added by a later version. The clause is "文件存在和类型", and "I do not know this type" is
// not evidence of a mismatch. What the rule CAN decide is the case it names: a picture asset whose
// file is a video, and an audio asset whose file is an image.
func mimeMatchesAssetType(assetType asset.Type, mimeType string) bool {
	mime := strings.ToLower(strings.TrimSpace(mimeType))
	if mime == "" {
		return true
	}
	switch assetType {
	case asset.TypeImage, asset.TypeCharacter, asset.TypeLocation, asset.TypeProp,
		asset.TypeCostume, asset.TypeVehicle, asset.TypeCreature, asset.TypeStyle,
		asset.TypeStyleReference, asset.TypeDerivedAsset:
		return strings.HasPrefix(mime, "image/")
	case asset.TypeVideo:
		return strings.HasPrefix(mime, "video/")
	case asset.TypeAudio:
		return strings.HasPrefix(mime, "audio/")
	case asset.TypeDoc:
		// A document is anything a FileStore holds: the import path stores text, PDFs and archives
		// under this type, and a rule that required `text/` would flag a scanned script.
		return true
	default:
		return true
	}
}

// RevisionBudgetFinding reports a stage whose automatic revision budget is spent.
//
// It is a FUNCTION rather than a Checker method, and the reason is where the answer lives: every
// other rule in this package reads through a port the Checker holds, while the revision count belongs
// to the workflow engine — the same object that refuses the over-budget revision. A method here would
// have to be handed the count by a caller that had already gone to the engine, which is a port
// pretending to be a read. The stage pipeline calls this with the engine's own numbers.
//
// # Why this is FR-110's COST category
//
// The PRD's category is 「不必要的高成本重试」 — unnecessarily expensive retries — and the mechanism
// this build has against exactly that is the engine's `MaxAutoFix` budget: a stage may be revised
// automatically while `revisions < budget`, and after that a person decides (AC-AGENT-005's
// 「FIX 超过 2 次转人工」). When the budget IS spent, the next automatic attempt would be a retry the
// pipeline already decided against, and the cost of it is why the budget exists.
//
// # What it does NOT claim
//
// It does not report a currency amount, because this build prices nothing: there is no rate column
// and no per-model cost join, and a rule that multiplied token counts by a guess would put a number
// in front of a user that nobody stands behind. The finding says what a reader can act on — the
// budget is spent, so more automatic work on this attempt is not the way forward — and names the
// count it read.
//
// # Why it is a finding rather than only an engine refusal
//
// The engine refuses `StartRevision` over budget and moves the stage to `waiting_user`, so a caller
// cannot slip past it. What is missing is the RECORD: a review report is what a person reads, and
// "this attempt has spent its automatic revisions" is a fact about the review that a report should
// carry, beside the findings that caused the revisions. Without it a user sees a stage waiting for
// them and no statement of why.
func RevisionBudgetFinding(stageRunID string, revisions, budget int) (consistency.Finding, bool) {
	// A spent budget is the only thing this reports. The caller states whether it could COUNT the
	// revisions at all: an engine with no counter refuses rather than reporting zero, and a rule that
	// treated "cannot count" as "no revisions yet" would be the fail-open mistake this package's
	// optional ports exist to avoid. `budget <= 0` is the same answer from the other side — a stage
	// configured with no automatic revisions spends its budget immediately and reporting that would
	// put a finding on every review.
	if revisions < budget || budget <= 0 {
		return consistency.Finding{}, false
	}
	return consistency.Finding{
		Rule:       consistency.RuleRevisionBudgetSpent,
		Category:   consistency.CategoryCost,
		Severity:   consistency.SeverityMinor,
		EntityType: "stage_run",
		EntityID:   stageRunID,
		Field:      "autoFix",
		Problem: "This attempt has used " + itoa(revisions) + " of its " + itoa(budget) +
			" automatic revisions, so the next failure is yours to decide.",
		Suggestion: "Decide what to do about the open findings: accept them, fix the artifact " +
			"yourself, or start a new attempt.",
		// Choosing whether more work is worth it is the user's judgement, which is the point of the
		// budget rather than a defect in the artifact.
		AutoFixable: false,
	}, true
}

// checkContentPolicyRefusals reports a job that a provider refused for content-policy reasons.
//
// # Why this rule needs a job table and not an artifact
//
// FR-110's SAFETY category is 「内容与供应商规则」 — content and VENDOR rules — and the two halves are
// answerable in different places. The content half is a judgement about the artifact and belongs to
// the supervisor. The VENDOR half is a fact: a provider refused a request, and the refusal was
// recorded. This build stores exactly that on `generation_jobs.error_code`, which the worker sets
// from the provider's own error category, and the category that matters is
// `job.CategoryContentPolicy` — stored as the string "content_policy".
//
// # Why it is `major` and not `critical`
//
// A content-policy refusal is not corrupt data and not a broken artifact: the picture simply was not
// made. What it needs is a decision — reword the prompt, change the framing, or accept that this
// provider will not render it — and the finding's job is to put that decision in front of a person
// with the row it was about. `critical` would force a manual gate on a stage that may have plenty of
// other candidates, which is a heavier response than the fault.
//
// # Why the rule reports the job's CONSUMER rather than the job
//
// Because a report's findings address things a user can open (FR-110's "报告问题可以在 UI 中跳转到
// 实体"). The job is a row in a job centre; the storyboard item is where the work is. So the entity is
// the consumer the job was submitted for, and the job's identifier travels as evidence.
//
// # What it deliberately does not do
//
// It does not report OTHER failure categories — a timeout, a storage error — because those are
// transient or environmental and the job manager already retries and reports them. Refusing for
// content is the one category where retrying the same prompt will fail the same way every time,
// which is what makes it a finding about the ARTIFACT rather than about the request.
func (c *Checker) checkContentPolicyRefusals(ctx context.Context, items []storyboard.StoryboardItem) []consistency.Finding {
	if c.jobs == nil || len(items) == 0 {
		return nil
	}
	consumerIDs := make([]string, 0, len(items))
	for _, item := range items {
		consumerIDs = append(consumerIDs, item.ID)
	}
	failures, err := c.jobs.FailedJobsForConsumers(ctx, "storyboard_item", consumerIDs)
	if err != nil {
		// A job store that cannot be read is not a project with no refusals.
		return nil
	}
	findings := make([]consistency.Finding, 0, len(failures))
	for _, failure := range failures {
		if failure.ErrorCode != contentPolicyErrorCode {
			continue
		}
		findings = append(findings, consistency.Finding{
			Rule:       consistency.RuleContentPolicyRefused,
			Category:   consistency.CategorySafety,
			Severity:   consistency.SeverityMajor,
			EntityType: failure.EntityType,
			EntityID:   failure.EntityID,
			Field:      "prompt",
			Problem: "A provider refused generation for this row because of its content policy. " +
				"Retrying the same request will fail the same way.",
			Suggestion: "Change what the prompt asks for, or choose a provider whose policy allows " +
				"it. The refusal is the provider's rule rather than a fault in the row.",
			Evidence:    []consistency.Evidence{{Type: "job", Ref: failure.JobID}},
			AutoFixable: false,
		})
	}
	return findings
}

// contentPolicyErrorCode is how a content-policy refusal is spelled in `generation_jobs.error_code`.
//
// It is a LITERAL rather than an import of `job.CategoryContentPolicy`, and that is deliberate: this
// package must not import the job domain for one string, and the value is a stored identifier rather
// than a live Go constant — rows written by an older build carry it, and a change to the constant
// would silently stop matching them. A test in this package asserts the two agree, which is what
// makes the duplicate safe: the drift is caught where the string is written rather than at run time.
const contentPolicyErrorCode = "content_policy"

// ContentPolicyErrorCode is the stored spelling, exported for the parity test.
//
// The test is what makes the duplicate literal above safe: `internal/infrastructure/database`'s
// `TestTheContentPolicyCodeMatchesTheJobVocabulary` asserts this equals `job.CategoryContentPolicy`,
// so a respelling in the job domain fails a test rather than silently stopping the safety rule from
// firing.
const ContentPolicyErrorCode = contentPolicyErrorCode
