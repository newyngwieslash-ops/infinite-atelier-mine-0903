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

// Service holds the combined project queries the desktop layer exposes.
type Service struct {
	projects ProjectRepository
	canvas   CanvasRepository
	clock    Clock
	ids      IDGenerator
}

// Options configures a Service.
type Options struct {
	Projects ProjectRepository
	Canvas   CanvasRepository
	Clock    Clock
	IDs      IDGenerator
}
