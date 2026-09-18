package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	appassets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
)

// assetStore is an in-memory double for the asset persistence port. It
// reproduces the real repository's revision and status handling, so the
// binding's validation and error mapping are tested without a database.
type assetStore struct {
	mu        sync.Mutex
	assets    map[string]asset.Asset
	versions  map[string]asset.Version
	files     map[string][]asset.File
	relations []asset.Relation
	usages    []asset.Usage
	// failNext makes the next write fail, so a storage failure is testable.
	failNext error
	// failList makes the next query fail, the way a repository reports a store
	// it cannot reach.
	failList error
}

func newAssetStore() *assetStore {
	return &assetStore{
		assets:   map[string]asset.Asset{},
		versions: map[string]asset.Version{},
		files:    map[string][]asset.File{},
	}
}

func (s *assetStore) CreateAsset(_ context.Context, record asset.Asset) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNext != nil {
		err := s.failNext
		s.failNext = nil
		return err
	}
	if _, exists := s.assets[record.ID]; exists {
		return asset.ConflictError("An asset with that id already exists.")
	}
	s.assets[record.ID] = record
	return nil
}

func (s *assetStore) GetAsset(_ context.Context, id string) (asset.Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.assets[id]
	if !ok {
		return asset.Asset{}, asset.NotFoundError()
	}
	return record, nil
}

func (s *assetStore) ListAssets(_ context.Context, filter appassets.ListFilter) ([]asset.Asset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failList != nil {
		err := s.failList
		s.failList = nil
		return nil, err
	}
	var records []asset.Asset
	for _, record := range s.assets {
		if filter.ProjectID != "" && record.ProjectID != filter.ProjectID {
			continue
		}
		if len(filter.Types) > 0 {
			matched := false
			for _, candidate := range filter.Types {
				if record.Type == candidate {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		if !filter.IncludeDeleted && !record.DeletedAt.IsZero() {
			continue
		}
		records = append(records, record)
	}
	return records, nil
}

func (s *assetStore) CountAssets(_ context.Context, projectID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, record := range s.assets {
		if record.ProjectID == projectID {
			count++
		}
	}
	return count, nil
}

func (s *assetStore) UpdateAsset(_ context.Context, record asset.Asset, expectedRevision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.assets[record.ID]
	if !ok {
		return asset.NotFoundError()
	}
	if current.Revision != expectedRevision {
		return asset.ConflictError("This asset changed in another window. Reload it and try again.")
	}
	record.Revision = expectedRevision + 1
	s.assets[record.ID] = record
	return nil
}

func (s *assetStore) CreateVersion(_ context.Context, version asset.Version) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNext != nil {
		err := s.failNext
		s.failNext = nil
		return err
	}
	for _, existing := range s.versions {
		if existing.AssetID == version.AssetID && existing.VersionNumber == version.VersionNumber {
			return asset.ConflictError("That version number is already used for this asset.")
		}
	}
	s.versions[version.ID] = version
	return nil
}

func (s *assetStore) GetVersion(_ context.Context, id string) (asset.Version, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	version, ok := s.versions[id]
	if !ok {
		return asset.Version{}, asset.NotFoundError()
	}
	return version, nil
}

func (s *assetStore) ListVersions(_ context.Context, assetID string) ([]asset.Version, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var records []asset.Version
	for _, version := range s.versions {
		if version.AssetID == assetID {
			records = append(records, version)
		}
	}
	return records, nil
}

func (s *assetStore) MaxVersionNumber(_ context.Context, assetID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	highest := 0
	for _, version := range s.versions {
		if version.AssetID == assetID && version.VersionNumber > highest {
			highest = version.VersionNumber
		}
	}
	return highest, nil
}

func (s *assetStore) UpdateVersionStatus(_ context.Context, versionID string, status asset.VersionStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	version, ok := s.versions[versionID]
	if !ok {
		return asset.NotFoundError()
	}
	version.Status = status
	s.versions[versionID] = version
	return nil
}

func (s *assetStore) AddFile(_ context.Context, file asset.File) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNext != nil {
		err := s.failNext
		s.failNext = nil
		return err
	}
	s.files[file.VersionID] = append(s.files[file.VersionID], file)
	return nil
}

func (s *assetStore) ListFiles(_ context.Context, versionID string) ([]asset.File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]asset.File{}, s.files[versionID]...), nil
}

func (s *assetStore) CountFiles(_ context.Context, versionID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.files[versionID]), nil
}

// Lineage and usage. The in-memory double keeps them in slices so a test can
// assert the round trip without a database, mirroring how the real repository
// stores them in their own tables.
func (s *assetStore) AddRelation(_ context.Context, relation asset.Relation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.relations = append(s.relations, relation)
	return nil
}

func (s *assetStore) ListRelationsFrom(_ context.Context, versionID string) ([]asset.Relation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var relations []asset.Relation
	for _, relation := range s.relations {
		if relation.SourceAssetVersionID == versionID {
			relations = append(relations, relation)
		}
	}
	return relations, nil
}

func (s *assetStore) ListRelationsTo(_ context.Context, versionID string) ([]asset.Relation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var relations []asset.Relation
	for _, relation := range s.relations {
		if relation.TargetAssetVersionID == versionID {
			relations = append(relations, relation)
		}
	}
	return relations, nil
}

func (s *assetStore) AddUsage(_ context.Context, usage asset.Usage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.usages = append(s.usages, usage)
	return nil
}

func (s *assetStore) ListUsages(_ context.Context, versionID string) ([]asset.Usage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var usages []asset.Usage
	for _, usage := range s.usages {
		if usage.AssetVersionID == versionID {
			usages = append(usages, usage)
		}
	}
	return usages, nil
}

func (s *assetStore) CountUsages(_ context.Context, versionID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, usage := range s.usages {
		if usage.AssetVersionID == versionID {
			count++
		}
	}
	return count, nil
}

// attachAssetsFixture builds a binding over the in-memory store.
func attachAssetsFixture() (*AssetsBinding, *assetStore) {
	store := newAssetStore()
	binding := &AssetsBinding{}
	AttachAssets(binding, context.Background(), appassets.NewService(appassets.Options{
		Repository: store, Clock: fixedDramaClock{}, IDs: fixedIDs(),
	}))
	return binding, store
}

// TestAssetsBindingFailsClosedWhenUnattached proves every method refuses before
// a service exists, and that a nil receiver refuses rather than panics.
func TestAssetsBindingFailsClosedWhenUnattached(t *testing.T) {
	binding := &AssetsBinding{}
	var none *AssetsBinding

	calls := []struct {
		name string
		call func() error
	}{
		{"CreateAsset", func() error { _, err := binding.CreateAsset(CreateAssetRequest{}); return err }},
		{"ListAssets", func() error { _, err := binding.ListAssets(ListAssetsRequest{}); return err }},
		{"GetAsset", func() error { _, err := binding.GetAsset("a"); return err }},
		{"AddVersion", func() error { _, err := binding.AddVersion(AddVersionRequest{}); return err }},
		{"ListVersions", func() error { _, err := binding.ListVersions("a"); return err }},
		{"ApproveVersion", func() error { _, err := binding.ApproveVersion(ApproveVersionRequest{}); return err }},
		{"AttachFile", func() error { _, err := binding.AttachFile(AttachFileRequest{}); return err }},
		{"ListFiles", func() error { _, err := binding.ListFiles("v"); return err }},
	}
	for _, call := range calls {
		if err := call.call(); err == nil {
			t.Fatalf("%s succeeded on an unattached binding", call.name)
		} else {
			assertBindingCode(t, err, "DESKTOP_BINDING_UNAVAILABLE")
		}
	}

	_, err := none.CreateAsset(CreateAssetRequest{})
	assertBindingCode(t, err, "DESKTOP_BINDING_UNAVAILABLE")
	_, err = none.ListFiles("v")
	assertBindingCode(t, err, "DESKTOP_BINDING_UNAVAILABLE")
}

// TestAssetsBindingRoundTrip proves the asset commands work end to end and that
// the DTOs carry lowerCamel JSON keys, which is the regression test for the
// WP-04 missing-JSON-tag bug.
func TestAssetsBindingRoundTrip(t *testing.T) {
	binding, _ := attachAssetsFixture()

	created, err := binding.CreateAsset(CreateAssetRequest{
		ProjectID: "p1", Type: "character", Name: "Lin", Description: "the lead",
	})
	if err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}
	if created.ID == "" || created.Revision != 1 || created.Status != "active" {
		t.Fatalf("created = %+v", created)
	}
	assertCamelKey(t, created, "projectId")
	assertCamelKey(t, created, "createdAt")
	if created.CreatedAt != "2026-09-17T09:00:00Z" {
		t.Fatalf("createdAt = %q, want the pinned clock in RFC3339", created.CreatedAt)
	}
	// The service creates the asset's first draft version; the binding reads it
	// back through the ordinary query.
	versions, err := binding.ListVersions(created.ID)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 1 || versions[0].VersionNumber != 1 || versions[0].Status != "draft" {
		t.Fatalf("versions = %+v, want the initial draft version", versions)
	}

	hash := strings.Repeat("c", 64)
	file, err := binding.AttachFile(AttachFileRequest{
		VersionID: versions[0].ID, FileHash: hash,
	})
	if err != nil {
		t.Fatalf("AttachFile: %v", err)
	}
	if file.FileHash != hash || file.Role != "primary" {
		t.Fatalf("file = %+v", file)
	}
	assertCamelKey(t, file, "fileHash")
	assertCamelKey(t, file, "versionId")

	// A malformed hash is refused before the write, by the domain's own check.
	_, err = binding.AttachFile(AttachFileRequest{VersionID: versions[0].ID, FileHash: "not-a-digest"})
	assertBindingCode(t, err, "ASSET_INVALID_INPUT")

	// The version now has a committed file, so the impact-acknowledged approval
	// is the one that succeeds.
	version, err := binding.ApproveVersion(ApproveVersionRequest{
		VersionID: versions[0].ID, ImpactAcknowledged: true,
	})
	if err != nil {
		t.Fatalf("ApproveVersion: %v", err)
	}
	if version.Status != "approved" {
		t.Fatalf("approved = %+v", version)
	}
	assertCamelKey(t, version, "assetId")
	assertCamelKey(t, version, "createdByType")

	// The asset's approved-version pointer moved with it.
	reloaded, err := binding.GetAsset(created.ID)
	if err != nil {
		t.Fatalf("GetAsset: %v", err)
	}
	if reloaded.CurrentApprovedVersionID != version.ID {
		t.Fatalf("asset = %+v, want the approved version pointer", reloaded)
	}

	added, err := binding.AddVersion(AddVersionRequest{
		AssetID: created.ID, Prompt: "a portrait in ink",
	})
	if err != nil {
		t.Fatalf("AddVersion: %v", err)
	}
	if added.VersionNumber != 2 || added.CreatedByType != "user" {
		t.Fatalf("added = %+v", added)
	}
	assertCamelKey(t, added, "versionNumber")

	files, err := binding.ListFiles(version.ID)
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(files) != 1 || files[0].FileHash != hash {
		t.Fatalf("files = %+v", files)
	}
}

// TestAssetsBindingListFiltersAndEmptyCollections proves the query filter works
// and that an empty result marshals as [].
func TestAssetsBindingListFiltersAndEmptyCollections(t *testing.T) {
	binding, store := attachAssetsFixture()

	character, err := binding.CreateAsset(CreateAssetRequest{ProjectID: "p1", Type: "character", Name: "Lin"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := binding.CreateAsset(CreateAssetRequest{ProjectID: "p1", Type: "prop", Name: "Sword"}); err != nil {
		t.Fatal(err)
	}
	if _, err := binding.CreateAsset(CreateAssetRequest{ProjectID: "p2", Type: "character", Name: "Other"}); err != nil {
		t.Fatal(err)
	}

	all, err := binding.ListAssets(ListAssetsRequest{ProjectID: "p1"})
	if err != nil {
		t.Fatalf("ListAssets: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("p1 assets = %+v, want 2", all)
	}
	assertCamelKey(t, all[0], "projectId")

	narrowed, err := binding.ListAssets(ListAssetsRequest{ProjectID: "p1", Types: []string{"character"}})
	if err != nil {
		t.Fatalf("ListAssets with a type filter: %v", err)
	}
	if len(narrowed) != 1 || narrowed[0].ID != character.ID {
		t.Fatalf("filtered assets = %+v, want the character", narrowed)
	}

	// A soft-deleted row is hidden unless the filter asks for it.
	store.mu.Lock()
	deleted := store.assets[character.ID]
	deleted.DeletedAt = time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	store.assets[character.ID] = deleted
	store.mu.Unlock()

	visible, err := binding.ListAssets(ListAssetsRequest{ProjectID: "p1", Types: []string{"character"}})
	if err != nil {
		t.Fatalf("ListAssets: %v", err)
	}
	if len(visible) != 0 {
		t.Fatalf("a soft-deleted asset is still listed: %+v", visible)
	}
	withDeleted, err := binding.ListAssets(ListAssetsRequest{
		ProjectID: "p1", Types: []string{"character"}, IncludeDeleted: true,
	})
	if err != nil {
		t.Fatalf("ListAssets including deleted: %v", err)
	}
	if len(withDeleted) != 1 {
		t.Fatalf("includeDeleted assets = %+v, want the deleted row", withDeleted)
	}
	assertCamelKey(t, withDeleted[0], "deletedAt")

	empty, err := binding.ListAssets(ListAssetsRequest{ProjectID: "absent"})
	if err != nil {
		t.Fatalf("ListAssets: %v", err)
	}
	assertEmptyJSONList(t, empty)

	emptyVersions, err := binding.ListVersions("absent")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	assertEmptyJSONList(t, emptyVersions)

	emptyFiles, err := binding.ListFiles("absent")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	assertEmptyJSONList(t, emptyFiles)
}

// TestAssetsBindingRejectsInvalidInput proves validation happens at the
// boundary, with the domain's own messages.
func TestAssetsBindingRejectsInvalidInput(t *testing.T) {
	binding, _ := attachAssetsFixture()

	_, err := binding.CreateAsset(CreateAssetRequest{ProjectID: "p1", Type: "unknown", Name: "x"})
	appErr := assertBindingCode(t, err, "ASSET_INVALID_INPUT")
	if appErr.SafeMessage != "That asset type is not recognised." {
		t.Fatalf("message = %q, want the domain's own text", appErr.SafeMessage)
	}
	if appErr.Category != "asset" {
		t.Fatalf("category = %q, want asset", appErr.Category)
	}
	if appErr.Cause != nil {
		t.Fatalf("the mapped error carries a cause: %v", appErr.Cause)
	}

	_, err = binding.CreateAsset(CreateAssetRequest{ProjectID: "p1", Type: "character", Name: "   "})
	assertBindingCode(t, err, "ASSET_INVALID_INPUT")

	// An approval without the impact acknowledgement is refused rather than
	// silently granted.
	created, err := binding.CreateAsset(CreateAssetRequest{ProjectID: "p1", Type: "character", Name: "Lin"})
	if err != nil {
		t.Fatal(err)
	}
	versions, err := binding.ListVersions(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = binding.ApproveVersion(ApproveVersionRequest{VersionID: versions[0].ID})
	assertBindingCode(t, err, "ASSET_INVALID_INPUT")

	// An unknown asset is the domain's not-found category.
	_, err = binding.GetAsset("absent")
	assertBindingCode(t, err, "ASSET_NOT_FOUND")
}

// TestAssetsBindingBoundsCollectionArguments proves the type filter is bounded
// before the service is called.
func TestAssetsBindingBoundsCollectionArguments(t *testing.T) {
	binding, _ := attachAssetsFixture()

	types := make([]string, maxBatchAssetTypes+1)
	for index := range types {
		types[index] = "character"
	}
	_, err := binding.ListAssets(ListAssetsRequest{ProjectID: "p1", Types: types})
	assertBindingCode(t, err, "DESKTOP_BINDING_INVALID_INPUT")
}

// TestAssetsBindingClampsPageSize proves an omitted or oversize page size is
// clamped rather than passed through, so one query cannot ask for an unbounded
// page.
func TestAssetsBindingClampsPageSize(t *testing.T) {
	if got := clampPageSize(0); got != 200 {
		t.Fatalf("clampPageSize(0) = %d, want the default page", got)
	}
	if got := clampPageSize(-5); got != 200 {
		t.Fatalf("clampPageSize(-5) = %d, want the default page", got)
	}
	if got := clampPageSize(maxPageSize + 1); got != maxPageSize {
		t.Fatalf("clampPageSize(oversize) = %d, want the ceiling", got)
	}
	if got := clampPageSize(50); got != 50 {
		t.Fatalf("clampPageSize(50) = %d, want 50", got)
	}
}

// TestAssetsBindingPassesThroughApplicationError proves an error the
// infrastructure already classified keeps its own stable code.
func TestAssetsBindingPassesThroughApplicationError(t *testing.T) {
	binding, store := attachAssetsFixture()
	store.failList = apperror.New("ASSET_READ_FAILED", "storage", false,
		"The assets could not be read.", errors.New("driver text that must not surface"))

	_, err := binding.ListAssets(ListAssetsRequest{ProjectID: "p1"})
	appErr := assertBindingCode(t, err, "ASSET_READ_FAILED")
	if strings.Contains(appErr.Error(), "driver text") {
		t.Fatalf("the application error leaked its cause: %q", appErr.Error())
	}
}

// TestAssetsBindingMapsUnknownErrorToRequestFailed proves an error the domain
// does not own still arrives as a stable asset code.
func TestAssetsBindingMapsUnknownErrorToRequestFailed(t *testing.T) {
	appErr := assertBindingCode(t, toAssetError(errors.New("an unmapped failure")), "ASSET_REQUEST_FAILED")
	if appErr.Category != "asset" {
		t.Fatalf("category = %q, want asset", appErr.Category)
	}
	if toAssetError(nil) != nil {
		t.Fatal("toAssetError turned a nil error into one")
	}
}

// TestAssetsBindingCarriesNoPathsOrBytes proves the transport views hold no
// filesystem location and no media payload: a file link is a content hash, a
// role and a position.
func TestAssetsBindingCarriesNoPathsOrBytes(t *testing.T) {
	binding, _ := attachAssetsFixture()

	created, err := binding.CreateAsset(CreateAssetRequest{ProjectID: "p1", Type: "image", Name: "Panel"})
	if err != nil {
		t.Fatal(err)
	}
	versions, err := binding.ListVersions(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("d", 64)
	file, err := binding.AttachFile(AttachFileRequest{VersionID: versions[0].ID, FileHash: hash})
	if err != nil {
		t.Fatal(err)
	}

	// The file DTO has no field that could carry bytes or a path: its whole
	// content is the hash, the role, the ordinal and the timestamp.
	encoded, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 5 {
		t.Fatalf("the file view carries %d fields: %s", len(decoded), encoded)
	}
	for _, key := range []string{"versionId", "fileHash", "role", "ordinal", "createdAt"} {
		if _, present := decoded[key]; !present {
			t.Fatalf("the file view is missing %q: %s", key, encoded)
		}
	}
	for key, raw := range decoded {
		text, isText := raw.(string)
		if !isText {
			continue
		}
		if strings.ContainsAny(text, `/\`) || strings.HasPrefix(text, "http") {
			t.Fatalf("field %q looks like a location: %q", key, text)
		}
	}
}

// TestAssetsDTOsDeclareCamelCaseJSONTags walks every asset transport type and
// requires each exported field to declare a lowerCamel JSON key, which is the
// structural regression test for the WP-04 missing-JSON-tag bug.
func TestAssetsDTOsDeclareCamelCaseJSONTags(t *testing.T) {
	types := []any{
		AssetDTO{}, AssetVersionDTO{}, AssetFileDTO{},
		CreateAssetRequest{}, ListAssetsRequest{}, AddVersionRequest{},
		ApproveVersionRequest{}, AttachFileRequest{},
	}
	for _, value := range types {
		assertEveryFieldTagged(t, value)
	}
}

// TestAssetsBindingListAssetsRequestFieldNames pins the request shape the task
// specified, field by field: a rename would silently drop a filter.
func TestAssetsBindingListAssetsRequestFieldNames(t *testing.T) {
	decoded := decodeDTO(t, ListAssetsRequest{})
	encoded, err := json.Marshal(ListAssetsRequest{
		ProjectID: "p1", Types: []string{"character"}, IncludeDeleted: true, Limit: 10, Offset: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"projectId", "types", "includeDeleted", "limit", "offset"} {
		if _, present := decoded[key]; !present {
			t.Fatalf("ListAssetsRequest has no %q field: %s", key, encoded)
		}
	}
}
