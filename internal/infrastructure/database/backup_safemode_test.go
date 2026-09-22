package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appbackup "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/backup"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

// backup_safemode_test.go covers WP-12 item 13's recovery case: can a user whose database
// will not open still restore the backup that would fix it?
//
// # The gap this closes
//
// `composeProjects` returns nil when `handle.SQL()` is nil, and every safe-mode handle has no
// pool — the database could not be opened, failed its integrity check, or could not be
// migrated, so there is nothing to query. That is right for the project and import services,
// which ARE queries.
//
// It was wrong for the backup. A user whose database is broken is a user whose next step is
// to restore the backup that fixes it, and in that state the restore used to be unavailable:
// safe mode existed to make recovery possible and then refused the recovery.
//
// # What these tests assert
//
// They build the restore over a nil pool — the shape `composeBackupOnly` produces — and drive
// a real archive through it. The assertion is that the whole pipeline works without a
// connection: validation, staging, verification of the ARCHIVED database (which is opened
// from its own file), and the promotion's renames. The export is the half that needs the
// live database, and it is expected to fail closed here.

// safeModeStore builds a BackupStore with no connection, which is the composition a build
// whose database could not be opened uses.
func safeModeStore(t *testing.T, root string) *BackupStore {
	t.Helper()
	filesDir := filepath.Join(root, "files")
	tempDir := filepath.Join(root, "temp")
	for _, directory := range []string{filesDir, tempDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// nil pool: this is the safe-mode shape, not an oversight.
	return NewBackupStore(nil, filepath.Join(root, "studio.db"), filesDir, tempDir,
		filepath.Join(root, "snapshots"), "test")
}

// TestWPSafeModeARestoreRunsWithoutADatabase is the assertion the item exists for.
func TestWPSafeModeARestoreRunsWithoutADatabase(t *testing.T) {
	ctx := context.Background()
	// The archive is produced by a NORMAL build, so what a safe-mode build restores is a real
	// backup rather than one this test assembled to suit it.
	source := newPromoteFixture(t)
	// One object in the STORE, with its metadata row, so the archive has something in it.
	//
	// It goes through `StageFile` — the same pairing the file service performs — rather than
	// writing a file into `files/` by hand, because `Files` enumerates `file_objects` and a
	// file with no row is invisible to the export. An earlier version of this fixture did
	// exactly that and the archive came back empty, which is the assertion below reporting
	// that none of the object read paths had run.
	//
	// It is written into `filesDir` rather than through `StageFile`, because `Open` reads the
	// LIVE store's layout (`<filesDir>/<hh>/<hash>`) while `StageFile` writes into the
	// restore's staging area — the two are deliberately different directories, and using the
	// second here would produce an archive whose bytes the writer could not find.
	//
	// The name is the SHA-256 of the bytes, because the restore verifies exactly that: an
	// entry whose content does not hash to the name it was stored under is refused as an
	// internally inconsistent archive. A fixture with a made-up name therefore fails at the
	// reader, which is the check working rather than a problem with the fixture.
	body := []byte("an object")
	sum := sha256.Sum256(body)
	object := hex.EncodeToString(sum[:])
	if err := os.MkdirAll(filepath.Join(source.filesDir, object[:2]), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source.filesDir, object[:2], object), body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := source.handle.SQL().ExecContext(context.Background(),
		`INSERT INTO file_objects (hash, storage_key, mime_type, size_bytes, created_at)
		 VALUES (?, ?, 'application/octet-stream', ?, '2026-01-01T00:00:00Z')`,
		object, object, len(body)); err != nil {
		t.Fatalf("recording the object: %v", err)
	}
	exportService := appbackup.NewExportService(appbackup.ExportOptions{
		Source: source.store, Clock: realClock{}, AppVersion: "test", WorkDir: filepath.Join(source.root, "temp"),
	})
	archive, err := exportService.Export(ctx)
	if err != nil {
		t.Fatalf("producing the archive: %v", err)
	}

	// The broken build: no pool, and a database file that is not a database.
	broken := t.TempDir()
	if err := os.WriteFile(filepath.Join(broken, "studio.db"), []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := safeModeStore(t, broken)
	restore := appbackup.NewRestoreService(store)

	// THE PIPELINE THAT MUST WORK IN SAFE MODE. Validation and staging read the archive and
	// the archived database, neither of which needs the live connection.
	staged, err := restore.Restore(ctx, archive.Bytes)
	if err != nil {
		t.Fatalf("a safe-mode restore refused a valid archive: %v", err)
	}
	if staged.Files == 0 {
		t.Fatal("the safe-mode restore staged no objects, so the archive was not read")
	}
	// The archived database was opened and checked from its own file, which is the part a
	// safe-mode build must still be able to do: it is how the archive is validated.
	if staged.DatabasePath == "" {
		t.Fatal("the safe-mode restore staged no database")
	}
	if !opensAsDatabase(t, staged.DatabasePath) {
		t.Fatal("the staged database cannot be opened")
	}

	// The promotion replaces the broken file with the archived one.
	result, err := store.Promote(ctx, appbackup.PromoteRequest{Confirmed: true})
	if err != nil {
		t.Fatalf("promoting from safe mode: %v", err)
	}
	if result.RolledBack {
		t.Fatal("the promotion rolled back, so the broken database was kept")
	}
	if !opensAsDatabase(t, filepath.Join(broken, "studio.db")) {
		t.Fatal("after the restore the database still cannot be opened, so safe mode could not recover")
	}
	// And the broken file it replaced was kept: the user may still want to know what was in
	// it, and this is the only copy.
	if body := readFile(t, filepath.Join(broken, "temp", "restore-previous", "database")); body != "not a database" {
		t.Fatalf("the displaced file is %q, want the broken database", body)
	}
}

// TestWPSafeModeTheExportFailsClosedWithoutADatabase is the other half of the same boundary.
//
// The export SNAPSHOTS the live database and counts its rows, so it cannot run without one.
// A safe-mode build must therefore report it as unavailable rather than producing an archive
// of nothing — an empty backup that looked valid would be worse than a refusal, because a
// user would keep it as their safety net.
func TestWPSafeModeTheExportFailsClosedWithoutADatabase(t *testing.T) {
	store := safeModeStore(t, t.TempDir())
	export := appbackup.NewExportService(appbackup.ExportOptions{
		Source: store, Clock: realClock{}, AppVersion: "test", WorkDir: filepath.Join(t.TempDir(), "temp"),
	})
	if _, err := export.Export(context.Background()); err == nil {
		t.Fatal("a safe-mode export produced an archive")
	}
}

// TestWPSafeModeThePromotionStillRefusesWhatItShould pins that the guards are not relaxed in
// safe mode.
//
// The restore is available there precisely BECAUSE it is the recovery path, which makes it
// the last place a guard should be weakened: a build that skipped the confirmation or the
// staged-database check when it had no pool would let a user destroy the data they were
// trying to recover.
func TestWPSafeModeThePromotionStillRefusesWhatItShould(t *testing.T) {
	ctx := context.Background()
	store := safeModeStore(t, t.TempDir())

	// No staged database.
	if _, err := store.Promote(ctx, appbackup.PromoteRequest{Confirmed: true}); err == nil {
		t.Fatal("a safe-mode promotion with nothing staged was accepted")
	}
	// No confirmation.
	if _, err := store.Promote(ctx, appbackup.PromoteRequest{}); err == nil {
		t.Fatal("a safe-mode promotion with no confirmation was accepted")
	}
	// And the export's absence does not make the restore's commands panic: they answer.
	if store.HasBackupState() {
		t.Fatal("a fresh safe-mode store reports displaced state")
	}
	if err := store.DiscardPrevious(ctx); err != nil {
		t.Fatalf("discarding nothing failed: %v", err)
	}
	// A rollback with nothing to put back is a REFUSAL rather than a silent success, which is
	// the same rule the normal path follows.
	if _, err := store.Rollback(ctx); err == nil {
		t.Fatal("a safe-mode rollback with nothing to restore reported success")
	}
	// The refusals are recognisable rather than generic.
	_, err := store.Promote(ctx, appbackup.PromoteRequest{})
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("the refusal is a %T, want an application error", err)
	}
	if appErr.Code != "BACKUP_NOT_CONFIRMED" {
		t.Fatalf("the refusal carries %s, and the confirmation gate runs before the staged check", appErr.Code)
	}
}

// TestWPSafeModeTheStoreReportsItsMissingDatabase pins the fail-closed direction for the
// reads that need a connection.
//
// `SchemaVersion`, `Counts` and `Files` query the live database. With no pool they must
// REFUSE rather than answer zero: a manifest claiming nought projects and nought assets would
// be an archive that looks valid and restores to an empty application.
func TestWPSafeModeTheStoreReportsItsMissingDatabase(t *testing.T) {
	ctx := context.Background()
	store := safeModeStore(t, t.TempDir())
	if _, err := store.SchemaVersion(ctx); err == nil {
		t.Fatal("a store with no database reported a schema version")
	}
	counts, err := store.Counts(ctx)
	if err == nil {
		t.Fatalf("a store with no database reported counts: %+v", counts)
	}
	if _, err := store.Files(ctx); err == nil {
		t.Fatal("a store with no database listed objects")
	}
	// The message is a sentence a user can read rather than a driver error.
	if !strings.Contains(err.Error(), "database") && !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("the refusal reads %q, which does not say what is missing", err.Error())
	}
}
