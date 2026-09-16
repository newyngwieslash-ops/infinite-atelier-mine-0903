package database

import (
	"context"
	"database/sql"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/projects"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/project"
)

// ProjectRepository is the SQLite implementation of the project port. It owns
// every statement touching workspaces and projects; SQL never leaves this
// package (ARCHITECTURE §21).
type ProjectRepository struct {
	db *sql.DB
	tx *sql.Tx
}

// NewProjectRepository builds the repository over a database handle.
func NewProjectRepository(db *sql.DB) *ProjectRepository {
	return &ProjectRepository{db: db}
}

// WithinTx returns a repository bound to one transaction. The legacy import
// uses this so a project and everything under it commit together.
func (r *ProjectRepository) WithinTx(tx *sql.Tx) *ProjectRepository {
	return &ProjectRepository{db: r.db, tx: tx}
}

func (r *ProjectRepository) conn() querier {
	if r == nil {
		return nil
	}
	return connection(r.db, r.tx)
}

// CreateWorkspace stores a workspace.
func (r *ProjectRepository) CreateWorkspace(ctx context.Context, workspace project.Workspace) error {
	conn := r.conn()
	if conn == nil {
		return storageError("PROJECT_STORE_UNAVAILABLE", "The project store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO workspaces (id, name, kind, created_at, updated_at, revision)
		VALUES (?, ?, ?, ?, ?, ?)`,
		workspace.ID, workspace.Name, string(workspace.Kind),
		formatTime(workspace.CreatedAt), formatTime(workspace.UpdatedAt), workspace.Revision)
	if err != nil {
		return storageError("PROJECT_WRITE_FAILED", "The workspace could not be saved.", err)
	}
	return nil
}

// GetWorkspace returns one workspace.
func (r *ProjectRepository) GetWorkspace(ctx context.Context, id string) (project.Workspace, error) {
	conn := r.conn()
	if conn == nil {
		return project.Workspace{}, storageError("PROJECT_STORE_UNAVAILABLE", "The project store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, `SELECT id, name, kind, created_at, updated_at, revision FROM workspaces WHERE id = ?`, id)
	var workspace project.Workspace
	var kind, createdAt, updatedAt string
	if err := row.Scan(&workspace.ID, &workspace.Name, &kind, &createdAt, &updatedAt, &workspace.Revision); err != nil {
		if err == sql.ErrNoRows {
			return project.Workspace{}, project.NotFoundError()
		}
		return project.Workspace{}, storageError("PROJECT_READ_FAILED", "The workspace could not be read.", err)
	}
	workspace.Kind = project.WorkspaceKind(kind)
	workspace.CreatedAt = parseTime(createdAt)
	workspace.UpdatedAt = parseTime(updatedAt)
	return workspace, nil
}

const projectSelectColumns = `SELECT id, workspace_id, project_type, name, description, language, status,
	cover_asset_version_id, deleted_at, deleted_by, created_at, updated_at, revision
	FROM projects`

// CreateProject stores a new project. A duplicate id is a conflict.
func (r *ProjectRepository) CreateProject(ctx context.Context, record project.Project) error {
	conn := r.conn()
	if conn == nil {
		return storageError("PROJECT_STORE_UNAVAILABLE", "The project store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO projects
		(id, workspace_id, project_type, name, description, language, status, cover_asset_version_id,
		 deleted_at, deleted_by, created_at, updated_at, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.WorkspaceID, string(record.Type), record.Name, record.Description,
		record.Language, string(record.Status), record.CoverAssetVersionID,
		formatTime(record.DeletedAt), record.DeletedBy,
		formatTime(record.CreatedAt), formatTime(record.UpdatedAt), record.Revision)
	if err != nil {
		if isUniqueViolation(err) {
			return project.ConflictError("A project with that id already exists.")
		}
		return storageError("PROJECT_WRITE_FAILED", "The project could not be saved.", err)
	}
	return nil
}

// GetProject returns one project by id.
func (r *ProjectRepository) GetProject(ctx context.Context, id string) (project.Project, error) {
	conn := r.conn()
	if conn == nil {
		return project.Project{}, storageError("PROJECT_STORE_UNAVAILABLE", "The project store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, projectSelectColumns+` WHERE id = ?`, id)
	record, err := scanProject(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return project.Project{}, project.NotFoundError()
		}
		return project.Project{}, storageError("PROJECT_READ_FAILED", "The project could not be read.", err)
	}
	return record, nil
}

// ListProjects returns projects newest first.
func (r *ProjectRepository) ListProjects(ctx context.Context, filter projects.ListFilter) ([]project.Project, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("PROJECT_STORE_UNAVAILABLE", "The project store is unavailable.", nil)
	}
	query := projectSelectColumns
	conditions, args := projectFilterSQL(filter)
	if conditions != "" {
		query += " WHERE " + conditions
	}
	query += " ORDER BY created_at DESC, id DESC"
	limit := filter.Limit
	if limit <= 0 {
		limit = 500
	}
	query += " LIMIT ? OFFSET ?"
	args = append(args, limit, filter.Offset)

	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, storageError("PROJECT_READ_FAILED", "The projects could not be read.", err)
	}
	defer rows.Close()
	var records []project.Project
	for rows.Next() {
		record, scanErr := scanProject(rows)
		if scanErr != nil {
			return nil, storageError("PROJECT_READ_FAILED", "The projects could not be read.", scanErr)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("PROJECT_READ_FAILED", "The projects could not be read.", err)
	}
	return records, nil
}

// CountProjects reports how many rows match, for import verification.
func (r *ProjectRepository) CountProjects(ctx context.Context, filter projects.ListFilter) (int, error) {
	conn := r.conn()
	if conn == nil {
		return 0, storageError("PROJECT_STORE_UNAVAILABLE", "The project store is unavailable.", nil)
	}
	query := "SELECT COUNT(*) FROM projects"
	conditions, args := projectFilterSQL(filter)
	if conditions != "" {
		query += " WHERE " + conditions
	}
	var count int
	if err := conn.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, storageError("PROJECT_READ_FAILED", "The projects could not be counted.", err)
	}
	return count, nil
}

// projectFilterSQL builds the shared WHERE clause for project reads.
func projectFilterSQL(filter projects.ListFilter) (string, []any) {
	clauses := make([]string, 0, 3)
	args := make([]any, 0, 4)
	if filter.WorkspaceID != "" {
		clauses = append(clauses, "workspace_id = ?")
		args = append(args, filter.WorkspaceID)
	}
	if len(filter.Statuses) > 0 {
		placeholders := ""
		for index, status := range filter.Statuses {
			if index > 0 {
				placeholders += ", "
			}
			placeholders += "?"
			args = append(args, string(status))
		}
		clauses = append(clauses, "status IN ("+placeholders+")")
	}
	if !filter.IncludeDeleted {
		clauses = append(clauses, "deleted_at = ''")
	}
	return joinClauses(clauses), args
}

// joinClauses joins conditions with AND.
func joinClauses(clauses []string) string {
	result := ""
	for index, clause := range clauses {
		if index > 0 {
			result += " AND "
		}
		result += clause
	}
	return result
}

// UpdateProject persists a change guarded by the expected revision.
func (r *ProjectRepository) UpdateProject(ctx context.Context, record project.Project, expectedRevision int64) error {
	conn := r.conn()
	if conn == nil {
		return storageError("PROJECT_STORE_UNAVAILABLE", "The project store is unavailable.", nil)
	}
	result, err := conn.ExecContext(ctx, `UPDATE projects
		SET workspace_id = ?, project_type = ?, name = ?, description = ?, language = ?, status = ?,
		    cover_asset_version_id = ?, deleted_at = ?, deleted_by = ?, updated_at = ?,
		    revision = revision + 1
		WHERE id = ? AND revision = ?`,
		record.WorkspaceID, string(record.Type), record.Name, record.Description, record.Language,
		string(record.Status), record.CoverAssetVersionID, formatTime(record.DeletedAt), record.DeletedBy,
		formatTime(record.UpdatedAt), record.ID, expectedRevision)
	if err != nil {
		return storageError("PROJECT_WRITE_FAILED", "The project could not be updated.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError("PROJECT_WRITE_FAILED", "The project could not be updated.", err)
	}
	if affected == 0 {
		return project.RevisionMismatchError()
	}
	return nil
}

// DeleteProject removes a project; the schema cascades to everything under it.
func (r *ProjectRepository) DeleteProject(ctx context.Context, id string) error {
	conn := r.conn()
	if conn == nil {
		return storageError("PROJECT_STORE_UNAVAILABLE", "The project store is unavailable.", nil)
	}
	result, err := conn.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return storageError("PROJECT_WRITE_FAILED", "The project could not be deleted.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError("PROJECT_WRITE_FAILED", "The project could not be deleted.", err)
	}
	if affected == 0 {
		return project.NotFoundError()
	}
	return nil
}

// scanProject reads one project row. It uses the shared rowScanner interface
// declared alongside the job repository: *sql.Row and *sql.Rows both satisfy it.
func scanProject(row rowScanner) (project.Project, error) {
	var record project.Project
	var projectType, status, coverVersion, deletedAt, deletedBy, createdAt, updatedAt string
	if err := row.Scan(&record.ID, &record.WorkspaceID, &projectType, &record.Name, &record.Description,
		&record.Language, &status, &coverVersion, &deletedAt, &deletedBy, &createdAt, &updatedAt,
		&record.Revision); err != nil {
		return project.Project{}, err
	}
	record.Type = project.ProjectType(projectType)
	record.Status = project.ProjectStatus(status)
	record.CoverAssetVersionID = coverVersion
	record.DeletedAt = parseTime(deletedAt)
	record.DeletedBy = deletedBy
	record.CreatedAt = parseTime(createdAt)
	record.UpdatedAt = parseTime(updatedAt)
	return record, nil
}

// Ensure the repositories satisfy the application ports.
var (
	_ projects.ProjectRepository = (*ProjectRepository)(nil)
)
