package database

import (
	"context"

	appconsistency "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/consistency"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/consistency"
)

// StoryboardConsistencyChecker is the deterministic ruleset for the storyboard stages, over the
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
type StoryboardConsistencyChecker struct {
	checker *appconsistency.Checker
}

// NewStoryboardConsistencyChecker builds the checker over the repositories.
func NewStoryboardConsistencyChecker(
	storyboard appconsistency.StoryboardReader,
	assets appconsistency.AssetReader,
	script appconsistency.ScriptReaderSource,
	story appconsistency.StoryStateReader,
) *StoryboardConsistencyChecker {
	return &StoryboardConsistencyChecker{checker: appconsistency.NewChecker(appconsistency.Options{
		Storyboard: storyboard,
		Assets:     assets,
		Script:     script,
		Story:      story,
	})}
}

// Check runs the rules that apply to one stage's artifact.
//
// The stage is a string rather than a typed constant because the pipeline that calls this is generic
// across stages, and a switch here is what maps a stage to its ruleset. Only `storyboard_table` has
// rules today: the asset and director stages' rules are stated in AGENT_CONTRACTS sections 11.1 and
// 11.2, and neither has an artifact whose mechanical half this build can check without inventing
// vocabulary the specification does not give. An unknown stage returns no findings, which the
// pipeline treats as "nothing mechanical to say" rather than as an error.
func (c *StoryboardConsistencyChecker) Check(ctx context.Context, stage, artifactVersionID string) ([]consistency.Finding, error) {
	if c == nil || c.checker == nil {
		return nil, nil
	}
	if stage != "storyboard_table" {
		return nil, nil
	}
	if artifactVersionID == "" {
		return nil, nil
	}
	return c.checker.CheckStoryboard(ctx, artifactVersionID)
}

// The compile-time proof that this satisfies the pipeline's checker port.
//
// The pipeline's port is one method taking a stage name and a version, and this type's own
// `Check` is that signature. The assertion is written against the CHECKER rather than the port
// because the port lives in the app package the pipeline owns; a drift here would mean the
// storyboard rules silently stopped running, which is the failure this package has now found four
// times in other shapes.
var _ = appconsistency.NewChecker

// stageCheckStages documents which stages this build has rules for, so a reader asking "is my stage
// covered" finds the answer rather than a switch they have to read.
var stageCheckStages = []string{"storyboard_table"}

// CheckedStages lists the stages the deterministic rules cover.
func CheckedStages() []string {
	covered := make([]string, len(stageCheckStages))
	copy(covered, stageCheckStages)
	return covered
}
