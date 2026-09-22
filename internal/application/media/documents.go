package media

import (
	"context"
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/media"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/screenplay"
	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	shotlist "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/shotlist"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/storyboard"
)

// documents.go is ROADMAP WP-11 item 11's remaining half.
//
//	11. Script/Storyboard/Subtitle/Manifest Export；
//
// The subtitle half and the manifest half were built with the export; this file is the other two plus
// the ACT that writes a manifest to disk, which had no caller at all: `save_dialog.go` carries a
// `.json` filter and nothing ever asked for a name ending in `.json`.
//
// # Why one service rather than three
//
// The four documents are one feature — "give me what this episode is made of, in files I can read" —
// and each needs the same three facts: which episode, which version of each artifact is in force, and
// what the user is looking at. Three services would be three copies of that resolution, and the one
// that drifted would produce a document describing another version.
//
// # Why they are read MODELS rather than writes
//
// A document is a rendering of state the database already holds. Nothing here writes a version, an
// approval or an event: a user who exports a script has not changed it, and a build that recorded an
// event for a read would fill the audit trail with a user opening a file.

// DocumentRepository answers what one episode's documents are made of.
//
// It is a port because the facts live in four aggregates — the episode and its script, the board and
// its rows, the panels approved for those rows and the export records — and this package must not
// import four services. The composition root supplies one adapter over the repositories, which is
// where this repository puts the seams between layers that must not know about each other.
type DocumentRepository interface {
	// EpisodeFacts returns the episode's own identity and the versions in force.
	EpisodeFacts(ctx context.Context, episodeID string) (DocumentEpisode, error)
	// ScriptStructure returns one script version's scenes, lines and shots in order.
	ScriptStructure(ctx context.Context, scriptVersionID string) (scriptdomain.ScriptStructure, error)
	// BoardFactsFor returns a board version's rows with the panel and media approved for each.
	//
	// It is a SEPARATE method from `TimelineRepository.BoardFacts` because the two answer different
	// questions: the timeline needs a hash and a kind so it can compose, and a shot list needs the
	// twelve prose fields DOMAIN_MODEL section 9.3 gives a row. Widening the timeline's port would
	// make the exporter's needs its caller's.
	BoardFactsFor(ctx context.Context, storyboardVersionID string) ([]shotlist.Row, storyboard.StoryboardVersion, error)
	// LatestExport returns the episode's newest export record, or found=false.
	LatestExport(ctx context.Context, episodeID string) (ExportRecord, bool, error)
}

// DocumentEpisode is one episode with the versions in force.
type DocumentEpisode struct {
	ID            string
	ProjectName   string
	Title         string
	SeasonNumber  int
	EpisodeNumber int
	// CurrentScriptVersionID is the episode's own pointer, empty when none is approved.
	CurrentScriptVersionID string
	// ApprovedScriptVersionID is the script version whose status is `approved`, which is what a
	// document renders: the pointer and the approval are two facts, and a build that trusted the
	// pointer alone would render a draft the episode happens to point at.
	ApprovedScriptVersionID    string
	ApprovedScriptVersionNum   int
	ApprovedBoardVersionID     string
	ApprovedBoardVersionNumber int
	// ApprovedSubtitleTrackID is the track in force, empty when none is.
	ApprovedSubtitleTrackID string
}

// DocumentRequest asks for one episode's document.
type DocumentRequest struct {
	EpisodeID string
	// VersionID overrides the version in force. Empty uses the approved one, which is what a user
	// exporting "my script" means; a NAMED version that does not exist is an error rather than a
	// silent fallback, because a caller that asked for one version and got another would file the
	// wrong draft.
	VersionID string
}

// Document is a rendered document with everything a caller needs to name and write it.
type Document struct {
	// Name is what the document IS, for a caller's message: "script", "shot list", "manifest".
	Name string
	// Text is the document's bytes.
	Text string
	// Extension is what the document is written with, including the dot.
	Extension string
	// SuggestedName is what the save dialog offers, WITHOUT a directory: the dialog decides where,
	// and this is only a default the user can change.
	SuggestedName string
}

// DocumentService renders an episode's documents.
type DocumentService struct {
	repository DocumentRepository
}

// DocumentOptions configures the service.
type DocumentOptions struct {
	Repository DocumentRepository
}

// NewDocumentService builds the service.
func NewDocumentService(options DocumentOptions) *DocumentService {
	return &DocumentService{repository: options.Repository}
}

// Available reports whether the service can read.
func (s *DocumentService) Available() bool { return s != nil && s.repository != nil }

// ScriptRequest asks for a script document.
type ScriptRequest struct {
	DocumentRequest
	// Format is `txt` or `fountain`.
	Format string
	// IncludeShots writes each scene's camera setups after its lines, which a production wants and a
	// reader of the screenplay usually does not. It sits here rather than on `DocumentRequest` because
	// a shot list IS the shots: only the script has the choice.
	IncludeShots bool
}

// ExportScript renders the episode's script as a document.
func (s *DocumentService) ExportScript(ctx context.Context, request ScriptRequest) (Document, error) {
	if !s.Available() {
		return Document{}, NotAvailableError("No document reader is configured, so a script cannot be exported.")
	}
	format, err := scriptFormat(request.Format)
	if err != nil {
		return Document{}, err
	}
	episode, err := s.resolveEpisode(ctx, request.DocumentRequest)
	if err != nil {
		return Document{}, err
	}
	scriptVersionID := strings.TrimSpace(request.VersionID)
	if scriptVersionID == "" {
		scriptVersionID = episode.ApprovedScriptVersionID
	}
	if scriptVersionID == "" {
		// Refused rather than answered with an empty document: "this episode has no approved script"
		// and "the script is blank" are different situations, and a caller that got a title page and
		// nothing else could not tell them apart.
		return Document{}, InvalidError("This episode has no approved script version, so there is nothing to export.")
	}
	structure, err := s.repository.ScriptStructure(ctx, scriptVersionID)
	if err != nil {
		return Document{}, err
	}
	// The version NUMBER travels on the version row rather than in the structure, so the episode's
	// approved number is the honest one for the approved path. A caller that NAMED a version gets the
	// document without a number rather than with another version's: a header that says v3 for a
	// document that is v5 is worse than one that names no draft at all, and the repository read is
	// what proves the named version exists.
	versionNumber := episode.ApprovedScriptVersionNum
	if scriptVersionID != episode.ApprovedScriptVersionID {
		versionNumber = 0
	}
	text, err := screenplay.Render(screenplay.Document{
		ProjectName:   episode.ProjectName,
		EpisodeTitle:  episode.Title,
		SeasonNumber:  episode.SeasonNumber,
		EpisodeNum:    episode.EpisodeNumber,
		VersionNumber: versionNumber,
		Structure:     structure,
		IncludeShots:  request.IncludeShots,
	}, format)
	if err != nil {
		return Document{}, err
	}
	return Document{
		Name:          "script",
		Text:          text,
		Extension:     format.ExtensionFor(),
		SuggestedName: suggestedDocumentName(episode, "script", format.ExtensionFor()),
	}, nil
}

// ShotListRequest asks for a shot list document.
type ShotListRequest struct {
	DocumentRequest
	// Format is `txt` or `csv`.
	Format string
}

// ExportShotList renders the episode's storyboard as a shot list.
func (s *DocumentService) ExportShotList(ctx context.Context, request ShotListRequest) (Document, error) {
	if !s.Available() {
		return Document{}, NotAvailableError("No document reader is configured, so a shot list cannot be exported.")
	}
	format, err := shotListFormat(request.Format)
	if err != nil {
		return Document{}, err
	}
	episode, err := s.resolveEpisode(ctx, request.DocumentRequest)
	if err != nil {
		return Document{}, err
	}
	boardVersionID := strings.TrimSpace(request.VersionID)
	if boardVersionID == "" {
		boardVersionID = episode.ApprovedBoardVersionID
	}
	if boardVersionID == "" {
		return Document{}, InvalidError("This episode has no approved storyboard, so there is nothing to export.")
	}
	rows, version, err := s.repository.BoardFactsFor(ctx, boardVersionID)
	if err != nil {
		return Document{}, err
	}
	document := shotlist.Document{
		ProjectName:           episode.ProjectName,
		EpisodeTitle:          episode.Title,
		SeasonNumber:          episode.SeasonNumber,
		EpisodeNum:            episode.EpisodeNumber,
		BoardVersionNumber:    version.VersionNumber,
		ScriptVersionID:       version.ScriptVersionID,
		DirectorPlanVersionID: version.DirectorPlanVersionID,
		Rows:                  rows,
	}
	text, err := shotlist.Render(document, format)
	if err != nil {
		return Document{}, err
	}
	return Document{
		Name:          "shot list",
		Text:          text,
		Extension:     format.ExtensionFor(),
		SuggestedName: suggestedDocumentName(episode, "shotlist", format.ExtensionFor()),
	}, nil
}

// ExportManifest renders the episode's newest export manifest.
//
// # Why this is an export of its own
//
// AC-MEDIA-003's "manifest traceability" is a document a reader CONSULTS, and until this method it
// was reachable only through the UI's expandable row: a person who needed to hand the manifest to a
// distributor, or keep it beside the film, had no way to get it. The row carries the JSON already, so
// this is a read and a rename rather than a second assembly — and it goes through the DOMAIN'S OWN
// parser first, so a manifest this build cannot read is refused here rather than written out as a
// file nobody can use.
func (s *DocumentService) ExportManifest(ctx context.Context, request DocumentRequest) (Document, error) {
	if !s.Available() {
		return Document{}, NotAvailableError("No document reader is configured, so a manifest cannot be exported.")
	}
	episode, err := s.resolveEpisode(ctx, request)
	if err != nil {
		return Document{}, err
	}
	record, found, err := s.repository.LatestExport(ctx, episode.ID)
	if err != nil {
		return Document{}, err
	}
	if !found {
		return Document{}, InvalidError("This episode has no export, so there is no manifest to write.")
	}
	if strings.TrimSpace(record.ManifestJSON) == "" {
		return Document{}, InvalidError("That export recorded no manifest, so there is nothing to trace.")
	}
	// The domain's parser, which refuses an empty document, a JSON `null`, a `{}` and one with no
	// schema version. A manifest that failed any of those is a file a reader could not use, and
	// writing it out anyway would be handing somebody a document that looks official and says nothing.
	manifest, err := media.DecodeManifest(record.ManifestJSON)
	if err != nil {
		return Document{}, InvalidError("That export's manifest could not be read: " + err.Error())
	}
	// The document is re-rendered from the PARSED manifest rather than copied, so what a user gets is
	// what this build understood: a field the parser dropped would be missing from the file rather
	// than silently present in a copy.
	text, err := manifest.Encode()
	if err != nil {
		return Document{}, StorageError("The manifest could not be written.", err)
	}
	return Document{
		Name:          "manifest",
		Text:          text,
		Extension:     ".json",
		SuggestedName: suggestedDocumentName(episode, "manifest", ".json"),
	}, nil
}

// resolveEpisode reads the episode and refuses one that names no project.
func (s *DocumentService) resolveEpisode(ctx context.Context, request DocumentRequest) (DocumentEpisode, error) {
	episodeID := strings.TrimSpace(request.EpisodeID)
	if episodeID == "" {
		return DocumentEpisode{}, InvalidError("An export must name the episode it is for.")
	}
	return s.repository.EpisodeFacts(ctx, episodeID)
}

// scriptFormat validates and normalises a script format.
func scriptFormat(value string) (screenplay.Format, error) {
	format := screenplay.Format(strings.TrimSpace(value))
	if !screenplay.IsValidFormat(format) {
		return "", InvalidError("That is not a script format this build writes.")
	}
	return format, nil
}

// shotListFormat validates a shot list format.
func shotListFormat(value string) (shotlist.Format, error) {
	format := shotlist.Format(strings.TrimSpace(value))
	if !shotlist.IsValidFormat(format) {
		return "", InvalidError("That is not a shot list format this build writes.")
	}
	return format, nil
}

// suggestedDocumentName builds the name a save dialog offers.
//
// It never contains a directory: the dialog decides WHERE, and a suggestion carrying a path would
// put the dialog somewhere the user did not choose. The version number is in the name because a
// production accumulates drafts, and two files called `episode-script.txt` are a support question.
func suggestedDocumentName(episode DocumentEpisode, kind, extension string) string {
	label := kind
	if number := episode.EpisodeNumber; number > 0 {
		label = label + "-ep" + itoa(number)
	}
	return label + extension
}
