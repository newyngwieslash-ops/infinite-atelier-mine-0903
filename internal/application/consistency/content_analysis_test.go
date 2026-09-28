package consistency

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/consistency"
)

// content_analysis_test.go is RP-07.2's port contract: the analyser's report
// becomes STRUCTURED findings with located evidence, "not analysed" is its
// own visible finding (never a clean bill), and one unreadable file does not
// fail the whole review.

// rp07FactsReader is the scripted FinalReader the content tests drive.
type rp07FactsReader struct {
	shots     []FinalShot
	episodeID string
}

func (r rp07FactsReader) FinalFacts(_ context.Context, episodeID string) (FinalFacts, error) {
	return FinalFacts{Shots: r.shots, EpisodeID: episodeID}, nil
}

// rp07Analyzer is a scripted ContentAnalyzer the tests drive.
type rp07Analyzer struct {
	reports map[string]ContentReport
	found   map[string]bool
	errs    map[string]error
	calls   int
}

func (a *rp07Analyzer) AnalyzeFile(_ context.Context, fileHash string) (ContentReport, bool, error) {
	a.calls++
	if err, ok := a.errs[fileHash]; ok {
		return ContentReport{}, false, err
	}
	if found, ok := a.found[fileHash]; ok && !found {
		return ContentReport{}, false, nil
	}
	return a.reports[fileHash], true, nil
}

// TestRP07ContentFindingsCarryLocatedEvidence proves a black/silent report
// becomes MAJOR findings whose evidence names the exact time ranges.
func TestRP07ContentFindingsCarryLocatedEvidence(t *testing.T) {
	analyzer := &rp07Analyzer{
		reports: map[string]ContentReport{
			"hash-black": {BlackRanges: [][2]int{{1000, 2500}}, SilentRanges: [][2]int{{500, 900}}},
		},
		found: map[string]bool{"hash-black": true},
	}
	ruleset := &FinalRuleset{reader: rp07FactsReader{
		shots: []FinalShot{{
			Ordinal: 1, ItemID: "item-1", ShotID: "shot-1",
			MediaVersionID: "mv-1", MediaHash: "hash-black", MediaKind: "video",
		}},
		episodeID: "ep-1",
	}}
	ruleset.AttachContentAnalyzer(analyzer)

	findings, err := ruleset.CheckEpisode(context.Background(), "ep-1")
	if err != nil {
		t.Fatalf("CheckEpisode: %v", err)
	}
	var black, silence *consistency.Finding
	for i := range findings {
		switch findings[i].Rule {
		case RuleContentBlackFrames:
			black = &findings[i]
		case RuleContentSilence:
			silence = &findings[i]
		}
	}
	if black == nil || silence == nil {
		t.Fatalf("findings = %+v, want black-frame and silence rules", findings)
	}
	if !strings.Contains(black.Problem, "1 black window") {
		t.Fatalf("black finding = %q", black.Problem)
	}
	if len(black.Evidence) != 1 || black.Evidence[0].Ref != "1000-2500ms" {
		t.Fatalf("black evidence = %+v, want the located range", black.Evidence)
	}
	if black.Severity != consistency.SeverityMajor {
		t.Fatalf("black severity = %v, want major (reviewer decides, not auto-block)", black.Severity)
	}
}

// TestRP07UnanalysedIsVisibleNotClean proves a missing engine produces its
// OWN finding — the state a machine without ffmpeg is in must not read as
// "no black frames found".
func TestRP07UnanalysedIsVisibleNotClean(t *testing.T) {
	analyzer := &rp07Analyzer{
		found: map[string]bool{"hash-x": false},
	}
	ruleset := &FinalRuleset{reader: rp07FactsReader{
		shots: []FinalShot{{
			Ordinal: 1, ItemID: "item-1", ShotID: "shot-1",
			MediaVersionID: "mv-1", MediaHash: "hash-x", MediaKind: "video",
		}},
		episodeID: "ep-1",
	}}
	ruleset.AttachContentAnalyzer(analyzer)

	findings, err := ruleset.CheckEpisode(context.Background(), "ep-1")
	if err != nil {
		t.Fatalf("CheckEpisode: %v", err)
	}
	sawNotAnalysed := false
	for _, finding := range findings {
		if finding.Rule == RuleContentNotAnalysed {
			sawNotAnalysed = true
			if !strings.Contains(finding.Problem, "1 approved video") {
				t.Fatalf("unanalysed finding = %q", finding.Problem)
			}
		}
	}
	if !sawNotAnalysed {
		t.Fatal("the not-analysed state was invisible")
	}
}

// TestRP07UnreadableFileIsAFindingNotAFailure proves one bad file names that
// file and the review continues.
func TestRP07UnreadableFileIsAFindingNotAFailure(t *testing.T) {
	analyzer := &rp07Analyzer{
		errs: map[string]error{"hash-broken": fmt.Errorf("store miss")},
		found: map[string]bool{
			"hash-good": true,
		},
		reports: map[string]ContentReport{
			"hash-good": {},
		},
	}
	ruleset := &FinalRuleset{reader: rp07FactsReader{
		shots: []FinalShot{
			{Ordinal: 1, ItemID: "item-1", ShotID: "shot-1", MediaVersionID: "mv-1", MediaHash: "hash-broken", MediaKind: "video"},
			{Ordinal: 2, ItemID: "item-2", ShotID: "shot-2", MediaVersionID: "mv-2", MediaHash: "hash-good", MediaKind: "video"},
		},
		episodeID: "ep-1",
	}}
	ruleset.AttachContentAnalyzer(analyzer)

	findings, err := ruleset.CheckEpisode(context.Background(), "ep-1")
	if err != nil {
		t.Fatalf("the review failed over one unreadable file: %v", err)
	}
	sawOpenFinding := false
	for _, finding := range findings {
		if finding.EntityID == "item-1" && finding.Rule == RuleMediaFilePresent {
			sawOpenFinding = true
		}
	}
	if !sawOpenFinding {
		t.Fatalf("the unreadable file was not located: %+v", findings)
	}
}

// TestRP07NilAnalyzerKeepsMetadataOnlyReview proves the port is optional and
// a build without it produces NO content findings (the metadata rules stand
// alone).
func TestRP07NilAnalyzerKeepsMetadataOnlyReview(t *testing.T) {
	ruleset := &FinalRuleset{reader: rp07FactsReader{
		shots: []FinalShot{{
			Ordinal: 1, ItemID: "item-1", ShotID: "shot-1",
			MediaVersionID: "mv-1", MediaHash: "hash-1", MediaKind: "video",
		}},
		episodeID: "ep-1",
	}}
	findings, err := ruleset.CheckEpisode(context.Background(), "ep-1")
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range findings {
		if finding.Rule == RuleContentBlackFrames || finding.Rule == RuleContentSilence || finding.Rule == RuleContentNotAnalysed {
			t.Fatalf("a ruleset with no analyser produced a content finding: %+v", finding)
		}
	}
}
