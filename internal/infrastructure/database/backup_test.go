package database

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/backup"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/archive"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/filestore"
)

// backupFixture builds a store over a real database, a real FileStore and a
// private temp area, which is the arrangement the application uses.
func backupFixture(t *testing.T) (*BackupStore, *filestore.Store, *FileRepository, *ProjectRepository) {
	t.Helper()
	handle := openWP04Handle(t)
	root := t.TempDir()
	store, err := filestore.New(filepath.Join(root, "files"), filepath.Join(root, "temp"))
	if err != nil {
		t.Fatal(err)
	}
	backupStore := NewBackupStore(handle.SQL(), filepath.Join(root, "files"),
		filepath.Join(root, "temp"), filepath.Join(root, "snapshots"), "1.0.0-test")
	return backupStore, store, NewFileRepository(handle.SQL()), NewProjectRepository(handle.SQL())
}

// TestBackupExportAgainstRealStorage is the end-to-end export over a real
// database and a real FileStore.
func TestBackupExportAgainstRealStorage(t *testing.T) {
	backupStore, files, filesRepo, projectsRepo := backupFixture(t)
	ctx := context.Background()

	// A project, so the counts are non-zero.
	record := sampleProject(t, newTestIDGenerator())
	if err := projectsRepo.CreateProject(ctx, record); err != nil {
		t.Fatal(err)
	}
	// Two stored objects, one referenced by a project-owned file link so the
	// archive is not just a database with orphan bytes.
	imageHash := putObject(t, files, filesRepo, "note.png", []byte("\x89PNG\r\n\x1a\nimage payload"))
	videoHash := putObject(t, files, filesRepo, "clip.mp4", []byte{0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p',
		'i', 's', 'o', 'm', 0x00, 0x00, 0x02, 0x00, 'm', 'p', '4', '1', 'i', 's', 'o', 'm'})

	export := backup.NewExportService(backup.ExportOptions{
		Source: backupStore, AppVersion: "1.0.0-test", WorkDir: t.TempDir(),
		Clock: realClock{},
	})
	result, err := export.Export(ctx)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Manifest.SchemaVersion != 4 {
		t.Fatalf("schema version = %d, want the applied head", result.Manifest.SchemaVersion)
	}
	if result.Manifest.Projects != 1 {
		t.Fatalf("projects = %d, want 1", result.Manifest.Projects)
	}
	if result.Manifest.Files != 2 {
		t.Fatalf("files = %d, want 2", result.Manifest.Files)
	}
	if result.Manifest.HasSecrets {
		t.Fatal("an ordinary backup reports secrets")
	}

	reader, err := archive.Open(result.Bytes, archive.Limits{})
	if err != nil {
		t.Fatalf("the archive does not open: %v", err)
	}
	// Both objects are present under their content hashes, byte for byte.
	for _, hash := range []string{imageHash, videoHash} {
		name := "files/" + hash
		if !reader.Has(name) {
			t.Fatalf("the archive is missing %s", hash)
		}
		content, readErr := reader.Read(name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !isLowerHexDigest(hash) {
			t.Fatalf("the object hash is not a digest: %q", hash)
		}
		if len(content) == 0 {
			t.Fatalf("object %s is empty in the archive", hash)
		}
	}
	// The checksums cover everything.
	if err := archive.VerifyChecksums(reader); err != nil {
		t.Fatalf("the archive's own checksums do not verify: %v", err)
	}
	// The archived database opens and reports the same schema version.
	databaseBytes, err := reader.Read(backup.DatabaseName)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(databaseBytes, []byte("SQLite format 3\x00")) {
		t.Fatal("the archived database is not a SQLite file")
	}
}

// TestBackupRestoreVerifiesAgainstRealDatabase proves the reader's database
// check works on a real snapshot and that a corrupt one is refused.
func TestBackupRestoreVerifiesAgainstRealDatabase(t *testing.T) {
	backupStore, _, _, _ := backupFixture(t)
	ctx := context.Background()

	path, err := backupStore.SnapshotDatabase(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("SnapshotDatabase: %v", err)
	}
	version, err := backupStore.VerifyDatabase(ctx, path)
	if err != nil {
		t.Fatalf("VerifyDatabase: %v", err)
	}
	if version != 4 {
		t.Fatalf("version = %d, want 4", version)
	}

	// A file that is not a database is refused.
	corrupt := filepath.Join(t.TempDir(), "corrupt.sqlite")
	if err := os.WriteFile(corrupt, []byte("definitely not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := backupStore.VerifyDatabase(ctx, corrupt); err == nil {
		t.Fatal("a corrupt database passed verification")
	}
}

// TestBackupOpenRejectsMangledHash proves the object reader validates the hash
// before it builds a path from it.
func TestBackupOpenRejectsMangledHash(t *testing.T) {
	backupStore, _, _, _ := backupFixture(t)
	ctx := context.Background()
	for _, bad := range []string{
		"",
		"../../etc/passwd",
		"ABCDEF" + string(make([]byte, 58)),
		"not-a-hash",
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcde",
	} {
		if _, err := backupStore.Open(ctx, bad); err == nil {
			t.Fatalf("Open(%q) succeeded, want a refusal", bad)
		}
	}
}

// realClock is the production clock.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

// putObject stores bytes through the real FileStore and records the metadata
// row the way the application's file service does. The two steps belong
// together: the FileStore owns the bytes, the repository owns the record, and
// the backup reads the repository.
func putObject(t *testing.T, store *filestore.Store, files *FileRepository, name string, content []byte) string {
	t.Helper()
	object, err := store.Put(context.Background(), name, bytes.NewReader(content))
	if err != nil {
		t.Fatalf("storing %s: %v", name, err)
	}
	if err := files.UpsertObject(context.Background(), object); err != nil {
		t.Fatalf("recording %s: %v", name, err)
	}
	return object.Hash
}
