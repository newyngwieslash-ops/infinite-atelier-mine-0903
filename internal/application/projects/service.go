package projects

import (
	"context"
	"strings"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/project"
)

// NewService builds the project service.
func NewService(options Options) *Service {
	return &Service{
		projects: options.Projects,
		canvas:   options.Canvas,
		clock:    options.Clock,
		ids:      options.IDs,
	}
}

// Available reports whether the service has the dependencies it needs. An
// unattached binding fails closed rather than panicking.
func (s *Service) Available() bool {
	return s != nil && s.projects != nil && s.ids != nil
}

func (s *Service) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now().UTC()
	}
	return s.clock.Now().UTC()
}

// CreateProjectRequest is a caller's request to create a project.
type CreateProjectRequest struct {
	WorkspaceID string
	Type        project.ProjectType
	Name        string
	Description string
	Language    string
}

// CreateProject stores a new project and its default free canvas.
//
// The canvas is created in the same call because a project without a canvas
// cannot be opened by the UI, and creating it later would mean every caller
// had to know that. The pair is not atomic across tables here — the canvas
// creation is a separate repository call — so a failure between them leaves a
// project whose canvas list is empty, which the read path tolerates and the
// caller can retry. The import path, which needs atomicity, uses its own
// transaction.
func (s *Service) CreateProject(ctx context.Context, request CreateProjectRequest) (project.Project, error) {
	if !s.Available() {
		return project.Project{}, storageFailure()
	}
	if !project.IsValidProjectType(request.Type) {
		return project.Project{}, project.InvalidError("That project type is not supported.")
	}
	if err := project.ValidateProjectName(request.Name); err != nil {
		return project.Project{}, err
	}
	workspaceID := strings.TrimSpace(request.WorkspaceID)
	if workspaceID == "" {
		workspaceID = project.DefaultLocalWorkspaceID
	}
	// The workspace must exist: the schema has a foreign key, and resolving it
	// here turns a database error into a domain error.
	if _, err := s.projects.GetWorkspace(ctx, workspaceID); err != nil {
		return project.Project{}, err
	}

	id, err := s.ids.New()
	if err != nil {
		return project.Project{}, storageFailure()
	}
	now := s.now()
	record := project.Project{
		ID:          id,
		WorkspaceID: workspaceID,
		Type:        request.Type,
		Name:        strings.TrimSpace(request.Name),
		Description: request.Description,
		Language:    defaultLanguage(request.Language),
		Status:      project.ProjectActive,
		CreatedAt:   now,
		UpdatedAt:   now,
		Revision:    1,
	}
	if err := s.projects.CreateProject(ctx, record); err != nil {
		return project.Project{}, err
	}

	if s.canvas != nil {
		documentID, idErr := s.ids.New()
		if idErr != nil {
			return record, storageFailure()
		}
		document := project.CanvasDocument{
			ID:        documentID,
			ProjectID: record.ID,
			Name:      record.Name,
			Kind:      project.CanvasFree,
			Viewport:  project.Viewport{K: 1},
			CreatedAt: now,
			UpdatedAt: now,
			Revision:  1,
		}
		if err := s.canvas.CreateDocument(ctx, document); err != nil {
			// The project exists; report the failure so the caller can retry the
			// canvas rather than believing the project is unusable.
			return record, err
		}
	}
	return record, nil
}

func defaultLanguage(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "zh-CN"
	}
	return trimmed
}

// GetProject returns one project.
func (s *Service) GetProject(ctx context.Context, id string) (project.Project, error) {
	if !s.Available() {
		return project.Project{}, storageFailure()
	}
	return s.projects.GetProject(ctx, id)
}

// ListProjects returns projects newest first.
func (s *Service) ListProjects(ctx context.Context, filter ListFilter) ([]project.Project, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	return s.projects.ListProjects(ctx, filter)
}

// RenameProjectRequest carries a rename.
type RenameProjectRequest struct {
	ID       string
	Name     string
	Revision int64
}

// RenameProject changes a project's name under a revision guard.
func (s *Service) RenameProject(ctx context.Context, request RenameProjectRequest) (project.Project, error) {
	if !s.Available() {
		return project.Project{}, storageFailure()
	}
	if err := project.ValidateProjectName(request.Name); err != nil {
		return project.Project{}, err
	}
	record, err := s.projects.GetProject(ctx, request.ID)
	if err != nil {
		return project.Project{}, err
	}
	record.Name = strings.TrimSpace(request.Name)
	record.UpdatedAt = s.now()
	if err := s.projects.UpdateProject(ctx, record, request.Revision); err != nil {
		return project.Project{}, err
	}
	record.Revision = request.Revision + 1
	return record, nil
}

// SetProjectStatusRequest carries a lifecycle change.
type SetProjectStatusRequest struct {
	ID       string
	Status   project.ProjectStatus
	Revision int64
}

// SetProjectStatus archives, restores or trashes a project.
func (s *Service) SetProjectStatus(ctx context.Context, request SetProjectStatusRequest) (project.Project, error) {
	if !s.Available() {
		return project.Project{}, storageFailure()
	}
	if !project.IsValidProjectStatus(request.Status) {
		return project.Project{}, project.InvalidError("That project state is not recognised.")
	}
	record, err := s.projects.GetProject(ctx, request.ID)
	if err != nil {
		return project.Project{}, err
	}
	if !record.CanTransition(request.Status) {
		return project.Project{}, project.ConflictError("The project cannot move to that state from its current one.")
	}
	now := s.now()
	record.Status = request.Status
	record.UpdatedAt = now
	// Trashing sets the soft-delete stamp as well, because DOMAIN_MODEL §2.4
	// treats the stamp as the record of the deletion; restoring clears it.
	if request.Status == project.ProjectTrashed {
		record.DeletedAt = now
	} else {
		record.DeletedAt = time.Time{}
		record.DeletedBy = ""
	}
	if err := s.projects.UpdateProject(ctx, record, request.Revision); err != nil {
		return project.Project{}, err
	}
	record.Revision = request.Revision + 1
	return record, nil
}

// DeleteProject permanently removes a project and everything under it.
//
// DOMAIN_MODEL §2.4 allows physical deletion only for drafts, explicitly
// confirmed deletions and security cleanup. This is the explicitly confirmed
// path: trashing is the default, and reaching here means the user chose to
// remove the data. The cascades in migration 000004 remove the canvas, nodes,
// edges, chats and history with it.
func (s *Service) DeleteProject(ctx context.Context, id string) error {
	if !s.Available() {
		return storageFailure()
	}
	if strings.TrimSpace(id) == "" {
		return project.InvalidError("A project id is required.")
	}
	return s.projects.DeleteProject(ctx, id)
}

// CanvasFor returns a project's primary canvas document.
//
// A project whose canvas is missing (a partial create) returns not-found
// rather than an empty document, so the caller can distinguish "no canvas yet"
// from "this project has an empty canvas".
func (s *Service) CanvasFor(ctx context.Context, projectID string) (project.CanvasDocument, error) {
	if !s.Available() || s.canvas == nil {
		return project.CanvasDocument{}, storageFailure()
	}
	documents, err := s.canvas.ListDocuments(ctx, projectID)
	if err != nil {
		return project.CanvasDocument{}, err
	}
	if len(documents) == 0 {
		return project.CanvasDocument{}, project.NotFoundError()
	}
	return documents[0], nil
}

// CanvasSnapshot is everything the canvas UI needs to render a project.
type CanvasSnapshot struct {
	Project      project.Project
	Document     project.CanvasDocument
	Nodes        []project.Node
	Edges        []project.Edge
	ChatSessions []project.ChatSession
}

// LoadCanvas returns the full canvas projection for a project.
func (s *Service) LoadCanvas(ctx context.Context, projectID string) (CanvasSnapshot, error) {
	if !s.Available() || s.canvas == nil {
		return CanvasSnapshot{}, storageFailure()
	}
	record, err := s.projects.GetProject(ctx, projectID)
	if err != nil {
		return CanvasSnapshot{}, err
	}
	document, err := s.CanvasFor(ctx, projectID)
	if err != nil {
		return CanvasSnapshot{}, err
	}
	nodes, err := s.canvas.ListNodes(ctx, document.ID)
	if err != nil {
		return CanvasSnapshot{}, err
	}
	edges, err := s.canvas.ListEdges(ctx, document.ID)
	if err != nil {
		return CanvasSnapshot{}, err
	}
	sessions, err := s.canvas.ListChatSessions(ctx, document.ID)
	if err != nil {
		return CanvasSnapshot{}, err
	}
	return CanvasSnapshot{Project: record, Document: document, Nodes: nodes, Edges: edges, ChatSessions: sessions}, nil
}

// UpdateViewportRequest persists a pan/zoom change.
//
// A viewport is canvas-only state (ARCHITECTURE §9.1: "仅移动、缩放、折叠：只改
// Canvas"), so it never touches a domain entity.
type UpdateViewportRequest struct {
	DocumentID string
	Viewport   project.Viewport
	Revision   int64
}

// UpdateViewport stores a canvas viewport.
func (s *Service) UpdateViewport(ctx context.Context, request UpdateViewportRequest) (project.CanvasDocument, error) {
	if !s.Available() || s.canvas == nil {
		return project.CanvasDocument{}, storageFailure()
	}
	if !request.Viewport.IsUsable() {
		return project.CanvasDocument{}, project.InvalidError("The viewport is not usable.")
	}
	document, err := s.canvas.GetDocument(ctx, request.DocumentID)
	if err != nil {
		return project.CanvasDocument{}, err
	}
	document.Viewport = request.Viewport
	document.UpdatedAt = s.now()
	if err := s.canvas.UpdateDocument(ctx, document, request.Revision); err != nil {
		return project.CanvasDocument{}, err
	}
	document.Revision = request.Revision + 1
	return document, nil
}

// NodeInput is the caller-supplied shape of a node write.
type NodeInput struct {
	ID             string
	NodeType       string
	Title          string
	EntityType     string
	EntityID       string
	PositionX      float64
	PositionY      float64
	Width          float64
	Height         float64
	ZIndex         int
	UIState        string
	LegacyMetadata string
}

// CreateNode adds a node to a canvas.
func (s *Service) CreateNode(ctx context.Context, documentID string, input NodeInput) (project.Node, error) {
	if !s.Available() || s.canvas == nil {
		return project.Node{}, storageFailure()
	}
	id := strings.TrimSpace(input.ID)
	if id == "" {
		generated, err := s.ids.New()
		if err != nil {
			return project.Node{}, storageFailure()
		}
		id = generated
	}
	now := s.now()
	node := project.Node{
		ID:               id,
		CanvasDocumentID: documentID,
		NodeType:         strings.TrimSpace(input.NodeType),
		Title:            input.Title,
		EntityType:       strings.TrimSpace(input.EntityType),
		EntityID:         strings.TrimSpace(input.EntityID),
		PositionX:        input.PositionX,
		PositionY:        input.PositionY,
		Width:            input.Width,
		Height:           input.Height,
		ZIndex:           input.ZIndex,
		UIState:          input.UIState,
		LegacyMetadata:   input.LegacyMetadata,
		CreatedAt:        now,
		UpdatedAt:        now,
		Revision:         1,
	}
	if err := project.ValidateNode(node); err != nil {
		return project.Node{}, err
	}
	if err := s.canvas.CreateNode(ctx, node); err != nil {
		return project.Node{}, err
	}
	return node, nil
}

// UpdateNodeRequest replaces a node's stored fields.
//
// A canvas save sends the node's current shape, so this is a full replace
// rather than a patch: a field the caller omits becomes its zero value, which
// is what makes the stored row match what the canvas rendered.
type UpdateNodeRequest struct {
	ID             string
	NodeType       string
	Title          string
	EntityType     string
	EntityID       string
	PositionX      float64
	PositionY      float64
	Width          float64
	Height         float64
	ZIndex         int
	UIState        string
	LegacyMetadata string
	Revision       int64
}

// UpdateNode stores a node's new shape under a revision guard.
func (s *Service) UpdateNode(ctx context.Context, request UpdateNodeRequest) (project.Node, error) {
	if !s.Available() || s.canvas == nil {
		return project.Node{}, storageFailure()
	}
	node, err := s.canvas.GetNode(ctx, request.ID)
	if err != nil {
		return project.Node{}, err
	}
	node.NodeType = strings.TrimSpace(request.NodeType)
	node.Title = request.Title
	node.EntityType = strings.TrimSpace(request.EntityType)
	node.EntityID = strings.TrimSpace(request.EntityID)
	node.PositionX = request.PositionX
	node.PositionY = request.PositionY
	node.Width = request.Width
	node.Height = request.Height
	node.ZIndex = request.ZIndex
	node.UIState = request.UIState
	node.LegacyMetadata = request.LegacyMetadata
	node.UpdatedAt = s.now()
	if err := project.ValidateNode(node); err != nil {
		return project.Node{}, err
	}
	if err := s.canvas.UpdateNode(ctx, node, request.Revision); err != nil {
		return project.Node{}, err
	}
	node.Revision = request.Revision + 1
	return node, nil
}

// MoveNodesRequest moves one or more nodes in a single pass.
type MoveNodesRequest struct {
	DocumentID string
	Positions  []NodePosition
}

// NodePosition is one node's new placement.
type NodePosition struct {
	ID       string
	X        float64
	Y        float64
	ZIndex   int
	Revision int64
}

// MoveNodes stores new node positions.
//
// A move is canvas-only state, so it never sends a domain command
// (ARCHITECTURE §9.1). Each node is updated under its own revision guard; a
// node that changed elsewhere is skipped rather than silently relocated, and
// the count of applied moves is returned so the caller can report a partial
// result instead of assuming success.
func (s *Service) MoveNodes(ctx context.Context, request MoveNodesRequest) (int, error) {
	if !s.Available() || s.canvas == nil {
		return 0, storageFailure()
	}
	if len(request.Positions) == 0 {
		return 0, nil
	}
	if len(request.Positions) > MaxBatchMoves {
		return 0, project.InvalidError("Too many nodes in one move.")
	}
	now := s.now()
	applied := 0
	for _, position := range request.Positions {
		node, err := s.canvas.GetNode(ctx, position.ID)
		if err != nil {
			// A node deleted by another window is skipped, not fatal.
			continue
		}
		node.PositionX = position.X
		node.PositionY = position.Y
		if position.ZIndex != 0 {
			node.ZIndex = position.ZIndex
		}
		node.UpdatedAt = now
		if err := s.canvas.UpdateNode(ctx, node, position.Revision); err != nil {
			continue
		}
		applied++
	}
	return applied, nil
}

// MaxBatchMoves bounds one move command, so a malformed request cannot make a
// single call walk an unbounded list.
const MaxBatchMoves = 5000

// DeleteNodes removes nodes and reports how many were removed.
//
// Deleting a projection does not delete a domain entity (DOMAIN_MODEL §10.2):
// only the canvas row goes away. The edges that referenced a removed node are
// removed with it by the schema's cascade.
func (s *Service) DeleteNodes(ctx context.Context, ids []string) (int, error) {
	if !s.Available() || s.canvas == nil {
		return 0, storageFailure()
	}
	if len(ids) == 0 {
		return 0, nil
	}
	if len(ids) > MaxBatchMoves {
		return 0, project.InvalidError("Too many nodes in one delete.")
	}
	deleted := 0
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			continue
		}
		if err := s.canvas.DeleteNode(ctx, id); err != nil {
			continue
		}
		deleted++
	}
	return deleted, nil
}

// CreateEdgeRequest adds a connection.
type CreateEdgeRequest struct {
	DocumentID   string
	FromNodeID   string
	ToNodeID     string
	RelationType project.RelationType
	FromPort     string
	ToPort       string
	Required     bool
	Metadata     string
}

// CreateEdge adds a connection to a canvas.
//
// The relation type is validated against the registry before the write, so a
// semantic edge cannot be created with an unregistered meaning
// (ARCHITECTURE §9.1: "创建语义连线：校验关系注册表后写领域关系/投影关系").
func (s *Service) CreateEdge(ctx context.Context, request CreateEdgeRequest) (project.Edge, error) {
	if !s.Available() || s.canvas == nil {
		return project.Edge{}, storageFailure()
	}
	relation := request.RelationType
	if relation == "" {
		relation = project.RelationGeneric
	}
	if !project.IsValidRelationType(relation) {
		return project.Edge{}, project.InvalidError("That connection type is not recognised.")
	}
	if strings.TrimSpace(request.FromNodeID) == "" || strings.TrimSpace(request.ToNodeID) == "" {
		return project.Edge{}, project.InvalidError("A connection needs both endpoints.")
	}
	id, err := s.ids.New()
	if err != nil {
		return project.Edge{}, storageFailure()
	}
	now := s.now()
	edge := project.Edge{
		ID:               id,
		CanvasDocumentID: request.DocumentID,
		FromNodeID:       request.FromNodeID,
		ToNodeID:         request.ToNodeID,
		RelationType:     relation,
		FromPort:         request.FromPort,
		ToPort:           request.ToPort,
		Required:         request.Required,
		ValidationStatus: project.EdgeUnknown,
		Metadata:         request.Metadata,
		CreatedAt:        now,
		UpdatedAt:        now,
		Revision:         1,
	}
	if err := s.canvas.CreateEdge(ctx, edge); err != nil {
		return project.Edge{}, err
	}
	return edge, nil
}

// DeleteEdges removes connections and reports how many were removed.
func (s *Service) DeleteEdges(ctx context.Context, ids []string) (int, error) {
	if !s.Available() || s.canvas == nil {
		return 0, storageFailure()
	}
	if len(ids) == 0 {
		return 0, nil
	}
	if len(ids) > MaxBatchMoves {
		return 0, project.InvalidError("Too many connections in one delete.")
	}
	deleted := 0
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			continue
		}
		if err := s.canvas.DeleteEdge(ctx, id); err != nil {
			continue
		}
		deleted++
	}
	return deleted, nil
}

// SaveChatSessionRequest stores a chat session's message list.
type SaveChatSessionRequest struct {
	ID               string
	CanvasDocumentID string
	Title            string
	MessagesJSON     string
	Revision         int64
}

// SaveChatSession creates or updates a chat session.
func (s *Service) SaveChatSession(ctx context.Context, request SaveChatSessionRequest) (project.ChatSession, error) {
	if !s.Available() || s.canvas == nil {
		return project.ChatSession{}, storageFailure()
	}
	if strings.TrimSpace(request.CanvasDocumentID) == "" {
		return project.ChatSession{}, project.InvalidError("A chat session needs a canvas.")
	}
	now := s.now()
	if strings.TrimSpace(request.ID) == "" {
		id, err := s.ids.New()
		if err != nil {
			return project.ChatSession{}, storageFailure()
		}
		session := project.ChatSession{
			ID:               id,
			CanvasDocumentID: request.CanvasDocumentID,
			Title:            request.Title,
			MessagesJSON:     nonEmptyJSON(request.MessagesJSON),
			CreatedAt:        now,
			UpdatedAt:        now,
			Revision:         1,
		}
		if err := s.canvas.CreateChatSession(ctx, session); err != nil {
			return project.ChatSession{}, err
		}
		return session, nil
	}
	session, err := s.canvas.GetChatSession(ctx, request.ID)
	if err != nil {
		return project.ChatSession{}, err
	}
	session.Title = request.Title
	session.MessagesJSON = nonEmptyJSON(request.MessagesJSON)
	session.UpdatedAt = now
	if err := s.canvas.UpdateChatSession(ctx, session, request.Revision); err != nil {
		return project.ChatSession{}, err
	}
	session.Revision = request.Revision + 1
	return session, nil
}

// nonEmptyJSON keeps the message column valid JSON: the schema defaults it to
// an empty array, and a caller with nothing to store should not write a blank
// string that a reader would fail to parse.
func nonEmptyJSON(value string) string {
	if strings.TrimSpace(value) == "" {
		return "[]"
	}
	return value
}

// storageFailure is the fail-closed error for an unattached service.
func storageFailure() error {
	return project.StorageError("The project store is unavailable.", nil)
}
