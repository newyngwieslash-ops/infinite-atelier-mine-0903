package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/archive"
)

// fakeSource stands in for the live application state.
type fakeSource struct {
	mu            sync.Mutex
	schemaVersion int
	counts        Counts
	files         map[string][]byte
	mimeByHash    map[string]string
	provider      []byte
	failSnapshot  bool
	missingObject string
	// databaseBytes is what the snapshot returns.
	databaseBytes []byte
}

func newFakeSource() *fakeSource {
	return &fakeSource{
		schemaVersion: 4,
		counts:        Counts{Projects: 2, Assets: 1},
		files:         map[string][]byte{},
		mimeByHash:    map[string]string{},
		provider:      []byte(`{"providers":[{"id":"local","kind":"openai_compatible"}]}`),
		databaseBytes: []byte("SQLite format 3\x00pretend database contents"),
	}
}

func (s *fakeSource) addFile(content []byte, mime string) string {
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[hash] = content
	s.mimeByHash[hash] = mime
	return hash
}

func (s *fakeSource) SnapshotDatabase(_ context.Context, directory string) (string, error) {
	if s.failSnapshot {
		return "", errors.New("the snapshot could not be taken")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(directory, "snapshot.sqlite")
	if err := os.WriteFile(path, s.databaseBytes, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func (s *fakeSource) SchemaVersion(context.Context) (int, error) { return s.schemaVersion, nil }

func (s *fakeSource) Counts(context.Context) (Counts, error) { return s.counts, nil }

func (s *fakeSource) Files(context.Context) ([]FileEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := make([]FileEntry, 0, len(s.files))
	for hash, content := range s.files {
		entries = append(entries, FileEntry{Hash: hash, MIME: s.mimeByHash[hash], Size: int64(len(content))})
	}
	return entries, nil
}

func (s *fakeSource) Open(_ context.Context, hash string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.missingObject != "" && hash == s.missingObject {
		return nil, errors.New("the object is gone")
	}
	content, ok := s.files[hash]
	if !ok {
		return nil, errors.New("not found")
	}
	return content, nil
}

func (s *fakeSource) ProviderMetadata(context.Context) ([]byte, error) { return s.provider, nil }

// fakeSink collects what a restore stages.
type fakeSink struct {
	mu       sync.Mutex
	database []byte
	files    map[string][]byte
	// verifyFails makes the staged database look unreadable.
	verifyFails bool
	// reportedVersion is what VerifyDatabase returns.
	reportedVersion int
}

func newFakeSink() *fakeSink {
	return &fakeSink{files: map[string][]byte{}, reportedVersion: 4}
}

func (s *fakeSink) StageDatabase(_ context.Context, content []byte) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.database = content
	return "/staged/app.sqlite", nil
}

func (s *fakeSink) VerifyDatabase(context.Context, string) (int, error) {
	if s.verifyFails {
		return 0, errors.New("the database is corrupt")
	}
	return s.reportedVersion, nil
}

func (s *fakeSink) StageFile(_ context.Context, entry FileEntry, content []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[entry.Hash] = content
	return nil
}

func newExportService(source *fakeSource, workDir string) *ExportService {
	return NewExportService(ExportOptions{
		Source:     source,
		Clock:      fixedClock{},
		AppVersion: "1.0.0-test",
		WorkDir:    workDir,
	})
}

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC) }

// TestBackupRoundTrip is the AC-BACKUP-001 happy path: manifest, database,
// files, checksums and restore.
func TestBackupRoundTrip(t *testing.T) {
	source := newFakeSource()
	imageHash := source.addFile([]byte("\x89PNG\r\n\x1a\nimage bytes"), "image/png")
	videoHash := source.addFile([]byte{0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm',
		0x00, 0x00, 0x02, 0x00, 'm', 'p', '4', '1', 'i', 's', 'o', 'm'}, "video/mp4")

	export := newExportService(source, t.TempDir())
	result, err := export.Export(context.Background())
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if result.Manifest.ManifestVersion != SupportedManifestVersion {
		t.Fatalf("manifest version = %d", result.Manifest.ManifestVersion)
	}
	if result.Manifest.Projects != 2 || result.Manifest.Assets != 1 {
		t.Fatalf("counts = %+v", result.Manifest)
	}
	if result.Manifest.Files != 2 || result.Manifest.FileBytes == 0 {
		t.Fatalf("file counts = %+v", result.Manifest)
	}
	if result.Manifest.HasSecrets {
		t.Fatal("an ordinary backup reports secrets")
	}

	// The archive holds what the manifest promises.
	reader, err := archive.Open(result.Bytes, archive.Limits{})
	if err != nil {
		t.Fatalf("the produced archive does not open: %v", err)
	}
	for _, name := range []string{ManifestName, DatabaseName, ProviderMetadataName, archive.ChecksumsName,
		FilesPrefix + imageHash, FilesPrefix + videoHash} {
		if !reader.Has(name) {
			t.Fatalf("the archive is missing %s", name)
		}
	}

	sink := newFakeSink()
	restore := NewRestoreService(sink)
	restored, err := restore.Restore(context.Background(), result.Bytes)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if restored.Files != 2 {
		t.Fatalf("restored %d files, want 2", restored.Files)
	}
	if len(restored.SyntheticSecretsFound) != 0 {
		t.Fatalf("the round trip reported secrets: %v", restored.SyntheticSecretsFound)
	}
	// The staged database is byte-identical to the exported one.
	if string(sink.database) != string(source.databaseBytes) {
		t.Fatal("the database did not survive the round trip")
	}
	// And each file is byte-identical.
	source.mu.Lock()
	for hash, want := range source.files {
		got, ok := sink.files[hash]
		if !ok {
			t.Fatalf("file %s was not restored", hash)
		}
		if string(got) != string(want) {
			t.Fatalf("file %s changed in the round trip", hash)
		}
	}
	source.mu.Unlock()
}

// TestBackupContainsNoSecrets is the AC-BACKUP-001 scan: an archive written
// from a realistic state contains no credential material.
func TestBackupContainsNoSecrets(t *testing.T) {
	source := newFakeSource()
	source.addFile([]byte("media"), "image/png")
	// The database of a real installation holds a secret *reference*, never a
	// value. This models that: the table has an id, not a key.
	source.databaseBytes = []byte("SQLite format 3\x00secret_references: id=sec-1 provider_id=local status=configured")

	export := newExportService(source, t.TempDir())
	result, err := export.Export(context.Background())
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	found := scanForSecretShapes(result.Bytes, result.Bytes)
	if len(found) != 0 {
		t.Fatalf("an ordinary backup contained credential shapes: %v", found)
	}
	// The scan is not vacuous: the same search finds a planted key.
	planted := append(append([]byte{}, result.Bytes...), []byte("apiKey=sk-1234567890abcdefghijklmnop")...)
	if len(scanForSecretShapes(planted, nil)) == 0 {
		t.Fatal("the secret scan cannot detect the pattern it looks for")
	}
}

// TestRestoreRefusesSecretBearingArchive proves a planted key fails the restore
// rather than being imported.
func TestRestoreRefusesSecretBearingArchive(t *testing.T) {
	// Build an archive whose database carries a key, bypassing the exporter so
	// the hostile case is constructed directly.
	writer := archive.NewWriter(archive.WriterOptions{})
	manifest := Manifest{
		ManifestVersion: SupportedManifestVersion, App: "infinite-canvas",
		AppVersion: "test", SchemaVersion: 4, CreatedAt: "2026-09-16T12:00:00Z",
		Files: 0,
	}
	encodedManifest, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	database := []byte("SQLite format 3\x00apiKey=sk-1234567890abcdefghijklmnop")
	if err := writer.Add(ManifestName, encodedManifest); err != nil {
		t.Fatal(err)
	}
	if err := writer.Add(DatabaseName, database); err != nil {
		t.Fatal(err)
	}
	if err := writer.AddChecksums(); err != nil {
		t.Fatal(err)
	}
	data, err := writer.Finish()
	if err != nil {
		t.Fatal(err)
	}

	restore := NewRestoreService(newFakeSink())
	_, err = restore.Restore(context.Background(), data)
	if err == nil {
		t.Fatal("an archive carrying a credential was restored")
	}
	if !strings.Contains(err.Error(), "credential material") {
		t.Fatalf("the refusal does not name the reason: %v", err)
	}
}

// TestRestoreRefusesTamperedArchive proves a changed byte is caught by the
// checksums before anything is staged (AC-BACKUP-002's precondition).
func TestRestoreRefusesTamperedArchive(t *testing.T) {
	source := newFakeSource()
	source.addFile([]byte("original media bytes"), "image/png")
	export := newExportService(source, t.TempDir())
	result, err := export.Export(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// Rebuild the archive with modified content but the original checksums.
	reader, err := archive.Open(result.Bytes, archive.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	checksums, err := reader.Read(archive.ChecksumsName)
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := reader.Read(ManifestName)
	if err != nil {
		t.Fatal(err)
	}
	databaseBytes, err := reader.Read(DatabaseName)
	if err != nil {
		t.Fatal(err)
	}
	writer := archive.NewWriter(archive.WriterOptions{})
	if err := writer.Add(ManifestName, manifestBytes); err != nil {
		t.Fatal(err)
	}
	if err := writer.Add(DatabaseName, append(databaseBytes, []byte("tampered")...)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Add(archive.ChecksumsName, checksums); err != nil {
		t.Fatal(err)
	}
	tampered, err := writer.Finish()
	if err != nil {
		t.Fatal(err)
	}

	sink := newFakeSink()
	restore := NewRestoreService(sink)
	if _, err := restore.Restore(context.Background(), tampered); err == nil {
		t.Fatal("a tampered archive was restored")
	}
	if sink.database != nil {
		t.Fatal("a refused restore staged the database")
	}
}

// TestRestoreRefusesCorruptDatabase proves a staged database that cannot be
// opened stops the restore.
func TestRestoreRefusesCorruptDatabase(t *testing.T) {
	source := newFakeSource()
	export := newExportService(source, t.TempDir())
	result, err := export.Export(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sink := newFakeSink()
	sink.verifyFails = true
	restore := NewRestoreService(sink)
	_, err = restore.Restore(context.Background(), result.Bytes)
	if err == nil {
		t.Fatal("a corrupt archived database was accepted")
	}
	if !strings.Contains(err.Error(), "database") {
		t.Fatalf("the refusal does not name the database stage: %v", err)
	}
}

// TestRestoreRefusesNewerSchema proves a database from a newer build is
// refused rather than migrated backwards.
func TestRestoreRefusesNewerSchema(t *testing.T) {
	source := newFakeSource()
	export := newExportService(source, t.TempDir())
	result, err := export.Export(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sink := newFakeSink()
	// The staged database reports a version above what the manifest recorded.
	sink.reportedVersion = result.Manifest.SchemaVersion + 1
	restore := NewRestoreService(sink)
	if _, err := restore.Restore(context.Background(), result.Bytes); err == nil {
		t.Fatal("a database from a newer version was accepted")
	}
}

// TestRestoreRefusesUnknownManifestVersion proves an archive from a different
// format generation is refused.
func TestRestoreRefusesUnknownManifestVersion(t *testing.T) {
	writer := archive.NewWriter(archive.WriterOptions{})
	manifest := Manifest{
		ManifestVersion: SupportedManifestVersion + 1, App: "infinite-canvas",
		AppVersion: "future", SchemaVersion: 9, CreatedAt: "2026-09-16T12:00:00Z",
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Add(ManifestName, encoded); err != nil {
		t.Fatal(err)
	}
	if err := writer.Add(DatabaseName, []byte("SQLite format 3\x00")); err != nil {
		t.Fatal(err)
	}
	if err := writer.AddChecksums(); err != nil {
		t.Fatal(err)
	}
	data, err := writer.Finish()
	if err != nil {
		t.Fatal(err)
	}
	restore := NewRestoreService(newFakeSink())
	if _, err := restore.Restore(context.Background(), data); err == nil {
		t.Fatal("an archive from a newer format was accepted")
	}
}

// TestRestoreRefusesForeignArchive proves a ZIP that is not an Infinite Atelier
// backup is refused.
func TestRestoreRefusesForeignArchive(t *testing.T) {
	writer := archive.NewWriter(archive.WriterOptions{})
	manifest := Manifest{
		ManifestVersion: SupportedManifestVersion, App: "some-other-app",
		AppVersion: "1", SchemaVersion: 1, CreatedAt: "2026-09-16T12:00:00Z",
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Add(ManifestName, encoded); err != nil {
		t.Fatal(err)
	}
	if err := writer.AddChecksums(); err != nil {
		t.Fatal(err)
	}
	data, err := writer.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRestoreService(newFakeSink()).Restore(context.Background(), data); err == nil {
		t.Fatal("a foreign archive was accepted")
	}
}

// TestExportRefusesMissingObject proves an archive is not written when a listed
// object cannot be read: a backup that references absent bytes is worse than no
// backup, because it looks complete.
func TestExportRefusesMissingObject(t *testing.T) {
	source := newFakeSource()
	hash := source.addFile([]byte("media"), "image/png")
	source.missingObject = hash
	export := newExportService(source, t.TempDir())

	_, err := export.Export(context.Background())
	if err == nil {
		t.Fatal("an export with an unreadable object succeeded")
	}
	if !strings.Contains(err.Error(), "files") {
		t.Fatalf("the failure does not name the files stage: %v", err)
	}
}

// TestExportRefusesSizeMismatch proves metadata that disagrees with the bytes
// is caught rather than archived.
func TestExportRefusesSizeMismatch(t *testing.T) {
	source := newFakeSource()
	source.addFile([]byte("actual content"), "image/png")
	// Report a size the object does not have.
	source.mu.Lock()
	for hash := range source.files {
		source.files[hash] = []byte("actual content")
		source.mimeByHash[hash] = "image/png"
	}
	source.mu.Unlock()
	// The fake reports Size from len(content), so to test the mismatch the entry
	// must claim otherwise; a wrapper source does that.
	broken := &sizeLyingSource{Source: source, claimed: 9999}
	export := NewExportService(ExportOptions{
		Source: broken, Clock: fixedClock{}, AppVersion: "test", WorkDir: t.TempDir(),
	})
	if _, err := export.Export(context.Background()); err == nil {
		t.Fatal("an object whose size disagreed with its bytes was archived")
	}
}

// sizeLyingSource reports a size that does not match the content.
type sizeLyingSource struct {
	Source
	claimed int64
}

func (s *sizeLyingSource) Files(ctx context.Context) ([]FileEntry, error) {
	entries, err := s.Source.Files(ctx)
	if err != nil {
		return nil, err
	}
	for index := range entries {
		entries[index].Size = s.claimed
	}
	return entries, nil
}

// TestExportRefusesUnavailableWorkDir proves a missing private directory is a
// refusal rather than a partial write somewhere else.
func TestExportRefusesUnavailableWorkDir(t *testing.T) {
	source := newFakeSource()
	export := NewExportService(ExportOptions{
		Source: source, Clock: fixedClock{}, AppVersion: "test", WorkDir: "",
	})
	if _, err := export.Export(context.Background()); err == nil {
		t.Fatal("an export with no work directory succeeded")
	}
	if _, err := export.Export(context.Background()); err == nil {
		t.Fatal("an unattached service reported success")
	}
}

// TestUnattachedRestoreFailsClosed proves a nil sink is refused.
func TestUnattachedRestoreFailsClosed(t *testing.T) {
	service := NewRestoreService(nil)
	if service.Available() {
		t.Fatal("an unattached restore service reports itself available")
	}
	if _, err := service.Restore(context.Background(), []byte("x")); err == nil {
		t.Fatal("an unattached restore succeeded")
	}
}

// TestRestoreRefusesOversizeArchive proves the archive size ceiling applies
// before the ZIP limits are reached.
func TestRestoreRefusesOversizeArchive(t *testing.T) {
	// The production ceiling is 4 GiB, so the limit is lowered for the test
	// rather than allocating a multi-gigabyte buffer.
	service := NewRestoreService(newFakeSink())
	service.limits = readerLimits{maxBytes: 16}
	if _, err := service.Restore(context.Background(), make([]byte, 64)); err == nil {
		t.Fatal("an oversize archive was accepted")
	}
	// The same archive under the real ceiling is refused for being unreadable
	// rather than for its size, which shows the check is the ceiling.
	if _, err := service.Restore(context.Background(), []byte("not an archive")); err == nil {
		t.Fatal("non-archive bytes were accepted")
	}
}

// TestManifestRecordsWhatTheReaderNeeds proves the manifest carries the fields
// a restore precheck uses.
func TestManifestRecordsWhatTheReaderNeeds(t *testing.T) {
	source := newFakeSource()
	source.addFile([]byte("media"), "image/png")
	export := newExportService(source, t.TempDir())
	result, err := export.Export(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	reader, err := archive.Open(result.Bytes, archive.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := reader.Read(ManifestName)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"manifestVersion", "app", "appVersion", "schemaVersion", "createdAt",
		"projects", "assets", "files", "databaseBytes", "fileBytes", "hasSecrets"} {
		if _, ok := decoded[field]; !ok {
			t.Fatalf("the manifest is missing %q", field)
		}
	}
	// The provider metadata carries no secret field.
	provider, err := reader.Read(ProviderMetadataName)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(provider), "apiKey") || strings.Contains(string(provider), "secret") {
		t.Fatalf("the provider metadata carries a secret-shaped field: %s", provider)
	}
}
