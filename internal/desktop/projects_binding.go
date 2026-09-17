package desktop

import (
	"context"
	"sync"

	appprojects "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/projects"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/project"
)

// ProjectsBinding is the narrow Wails surface for projects and their canvases.
//
// It exposes the commands and queries the canvas needs and nothing else: no
// SQL, no arbitrary path, no file bytes (a stored artifact is read through the
// job result reader by content hash, not by path).
type ProjectsBinding struct {
	mu      sync.RWMutex
	ctx     context.Context
	service *appprojects.Service
	imports ImportService
}

// ImportService is the legacy import surface this binding exposes, kept as an
// interface so the binding does not depend on the import package's internals.
type ImportService interface {
	// Precheck reports what an import would do, without writing projects.
	Precheck(ctx context.Context, snapshotJSON string) (string, error)
	// Import writes a snapshot atomically.
	Import(ctx context.Context, snapshotJSON string, mode, sourceCase, legacyRoot string) (string, error)
	// ListImports returns recent runs newest first.
	ListImports(ctx context.Context, limit int) (string, error)
}

// AttachProjects supplies the services. A nil service leaves the binding
// unattached, and every method then fails closed.
func AttachProjects(binding *ProjectsBinding, ctx context.Context, service *appprojects.Service) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.ctx = ctx
	binding.service = service
	binding.mu.Unlock()
}

// AttachProjectImport supplies the import surface.
func AttachProjectImport(binding *ProjectsBinding, imports ImportService) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.imports = imports
	binding.mu.Unlock()
}

func (b *ProjectsBinding) context() context.Context {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.ctx == nil {
		return context.Background()
	}
	return b.ctx
}

func (b *ProjectsBinding) projectService() *appprojects.Service {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.service
}

func (b *ProjectsBinding) importService() ImportService {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.imports
}

// ProjectDTO is the transport view of a project.
type ProjectDTO struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	ProjectType string `json:"projectType"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Language    string `json:"language"`
	Status      string `json:"status"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
	Revision    int64  `json:"revision"`
}

// ViewportDTO is a canvas pan/zoom transform.
type ViewportDTO struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	K float64 `json:"k"`
}

// CanvasNodeDTO is the transport view of a canvas projection.
type CanvasNodeDTO struct {
	ID       string `json:"id"`
	NodeType string `json:"nodeType"`
	Title    string `json:"title"`
	// EntityType and EntityID are empty for a standalone node.
	EntityType string  `json:"entityType,omitempty"`
	EntityID   string  `json:"entityId,omitempty"`
	PositionX  float64 `json:"positionX"`
	PositionY  float64 `json:"positionY"`
	Width      float64 `json:"width"`
	Height     float64 `json:"height"`
	ZIndex     int     `json:"zIndex"`
	// UIState carries the canvas display fields the domain does not model.
	UIState string `json:"uiState,omitempty"`
	// LegacyMetadata retains migrated fields the schema does not model, so the
	// canvas can round-trip them.
	LegacyMetadata string `json:"legacyMetadata,omitempty"`
	Revision       int64  `json:"revision"`
}

// CanvasEdgeDTO is the transport view of a connection.
type CanvasEdgeDTO struct {
	ID               string `json:"id"`
	FromNodeID       string `json:"fromNodeId"`
	ToNodeID         string `json:"toNodeId"`
	RelationType     string `json:"relationType"`
	FromPort         string `json:"fromPort,omitempty"`
	ToPort           string `json:"toPort,omitempty"`
	Required         bool   `json:"required"`
	ValidationStatus string `json:"validationStatus"`
	Metadata         string `json:"metadata,omitempty"`
	LegacyMetadata   string `json:"legacyMetadata,omitempty"`
	Revision         int64  `json:"revision"`
}

// CanvasChatSessionDTO is the transport view of a chat session.
type CanvasChatSessionDTO struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	MessagesJSON string `json:"messagesJson"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
	Revision     int64  `json:"revision"`
}

// CanvasSnapshotDTO is everything the canvas needs to render a project.
type CanvasSnapshotDTO struct {
	Project      ProjectDTO             `json:"project"`
	DocumentID   string                 `json:"documentId"`
	CanvasKind   string                 `json:"canvasKind"`
	Viewport     ViewportDTO            `json:"viewport"`
	Background   string                 `json:"background,omitempty"`
	Nodes        []CanvasNodeDTO        `json:"nodes"`
	Edges        []CanvasEdgeDTO        `json:"edges"`
	ChatSessions []CanvasChatSessionDTO `json:"chatSessions"`
}

// CreateProjectRequest is the create command.
type CreateProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// ProjectType is "free_canvas" or "drama".
	ProjectType string `json:"projectType"`
	Language    string `json:"language"`
}

// CreateProject stores a project and its default canvas.
func (b *ProjectsBinding) CreateProject(request CreateProjectRequest) (ProjectDTO, error) {
	service := b.projectService()
	if service == nil {
		return ProjectDTO{}, bindingUnavailable()
	}
	record, err := service.CreateProject(b.context(), appprojects.CreateProjectRequest{
		Type:        project.ProjectType(request.ProjectType),
		Name:        request.Name,
		Description: request.Description,
		Language:    request.Language,
	})
	if err != nil {
		return ProjectDTO{}, toProjectError(err)
	}
	return toProjectDTO(record), nil
}

// ListProjectsRequest narrows a project query.
type ListProjectsRequest struct {
	Statuses []string `json:"statuses,omitempty"`
	Limit    int      `json:"limit,omitempty"`
	Offset   int      `json:"offset,omitempty"`
	// IncludeTrashed includes soft-deleted projects.
	IncludeTrashed bool `json:"includeTrashed,omitempty"`
}

// ListProjects returns projects newest first.
func (b *ProjectsBinding) ListProjects(request ListProjectsRequest) ([]ProjectDTO, error) {
	service := b.projectService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	if len(request.Statuses) > maxStatusFilter {
		return nil, bindingInvalidInput()
	}
	filter := appprojects.ListFilter{
		IncludeDeleted: request.IncludeTrashed,
		Limit:          clampLimit(request.Limit),
		Offset:         request.Offset,
	}
	for _, status := range request.Statuses {
		filter.Statuses = append(filter.Statuses, project.ProjectStatus(status))
	}
	records, err := service.ListProjects(b.context(), filter)
	if err != nil {
		return nil, toProjectError(err)
	}
	projects := make([]ProjectDTO, 0, len(records))
	for _, record := range records {
		projects = append(projects, toProjectDTO(record))
	}
	return projects, nil
}

// GetProject returns one project.
func (b *ProjectsBinding) GetProject(id string) (ProjectDTO, error) {
	service := b.projectService()
	if service == nil {
		return ProjectDTO{}, bindingUnavailable()
	}
	record, err := service.GetProject(b.context(), id)
	if err != nil {
		return ProjectDTO{}, toProjectError(err)
	}
	return toProjectDTO(record), nil
}

// RenameProjectRequest renames a project under a revision guard.
type RenameProjectRequest struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Revision int64  `json:"revision"`
}

// RenameProject changes a project's name.
func (b *ProjectsBinding) RenameProject(request RenameProjectRequest) (ProjectDTO, error) {
	service := b.projectService()
	if service == nil {
		return ProjectDTO{}, bindingUnavailable()
	}
	record, err := service.RenameProject(b.context(), appprojects.RenameProjectRequest{
		ID: request.ID, Name: request.Name, Revision: request.Revision,
	})
	if err != nil {
		return ProjectDTO{}, toProjectError(err)
	}
	return toProjectDTO(record), nil
}

// SetProjectStatusRequest changes a project's lifecycle state.
type SetProjectStatusRequest struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Revision int64  `json:"revision"`
}

// SetProjectStatus archives, restores or trashes a project.
func (b *ProjectsBinding) SetProjectStatus(request SetProjectStatusRequest) (ProjectDTO, error) {
	service := b.projectService()
	if service == nil {
		return ProjectDTO{}, bindingUnavailable()
	}
	record, err := service.SetProjectStatus(b.context(), appprojects.SetProjectStatusRequest{
		ID: request.ID, Status: project.ProjectStatus(request.Status), Revision: request.Revision,
	})
	if err != nil {
		return ProjectDTO{}, toProjectError(err)
	}
	return toProjectDTO(record), nil
}

// DeleteProject permanently removes a project and everything under it.
//
// Trashing is the default path; this is the explicitly confirmed removal, which
// DOMAIN_MODEL §2.4 allows for a deliberate user action.
func (b *ProjectsBinding) DeleteProject(id string) error {
	service := b.projectService()
	if service == nil {
		return bindingUnavailable()
	}
	if err := service.DeleteProject(b.context(), id); err != nil {
		return toProjectError(err)
	}
	return nil
}

// LoadCanvas returns the full canvas projection for a project.
func (b *ProjectsBinding) LoadCanvas(projectID string) (CanvasSnapshotDTO, error) {
	service := b.projectService()
	if service == nil {
		return CanvasSnapshotDTO{}, bindingUnavailable()
	}
	snapshot, err := service.LoadCanvas(b.context(), projectID)
	if err != nil {
		return CanvasSnapshotDTO{}, toProjectError(err)
	}
	result := CanvasSnapshotDTO{
		Project:    toProjectDTO(snapshot.Project),
		DocumentID: snapshot.Document.ID,
		CanvasKind: string(snapshot.Document.Kind),
		Viewport: ViewportDTO{
			X: snapshot.Document.Viewport.X,
			Y: snapshot.Document.Viewport.Y,
			K: snapshot.Document.Viewport.K,
		},
		Background: snapshot.Document.Background,
	}
	for _, node := range snapshot.Nodes {
		result.Nodes = append(result.Nodes, CanvasNodeDTO{
			ID: node.ID, NodeType: node.NodeType, Title: node.Title,
			EntityType: node.EntityType, EntityID: node.EntityID,
			PositionX: node.PositionX, PositionY: node.PositionY,
			Width: node.Width, Height: node.Height, ZIndex: node.ZIndex,
			UIState: node.UIState, LegacyMetadata: node.LegacyMetadata, Revision: node.Revision,
		})
	}
	for _, edge := range snapshot.Edges {
		result.Edges = append(result.Edges, CanvasEdgeDTO{
			ID: edge.ID, FromNodeID: edge.FromNodeID, ToNodeID: edge.ToNodeID,
			RelationType: string(edge.RelationType), FromPort: edge.FromPort, ToPort: edge.ToPort,
			Required: edge.Required, ValidationStatus: string(edge.ValidationStatus),
			Metadata: edge.Metadata, LegacyMetadata: edge.LegacyMetadata, Revision: edge.Revision,
		})
	}
	for _, session := range snapshot.ChatSessions {
		result.ChatSessions = append(result.ChatSessions, CanvasChatSessionDTO{
			ID: session.ID, Title: session.Title, MessagesJSON: session.MessagesJSON,
			CreatedAt: session.CreatedAt.UTC().Format(rfc3339), UpdatedAt: session.UpdatedAt.UTC().Format(rfc3339),
			Revision: session.Revision,
		})
	}
	return result, nil
}

// UpdateViewportRequest persists a pan/zoom change.
type UpdateViewportRequest struct {
	DocumentID string      `json:"documentId"`
	Viewport   ViewportDTO `json:"viewport"`
	Revision   int64       `json:"revision"`
}

// UpdateViewport stores a canvas viewport.
func (b *ProjectsBinding) UpdateViewport(request UpdateViewportRequest) (CanvasSnapshotDTO, error) {
	service := b.projectService()
	if service == nil {
		return CanvasSnapshotDTO{}, bindingUnavailable()
	}
	document, err := service.UpdateViewport(b.context(), appprojects.UpdateViewportRequest{
		DocumentID: request.DocumentID,
		Viewport:   project.Viewport{X: request.Viewport.X, Y: request.Viewport.Y, K: request.Viewport.K},
		Revision:   request.Revision,
	})
	if err != nil {
		return CanvasSnapshotDTO{}, toProjectError(err)
	}
	return CanvasSnapshotDTO{
		DocumentID: document.ID,
		CanvasKind: string(document.Kind),
		Viewport: ViewportDTO{
			X: document.Viewport.X, Y: document.Viewport.Y, K: document.Viewport.K,
		},
		Background: document.Background,
	}, nil
}

// UpsertNodeRequest creates a node, or replaces one when ID is set.
type UpsertNodeRequest struct {
	// ID is empty to create a node and set to update one.
	ID         string  `json:"id,omitempty"`
	DocumentID string  `json:"documentId"`
	NodeType   string  `json:"nodeType"`
	Title      string  `json:"title"`
	EntityType string  `json:"entityType,omitempty"`
	EntityID   string  `json:"entityId,omitempty"`
	PositionX  float64 `json:"positionX"`
	PositionY  float64 `json:"positionY"`
	Width      float64 `json:"width"`
	Height     float64 `json:"height"`
	ZIndex     int     `json:"zIndex"`
	UIState    string  `json:"uiState,omitempty"`
	// LegacyMetadata is preserved verbatim, so a canvas round-trip does not lose
	// fields the schema does not model.
	LegacyMetadata string `json:"legacyMetadata,omitempty"`
	// Revision guards an update; ignored on create.
	Revision int64 `json:"revision,omitempty"`
}

// UpsertNode adds or updates one canvas node, returning the stored row.
//
// The canvas owns its node identifiers: it mints an id when it creates a node
// and keeps it, so a save sends a non-empty id for a node the database has never
// seen. Creation is therefore decided by whether the row exists, not by whether
// the id is empty. An update carries the revision the canvas last read, so a
// concurrent edit is reported as a conflict rather than overwritten.
func (b *ProjectsBinding) UpsertNode(request UpsertNodeRequest) (CanvasNodeDTO, error) {
	service := b.projectService()
	if service == nil {
		return CanvasNodeDTO{}, bindingUnavailable()
	}
	if request.ID == "" {
		// An id-less write is a create: the canvas has no identifier yet.
		return b.createNode(request)
	}
	node, err := service.UpdateNode(b.context(), appprojects.UpdateNodeRequest{
		ID: request.ID, NodeType: request.NodeType, Title: request.Title,
		EntityType: request.EntityType, EntityID: request.EntityID,
		PositionX: request.PositionX, PositionY: request.PositionY,
		Width: request.Width, Height: request.Height, ZIndex: request.ZIndex,
		UIState: request.UIState, LegacyMetadata: request.LegacyMetadata,
		Revision: request.Revision,
	})
	if err == nil {
		return toNodeDTO(node), nil
	}
	// A node the database does not hold is the canvas's first save of it, which
	// is a create. Any other failure is reported as it stands.
	if !b.isMissingNode(err) {
		return CanvasNodeDTO{}, toProjectError(err)
	}
	return b.createNode(request)
}

// isMissingNode reports whether an error means the row does not exist.
func (b *ProjectsBinding) isMissingNode(err error) bool {
	domainErr, ok := project.AsError(err)
	return ok && domainErr.Category == project.CategoryNotFound
}

// createNode stores a node the canvas has already identified.
func (b *ProjectsBinding) createNode(request UpsertNodeRequest) (CanvasNodeDTO, error) {
	service := b.projectService()
	if service == nil {
		return CanvasNodeDTO{}, bindingUnavailable()
	}
	if request.DocumentID == "" {
		return CanvasNodeDTO{}, bindingInvalidInput()
	}
	node, err := service.CreateNode(b.context(), request.DocumentID, appprojects.NodeInput{
		// The canvas's identifier is preserved so the canvas keeps addressing the
		// same node across a save and a reload.
		ID:         request.ID,
		NodeType:   request.NodeType,
		Title:      request.Title,
		EntityType: request.EntityType, EntityID: request.EntityID,
		PositionX: request.PositionX, PositionY: request.PositionY,
		Width: request.Width, Height: request.Height, ZIndex: request.ZIndex,
		UIState: request.UIState, LegacyMetadata: request.LegacyMetadata,
	})
	if err != nil {
		return CanvasNodeDTO{}, toProjectError(err)
	}
	return toNodeDTO(node), nil
}

// MoveNodesRequest moves one or more nodes.
type MoveNodesRequest struct {
	DocumentID string            `json:"documentId"`
	Positions  []NodePositionDTO `json:"positions"`
}

// NodePositionDTO is one node's new placement.
type NodePositionDTO struct {
	ID       string  `json:"id"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	ZIndex   int     `json:"zIndex,omitempty"`
	Revision int64   `json:"revision"`
}

// MoveNodes stores new node positions and reports how many were applied.
//
// A node that changed in another window is skipped rather than relocated, so a
// concurrent edit is not silently overwritten.
func (b *ProjectsBinding) MoveNodes(request MoveNodesRequest) (int, error) {
	service := b.projectService()
	if service == nil {
		return 0, bindingUnavailable()
	}
	if len(request.Positions) > maxBatchNodes {
		return 0, bindingInvalidInput()
	}
	positions := make([]appprojects.NodePosition, 0, len(request.Positions))
	for _, position := range request.Positions {
		positions = append(positions, appprojects.NodePosition{
			ID: position.ID, X: position.X, Y: position.Y,
			ZIndex: position.ZIndex, Revision: position.Revision,
		})
	}
	applied, err := service.MoveNodes(b.context(), appprojects.MoveNodesRequest{
		DocumentID: request.DocumentID, Positions: positions,
	})
	if err != nil {
		return 0, toProjectError(err)
	}
	return applied, nil
}

// DeleteNodes removes nodes and reports how many were deleted.
func (b *ProjectsBinding) DeleteNodes(ids []string) (int, error) {
	service := b.projectService()
	if service == nil {
		return 0, bindingUnavailable()
	}
	if len(ids) > maxBatchNodes {
		return 0, bindingInvalidInput()
	}
	deleted, err := service.DeleteNodes(b.context(), ids)
	if err != nil {
		return 0, toProjectError(err)
	}
	return deleted, nil
}

// CreateEdgeRequest adds a connection.
type CreateEdgeRequest struct {
	DocumentID   string `json:"documentId"`
	FromNodeID   string `json:"fromNodeId"`
	ToNodeID     string `json:"toNodeId"`
	RelationType string `json:"relationType,omitempty"`
	FromPort     string `json:"fromPort,omitempty"`
	ToPort       string `json:"toPort,omitempty"`
	Required     bool   `json:"required,omitempty"`
	Metadata     string `json:"metadata,omitempty"`
}

// CreateEdge adds a connection, validating the relation type against the
// registry before the write.
func (b *ProjectsBinding) CreateEdge(request CreateEdgeRequest) (CanvasEdgeDTO, error) {
	service := b.projectService()
	if service == nil {
		return CanvasEdgeDTO{}, bindingUnavailable()
	}
	edge, err := service.CreateEdge(b.context(), appprojects.CreateEdgeRequest{
		DocumentID: request.DocumentID, FromNodeID: request.FromNodeID, ToNodeID: request.ToNodeID,
		RelationType: project.RelationType(request.RelationType),
		FromPort:     request.FromPort, ToPort: request.ToPort,
		Required: request.Required, Metadata: request.Metadata,
	})
	if err != nil {
		return CanvasEdgeDTO{}, toProjectError(err)
	}
	return CanvasEdgeDTO{
		ID: edge.ID, FromNodeID: edge.FromNodeID, ToNodeID: edge.ToNodeID,
		RelationType: string(edge.RelationType), FromPort: edge.FromPort, ToPort: edge.ToPort,
		Required: edge.Required, ValidationStatus: string(edge.ValidationStatus),
		Metadata: edge.Metadata, LegacyMetadata: edge.LegacyMetadata, Revision: edge.Revision,
	}, nil
}

// DeleteEdges removes connections and reports how many were deleted.
func (b *ProjectsBinding) DeleteEdges(ids []string) (int, error) {
	service := b.projectService()
	if service == nil {
		return 0, bindingUnavailable()
	}
	if len(ids) > maxBatchNodes {
		return 0, bindingInvalidInput()
	}
	deleted, err := service.DeleteEdges(b.context(), ids)
	if err != nil {
		return 0, toProjectError(err)
	}
	return deleted, nil
}

// SaveChatSessionRequest stores a chat session.
type SaveChatSessionRequest struct {
	ID               string `json:"id,omitempty"`
	CanvasDocumentID string `json:"canvasDocumentId"`
	Title            string `json:"title"`
	MessagesJSON     string `json:"messagesJson"`
	Revision         int64  `json:"revision,omitempty"`
}

// SaveChatSession creates or updates a chat session.
func (b *ProjectsBinding) SaveChatSession(request SaveChatSessionRequest) (CanvasChatSessionDTO, error) {
	service := b.projectService()
	if service == nil {
		return CanvasChatSessionDTO{}, bindingUnavailable()
	}
	session, err := service.SaveChatSession(b.context(), appprojects.SaveChatSessionRequest{
		ID: request.ID, CanvasDocumentID: request.CanvasDocumentID,
		Title: request.Title, MessagesJSON: request.MessagesJSON, Revision: request.Revision,
	})
	if err != nil {
		return CanvasChatSessionDTO{}, toProjectError(err)
	}
	return CanvasChatSessionDTO{
		ID: session.ID, Title: session.Title, MessagesJSON: session.MessagesJSON,
		CreatedAt: session.CreatedAt.UTC().Format(rfc3339),
		UpdatedAt: session.UpdatedAt.UTC().Format(rfc3339), Revision: session.Revision,
	}, nil
}

// PrecheckImport reports what importing a snapshot would do, without writing.
//
// The snapshot crosses the boundary as JSON because it is a document the
// frontend extracted from browser storage; the media bytes travel separately
// through the chunked upload path so a large blob never inflates this payload.
func (b *ProjectsBinding) PrecheckImport(snapshotJSON string) (string, error) {
	imports := b.importService()
	if imports == nil {
		return "", bindingUnavailable()
	}
	if len(snapshotJSON) > maxSnapshotBytes {
		return "", bindingInvalidInput()
	}
	result, err := imports.Precheck(b.context(), snapshotJSON)
	if err != nil {
		return "", toImportError(err)
	}
	return result, nil
}

// ImportProjectsRequest imports a snapshot.
type ImportProjectsRequest struct {
	SnapshotJSON string `json:"snapshotJson"`
	// Mode is "initial" or "copy". There is no overwrite mode.
	Mode string `json:"mode,omitempty"`
	// SourceCase labels the run for the report.
	SourceCase string `json:"sourceCase,omitempty"`
	LegacyRoot string `json:"legacyRoot,omitempty"`
}

// ImportProjects writes a snapshot atomically.
func (b *ProjectsBinding) ImportProjects(request ImportProjectsRequest) (string, error) {
	imports := b.importService()
	if imports == nil {
		return "", bindingUnavailable()
	}
	if len(request.SnapshotJSON) > maxSnapshotBytes {
		return "", bindingInvalidInput()
	}
	if request.Mode != "" && request.Mode != "initial" && request.Mode != "copy" {
		return "", bindingInvalidInput()
	}
	result, err := imports.Import(b.context(), request.SnapshotJSON, request.Mode,
		request.SourceCase, request.LegacyRoot)
	if err != nil {
		return "", toImportError(err)
	}
	return result, nil
}

// ListImports returns recent import runs newest first.
func (b *ProjectsBinding) ListImports(limit int) (string, error) {
	imports := b.importService()
	if imports == nil {
		return "", bindingUnavailable()
	}
	result, err := imports.ListImports(b.context(), clampLimit(limit))
	if err != nil {
		return "", toImportError(err)
	}
	return result, nil
}

// Bounds for the canvas commands. They exist so one call cannot send an
// unbounded list (SECURITY: a canvas command must stay bounded).
const (
	maxBatchNodes   = 5000
	maxStatusFilter = 8
	// maxSnapshotBytes bounds the snapshot document. A legacy canvas with
	// 50,000 nodes is well under 64 MiB of JSON; the media travels separately.
	maxSnapshotBytes = 64 << 20
	// rfc3339 is the timestamp format the DTOs use.
	rfc3339 = "2006-01-02T15:04:05Z07:00"
)

// clampLimit bounds a client-supplied page size.
func clampLimit(value int) int {
	if value <= 0 {
		return 200
	}
	if value > 1000 {
		return 1000
	}
	return value
}

// toProjectDTO converts a domain project to its transport view.
func toProjectDTO(record project.Project) ProjectDTO {
	return ProjectDTO{
		ID: record.ID, WorkspaceID: record.WorkspaceID, ProjectType: string(record.Type),
		Name: record.Name, Description: record.Description, Language: record.Language,
		Status:    string(record.Status),
		CreatedAt: record.CreatedAt.UTC().Format(rfc3339),
		UpdatedAt: record.UpdatedAt.UTC().Format(rfc3339),
		Revision:  record.Revision,
	}
}

// toNodeDTO converts a domain node to its transport view.
func toNodeDTO(node project.Node) CanvasNodeDTO {
	return CanvasNodeDTO{
		ID: node.ID, NodeType: node.NodeType, Title: node.Title,
		EntityType: node.EntityType, EntityID: node.EntityID,
		PositionX: node.PositionX, PositionY: node.PositionY,
		Width: node.Width, Height: node.Height, ZIndex: node.ZIndex,
		UIState: node.UIState, LegacyMetadata: node.LegacyMetadata, Revision: node.Revision,
	}
}

// upperText upper-cases a stable code fragment for use in an error code.
//
// It is the string counterpart of the provider binding's upperCategory, kept
// separate so neither caller has to convert its vocabulary into the other's
// named type.
func upperText(value string) string {
	upper := make([]byte, 0, len(value))
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character >= 'a' && character <= 'z' {
			character -= 'a' - 'A'
		}
		upper = append(upper, character)
	}
	return string(upper)
}

// toProjectError maps a project domain error to a stable application error.
func toProjectError(err error) error {
	if err == nil {
		return nil
	}
	if appErr, ok := err.(*apperror.Error); ok {
		return appErr
	}
	domainErr, ok := project.AsError(err)
	if !ok {
		return apperror.New("PROJECT_REQUEST_FAILED", "internal", false, "The project request failed.", err)
	}
	code := "PROJECT_" + upperText(string(domainErr.Category))
	return apperror.New(code, "project", false, domainErr.SafeMessage, nil)
}

// toImportError maps an import failure, preserving the failed stage so the UI
// can report where it stopped (AC-LEGACY-003).
func toImportError(err error) error {
	if err == nil {
		return nil
	}
	if appErr, ok := err.(*apperror.Error); ok {
		return appErr
	}
	if importErr, ok := project.AsImportError(err); ok {
		return apperror.New("IMPORT_"+upperText(importErr.Stage), "import", false, importErr.SafeMessage, nil)
	}
	if domainErr, ok := project.AsError(err); ok {
		return apperror.New("IMPORT_"+upperText(string(domainErr.Category)), "import", false, domainErr.SafeMessage, nil)
	}
	return apperror.New("IMPORT_FAILED", "import", false, "The import failed.", err)
}

// projectBindingError builds the fail-closed error an unattached project
// binding returns. It is exported to the composition root through these
// helpers so the wiring does not have to duplicate the code strings.
// ProjectBindingUnavailable is the fail-closed error the composition root
// returns when the project service could not be composed.
func ProjectBindingUnavailable() error {
	return bindingUnavailable()
}

// projectImportInvalid builds the error a malformed snapshot returns.
// ProjectImportInvalid reports a snapshot the adapter could not parse.
func ProjectImportInvalid() error {
	return apperror.New("IMPORT_INVALID_REQUEST", "import", false,
		"That backup could not be read.", nil)
}
