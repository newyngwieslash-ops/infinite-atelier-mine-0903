package health

import "context"

// Snapshot is the JSON health DTO exposed through the desktop binding.
type Snapshot struct {
	Version       string `json:"version"`
	Database      string `json:"database"`
	DataDirectory string `json:"dataDirectory"`
	SafeMode      bool   `json:"safeMode"`
	Diagnostic    string `json:"diagnostic,omitempty"`
	// Embedding is the LOCAL embedding path's state (T18): one of "local",
	// "not_configured", "unavailable", or "provider". A user who configured a
	// local model must see here whether it actually loaded — the quiet loss
	// of the local path is the failure FR-120 names.
	Embedding       string `json:"embedding"`
	EmbeddingReason string `json:"embeddingReason,omitempty"`
}

// Probe is the smallest database liveness check the health service needs.
type Probe interface {
	PingContext(context.Context) error
}

// EmbeddingState is the local embedding path's state, read by health.
type EmbeddingState string

const (
	// EmbeddingLocal means a local model is configured, loaded, and answers
	// on this machine — no project text leaves for embeddings.
	EmbeddingLocal EmbeddingState = "local"
	// EmbeddingNotConfigured means no local model was configured; the bridge
	// uses a provider only when the project names one.
	EmbeddingNotConfigured EmbeddingState = "not_configured"
	// EmbeddingUnavailable means a local model WAS configured but did not
	// load — the state a user must see rather than lose quietly.
	EmbeddingUnavailable EmbeddingState = "unavailable"
)

// EmbeddingStatus is the read the health snapshot takes, declared as a
// function so the composition root can supply the composition-time facts
// without the health service importing the embedding packages.
type EmbeddingStatus func() (EmbeddingState, string)

// Service reports desktop core health without exposing SQL or file paths to the database.
type Service struct {
	version    string
	dataDir    string
	probe      Probe
	safeMode   bool
	diagnostic string
	embedding  EmbeddingStatus
}

// New constructs a health service for the approved application data root.
func New(version, dataDir string, probe Probe, safeMode bool, diagnostic string) *Service {
	return &Service{version: version, dataDir: dataDir, probe: probe, safeMode: safeMode, diagnostic: diagnostic}
}

// WithEmbeddingStatus supplies the local embedding read (T18). It is optional;
// a build without it reports "not_configured" rather than guessing.
func (s *Service) WithEmbeddingStatus(status EmbeddingStatus) *Service {
	if s == nil {
		return s
	}
	s.embedding = status
	return s
}

// Snapshot returns ready or safe-mode health. It never includes a database file path.
func (s *Service) Snapshot(ctx context.Context) Snapshot {
	if s == nil {
		return Snapshot{Database: "safe", SafeMode: true}
	}
	snapshot := Snapshot{Version: s.version, DataDirectory: s.dataDir, Database: "ready", Embedding: string(EmbeddingNotConfigured)}
	if s.embedding != nil {
		state, reason := s.embedding()
		snapshot.Embedding = string(state)
		snapshot.EmbeddingReason = reason
	}
	if s.safeMode || s.probe == nil {
		snapshot.Database = "safe"
		snapshot.SafeMode = true
		snapshot.Diagnostic = s.diagnostic
		return snapshot
	}
	if err := s.probe.PingContext(ctx); err != nil {
		snapshot.Database = "safe"
		snapshot.SafeMode = true
		snapshot.Diagnostic = s.diagnostic
		if snapshot.Diagnostic == "" {
			snapshot.Diagnostic = "unavailable"
		}
		return snapshot
	}
	return snapshot
}
