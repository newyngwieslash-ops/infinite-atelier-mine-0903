package database

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/projects"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/project"
)

// CanvasRepository is the SQLite implementation of the canvas port. It owns
// every statement touching canvas documents, nodes, edges and chat sessions.
//
// The viewport and background are stored as JSON because their shape is a UI
// concern (ARCHITECTURE §9.1 keeps layout in the canvas document); they are
// parsed on read so a malformed value degrades to a default rather than
// breaking a project load.
type CanvasRepository struct {
	db *sql.DB
	tx *sql.Tx
}

// NewCanvasRepository builds the repository over a database handle.
func NewCanvasRepository(db *sql.DB) *CanvasRepository {
	return &CanvasRepository{db: db}
}

// WithinTx returns a repository bound to one transaction.
func (r *CanvasRepository) WithinTx(tx *sql.Tx) *CanvasRepository {
	return &CanvasRepository{db: r.db, tx: tx}
}

func (r *CanvasRepository) conn() querier {
	if r == nil {
		return nil
	}
	return connection(r.db, r.tx)
}

const documentSelectColumns = `SELECT id, project_id, name, canvas_kind, viewport_json, background_json,
	legacy_metadata_json, created_at, updated_at, revision FROM canvas_documents`

// CreateDocument stores a canvas document.
func (r *CanvasRepository) CreateDocument(ctx context.Context, document project.CanvasDocument) error {
	conn := r.conn()
	if conn == nil {
		return storageError("CANVAS_STORE_UNAVAILABLE", "The canvas store is unavailable.", nil)
	}
	viewport, err := encodeViewport(document.Viewport)
	if err != nil {
		return project.InvalidError("The canvas viewport could not be stored.")
	}
	_, err = conn.ExecContext(ctx, `INSERT INTO canvas_documents
		(id, project_id, name, canvas_kind, viewport_json, background_json, legacy_metadata_json,
		 created_at, updated_at, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		document.ID, document.ProjectID, document.Name, string(document.Kind), viewport,
		document.Background, "", formatTime(document.CreatedAt), formatTime(document.UpdatedAt),
		document.Revision)
	if err != nil {
		if isUniqueViolation(err) {
			return project.ConflictError("A canvas with that id already exists.")
		}
		return storageError("CANVAS_WRITE_FAILED", "The canvas could not be saved.", err)
	}
	return nil
}

// GetDocument returns one canvas document.
func (r *CanvasRepository) GetDocument(ctx context.Context, id string) (project.CanvasDocument, error) {
	conn := r.conn()
	if conn == nil {
		return project.CanvasDocument{}, storageError("CANVAS_STORE_UNAVAILABLE", "The canvas store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, documentSelectColumns+` WHERE id = ?`, id)
	document, err := scanDocument(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return project.CanvasDocument{}, project.NotFoundError()
		}
		return project.CanvasDocument{}, storageError("CANVAS_READ_FAILED", "The canvas could not be read.", err)
	}
	return document, nil
}

// ListDocuments returns a project's canvases oldest first.
func (r *CanvasRepository) ListDocuments(ctx context.Context, projectID string) ([]project.CanvasDocument, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("CANVAS_STORE_UNAVAILABLE", "The canvas store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, documentSelectColumns+` WHERE project_id = ? ORDER BY created_at ASC, id ASC`, projectID)
	if err != nil {
		return nil, storageError("CANVAS_READ_FAILED", "The canvases could not be read.", err)
	}
	defer rows.Close()
	var documents []project.CanvasDocument
	for rows.Next() {
		document, scanErr := scanDocument(rows)
		if scanErr != nil {
			return nil, storageError("CANVAS_READ_FAILED", "The canvases could not be read.", scanErr)
		}
		documents = append(documents, document)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("CANVAS_READ_FAILED", "The canvases could not be read.", err)
	}
	return documents, nil
}

// UpdateDocument persists a canvas change under a revision guard.
func (r *CanvasRepository) UpdateDocument(ctx context.Context, document project.CanvasDocument, expectedRevision int64) error {
	conn := r.conn()
	if conn == nil {
		return storageError("CANVAS_STORE_UNAVAILABLE", "The canvas store is unavailable.", nil)
	}
	viewport, err := encodeViewport(document.Viewport)
	if err != nil {
		return project.InvalidError("The canvas viewport could not be stored.")
	}
	result, err := conn.ExecContext(ctx, `UPDATE canvas_documents
		SET name = ?, canvas_kind = ?, viewport_json = ?, background_json = ?, updated_at = ?,
		    revision = revision + 1
		WHERE id = ? AND revision = ?`,
		document.Name, string(document.Kind), viewport, document.Background,
		formatTime(document.UpdatedAt), document.ID, expectedRevision)
	if err != nil {
		return storageError("CANVAS_WRITE_FAILED", "The canvas could not be updated.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError("CANVAS_WRITE_FAILED", "The canvas could not be updated.", err)
	}
	if affected == 0 {
		return project.RevisionMismatchError()
	}
	return nil
}

const nodeSelectColumns = `SELECT id, canvas_document_id, node_type, entity_type, entity_id, entity_version_id,
	workflow_run_id, title, position_x, position_y, width, height, z_index, ui_state_json,
	legacy_metadata_json, created_at, updated_at, revision FROM canvas_nodes`

// CreateNode stores a node.
func (r *CanvasRepository) CreateNode(ctx context.Context, node project.Node) error {
	conn := r.conn()
	if conn == nil {
		return storageError("CANVAS_STORE_UNAVAILABLE", "The canvas store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO canvas_nodes
		(id, canvas_document_id, node_type, entity_type, entity_id, entity_version_id, workflow_run_id,
		 title, position_x, position_y, width, height, z_index, ui_state_json, legacy_metadata_json,
		 created_at, updated_at, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		node.ID, node.CanvasDocumentID, node.NodeType, node.EntityType, node.EntityID,
		node.EntityVersionID, node.WorkflowRunID, node.Title, node.PositionX, node.PositionY,
		node.Width, node.Height, node.ZIndex, node.UIState, node.LegacyMetadata,
		formatTime(node.CreatedAt), formatTime(node.UpdatedAt), node.Revision)
	if err != nil {
		if isUniqueViolation(err) {
			return project.ConflictError("A node with that id already exists.")
		}
		return storageError("CANVAS_WRITE_FAILED", "The node could not be saved.", err)
	}
	return nil
}

// GetNode returns one node.
func (r *CanvasRepository) GetNode(ctx context.Context, id string) (project.Node, error) {
	conn := r.conn()
	if conn == nil {
		return project.Node{}, storageError("CANVAS_STORE_UNAVAILABLE", "The canvas store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, nodeSelectColumns+` WHERE id = ?`, id)
	node, err := scanNode(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return project.Node{}, project.NotFoundError()
		}
		return project.Node{}, storageError("CANVAS_READ_FAILED", "The node could not be read.", err)
	}
	return node, nil
}

// ListNodes returns a canvas's nodes in paint order.
func (r *CanvasRepository) ListNodes(ctx context.Context, documentID string) ([]project.Node, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("CANVAS_STORE_UNAVAILABLE", "The canvas store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, nodeSelectColumns+
		` WHERE canvas_document_id = ? ORDER BY z_index ASC, created_at ASC, id ASC`, documentID)
	if err != nil {
		return nil, storageError("CANVAS_READ_FAILED", "The nodes could not be read.", err)
	}
	defer rows.Close()
	var nodes []project.Node
	for rows.Next() {
		node, scanErr := scanNode(rows)
		if scanErr != nil {
			return nil, storageError("CANVAS_READ_FAILED", "The nodes could not be read.", scanErr)
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("CANVAS_READ_FAILED", "The nodes could not be read.", err)
	}
	return nodes, nil
}

// UpdateNode persists a node change under a revision guard.
func (r *CanvasRepository) UpdateNode(ctx context.Context, node project.Node, expectedRevision int64) error {
	conn := r.conn()
	if conn == nil {
		return storageError("CANVAS_STORE_UNAVAILABLE", "The canvas store is unavailable.", nil)
	}
	result, err := conn.ExecContext(ctx, `UPDATE canvas_nodes
		SET node_type = ?, entity_type = ?, entity_id = ?, entity_version_id = ?, workflow_run_id = ?,
		    title = ?, position_x = ?, position_y = ?, width = ?, height = ?, z_index = ?,
		    ui_state_json = ?, legacy_metadata_json = ?, updated_at = ?, revision = revision + 1
		WHERE id = ? AND revision = ?`,
		node.NodeType, node.EntityType, node.EntityID, node.EntityVersionID, node.WorkflowRunID,
		node.Title, node.PositionX, node.PositionY, node.Width, node.Height, node.ZIndex,
		node.UIState, node.LegacyMetadata, formatTime(node.UpdatedAt), node.ID, expectedRevision)
	if err != nil {
		return storageError("CANVAS_WRITE_FAILED", "The node could not be updated.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError("CANVAS_WRITE_FAILED", "The node could not be updated.", err)
	}
	if affected == 0 {
		return project.RevisionMismatchError()
	}
	return nil
}

// DeleteNode removes a node. Edges that referenced it go with it through the
// schema's cascade, so a canvas cannot be left with a dangling connection.
func (r *CanvasRepository) DeleteNode(ctx context.Context, id string) error {
	conn := r.conn()
	if conn == nil {
		return storageError("CANVAS_STORE_UNAVAILABLE", "The canvas store is unavailable.", nil)
	}
	if _, err := conn.ExecContext(ctx, `DELETE FROM canvas_nodes WHERE id = ?`, id); err != nil {
		return storageError("CANVAS_WRITE_FAILED", "The node could not be deleted.", err)
	}
	return nil
}

// CountNodes reports how many nodes a canvas has, for import verification.
func (r *CanvasRepository) CountNodes(ctx context.Context, documentID string) (int, error) {
	return r.count(ctx, `SELECT COUNT(*) FROM canvas_nodes WHERE canvas_document_id = ?`, documentID)
}

const edgeSelectColumns = `SELECT id, canvas_document_id, from_node_id, to_node_id, relation_type,
	from_port, to_port, required, validation_status, metadata_json, legacy_metadata_json,
	created_at, updated_at, revision FROM canvas_edges`

// CreateEdge stores a connection.
func (r *CanvasRepository) CreateEdge(ctx context.Context, edge project.Edge) error {
	conn := r.conn()
	if conn == nil {
		return storageError("CANVAS_STORE_UNAVAILABLE", "The canvas store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO canvas_edges
		(id, canvas_document_id, from_node_id, to_node_id, relation_type, from_port, to_port,
		 required, validation_status, metadata_json, legacy_metadata_json, created_at, updated_at, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		edge.ID, edge.CanvasDocumentID, edge.FromNodeID, edge.ToNodeID, string(edge.RelationType),
		edge.FromPort, edge.ToPort, boolInt(edge.Required), string(edge.ValidationStatus),
		edge.Metadata, edge.LegacyMetadata, formatTime(edge.CreatedAt), formatTime(edge.UpdatedAt),
		edge.Revision)
	if err != nil {
		if isUniqueViolation(err) {
			return project.ConflictError("A connection with that id already exists.")
		}
		return storageError("CANVAS_WRITE_FAILED", "The connection could not be saved.", err)
	}
	return nil
}

// ListEdges returns a canvas's connections.
func (r *CanvasRepository) ListEdges(ctx context.Context, documentID string) ([]project.Edge, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("CANVAS_STORE_UNAVAILABLE", "The canvas store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, edgeSelectColumns+
		` WHERE canvas_document_id = ? ORDER BY created_at ASC, id ASC`, documentID)
	if err != nil {
		return nil, storageError("CANVAS_READ_FAILED", "The connections could not be read.", err)
	}
	defer rows.Close()
	var edges []project.Edge
	for rows.Next() {
		edge, scanErr := scanEdge(rows)
		if scanErr != nil {
			return nil, storageError("CANVAS_READ_FAILED", "The connections could not be read.", scanErr)
		}
		edges = append(edges, edge)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("CANVAS_READ_FAILED", "The connections could not be read.", err)
	}
	return edges, nil
}

// UpdateEdge persists a connection change under a revision guard.
func (r *CanvasRepository) UpdateEdge(ctx context.Context, edge project.Edge, expectedRevision int64) error {
	conn := r.conn()
	if conn == nil {
		return storageError("CANVAS_STORE_UNAVAILABLE", "The canvas store is unavailable.", nil)
	}
	result, err := conn.ExecContext(ctx, `UPDATE canvas_edges
		SET relation_type = ?, from_port = ?, to_port = ?, required = ?, validation_status = ?,
		    metadata_json = ?, legacy_metadata_json = ?, updated_at = ?, revision = revision + 1
		WHERE id = ? AND revision = ?`,
		string(edge.RelationType), edge.FromPort, edge.ToPort, boolInt(edge.Required),
		string(edge.ValidationStatus), edge.Metadata, edge.LegacyMetadata,
		formatTime(edge.UpdatedAt), edge.ID, expectedRevision)
	if err != nil {
		return storageError("CANVAS_WRITE_FAILED", "The connection could not be updated.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError("CANVAS_WRITE_FAILED", "The connection could not be updated.", err)
	}
	if affected == 0 {
		return project.RevisionMismatchError()
	}
	return nil
}

// DeleteEdge removes a connection.
func (r *CanvasRepository) DeleteEdge(ctx context.Context, id string) error {
	conn := r.conn()
	if conn == nil {
		return storageError("CANVAS_STORE_UNAVAILABLE", "The canvas store is unavailable.", nil)
	}
	if _, err := conn.ExecContext(ctx, `DELETE FROM canvas_edges WHERE id = ?`, id); err != nil {
		return storageError("CANVAS_WRITE_FAILED", "The connection could not be deleted.", err)
	}
	return nil
}

// CountEdges reports how many connections a canvas has.
func (r *CanvasRepository) CountEdges(ctx context.Context, documentID string) (int, error) {
	return r.count(ctx, `SELECT COUNT(*) FROM canvas_edges WHERE canvas_document_id = ?`, documentID)
}

const chatSelectColumns = `SELECT id, canvas_document_id, title, messages_json, legacy_metadata_json,
	created_at, updated_at, revision FROM canvas_chat_sessions`

// CreateChatSession stores a chat session.
func (r *CanvasRepository) CreateChatSession(ctx context.Context, session project.ChatSession) error {
	conn := r.conn()
	if conn == nil {
		return storageError("CANVAS_STORE_UNAVAILABLE", "The canvas store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO canvas_chat_sessions
		(id, canvas_document_id, title, messages_json, legacy_metadata_json, created_at, updated_at, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID, session.CanvasDocumentID, session.Title, session.MessagesJSON,
		session.LegacyMetadata, formatTime(session.CreatedAt), formatTime(session.UpdatedAt),
		session.Revision)
	if err != nil {
		if isUniqueViolation(err) {
			return project.ConflictError("A chat session with that id already exists.")
		}
		return storageError("CANVAS_WRITE_FAILED", "The chat session could not be saved.", err)
	}
	return nil
}

// GetChatSession returns one chat session.
func (r *CanvasRepository) GetChatSession(ctx context.Context, id string) (project.ChatSession, error) {
	conn := r.conn()
	if conn == nil {
		return project.ChatSession{}, storageError("CANVAS_STORE_UNAVAILABLE", "The canvas store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, chatSelectColumns+` WHERE id = ?`, id)
	session, err := scanChatSession(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return project.ChatSession{}, project.NotFoundError()
		}
		return project.ChatSession{}, storageError("CANVAS_READ_FAILED", "The chat session could not be read.", err)
	}
	return session, nil
}

// ListChatSessions returns a canvas's chat sessions oldest first.
func (r *CanvasRepository) ListChatSessions(ctx context.Context, documentID string) ([]project.ChatSession, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("CANVAS_STORE_UNAVAILABLE", "The canvas store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, chatSelectColumns+
		` WHERE canvas_document_id = ? ORDER BY created_at ASC, id ASC`, documentID)
	if err != nil {
		return nil, storageError("CANVAS_READ_FAILED", "The chat sessions could not be read.", err)
	}
	defer rows.Close()
	var sessions []project.ChatSession
	for rows.Next() {
		session, scanErr := scanChatSession(rows)
		if scanErr != nil {
			return nil, storageError("CANVAS_READ_FAILED", "The chat sessions could not be read.", scanErr)
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("CANVAS_READ_FAILED", "The chat sessions could not be read.", err)
	}
	return sessions, nil
}

// UpdateChatSession persists a chat change under a revision guard.
func (r *CanvasRepository) UpdateChatSession(ctx context.Context, session project.ChatSession, expectedRevision int64) error {
	conn := r.conn()
	if conn == nil {
		return storageError("CANVAS_STORE_UNAVAILABLE", "The canvas store is unavailable.", nil)
	}
	result, err := conn.ExecContext(ctx, `UPDATE canvas_chat_sessions
		SET title = ?, messages_json = ?, legacy_metadata_json = ?, updated_at = ?, revision = revision + 1
		WHERE id = ? AND revision = ?`,
		session.Title, session.MessagesJSON, session.LegacyMetadata,
		formatTime(session.UpdatedAt), session.ID, expectedRevision)
	if err != nil {
		return storageError("CANVAS_WRITE_FAILED", "The chat session could not be updated.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError("CANVAS_WRITE_FAILED", "The chat session could not be updated.", err)
	}
	if affected == 0 {
		return project.RevisionMismatchError()
	}
	return nil
}

func (r *CanvasRepository) count(ctx context.Context, query string, args ...any) (int, error) {
	conn := r.conn()
	if conn == nil {
		return 0, storageError("CANVAS_STORE_UNAVAILABLE", "The canvas store is unavailable.", nil)
	}
	var value int
	if err := conn.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
		return 0, storageError("CANVAS_READ_FAILED", "The canvas could not be counted.", err)
	}
	return value, nil
}

// scanDocument reads one canvas document row.
func scanDocument(row rowScanner) (project.CanvasDocument, error) {
	var document project.CanvasDocument
	var kind, viewport, background, legacy, createdAt, updatedAt string
	if err := row.Scan(&document.ID, &document.ProjectID, &document.Name, &kind, &viewport, &background,
		&legacy, &createdAt, &updatedAt, &document.Revision); err != nil {
		return project.CanvasDocument{}, err
	}
	document.Kind = project.CanvasKind(kind)
	document.Viewport = decodeViewport(viewport)
	document.Background = background
	document.CreatedAt = parseTime(createdAt)
	document.UpdatedAt = parseTime(updatedAt)
	return document, nil
}

// scanNode reads one node row.
func scanNode(row rowScanner) (project.Node, error) {
	var node project.Node
	var createdAt, updatedAt string
	if err := row.Scan(&node.ID, &node.CanvasDocumentID, &node.NodeType, &node.EntityType, &node.EntityID,
		&node.EntityVersionID, &node.WorkflowRunID, &node.Title, &node.PositionX, &node.PositionY,
		&node.Width, &node.Height, &node.ZIndex, &node.UIState, &node.LegacyMetadata,
		&createdAt, &updatedAt, &node.Revision); err != nil {
		return project.Node{}, err
	}
	node.CreatedAt = parseTime(createdAt)
	node.UpdatedAt = parseTime(updatedAt)
	return node, nil
}

// scanEdge reads one edge row.
func scanEdge(row rowScanner) (project.Edge, error) {
	var edge project.Edge
	var relation, status, createdAt, updatedAt string
	var required int
	if err := row.Scan(&edge.ID, &edge.CanvasDocumentID, &edge.FromNodeID, &edge.ToNodeID, &relation,
		&edge.FromPort, &edge.ToPort, &required, &status, &edge.Metadata, &edge.LegacyMetadata,
		&createdAt, &updatedAt, &edge.Revision); err != nil {
		return project.Edge{}, err
	}
	edge.RelationType = project.RelationType(relation)
	edge.ValidationStatus = project.EdgeValidationStatus(status)
	edge.Required = required != 0
	edge.CreatedAt = parseTime(createdAt)
	edge.UpdatedAt = parseTime(updatedAt)
	return edge, nil
}

// scanChatSession reads one chat session row.
func scanChatSession(row rowScanner) (project.ChatSession, error) {
	var session project.ChatSession
	var createdAt, updatedAt string
	if err := row.Scan(&session.ID, &session.CanvasDocumentID, &session.Title, &session.MessagesJSON,
		&session.LegacyMetadata, &createdAt, &updatedAt, &session.Revision); err != nil {
		return project.ChatSession{}, err
	}
	session.CreatedAt = parseTime(createdAt)
	session.UpdatedAt = parseTime(updatedAt)
	return session, nil
}

// encodeViewport serialises a viewport for storage.
func encodeViewport(viewport project.Viewport) (string, error) {
	if !viewport.IsUsable() {
		// An unusable viewport is stored as the identity rather than rejected:
		// a default transform is always recoverable, and refusing the write
		// would lose the canvas layout change the caller was making.
		viewport = project.Viewport{K: 1}
	}
	encoded, err := json.Marshal(viewport)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// decodeViewport parses a stored viewport, falling back to the identity when
// the column is empty or unreadable.
func decodeViewport(value string) project.Viewport {
	if value == "" {
		return project.Viewport{K: 1}
	}
	var viewport project.Viewport
	if err := json.Unmarshal([]byte(value), &viewport); err != nil {
		return project.Viewport{K: 1}
	}
	if !viewport.IsUsable() {
		return project.Viewport{K: 1}
	}
	return viewport
}

// Ensure the repository satisfies the application port.
var _ projects.CanvasRepository = (*CanvasRepository)(nil)
