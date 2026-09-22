package desktop

import (
	"context"
	"strings"

	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	domainmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/media"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
)

// NewSpokenLineReader adapts the script service to the media package's line port.
//
// # Why an adapter and where it lives
//
// The subtitle generator needs two fields from a dialogue line — who speaks and what they say — plus
// the version's own duration estimate. It must not import the script domain to get them, because a
// cue and a dialogue line are different aggregates with separate versioning, and tying the two
// together would make one's version model depend on the other's.
//
// So the port is declared in the media package and the translation lives here, which is where this
// repository puts the seams between layers that must not know about each other.
//
// # Why it returns a scene total rather than per-scene durations
//
// The generator places cues in proportion to a line's length, scene by scene, and the sum of the
// scenes' estimates is what it needs to know how much time it has. Returning the total rather than a
// per-scene map keeps the port at two values: the lines, in order, and the milliseconds they share.
// A per-scene breakdown would be the shape to change if the generator ever needed to respect scene
// boundaries — which it does not, because a subtitle file is a flat list.
type SpokenLineReader struct {
	script *appscript.Service
}

// NewSpokenLineReader builds the adapter.
func NewSpokenLineReader(service *appscript.Service) *SpokenLineReader {
	return &SpokenLineReader{script: service}
}

// Lines returns a script version's dialogue lines in script order, with the version's total
// estimated duration in milliseconds.
//
// It returns EVERY line and lets the service decide which become subtitles: the rule about what a
// subtitle is belongs to the media package, and a reader that filtered would be a second statement
// of it.
func (r *SpokenLineReader) Lines(ctx context.Context, scriptVersionID string) ([]domainmedia.SpokenLine, int, error) {
	if r == nil || r.script == nil {
		return nil, 0, domainmedia.InvalidError("The script reader is not configured.")
	}
	versionID := strings.TrimSpace(scriptVersionID)
	if versionID == "" {
		return nil, 0, domainmedia.InvalidError("A subtitle draft must name the script version it renders.")
	}
	version, err := r.script.GetScriptVersion(ctx, versionID)
	if err != nil {
		return nil, 0, err
	}
	structure, err := r.script.GetScriptStructure(ctx, versionID)
	if err != nil {
		return nil, 0, err
	}
	// The lines come out in the structure's own order, which is the order the version stores: scenes
	// by ordinal, and each scene's lines by ordinal inside it. That is what makes the draft's times
	// follow the script rather than a sort this adapter invented.
	lines := []domainmedia.SpokenLine{}
	for _, scene := range structure.Scenes {
		for _, line := range scene.DialogueLines {
			lines = append(lines, domainmedia.SpokenLine{
				LineID:            line.ID,
				Type:              string(line.Type),
				CharacterEntityID: line.CharacterEntityID,
				Text:              line.Text,
			})
		}
	}
	// The duration is the VERSION's own estimate, converted to milliseconds. The script states it in
	// seconds (DOMAIN_MODEL section 7.6), and the subtitle timeline is in milliseconds, so the
	// conversion happens once here rather than at each call site.
	totalMS := version.EstimatedDurationSeconds * 1000
	return lines, totalMS, nil
}

// The compile-time proof that this satisfies the port.
//
// It is against the media package's interface rather than a local copy, so a signature drift fails the
// build — the shape this repository's reviews have found missing in four other places.
var _ appmedia.LineReader = (*SpokenLineReader)(nil)

// lineTypeOf is kept so a reader can see which types the generator treats as spoken without opening
// the domain package: the set is stated where it is used.
var _ = script.LineDialogue
