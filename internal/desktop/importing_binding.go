package desktop

import (
	"context"
	"sync"

	appextraction "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/extraction"
	appimporting "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/importing"
)

// importing_binding.go exposes document import and event extraction.
//
// It is a second binding over the same DramaBinding rather than a separate Wails
// service, because both speak the same domain: a document is imported once,
// its chapters are confirmed, and extraction reads those chapters. Splitting
// them across two Wails types would mean two frontend services that can drift.
//
// The surface is deliberately narrow in two places:
//
//   - Nothing returns a file path or a byte count of a stored file. An import
//     reports the identifiers it stored and the counts it read; the bytes stay
//     behind the store, so a compromised frontend cannot ask for a path.
//   - Nothing accepts arbitrary bytes from the frontend for a model to read.
//     Extraction reads the chapter the database holds, so what a model sees is
//     what was imported rather than what a caller claims about it.

// ImportBinding is the Wails surface for importing and extracting.
//
// It holds the same service pointers as DramaBinding and is attached by the same
// wiring, so a build that has no database leaves both unattached and both fail
// closed.
type ImportBinding struct {
	mu         sync.RWMutex
	ctx        context.Context
	importing  *appimporting.Service
	extraction *appextraction.Service
}

// AttachImporting supplies the import service.
func AttachImporting(binding *ImportBinding, ctx context.Context, service *appimporting.Service) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.ctx = ctx
	binding.importing = service
	binding.mu.Unlock()
}

// AttachExtraction supplies the extraction service.
//
// A build with no extractor is normal rather than broken: the production default
// has none until WP-07 supplies one, and the commands below refuse with a reason
// that says so instead of inventing facts.
func AttachExtraction(binding *ImportBinding, ctx context.Context, service *appextraction.Service) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.ctx = ctx
	binding.extraction = service
	binding.mu.Unlock()
}

func (b *ImportBinding) context() context.Context {
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

func (b *ImportBinding) importService() *appimporting.Service {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.importing
}

func (b *ImportBinding) extractionService() *appextraction.Service {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.extraction
}

// PrecheckImportRequest inspects a document without storing it.
type PrecheckImportRequest struct {
	ProjectID string `json:"projectId"`
	// Format is a hint derived from the file name. The bytes decide.
	Format string `json:"format,omitempty"`
	Name   string `json:"name,omitempty"`
	// Content is the raw document. Wails transports it as a byte array, so a
	// 100k-character novel crosses as UTF-8 bytes rather than as a string the
	// transport would have to re-encode.
	Content []byte `json:"content"`
}

// ChapterBoundaryDTO is one detected boundary, before it is stored.
//
// It carries no identifier, because nothing is stored yet: a boundary is a
// proposal, and the chapter row is created by the import that accepts it.
type ChapterBoundaryDTO struct {
	Ordinal     int    `json:"ordinal"`
	Title       string `json:"title"`
	StartOffset int    `json:"startOffset"`
	// TitleEndOffset is where the heading ends and the body begins. It equals
	// StartOffset for a boundary with no heading.
	TitleEndOffset int `json:"titleEndOffset"`
	EndOffset      int `json:"endOffset"`
	// Source says how the boundary was decided: "heading", "regex", "whole" or
	// "manual".
	Source string `json:"source"`
}

// PrecheckImportResult is what an import would do.
type PrecheckImportResult struct {
	Format       string `json:"format"`
	Encoding     string `json:"encoding"`
	CharCount    int    `json:"charCount"`
	ChapterCount int    `json:"chapterCount"`
	// Chapters is bounded, because a 5,000-chapter report is a transfer rather
	// than a preview. ChapterCount is the true total.
	Chapters []ChapterBoundaryDTO `json:"chapters"`
	// Duplicate reports that this file's hash is already in the project, with the
	// name it was imported under. PRD FR-020 requires the warning.
	Duplicate             bool     `json:"duplicate"`
	DuplicateDocumentID   string   `json:"duplicateDocumentId,omitempty"`
	DuplicateDocumentName string   `json:"duplicateDocumentName,omitempty"`
	Warnings              []string `json:"warnings"`
}

// PrecheckChaptersReported bounds how many boundaries one preview returns.
const PrecheckChaptersReported = 200

// PrecheckImport reports what an import would do, without writing.
func (b *ImportBinding) PrecheckImport(request PrecheckImportRequest) (PrecheckImportResult, error) {
	service := b.importService()
	if service == nil {
		return PrecheckImportResult{}, bindingUnavailable()
	}
	result, err := service.Precheck(b.context(), appimporting.PrecheckRequest{
		ProjectID: request.ProjectID,
		Format:    request.Format,
		Name:      request.Name,
		Content:   request.Content,
	})
	if err != nil {
		return PrecheckImportResult{}, toDramaError(err)
	}
	chapters := make([]ChapterBoundaryDTO, 0, len(result.Chapters))
	for index, boundary := range result.Chapters {
		if index >= PrecheckChaptersReported {
			break
		}
		chapters = append(chapters, ChapterBoundaryDTO{
			Ordinal:        boundary.Ordinal,
			Title:          boundary.Title,
			StartOffset:    boundary.StartOffset,
			TitleEndOffset: boundary.TitleEndOffset,
			EndOffset:      boundary.EndOffset,
			Source:         string(boundary.Source),
		})
	}
	return PrecheckImportResult{
		Format:                string(result.Format),
		Encoding:              string(result.Encoding),
		CharCount:             result.CharCount,
		ChapterCount:          result.ChapterCount,
		Chapters:              chapters,
		Duplicate:             result.Duplicate,
		DuplicateDocumentID:   result.DuplicateDocumentID,
		DuplicateDocumentName: result.DuplicateDocumentName,
		Warnings:              nonNilStrings(result.Warnings),
	}, nil
}

// ImportDocumentRequest stores a document.
type ImportDocumentRequest struct {
	ProjectID string `json:"projectId"`
	// DocumentID continues an existing document with a new version. Empty
	// creates one.
	DocumentID string `json:"documentId,omitempty"`
	// DocumentType defaults from the format when empty.
	DocumentType string `json:"documentType,omitempty"`
	Name         string `json:"name,omitempty"`
	Format       string `json:"format,omitempty"`
	Content      []byte `json:"content"`
	// ConfirmDuplicate must be set to import a file whose hash is already in the
	// project. The default refuses, because a warning a caller can ignore
	// silently is not a warning.
	ConfirmDuplicate bool `json:"confirmDuplicate,omitempty"`
}

// ImportDocumentResult is what the import stored.
type ImportDocumentResult struct {
	Document  SourceDocumentDTO        `json:"document"`
	Version   SourceDocumentVersionDTO `json:"version"`
	Chapters  []ChapterDTO             `json:"chapters"`
	CharCount int                      `json:"charCount"`
	Encoding  string                   `json:"encoding"`
	Format    string                   `json:"format"`
}

// ImportDocument stores a document, its version and its chapters.
func (b *ImportBinding) ImportDocument(request ImportDocumentRequest) (ImportDocumentResult, error) {
	service := b.importService()
	if service == nil {
		return ImportDocumentResult{}, bindingUnavailable()
	}
	result, err := service.Import(b.context(), appimporting.ImportRequest{
		ProjectID:        request.ProjectID,
		DocumentID:       request.DocumentID,
		DocumentType:     request.DocumentType,
		Name:             request.Name,
		Format:           request.Format,
		Content:          request.Content,
		ConfirmDuplicate: request.ConfirmDuplicate,
	})
	if err != nil {
		return ImportDocumentResult{}, toDramaError(err)
	}
	chapters := make([]ChapterDTO, 0, len(result.Chapters))
	for _, chapter := range result.Chapters {
		chapters = append(chapters, toChapterDTO(chapter))
	}
	return ImportDocumentResult{
		Document:  toSourceDocumentDTO(result.Document),
		Version:   toSourceDocumentVersionDTO(result.Version),
		Chapters:  chapters,
		CharCount: result.CharCount,
		Encoding:  string(result.Encoding),
		Format:    string(result.Format),
	}, nil
}

// ReadDocumentRangeRequest reads one page of a version's text.
type ReadDocumentRangeRequest struct {
	SourceDocumentVersionID string `json:"sourceDocumentVersionId"`
	StartRune               int    `json:"startRune"`
	EndRune                 int    `json:"endRune"`
}

// DocumentRangeDTO is one page of text.
type DocumentRangeDTO struct {
	Text       string `json:"text"`
	StartRune  int    `json:"startRune"`
	EndRune    int    `json:"endRune"`
	TotalRunes int    `json:"totalRunes"`
}

// ReadDocumentRange returns one page of a version's normalized text.
//
// The page size is the service's own ceiling, so a caller cannot ask for a whole
// 100k-character document in one response: the response is bounded no matter what
// the caller sends. That is what makes the READER's cost independent of document
// length.
//
// It is not, by itself, a claim that the whole import is non-blocking. The
// transfer that feeds an import is bounded separately, by ImportUploadBinding's
// chunking; the precheck still crosses in one message and is the remaining cost.
// Saying "keeps the reader non-blocking" and stopping there was the kind of claim
// this package has had to correct before — precise about one path, silent about
// the other.
func (b *ImportBinding) ReadDocumentRange(request ReadDocumentRangeRequest) (DocumentRangeDTO, error) {
	service := b.importService()
	if service == nil {
		return DocumentRangeDTO{}, bindingUnavailable()
	}
	result, err := service.ReadRange(b.context(), request.SourceDocumentVersionID, request.StartRune, request.EndRune)
	if err != nil {
		return DocumentRangeDTO{}, toDramaError(err)
	}
	return DocumentRangeDTO{
		Text:       result.Text,
		StartRune:  result.StartRune,
		EndRune:    result.EndRune,
		TotalRunes: result.TotalRunes,
	}, nil
}

// ConfirmChaptersRequestDTO confirms a version's chapter boundaries.
type ConfirmChaptersRequestDTO struct {
	SourceDocumentVersionID string `json:"sourceDocumentVersionId"`
	TraceID                 string `json:"traceId,omitempty"`
}

// ConfirmChapters confirms every detected boundary and records the decision.
func (b *ImportBinding) ConfirmChapters(request ConfirmChaptersRequestDTO) ([]ChapterDTO, error) {
	service := b.importService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.ConfirmChapters(b.context(), appimporting.ConfirmChaptersRequest{
		SourceDocumentVersionID: request.SourceDocumentVersionID,
		TraceID:                 request.TraceID,
	})
	if err != nil {
		return nil, toDramaError(err)
	}
	chapters := make([]ChapterDTO, 0, len(records))
	for _, chapter := range records {
		chapters = append(chapters, toChapterDTO(chapter))
	}
	return chapters, nil
}

// ExtractChapterRequest asks for one chapter to be read.
type ExtractChapterRequest struct {
	ChapterID string `json:"chapterId"`
}

// ExtractionResultDTO reports what one reading wrote.
type ExtractionResultDTO struct {
	ChapterID string `json:"chapterId"`
	// Extracted counts the proposals that passed validation, before any write,
	// so a caller can tell "the model proposed nothing" from "the write failed".
	Extracted    int `json:"extracted"`
	Entities     int `json:"entities"`
	Events       int `json:"events"`
	Relations    int `json:"relations"`
	Aliases      int `json:"aliases"`
	Participants int `json:"participants"`
	Evidence     int `json:"evidence"`
	// Summary is the extractor's own description of the chapter, for the user's
	// review. It is a label, not an instruction, and nothing parses it.
	Summary string `json:"summary,omitempty"`
}

// ExtractChapterEventCandidates reads one chapter and stores what it proposes.
//
// Everything it writes is a candidate. There is no parameter that changes that:
// PRD FR-030 keeps a user or rule gate in front of every confirmed fact, and the
// gate is the accept command on the story surface rather than an option here.
func (b *ImportBinding) ExtractChapterEventCandidates(request ExtractChapterRequest) (ExtractionResultDTO, error) {
	service := b.extractionService()
	if service == nil {
		return ExtractionResultDTO{}, bindingUnavailable()
	}
	result, err := service.ExtractChapterEventCandidates(b.context(), request.ChapterID)
	if err != nil {
		return ExtractionResultDTO{}, toDramaError(err)
	}
	return ExtractionResultDTO{
		ChapterID:    result.ChapterID,
		Extracted:    result.Extracted,
		Entities:     result.Entities,
		Events:       result.Events,
		Relations:    result.Relations,
		Aliases:      result.Aliases,
		Participants: result.Participants,
		Evidence:     result.Evidence,
		Summary:      result.Summary,
	}, nil
}

// nonNilStrings returns an empty slice rather than nil, so the JSON is `[]`
// rather than `null` and the frontend does not have to handle both.
func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
