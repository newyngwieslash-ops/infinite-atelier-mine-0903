package legacy

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/project"
)

// IDSource mints the identifiers a transformed entity receives (ADR-0005).
type IDSource interface {
	New() (string, error)
}

// TransformOptions carries what the transform needs from its caller.
type TransformOptions struct {
	IDs IDSource
	// Now stamps the new rows.
	Now time.Time
	// ProjectID overrides the generated project id, for a test that needs a
	// fixed value. Empty means "generate".
	ProjectID string
	// PreserveIDs keeps the legacy identifiers instead of generating new ones.
	//
	// The initial import uses generated ids so a legacy nanoid can never collide
	// with a UUIDv7; copy mode also generates, which is what makes a second
	// import a genuinely separate project (AC-LEGACY-002's "新副本").
	PreserveIDs bool
	// MediaHashes maps a legacy storage key to the SHA-256 of its committed
	// bytes, so a node's reference can be pointed at real content.
	MediaHashes map[string]string
	// MissingMedia lists legacy keys whose bytes never arrived.
	MissingMedia map[string]bool
	// Unsupported carries the envelope's own record of state that belongs to
	// another tool (MONOFORM's scene data, for example). Each key becomes a
	// warning, so what was found is reported rather than dropped.
	Unsupported map[string]any
}

// TransformProject converts one legacy project into an importable bundle.
//
// Everything the new schema does not model lands in a `legacy_metadata_json`
// column with a warning (DOMAIN_MODEL §20.1). Nothing is dropped: a field the
// transform does not recognise is retained verbatim, and a node type outside
// the built-in set is imported with its type unchanged, because the legacy type
// is an open string that a plugin owns.
func TransformProject(legacyProject LegacyProject, options TransformOptions) (ProjectBundle, error) {
	if options.IDs == nil {
		return ProjectBundle{}, fmt.Errorf("legacy: transform needs an id source")
	}
	now := options.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	bundle := ProjectBundle{}
	warnings := make([]Warning, 0)

	projectID, err := resolveID(options, legacyProject.ID, options.ProjectID)
	if err != nil {
		return ProjectBundle{}, err
	}
	documentID, err := options.IDs.New()
	if err != nil {
		return ProjectBundle{}, err
	}

	createdAt := parseLegacyTime(legacyProject.CreatedAt, now)
	updatedAt := parseLegacyTime(legacyProject.UpdatedAt, createdAt)

	title := strings.TrimSpace(legacyProject.Title)
	if title == "" {
		// The schema requires a name; an untitled legacy project gets a stable
		// placeholder rather than a refusal, since refusing would lose a whole
		// project over a cosmetic field.
		title = "Imported project"
		warnings = append(warnings, Warning{
			Code: WarningUnsupportedMetadata, LegacyID: legacyProject.ID,
			Detail: "the project had no title and was imported with a placeholder", Occurrences: 1,
		})
	}

	viewport := project.Viewport{X: legacyProject.Viewport.X, Y: legacyProject.Viewport.Y, K: legacyProject.Viewport.K}
	if !viewport.IsUsable() {
		viewport = project.Viewport{K: 1}
		warnings = append(warnings, Warning{
			Code: WarningUnusableViewport, LegacyID: legacyProject.ID,
			Detail: "the stored viewport was not usable and the default was applied", Occurrences: 1,
		})
	}

	background := encodeBackground(legacyProject)
	bundle.Project = project.Project{
		ID:          projectID,
		WorkspaceID: project.DefaultLocalWorkspaceID,
		// A legacy project is a free canvas: the drama type did not exist when it
		// was created (DOMAIN_MODEL §8: project_type is immutable after creation).
		Type:      project.ProjectFreeCanvas,
		Name:      title,
		Language:  "zh-CN",
		Status:    project.ProjectActive,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
		Revision:  1,
	}
	bundle.Document = project.CanvasDocument{
		ID:        documentID,
		ProjectID: projectID,
		Name:      title,
		Kind:      project.CanvasFree,
		Viewport:  viewport,
		// backgroundMode and showImageInfo are canvas display flags the domain
		// does not model; they are kept verbatim so a future field can read them
		// and the report can say they were seen.
		Background: background,
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
		Revision:   1,
	}
	bundle.Mappings = append(bundle.Mappings,
		IDMapping{Kind: MapProject, LegacyID: legacyProject.ID, NewID: projectID},
	)

	// Nodes: the legacy id becomes a mapping entry, so a later import can relate
	// an old canvas to the new rows.
	nodeIDs := make(map[string]string, len(legacyProject.Nodes))
	for index, legacyNode := range legacyProject.Nodes {
		nodeID, idErr := options.IDs.New()
		if idErr != nil {
			return ProjectBundle{}, idErr
		}
		nodeIDs[legacyNode.ID] = nodeID

		uiState, metadataWarnings, unknownType := transformNodeMetadata(legacyNode)
		warnings = append(warnings, metadataWarnings...)
		if unknownType {
			warnings = append(warnings, Warning{
				Code: WarningUnsupportedNodeType, LegacyID: legacyNode.ID,
				Detail: "the node type is not one of the built-in types and was kept as-is", Occurrences: 1,
			})
		}
		legacyFields, err := json.Marshal(legacyNode.Metadata)
		if err != nil {
			// A metadata map that cannot be re-serialised (it should not happen
			// for JSON-decoded input) is retained as an empty object rather than
			// failing the import; the warning below records the loss.
			legacyFields = []byte("{}")
			warnings = append(warnings, Warning{
				Code: WarningUnsupportedMetadata, LegacyID: legacyNode.ID,
				Detail: "the node metadata could not be re-encoded", Occurrences: 1,
			})
		}

		node := project.Node{
			ID:               nodeID,
			CanvasDocumentID: documentID,
			NodeType:         strings.TrimSpace(legacyNode.Type),
			Title:            legacyNode.Title,
			PositionX:        legacyNode.Position.X,
			PositionY:        legacyNode.Position.Y,
			Width:            legacyNode.Width,
			Height:           legacyNode.Height,
			ZIndex:           index,
			UIState:          uiState,
			LegacyMetadata:   string(legacyFields),
			CreatedAt:        createdAt,
			UpdatedAt:        updatedAt,
			Revision:         1,
		}
		if node.NodeType == "" {
			node.NodeType = "text"
			warnings = append(warnings, Warning{
				Code: WarningUnsupportedMetadata, LegacyID: legacyNode.ID,
				Detail: "the node had no type and was imported as a text node", Occurrences: 1,
			})
		}
		bundle.Nodes = append(bundle.Nodes, node)
		bundle.Mappings = append(bundle.Mappings,
			IDMapping{Kind: MapNode, LegacyID: legacyNode.ID, NewID: nodeID})
	}

	// Connections: an untyped legacy link becomes `generic` (PRD FR-130). A
	// connection whose endpoints did not survive is skipped with a warning
	// rather than imported dangling, because the schema's foreign keys would
	// reject it anyway.
	for _, connection := range legacyProject.Connections {
		fromID, fromOK := nodeIDs[connection.FromNodeID]
		toID, toOK := nodeIDs[connection.ToNodeID]
		if !fromOK || !toOK {
			warnings = append(warnings, Warning{
				Code: WarningUnsupportedEdgeMetadata, LegacyID: connection.ID,
				Detail:      "the connection referenced a node that is not in the project and was skipped",
				Occurrences: 1,
			})
			continue
		}
		edgeID, idErr := options.IDs.New()
		if idErr != nil {
			return ProjectBundle{}, idErr
		}
		legacyFields := ""
		if len(connection.Extra) > 0 {
			encoded, encodeErr := json.Marshal(connection.Extra)
			if encodeErr == nil {
				legacyFields = string(encoded)
			}
			warnings = append(warnings, Warning{
				Code: WarningUnsupportedEdgeMetadata, LegacyID: connection.ID,
				Detail: "the connection carried fields the schema does not model", Occurrences: 1,
			})
		}
		bundle.Edges = append(bundle.Edges, project.Edge{
			ID:               edgeID,
			CanvasDocumentID: documentID,
			FromNodeID:       fromID,
			ToNodeID:         toID,
			RelationType:     project.RelationGeneric,
			// The relation has not been validated against the registry; WP-05
			// owns semantic validation.
			ValidationStatus: project.EdgeUnknown,
			LegacyMetadata:   legacyFields,
			CreatedAt:        createdAt,
			UpdatedAt:        updatedAt,
			Revision:         1,
		})
	}

	// Chat sessions keep their messages verbatim: the message shape is owned by
	// the assistant feature, not by this schema.
	for _, session := range legacyProject.ChatSessions {
		sessionID, idErr := options.IDs.New()
		if idErr != nil {
			return ProjectBundle{}, idErr
		}
		messages, marshalErr := json.Marshal(session.Messages)
		if marshalErr != nil {
			messages = []byte("[]")
		}
		bundle.ChatSessions = append(bundle.ChatSessions, project.ChatSession{
			ID:               sessionID,
			CanvasDocumentID: documentID,
			Title:            session.Title,
			MessagesJSON:     string(messages),
			CreatedAt:        parseLegacyTime(session.CreatedAt, createdAt),
			UpdatedAt:        parseLegacyTime(session.UpdatedAt, updatedAt),
			Revision:         1,
		})
	}

	// Record the state WP-04 deliberately leaves in the browser, so the report
	// can say it was found and not lost.
	if len(legacyProject.ChatSessions) > 0 {
		warnings = append(warnings, Warning{
			Code: WarningUnsupportedMetadata, LegacyID: legacyProject.ID,
			Detail:      "chat sessions were imported; their active-session flag is a UI concern and was not",
			Occurrences: 1,
		})
	}

	// Missing media: a node that points at a key whose bytes never arrived is
	// reported so the user learns which file to re-supply.
	for _, key := range referencedMediaKeys(legacyProject) {
		if options.MissingMedia[key] {
			warnings = append(warnings, Warning{
				Code: WarningMissingMedia, LegacyID: key,
				Detail: "the file was listed but its content was not available", Occurrences: 1,
			})
		}
	}

	// Content the envelope recorded but this package does not convert is
	// reported rather than dropped: ADR-0006 §6 names MONOFORM's scene data as a
	// case the user must be able to see was found and left alone.
	for kind := range options.Unsupported {
		warnings = append(warnings, Warning{
			Code: WarningMonofromState, LegacyID: kind,
			Detail:      "state that belongs to another tool was found and was not imported",
			Occurrences: 1,
		})
	}

	bundle.Warnings = warnings
	bundle.Fingerprint = Fingerprint(legacyProject, options.MediaHashes)
	return bundle, nil
}

// TransformHistory archives one project's generation history.
//
// The history is a list of what the user generated: a prompt, the model, the
// images it produced and how many succeeded. It is imported into its own table
// rather than turned into provider audit rows, because the audit rows record
// calls that actually happened and the legacy list carries no call metadata
// (ADR-0006 §7 records that deviation).
//
// History is deliberately not part of a project's content fingerprint: the
// fingerprint answers "is this project already imported", and a changed history
// list is not a changed project.
func TransformHistory(records []LegacyHistory, options TransformOptions) ([]HistoryRecord, []Warning, error) {
	if options.IDs == nil {
		return nil, nil, fmt.Errorf("legacy: transform needs an id source")
	}
	now := options.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	out := make([]HistoryRecord, 0, len(records))
	warnings := make([]Warning, 0, 1)
	for _, record := range records {
		id, err := options.IDs.New()
		if err != nil {
			return nil, nil, err
		}
		images, marshalErr := json.Marshal(record.Images)
		if marshalErr != nil {
			// An entry whose image list cannot be re-encoded is imported with an
			// empty list and reported, rather than losing the prompt and the counts
			// along with it.
			images = []byte("[]")
			warnings = append(warnings, Warning{
				Code: WarningUnsupportedMetadata, LegacyID: record.ID,
				Detail: "the history entry's image list could not be read", Occurrences: 1,
			})
		}
		out = append(out, HistoryRecord{
			ID:       id,
			LegacyID: record.ID,
			Prompt:   record.Prompt,
			Model:    record.Model,
			// The list is kept as the legacy application wrote it: the entries are
			// storage keys, and the media mapping resolves them.
			ImagesJSON:  string(images),
			Success:     record.SuccessCount,
			Fail:        record.FailCount,
			GeneratedAt: legacyMillis(record.CreatedAt, now),
		})
	}
	return out, warnings, nil
}

// legacyMillis converts the legacy millisecond timestamp, falling back when the
// value is absent or implausible.
func legacyMillis(value int64, fallback time.Time) time.Time {
	if value <= 0 {
		return fallback
	}
	// 2100-01-01 in milliseconds: anything beyond this is a corrupt field rather
	// than a real date, and storing it would put a year-50000 row in the archive.
	const maxPlausibleMillis = int64(4102444800000)
	if value > maxPlausibleMillis {
		return fallback
	}
	return time.UnixMilli(value).UTC()
}

// transformNodeMetadata separates the display state from the retained legacy
// fields and reports what it kept.
//
// The split matters: values the canvas actually renders (a node's stored
// content, its status) stay in a column the UI reads, while everything else is
// retained as opaque JSON. Putting a rendered value in the opaque blob would
// make it invisible to the canvas.
func transformNodeMetadata(node LegacyNode) (uiState string, warnings []Warning, unknownType bool) {
	if node.Metadata == nil {
		return "", nil, !project.IsBuiltinNodeType(node.Type)
	}
	retained := map[string]any{}
	display := map[string]any{}
	for key, value := range node.Metadata {
		// content and status are read by the canvas; they move into ui_state so
		// the projection can render without parsing the legacy blob.
		switch key {
		case "content", "status", "errorDetails":
			display[key] = value
		default:
			retained[key] = value
		}
	}
	encodedDisplay := "{}"
	if len(display) > 0 {
		if bytes, err := json.Marshal(display); err == nil {
			encodedDisplay = string(bytes)
		}
	}
	if len(retained) > 0 {
		warnings = append(warnings, Warning{
			Code: WarningUnsupportedMetadata, LegacyID: node.ID,
			Detail:      fmt.Sprintf("%d metadata field(s) were retained without being modelled", len(retained)),
			Occurrences: len(retained),
		})
	}
	unknown := !project.IsBuiltinNodeType(node.Type)
	return encodedDisplay, warnings, unknown
}

// encodeBackground stores the canvas display flags the domain does not model.
func encodeBackground(legacyProject LegacyProject) string {
	payload := map[string]any{
		"backgroundMode": legacyProject.BackgroundMode,
		"showImageInfo":  legacyProject.ShowImageInfo,
		"activeChatId":   legacyProject.ActiveChatID,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// resolveID returns the identifier a transformed entity receives.
func resolveID(options TransformOptions, legacyID, override string) (string, error) {
	if options.ProjectID != "" {
		return options.ProjectID, nil
	}
	if options.PreserveIDs && strings.TrimSpace(legacyID) != "" {
		return legacyID, nil
	}
	return options.IDs.New()
}

// parseLegacyTime reads a legacy timestamp, falling back to the supplied
// default when it is empty or unreadable. A project with a broken timestamp is
// imported with a sensible one rather than refused.
func parseLegacyTime(value string, fallback time.Time) time.Time {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.000Z0700"} {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed.UTC()
		}
	}
	return fallback
}
