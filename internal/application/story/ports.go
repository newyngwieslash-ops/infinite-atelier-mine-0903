// Package story is the application layer for source documents, chapters and
// the story fact layer of DOMAIN_MODEL §5 and §6. It owns the commands and
// queries and defines the persistence ports infrastructure implements. It
// performs no I/O itself.
package story

import (
	"context"
	eventsapp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/events"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
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
	// GetSourceDocumentVersion returns one version by id.
	GetSourceDocumentVersion(ctx context.Context, id string) (storydomain.SourceDocumentVersion, error)
	// MaxSourceDocumentVersionNumber reports the highest version number a
	// document has, or zero when it has none.
	MaxSourceDocumentVersionNumber(ctx context.Context, sourceDocumentID string) (int, error)
	// FindVersionBySourceHash returns the newest version in a project whose
	// original upload has this hash, with the name of the document it belongs
	// to. The final result is false when none does.
	//
	// It exists for PRD FR-020's duplicate warning. The check is scoped to a
	// project because the same file imported into two dramas is two documents
	// rather than a duplicate: a cross-project match would warn a user about
	// someone else's story.
	FindVersionBySourceHash(ctx context.Context, projectID, sourceHash string) (storydomain.SourceDocumentVersion, string, bool, error)
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
	// ConfirmChapters marks a version's boundaries confirmed and records the
	// governance event in the same transaction.
	//
	// One call rather than a loop of updates, because the event records ONE
	// decision ("the user confirmed these boundaries") while the writes are
	// many. Splitting them would let the record claim a confirmation that only
	// partially landed.
	ConfirmChapters(ctx context.Context, sourceDocumentVersionID string, record event.Event) error
}

// StoryEntityRepository persists story entities.
type StoryEntityRepository interface {
	// CreateStoryEntity stores an entity.
	CreateStoryEntity(ctx context.Context, record storydomain.StoryEntity) error
	// GetStoryEntity returns one entity by id.
	GetStoryEntity(ctx context.Context, id string) (storydomain.StoryEntity, error)
	// UpdateStoryEntity persists a change guarded by the expected revision.
	UpdateStoryEntity(ctx context.Context, record storydomain.StoryEntity, expectedRevision int64) error
	// ListStoryEntities returns a project's entities, oldest first, without the
	// rows a soft delete hid.
	//
	// A status filter is a separate argument rather than folded into the project
	// id, because the story-graph panel asks two different questions: everything
	// the project has, and the candidates awaiting review. An empty status means
	// "any".
	ListStoryEntities(ctx context.Context, projectID string, status storydomain.FactStatus) ([]storydomain.StoryEntity, error)
	// CreateStoryEntityAlias stores an alternative name. A duplicate
	// (entity, alias) pair is a conflict, because the schema's unique index is
	// over exactly that pair.
	CreateStoryEntityAlias(ctx context.Context, record storydomain.StoryEntityAlias) error
	// ListStoryEntityAliases returns an entity's aliases oldest first.
	ListStoryEntityAliases(ctx context.Context, storyEntityID string) ([]storydomain.StoryEntityAlias, error)
}

// StoryEventRepository persists story events.
type StoryEventRepository interface {
	// CreateStoryEvent stores an event.
	CreateStoryEvent(ctx context.Context, record storydomain.StoryEvent) error
	// GetStoryEvent returns one event by id.
	GetStoryEvent(ctx context.Context, id string) (storydomain.StoryEvent, error)
	// UpdateStoryEvent persists a change guarded by the expected revision.
	UpdateStoryEvent(ctx context.Context, record storydomain.StoryEvent, expectedRevision int64) error
	// ListStoryEvents returns a project's events in story order, without the
	// rows a soft delete hid. An empty chapterID means every chapter; an empty
	// status means any status.
	//
	// The order is (ordinal, id) rather than chapter order, because the panel
	// shows the story's timeline and that is what ordinal means. The id breaks a
	// tie so two events at one position come back in a stable order rather than
	// in whatever order SQLite happened to return.
	ListStoryEvents(ctx context.Context, projectID, chapterID string, status storydomain.FactStatus) ([]storydomain.StoryEvent, error)
	// CreateStoryEventParticipant links an entity to an event. A duplicate
	// (event, entity, role) triple is a conflict.
	CreateStoryEventParticipant(ctx context.Context, record storydomain.StoryEventParticipant) error
	// ListStoryEventParticipants returns an event's participants ordered by role
	// then entity, so the same event renders the same way twice.
	ListStoryEventParticipants(ctx context.Context, storyEventID string) ([]storydomain.StoryEventParticipant, error)
}

// StoryRelationRepository persists story relations.
type StoryRelationRepository interface {
	// CreateStoryRelation stores a relation. A duplicate id is a conflict.
	CreateStoryRelation(ctx context.Context, record storydomain.StoryRelation) error
	// GetStoryRelation returns one relation by id.
	GetStoryRelation(ctx context.Context, id string) (storydomain.StoryRelation, error)
	// ListStoryRelations returns a project's relations, oldest first, without the
	// rows a soft delete hid. An empty status means any status.
	ListStoryRelations(ctx context.Context, projectID string, status storydomain.FactStatus) ([]storydomain.StoryRelation, error)
}

// StoryFactSourceRepository persists the evidence a fact cites.
type StoryFactSourceRepository interface {
	// CreateStoryFactSource stores one evidence row.
	CreateStoryFactSource(ctx context.Context, record storydomain.StoryFactSource) error
	// ListStoryFactSources returns the evidence for one fact, oldest first.
	ListStoryFactSources(ctx context.Context, factType storydomain.FactType, factID string) ([]storydomain.StoryFactSource, error)
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
	StoryFactSourceRepository
	StoryConflictRepository
}

// EventRecorder builds and writes domain events for the commands that emit them.
//
// Two paths, and the difference is deliberate (ADR-0009):
//
//   - Build assembles an event for a command that records it inside its own
//     transaction. An approval uses this, and refuses without a recorder,
//     because its event is a governance record rather than a notification.
//   - RecordBestEffort writes a notification event and reports no error. The
//     command's own write has already succeeded, so failing it because the
//     announcement did not land would be the wrong trade.
//
// The port is optional: a Service composed without a recorder still serves every
// command, and only the transactional path refuses.
type EventRecorder interface {
	Build(ctx context.Context, draft eventsapp.Draft) (event.Event, error)
	RecordBestEffort(ctx context.Context, draft eventsapp.Draft)
}

// Service holds the story commands and queries.
type Service struct {
	repository Repository
	clock      Clock
	ids        IDGenerator
	events     EventRecorder
}

// Options configures a Service.
type Options struct {
	Repository Repository
	Clock      Clock
	IDs        IDGenerator
	// Events enables the commands that announce a fact change.
	Events EventRecorder
}
