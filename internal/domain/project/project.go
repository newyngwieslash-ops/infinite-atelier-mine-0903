// Package project is the domain layer for workspaces, projects, canvas
// documents and their projections.
//
// It owns the vocabulary and invariants of docs/DOMAIN_MODEL.md §8-§10: which
// project types exist, which canvas kinds exist, which relation types a
// semantic edge may carry, and how a projection references a domain entity. It
// performs no I/O, imports no infrastructure, and never mints an identifier —
// identifiers arrive from the application layer (ADR-0005).
package project

import (
	"strings"
	"time"
)

// ProjectType is fixed at creation and never changes (DOMAIN_MODEL §8).
type ProjectType string

const (
	ProjectFreeCanvas ProjectType = "free_canvas"
	ProjectDrama      ProjectType = "drama"
)

// IsValidProjectType reports whether a type may be persisted.
func IsValidProjectType(value ProjectType) bool {
	return value == ProjectFreeCanvas || value == ProjectDrama
}

// ProjectStatus is the lifecycle of a project row.
type ProjectStatus string

const (
	ProjectActive   ProjectStatus = "active"
	ProjectArchived ProjectStatus = "archived"
	ProjectTrashed  ProjectStatus = "trashed"
)

// IsValidProjectStatus reports whether a status may be persisted.
func IsValidProjectStatus(value ProjectStatus) bool {
	switch value {
	case ProjectActive, ProjectArchived, ProjectTrashed:
		return true
	default:
		return false
	}
}

// WorkspaceKind distinguishes the always-present local workspace from a future
// team workspace.
type WorkspaceKind string

const (
	WorkspaceLocal      WorkspaceKind = "local"
	WorkspaceTeamFuture WorkspaceKind = "team_future"
)

// DefaultLocalWorkspaceID is the fixed identifier seeded by migration 000004.
// DOMAIN_MODEL §8 requires that a local workspace always exists and cannot be
// deleted, so the value is a constant rather than something generated at first
// run: tests and recovery paths must be able to address it without a lookup.
const DefaultLocalWorkspaceID = "00000000-0000-7000-8000-000000000001"

// Workspace is the container a project belongs to.
type Workspace struct {
	ID        string
	Name      string
	Kind      WorkspaceKind
	CreatedAt time.Time
	UpdatedAt time.Time
	Revision  int64
}

// Project is the aggregate root for a canvas workspace.
type Project struct {
	ID          string
	WorkspaceID string
	Type        ProjectType
	Name        string
	Description string
	Language    string
	Status      ProjectStatus
	// CoverAssetVersionID is empty when the project has no cover.
	CoverAssetVersionID string
	// DeletedAt is zero for a live row. Soft delete is the default per
	// DOMAIN_MODEL §2.4; physical deletion is a separate, explicit operation.
	DeletedAt time.Time
	DeletedBy string
	CreatedAt time.Time
	UpdatedAt time.Time
	Revision  int64
}

// MaxProjectNameLength mirrors the CHECK constraint in migration 000004.
const MaxProjectNameLength = 200

// ValidateProjectName rejects a name the schema would reject anyway, so the
// caller gets a domain error instead of a database error.
func ValidateProjectName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return InvalidError("A project name is required.")
	}
	if len([]rune(trimmed)) > MaxProjectNameLength {
		return InvalidError("The project name is too long.")
	}
	return nil
}

// CanTransition reports whether a project may move between two statuses.
//
// Archiving is reversible and trashing is reversible, so this is deliberately
// permissive; the point is to reject transitions that make no sense, such as
// leaving the trashed state without restoring first.
func (p Project) CanTransition(to ProjectStatus) bool {
	if !IsValidProjectStatus(p.Status) || !IsValidProjectStatus(to) {
		return false
	}
	if p.Status == to {
		return true
	}
	switch p.Status {
	case ProjectActive:
		return to == ProjectArchived || to == ProjectTrashed
	case ProjectArchived:
		return to == ProjectActive || to == ProjectTrashed
	case ProjectTrashed:
		// A trashed project returns to active; it does not skip to archived,
		// because the archive flag would otherwise be set on a hidden row.
		return to == ProjectActive
	default:
		return false
	}
}

// CanvasKind is the role a canvas document plays in a project.
type CanvasKind string

const (
	CanvasFree       CanvasKind = "free"
	CanvasDrama      CanvasKind = "drama"
	CanvasEpisode    CanvasKind = "episode"
	CanvasStoryboard CanvasKind = "storyboard"
	CanvasAsset      CanvasKind = "asset"
)

// IsValidCanvasKind reports whether a kind may be persisted.
func IsValidCanvasKind(value CanvasKind) bool {
	switch value {
	case CanvasFree, CanvasDrama, CanvasEpisode, CanvasStoryboard, CanvasAsset:
		return true
	default:
		return false
	}
}

// CanvasDocument holds a canvas's layout and projection references. The domain
// entities a projection points at live in their own tables: this document is
// the layout, not the fact (ARCHITECTURE §9.1).
type CanvasDocument struct {
	ID         string
	ProjectID  string
	Name       string
	Kind       CanvasKind
	Viewport   Viewport
	Background string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Revision   int64
}

// Viewport is the pan/zoom transform of a canvas.
type Viewport struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	K float64 `json:"k"`
}

// IsUsable reports whether a viewport can be applied. A zero scale would make
// the canvas invisible and is treated as absent rather than honoured.
func (v Viewport) IsUsable() bool {
	if v.K <= 0 {
		return false
	}
	if v.K != v.K || v.X != v.X || v.Y != v.Y { // NaN check
		return false
	}
	return true
}

// Node is a canvas projection: a placed visual element that either stands alone
// or references a domain entity.
type Node struct {
	ID               string
	CanvasDocumentID string
	NodeType         string
	// EntityType and EntityID are both empty for a standalone node, and both
	// non-empty for a projection. Half a reference is rejected by the schema and
	// by the domain.
	EntityType      string
	EntityID        string
	EntityVersionID string
	WorkflowRunID   string
	Title           string
	PositionX       float64
	PositionY       float64
	Width           float64
	Height          float64
	ZIndex          int
	// UIState carries canvas-only display state that the domain does not model.
	UIState string
	// LegacyMetadata retains fields of a migrated row that the new schema does
	// not model. DOMAIN_MODEL §20.1 forbids dropping them silently.
	LegacyMetadata string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Revision       int64
}

// MaxNodeTypeLength mirrors the CHECK constraint in migration 000004. Plugin
// node types are open strings (types/canvas.ts), so the bound is generous.
const MaxNodeTypeLength = 120

// BuiltinNodeTypes are the node types the canvas registry knows. A node type
// outside this set is a plugin type, so it is a valid row that a migration
// reports but does not rewrite.
var BuiltinNodeTypes = []string{"image", "text", "config", "video", "audio", "group", "director"}

// IsValidNodeType reports whether a node type may be persisted.
//
// Any non-empty type is storable, because plugin types are part of the legacy
// data model and refusing them would lose nodes. IsBuiltinNodeType answers the
// narrower question a migration asks when it decides whether to warn.
func IsValidNodeType(value string) bool {
	return strings.TrimSpace(value) != ""
}

// IsBuiltinNodeType reports whether a type is one the built-in registry knows.
func IsBuiltinNodeType(value string) bool {
	trimmed := strings.TrimSpace(value)
	for _, candidate := range BuiltinNodeTypes {
		if candidate == trimmed {
			return true
		}
	}
	return false
}

// ValidateNode checks the invariants the schema also enforces, so a caller
// learns about a malformed projection before a write is attempted.
func ValidateNode(node Node) error {
	trimmedType := strings.TrimSpace(node.NodeType)
	if trimmedType == "" {
		return InvalidError("A node type is required.")
	}
	if len([]rune(trimmedType)) > MaxNodeTypeLength {
		return InvalidError("The node type is too long.")
	}
	hasType := strings.TrimSpace(node.EntityType) != ""
	hasID := strings.TrimSpace(node.EntityID) != ""
	if hasType != hasID {
		return InvalidError("An entity reference needs both a type and an id.")
	}
	if node.Width < 0 || node.Height < 0 {
		return InvalidError("A node size cannot be negative.")
	}
	return nil
}

// IsProjection reports whether the node references a domain entity.
func (n Node) IsProjection() bool {
	return strings.TrimSpace(n.EntityType) != "" && strings.TrimSpace(n.EntityID) != ""
}

// RelationType is the meaning of a canvas edge. The set is the relation
// registry of DOMAIN_MODEL §10.4 plus "generic", which PRD FR-130 requires for
// migrated connections that had no type.
type RelationType string

// RelationGeneric is the type a legacy untyped connection becomes.
const RelationGeneric RelationType = "generic"

// RelationTypes is the registry, in the schema's order.
var RelationTypes = []RelationType{
	RelationGeneric,
	"contains", "adapts_to", "references", "derived_from", "continues_from",
	"generated_by", "reviewed_by", "supersedes", "first_frame_of", "last_frame_of",
	"uses_character", "uses_location", "uses_prop", "uses_asset", "appears_in",
	"located_in", "causes", "precedes", "contradicts",
}

// IsValidRelationType reports whether a relation may be persisted.
func IsValidRelationType(value RelationType) bool {
	for _, candidate := range RelationTypes {
		if candidate == value {
			return true
		}
	}
	return false
}

// EdgeValidationStatus records whether a relation has been checked against the
// registry and its endpoints. WP-04 imports edges as "unknown": validating the
// semantic relations themselves is WP-05's job.
type EdgeValidationStatus string

const (
	EdgeValid   EdgeValidationStatus = "valid"
	EdgeInvalid EdgeValidationStatus = "invalid"
	EdgeStale   EdgeValidationStatus = "stale"
	EdgeUnknown EdgeValidationStatus = "unknown"
)

// IsValidEdgeValidationStatus reports whether a status may be persisted.
func IsValidEdgeValidationStatus(value EdgeValidationStatus) bool {
	switch value {
	case EdgeValid, EdgeInvalid, EdgeStale, EdgeUnknown:
		return true
	default:
		return false
	}
}

// Edge is a connection between two nodes of one canvas document.
type Edge struct {
	ID               string
	CanvasDocumentID string
	FromNodeID       string
	ToNodeID         string
	RelationType     RelationType
	FromPort         string
	ToPort           string
	Required         bool
	ValidationStatus EdgeValidationStatus
	// Metadata carries relation-specific data (evidence, notes).
	Metadata string
	// LegacyMetadata retains the original untyped connection row.
	LegacyMetadata string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Revision       int64
}

// ChatSession is a canvas-scoped assistant conversation. It is stored as an
// opaque message list because its shape is owned by the assistant feature, not
// by the domain.
type ChatSession struct {
	ID               string
	CanvasDocumentID string
	Title            string
	MessagesJSON     string
	LegacyMetadata   string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Revision         int64
}
