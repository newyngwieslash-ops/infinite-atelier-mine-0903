package health

import "context"

// Snapshot is the JSON health DTO exposed through the desktop binding.
type Snapshot struct {
	Version       string `json:"version"`
	Database      string `json:"database"`
	DataDirectory string `json:"dataDirectory"`
	SafeMode      bool   `json:"safeMode"`
	Diagnostic    string `json:"diagnostic,omitempty"`
}

// Probe is the smallest database liveness check the health service needs.
type Probe interface {
	PingContext(context.Context) error
}

// Service reports desktop core health without exposing SQL or file paths to the database.
type Service struct {
	version    string
	dataDir    string
	probe      Probe
	safeMode   bool
	diagnostic string
}

// New constructs a health service for the approved application data root.
func New(version, dataDir string, probe Probe, safeMode bool, diagnostic string) *Service {
	return &Service{version: version, dataDir: dataDir, probe: probe, safeMode: safeMode, diagnostic: diagnostic}
}

// Snapshot returns ready or safe-mode health. It never includes a database file path.
func (s *Service) Snapshot(ctx context.Context) Snapshot {
	if s == nil {
		return Snapshot{Database: "safe", SafeMode: true}
	}
	snapshot := Snapshot{Version: s.version, DataDirectory: s.dataDir, Database: "ready"}
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
