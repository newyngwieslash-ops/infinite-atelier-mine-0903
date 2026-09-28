package agentassembly

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/skill"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
)

// skill_versioning.go is RP-06.1's version-management service: the commands
// that let a USER create a new skill version from an existing one, roll back
// to a historical version, and snapshot exactly what a run will send —
// without touching the builtin registration path.
//
// # The three rules the service enforces
//
//   - IMMUTABLE CONTENT: creating a version stores a NEW document set keyed
//     by its content hash. The historical version's rows are never rewritten,
//     so a run's citation stays meaningful (AGENT_CONTRACTS §13.4).
//   - ACTIVATION IS A POINTER SWITCH, atomic with the revision check: a
//     concurrent change to the skill's active pointer or to the skill row
//     makes the second switch fail loudly rather than land on a stale base.
//   - ONE SNAPSHOT PER RUN: the caller reads document, version id and hash in
//     ONE call, so a concurrent activation cannot make a run's recorded hash
//     disagree with the document it actually sent.
//
// # What this does NOT do
//
// It does not register builtin packs — that remains Build's path, which
// refuses a (key, version) collision with different content. It does not
// upgrade a user skill's tool permissions or model contract: a user-derived
// version keeps the SAME spec its parent had, and the registry's ACL is what
// stops a Skill from claiming more than its agent's layer allows.

// SkillSnapshot is what one run sends: the document, its version identity and
// its spec, read together in one call.
type SkillSnapshot struct {
	AgentKey    string
	VersionID   string
	ContentHash string
	Document    string
	Spec        agent.Spec
}

// SkillManagementStore is the persistence port for user-managed versions.
// (The package already declares SkillVersionStore for the builtin
// registration path; this is the management half, RP-06.1.)
type SkillManagementStore interface {
	// ListVersionsBySkill returns a skill's versions, newest first.
	ListVersionsBySkill(ctx context.Context, skillKey string) ([]agent.SkillVersion, error)
	// GetSkillVersion returns one version by id.
	GetSkillVersion(ctx context.Context, versionID string) (agent.SkillVersion, error)
	// InsertSkillVersion writes one new version row. A (skill_key, version)
	// collision is a conflict.
	InsertSkillVersion(ctx context.Context, version agent.SkillVersion) error
	// DeactivateVersions marks a skill's other versions superseded, and
	// activates the named one, in ONE transaction guarded by expectedRevision.
	DeactivateVersions(ctx context.Context, skillKey, versionID string, expectedRevision int64) error
	// SkillRevision returns the skill's current revision for the guard.
	SkillRevision(ctx context.Context, skillKey string) (int64, error)
	// SkillContent returns a version's document bytes by its content address.
	SkillContent(ctx context.Context, contentFileID string) (string, error)
	// WriteSkillContent stores derived content and returns its address. A
	// derived version's document is NEW bytes, so it gets a NEW address —
	// keeping the parent's address would make the parent's hash lie.
	WriteSkillContent(ctx context.Context, content string) (string, error)
}

// skillVersioning carries the version-management commands.
type skillVersioning struct {
	store SkillManagementStore
	specs map[string]agent.Spec
	mu    sync.RWMutex
}

// SkillVersioning returns the version-management surface, or nil for an
// assembly built without it.
func (a *Assembly) SkillVersioning() *skillVersioning { return a.skillVersioning }

// SkillVersionsOf lists one agent key's versions, newest first.
func (v *skillVersioning) SkillVersionsOf(ctx context.Context, agentKey string) ([]agent.SkillVersion, error) {
	if v == nil || v.store == nil {
		return nil, agent.InvalidError("The skill version store is unavailable.")
	}
	return v.store.ListVersionsBySkill(ctx, agentKey)
}

// CreateSkillVersion derives a NEW version of one agent's skill from an
// EXISTING version's document, with the stated edits applied. The new version
// is stored immutably under its own hash; activation is a separate command so
// a user can review before switching.
func (v *skillVersioning) CreateSkillVersion(ctx context.Context, agentKey, basedOnVersionID, newVersionLabel, document string) (agent.SkillVersion, error) {
	if v == nil || v.store == nil {
		return agent.SkillVersion{}, agent.InvalidError("The skill version store is unavailable.")
	}
	if strings.TrimSpace(agentKey) == "" || strings.TrimSpace(basedOnVersionID) == "" || strings.TrimSpace(newVersionLabel) == "" {
		return agent.SkillVersion{}, agent.InvalidError("A derived version must name the agent, the version it is based on, and its own label.")
	}
	base, err := v.store.GetSkillVersion(ctx, basedOnVersionID)
	if err != nil {
		return agent.SkillVersion{}, err
	}
	if base.SkillKey != agentKey {
		// A version of ANOTHER agent cannot be the base: the derivation must
		// stay inside the skill it edits.
		return agent.SkillVersion{}, agent.InvalidError("That version belongs to a different skill.")
	}
	content := strings.TrimSpace(document)
	if content == "" {
		return agent.SkillVersion{}, agent.InvalidError("A skill document cannot be empty.")
	}
	if err := validateSkillDocumentShape(content); err != nil {
		return agent.SkillVersion{}, err
	}
	sum := sha256.Sum256([]byte(content))
	contentFileID, err := v.store.WriteSkillContent(ctx, content)
	if err != nil {
		return agent.SkillVersion{}, err
	}
	if strings.TrimSpace(contentFileID) == "" {
		return agent.SkillVersion{}, agent.StorageError("The derived skill document was stored without an address.", nil)
	}
	// The version id is DETERMINED BY CONTENT: the derived document's hash
	// is its identity, so two derivations of the same content cannot become
	// two rows (the store's (skill_key, version) uniqueness also agrees), and
	// a run citing the id cites bytes, not a mutable label.
	version := agent.SkillVersion{
		ID:            "skv-" + hex.EncodeToString(sum[:])[:24],
		SkillKey:      agentKey,
		Version:       strings.TrimSpace(newVersionLabel),
		ContentHash:   hex.EncodeToString(sum[:]),
		ManifestJSON:  base.ManifestJSON,
		ContentFileID: contentFileID,
		Status:        agent.SkillSuperseded,
	}
	if err := v.store.InsertSkillVersion(ctx, version); err != nil {
		return agent.SkillVersion{}, err
	}
	return version, nil
}

// ActivateSkillVersion rolls the skill's active pointer to a historical
// version — rollback is a pointer switch, never a rewrite.
func (v *skillVersioning) ActivateSkillVersion(ctx context.Context, agentKey, versionID string) error {
	if v == nil || v.store == nil {
		return agent.InvalidError("The skill version store is unavailable.")
	}
	version, err := v.store.GetSkillVersion(ctx, versionID)
	if err != nil {
		return err
	}
	if version.SkillKey != agentKey {
		return agent.InvalidError("That version belongs to a different skill.")
	}
	revision, err := v.store.SkillRevision(ctx, agentKey)
	if err != nil {
		return err
	}
	return v.store.DeactivateVersions(ctx, agentKey, versionID, revision)
}

// Snapshot returns ONE atomic read of what a run for this agent would send:
// document, active version id and content hash. A concurrent activation
// changes what the NEXT snapshot returns; this one is already consistent.
func (v *skillVersioning) Snapshot(ctx context.Context, agentKey string) (SkillSnapshot, bool, error) {
	if v == nil || v.store == nil {
		return SkillSnapshot{}, false, agent.InvalidError("The skill version store is unavailable.")
	}
	v.mu.RLock()
	spec, hasSpec := v.specs[agentKey]
	v.mu.RUnlock()
	if !hasSpec {
		return SkillSnapshot{}, false, nil
	}
	versions, err := v.store.ListVersionsBySkill(ctx, agentKey)
	if err != nil {
		return SkillSnapshot{}, false, err
	}
	if len(versions) == 0 {
		return SkillSnapshot{}, false, nil
	}
	active := versions[0]
	document, err := v.store.SkillContent(ctx, active.ContentFileID)
	if err != nil {
		return SkillSnapshot{}, false, err
	}
	return SkillSnapshot{
		AgentKey:    agentKey,
		VersionID:   active.ID,
		ContentHash: active.ContentHash,
		Document:    document,
		Spec:        spec,
	}, true, nil
}

// validateSkillDocumentShape applies the section 4.3 heading rule the loader
// enforces (skill.RequiredSections is the SAME list), so a user-derived
// document meets the same bar a builtin pack does.
func validateSkillDocumentShape(document string) error {
	for _, section := range skill.RequiredSections() {
		if !strings.Contains(document, "# "+section) {
			return agent.InvalidError("A skill document must carry the section 4.3 headings; missing: " + section)
		}
	}
	return nil
}

// sortedKeys is a small helper for deterministic listings.
func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
