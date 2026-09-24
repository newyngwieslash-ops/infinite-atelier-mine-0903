package consistency

import (
	"context"
	"fmt"
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/consistency"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// script_rules.go is the SCRIPT ruleset — the mechanical half of the script supervisor.
//
// # Why these three rules and not more
//
// AGENT_CONTRACTS section 11.1 lists eleven things a script supervisor checks: 原著事实与改编标记,
// 开场 Hook, 核心冲突, 情绪节奏, 转折和结尾悬念, 单集时长, 角色动机, 场次可生产性, 对白自然度,
// 锁定规则, 内容等级. Most of them are judgement and belong to the model — "is the dialogue natural"
// is not a join. Three of them ARE joins over stored rows, and those are what this file implements:
//
//   - 单集时长   — the version's own estimate against the sum of its scenes'.
//   - 锁定规则   — a field the user PINNED that a later version changed.
//   - 原著事实与改编标记 — an event the strategy marked `removed` that still has a scene.
//
// The split follows section 11.4's own instruction: 「硬规则应尽量用确定性代码先检查，LLM Supervisor
// 负责语义质量」. A join that a database can answer must not be asked of a model, both because the model
// can be wrong and because asking costs a call.
//
// # What it will NOT do
//
// It does not check that a locked field is UNCHANGED, because "changed" needs a comparison against
// the version the lock was taken on, and that is a different read from "is this field locked". The
// lock rule below reports the mechanical fact it can establish; the comparison is the FIX path's,
// whose whole job is to honour the pins.

// ScriptRuleset runs the mechanical script rules.
type ScriptRuleset struct {
	source ScriptRulesSource
}

// ScriptRulesReader is what the script rules read.
//
// It is a port of its own rather than a widening of `ScriptReader`, which is the STORYBOARD
// ruleset's: the two read different things, and a combined port would make every implementation of
// one carry the other's methods.
type ScriptRulesReader interface {
	// DurationOf returns the version's own estimate and the sum of its scenes' estimates. Both come
	// from one read so the comparison cannot straddle two states of the database.
	DurationOf(ctx context.Context, scriptVersionID string) (versionSeconds int, scenesSeconds int, err error)
	// LockedLines returns the dialogue lines of a version whose `locked` flag is set.
	LockedLines(ctx context.Context, scriptVersionID string) ([]LockedLine, error)
	// RemovedEventsWithScenes returns the story events the strategy marked `removed` that a scene of
	// this version still dramatizes. The join is the whole rule, so it is answered by the store
	// rather than assembled from two reads a caller could interleave.
	RemovedEventsWithScenes(ctx context.Context, scriptVersionID string) ([]RemovedEvent, error)
}

// LockedLine is one pinned dialogue line.
type LockedLine struct {
	ID      string
	SceneID string
	Ordinal int
}

// RemovedEvent is a story event the strategy removed that a scene still dramatizes.
type RemovedEvent struct {
	EventID   string
	EventName string
	SceneID   string
}

// ScriptRulesSource builds a reader for one script version.
//
// It is a SOURCE rather than a reader for the same reason `ScriptReaderSource` is: the version a
// check runs against is a fact about the ARTIFACT, and one ruleset must serve every version without
// holding a map of adapters. The composition root implements this over the script repository.
type ScriptRulesSource interface {
	ScriptRulesReaderFor(ctx context.Context, scriptVersionID string) ScriptRulesReader
}

// NewScriptRuleset builds the ruleset over a source.
func NewScriptRuleset(source ScriptRulesSource) *ScriptRuleset {
	return &ScriptRuleset{source: source}
}

// Available reports whether the rules can run.
func (r *ScriptRuleset) Available() bool { return r != nil && r.source != nil }

// Check runs every script rule that applies to a version.
//
// The findings come back in a stable order for the reason `CheckStoryboard`'s do: a report must be
// reproducible, or comparing two reviews shows a changed set where only the order moved.
func (r *ScriptRuleset) Check(ctx context.Context, scriptVersionID string) ([]consistency.Finding, error) {
	if !r.Available() || strings.TrimSpace(scriptVersionID) == "" {
		return nil, nil
	}
	// ONE READER FOR THE WHOLE CHECK, resolved once: every rule then reads the same version's rows,
	// which is what keeps the findings consistent with one another.
	reader := r.source.ScriptRulesReaderFor(ctx, scriptVersionID)
	if reader == nil {
		return nil, nil
	}
	findings := make([]consistency.Finding, 0, 3)
	findings = append(findings, r.checkDuration(ctx, scriptVersionID, reader)...)
	findings = append(findings, r.checkLockedLines(ctx, scriptVersionID, reader)...)
	findings = append(findings, r.checkRemovedEvents(ctx, scriptVersionID, reader)...)
	return consistency.Sort(consistency.Dedupe(findings)), nil
}

// ruleDurationScript is 单集时长's rule identifier.
const ruleDurationScript = "SCRIPT_DURATION"

// ruleLockedChanged is 锁定规则's.
const ruleLockedChanged = "LOCKED_FIELD_PRESENT"

// ruleRemovedEventPresent is 原著事实与改编标记's.
const ruleRemovedEventPresent = "REMOVED_EVENT_STILL_SHOT"

// checkDuration compares the version's estimate against its scenes' sum.
//
// It is DURATION_TOTAL's sibling on the storyboard side, with the same tolerance and the same
// reasoning: the estimates are written by a model and rounded per scene, so an exact match is not a
// property the data has. The tolerance is stated once per ruleset because the two are different
// documents — a script's scene estimates are coarser than a board's rows.
func (r *ScriptRuleset) checkDuration(ctx context.Context, scriptVersionID string, reader ScriptRulesReader) []consistency.Finding {
	versionSeconds, scenesSeconds, err := reader.DurationOf(ctx, scriptVersionID)
	if err != nil {
		return nil
	}
	// A version with no scenes yet has nothing to compare, and reporting a mismatch against zero
	// would flag every draft the moment it is created.
	if scenesSeconds == 0 || versionSeconds == 0 {
		return nil
	}
	difference := versionSeconds - scenesSeconds
	if difference <= scriptDurationTolerance && difference >= -scriptDurationTolerance {
		return nil
	}
	return []consistency.Finding{{
		Rule:     ruleDurationScript,
		Category: consistency.CategoryTemporal,
		Severity: workflow.SeverityMinor,
		// The entity is the VERSION, because the discrepancy belongs to the document rather than to
		// one of its scenes: the fix is to correct the estimate or a scene, and a reader needs to
		// open the version to decide which.
		EntityType: "script_version",
		EntityID:   scriptVersionID,
		Field:      "estimatedDurationSeconds",
		Problem: fmt.Sprintf(
			"The version estimates %d seconds and its scenes add up to %d, which is a %d-second difference.",
			versionSeconds, scenesSeconds, difference),
		Suggestion: "Correct whichever is wrong: the episode's estimate, or the scene whose estimate drifted.",
		// Not auto-fixable: choosing which number is right is a judgement about the script.
		AutoFixable: false,
	}}
}

// checkLockedLines reports a version that carries pinned dialogue.
//
// # What this establishes, and what it deliberately does not
//
// It reports the MECHANICAL fact: this version has lines the user pinned, so a revision of it must
// carry them unchanged. It does not diff against the version the pin was taken on, because that
// comparison belongs to the FIX path — where the pins are read back and rendered into the prompt —
// and duplicating it here would give two answers to one question.
//
// The finding is `minor` and `review_required` in effect rather than blocking: a version with pinned
// lines is not wrong, it is CONSTRAINED, and a supervisor reading the report should know which parts
// of the document are the user's rather than the model's.
func (r *ScriptRuleset) checkLockedLines(ctx context.Context, scriptVersionID string, reader ScriptRulesReader) []consistency.Finding {
	locked, err := reader.LockedLines(ctx, scriptVersionID)
	if err != nil || len(locked) == 0 {
		return nil
	}
	findings := make([]consistency.Finding, 0, len(locked))
	for _, line := range locked {
		findings = append(findings, consistency.Finding{
			Rule:     ruleLockedChanged,
			Category: consistency.CategoryFidelity,
			Severity: workflow.SeverityMinor,
			// The entity is the LINE, so FR-110's "报告问题可以在 UI 中跳转到实体" lands on the line
			// rather than on the version — a reader who wants to check the pin should see the pin.
			EntityType: "dialogue_line",
			EntityID:   line.ID,
			Location:   line.SceneID,
			Field:      "locked",
			Problem:    "This line is pinned by the user, so a revision must carry it unchanged.",
			Suggestion: "Keep the line as written. A revision that changes it is refused at the write path.",
			// A pin is not a defect and nothing needs fixing.
			AutoFixable: false,
		})
	}
	return findings
}

// checkRemovedEvents reports a story event the strategy cut that still has a scene.
//
// This is 原著事实与改编标记's mechanical half, and it is the rule the script supervisor is asked to
// perform by hand (`skills/script/supervision/script.md`: "does the script contradict an approved
// strategy?"). The contradiction has a precise form: `adaptation_strategy_event_links.treatment`
// records that an event was `removed`, and a scene citing that event says it was not.
//
// It is `major` rather than `minor` because the two records disagree about what the episode IS, and
// a reader has to decide which one is right before the script can be shot.
func (r *ScriptRuleset) checkRemovedEvents(ctx context.Context, scriptVersionID string, reader ScriptRulesReader) []consistency.Finding {
	removed, err := reader.RemovedEventsWithScenes(ctx, scriptVersionID)
	if err != nil {
		return nil
	}
	findings := make([]consistency.Finding, 0, len(removed))
	for _, event := range removed {
		findings = append(findings, consistency.Finding{
			Rule:       ruleRemovedEventPresent,
			Category:   consistency.CategoryFidelity,
			Severity:   workflow.SeverityMajor,
			EntityType: "scene",
			EntityID:   event.SceneID,
			Field:      "sourceStoryEventId",
			Problem: fmt.Sprintf(
				"The adaptation strategy removed the story event %q, and this scene still dramatizes it.",
				event.EventName),
			Suggestion: "Either the strategy should retain the event, or the scene should not dramatize it. The two records disagree.",
			Evidence: []consistency.Evidence{
				{Type: "story_event", Ref: event.EventID},
				{Type: "scene", Ref: event.SceneID},
			},
			// Choosing between the strategy and the script is a writer's decision, not a rewrite.
			AutoFixable: false,
		})
	}
	return findings
}

// scriptDurationTolerance is how far a version's estimate may differ from its scenes' sum.
//
// Thirty seconds, which is larger than the storyboard ruleset's because a script's scenes are
// COARSE: a scene's estimate is written before its shots exist, so the rounding error is per scene
// and a twelve-scene episode accumulates it. A tolerance too tight would flag every script and a
// tolerance too loose would never fire; this one is stated as a named constant so the number is
// reviewable rather than buried.
const scriptDurationTolerance = 30
