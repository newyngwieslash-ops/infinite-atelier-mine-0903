package database

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/legacy"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/project"
)

// LegacyRepository owns every statement touching legacy_imports and
// legacy_id_map, and it performs the whole atomic import.
//
// The transaction boundary lives here, in the infrastructure layer, because
// it is a SQL concern: the application layer asks for "write this snapshot"
// and this type decides how. That is also what keeps a network call from ever
// being made inside a transaction (ADR-0006 §3), since the caller must have
// committed its files before it calls.
type LegacyRepository struct {
	db *sql.DB
	tx *sql.Tx
}

// NewLegacyRepository builds the repository over a database handle.
func NewLegacyRepository(db *sql.DB) *LegacyRepository {
	return &LegacyRepository{db: db}
}

// WithinTx binds the repository to a transaction.
func (r *LegacyRepository) WithinTx(tx *sql.Tx) *LegacyRepository {
	return &LegacyRepository{db: r.db, tx: tx}
}

func (r *LegacyRepository) conn() querier {
	if r == nil {
		return nil
	}
	return connection(r.db, r.tx)
}

// HasCompletedImport reports whether this project content already imported.
//
// The fingerprint identifies one project, not one run: the decision this answers
// is per project, because a snapshot may hold several and only some may have
// been imported before. A row exists only when the project committed, so a
// failed import is retried rather than treated as a duplicate.
func (r *LegacyRepository) HasCompletedImport(ctx context.Context, fingerprint string) (bool, error) {
	conn := r.conn()
	if conn == nil {
		return false, storageError("LEGACY_STORE_UNAVAILABLE", "The import store is unavailable.", nil)
	}
	var count int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM legacy_project_imports WHERE fingerprint = ?`,
		fingerprint).Scan(&count); err != nil {
		return false, storageError("LEGACY_READ_FAILED", "The import records could not be read.", err)
	}
	return count > 0, nil
}

// ImportSnapshot writes every bundle in one transaction.
//
// The order inside the transaction follows the foreign keys: project, then
// canvas document, then nodes (edges reference them), then edges, chats,
// assets, versions, file links, history, and finally the import record and its
// mapping rows. A verify pass re-reads the counts before the commit, so the
// numbers in the report describe persisted state rather than the plan.
func (r *LegacyRepository) ImportSnapshot(ctx context.Context, request legacy.ImportRequest) (legacy.ImportOutcome, error) {
	if r == nil || r.db == nil {
		return legacy.ImportOutcome{}, storageError("LEGACY_STORE_UNAVAILABLE", "The import store is unavailable.", nil)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return legacy.ImportOutcome{}, storageError("LEGACY_TX_FAILED", "The import could not be started.", err)
	}
	committed := false
	defer func() {
		if !committed {
			// A rollback after a successful commit is a no-op, so this is safe
			// on every path, and it is what guarantees "no half-import"
			// (AC-LEGACY-003) even if a later statement fails.
			_ = tx.Rollback()
		}
	}()

	projectsRepo := NewProjectRepository(r.db).WithinTx(tx)
	canvasRepo := NewCanvasRepository(r.db).WithinTx(tx)
	assetsRepo := NewAssetRepository(r.db).WithinTx(tx)
	legacyRepo := r.WithinTx(tx)

	outcome := legacy.ImportOutcome{}

	for _, bundle := range request.Bundles {
		if err := projectsRepo.CreateProject(ctx, bundle.Project); err != nil {
			return legacy.ImportOutcome{}, err
		}
		outcome.Projects++

		if err := canvasRepo.CreateDocument(ctx, bundle.Document); err != nil {
			return legacy.ImportOutcome{}, err
		}
		for _, node := range bundle.Nodes {
			if err := canvasRepo.CreateNode(ctx, node); err != nil {
				return legacy.ImportOutcome{}, err
			}
			outcome.Nodes++
		}
		for _, edge := range bundle.Edges {
			if err := canvasRepo.CreateEdge(ctx, edge); err != nil {
				return legacy.ImportOutcome{}, err
			}
			outcome.Edges++
		}
		for _, session := range bundle.ChatSessions {
			if err := canvasRepo.CreateChatSession(ctx, session); err != nil {
				return legacy.ImportOutcome{}, err
			}
		}
		for _, assetBundle := range bundle.Assets {
			if err := assetsRepo.CreateAsset(ctx, assetBundle.Asset); err != nil {
				return legacy.ImportOutcome{}, err
			}
			if err := assetsRepo.CreateVersion(ctx, assetBundle.Version); err != nil {
				return legacy.ImportOutcome{}, err
			}
			for _, link := range assetBundle.Files {
				if err := assetsRepo.AddFile(ctx, asset.File{
					VersionID: link.VersionID,
					FileHash:  link.FileHash,
					Role:      link.Role,
					CreatedAt: request.StartedAt,
				}); err != nil {
					return legacy.ImportOutcome{}, err
				}
			}
			outcome.Assets++
		}
		for _, record := range bundle.History {
			if err := legacyRepo.createHistory(ctx, bundle.Project.ID, record); err != nil {
				return legacy.ImportOutcome{}, err
			}
			outcome.History++
		}
	}

	// The import record goes in before the mappings, because the mappings
	// reference it.
	importID, err := legacyRepo.createImport(ctx, request, legacy.StatusCompleted, "")
	if err != nil {
		return legacy.ImportOutcome{}, err
	}
	outcome.ImportID = importID

	for _, bundle := range request.Bundles {
		for _, mapping := range bundle.Mappings {
			if err := legacyRepo.insertMapping(ctx, importID, mapping); err != nil {
				return legacy.ImportOutcome{}, err
			}
		}
		// The per-project fingerprint is recorded here, inside the transaction, so
		// it exists only if the project rows did.
		if err := legacyRepo.recordProjectFingerprint(ctx, importID, bundle); err != nil {
			return legacy.ImportOutcome{}, err
		}
	}

	// Verify inside the transaction: the counts must describe what is stored,
	// not what was planned.
	if err := legacyRepo.verifyCounts(ctx, request, outcome); err != nil {
		return legacy.ImportOutcome{}, err
	}

	if err := tx.Commit(); err != nil {
		return legacy.ImportOutcome{}, storageError("LEGACY_TX_FAILED", "The import could not be saved.", err)
	}
	committed = true
	return outcome, nil
}

// verifyCounts re-reads the persisted rows and compares them with the plan.
func (r *LegacyRepository) verifyCounts(ctx context.Context, request legacy.ImportRequest, outcome legacy.ImportOutcome) error {
	conn := r.conn()
	if conn == nil {
		return storageError("LEGACY_STORE_UNAVAILABLE", "The import store is unavailable.", nil)
	}
	nodes, edges, assets, history := 0, 0, 0, 0
	for _, bundle := range request.Bundles {
		var projectNodes, projectEdges int
		if err := conn.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM canvas_nodes WHERE canvas_document_id = ?`, bundle.Document.ID).Scan(&projectNodes); err != nil {
			return storageError("LEGACY_VERIFY_FAILED", "The imported canvas could not be verified.", err)
		}
		if err := conn.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM canvas_edges WHERE canvas_document_id = ?`, bundle.Document.ID).Scan(&projectEdges); err != nil {
			return storageError("LEGACY_VERIFY_FAILED", "The imported canvas could not be verified.", err)
		}
		nodes += projectNodes
		edges += projectEdges

		var projectAssets, projectHistory int
		if err := conn.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM assets WHERE project_id = ?`, bundle.Project.ID).Scan(&projectAssets); err != nil {
			return storageError("LEGACY_VERIFY_FAILED", "The imported assets could not be verified.", err)
		}
		if err := conn.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM generation_history WHERE project_id = ?`, bundle.Project.ID).Scan(&projectHistory); err != nil {
			return storageError("LEGACY_VERIFY_FAILED", "The imported history could not be verified.", err)
		}
		assets += projectAssets
		history += projectHistory
	}
	if nodes != outcome.Nodes || edges != outcome.Edges || assets != outcome.Assets || history != outcome.History {
		// A mismatch means the write did not land as planned. Returning an error
		// rolls the whole import back rather than reporting a partial success.
		return &project.ImportError{
			Stage:       "verify",
			SafeMessage: "The imported project did not match what was requested.",
		}
	}
	return nil
}

// createImport inserts the run record and returns its id.
func (r *LegacyRepository) createImport(ctx context.Context, request legacy.ImportRequest, status, reportJSON string) (string, error) {
	conn := r.conn()
	if conn == nil {
		return "", storageError("LEGACY_STORE_UNAVAILABLE", "The import store is unavailable.", nil)
	}
	id, err := newImportID()
	if err != nil {
		return "", storageError("LEGACY_WRITE_FAILED", "The import record could not be created.", err)
	}
	warningsJSON, err := json.Marshal(collectWarnings(request.Bundles))
	if err != nil {
		warningsJSON = []byte("[]")
	}
	now := request.StartedAt.UTC().Format(timeFormat)
	if _, err := conn.ExecContext(ctx, `INSERT INTO legacy_imports
		(id, source_fingerprint, source_case, mode, status, report_json, warnings_json, legacy_root,
		 started_at, finished_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, request.Fingerprint, request.SourceCase, request.Mode, status, reportJSON,
		string(warningsJSON), request.LegacyRoot, formatTime(request.StartedAt), now, now); err != nil {
		return "", storageError("LEGACY_WRITE_FAILED", "The import record could not be saved.", err)
	}
	return id, nil
}

// collectWarnings flattens the per-bundle warnings into one list.
func collectWarnings(bundles []legacy.ProjectBundle) []legacy.Warning {
	warnings := make([]legacy.Warning, 0)
	for _, bundle := range bundles {
		warnings = append(warnings, bundle.Warnings...)
		for _, assetBundle := range bundle.Assets {
			warnings = append(warnings, assetBundle.Warnings...)
		}
	}
	return warnings
}

// RecordImport stores a run that did not succeed, so a failure is visible.
func (r *LegacyRepository) RecordImport(ctx context.Context, request legacy.ImportRequest, status string, reportJSON string, warnings []legacy.Warning) (string, error) {
	conn := r.conn()
	if conn == nil {
		return "", storageError("LEGACY_STORE_UNAVAILABLE", "The import store is unavailable.", nil)
	}
	id, err := newImportID()
	if err != nil {
		return "", storageError("LEGACY_WRITE_FAILED", "The import record could not be created.", err)
	}
	warningsJSON, err := json.Marshal(warnings)
	if err != nil {
		warningsJSON = []byte("[]")
	}
	now := request.StartedAt.UTC().Format(timeFormat)
	if _, err := conn.ExecContext(ctx, `INSERT INTO legacy_imports
		(id, source_fingerprint, source_case, mode, status, report_json, warnings_json, legacy_root,
		 started_at, finished_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, request.Fingerprint, request.SourceCase, request.Mode, status, reportJSON,
		string(warningsJSON), request.LegacyRoot, formatTime(request.StartedAt), now, now); err != nil {
		return "", storageError("LEGACY_WRITE_FAILED", "The import record could not be saved.", err)
	}
	return id, nil
}

// insertMapping stores one legacy-to-new identifier mapping.
func (r *LegacyRepository) insertMapping(ctx context.Context, importID string, mapping legacy.IDMapping) error {
	conn := r.conn()
	if conn == nil {
		return storageError("LEGACY_STORE_UNAVAILABLE", "The import store is unavailable.", nil)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO legacy_id_map
		(import_id, kind, legacy_id, new_id, created_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(import_id, kind, legacy_id) DO NOTHING`,
		importID, mapping.Kind, mapping.LegacyID, mapping.NewID, requestTime()); err != nil {
		return storageError("LEGACY_WRITE_FAILED", "The import mapping could not be saved.", err)
	}
	return nil
}

// recordProjectFingerprint notes that one project content is now imported.
func (r *LegacyRepository) recordProjectFingerprint(ctx context.Context, importID string, bundle legacy.ProjectBundle) error {
	conn := r.conn()
	if conn == nil {
		return storageError("LEGACY_STORE_UNAVAILABLE", "The import store is unavailable.", nil)
	}
	legacyProjectID := ""
	for _, mapping := range bundle.Mappings {
		if mapping.Kind == legacy.MapProject {
			legacyProjectID = mapping.LegacyID
			break
		}
	}
	if legacyProjectID == "" || bundle.Fingerprint == "" {
		// Without an identifier and a fingerprint there is nothing to detect a
		// repeat by, and writing a blank key would make every later import look
		// like a duplicate of nothing.
		return nil
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO legacy_project_imports
		(fingerprint, import_id, legacy_project_id, project_id, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(fingerprint) DO NOTHING`,
		bundle.Fingerprint, importID, legacyProjectID, bundle.Project.ID, requestTime()); err != nil {
		return storageError("LEGACY_WRITE_FAILED", "The import record could not be saved.", err)
	}
	return nil
}

// createHistory stores one archived history row.
func (r *LegacyRepository) createHistory(ctx context.Context, projectID string, record legacy.HistoryRecord) error {
	conn := r.conn()
	if conn == nil {
		return storageError("LEGACY_STORE_UNAVAILABLE", "The import store is unavailable.", nil)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO generation_history
		(id, project_id, legacy_id, prompt, model, images_json, success_count, fail_count, generated_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, projectID, record.LegacyID, record.Prompt, record.Model,
		nonEmptyJSONArray(record.ImagesJSON), record.Success, record.Fail,
		formatTime(record.GeneratedAt), requestTime()); err != nil {
		return storageError("LEGACY_WRITE_FAILED", "The generation history could not be saved.", err)
	}
	return nil
}

// nonEmptyJSONArray keeps the images column valid JSON.
func nonEmptyJSONArray(value string) string {
	if value == "" {
		return "[]"
	}
	return value
}

const importSelectColumns = `SELECT id, source_fingerprint, source_case, mode, status, report_json,
	warnings_json, legacy_root, started_at, finished_at, created_at FROM legacy_imports`

// ListImports returns recent runs newest first.
func (r *LegacyRepository) ListImports(ctx context.Context, limit int) ([]legacy.ImportRecord, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("LEGACY_STORE_UNAVAILABLE", "The import store is unavailable.", nil)
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := conn.QueryContext(ctx, importSelectColumns+` ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, storageError("LEGACY_READ_FAILED", "The import records could not be read.", err)
	}
	defer rows.Close()
	var records []legacy.ImportRecord
	for rows.Next() {
		record, scanErr := scanImport(rows)
		if scanErr != nil {
			return nil, storageError("LEGACY_READ_FAILED", "The import records could not be read.", scanErr)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("LEGACY_READ_FAILED", "The import records could not be read.", err)
	}
	return records, nil
}

// GetImport returns one run.
func (r *LegacyRepository) GetImport(ctx context.Context, id string) (legacy.ImportRecord, error) {
	conn := r.conn()
	if conn == nil {
		return legacy.ImportRecord{}, storageError("LEGACY_STORE_UNAVAILABLE", "The import store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, importSelectColumns+` WHERE id = ?`, id)
	record, err := scanImport(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return legacy.ImportRecord{}, project.NotFoundError()
		}
		return legacy.ImportRecord{}, storageError("LEGACY_READ_FAILED", "The import record could not be read.", err)
	}
	return record, nil
}

// LookupMapping resolves a legacy id to what a past import created.
//
// When a legacy id was imported more than once (a user chose a copy), the most
// recent mapping wins: that is the entity the user is most likely to mean.
func (r *LegacyRepository) LookupMapping(ctx context.Context, kind, legacyID string) (string, bool, error) {
	conn := r.conn()
	if conn == nil {
		return "", false, storageError("LEGACY_STORE_UNAVAILABLE", "The import store is unavailable.", nil)
	}
	var newID string
	err := conn.QueryRowContext(ctx, `SELECT m.new_id FROM legacy_id_map m
		JOIN legacy_imports i ON i.id = m.import_id
		WHERE m.kind = ? AND m.legacy_id = ?
		ORDER BY i.created_at DESC, m.created_at DESC LIMIT 1`, kind, legacyID).Scan(&newID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, storageError("LEGACY_READ_FAILED", "The import mapping could not be read.", err)
	}
	return newID, true, nil
}

// timeFormat is the storage format for timestamps written by this file. It
// matches the format the other repositories use, so a reader can parse every
// column with one helper.
const timeFormat = "2006-01-02T15:04:05.999999999Z07:00"

// requestTime is the timestamp for a row written outside a request's own time
// window (a mapping row, a history row).
func requestTime() string {
	return formatTime(timeNow())
}

// newImportID mints the identifier for an import run.
//
// Import records are not user-visible entities, so they use the same UUIDv7
// generator as everything else rather than a private scheme (ADR-0005).
func newImportID() (string, error) {
	return defaultIDs.New()
}

// scanImport reads one import row.
func scanImport(row rowScanner) (legacy.ImportRecord, error) {
	var record legacy.ImportRecord
	var warningsJSON, startedAt, finishedAt, createdAt string
	if err := row.Scan(&record.ID, &record.Fingerprint, &record.SourceCase, &record.Mode,
		&record.Status, &record.ReportJSON, &warningsJSON, &record.LegacyRoot,
		&startedAt, &finishedAt, &createdAt); err != nil {
		return legacy.ImportRecord{}, err
	}
	var warnings []legacy.Warning
	if err := json.Unmarshal([]byte(warningsJSON), &warnings); err != nil {
		// A malformed warnings blob must not break the report; an empty list is
		// the safe reading, and the record's other fields still render.
		warnings = nil
	}
	record.Warnings = warnings
	record.StartedAt = parseTime(startedAt)
	record.FinishedAt = parseTime(finishedAt)
	record.CreatedAt = parseTime(createdAt)
	return record, nil
}

// Ensure the repository satisfies the application port.
var _ legacy.ImportStore = (*LegacyRepository)(nil)
