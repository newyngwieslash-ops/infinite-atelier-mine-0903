package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/archive"
)

// ExportService writes an ordinary backup.
type ExportService struct {
	source Source
	clock  Clock
	// appVersion labels the archive.
	appVersion string
	// workDir is where the database snapshot is taken before it is read into
	// the archive. It must be private to the application.
	workDir string
}

// ExportOptions configures an export.
type ExportOptions struct {
	Source     Source
	Clock      Clock
	AppVersion string
	// WorkDir is a private directory the snapshot is taken in.
	WorkDir string
}

// NewExportService builds the exporter.
func NewExportService(options ExportOptions) *ExportService {
	return &ExportService{
		source:     options.Source,
		clock:      options.Clock,
		appVersion: options.AppVersion,
		workDir:    options.WorkDir,
	}
}

// Available reports whether the service can run.
func (s *ExportService) Available() bool {
	return s != nil && s.source != nil && strings.TrimSpace(s.workDir) != ""
}

func (s *ExportService) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now().UTC()
	}
	return s.clock.Now().UTC()
}

// Export writes the archive.
//
// The order matters: the database snapshot is taken first, so the file list is
// read from a point-in-time view rather than a moving one. Every listed object
// must be readable: an archive that references a file it does not contain would
// restore into broken references, so a missing object fails the export rather
// than producing a quietly incomplete backup.
func (s *ExportService) Export(ctx context.Context) (ExportResult, error) {
	if !s.Available() {
		return ExportResult{}, unavailableError()
	}
	counts, err := s.source.Counts(ctx)
	if err != nil {
		return ExportResult{}, exportError("counts", "The project list could not be read.", err)
	}
	schemaVersion, err := s.source.SchemaVersion(ctx)
	if err != nil {
		return ExportResult{}, exportError("schema", "The database version could not be read.", err)
	}
	databasePath, err := s.source.SnapshotDatabase(ctx, s.workDir)
	if err != nil {
		return ExportResult{}, exportError("snapshot", "The database could not be snapshotted.", err)
	}
	databaseBytes, err := readDatabaseBytes(databasePath)
	if err != nil {
		return ExportResult{}, exportError("snapshot", "The database snapshot could not be read.", err)
	}
	entries, err := s.source.Files(ctx)
	if err != nil {
		return ExportResult{}, exportError("files", "The media list could not be read.", err)
	}
	providerMetadata, err := s.source.ProviderMetadata(ctx)
	if err != nil {
		return ExportResult{}, exportError("providers", "The provider settings could not be read.", err)
	}

	manifest := Manifest{
		ManifestVersion: SupportedManifestVersion,
		App:             "infinite-canvas",
		AppVersion:      s.appVersion,
		SchemaVersion:   schemaVersion,
		CreatedAt:       s.now().Format(time.RFC3339),
		Projects:        counts.Projects,
		Assets:          counts.Assets,
		Files:           len(entries),
		DatabaseBytes:   int64(len(databaseBytes)),
		// HasSecrets stays false: an ordinary backup cannot carry one, and a
		// reader treats a true value as a refusal (AC-BACKUP-001).
		HasSecrets: false,
	}

	writer := archive.NewWriter(archive.WriterOptions{})
	if err := writer.Add(DatabaseName, databaseBytes); err != nil {
		return ExportResult{}, exportError("archive", "The database could not be archived.", err)
	}
	var fileBytes int64
	for _, entry := range entries {
		content, readErr := s.source.Open(ctx, entry.Hash)
		if readErr != nil {
			return ExportResult{}, exportError("files",
				"An archived file could not be read, so the backup was stopped rather than saved incomplete.", readErr)
		}
		// The bytes are the truth: a stored size that disagrees with the content
		// means the metadata and the object drifted, which is worth failing on.
		if entry.Size > 0 && int64(len(content)) != entry.Size {
			return ExportResult{}, exportError("files",
				"A stored file did not match its recorded size.", nil)
		}
		fileBytes += int64(len(content))
		if err := writer.Add(FilesPrefix+entry.Hash, content); err != nil {
			return ExportResult{}, exportError("archive", "A file could not be archived.", err)
		}
	}
	manifest.FileBytes = fileBytes
	encodedManifest, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return ExportResult{}, exportError("manifest", "The manifest could not be written.", err)
	}
	if err := writer.Add(ManifestName, encodedManifest); err != nil {
		return ExportResult{}, exportError("manifest", "The manifest could not be written.", err)
	}
	if len(providerMetadata) > 0 {
		if err := writer.Add(ProviderMetadataName, providerMetadata); err != nil {
			return ExportResult{}, exportError("providers", "The provider settings could not be archived.", err)
		}
	}
	if err := writer.AddChecksums(); err != nil {
		return ExportResult{}, exportError("checksums", "The archive checksums could not be written.", err)
	}
	data, err := writer.Finish()
	if err != nil {
		return ExportResult{}, exportError("archive", "The archive could not be finalised.", err)
	}
	return ExportResult{Manifest: manifest, Bytes: data}, nil
}

// RestoreService reads an archive.
type RestoreService struct {
	sink   Sink
	limits readerLimits
}

// NewRestoreService builds the reader.
func NewRestoreService(sink Sink) *RestoreService {
	return &RestoreService{sink: sink, limits: defaultReaderLimits()}
}

// Available reports whether the service can run.
func (r *RestoreService) Available() bool { return r != nil && r.sink != nil }

// Restore validates an archive and stages its contents.
//
// The pipeline is ARCHITECTURE §16's: open with limits, validate the manifest,
// verify checksums, stage, then open the staged database and check it. Nothing
// live is touched; the caller promotes the staged result only after this
// returns successfully, which is what makes a failed restore a no-op
// (AC-BACKUP-002).
func (r *RestoreService) Restore(ctx context.Context, data []byte) (RestoreResult, error) {
	if !r.Available() {
		return RestoreResult{}, unavailableError()
	}
	if len(data) == 0 {
		return RestoreResult{}, importError("archive", "The archive is empty.", nil)
	}
	if int64(len(data)) > r.limits.maxBytes {
		return RestoreResult{}, apperror.New(
			archive.CodeTooLarge, "security", false, "The archive is too large to open.", nil)
	}
	reader, err := archive.Open(data, archive.Limits{})
	if err != nil {
		// The archive package already classified the refusal.
		return RestoreResult{}, err
	}
	if !reader.Has(ManifestName) {
		return RestoreResult{}, importError("manifest", "The archive has no manifest.", nil)
	}
	rawManifest, err := reader.Read(ManifestName)
	if err != nil {
		return RestoreResult{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(rawManifest, &manifest); err != nil {
		return RestoreResult{}, importError("manifest", "The archive manifest could not be read.", err)
	}
	if err := validateManifest(manifest); err != nil {
		return RestoreResult{}, err
	}
	// The checksums cover every entry, so a byte that changed after the writer
	// finished is caught before anything is staged.
	if err := archive.VerifyChecksums(reader); err != nil {
		return RestoreResult{}, err
	}
	if !reader.Has(DatabaseName) {
		return RestoreResult{}, importError("database", "The archive does not contain a database.", nil)
	}
	databaseBytes, err := reader.Read(DatabaseName)
	if err != nil {
		return RestoreResult{}, err
	}

	result := RestoreResult{Manifest: manifest}
	// A secret-shaped string in an ordinary archive means the archive is not
	// ordinary; it is refused rather than imported (AC-BACKUP-001).
	result.SyntheticSecretsFound = scanForSecretShapes(rawManifest, databaseBytes)
	if len(result.SyntheticSecretsFound) > 0 {
		return RestoreResult{}, importError("secrets",
			"The archive contains credential material, which an ordinary backup must not.", nil)
	}

	stagedDatabase, err := r.sink.StageDatabase(ctx, databaseBytes)
	if err != nil {
		return RestoreResult{}, importError("database", "The database could not be staged.", err)
	}
	result.DatabasePath = stagedDatabase
	schemaVersion, err := r.sink.VerifyDatabase(ctx, stagedDatabase)
	if err != nil {
		return RestoreResult{}, importError("database",
			"The archived database could not be opened, so the restore was stopped.", err)
	}
	// A database from a newer application cannot be read by this build. The
	// migration runner would refuse it later, but refusing here keeps the
	// staging directory clean.
	if schemaVersion > manifest.SchemaVersion {
		return RestoreResult{}, importError("database",
			"The archive was written by a newer version and cannot be restored.", nil)
	}

	for _, entry := range reader.Entries() {
		if entry.IsDir || !strings.HasPrefix(entry.Name, FilesPrefix) {
			continue
		}
		hash := strings.TrimPrefix(entry.Name, FilesPrefix)
		if !isLowerHex64(hash) {
			return RestoreResult{}, importError("files", "The archive contains an object with an invalid name.", nil)
		}
		content, readErr := reader.Read(entry.Name)
		if readErr != nil {
			return RestoreResult{}, readErr
		}
		// The entry name is the content hash, so the bytes can be checked
		// against the name they were stored under: a mismatch means the archive
		// is internally inconsistent.
		sum := sha256.Sum256(content)
		if hex.EncodeToString(sum[:]) != hash {
			return RestoreResult{}, importError("files",
				"An archived file does not match the name it was stored under.", nil)
		}
		if err := r.sink.StageFile(ctx, FileEntry{Hash: hash, Size: int64(len(content))}, content); err != nil {
			return RestoreResult{}, importError("files", "A file could not be staged.", err)
		}
		result.Files++
	}
	// The manifest's counts are compared with what actually arrived, so a
	// truncated archive is a refusal rather than a partial restore.
	if manifest.Files != result.Files {
		return RestoreResult{}, importError("files",
			fmt.Sprintf("The archive promised %d files and contained %d.", manifest.Files, result.Files), nil)
	}
	return result, nil
}

// validateManifest refuses an archive this build cannot read.
func validateManifest(manifest Manifest) error {
	if manifest.ManifestVersion != SupportedManifestVersion {
		return importError("manifest",
			"That backup was written by a different version and cannot be restored.", nil)
	}
	if manifest.App != "infinite-canvas" {
		return importError("manifest", "That file is not an Infinite Atelier backup.", nil)
	}
	if manifest.HasSecrets {
		// The writer asserted the archive carries secrets. This is the ordinary
		// path, which never imports one.
		return importError("secrets", "That is a sensitive backup, which this build does not restore.", nil)
	}
	if manifest.SchemaVersion <= 0 {
		return importError("manifest", "The archive does not record a database version.", nil)
	}
	return nil
}

// secretShapes are prefixes that identify credential material. They are the
// same patterns the security scanner uses, so a key that would be caught in a
// source file is caught in an archive too.
var secretShapes = []string{"sk-", "AKIA", "ghp_", "gho_", "xoxb-", "-----BEGIN "}

// scanForSecretShapes reports which credential shapes appear in the archived
// bytes.
//
// It searches the database snapshot and the manifest because those are the
// places a key could hide: the media files are opaque bytes, and the provider
// metadata is written from a shape that has no secret field. Finding one is
// reported by shape only, never by value, so the report cannot leak what it
// found.
func scanForSecretShapes(manifest, database []byte) []string {
	found := make([]string, 0, 2)
	seen := map[string]bool{}
	for _, payload := range [][]byte{manifest, database} {
		// The database is binary; searching it as text is still meaningful
		// because a leaked key would be stored as a text column value.
		haystack := string(payload)
		for _, shape := range secretShapes {
			if seen[shape] {
				continue
			}
			if strings.Contains(haystack, shape) {
				seen[shape] = true
				found = append(found, shape)
			}
		}
	}
	return found
}

// isLowerHex64 reports whether a value is a 64-character lowercase hex digest.
func isLowerHex64(value string) bool {
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

// unavailableError is the fail-closed error for an unattached service.
func unavailableError() error {
	return apperror.New("BACKUP_UNAVAILABLE", "storage", false, "The backup service is unavailable.", nil)
}

// exportError names the export stage that failed.
func exportError(stage, message string, cause error) error {
	return apperror.New("BACKUP_EXPORT_FAILED", "storage", false, message+" (stage "+stage+")", cause)
}

// importError names the restore stage that failed.
func importError(stage, message string, cause error) error {
	return apperror.New("BACKUP_RESTORE_FAILED", "security", false, message+" (stage "+stage+")", cause)
}

// readDatabaseBytes reads the snapshot file into memory.
//
// The archive is assembled in memory because the ZIP writer this package uses
// is byte-oriented, and the alternative (streaming both the database and the
// media through a growing buffer) would complicate the ordering for no
// practical gain: the archive size is already capped by MaxArchiveBytes.
func readDatabaseBytes(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return data, nil
}
