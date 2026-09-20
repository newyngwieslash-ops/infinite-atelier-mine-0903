package agentassembly

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"sync"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/files"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
)

type memVersions struct {
	mu   sync.Mutex
	rows map[string]agent.SkillVersion
}

func newMemVersions() *memVersions { return &memVersions{rows: map[string]agent.SkillVersion{}} }

func (m *memVersions) RegisterSkillVersion(_ context.Context, version agent.SkillVersion) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, row := range m.rows {
		if row.SkillKey == version.SkillKey && row.Version == version.Version {
			return agent.ConflictError("already registered")
		}
	}
	m.rows[version.ID] = version
	return nil
}

func (m *memVersions) SkillVersionByKey(_ context.Context, key string) (agent.SkillVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var newest agent.SkillVersion
	found := false
	for _, row := range m.rows {
		if row.SkillKey != key {
			continue
		}
		if !found || row.CreatedAt.After(newest.CreatedAt) {
			newest, found = row, true
		}
	}
	if !found {
		return agent.SkillVersion{}, agent.NotFoundError()
	}
	return newest, nil
}

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

type seqIDs struct{ n int }

func (g *seqIDs) New() (string, error) { g.n++; return "id-" + itoa(g.n), nil }

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	d := ""
	for v > 0 {
		d = string(rune('0'+v%10)) + d
		v /= 10
	}
	return d
}

// memFiles is a ContentStore that hashes its input, so a pack's address is stable across
// builds exactly as the real store's is.
type memFiles struct {
	mu      sync.Mutex
	objects map[string]files.Object
}

func newMemFiles() *memFiles { return &memFiles{objects: map[string]files.Object{}} }

func (m *memFiles) Import(_ context.Context, displayName string, body io.Reader) (files.Object, error) {
	payload, err := io.ReadAll(body)
	if err != nil {
		return files.Object{}, err
	}
	sum := sha256.Sum256(payload)
	object := files.Object{
		Hash:       hex.EncodeToString(sum[:]),
		StorageKey: displayName,
		Size:       int64(len(payload)),
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[object.Hash] = object
	return object, nil
}

// Len reports how many distinct objects were stored.
func (m *memFiles) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.objects)
}
