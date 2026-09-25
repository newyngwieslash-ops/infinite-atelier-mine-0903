package database

import (
	"context"
	"database/sql"
	"strings"
	"time"

	appmemory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/memory"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/memory"
)

// memory.go is the storage of the persistent memory aggregate (migration 000019).
//
// TWO READS, TWO TABLES, ONE RULE. The scope's parts are columns and every query filters
// on them (section 14.4's "检索实现必须能按结构字段隔离，不依赖脆弱字符串前缀"), so the
// project is always the first thing a WHERE clause names. A query that reached this file
// without a project is a leak, which is why no method here has a shape that could omit
// one.

// MemoryRepository stores memory items and their relations.
type MemoryRepository struct {
	db *sql.DB
	tx *sql.Tx
}

// NewMemoryRepository builds a repository over a connection.
func NewMemoryRepository(db *sql.DB) *MemoryRepository {
	return &MemoryRepository{db: db}
}

// WithinTx returns a repository bound to a transaction.
func (r *MemoryRepository) WithinTx(tx *sql.Tx) *MemoryRepository {
	return &MemoryRepository{db: r.db, tx: tx}
}

func (r *MemoryRepository) conn() querier {
	if r == nil {
		return nil
	}
	return connection(r.db, r.tx)
}

// withinTx runs fn inside a transaction of this repository's database.
//
// The same helper the asset repository states, repeated because the two repositories are
// separate types over the same connection and Go has no shared base class. It is NOT a
// second implementation of a rule: the rule is "begin, run, commit" and there is nothing
// to disagree about.
func (r *MemoryRepository) withinTx(ctx context.Context, fn func(repo *MemoryRepository) error) error {
	if r == nil || r.db == nil {
		return memory.StorageError("The memory store is unavailable.", nil)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return memory.StorageError("The memory could not be saved.", err)
	}
	if err := fn(r.WithinTx(tx)); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return memory.StorageError("The memory could not be saved.", err)
	}
	return nil
}

// memoryItemSelectColumns is the item's read list, in the order scanMemoryItem expects.
//
// It is a const rather than repeated in each query so adding a column is one edit: a
// second list would be a place for the two to disagree, and a scan that read the wrong
// column would produce a plausible record with the fields swapped.
const memoryItemSelectColumns = `SELECT id, scope_key, scope_tenant, scope_workspace,
	scope_project, scope_episode, scope_agent_key, scope_session, memory_type, role,
	agent_key, content, importance, confidence, embedding_blob, embedding_model,
	embedding_version, embedded_at, summarized, summary_level, locked, source_type, source_id,
	deleted_at, created_at, updated_at, revision FROM memory_items`

// CreateItem stores one memory.
func (r *MemoryRepository) CreateItem(ctx context.Context, item memory.MemoryItem) error {
	if err := item.Validate(); err != nil {
		return err
	}
	conn := r.conn()
	if conn == nil {
		return memory.StorageError("The memory store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO memory_items
		(id, scope_key, scope_tenant, scope_workspace, scope_project, scope_episode,
		 scope_agent_key, scope_session, memory_type, role, agent_key, content, importance,
		 confidence, embedding_blob, embedding_model, embedding_version, embedded_at,
		 summarized, summary_level, locked, source_type, source_id, deleted_at, created_at, updated_at, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.Scope.Key(), item.Scope.Tenant, item.Scope.Workspace, item.Scope.Project,
		item.Scope.Episode, item.Scope.AgentKey, item.Scope.Session, string(item.Type),
		string(item.Role), item.AgentKey, item.Content, item.Importance, item.Confidence,
		nullableBytes(item.EmbeddingBlob), item.EmbeddingModel, item.EmbeddingVersion,
		formatTime(item.EmbeddedAt), boolInt(item.Summarized), item.SummaryLevel, boolInt(item.Locked),
		string(item.SourceType), item.SourceID, formatTime(item.DeletedAt),
		formatTime(item.CreatedAt), formatTime(item.UpdatedAt), item.Revision)
	if err != nil {
		if isUniqueViolation(err) {
			return memory.ConflictError("That memory already exists.")
		}
		return memory.StorageError("The memory could not be saved.", err)
	}
	return nil
}

// MemoryItemExists reports whether an identifier is taken.
//
// It exists so a caller that MINTED an identifier can tell a collision from a duplicate
// write: the two are the same SQLite error, and the first is worth retrying while the
// second is not.
func (r *MemoryRepository) MemoryItemExists(ctx context.Context, id string) (bool, error) {
	conn := r.conn()
	if conn == nil {
		return false, memory.StorageError("The memory store is unavailable.", nil)
	}
	var count int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_items WHERE id = ?`, id).Scan(&count); err != nil {
		return false, memory.StorageError("The memory could not be read.", err)
	}
	return count > 0, nil
}

// GetItem reads one memory, deleted ones included.
//
// A soft-deleted item is still readable, because AC-MEM-004 asks what happens to a
// summary when its source is deleted: a reader has to be able to see that the source was
// deleted rather than that it never existed. What filters deleted items out is the
// RECALL, which is a different question from "what is this row".
func (r *MemoryRepository) GetItem(ctx context.Context, id string) (memory.MemoryItem, error) {
	conn := r.conn()
	if conn == nil {
		return memory.MemoryItem{}, memory.StorageError("The memory store is unavailable.", nil)
	}
	item, err := scanMemoryItem(conn.QueryRowContext(ctx, memoryItemSelectColumns+` WHERE id = ?`, id))
	if err != nil {
		if err == sql.ErrNoRows {
			return memory.MemoryItem{}, memory.NotFoundError()
		}
		return memory.MemoryItem{}, memory.StorageError("The memory could not be read.", err)
	}
	return item, nil
}

// MemoryListFilter is the application port's own type, ALIASED rather than restated.
//
// The first version of this file declared a second struct with the same fields, and that was
// a defect rather than a style choice: two named types with one shape are not assignable, so
// `var _ Repository = (*MemoryRepository)(nil)` would not compile and — because nobody had
// written that assertion — nothing failed. The interface went unsatisfied, the service's
// `StorageAvailable` was false in every composed build, and every memory command returned
// "no memory store is configured" while the store sat there implemented and tested. The
// compile-time assertions at the bottom of this file are what refuse that shape now.
type MemoryListFilter = appmemory.MemoryListFilter

// DefaultMemoryListLimit bounds a list read, and MaxMemoryListLimit its ceiling.
const (
	DefaultMemoryListLimit = 100
	MaxMemoryListLimit     = 500
)

// ListItems returns a project's memories newest first.
func (r *MemoryRepository) ListItems(ctx context.Context, filter MemoryListFilter) ([]memory.MemoryItem, error) {
	conn := r.conn()
	if conn == nil {
		return nil, memory.StorageError("The memory store is unavailable.", nil)
	}
	project := strings.TrimSpace(filter.ProjectID)
	if project == "" {
		return nil, memory.InvalidError("A memory list must name its project.")
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = DefaultMemoryListLimit
	}
	if limit > MaxMemoryListLimit {
		limit = MaxMemoryListLimit
	}
	clauses := []string{"scope_project = ?"}
	arguments := []any{project}
	if len(filter.Types) > 0 {
		placeholders := make([]string, 0, len(filter.Types))
		for _, memoryType := range filter.Types {
			if !memory.IsValidMemoryType(memoryType) {
				return nil, memory.InvalidError("The memory type is not recognised.")
			}
			placeholders = append(placeholders, "?")
			arguments = append(arguments, string(memoryType))
		}
		clauses = append(clauses, "memory_type IN ("+strings.Join(placeholders, ", ")+")")
	}
	if !filter.IncludeDeleted {
		clauses = append(clauses, "deleted_at = ''")
	}
	arguments = append(arguments, limit)
	rows, err := conn.QueryContext(ctx, memoryItemSelectColumns+
		` WHERE `+strings.Join(clauses, " AND ")+` ORDER BY created_at DESC, id DESC LIMIT ?`, arguments...)
	if err != nil {
		return nil, memory.StorageError("The memories could not be read.", err)
	}
	defer rows.Close()
	return scanMemoryItems(rows)
}

// DeleteItem soft-deletes one memory, refusing the wrong actor on a pinned row.
//
// ONE STATEMENT RATHER THAN A READ, A CHECK AND A WRITE. The locked rule is in the WHERE
// clause, so two actors racing on the same pinned memory cannot both pass a check that
// each made against a stale read. The caller states which actor it is, and the database
// decides.
func (r *MemoryRepository) DeleteItem(ctx context.Context, id, actor string, at time.Time) (bool, error) {
	conn := r.conn()
	if conn == nil {
		return false, memory.StorageError("The memory store is unavailable.", nil)
	}
	query := `UPDATE memory_items SET deleted_at = ?, updated_at = ?, revision = revision + 1
		WHERE id = ? AND deleted_at = ''`
	arguments := []any{formatTime(at), formatTime(at), id}
	if memory.ActorType(actor) != memory.ActorUser {
		// Section 14.5: only the user may delete a pinned memory.
		query += ` AND locked = 0`
	}
	result, err := conn.ExecContext(ctx, query, arguments...)
	if err != nil {
		return false, memory.StorageError("The memory could not be deleted.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, memory.StorageError("The memory could not be deleted.", err)
	}
	return affected > 0, nil
}

// SetLocked pins or unpins a memory.
//
// The actor must be the user for BOTH directions. Unpinning a memory an agent pinned is
// the same act as pinning one, and section 14.5 gives the user the pin rather than giving
// the agent a way to remove it.
func (r *MemoryRepository) SetLocked(ctx context.Context, id string, locked bool, actor string, at time.Time) (bool, error) {
	conn := r.conn()
	if conn == nil {
		return false, memory.StorageError("The memory store is unavailable.", nil)
	}
	if memory.ActorType(actor) != memory.ActorUser {
		return false, memory.ConflictError("Only the user can pin or unpin a memory.")
	}
	result, err := conn.ExecContext(ctx, `UPDATE memory_items
		SET locked = ?, updated_at = ?, revision = revision + 1 WHERE id = ?`,
		boolInt(locked), formatTime(at), id)
	if err != nil {
		return false, memory.StorageError("The memory could not be pinned.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, memory.StorageError("The memory could not be pinned.", err)
	}
	return affected > 0, nil
}

// UpdateContent replaces a memory's text, refusing the wrong actor on a pinned row.
//
// The embedding is CLEARED, and that is the rule rather than an oversight: section 14.5
// says a model change does not overwrite an old vector, and the sharper version of the
// same rule is that an edit makes the vector WRONG. A vector that still described the old
// text would make the memory match queries about something it no longer says, which is a
// wrong answer nothing downstream could detect. Re-embedding is a separate command, so the
// gap is visible as "not embedded yet" rather than as a stale match.
func (r *MemoryRepository) UpdateContent(ctx context.Context, id, content string, actor string, at time.Time) (bool, error) {
	conn := r.conn()
	if conn == nil {
		return false, memory.StorageError("The memory store is unavailable.", nil)
	}
	if len([]rune(content)) > memory.MaxContentLength {
		return false, memory.InvalidError("The memory is larger than one message may be.")
	}
	query := `UPDATE memory_items SET content = ?, embedding_blob = NULL, embedding_model = '',
		embedding_version = '', embedded_at = '', updated_at = ?, revision = revision + 1
		WHERE id = ? AND deleted_at = ''`
	arguments := []any{content, formatTime(at), id}
	if memory.ActorType(actor) != memory.ActorUser {
		query += ` AND locked = 0`
	}
	result, err := conn.ExecContext(ctx, query, arguments...)
	if err != nil {
		return false, memory.StorageError("The memory could not be edited.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, memory.StorageError("The memory could not be edited.", err)
	}
	return affected > 0, nil
}

// AssignEmbedding writes a memory's vector.
//
// The model and version travel with the blob, because the index selects candidates by them
// and a vector whose provenance was not recorded would be invisible to every search.
func (r *MemoryRepository) AssignEmbedding(ctx context.Context, id string, embeddingBlob []byte, model, version string, at time.Time) error {
	conn := r.conn()
	if conn == nil {
		return memory.StorageError("The memory store is unavailable.", nil)
	}
	if len(embeddingBlob) == 0 || strings.TrimSpace(model) == "" || strings.TrimSpace(version) == "" {
		return memory.InvalidError("An embedding must carry a vector, its model and its version.")
	}
	_, err := conn.ExecContext(ctx, `UPDATE memory_items
		SET embedding_blob = ?, embedding_model = ?, embedding_version = ?, embedded_at = ?,
		    updated_at = ?, revision = revision + 1
		WHERE id = ? AND deleted_at = ''`,
		embeddingBlob, model, version, formatTime(at), formatTime(at), id)
	if err != nil {
		return memory.StorageError("The embedding could not be saved.", err)
	}
	return nil
}

// ItemsNeedingEmbedding returns the memories of a project that have no vector from the
// named model and version.
//
// The predicate is "not current" rather than "has no blob", which is what makes a rebuild
// after a model change do something: section 14.5's "重建后切换索引版本" needs the rows
// embedded by the OLD model to be found again, and a predicate that only looked for an
// empty blob would skip every one of them.
func (r *MemoryRepository) ItemsNeedingEmbedding(ctx context.Context, projectID, model, version string, limit int) ([]memory.MemoryItem, error) {
	conn := r.conn()
	if conn == nil {
		return nil, memory.StorageError("The memory store is unavailable.", nil)
	}
	project := strings.TrimSpace(projectID)
	if project == "" {
		return nil, memory.InvalidError("A rebuild must name its project.")
	}
	if limit <= 0 {
		limit = DefaultMemoryListLimit
	}
	if limit > MaxMemoryListLimit {
		limit = MaxMemoryListLimit
	}
	rows, err := conn.QueryContext(ctx, memoryItemSelectColumns+
		` WHERE scope_project = ? AND deleted_at = ''
		  AND (embedding_blob IS NULL OR embedding_model <> ? OR embedding_version <> ?)
		  ORDER BY created_at ASC, id ASC LIMIT ?`, project, model, version, limit)
	if err != nil {
		return nil, memory.StorageError("The memories could not be read.", err)
	}
	defer rows.Close()
	return scanMemoryItems(rows)
}

// VectorCandidates returns the embedded memories a search may score.
//
// FOUR FILTERS, IN THIS ORDER, AND THEY ARE ALL IN SQL: the project, the agent, the model
// and version, and the deleted flag. AGENT_CONTRACTS section 12.2 requires the scope filter to
// run BEFORE scoring, and doing it here is what makes that true structurally — the scorer
// cannot see another scope's row because it was never returned.
//
// THE AGENT IS ONE OF THEM, and it was missing until the review caught it. The first version
// filtered the project and the episode and dropped scope.AgentKey on the floor, while four doc
// comments — this file's, the index's, the service's and the port's — all asserted the scope
// filter ran in full. The consequence was that a decision agent could recall a supervisor's
// conversation by similarity, which is the cross-scope leak section 14.4's structural filtering
// exists to prevent; the transcript path had always filtered it, so the two channels disagreed
// about what a scope was. A blank part still widens (see scopeClauses), which is what makes a
// project-level or episode-level read expressible.
func (r *MemoryRepository) VectorCandidates(ctx context.Context, scope memory.Scope, model, version string, limit int) ([]memory.MemoryItem, error) {
	conn := r.conn()
	if conn == nil {
		return nil, memory.StorageError("The memory store is unavailable.", nil)
	}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(model) == "" || strings.TrimSpace(version) == "" {
		return nil, memory.InvalidError("A memory search must name the embedding model and version.")
	}
	if limit <= 0 {
		limit = appmemory.MaxCandidates
	}
	if limit > appmemory.MaxCandidates {
		limit = appmemory.MaxCandidates
	}
	clauses, arguments := scopeClauses(scope)
	clauses = append(clauses, "embedding_model = ?", "embedding_version = ?",
		"embedding_blob IS NOT NULL", "deleted_at = ''")
	arguments = append(arguments, model, version, limit)
	rows, err := conn.QueryContext(ctx, memoryItemSelectColumns+
		` WHERE `+strings.Join(clauses, " AND ")+
		` ORDER BY created_at DESC, id DESC LIMIT ?`, arguments...)
	if err != nil {
		return nil, memory.StorageError("The memories could not be read.", err)
	}
	defer rows.Close()
	return scanMemoryItems(rows)
}

// SetSummarized marks memories that a summary now covers.
func (r *MemoryRepository) SetSummarized(ctx context.Context, ids []string, at time.Time) error {
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
				SET summarized = 1, updated_at = ?, revision = revision + 1 WHERE id = ?`,
				formatTime(at), id); err != nil {
				return memory.StorageError("The summarised memories could not be marked.", err)
			}
		}
		return nil
	})
}

// CreateSummaryWithSources stores a summary and its source links in one transaction.
//
// One transaction because a summary with no sources is a summary of nothing: AC-MEM-004's
// "Summary 关联源消息表" would be vacuously true for it, and the reader that reverses a
// summary to its messages would find nothing. The same transaction marks the sources
// summarised, so the window that produces the next summary cannot pick them up twice.
func (r *MemoryRepository) CreateSummaryWithSources(ctx context.Context, summary memory.MemoryItem, sources []memory.SummarySource, markSummarized bool) error {
	if err := summary.Validate(); err != nil {
		return err
	}
	if summary.Type != memory.TypeSummary {
		return memory.InvalidError("A summary must be of the summary type.")
	}
	if len(sources) == 0 {
		return memory.InvalidError("A summary must cite at least one memory it summarises.")
	}
	for _, source := range sources {
		if err := source.Validate(); err != nil {
			return err
		}
		if source.SummaryID != summary.ID {
			return memory.InvalidError("A summary source belongs to a different summary than the one being written.")
		}
	}
	return r.withinTx(ctx, func(repo *MemoryRepository) error {
		conn := repo.conn()
		if conn == nil {
			return memory.StorageError("The memory store is unavailable.", nil)
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO memory_items
			(id, scope_key, scope_tenant, scope_workspace, scope_project, scope_episode,
			 scope_agent_key, scope_session, memory_type, role, agent_key, content, importance,
			 confidence, embedding_blob, embedding_model, embedding_version, embedded_at,
			 summarized, summary_level, locked, source_type, source_id, deleted_at, created_at, updated_at, revision)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			summary.ID, summary.Scope.Key(), summary.Scope.Tenant, summary.Scope.Workspace,
			summary.Scope.Project, summary.Scope.Episode, summary.Scope.AgentKey,
			summary.Scope.Session, string(summary.Type), string(summary.Role), summary.AgentKey,
			summary.Content, summary.Importance, summary.Confidence,
			nullableBytes(summary.EmbeddingBlob), summary.EmbeddingModel, summary.EmbeddingVersion,
			formatTime(summary.EmbeddedAt), boolInt(summary.Summarized), summary.SummaryLevel,
			boolInt(summary.Locked),
			string(summary.SourceType), summary.SourceID, formatTime(summary.DeletedAt),
			formatTime(summary.CreatedAt), formatTime(summary.UpdatedAt), summary.Revision); err != nil {
			if isUniqueViolation(err) {
				return memory.ConflictError("That summary already exists.")
			}
			return memory.StorageError("The summary could not be saved.", err)
		}
		for _, source := range sources {
			if _, err := conn.ExecContext(ctx, `INSERT INTO memory_summary_sources
				(summary_id, source_memory_id, source_order, created_at) VALUES (?, ?, ?, ?)`,
				source.SummaryID, source.SourceMemoryID, source.SourceOrder,
				formatTime(source.CreatedAt)); err != nil {
				if isUniqueViolation(err) {
					return memory.ConflictError("A memory is listed twice as a source of the same summary.")
				}
				if isForeignKeyViolation(err) {
					return memory.InvalidError("A summary cites a memory that does not exist.")
				}
				return memory.StorageError("The summary sources could not be saved.", err)
			}
			if markSummarized {
				if _, err := conn.ExecContext(ctx, `UPDATE memory_items
					SET summarized = 1, updated_at = ?, revision = revision + 1 WHERE id = ?`,
					formatTime(summary.CreatedAt), source.SourceMemoryID); err != nil {
					return memory.StorageError("The summarised memories could not be marked.", err)
				}
			}
		}
		return nil
	})
}

// ListSummarySources returns a summary's sources in their stored order.
func (r *MemoryRepository) ListSummarySources(ctx context.Context, summaryID string) ([]memory.SummarySource, error) {
	conn := r.conn()
	if conn == nil {
		return nil, memory.StorageError("The memory store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, `SELECT summary_id, source_memory_id, source_order, created_at
		FROM memory_summary_sources WHERE summary_id = ? ORDER BY source_order`, summaryID)
	if err != nil {
		return nil, memory.StorageError("The summary sources could not be read.", err)
	}
	defer rows.Close()
	sources := []memory.SummarySource{}
	for rows.Next() {
		var source memory.SummarySource
		var createdAt string
		if err := rows.Scan(&source.SummaryID, &source.SourceMemoryID, &source.SourceOrder, &createdAt); err != nil {
			return nil, memory.StorageError("The summary sources could not be read.", err)
		}
		source.CreatedAt = parseTime(createdAt)
		sources = append(sources, source)
	}
	if err := rows.Err(); err != nil {
		return nil, memory.StorageError("The summary sources could not be read.", err)
	}
	return sources, nil
}

// SummariesOf returns the summaries that cite one memory, newest first.
//
// It is the read that makes AC-MEM-004's deletion policy a behaviour rather than a claim:
// a caller deleting a memory asks this to find what has to be invalidated.
func (r *MemoryRepository) SummariesOf(ctx context.Context, sourceMemoryID string) ([]memory.MemoryItem, error) {
	conn := r.conn()
	if conn == nil {
		return nil, memory.StorageError("The memory store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, memoryItemSelectColumns+
		` WHERE id IN (SELECT summary_id FROM memory_summary_sources WHERE source_memory_id = ?)
		  ORDER BY created_at DESC, id DESC`, sourceMemoryID)
	if err != nil {
		return nil, memory.StorageError("The summaries could not be read.", err)
	}
	defer rows.Close()
	return scanMemoryItems(rows)
}

// UnsummarisedItemsForLevel returns the window a summary at one rung may cover, oldest first.
//
// # The three predicates, and why each is load-bearing
//
//   - `summarized = 0`, which is what migration 000019's column is for: a window that selected
//     by position would re-summarise the same turns every time it ran, and a window that
//     selected by time would skip a burst of activity it happened to miss.
//   - `memory_type = ?`, derived from the RUNG: a message-rung window is EPISODIC rows and
//     every window above it is SUMMARY rows. The first version of this read took the type from
//     the caller and read every type for a level-one window, so a summary found its own row
//     among its sources and condensed itself. The type is therefore not the caller's to choose.
//   - `summary_level = ?`, the rung BELOW, which is WP-18's addition. Before it, "uncondensed
//     summaries in this scope" identified no rung: with three rungs a project window would
//     accept an episode window's summary, and once two such rows were uncondensed it would
//     condense them together — a summary of a summary of a summary, which is the same self-
//     condensing defect one rung up. The rung is a column now, so the window names it.
//
// The scope is the rung's own, which the caller widens: a message rung stays in its
// conversation, an episode rung drops the agent and session, a project rung drops the episode
// too. `scopeClauses` skips the parts a scope leaves empty, so widening is what makes a
// project-level window match project-level rows.
func (r *MemoryRepository) UnsummarisedItemsForLevel(ctx context.Context, scope memory.Scope, level int, limit int) ([]memory.MemoryItem, error) {
	conn := r.conn()
	if conn == nil {
		return nil, memory.StorageError("The memory store is unavailable.", nil)
	}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	if !memory.IsValidSummaryLevel(level) || level == 0 {
		return nil, memory.InvalidError("The summary level is not recognised.")
	}
	if limit <= 0 {
		limit = appmemory.MaxSummaryWindow
	}
	// The rung below is what a rung covers. Rung one covers MESSAGES, so its window is the
	// episodic rows and there is no rung below it to name.
	clauses, arguments := scopeClauses(scope)
	if level == memory.SummaryLevelMessage {
		clauses = append(clauses, "memory_type = ?", "summarized = 0", "deleted_at = ''")
		arguments = append(arguments, string(memory.TypeEpisodic), limit)
	} else {
		clauses = append(clauses, "memory_type = ?", "summary_level = ?", "summarized = 0", "deleted_at = ''")
		arguments = append(arguments, string(memory.TypeSummary), level-1, limit)
	}
	rows, err := conn.QueryContext(ctx, memoryItemSelectColumns+
		` WHERE `+strings.Join(clauses, " AND ")+` ORDER BY created_at ASC, id ASC LIMIT ?`, arguments...)
	if err != nil {
		return nil, memory.StorageError("The memories could not be read.", err)
	}
	defer rows.Close()
	return scanMemoryItems(rows)
}

// AddEntityLinks records entity links in one transaction.
func (r *MemoryRepository) AddEntityLinks(ctx context.Context, links []memory.EntityLink) error {
	if len(links) == 0 {
		return nil
	}
	for _, link := range links {
		if err := link.Validate(); err != nil {
			return err
		}
	}
	return r.withinTx(ctx, func(repo *MemoryRepository) error {
		conn := repo.conn()
		if conn == nil {
			return memory.StorageError("The memory store is unavailable.", nil)
		}
		for _, link := range links {
			// INSERT OR IGNORE rather than a plain INSERT: the primary key is the whole
			// tuple, so recording the same relation twice is a no-op in meaning. Refusing
			// it would make a summarise run that overlapped a previous one fail on a fact
			// it had already recorded.
			if _, err := conn.ExecContext(ctx, `INSERT INTO memory_entity_links
				(memory_id, entity_type, entity_id, relation_type, created_at)
				VALUES (?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
				link.MemoryID, link.EntityType, link.EntityID, link.RelationType,
				formatTime(link.CreatedAt)); err != nil {
				if isForeignKeyViolation(err) {
					return memory.InvalidError("An entity link cites a memory that does not exist.")
				}
				return memory.StorageError("The entity links could not be saved.", err)
			}
		}
		return nil
	})
}

// ListEntityLinks returns a memory's links in a stable order.
func (r *MemoryRepository) ListEntityLinks(ctx context.Context, memoryID string) ([]memory.EntityLink, error) {
	conn := r.conn()
	if conn == nil {
		return nil, memory.StorageError("The memory store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, `SELECT memory_id, entity_type, entity_id, relation_type, created_at
		FROM memory_entity_links WHERE memory_id = ? ORDER BY entity_type, entity_id, relation_type`, memoryID)
	if err != nil {
		return nil, memory.StorageError("The entity links could not be read.", err)
	}
	defer rows.Close()
	links := []memory.EntityLink{}
	for rows.Next() {
		var link memory.EntityLink
		var createdAt string
		if err := rows.Scan(&link.MemoryID, &link.EntityType, &link.EntityID, &link.RelationType, &createdAt); err != nil {
			return nil, memory.StorageError("The entity links could not be read.", err)
		}
		link.CreatedAt = parseTime(createdAt)
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		return nil, memory.StorageError("The entity links could not be read.", err)
	}
	return links, nil
}

// scopeClauses builds the WHERE fragments and arguments for an exact scope match.
//
// The project is always present and never optional; the other five match only when the
// caller named them, and a blank part matches the blank part rather than everything. That
// is why this returns clauses rather than a single expression: the caller has to see that
// it appends its own predicates to a scope that is already project-bound.
func scopeClauses(scope memory.Scope) ([]string, []any) {
	clauses := []string{"scope_project = ?"}
	arguments := []any{scope.Project}
	optional := []struct {
		column string
		value  string
	}{
		{"scope_tenant", scope.Tenant},
		{"scope_workspace", scope.Workspace},
		{"scope_episode", scope.Episode},
		{"scope_agent_key", scope.AgentKey},
		{"scope_session", scope.Session},
	}
	for _, part := range optional {
		if part.value == "" {
			continue
		}
		clauses = append(clauses, part.column+" = ?")
		arguments = append(arguments, part.value)
	}
	return clauses, arguments
}

// scanMemoryItem reads one item row.
func scanMemoryItem(row rowScanner) (memory.MemoryItem, error) {
	var item memory.MemoryItem
	var scopeKey, memoryType, role, sourceType string
	var embeddingBlob []byte
	var summarized, locked int
	var embeddedAt, deletedAt, createdAt, updatedAt string
	err := row.Scan(&item.ID, &scopeKey, &item.Scope.Tenant, &item.Scope.Workspace,
		&item.Scope.Project, &item.Scope.Episode, &item.Scope.AgentKey, &item.Scope.Session,
		&memoryType, &role, &item.AgentKey, &item.Content, &item.Importance, &item.Confidence,
		&embeddingBlob, &item.EmbeddingModel, &item.EmbeddingVersion, &embeddedAt,
		&summarized, &item.SummaryLevel, &locked, &sourceType, &item.SourceID, &deletedAt,
		&createdAt, &updatedAt, &item.Revision)
	if err != nil {
		return memory.MemoryItem{}, err
	}
	item.Type = memory.MemoryType(memoryType)
	item.Role = agent.MessageRole(role)
	item.SourceType = memory.SourceType(sourceType)
	item.EmbeddingBlob = embeddingBlob
	item.Summarized = summarized != 0
	item.Locked = locked != 0
	item.EmbeddedAt = parseTime(embeddedAt)
	item.DeletedAt = parseTime(deletedAt)
	item.CreatedAt = parseTime(createdAt)
	item.UpdatedAt = parseTime(updatedAt)
	return item, nil
}

// scanMemoryItems reads a result set of item rows.
func scanMemoryItems(rows *sql.Rows) ([]memory.MemoryItem, error) {
	items := []memory.MemoryItem{}
	for rows.Next() {
		item, err := scanMemoryItem(rows)
		if err != nil {
			return nil, memory.StorageError("The memories could not be read.", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, memory.StorageError("The memories could not be read.", err)
	}
	return items, nil
}

// nullableBytes maps an empty blob to NULL.
//
// The two are the same fact — no embedding — and storing an empty slice would make
// `embedding_blob IS NOT NULL` true for a row with no vector, so the index query that uses
// it as a filter would return rows it cannot score.
func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

// The compile-time proof that the store satisfies the port.
//
// It is not decoration. Without it a signature drift is INVISIBLE: the service takes an
// interface, so a repository that no longer satisfies it compiles everywhere and simply leaves
// `Items` nil at composition, which reads at runtime as "no memory store is configured". That
// is what happened before these two lines existed, and a build with no memory commands at all
// looked exactly like a build whose memory was empty.
var (
	_ appmemory.Repository  = (*MemoryRepository)(nil)
	_ appmemory.VectorIndex = (*MemoryVectorIndex)(nil)
)
