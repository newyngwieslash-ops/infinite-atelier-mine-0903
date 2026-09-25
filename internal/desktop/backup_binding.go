package desktop

import (
	"context"
	"encoding/base64"
	"sync"

	appbackup "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/backup"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/database"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/filestore"
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
	// promoter puts a staged restore in force. It is separate from the sink because
	// the two are called at different times: staging happens while an archive is
	// being validated, and promoting happens after a user has been shown what the
	// archive contains and has confirmed.
	promoter appbackup.Promoter
	// collector answers FR-160's 「垃圾回收执行前显示将删除内容并支持取消」: a preview of the objects
	// nothing references, and a collection that removes their rows.
	collector *database.GarbageCollector
	// files is the store the collected objects' BYTES live in. The collector removes rows and this
	// removes bytes: the two are different things, and the binding is where the two halves are joined
	// because it is the component that holds both.
	files *filestore.Store
	// collecting reports whether a collection is in flight, so a cancel can be told apart from a
	// no-op and a second collection cannot start on top of the first.
	collecting bool
	cancel     context.CancelFunc
}

// AttachBackup supplies the services. A nil service leaves the binding
// unattached, and every method then fails closed.
func AttachBackup(binding *BackupBinding, ctx context.Context, export *appbackup.ExportService, restore *appbackup.RestoreService, sink appbackup.Sink, promoter appbackup.Promoter) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.ctx = ctx
	binding.export = export
	binding.restore = restore
	binding.sink = sink
	binding.promoter = promoter
	binding.mu.Unlock()
}

// AttachGarbageCollection supplies the collector and the store the collection needs.
//
// A separate method rather than another parameter on `AttachBackup`, for the reason
// `WithFinalRuleset` is separate from the storyboard checker's constructor: the two capabilities are
// independent, and a signature that made them look like a pair would invite a caller to pass one
// connection where the other was meant. A build that calls only `AttachBackup` answers the
// collection's calls as unavailable, which is a state the UI can render.
func AttachGarbageCollection(binding *BackupBinding, collector *database.GarbageCollector, files *filestore.Store) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.collector = collector
	binding.files = files
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

// PreviewBackup validates an archive and reports what restoring it would do, without
// touching live data and without leaving anything staged.
//
// This is the restore's first half: everything the acceptance criteria ask for — path,
// size, ratio, manifest, checksum and database checks — happens here, and a failure leaves
// the running application exactly as it was.
//
// # Why this DISCARDS what it staged, and RestoreBackup does not
//
// A preview answers a question, and a question must be safe to ask twice: leaving the
// staged copy behind would accumulate a full archive per preview. A restore is the answer
// to that question and needs the staged copy to survive until the confirmation, which is
// why the two are separate commands rather than one with a flag. A caller that previews and
// then restores pays for the validation twice, and that is the correct trade: the
// alternative is a command that keeps a copy of a user's whole library on disk because
// somebody looked at a dialog.
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
	// A preview answers a question rather than preparing a restore, so the
	// staged copy is discarded here: leaving it would accumulate a full archive
	// per preview.
	defer func() {
		b.mu.RLock()
		sink := b.sink
		b.mu.RUnlock()
		if cleaner, ok := sink.(interface{ CleanStaging() error }); ok {
			_ = cleaner.CleanStaging()
		}
	}()
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

// RestoreBackup validates an archive, stages it, and puts it in force.
//
// # It is ONE command rather than two, and that is the safety argument
//
// The staged copy is what a promotion consumes, and it lives in the application's private
// temp area. A caller that staged with one command and promoted with another would have to
// hold a staged archive across a user's decision — which means either deleting it when they
// change their mind, or leaving a copy of their whole library on disk until something
// cleans up. Doing both steps in one call means the archive crosses the boundary ONCE, is
// validated ONCE, and is staged only for as long as it takes to confirm and move it.
//
// # The confirmation is required, and it is not a formality
//
// `confirm` must be true, and the user's own consent is what it records. `PreviewBackup` is
// what shows them what is in the archive — how many projects, how many assets, when it was
// made — and a caller that restores without having shown them anything is restoring over
// work they never saw.
//
// # What a caller must do afterwards
//
// This replaces the database file and the object store, and it CLOSES the database to do
// it. A caller must therefore restart the application, which is also the only way to reopen
// the restored database. The result's `previousDatabasePath` names where the displaced data
// was kept, and `DiscardBackupState` is what removes it — after the user has opened a
// project and seen that the restore worked.
func (b *BackupBinding) RestoreBackup(archiveBase64 string, confirm bool) (RestoreResult, error) {
	if !confirm {
		// Refused at the boundary as well as in the service, so a caller that forgot the
		// flag hears it before the archive is decoded.
		return RestoreResult{}, apperror.New("BACKUP_NOT_CONFIRMED", "security", false,
			"Restoring replaces this application's projects with the backup's. Confirm that you have reviewed the archive before restoring.", nil)
	}
	b.mu.RLock()
	restore := b.restore
	promoter := b.promoter
	b.mu.RUnlock()
	if restore == nil || promoter == nil {
		return RestoreResult{}, bindingUnavailable()
	}
	data, err := decodeBackup(archiveBase64)
	if err != nil {
		return RestoreResult{}, err
	}
	// Stage. A failure here has touched nothing, which is the property `Restore` has had
	// since WP-04 and the reason a bad archive is a no-op rather than a partial restore.
	staged, err := restore.Restore(b.context(), data)
	if err != nil {
		return RestoreResult{}, err
	}
	promoted, err := promoter.Promote(b.context(), appbackup.PromoteRequest{Confirmed: true})
	if err != nil {
		return RestoreResult{}, err
	}
	return RestoreResult{
		ManifestVersion:      staged.Manifest.ManifestVersion,
		Projects:             staged.Manifest.Projects,
		Assets:               staged.Manifest.Assets,
		Files:                promoted.FilesReplaced,
		DatabasePath:         promoted.DatabasePath,
		PreviousDatabasePath: promoted.PreviousDatabasePath,
		RolledBack:           promoted.RolledBack,
		RestartRequired:      true,
	}, nil
}

// RestoreResult reports what a restore did.
type RestoreResult struct {
	ManifestVersion int    `json:"manifestVersion"`
	Projects        int    `json:"projects"`
	Assets          int    `json:"assets"`
	Files           int    `json:"files"`
	DatabasePath    string `json:"databasePath"`
	// PreviousDatabasePath is where the displaced data was kept. It is the only copy of what
	// the user had until they confirm the restore worked, so a caller should show it.
	PreviousDatabasePath string `json:"previousDatabasePath,omitempty"`
	// RolledBack reports that the promotion failed and the previous state was put back. A
	// caller seeing true must NOT report the restore as successful.
	RolledBack bool `json:"rolledBack"`
	// RestartRequired is always true: the database was closed so its file could be moved, and
	// only a restart reopens the restored one.
	RestartRequired bool `json:"restartRequired"`
}

// DiscardBackupState removes the data a restore displaced, once it is no longer needed.
//
// It is the only irreversible act in a restore, and it is a command of its own for that
// reason: the previous state is the only copy of a user's work until they have opened a
// project and seen that the restored data is what they wanted.
func (b *BackupBinding) DiscardBackupState() error {
	b.mu.RLock()
	promoter := b.promoter
	b.mu.RUnlock()
	if promoter == nil {
		return bindingUnavailable()
	}
	return promoter.DiscardPrevious(b.context())
}

// BackupStateHeld reports whether a restore's displaced data is still on disk.
//
// A caller asks this at startup: a true value means a restore happened and was never
// confirmed as good, so the user still has the option of going back.
func (b *BackupBinding) BackupStateHeld() (bool, error) {
	b.mu.RLock()
	promoter := b.promoter
	b.mu.RUnlock()
	if promoter == nil {
		return false, bindingUnavailable()
	}
	return promoter.HasBackupState(), nil
}

// ---------------------------------------------------------------------------
// Garbage collection (FR-160's 「垃圾回收执行前显示将删除内容并支持取消」)
// ---------------------------------------------------------------------------

// GarbageCandidateDTO is one object a collection would remove.
type GarbageCandidateDTO struct {
	Hash       string `json:"hash"`
	StorageKey string `json:"storageKey"`
	MIME       string `json:"mimeType"`
	SizeBytes  int64  `json:"sizeBytes"`
}

// GarbagePreviewDTO is the list a user decides on.
type GarbagePreviewDTO struct {
	Candidates []GarbageCandidateDTO `json:"candidates"`
	TotalBytes int64                 `json:"totalBytes"`
	// Collecting reports whether a collection is running, so a panel that reloaded mid-run does not
	// offer a second one.
	Collecting bool `json:"collecting"`
}

// GarbageCollectResultDTO reports what a collection removed.
type GarbageCollectResultDTO struct {
	Removed []GarbageCandidateDTO `json:"removed"`
	// Skipped names the objects the collection found referenced even though the preview offered them,
	// which happens when something adopts an object between the two calls.
	Skipped []string `json:"skipped"`
	// FreedBytes is what the removed objects occupied.
	FreedBytes int64 `json:"freedBytes"`
	// Cancelled reports whether a user stopped the collection part-way. The objects already removed
	// stay removed: a collection is not a transaction across the store, and pretending it were would
	// mean putting back bytes whose rows are gone.
	Cancelled bool `json:"cancelled"`
	// BytesRemoved counts the objects whose BYTES were deleted, which can be fewer than the rows
	// removed when a file was already absent — the two are reported apart because a collector that
	// could not tell them apart would report freeing space it did not free.
	BytesRemoved int `json:"bytesRemoved"`
}

// PreviewGarbageListing answers 「执行前显示将删除内容」.
//
// It is a READ and safe to call twice: nothing is removed by looking. It is separate from
// `RunGarbageCollection` for the reason `PreviewBackup` is separate from `RestoreBackup` — a preview
// answers a question, and a destructive act is the ANSWER rather than the same act with a flag.
func (b *BackupBinding) PreviewGarbageListing() (GarbagePreviewDTO, error) {
	b.mu.RLock()
	collector := b.collector
	collecting := b.collecting
	b.mu.RUnlock()
	if collector == nil {
		return GarbagePreviewDTO{}, bindingUnavailable()
	}
	preview, err := collector.Preview(b.context())
	if err != nil {
		return GarbagePreviewDTO{}, err
	}
	candidates := make([]GarbageCandidateDTO, 0, len(preview.Candidates))
	for _, candidate := range preview.Candidates {
		candidates = append(candidates, GarbageCandidateDTO{
			Hash: candidate.Hash, StorageKey: candidate.StorageKey,
			MIME: candidate.MIME, SizeBytes: candidate.SizeBytes,
		})
	}
	return GarbagePreviewDTO{Candidates: candidates, TotalBytes: preview.TotalBytes, Collecting: collecting}, nil
}

// RunGarbageCollection removes the objects nothing references.
//
// # The confirmation is a parameter, not a dialog
//
// The same shape `RestoreBackup` uses and for the same reason: the refusal happens at the boundary,
// before anything is read, so a caller that forgot the flag hears it rather than discovering it after
// a partial run. The service is the one that knows what confirming MEANS here; the binding's job is
// not to let the call through without it.
//
// # Cancellation
//
// A user can stop a collection with `CancelGarbageCollection`. The objects already removed stay
// removed — the store and the database are two things, and putting bytes back whose rows are gone
// would leave references this build cannot repair — and the result says the run was cancelled so a
// caller does not report it as complete.
func (b *BackupBinding) RunGarbageCollection(confirm bool) (GarbageCollectResultDTO, error) {
	if !confirm {
		return GarbageCollectResultDTO{}, apperror.New("GARBAGE_NOT_CONFIRMED", "security", false,
			"Collecting removes the files nothing references. Review the list before collecting.", nil)
	}
	b.mu.Lock()
	collector := b.collector
	files := b.files
	if collector == nil || files == nil {
		b.mu.Unlock()
		return GarbageCollectResultDTO{}, bindingUnavailable()
	}
	if b.collecting {
		b.mu.Unlock()
		return GarbageCollectResultDTO{}, apperror.New("GARBAGE_ALREADY_RUNNING", "conflict", false,
			"A collection is already running.", nil)
	}
	ctx, cancel := context.WithCancel(b.context())
	b.collecting = true
	b.cancel = cancel
	b.mu.Unlock()

	defer func() {
		cancel()
		b.mu.Lock()
		b.collecting = false
		b.cancel = nil
		b.mu.Unlock()
	}()

	result, err := collector.Collect(ctx)
	if err != nil {
		// A cancelled context is the user's own act rather than a fault, so it is reported as a
		// cancelled run rather than as a failure: the rows already removed are gone either way.
		if ctx.Err() != nil {
			return GarbageCollectResultDTO{Cancelled: true}, nil
		}
		return GarbageCollectResultDTO{}, err
	}
	// The BYTES are removed after the rows, and only for the objects the collection actually removed.
	// Doing it in this order means an interruption leaves a row whose file is still on disk — which
	// the next preview will offer again — rather than a file no row knows about.
	removed := make([]GarbageCandidateDTO, 0, len(result.Removed))
	bytesRemoved := 0
	for _, candidate := range result.Removed {
		if ctx.Err() != nil {
			return GarbageCollectResultDTO{
				Removed: removed, Skipped: result.Skipped, FreedBytes: result.FreedBytes,
				Cancelled: true, BytesRemoved: bytesRemoved,
			}, nil
		}
		if err := files.Remove(ctx, candidate.StorageKey); err != nil {
			return GarbageCollectResultDTO{}, err
		}
		bytesRemoved++
		removed = append(removed, GarbageCandidateDTO{
			Hash: candidate.Hash, StorageKey: candidate.StorageKey,
			MIME: candidate.MIME, SizeBytes: candidate.SizeBytes,
		})
	}
	return GarbageCollectResultDTO{
		Removed: removed, Skipped: result.Skipped,
		FreedBytes: result.FreedBytes, BytesRemoved: bytesRemoved,
	}, nil
}

// CancelGarbageCollection stops a running collection.
//
// It is a separate command rather than a flag on the run, because the user who wants it is looking at
// a progress list rather than at the call they made. It reports whether anything was running, so a
// caller can tell "stopped" from "there was nothing to stop" — a difference a button's feedback
// depends on.
func (b *BackupBinding) CancelGarbageCollection() (bool, error) {
	b.mu.RLock()
	cancel := b.cancel
	collecting := b.collecting
	b.mu.RUnlock()
	if !collecting || cancel == nil {
		return false, nil
	}
	cancel()
	return true, nil
}
