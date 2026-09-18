// Package story is the application layer for source documents, chapters and
// the story fact layer of DOMAIN_MODEL §5 and §6. It owns the commands and
// queries and defines the persistence ports infrastructure implements. It
// performs no I/O itself.
package story

import (
	"context"
	"time"

	storydomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/story"
)

// Clock abstracts time so tests are deterministic.
type Clock interface {
	Now() time.Time
}

// IDGenerator produces entity identifiers. ADR-0005 fixes the format as
// UUIDv7 and requires generation to happen here, in the application layer, so
// every repository receives an identifier it did not mint.
type IDGenerator interface {
	New() (string, error)
}

// SourceDocumentRepository persists source documents and their versions.
type SourceDocumentRepository interface {
	// CreateSourceDocument stores a document. A duplicate id is a conflict.
	CreateSourceDocument(ctx context.Context, record storydomain.SourceDocument) error
	// GetSourceDocument returns one document by id.
	GetSourceDocument(ctx context.Context, id string) (storydomain.SourceDocument, error)
	// ListSourceDocuments returns a project's documents oldest first, without
	// the rows a soft delete hid.
	ListSourceDocuments(ctx context.Context, projectID string) ([]storydomain.SourceDocument, error)
	// UpdateSourceDocument persists a change guarded by the expected revision.
	UpdateSourceDocument(ctx context.Context, record storydomain.SourceDocument, expectedRevision int64) error

	// CreateSourceDocumentVersion stores one import of a document. A duplicate
	// (document, version number) pair is a conflict.
	CreateSourceDocumentVersion(ctx context.Context, version storydomain.SourceDocumentVersion) error
	// MaxSourceDocumentVersionNumber reports the highest version number a
	// document has, or zero when it has none.
	MaxSourceDocumentVersionNumber(ctx context.Context, sourceDocumentID string) (int, error)
}

// ChapterRepository persists chapters of a document version.
type ChapterRepository interface {
	// CreateChapter stores a chapter. A duplicate (version, ordinal) pair is a
	// conflict.
	CreateChapter(ctx context.Context, chapter storydomain.Chapter) error
	// GetChapter returns one chapter by id.
	GetChapter(ctx context.Context, id string) (storydomain.Chapter, error)
	// ListChapters returns a version's chapters in reading order.
	ListChapters(ctx context.Context, sourceDocumentVersionID string) ([]storydomain.Chapter, error)
	// UpdateChapter persists a change guarded by the expected revision.
	UpdateChapter(ctx context.Context, chapter storydomain.Chapter, expectedRevision int64) error
}

// StoryEntityRepository persists story entities.
type StoryEntityRepository interface {
	// CreateStoryEntity stores an entity.
	CreateStoryEntity(ctx context.Context, record storydomain.StoryEntity) error
	// GetStoryEntity returns one entity by id.
	GetStoryEntity(ctx context.Context, id string) (storydomain.StoryEntity, error)
	// UpdateStoryEntity persists a change guarded by the expected revision.
	UpdateStoryEntity(ctx context.Context, record storydomain.StoryEntity, expectedRevision int64) error
}

// StoryEventRepository persists story events.
type StoryEventRepository interface {
	// CreateStoryEvent stores an event.
	CreateStoryEvent(ctx context.Context, record storydomain.StoryEvent) error
	// GetStoryEvent returns one event by id.
	GetStoryEvent(ctx context.Context, id string) (storydomain.StoryEvent, error)
	// UpdateStoryEvent persists a change guarded by the expected revision.
	UpdateStoryEvent(ctx context.Context, record storydomain.StoryEvent, expectedRevision int64) error
}

// StoryRelationRepository persists story relations.
type StoryRelationRepository interface {
	// CreateStoryRelation stores a relation. A duplicate id is a conflict.
	CreateStoryRelation(ctx context.Context, record storydomain.StoryRelation) error
	// GetStoryRelation returns one relation by id.
	GetStoryRelation(ctx context.Context, id string) (storydomain.StoryRelation, error)
}

// StoryConflictRepository persists recorded fact conflicts.
//
// The conflict table carries no revision column (migration 000007 is the
// authority), so the state a caller read is the concurrency token: an update
// names the status it expects and matches no row once the conflict moved.
type StoryConflictRepository interface {
	// CreateStoryConflict stores a conflict. The schema's unique constraint is
	// over the ordered fact pair, so the same pair in one order is a conflict.
	CreateStoryConflict(ctx context.Context, record storydomain.StoryFactConflict) error
	// GetStoryConflict returns one conflict by id.
	GetStoryConflict(ctx context.Context, id string) (storydomain.StoryFactConflict, error)
	// UpdateStoryConflict persists a change guarded by the expected status.
	UpdateStoryConflict(ctx context.Context, record storydomain.StoryFactConflict, expectedStatus storydomain.ConflictStatus) error
}

// Repository is everything the story service drives. Infrastructure supplies
// one implementation of each family over the same database handle.
type Repository interface {
	SourceDocumentRepository
	ChapterRepository
	StoryEntityRepository
	StoryEventRepository
	StoryRelationRepository
	StoryConflictRepository
}

// Service holds the story commands and queries.
type Service struct {
	repository Repository
	clock      Clock
	ids        IDGenerator
}

// Options configures a Service.
type Options struct {
	Repository Repository
	Clock      Clock
	IDs        IDGenerator
}
