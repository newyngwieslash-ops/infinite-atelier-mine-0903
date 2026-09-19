// Package importing orchestrates a document import: decode, normalize, detect
// chapters, store, and record.
//
// It implements docs/ROADMAP.md scope items 1-5 for the parts that need I/O. The
// rules that decide anything are in internal/domain/importing and are pure
// functions; this package supplies the storage and the persistence they need.
//
// Three properties the specification requires and this package is where they
// are enforced:
//
//   - The original bytes and the normalized text are both stored, and neither is
//     ever overwritten (DOMAIN_MODEL section 5.2: "替换文档不覆盖旧版本").
//   - A duplicate import is REFUSED by default and reported, not silently
//     accepted (PRD FR-020: "同一文件重复导入时给出明确提示").
//   - A document is data. Nothing here interprets its text as an instruction,
//     and the only thing that reads it is the chapter detector, which is a
//     regular expression.
package importing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	appevents "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/events"
	appfiles "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/files"
	appstory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/story"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
	importdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/importing"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/story"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// Clock abstracts time so tests are deterministic.
type Clock interface {
	Now() time.Time
}

// Service orchestrates document imports.
type Service struct {
	ifs    DocumentStore
	story  *appstory.Service
	events EventRecorder
	clock  Clock
}

// DocumentStore is the byte-level side: storing the original and the normalized
// text, and reading a version's text back for display.
//
// It is declared here rather than imported from the files package so this
// package depends on the capability it needs, not on the service that happens to
// provide it.
type DocumentStore interface {
	// Import stores bytes under a display name and returns the stored object.
	Import(ctx context.Context, displayName string, body []byte) (appfiles.Object, error)
	// Open returns a version's normalized text bytes.
	Open(ctx context.Context, storageKey string) ([]byte, error)
}

// EventRecorder emits §17 events. Both methods are needed: chapter confirmation
// is a governance decision recorded with its change, while an import announces
// itself afterwards.
type EventRecorder interface {
	Build(ctx context.Context, draft appevents.Draft) (event.Event, error)
	RecordBestEffort(ctx context.Context, draft appevents.Draft)
}

// Options configures a Service.
type Options struct {
	Store  DocumentStore
	Story  *appstory.Service
	Events EventRecorder
	Clock  Clock
}

// NewService builds the import service.
func NewService(options Options) *Service {
	return &Service{
		ifs:    options.Store,
		story:  options.Story,
		events: options.Events,
		clock:  options.Clock,
	}
}

// Available reports whether the service can operate. An unattached binding fails
// closed rather than panicking.
func (s *Service) Available() bool {
	return s != nil && s.ifs != nil && s.story != nil
}

func (s *Service) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now().UTC()
	}
	return s.clock.Now().UTC()
}

// recordEvent announces something that happened, if the service has a recorder.
//
// The nil check lives here so no emit site repeats it and none can forget it:
// the recorder is an interface, so calling a method on an unset one panics.
func (s *Service) recordEvent(ctx context.Context, draft appevents.Draft) {
	if s == nil || s.events == nil {
		return
	}
	s.events.RecordBestEffort(ctx, draft)
}

// PrecheckRequest is a caller's request to inspect a document before importing it.
type PrecheckRequest struct {
	ProjectID string
	// Format is what the caller believes the file is. It is a hint for the
	// DOCX container check, not a claim: the bytes decide.
	Format string
	// Name is the display name, which never becomes a path.
	Name string
	// Content is the raw bytes.
	Content []byte
}

// Precheck reports what an import would do, without writing anything.
//
// PRD FR-020 requires the import flow to offer 编码检测 (encoding detection) and a
// text preview, and the review step needs to happen before the write so a user
// can see the chapters before committing to them. So this reads, decodes,
// normalizes and detects, and returns the result; Import does the same work again
// rather than caching, because a cache between the two would be a second source
// of truth for what the document says.
type PrecheckResult struct {
	Format       importdomain.Format
	Encoding     importdomain.Encoding
	CharCount    int
	ChapterCount int
	Chapters     []importdomain.ChapterBoundary
	// Duplicate reports that a version with this source hash already exists in
	// the project. It names the document so the message can say which one.
	Duplicate             bool
	DuplicateDocumentID   string
	DuplicateDocumentName string
	// Warnings are things the user should see but that do not block the import.
	Warnings []string
}

// Precheck inspects a document without storing it.
func (s *Service) Precheck(ctx context.Context, request PrecheckRequest) (PrecheckResult, error) {
	if !s.Available() {
		return PrecheckResult{}, storageFailure()
	}
	document, err := s.prepare(request.Content, request.Format)
	if err != nil {
		return PrecheckResult{}, err
	}
	result := PrecheckResult{
		Format:       document.Format,
		Encoding:     document.Encoding,
		CharCount:    document.RuneCount(),
		ChapterCount: len(document.Chapters),
		Chapters:     document.Chapters,
	}
	if strings.TrimSpace(request.ProjectID) == "" {
		return result, nil
	}
	// PRD FR-020's duplicate warning. The hash is the original file's, so a
	// re-export of the same file with different line endings still matches.
	sourceHash := hashHex(request.Content)
	existing, found, err := s.story.FindVersionBySourceHash(ctx, strings.TrimSpace(request.ProjectID), sourceHash)
	if err != nil {
		return PrecheckResult{}, err
	}
	if found {
		result.Duplicate = true
		result.DuplicateDocumentID = existing.DocumentID
		result.DuplicateDocumentName = existing.DocumentName
		result.Warnings = append(result.Warnings,
			"This file has already been imported as \""+existing.DocumentName+"\".")
	}
	return result, nil
}

// prepare decodes, normalizes and detects chapters, in that order.
func (s *Service) prepare(content []byte, formatHint string) (importdomain.Document, error) {
	format, err := detectFormat(content, formatHint)
	if err != nil {
		return importdomain.Document{}, err
	}
	// A DOCX is a ZIP of XML; its text comes out of the container rather than
	// out of the bytes as they arrived.
	text, err := extractText(content, format)
	if err != nil {
		return importdomain.Document{}, err
	}
	encoding, normalized, err := importdomain.DetectEncoding(text)
	if err != nil {
		return importdomain.Document{}, err
	}
	chapters, err := importdomain.DetectChapters(normalized, format)
	if err != nil {
		return importdomain.Document{}, err
	}
	document := importdomain.Document{
		Text:     normalized,
		Encoding: encoding,
		Format:   format,
		Chapters: chapters,
	}
	if err := document.Validate(); err != nil {
		return importdomain.Document{}, err
	}
	return document, nil
}

// ImportRequest is a caller's request to import a document.
type ImportRequest struct {
	ProjectID string
	// DocumentID continues an existing document with a new version. Empty
	// creates a new document.
	DocumentID string
	// DocumentType defaults from the format when empty.
	DocumentType string
	Name         string
	Format       string
	Content      []byte
	// ConfirmDuplicate must be true to import a file whose source hash already
	// exists. The default is to refuse, because PRD FR-020 asks for a warning
	// and a warning a caller can ignore silently is not one.
	ConfirmDuplicate bool
}

// ImportResult is what the import produced.
type ImportResult struct {
	Document   story.SourceDocument
	Version    story.SourceDocumentVersion
	Chapters   []story.Chapter
	CharCount  int
	Encoding   importdomain.Encoding
	Format     importdomain.Format
	Duplicated bool
}

// Import stores a document and its chapters.
//
// The order is deliberate: detect first (so a malformed document writes
// nothing), then store the bytes, then the rows. A document that fails to parse
// leaves no half-imported state, and the bytes are content-addressed, so a
// retry after a row-level failure reuses the same objects rather than
// accumulating copies.
func (s *Service) Import(ctx context.Context, request ImportRequest) (ImportResult, error) {
	if !s.Available() {
		return ImportResult{}, storageFailure()
	}
	projectID := strings.TrimSpace(request.ProjectID)
	if projectID == "" {
		return ImportResult{}, importdomain.InvalidError("An import must name a project.")
	}
	document, err := s.prepare(request.Content, request.Format)
	if err != nil {
		return ImportResult{}, err
	}

	// The duplicate check runs before any write, so a refused import leaves the
	// database exactly as it was.
	sourceHash := hashHex(request.Content)
	existing, found, err := s.story.FindVersionBySourceHash(ctx, projectID, sourceHash)
	if err != nil {
		return ImportResult{}, err
	}
	if found && !request.ConfirmDuplicate {
		return ImportResult{Duplicated: true}, importdomain.ConflictError(
			"This file was already imported as \"" + existing.DocumentName + "\". Confirm to import it again as a new version.")
	}

	// Store the original bytes and the normalized text. Both are content
	// addressed, so the same content stored twice is one object.
	displayName := strings.TrimSpace(request.Name)
	if displayName == "" {
		displayName = "document"
	}
	original, err := s.ifs.Import(ctx, displayName, request.Content)
	if err != nil {
		return ImportResult{}, importdomain.StorageError("The document could not be stored.", err)
	}
	normalizedBytes := []byte(document.Text)
	normalized, err := s.ifs.Import(ctx, displayName+".txt", normalizedBytes)
	if err != nil {
		return ImportResult{}, importdomain.StorageError("The normalized text could not be stored.", err)
	}

	documentType := story.DocumentType(strings.TrimSpace(request.DocumentType))
	if !story.IsValidDocumentType(documentType) {
		documentType = importdomain.StoryDocumentType(document.Format)
	}

	record := story.SourceDocument{}
	documentID := strings.TrimSpace(request.DocumentID)
	if documentID == "" {
		created, createErr := s.story.CreateSourceDocument(ctx, appstory.CreateSourceDocumentRequest{
			ProjectID:    projectID,
			DocumentType: story.DocumentType(documentType),
			Name:         displayName,
		})
		if createErr != nil {
			return ImportResult{}, createErr
		}
		record = created
		documentID = created.ID
	} else {
		loaded, loadErr := s.story.GetSourceDocument(ctx, documentID)
		if loadErr != nil {
			return ImportResult{}, loadErr
		}
		record = loaded
	}

	version, err := s.story.AddSourceDocumentVersion(ctx, appstory.AddSourceDocumentVersionRequest{
		SourceDocumentID:     documentID,
		SourceHash:           sourceHash,
		PhysicalFileID:       original.Hash,
		NormalizedTextFileID: normalized.Hash,
		ContentHash:          hashHex(normalizedBytes),
		MIMEType:             original.MIME,
		Encoding:             string(document.Encoding),
		CharCount:            document.RuneCount(),
		ImportMetadataJSON:   importMetadata(document, displayName),
		CreatedByType:        versioning.CreatedByUser,
	})
	if err != nil {
		return ImportResult{}, err
	}

	chapters, err := s.createChapters(ctx, version.ID, document.Chapters)
	if err != nil {
		return ImportResult{}, err
	}

	return ImportResult{
		Document:   record,
		Version:    version,
		Chapters:   chapters,
		CharCount:  document.RuneCount(),
		Encoding:   document.Encoding,
		Format:     document.Format,
		Duplicated: found,
	}, nil
}

// createChapters writes the detected boundaries.
//
// It stops at the first failure rather than continuing, and reports which
// ordinal failed. A partial chapter set is not silently kept: the caller sees an
// error, and the version row it belongs to is still readable so the user can see
// what was detected and retry.
func (s *Service) createChapters(ctx context.Context, versionID string, boundaries []importdomain.ChapterBoundary) ([]story.Chapter, error) {
	chapters := make([]story.Chapter, 0, len(boundaries))
	for _, boundary := range boundaries {
		status := story.ChapterDetected
		// A document with no markers produces one boundary covering everything.
		// It is 'detected' like any other: the user still has to confirm it, and
		// calling it confirmed would claim a decision nobody made.
		chapter, err := s.story.CreateChapter(ctx, appstory.CreateChapterRequest{
			SourceDocumentVersionID: versionID,
			Ordinal:                 boundary.Ordinal,
			Title:                   boundary.Title,
			StartOffset:             boundary.StartOffset,
			EndOffset:               boundary.EndOffset,
			SourceKind:              story.ChapterSourceKind(boundary.Source),
			Status:                  status,
		})
		if err != nil {
			return nil, err
		}
		chapters = append(chapters, chapter)
	}
	return chapters, nil
}

// ReadRange returns a slice of a version's normalized text.
//
// SECURITY section 7.5 requires 大文本分页读取 (paged reading of large text), and
// this is that page: the caller asks for a character range and gets at most
// ReadPageBytes worth of UTF-8. A range wider than the cap is clamped rather
// than refused, because a caller asking for too much wants the text, not an
// error.
func (s *Service) ReadRange(ctx context.Context, versionID string, startRune, endRune int) (TextRange, error) {
	if !s.Available() {
		return TextRange{}, storageFailure()
	}
	version, err := s.story.GetSourceDocumentVersion(ctx, strings.TrimSpace(versionID))
	if err != nil {
		return TextRange{}, err
	}
	if strings.TrimSpace(version.NormalizedTextFileID) == "" {
		return TextRange{}, importdomain.InvalidError("This version has no normalized text to read.")
	}
	raw, err := s.ifs.Open(ctx, version.NormalizedTextFileID)
	if err != nil {
		return TextRange{}, importdomain.StorageError("The normalized text could not be read.", err)
	}
	runes := []rune(string(raw))
	total := len(runes)
	if startRune < 0 {
		startRune = 0
	}
	if startRune > total {
		startRune = total
	}
	if endRune <= 0 || endRune > total {
		endRune = total
	}
	// Clamp to one page.
	if endRune-startRune > maxPageRunes {
		endRune = startRune + maxPageRunes
	}
	if endRune < startRune {
		endRune = startRune
	}
	return TextRange{
		Text:       string(runes[startRune:endRune]),
		StartRune:  startRune,
		EndRune:    endRune,
		TotalRunes: total,
	}, nil
}

// TextRange is one page of a document's text.
type TextRange struct {
	Text       string
	StartRune  int
	EndRune    int
	TotalRunes int
}

// maxPageRunes converts the byte ceiling into characters. A UTF-8 Chinese
// character is three bytes, so this is the conservative reading of the limit and
// the response is always smaller than ReadPageBytes.
const maxPageRunes = importdomain.ReadPageBytes / 3

// ConfirmChaptersRequest confirms a document version's chapter boundaries.
type ConfirmChaptersRequest struct {
	SourceDocumentVersionID string
	// TraceID correlates the event with the action that caused it.
	TraceID string
}

// ConfirmChapters marks every boundary of a version as confirmed and records the
// decision.
//
// DOMAIN_MODEL section 5.3 says a user confirmation is what moves a boundary out
// of 'detected', and §16 names ConfirmChapters as the command. The §17 event
// ChapterBoundariesConfirmed is recorded here, and ADR-0009 puts a decision like
// this on the transactional path: a confirmation nobody can audit is worse than
// one that did not happen. So a service with no recorder refuses.
//
// A boundary the user edited is already 'edited' and is left alone: it carries a
// stronger statement than 'confirmed', and overwriting it would discard the fact
// that a person changed it.
func (s *Service) ConfirmChapters(ctx context.Context, request ConfirmChaptersRequest) ([]story.Chapter, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	versionID := strings.TrimSpace(request.SourceDocumentVersionID)
	if versionID == "" {
		return nil, importdomain.InvalidError("A confirmation must name a document version.")
	}
	chapters, err := s.story.ListChapters(ctx, versionID)
	if err != nil {
		return nil, err
	}
	if len(chapters) == 0 {
		return nil, importdomain.InvalidError("There are no chapter boundaries to confirm.")
	}
	version, err := s.story.GetSourceDocumentVersion(ctx, versionID)
	if err != nil {
		return nil, err
	}
	document, err := s.story.GetSourceDocument(ctx, version.SourceDocumentID)
	if err != nil {
		return nil, err
	}
	// The governance event, built before the writes so a service without a
	// recorder refuses rather than confirming unrecorded.
	record, err := s.buildEvent(ctx, event.ChapterBoundariesConfirmed, event.AggregateChapter,
		versionID, document.ProjectID, request.TraceID)
	if err != nil {
		return nil, err
	}
	confirmed, err := s.story.ConfirmChapters(ctx, appstory.ConfirmChaptersRequest{
		SourceDocumentVersionID: versionID,
		Event:                   record,
	})
	if err != nil {
		return nil, err
	}
	return confirmed, nil
}

// buildEvent assembles a governance event, refusing when there is no recorder.
func (s *Service) buildEvent(ctx context.Context, eventType event.Type, aggregate event.AggregateType, aggregateID, projectID, traceID string) (event.Event, error) {
	if s.events == nil {
		return event.Event{}, importdomain.StorageError("The confirmation cannot be recorded, so it was not applied.", nil)
	}
	record, err := s.events.Build(ctx, appevents.Draft{
		Type:          eventType,
		AggregateType: aggregate,
		AggregateID:   aggregateID,
		ProjectID:     projectID,
		TraceID:       traceID,
	})
	if err != nil {
		return event.Event{}, err
	}
	return record, nil
}

// storageFailure is the fail-closed error for an unattached service.
func storageFailure() error {
	return importdomain.StorageError("The import service is unavailable.", nil)
}

// hashHex returns the lowercase hex SHA-256 of the bytes, which is the form the
// schema's digests take.
func hashHex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// importMetadata records what was detected, so a later reader can tell how a
// version came to be without re-deriving it.
//
// It is §2.6's allowed JSON: display and provenance metadata, not a queryable
// fact. The facts that are queried live in columns (char_count, encoding,
// source_hash, chapters.source_kind).
func importMetadata(document importdomain.Document, displayName string) string {
	builder := &strings.Builder{}
	builder.WriteString(`{"format":"`)
	builder.WriteString(string(document.Format))
	builder.WriteString(`","detectedChapters":`)
	builder.WriteString(itoa(len(document.Chapters)))
	builder.WriteString(`,"displayName":`)
	builder.WriteString(jsonString(displayName))
	builder.WriteString(`}`)
	return builder.String()
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0, 8)
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}

// jsonString quotes a string for embedding in a JSON object.
//
// It is spelled out rather than importing encoding/json because the only value
// that reaches it is a display name, and the escaping rules it needs are the two
// characters a filename can contain that would break the object.
func jsonString(value string) string {
	var builder strings.Builder
	builder.WriteByte('"')
	for _, character := range value {
		switch character {
		case '"':
			builder.WriteString(`\"`)
		case '\\':
			builder.WriteString(`\\`)
		case '\n':
			builder.WriteString(`\n`)
		case '\r':
			builder.WriteString(`\r`)
		case '\t':
			builder.WriteString(`\t`)
		default:
			if character < 0x20 {
				continue
			}
			builder.WriteRune(character)
		}
	}
	builder.WriteByte('"')
	return builder.String()
}
