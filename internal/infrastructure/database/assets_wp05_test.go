package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	appassets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
)

// openWP05AssetService opens a migrated database and returns the asset service
// over its repository, plus the pieces a test asserts against.
func openWP05AssetService(t *testing.T) (*appassets.Service, *AssetRepository, *sql.DB) {
	t.Helper()
	handle, err := open(context.Background(), filepath.Join(t.TempDir(), "app.db"),
		filepath.Join(t.TempDir(), "snapshots"), wp05Migrations(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := handle.Close(context.Background()); closeErr != nil {
			t.Error(closeErr)
		}
	})
	db := handle.SQL()
	seedWP05Parents(t, db)
	// The fixture seeds one file object per hash the tests attach. A version
	// cannot be approved without a committed file, and the schema's foreign key
	// rejects a reference to bytes that were never stored, so both hashes the
	// approval fixture uses must exist here.
	for _, hash := range []string{testHashA, testHashB} {
		if _, err := db.ExecContext(context.Background(),
			"INSERT OR IGNORE INTO file_objects (hash, storage_key, mime_type, size_bytes, created_at) VALUES (?, ?, 'image/png', 10, '2026-01-01T00:00:00Z')",
			hash, hash); err != nil {
			t.Fatalf("seeding the file object %s: %v", hash, err)
		}
	}
	repository := NewAssetRepository(db)
	service := appassets.NewService(appassets.Options{
		Repository: repository,
		Clock:      fixedClockProvider{},
		IDs:        newTestIDGenerator(),
	})
	return service, repository, db
}

// approveFixture creates an asset with two versions that each have a committed
// file, which is the minimum an approval needs.
func approveFixture(t *testing.T, service *appassets.Service) (asset.Asset, asset.Version, asset.Version) {
	t.Helper()
	ctx := context.Background()
	record, first, err := service.CreateAsset(ctx, appassets.CreateAssetRequest{
		ProjectID: "project-1", Type: asset.TypeCharacter, Name: "Mira",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.AddVersion(ctx, appassets.AddVersionRequest{AssetID: record.ID})
	if err != nil {
		t.Fatal(err)
	}
	// Both versions need a file: CountFiles is what CanApprove reads.
	for index, version := range []asset.Version{first, second} {
		hash := testHashA
		if index == 1 {
			hash = testHashB
		}
		if _, err := service.AttachFile(ctx, version.ID, hash, asset.RolePrimary); err != nil {
			t.Fatal(err)
		}
	}
	return record, first, second
}

// TestApproveVersionSupersedesThePreviousApproval covers DOMAIN_MODEL §2.5's
// "批准新版本时旧批准版本变为 superseded" through the command path.
//
// This behaviour had no test at all: the application package shipped without a
// service test and there was no assets integration test, so removing the
// supersede step left the whole suite green.
func TestApproveVersionSupersedesThePreviousApproval(t *testing.T) {
	service, repository, _ := openWP05AssetService(t)
	ctx := context.Background()
	record, first, second := approveFixture(t, service)

	approvedFirst, err := service.ApproveVersion(ctx, appassets.ApproveVersionRequest{
		VersionID: first.ID, ImpactAcknowledged: true,
	})
	if err != nil {
		t.Fatalf("approving the first version: %v", err)
	}
	if approvedFirst.Status != asset.VersionApproved {
		t.Fatalf("the first version's status = %q", approvedFirst.Status)
	}
	// The asset now points at it.
	stored, err := repository.GetAsset(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.CurrentApprovedVersionID != first.ID {
		t.Fatalf("the asset points at %q, want %q", stored.CurrentApprovedVersionID, first.ID)
	}

	// Approving the second supersedes the first rather than leaving two.
	if _, err := service.ApproveVersion(ctx, appassets.ApproveVersionRequest{
		VersionID: second.ID, ImpactAcknowledged: true,
	}); err != nil {
		t.Fatalf("approving the second version: %v", err)
	}
	reloadedFirst, err := repository.GetVersion(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloadedFirst.Status != asset.VersionSuperseded {
		t.Fatalf("the replaced version's status = %q, want superseded", reloadedFirst.Status)
	}
	reloadedSecond, err := repository.GetVersion(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloadedSecond.Status != asset.VersionApproved {
		t.Fatalf("the new version's status = %q, want approved", reloadedSecond.Status)
	}
	// The asset follows the new approval.
	stored, err = repository.GetAsset(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.CurrentApprovedVersionID != second.ID {
		t.Fatalf("the asset still points at %q", stored.CurrentApprovedVersionID)
	}
	// Exactly one approved row exists, which is what the schema's partial unique
	// index demands and what a second approval would have violated.
	var approved int
	if err := dbQueryRow(t, ctx, repository.db,
		`SELECT COUNT(*) FROM asset_versions WHERE asset_id = ? AND status = 'approved'`, record.ID, &approved); err != nil {
		t.Fatal(err)
	}
	if approved != 1 {
		t.Fatalf("%d approved versions, want exactly 1", approved)
	}
}

// TestApproveVersionRefusesWithoutImpactAcknowledgement covers the precondition
// §8.2 requires before an approval switch.
func TestApproveVersionRefusesWithoutImpactAcknowledgement(t *testing.T) {
	service, _, _ := openWP05AssetService(t)
	ctx := context.Background()
	_, first, _ := approveFixture(t, service)

	if _, err := service.ApproveVersion(ctx, appassets.ApproveVersionRequest{VersionID: first.ID}); err == nil {
		t.Fatal("an unacknowledged approval was granted")
	}
	if _, err := service.ApproveVersion(ctx, appassets.ApproveVersionRequest{
		VersionID: first.ID, ImpactAcknowledged: true,
	}); err != nil {
		t.Fatalf("an acknowledged approval was refused: %v", err)
	}
}

// TestApproveVersionRefusesAVersionWithNoFile covers the §8.2 rule that a
// version needs a committed file before it can be approved.
func TestApproveVersionRefusesAVersionWithNoFile(t *testing.T) {
	service, _, _ := openWP05AssetService(t)
	ctx := context.Background()
	record, _, err := service.CreateAsset(ctx, appassets.CreateAssetRequest{
		ProjectID: "project-1", Type: asset.TypeProp, Name: "Lantern",
	})
	if err != nil {
		t.Fatal(err)
	}
	// The draft version CreateAsset makes has no file attached.
	versions, err := service.ListVersions(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) == 0 {
		t.Fatal("CreateAsset made no version")
	}
	if _, err := service.ApproveVersion(ctx, appassets.ApproveVersionRequest{
		VersionID: versions[0].ID, ImpactAcknowledged: true,
	}); err == nil {
		t.Fatal("a version with no committed file was approved")
	}
}

// TestApprovalImpactListsWhatTheSwitchDisturbs covers the impact analysis §8.2
// requires and PRD FR-050's "替换批准版本时，系统列出受影响的分镜和镜头".
func TestApprovalImpactListsWhatTheSwitchDisturbs(t *testing.T) {
	service, _, _ := openWP05AssetService(t)
	ctx := context.Background()
	_, first, second := approveFixture(t, service)

	// Nothing is approved yet, so a switch disturbs nothing.
	impact, err := service.ApprovalImpactOf(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if impact.Replaces != "" || len(impact.Consumers) != 0 {
		t.Fatalf("an asset with no approval reported impact: %+v", impact)
	}

	if _, err := service.ApproveVersion(ctx, appassets.ApproveVersionRequest{
		VersionID: first.ID, ImpactAcknowledged: true,
	}); err != nil {
		t.Fatal(err)
	}
	// Two consumers use the approved version: one required, one not.
	if _, err := service.AddUsage(ctx, appassets.AddUsageRequest{
		AssetVersionID: first.ID, ConsumerType: asset.ConsumerShot,
		ConsumerID: "shot-1", UsageRole: "costume", Required: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AddUsage(ctx, appassets.AddUsageRequest{
		AssetVersionID: first.ID, ConsumerType: asset.ConsumerScene,
		ConsumerID: "scene-1", UsageRole: "reference",
	}); err != nil {
		t.Fatal(err)
	}

	impact, err = service.ApprovalImpactOf(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if impact.Replaces != first.ID {
		t.Fatalf("the impact names %q as replaced, want %q", impact.Replaces, first.ID)
	}
	if len(impact.Consumers) != 2 {
		t.Fatalf("%d consumers listed, want 2", len(impact.Consumers))
	}
	// The required one is separated out, because that is the one a caller must
	// not silently break.
	if len(impact.RequiredConsumers) != 1 || impact.RequiredConsumers[0].ConsumerID != "shot-1" {
		t.Fatalf("required consumers = %+v", impact.RequiredConsumers)
	}
	// Asking about the version already approved reports nothing to replace.
	impact, err = service.ApprovalImpactOf(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if impact.Replaces != "" {
		t.Fatalf("the approved version reported itself as replaced: %+v", impact)
	}
}

// TestAssetLineageRoundTrips covers §8.5's lineage through the command path,
// which PRD FR-050's "任何派生资产都能追溯父资产及变换原因" is built on.
func TestAssetLineageRoundTrips(t *testing.T) {
	service, _, _ := openWP05AssetService(t)
	ctx := context.Background()
	_, first, second := approveFixture(t, service)

	// The second version derives from the first.
	relation, err := service.AddRelation(ctx, appassets.AddRelationRequest{
		SourceAssetVersionID: second.ID,
		TargetAssetVersionID: first.ID,
		Type:                 asset.RelationDerivedFrom,
	})
	if err != nil {
		t.Fatalf("AddRelation: %v", err)
	}
	if relation.Type != asset.RelationDerivedFrom {
		t.Fatalf("relation type = %q", relation.Type)
	}

	from, to, err := service.ListLineage(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(from) != 1 || from[0].TargetAssetVersionID != first.ID {
		t.Fatalf("lineage from the derived version = %+v", from)
	}
	// The parent sees the child from the other direction.
	if len(to) != 0 {
		t.Fatalf("the derived version reported inbound lineage: %+v", to)
	}
	reverseFrom, reverseTo, err := service.ListLineage(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	// The parent has no parents, and its derived version arrives inbound.
	if len(reverseFrom) != 0 {
		t.Fatalf("the parent reported outbound lineage: %+v", reverseFrom)
	}
	if len(reverseTo) != 1 || reverseTo[0].SourceAssetVersionID != second.ID {
		t.Fatalf("the parent's inbound lineage = %+v", reverseTo)
	}

	// A self-relation is refused, and so is an unknown version.
	if _, err := service.AddRelation(ctx, appassets.AddRelationRequest{
		SourceAssetVersionID: first.ID, TargetAssetVersionID: first.ID, Type: asset.RelationDerivedFrom,
	}); err == nil {
		t.Fatal("a version was allowed to derive from itself")
	}
	if _, err := service.AddRelation(ctx, appassets.AddRelationRequest{
		SourceAssetVersionID: first.ID, TargetAssetVersionID: "no-such-version", Type: asset.RelationDerivedFrom,
	}); err == nil {
		t.Fatal("a lineage edge to a missing version was accepted")
	}
}

// TestAssetUsageIsUniquePerConsumerRole covers §8.6's uniqueness rule, which is
// what makes a usage list meaningful rather than repeated.
func TestAssetUsageIsUniquePerConsumerRole(t *testing.T) {
	service, _, _ := openWP05AssetService(t)
	ctx := context.Background()
	_, first, _ := approveFixture(t, service)

	request := appassets.AddUsageRequest{
		AssetVersionID: first.ID, ConsumerType: asset.ConsumerShot,
		ConsumerID: "shot-1", UsageRole: "costume", Required: true,
	}
	if _, err := service.AddUsage(ctx, request); err != nil {
		t.Fatalf("AddUsage: %v", err)
	}
	// The same consumer, role and version again is refused.
	if _, err := service.AddUsage(ctx, request); err == nil {
		t.Fatal("a duplicate usage was accepted")
	}
	// A different role for the same consumer is a different usage.
	other := request
	other.UsageRole = "reference"
	if _, err := service.AddUsage(ctx, other); err != nil {
		t.Fatalf("a different role was refused: %v", err)
	}
	usages, err := service.ListUsages(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(usages) != 2 {
		t.Fatalf("%d usages, want 2", len(usages))
	}
}

// dbQueryRow is a small helper so the assertions above stay readable.
func dbQueryRow(t *testing.T, ctx context.Context, db *sql.DB, query string, arg any, into *int) error {
	t.Helper()
	return db.QueryRowContext(ctx, query, arg).Scan(into)
}
