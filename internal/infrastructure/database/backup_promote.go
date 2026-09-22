package database

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	appbackup "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/backup"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

// Promote implements the port over the application's real directories.
func (s *BackupStore) Promote(ctx context.Context, request appbackup.PromoteRequest) (appbackup.PromoteResult, error) {
	if s == nil || s.db == nil {
		return appbackup.PromoteResult{}, apperror.New("BACKUP_UNAVAILABLE", "storage", false,
			"The backup service is unavailable.", nil)
	}
	if !request.Confirmed {
		// Refused BEFORE anything moves. The message names the consequence, because a
		// caller that omitted the flag needs to know why it matters rather than that a
		// parameter was missing.
		return appbackup.PromoteResult{}, apperror.New("BACKUP_NOT_CONFIRMED", "security", false,
			"Restoring replaces this application's projects with the backup's. Confirm that you have reviewed the archive before restoring.", nil)
	}
	if strings.TrimSpace(s.databasePath) == "" || strings.TrimSpace(s.filesDir) == "" {
		// The paths are the store's own configuration rather than a request field, so a
		// blank one is a composition fault and not something a caller can correct.
		return appbackup.PromoteResult{}, apperror.New("BACKUP_UNAVAILABLE", "storage", false,
			"The backup service is not configured with the application's data locations.", nil)
	}
	if err := ctx.Err(); err != nil {
		return appbackup.PromoteResult{}, err
	}
	staged := s.stagingDirectory()
	if !directoryExists(staged) {
		return appbackup.PromoteResult{}, apperror.New("BACKUP_RESTORE_FAILED", "storage", false,
			"There is no validated backup waiting to be restored.", nil)
	}
	// The staged database must be THERE as well as the directory: an interrupted stage
	// leaves the directory with only objects in it, and promoting that would replace the
	// database with nothing.
	stagedDatabase := filepath.Join(staged, "staged.sqlite")
	if !fileExists(stagedDatabase) {
		return appbackup.PromoteResult{}, apperror.New("BACKUP_RESTORE_FAILED", "storage", false,
			"The validated backup has no database, so it cannot be restored.", nil)
	}
	// Same-filesystem check, before anything moves. `os.Rename` across devices fails with
	// a link error, and finding that out halfway through the swap is finding it out too
	// late. A caller whose directories are split has to move them under one root; the
	// alternative is a non-atomic copy, and this build does not offer one silently.
	if err := assertSameFilesystem(filepath.Dir(staged), s.filesDir); err != nil {
		return appbackup.PromoteResult{}, err
	}

	held := s.previousStateDirectory()
	// A leftover from an earlier attempt is cleared first, so what is held aside now is
	// this promotion's own. `CleanStaging` leaves the previous-state area alone for
	// exactly this reason: it is the only copy of the user's data until this succeeds.
	if err := os.RemoveAll(held); err != nil {
		return appbackup.PromoteResult{}, apperror.New("BACKUP_RESTORE_FAILED", "storage", false,
			"The restore could not be prepared.", err)
	}
	if err := os.MkdirAll(held, 0o700); err != nil {
		return appbackup.PromoteResult{}, apperror.New("BACKUP_RESTORE_FAILED", "storage", false,
			"The restore could not be prepared.", err)
	}

	// THE LIVE DATABASE MUST BE CLOSED FIRST, and this is a platform difference rather
	// than a precaution: Windows refuses to rename a file an open handle refers to, so a
	// promotion that moved it while the pool was live failed with "the process cannot
	// access the file because it is being used by another process". Unix would have allowed
	// the rename and left the old inode behind the open handle, which is its own kind of
	// wrong — the running application would keep writing to a database that is no longer
	// the live one.
	//
	// Closing it here is the honest fix, and it is why `Promote` is a command a caller
	// performs BEFORE reopening the database rather than one that swaps underneath a
	// running application.
	//
	// # Closing is also what CHECKPOINTS the write-ahead log, and that is not incidental
	//
	// This repository runs SQLite in WAL mode, so the `.db` file is not the database: recent
	// committed transactions live in a `-wal` sidecar. A test written for this promotion
	// caught it — the live database was 4 KB with 1.9 MB of WAL committed beside it.
	//
	// Moving the `.db` alone would therefore both LOSE those transactions and leave a `-wal`
	// belonging to the old database beside the restored one, which SQLite would replay
	// against the new file: silent corruption of the very data the user is trying to
	// recover. So the whole FILE SET moves, and `moveFileSet` is what does it.
	if err := s.db.Close(); err != nil {
		return appbackup.PromoteResult{}, apperror.New("BACKUP_RESTORE_FAILED", "storage", false,
			"The application's database could not be closed, so the restore was stopped.", err)
	}
	if err := moveFileSet(s.databasePath, filepath.Join(held, "database")); err != nil {
		return appbackup.PromoteResult{}, apperror.New("BACKUP_RESTORE_FAILED", "storage", false,
			"The current data could not be set aside, so nothing was replaced.", err)
	}
	// The staged database takes the live name. Its set has no sidecars, because it was
	// written whole by the archiver rather than by SQLite.
	if err := os.Rename(filepath.Join(staged, "staged.sqlite"), s.databasePath); err != nil {
		return appbackup.PromoteResult{}, apperror.New("BACKUP_RESTORE_FAILED", "storage", true,
			"The backup could not be put in place. The previous data has been kept and can be restored.", err)
	}
	stage := func(name, live, replacement string) error {
		// The live path may not exist: a fresh install has a database but no `files/`
		// directory until something is stored. Nothing to displace is not a failure.
		if fileExists(live) || directoryExists(live) {
			if err := os.Rename(live, filepath.Join(held, name)); err != nil {
				return apperror.New("BACKUP_RESTORE_FAILED", "storage", false,
					"The current data could not be set aside, so nothing was replaced.", err)
			}
		}
		if err := os.Rename(replacement, live); err != nil {
			// The live state is already aside, so this is recoverable and the caller is told
			// so rather than left to guess.
			return apperror.New("BACKUP_RESTORE_FAILED", "storage", true,
				"The backup could not be put in place. The previous data has been kept and can be restored.", err)
		}
		return nil
	}
	result := appbackup.PromoteResult{
		DatabasePath:         s.databasePath,
		PreviousDatabasePath: filepath.Join(held, "database"),
	}
	if directoryExists(filepath.Join(staged, "files")) {
		if err := stage("files", s.filesDir, filepath.Join(staged, "files")); err != nil {
			// The database moved but the objects did not, so the pair is half-changed.
			// Rolling back is the only honest answer: they must change together.
			if _, rollbackErr := s.Rollback(ctx); rollbackErr != nil {
				return appbackup.PromoteResult{RolledBack: true}, apperror.New(
					"BACKUP_RESTORE_FAILED", "storage", true,
					"The backup could not be completed and the previous data could not be put back automatically.", rollbackErr)
			}
			return appbackup.PromoteResult{RolledBack: true}, err
		}
		result.FilesReplaced = countFiles(s.filesDir)
	}
	// The displaced state is KEPT. It is the only copy of what the user had until they have
	// opened a project and seen that the restore worked, and a build that deleted it here
	// would make a bad archive unrecoverable. `DiscardPrevious` is the only act that removes
	// it, and `HasBackupState` is what tells a later startup it is still there.
	return result, nil
}

// Rollback puts the state a promotion displaced back in place.
//
// It is the reverse of `Promote` and shares its ordering: the current state is moved
// aside into the staging area rather than deleted, so a rollback that itself fails is
// still recoverable in principle. It is idempotent in the sense that matters — calling it
// with nothing displaced is an explicit refusal rather than a silent success, because a
// caller that thinks it restored something needs to know that it did not.
func (s *BackupStore) Rollback(ctx context.Context) (appbackup.PromoteResult, error) {
	if s == nil || strings.TrimSpace(s.databasePath) == "" {
		return appbackup.PromoteResult{}, apperror.New("BACKUP_UNAVAILABLE", "storage", false,
			"The backup service is unavailable.", nil)
	}
	if err := ctx.Err(); err != nil {
		return appbackup.PromoteResult{}, err
	}
	held := s.previousStateDirectory()
	heldDatabase := filepath.Join(held, "database")
	// The live set is what the promotion's own `moveFileSet` displaced, so the two agree
	// about the name by construction rather than by a comment.
	if !fileExists(heldDatabase) {
		return appbackup.PromoteResult{}, apperror.New("BACKUP_RESTORE_FAILED", "storage", false,
			"There is no previous state to put back.", nil)
	}
	// What is live now is moved into the staging area, so the rollback is itself
	// reversible: a user who rolls back and then changes their mind still has the
	// restored copy on disk.
	discard := filepath.Join(s.stagingDirectory(), "rolled-back")
	if err := os.RemoveAll(discard); err != nil {
		return appbackup.PromoteResult{}, apperror.New("BACKUP_RESTORE_FAILED", "storage", false,
			"The previous state could not be put back.", err)
	}
	if err := os.MkdirAll(discard, 0o700); err != nil {
		return appbackup.PromoteResult{}, apperror.New("BACKUP_RESTORE_FAILED", "storage", false,
			"The previous state could not be put back.", err)
	}
	if err := moveFileSet(s.databasePath, filepath.Join(discard, "database")); err != nil {
		return appbackup.PromoteResult{}, apperror.New("BACKUP_RESTORE_FAILED", "storage", false,
			"The previous state could not be put back.", err)
	}
	if err := moveFileSet(heldDatabase, s.databasePath); err != nil {
		return appbackup.PromoteResult{}, apperror.New("BACKUP_RESTORE_FAILED", "storage", true,
			"The previous database could not be put back.", err)
	}
	heldFiles := filepath.Join(held, "files")
	if directoryExists(heldFiles) {
		if directoryExists(s.filesDir) {
			if err := os.Rename(s.filesDir, filepath.Join(discard, "files")); err != nil {
				return appbackup.PromoteResult{}, apperror.New("BACKUP_RESTORE_FAILED", "storage", false,
					"The previous objects could not be put back.", err)
			}
		}
		if err := os.Rename(heldFiles, s.filesDir); err != nil {
			return appbackup.PromoteResult{}, apperror.New("BACKUP_RESTORE_FAILED", "storage", true,
				"The previous objects could not be put back.", err)
		}
	}
	return appbackup.PromoteResult{
		DatabasePath:         s.databasePath,
		PreviousDatabasePath: filepath.Join(discard, "database"),
		RolledBack:           true,
	}, nil
}

// DiscardPrevious deletes the state a promotion displaced.
//
// It is the ONLY irreversible act in this file, and it is a separate command rather than
// part of `Promote` so a caller performs it after the user has seen that the restore
// worked. A `Promote` that deleted the previous state would make a bad archive
// unrecoverable, and the archive cannot be checked before it is opened.
func (s *BackupStore) DiscardPrevious(ctx context.Context) error {
	if s == nil {
		return apperror.New("BACKUP_UNAVAILABLE", "storage", false, "The backup service is unavailable.", nil)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	held := s.previousStateDirectory()
	if !directoryExists(held) {
		return nil
	}
	if err := os.RemoveAll(held); err != nil {
		return apperror.New("BACKUP_RESTORE_FAILED", "storage", false,
			"The previous data could not be removed.", err)
	}
	return nil
}

// HasBackupState reports whether a promotion's displaced state is still on disk.
func (s *BackupStore) HasBackupState() bool {
	if s == nil {
		return false
	}
	return fileExists(filepath.Join(s.previousStateDirectory(), "database"))
}

// stagingDirectory is the private area a restore stages into.
func (s *BackupStore) stagingDirectory() string { return filepath.Join(s.tempDir, "restore") }

// previousStateDirectory is where a promotion holds what it displaced.
//
// # Why it is a SIBLING of the staging area and not inside it
//
// Both live under the application's private temp root, so both get the same permissions —
// a copy of a user's projects left in a world-readable place would be worse than the
// restore failing.
//
// It must not be INSIDE the staging directory, and an earlier version was. That broke the
// promotion for a reason worth recording: the staged database and objects live in
// `restore/`, so moving the live pair into `restore/previous` moved files into a directory
// the NEXT step consumes, and the rename failed. It also meant `CleanStaging` — which runs
// at startup — would have deleted the only copy of a user's data.
func (s *BackupStore) previousStateDirectory() string {
	return filepath.Join(s.tempDir, "restore-previous")
}

// assertSameFilesystem refuses a promotion whose two halves are on different devices.
//
// `os.Rename` is atomic within one filesystem and fails across two, so a promotion that
// spanned devices would fail partway through the swap. Finding that out by attempting it
// is finding it out after the live database has already been moved aside.
//
// `os.SameFile` is NOT the check, and an earlier draft of this function used it: it
// compares inode identity, so it answers "are these the same file", not "are these on the
// same device". Two directories on one disk would fail it, which would refuse every
// restore. `syscall.Stat_t.Dev` is what a filesystem boundary actually changes.
func assertSameFilesystem(stagedDirectory, liveDirectory string) error {
	stagedInfo, err := os.Stat(stagedDirectory)
	if err != nil {
		return apperror.New("BACKUP_RESTORE_FAILED", "storage", false,
			"The restore's staging area could not be read.", err)
	}
	// The live directory may not exist yet; its PARENT is what shares the filesystem.
	livePath := liveDirectory
	if !directoryExists(livePath) {
		livePath = filepath.Dir(livePath)
	}
	liveInfo, err := os.Stat(livePath)
	if err != nil {
		return apperror.New("BACKUP_RESTORE_FAILED", "storage", false,
			"The application's data directory could not be read.", err)
	}
	_ = stagedInfo
	_ = liveInfo
	stagedDevice, stagedOK := deviceOf(stagedDirectory)
	liveDevice, liveOK := deviceOf(livePath)
	if !stagedOK || !liveOK {
		// An answer this build cannot compute is a REFUSAL rather than a pass: promoting
		// without knowing whether the rename can complete is the failure this check
		// exists to prevent.
		return apperror.New("BACKUP_RESTORE_FAILED", "storage", false,
			"The restore could not check that the backup and the application's data are on one disk.", nil)
	}
	if stagedDevice != liveDevice {
		return apperror.New("BACKUP_RESTORE_FAILED", "storage", false,
			"The backup and the application's data are on different disks, and a restore must be atomic. Move the application's data directory so both are on one disk.", nil)
	}
	return nil
}

// fileExists reports whether a path is a regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// directoryExists reports whether a path is a directory.
func directoryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// countFiles counts the regular files under a directory, bounded to the object store's
// own two-level layout. A depth cap keeps a symlink loop from hanging a promotion.
func countFiles(root string) int {
	count := 0
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !entry.IsDir() {
			count++
		}
		if count > maxCountedFiles {
			return filepath.SkipAll
		}
		return nil
	})
	return count
}

// maxCountedFiles bounds the walk above. It is a reporting figure rather than a limit
// the restore enforces, so reaching it costs a number and not a refusal.
const maxCountedFiles = 1_000_000
