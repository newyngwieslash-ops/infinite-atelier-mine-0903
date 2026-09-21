package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	stalenessapp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/staleness"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/staleness"
)

// StalenessRepository is the SQLite implementation of the staleness mark port.
//
// Column names come from migration 000012. The table has no id column and no
// revision-guarded update by id, because its primary key is the artifact itself:
// (artifact_type, artifact_id). Recording a mark is therefore an UPSERT — a
// second mark for the same artifact updates the row it collided with rather
// than adding a second one — and the row's revision is incremented in the
// DO UPDATE clause. Two open marks for one artifact would leave every reader
// choosing which severity applies, which is what the key exists to prevent.
type StalenessRepository struct {
	db *sql.DB
	tx *sql.Tx
}

// NewStalenessRepository builds the repository over a database handle.
func NewStalenessRepository(db *sql.DB) *StalenessRepository {
	return &StalenessRepository{db: db}
}

// WithinTx returns a repository bound to one transaction.
func (r *StalenessRepository) WithinTx(tx *sql.Tx) *StalenessRepository {
	return &StalenessRepository{db: r.db, tx: tx}
}

func (r *StalenessRepository) conn() querier {
	if r == nil {
		return nil
	}
	return connection(r.db, r.tx)
}

const stalenessSelectColumns = `SELECT artifact_type, artifact_id, project_id, severity, reason,
	upstream_type, upstream_id, waived, waived_by_decision_id, waived_reason, cleared_at,
	created_at, updated_at, revision FROM artifact_staleness`

// upsertMarkSQL writes one mark, keyed by the artifact.
//
// created_at is not rewritten on the update branch: the row's first sighting is
// a fact worth keeping, so a reader can see when the artifact first went stale
// and when it was last re-marked. revision moves with every write, so a reader
// holding an older revision can tell the mark changed.
const upsertMarkSQL = `INSERT INTO artifact_staleness
	(artifact_type, artifact_id, project_id, severity, reason, upstream_type, upstream_id,
	 waived, waived_by_decision_id, waived_reason, cleared_at, created_at, updated_at, revision)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
	ON CONFLICT(artifact_type, artifact_id) DO UPDATE SET
		project_id = excluded.project_id,
		severity = excluded.severity,
		reason = excluded.reason,
		upstream_type = excluded.upstream_type,
		upstream_id = excluded.upstream_id,
		waived = excluded.waived,
		waived_by_decision_id = excluded.waived_by_decision_id,
		waived_reason = excluded.waived_reason,
		cleared_at = excluded.cleared_at,
		updated_at = excluded.updated_at,
		revision = artifact_staleness.revision + 1`

// UpsertMark stores one mark, replacing any existing mark for the same
// artifact.
func (r *StalenessRepository) UpsertMark(ctx context.Context, mark staleness.Mark, updatedAt time.Time) error {
	conn := r.conn()
	if conn == nil {
		return storageError("STALENESS_STORE_UNAVAILABLE", "The staleness store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, upsertMarkSQL,
		string(mark.ArtifactType), mark.ArtifactID, mark.ProjectID, string(mark.Severity),
		mark.Reason, string(mark.UpstreamType), mark.UpstreamID, boolInt(mark.Waived),
		mark.WaivedByDecisionID, mark.WaivedReason, mark.ClearedAt, formatTime(updatedAt),
		formatTime(updatedAt))
	if err != nil {
		if isForeignKeyViolation(err) {
			return staleness.InvalidError("That project does not exist.")
		}
		return storageError("STALENESS_WRITE_FAILED", "The stale mark could not be saved.", err)
	}
	return nil
}

// UpsertMarks stores several marks in one transaction, so a propagation either
// records every dependent it found or none of them.
func (r *StalenessRepository) UpsertMarks(ctx context.Context, marks []staleness.Mark, updatedAt time.Time) error {
	if len(marks) == 0 {
		return nil
	}
	if r == nil || r.db == nil {
		return storageError("STALENESS_STORE_UNAVAILABLE", "The staleness store is unavailable.", nil)
	}
	// A repository already bound to a caller's transaction writes through it:
	// SQLite has one writer, and opening a second transaction here would both
	// deadlock and break the caller's atomicity.
	if r.tx != nil {
		return r.upsertMarksOn(ctx, r, marks, updatedAt)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return storageError("STALENESS_TX_FAILED", "The propagation could not be started.", err)
	}
	committed := false
	defer func() {
		if !committed {
			// A rollback after a successful commit is a no-op, so this is safe
			// on every path.
			_ = tx.Rollback()
		}
	}()
	if err := r.upsertMarksOn(ctx, r.WithinTx(tx), marks, updatedAt); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return storageError("STALENESS_TX_FAILED", "The propagation could not be saved.", err)
	}
	committed = true
	return nil
}

// upsertMarksOn writes every mark through one bound repository.
func (r *StalenessRepository) upsertMarksOn(ctx context.Context, repo *StalenessRepository, marks []staleness.Mark, updatedAt time.Time) error {
	for _, mark := range marks {
		if err := repo.UpsertMark(ctx, mark, updatedAt); err != nil {
			return err
		}
	}
	return nil
}

// GetMark returns one artifact's mark.
func (r *StalenessRepository) GetMark(ctx context.Context, artifactType staleness.ArtifactType, artifactID string) (staleness.Mark, bool, error) {
	conn := r.conn()
	if conn == nil {
		return staleness.Mark{}, false, storageError("STALENESS_STORE_UNAVAILABLE", "The staleness store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, stalenessSelectColumns+
		` WHERE artifact_type = ? AND artifact_id = ?`, string(artifactType), artifactID)
	mark, err := scanMark(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return staleness.Mark{}, false, nil
		}
		return staleness.Mark{}, false, storageError("STALENESS_READ_FAILED", "The stale mark could not be read.", err)
	}
	return mark, true, nil
}

// ListMarks returns a project's marks, cleared and open alike.
func (r *StalenessRepository) ListMarks(ctx context.Context, projectID string) ([]staleness.Mark, error) {
	return r.list(ctx, stalenessSelectColumns+
		` WHERE project_id = ? ORDER BY artifact_type ASC, artifact_id ASC`, projectID)
}

// ListOpenMarks returns a project's marks that are not cleared.
func (r *StalenessRepository) ListOpenMarks(ctx context.Context, projectID string) ([]staleness.Mark, error) {
	return r.list(ctx, stalenessSelectColumns+
		` WHERE project_id = ? AND cleared_at = '' ORDER BY artifact_type ASC, artifact_id ASC`, projectID)
}

// ClearMark stamps cleared_at on one artifact's mark.
//
// The row is not deleted: migration 000012 keeps it "so the history of what was
// invalidated survives", and a cleared mark is still the record that the
// artifact once went stale.
func (r *StalenessRepository) ClearMark(ctx context.Context, artifactType staleness.ArtifactType, artifactID string, clearedAt time.Time) (bool, error) {
	return r.touch(ctx, `UPDATE artifact_staleness
		SET cleared_at = ?, updated_at = ?, revision = revision + 1
		WHERE artifact_type = ? AND artifact_id = ?`,
		formatTime(clearedAt), formatTime(clearedAt), string(artifactType), artifactID)
}

// WaiveMark records a waiver on one artifact's mark.
func (r *StalenessRepository) WaiveMark(ctx context.Context, artifactType staleness.ArtifactType, artifactID, decisionID, reason string, updatedAt time.Time) (bool, error) {
	return r.touch(ctx, `UPDATE artifact_staleness
		SET waived = 1, waived_by_decision_id = ?, waived_reason = ?, updated_at = ?,
		    revision = revision + 1
		WHERE artifact_type = ? AND artifact_id = ?`,
		decisionID, reason, formatTime(updatedAt), string(artifactType), artifactID)
}

// touch runs a single-row update and reports whether a row matched.
func (r *StalenessRepository) touch(ctx context.Context, query string, args ...any) (bool, error) {
	conn := r.conn()
	if conn == nil {
		return false, storageError("STALENESS_STORE_UNAVAILABLE", "The staleness store is unavailable.", nil)
	}
	result, err := conn.ExecContext(ctx, query, args...)
	if err != nil {
		return false, storageError("STALENESS_WRITE_FAILED", "The stale mark could not be updated.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, storageError("STALENESS_WRITE_FAILED", "The stale mark could not be updated.", err)
	}
	return affected > 0, nil
}

// list runs a mark query and reads every row.
func (r *StalenessRepository) list(ctx context.Context, query string, args ...any) ([]staleness.Mark, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("STALENESS_STORE_UNAVAILABLE", "The staleness store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storageError("STALENESS_READ_FAILED", "The stale marks could not be read.", err)
	}
	defer rows.Close()
	var marks []staleness.Mark
	for rows.Next() {
		mark, scanErr := scanMark(rows)
		if scanErr != nil {
			return nil, storageError("STALENESS_READ_FAILED", "The stale marks could not be read.", scanErr)
		}
		marks = append(marks, mark)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("STALENESS_READ_FAILED", "The stale marks could not be read.", err)
	}
	return marks, nil
}

// scanMark reads one artifact_staleness row.
func scanMark(row rowScanner) (staleness.Mark, error) {
	var mark staleness.Mark
	var artifactType, severity, upstreamType string
	var waived int
	var clearedAt, createdAt, updatedAt string
	var revision int64
	if err := row.Scan(&artifactType, &mark.ArtifactID, &mark.ProjectID, &severity, &mark.Reason,
		&upstreamType, &mark.UpstreamID, &waived, &mark.WaivedByDecisionID, &mark.WaivedReason,
		&clearedAt, &createdAt, &updatedAt, &revision); err != nil {
		return staleness.Mark{}, err
	}
	mark.ArtifactType = staleness.ArtifactType(artifactType)
	mark.Severity = staleness.Severity(severity)
	mark.UpstreamType = staleness.ArtifactType(upstreamType)
	mark.Waived = waived == 1
	mark.ClearedAt = clearedAt
	return mark, nil
}

// DependentFinder is the SQLite implementation of the propagation lookup: it
// answers "which rows of type T reference upstream row U" by reading the column
// that holds the reference.
//
// This is the one place the mapping from an upstream artifact type to its
// referencing columns lives. Every pair below is a column that migrations
// 000006 through 000012 actually declare, so the table is checkable rather than
// aspirational. A pair with no entry returns an empty slice, which the
// application reads as "nothing references this" rather than as a failure.
type DependentFinder struct {
	db *sql.DB
	tx *sql.Tx
}

// NewDependentFinder builds the finder over a database handle.
func NewDependentFinder(db *sql.DB) *DependentFinder {
	return &DependentFinder{db: db}
}

// WithinTx returns a finder bound to one transaction.
func (r *DependentFinder) WithinTx(tx *sql.Tx) *DependentFinder {
	return &DependentFinder{db: r.db, tx: tx}
}

func (r *DependentFinder) conn() querier {
	if r == nil {
		return nil
	}
	return connection(r.db, r.tx)
}

// dependentQuery is the set of columns that reference an upstream artifact for
// one (dependent, upstream) pair.
//
// It is a slice because one pair can be reached through more than one column:
// a scene references a story event directly, and also owns the dialogue lines
// that reference it, so both queries answer the same question and their results
// are deduplicated by the applicaton's own seen-set.
type dependentQuery struct {
	dependent staleness.ArtifactType
	upstream  staleness.ArtifactType
	queries   []string
}

// dependentQueries is the schema's reference table for the artifact families
// this package is responsible for.
//
// Deliberate omissions, so that a missing entry is a documented fact rather
// than an oversight:
//
//   - source_document_version is referenced by chapters only. story_entities
//     carries no document column: the evidence for an extracted fact is a
//     story_fact_sources row, whose fact_id is polymorphic and therefore not a
//     foreign key this finder can walk.
//   - chapter is referenced by story_events.chapter_id. story_entity_aliases
//     has source_chapter_id, but an alias is a name variant rather than a
//     first-class chain node (the chain has no story_entity_alias), so a change
//     to a chapter does not re-review an alias on its own.
//   - story_relation is referenced by nothing: story_relations stores
//     polymorphic source_entity_id / target_entity_id pairs with
//     source_entity_type / target_entity_type tags (migration 000007), so there
//     is no column whose meaning is "this relation's entity". The domain's
//     chain still lists story_relation as a direct dependent of story_entity,
//     and the propagator will find nothing for it here.
//   - character_state is reached through character_states.character_entity_id.
//   - asset_version is reached through assets.story_entity_id, and only the
//     asset's current approved version is returned: that is the version in use,
//     which is the one a picture of a changed character invalidates. Historical
//     versions stay as they were.
//   - dialogue_lines has a source_story_event_id column, but the staleness
//     chain has no dialogue_line node (migration 000012's CHECK is the closed
//     list), so a mark cannot name one. The column is still read: the line's
//     scene is the markable artifact, so the query below returns the scene that
//     owns a line naming the changed event, alongside the ones that reference it
//     directly.
//   - the storyboard family is covered end to end for the columns the schema
//     declares: script_version to director_plan_versions and to
//     storyboard_versions, director_plan_version to storyboard_versions,
//     storyboard_version to storyboard_items, storyboard_item to
//     storyboard_panel_versions, and shot to storyboard_items.
var dependentQueries = []dependentQuery{
	{
		dependent: staleness.ArtifactChapter,
		upstream:  staleness.ArtifactSourceDocumentVersion,
		queries: []string{
			`SELECT id FROM chapters WHERE source_document_version_id = ?`,
		},
	},
	{
		dependent: staleness.ArtifactStoryEvent,
		upstream:  staleness.ArtifactChapter,
		queries: []string{
			`SELECT id FROM story_events WHERE chapter_id = ?`,
		},
	},
	{
		// A scene references the script version it belongs to.
		dependent: staleness.ArtifactScene,
		upstream:  staleness.ArtifactScriptVersion,
		queries: []string{
			`SELECT id FROM scenes WHERE script_version_id = ?`,
		},
	},
	{
		// A scene dramatises a story event directly, and owns the dialogue lines
		// that name the same event. Both columns are read, and a scene found by
		// both is returned once: FindDependents deduplicates its results.
		dependent: staleness.ArtifactScene,
		upstream:  staleness.ArtifactStoryEvent,
		queries: []string{
			`SELECT id FROM scenes WHERE source_story_event_id = ?`,
			`SELECT s.id FROM scenes s
			 JOIN dialogue_lines l ON l.scene_id = s.id
			 WHERE l.source_story_event_id = ?`,
		},
	},
	{
		dependent: staleness.ArtifactStorySkeleton,
		upstream:  staleness.ArtifactStoryEvent,
		queries: []string{
			`SELECT skeleton_version_id FROM story_skeleton_event_links WHERE story_event_id = ?`,
		},
	},
	{
		dependent: staleness.ArtifactAdaptationStrategy,
		upstream:  staleness.ArtifactStoryEvent,
		queries: []string{
			`SELECT strategy_version_id FROM adaptation_strategy_event_links WHERE story_event_id = ?`,
		},
	},
	{
		dependent: staleness.ArtifactAssetVersion,
		upstream:  staleness.ArtifactStoryEntity,
		queries: []string{
			`SELECT current_approved_version_id FROM assets
			 WHERE story_entity_id = ? AND current_approved_version_id != ''`,
		},
	},
	{
		dependent: staleness.ArtifactStoryEvent,
		upstream:  staleness.ArtifactStoryEntity,
		queries: []string{
			`SELECT story_event_id FROM story_event_participants WHERE story_entity_id = ?`,
		},
	},
	{
		dependent: staleness.ArtifactCharacterState,
		upstream:  staleness.ArtifactStoryEntity,
		queries: []string{
			`SELECT id FROM character_states WHERE character_entity_id = ?`,
		},
	},
	{
		dependent: staleness.ArtifactShot,
		upstream:  staleness.ArtifactScene,
		queries: []string{
			`SELECT id FROM shots WHERE scene_id = ?`,
		},
	},
	{
		dependent: staleness.ArtifactStoryboardItem,
		upstream:  staleness.ArtifactShot,
		queries: []string{
			`SELECT id FROM storyboard_items WHERE shot_id = ?`,
		},
	},
	{
		dependent: staleness.ArtifactStoryboardPanel,
		upstream:  staleness.ArtifactStoryboardItem,
		queries: []string{
			`SELECT id FROM storyboard_panel_versions WHERE storyboard_item_id = ?`,
		},
	},
	{
		// The image a panel approved is an asset version (§9.5), so approving a new one
		// is a change the panels holding the old one must be told about. This is the
		// query behind PRD FR-050's "替换批准版本时，系统列出受影响的分镜和镜头".
		//
		// Only the panels whose APPROVED image it is: a panel that merely HAS the version
		// among its candidates is unaffected by which one is in force, which is the
		// distinction `required`-vs-optional makes one level up.
		dependent: staleness.ArtifactStoryboardPanel,
		upstream:  staleness.ArtifactAssetVersion,
		queries: []string{
			`SELECT id FROM storyboard_panel_versions WHERE approved_image_asset_version_id = ?`,
		},
	},
	{
		dependent: staleness.ArtifactDirectorPlan,
		upstream:  staleness.ArtifactScriptVersion,
		queries: []string{
			`SELECT id FROM director_plan_versions WHERE script_version_id = ?`,
		},
	},
	{
		dependent: staleness.ArtifactStoryboardVersion,
		upstream:  staleness.ArtifactScriptVersion,
		queries: []string{
			`SELECT id FROM storyboard_versions WHERE script_version_id = ?`,
		},
	},
	{
		dependent: staleness.ArtifactStoryboardVersion,
		upstream:  staleness.ArtifactDirectorPlan,
		queries: []string{
			`SELECT id FROM storyboard_versions WHERE director_plan_version_id = ?`,
		},
	},
	{
		dependent: staleness.ArtifactStoryboardItem,
		upstream:  staleness.ArtifactStoryboardVersion,
		queries: []string{
			`SELECT id FROM storyboard_items WHERE storyboard_version_id = ?`,
		},
	},
	{
		dependent: staleness.ArtifactStageRun,
		upstream:  staleness.ArtifactWorkflowRun,
		queries: []string{
			`SELECT id FROM stage_runs WHERE workflow_run_id = ?`,
		},
	},
}

// FindDependents returns the identifiers of upstreamID's referencing rows of
// artifactType.
//
// The artifactType argument is the dependent family the caller is asking about,
// which is why the same upstream can be looked up more than once with different
// answers (a chapter change looks up story_events, a script change looks up
// scenes). One pair may be answered by several queries; their results are
// deduplicated here, so a row reached through two columns is returned once. A
// pair with no entry returns an empty slice, not an error.
func (r *DependentFinder) FindDependents(ctx context.Context, artifactType, upstreamType staleness.ArtifactType, upstreamID string) ([]stalenessapp.DependentRef, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("STALENESS_STORE_UNAVAILABLE", "The staleness store is unavailable.", nil)
	}
	for _, candidate := range dependentQueries {
		if candidate.dependent != artifactType || candidate.upstream != upstreamType {
			continue
		}
		refs := make([]stalenessapp.DependentRef, 0)
		seen := make(map[string]bool)
		for _, query := range candidate.queries {
			found, err := r.collectRefs(ctx, conn, artifactType, query, upstreamID)
			if err != nil {
				return nil, err
			}
			for _, ref := range found {
				if seen[ref.ArtifactID] {
					continue
				}
				seen[ref.ArtifactID] = true
				refs = append(refs, ref)
			}
		}
		return refs, nil
	}
	return []stalenessapp.DependentRef{}, nil
}

// collectRefs runs one reference query and turns its rows into refs.
func (r *DependentFinder) collectRefs(ctx context.Context, conn querier, artifactType staleness.ArtifactType, query, upstreamID string) ([]stalenessapp.DependentRef, error) {
	rows, err := conn.QueryContext(ctx, query, upstreamID)
	if err != nil {
		return nil, storageError("STALENESS_READ_FAILED", "The dependent artifacts could not be read.", err)
	}
	defer rows.Close()
	refs := make([]stalenessapp.DependentRef, 0)
	for rows.Next() {
		var id string
		if scanErr := rows.Scan(&id); scanErr != nil {
			return nil, storageError("STALENESS_READ_FAILED", "The dependent artifacts could not be read.", scanErr)
		}
		refs = append(refs, stalenessapp.DependentRef{ArtifactType: artifactType, ArtifactID: id})
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("STALENESS_READ_FAILED", "The dependent artifacts could not be read.", err)
	}
	return refs, nil
}

// ProjectResolver answers which project an artifact instance belongs to, by
// walking the foreign keys from the row to its project.
//
// It exists because a stale mark names a project (migration 000012 makes
// project_id a foreign key with ON DELETE CASCADE) and is read back per
// project, so a mark filed under the wrong project would be invisible where it
// belongs. A type with no query here answers "not found" rather than an error,
// and so does a row that no longer exists, which the propagation reads as "skip
// this row": propagation is often triggered by exactly the deletion that
// removed the row.
//
// The table covers every artifact family dependentQueries can return, so a mark
// for a direct dependent is always filed under the project the caller named. A
// type the finder cannot return is not resolvable and never reaches this call.
type ProjectResolver struct {
	db *sql.DB
	tx *sql.Tx
}

// NewProjectResolver builds the resolver over a database handle.
func NewProjectResolver(db *sql.DB) *ProjectResolver {
	return &ProjectResolver{db: db}
}

// WithinTx returns a resolver bound to one transaction.
func (r *ProjectResolver) WithinTx(tx *sql.Tx) *ProjectResolver {
	return &ProjectResolver{db: r.db, tx: tx}
}

func (r *ProjectResolver) conn() querier {
	if r == nil {
		return nil
	}
	return connection(r.db, r.tx)
}

// projectQueries maps an artifact type to a query returning the id of the
// project that owns one instance of it. Each query takes the artifact id.
//
// Every join below follows a foreign key the migrations declare, so a row that
// resolves is one the schema can actually reach from projects.
var projectQueries = map[staleness.ArtifactType]string{
	staleness.ArtifactChapter: `SELECT d.project_id FROM chapters c
		JOIN source_document_versions v ON v.id = c.source_document_version_id
		JOIN source_documents d ON d.id = v.source_document_id
		WHERE c.id = ?`,
	staleness.ArtifactStoryEntity: `SELECT project_id FROM story_entities WHERE id = ?`,
	staleness.ArtifactStoryEvent:  `SELECT project_id FROM story_events WHERE id = ?`,
	staleness.ArtifactCharacterState: `SELECT e.project_id FROM character_states s
		JOIN story_entities e ON e.id = s.character_entity_id
		WHERE s.id = ?`,
	staleness.ArtifactStorySkeleton: `SELECT e.project_id FROM story_skeleton_versions s
		JOIN episodes e ON e.id = s.episode_id
		WHERE s.id = ?`,
	staleness.ArtifactAdaptationStrategy: `SELECT e.project_id FROM adaptation_strategy_versions s
		JOIN episodes e ON e.id = s.episode_id
		WHERE s.id = ?`,
	staleness.ArtifactScriptVersion: `SELECT e.project_id FROM script_versions v
		JOIN scripts s ON s.id = v.script_id
		JOIN episodes e ON e.id = s.episode_id
		WHERE v.id = ?`,
	staleness.ArtifactScene: `SELECT e.project_id FROM scenes c
		JOIN script_versions v ON v.id = c.script_version_id
		JOIN scripts s ON s.id = v.script_id
		JOIN episodes e ON e.id = s.episode_id
		WHERE c.id = ?`,
	staleness.ArtifactShot: `SELECT e.project_id FROM shots sh
		JOIN scenes c ON c.id = sh.scene_id
		JOIN script_versions v ON v.id = c.script_version_id
		JOIN scripts s ON s.id = v.script_id
		JOIN episodes e ON e.id = s.episode_id
		WHERE sh.id = ?`,
	staleness.ArtifactAssetVersion: `SELECT a.project_id FROM asset_versions v
		JOIN assets a ON a.id = v.asset_id
		WHERE v.id = ?`,
	staleness.ArtifactDirectorPlan: `SELECT e.project_id FROM director_plan_versions p
		JOIN episodes e ON e.id = p.episode_id
		WHERE p.id = ?`,
	staleness.ArtifactStoryboardVersion: `SELECT e.project_id FROM storyboard_versions v
		JOIN storyboards b ON b.id = v.storyboard_id
		JOIN episodes e ON e.id = b.episode_id
		WHERE v.id = ?`,
	staleness.ArtifactStoryboardItem: `SELECT e.project_id FROM storyboard_items i
		JOIN storyboard_versions v ON v.id = i.storyboard_version_id
		JOIN storyboards b ON b.id = v.storyboard_id
		JOIN episodes e ON e.id = b.episode_id
		WHERE i.id = ?`,
	staleness.ArtifactStoryboardPanel: `SELECT e.project_id FROM storyboard_panel_versions p
		JOIN storyboard_items i ON i.id = p.storyboard_item_id
		JOIN storyboard_versions v ON v.id = i.storyboard_version_id
		JOIN storyboards b ON b.id = v.storyboard_id
		JOIN episodes e ON e.id = b.episode_id
		WHERE p.id = ?`,
	staleness.ArtifactWorkflowRun: `SELECT project_id FROM workflow_runs WHERE id = ?`,
	staleness.ArtifactStageRun: `SELECT r.project_id FROM stage_runs s
		JOIN workflow_runs r ON r.id = s.workflow_run_id
		WHERE s.id = ?`,
}

// ProjectFor returns the project an artifact instance belongs to.
func (r *ProjectResolver) ProjectFor(ctx context.Context, artifactType staleness.ArtifactType, artifactID string) (string, bool, error) {
	conn := r.conn()
	if conn == nil {
		return "", false, storageError("STALENESS_STORE_UNAVAILABLE", "The staleness store is unavailable.", nil)
	}
	query, ok := projectQueries[artifactType]
	if !ok {
		return "", false, nil
	}
	var projectID string
	err := conn.QueryRowContext(ctx, query, artifactID).Scan(&projectID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, storageError("STALENESS_READ_FAILED", "The owning project could not be read.", err)
	}
	if projectID == "" {
		return "", false, fmt.Errorf("record %s/%s has no project", artifactType, artifactID)
	}
	return projectID, true, nil
}

// Ensure the implementations satisfy the application ports.
var (
	_ stalenessapp.MarkRepository        = (*StalenessRepository)(nil)
	_ stalenessapp.DependentFinder       = (*DependentFinder)(nil)
	_ stalenessapp.ProjectEntityResolver = (*ProjectResolver)(nil)
)
