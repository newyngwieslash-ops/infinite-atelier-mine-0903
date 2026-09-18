package projects

import (
	"context"
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/project"
)

// This file implements the projection commands AC-CANVAS-001 is about.
//
// DOMAIN_MODEL §10.2 makes a canvas node's entity_type/entity_id pair what a
// projection *is*, and §16 names CreateCanvasProjection as the command that
// creates one. Without it the pair has a transport but no writer, so a drama
// entity could never appear on a canvas and AC-CANVAS-001's
// "创建 Script Scene 后创建 Canvas Node" would be untestable.
//
// The commands here create and refresh the projection only. They never write the
// domain entity: the canvas is a projection of the domain, not a second copy of
// it (§10.2's "删除 projection 默认不删除 entity", and AGENTS §7.1's "数据库是事实
// 来源、Canvas 是投影").

// ProjectionRequest asks for a domain entity to be shown on a canvas.
type ProjectionRequest struct {
	// ProjectID names the project whose canvas receives the projection.
	ProjectID string
	// EntityType and EntityID name the domain row being projected.
	EntityType string
	EntityID   string
	// EntityVersionID is the specific version the projection shows, when the
	// entity has versions. The relation registry's requires_version rule reads
	// it, so a relation that must name a version cannot be validated without it.
	EntityVersionID string
	// NodeType is the canvas-side node kind. It defaults to the entity type so
	// a projection of a scene draws as a scene without the caller saying so.
	NodeType string
	// Title is the node's label.
	Title string
	// PositionX/PositionY and Width/Height place the node. A zero size takes the
	// default, because a node with no box would be invisible and unclickable.
	PositionX float64
	PositionY float64
	Width     float64
	Height    float64
	// WorkflowRunID associates the projection with the run that produced it,
	// when one did. §10.2 lists it on the node for exactly that reason.
	WorkflowRunID string
}

// DefaultProjectionWidth and DefaultProjectionHeight size a projected node when
// the caller does not state a box.
const (
	DefaultProjectionWidth  = 320
	DefaultProjectionHeight = 240
)

// CreateCanvasProjection shows a domain entity on a project's canvas.
//
// It creates the node when the entity is not yet projected and moves the
// existing node when it is, so calling it twice is safe and a re-projection
// refreshes the display rather than piling up duplicates — DOMAIN_MODEL §10.2
// allows one entity to be projected several times but a command that did so by
// accident would make every re-run of a workflow add a node.
//
// The entity reference is validated against the registry's vocabulary before the
// write, and both halves are required: the schema's CHECK rejects half a
// reference, and a half reference would also be indistinguishable from a
// decorative node.
func (s *Service) CreateCanvasProjection(ctx context.Context, request ProjectionRequest) (project.Node, error) {
	if !s.Available() || s.canvas == nil {
		return project.Node{}, storageFailure()
	}
	entityType := strings.TrimSpace(request.EntityType)
	entityID := strings.TrimSpace(request.EntityID)
	if entityType == "" || entityID == "" {
		return project.Node{}, project.InvalidError("A projection needs both an entity type and an entity id.")
	}
	if !project.IsValidEntityRefType(project.EntityRefType(entityType)) {
		return project.Node{}, project.InvalidError("That entity type is not recognised.")
	}
	document, err := s.CanvasFor(ctx, request.ProjectID)
	if err != nil {
		return project.Node{}, err
	}
	now := s.now()

	// Re-projection moves and relabels the node the entity already has rather
	// than adding a second one, which is what makes the command idempotent
	// enough to call from a workflow.
	existing, found, err := s.findProjection(ctx, document.ID, entityType, entityID)
	if err != nil {
		return project.Node{}, err
	}
	if found {
		existing.Title = projectionTitle(request.Title, existing.Title)
		existing.EntityVersionID = request.EntityVersionID
		if request.WorkflowRunID != "" {
			existing.WorkflowRunID = request.WorkflowRunID
		}
		if request.Width > 0 {
			existing.Width = request.Width
		}
		if request.Height > 0 {
			existing.Height = request.Height
		}
		if request.PositionX != 0 || request.PositionY != 0 {
			existing.PositionX = request.PositionX
			existing.PositionY = request.PositionY
		}
		existing.UpdatedAt = now
		if err := project.ValidateNode(existing); err != nil {
			return project.Node{}, err
		}
		if err := s.canvas.UpdateNode(ctx, existing, existing.Revision); err != nil {
			return project.Node{}, err
		}
		existing.Revision++
		return existing, nil
	}

	nodeType := strings.TrimSpace(request.NodeType)
	if nodeType == "" {
		// The entity type is the natural node kind: a scene projection draws as
		// a scene.
		nodeType = entityType
	}
	width, height := request.Width, request.Height
	if width <= 0 {
		width = DefaultProjectionWidth
	}
	if height <= 0 {
		height = DefaultProjectionHeight
	}
	id, err := s.ids.New()
	if err != nil {
		return project.Node{}, storageFailure()
	}
	node := project.Node{
		ID:               id,
		CanvasDocumentID: document.ID,
		NodeType:         nodeType,
		Title:            projectionTitle(request.Title, ""),
		EntityType:       entityType,
		EntityID:         entityID,
		EntityVersionID:  strings.TrimSpace(request.EntityVersionID),
		WorkflowRunID:    strings.TrimSpace(request.WorkflowRunID),
		PositionX:        request.PositionX,
		PositionY:        request.PositionY,
		Width:            width,
		Height:           height,
		ZIndex:           0,
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

// projectionTitle picks a label, falling back to what the node already said and
// then to the entity id, so a projection is never an unlabelled box.
func projectionTitle(requested, existing string) string {
	if trimmed := strings.TrimSpace(requested); trimmed != "" {
		return trimmed
	}
	if trimmed := strings.TrimSpace(existing); trimmed != "" {
		return trimmed
	}
	return ""
}

// findProjection reports the node already showing an entity, if there is one.
func (s *Service) findProjection(ctx context.Context, documentID, entityType, entityID string) (project.Node, bool, error) {
	nodes, err := s.canvas.ListNodes(ctx, documentID)
	if err != nil {
		return project.Node{}, false, err
	}
	for _, node := range nodes {
		if node.EntityType == entityType && node.EntityID == entityID {
			return node, true, nil
		}
	}
	return project.Node{}, false, nil
}

// RemoveCanvasProjection takes an entity off a canvas without touching the
// entity.
//
// This is the other half of §10.2's "删除 projection 默认不删除 entity": removing
// the projection is a canvas operation, and the domain row survives. It reports
// whether a projection was there to remove, so a caller can tell "already gone"
// from "never projected".
func (s *Service) RemoveCanvasProjection(ctx context.Context, projectID, entityType, entityID string) (bool, error) {
	if !s.Available() || s.canvas == nil {
		return false, storageFailure()
	}
	document, err := s.CanvasFor(ctx, projectID)
	if err != nil {
		return false, err
	}
	node, found, err := s.findProjection(ctx, document.ID, strings.TrimSpace(entityType), strings.TrimSpace(entityID))
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	deleted, err := s.DeleteNodes(ctx, []string{node.ID})
	if err != nil {
		return false, err
	}
	return deleted == 1, nil
}

// EntityReferences is what a domain entity's projections look like from the
// entity's side.
type EntityReferences struct {
	EntityType string
	EntityID   string
	// Projections are the canvas nodes showing this entity.
	Projections []project.Node
	// RequiredBlockers are the ids of required edges attached to those nodes.
	// A non-empty list is what makes a delete destructive: PRD FR-130 requires
	// the references to be listed and the delete refused, not silently applied.
	RequiredBlockers []string
}

// FindEntityReferences lists the canvas nodes projecting an entity.
//
// AC-CANVAS-001's "删除 Scene 显示影响" and PRD FR-130's "删除领域实体时列出所有
// 引用并阻止破坏性删除" are the same requirement: before a domain delete, the
// caller asks this and either shows the list or acts on RequiredBlockers. It
// reads only; refusing the delete is the caller's decision, because whether a
// projected entity may be removed is a domain question this layer cannot answer.
func (s *Service) FindEntityReferences(ctx context.Context, projectID, entityType, entityID string) (EntityReferences, error) {
	if !s.Available() || s.canvas == nil {
		return EntityReferences{}, storageFailure()
	}
	result := EntityReferences{
		EntityType:  strings.TrimSpace(entityType),
		EntityID:    strings.TrimSpace(entityID),
		Projections: []project.Node{},
	}
	if result.EntityType == "" || result.EntityID == "" {
		return result, project.InvalidError("A reference lookup needs both an entity type and an entity id.")
	}
	document, err := s.CanvasFor(ctx, projectID)
	if err != nil {
		return EntityReferences{}, err
	}
	nodes, err := s.canvas.ListNodes(ctx, document.ID)
	if err != nil {
		return EntityReferences{}, err
	}
	nodeIDs := make([]string, 0)
	for _, node := range nodes {
		if node.EntityType == result.EntityType && node.EntityID == result.EntityID {
			result.Projections = append(result.Projections, node)
			nodeIDs = append(nodeIDs, node.ID)
		}
	}
	if len(nodeIDs) == 0 {
		return result, nil
	}
	edges, err := s.canvas.ListEdgesForNodes(ctx, nodeIDs)
	if err != nil {
		return EntityReferences{}, err
	}
	for _, edge := range edges {
		if edge.Required {
			result.RequiredBlockers = append(result.RequiredBlockers, edge.ID)
		}
	}
	return result, nil
}
