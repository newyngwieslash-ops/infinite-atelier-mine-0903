package database

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/projects"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/project"
)

// openWP05ProjectService opens a migrated database and returns a project service
// with its canvas repository, which is what the projection commands need.
func openWP05ProjectService(t *testing.T) (*projects.Service, *CanvasRepository) {
	t.Helper()
	ctx := context.Background()
	handle, err := open(ctx, filepath.Join(t.TempDir(), "app.db"), filepath.Join(t.TempDir(), "snapshots"), wp05Migrations(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close(ctx) })
	db := handle.SQL()
	canvas := NewCanvasRepository(db)
	service := projects.NewService(projects.Options{
		Projects: NewProjectRepository(db),
		Canvas:   canvas,
		Settings: NewDramaSettingsRepository(db),
		Clock:    fixedClockProvider{},
		IDs:      newTestIDGenerator(),
	})
	return service, canvas
}

// TestCreateCanvasProjectionWritesTheEntityReference is AC-CANVAS-001's
// "创建 Script Scene 后创建 Canvas Node" and "Node 有 entity refs": without a
// projection command the reference has a transport but no writer.
func TestCreateCanvasProjectionWritesTheEntityReference(t *testing.T) {
	service, canvas := openWP05ProjectService(t)
	ctx := context.Background()
	record, err := service.CreateDramaProject(ctx, projects.CreateDramaProjectRequest{Name: "Projection"})
	if err != nil {
		t.Fatal(err)
	}

	node, err := service.CreateCanvasProjection(ctx, projects.ProjectionRequest{
		ProjectID:  record.ID,
		EntityType: string(project.EntityScene),
		EntityID:   "scene-42",
		Title:      "Scene 42",
	})
	if err != nil {
		t.Fatalf("CreateCanvasProjection: %v", err)
	}
	if node.EntityType != string(project.EntityScene) || node.EntityID != "scene-42" {
		t.Fatalf("the projection lost its entity reference: %+v", node)
	}
	// The node kind defaults to the entity type so a scene draws as a scene.
	if node.NodeType != string(project.EntityScene) {
		t.Fatalf("node type = %q, want the entity type", node.NodeType)
	}
	// A default box, because a node with no size would be invisible.
	if node.Width <= 0 || node.Height <= 0 {
		t.Fatalf("the projection has no size: %+v", node)
	}

	// It is readable back through the repository with the reference intact.
	stored, err := canvas.GetNode(ctx, node.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.EntityType != string(project.EntityScene) || stored.EntityID != "scene-42" {
		t.Fatalf("the stored node lost its reference: %+v", stored)
	}
}

// TestCreateCanvasProjectionIsIdempotent covers the property that makes the
// command safe to call from a workflow: projecting an entity twice refreshes the
// node rather than adding a second one.
func TestCreateCanvasProjectionIsIdempotent(t *testing.T) {
	service, canvas := openWP05ProjectService(t)
	ctx := context.Background()
	record, err := service.CreateDramaProject(ctx, projects.CreateDramaProjectRequest{Name: "Idempotent"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.CreateCanvasProjection(ctx, projects.ProjectionRequest{
		ProjectID: record.ID, EntityType: string(project.EntityScene), EntityID: "scene-1", Title: "First",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateCanvasProjection(ctx, projects.ProjectionRequest{
		ProjectID: record.ID, EntityType: string(project.EntityScene), EntityID: "scene-1", Title: "Renamed",
	})
	if err != nil {
		t.Fatalf("re-projecting failed: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("re-projection created a second node: %s then %s", first.ID, second.ID)
	}
	if second.Title != "Renamed" {
		t.Fatalf("the label was not refreshed: %q", second.Title)
	}
	document, err := service.CanvasFor(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := canvas.ListNodes(ctx, document.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("%d nodes after two projections, want 1", len(nodes))
	}
}

// TestCreateCanvasProjectionRefusesHalfAReference covers the schema's CHECK
// stated in the service: half a reference is refused before the write.
func TestCreateCanvasProjectionRefusesHalfAReference(t *testing.T) {
	service, _ := openWP05ProjectService(t)
	ctx := context.Background()
	record, err := service.CreateDramaProject(ctx, projects.CreateDramaProjectRequest{Name: "Half"})
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []projects.ProjectionRequest{
		{ProjectID: record.ID, EntityType: string(project.EntityScene)},
		{ProjectID: record.ID, EntityID: "scene-1"},
		{ProjectID: record.ID, EntityType: "invented", EntityID: "x-1"},
	} {
		if _, err := service.CreateCanvasProjection(ctx, request); err == nil {
			t.Fatalf("a malformed projection was accepted: %+v", request)
		}
	}
}

// TestRemoveCanvasProjectionKeepsTheEntity is AC-CANVAS-001's "移除 Node 不删除
// Scene": the projection goes, the domain row is untouched.
func TestRemoveCanvasProjectionKeepsTheEntity(t *testing.T) {
	service, canvas := openWP05ProjectService(t)
	ctx := context.Background()
	record, err := service.CreateDramaProject(ctx, projects.CreateDramaProjectRequest{Name: "Removal"})
	if err != nil {
		t.Fatal(err)
	}
	node, err := service.CreateCanvasProjection(ctx, projects.ProjectionRequest{
		ProjectID: record.ID, EntityType: string(project.EntityScript), EntityID: "script-7", Title: "Script",
	})
	if err != nil {
		t.Fatal(err)
	}
	removed, err := service.RemoveCanvasProjection(ctx, record.ID, string(project.EntityScript), "script-7")
	if err != nil {
		t.Fatalf("RemoveCanvasProjection: %v", err)
	}
	if !removed {
		t.Fatal("the projection was not removed")
	}
	// The node is gone from the canvas.
	if _, err := canvas.GetNode(ctx, node.ID); err == nil {
		t.Fatal("the node survived its removal")
	}
	// Removing it again reports that there was nothing to remove, which is a
	// different answer from "the removal failed".
	again, err := service.RemoveCanvasProjection(ctx, record.ID, string(project.EntityScript), "script-7")
	if err != nil {
		t.Fatal(err)
	}
	if again {
		t.Fatal("a second removal reported that it removed something")
	}
}

// TestDeleteNodesRefusesARequiredReference is AC-CANVAS-002's "required ref 删除
// 被阻止" and PRD FR-130's "阻止破坏性删除".
func TestDeleteNodesRefusesARequiredReference(t *testing.T) {
	service, _ := openWP05ProjectService(t)
	ctx := context.Background()
	record, err := service.CreateDramaProject(ctx, projects.CreateDramaProjectRequest{Name: "Required"})
	if err != nil {
		t.Fatal(err)
	}
	from, err := service.CreateCanvasProjection(ctx, projects.ProjectionRequest{
		ProjectID: record.ID, EntityType: string(project.EntityShot), EntityID: "shot-1", EntityVersionID: "shot-1-v1", Title: "Shot",
	})
	if err != nil {
		t.Fatal(err)
	}
	to, err := service.CreateCanvasProjection(ctx, projects.ProjectionRequest{
		ProjectID: record.ID, EntityType: string(project.EntityAssetVersion), EntityID: "av-1", EntityVersionID: "av-1", Title: "Costume",
	})
	if err != nil {
		t.Fatal(err)
	}
	// A required uses_character edge from the shot to the costume.
	if _, err := service.CreateEdge(ctx, projects.CreateEdgeRequest{
		DocumentID: from.CanvasDocumentID, FromNodeID: from.ID, ToNodeID: to.ID,
		RelationType: "uses_character", Required: true,
	}); err != nil {
		t.Fatalf("creating the required edge failed: %v", err)
	}

	// The delete is refused.
	if _, err := service.DeleteNodes(ctx, []string{to.ID}); err == nil {
		t.Fatal("a node with a required reference was deleted")
	}
	// Neither node was removed: the batch is refused whole rather than partly.
	remaining, err := service.LoadCanvas(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining.Nodes) != 2 {
		t.Fatalf("%d nodes remain, want 2", len(remaining.Nodes))
	}
}

// TestDeleteNodesAllowsANotRequiredReference is the negative half: an edge that
// is not required must not block a removal, or the guard would make the canvas
// uneditable.
func TestDeleteNodesAllowsANotRequiredReference(t *testing.T) {
	service, _ := openWP05ProjectService(t)
	ctx := context.Background()
	record, err := service.CreateDramaProject(ctx, projects.CreateDramaProjectRequest{Name: "Optional"})
	if err != nil {
		t.Fatal(err)
	}
	from, err := service.CreateCanvasProjection(ctx, projects.ProjectionRequest{
		ProjectID: record.ID, EntityType: string(project.EntityShot), EntityID: "shot-1", EntityVersionID: "shot-1", Title: "Shot",
	})
	if err != nil {
		t.Fatal(err)
	}
	to, err := service.CreateCanvasProjection(ctx, projects.ProjectionRequest{
		ProjectID: record.ID, EntityType: string(project.EntityAssetVersion), EntityID: "av-1", EntityVersionID: "av-1", Title: "Costume",
	})
	if err != nil {
		t.Fatal(err)
	}
	// uses_character without required.
	if _, err := service.CreateEdge(ctx, projects.CreateEdgeRequest{
		DocumentID: from.CanvasDocumentID, FromNodeID: from.ID, ToNodeID: to.ID,
		RelationType: "uses_character",
	}); err != nil {
		t.Fatal(err)
	}
	deleted, err := service.DeleteNodes(ctx, []string{to.ID})
	if err != nil {
		t.Fatalf("an optional reference blocked a delete: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
}

// TestCreateEdgeValidatesAgainstTheRegistry is AC-CANVAS-002's "合法 references
// 成功" and "非法 source/target 拒绝" through the command path.
func TestCreateEdgeValidatesAgainstTheRegistry(t *testing.T) {
	service, _ := openWP05ProjectService(t)
	ctx := context.Background()
	record, err := service.CreateDramaProject(ctx, projects.CreateDramaProjectRequest{Name: "Registry"})
	if err != nil {
		t.Fatal(err)
	}
	shot, err := service.CreateCanvasProjection(ctx, projects.ProjectionRequest{
		ProjectID: record.ID, EntityType: string(project.EntityShot), EntityID: "shot-1", EntityVersionID: "shot-1", Title: "Shot",
	})
	if err != nil {
		t.Fatal(err)
	}
	asset, err := service.CreateCanvasProjection(ctx, projects.ProjectionRequest{
		ProjectID: record.ID, EntityType: string(project.EntityAssetVersion), EntityID: "av-1", EntityVersionID: "av-1", Title: "Asset",
	})
	if err != nil {
		t.Fatal(err)
	}
	// A legal uses_character is accepted and marked valid.
	edge, err := service.CreateEdge(ctx, projects.CreateEdgeRequest{
		DocumentID: shot.CanvasDocumentID, FromNodeID: shot.ID, ToNodeID: asset.ID, RelationType: "uses_character",
	})
	if err != nil {
		t.Fatalf("a legal semantic edge was refused: %v", err)
	}
	if edge.ValidationStatus != project.EdgeValid {
		t.Fatalf("the edge's validation status = %q, want valid", edge.ValidationStatus)
	}

	// An edge whose endpoints the relation forbids is refused outright.
	if _, err := service.CreateEdge(ctx, projects.CreateEdgeRequest{
		DocumentID: shot.CanvasDocumentID, FromNodeID: shot.ID, ToNodeID: shot.ID, RelationType: "adapts_to",
	}); err == nil {
		t.Fatal("an illegal semantic edge was accepted")
	}

	// A decorative node cannot take part in a semantic relation.
	document, err := service.CanvasFor(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := service.CreateNode(ctx, document.ID, projects.NodeInput{NodeType: "text", Title: "note", Width: 10, Height: 10})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateEdge(ctx, projects.CreateEdgeRequest{
		DocumentID: document.ID, FromNodeID: plain.ID, ToNodeID: asset.ID, RelationType: "uses_character",
	}); err == nil {
		t.Fatal("a node projecting nothing was allowed into a semantic relation")
	}

	// A generic edge is still accepted between anything: PRD FR-130 requires
	// imported untyped links to survive.
	generic, err := service.CreateEdge(ctx, projects.CreateEdgeRequest{
		DocumentID: document.ID, FromNodeID: plain.ID, ToNodeID: asset.ID,
	})
	if err != nil {
		t.Fatalf("a generic edge was refused: %v", err)
	}
	if generic.ValidationStatus != project.EdgeUnknown {
		t.Fatalf("a generic edge's status = %q, want unknown", generic.ValidationStatus)
	}
}

// TestFindEntityReferencesListsWhatUsesAnEntity is AC-CANVAS-001's "删除 Scene
// 显示影响" and PRD FR-130's "删除领域实体时列出所有引用".
func TestFindEntityReferencesListsWhatUsesAnEntity(t *testing.T) {
	service, _ := openWP05ProjectService(t)
	ctx := context.Background()
	record, err := service.CreateDramaProject(ctx, projects.CreateDramaProjectRequest{Name: "Impact"})
	if err != nil {
		t.Fatal(err)
	}
	scene, err := service.CreateCanvasProjection(ctx, projects.ProjectionRequest{
		ProjectID: record.ID, EntityType: string(project.EntityScene), EntityID: "scene-1", Title: "Scene",
	})
	if err != nil {
		t.Fatal(err)
	}
	shot, err := service.CreateCanvasProjection(ctx, projects.ProjectionRequest{
		ProjectID: record.ID, EntityType: string(project.EntityShot), EntityID: "shot-1", EntityVersionID: "shot-1", Title: "Shot",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateEdge(ctx, projects.CreateEdgeRequest{
		DocumentID: scene.CanvasDocumentID, FromNodeID: shot.ID, ToNodeID: scene.ID,
		RelationType: "references", Required: true,
	}); err != nil {
		t.Fatal(err)
	}

	references, err := service.FindEntityReferences(ctx, record.ID, string(project.EntityScene), "scene-1")
	if err != nil {
		t.Fatalf("FindEntityReferences: %v", err)
	}
	if len(references.Projections) != 1 || references.Projections[0].ID != scene.ID {
		t.Fatalf("projections = %+v", references.Projections)
	}
	// The required edge is reported as a blocker, which is what makes the delete
	// destructive rather than merely referenced.
	if len(references.RequiredBlockers) != 1 {
		t.Fatalf("required blockers = %v, want one", references.RequiredBlockers)
	}
	// An entity nothing projects reports an empty list rather than an error.
	empty, err := service.FindEntityReferences(ctx, record.ID, string(project.EntityScript), "script-absent")
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Projections) != 0 || len(empty.RequiredBlockers) != 0 {
		t.Fatalf("an unprojected entity reported references: %+v", empty)
	}
}
