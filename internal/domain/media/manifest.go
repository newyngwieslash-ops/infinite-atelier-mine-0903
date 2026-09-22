package media

import (
	"encoding/json"
	"strings"
	"time"
)

// The export manifest, which is AC-MEDIA-003's "manifest traceability".
//
// # The question a manifest answers
//
// Not "what files are in this export" — that is the archive's job — but "which VERSIONS of which
// artifacts was this film made from". A frame a week later has to be defensible: a reader asks which
// script version produced shot 6, and the answer is either in the manifest or nowhere, because the
// script has been revised three times since.
//
// That is why every reference carries an ID AND a hash: the ID says what it was, and the hash says
// whether the row still holds the same bytes. DOMAIN_MODEL section 15.2's chain ends at this node,
// and a chain with a broken link is a frame nobody can explain.

// ManifestSchemaVersion is the document's own version.
//
// It is written into every manifest so a reader can tell which shape it is reading, and it is
// separate from the application's version because the two change for different reasons: a field
// added here bumps this, and a bug fixed elsewhere does not.
const ManifestSchemaVersion = 1

// ReferenceKind names what an export's input was.
type ReferenceKind string

const (
	// RefScript is the script version the film renders.
	RefScript ReferenceKind = "script_version"
	// RefDirectorPlan is the plan its framing came from.
	RefDirectorPlan ReferenceKind = "director_plan_version"
	// RefStoryboard is the board whose rows ordered it.
	RefStoryboard ReferenceKind = "storyboard_version"
	// RefPanel is one panel version an approved frame came from. There is one per shot.
	RefPanel ReferenceKind = "storyboard_panel_version"
	// RefAssetVersion is one media or image version used.
	RefAssetVersion ReferenceKind = "asset_version"
	// RefSubtitleTrack is the track muxed in, when there was one.
	RefSubtitleTrack ReferenceKind = "subtitle_track"
)

// ReferenceKinds lists the documented kinds in the schema's order.
var ReferenceKinds = []ReferenceKind{
	RefScript, RefDirectorPlan, RefStoryboard, RefPanel, RefAssetVersion, RefSubtitleTrack,
}

// IsValidReferenceKind reports whether a kind may be recorded.
func IsValidReferenceKind(value ReferenceKind) bool {
	for _, candidate := range ReferenceKinds {
		if candidate == value {
			return true
		}
	}
	return false
}

// ManifestReference is one input, with what it was and whether it is still the same.
type ManifestReference struct {
	Kind ReferenceKind `json:"kind"`
	ID   string        `json:"id"`
	// Hash is the content hash of the artifact's own file, when it has one. A version whose bytes
	// changed under the same identifier is the case a hash catches and an ID cannot.
	Hash string `json:"hash,omitempty"`
	// Label is what a reader sees in a list: a shot number, a track name. It is the only field here
	// that exists for display, and it is worth the bytes because "storyboard_panel_version
	// 0192f3…" is not a thing a person can find.
	Label string `json:"label,omitempty"`
	// ShotID and Ordinal place a panel in the film's order, empty for the references that have no
	// position.
	ShotID  string `json:"shotId,omitempty"`
	Ordinal int    `json:"ordinal,omitempty"`
}

// Validate checks one reference.
func (r ManifestReference) Validate() error {
	if !IsValidReferenceKind(r.Kind) {
		return InvalidError("The manifest names a kind of reference that is not recognised.")
	}
	if strings.TrimSpace(r.ID) == "" {
		return InvalidError("A manifest reference must name the artifact it cites.")
	}
	return nil
}

// Manifest is what an export was made from.
type Manifest struct {
	SchemaVersion int    `json:"schemaVersion"`
	EpisodeID     string `json:"episodeId"`
	// Quality, Width, Height, FPS and SubtitleMode are the RECIPE. They are in the manifest rather
	// than left to the export row because a manifest is meant to be portable: a reader holding only
	// this document should be able to say how the film was made.
	Quality      string `json:"quality"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	FPS          int    `json:"fps"`
	SubtitleMode string `json:"subtitleMode"`
	// DurationMS is what the OUTPUT said its duration was, read back after composing.
	DurationMS int `json:"durationMs"`
	// Engine names the program that composed the film and its version. It is here for the same
	// reason a generation records its model: reproducibility depends on what produced the bytes.
	Engine string `json:"engine,omitempty"`
	// References are the inputs, in the order the film uses them.
	References []ManifestReference `json:"references"`
	// ExportVersionNumber is the export's own position in its episode's history, so a reader can tell
	// which of several exports this manifest belongs to.
	ExportVersionNumber int       `json:"exportVersionNumber"`
	CreatedAt           time.Time `json:"createdAt"`
}

// Validate checks a manifest before it is stored.
//
// The reference rules are what makes a manifest COMPLETE rather than merely well-formed, and this is
// where AC-MEDIA-003's traceability is either true or not:
//
//   - a script version and a storyboard version are REQUIRED, because a film that cannot name what
//     it renders cannot be traced to anything;
//   - there must be at least one panel or asset reference, because a film with no inputs is not a
//     film;
//   - every reference must be valid, and the frame size must be positive and even, which is the same
//     rule the engine enforces — stated here so a caller learns it before a process starts.
func (m Manifest) Validate() error {
	if m.SchemaVersion != ManifestSchemaVersion {
		return InvalidError("The manifest's schema version is not one this build writes.")
	}
	if strings.TrimSpace(m.EpisodeID) == "" {
		return InvalidError("A manifest must name its episode.")
	}
	if m.Width <= 0 || m.Height <= 0 || m.Width%2 != 0 || m.Height%2 != 0 {
		return InvalidError("A manifest's frame size must be positive and even in both directions.")
	}
	if !IsValidExportQuality(ExportQuality(m.Quality)) {
		return InvalidError("The manifest's quality is not recognised.")
	}
	if len(m.References) == 0 {
		return InvalidError("A manifest must cite at least one input.")
	}
	kinds := map[ReferenceKind]int{}
	inputs := 0
	for _, reference := range m.References {
		if err := reference.Validate(); err != nil {
			return err
		}
		kinds[reference.Kind]++
		if reference.Kind == RefPanel || reference.Kind == RefAssetVersion {
			inputs++
		}
	}
	if kinds[RefScript] == 0 {
		return InvalidError("A manifest must cite the script version it renders.")
	}
	if kinds[RefStoryboard] == 0 {
		return InvalidError("A manifest must cite the storyboard version that ordered it.")
	}
	if inputs == 0 {
		return InvalidError("A manifest must cite at least one frame or asset it was made from.")
	}
	return nil
}

// ReferencesOf returns the manifest's references of one kind, in order.
func (m Manifest) ReferencesOf(kind ReferenceKind) []ManifestReference {
	matches := []ManifestReference{}
	for _, reference := range m.References {
		if reference.Kind == kind {
			matches = append(matches, reference)
		}
	}
	return matches
}

// Encode renders the manifest as the JSON document the export row stores.
func (m Manifest) Encode() (string, error) {
	if err := m.Validate(); err != nil {
		return "", err
	}
	document, err := json.Marshal(m)
	if err != nil {
		return "", StorageError("The export manifest could not be written.", err)
	}
	return string(document), nil
}

// DecodeManifest reads a manifest back.
//
// A stored document that will not parse is a REFUSAL rather than an empty manifest: the caller is
// about to compare it against what is currently approved, and an empty one would compare as "nothing
// to check" — which reads as a clean export and is the opposite of the truth.
func DecodeManifest(document string) (Manifest, error) {
	trimmed := strings.TrimSpace(document)
	if trimmed == "" {
		return Manifest{}, InvalidError("That export has no manifest, so it cannot be traced.")
	}
	// `null` and `{}` both unmarshal into a zero struct WITHOUT an error, and a zero manifest is the
	// case this function exists to refuse: the caller compares it against what is currently approved,
	// and every comparison against a zero value reads as "nothing to check". The schema version is
	// what tells the two apart — a real document states one — so its absence is the refusal.
	if trimmed == "null" || trimmed == "{}" {
		return Manifest{}, InvalidError("That export's manifest is empty, so it cannot be traced.")
	}
	var manifest Manifest
	if err := json.Unmarshal([]byte(trimmed), &manifest); err != nil {
		return Manifest{}, InvalidError("That export's manifest could not be read.")
	}
	if manifest.SchemaVersion == 0 {
		return Manifest{}, InvalidError("That export's manifest states no schema version, so its shape is unknown.")
	}
	return manifest, nil
}

// SubtitleMode is how a subtitle track travels into an export.
//
// It lives in the domain rather than beside the engine port because it is a VOCABULARY with
// validation, and two callers need the same answer: the export service decides what to ask for, and
// the engine decides what to run. A second definition on either side would be a second answer.
//
// The ZERO VALUE is none, and that is deliberate: most exports have no subtitles, so a caller that
// says nothing gets a film without them rather than a refusal about a mode it never chose.
type SubtitleMode string

const (
	// SubtitleNone exports without subtitles. It is the zero value.
	SubtitleNone SubtitleMode = ""
	// SubtitleSidecar muxes the subtitles as a stream the player can turn off.
	SubtitleSidecar SubtitleMode = "sidecar"
	// SubtitleBurn draws them into the picture.
	SubtitleBurn SubtitleMode = "burn"
)

// SubtitleModes lists the documented modes in the schema's order.
var SubtitleModes = []SubtitleMode{SubtitleNone, SubtitleSidecar, SubtitleBurn}

// IsValidSubtitleMode reports whether a mode may be requested.
func IsValidSubtitleMode(value SubtitleMode) bool {
	for _, candidate := range SubtitleModes {
		if candidate == value {
			return true
		}
	}
	return false
}

// ExportQuality is how much a film was made for.
type ExportQuality string

const (
	// QualityPreview is a fast, smaller export for looking at.
	QualityPreview ExportQuality = "preview"
	// QualityFinal is the deliverable.
	QualityFinal ExportQuality = "final"
)

// ExportQualities lists the documented qualities in the schema's order.
var ExportQualities = []ExportQuality{QualityPreview, QualityFinal}

// IsValidExportQuality reports whether a quality may be persisted.
func IsValidExportQuality(value ExportQuality) bool {
	for _, candidate := range ExportQualities {
		if candidate == value {
			return true
		}
	}
	return false
}

// Resolution is an export's frame size.
type Resolution struct {
	Width  int
	Height int
}

// Bounds on an export's frame size.
//
// The upper bound is 4K because that is the largest thing a desktop export of an episode has any
// reason to produce, and SECURITY section 8.4's "限制时长/分辨率" is about refusing the enormous rather
// than about optimising the merely large.
const (
	MinExportWidth  = 64
	MinExportHeight = 64
	MaxExportWidth  = 7680
	MaxExportHeight = 4320
)

// The named sizes FR-080 asks for, plus the two an episode is usually shot at.
var (
	// ResolutionLandscape is 1920x1080: FR-080's 横屏.
	ResolutionLandscape = Resolution{Width: 1920, Height: 1080}
	// ResolutionPortrait is 1080x1920: FR-080's 竖屏.
	ResolutionPortrait = Resolution{Width: 1080, Height: 1920}
	// ResolutionSquare is 1080x1080, for the platforms that want it.
	ResolutionSquare = Resolution{Width: 1080, Height: 1080}
	// ResolutionPreview is a quarter-size frame for a preview export.
	ResolutionPreview = Resolution{Width: 960, Height: 540}
)

// Validate checks a resolution.
//
// The even-dimension rule is here rather than only in the engine, because the engine's refusal
// arrives as a media error about a file: H.264 encodes in 2x2 blocks and an odd size fails at the
// encoder. A caller should learn that from the number it passed.
func (r Resolution) Validate() error {
	if r.Width%2 != 0 || r.Height%2 != 0 {
		return InvalidError("An export's frame size must be even in both directions.")
	}
	if r.Width < MinExportWidth || r.Height < MinExportHeight {
		return InvalidError("An export's frame size is smaller than this build produces.")
	}
	if r.Width > MaxExportWidth || r.Height > MaxExportHeight {
		return InvalidError("An export's frame size is larger than this build produces.")
	}
	return nil
}

// Orientation says whether a frame is wider than it is tall.
//
// It is a helper rather than a field because it is DERIVED: a stored orientation could disagree with
// the two numbers it was computed from, and the numbers are what a renderer uses.
func (r Resolution) Orientation() string {
	if r.Height > r.Width {
		return "portrait"
	}
	if r.Width == r.Height {
		return "square"
	}
	return "landscape"
}

// ResolutionForQuality picks the default frame size for a quality and an orientation.
//
// A preview is a quarter-size landscape frame whatever the final will be: FR-080's "预览质量和最终
// 质量" is about how long a render takes, and a preview that cost as much as the deliverable would
// not be a preview.
func ResolutionForQuality(quality ExportQuality, orientation string) Resolution {
	if quality == QualityPreview {
		return ResolutionPreview
	}
	switch orientation {
	case "portrait":
		return ResolutionPortrait
	case "square":
		return ResolutionSquare
	default:
		return ResolutionLandscape
	}
}

// Export recipes: the frame rate this build renders at.
const (
	// DefaultExportFPS is 24, which is what animation and most drama is shot at.
	DefaultExportFPS = 24
	// MaxExportFPS bounds a caller's rate. Above this the render is slower than it is smoother, and
	// the file is larger for no visible gain.
	MaxExportFPS = 60
)
