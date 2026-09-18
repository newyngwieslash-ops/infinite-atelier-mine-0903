package storyboard

import (
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// TestShotCountBounds covers the fan-out guard. The two boundary values matter
// most: max must be accepted and max+1 must not, or the bound is off by one and
// the writer either refuses a storable version or stores one the schema's
// readers were sized for.
func TestShotCountBounds(t *testing.T) {
	cases := []struct {
		name  string
		count int
		ok    bool
	}{
		{"zero shots", 0, true},
		{"one shot", 1, true},
		{"the maximum", MaxShotCountPerVersion, true},
		{"one over the maximum", MaxShotCountPerVersion + 1, false},
		{"negative", -1, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := ValidateShotCount(testCase.count)
			if testCase.ok && err != nil {
				t.Fatalf("%d shots must be storable: %v", testCase.count, err)
			}
			if !testCase.ok && err == nil {
				t.Fatalf("%d shots was accepted, which is outside the version bound", testCase.count)
			}
		})
	}
}

// TestVersionVocabularyIsTheSharedOne proves every versioned aggregate in this
// package routes through the versioning package instead of carrying a second
// copy of section 2.5's set. A locally invented status would compile and pass a
// narrower test, so this checks the documented set directly.
func TestVersionVocabularyIsTheSharedOne(t *testing.T) {
	documented := []versioning.Status{
		versioning.StatusDraft, versioning.StatusCandidate, versioning.StatusUnderReview,
		versioning.StatusApproved, versioning.StatusRejected, versioning.StatusSuperseded,
		versioning.StatusDeprecated, versioning.StatusStale,
	}
	for _, status := range documented {
		if !versioning.IsValidStatus(status) {
			t.Fatalf("documented status %q is not recognised", status)
		}
		plan := DirectorPlanVersion{EpisodeID: "ep-1", VersionNumber: 1, Status: status, ScriptVersionID: "sv-1", CreatedByType: versioning.CreatedByUser}
		if err := plan.Validate(); err != nil {
			t.Fatalf("a director plan in status %s was rejected: %v", status, err)
		}
		version := StoryboardVersion{StoryboardID: "sb-1", VersionNumber: 1, Status: status, ScriptVersionID: "sv-1", DirectorPlanVersionID: "dp-1", CreatedByType: versioning.CreatedByUser}
		if err := version.Validate(); err != nil {
			t.Fatalf("a storyboard version in status %s was rejected: %v", status, err)
		}
		item := StoryboardItem{StoryboardVersionID: "sbv-1", ShotID: "shot-1", Ordinal: 1, Status: status, Revision: 1}
		if err := item.Validate(); err != nil {
			t.Fatalf("a storyboard item in status %s was rejected: %v", status, err)
		}
		panel := StoryboardPanelVersion{StoryboardItemID: "item-1", VersionNumber: 1, Status: status, CreatedByType: versioning.CreatedByUser}
		if err := panel.Validate(); err != nil {
			t.Fatalf("a panel version in status %s was rejected: %v", status, err)
		}
	}
	for _, rejected := range []versioning.Status{"", "review", "executed", "Approved", "passed"} {
		if versioning.IsValidStatus(rejected) {
			t.Fatalf("undocumented status %q is accepted", rejected)
		}
		plan := DirectorPlanVersion{EpisodeID: "ep-1", VersionNumber: 1, Status: rejected, ScriptVersionID: "sv-1", CreatedByType: versioning.CreatedByUser}
		if err := plan.Validate(); err == nil {
			t.Fatalf("a director plan in undocumented status %q was accepted", rejected)
		}
	}
}

// TestDirectorPlanValidate covers §9.1's required references.
func TestDirectorPlanValidate(t *testing.T) {
	base := DirectorPlanVersion{
		EpisodeID:       "ep-1",
		VersionNumber:   1,
		Status:          versioning.StatusDraft,
		ScriptVersionID: "sv-1",
		CreatedByType:   versioning.CreatedByUser,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed director plan was rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*DirectorPlanVersion)
	}{
		{"no episode", func(p *DirectorPlanVersion) { p.EpisodeID = "  " }},
		{"zero version number", func(p *DirectorPlanVersion) { p.VersionNumber = 0 }},
		{"unknown status", func(p *DirectorPlanVersion) { p.Status = "ready" }},
		{"no script version", func(p *DirectorPlanVersion) { p.ScriptVersionID = "" }},
		{"unknown producer", func(p *DirectorPlanVersion) { p.CreatedByType = "robot" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			plan := base
			testCase.mutate(&plan)
			if err := plan.Validate(); err == nil {
				t.Fatal("a malformed director plan was accepted")
			}
		})
	}
}

// TestStoryboardValidate covers the identity row, whose only invariant is the
// revision the schema also checks.
func TestStoryboardValidate(t *testing.T) {
	base := Storyboard{EpisodeID: "ep-1", Revision: 1}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed storyboard was rejected: %v", err)
	}
	// current_version_id is empty until a version is approved, which §9.2 marks
	// nullable and the schema stores as an empty default.
	if err := base.Validate(); err != nil {
		t.Fatalf("a storyboard with no current version must be valid: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*Storyboard)
	}{
		{"no episode", func(s *Storyboard) { s.EpisodeID = "" }},
		{"zero revision", func(s *Storyboard) { s.Revision = 0 }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			value := base
			testCase.mutate(&value)
			if err := value.Validate(); err == nil {
				t.Fatal("a malformed storyboard was accepted")
			}
		})
	}
}

// TestStoryboardVersionValidate covers §9.3's two mandatory upstream references.
func TestStoryboardVersionValidate(t *testing.T) {
	base := StoryboardVersion{
		StoryboardID:          "sb-1",
		VersionNumber:         1,
		Status:                versioning.StatusDraft,
		ScriptVersionID:       "sv-1",
		DirectorPlanVersionID: "dp-1",
		CreatedByType:         versioning.CreatedByUser,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed storyboard version was rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*StoryboardVersion)
	}{
		{"no storyboard", func(v *StoryboardVersion) { v.StoryboardID = "" }},
		{"zero version number", func(v *StoryboardVersion) { v.VersionNumber = 0 }},
		{"unknown status", func(v *StoryboardVersion) { v.Status = "pending" }},
		{"no script version", func(v *StoryboardVersion) { v.ScriptVersionID = "   " }},
		{"no director plan", func(v *StoryboardVersion) { v.DirectorPlanVersionID = "" }},
		{"unknown producer", func(v *StoryboardVersion) { v.CreatedByType = "service" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			version := base
			testCase.mutate(&version)
			if err := version.Validate(); err == nil {
				t.Fatal("a malformed storyboard version was accepted")
			}
		})
	}
}

// TestValidateAgainstRequiresTheRecordedInputs covers §9.3's provenance rule:
// the version's own references are what it means, so a caller supplying a
// different pair is refused rather than believed.
func TestValidateAgainstRequiresTheRecordedInputs(t *testing.T) {
	version := StoryboardVersion{
		StoryboardID:          "sb-1",
		VersionNumber:         1,
		Status:                versioning.StatusApproved,
		ScriptVersionID:       "sv-1",
		DirectorPlanVersionID: "dp-1",
		CreatedByType:         versioning.CreatedByUser,
	}
	if err := version.ValidateAgainst("sv-1", "dp-1"); err != nil {
		t.Fatalf("the version's own inputs must match: %v", err)
	}
	if err := version.ValidateAgainst("sv-2", "dp-1"); err == nil {
		t.Fatal("a script version the storyboard was not built from was accepted")
	}
	if err := version.ValidateAgainst("sv-1", "dp-2"); err == nil {
		t.Fatal("a director plan the storyboard was not built from was accepted")
	}
	if err := version.ValidateAgainst("sv-2", "dp-2"); err == nil {
		t.Fatal("a wholly different input pair was accepted")
	}
	// An empty argument is not a wildcard: the caller must state what it thinks
	// the inputs are, and an absent claim cannot be verified.
	if err := version.ValidateAgainst("", "dp-1"); err == nil {
		t.Fatal("an empty script version argument was accepted as a match")
	}
	if err := version.ValidateAgainst("sv-1", ""); err == nil {
		t.Fatal("an empty director plan argument was accepted as a match")
	}

	// A version missing its own references cannot be validated against anything,
	// even something that looks like a match for the empty string.
	incomplete := version
	incomplete.ScriptVersionID = ""
	if err := incomplete.ValidateAgainst("", "dp-1"); err == nil {
		t.Fatal("a version with no recorded script version reported a match")
	}
	incomplete = version
	incomplete.DirectorPlanVersionID = ""
	if err := incomplete.ValidateAgainst("sv-1", ""); err == nil {
		t.Fatal("a version with no recorded director plan reported a match")
	}
}

// TestStoryboardItemValidate covers §9.4's uniqueness precondition and the
// column bounds the schema also enforces.
func TestStoryboardItemValidate(t *testing.T) {
	base := StoryboardItem{
		StoryboardVersionID: "sbv-1",
		ShotID:              "shot-1",
		Ordinal:             1,
		DurationSeconds:     4,
		Status:              versioning.StatusDraft,
		Revision:            1,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed storyboard item was rejected: %v", err)
	}
	// A zero duration is allowed: a still panel is a legal item and an unknown
	// duration is better represented as zero than as a guess.
	still := base
	still.DurationSeconds = 0
	if err := still.Validate(); err != nil {
		t.Fatalf("a still item must be storable: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*StoryboardItem)
	}{
		{"no storyboard version", func(i *StoryboardItem) { i.StoryboardVersionID = "" }},
		{"empty shot id", func(i *StoryboardItem) { i.ShotID = "" }},
		{"blank shot id", func(i *StoryboardItem) { i.ShotID = "   " }},
		{"ordinal below one", func(i *StoryboardItem) { i.Ordinal = 0 }},
		{"negative duration", func(i *StoryboardItem) { i.DurationSeconds = -1 }},
		{"unknown status", func(i *StoryboardItem) { i.Status = "executed" }},
		{"zero revision", func(i *StoryboardItem) { i.Revision = 0 }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			item := base
			testCase.mutate(&item)
			if err := item.Validate(); err == nil {
				t.Fatal("a malformed storyboard item was accepted")
			}
		})
	}
}

// TestStoryboardPanelVersionValidate covers §9.5's shape.
func TestStoryboardPanelVersionValidate(t *testing.T) {
	base := StoryboardPanelVersion{
		StoryboardItemID: "item-1",
		VersionNumber:    1,
		Status:           versioning.StatusCandidate,
		CreatedByType:    versioning.CreatedByUser,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed panel version was rejected: %v", err)
	}
	// No image is approved yet, which §9.5 marks nullable and the schema stores
	// as an empty default.
	if base.ApprovedImageAssetVersionID != "" {
		t.Fatal("a fresh panel version must not claim an approved image")
	}
	cases := []struct {
		name   string
		mutate func(*StoryboardPanelVersion)
	}{
		{"no storyboard item", func(p *StoryboardPanelVersion) { p.StoryboardItemID = "" }},
		{"zero version number", func(p *StoryboardPanelVersion) { p.VersionNumber = 0 }},
		{"unknown status", func(p *StoryboardPanelVersion) { p.Status = "queued" }},
		{"unknown producer", func(p *StoryboardPanelVersion) { p.CreatedByType = "robot" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			panel := base
			testCase.mutate(&panel)
			if err := panel.Validate(); err == nil {
				t.Fatal("a malformed panel version was accepted")
			}
		})
	}
}

// TestCanApproveImage covers §9.5's "approved image 必须属于该 Panel 的候选或经
// 用户明确关联". The negative cases are the point: approving an image the panel
// never offered would put an unreviewed picture in the final output.
func TestCanApproveImage(t *testing.T) {
	panel := StoryboardPanelVersion{
		StoryboardItemID: "item-1",
		VersionNumber:    1,
		Status:           versioning.StatusUnderReview,
		CreatedByType:    versioning.CreatedByUser,
	}
	candidates := []string{"asset-version-1", "asset-version-2"}

	t.Run("a candidate is approved", func(t *testing.T) {
		for _, candidate := range candidates {
			if err := panel.CanApproveImage(candidate, candidates); err != nil {
				t.Fatalf("candidate %s must be approvable: %v", candidate, err)
			}
		}
	})
	t.Run("an image outside the candidates is refused", func(t *testing.T) {
		if err := panel.CanApproveImage("asset-version-9", candidates); err == nil {
			t.Fatal("an image the panel never offered as a candidate was approved")
		}
	})
	t.Run("no candidates at all", func(t *testing.T) {
		if err := panel.CanApproveImage("asset-version-1", nil); err == nil {
			t.Fatal("an image was approved for a panel with no candidates")
		}
		if err := panel.CanApproveImage("asset-version-1", []string{}); err == nil {
			t.Fatal("an image was approved for a panel with an empty candidate list")
		}
	})
	t.Run("an empty image id is invalid input", func(t *testing.T) {
		for _, value := range []string{"", "   "} {
			err := panel.CanApproveImage(value, candidates)
			if err == nil {
				t.Fatalf("the empty image id %q was accepted", value)
			}
			domainErr, ok := AsError(err)
			if !ok {
				t.Fatalf("the refusal for %q is not a domain error: %v", value, err)
			}
			if domainErr.Category != CategoryInvalidInput {
				t.Fatalf("an unset image id is invalid input, got category %q", domainErr.Category)
			}
		}
	})
	t.Run("candidacy survives a longer list", func(t *testing.T) {
		// The check is membership, not position: the first and the last candidate
		// must both be approvable.
		long := []string{"a", "b", "c", "asset-version-1", "e"}
		if err := panel.CanApproveImage("asset-version-1", long); err != nil {
			t.Fatalf("a candidate later in the list must be approvable: %v", err)
		}
	})
}
