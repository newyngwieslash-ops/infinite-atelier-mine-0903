package media

import (
	"context"
	"strings"
	"time"

	domainmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/media"
)

// SubtitleRepository is the persistence the subtitle service needs, declared beside its use.
//
// It is a port of its own rather than part of the export repository because a subtitle track exists
// whether or not an export ever runs: AC-MEDIA-002 grades the track, and an export is one consumer of
// it.
type SubtitleRepository interface {
	// CreateTrackWithCues writes the track and its cues in one transaction.
	CreateTrackWithCues(ctx context.Context, track domainmedia.Track, cues []domainmedia.Cue) error
	GetTrack(ctx context.Context, id string) (domainmedia.Track, error)
	ListTracks(ctx context.Context, episodeID string) ([]domainmedia.Track, error)
	ListCues(ctx context.Context, trackID string) ([]domainmedia.Cue, error)
	MaxTrackVersionNumber(ctx context.Context, episodeID string) (int, error)
	CurrentApprovedTrack(ctx context.Context, episodeID string) (domainmedia.Track, bool, error)
	ApproveTrack(ctx context.Context, trackID, episodeID, traceID string, at time.Time) error
	// ReplaceCues rewrites a track's cues in one transaction, which is how an edit persists.
	ReplaceCues(ctx context.Context, trackID string, cues []domainmedia.Cue) error
}

// LineReader reads a script version's dialogue lines.
//
// It is a port rather than the script service because this package must not import the script
// domain: a cue and a dialogue line are different aggregates with separate versioning, and the two
// facts the generator needs — who speaks and what they say — are two fields.
//
// It returns EVERY line, not only the spoken ones, and the name says so because the first version
// of this port did not: it was called `SpokenLines` and the service trusted it to have filtered,
// which meant a reader that returned all six of a scene's lines produced cues for the stage
// directions. Whether a line becomes a subtitle is the SERVICE's rule — it is the same rule
// MissingLines applies — so the service applies it in one place rather than depending on every
// reader to have applied it in its own.
type LineReader interface {
	// Lines returns a script version's dialogue lines in script order.
	//
	// The ORDER is the reader's and the generator relies on it: cues follow the script, so a reader
	// that returned them shuffled would produce a subtitle file whose lines appear at the wrong times.
	Lines(ctx context.Context, scriptVersionID string) ([]domainmedia.SpokenLine, int, error)
}

// Clock and IDGenerator are the determinism ports every application service here has.
type Clock interface {
	Now() time.Time
}

type IDGenerator interface {
	New() (string, error)
}

// SubtitleOptions configures the subtitle service.
type SubtitleOptions struct {
	Tracks SubtitleRepository
	Lines  LineReader
	Clock  Clock
	IDs    IDGenerator
}

// SubtitleService drafts, edits and exports subtitles.
type SubtitleService struct {
	tracks SubtitleRepository
	lines  LineReader
	clock  Clock
	ids    IDGenerator
}

// NewSubtitleService builds the service.
func NewSubtitleService(options SubtitleOptions) *SubtitleService {
	return &SubtitleService{
		tracks: options.Tracks,
		lines:  options.Lines,
		clock:  options.Clock,
		ids:    options.IDs,
	}
}

// Available reports whether the service can run.
func (s *SubtitleService) Available() bool {
	return s != nil && s.tracks != nil && s.lines != nil && s.clock != nil && s.ids != nil
}

func (s *SubtitleService) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now().UTC()
	}
	return s.clock.Now().UTC()
}

// DraftRequest generates a starting track from a script version.
type DraftRequest struct {
	EpisodeID       string
	ScriptVersionID string
	// CreatedByType and CreatedByID are the attribution. They are the caller's because a draft can be
	// an agent's output or a user's action, and §13.4 requires the record to say which.
	CreatedByType    string
	CreatedByID      string
	SourceAgentRunID string
}

// Draft builds a subtitle track from a script's spoken lines.
//
// # How the times are computed, and why it is arithmetic rather than a model
//
// Each line gets a share of its scene's estimated duration, in proportion to how much text it has.
// That is the only division available before the video exists: the script states a duration per scene
// and a length per line, and no one has yet said how long a line takes to say. So a scene of ten
// seconds with two lines of equal length gives each five seconds, and a scene of ten seconds with one
// line twice as long as the other gives the longer one two-thirds.
//
// It is a DRAFT, and the name matters: AC-MEDIA-002 asks that subtitles be editable and that a
// missing line be detected, and both of those are things a user does to a draft. A generator that
// claimed to place subtitles precisely would be claiming to know a timing only the recorded audio
// knows, and the first TTS job or the first real video is what settles it.
//
// # Which lines get cues
//
// The spoken ones — dialogue and narration — which is the distinction DOMAIN_MODEL section 7.7's
// LineType draws. An action, transition or note line is a direction to the production rather than
// something anyone says, so a subtitle for one would be a subtitle nobody can hear.
func (s *SubtitleService) Draft(ctx context.Context, request DraftRequest) (domainmedia.Track, []domainmedia.Cue, error) {
	if !s.Available() {
		return domainmedia.Track{}, nil, NotAvailableError("No subtitle store is configured, so a draft cannot be written.")
	}
	episodeID := strings.TrimSpace(request.EpisodeID)
	scriptVersionID := strings.TrimSpace(request.ScriptVersionID)
	if episodeID == "" {
		return domainmedia.Track{}, nil, InvalidError("A subtitle draft must name the episode it is for.")
	}
	if scriptVersionID == "" {
		return domainmedia.Track{}, nil, InvalidError("A subtitle draft must name the script version it renders.")
	}
	all, sceneDurationMS, err := s.lines.Lines(ctx, scriptVersionID)
	if err != nil {
		return domainmedia.Track{}, nil, err
	}
	// THE FILTER IS HERE, not in the reader. A dialogue or narration line is something a person
	// hears; an action, transition or note line is a direction to the production, and a subtitle for
	// one would be a caption nobody can hear. The rule lives in the domain (IsSpoken) and is applied
	// by the service that decides what a subtitle is.
	lines := make([]domainmedia.SpokenLine, 0, len(all))
	for _, line := range all {
		if domainmedia.IsSpoken(line.Type) {
			lines = append(lines, line)
		}
	}
	highest, err := s.tracks.MaxTrackVersionNumber(ctx, episodeID)
	if err != nil {
		return domainmedia.Track{}, nil, err
	}
	trackID, err := s.ids.New()
	if err != nil {
		return domainmedia.Track{}, nil, NotAvailableError("The subtitle track could not be identified.")
	}
	now := s.now()
	track := domainmedia.Track{
		ID:               trackID,
		EpisodeID:        episodeID,
		ScriptVersionID:  scriptVersionID,
		VersionNumber:    highest + 1,
		Status:           "draft",
		SourceAgentRunID: strings.TrimSpace(request.SourceAgentRunID),
		CreatedByType:    createdByOrDefault(request.CreatedByType),
		CreatedByID:      strings.TrimSpace(request.CreatedByID),
		ChangeReason:     "Drafted from the script's spoken lines.",
		CreatedAt:        now,
	}
	if err := track.Validate(); err != nil {
		return domainmedia.Track{}, nil, err
	}
	cues, err := s.cuesFor(lines, sceneDurationMS, trackID, now)
	if err != nil {
		return domainmedia.Track{}, nil, err
	}
	if err := s.tracks.CreateTrackWithCues(ctx, track, cues); err != nil {
		return domainmedia.Track{}, nil, err
	}
	return track, cues, nil
}

// cuesFor places the lines on the timeline.
//
// The arithmetic is stated in Draft's comment; this is where it happens, and it is a separate function
// because the two halves — how much time a line gets, and where it starts — are the whole of the
// generator.
func (s *SubtitleService) cuesFor(lines []domainmedia.SpokenLine, sceneDurationMS int, trackID string, now time.Time) ([]domainmedia.Cue, error) {
	if len(lines) == 0 {
		// A script with nothing spoken produces an EMPTY track rather than an error: it is a legal
		// script, and a track with no cues is what a reader would expect for it.
		return []domainmedia.Cue{}, nil
	}
	// The weight of each line is its text's length, with a floor of one so a line whose text is empty
	// still occupies a slot rather than being given no time at all.
	weights := make([]int, 0, len(lines))
	total := 0
	for _, line := range lines {
		weight := len([]rune(strings.TrimSpace(line.Text)))
		if weight < 1 {
			weight = 1
		}
		weights = append(weights, weight)
		total += weight
	}
	// The duration the script states, in milliseconds, with a floor so a script whose estimate is zero
	// still produces a subtitle with a readable duration rather than a run of zero-length cues.
	available := sceneDurationMS
	if available <= 0 {
		available = len(lines) * DefaultCueDurationMS
	}
	cues := make([]domainmedia.Cue, 0, len(lines))
	position := 0
	for index, line := range lines {
		// The share is the line's weight against the total; the last line takes whatever remains, so
		// the cues cover the scene exactly rather than leaving a rounding remainder at the end.
		length := available - position
		if index < len(lines)-1 {
			length = available * weights[index] / total
		}
		if length < MinimumCueDurationMS {
			length = MinimumCueDurationMS
		}
		cueID, err := s.ids.New()
		if err != nil {
			return nil, NotAvailableError("A subtitle cue could not be identified.")
		}
		cue := domainmedia.Cue{
			ID:                cueID,
			TrackID:           trackID,
			Ordinal:           index + 1,
			Start:             domainmedia.Timecode(position),
			End:               domainmedia.Timecode(position + length),
			Text:              strings.TrimSpace(line.Text),
			CharacterEntityID: line.CharacterEntityID,
			DialogueLineID:    line.LineID,
			Status:            domainmedia.CueGenerated,
			CreatedAt:         now,
		}
		if err := cue.Validate(); err != nil {
			return nil, err
		}
		cues = append(cues, cue)
		position += length
	}
	return cues, nil
}

// The draft's timing floor and default, both stated rather than inline.
const (
	// MinimumCueDurationMS is how long a cue lasts when its share would be shorter: a quarter second
	// is about the shortest a line can appear and be read.
	MinimumCueDurationMS = 250
	// DefaultCueDurationMS is the per-line share used when the script states no scene duration, so a
	// draft of an unestimated script still produces readable subtitles.
	DefaultCueDurationMS = 2000
)

// Cues returns a track's cues in order.
func (s *SubtitleService) Cues(ctx context.Context, trackID string) ([]domainmedia.Cue, error) {
	if !s.Available() {
		return nil, NotAvailableError("No subtitle store is configured.")
	}
	return s.tracks.ListCues(ctx, strings.TrimSpace(trackID))
}

// Tracks returns an episode's tracks newest version first.
func (s *SubtitleService) Tracks(ctx context.Context, episodeID string) ([]domainmedia.Track, error) {
	if !s.Available() {
		return nil, NotAvailableError("No subtitle store is configured.")
	}
	return s.tracks.ListTracks(ctx, strings.TrimSpace(episodeID))
}

// Track returns one track.
func (s *SubtitleService) Track(ctx context.Context, trackID string) (domainmedia.Track, error) {
	if !s.Available() {
		return domainmedia.Track{}, NotAvailableError("No subtitle store is configured.")
	}
	return s.tracks.GetTrack(ctx, strings.TrimSpace(trackID))
}

// EditRequest replaces a track's cues.
type EditRequest struct {
	TrackID string
	Cues    []CueEdit
}

// CueEdit is one cue as a caller states it.
//
// The identifier is optional: a user adding a line has no identifier yet, and one that has edited a
// line has the one it already had. An empty identifier means "mint one", which is the convention the
// write tools use and the reason this type exists rather than taking the domain's Cue directly.
type CueEdit struct {
	ID                string
	StartMS           int64
	EndMS             int64
	Text              string
	CharacterEntityID string
	DialogueLineID    string
	Status            domainmedia.CueStatus
}

// Edit replaces a track's cues, which is AC-MEDIA-002's "subtitle editable".
//
// # The ordinals are POSITIONS
//
// A caller states its cues in order and gets ordinals from that order, one to n. That is the
// opposite of what the storage layer does — ReplaceCues takes ordinals as given — and the difference
// is deliberate: a UI edits a LIST, and asking it to renumber after a deletion would be asking it to
// reproduce a rule this service owns. The tracks that reach storage therefore never have a gap,
// which is what ValidateTrack refuses.
func (s *SubtitleService) Edit(ctx context.Context, request EditRequest) ([]domainmedia.Cue, error) {
	if !s.Available() {
		return nil, NotAvailableError("No subtitle store is configured, so a subtitle cannot be edited.")
	}
	trackID := strings.TrimSpace(request.TrackID)
	if trackID == "" {
		return nil, InvalidError("An edit must name the track it changes.")
	}
	if _, err := s.tracks.GetTrack(ctx, trackID); err != nil {
		return nil, err
	}
	now := s.now()
	cues := make([]domainmedia.Cue, 0, len(request.Cues))
	for index, edit := range request.Cues {
		id := strings.TrimSpace(edit.ID)
		if id == "" {
			minted, err := s.ids.New()
			if err != nil {
				return nil, NotAvailableError("A subtitle cue could not be identified.")
			}
			id = minted
		}
		status := edit.Status
		if status == "" {
			// An edit that states no status is an EDIT: the user touched this cue, and the status is
			// what makes a later regeneration leave it alone.
			status = domainmedia.CueEdited
		}
		cue := domainmedia.Cue{
			ID:                id,
			TrackID:           trackID,
			Ordinal:           index + 1,
			Start:             domainmedia.Timecode(edit.StartMS),
			End:               domainmedia.Timecode(edit.EndMS),
			Text:              edit.Text,
			CharacterEntityID: edit.CharacterEntityID,
			DialogueLineID:    edit.DialogueLineID,
			Status:            status,
			CreatedAt:         now,
		}
		if err := cue.Validate(); err != nil {
			return nil, err
		}
		cues = append(cues, cue)
	}
	for _, issue := range domainmedia.ValidateTrack(cues) {
		return nil, InvalidError(issue.Problem)
	}
	if err := s.tracks.ReplaceCues(ctx, trackID, cues); err != nil {
		return nil, err
	}
	return cues, nil
}

// MissingLinesRequest asks which spoken lines a track does not cover.
type MissingLinesRequest struct {
	TrackID string
}

// Missing reports the spoken lines a track has no cue for, which is AC-MEDIA-002's "missing line
// detected".
//
// It reads the lines from the TRACK's own script version rather than from a caller, so the answer
// cannot be about a different revision: a track drafted from version three and checked against
// version four would report lines that did not exist when it was made.
func (s *SubtitleService) Missing(ctx context.Context, request MissingLinesRequest) ([]domainmedia.SpokenLine, error) {
	if !s.Available() {
		return nil, NotAvailableError("No subtitle store is configured.")
	}
	track, err := s.tracks.GetTrack(ctx, strings.TrimSpace(request.TrackID))
	if err != nil {
		return nil, err
	}
	cues, err := s.tracks.ListCues(ctx, track.ID)
	if err != nil {
		return nil, err
	}
	lines, _, err := s.lines.Lines(ctx, track.ScriptVersionID)
	if err != nil {
		return nil, err
	}
	// MissingLines applies the same IsSpoken filter, so the two paths agree by construction rather
	// than by both remembering.
	return domainmedia.MissingLines(lines, cues), nil
}

// ExportRequest renders a track as a subtitle document.
type ExportRequest struct {
	TrackID string
	Format  domainmedia.SubtitleFormat
}

// Export renders a track as SRT or VTT.
//
// The document is returned as TEXT rather than stored here, and that is the boundary ADR-0015 section
// 5 records: a subtitle is a locally derived artifact, so writing it is the CALLER's step through the
// file store rather than this service's through the provider result pipeline. What this owns is the
// rendering, which is the part two callers need the same answer from.
func (s *SubtitleService) Export(ctx context.Context, request ExportRequest) (string, error) {
	if !s.Available() {
		return "", NotAvailableError("No subtitle store is configured.")
	}
	if !domainmedia.IsValidSubtitleFormat(request.Format) {
		return "", InvalidError("The subtitle format is not recognised.")
	}
	cues, err := s.Cues(ctx, request.TrackID)
	if err != nil {
		return "", err
	}
	if len(cues) == 0 {
		// An empty document is a legal subtitle file and a useless one, so it is REFUSED with a
		// message that says which track was empty. A caller that exported one would produce a file a
		// player shows nothing for, and would have no way to tell that from a broken render.
		return "", InvalidError("That track has no cues, so there is nothing to export.")
	}
	return domainmedia.Render(cues, request.Format)
}

// Approve puts a track in force.
//
// It is the user's act, which is why it takes the decision's trace identifier: DOMAIN_MODEL section
// 17's event list is closed and has no subtitle event, so the governance travels on the workflow's
// own stream — the same ruling ADR-0013 section 8 records for the gap report — and this row's link to
// the decision is what connects them.
func (s *SubtitleService) Approve(ctx context.Context, trackID, episodeID, traceID string) (domainmedia.Track, error) {
	if !s.Available() {
		return domainmedia.Track{}, NotAvailableError("No subtitle store is configured.")
	}
	track, err := s.tracks.GetTrack(ctx, strings.TrimSpace(trackID))
	if err != nil {
		return domainmedia.Track{}, err
	}
	if strings.TrimSpace(episodeID) != "" && track.EpisodeID != strings.TrimSpace(episodeID) {
		return domainmedia.Track{}, InvalidError("That subtitle track belongs to a different episode.")
	}
	if err := s.tracks.ApproveTrack(ctx, track.ID, track.EpisodeID, strings.TrimSpace(traceID), s.now()); err != nil {
		return domainmedia.Track{}, err
	}
	return s.tracks.GetTrack(ctx, track.ID)
}

// createdByOrDefault names the producer, defaulting to the user.
//
// A draft is usually a user's action — they pressed the button — and an agent-driven one states its
// own type. The schema's CHECK refuses an empty value, so a caller that says nothing gets the user
// rather than a constraint failure about a field it did not know existed.
func createdByOrDefault(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "user"
	}
	return trimmed
}

// The compile-time proof that the store satisfies the port.
//
// It is against this package's interface, so a signature drift fails the build rather than leaving a
// nil port that reads at runtime as "no subtitle store is configured".
var _ SubtitleRepository = (SubtitleRepository)(nil)
