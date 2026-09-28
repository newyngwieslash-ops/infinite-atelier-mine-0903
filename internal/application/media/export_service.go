package media

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	domainmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/media"
)

// The export service: AC-MEDIA-003's "export" and "manifest traceability".
//
// # The chain, and where it crosses a boundary
//
//	read the approved board and its media          (TimelineRepository)
//	→ assemble the manifest                        (this file, pure assembly)
//	→ compose the file                             (MediaEngine, the one subprocess)
//	→ store the bytes                              (Store, NOT the result pipeline)
//	→ record the export row                        (ExportRepository)
//	→ hand the user a file                         (SaveFile, a dialog)
//
// The fourth step is the boundary ADR-0015 section 5 records: a locally derived artifact goes through
// the file store rather than through `jobs.ResultStore`, whose allowlist exists for provider results
// and would refuse a subtitle file as a malformed image. The sixth is the one this repository had no
// path for at all before this package — 184 binding methods, none of which could write where a user
// pointed.

// FileStore is the content-addressed store an export writes through.
type FileStore interface {
	Put(ctx context.Context, displayName string, body io.Reader) (StoredObject, error)
	Open(ctx context.Context, storageKey string) (io.ReadCloser, error)
}

// StoredObject is what a Put produced.
type StoredObject struct {
	Hash       string
	StorageKey string
	MIME       string
	Size       int64
}

// AudioFileReader resolves an approved audio version to the bytes the store holds.
//
// # Why this is a port of its own rather than a method on FileStore
//
// `FileStore.Open` takes a STORAGE KEY, which is the file's content hash — and an export knows a
// VERSION identifier, not a hash. The join between the two lives in the asset tables: a version has
// an `asset_files` row whose role is `primary`, and that row names the hash. Putting that query behind
// this port keeps the export service out of the asset schema, which is the same division every other
// read in this package keeps.
//
// It is a per-version call rather than a batch because the caller already holds the ids and the count
// is bounded by the mix limit. A batch would be an optimisation for a case the bound makes small.
type AudioFileReader interface {
	// AudioFileFor returns the primary file's hash for one asset version. found=false means the
	// version has no primary file, which is a version nothing can play.
	AudioFileFor(ctx context.Context, assetVersionID string) (hash string, found bool, err error)
}

// ExportRepository records what an export was.
type ExportRepository interface {
	CreateExport(ctx context.Context, record ExportRecord) error
	GetExport(ctx context.Context, id string) (ExportRecord, error)
	ListExports(ctx context.Context, episodeID string) ([]ExportRecord, error)
	MaxExportVersionNumber(ctx context.Context, episodeID string) (int, error)
	CurrentApprovedExport(ctx context.Context, episodeID string) (ExportRecord, bool, error)
	ApproveExport(ctx context.Context, exportID, episodeID, traceID string, at time.Time) error
	// MarkUnderReview moves a draft export to the state an approval requires.
	//
	// IT IS ON THE PORT BECAUSE NOTHING ELSE COULD REACH THAT STATE. An earlier version of this
	// interface omitted it while the repository had the method, so the only caller in the whole build
	// was an acceptance test reaching past the service to the concrete repository — and `Approve`
	// refuses anything that is not `under_review`. In a real build the approval was therefore
	// UNREACHABLE: an export could be composed and listed and never approved, with the UI's only clue
	// a conflict message about a status nothing could change. An independent review found it while
	// wiring the approval control.
	//
	// The review is separate from the approval rather than folded into it because they are two acts:
	// a draft becomes a candidate for release, and then a person puts it in force. Folding them would
	// let the approve command move a row nobody had looked at.
	MarkUnderReview(ctx context.Context, exportID string) error
}

// ExportRecord is one row of `episode_exports`.
type ExportRecord struct {
	ID               string
	EpisodeID        string
	VersionNumber    int
	Status           string
	Quality          string
	Width            int
	Height           int
	DurationMS       int
	OutputFileHash   string
	SubtitleTrackID  string
	ManifestJSON     string
	ApprovalTraceID  string
	SourceAgentRunID string
	CreatedByType    string
	CreatedByID      string
	ChangeReason     string
	CreatedAt        time.Time
}

// Validate checks a record before it is stored.
func (r ExportRecord) Validate() error {
	if strings.TrimSpace(r.ID) == "" {
		return InvalidError("An export needs an identifier.")
	}
	if strings.TrimSpace(r.EpisodeID) == "" {
		return InvalidError("An export must name its episode.")
	}
	if r.VersionNumber < 1 {
		return InvalidError("An export's version number starts at one.")
	}
	if !domainmedia.IsValidExportQuality(domainmedia.ExportQuality(r.Quality)) {
		return InvalidError("The export's quality is not recognised.")
	}
	if r.Width <= 0 || r.Height <= 0 {
		return InvalidError("An export must state its frame size.")
	}
	return nil
}

// TempDir is where the engine writes before the bytes are stored.
//
// It is an interface rather than a path so the service does not discover a directory: the composition
// root knows where the application's temporary space is, and a service that looked one up would be a
// second answer to where things live.
type TempDir interface {
	// NewScratchDir creates a directory for one export and returns its path.
	NewScratchDir(ctx context.Context, prefix string) (string, error)
	// RemoveScratchDir removes one, on every path out.
	RemoveScratchDir(path string) error
}

// ExportOptions configures the export service.
type ExportOptions struct {
	Timeline TimelineService
	Engine   MediaEngine
	Files    FileStore
	// Audio resolves an approved audio version to its bytes. It is REQUIRED for an episode with
	// audio: a build composed without it composes silent films, and `Available` refuses rather than
	// letting that happen quietly — see `buildMix`.
	Audio    AudioFileReader
	Exports  ExportRepository
	Temp     TempDir
	Subtitle SubtitleReader
	Clock    Clock
	IDs      IDGenerator
}

// SubtitleReader reads a track's cues for the export.
//
// It is a narrow port rather than the whole subtitle service because this file needs one method from
// it, and a wider dependency would make an export impossible to test without a subtitle store.
type SubtitleReader interface {
	Cues(ctx context.Context, trackID string) ([]domainmedia.Cue, error)
}

// ExportService composes an episode and records what it was made from.
type ExportService struct {
	timeline TimelineService
	engine   MediaEngine
	files    FileStore
	audio    AudioFileReader
	exports  ExportRepository
	temp     TempDir
	subtitle SubtitleReader
	clock    Clock
	ids      IDGenerator
}

// NewExportService builds the service.
func NewExportService(options ExportOptions) *ExportService {
	return &ExportService{
		timeline: options.Timeline,
		engine:   options.Engine,
		files:    options.Files,
		audio:    options.Audio,
		exports:  options.Exports,
		temp:     options.Temp,
		subtitle: options.Subtitle,
		clock:    options.Clock,
		ids:      options.IDs,
	}
}

// Available reports whether an export can run.
//
// The ENGINE's availability is separate and reported by Diagnostic: a machine without ffmpeg can still
// read a timeline and a manifest, and saying "export is unavailable" here would make the section unable
// to show what it is that cannot be exported.
func (s *ExportService) Available() bool {
	return s != nil && s.engine != nil && s.files != nil && s.exports != nil &&
		s.temp != nil && s.clock != nil && s.ids != nil
}

// Diagnostic explains why the engine cannot compose, empty when it can.
func (s *ExportService) Diagnostic() string {
	if s == nil || s.engine == nil {
		return "No media engine is configured."
	}
	return s.engine.Diagnostic()
}

func (s *ExportService) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now().UTC()
	}
	return s.clock.Now().UTC()
}

// ExportRequest asks for one episode export.
type ExportRequest struct {
	EpisodeID string
	// BoardVersionID, empty uses the approved board.
	BoardVersionID string
	// SubtitleTrackID, empty uses the approved track if there is one and exports without subtitles if
	// there is not. A NAMED track that does not exist is an error rather than a silent omission.
	SubtitleTrackID string
	Quality         domainmedia.ExportQuality
	// Width and Height override the quality's default size. Zero uses the default for the quality.
	Width  int
	Height int
	FPS    int
	// SubtitleMode is how the subtitles travel: sidecar, burn or none.
	SubtitleMode     domainmedia.SubtitleMode
	CreatedByType    string
	CreatedByID      string
	SourceAgentRunID string
}

// Export composes an episode and records what it was made from.
func (s *ExportService) Export(ctx context.Context, request ExportRequest) (ExportRecord, domainmedia.Manifest, error) {
	if !s.Available() {
		return ExportRecord{}, domainmedia.Manifest{}, NotAvailableError("No export service is configured.")
	}
	if !s.engine.Available() {
		// The machine's answer rather than the request's, with the diagnostic that says what to
		// install — ARCHITECTURE's "媒体引擎不可用：禁用相关能力并显示诊断".
		return ExportRecord{}, domainmedia.Manifest{}, NotAvailableError(s.engine.Diagnostic())
	}
	episodeID := strings.TrimSpace(request.EpisodeID)
	if episodeID == "" {
		return ExportRecord{}, domainmedia.Manifest{}, InvalidError("An export must name its episode.")
	}
	timeline, err := s.timeline.Read(ctx, TimelineRequest{
		EpisodeID: episodeID, BoardVersionID: request.BoardVersionID,
	})
	if err != nil {
		return ExportRecord{}, domainmedia.Manifest{}, err
	}
	if len(timeline.Shots) == 0 {
		return ExportRecord{}, domainmedia.Manifest{}, InvalidError("That storyboard has no rows, so there is nothing to export.")
	}
	// A shot with no approved media is REFUSED rather than skipped: a film that quietly left out the
	// shots nobody had approved would be a film with holes, and the Final Ruleset's first rule would
	// report it afterwards rather than here where it can be fixed.
	if timeline.MissingMedia > 0 {
		return ExportRecord{}, domainmedia.Manifest{}, InvalidError(
			"Some shots have no approved media, so the episode cannot be exported yet.")
	}
	// The size: the request's, or the quality's default.
	size := domainmedia.Resolution{Width: request.Width, Height: request.Height}
	if size.Width == 0 || size.Height == 0 {
		size = domainmedia.ResolutionForQuality(request.Quality, "landscape")
	}
	if err := size.Validate(); err != nil {
		return ExportRecord{}, domainmedia.Manifest{}, err
	}
	fps := request.FPS
	if fps <= 0 {
		fps = domainmedia.DefaultExportFPS
	}
	if fps > domainmedia.MaxExportFPS {
		return ExportRecord{}, domainmedia.Manifest{}, InvalidError("The export's frame rate is higher than this build renders.")
	}
	mode := request.SubtitleMode
	if !domainmedia.IsValidSubtitleMode(mode) {
		return ExportRecord{}, domainmedia.Manifest{}, InvalidError("The subtitle mode is not recognised.")
	}

	// The scratch directory is this export's own, and it is removed on every path out.
	scratch, err := s.temp.NewScratchDir(ctx, "export-")
	if err != nil {
		return ExportRecord{}, domainmedia.Manifest{}, err
	}
	defer func() { _ = s.temp.RemoveScratchDir(scratch) }()

	// The segments: one per shot, in the board's order, each lasting its own duration. This is the
	// composition ADR-0015 section 2 records — approved panel IMAGES rather than shot videos, because
	// the video provider in this build is a mock whose payload is a container header and nothing
	// decodes it.
	segments := make([]Segment, 0, len(timeline.Shots))
	manifest := domainmedia.Manifest{
		SchemaVersion: domainmedia.ManifestSchemaVersion,
		EpisodeID:     episodeID,
		Quality:       string(request.Quality),
		Width:         size.Width,
		Height:        size.Height,
		FPS:           fps,
		SubtitleMode:  string(mode),
		Engine:        s.engine.Version(ctx),
		CreatedAt:     s.now(),
	}
	for _, shot := range timeline.Shots {
		path, err := s.materialise(ctx, scratch, shot)
		if err != nil {
			return ExportRecord{}, domainmedia.Manifest{}, err
		}
		segments = append(segments, Segment{
			Kind:       segmentKindFor(shot.MediaKind),
			Path:       path,
			DurationMS: shot.DurationMS,
			Label:      "shot " + itoa(shot.Ordinal),
		})
		manifest.References = append(manifest.References, domainmedia.ManifestReference{
			Kind:    domainmedia.RefPanel,
			ID:      shot.PanelVersionID,
			Hash:    shot.MediaHash,
			Label:   itoa(shot.Ordinal),
			ShotID:  shot.ShotID,
			Ordinal: shot.Ordinal,
		})
		manifest.References = append(manifest.References, domainmedia.ManifestReference{
			Kind: domainmedia.RefAssetVersion, ID: shot.MediaVersionID, Hash: shot.MediaHash,
			ShotID: shot.ShotID, Ordinal: shot.Ordinal,
		})
	}
	// The header references: what the film renders and what ordered it, which are the two a manifest
	// cannot be valid without.
	manifest.References = append([]domainmedia.ManifestReference{
		{Kind: domainmedia.RefScript, ID: timeline.ScriptVersionID},
		{Kind: domainmedia.RefStoryboard, ID: timeline.BoardVersionID},
	}, manifest.References...)

	// The subtitles, when there are any.
	subtitlePath := ""
	if mode != domainmedia.SubtitleNone && strings.TrimSpace(request.SubtitleTrackID) != "" {
		subtitlePath, err = s.writeSubtitles(ctx, scratch, strings.TrimSpace(request.SubtitleTrackID))
		if err != nil {
			return ExportRecord{}, domainmedia.Manifest{}, err
		}
		manifest.References = append(manifest.References, domainmedia.ManifestReference{
			Kind: domainmedia.RefSubtitleTrack, ID: strings.TrimSpace(request.SubtitleTrackID),
		})
	}

	// THE SOUND, which the export used to omit entirely: the request carried segments and a subtitle
	// path and no audio at all, so every film was silent while the timeline reported that the episode
	// had audio. The mix is built before the compose call so a refusal (no reader, a version with no
	// file, too many clips) happens before any encoding work.
	mix, audioReferences, err := s.buildMix(ctx, scratch, timeline)
	if err != nil {
		return ExportRecord{}, domainmedia.Manifest{}, err
	}
	// Every clip that was composed is a manifest reference, which is what makes the sound traceable
	// in the same way the picture is: a reader can see which approved audio version a film contains.
	manifest.References = append(manifest.References, audioReferences...)

	outputPath := filepath.Join(scratch, "episode.mp4")
	result, err := s.engine.Compose(ctx, ComposeRequest{
		Segments:     segments,
		Width:        size.Width,
		Height:       size.Height,
		FPS:          fps,
		AudioMix:     mix,
		SubtitlePath: subtitlePath,
		SubtitleMode: mode,
		OutputPath:   outputPath,
	})
	if err != nil {
		return ExportRecord{}, domainmedia.Manifest{}, err
	}
	manifest.DurationMS = result.DurationMS
	if err := manifest.Validate(); err != nil {
		return ExportRecord{}, domainmedia.Manifest{}, err
	}

	// The bytes go through the STORE rather than the result pipeline: a locally composed file is not a
	// provider result, and the pipeline's allowlist would refuse it as one. ADR-0015 section 5.
	stored, err := s.putFile(ctx, outputPath, "episode-"+episodeID+".mp4")
	if err != nil {
		return ExportRecord{}, domainmedia.Manifest{}, err
	}

	exportID, err := s.ids.New()
	if err != nil {
		return ExportRecord{}, domainmedia.Manifest{}, NotAvailableError("The export could not be identified.")
	}
	highest, err := s.exports.MaxExportVersionNumber(ctx, episodeID)
	if err != nil {
		return ExportRecord{}, domainmedia.Manifest{}, err
	}
	manifest.ExportVersionNumber = highest + 1
	document, err := manifest.Encode()
	if err != nil {
		return ExportRecord{}, domainmedia.Manifest{}, err
	}
	record := ExportRecord{
		ID:               exportID,
		EpisodeID:        episodeID,
		VersionNumber:    manifest.ExportVersionNumber,
		Status:           "draft",
		Quality:          string(request.Quality),
		Width:            size.Width,
		Height:           size.Height,
		DurationMS:       result.DurationMS,
		OutputFileHash:   stored.Hash,
		SubtitleTrackID:  strings.TrimSpace(request.SubtitleTrackID),
		ManifestJSON:     document,
		SourceAgentRunID: strings.TrimSpace(request.SourceAgentRunID),
		CreatedByType:    createdByOrDefault(request.CreatedByType),
		CreatedByID:      strings.TrimSpace(request.CreatedByID),
		ChangeReason:     "Composed from the approved storyboard.",
		CreatedAt:        s.now(),
	}
	if err := record.Validate(); err != nil {
		return ExportRecord{}, domainmedia.Manifest{}, err
	}
	if err := s.exports.CreateExport(ctx, record); err != nil {
		return ExportRecord{}, domainmedia.Manifest{}, err
	}
	return record, manifest, nil
}

// buildMix turns the timeline's audio into the mix the engine composes (FR-080's 简单混音).
//
// # What this fixes, stated as the defect it was
//
// The export used to pass NO audio at all: `Compose` was given the segments and a subtitle path, and
// `ComposeRequest` had no audio field in the call. Every export was therefore a silent film while
// `TimelineShot.HasAudio` reported that the episode had audio — the acceptance walk recorded the
// silence as a known limit rather than as a bug, and this function is what closes it.
//
// # How a clip's position is decided
//
// A dialogue clip is placed at its SHOT's start. The timeline already computes that running total —
// it places the cues against it — so the shot carries `StartMS` and this function does not redo the
// arithmetic: a second implementation of "where does shot six begin" is a second answer, and the two
// would eventually disagree about the film.
//
// # Why the order is dialogue, then effects, then music
//
// The engine mixes by summing, so the order does not change the sound — it changes the NUMBERING in
// ffmpeg's argument list, which is what a failure a reader sees refers to. Putting dialogue first
// means "the third input is a line" stays true as a project grows, which is what makes an error
// message about input 3 actionable. A music bed is last because it is the one clip whose placement
// does not depend on a shot.
// buildMix stages every approved audio version and lays them out over the film.
//
// # The order the clips are added in, and why it is not the order they are read in
//
// BEDS FIRST, THEN EFFECTS, THEN DIALOGUE. The mix is `amix`, which is symmetric — the order does not
// change the sound — and it DOES change two things a reader depends on:
//
//   - ffmpeg numbers the inputs by the order they are given, and a failure names one of them. A stable
//     order makes that number map back to a clip, which is what `Label` is for.
//   - the `AudioMix` a caller inspects should read the way a mixer's channels do, with the bed at the
//     bottom. It is the order the plan (ADR-0024) described and the order a person expects.
//
// # The defect this function used to carry
//
// Every clip was passed `AudioRoleDialogue` as a HARDCODED ARGUMENT, and the `music` slice was
// initialized empty and never appended to. The consequence was that `DefaultGainFor(AudioRoleMusic)`
// — a documented 0.35, reasoned about at length in `audio.go` — was a rule no code could reach: an
// imported music bed would have mixed at unity and buried the dialogue it was supposed to sit under.
// The role now comes from the row's `usage_role`, so the rule is reachable and the defect is
// unrepresentable: there is no argument left to get wrong.
func (s *ExportService) buildMix(ctx context.Context, scratch string, timeline Timeline) (AudioMix, []domainmedia.ManifestReference, error) {
	// Three lists in mix order, each sized from the whole timeline because a shot may contribute any
	// number of clips of any role.
	music := make([]AudioClip, 0, 1)
	effects := make([]AudioClip, 0, len(timeline.Shots))
	dialogue := make([]AudioClip, 0, len(timeline.Shots))
	references := make([]domainmedia.ManifestReference, 0)
	for _, shot := range timeline.Shots {
		for index, audio := range shot.AudioClips {
			// The label names the role as well as the position, because a failure that said "shot 3
			// line 2" for a music bed would send a reader looking for a line of dialogue.
			label := "shot " + itoa(shot.Ordinal) + " " + string(audio.Role) + " " + itoa(index+1)
			// WHERE THE CLIP STARTS DEPENDS ON ITS ROLE, and passing the shot's start for every role was
			// a defect a WP-29 probe found: `AudioClip.StartMS` documents that "zero means the beginning,
			// which is where a music bed starts and where a dialogue clip does NOT", and the code gave
			// EVERY clip the shot's offset. A bed attached to shot 3 of a four-second-per-shot episode
			// therefore began at 8000ms — the first eight seconds of the film were silent, which is the
			// opposite of what a music bed is for.
			//
			// The existing tests could not see it because every one of them attached its music to the
			// FIRST shot, where the shot's start is zero and the two answers coincide. A fixture that
			// puts a bed anywhere but the beginning is what makes the difference observable.
			startMS := shot.StartMS
			if audio.Role == AudioRoleMusic {
				// A BED RUNS FROM THE TOP regardless of which shot it was attached to. The attachment
				// is how a user says "this episode has this music"; the placement is the mixer's, and a
				// user who wants the bed to begin later says so by trimming it rather than by hanging it
				// off a later shot.
				startMS = 0
			}
			// THE USE'S OWN PLACEMENT (T05) overrides the role's default: the
			// stored offset moves the clip relative to that default, and the
			// trim/volume/mute travel with the clip. A use with no parameters
			// keeps the behaviour every row written before the column existed
			// had — the sparse-document rule `TrackParams` states.
			if audio.Params.OffsetMS != nil {
				startMS += *audio.Params.OffsetMS
				if startMS < 0 {
					startMS = 0
				}
			}
			clip, reference, err := s.audioClipFor(ctx, scratch, audio.VersionID, audio.Role, startMS, label)
			if err != nil {
				return AudioMix{}, nil, err
			}
			// Trim and volume are the USE's, applied after the file is staged.
			// Mute is applied as a zero gain — the mix keeps the clip's row so
			// the label and the manifest still name it, and a `volume=0` is
			// what the engine formats for silence.
			if audio.Params.SourceStartMS != nil && *audio.Params.SourceStartMS > 0 {
				clip.SourceStartMS = *audio.Params.SourceStartMS
			}
			if audio.Params.SourceEndMS != nil && *audio.Params.SourceEndMS > 0 {
				clip.SourceEndMS = *audio.Params.SourceEndMS
			}
			if audio.Params.DurationMS != nil && *audio.Params.DurationMS > 0 {
				clip.DurationMS = *audio.Params.DurationMS
			}
			if audio.Params.Volume != nil {
				clip.Gain = *audio.Params.Volume
			}
			if audio.Params.Muted != nil && *audio.Params.Muted {
				clip.Gain = 0
			}
			if audio.Params.Volume != nil || (audio.Params.Muted != nil && *audio.Params.Muted) {
				clip.Overrides = &TrackOverrides{Volume: audio.Params.Volume, Muted: audio.Params.Muted != nil && *audio.Params.Muted}
			}
			references = append(references, reference)
			switch audio.Role {
			case AudioRoleMusic:
				music = append(music, clip)
			case AudioRoleEffect:
				effects = append(effects, clip)
			default:
				dialogue = append(dialogue, clip)
			}
		}
	}
	// The mix is bounded, and the bound is checked HERE rather than left to the engine: a refusal
	// that names the count is actionable, while an ffmpeg argument-limit failure says nothing about
	// what the user should remove.
	total := len(music) + len(effects) + len(dialogue)
	if total > MaxAudioClips() {
		return AudioMix{}, nil, LimitError(
			"That episode needs more audio clips than one composition mixes. Export a scene at a time.")
	}
	clips := make([]AudioClip, 0, total)
	clips = append(clips, music...)
	clips = append(clips, effects...)
	clips = append(clips, dialogue...)
	// NORMALIZED HERE, and the omission was a second reason the music default was unreachable.
	//
	// `AudioMix.Normalized` resolves every clip's unstated gain to its role's default — 0.35 for music
	// — and NOTHING CALLED IT. The function existed, was documented, and had a test of its own, while
	// the only path that reaches the engine built its clips with a zero gain. The engine then formatted
	// `volume=0`, so an imported bed would have been SILENT rather than merely too loud: a worse
	// outcome than the one the missing role caused, and one that no test of `Normalized` could see
	// because the defect was the absence of a call.
	//
	// The adapter formats the gain with `%g`, so a zero here is a literal `volume=0`. Normalizing at
	// the point the mix is BUILT is what makes "unstated means the role's default" true of the value
	// the engine receives, and `TestTheComposedRequestOrdersBedsBeforeEffectsBeforeDialogue` asserts the
	// 0.35 in the request rather than in the struct this function returns.
		mix := AudioMix{Clips: clips}.Normalized()
	// THE USE'S OWN LEVEL (T05), applied AFTER normalization so an explicit
	// value overrides the role default: a stated volume replaces it, and a
	// stated mute is literal silence — `volume=0` — rather than the default
	// the zero-gain convention would otherwise have Normalized into.
	for index, clip := range mix.Clips {
		if clip.Overrides != nil && clip.Overrides.Muted {
			mix.Clips[index].Gain = 0
			continue
		}
		if clip.Overrides != nil && clip.Overrides.Volume != nil {
			mix.Clips[index].Gain = *clip.Overrides.Volume
		}
	}
	return mix, references, nil
}

// audioClipFor stages one approved audio version and builds its clip.
//
// It is the same staging the picture takes — bytes copied into this export's own scratch directory
// under a name this adapter chose — because the engine must never be handed a path into the store.
func (s *ExportService) audioClipFor(ctx context.Context, scratch, versionID string, role AudioRole, startMS int, label string) (AudioClip, domainmedia.ManifestReference, error) {
	if s.audio == nil {
		// Fail closed rather than composing a silent film: an episode WITH audio whose export dropped
		// it is the defect this function exists to fix, and reproducing it quietly would be worse
		// than refusing. A build with no audio reader cannot export a voiced episode.
		return AudioClip{}, domainmedia.ManifestReference{}, NotAvailableError(
			"This build cannot read an episode's audio, so it will not export a silent film in its place.")
	}
	hash, found, err := s.audio.AudioFileFor(ctx, versionID)
	if err != nil {
		return AudioClip{}, domainmedia.ManifestReference{}, err
	}
	if !found {
		return AudioClip{}, domainmedia.ManifestReference{}, InvalidError(
			"An approved line of dialogue has no audio file, so the film cannot be composed.")
	}
	reader, err := s.files.Open(ctx, hash)
	if err != nil {
		return AudioClip{}, domainmedia.ManifestReference{}, err
	}
	defer reader.Close()
	name := "audio-" + safeSegment(label) + ".m4a"
	path := filepath.Join(scratch, name)
	file, err := os.Create(path)
	if err != nil {
		return AudioClip{}, domainmedia.ManifestReference{}, StorageError(
			"The dialogue could not be staged for export.", err)
	}
	defer file.Close()
	if _, err := io.Copy(file, reader); err != nil {
		return AudioClip{}, domainmedia.ManifestReference{}, StorageError(
			"The dialogue could not be staged for export.", err)
	}
	return AudioClip{Role: role, Path: path, StartMS: startMS, Label: label},
		domainmedia.ManifestReference{Kind: domainmedia.RefAssetVersion, ID: versionID, Hash: hash},
		nil
}

// safeSegment renders a label as a filename segment.
//
// The label is built from a shot ordinal and a line index — integers — so this is not sanitising
// user text; it is making the CONTRACT explicit, so that a later change which put a filename or a
// user string in a label cannot silently reach the filesystem.
func safeSegment(label string) string {
	builder := strings.Builder{}
	for _, symbol := range label {
		switch {
		case symbol >= 'a' && symbol <= 'z', symbol >= 'A' && symbol <= 'Z',
			symbol >= '0' && symbol <= '9', symbol == '-':
			builder.WriteRune(symbol)
		default:
			builder.WriteRune('-')
		}
	}
	return builder.String()
}

// materialise copies one shot's approved media out of the store and into the scratch directory.
//
// The engine needs a PATH, and the store is content-addressed with a key the engine must never see:
// handing it a path into the store would let a media tool write where the application's own files
// live. So the bytes are copied into this export's scratch directory under a name this adapter
// chose, and the engine works there.
func (s *ExportService) materialise(ctx context.Context, scratch string, shot TimelineShot) (string, error) {
	reader, err := s.files.Open(ctx, shot.MediaHash)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	path := filepath.Join(scratch, "shot-"+itoa(shot.Ordinal)+extensionFor(shot.MediaKind))
	file, err := os.Create(path)
	if err != nil {
		return "", StorageError("The shot's media could not be staged for export.", err)
	}
	defer file.Close()
	if _, err := io.Copy(file, reader); err != nil {
		return "", StorageError("The shot's media could not be staged for export.", err)
	}
	return path, nil
}

// writeSubtitles renders the track into the scratch directory as an SRT.
//
// SRT rather than VTT because the engine's muxer and its subtitle filter both take SubRip, and a
// conversion step would be a second place for the timing to be reinterpreted. The VTT form is what
// the EXPORT to a user produces; this is what a muxer is given.
func (s *ExportService) writeSubtitles(ctx context.Context, scratch, trackID string) (string, error) {
	if s.subtitle == nil {
		return "", InvalidError("This build cannot read subtitles, so an export cannot include them.")
	}
	cues, err := s.subtitle.Cues(ctx, trackID)
	if err != nil {
		return "", err
	}
	if len(cues) == 0 {
		return "", InvalidError("That subtitle track has no cues, so there is nothing to mux.")
	}
	path := filepath.Join(scratch, "subtitles.srt")
	if err := os.WriteFile(path, []byte(domainmedia.RenderSRT(cues)), 0o600); err != nil {
		return "", StorageError("The subtitles could not be staged for export.", err)
	}
	return path, nil
}

// putFile streams a composed file into the store.
func (s *ExportService) putFile(ctx context.Context, path, displayName string) (StoredObject, error) {
	file, err := os.Open(path)
	if err != nil {
		return StoredObject{}, StorageError("The export could not be read back.", err)
	}
	defer file.Close()
	stored, err := s.files.Put(ctx, displayName, file)
	if err != nil {
		return StoredObject{}, err
	}
	return stored, nil
}

// Open returns a stored export's bytes, for the save dialog to stream to the user.
//
// It is here rather than on the binding so the path from a storage key to bytes has one
// implementation, and so a binding cannot be handed a path.
func (s *ExportService) Open(ctx context.Context, storageKey string) (io.ReadCloser, error) {
	if !s.Available() {
		return nil, NotAvailableError("No export service is configured.")
	}
	return s.files.Open(ctx, storageKey)
}

// Exports returns an episode's exports newest version first.
func (s *ExportService) Exports(ctx context.Context, episodeID string) ([]ExportRecord, error) {
	if s == nil || s.exports == nil {
		return nil, NotAvailableError("No export service is configured.")
	}
	return s.exports.ListExports(ctx, strings.TrimSpace(episodeID))
}

// Export returns one export record.
func (s *ExportService) ExportRecord(ctx context.Context, id string) (ExportRecord, error) {
	if s == nil || s.exports == nil {
		return ExportRecord{}, NotAvailableError("No export service is configured.")
	}
	return s.exports.GetExport(ctx, strings.TrimSpace(id))
}

// SubmitForReview moves a draft export to the state an approval requires.
//
// # Why this is a command rather than part of Approve
//
// `ApproveExport` refuses a row that is not `under_review`, and before this method nothing in the
// build could put one there — see the port's comment. The two acts are kept separate because they
// are a person's two decisions: this one says "I am asking for this to be reviewed", and the
// approval says "I have reviewed it". A command that did both would let a caller approve an export
// nobody had looked at, which is the shape every other version family in this build avoids by
// refusing an approval whose expected status is wrong.
func (s *ExportService) SubmitForReview(ctx context.Context, exportID, episodeID string) (ExportRecord, error) {
	if s == nil || s.exports == nil {
		return ExportRecord{}, NotAvailableError("No export service is configured.")
	}
	record, err := s.exports.GetExport(ctx, strings.TrimSpace(exportID))
	if err != nil {
		return ExportRecord{}, err
	}
	if named := strings.TrimSpace(episodeID); named != "" && record.EpisodeID != named {
		return ExportRecord{}, InvalidError("That export belongs to a different episode.")
	}
	if err := s.exports.MarkUnderReview(ctx, record.ID); err != nil {
		return ExportRecord{}, err
	}
	return s.exports.GetExport(ctx, record.ID)
}

// Approve puts an export in force.
//
// The trace identifier is the user gate decision's, for the reason the subtitle track's approval
// states: section 17's event list is closed and has no export event, so the governance travels on the
// workflow's stream and this column is the link.
func (s *ExportService) Approve(ctx context.Context, exportID, episodeID, traceID string) (ExportRecord, error) {
	if s == nil || s.exports == nil {
		return ExportRecord{}, NotAvailableError("No export service is configured.")
	}
	record, err := s.exports.GetExport(ctx, strings.TrimSpace(exportID))
	if err != nil {
		return ExportRecord{}, err
	}
	if named := strings.TrimSpace(episodeID); named != "" && record.EpisodeID != named {
		return ExportRecord{}, InvalidError("That export belongs to a different episode.")
	}
	if err := s.exports.ApproveExport(ctx, record.ID, record.EpisodeID, strings.TrimSpace(traceID), s.now()); err != nil {
		return ExportRecord{}, err
	}
	return s.exports.GetExport(ctx, record.ID)
}

// itoa renders a small integer, so a message can quote a position without a conversion at each site.
func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := []byte{}
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}

// segmentKindFor maps an asset kind to how the engine treats it.
//
// A `video` asset is a clip and everything else is a still, which is what the media pipeline produces
// today: the image batch writes PNGs and the video provider is a mock. A kind this build does not
// recognise is treated as a STILL rather than refused, because a poster frame and a thumbnail are both
// stills and neither is a clip — and the engine's own probe refuses a file it cannot read.
func segmentKindFor(assetKind string) SegmentKind {
	if assetKind == "video" {
		return SegmentVideo
	}
	return SegmentImage
}

// extensionFor names a staged file, because the engine infers a format from the name.
//
// It is not a security boundary — the bytes decide what the file is, and the engine sniffs them — but
// ffmpeg's demuxer selection uses the extension when it can, and a file called `shot-1.bin` would make
// it guess.
func extensionFor(assetKind string) string {
	if assetKind == "video" {
		return ".mp4"
	}
	return ".png"
}
