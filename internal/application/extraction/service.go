// Package extraction orchestrates one chapter's reading: it asks an Extractor
// for proposals, validates them against the contract, and writes the accepted
// ones as candidates.
//
// It implements docs/ROADMAP.md scope items 6 and 7. The shape is deliberate and
// the roadmap names why: "如果 Agent Runtime 尚未完成，Event Extraction 先通过明确的
// Application Service + Mock/Provider 适配实现，WP-07 再接入统一 Runtime，不创建临时不
// 可迁移架构.". So the Extractor port is the seam WP-07's runtime will implement,
// and nothing here is a placeholder to be thrown away: it is the application
// service the runtime will call.
//
// Three invariants this package enforces, each from a different part of the
// specification:
//
//   - Everything it writes is a CANDIDATE. There is no path here that accepts,
//     locks or approves a fact, which is PRD FR-030's gate.
//   - A chapter is untrusted data. Nothing in its text is executed, followed or
//     obeyed; the injection fixture exists so a test can say so.
//   - It writes nothing until the whole document validates. A half-written
//     extraction would leave the user reviewing a graph that does not match any
//     document, which is worse than a refusal.
package extraction

import (
	"context"
	"strings"
	"time"

	appstory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/story"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/validation"
	extractiondomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/extraction"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/story"
)

// Extractor reads a chapter and proposes facts about it.
//
// This is the seam WP-07 replaces with the agent runtime. Its contract is
// deliberately narrow: text in, raw JSON bytes out, no write access, no tools.
//
// The bytes it returns are WHAT THE MODEL SAID, unvalidated. Validation is this
// package's job, not the extractor's, because an extractor that validated its own
// output would be grading its own work — and because AGENT_CONTRACTS section 14.3
// puts validation in the runtime that can then ask for one repair.
type Extractor interface {
	// Extract returns the model's raw output for one chapter.
	//
	// The context carries cancellation, and an implementation must honour it:
	// AGENT_CONTRACTS section 15 requires a cancel to reach the provider.
	Extract(ctx context.Context, request Request) ([]byte, error)
}

// Request is what an extractor is given about one chapter.
//
// It carries the text because extraction needs it, and nothing else: no project
// identifier, no other chapter, no fact from the graph. An extractor that could
// read the whole project could correlate across chapters, which is a different
// stage with different evidence rules.
type Request struct {
	// ChapterID names the chapter, so an extractor's log line can point at what
	// it read.
	ChapterID string
	// Title is the chapter's heading, which a model uses for context.
	Title string
	// Text is the chapter's normalized text.
	Text string
	// Language is the document's language where it is known, so an extractor can
	// ask for a reply in the same language.
	Language string
}

// Clock abstracts time so tests are deterministic.
type Clock interface {
	Now() time.Time
}

// IDGenerator produces entity identifiers.
type IDGenerator interface {
	New() (string, error)
}

// ChapterReader is the input side: the chapter text and the identity of the
// document version it came from.
type ChapterReader interface {
	// ChapterWithText returns one chapter together with its text and the version
	// that text belongs to.
	//
	// One call rather than two, because the evidence row needs the version id and
	// a second lookup could observe a different version than the one the text was
	// read from.
	ChapterWithText(ctx context.Context, chapterID string) (ChapterText, error)
}

// ChapterText is a chapter and the text it indexes.
type ChapterText struct {
	Chapter story.Chapter
	// ProjectID is the project the chapter's document belongs to.
	//
	// A Chapter row does not carry it — the chain is chapter to version to
	// document to project — so the reader, which walks that chain to find the
	// text anyway, reports it here rather than making this package walk it a
	// second time and risk reading two different states.
	ProjectID string
	// SourceDocumentVersionID is the version whose normalized text the chapter's
	// offsets index. It is what an evidence row names.
	SourceDocumentVersionID string
	Text                    string
	// Language of the document, where known.
	Language string
}

// Service orchestrates one extraction.
type Service struct {
	extractor Extractor
	reader    ChapterReader
	story     *appstory.Service
	clock     Clock
	ids       IDGenerator
}

// Options configures a Service.
type Options struct {
	Extractor Extractor
	Reader    ChapterReader
	Story     *appstory.Service
	Clock     Clock
	IDs       IDGenerator
}

// NewService builds the extraction service.
func NewService(options Options) *Service {
	return &Service{
		extractor: options.Extractor,
		reader:    options.Reader,
		story:     options.Story,
		clock:     options.Clock,
		ids:       options.IDs,
	}
}

// Available reports whether the service can operate.
//
// A service without an extractor is NOT available, and that is the fail-closed
// rule for this package: there is no default extractor, so a build that was
// never given one refuses the command with a reason instead of inventing facts.
// The Mock extractor lives in the test tree and is attached explicitly.
func (s *Service) Available() bool {
	return s != nil && s.extractor != nil && s.reader != nil && s.story != nil && s.ids != nil
}

func (s *Service) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now().UTC()
	}
	return s.clock.Now().UTC()
}

// Result reports what one extraction wrote.
type Result struct {
	ChapterID string
	// Extracted counts the proposals that passed validation, before any write,
	// so a caller can tell "the model proposed nothing" from "the write failed".
	Extracted int
	// Entities, Events, Relations and Aliases count what was stored.
	Entities  int
	Events    int
	Relations int
	Aliases   int
	// Participants counts the links written.
	Participants int
	// Evidence counts the fact-source rows written.
	Evidence int
	// Summary is the extractor's own one-line description, for the user's review.
	Summary string
}

// ExtractChapterEventCandidates reads one chapter and stores what it proposes.
//
// The order is: read the text, ask, validate, then write. Each step can refuse
// and the refusals are distinct, because the caller acts on them differently: a
// validation failure is worth one repair attempt, a storage failure is worth a
// retry, and a missing chapter is worth nothing at all.
//
// Nothing is written until validation has passed, so a refused document leaves
// the graph exactly as it was.
func (s *Service) ExtractChapterEventCandidates(ctx context.Context, chapterID string) (Result, error) {
	if !s.Available() {
		return Result{}, extractiondomain.UnavailableError()
	}
	trimmed := strings.TrimSpace(chapterID)
	if trimmed == "" {
		return Result{}, extractiondomain.InvalidRequestError("A chapter is required.")
	}
	chapter, err := s.reader.ChapterWithText(ctx, trimmed)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(chapter.Text) == "" {
		return Result{}, extractiondomain.InvalidRequestError("That chapter has no text to read.")
	}
	raw, err := s.extractor.Extract(ctx, Request{
		ChapterID: chapter.Chapter.ID,
		Title:     chapter.Chapter.Title,
		Text:      chapter.Text,
		Language:  chapter.Language,
	})
	if err != nil {
		// The extractor's own failure passes through: it already carries a
		// category, and re-wrapping it would lose whether a retry could help.
		return Result{}, err
	}
	document, err := validation.EventExtraction(raw)
	if err != nil {
		return Result{}, err
	}
	return s.store(ctx, chapter, document)
}
