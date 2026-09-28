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
	// The four reads the asset and continuity rules share, kept so `WithJobFailures` can rebuild the
	// checker with one more port: `NewChecker` returned a value whose fields are unexported, so a
	// method that wants to add a port has to hold what it was built from.
	storyboard appconsistency.StoryboardReader
	assets     appconsistency.AssetReader
	script     appconsistency.ScriptReaderSource
	story      appconsistency.StoryStateReader
	// final is the Final Ruleset of AGENT_CONTRACTS section 11.4, nil in a build composed without a
	// final reader — in which case `final_episode` returns no findings rather than failing, which is
	// the same answer every stage this build has no rules for gives.
	final *appconsistency.FinalRuleset
	// scripts is the SCRIPT ruleset of AGENT_CONTRACTS section 11.1, nil in a build composed without
	// a script repository — in which case `script_generation` returns no findings rather than failing.
	scripts *appconsistency.ScriptRuleset
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
		storyboard: storyboard,
		assets:     assets,
		script:     script,
		story:      story,
		final:      nil,
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

// WithContentAnalyzer returns the same checker with RP-07.2's content
// analyzer attached to the final ruleset. It is a separate method for the
// same reason WithFinalRuleset is: the analyzer is an OPTIONAL capability a
// build without ffmpeg does not carry, and the report states its absence
// rather than the rules going quiet.
func (c *StoryboardConsistencyChecker) WithContentAnalyzer(analyzer appconsistency.ContentAnalyzer) *StoryboardConsistencyChecker {
	if c == nil || c.final == nil {
		return c
	}
	c.final.AttachContentAnalyzer(analyzer)
	return c
}

// WithJobFailures returns the same checker with the safety rule's job read attached.
//
// A separate method for the reason the other two are: this read is over the JOB table, which has
// nothing to do with a board's rows or a script's structure, and a constructor parameter would
// invite a caller to think the rulesets came as a set. Without it the asset rules still run — the
// safety rule is the one that goes quiet, which is the documented answer for a rule whose read is
// absent.
func (c *StoryboardConsistencyChecker) WithJobFailures(reader appconsistency.JobFailureReader) *StoryboardConsistencyChecker {
	// The reader is handed to the CHECKER rather than held here, because the rule it serves belongs
	// to the asset ruleset: this type wires ports, and which ruleset consumes one is the application
	// layer's business.
	if c == nil || c.checker == nil || reader == nil {
		return c
	}
	c.checker = appconsistency.NewChecker(appconsistency.Options{
		Storyboard: c.storyboard,
		Assets:     c.assets,
		Script:     c.script,
		Story:      c.story,
		Jobs:       reader,
	})
	return c
}

// WithScriptRuleset returns the same checker with the SCRIPT ruleset attached.
//
// A separate method for the reason `WithFinalRuleset` is: the call sites that build this checker for
// one ruleset have no connection to hand another, and a signature that made the rulesets look like a
// pair would invite a caller to pass one repository where the other was meant.
func (c *StoryboardConsistencyChecker) WithScriptRuleset(source appconsistency.ScriptRulesSource) *StoryboardConsistencyChecker {
	if c == nil {
		return c
	}
	c.scripts = appconsistency.NewScriptRuleset(source)
	return c
}

// Check runs the rules that apply to one stage's artifact.
//
// The stage is a string rather than a typed constant because the pipeline that calls this is generic
// across stages, and a switch here is what maps a stage to its ruleset. Three stages have rules today:
// `script_generation`, whose rules are AGENT_CONTRACTS section 11.1's mechanical half;
// `storyboard_table`, whose rules are section 11.3's; and `final_episode`, whose rules are section
// 11.4's. The asset and director stages' rules are stated in sections 11.1 and 11.2 and neither has an
// artifact whose mechanical half this build can check without inventing vocabulary the specification
// does not give — section 11.2's 场景时间/天气 has no column, which is the reason recorded in STATUS
// section 0k. An unknown stage returns no findings, which the pipeline treats as "nothing mechanical to
// say" rather than as an error.
func (c *StoryboardConsistencyChecker) Check(ctx context.Context, stage, artifactVersionID string) ([]consistency.Finding, error) {
	if c == nil || c.checker == nil {
		return nil, nil
	}
	if artifactVersionID == "" {
		return nil, nil
	}
	switch stage {
	case "script_generation":
		// The script ruleset's rules are AGENT_CONTRACTS section 11.1's mechanical half. The artifact
		// a script stage reports is the `script_version` it wrote, which is what the ruleset reads.
		if c.scripts == nil {
			return nil, nil
		}
		return c.scripts.Check(ctx, artifactVersionID)
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
var stageCheckStages = []string{"script_generation", "storyboard_table", "final_episode"}

// CheckedStages lists the stages the deterministic rules cover.
func CheckedStages() []string {
	covered := make([]string, len(stageCheckStages))
	copy(covered, stageCheckStages)
	return covered
}
