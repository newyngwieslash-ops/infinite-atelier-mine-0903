package database

import (
	"context"

	appconsistency "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/consistency"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/consistency"
)

// StoryboardConsistencyChecker is the deterministic ruleset for the production stages, over the
// database.
//
// # Why the checker is composed here rather than in the application layer
//
// The rules need four reads that live in four repositories — the board's rows, the script's
// structure, the assets a row cites, and the story state a character is in — and the application
// layer's services do not all expose them: `UsagesForConsumer`, `CostumeStateAt` and
// `StoryEventParticipantsFor` are storage questions with no domain rule attached, and adding
// pass-through methods to three services for one caller would grow their public surfaces to serve a
// rule. The infrastructure layer is where those tables already are.
//
// What this type does NOT contain is any rule. It wires the ports; the rules are in the application
// package, where they can be read and tested without a database.
//
// # Two rulesets, one checker
//
// WP-11 added the FINAL ruleset and it is dispatched from this same switch, for the reason the
// `Check` port's signature already anticipated: the stage machine asks one object about a stage, and
// a second checker registered somewhere else would be a second place to look when a stage's rules
// appear not to run. The two rulesets share nothing but their finding type, so each keeps its own
// type and its own `Options`; this struct holds both.
type StoryboardConsistencyChecker struct {
	checker *appconsistency.Checker
	// final is the Final Ruleset of AGENT_CONTRACTS section 11.4, nil in a build composed without a
	// final reader — in which case `final_episode` returns no findings rather than failing, which is
	// the same answer every stage this build has no rules for gives.
	final *appconsistency.FinalRuleset
}

// NewStoryboardConsistencyChecker builds the checker over the repositories.
//
// The final ruleset is composed from the SAME connection, so a build either has both rulesets or
// neither. It takes no options today: `FinalRuleset`'s bounds are its defaults, which are PRD
// FR-080's (4K at 60fps) and a one-kilobyte floor for a file that would otherwise be a placeholder.
// A build that wanted different bounds would add an options parameter here rather than a second
// constructor.
func NewStoryboardConsistencyChecker(
	storyboard appconsistency.StoryboardReader,
	assets appconsistency.AssetReader,
	script appconsistency.ScriptReaderSource,
	story appconsistency.StoryStateReader,
) *StoryboardConsistencyChecker {
	return &StoryboardConsistencyChecker{
		checker: appconsistency.NewChecker(appconsistency.Options{
			Storyboard: storyboard,
			Assets:     assets,
			Script:     script,
			Story:      story,
		}),
		final: nil,
	}
}

// WithFinalRuleset returns the same checker with the Final Ruleset attached.
//
// A separate method rather than another constructor parameter, because the two call sites that build
// this checker for a STORYBOARD's rules have no connection to hand it and no business owning one: the
// rulesets are independent, and a signature that made them look like a pair would invite a caller to
// pass one repository where the other was meant.
func (c *StoryboardConsistencyChecker) WithFinalRuleset(reader appconsistency.FinalReader) *StoryboardConsistencyChecker {
	if c == nil {
		return c
	}
	c.final = appconsistency.NewFinalRuleset(appconsistency.FinalOptions{Reader: reader})
	return c
}

// Check runs the rules that apply to one stage's artifact.
//
// The stage is a string rather than a typed constant because the pipeline that calls this is generic
// across stages, and a switch here is what maps a stage to its ruleset. Two stages have rules today:
// `storyboard_table`, whose rules are AGENT_CONTRACTS section 11.3's, and `final_episode`, whose
// rules are section 11.4's. The asset and director stages' rules are stated in sections 11.1 and 11.2
// and neither has an artifact whose mechanical half this build can check without inventing vocabulary
// the specification does not give. An unknown stage returns no findings, which the pipeline treats as
// "nothing mechanical to say" rather than as an error.
func (c *StoryboardConsistencyChecker) Check(ctx context.Context, stage, artifactVersionID string) ([]consistency.Finding, error) {
	if c == nil || c.checker == nil {
		return nil, nil
	}
	if artifactVersionID == "" {
		return nil, nil
	}
	switch stage {
	case "storyboard_table":
		return c.checker.CheckStoryboard(ctx, artifactVersionID)
	case "final_episode":
		// The artifact a final stage reports is the EPISODE rather than a version, which is what
		// `stagepipeline.StageAgents.ArtifactType` states for the stage: an episode is what the
		// review's subject is, because the clause is about the assembled film rather than about one
		// artifact of it.
		if c.final == nil {
			return nil, nil
		}
		return c.final.CheckEpisode(ctx, artifactVersionID)
	default:
		return nil, nil
	}
}

// The compile-time proof that this satisfies the pipeline's checker port.
//
// The first version of this assertion was `var _ = appconsistency.NewChecker` — a FUNCTION VALUE,
// which proves nothing at all: it compiles whatever `Check`'s signature is. An independent review
// caught it. What follows is the real assertion, against a locally declared interface with the
// pipeline's own shape, which fails to compile the moment the two drift.
type stageCheckerPort interface {
	Check(ctx context.Context, stage string, artifactVersionID string) ([]consistency.Finding, error)
}

var _ stageCheckerPort = (*StoryboardConsistencyChecker)(nil)

// stageCheckStages documents which stages this build has rules for, so a reader asking "is my stage
// covered" finds the answer rather than a switch they have to read.
//
// It must match the switch in `Check` exactly, and `TestCheckedStagesMatchTheSwitch` is what keeps
// the two from drifting: a list that named a stage the switch does not handle would tell a reader
// their stage is covered when it is not, which is worse than no list at all.
var stageCheckStages = []string{"storyboard_table", "final_episode"}

// CheckedStages lists the stages the deterministic rules cover.
func CheckedStages() []string {
	covered := make([]string, len(stageCheckStages))
	copy(covered, stageCheckStages)
	return covered
}
