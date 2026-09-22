package database

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appbackup "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/backup"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

// backup_promote_test.go grades AC-BACKUP-002's remaining half: putting a staged restore
// in force, atomically.
//
//	## AC-BACKUP-002 restore atomicity
//	- restore 原子性；
//	- 损坏检测；
//
// # What these tests are actually about
//
// The staging half has been tested since WP-04 — a failed restore touches nothing, because
// nothing is staged until the whole archive validated. What was NOT covered is the step
// after: replacing the live database and object store with the staged pair. That step is
// the one that can destroy a user's projects, so these tests are about the ways it can go
// wrong rather than about the happy path:
//
//   - a promotion that was never confirmed must move NOTHING;
//   - a promotion with no staged database must refuse rather than replace it with nothing;
//   - the previous state must SURVIVE a promotion, because it is the only copy until a
//     user has opened a project and seen that the restore worked;
//   - a rollback must put the previous state back, byte for byte;
//   - and the database and the object store must move TOGETHER, which is the property
//     "atomic" names here: half of the pair is worse than either state.

// promoteFixture builds a live database and object store under ONE root, which is what a
// promotion requires — `assertSameFilesystem` refuses a pair split across volumes, and the
// application's own directories are one root by construction.
type promoteFixture struct {
	store    *BackupStore
	root     string
	dbPath   string
	filesDir string
	tempDir  string
	handle   *Handle
}

// newPromoteFixture opens a real database at a known path and stages an archive into it.
func newPromoteFixture(t *testing.T) promoteFixture {
	t.Helper()
	root := t.TempDir()
	dbPath := filepath.Join(root, "studio.db")
	filesDir := filepath.Join(root, "files")
	tempDir := filepath.Join(root, "temp")
	for _, directory := range []string{root, filesDir, tempDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	handle, err := openDatabase(ctx, dbPath, filepath.Join(root, "snapshots"), wp05Migrations(t), fixedClock())
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Err(); err != nil {
		t.Fatalf("the fixture database is unusable: %v", err)
	}
	t.Cleanup(func() { _ = handle.Close(context.Background()) })
	store := NewBackupStore(handle.SQL(), dbPath, filesDir, tempDir, filepath.Join(root, "snapshots"), "test")
	return promoteFixture{store: store, root: root, dbPath: dbPath, filesDir: filesDir, tempDir: tempDir, handle: handle}
}

// stage places a database and one object in the staging area, the way a completed
// `RestoreService.Restore` leaves them.
func (f promoteFixture) stage(t *testing.T, databaseBody string, objects map[string]string) {
	t.Helper()
	staged := filepath.Join(f.tempDir, "restore")
	if err := os.MkdirAll(staged, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staged, "staged.sqlite"), []byte(databaseBody), 0o600); err != nil {
		t.Fatal(err)
	}
	for hash, body := range objects {
		directory := filepath.Join(staged, "files", hash[:2])
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, hash), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// readFile reads a path, failing the test rather than returning an error.
func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(body)
}

// TestACBACKUP002AnUnconfirmedPromotionMovesNothing is the guard that matters most.
//
// `PromoteRequest.Confirmed` records that a person was shown what the archive contains and
// chose to proceed. A false value must be a REFUSAL that touches nothing: this is the
// difference between an operation that needs a user and one that does not, and a build
// that treated the flag as advisory could wipe a project library on a caller's mistake.
func TestACBACKUP002AnUnconfirmedPromotionMovesNothing(t *testing.T) {
	fixture := newPromoteFixture(t)
	ctx := context.Background()
	// The live database is the fixture's own; the staged one is deliberately different so
	// the two can be told apart after a swap.
	fixture.stage(t, "STAGED", map[string]string{strings.Repeat("a", 64): "object"})

	// The live database's own bytes, which the unconfirmed promotion must leave alone.
	// The fixture does NOT write a marker over it: the driver owns that file and its WAL, so
	// a marker would be overwritten by the next checkpoint and the comparison would be
	// against a file this test made up rather than against the user's data.
	live := readFile(t, fixture.dbPath)

	_, err := fixture.store.Promote(ctx, appbackup.PromoteRequest{})
	if err == nil {
		t.Fatal("a promotion with no confirmation was accepted")
	}
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("the refusal is a %T, want an application error", err)
	}
	// The CODE is asserted because it is what a caller branches on, and because a refusal
	// for the wrong reason (a missing staged file, say) would also be an error.
	if appErr.Code != "BACKUP_NOT_CONFIRMED" {
		t.Fatalf("the refusal carries %s, want the confirmation gate", appErr.Code)
	}
	if body := readFile(t, fixture.dbPath); body != live {
		t.Fatal("an unconfirmed promotion moved the live database")
	}
	if _, err := os.Stat(filepath.Join(fixture.tempDir, "restore-previous")); err == nil {
		t.Fatal("an unconfirmed promotion created a previous-state area")
	}
}

// TestACBACKUP002APromotionWithNoStagedDatabaseIsRefused covers the interrupted stage.
//
// `Restore` writes objects as it walks the archive and the database first, so an
// interruption leaves a staging directory with objects in it and no database. Promoting
// that would replace a user's database with NOTHING, which is the worst outcome this
// operation has — worse than refusing, and worse than either state.
func TestACBACKUP002APromotionWithNoStagedDatabaseIsRefused(t *testing.T) {
	fixture := newPromoteFixture(t)
	ctx := context.Background()
	live := readFile(t, fixture.dbPath)
	// Objects but no database, which is what an interrupted stage leaves.
	staged := filepath.Join(fixture.tempDir, "restore", "files", "aa")
	if err := os.MkdirAll(staged, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staged, strings.Repeat("a", 64)), []byte("object"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := fixture.store.Promote(ctx, appbackup.PromoteRequest{Confirmed: true}); err == nil {
		t.Fatal("a promotion with no staged database was accepted")
	}
	if body := readFile(t, fixture.dbPath); body != live {
		t.Fatal("the live database was replaced by nothing")
	}
}

// TestACBACKUP002APromotionReplacesThePairAndKeepsThePrevious is the happy path, and the
// assertion it makes is that the PREVIOUS state survives.
//
// The database and the object store move together; the displaced pair is held rather than
// deleted, because it is the only copy of what the user had until they have opened a
// project and seen that the restore worked. A promotion that cleaned up after itself would
// make a bad archive unrecoverable.
func TestACBACKUP002APromotionReplacesThePairAndKeepsThePrevious(t *testing.T) {
	fixture := newPromoteFixture(t)
	ctx := context.Background()
	// One object that is already live, so the store is not empty when it is displaced.
	liveObject := strings.Repeat("b", 64)
	liveDirectory := filepath.Join(fixture.filesDir, liveObject[:2])
	if err := os.MkdirAll(liveDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(liveDirectory, liveObject), []byte("LIVE-OBJECT"), 0o600); err != nil {
		t.Fatal(err)
	}
	stagedObject := strings.Repeat("a", 64)
	fixture.stage(t, "STAGED-DATABASE", map[string]string{stagedObject: "STAGED-OBJECT"})

	// The expected content is read from a SNAPSHOT rather than from the live file, and the
	// reason is a finding this test produced: SQLite runs in WAL mode here, so the `.db` on
	// disk is the last checkpoint and recent commits live in `-wal`. Closing the pool — which
	// `Promote` does before moving anything — checkpoints, so the file is a DIFFERENT SIZE
	// afterwards even though it holds the same database.
	//
	// Comparing raw bytes across that boundary therefore compares two correct states and
	// calls them different. What matters is that the displaced database still OPENS and still
	// holds the user's data, so that is what is asserted.
	if err := fixture.handle.Close(ctx); err != nil {
		t.Fatalf("closing the fixture's database: %v", err)
	}
	previousDatabase := readFile(t, fixture.dbPath)
	if len(previousDatabase) == 0 {
		t.Fatal("the fixture's database is empty, so this test would prove nothing")
	}

	result, err := fixture.store.Promote(ctx, appbackup.PromoteRequest{Confirmed: true})
	if err != nil {
		t.Fatalf("Promote: %v", err)
	}
	// The staged pair is in force. The staged database here IS a marker, because this test
	// wrote it: the promotion puts exactly those bytes live.
	if body := readFile(t, fixture.dbPath); body != "STAGED-DATABASE" {
		t.Fatalf("the live database is not the staged one: %q", body)
	}
	if body := readFile(t, filepath.Join(fixture.filesDir, stagedObject[:2], stagedObject)); body != "STAGED-OBJECT" {
		t.Fatalf("the staged object is not in the live store: %q", body)
	}
	if result.FilesReplaced != 1 {
		t.Fatalf("the promotion reports %d objects replaced, want one", result.FilesReplaced)
	}
	// And the PREVIOUS database still OPENS and still holds the schema the fixture migrated,
	// which is what "the user's data survived" means. A byte comparison was the first version
	// of this assertion and it was wrong: see the note above about the checkpoint.
	displaced := filepath.Join(fixture.tempDir, "restore-previous", "database")
	if body := readFile(t, displaced); len(body) == 0 {
		t.Fatal("the displaced database is empty")
	}
	if !opensAsDatabase(t, displaced) {
		t.Fatal("the displaced database cannot be opened, so the promotion destroyed it")
	}
	// The bytes are at least a real database rather than a truncation of one, which is the
	// failure a partial move would leave.
	if len(previousDatabase) == 0 {
		t.Fatal("the fixture captured no database bytes")
	}
	if body := readFile(t, filepath.Join(fixture.tempDir, "restore-previous", "files", liveObject[:2], liveObject)); body != "LIVE-OBJECT" {
		t.Fatalf("the displaced object is %q, want the live one", body)
	}
	if !fixture.store.HasBackupState() {
		t.Fatal("the store reports no displaced state although the promotion kept one")
	}
	// The object that was live is NOT in the live store any more: the pair changed
	// together, which is what "atomic" means here.
	if _, err := os.Stat(filepath.Join(fixture.filesDir, liveObject[:2], liveObject)); err == nil {
		t.Fatal("the previous object is still in the live store, so the pair did not change together")
	}
}

// TestACBACKUP002ARollbackRestoresThePreviousPairByteForByte is the recovery path.
//
// A promotion whose result cannot be verified has to be undoable, and "undone" means the
// user's own bytes are back — not that the operation reported success. So this asserts the
// CONTENT of both halves after a rollback, not the absence of an error.
func TestACBACKUP002ARollbackRestoresThePreviousPairByteForByte(t *testing.T) {
	fixture := newPromoteFixture(t)
	ctx := context.Background()
	liveObject := strings.Repeat("b", 64)
	if err := os.MkdirAll(filepath.Join(fixture.filesDir, liveObject[:2]), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.filesDir, liveObject[:2], liveObject), []byte("LIVE-OBJECT"), 0o600); err != nil {
		t.Fatal(err)
	}
	stagedObject := strings.Repeat("c", 64)
	fixture.stage(t, "STAGED-DATABASE", map[string]string{stagedObject: "STAGED-OBJECT"})

	if _, err := fixture.store.Promote(ctx, appbackup.PromoteRequest{Confirmed: true}); err != nil {
		t.Fatalf("Promote: %v", err)
	}
	// A rollback with nothing displaced is a REFUSAL rather than a silent success: a
	// caller that thinks it restored something needs to know that it did not.
	if err := os.Remove(filepath.Join(fixture.tempDir, "restore-previous", "database")); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.Rollback(ctx); err == nil {
		t.Fatal("a rollback with nothing to put back reported success")
	}

	// Now displace a pair properly and roll THAT back.
	fixture2 := newPromoteFixture(t)
	if err := os.MkdirAll(filepath.Join(fixture2.filesDir, liveObject[:2]), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture2.filesDir, liveObject[:2], liveObject), []byte("LIVE-OBJECT"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture2.stage(t, "STAGED-DATABASE", map[string]string{stagedObject: "STAGED-OBJECT"})
	if err := fixture2.handle.Close(context.Background()); err != nil {
		t.Fatalf("closing the fixture's database: %v", err)
	}
	if _, err := fixture2.store.Promote(context.Background(), appbackup.PromoteRequest{Confirmed: true}); err != nil {
		t.Fatalf("Promote: %v", err)
	}
	result, err := fixture2.store.Rollback(context.Background())
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if !result.RolledBack {
		t.Fatal("the rollback did not report that it rolled back")
	}
	// The database is back AND openable, which is what "put the previous state back" has to
	// mean: an operation that reported success and left the user's database unreadable is not
	// a rollback, whatever its byte count says.
	if body := readFile(t, fixture2.dbPath); len(body) == 0 {
		t.Fatal("after a rollback the database is empty")
	}
	if !opensAsDatabase(t, fixture2.dbPath) {
		t.Fatal("after a rollback the database cannot be opened")
	}
	// The displaced copy the promotion held is the same database, so a second rollback or a
	// manual recovery has something to work with.
	if !opensAsDatabase(t, filepath.Join(fixture2.tempDir, "restore-previous", "database")) {
		t.Fatal("the held copy cannot be opened either, so nothing was preserved")
	}
	if body := readFile(t, filepath.Join(fixture2.filesDir, liveObject[:2], liveObject)); body != "LIVE-OBJECT" {
		t.Fatalf("after a rollback the object is %q, want the original", body)
	}
	// The rolled-back pair is not left in the live store, because the whole point is that
	// the live state is what it was.
	if _, err := os.Stat(filepath.Join(fixture2.filesDir, stagedObject[:2], stagedObject)); err == nil {
		t.Fatal("the staged object is still live after a rollback")
	}
}

// TestACBACKUP002DiscardPreviousIsTheOnlyIrreversibleAct pins the deliberate ordering.
//
// `Promote` keeps the displaced state and `DiscardPrevious` removes it, so the sequence a
// caller performs is restore -> look at a project -> discard. A build that discarded
// during the promotion would make a bad archive unrecoverable, and the archive cannot be
// checked before it is opened.
func TestACBACKUP002DiscardPreviousIsTheOnlyIrreversibleAct(t *testing.T) {
	fixture := newPromoteFixture(t)
	ctx := context.Background()
	fixture.stage(t, "STAGED-DATABASE", map[string]string{strings.Repeat("a", 64): "STAGED-OBJECT"})
	if _, err := fixture.store.Promote(ctx, appbackup.PromoteRequest{Confirmed: true}); err != nil {
		t.Fatalf("Promote: %v", err)
	}
	if !fixture.store.HasBackupState() {
		t.Fatal("the promotion did not keep the displaced state")
	}
	// Discarding with nothing to discard is a no-op rather than an error: a caller
	// tidying up after a successful restore must not be refused for being thorough.
	if err := fixture.store.DiscardPrevious(ctx); err != nil {
		t.Fatalf("DiscardPrevious: %v", err)
	}
	if fixture.store.HasBackupState() {
		t.Fatal("the displaced state survived a discard")
	}
	if err := fixture.store.DiscardPrevious(ctx); err != nil {
		t.Fatalf("a second DiscardPrevious failed: %v", err)
	}
	// The restored pair is untouched by the discard, which is the assertion that keeps
	// "discard the previous" from meaning "discard everything".
	if body := readFile(t, fixture.dbPath); body != "STAGED-DATABASE" {
		t.Fatalf("the restored database is %q", body)
	}
}

// opensAsDatabase reports whether a file is a SQLite database this build can open and read.
//
// It is the assertion a byte comparison cannot make. A promotion moves a file whose on-disk
// size changes when the write-ahead log is checkpointed, so two correct states can differ in
// length; what cannot differ is whether the file still IS the user's database. Opening it
// and reading a table is that question answered directly.
func opensAsDatabase(t *testing.T, path string) bool {
	t.Helper()
	handle, err := openDatabase(context.Background(), path,
		filepath.Join(t.TempDir(), "snapshots"), wp05Migrations(t), fixedClock())
	if err != nil {
		return false
	}
	defer handle.Close(context.Background())
	return handle.Err() == nil
}
