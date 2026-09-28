package media

import (
	"context"
	"strings"

	domainmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/media"
)

// Timeline is the ordered read model AC-MEDIA-003's first clause is graded on.
//
//	## AC-MEDIA-003 Timeline/MP4
//	- ordered Shots；
//	- audio/subtitle；
//	- replace clip；
//	- export；
//
// # Why it is a read model rather than a table
//
// The order already exists: `storyboard_items.ordinal` runs from one with no gap, enforced by the
// unique constraint migration 000010 created and by `ValidateTrack`'s sibling rule for the board. A
// timeline table would be a second copy of an order that is already stored, and the two would
// eventually disagree about what comes after shot six.
//
// What did not exist is the READ: nothing joined a board row to the media approved for it, to the
// audio a dialogue line produced, and to the cue that renders it. That join is what this file is.

// TimelineShot is one shot with everything the film needs for it.
type TimelineShot struct {
	// Ordinal is the position in the episode, from the board's own rows.
	Ordinal int
	ItemID  string
	ShotID  string
	// DurationMS is what the shot lasts in the export. It comes from the panel's ITEM rather than
	// from the script's shot, because the board is where the directing decisions live and a board
	// that stated four seconds for a shot is what the film should be.
	DurationMS int
	// MediaVersionID and MediaHash are the approved media for this shot, empty when none is approved.
	// The hash is what makes the manifest traceable: a version whose bytes changed under the same
	// identifier is caught by comparing hashes rather than identifiers.
	MediaVersionID string
	MediaHash      string
	// MediaKind is the approved media's own type — a video frame or a still — so a caller knows what
	// it is about to compose rather than assuming.
	MediaKind string
	// PanelVersionID is the panel the frame came from.
	PanelVersionID string
	// HasAudio reports whether any audio version is approved for a line in this shot's scene.
	HasAudio bool
	// AudioClips are the approved audio versions for that scene's lines WITH THEIR ROLES, which is
	// what the export composes. The timeline carries them so the export does not have to re-read the
	// board: two reads of the same join could disagree, and the export's whole job is to record what
	// it actually used. See `BoardRow.AudioClips` for why the role travels rather than being assumed.
	AudioClips []AudioVersionRef
	// StartMS is where this shot begins in the film, which is the sum of the durations before it.
	// It is what places a dialogue clip: a line's audio belongs at its shot's start, and without
	// this the export would have to recompute the running total — a second implementation of the
	// arithmetic the timeline already did.
	StartMS int
	// CueCount is how many subtitle cues fall inside this shot's span.
	CueCount int
}

// Timeline is an episode's ordered shots and the totals a reader needs.
type Timeline struct {
	EpisodeID       string
	BoardVersionID  string
	ScriptVersionID string
	Shots           []TimelineShot
	// TotalDurationMS is the sum of the shots' own durations, which is what the film will be.
	TotalDurationMS int
	// AssetDurationsMS is what the script estimated. The two are separate because a Final Ruleset rule
	// compares them: a board that totals ten minutes against a script that says eight is a directing
	// decision somebody has to have made on purpose.
	AssetDurationsMS int
	// MissingMedia counts the shots with no approved media, which is the first thing a reader wants to
	// know before exporting.
	MissingMedia int
	// CueCount is how many cues the approved track has, and MissingLines how many spoken lines it does
	// not cover.
	CueCount     int
	MissingLines int
}

// TimelineRepository reads what a timeline is made of.
//
// It is a port because the three reads belong to three different aggregates — the board, the assets,
// the subtitles — and this package must not import any of their services. The composition root
// supplies one adapter over the repositories, which is where this repository puts the seams between
// layers that must not know about each other.
type TimelineRepository interface {
	// BoardFacts returns a board version's rows in ordinal order with the media approved for each.
	//
	// One call rather than a call per row, because a board is read as a unit and a per-row port would
	// make an episode's timeline cost one query per shot.
	BoardFacts(ctx context.Context, storyboardVersionID string) ([]BoardRow, error)
	// BoardVersion returns the version row, so the timeline can name what it read.
	BoardVersion(ctx context.Context, storyboardVersionID string) (BoardVersion, error)
	// CurrentBoardVersion returns the episode's approved board, or found=false.
	CurrentBoardVersion(ctx context.Context, episodeID string) (BoardVersion, bool, error)
	// CuesInRange returns the approved subtitle cues for an episode, in order.
	ApprovedCues(ctx context.Context, episodeID string) ([]domainmedia.Cue, int, error)
}

// BoardVersion is the board version a timeline read.
type BoardVersion struct {
	ID              string
	EpisodeID       string
	ScriptVersionID string
	VersionNumber   int
	Status          string
}

// BoardRow is one board row with its approved media resolved.
//
// The media fields are the JOIN the timeline exists for: `storyboard_panel_versions` carries
// `approved_image_asset_version_id`, and a row's panel is the newest approved one.
type BoardRow struct {
	ItemID       string
	ShotID       string
	Ordinal      int
	DurationSecs int
	// PanelVersionID and ApprovedVersionID are the panel and the asset version approved for it, both
	// empty when nothing is approved.
	PanelVersionID    string
	ApprovedVersionID string
	// MediaHash is the content hash of the approved version's primary file, empty when the version has
	// no file.
	MediaHash string
	// MediaKind is the asset's own type, so a caller knows whether it is composing a frame or a clip.
	MediaKind string
	// AudioApproved reports whether audio is approved for any line in this row's scene. It is what the
	// timeline reports for COMPLETENESS — "is this shot's scene voiced" — and it is a boolean because
	// a reader of a timeline wants a yes or no rather than a list of files.
	AudioApproved bool
	// AudioClips are the approved audio versions for the lines of this row's scene, WITH THE ROLE each
	// one was attached as.
	//
	// IT EXISTS BECAUSE THE EXPORT NEEDS THE FILES, and the comment above used to claim the export
	// "reads the actual files through the same join" — a join that did not exist. The consequence was
	// measured rather than imagined: `ExportService.compose` built a `ComposeRequest` with segments and
	// a subtitle path and NO audio at all, so every export was a silent film while
	// `TimelineShot.HasAudio` reported that the episode had audio. The acceptance walk recorded the
	// silence as a known limit; this field is what closes it.
	//
	// THE ROLE TRAVELS WITH THE ID because it decides the mix. It used to be a `[]string` and the
	// export hardcoded `AudioRoleDialogue` for every entry — so an imported music bed and a generated
	// whisper were both mixed as dialogue, and `DefaultGainFor(AudioRoleMusic)`'s 0.35 was a rule no
	// code could ever reach. Carrying the role makes that unreachable state unrepresentable: a caller
	// cannot compile a mix that forgot which clip is music.
	//
	// The ids are versions rather than paths because the store's bytes are content-addressed and the
	// export materialises them into its own scratch directory — the same path the picture takes.
	AudioClips []AudioVersionRef
}

// AudioVersionRef is one approved audio version and the role it was attached as.
type AudioVersionRef struct {
	VersionID string
	// Role is the role the version's `asset_usages.usage_role` named, mapped into the mixer's
	// vocabulary. An unrecognised or absent role maps to dialogue, which is what every row written
	// before the roles existed meant.
	Role AudioRole
	// Params is the use's own placement document (T05): offset, trim, volume,
	// mute, the line a dialogue clip renders. Zero value means "play it as
	// the mixer defaults", which is what every row written before the column
	// existed means.
	Params TrackParams
}

// TimelineOptions configures the service.
type TimelineOptions struct {
	Board TimelineRepository
}

// TimelineService reads an episode's timeline.
type TimelineService struct {
	board TimelineRepository
}

// NewTimelineService builds the service.
func NewTimelineService(options TimelineOptions) *TimelineService {
	return &TimelineService{board: options.Board}
}

// Available reports whether the service can read.
func (s *TimelineService) Available() bool {
	return s != nil && s.board != nil
}

// TimelineRequest asks for one episode's timeline.
type TimelineRequest struct {
	EpisodeID string
	// BoardVersionID names the board to read. Empty uses the episode's approved one, which is what a
	// user exporting an episode means.
	BoardVersionID string
}

// Read returns an episode's ordered timeline.
func (s *TimelineService) Read(ctx context.Context, request TimelineRequest) (Timeline, error) {
	if !s.Available() {
		return Timeline{}, NotAvailableError("No timeline reader is configured.")
	}
	episodeID := strings.TrimSpace(request.EpisodeID)
	if episodeID == "" {
		return Timeline{}, InvalidError("A timeline must name its episode.")
	}
	var version BoardVersion
	if named := strings.TrimSpace(request.BoardVersionID); named != "" {
		read, err := s.board.BoardVersion(ctx, named)
		if err != nil {
			return Timeline{}, err
		}
		// A caller that named another episode's board gets a refusal rather than a timeline of the
		// wrong episode, which is the same scope rule every read in this build keeps.
		if read.EpisodeID != episodeID {
			return Timeline{}, InvalidError("That storyboard version belongs to a different episode.")
		}
		version = read
	} else {
		approved, found, err := s.board.CurrentBoardVersion(ctx, episodeID)
		if err != nil {
			return Timeline{}, err
		}
		if !found {
			// Refused rather than answered with an empty timeline: "no board is approved" and "the
			// board is empty" are different situations, and a caller that got zero shots could not tell
			// them apart.
			return Timeline{}, InvalidError("This episode has no approved storyboard, so there is nothing to export yet.")
		}
		version = approved
	}
	rows, err := s.board.BoardFacts(ctx, version.ID)
	if err != nil {
		return Timeline{}, err
	}
	cues, missingLines, err := s.board.ApprovedCues(ctx, episodeID)
	if err != nil {
		return Timeline{}, err
	}
	timeline := Timeline{
		EpisodeID:       episodeID,
		BoardVersionID:  version.ID,
		ScriptVersionID: version.ScriptVersionID,
		Shots:           make([]TimelineShot, 0, len(rows)),
		CueCount:        len(cues),
		MissingLines:    missingLines,
	}
	// The cues are placed against the shots' RUNNING span, because a cue carries an absolute time and
	// a shot does not: the board states durations, and where a shot starts is the sum of what came
	// before it.
	position := 0
	for _, row := range rows {
		durationMS := row.DurationSecs * 1000
		shot := TimelineShot{
			Ordinal:         row.Ordinal,
			ItemID:          row.ItemID,
			ShotID:          row.ShotID,
			DurationMS:      durationMS,
			MediaVersionID:  row.ApprovedVersionID,
			MediaHash:       row.MediaHash,
			MediaKind:       row.MediaKind,
			PanelVersionID:  row.PanelVersionID,
			HasAudio:        row.AudioApproved,
			AudioClips:      row.AudioClips,
			// The shot starts where everything before it ended, which is the same running total the
			// cue placement below uses — computed once, here, so the two cannot disagree.
			StartMS: position,
		}
		if row.ApprovedVersionID == "" {
			timeline.MissingMedia++
		}
		for _, cue := range cues {
			if int(cue.Start) >= position && int(cue.Start) < position+durationMS {
				shot.CueCount++
			}
		}
		timeline.Shots = append(timeline.Shots, shot)
		timeline.TotalDurationMS += durationMS
		position += durationMS
	}
	return timeline, nil
}

// The compile-time proof that the port is satisfied by whatever the composition root supplies is not
// written here: the adapter lives in the infrastructure layer and its own assertion names this
// interface, which is the shape that fails the build when the two drift.
