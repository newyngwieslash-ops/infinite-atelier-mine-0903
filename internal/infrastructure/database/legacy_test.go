package database

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/legacy"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/project"
)

// legacyHandle opens a fresh database for tests that need the import store or
// must seed rows directly.
func legacyHandle(t *testing.T) *Handle {
	t.Helper()
	return openWP04Handle(t)
}

// bundleFixture builds one importable project bundle.
func bundleFixture(t *testing.T, projectID, documentID, nodeID, edgeFrom, edgeTo string) legacy.ProjectBundle {
	t.Helper()
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	return legacy.ProjectBundle{
		Project: project.Project{
			ID: projectID, WorkspaceID: project.DefaultLocalWorkspaceID,
			Type: project.ProjectFreeCanvas, Name: "Imported", Language: "zh-CN",
			Status: project.ProjectActive, CreatedAt: now, UpdatedAt: now, Revision: 1,
		},
		Document: project.CanvasDocument{
			ID: documentID, ProjectID: projectID, Name: "Imported", Kind: project.CanvasFree,
			Viewport: project.Viewport{X: 1, Y: 2, K: 1}, CreatedAt: now, UpdatedAt: now, Revision: 1,
		},
		Nodes: []project.Node{
			{
				ID: nodeID, CanvasDocumentID: documentID, NodeType: "text", Title: "One",
				PositionX: 10, PositionY: 20, Width: 100, Height: 50,
				CreatedAt: now, UpdatedAt: now, Revision: 1,
			},
			{
				ID: edgeTo, CanvasDocumentID: documentID, NodeType: "text", Title: "Two",
				PositionX: 200, PositionY: 20, Width: 100, Height: 50,
				CreatedAt: now, UpdatedAt: now, Revision: 1,
			},
		},
		Edges: []project.Edge{
			{
				ID: edgeFrom, CanvasDocumentID: documentID, FromNodeID: nodeID, ToNodeID: edgeTo,
				RelationType: project.RelationGeneric, ValidationStatus: project.EdgeUnknown,
				CreatedAt: now, UpdatedAt: now, Revision: 1,
			},
		},
		ChatSessions: []project.ChatSession{
			{
				ID: "chat-" + projectID, CanvasDocumentID: documentID, Title: "Chat",
				MessagesJSON: `[]`, CreatedAt: now, UpdatedAt: now, Revision: 1,
			},
		},
		Mappings: []legacy.IDMapping{
			{Kind: legacy.MapProject, LegacyID: projectID, NewID: projectID},
			{Kind: legacy.MapNode, LegacyID: nodeID, NewID: nodeID},
		},
		Fingerprint: strings.Repeat("a", 64),
	}
}

// TestLegacyImportSnapshotIsAtomic proves the whole snapshot lands, including
// the mapping rows and the import record.
func TestLegacyImportSnapshotIsAtomic(t *testing.T) {
	handle := legacyHandle(t)
	ctx := context.Background()
	projectsRepo := NewProjectRepository(handle.SQL())
	canvasRepo := NewCanvasRepository(handle.SQL())
	legacyRepo := NewLegacyRepository(handle.SQL())

	bundle := bundleFixture(t, "proj-legacy-1", "doc-1", "node-1", "edge-1", "node-2")
	request := legacy.ImportRequest{
		Fingerprint: strings.Repeat("b", 64),
		Mode:        legacy.ModeInitial,
		SourceCase:  "all-node-types",
		StartedAt:   time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC),
		Bundles:     []legacy.ProjectBundle{bundle},
	}

	outcome, err := legacyRepo.ImportSnapshot(ctx, request)
	if err != nil {
		t.Fatalf("ImportSnapshot: %v", err)
	}
	if outcome.Projects != 1 || outcome.Nodes != 2 || outcome.Edges != 1 {
		t.Fatalf("outcome = %+v", outcome)
	}
	if outcome.ImportID == "" {
		t.Fatal("no import id was returned")
	}

	// Everything is readable afterwards.
	if _, err := projectsRepo.GetProject(ctx, "proj-legacy-1"); err != nil {
		t.Fatalf("the imported project is not readable: %v", err)
	}
	nodes, err := canvasRepo.ListNodes(ctx, "doc-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("nodes = %d, want 2", len(nodes))
	}
	edges, err := canvasRepo.ListEdges(ctx, "doc-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 1 || edges[0].RelationType != project.RelationGeneric {
		t.Fatalf("edges = %+v", edges)
	}

	// The mapping is queryable.
	newID, found, err := legacyRepo.LookupMapping(ctx, legacy.MapProject, "proj-legacy-1")
	if err != nil {
		t.Fatal(err)
	}
	if !found || newID != "proj-legacy-1" {
		t.Fatalf("mapping lookup = %q, %v", newID, found)
	}

	// The fingerprint is recorded as completed, so a second import detects it.
	done, err := legacyRepo.HasCompletedImport(ctx, strings.Repeat("b", 64))
	if err != nil {
		t.Fatal(err)
	}
	if !done {
		t.Fatal("the fingerprint was not recorded as completed")
	}
}

// TestLegacyImportRollsBackCompletely is AC-LEGACY-003: a failure inside the
// import must leave no half-written project, and no import record claiming
// success.
func TestLegacyImportRollsBackCompletely(t *testing.T) {
	handle := legacyHandle(t)
	ctx := context.Background()
	projectsRepo := NewProjectRepository(handle.SQL())
	canvasRepo := NewCanvasRepository(handle.SQL())
	legacyRepo := NewLegacyRepository(handle.SQL())

	good := bundleFixture(t, "proj-ok", "doc-ok", "node-ok-1", "edge-ok", "node-ok-2")
	// The second bundle is malformed: its first node references a canvas
	// document that does not exist, which the foreign key rejects mid-import.
	broken := bundleFixture(t, "proj-bad", "doc-bad", "node-bad-1", "edge-bad", "node-bad-2")
	broken.Nodes[0].CanvasDocumentID = "doc-does-not-exist"

	request := legacy.ImportRequest{
		Fingerprint: strings.Repeat("c", 64),
		Mode:        legacy.ModeInitial,
		SourceCase:  "rollback",
		StartedAt:   time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC),
		Bundles:     []legacy.ProjectBundle{good, broken},
	}

	if _, err := legacyRepo.ImportSnapshot(ctx, request); err == nil {
		t.Fatal("a malformed import reported success")
	}

	// The good bundle must not be present: the transaction rolled back as one.
	if _, err := projectsRepo.GetProject(ctx, "proj-ok"); err == nil {
		t.Fatal("the first project survived a failed import; the write was not atomic")
	}
	if _, err := projectsRepo.GetProject(ctx, "proj-bad"); err == nil {
		t.Fatal("the malformed project was partially written")
	}
	// No canvas rows either.
	for _, documentID := range []string{"doc-ok", "doc-bad"} {
		nodes, err := canvasRepo.ListNodes(ctx, documentID)
		if err != nil {
			t.Fatal(err)
		}
		if len(nodes) != 0 {
			t.Fatalf("canvas %s has %d nodes after a rolled-back import", documentID, len(nodes))
		}
	}
	// And no completed fingerprint, so a retry is a retry rather than a
	// duplicate detection.
	done, err := legacyRepo.HasCompletedImport(ctx, strings.Repeat("c", 64))
	if err != nil {
		t.Fatal(err)
	}
	if done {
		t.Fatal("a failed import recorded itself as completed")
	}
}

// TestLegacyImportRecordsFailure proves a failed run can still be reported to
// the user: the record is stored outside the rolled-back transaction.
func TestLegacyImportRecordsFailure(t *testing.T) {
	ctx := context.Background()
	legacyRepo := NewLegacyRepository(legacyHandle(t).SQL())

	request := legacy.ImportRequest{
		Fingerprint: strings.Repeat("d", 64),
		Mode:        legacy.ModeInitial,
		SourceCase:  "failure-report",
		StartedAt:   time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC),
	}
	id, err := legacyRepo.RecordImport(ctx, request, legacy.StatusFailed, `{"stage":"transform"}`,
		[]legacy.Warning{{Code: legacy.WarningMissingMedia, LegacyID: "image:gone", Occurrences: 1}})
	if err != nil {
		t.Fatalf("RecordImport: %v", err)
	}
	record, err := legacyRepo.GetImport(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != legacy.StatusFailed {
		t.Fatalf("status = %q", record.Status)
	}
	if record.ReportJSON != `{"stage":"transform"}` {
		t.Fatalf("report = %q", record.ReportJSON)
	}
	if len(record.Warnings) != 1 || record.Warnings[0].Code != legacy.WarningMissingMedia {
		t.Fatalf("warnings = %+v", record.Warnings)
	}
	// A failed run must not satisfy the fingerprint check.
	done, err := legacyRepo.HasCompletedImport(ctx, strings.Repeat("d", 64))
	if err != nil {
		t.Fatal(err)
	}
	if done {
		t.Fatal("a failed record satisfied the completed check")
	}
}

// TestLegacyImportStoresAssetsAndHistory proves the asset and history rows
// land inside the same transaction.
func TestLegacyImportStoresAssetsAndHistory(t *testing.T) {
	handle := legacyHandle(t)
	ctx := context.Background()
	assetsRepo := NewAssetRepository(handle.SQL())
	legacyRepo := NewLegacyRepository(handle.SQL())

	hash := strings.Repeat("e", 64)
	if _, err := legacyRepo.conn().ExecContext(ctx,
		`INSERT INTO file_objects (hash, storage_key, mime_type, size_bytes, created_at) VALUES (?, ?, ?, ?, ?)`,
		hash, hash, "image/png", 12, "2026-09-16T00:00:00Z"); err != nil {
		t.Fatal(err)
	}

	bundle := bundleFixture(t, "proj-assets", "doc-assets", "node-a1", "edge-a", "node-a2")
	bundle.Assets = []legacy.AssetBundle{
		{
			Asset: asset.Asset{
				ID: "asset-1", ProjectID: "proj-assets", Type: asset.TypeImage, Name: "Hero",
				Status: asset.StatusActive, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), Revision: 1,
			},
			Version: asset.Version{
				ID: "ver-1", AssetID: "asset-1", VersionNumber: 1, Status: asset.VersionDraft,
				CreatedByType: asset.CreatedByMigration, CreatedAt: time.Now().UTC(),
			},
			Files: []legacy.FileLink{
				{VersionID: "ver-1", FileHash: hash, Role: asset.RolePrimary, LegacyKey: "image:legacy-1"},
			},
		},
	}
	bundle.History = []legacy.HistoryRecord{
		{
			ID: "hist-1", LegacyID: "legacy-history-1", Prompt: "a lighthouse", Model: "legacy-model",
			ImagesJSON: `[{"storageKey":"image:legacy-1"}]`, Success: 1, Fail: 0,
			GeneratedAt: time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC),
		},
	}

	outcome, err := legacyRepo.ImportSnapshot(ctx, legacy.ImportRequest{
		Fingerprint: strings.Repeat("f", 64),
		Mode:        legacy.ModeInitial,
		StartedAt:   time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC),
		Bundles:     []legacy.ProjectBundle{bundle},
	})
	if err != nil {
		t.Fatalf("ImportSnapshot: %v", err)
	}
	if outcome.Assets != 1 || outcome.History != 1 {
		t.Fatalf("outcome = %+v", outcome)
	}
	versions, err := assetsRepo.ListVersions(ctx, "asset-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || versions[0].VersionNumber != 1 {
		t.Fatalf("versions = %+v", versions)
	}
	files, err := assetsRepo.ListFiles(ctx, "ver-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].FileHash != hash {
		t.Fatalf("files = %+v", files)
	}
}
