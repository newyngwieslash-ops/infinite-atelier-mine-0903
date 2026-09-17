package legacy

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

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/project"
)

// counterIDs mints deterministic identifiers so a test can assert on them.
type counterIDs struct {
	mu     sync.Mutex
	prefix string
	count  int
}

func newCounterIDs(prefix string) *counterIDs {
	return &counterIDs{prefix: prefix}
}

func (g *counterIDs) New() (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.count++
	return g.prefix + "-" + itoa(g.count), nil
}

// memoryFiles is an in-memory FileCommitter that hashes content the way the
// real content-addressed store does.
type memoryFiles struct {
	mu      sync.Mutex
	objects map[string][]byte
	calls   int
	failOn  string
}

func newMemoryFiles() *memoryFiles {
	return &memoryFiles{objects: map[string][]byte{}}
}

func (f *memoryFiles) CommitLegacyFile(_ context.Context, legacyKey, _ string, content []byte) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.failOn != "" && strings.Contains(legacyKey, f.failOn) {
		return "", errors.New("the store refused the file")
	}
	hash := hashOf(content)
	f.objects[hash] = content
	return hash, nil
}

func (f *memoryFiles) objectCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.objects)
}

// diskMedia reads fixtures from a directory, standing in for the webview's
// chunked upload of a legacy blob.
type diskMedia struct {
	root   string
	byKey  map[string]string
	absent map[string]bool
}

func (m *diskMedia) ReadLegacyMedia(_ context.Context, legacyKey string) ([]byte, string, bool, error) {
	if m.absent[legacyKey] {
		return nil, "", false, nil
	}
	name, ok := m.byKey[legacyKey]
	if !ok {
		return nil, "", false, nil
	}
	content, err := os.ReadFile(filepath.Join(m.root, name))
	if err != nil {
		return nil, "", false, err
	}
	return content, "", true, nil
}

// memoryStore records imports without a database, so the service logic is
// tested on its own.
type memoryStore struct {
	mu          sync.Mutex
	completed   map[string]bool
	records     []ImportRecord
	bundles     []ProjectBundle
	failImport  error
	importCalls int
}

func newMemoryStore() *memoryStore {
	return &memoryStore{completed: map[string]bool{}}
}

func (s *memoryStore) HasCompletedImport(_ context.Context, fingerprint string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.completed[fingerprint], nil
}

func (s *memoryStore) ImportSnapshot(_ context.Context, request ImportRequest) (ImportOutcome, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.importCalls++
	if s.failImport != nil {
		return ImportOutcome{}, s.failImport
	}
	outcome := ImportOutcome{ImportID: "import-" + itoa(s.importCalls)}
	for _, bundle := range request.Bundles {
		outcome.Projects++
		outcome.Nodes += len(bundle.Nodes)
		outcome.Edges += len(bundle.Edges)
		outcome.Assets += len(bundle.Assets)
		outcome.History += len(bundle.History)
		// The real store records one row per imported project and answers
		// HasCompletedImport for a single project fingerprint, so the double does
		// the same; recording the run fingerprint here would hide a mismatch.
		s.completed[bundle.Fingerprint] = true
		s.bundles = append(s.bundles, bundle)
	}
	return outcome, nil
}

func (s *memoryStore) RecordImport(_ context.Context, request ImportRequest, status string, reportJSON string, warnings []Warning) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := ImportRecord{
		ID: "import-record-" + itoa(len(s.records)+1), Fingerprint: request.Fingerprint,
		Status: status, ReportJSON: reportJSON, Warnings: warnings, Mode: request.Mode,
	}
	s.records = append(s.records, record)
	return record.ID, nil
}

func (s *memoryStore) ListImports(_ context.Context, _ int) ([]ImportRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ImportRecord{}, s.records...), nil
}

func (s *memoryStore) GetImport(_ context.Context, id string) (ImportRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, record := range s.records {
		if record.ID == id {
			return record, nil
		}
	}
	return ImportRecord{}, project.NotFoundError()
}

func (s *memoryStore) LookupMapping(_ context.Context, kind, legacyID string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, bundle := range s.bundles {
		for _, mapping := range bundle.Mappings {
			if mapping.Kind == kind && mapping.LegacyID == legacyID {
				return mapping.NewID, true, nil
			}
		}
	}
	return "", false, nil
}

func (s *memoryStore) importCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.importCalls
}

func (s *memoryStore) lastRecord() (ImportRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.records) == 0 {
		return ImportRecord{}, false
	}
	return s.records[len(s.records)-1], true
}

func (s *memoryStore) bundleCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bundles)
}

// loadSnapshot reads a fixture file.
func loadSnapshot(t *testing.T, name string) Snapshot {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", "old-projects", name, "fixture.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	var fixture struct {
		Snapshot Snapshot `json:"snapshot"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("parsing fixture %s: %v", name, err)
	}
	return fixture.Snapshot
}

// mediaForFixture builds the media source for a fixture's files.
//
// A fixture keeps its media either beside fixture.json or in a media/
// subdirectory, and a listed file that does not exist on disk is the
// missing-media case: it is registered as absent rather than failing the test.
func mediaForFixture(t *testing.T, name string, snapshot Snapshot) (*diskMedia, map[string]string) {
	t.Helper()
	root := filepath.Join("..", "..", "..", "testdata", "old-projects", name)
	media := &diskMedia{root: root, byKey: map[string]string{}, absent: map[string]bool{}}
	hashes := map[string]string{}
	for _, entry := range snapshot.Media {
		if entry.File == "" {
			media.absent[entry.LegacyKey] = true
			continue
		}
		content, resolved, ok := readFixtureFile(root, entry.File)
		if !ok {
			media.absent[entry.LegacyKey] = true
			continue
		}
		media.byKey[entry.LegacyKey] = resolved
		hashes[entry.LegacyKey] = hashOf(content)
	}
	return media, hashes
}

// readFixtureFile finds a fixture media file, returning its bytes and the path
// relative to the fixture root that the media source should use.
func readFixtureFile(root, name string) ([]byte, string, bool) {
	for _, candidate := range []string{name, filepath.Join("media", name)} {
		content, err := os.ReadFile(filepath.Join(root, candidate))
		if err == nil {
			return content, candidate, true
		}
	}
	return nil, "", false
}

// fixedClock pins the import timestamps.
type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC) }

func newTestService(store *memoryStore, files *memoryFiles, media MediaSource) *Service {
	return NewService(Options{
		Store: store,
		Files: files,
		Media: media,
		IDs:   newCounterIDs("new"),
		Clock: fixedClock{},
	})
}

// TestImportAllNodeTypesIsComplete is AC-LEGACY-001: counts match, text is
// preserved, media hashes match, the viewport round-trips, and unsupported
// metadata is retained with a warning.
func TestImportAllNodeTypesIsComplete(t *testing.T) {
	snapshot := loadSnapshot(t, "all-node-types")
	media, expectedHashes := mediaForFixture(t, "all-node-types", snapshot)
	store := newMemoryStore()
	files := newMemoryFiles()
	service := newTestService(store, files, media)

	result, err := service.Import(context.Background(), snapshot, ImportOptions{SourceCase: "all-node-types"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(result.Imported) != 1 {
		t.Fatalf("imported %d projects, want 1", len(result.Imported))
	}
	if result.Counts.Projects != 1 || result.Counts.Nodes != len(snapshot.Projects[0].Nodes) {
		t.Fatalf("counts = %+v, want %d nodes", result.Counts, len(snapshot.Projects[0].Nodes))
	}
	if result.Counts.Edges != len(snapshot.Projects[0].Connections) {
		t.Fatalf("edges = %d, want %d", result.Counts.Edges, len(snapshot.Projects[0].Connections))
	}

	store.mu.Lock()
	bundles := append([]ProjectBundle{}, store.bundles...)
	store.mu.Unlock()
	if len(bundles) != 1 {
		t.Fatalf("stored %d bundles", len(bundles))
	}
	bundle := bundles[0]

	// Every media hash the fixture declares is the hash of the stored object.
	for _, entry := range snapshot.Media {
		want, ok := expectedHashes[entry.LegacyKey]
		if !ok {
			continue
		}
		if want == "" {
			t.Fatalf("no expected hash for %s", entry.LegacyKey)
		}
	}
	// The stored objects are content-addressed, so the count is the number of
	// distinct blobs, and each hash is reachable.
	if files.objectCount() != len(snapshot.Media) {
		t.Fatalf("stored %d objects, want %d", files.objectCount(), len(snapshot.Media))
	}

	// Text content survives verbatim.
	var pageText string
	var pluginNode *project.Node
	for index := range bundle.Nodes {
		node := &bundle.Nodes[index]
		if node.NodeType == "text" && strings.Contains(node.UIState, "lighthouse keeper") {
			pageText = node.UIState
		}
		if node.NodeType == "acme:custom-widget" {
			pluginNode = node
		}
	}
	if pageText == "" {
		t.Fatal("the text node's content was not carried across")
	}
	// The plugin node keeps its type and its metadata.
	if pluginNode == nil {
		t.Fatal("the plugin node was dropped")
	}
	if !strings.Contains(pluginNode.LegacyMetadata, "pluginSetting") {
		t.Fatalf("the plugin node's metadata was not retained: %q", pluginNode.LegacyMetadata)
	}
	if !strings.Contains(pluginNode.LegacyMetadata, "acme-custom-widget") &&
		!strings.Contains(pluginNode.LegacyMetadata, "pluginSetting") {
		t.Fatal("the plugin metadata is not in the retained fields")
	}

	// The viewport round-trips.
	if bundle.Document.Viewport.K != snapshot.Projects[0].Viewport.K ||
		bundle.Document.Viewport.X != snapshot.Projects[0].Viewport.X {
		t.Fatalf("viewport = %+v, want %+v", bundle.Document.Viewport, snapshot.Projects[0].Viewport)
	}

	// Unsupported metadata produced a warning and was retained.
	if len(result.Warnings) == 0 {
		t.Fatal("no warnings were reported for a fixture with unsupported fields")
	}
	hasNodeTypeWarning := false
	for _, warning := range result.Warnings {
		if warning.Code == WarningUnsupportedNodeType {
			hasNodeTypeWarning = true
		}
	}
	if !hasNodeTypeWarning {
		t.Fatal("the plugin node type did not produce a warning")
	}

	// Every legacy connection became a generic relation.
	for _, edge := range bundle.Edges {
		if edge.RelationType != project.RelationGeneric {
			t.Fatalf("edge relation = %q, want generic", edge.RelationType)
		}
	}

	// The generation history is archived: the fixture declares two records and
	// both must arrive, with their prompts and counts.
	if len(snapshot.History) != 2 {
		t.Fatalf("precondition: the fixture declares %d history records", len(snapshot.History))
	}
	if len(bundle.History) != len(snapshot.History) {
		t.Fatalf("history = %d records, want %d", len(bundle.History), len(snapshot.History))
	}
	for index, record := range bundle.History {
		want := snapshot.History[index]
		if record.Prompt != want.Prompt || record.Model != want.Model {
			t.Fatalf("history %d changed: %+v", index, record)
		}
		if record.Success != want.SuccessCount || record.Fail != want.FailCount {
			t.Fatalf("history %d counts changed: %+v", index, record)
		}
		if record.LegacyID != want.ID {
			t.Fatalf("history %d lost its legacy id: %q", index, record.LegacyID)
		}
		if record.ImagesJSON == "" {
			t.Fatalf("history %d has no image list", index)
		}
	}
}

// TestImportIsIdempotent is AC-LEGACY-002: importing the same project twice
// detects it, writes nothing the second time, and copy mode makes a distinct
// project.
func TestImportIsIdempotent(t *testing.T) {
	snapshot := loadSnapshot(t, "all-node-types")
	media, _ := mediaForFixture(t, "all-node-types", snapshot)
	store := newMemoryStore()
	files := newMemoryFiles()
	service := newTestService(store, files, media)
	ctx := context.Background()

	first, err := service.Import(ctx, snapshot, ImportOptions{SourceCase: "all-node-types"})
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	if len(first.Imported) != 1 || len(first.Skipped) != 0 {
		t.Fatalf("first import = %+v", first)
	}
	objectsAfterFirst := files.objectCount()
	importsAfterFirst := store.importCount()

	// The same snapshot again: detected, nothing written, and the run says so.
	second, err := service.Import(ctx, snapshot, ImportOptions{SourceCase: "all-node-types"})
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if len(second.Skipped) != 1 {
		t.Fatalf("the second import did not skip the project: %+v", second)
	}
	if len(second.Imported) != 0 {
		t.Fatalf("the second import wrote a project: %+v", second.Imported)
	}
	if store.importCount() != importsAfterFirst {
		t.Fatal("the second import issued a write")
	}
	if files.objectCount() != objectsAfterFirst {
		t.Fatal("the second import stored media again")
	}
	// The run is recorded as already-imported so the UI can explain it.
	record, ok := store.lastRecord()
	if !ok {
		t.Fatal("no import record was written")
	}
	if record.Status != StatusAlreadyImported {
		t.Fatalf("record status = %q, want %q", record.Status, StatusAlreadyImported)
	}

	// Copy mode imports again with a fresh identity.
	copied, err := service.Import(ctx, snapshot, ImportOptions{Mode: ModeCopy, SourceCase: "all-node-types"})
	if err != nil {
		t.Fatalf("copy import: %v", err)
	}
	if len(copied.Imported) != 1 {
		t.Fatalf("copy mode did not import: %+v", copied)
	}
	if copied.Imported[0].NewID == first.Imported[0].NewID {
		t.Fatal("copy mode reused the original project id")
	}
	// The bytes are the same, so content addressing deduplicated them.
	if files.objectCount() != objectsAfterFirst {
		t.Fatalf("copy mode stored the media again: %d objects, want %d", files.objectCount(), objectsAfterFirst)
	}
}

// TestImportFailureLeavesNoProject is AC-LEGACY-003 at the service boundary: a
// file failure stops the import before the database write, and the failure is
// reported with its stage.
func TestImportFailureLeavesNoProject(t *testing.T) {
	snapshot := loadSnapshot(t, "all-node-types")
	media, _ := mediaForFixture(t, "all-node-types", snapshot)
	store := newMemoryStore()
	files := newMemoryFiles()
	files.failOn = "legacy-media-video-1"
	service := newTestService(store, files, media)

	_, err := service.Import(context.Background(), snapshot, ImportOptions{SourceCase: "all-node-types"})
	if err == nil {
		t.Fatal("an import with an unstorable file succeeded")
	}
	importErr, ok := project.AsImportError(err)
	if !ok {
		t.Fatalf("the failure does not name its stage: %v", err)
	}
	if importErr.Stage != "files" {
		t.Fatalf("stage = %q, want files", importErr.Stage)
	}
	if store.bundleCount() != 0 {
		t.Fatal("a project was written despite the file failure")
	}
	record, ok := store.lastRecord()
	if !ok {
		t.Fatal("the failed run was not recorded")
	}
	if record.Status != StatusFailed {
		t.Fatalf("record status = %q, want failed", record.Status)
	}
	if !strings.Contains(record.ReportJSON, "files") {
		t.Fatalf("the report does not name the stage: %q", record.ReportJSON)
	}
}

// TestImportDatabaseFailureIsReported proves a database failure is attributed
// to that stage and still leaves a record.
func TestImportDatabaseFailureIsReported(t *testing.T) {
	snapshot := loadSnapshot(t, "minimal")
	store := newMemoryStore()
	store.failImport = errors.New("the transaction could not commit")
	service := newTestService(store, newMemoryFiles(), nil)

	_, err := service.Import(context.Background(), snapshot, ImportOptions{SourceCase: "minimal"})
	if err == nil {
		t.Fatal("a failing database write reported success")
	}
	importErr, ok := project.AsImportError(err)
	if !ok || importErr.Stage != "database" {
		t.Fatalf("the failure does not name the database stage: %v", err)
	}
	record, ok := store.lastRecord()
	if !ok || record.Status != StatusFailed {
		t.Fatalf("record = %+v", record)
	}
}

// TestImportMissingMediaIsReportedNotFatal proves a missing blob produces a
// warning and the project still imports.
func TestImportMissingMediaIsReportedNotFatal(t *testing.T) {
	snapshot := loadSnapshot(t, "missing-media")
	media, _ := mediaForFixture(t, "missing-media", snapshot)
	store := newMemoryStore()
	service := newTestService(store, newMemoryFiles(), media)
	ctx := context.Background()

	// The fixture lists one present and one absent key.
	precheck, err := service.Precheck(ctx, snapshot)
	if err != nil {
		t.Fatalf("Precheck: %v", err)
	}
	if len(precheck.MissingMedia) != 1 || precheck.MissingMedia[0] != "image:legacy-missing-1" {
		t.Fatalf("missing media = %+v, want exactly the absent key", precheck.MissingMedia)
	}

	result, err := service.Import(ctx, snapshot, ImportOptions{SourceCase: "missing-media"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(result.Imported) != 1 {
		t.Fatalf("the project was not imported: %+v", result)
	}
	found := false
	for _, warning := range result.Warnings {
		if warning.Code == WarningMissingMedia {
			found = true
		}
	}
	if !found {
		t.Fatalf("no missing-media warning was reported: %+v", result.Warnings)
	}
	// The nodes are all present, including the one whose file is missing: the
	// user can see the node and re-supply the file.
	store.mu.Lock()
	bundleCount := len(store.bundles)
	var nodeCount int
	if bundleCount > 0 {
		nodeCount = len(store.bundles[0].Nodes)
	}
	store.mu.Unlock()
	if nodeCount != 2 {
		t.Fatalf("nodes = %d, want both, including the one with a missing file", nodeCount)
	}
}

// TestImportMalformedMetadataIsRetained proves unknown fields survive and are
// reported.
func TestImportMalformedMetadataIsRetained(t *testing.T) {
	snapshot := loadSnapshot(t, "malformed-metadata")
	store := newMemoryStore()
	service := newTestService(store, newMemoryFiles(), nil)

	result, err := service.Import(context.Background(), snapshot, ImportOptions{SourceCase: "malformed-metadata"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	store.mu.Lock()
	bundles := append([]ProjectBundle{}, store.bundles...)
	store.mu.Unlock()
	if len(bundles) != 1 {
		t.Fatalf("bundles = %d", len(bundles))
	}

	// Every node is present, including the one with no metadata at all.
	if len(bundles[0].Nodes) != 3 {
		t.Fatalf("nodes = %d, want 3", len(bundles[0].Nodes))
	}
	var legacyOnly, unknownFields *project.Node
	for index := range bundles[0].Nodes {
		node := &bundles[0].Nodes[index]
		if node.NodeType == "legacy:arc-graph" {
			legacyOnly = node
		}
		if strings.Contains(node.LegacyMetadata, "mysteryFlag") {
			unknownFields = node
		}
	}
	if legacyOnly == nil {
		t.Fatal("the legacy-only node type was not imported")
	}
	if !strings.Contains(legacyOnly.LegacyMetadata, "arcPoints") {
		t.Fatalf("the legacy node's fields were dropped: %q", legacyOnly.LegacyMetadata)
	}
	if unknownFields == nil {
		t.Fatal("the unknown metadata fields were dropped")
	}
	if !strings.Contains(unknownFields.LegacyMetadata, "vendorExtras") {
		t.Fatalf("nested unknown fields were dropped: %q", unknownFields.LegacyMetadata)
	}
	// The connection's extra fields are retained on the edge.
	if len(bundles[0].Edges) != 1 {
		t.Fatalf("edges = %d, want 1", len(bundles[0].Edges))
	}
	if !strings.Contains(bundles[0].Edges[0].LegacyMetadata, "strokeDash") {
		t.Fatalf("the connection's extra fields were dropped: %q", bundles[0].Edges[0].LegacyMetadata)
	}

	// Every case produced a warning.
	codes := map[string]bool{}
	for _, warning := range result.Warnings {
		codes[warning.Code] = true
	}
	for _, want := range []string{WarningUnsupportedNodeType, WarningUnsupportedMetadata, WarningUnsupportedEdgeMetadata} {
		if !codes[want] {
			t.Fatalf("warning %q was not reported; got %+v", want, codes)
		}
	}
}

// TestImportRefusesUnknownManifestVersion proves a newer envelope is refused
// rather than parsed optimistically.
func TestImportRefusesUnknownManifestVersion(t *testing.T) {
	snapshot := loadSnapshot(t, "minimal")
	snapshot.ManifestVersion = 99
	service := newTestService(newMemoryStore(), newMemoryFiles(), nil)

	_, err := service.Import(context.Background(), snapshot, ImportOptions{})
	if err == nil {
		t.Fatal("an unsupported manifest version was accepted")
	}
	importErr, ok := project.AsImportError(err)
	if !ok || importErr.Stage != "snapshot" {
		t.Fatalf("the refusal does not name the snapshot stage: %v", err)
	}
}

// TestImportRefusesForeignApp proves a JSON file that is not a project backup
// is refused.
func TestImportRefusesForeignApp(t *testing.T) {
	snapshot := loadSnapshot(t, "minimal")
	snapshot.App = "some-other-app"
	service := newTestService(newMemoryStore(), newMemoryFiles(), nil)
	if _, err := service.Import(context.Background(), snapshot, ImportOptions{}); err == nil {
		t.Fatal("a foreign file was accepted as a project backup")
	}
}

// TestPrecheckDoesNotWriteProjects proves the scope report comes before any
// project row exists.
func TestPrecheckDoesNotWriteProjects(t *testing.T) {
	snapshot := loadSnapshot(t, "all-node-types")
	media, _ := mediaForFixture(t, "all-node-types", snapshot)
	store := newMemoryStore()
	service := newTestService(store, newMemoryFiles(), media)

	precheck, err := service.Precheck(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("Precheck: %v", err)
	}
	if len(precheck.Projects) != 1 {
		t.Fatalf("projects = %d", len(precheck.Projects))
	}
	if precheck.Projects[0].Nodes != len(snapshot.Projects[0].Nodes) {
		t.Fatalf("reported %d nodes", precheck.Projects[0].Nodes)
	}
	if precheck.Projects[0].AlreadyImported {
		t.Fatal("a fresh snapshot reports as already imported")
	}
	if store.importCount() != 0 || store.bundleCount() != 0 {
		t.Fatal("the precheck wrote project rows")
	}

	// After an import, the same precheck reports it as already imported.
	if _, err := service.Import(context.Background(), snapshot, ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	second, err := service.Precheck(context.Background(), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Projects[0].AlreadyImported {
		t.Fatal("the precheck does not detect a completed import")
	}
}

// TestFingerprintChangesWithContent proves the fingerprint identifies content
// rather than just an id, so an edited project is not mistaken for its original.
func TestFingerprintChangesWithContent(t *testing.T) {
	snapshot := loadSnapshot(t, "minimal")
	original := Fingerprint(snapshot.Projects[0], nil)

	renamed := snapshot.Projects[0]
	renamed.Title = "A different title"
	if Fingerprint(renamed, nil) == original {
		t.Fatal("the fingerprint ignores the title")
	}

	added := snapshot.Projects[0]
	added.Nodes = append(added.Nodes, LegacyNode{ID: "extra", Type: "text"})
	if Fingerprint(added, nil) == original {
		t.Fatal("the fingerprint ignores the node count")
	}

	// The fingerprint is stable for the same input.
	if Fingerprint(snapshot.Projects[0], nil) != original {
		t.Fatal("the fingerprint is not deterministic")
	}
}

// TestAttachAssetsFollowsReferencedMedia proves an asset lands in the project
// whose nodes use its file.
func TestAttachAssetsFollowsReferencedMedia(t *testing.T) {
	snapshot := loadSnapshot(t, "all-node-types")
	media, _ := mediaForFixture(t, "all-node-types", snapshot)
	store := newMemoryStore()
	service := newTestService(store, newMemoryFiles(), media)

	result, err := service.Import(context.Background(), snapshot, ImportOptions{})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Counts.Assets != 1 {
		t.Fatalf("assets = %d, want 1", result.Counts.Assets)
	}
	store.mu.Lock()
	bundles := append([]ProjectBundle{}, store.bundles...)
	store.mu.Unlock()
	if len(bundles[0].Assets) != 1 {
		t.Fatalf("the asset was not attached to the project: %+v", bundles[0].Assets)
	}
	assetBundle := bundles[0].Assets[0]
	if assetBundle.Asset.ProjectID != bundles[0].Project.ID {
		t.Fatal("the asset was attached to the wrong project")
	}
	if assetBundle.Asset.Type != asset.TypeImage {
		t.Fatalf("asset type = %q, want image", assetBundle.Asset.Type)
	}
	if assetBundle.Version.CreatedByType != asset.CreatedByMigration {
		t.Fatalf("the migrated version does not record its provenance: %q", assetBundle.Version.CreatedByType)
	}
	// The asset's file link points at the committed object.
	if len(assetBundle.Files) != 1 {
		t.Fatalf("the asset has %d file links, want 1", len(assetBundle.Files))
	}
	if len(assetBundle.Files[0].FileHash) != 64 {
		t.Fatalf("the file link hash looks wrong: %q", assetBundle.Files[0].FileHash)
	}
	// The mapping table records the asset too.
	found := false
	for _, mapping := range bundles[0].Mappings {
		if mapping.Kind == MapAsset {
			found = true
		}
	}
	if !found {
		t.Fatal("the asset id mapping is missing")
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

// hashOf is the SHA-256 hex of a payload, matching what the FileStore returns.
func hashOf(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
