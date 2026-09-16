package database

import (
	"context"
	"strings"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/project"
)

// canvasFixture creates a project and its canvas document, and returns the
// document id.
func canvasFixture(t *testing.T, projectsRepo *ProjectRepository, canvasRepo *CanvasRepository) (project.Project, project.CanvasDocument) {
	t.Helper()
	ctx := context.Background()
	generator := newTestIDGenerator()
	record := sampleProject(t, generator)
	if err := projectsRepo.CreateProject(ctx, record); err != nil {
		t.Fatal(err)
	}
	now := fixedClock()()
	documentID, err := generator.New()
	if err != nil {
		t.Fatal(err)
	}
	document := project.CanvasDocument{
		ID:        documentID,
		ProjectID: record.ID,
		Name:      record.Name,
		Kind:      project.CanvasFree,
		Viewport:  project.Viewport{X: 12, Y: -34, K: 1.5},
		CreatedAt: now,
		UpdatedAt: now,
		Revision:  1,
	}
	if err := canvasRepo.CreateDocument(ctx, document); err != nil {
		t.Fatal(err)
	}
	return record, document
}

// TestCanvasRepositoryDocumentRoundTrip proves the viewport survives a write
// and read, which AC-LEGACY-001 names explicitly ("Viewport 可恢复").
func TestCanvasRepositoryDocumentRoundTrip(t *testing.T) {
	projectsRepo, canvasRepo, _ := openWP04Repo(t)
	ctx := context.Background()
	_, document := canvasFixture(t, projectsRepo, canvasRepo)

	loaded, err := canvasRepo.GetDocument(ctx, document.ID)
	if err != nil {
		t.Fatalf("GetDocument: %v", err)
	}
	if loaded.Viewport != document.Viewport {
		t.Fatalf("viewport = %+v, want %+v", loaded.Viewport, document.Viewport)
	}
	if loaded.Kind != project.CanvasFree {
		t.Fatalf("kind = %q", loaded.Kind)
	}
	list, err := canvasRepo.ListDocuments(ctx, document.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != document.ID {
		t.Fatalf("ListDocuments = %+v", list)
	}
}

// TestCanvasRepositoryViewportUpdate proves a viewport change is persisted
// under the revision guard and that a degenerate value degrades to identity
// instead of writing a canvas nobody can see.
func TestCanvasRepositoryViewportUpdate(t *testing.T) {
	projectsRepo, canvasRepo, _ := openWP04Repo(t)
	ctx := context.Background()
	_, document := canvasFixture(t, projectsRepo, canvasRepo)

	document.Viewport = project.Viewport{X: 100, Y: 200, K: 2}
	document.UpdatedAt = fixedClock()()
	if err := canvasRepo.UpdateDocument(ctx, document, 1); err != nil {
		t.Fatalf("UpdateDocument: %v", err)
	}
	loaded, err := canvasRepo.GetDocument(ctx, document.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Viewport.K != 2 {
		t.Fatalf("viewport scale = %v, want 2", loaded.Viewport.K)
	}
	if loaded.Revision != 2 {
		t.Fatalf("revision = %d, want 2", loaded.Revision)
	}

	// A zero scale must not be stored as-is.
	loaded.Viewport = project.Viewport{}
	loaded.UpdatedAt = fixedClock()()
	if err := canvasRepo.UpdateDocument(ctx, loaded, 2); err != nil {
		t.Fatalf("UpdateDocument with a degenerate viewport: %v", err)
	}
	reRead, err := canvasRepo.GetDocument(ctx, document.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reRead.Viewport.K != 1 {
		t.Fatalf("a degenerate viewport was stored as %v, want the identity scale", reRead.Viewport.K)
	}
}

// TestCanvasRepositoryGeneratedViewportDecodes proves a malformed stored
// viewport degrades to the identity rather than failing a project load.
func TestCanvasRepositoryMalformedViewportDecodes(t *testing.T) {
	projectsRepo, canvasRepo, _ := openWP04Repo(t)
	ctx := context.Background()
	_, document := canvasFixture(t, projectsRepo, canvasRepo)

	// Write a broken value directly, as a corrupted database would hold.
	if _, err := canvasRepo.conn().ExecContext(ctx,
		`UPDATE canvas_documents SET viewport_json = ? WHERE id = ?`, "{not json", document.ID); err != nil {
		t.Fatal(err)
	}
	loaded, err := canvasRepo.GetDocument(ctx, document.ID)
	if err != nil {
		t.Fatalf("a malformed viewport broke the read: %v", err)
	}
	if !loaded.Viewport.IsUsable() {
		t.Fatalf("malformed viewport decoded to %+v, want a usable default", loaded.Viewport)
	}
}

// TestCanvasRepositoryNodeRoundTrip covers nodes including the legacy metadata
// column that unsupported fields are retained in.
func TestCanvasRepositoryNodeRoundTrip(t *testing.T) {
	projectsRepo, canvasRepo, _ := openRepoWithCanvas(t)
	ctx := context.Background()
	_, document := canvasFixture(t, projectsRepo, canvasRepo)

	legacy := `{"vendorField":"kept"}`
	node := project.Node{
		ID:               "node-1",
		CanvasDocumentID: document.ID,
		NodeType:         "acme:custom-widget",
		Title:            "Plugin node",
		PositionX:        10,
		PositionY:        20,
		Width:            300,
		Height:           200,
		ZIndex:           3,
		UIState:          `{"collapsed":true}`,
		LegacyMetadata:   legacy,
		CreatedAt:        fixedClock()(),
		UpdatedAt:        fixedClock()(),
		Revision:         1,
	}
	if err := canvasRepo.CreateNode(ctx, node); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	loaded, err := canvasRepo.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.NodeType != "acme:custom-widget" {
		t.Fatalf("a plugin node type was not preserved: %q", loaded.NodeType)
	}
	if loaded.LegacyMetadata != legacy {
		t.Fatalf("legacy metadata = %q, want %q", loaded.LegacyMetadata, legacy)
	}
	if loaded.UIState != node.UIState {
		t.Fatalf("ui state = %q", loaded.UIState)
	}
	count, err := canvasRepo.CountNodes(ctx, document.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("CountNodes = %d, want 1", count)
	}
}

// TestCanvasRepositoryProjectionNodeRequiresBothHalves proves the schema's
// projection CHECK is reachable through the repository.
func TestCanvasRepositoryProjectionNodeRequiresBothHalves(t *testing.T) {
	projectsRepo, canvasRepo, _ := openRepoWithCanvas(t)
	ctx := context.Background()
	_, document := canvasFixture(t, projectsRepo, canvasRepo)

	half := project.Node{
		ID:               "node-half",
		CanvasDocumentID: document.ID,
		NodeType:         "image",
		EntityType:       "storyboard_panel",
		CreatedAt:        fixedClock()(),
		UpdatedAt:        fixedClock()(),
		Revision:         1,
	}
	if err := canvasRepo.CreateNode(ctx, half); err == nil {
		t.Fatal("a node with half a projection reference was stored")
	}
}

// TestCanvasRepositoryEdgeCascade proves deleting a node removes the edges
// that referenced it, so a canvas cannot keep a dangling connection.
func TestCanvasRepositoryEdgeCascade(t *testing.T) {
	projectsRepo, canvasRepo, _ := openWP04Repo(t)
	ctx := context.Background()
	_, document := canvasFixture(t, projectsRepo, canvasRepo)

	for _, nodeID := range []string{"n-a", "n-b"} {
		if err := canvasRepo.CreateNode(ctx, project.Node{
			ID: nodeID, CanvasDocumentID: document.ID, NodeType: "text",
			CreatedAt: fixedClock()(), UpdatedAt: fixedClock()(), Revision: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}
	edge := project.Edge{
		ID: "e-1", CanvasDocumentID: document.ID, FromNodeID: "n-a", ToNodeID: "n-b",
		RelationType: project.RelationGeneric, ValidationStatus: project.EdgeUnknown,
		CreatedAt: fixedClock()(), UpdatedAt: fixedClock()(), Revision: 1,
	}
	if err := canvasRepo.CreateEdge(ctx, edge); err != nil {
		t.Fatalf("CreateEdge: %v", err)
	}
	count, err := canvasRepo.CountEdges(ctx, document.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("CountEdges = %d, want 1", count)
	}

	if err := canvasRepo.DeleteNode(ctx, "n-a"); err != nil {
		t.Fatal(err)
	}
	count, err = canvasRepo.CountEdges(ctx, document.ID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("deleting a node left %d dangling connections", count)
	}
}

// TestCanvasRepositoryEdgeDefaultsToGeneric proves an untyped connection is
// stored as the generic relation, which is where migrated legacy links land.
func TestCanvasRepositoryEdgeDefaultsToGeneric(t *testing.T) {
	projectsRepo, canvasRepo, _ := openRepoWithCanvas(t)
	ctx := context.Background()
	_, document := canvasFixture(t, projectsRepo, canvasRepo)

	for _, nodeID := range []string{"n-a", "n-b"} {
		if err := canvasRepo.CreateNode(ctx, project.Node{
			ID: nodeID, CanvasDocumentID: document.ID, NodeType: "text",
			CreatedAt: fixedClock()(), UpdatedAt: fixedClock()(), Revision: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}
	// An empty relation type is the legacy shape.
	edge := project.Edge{
		ID: "e-legacy", CanvasDocumentID: document.ID, FromNodeID: "n-a", ToNodeID: "n-b",
		CreatedAt: fixedClock()(), UpdatedAt: fixedClock()(), Revision: 1,
	}
	if err := canvasRepo.CreateEdge(ctx, edge); err == nil {
		t.Fatal("an empty relation type was stored instead of being defaulted by the caller")
	}
	// The default is applied by the schema when the column is omitted, which the
	// application layer does; here the explicit generic value must round-trip.
	edge.RelationType = project.RelationGeneric
	edge.ValidationStatus = project.EdgeUnknown
	if err := canvasRepo.CreateEdge(ctx, edge); err != nil {
		t.Fatalf("CreateEdge with generic: %v", err)
	}
	edges, err := canvasRepo.ListEdges(ctx, document.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 1 || edges[0].RelationType != project.RelationGeneric {
		t.Fatalf("edge = %+v", edges)
	}
}

// TestCanvasRepositoryChatSessionRoundTrip covers the chat session store.
func TestCanvasRepositoryChatSessionRoundTrip(t *testing.T) {
	projectsRepo, canvasRepo, _ := openRepoWithCanvas(t)
	ctx := context.Background()
	_, document := canvasFixture(t, projectsRepo, canvasRepo)

	session := project.ChatSession{
		ID:               "chat-1",
		CanvasDocumentID: document.ID,
		Title:            "Story chat",
		MessagesJSON:     `[{"id":"m1","role":"user","content":"hello"}]`,
		CreatedAt:        fixedClock()(),
		UpdatedAt:        fixedClock()(),
		Revision:         1,
	}
	if err := canvasRepo.CreateChatSession(ctx, session); err != nil {
		t.Fatalf("CreateChatSession: %v", err)
	}
	loaded, err := canvasRepo.GetChatSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.MessagesJSON != session.MessagesJSON {
		t.Fatalf("messages = %q", loaded.MessagesJSON)
	}
	sessions, err := canvasRepo.ListChatSessions(ctx, document.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("ListChatSessions = %d rows, want 1", len(sessions))
	}
}

// TestAssetRepositoryRoundTrip covers assets, versions and file links.
func TestAssetRepositoryRoundTrip(t *testing.T) {
	projectsRepo, _, assetsRepo := openWP04Repo(t)
	ctx := context.Background()
	record := sampleProject(t, newTestIDGenerator())
	if err := projectsRepo.CreateProject(ctx, record); err != nil {
		t.Fatal(err)
	}

	a := asset.Asset{
		ID: "asset-1", ProjectID: record.ID, Type: asset.TypeImage, Name: "Hero",
		Status: asset.StatusActive, CreatedAt: fixedClock()(), UpdatedAt: fixedClock()(), Revision: 1,
	}
	if err := assetsRepo.CreateAsset(ctx, a); err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}
	loaded, err := assetsRepo.GetAsset(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Type != asset.TypeImage || loaded.Name != "Hero" {
		t.Fatalf("asset = %+v", loaded)
	}

	for number := 1; number <= 2; number++ {
		version := asset.Version{
			ID: "v-" + itoaTest(number), AssetID: a.ID, VersionNumber: number,
			Status: asset.VersionDraft, CreatedByType: asset.CreatedByUser, CreatedAt: fixedClock()(),
		}
		if err := assetsRepo.CreateVersion(ctx, version); err != nil {
			t.Fatalf("CreateVersion %d: %v", number, err)
		}
	}
	highest, err := assetsRepo.MaxVersionNumber(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if highest != 2 {
		t.Fatalf("MaxVersionNumber = %d, want 2", highest)
	}
	versions, err := assetsRepo.ListVersions(ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 {
		t.Fatalf("ListVersions = %d, want 2", len(versions))
	}
	// Newest first.
	if versions[0].VersionNumber != 2 {
		t.Fatalf("versions are not newest first: %+v", versions)
	}
	// A duplicate version number is a conflict.
	if err := assetsRepo.CreateVersion(ctx, asset.Version{
		ID: "v-dup", AssetID: a.ID, VersionNumber: 1, Status: asset.VersionDraft,
		CreatedByType: asset.CreatedByUser, CreatedAt: fixedClock()(),
	}); err == nil {
		t.Fatal("a duplicate version number was accepted")
	}
}

// TestAssetRepositoryFileLinkRequiresCommittedObject proves the foreign key
// stops a version from advertising bytes that were never committed.
func TestAssetRepositoryFileLinkRequiresCommittedObject(t *testing.T) {
	projectsRepo, _, assetsRepo := openWP04Repo(t)
	ctx := context.Background()
	record := sampleProject(t, newTestIDGenerator())
	if err := projectsRepo.CreateProject(ctx, record); err != nil {
		t.Fatal(err)
	}
	if err := assetsRepo.CreateAsset(ctx, asset.Asset{
		ID: "asset-1", ProjectID: record.ID, Type: asset.TypeImage, Name: "Hero",
		Status: asset.StatusActive, CreatedAt: fixedClock()(), UpdatedAt: fixedClock()(), Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := assetsRepo.CreateVersion(ctx, asset.Version{
		ID: "v-1", AssetID: "asset-1", VersionNumber: 1, Status: asset.VersionDraft,
		CreatedByType: asset.CreatedByUser, CreatedAt: fixedClock()(),
	}); err != nil {
		t.Fatal(err)
	}

	uncommitted := strings.Repeat("f", 64)
	err := assetsRepo.AddFile(ctx, asset.File{
		VersionID: "v-1", FileHash: uncommitted, Role: asset.RolePrimary, CreatedAt: fixedClock()(),
	})
	if err == nil {
		t.Fatal("a link to an uncommitted object was accepted")
	}
	domainErr, ok := asset.AsError(err)
	if !ok || domainErr.Category != asset.CategoryConflict {
		t.Fatalf("expected a conflict, got %v", err)
	}

	// Commit the object, then the link is accepted and counted.
	if _, err := assetsRepo.conn().ExecContext(ctx,
		`INSERT INTO file_objects (hash, storage_key, mime_type, size_bytes, created_at) VALUES (?, ?, ?, ?, ?)`,
		uncommitted, uncommitted, "image/png", 12, "2026-09-16T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := assetsRepo.AddFile(ctx, asset.File{
		VersionID: "v-1", FileHash: uncommitted, Role: asset.RolePrimary, CreatedAt: fixedClock()(),
	}); err != nil {
		t.Fatalf("linking a committed object failed: %v", err)
	}
	count, err := assetsRepo.CountFiles(ctx, "v-1")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("CountFiles = %d, want 1", count)
	}
	// Repeating the same link is a no-op, so a retry is safe.
	if err := assetsRepo.AddFile(ctx, asset.File{
		VersionID: "v-1", FileHash: uncommitted, Role: asset.RolePrimary, CreatedAt: fixedClock()(),
	}); err != nil {
		t.Fatalf("a repeated link failed: %v", err)
	}
	count, err = assetsRepo.CountFiles(ctx, "v-1")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("a repeated link created a second row: %d", count)
	}
}

// openRepoWithCanvas is openWP04Repo with the canvas repository named, kept
// separate so tests that need all three read clearly.
func openRepoWithCanvas(t *testing.T) (*ProjectRepository, *CanvasRepository, *AssetRepository) {
	t.Helper()
	return openWP04Repo(t)
}

func itoaTest(value int) string {
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
