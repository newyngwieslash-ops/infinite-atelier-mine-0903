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

// ExportRepository records what an export was.
type ExportRepository interface {
	CreateExport(ctx context.Context, record ExportRecord) error
	GetExport(ctx context.Context, id string) (ExportRecord, error)
	ListExports(ctx context.Context, episodeID string) ([]ExportRecord, error)
	MaxExportVersionNumber(ctx context.Context, episodeID string) (int, error)
	CurrentApprovedExport(ctx context.Context, episodeID string) (ExportRecord, bool, error)
	ApproveExport(ctx context.Context, exportID, episodeID, traceID string, at time.Time) error
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

	outputPath := filepath.Join(scratch, "episode.mp4")
	result, err := s.engine.Compose(ctx, ComposeRequest{
		Segments:     segments,
		Width:        size.Width,
		Height:       size.Height,
		FPS:          fps,
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
