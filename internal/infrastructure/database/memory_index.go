package database

import (
	"context"
	"strings"
	"time"

	appmemory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/memory"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/memory"
)

// The index's vocabulary is the APPLICATION package's, not this one's.
//
// It is a type alias set rather than a second definition, because the port and its
// implementation have to agree about what a VectorItem is down to the field names: two
// structurally identical structs would compile at every call site and fail the moment one of
// them gained a field.
type VectorItem = appmemory.VectorItem
type VectorHit = appmemory.VectorHit
type SearchOptions = appmemory.SearchOptions

// memory_index.go is the VectorIndex implementation of ARCHITECTURE section 12.3.
//
// # What "index" means here
//
// An exact scan, not an approximate one. Section 12.3's MVP is "Float32 BLOB、小规模归一化点积、
// Scope 过滤、Threshold + TopK、可测试排序" and its V1 is a sqlite-vec adapter; this is the MVP,
// and the honest description of it is a scan with the right filters rather than an index.
//
// That is a deliberate choice with a recorded reason rather than a shortcut. ADR-0002 section
// 68 denies dynamic extension loading by default: "sqlite-vec or another vector mechanism
// requires its own compatibility/security evidence; persistent Memory does not justify enabling
// arbitrary extension loading". So the vectors live in a BLOB column and are scored in Go, and
// the port is the four methods a future adapter replaces.
//
// # Why the scope filter is a query rather than a condition
//
// AGENT_CONTRACTS section 12.2 requires the scope filter to run BEFORE scoring. The way to make
// that true rather than intended is for the search to read its candidates through a query that
// already carries the scope, which is what the repository's VectorCandidates does. A search
// that read every row and filtered in Go would satisfy the letter of the requirement and cost
// the whole project's vectors on every question, and a mistake in the filter would be a leak
// rather than a slow query.
//
// # The ceiling
//
// Every read is bounded by appmemory.MaxCandidates, taken from the caller's options and clamped
// here. SECURITY section 7.5's "Memory 候选上限" is the reason: the cost of a search must not be
// a function of how much a user has accumulated.

// MemoryVectorIndex scores stored vectors for one scope.
type MemoryVectorIndex struct {
	items *MemoryRepository
}

// NewMemoryVectorIndex builds the index over the memory repository.
//
// It takes the repository rather than a connection because the two are one storage: the
// vectors ARE columns on memory_items rather than a side table, so an index that could not see
// the rows it indexes would be a second reader of the same table with its own ideas about the
// scope filter.
func NewMemoryVectorIndex(items *MemoryRepository) *MemoryVectorIndex {
	return &MemoryVectorIndex{items: items}
}

// Upsert writes the vectors of the named items.
//
// ARCHITECTURE section 12.3 declares this method as "Upsert(ctx, items []VectorItem) error" and
// the port adds a scope, because a vector without a scope cannot be filtered and section 12.2
// requires the filter. What the method DOES is the storage's business, and here the vector is a
// column on the item row — so this writes that column, through the same repository call every
// other path uses.
//
// THE FIRST VERSION VALIDATED AND RETURNED NIL WITHOUT WRITING, and its comment justified that
// as "the write belongs to the item's own update". The reviewer's probe called it directly, got
// no error, and found the row still unembedded: a method named Upsert that silently does not
// upsert is exactly the "interface with no real path" shape, and a caller that trusted it would
// find out through a search returning nothing. Refusing instead of writing would have been
// honest; writing is better, because the port says write.
//
// The scope is CHECKED against the row rather than assumed. A caller that passed another
// project's item under this scope would otherwise store a vector that the scope-filtered search
// could never return — a silent no-op wearing the other mask.
func (i *MemoryVectorIndex) Upsert(ctx context.Context, scope memory.Scope, items []VectorItem) error {
	if i == nil || i.items == nil {
		return memory.StorageError("The memory index is unavailable.", nil)
	}
	if err := scope.Validate(); err != nil {
		return err
	}
	rows := make([]upsertedVector, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.ID) == "" {
			return memory.InvalidError("A vector must name the memory it belongs to.")
		}
		if len(item.Vector) == 0 {
			return memory.InvalidError("A vector cannot be empty.")
		}
		record, err := i.items.GetItem(ctx, item.ID)
		if err != nil {
			return err
		}
		if record.Deleted() {
			return memory.InvalidError("A deleted memory cannot be embedded.")
		}
		// The row's own scope decides, not the caller's argument: a mismatch means the caller is
		// storing under a scope the row does not live in, and the vector would be unreachable.
		if record.Scope.Project != scope.Project {
			return memory.InvalidError("A vector belongs to the project its memory lives in.")
		}
		if strings.TrimSpace(item.Model) == "" || strings.TrimSpace(item.Version) == "" {
			// Section 14.5 again: a vector with no model cannot be searched, because the search
			// selects candidates by model and version. Storing one would be storing a row no query
			// can ever reach.
			return memory.InvalidError("A vector must name the model and version that produced it.")
		}
		rows = append(rows, upsertedVector{
			id:      item.ID,
			blob:    memory.EncodeVector(memory.Normalize(item.Vector)),
			model:   strings.TrimSpace(item.Model),
			version: strings.TrimSpace(item.Version),
		})
	}
	now := time.Now().UTC()
	for _, row := range rows {
		if err := i.items.AssignEmbedding(ctx, row.id, row.blob, row.model, row.version, now); err != nil {
			return err
		}
	}
	return nil
}

// upsertedVector is one encoded vector on its way to a row.
type upsertedVector struct {
	id      string
	blob    []byte
	model   string
	version string
}

// Search returns the nearest items in one scope and one embedding version.
//
// The order of the work is the requirement: read the scope's candidates for this model and
// version, then normalise nothing (the stored vectors are already normalised), then score, then
// order, then cut to TopK. TopK is applied HERE rather than by the caller because a search that
// returned everything and let the caller slice would make the cost of a query a function of the
// project's size.
func (i *MemoryVectorIndex) Search(ctx context.Context, scope memory.Scope, vector []float32, options SearchOptions) ([]VectorHit, error) {
	if i == nil || i.items == nil {
		return nil, memory.StorageError("The memory index is unavailable.", nil)
	}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if len(vector) == 0 {
		return nil, memory.InvalidError("A search needs a query vector.")
	}
	if strings.TrimSpace(options.Model) == "" || strings.TrimSpace(options.Version) == "" {
		return nil, memory.InvalidError("A search must name the embedding model and version.")
	}
	// A zero limit is left at zero and the repository applies its ceiling, which is
	// MaxCandidates. The port's doc names DefaultCandidates for a zero, and that was wrong: the
	// repository has one answer for a caller that stated no bound, and naming a second one here
	// would be a number no code produces.
	if options.Limit < 0 {
		options.Limit = 0
	}
	// The WHOLE scope, not its project and episode: dropping the agent key here was the defect
	// the review found, and it made a decision agent able to recall a supervisor's conversation.
	items, err := i.items.VectorCandidates(ctx, scope, options.Model, options.Version, options.Limit)
	if err != nil {
		return nil, err
	}
	query := memory.Normalize(vector)
	hits := make([]VectorHit, 0, len(items))
	for _, item := range items {
		// Section 14.5's "重建后切换索引版本" enforced once more at the point of comparison:
		// the query already selected by model and version, and this refuses a row whose blob is
		// absent or from another space, so a decode of the wrong length cannot become a score.
		if !item.EmbeddingIsCurrent(options.Model, options.Version) {
			continue
		}
		stored, err := memory.DecodeVector(item.EmbeddingBlob)
		if err != nil {
			// A row that cannot be decoded is SKIPPED rather than failing the search. It is a
			// storage defect on one row, and refusing every query for it would make one bad
			// vector take the whole feature down; the rebuild command is what repairs it.
			continue
		}
		if len(stored) != len(query) {
			// Different dimensions, so the two vectors are from different spaces whatever their
			// columns say. Scoring them would produce a number with no meaning.
			continue
		}
		hits = append(hits, VectorHit{ID: item.ID, Similarity: memory.Dot(query, stored)})
	}
	// Best first, ties by identifier, so the order is total and repeated searches agree.
	for index := 1; index < len(hits); index++ {
		current := hits[index]
		position := index - 1
		for position >= 0 {
			better := current.Similarity > hits[position].Similarity
			if !better && current.Similarity == hits[position].Similarity {
				better = current.ID < hits[position].ID
			}
			if !better {
				break
			}
			hits[position+1] = hits[position]
			position--
		}
		hits[position+1] = current
	}
	topK := options.TopK
	if topK <= 0 {
		topK = 0
	}
	if topK > 0 && len(hits) > topK {
		hits = hits[:topK]
	}
	return hits, nil
}

// Delete removes the named items' vectors.
//
// Clearing the columns rather than the rows: an item without a vector is a memory that has not
// been embedded yet, which is a state the rebuild command recovers from, while a deleted item is
// a memory the user removed.
func (i *MemoryVectorIndex) Delete(ctx context.Context, ids []string) error {
	if i == nil || i.items == nil {
		return memory.StorageError("The memory index is unavailable.", nil)
	}
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			return memory.InvalidError("A vector delete must name the memory.")
		}
	}
	return i.items.ClearEmbedding(ctx, ids, time.Now().UTC())
}

// Rebuild re-embeds a scope from scratch.
//
// The port declares it and this implementation REFUSES it, which is the honest answer rather
// than a stub: re-embedding needs an embedding provider, and the infrastructure layer has no
// provider registry. The rebuild lives on the application service
// (RebuildEmbedding), which does have one, and it reaches the same rows through
// ItemsNeedingEmbedding. A method here that pretended to do it would need a provider it cannot
// obtain, and a caller that called it would get a rebuild that did not rebuild.
func (i *MemoryVectorIndex) Rebuild(ctx context.Context, scope memory.Scope) error {
	if i == nil || i.items == nil {
		return memory.StorageError("The memory index is unavailable.", nil)
	}
	if err := scope.Validate(); err != nil {
		return err
	}
	return memory.InvalidError("Rebuilding embeddings needs an embedding provider, so it is a command on the memory service rather than an index operation.")
}

// ClearEmbedding removes the vectors of the named items.
func (r *MemoryRepository) ClearEmbedding(ctx context.Context, ids []string, at time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	return r.withinTx(ctx, func(repo *MemoryRepository) error {
		conn := repo.conn()
		if conn == nil {
			return memory.StorageError("The memory store is unavailable.", nil)
		}
		for _, id := range ids {
			if _, err := conn.ExecContext(ctx, `UPDATE memory_items
				SET embedding_blob = NULL, embedding_model = '', embedding_version = '',
				    embedded_at = '', updated_at = ?, revision = revision + 1 WHERE id = ?`,
				formatTime(at), id); err != nil {
				return memory.StorageError("The embedding could not be cleared.", err)
			}
		}
		return nil
	})
}

// PinnedItems returns a project's locked high-importance memories.
//
// The predicate is in SQL rather than in Go because it is the channel AC-MEM-003 names, and a
// channel that read every memory to find its members would make a project's size the cost of a
// constant answer. The importance floor is passed rather than written into the statement so the
// domain owns the number.
func (r *MemoryRepository) PinnedItems(ctx context.Context, projectID string, limit int) ([]memory.MemoryItem, error) {
	conn := r.conn()
	if conn == nil {
		return nil, memory.StorageError("The memory store is unavailable.", nil)
	}
	project := strings.TrimSpace(projectID)
	if project == "" {
		return nil, memory.InvalidError("A pinned read must name its project.")
	}
	if limit <= 0 {
		limit = DefaultMemoryListLimit
	}
	if limit > MaxMemoryListLimit {
		limit = MaxMemoryListLimit
	}
	rows, err := conn.QueryContext(ctx, memoryItemSelectColumns+
		` WHERE scope_project = ? AND locked = 1 AND importance >= ? AND deleted_at = ''
		  ORDER BY importance DESC, created_at DESC, id DESC LIMIT ?`,
		project, memory.HighImportance, limit)
	if err != nil {
		return nil, memory.StorageError("The pinned memories could not be read.", err)
	}
	defer rows.Close()
	return scanMemoryItems(rows)
}
