package database

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/projects"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/project"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/platform/id"
)

// openWP04Repo is the full-stack fixture for the WP-04 repositories: the
// production open path over the v4 migration set, with a temporary directory
// for the database and its snapshots.
func openWP04Repo(t *testing.T) (*ProjectRepository, *CanvasRepository, *AssetRepository) {
	t.Helper()
	handle := openWP04Handle(t)
	return NewProjectRepository(handle.SQL()), NewCanvasRepository(handle.SQL()), NewAssetRepository(handle.SQL())
}

// openWP04Handle returns the raw handle, for tests that need a repository this
// helper does not construct (the legacy import store, for example) or that must
// seed a row directly.
func openWP04Handle(t *testing.T) *Handle {
	t.Helper()
	handle, err := open(context.Background(), filepath.Join(t.TempDir(), "app.db"),
		filepath.Join(t.TempDir(), "snapshots"), wp04Migrations(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := handle.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if handle.Mode() != ModeReady {
		t.Fatalf("database not ready: %v", handle.Err())
	}
	return handle
}

// fixedClock is a deterministic clock for repository tests.
func fixedClock() func() time.Time {
	base := time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC)
	return func() time.Time { return base }
}

// newTestIDGenerator builds a UUIDv7 generator over a fixed clock.
func newTestIDGenerator() *id.Generator {
	return id.NewGeneratorWithClock(fixedClock())
}

func sampleProject(t *testing.T, generator *id.Generator) project.Project {
	t.Helper()
	value, err := generator.New()
	if err != nil {
		t.Fatal(err)
	}
	now := fixedClock()()
	return project.Project{
		ID:          value,
		WorkspaceID: project.DefaultLocalWorkspaceID,
		Type:        project.ProjectFreeCanvas,
		Name:        "Sample",
		Language:    "zh-CN",
		Status:      project.ProjectActive,
		CreatedAt:   now,
		UpdatedAt:   now,
		Revision:    1,
	}
}

// TestProjectRepositoryRoundTrip covers create, read and list.
func TestProjectRepositoryRoundTrip(t *testing.T) {
	projectsRepo, _, _ := openWP04Repo(t)
	ctx := context.Background()
	generator := newTestIDGenerator()

	record := sampleProject(t, generator)
	if err := projectsRepo.CreateProject(ctx, record); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	loaded, err := projectsRepo.GetProject(ctx, record.ID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if loaded.ID != record.ID || loaded.Name != record.Name || loaded.Type != project.ProjectFreeCanvas {
		t.Fatalf("round trip changed the row: %+v", loaded)
	}
	if loaded.Revision != 1 {
		t.Fatalf("revision = %d, want 1", loaded.Revision)
	}
	if loaded.Status != project.ProjectActive {
		t.Fatalf("status = %q", loaded.Status)
	}

	second := sampleProject(t, generator)
	second.Name = "Second"
	if err := projectsRepo.CreateProject(ctx, second); err != nil {
		t.Fatal(err)
	}
	list, err := projectsRepo.ListProjects(ctx, projects.ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("ListProjects returned %d rows, want 2", len(list))
	}
	count, err := projectsRepo.CountProjects(ctx, projects.ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("CountProjects = %d, want 2", count)
	}
}

// TestProjectRepositoryRejectsUnknownWorkspace proves the foreign key is
// enforced through the repository: a project cannot be created in a workspace
// that does not exist.
func TestProjectRepositoryRejectsUnknownWorkspace(t *testing.T) {
	projectsRepo, _, _ := openWP04Repo(t)
	ctx := context.Background()
	record := sampleProject(t, newTestIDGenerator())
	record.WorkspaceID = "00000000-0000-7000-8000-0000000000ff"
	if err := projectsRepo.CreateProject(ctx, record); err == nil {
		t.Fatal("a project was created in a workspace that does not exist")
	}
}

// TestProjectRepositoryGetMissingIsNotFound proves a missing row maps to the
// domain's not-found category rather than a raw SQL error.
func TestProjectRepositoryGetMissingIsNotFound(t *testing.T) {
	projectsRepo, _, _ := openWP04Repo(t)
	_, err := projectsRepo.GetProject(context.Background(), "00000000-0000-7000-8000-0000000000aa")
	if err == nil {
		t.Fatal("reading a missing project succeeded")
	}
	domainErr, ok := project.AsError(err)
	if !ok || domainErr.Category != project.CategoryNotFound {
		t.Fatalf("expected not_found, got %v", err)
	}
}

// TestProjectRepositoryUpdateGuardsRevision proves the compare-and-swap guard:
// a stale revision is a conflict and the row is untouched.
func TestProjectRepositoryUpdateGuardsRevision(t *testing.T) {
	projectsRepo, _, _ := openWP04Repo(t)
	ctx := context.Background()
	record := sampleProject(t, newTestIDGenerator())
	if err := projectsRepo.CreateProject(ctx, record); err != nil {
		t.Fatal(err)
	}

	record.Name = "Renamed"
	record.UpdatedAt = fixedClock()()
	if err := projectsRepo.UpdateProject(ctx, record, 1); err != nil {
		t.Fatalf("first update failed: %v", err)
	}
	after, err := projectsRepo.GetProject(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != 2 {
		t.Fatalf("revision = %d, want 2 after one update", after.Revision)
	}
	if after.Name != "Renamed" {
		t.Fatalf("name = %q, want the new name", after.Name)
	}

	// Replaying the same expected revision must fail and must not write.
	stale := after
	stale.Name = "Should not stick"
	stale.UpdatedAt = fixedClock()()
	err = projectsRepo.UpdateProject(ctx, stale, 1)
	if err == nil {
		t.Fatal("a stale revision was accepted")
	}
	domainErr, ok := project.AsError(err)
	if !ok || domainErr.Category != project.CategoryConflict {
		t.Fatalf("expected conflict, got %v", err)
	}
	reRead, err := projectsRepo.GetProject(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reRead.Name != "Renamed" {
		t.Fatalf("a rejected update wrote the row: %q", reRead.Name)
	}
}

// TestProjectRepositorySoftDeleteFilter proves a trashed project is hidden by
// default and visible on request.
func TestProjectRepositorySoftDeleteFilter(t *testing.T) {
	projectsRepo, _, _ := openWP04Repo(t)
	ctx := context.Background()
	record := sampleProject(t, newTestIDGenerator())
	if err := projectsRepo.CreateProject(ctx, record); err != nil {
		t.Fatal(err)
	}
	record.Status = project.ProjectTrashed
	record.DeletedAt = fixedClock()()
	record.UpdatedAt = fixedClock()()
	if err := projectsRepo.UpdateProject(ctx, record, 1); err != nil {
		t.Fatal(err)
	}
	visible, err := projectsRepo.ListProjects(ctx, projects.ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 0 {
		t.Fatalf("a trashed project is still listed: %+v", visible)
	}
	all, err := projectsRepo.ListProjects(ctx, projects.ListFilter{IncludeDeleted: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("IncludeDeleted returned %d rows, want 1", len(all))
	}
}
