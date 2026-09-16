package desktop

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	appprojects "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/projects"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/project"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/platform/id"
)

// projectFixtureRepository is an in-memory project repository, so the binding's
// validation and error mapping are tested without a database.
type projectFixtureRepository struct {
	mu       sync.Mutex
	projects map[string]project.Project
}

func newProjectFixtureRepository() *projectFixtureRepository {
	return &projectFixtureRepository{projects: map[string]project.Project{}}
}

func (r *projectFixtureRepository) CreateWorkspace(context.Context, project.Workspace) error {
	return nil
}

func (r *projectFixtureRepository) GetWorkspace(context.Context, string) (project.Workspace, error) {
	return project.Workspace{ID: project.DefaultLocalWorkspaceID, Name: "Local"}, nil
}

func (r *projectFixtureRepository) CreateProject(_ context.Context, record project.Project) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.projects[record.ID] = record
	return nil
}

func (r *projectFixtureRepository) GetProject(_ context.Context, id string) (project.Project, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.projects[id]
	if !ok {
		return project.Project{}, project.NotFoundError()
	}
	return record, nil
}

func (r *projectFixtureRepository) ListProjects(_ context.Context, _ appprojects.ListFilter) ([]project.Project, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	records := make([]project.Project, 0, len(r.projects))
	for _, record := range r.projects {
		records = append(records, record)
	}
	return records, nil
}

func (r *projectFixtureRepository) CountProjects(context.Context, appprojects.ListFilter) (int, error) {
	return len(r.projects), nil
}

func (r *projectFixtureRepository) UpdateProject(_ context.Context, record project.Project, expected int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.projects[record.ID]
	if !ok {
		return project.NotFoundError()
	}
	if current.Revision != expected {
		return project.RevisionMismatchError()
	}
	record.Revision = expected + 1
	r.projects[record.ID] = record
	return nil
}

func (r *projectFixtureRepository) DeleteProject(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.projects[id]; !ok {
		return project.NotFoundError()
	}
	delete(r.projects, id)
	return nil
}

// canvasFixtureRepository is an in-memory canvas repository.
type canvasFixtureRepository struct {
	mu        sync.Mutex
	documents map[string]project.CanvasDocument
	nodes     map[string]project.Node
	edges     map[string]project.Edge
	sessions  map[string]project.ChatSession
}

func newCanvasFixtureRepository() *canvasFixtureRepository {
	return &canvasFixtureRepository{
		documents: map[string]project.CanvasDocument{},
		nodes:     map[string]project.Node{},
		edges:     map[string]project.Edge{},
		sessions:  map[string]project.ChatSession{},
	}
}

func (r *canvasFixtureRepository) CreateDocument(_ context.Context, document project.CanvasDocument) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.documents[document.ID] = document
	return nil
}

func (r *canvasFixtureRepository) GetDocument(_ context.Context, id string) (project.CanvasDocument, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	document, ok := r.documents[id]
	if !ok {
		return project.CanvasDocument{}, project.NotFoundError()
	}
	return document, nil
}

func (r *canvasFixtureRepository) ListDocuments(_ context.Context, projectID string) ([]project.CanvasDocument, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var documents []project.CanvasDocument
	for _, document := range r.documents {
		if document.ProjectID == projectID {
			documents = append(documents, document)
		}
	}
	return documents, nil
}

func (r *canvasFixtureRepository) UpdateDocument(_ context.Context, document project.CanvasDocument, expected int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.documents[document.ID]
	if !ok {
		return project.NotFoundError()
	}
	if current.Revision != expected {
		return project.RevisionMismatchError()
	}
	document.Revision = expected + 1
	r.documents[document.ID] = document
	return nil
}

func (r *canvasFixtureRepository) CreateNode(_ context.Context, node project.Node) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nodes[node.ID] = node
	return nil
}

func (r *canvasFixtureRepository) GetNode(_ context.Context, id string) (project.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	node, ok := r.nodes[id]
	if !ok {
		return project.Node{}, project.NotFoundError()
	}
	return node, nil
}

func (r *canvasFixtureRepository) ListNodes(_ context.Context, documentID string) ([]project.Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var nodes []project.Node
	for _, node := range r.nodes {
		if node.CanvasDocumentID == documentID {
			nodes = append(nodes, node)
		}
	}
	return nodes, nil
}

func (r *canvasFixtureRepository) UpdateNode(_ context.Context, node project.Node, expected int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	current, ok := r.nodes[node.ID]
	if !ok {
		return project.NotFoundError()
	}
	if current.Revision != expected {
		return project.RevisionMismatchError()
	}
	node.Revision = expected + 1
	r.nodes[node.ID] = node
	return nil
}

func (r *canvasFixtureRepository) DeleteNode(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.nodes, id)
	return nil
}

func (r *canvasFixtureRepository) CountNodes(_ context.Context, documentID string) (int, error) {
	nodes, _ := r.ListNodes(context.Background(), documentID)
	return len(nodes), nil
}

func (r *canvasFixtureRepository) CreateEdge(_ context.Context, edge project.Edge) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.edges[edge.ID] = edge
	return nil
}

func (r *canvasFixtureRepository) ListEdges(_ context.Context, documentID string) ([]project.Edge, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var edges []project.Edge
	for _, edge := range r.edges {
		if edge.CanvasDocumentID == documentID {
			edges = append(edges, edge)
		}
	}
	return edges, nil
}

func (r *canvasFixtureRepository) UpdateEdge(_ context.Context, edge project.Edge, expected int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	edge.Revision = expected + 1
	r.edges[edge.ID] = edge
	return nil
}

func (r *canvasFixtureRepository) DeleteEdge(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.edges, id)
	return nil
}

func (r *canvasFixtureRepository) CountEdges(_ context.Context, documentID string) (int, error) {
	edges, _ := r.ListEdges(context.Background(), documentID)
	return len(edges), nil
}

func (r *canvasFixtureRepository) CreateChatSession(_ context.Context, session project.ChatSession) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[session.ID] = session
	return nil
}

func (r *canvasFixtureRepository) GetChatSession(_ context.Context, id string) (project.ChatSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	session, ok := r.sessions[id]
	if !ok {
		return project.ChatSession{}, project.NotFoundError()
	}
	return session, nil
}

func (r *canvasFixtureRepository) ListChatSessions(_ context.Context, documentID string) ([]project.ChatSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var sessions []project.ChatSession
	for _, session := range r.sessions {
		if session.CanvasDocumentID == documentID {
			sessions = append(sessions, session)
		}
	}
	return sessions, nil
}

func (r *canvasFixtureRepository) UpdateChatSession(_ context.Context, session project.ChatSession, expected int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	session.Revision = expected + 1
	r.sessions[session.ID] = session
	return nil
}

func fixedIDs() *id.Generator {
	return id.NewGeneratorWithClock(func() time.Time { return time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC) })
}

// attachProjectsFixture builds a binding over in-memory repositories.
func attachProjectsFixture(t *testing.T) *ProjectsBinding {
	t.Helper()
	service := appprojects.NewService(appprojects.Options{
		Projects: newProjectFixtureRepository(),
		Canvas:   newCanvasFixtureRepository(),
		IDs:      fixedIDs(),
		Clock:    fixedClock{},
	})
	binding := &ProjectsBinding{}
	AttachProjects(binding, context.Background(), service)
	return binding
}

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC) }

// TestProjectsBindingFailsClosedWhenUnattached proves every method refuses
// before a service exists.
func TestProjectsBindingFailsClosedWhenUnattached(t *testing.T) {
	binding := &ProjectsBinding{}
	if _, err := binding.CreateProject(CreateProjectRequest{Name: "x", ProjectType: "free_canvas"}); err == nil {
		t.Fatal("CreateProject succeeded while unattached")
	}
	if _, err := binding.ListProjects(ListProjectsRequest{}); err == nil {
		t.Fatal("ListProjects succeeded while unattached")
	}
	if _, err := binding.GetProject("id"); err == nil {
		t.Fatal("GetProject succeeded while unattached")
	}
	if _, err := binding.LoadCanvas("id"); err == nil {
		t.Fatal("LoadCanvas succeeded while unattached")
	}
	if _, err := binding.MoveNodes(MoveNodesRequest{}); err == nil {
		t.Fatal("MoveNodes succeeded while unattached")
	}
	if _, err := binding.CreateEdge(CreateEdgeRequest{}); err == nil {
		t.Fatal("CreateEdge succeeded while unattached")
	}
	if _, err := binding.PrecheckImport("{}"); err == nil {
		t.Fatal("PrecheckImport succeeded while unattached")
	}
	if _, err := binding.ImportProjects(ImportProjectsRequest{}); err == nil {
		t.Fatal("ImportProjects succeeded while unattached")
	}
	if _, err := binding.ListImports(10); err == nil {
		t.Fatal("ListImports succeeded while unattached")
	}
	if err := binding.DeleteProject("id"); err == nil {
		t.Fatal("DeleteProject succeeded while unattached")
	}
}

// TestProjectsBindingCreateAndLoadCanvas proves a project and its canvas round
// trip through the binding, which is what the canvas adapter calls.
func TestProjectsBindingCreateAndLoadCanvas(t *testing.T) {
	binding := attachProjectsFixture(t)
	created, err := binding.CreateProject(CreateProjectRequest{
		Name: "A project", Description: "desc", ProjectType: "free_canvas", Language: "zh-CN",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if created.ID == "" || created.Revision != 1 {
		t.Fatalf("created = %+v", created)
	}
	if created.ProjectType != "free_canvas" {
		t.Fatalf("project type = %q", created.ProjectType)
	}

	snapshot, err := binding.LoadCanvas(created.ID)
	if err != nil {
		t.Fatalf("LoadCanvas: %v", err)
	}
	if snapshot.Project.ID != created.ID {
		t.Fatalf("snapshot project = %+v", snapshot.Project)
	}
	if snapshot.DocumentID == "" {
		t.Fatal("the project has no canvas document")
	}
	if snapshot.Viewport.K != 1 {
		t.Fatalf("a new canvas viewport = %+v, want the identity", snapshot.Viewport)
	}
	if len(snapshot.Nodes) != 0 || len(snapshot.Edges) != 0 {
		t.Fatalf("a new canvas is not empty: %+v", snapshot)
	}
}

// TestProjectsBindingNodeLifecycle proves the node commands the canvas uses.
func TestProjectsBindingNodeLifecycle(t *testing.T) {
	binding := attachProjectsFixture(t)
	created, err := binding.CreateProject(CreateProjectRequest{Name: "Canvas", ProjectType: "free_canvas"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := binding.LoadCanvas(created.ID)
	if err != nil {
		t.Fatal(err)
	}

	node, err := binding.UpsertNode(UpsertNodeRequest{
		DocumentID: snapshot.DocumentID, NodeType: "text", Title: "One",
		PositionX: 10, PositionY: 20, Width: 200, Height: 100,
		UIState: `{"content":"hello"}`,
	})
	if err != nil {
		t.Fatalf("UpsertNode create: %v", err)
	}
	if node.Revision != 1 || node.NodeType != "text" {
		t.Fatalf("node = %+v", node)
	}

	// A second node, then a connection between them.
	second, err := binding.UpsertNode(UpsertNodeRequest{
		DocumentID: snapshot.DocumentID, NodeType: "text", Title: "Two",
		PositionX: 300, PositionY: 20, Width: 200, Height: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	edge, err := binding.CreateEdge(CreateEdgeRequest{
		DocumentID: snapshot.DocumentID, FromNodeID: node.ID, ToNodeID: second.ID,
	})
	if err != nil {
		t.Fatalf("CreateEdge: %v", err)
	}
	// An omitted relation type becomes the generic relation, which is where a
	// migrated untyped connection lands.
	if edge.RelationType != string(project.RelationGeneric) {
		t.Fatalf("relation = %q, want generic", edge.RelationType)
	}

	// Move the first node.
	applied, err := binding.MoveNodes(MoveNodesRequest{
		DocumentID: snapshot.DocumentID,
		Positions:  []NodePositionDTO{{ID: node.ID, X: 50, Y: 60, Revision: node.Revision}},
	})
	if err != nil {
		t.Fatalf("MoveNodes: %v", err)
	}
	if applied != 1 {
		t.Fatalf("applied %d moves, want 1", applied)
	}

	// Update it through the same upsert path.
	updated, err := binding.UpsertNode(UpsertNodeRequest{
		ID: node.ID, NodeType: "text", Title: "One renamed",
		PositionX: 50, PositionY: 60, Width: 200, Height: 100, Revision: 2,
	})
	if err != nil {
		t.Fatalf("UpsertNode update: %v", err)
	}
	if updated.Title != "One renamed" {
		t.Fatalf("title = %q", updated.Title)
	}

	// Delete both nodes; the connection goes with them in a real database.
	deleted, err := binding.DeleteNodes([]string{node.ID, second.ID})
	if err != nil {
		t.Fatalf("DeleteNodes: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("deleted %d nodes, want 2", deleted)
	}
}

// TestProjectsBindingRejectsStaleRevision proves a concurrent edit is reported
// as a conflict rather than overwritten.
func TestProjectsBindingRejectsStaleRevision(t *testing.T) {
	binding := attachProjectsFixture(t)
	created, err := binding.CreateProject(CreateProjectRequest{Name: "P", ProjectType: "free_canvas"})
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := binding.RenameProject(RenameProjectRequest{ID: created.ID, Name: "First", Revision: 1})
	if err != nil {
		t.Fatalf("RenameProject: %v", err)
	}
	if renamed.Revision != 2 {
		t.Fatalf("revision = %d, want 2", renamed.Revision)
	}
	// Replaying the old revision must fail with a stable conflict code.
	_, err = binding.RenameProject(RenameProjectRequest{ID: created.ID, Name: "Second", Revision: 1})
	if err == nil {
		t.Fatal("a stale revision was accepted")
	}
	appErr, ok := err.(*apperror.Error)
	if !ok {
		t.Fatalf("the error is not an application error: %v", err)
	}
	if appErr.Code != "PROJECT_CONFLICT" {
		t.Fatalf("code = %q, want PROJECT_CONFLICT", appErr.Code)
	}
	if !strings.Contains(strings.ToLower(appErr.SafeMessage), "changed") {
		t.Fatalf("the message does not explain the conflict: %q", appErr.SafeMessage)
	}
}

// TestProjectsBindingRejectsInvalidInput proves validation happens before any
// write, and that the error is a stable invalid-input code.
func TestProjectsBindingRejectsInvalidInput(t *testing.T) {
	binding := attachProjectsFixture(t)
	if _, err := binding.CreateProject(CreateProjectRequest{Name: "   ", ProjectType: "free_canvas"}); err == nil {
		t.Fatal("a blank name was accepted")
	}
	if _, err := binding.CreateProject(CreateProjectRequest{Name: "x", ProjectType: "not_a_type"}); err == nil {
		t.Fatal("an unknown project type was accepted")
	}
	// A batch over the bound is refused rather than attempted.
	positions := make([]NodePositionDTO, maxBatchNodes+1)
	_, err := binding.MoveNodes(MoveNodesRequest{DocumentID: "d", Positions: positions})
	if err == nil {
		t.Fatal("an oversize batch was accepted")
	}
	appErr, ok := err.(*apperror.Error)
	if !ok || appErr.Code != "DESKTOP_BINDING_INVALID_INPUT" {
		t.Fatalf("code = %v", err)
	}
}

// TestProjectsBindingRejectsUnknownRelation proves the relation registry is
// enforced at the boundary.
func TestProjectsBindingRejectsUnknownRelation(t *testing.T) {
	binding := attachProjectsFixture(t)
	created, err := binding.CreateProject(CreateProjectRequest{Name: "P", ProjectType: "free_canvas"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := binding.LoadCanvas(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = binding.CreateEdge(CreateEdgeRequest{
		DocumentID: snapshot.DocumentID, FromNodeID: "a", ToNodeID: "b", RelationType: "points_at",
	})
	if err == nil {
		t.Fatal("an unregistered relation type was accepted")
	}
	if appErr, ok := err.(*apperror.Error); !ok || appErr.Code != "PROJECT_INVALID_INPUT" {
		t.Fatalf("code = %v", err)
	}
}

// TestProjectsBindingReturnsNoPaths proves the DTOs carry no filesystem path:
// a canvas row has no path column, and the transport view must not invent one.
func TestProjectsBindingReturnsNoPaths(t *testing.T) {
	binding := attachProjectsFixture(t)
	created, err := binding.CreateProject(CreateProjectRequest{Name: "P", ProjectType: "free_canvas"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := binding.LoadCanvas(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	node, err := binding.UpsertNode(UpsertNodeRequest{
		DocumentID: snapshot.DocumentID, NodeType: "image", Title: "T",
		UIState: `{"content":"image:abc"}`, LegacyMetadata: `{"legacy":"kept"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	// The legacy metadata is returned verbatim so the canvas can round-trip it.
	if node.LegacyMetadata != `{"legacy":"kept"}` {
		t.Fatalf("legacy metadata = %q", node.LegacyMetadata)
	}
	if strings.Contains(node.ID, "/") || strings.Contains(node.ID, "\\") {
		t.Fatalf("the identifier looks like a path: %q", node.ID)
	}
}

// TestProjectsBindingImportSnapshotJSONBounds proves the snapshot document is
// bounded before it is parsed.
func TestProjectsBindingImportSnapshotJSONBounds(t *testing.T) {
	binding := attachProjectsFixture(t)
	oversize := strings.Repeat("x", maxSnapshotBytes+1)
	if _, err := binding.PrecheckImport(oversize); err == nil {
		t.Fatal("an oversize snapshot was accepted")
	}
}
