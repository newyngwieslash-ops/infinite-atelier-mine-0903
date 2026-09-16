package legacy

import (
	"context"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/project"
)

// Warning codes reported by an import. They are stable identifiers so the UI
// can explain a case without parsing prose.
const (
	// WarningUnsupportedNodeType marks a node whose type the current registry
	// does not know (a plugin type). The node is imported with its type and
	// metadata intact.
	WarningUnsupportedNodeType = "unsupported_node_type"
	// WarningUnsupportedMetadata marks fields the new schema does not model.
	// They are retained in the entity's legacy metadata column.
	WarningUnsupportedMetadata = "unsupported_metadata"
	// WarningUnsupportedEdgeMetadata marks extra fields on a legacy connection.
	WarningUnsupportedEdgeMetadata = "unsupported_edge_metadata"
	// WarningMissingMedia marks a media key the manifest listed but whose bytes
	// never arrived. The node is imported; the file is reported.
	WarningMissingMedia = "missing_media"
	// WarningUnusableViewport marks a viewport that could not be applied, so
	// the identity transform was stored instead.
	WarningUnusableViewport = "unusable_viewport"
	// WarningUnmodelledAssetKind marks an asset whose legacy kind has no
	// equivalent, mapped to a documented type with a note.
	WarningUnmodelledAssetKind = "unmodelled_asset_kind"
	// WarningMonofromState marks MONOFORM iframe state that belongs to a
	// separate application and was deliberately left untouched.
	WarningMonofromState = "monoform_state_not_imported"
)

// Warning is one reported migration case. It never contains file paths or
// secret material: the reference is a legacy identifier the user recognises.
type Warning struct {
	Code        string `json:"code"`
	LegacyID    string `json:"legacyId,omitempty"`
	Detail      string `json:"detail,omitempty"`
	Occurrences int    `json:"occurrences"`
}

// FileLink records that an asset version owns a committed object.
type FileLink struct {
	VersionID string
	FileHash  string
	Role      asset.FileRole
	// LegacyKey is the legacy storage key this hash came from, for the mapping
	// table.
	LegacyKey string
}

// AssetBundle is one asset with its version and the files that version owns.
type AssetBundle struct {
	Asset    asset.Asset
	Version  asset.Version
	Files    []FileLink
	Warnings []Warning
}

// HistoryRecord is one archived generation-history row.
type HistoryRecord struct {
	ID          string
	LegacyID    string
	Prompt      string
	Model       string
	ImagesJSON  string
	Success     int
	Fail        int
	GeneratedAt time.Time
}

// IDMapping is one row of the DOMAIN_MODEL §20.1 mapping table.
type IDMapping struct {
	Kind     string
	LegacyID string
	NewID    string
}

// Mapping kinds.
const (
	MapProject           = "project"
	MapNode              = "node"
	MapAsset             = "asset"
	MapMedia             = "media"
	MapGenerationHistory = "generation_history"
)

// ProjectBundle is one project and everything under it, ready to be written.
// It is the unit of atomicity: either the whole bundle lands or none of it
// does.
type ProjectBundle struct {
	Project      project.Project
	Document     project.CanvasDocument
	Nodes        []project.Node
	Edges        []project.Edge
	ChatSessions []project.ChatSession
	Assets       []AssetBundle
	History      []HistoryRecord
	Mappings     []IDMapping
	Warnings     []Warning
	// Fingerprint identifies this project's content, so a later import can
	// recognise it (AC-LEGACY-002).
	Fingerprint string
}

// ImportRequest is one snapshot's worth of bundles plus the run's bookkeeping.
type ImportRequest struct {
	// Fingerprint covers the whole snapshot, for the run-level record.
	Fingerprint string
	// Mode is "initial" or "copy". An already-imported fingerprint is refused
	// before this point; the mode is recorded, not decided, here.
	Mode       string
	SourceCase string
	LegacyRoot string
	StartedAt  time.Time
	Bundles    []ProjectBundle
}

// ImportOutcome reports what was written.
type ImportOutcome struct {
	ImportID string
	Projects int
	Nodes    int
	Edges    int
	Media    int
	Assets   int
	History  int
}

// ImportStore writes a whole snapshot atomically.
//
// The port exposes the *operation* rather than a transaction handle because
// transaction lifetime is an infrastructure concern: the SQL lives in the
// repository, and the application layer must not see a `*sql.Tx` it could use
// to interleave network calls (ADR-0006 §3). One call means one transaction.
type ImportStore interface {
	// HasCompletedImport reports whether a fingerprint already finished, so the
	// caller can report "already imported" without writing anything.
	HasCompletedImport(ctx context.Context, fingerprint string) (bool, error)
	// ImportSnapshot writes every bundle in one transaction.
	ImportSnapshot(ctx context.Context, request ImportRequest) (ImportOutcome, error)
	// RecordImport stores a run that did not reach a successful import, so a
	// failed attempt is still visible to the user.
	RecordImport(ctx context.Context, request ImportRequest, status string, reportJSON string, warnings []Warning) (string, error)
	// ListImports returns recent runs newest first.
	ListImports(ctx context.Context, limit int) ([]ImportRecord, error)
	// GetImport returns one run.
	GetImport(ctx context.Context, id string) (ImportRecord, error)
	// LookupMapping resolves a legacy id to the entity a past import created.
	LookupMapping(ctx context.Context, kind, legacyID string) (string, bool, error)
}

// ImportRecord is one stored import run.
type ImportRecord struct {
	ID          string
	Fingerprint string
	SourceCase  string
	Mode        string
	Status      string
	ReportJSON  string
	Warnings    []Warning
	LegacyRoot  string
	StartedAt   time.Time
	FinishedAt  time.Time
	CreatedAt   time.Time
}

// Import run statuses.
const (
	StatusCompleted       = "completed"
	StatusFailed          = "failed"
	StatusAlreadyImported = "already_imported"
)

// Import modes.
const (
	ModeInitial = "initial"
	ModeCopy    = "copy"
)
