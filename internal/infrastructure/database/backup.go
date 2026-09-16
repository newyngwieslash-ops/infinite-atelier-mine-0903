package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/backup"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

// BackupStore implements the backup export source and restore sink over SQLite
// and the FileStore.
//
// It is the only place that reads every table for an archive and the only place
// that opens a staged database for verification. It never reads the credential
// store: an ordinary backup has no secret to read (ADR-0006 §9).
type BackupStore struct {
	db *sql.DB
	// filesDir and tempDir come from the resolved application directories.
	filesDir string
	tempDir  string
	// snapshotDir is where the export's point-in-time copy is taken.
	snapshotDir string
	// appVersion labels an archive.
	appVersion string
}

// NewBackupStore builds the store.
func NewBackupStore(db *sql.DB, filesDir, tempDir, snapshotDir, appVersion string) *BackupStore {
	return &BackupStore{db: db, filesDir: filesDir, tempDir: tempDir, snapshotDir: snapshotDir, appVersion: appVersion}
}

// SnapshotDatabase writes a consistent copy through VACUUM INTO.
//
// VACUUM INTO is what makes the copy point-in-time: copying the file while WAL
// pages are outstanding could archive a torn database, and PRD NFR-002 requires
// the backup to be consistent.
func (s *BackupStore) SnapshotDatabase(ctx context.Context, directory string) (string, error) {
	if s == nil || s.db == nil {
		return "", apperror.New("BACKUP_UNAVAILABLE", "storage", false, "The backup service is unavailable.", nil)
	}
	target := directory
	if target == "" {
		target = s.snapshotDir
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		return "", apperror.New("BACKUP_SNAPSHOT_FAILED", "storage", false, "The database could not be prepared for backup.", err)
	}
	dest := filepath.Join(target, "backup-export.sqlite")
	// A leftover from a previous attempt would make VACUUM INTO fail.
	_ = os.Remove(dest)
	var quoted string
	if err := s.db.QueryRowContext(ctx, "SELECT quote(?)", dest).Scan(&quoted); err != nil {
		return "", apperror.New("BACKUP_SNAPSHOT_FAILED", "storage", false, "The database could not be prepared for backup.", err)
	}
	if _, err := s.db.ExecContext(ctx, "VACUUM INTO "+quoted); err != nil {
		return "", apperror.New("BACKUP_SNAPSHOT_FAILED", "storage", false, "The database could not be snapshotted.", err)
	}
	return dest, nil
}

// SchemaVersion reports the applied migration version.
func (s *BackupStore) SchemaVersion(ctx context.Context) (int, error) {
	if s == nil || s.db == nil {
		return 0, apperror.New("BACKUP_UNAVAILABLE", "storage", false, "The backup service is unavailable.", nil)
	}
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return 0, apperror.New("BACKUP_READ_FAILED", "storage", false, "The database version could not be read.", err)
	}
	return version, nil
}

// Counts reports the entity counts the manifest advertises.
func (s *BackupStore) Counts(ctx context.Context) (backup.Counts, error) {
	if s == nil || s.db == nil {
		return backup.Counts{}, apperror.New("BACKUP_UNAVAILABLE", "storage", false, "The backup service is unavailable.", nil)
	}
	var counts backup.Counts
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM projects").Scan(&counts.Projects); err != nil {
		return backup.Counts{}, apperror.New("BACKUP_READ_FAILED", "storage", false, "The project count could not be read.", err)
	}
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM assets").Scan(&counts.Assets); err != nil {
		return backup.Counts{}, apperror.New("BACKUP_READ_FAILED", "storage", false, "The asset count could not be read.", err)
	}
	return counts, nil
}

// Files lists every stored object.
func (s *BackupStore) Files(ctx context.Context) ([]backup.FileEntry, error) {
	if s == nil || s.db == nil {
		return nil, apperror.New("BACKUP_UNAVAILABLE", "storage", false, "The backup service is unavailable.", nil)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT hash, mime_type, size_bytes FROM file_objects ORDER BY hash ASC`)
	if err != nil {
		return nil, apperror.New("BACKUP_READ_FAILED", "storage", false, "The media list could not be read.", err)
	}
	defer rows.Close()
	var entries []backup.FileEntry
	for rows.Next() {
		var entry backup.FileEntry
		if err := rows.Scan(&entry.Hash, &entry.MIME, &entry.Size); err != nil {
			return nil, apperror.New("BACKUP_READ_FAILED", "storage", false, "The media list could not be read.", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, apperror.New("BACKUP_READ_FAILED", "storage", false, "The media list could not be read.", err)
	}
	return entries, nil
}

// Open returns one stored object's bytes.
//
// The path is derived from the hash through the same sharding the FileStore
// uses, and the hash is validated first, so a mangled value cannot escape the
// managed directory.
func (s *BackupStore) Open(ctx context.Context, hash string) ([]byte, error) {
	if s == nil || s.filesDir == "" {
		return nil, apperror.New("BACKUP_UNAVAILABLE", "storage", false, "The backup service is unavailable.", nil)
	}
	if !isLowerHexDigest(hash) {
		return nil, apperror.New("BACKUP_READ_FAILED", "storage", false, "A media file could not be read.", nil)
	}
	path := filepath.Join(s.filesDir, hash[:2], hash)
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, apperror.New("BACKUP_READ_FAILED", "storage", false, "A media file could not be read.", err)
	}
	return content, nil
}

// ProviderMetadata returns the provider configuration without secret material.
//
// The query selects only the non-secret columns. `secret_ref` is included
// because it is a reference, not a value: it names which credential the
// provider uses, and the credential store is never read.
func (s *BackupStore) ProviderMetadata(ctx context.Context) ([]byte, error) {
	if s == nil || s.db == nil {
		return nil, apperror.New("BACKUP_UNAVAILABLE", "storage", false, "The backup service is unavailable.", nil)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, kind, display_name, base_url, secret_ref, local_approved, enabled
		FROM provider_configs ORDER BY id ASC`)
	if err != nil {
		return nil, apperror.New("BACKUP_READ_FAILED", "storage", false, "The provider settings could not be read.", err)
	}
	defer rows.Close()
	type providerEntry struct {
		ID            string `json:"id"`
		Kind          string `json:"kind"`
		DisplayName   string `json:"displayName"`
		BaseURL       string `json:"baseUrl"`
		SecretRef     string `json:"secretRef"`
		LocalApproved bool   `json:"localApproved"`
		Enabled       bool   `json:"enabled"`
	}
	providers := make([]providerEntry, 0)
	for rows.Next() {
		var entry providerEntry
		var localApproved, enabled int
		if err := rows.Scan(&entry.ID, &entry.Kind, &entry.DisplayName, &entry.BaseURL,
			&entry.SecretRef, &localApproved, &enabled); err != nil {
			return nil, apperror.New("BACKUP_READ_FAILED", "storage", false, "The provider settings could not be read.", err)
		}
		entry.LocalApproved = localApproved != 0
		entry.Enabled = enabled != 0
		providers = append(providers, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, apperror.New("BACKUP_READ_FAILED", "storage", false, "The provider settings could not be read.", err)
	}
	// The key names are chosen so the serialised payload contains no field that
	// could be mistaken for a credential.
	encoded, err := json.Marshal(map[string]any{
		"note":      "Credential values are not part of an ordinary backup and live in the OS store.",
		"providers": providers,
	})
	if err != nil {
		return nil, apperror.New("BACKUP_READ_FAILED", "storage", false, "The provider settings could not be written.", err)
	}
	return encoded, nil
}

// StageDatabase writes an archived database into the private staging area.
func (s *BackupStore) StageDatabase(ctx context.Context, content []byte) (string, error) {
	if s == nil || s.tempDir == "" {
		return "", apperror.New("BACKUP_UNAVAILABLE", "storage", false, "The backup service is unavailable.", nil)
	}
	directory := filepath.Join(s.tempDir, "restore")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", apperror.New("BACKUP_RESTORE_FAILED", "storage", false, "The restore could not be prepared.", err)
	}
	path := filepath.Join(directory, "staged.sqlite")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return "", apperror.New("BACKUP_RESTORE_FAILED", "storage", false, "The database could not be staged.", err)
	}
	return path, nil
}

// VerifyDatabase opens a staged database read-only and checks it.
//
// Opening read-only is deliberate: a verification must not modify the archive,
// and SECURITY §9.3 requires the integrity check to run before any migration.
// The connection is closed before returning so a later step can move the file.
func (s *BackupStore) VerifyDatabase(ctx context.Context, path string) (int, error) {
	// The driver is registered by this package's own import.
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=foreign_keys(1)")
	if err != nil {
		return 0, apperror.New("BACKUP_RESTORE_FAILED", "storage", false, "The archived database could not be opened.", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	var integrity string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return 0, apperror.New("BACKUP_RESTORE_FAILED", "storage", false, "The archived database could not be checked.", err)
	}
	if integrity != "ok" {
		return 0, apperror.New("BACKUP_RESTORE_FAILED", "storage", false, "The archived database failed its integrity check.", nil)
	}
	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return 0, apperror.New("BACKUP_RESTORE_FAILED", "storage", false, "The archived database version could not be read.", err)
	}
	return version, nil
}

// StageFile stores one object's verified bytes under its content hash.
func (s *BackupStore) StageFile(_ context.Context, entry backup.FileEntry, content []byte) error {
	if s == nil || s.tempDir == "" {
		return apperror.New("BACKUP_UNAVAILABLE", "storage", false, "The backup service is unavailable.", nil)
	}
	if !isLowerHexDigest(entry.Hash) {
		return apperror.New("BACKUP_RESTORE_FAILED", "security", false, "An archived file name is invalid.", nil)
	}
	directory := filepath.Join(s.tempDir, "restore", "files", entry.Hash[:2])
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return apperror.New("BACKUP_RESTORE_FAILED", "storage", false, "A media file could not be staged.", err)
	}
	path := filepath.Join(directory, entry.Hash)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return apperror.New("BACKUP_RESTORE_FAILED", "storage", false, "A media file could not be staged.", err)
	}
	return nil
}

// isLowerHexDigest reports whether a value is a 64-character lowercase hex
// digest. It is duplicated here from the file layer's equivalent so this store
// validates before it builds a path from the value.
func isLowerHexDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		switch {
		case character >= '0' && character <= '9':
		case character >= 'a' && character <= 'f':
		default:
			return false
		}
	}
	return true
}

// Ensure the store satisfies both ports.
var (
	_ backup.Source = (*BackupStore)(nil)
	_ backup.Sink   = (*BackupStore)(nil)
)
