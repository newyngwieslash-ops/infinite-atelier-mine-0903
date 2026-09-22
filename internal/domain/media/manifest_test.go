package media

import (
	"strings"
	"testing"
	"time"
)

// manifest_test.go covers the document AC-MEDIA-003's "manifest traceability" is graded on.
//
// The rules that matter are about COMPLETENESS rather than about shape: a manifest that parses but
// cites no script version cannot trace a frame to anything, and one that cites nothing it was made
// from describes a film with no inputs.

// goodManifest builds a manifest that satisfies every rule, so a test states only the field it is
// about.
func goodManifest() Manifest {
	return Manifest{
		SchemaVersion:       ManifestSchemaVersion,
		EpisodeID:           "episode-1",
		Quality:             string(QualityFinal),
		Width:               1920,
		Height:              1080,
		FPS:                 DefaultExportFPS,
		SubtitleMode:        "sidecar",
		DurationMS:          12000,
		Engine:              "ffmpeg 5.1.1",
		ExportVersionNumber: 1,
		CreatedAt:           time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		References: []ManifestReference{
			{Kind: RefScript, ID: "script-v3", Hash: "aa"},
			{Kind: RefDirectorPlan, ID: "plan-v2", Hash: "bb"},
			{Kind: RefStoryboard, ID: "board-v1", Hash: "cc"},
			{Kind: RefPanel, ID: "panel-1", Hash: "dd", ShotID: "shot-1", Ordinal: 1, Label: "1"},
			{Kind: RefPanel, ID: "panel-2", Hash: "ee", ShotID: "shot-2", Ordinal: 2, Label: "2"},
			{Kind: RefSubtitleTrack, ID: "track-v1"},
		},
	}
}

func TestManifestAcceptsACompleteOne(t *testing.T) {
	if err := goodManifest().Validate(); err != nil {
		t.Fatalf("a complete manifest was refused: %v", err)
	}
}

// TestManifestRefusesWhatCannotBeTraced is the completeness rule.
//
// Each case is a manifest that PARSES and cannot answer the question AC-MEDIA-003 asks. That is the
// distinction that matters here: a shape check would accept all of them.
func TestManifestRefusesWhatCannotBeTraced(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Manifest)
	}{
		{"an unknown schema version", func(m *Manifest) { m.SchemaVersion = 99 }},
		{"no episode", func(m *Manifest) { m.EpisodeID = "" }},
		{"no frame size", func(m *Manifest) { m.Width = 0 }},
		{"an odd frame size", func(m *Manifest) { m.Width = 1921 }},
		{"an unknown quality", func(m *Manifest) { m.Quality = "master" }},
		{"no references at all", func(m *Manifest) { m.References = nil }},
		// The two that make traceability true: a film that cannot name what it renders, or what
		// ordered it, cannot be traced to anything.
		{"no script version", func(m *Manifest) {
			m.References = []ManifestReference{{Kind: RefStoryboard, ID: "board-v1"}}
		}},
		{"no storyboard version", func(m *Manifest) {
			m.References = []ManifestReference{{Kind: RefScript, ID: "script-v3"}}
		}},
		// A film with no frames is not a film: the references name what it was MADE from.
		{"no frames or assets", func(m *Manifest) {
			m.References = []ManifestReference{
				{Kind: RefScript, ID: "script-v3"},
				{Kind: RefStoryboard, ID: "board-v1"},
				{Kind: RefDirectorPlan, ID: "plan-v2"},
			}
		}},
		{"a reference with no id", func(m *Manifest) {
			m.References = append(m.References, ManifestReference{Kind: RefPanel})
		}},
		{"an unknown reference kind", func(m *Manifest) {
			m.References = append(m.References, ManifestReference{Kind: "timeline", ID: "t1"})
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			manifest := goodManifest()
			testCase.change(&manifest)
			if err := manifest.Validate(); err == nil {
				t.Fatalf("%s was accepted", testCase.name)
			}
		})
	}
}

// TestManifestRoundTripsThroughItsEncoding is the stored form.
//
// The manifest travels as a JSON document in the export row, so what matters is that decoding gives
// back the same facts — including the two place-holding fields a reader uses to find a frame in a
// list.
func TestManifestRoundTripsThroughItsEncoding(t *testing.T) {
	original := goodManifest()
	document, err := original.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	// The document names versions rather than files: that is what makes it a manifest rather than an
	// inventory, and it is the distinction ADR-0015 section 8 records against the archive's own.
	if !strings.Contains(document, "script_version") || !strings.Contains(document, "storyboard_version") {
		t.Fatalf("the document does not name artifact kinds:\n%s", document)
	}
	decoded, err := DecodeManifest(document)
	if err != nil {
		t.Fatalf("DecodeManifest: %v", err)
	}
	if decoded.SchemaVersion != original.SchemaVersion || decoded.EpisodeID != original.EpisodeID {
		t.Fatalf("the header round tripped to %+v", decoded)
	}
	if decoded.Width != original.Width || decoded.Height != original.Height || decoded.FPS != original.FPS {
		t.Fatalf("the recipe round tripped to %+v", decoded)
	}
	if decoded.DurationMS != original.DurationMS || decoded.Engine != original.Engine {
		t.Fatalf("the outcome round tripped to %+v", decoded)
	}
	if len(decoded.References) != len(original.References) {
		t.Fatalf("the references round tripped to %d of %d", len(decoded.References), len(original.References))
	}
	for index, want := range original.References {
		got := decoded.References[index]
		if got.Kind != want.Kind || got.ID != want.ID || got.Hash != want.Hash {
			t.Fatalf("reference %d round tripped to %+v, want %+v", index, got, want)
		}
		// The display fields, which are what a reader uses to find a frame in a list.
		if got.ShotID != want.ShotID || got.Ordinal != want.Ordinal || got.Label != want.Label {
			t.Fatalf("reference %d lost its placement: %+v", index, got)
		}
	}
	// ReferencesOf keeps the order and filters by kind, which is how a checker asks "which panels".
	panels := decoded.ReferencesOf(RefPanel)
	if len(panels) != 2 || panels[0].ID != "panel-1" || panels[1].ID != "panel-2" {
		t.Fatalf("ReferencesOf returned %+v", panels)
	}
	if tracks := decoded.ReferencesOf(RefSubtitleTrack); len(tracks) != 1 || tracks[0].ID != "track-v1" {
		t.Fatalf("ReferencesOf returned %+v for the track", tracks)
	}
	if none := decoded.ReferencesOf(RefAssetVersion); len(none) != 0 {
		t.Fatalf("ReferencesOf returned %+v for a kind with none", none)
	}
}

// TestAnUnreadableManifestIsRefusedRatherThanEmpty is the rule that keeps a check honest.
//
// The caller is about to compare the manifest against what is currently approved. An empty manifest
// would compare as "nothing to check", which reads as a clean export and is the exact opposite of the
// truth — so a document that will not parse is a refusal.
func TestAnUnreadableManifestIsRefusedRatherThanEmpty(t *testing.T) {
	for _, document := range []string{"", "   ", "{not json", "[]", "null"} {
		if _, err := DecodeManifest(document); err == nil {
			t.Fatalf("the document %q decoded to a manifest", document)
		}
	}
	// And a well-formed one decodes, so the refusals above are not a function that never succeeds.
	if _, err := DecodeManifest(`{"schemaVersion":1,"episodeId":"e","references":[]}`); err != nil {
		t.Fatalf("a well-formed document was refused: %v", err)
	}
}

// TestEncodeRefusesAnIncompleteManifest keeps the two directions symmetric: a document that could not
// be validated is not written, so a row can never hold a manifest that fails its own rules.
func TestEncodeRefusesAnIncompleteManifest(t *testing.T) {
	broken := goodManifest()
	broken.References = nil
	if _, err := broken.Encode(); err == nil {
		t.Fatal("an incomplete manifest was encoded")
	}
}

// TestResolutionForQualityPicksTheDocumentedSizes covers FR-080's three shapes.
func TestResolutionForQualityPicksTheDocumentedSizes(t *testing.T) {
	// A preview is the small frame whatever the final will be: FR-080's two qualities are about how
	// long a render takes, and a preview that cost the same as the deliverable would not be one.
	for _, orientation := range []string{"landscape", "portrait", "square"} {
		if got := ResolutionForQuality(QualityPreview, orientation); got != ResolutionPreview {
			t.Fatalf("a preview in %s is %+v", orientation, got)
		}
	}
	if got := ResolutionForQuality(QualityFinal, "landscape"); got != ResolutionLandscape {
		t.Fatalf("a landscape final is %+v", got)
	}
	if got := ResolutionForQuality(QualityFinal, "portrait"); got != ResolutionPortrait {
		t.Fatalf("a portrait final is %+v", got)
	}
	// Every named size must pass its own validation, or the defaults would be unusable.
	for _, resolution := range []Resolution{ResolutionPreview, ResolutionLandscape, ResolutionPortrait, ResolutionSquare} {
		if err := resolution.Validate(); err != nil {
			t.Fatalf("the named size %+v was refused: %v", resolution, err)
		}
	}
	// The orientation is DERIVED, so it cannot disagree with the numbers it came from.
	if ResolutionPortrait.Orientation() != "portrait" || ResolutionLandscape.Orientation() != "landscape" {
		t.Fatal("the orientation does not follow the frame's shape")
	}
	if ResolutionSquare.Orientation() != "square" {
		t.Fatal("a square frame is not reported as square")
	}
}

func TestResolutionValidateRefusesTheImpossible(t *testing.T) {
	cases := []Resolution{
		{Width: 0, Height: 1080},
		{Width: 1920, Height: 0},
		// Odd sizes fail at the H.264 encoder, which surfaces as a media error about a file rather
		// than about the number that caused it.
		{Width: 1921, Height: 1080},
		{Width: 1920, Height: 1081},
		{Width: 2, Height: 2},
		{Width: MaxExportWidth + 2, Height: 1080},
	}
	for _, resolution := range cases {
		if err := resolution.Validate(); err == nil {
			t.Fatalf("%+v was accepted", resolution)
		}
	}
}
