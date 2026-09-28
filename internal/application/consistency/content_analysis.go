package consistency

import (
	"context"
	"fmt"

	consistency "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/consistency"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// content_analysis.go is RP-07.2's application port: the CONTENT half of the
// final review (black frames, silent audio), run over the approved media a
// final review already reads.
//
// # The port, and why the ruleset does not run processes
//
// Decoding pixels and audio is a process-level concern; a final review is a
// read-model concern. The ruleset takes the ANALYZER as a port (nil-safe) and
// the composition root injects the FFmpeg adapter — the same dependency
// direction every engine consumer here uses. A build with no engine keeps the
// metadata-only review, and the REPORT says so (a
// CONTENT_NOT_ANALYSED finding) so a missing engine can never masquerade as
// "no black frames found".
//
// # The finding shape
//
// A black or silent window is not automatically an error — an intentional
// fade-out IS a black range. So the finding carries the LOCATED ranges as
// evidence, at MAJOR severity with a suggestion to review, and the reviewer
// decides. The deterministic half never blocks on content judgement alone.

// ContentReport is one file's content-level result, in the application's own
// vocabulary. The infrastructure adapter fills it from the engine's report.
type ContentReport struct {
	// BlackRanges are the [startMS, endMS) windows whose frames were black.
	BlackRanges [][2]int
	// SilentRanges are the [startMS, endMS) windows whose audio was silent.
	SilentRanges [][2]int
}

// ContentAnalyzer is the port the ruleset calls for one file's content
// report. found=false reports "not analysed" (no engine, unsupported kind) —
// a state the ruleset surfaces as its own finding, distinct from "analysed
// and clean".
type ContentAnalyzer interface {
	// AnalyzeFile runs the content analysis over ONE managed file. The
	// implementation resolves the hash to bytes/paths itself; the ruleset
	// never handles an OS path. The whole analysis shares ONE deadline the
	// implementation owns.
	AnalyzeFile(ctx context.Context, fileHash string) (ContentReport, bool, error)
}

// AttachContentAnalyzer installs the content port. Nil-safe: a ruleset
// without an analyzer keeps the metadata-only review.
func (f *FinalRuleset) AttachContentAnalyzer(analyzer ContentAnalyzer) {
	if f == nil {
		return
	}
	f.analyzer = analyzer
}

// checkContent runs the content rules over the shots' approved video media.
func (f *FinalRuleset) checkContent(ctx context.Context, facts FinalFacts) ([]consistency.Finding, error) {
	if f.analyzer == nil {
		return nil, nil
	}
	findings := []consistency.Finding{}
	unanalysed := 0
	for _, shot := range facts.Shots {
		if shot.MediaHash == "" || shot.MediaKind != "video" {
			continue
		}
		report, found, err := f.analyzer.AnalyzeFile(ctx, shot.MediaHash)
		if err != nil {
			// One unreadable file is a finding about THAT file, not a review
			// failure: the review continues, the finding names the shot.
			findings = append(findings, consistency.Finding{
				Rule:        RuleMediaFilePresent,
				Severity:    workflow.SeverityMajor,
				EntityType:  "storyboard_item",
				EntityID:    shot.ItemID,
				Location:    fmt.Sprintf("shot %d", shot.Ordinal),
				Field:       "media",
				Problem:     "The approved media could not be opened for content analysis.",
				Suggestion:  "Check that the file still exists in the store, then re-run the review.",
				AutoFixable: false,
			})
			continue
		}
		if !found {
			unanalysed++
			continue
		}
		if len(report.BlackRanges) > 0 {
			findings = append(findings, consistency.Finding{
				Rule:       RuleContentBlackFrames,
				Severity:   workflow.SeverityMajor,
				EntityType: "storyboard_item",
				EntityID:   shot.ItemID,
				Location:   fmt.Sprintf("shot %d", shot.Ordinal),
				Field:      "media",
				Problem:    fmt.Sprintf("The approved video carries %d black window(s): an intentional fade should be confirmed by a reviewer.", len(report.BlackRanges)),
				Suggestion: "Review the black windows; replace the take if the black is not intended.",
				Evidence:   timeRangeEvidence(report.BlackRanges),
			})
		}
		if len(report.SilentRanges) > 0 {
			findings = append(findings, consistency.Finding{
				Rule:       RuleContentSilence,
				Severity:   workflow.SeverityMajor,
				EntityType: "storyboard_item",
				EntityID:   shot.ItemID,
				Location:   fmt.Sprintf("shot %d", shot.Ordinal),
				Field:      "audio",
				Problem:    fmt.Sprintf("The approved video carries %d silent window(s): confirm the quiet is intended.", len(report.SilentRanges)),
				Suggestion: "Review the silent windows; attach the shot's audio if it is missing.",
				Evidence:   timeRangeEvidence(report.SilentRanges),
			})
		}
	}
	if unanalysed > 0 {
		findings = append(findings, consistency.Finding{
			Rule:        RuleContentNotAnalysed,
			Severity:    workflow.SeverityMinor,
			EntityType:  "episode",
			EntityID:    facts.EpisodeID,
			Location:    "content analysis",
			Field:       "engine",
			Problem:     fmt.Sprintf("%d approved video(s) were not content-analysed: no engine is available on this machine.", unanalysed),
			Suggestion:  "Install FFmpeg and re-run the final review for a content-level answer.",
			AutoFixable: false,
		})
	}
	return findings, nil
}

// timeRangeEvidence renders located windows as evidence rows.
func timeRangeEvidence(ranges [][2]int) []consistency.Evidence {
	out := make([]consistency.Evidence, 0, len(ranges))
	for _, r := range ranges {
		out = append(out, consistency.Evidence{Type: "time_range", Ref: fmt.Sprintf("%d-%dms", r[0], r[1])})
	}
	return out
}
