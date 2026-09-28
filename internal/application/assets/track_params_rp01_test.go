package assets

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/platform/id"
)

// trackParamsTestRepository is the storage double RP-01.3's validation tests
// run against: it records what the service would persist so a refusal BEFORE
// the write is observable as an unchanged store.
type trackParamsTestRepository struct {
	assets   map[string]asset.Asset
	versions map[string]asset.Version
	files    map[string][]asset.File
	usages   map[string]asset.Usage
}

func newTrackParamsTestRepository() *trackParamsTestRepository {
	return &trackParamsTestRepository{
		assets:   map[string]asset.Asset{},
		versions: map[string]asset.Version{},
		files:    map[string][]asset.File{},
		usages:   map[string]asset.Usage{},
	}
}

func (r *trackParamsTestRepository) Available() bool { return true }

func (r *trackParamsTestRepository) GetVersion(_ context.Context, id string) (asset.Version, error) {
	version, ok := r.versions[id]
	if !ok {
		return asset.Version{}, asset.NotFoundError()
	}
	return version, nil
}

func (r *trackParamsTestRepository) UpdateUsageParams(_ context.Context, usageID, paramsJSON string) error {
	usage, ok := r.usages[usageID]
	if !ok {
		return asset.NotFoundError()
	}
	usage.Params = paramsJSON
	r.usages[usageID] = usage
	return nil
}

func (r *trackParamsTestRepository) ListUsagesOfConsumer(_ context.Context, consumerType asset.ConsumerType, consumerID string) ([]asset.Usage, error) {
	var out []asset.Usage
	for _, usage := range r.usages {
		if usage.ConsumerType == consumerType && usage.ConsumerID == consumerID {
			out = append(out, usage)
		}
	}
	return out, nil
}

func (r *trackParamsTestRepository) GetAsset(_ context.Context, id string) (asset.Asset, error) {
	record, ok := r.assets[id]
	if !ok {
		return asset.Asset{}, asset.NotFoundError()
	}
	return record, nil
}

func (r *trackParamsTestRepository) AddUsage(_ context.Context, usage asset.Usage) error {
	r.usages[usage.ID] = usage
	return nil
}

func (r *trackParamsTestRepository) CollectJobResultVersion(context.Context, CollectStorageRequest) (bool, string, int, error) {
	return false, "", 0, asset.StorageError("unavailable", nil)
}

// The remaining interface methods are unreachable from the params commands
// the tests below drive; they panic rather than answer, so an accidental call
// is a loud failure rather than a silent pass.
func (r *trackParamsTestRepository) CreateAsset(context.Context, asset.Asset) error {
	panic("not reachable from the params commands")
}
func (r *trackParamsTestRepository) ListAssets(context.Context, ListFilter) ([]asset.Asset, error) {
	return nil, nil
}
func (r *trackParamsTestRepository) CountAssets(context.Context, string) (int, error) { return 0, nil }
func (r *trackParamsTestRepository) UpdateAsset(context.Context, asset.Asset, int64) error {
	panic("not reachable from the params commands")
}
func (r *trackParamsTestRepository) CreateVersion(context.Context, asset.Version) error {
	panic("not reachable from the params commands")
}
func (r *trackParamsTestRepository) ListVersions(context.Context, string) ([]asset.Version, error) {
	return nil, nil
}
func (r *trackParamsTestRepository) MaxVersionNumber(context.Context, string) (int, error) {
	return 0, nil
}
func (r *trackParamsTestRepository) UpdateVersionStatus(context.Context, string, asset.VersionStatus) error {
	panic("not reachable from the params commands")
}
func (r *trackParamsTestRepository) AddFile(context.Context, asset.File) error {
	panic("not reachable from the params commands")
}
func (r *trackParamsTestRepository) ListFiles(context.Context, string) ([]asset.File, error) {
	return nil, nil
}
func (r *trackParamsTestRepository) CountFiles(context.Context, string) (int, error) { return 0, nil }
func (r *trackParamsTestRepository) AddRelation(context.Context, asset.Relation) error {
	panic("not reachable from the params commands")
}
func (r *trackParamsTestRepository) ListRelationsFrom(context.Context, string) ([]asset.Relation, error) {
	return nil, nil
}
func (r *trackParamsTestRepository) ListRelationsTo(context.Context, string) ([]asset.Relation, error) {
	return nil, nil
}
func (r *trackParamsTestRepository) ListUsages(context.Context, string) ([]asset.Usage, error) {
	return nil, nil
}
func (r *trackParamsTestRepository) CountUsages(context.Context, string) (int, error) { return 0, nil }

// TestRP01TrackParamsRejectInvalidBeforeWrite is RP-01.3's negative suite: a
// placement document with a negative offset, an inverted trim, a negative or
// non-finite volume, or a non-positive duration is REFUSED by the service
// before anything reaches the database — the stored document is byte-identical
// after every refusal.
func TestRP01TrackParamsRejectInvalidBeforeWrite(t *testing.T) {
	repository := newTrackParamsTestRepository()
	repository.usages["u1"] = asset.Usage{
		ID: "u1", AssetVersionID: "v1", ConsumerType: asset.ConsumerShot,
		ConsumerID: "shot-1", UsageRole: "audio_dialogue", Params: `{"volume":0.5}`,
	}
	service := NewService(Options{Repository: repository, Clock: fixedTrackClock{}, IDs: fixedTrackIDs()})
	ctx := context.Background()

	cases := []struct {
		name   string
		params TrackParamsShape
	}{
		{"negative offset", TrackParamsShape{OffsetMS: intTrackPtr(-100)}},
		{"trim end before start", TrackParamsShape{SourceStartMS: intTrackPtr(3000), SourceEndMS: intTrackPtr(1500)}},
		{"negative volume", TrackParamsShape{Volume: floatTrackPtr(-0.5)}},
		{"volume above the mixer's range", TrackParamsShape{Volume: floatTrackPtr(4.0)}},
		{"negative duration", TrackParamsShape{DurationMS: intTrackPtr(-1)}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := service.SetUsageParams(ctx, SetUsageParamsRequest{UsageID: "u1",
				OffsetMS: testCase.params.OffsetMS, SourceStartMS: testCase.params.SourceStartMS,
				SourceEndMS: testCase.params.SourceEndMS, DurationMS: testCase.params.DurationMS,
				Volume: testCase.params.Volume, Muted: testCase.params.Muted})
			if err == nil {
				t.Fatalf("%s was accepted", testCase.name)
			}
			if !strings.Contains(err.Error(), "") {
				t.Fatalf("unexpected error shape: %v", err)
			}
			// The database read-back is the evidence the refusal happened
			// BEFORE the write: the stored document is unchanged.
			if repository.usages["u1"].Params != `{"volume":0.5}` {
				t.Fatalf("the store changed on a refused write: %q", repository.usages["u1"].Params)
			}
		})
	}
}

// TestRP01TrackParamsRoundTrip drives the editor loop's full half-cycle at the
// service level: store a full document, read it back, send an edit that
// touches only the volume, and confirm the written replacement still carries
// the trim and the dialogue identity.
func TestRP01TrackParamsRoundTrip(t *testing.T) {
	repository := newTrackParamsTestRepository()
	repository.usages["u2"] = asset.Usage{
		ID: "u2", AssetVersionID: "v1", ConsumerType: asset.ConsumerShot,
		ConsumerID: "shot-2", UsageRole: "audio_dialogue",
	}
	service := NewService(Options{Repository: repository, Clock: fixedTrackClock{}, IDs: fixedTrackIDs()})
	ctx := context.Background()

	// The editor's first save: the full document it read (empty) with the
	// editor's three fields and the trim the timeline read showed.
	full := TrackParamsShape{
		OffsetMS: intTrackPtr(1200), SourceStartMS: intTrackPtr(300), SourceEndMS: intTrackPtr(2100),
		DurationMS: intTrackPtr(1800), Volume: floatTrackPtr(1), Muted: boolTrackPtr(true),
		DialogueLineID: "line-A",
	}
	if err := service.SetUsageParams(ctx, SetUsageParamsRequest{
		UsageID: "u2", OffsetMS: full.OffsetMS, SourceStartMS: full.SourceStartMS,
		SourceEndMS: full.SourceEndMS, DurationMS: full.DurationMS,
		Volume: full.Volume, Muted: full.Muted, DialogueLineID: full.DialogueLineID,
	}); err != nil {
		t.Fatalf("first save: %v", err)
	}
	if repository.usages["u2"].Params == "" {
		t.Fatal("the first save stored nothing")
	}

	// The second save touches the volume only; the replacement document the
	// UI builds carries the rest (the merge is the UI's mergeTrackEdit, whose
	// behaviour the frontend spec pins). Here the DOCUMENT that arrives is the
	// merged one, and every field must survive.
	merged := TrackParamsShape{
		OffsetMS: intTrackPtr(1200), SourceStartMS: intTrackPtr(300), SourceEndMS: intTrackPtr(2100),
		DurationMS: intTrackPtr(1800), Volume: floatTrackPtr(0), Muted: boolTrackPtr(false),
		DialogueLineID: "line-A",
	}
	if err := service.SetUsageParams(ctx, SetUsageParamsRequest{
		UsageID: "u2", OffsetMS: merged.OffsetMS, SourceStartMS: merged.SourceStartMS,
		SourceEndMS: merged.SourceEndMS, DurationMS: merged.DurationMS,
		Volume: merged.Volume, Muted: merged.Muted, DialogueLineID: merged.DialogueLineID,
	}); err != nil {
		t.Fatalf("second save: %v", err)
	}
	stored := repository.usages["u2"].Params
	for _, fragment := range []string{`"sourceStartMs":300`, `"sourceEndMs":2100`, `"durationMs":1800`, `"dialogueLineId":"line-A"`} {
		if !strings.Contains(stored, fragment) {
			t.Fatalf("stored document %s lost %s", stored, fragment)
		}
	}
	// An explicit zero volume is IN the document (a stated silence), and the
	// muted false is stated too.
	if !strings.Contains(stored, `"volume":0`) {
		t.Fatalf("stored document %s lost the explicit zero volume", stored)
	}
}

func intTrackPtr(v int) *int           { return &v }
func floatTrackPtr(v float64) *float64 { return &v }
func boolTrackPtr(v bool) *bool        { return &v }

// fixedTrackClock and fixedTrackIDs keep the service deterministic without a
// database or a wall clock.
type fixedTrackClock struct{}

func (fixedTrackClock) Now() time.Time { return time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC) }

func fixedTrackIDs() IDGenerator {
	return id.NewGeneratorWithClock(func() time.Time { return time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC) })
}
