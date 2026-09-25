package database

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"

	appdiagnostics "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/diagnostics"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

// diagnostics_reader.go answers SECURITY section 14.2's eight items over the real schema.
//
// Every method is a COUNT or a scalar, which is what the port's shape enforces: a bundle is a
// summary, and a reader that could return rows would invite a bundle that carried them. Two of the
// answers are deliberately narrow:
//
//   - `ProviderKinds` returns a kind and an enabled flag. NOT the display name (user text), NOT the
//     base URL (which may carry credentials in its user-info) and NOT the secret reference (a
//     credential TARGET). SECURITY 14.2 asks for 「Provider 类型与健康，不含密钥」 in as many words.
//   - `ErrorCodes` returns codes and counts with no messages, because a message can quote a path or
//     a payload.
type DiagnosticsReader struct {
	db *sql.DB
	// logPath is the redacted log file the logging handler writes.
	logPath string
}

// NewDiagnosticsReader builds the reader over a connection and the log directory.
func NewDiagnosticsReader(db *sql.DB, logDirectory string) *DiagnosticsReader {
	return &DiagnosticsReader{db: db, logPath: filepath.Join(logDirectory, "app.jsonl")}
}

// SchemaVersion returns the database's migration version.
func (r *DiagnosticsReader) SchemaVersion(ctx context.Context) (int, error) {
	if r == nil || r.db == nil {
		return 0, diagnosticsError("DIAGNOSTICS_STORE_UNAVAILABLE", "The diagnostics store is unavailable.", nil)
	}
	var version int
	if err := r.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return 0, diagnosticsError("DIAGNOSTICS_READ_FAILED", "The schema version could not be read.", err)
	}
	return version, nil
}

// PendingMigrations reports whether the database is behind this build.
//
// It compares the recorded version against the highest migration the build EMBEDS, which is the same
// comparison the startup gate makes — the number is read from the `schema_migrations` table rather
// than from `user_version` alone, because a migration that is recorded but whose version was not
// bumped would otherwise look pending forever.
func (r *DiagnosticsReader) PendingMigrations(ctx context.Context) (bool, error) {
	if r == nil || r.db == nil {
		return false, diagnosticsError("DIAGNOSTICS_STORE_UNAVAILABLE", "The diagnostics store is unavailable.", nil)
	}
	applied := 0
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&applied); err != nil {
		return false, diagnosticsError("DIAGNOSTICS_READ_FAILED", "The migration count could not be read.", err)
	}
	// The build's own set, read through the same loader the startup gate uses — so "pending" here
	// means the same thing it means there rather than a second count that could drift from it.
	embedded, err := loadMigrationFiles(embeddedMigrations)
	if err != nil {
		return false, diagnosticsError("DIAGNOSTICS_READ_FAILED", "The build's migrations could not be read.", err)
	}
	return applied < len(embedded), nil
}

// ProviderKinds returns each configured provider's kind and whether it is enabled.
//
// The SELECT names two columns, which is what makes "no secrets" a property of the query rather than
// a promise about the code that consumes it.
func (r *DiagnosticsReader) ProviderKinds(ctx context.Context) ([]appdiagnostics.ProviderSummary, error) {
	if r == nil || r.db == nil {
		return nil, diagnosticsError("DIAGNOSTICS_STORE_UNAVAILABLE", "The diagnostics store is unavailable.", nil)
	}
	rows, err := r.db.QueryContext(ctx, `SELECT kind, enabled FROM provider_configs ORDER BY kind ASC`)
	if err != nil {
		return nil, diagnosticsError("DIAGNOSTICS_READ_FAILED", "The providers could not be read.", err)
	}
	defer rows.Close()
	var summaries []appdiagnostics.ProviderSummary
	for rows.Next() {
		var summary appdiagnostics.ProviderSummary
		var enabled int
		if err := rows.Scan(&summary.Kind, &enabled); err != nil {
			return nil, diagnosticsError("DIAGNOSTICS_READ_FAILED", "The providers could not be read.", err)
		}
		summary.Enabled = enabled == 1
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, diagnosticsError("DIAGNOSTICS_READ_FAILED", "The providers could not be read.", err)
	}
	return summaries, nil
}

// FileStoreSummary reports how many objects the store holds and their total size.
func (r *DiagnosticsReader) FileStoreSummary(ctx context.Context) (appdiagnostics.FileStoreSummary, error) {
	if r == nil || r.db == nil {
		return appdiagnostics.FileStoreSummary{}, diagnosticsError("DIAGNOSTICS_STORE_UNAVAILABLE", "The diagnostics store is unavailable.", nil)
	}
	var summary appdiagnostics.FileStoreSummary
	var total sql.NullInt64
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*), SUM(size_bytes) FROM file_objects`).Scan(&summary.Objects, &total); err != nil {
		return appdiagnostics.FileStoreSummary{}, diagnosticsError("DIAGNOSTICS_READ_FAILED", "The file store summary could not be read.", err)
	}
	summary.TotalBytes = total.Int64
	return summary, nil
}

// JobCountsByStatus returns generation jobs grouped by status.
func (r *DiagnosticsReader) JobCountsByStatus(ctx context.Context) (map[string]int, error) {
	return r.countsByStatus(ctx, `SELECT status, COUNT(*) FROM generation_jobs GROUP BY status`,
		"The job summary could not be read.")
}

// WorkflowCountsByStatus returns workflow runs grouped by status.
func (r *DiagnosticsReader) WorkflowCountsByStatus(ctx context.Context) (map[string]int, error) {
	return r.countsByStatus(ctx, `SELECT status, COUNT(*) FROM workflow_runs GROUP BY status`,
		"The workflow summary could not be read.")
}

func (r *DiagnosticsReader) countsByStatus(ctx context.Context, query, message string) (map[string]int, error) {
	if r == nil || r.db == nil {
		return nil, diagnosticsError("DIAGNOSTICS_STORE_UNAVAILABLE", "The diagnostics store is unavailable.", nil)
	}
	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, diagnosticsError("DIAGNOSTICS_READ_FAILED", message, err)
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, diagnosticsError("DIAGNOSTICS_READ_FAILED", message, err)
		}
		counts[status] = count
	}
	if err := rows.Err(); err != nil {
		return nil, diagnosticsError("DIAGNOSTICS_READ_FAILED", message, err)
	}
	return counts, nil
}

// ErrorCodes returns the error codes the application has recorded, with counts.
//
// It reads `provider_requests.error_code`, which is the one place a stable code is PERSISTED for an
// operation rather than shown to a user. The SELECT names the code column alone: a request's payload,
// its URL and its headers are all on that row, and none may reach a bundle.
func (r *DiagnosticsReader) ErrorCodes(ctx context.Context) (map[string]int, error) {
	if r == nil || r.db == nil {
		return nil, diagnosticsError("DIAGNOSTICS_STORE_UNAVAILABLE", "The diagnostics store is unavailable.", nil)
	}
	rows, err := r.db.QueryContext(ctx, `SELECT error_code, COUNT(*) FROM provider_requests
		WHERE error_code <> '' GROUP BY error_code`)
	if err != nil {
		return nil, diagnosticsError("DIAGNOSTICS_READ_FAILED", "The error codes could not be read.", err)
	}
	defer rows.Close()
	codes := map[string]int{}
	for rows.Next() {
		var code string
		var count int
		if err := rows.Scan(&code, &count); err != nil {
			return nil, diagnosticsError("DIAGNOSTICS_READ_FAILED", "The error codes could not be read.", err)
		}
		codes[code] = count
	}
	if err := rows.Err(); err != nil {
		return nil, diagnosticsError("DIAGNOSTICS_READ_FAILED", "The error codes could not be read.", err)
	}
	return codes, nil
}

// LogBytes returns the tail of the redacted log file, bounded by limit.
//
// # Why the TAIL, and why the bound is applied here rather than by the caller
//
// The newest lines are what a diagnosis needs: a session that failed ten minutes ago has its cause at
// the END of the file. Reading the whole file and truncating would allocate a size the caller already
// said it did not want, on an installation whose log may be large precisely because something has
// been failing repeatedly — so the read seeks.
//
// A missing log file is NOT an error: a fresh installation has none, and a bundle that refused to be
// built because there were no logs would be a bundle a new user cannot send.
func (r *DiagnosticsReader) LogBytes(_ context.Context, limit int) ([]byte, error) {
	if r == nil || r.logPath == "" {
		return nil, nil
	}
	file, err := os.Open(r.logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, diagnosticsError("DIAGNOSTICS_READ_FAILED", "The log could not be read.", err)
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, diagnosticsError("DIAGNOSTICS_READ_FAILED", "The log could not be read.", err)
	}
	size := info.Size()
	if limit <= 0 || int64(limit) >= size {
		content := make([]byte, size)
		if _, err := file.ReadAt(content, 0); err != nil && err.Error() != "EOF" {
			return nil, diagnosticsError("DIAGNOSTICS_READ_FAILED", "The log could not be read.", err)
		}
		return content, nil
	}
	content := make([]byte, limit)
	if _, err := file.ReadAt(content, size-int64(limit)); err != nil && err.Error() != "EOF" {
		return nil, diagnosticsError("DIAGNOSTICS_READ_FAILED", "The log could not be read.", err)
	}
	return content, nil
}

func diagnosticsError(code, message string, cause error) error {
	return apperror.New(code, "storage", false, message, cause)
}

// Compile-time proof that this type is the port the diagnostics service declares.
var _ appdiagnostics.Reader = (*DiagnosticsReader)(nil)
