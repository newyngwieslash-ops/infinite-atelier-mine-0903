package desktop

import (
	"context"
	"encoding/base64"
	"sync"

	appbackup "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/backup"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

// BackupBinding is the narrow Wails surface for the ordinary backup.
//
// An ordinary backup carries no secret material by construction: the database
// holds references only and this path never reads the credential store
// (ADR-0006 §9). The archive crosses the binding as base64 because that is the
// only shape a Wails call can carry; the export is bounded by the same size
// ceiling the reader applies, so one call cannot allocate without limit.
type BackupBinding struct {
	mu      sync.RWMutex
	ctx     context.Context
	export  *appbackup.ExportService
	restore *appbackup.RestoreService
	// sink stages a restore, so the caller can inspect the result before anything
	// live is replaced.
	sink appbackup.Sink
}

// AttachBackup supplies the services. A nil service leaves the binding
// unattached, and every method then fails closed.
func AttachBackup(binding *BackupBinding, ctx context.Context, export *appbackup.ExportService, restore *appbackup.RestoreService, sink appbackup.Sink) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.ctx = ctx
	binding.export = export
	binding.restore = restore
	binding.sink = sink
	binding.mu.Unlock()
}

func (b *BackupBinding) context() context.Context {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.ctx == nil {
		return context.Background()
	}
	return b.ctx
}

// ExportBackup writes an ordinary backup and returns it as base64.
//
// The frontend saves the bytes through its own download path, so no filesystem
// location is chosen here: the binding never writes a user-visible file.
func (b *BackupBinding) ExportBackup() (string, error) {
	b.mu.RLock()
	export := b.export
	b.mu.RUnlock()
	if export == nil {
		return "", bindingUnavailable()
	}
	result, err := export.Export(b.context())
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(result.Bytes), nil
}

// BackupPreview is what a restore would do, reported before it is applied.
type BackupPreview struct {
	ManifestVersion int    `json:"manifestVersion"`
	AppVersion      string `json:"appVersion"`
	SchemaVersion   int    `json:"schemaVersion"`
	CreatedAt       string `json:"createdAt"`
	Projects        int    `json:"projects"`
	Assets          int    `json:"assets"`
	Files           int    `json:"files"`
	DatabaseBytes   int64  `json:"databaseBytes"`
	FileBytes       int64  `json:"fileBytes"`
	StagedFiles     int    `json:"stagedFiles"`
	StagedDatabase  string `json:"stagedDatabase"`
}

// PreviewBackup validates an archive and stages it without touching live data.
//
// This is the restore's first half: everything the acceptance criteria ask for —
// path, size, ratio, manifest, checksum and database checks — happens here, and
// a failure leaves the running application exactly as it was.
func (b *BackupBinding) PreviewBackup(archiveBase64 string) (BackupPreview, error) {
	b.mu.RLock()
	restore := b.restore
	b.mu.RUnlock()
	if restore == nil {
		return BackupPreview{}, bindingUnavailable()
	}
	data, err := decodeBackup(archiveBase64)
	if err != nil {
		return BackupPreview{}, err
	}
	result, err := restore.Restore(b.context(), data)
	if err != nil {
		return BackupPreview{}, err
	}
	return BackupPreview{
		ManifestVersion: result.Manifest.ManifestVersion,
		AppVersion:      result.Manifest.AppVersion,
		SchemaVersion:   result.Manifest.SchemaVersion,
		CreatedAt:       result.Manifest.CreatedAt,
		Projects:        result.Manifest.Projects,
		Assets:          result.Manifest.Assets,
		Files:           result.Manifest.Files,
		DatabaseBytes:   result.Manifest.DatabaseBytes,
		FileBytes:       result.Manifest.FileBytes,
		StagedFiles:     result.Files,
		StagedDatabase:  result.DatabasePath,
	}, nil
}

// decodeBackup decodes the archive and enforces the size ceiling before the
// decoder allocates.
func decodeBackup(value string) ([]byte, error) {
	if value == "" {
		return nil, bindingInvalidInput()
	}
	// The base64 form is a third larger than the decoded value, so the encoded
	// length is checked first: decoding an unbounded string would allocate the
	// thing the ceiling exists to prevent.
	if int64(len(value)) > appbackup.MaxArchiveBytes/3*4 {
		return nil, apperror.New("BACKUP_TOO_LARGE", "security", false,
			"That backup is too large to open.", nil)
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, bindingInvalidInput()
	}
	return decoded, nil
}
