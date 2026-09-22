package desktop

import (
	"context"
	"io"
	"strings"
	"sync"

	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	domainmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/media"
)

// MediaBinding is the Wails surface for the video, audio and timeline sections.
//
// # The one thing this surface does that nothing else in the application did
//
// `SaveFile` writes a file to a location the USER chose. Before this package the build had no such
// path at all: `ExportBackup` returns base64 and had no caller, and every other "save" in the
// application is a browser download from `file-saver` reading browser-local storage rather than the
// Go file store. AC-MEDIA-003's export and FR-080's "输出包含导出清单和版本信息" both end at a file,
// so the path had to be built.
//
// # Where the destination comes from, and where it does not
//
// The request names a STORAGE KEY and a suggested filename. It cannot name a destination: the path is
// whatever the save dialog returned, which is the user pointing at it. SECURITY section 11 allows
// writing only where the user pointed — "用户选择导出目录时只写明确目标" — and a `destination`
// parameter would be the opposite of that, because a frontend that had been compromised could write
// anywhere.
//
// # What it deliberately does not expose
//
// It does not send bytes as base64. A two-minute MP4 exceeds `ReadResultFile`'s 64 MiB data-URL cap,
// so an export built on that reader would fail on every real episode with an error that reads like a
// size limit rather than a limit hit by design. ADR-0015 section 10 records the ruling.

// MediaBinding is the Wails surface for composing, subtitling and exporting an episode.
type MediaBinding struct {
	mu       sync.RWMutex
	ctx      context.Context
	exports  *appmedia.ExportService
	subs     *appmedia.SubtitleService
	timeline *appmedia.TimelineService
	// saveFile is the dialog plus the copy, injected so a test can exercise the write without a
	// window. A nil one leaves SaveFile failing closed with a reason.
	saveFile func(ctx context.Context, suggestedName string, write func(io.Writer) error) (string, error)
}

// AttachMedia supplies the media services and the save path.
func AttachMedia(binding *MediaBinding, ctx context.Context, exports *appmedia.ExportService,
	subs *appmedia.SubtitleService, timeline *appmedia.TimelineService,
	saveFile func(ctx context.Context, suggestedName string, write func(io.Writer) error) (string, error)) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.ctx = ctx
	binding.exports = exports
	binding.subs = subs
	binding.timeline = timeline
	binding.saveFile = saveFile
	binding.mu.Unlock()
}

// MediaBindingUnavailable is the fail-closed error the composition root returns.
func MediaBindingUnavailable() error {
	return bindingUnavailable()
}

// mediaError extracts a media error's category and safe message.
//
// It maps BOTH of the media packages, and the two are separate calls because they are separate
// packages with separate taxonomies: `internal/application/media` adds the engine categories —
// `engine_unavailable` among them — and `internal/domain/media` owns the four the data model uses. A
// binding that mapped only the application's would leave every domain refusal — an absent track, a cue
// that ends before it starts — arriving at the frontend as the generic drama failure, which is the
// state this pair of functions was written to end.
func mediaError(err error) (category string, safeMessage string, ok bool) {
	if mediaErr, is := appmedia.AsError(err); is {
		return string(mediaErr.Category), mediaErr.SafeMessage, true
	}
	if mediaErr, is := domainmedia.AsError(err); is {
		return string(mediaErr.Category), mediaErr.SafeMessage, true
	}
	return "", "", false
}

func (b *MediaBinding) context() context.Context {
	if b == nil {
		return context.Background()
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.ctx == nil {
		return context.Background()
	}
	return b.ctx
}

func (b *MediaBinding) exportService() *appmedia.ExportService {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.exports
}

func (b *MediaBinding) subtitleService() *appmedia.SubtitleService {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.subs
}

func (b *MediaBinding) timelineService() *appmedia.TimelineService {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.timeline
}

func (b *MediaBinding) savePath() func(context.Context, string, func(io.Writer) error) (string, error) {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.saveFile
}

// MediaCapabilityDTO reports what this machine can do.
//
// It is a read rather than an error, because "this machine has no ffmpeg" is a fact a section renders
// as a disabled button and a diagnostic, not a failure that arrives when somebody presses it.
// ARCHITECTURE: "媒体引擎不可用：禁用相关能力并显示诊断".
type MediaCapabilityDTO struct {
	ExportAvailable bool   `json:"exportAvailable"`
	Diagnostic      string `json:"diagnostic,omitempty"`
	SaveAvailable   bool   `json:"saveAvailable"`
}

// MediaCapability reports whether export and save are available, and why not when they are not.
func (b *MediaBinding) MediaCapability() (MediaCapabilityDTO, error) {
	exports := b.exportService()
	capability := MediaCapabilityDTO{SaveAvailable: b.savePath() != nil}
	if exports == nil {
		capability.Diagnostic = "The media services are not composed."
		return capability, nil
	}
	capability.ExportAvailable = exports.Available() && exports.Diagnostic() == ""
	capability.Diagnostic = exports.Diagnostic()
	return capability, nil
}

// TimelineShotDTO is one shot's place in the episode.
type TimelineShotDTO struct {
	Ordinal        int    `json:"ordinal"`
	ItemID         string `json:"itemId"`
	ShotID         string `json:"shotId"`
	DurationMS     int    `json:"durationMs"`
	MediaVersionID string `json:"mediaVersionId,omitempty"`
	MediaHash      string `json:"mediaHash,omitempty"`
	MediaKind      string `json:"mediaKind,omitempty"`
	PanelVersionID string `json:"panelVersionId,omitempty"`
	HasAudio       bool   `json:"hasAudio"`
	CueCount       int    `json:"cueCount"`
}

// TimelineDTO is an episode's timeline, which the section renders and the export consumes.
type TimelineDTO struct {
	EpisodeID       string            `json:"episodeId"`
	BoardVersionID  string            `json:"boardVersionId"`
	ScriptVersionID string            `json:"scriptVersionId"`
	Shots           []TimelineShotDTO `json:"shots"`
	TotalDurationMS int               `json:"totalDurationMs"`
	MissingMedia    int               `json:"missingMedia"`
	CueCount        int               `json:"cueCount"`
	MissingLines    int               `json:"missingLines"`
}

// ReadTimeline returns an episode's ordered shots and their totals.
func (b *MediaBinding) ReadTimeline(request TimelineRequest) (TimelineDTO, error) {
	service := b.timelineService()
	if service == nil {
		return TimelineDTO{}, MediaBindingUnavailable()
	}
	timeline, err := service.Read(b.context(), appmedia.TimelineRequest{
		EpisodeID:      strings.TrimSpace(request.EpisodeID),
		BoardVersionID: strings.TrimSpace(request.BoardVersionID),
	})
	if err != nil {
		return TimelineDTO{}, toDramaError(err)
	}
	view := TimelineDTO{
		EpisodeID:       timeline.EpisodeID,
		BoardVersionID:  timeline.BoardVersionID,
		ScriptVersionID: timeline.ScriptVersionID,
		Shots:           make([]TimelineShotDTO, 0, len(timeline.Shots)),
		TotalDurationMS: timeline.TotalDurationMS,
		MissingMedia:    timeline.MissingMedia,
		CueCount:        timeline.CueCount,
		MissingLines:    timeline.MissingLines,
	}
	for _, shot := range timeline.Shots {
		view.Shots = append(view.Shots, TimelineShotDTO{
			Ordinal:        shot.Ordinal,
			ItemID:         shot.ItemID,
			ShotID:         shot.ShotID,
			DurationMS:     shot.DurationMS,
			MediaVersionID: shot.MediaVersionID,
			MediaHash:      shot.MediaHash,
			MediaKind:      shot.MediaKind,
			PanelVersionID: shot.PanelVersionID,
			HasAudio:       shot.HasAudio,
			CueCount:       shot.CueCount,
		})
	}
	return view, nil
}

// TimelineRequest names the episode a timeline read is about.
type TimelineRequest struct {
	EpisodeID      string `json:"episodeId"`
	BoardVersionID string `json:"boardVersionId,omitempty"`
}

// SubtitleCueDTO is one cue as the editor shows it.
type SubtitleCueDTO struct {
	ID                string `json:"id"`
	Ordinal           int    `json:"ordinal"`
	StartMS           int64  `json:"startMs"`
	EndMS             int64  `json:"endMs"`
	Text              string `json:"text"`
	CharacterEntityID string `json:"characterEntityId,omitempty"`
	DialogueLineID    string `json:"dialogueLineId,omitempty"`
	Status            string `json:"status"`
}

// SubtitleTrackDTO is one track.
type SubtitleTrackDTO struct {
	ID              string `json:"id"`
	EpisodeID       string `json:"episodeId"`
	ScriptVersionID string `json:"scriptVersionId"`
	VersionNumber   int    `json:"versionNumber"`
	Status          string `json:"status"`
	CreatedAt       string `json:"createdAt"`
}

func toSubtitleTrackDTO(track domainmedia.Track) SubtitleTrackDTO {
	return SubtitleTrackDTO{
		ID: track.ID, EpisodeID: track.EpisodeID, ScriptVersionID: track.ScriptVersionID,
		VersionNumber: track.VersionNumber, Status: track.Status,
		CreatedAt: rfc3339OrEmpty(track.CreatedAt),
	}
}

func toSubtitleCueDTOs(cues []domainmedia.Cue) []SubtitleCueDTO {
	views := make([]SubtitleCueDTO, 0, len(cues))
	for _, cue := range cues {
		views = append(views, SubtitleCueDTO{
			ID: cue.ID, Ordinal: cue.Ordinal, StartMS: int64(cue.Start), EndMS: int64(cue.End),
			Text: cue.Text, CharacterEntityID: cue.CharacterEntityID,
			DialogueLineID: cue.DialogueLineID, Status: string(cue.Status),
		})
	}
	return views
}

// DraftSubtitlesRequest generates a starting track from a script version.
type DraftSubtitlesRequest struct {
	EpisodeID       string `json:"episodeId"`
	ScriptVersionID string `json:"scriptVersionId"`
}

// SubtitleDraftDTO is what a draft produced.
type SubtitleDraftDTO struct {
	Track SubtitleTrackDTO `json:"track"`
	Cues  []SubtitleCueDTO `json:"cues"`
}

// DraftSubtitles builds a subtitle draft from the script's spoken lines.
func (b *MediaBinding) DraftSubtitles(request DraftSubtitlesRequest) (SubtitleDraftDTO, error) {
	service := b.subtitleService()
	if service == nil {
		return SubtitleDraftDTO{}, MediaBindingUnavailable()
	}
	track, cues, err := service.Draft(b.context(), appmedia.DraftRequest{
		EpisodeID:       strings.TrimSpace(request.EpisodeID),
		ScriptVersionID: strings.TrimSpace(request.ScriptVersionID),
		CreatedByType:   "user",
	})
	if err != nil {
		return SubtitleDraftDTO{}, toDramaError(err)
	}
	return SubtitleDraftDTO{Track: toSubtitleTrackDTO(track), Cues: toSubtitleCueDTOs(cues)}, nil
}

// ListSubtitleTracks returns an episode's tracks newest version first.
func (b *MediaBinding) ListSubtitleTracks(episodeID string) ([]SubtitleTrackDTO, error) {
	service := b.subtitleService()
	if service == nil {
		return nil, MediaBindingUnavailable()
	}
	tracks, err := service.Tracks(b.context(), episodeID)
	if err != nil {
		return nil, toDramaError(err)
	}
	views := make([]SubtitleTrackDTO, 0, len(tracks))
	for _, track := range tracks {
		views = append(views, toSubtitleTrackDTO(track))
	}
	return views, nil
}

// ListSubtitleCues returns a track's cues in order.
func (b *MediaBinding) ListSubtitleCues(trackID string) ([]SubtitleCueDTO, error) {
	service := b.subtitleService()
	if service == nil {
		return nil, MediaBindingUnavailable()
	}
	cues, err := service.Cues(b.context(), trackID)
	if err != nil {
		return nil, toDramaError(err)
	}
	return toSubtitleCueDTOs(cues), nil
}

// SubtitleCueEdit is one cue as the editor states it.
type SubtitleCueEdit struct {
	// ID is empty for a cue the user added, which is what the service mints one for.
	ID                string `json:"id,omitempty"`
	StartMS           int64  `json:"startMs"`
	EndMS             int64  `json:"endMs"`
	Text              string `json:"text"`
	CharacterEntityID string `json:"characterEntityId,omitempty"`
	DialogueLineID    string `json:"dialogueLineId,omitempty"`
}

// EditSubtitleCues replaces a track's cues, which is AC-MEDIA-002's "subtitle editable".
func (b *MediaBinding) EditSubtitleCues(trackID string, edits []SubtitleCueEdit) ([]SubtitleCueDTO, error) {
	service := b.subtitleService()
	if service == nil {
		return nil, MediaBindingUnavailable()
	}
	cues := make([]appmedia.CueEdit, 0, len(edits))
	for _, edit := range edits {
		cues = append(cues, appmedia.CueEdit{
			ID: edit.ID, StartMS: edit.StartMS, EndMS: edit.EndMS, Text: edit.Text,
			CharacterEntityID: edit.CharacterEntityID, DialogueLineID: edit.DialogueLineID,
		})
	}
	saved, err := service.Edit(b.context(), appmedia.EditRequest{
		TrackID: strings.TrimSpace(trackID), Cues: cues,
	})
	if err != nil {
		return nil, toDramaError(err)
	}
	return toSubtitleCueDTOs(saved), nil
}

// MissingLineDTO is one spoken line a track does not cover.
type MissingLineDTO struct {
	LineID            string `json:"lineId"`
	Type              string `json:"type"`
	CharacterEntityID string `json:"characterEntityId,omitempty"`
	Text              string `json:"text"`
}

// MissingSubtitleLines reports the spoken lines a track has no cue for.
func (b *MediaBinding) MissingSubtitleLines(trackID string) ([]MissingLineDTO, error) {
	service := b.subtitleService()
	if service == nil {
		return nil, MediaBindingUnavailable()
	}
	missing, err := service.Missing(b.context(), appmedia.MissingLinesRequest{TrackID: trackID})
	if err != nil {
		return nil, toDramaError(err)
	}
	views := make([]MissingLineDTO, 0, len(missing))
	for _, line := range missing {
		views = append(views, MissingLineDTO{
			LineID: line.LineID, Type: line.Type,
			CharacterEntityID: line.CharacterEntityID, Text: line.Text,
		})
	}
	return views, nil
}

// ExportSubtitlesRequest renders a track as a subtitle file.
type ExportSubtitlesRequest struct {
	TrackID string `json:"trackId"`
	Format  string `json:"format"`
}

// ExportSubtitles renders a track as SRT or VTT and writes it to a name this method chooses.
//
// It returns the TEXT rather than a file: the section shows the document so a user can read what they
// are about to save, and `SaveFile` is what writes it to where they point. Two steps rather than one
// because a user who wants to check their subtitles before exporting them cannot do that if the first
// press is the save dialog.
func (b *MediaBinding) ExportSubtitles(request ExportSubtitlesRequest) (string, error) {
	service := b.subtitleService()
	if service == nil {
		return "", MediaBindingUnavailable()
	}
	format := domainmedia.SubtitleFormat(strings.TrimSpace(request.Format))
	document, err := service.Export(b.context(), appmedia.SubtitleExportRequest{
		TrackID: strings.TrimSpace(request.TrackID), Format: format,
	})
	if err != nil {
		return "", toDramaError(err)
	}
	return document, nil
}

// ApproveSubtitleTrackRequest puts a track in force.
type ApproveSubtitleTrackRequest struct {
	TrackID   string `json:"trackId"`
	EpisodeID string `json:"episodeId,omitempty"`
	TraceID   string `json:"traceId,omitempty"`
}

// ApproveSubtitleTrack approves a track.
func (b *MediaBinding) ApproveSubtitleTrack(request ApproveSubtitleTrackRequest) (SubtitleTrackDTO, error) {
	service := b.subtitleService()
	if service == nil {
		return SubtitleTrackDTO{}, MediaBindingUnavailable()
	}
	track, err := service.Approve(b.context(), request.TrackID, request.EpisodeID, request.TraceID)
	if err != nil {
		return SubtitleTrackDTO{}, toDramaError(err)
	}
	return toSubtitleTrackDTO(track), nil
}

// SubmitSubtitleTrackForReviewRequest moves a draft track to the state an approval requires.
type SubmitSubtitleTrackForReviewRequest struct {
	TrackID   string `json:"trackId"`
	EpisodeID string `json:"episodeId,omitempty"`
}

// SubmitSubtitleTrackForReview moves a draft track to review.
//
// It is exposed because without it `ApproveSubtitleTrack` can never succeed: the approval refuses a
// track that is not `under_review`, and nothing else in the build writes that status. A section that
// offered only the approve button would be offering a control whose every press produced a conflict
// message about a state the user could not reach — the "interface with no real path" shape AGENTS
// section 12 refuses, arriving at the UI instead of in the code.
func (b *MediaBinding) SubmitSubtitleTrackForReview(request SubmitSubtitleTrackForReviewRequest) (SubtitleTrackDTO, error) {
	service := b.subtitleService()
	if service == nil {
		return SubtitleTrackDTO{}, MediaBindingUnavailable()
	}
	track, err := service.SubmitForReview(b.context(), request.TrackID, request.EpisodeID)
	if err != nil {
		return SubtitleTrackDTO{}, toDramaError(err)
	}
	return toSubtitleTrackDTO(track), nil
}

// SubmitExportForReviewRequest moves a draft export to the state an approval requires.
type SubmitExportForReviewRequest struct {
	ExportID  string `json:"exportId"`
	EpisodeID string `json:"episodeId,omitempty"`
}

// SubmitExportForReview moves a draft export to review, for the reason the subtitle one exists.
func (b *MediaBinding) SubmitExportForReview(request SubmitExportForReviewRequest) (ExportRecordDTO, error) {
	service := b.exportService()
	if service == nil {
		return ExportRecordDTO{}, MediaBindingUnavailable()
	}
	record, err := service.SubmitForReview(b.context(), request.ExportID, request.EpisodeID)
	if err != nil {
		return ExportRecordDTO{}, toDramaError(err)
	}
	return toExportRecordDTO(record), nil
}

// RunExportRequest asks for one episode export.
type RunExportRequest struct {
	EpisodeID      string `json:"episodeId"`
	BoardVersionID string `json:"boardVersionId,omitempty"`
	// SubtitleTrackID, empty uses the approved track when there is one.
	SubtitleTrackID string `json:"subtitleTrackId,omitempty"`
	Quality         string `json:"quality"`
	Width           int    `json:"width,omitempty"`
	Height          int    `json:"height,omitempty"`
	FPS             int    `json:"fps,omitempty"`
	SubtitleMode    string `json:"subtitleMode,omitempty"`
}

// ExportRecordDTO is one export row.
type ExportRecordDTO struct {
	ID              string `json:"id"`
	EpisodeID       string `json:"episodeId"`
	VersionNumber   int    `json:"versionNumber"`
	Status          string `json:"status"`
	Quality         string `json:"quality"`
	Width           int    `json:"width"`
	Height          int    `json:"height"`
	DurationMS      int    `json:"durationMs"`
	OutputFileHash  string `json:"outputFileHash,omitempty"`
	SubtitleTrackID string `json:"subtitleTrackId,omitempty"`
	// ManifestJSON travels with the row so a reader can inspect what the export was made from without
	// a second call: AC-MEDIA-003's traceability is the document, and hiding it behind a method would
	// make the common case two round trips.
	ManifestJSON    string `json:"manifestJson,omitempty"`
	ApprovalTraceID string `json:"approvalTraceId,omitempty"`
	CreatedAt       string `json:"createdAt"`
}

func toExportRecordDTO(record appmedia.ExportRecord) ExportRecordDTO {
	return ExportRecordDTO{
		ID: record.ID, EpisodeID: record.EpisodeID, VersionNumber: record.VersionNumber,
		Status: record.Status, Quality: record.Quality, Width: record.Width, Height: record.Height,
		DurationMS: record.DurationMS, OutputFileHash: record.OutputFileHash,
		SubtitleTrackID: record.SubtitleTrackID, ManifestJSON: record.ManifestJSON,
		ApprovalTraceID: record.ApprovalTraceID, CreatedAt: rfc3339OrEmpty(record.CreatedAt),
	}
}

// RunExport composes an episode and records what it was made from.
func (b *MediaBinding) RunExport(request RunExportRequest) (ExportRecordDTO, error) {
	service := b.exportService()
	if service == nil {
		return ExportRecordDTO{}, MediaBindingUnavailable()
	}
	record, _, err := service.Export(b.context(), appmedia.ExportRequest{
		EpisodeID:       strings.TrimSpace(request.EpisodeID),
		BoardVersionID:  strings.TrimSpace(request.BoardVersionID),
		SubtitleTrackID: strings.TrimSpace(request.SubtitleTrackID),
		Quality:         domainmedia.ExportQuality(strings.TrimSpace(request.Quality)),
		Width:           request.Width,
		Height:          request.Height,
		FPS:             request.FPS,
		SubtitleMode:    domainmedia.SubtitleMode(strings.TrimSpace(request.SubtitleMode)),
		CreatedByType:   "user",
	})
	if err != nil {
		return ExportRecordDTO{}, toDramaError(err)
	}
	return toExportRecordDTO(record), nil
}

// ListExports returns an episode's exports newest version first.
func (b *MediaBinding) ListExports(episodeID string) ([]ExportRecordDTO, error) {
	service := b.exportService()
	if service == nil {
		return nil, MediaBindingUnavailable()
	}
	records, err := service.Exports(b.context(), episodeID)
	if err != nil {
		return nil, toDramaError(err)
	}
	views := make([]ExportRecordDTO, 0, len(records))
	for _, record := range records {
		views = append(views, toExportRecordDTO(record))
	}
	return views, nil
}

// ApproveExportRequest puts an export in force.
type ApproveExportRequest struct {
	ExportID  string `json:"exportId"`
	EpisodeID string `json:"episodeId,omitempty"`
	TraceID   string `json:"traceId,omitempty"`
}

// ApproveExport approves one export.
func (b *MediaBinding) ApproveExport(request ApproveExportRequest) (ExportRecordDTO, error) {
	service := b.exportService()
	if service == nil {
		return ExportRecordDTO{}, MediaBindingUnavailable()
	}
	record, err := service.Approve(b.context(), request.ExportID, request.EpisodeID, request.TraceID)
	if err != nil {
		return ExportRecordDTO{}, toDramaError(err)
	}
	return toExportRecordDTO(record), nil
}

// SaveExportRequest writes a stored file to a location the user chooses.
type SaveExportRequest struct {
	// StorageKey is the content-addressed key of the stored bytes. It is NOT a path: the store's keys
	// are 64 hexadecimal characters, and the binding checks that shape before touching the store.
	StorageKey string `json:"storageKey"`
	// SuggestedName is what the save dialog offers as a filename. It is a HINT to a human, not a
	// destination — the user can change it, and whatever they leave is where the bytes go.
	SuggestedName string `json:"suggestedName"`
}

// SaveFileResultDTO reports where a save wrote, for the message a user reads.
type SaveFileResultDTO struct {
	// Written is false when the user cancelled the dialog, which is not an error.
	Written bool   `json:"written"`
	Path    string `json:"path,omitempty"`
}

// SaveExport writes a stored file to a location the user picks.
//
// # THE PATH COMES FROM THE DIALOG
//
// The request names a storage key and a suggested name; it cannot name a destination. SECURITY section
// 11 allows writing only where the user pointed — "用户选择导出目录时只写明确目标" — and a destination
// field would be the opposite of that, because a compromised frontend could write anywhere. The dialog
// is the user pointing, and this method is the only writer.
//
// # Why it streams rather than returning base64
//
// ADR-0015 section 10: `ReadResultFile` materialises a whole object into a data URL inside a JSON
// message and caps it at 64 MiB. A two-minute MP4 exceeds that, so an export built on it would fail on
// every real episode with an error that reads like a size limit rather than a limit hit by design. The
// bytes go from the store to the destination through an `io.Copy` and are never in the browser.
func (b *MediaBinding) SaveExport(request SaveExportRequest) (SaveFileResultDTO, error) {
	service := b.exportService()
	if service == nil {
		return SaveFileResultDTO{}, MediaBindingUnavailable()
	}
	save := b.savePath()
	if save == nil {
		return SaveFileResultDTO{}, bindingUnavailable()
	}
	// The shape is checked before the store is touched: a key that is not sixty-four hexadecimal
	// characters cannot be one the store holds, and refusing it here means a malformed request never
	// reaches a file operation. `isStorageKey` is the same check `ReadResultFile` makes, rather than a
	// second implementation of one rule.
	key := strings.TrimSpace(request.StorageKey)
	if !isStorageKey(key) {
		return SaveFileResultDTO{}, bindingInvalidInput()
	}
	name := strings.TrimSpace(request.SuggestedName)
	if name == "" {
		name = "episode.mp4"
	}
	ctx := b.context()
	// The dialog opens FIRST, and only a chosen path causes a read: a cancelled dialog must not have
	// touched the store at all.
	written, err := save(ctx, name, func(writer io.Writer) error {
		reader, err := service.Open(ctx, key)
		if err != nil {
			return err
		}
		defer reader.Close()
		if _, err := io.Copy(writer, reader); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return SaveFileResultDTO{}, toDramaError(err)
	}
	if written == "" {
		// A cancelled dialog. It is not an error: the user changed their mind, and reporting a failure
		// for that would make the section show a message for an ordinary act.
		return SaveFileResultDTO{Written: false}, nil
	}
	return SaveFileResultDTO{Written: true, Path: written}, nil
}
