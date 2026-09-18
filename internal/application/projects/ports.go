// Package projects is the application layer for workspaces, projects and
// canvas documents. It owns the commands and queries of ARCHITECTURE §7
// ("命令改变状态、查询读取状态") and defines the persistence ports that
// infrastructure implements. It performs no I/O itself.
package projects

import (
	"context"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/project"
)

// Clock abstracts time so tests are deterministic.
type Clock interface {
	Now() time.Time
}

// IDGenerator produces entity identifiers. ADR-0005 fixes the format as
// UUIDv7 and requires generation to happen here, in the application layer, so
// every repository receives an identifier it did not mint.
type IDGenerator interface {
	New() (string, error)
}

// UnitOfWork runs a function inside one database transaction.
//
// The legacy import needs it: a project, its canvas, its nodes and edges and
// the import bookkeeping must commit together or not at all
// (AC-LEGACY-003). Repositories accept the transaction through their own
// `WithinTx` methods, so this port stays free of SQL vocabulary.
type UnitOfWork interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// ProjectRepository persists projects and workspaces.
type ProjectRepository interface {
	// CreateWorkspace stores a workspace.
	CreateWorkspace(ctx context.Context, workspace project.Workspace) error
	// GetWorkspace returns one workspace.
	GetWorkspace(ctx context.Context, id string) (project.Workspace, error)
	// CreateProject stores a new project. A duplicate id is a conflict.
	CreateProject(ctx context.Context, record project.Project) error
	// GetProject returns one project by id, including soft-deleted rows unless
	// the caller filters them out.
	GetProject(ctx context.Context, id string) (project.Project, error)
	// ListProjects returns projects newest first.
	ListProjects(ctx context.Context, filter ListFilter) ([]project.Project, error)
	// UpdateProject persists a change guarded by the expected revision.
	UpdateProject(ctx context.Context, record project.Project, expectedRevision int64) error
	// DeleteProject removes a project and, through the schema's cascades, its
	// canvas documents, nodes, edges, chats and history.
	DeleteProject(ctx context.Context, id string) error
	// CountProjects reports how many rows match, for import verification.
	CountProjects(ctx context.Context, filter ListFilter) (int, error)
}

// ListFilter narrows a project query. Zero values mean "no constraint".
type ListFilter struct {
	WorkspaceID string
	Statuses    []project.ProjectStatus
	// IncludeDeleted includes soft-deleted rows.
	IncludeDeleted bool
	Limit          int
	Offset         int
}

// CanvasRepository persists canvas documents, nodes, edges and chat sessions.
type CanvasRepository interface {
	CreateDocument(ctx context.Context, document project.CanvasDocument) error
	GetDocument(ctx context.Context, id string) (project.CanvasDocument, error)
	// ListDocuments returns a project's canvases oldest first.
	ListDocuments(ctx context.Context, projectID string) ([]project.CanvasDocument, error)
	UpdateDocument(ctx context.Context, document project.CanvasDocument, expectedRevision int64) error

	CreateNode(ctx context.Context, node project.Node) error
	GetNode(ctx context.Context, id string) (project.Node, error)
	ListNodes(ctx context.Context, documentID string) ([]project.Node, error)
	UpdateNode(ctx context.Context, node project.Node, expectedRevision int64) error
	DeleteNode(ctx context.Context, id string) error
	CountNodes(ctx context.Context, documentID string) (int, error)

	CreateEdge(ctx context.Context, edge project.Edge) error
	ListEdges(ctx context.Context, documentID string) ([]project.Edge, error)
	UpdateEdge(ctx context.Context, edge project.Edge, expectedRevision int64) error
	DeleteEdge(ctx context.Context, id string) error
	CountEdges(ctx context.Context, documentID string) (int, error)

	CreateChatSession(ctx context.Context, session project.ChatSession) error
	GetChatSession(ctx context.Context, id string) (project.ChatSession, error)
	ListChatSessions(ctx context.Context, documentID string) ([]project.ChatSession, error)
	UpdateChatSession(ctx context.Context, session project.ChatSession, expectedRevision int64) error
}

// SettingsRepository persists a drama project's configuration: the settings
// value object of DOMAIN_MODEL §4.3, the rules of §4.4, the versioned style
// guides of §4.5 and the model policies §3 lists.
//
// It is a separate port from ProjectRepository because the four tables are the
// drama configuration rather than the project identity, and because a project
// that never becomes a drama has no rows in any of them.
type SettingsRepository interface {
	CreateSettings(ctx context.Context, settings project.Settings) error
	GetSettings(ctx context.Context, projectID string) (project.Settings, error)
	// UpdateSettings persists a change guarded by the expected revision, which
	// the settings value object carries like any other revisioned row.
	UpdateSettings(ctx context.Context, settings project.Settings, expectedRevision int64) error

	CreateRule(ctx context.Context, record project.Rule) error
	GetRule(ctx context.Context, id string) (project.Rule, error)
	ListRules(ctx context.Context, projectID string, includeDeleted bool) ([]project.Rule, error)
	UpdateRule(ctx context.Context, record project.Rule, expectedRevision int64) error

	CreateStyleGuide(ctx context.Context, guide project.StyleGuide) error
	ListStyleGuides(ctx context.Context, projectID string) ([]project.StyleGuide, error)
	// MaxStyleGuideVersion reports the highest version number a project's style
	// guides reach, or zero when it has none.
	MaxStyleGuideVersion(ctx context.Context, projectID string) (int, error)

	UpsertProviderPolicy(ctx context.Context, policy project.ProviderPolicy) error
	ListProviderPolicies(ctx context.Context, projectID string) ([]project.ProviderPolicy, error)
}

// Service holds the combined project queries the desktop layer exposes.
type Service struct {
	projects ProjectRepository
	canvas   CanvasRepository
	clock    Clock
	ids      IDGenerator
	// settings is optional: a Service composed without it still serves every
	// project and canvas command, and the drama configuration commands fail
	// closed with a storage error rather than half-working.
	settings SettingsRepository
}

// Options configures a Service.
type Options struct {
	Projects ProjectRepository
	Canvas   CanvasRepository
	Clock    Clock
	IDs      IDGenerator
	// Settings enables the drama configuration commands. A nil value leaves
	// them unavailable.
	Settings SettingsRepository
}
