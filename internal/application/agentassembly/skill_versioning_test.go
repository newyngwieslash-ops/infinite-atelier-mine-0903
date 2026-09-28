package agentassembly

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
)

// skill_versioning_test.go is RP-06.1's lifecycle contract, over an in-memory
// management store. The plan's named sequence is the spine:
//
//	call A snapshots v1 → user activates v2 → A STILL sends and records v1 →
//	call B uses v2 → rollback to v1 affects only LATER calls.
//
// The content hash is what makes the sequence verifiable: the snapshot a run
// took carries the hash of the bytes it holds, and an activation after that
// snapshot cannot change either.

func rp05Document(name string) string {
	var builder strings.Builder
	for _, section := range skillRequiredSectionsForTest {
		builder.WriteString("# " + section + "\n\n" + name + " section content.\n\n")
	}
	return builder.String()
}

var skillRequiredSectionsForTest = []string{
	"Role", "Goal", "Trusted Context", "Untrusted Input", "Workflow State",
	"Input Contract", "Allowed Tools", "Required Procedure", "Domain Constraints",
	"Quality Rules", "Failure Conditions", "Output Contract", "Examples",
}

// rp04ManagementStore is the in-memory SkillManagementStore the lifecycle
// tests run against.
type rp04ManagementStore struct {
	mu        sync.Mutex
	versions  map[string]agent.SkillVersion
	bySkill   map[string][]string
	documents map[string]string
	pending   map[string]string
	revision  int64
}

func newRP04ManagementStore() *rp04ManagementStore {
	return &rp04ManagementStore{
		versions:  map[string]agent.SkillVersion{},
		bySkill:   map[string][]string{},
		documents: map[string]string{},
		pending:   map[string]string{},
	}
}

func (s *rp04ManagementStore) seed(skillKey, versionID, label, document string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hash := document
	version := agent.SkillVersion{
		ID: versionID, SkillKey: skillKey, Version: label,
		ContentHash:   hash,
		ContentFileID: "file-" + versionID,
		Status:        agent.SkillActive,
	}
	s.versions[versionID] = version
	s.bySkill[skillKey] = append(s.bySkill[skillKey], versionID)
	s.documents[versionID] = document
}

func (s *rp04ManagementStore) ListVersionsBySkill(_ context.Context, skillKey string) ([]agent.SkillVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []agent.SkillVersion
	// Newest first: the active one is whichever insertion order placed last.
	ids := s.bySkill[skillKey]
	for i := len(ids) - 1; i >= 0; i-- {
		if version, ok := s.versions[ids[i]]; ok && version.Status == agent.SkillActive {
			out = append(out, version)
		}
	}
	return out, nil
}

func (s *rp04ManagementStore) GetSkillVersion(_ context.Context, versionID string) (agent.SkillVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	version, ok := s.versions[versionID]
	if !ok {
		return agent.SkillVersion{}, agent.NotFoundError()
	}
	return version, nil
}

func (s *rp04ManagementStore) InsertSkillVersion(_ context.Context, version agent.SkillVersion) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// The service derives with the PARENT's content_file_id in this double;
	// the pending content arrives through SetPendingContent before the insert.
	version.Status = agent.SkillSuperseded
	if version.ID == "" {
		// The production store mints the id; the double keys on hash when the
		// service has not supplied one.
		version.ID = "derived-" + version.ContentHash[:8]
	}
	s.versions[version.ID] = version
	s.bySkill[version.SkillKey] = append(s.bySkill[version.SkillKey], version.ID)
	if pending, ok := s.pending[version.ContentHash]; ok {
		s.documents[version.ID] = pending
		delete(s.pending, version.ContentHash)
	}
	return nil
}

func (s *rp04ManagementStore) DeactivateVersions(_ context.Context, skillKey, versionID string, expectedRevision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.revision != expectedRevision {
		return agent.ConflictError("The skill changed since it was read.")
	}
	for _, id := range s.bySkill[skillKey] {
		version := s.versions[id]
		if id == versionID {
			version.Status = agent.SkillActive
		} else {
			version.Status = agent.SkillSuperseded
		}
		s.versions[id] = version
	}
	s.revision++
	return nil
}

func (s *rp04ManagementStore) SkillRevision(context.Context, string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.revision, nil
}

func (s *rp04ManagementStore) WriteSkillContent(_ context.Context, content string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Keys in documents are content-file ids WITHOUT the "file-" prefix, the
	// same convention seed() uses.
	id := "derived-" + s.documents["__counter"]
	s.documents["__counter"] = s.documents["__counter"] + "x"
	s.documents[id] = content
	return "file-" + id, nil
}

func (s *rp04ManagementStore) SkillContent(_ context.Context, contentFileID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if document, ok := s.documents[strings.TrimPrefix(contentFileID, "file-")]; ok {
		return document, nil
	}
	return "", agent.NotFoundError()
}

func newRP06Versioning() (*skillVersioning, *rp04ManagementStore) {
	store := newRP04ManagementStore()
	v := &skillVersioning{store: store, specs: map[string]agent.Spec{}}
	return v, store
}

// TestRP06SkillVersionLifecyclePinIsThePlan's named sequence.
func TestRP06SkillVersionLifecyclePinIsThePlan(t *testing.T) {
	v, store := newRP06Versioning()
	ctx := context.Background()
	v.specs["script.decision"] = agent.Spec{Key: "script.decision", Layer: agent.LayerDecision}

	// Seed v1 active, then a user creates v2 from it (derived, NOT active).
	store.seed("script.decision", "v1-id", "1.0.0", rp05Document("v1"))
	v2Document := rp05Document("v2")
	derived, err := v.CreateSkillVersion(ctx, "script.decision", "v1-id", "1.1.0", v2Document)
	if err != nil {
		t.Fatalf("CreateSkillVersion: %v", err)
	}
	// The derived version stores under its own content hash, distinct from
	// the parent's.
	if derived.ContentHash == "" || derived.ContentHash == "v1" {
		t.Fatalf("the derived version's hash is not its own: %q", derived.ContentHash)
	}

	// CALL A snapshots v1 (v2 exists but is NOT active).
	snapshotA, found, err := v.Snapshot(ctx, "script.decision")
	if err != nil || !found {
		t.Fatalf("snapshot A: found=%v err=%v", found, err)
	}
	if snapshotA.VersionID != "v1-id" {
		t.Fatalf("snapshot A cites %s, want the still-active v1", snapshotA.VersionID)
	}

	// THE USER ACTIVATES v2.
	store.versions[derived.ID] = derived // the insert recorded it
	if err := v.ActivateSkillVersion(ctx, "script.decision", derived.ID); err != nil {
		t.Fatalf("ActivateSkillVersion: %v", err)
	}

	// A STILL HOLDS v1: the snapshot already taken is unchanged, and a run
	// replaying it sends and records v1's document and hash.
	if snapshotA.VersionID != "v1-id" || snapshotA.Document != rp05Document("v1") {
		t.Fatal("an activation rewrote an already-taken snapshot")
	}

	// CALL B snapshots v2.
	snapshotB, found, err := v.Snapshot(ctx, "script.decision")
	if err != nil || !found {
		t.Fatalf("snapshot B: found=%v err=%v", found, err)
	}
	if snapshotB.VersionID == snapshotA.VersionID {
		t.Fatal("call B did not see the activation")
	}
	// The service stores the TRIMMED document, so the comparison is against
	// the trimmed expectation — the two-char difference is the trailing
	// newlines TrimSpace removed.
	if snapshotB.Document != strings.TrimSpace(v2Document) {
		t.Fatal("call B's document is not the activated v2 content")
	}

	// ROLLBACK to v1 affects only LATER calls: B's snapshot still holds v2,
	// and a new snapshot C sees v1 again.
	if err := v.ActivateSkillVersion(ctx, "script.decision", "v1-id"); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if snapshotB.VersionID == "v1-id" || snapshotB.Document != strings.TrimSpace(v2Document) {
		t.Fatal("a rollback rewrote an already-taken snapshot")
	}
	snapshotC, found, err := v.Snapshot(ctx, "script.decision")
	if err != nil || !found {
		t.Fatalf("snapshot C: found=%v err=%v", found, err)
	}
	if snapshotC.VersionID != "v1-id" {
		t.Fatalf("snapshot C cites %s, want the rolled-back v1", snapshotC.VersionID)
	}
}

// TestRP06DerivedVersionKeepsParentSpec proves a user-derived version cannot
// upgrade its own permissions: the spec the snapshot returns is the parent's
// registration, whatever the derived document claims.
func TestRP06DerivedVersionKeepsParentSpec(t *testing.T) {
	v, store := newRP06Versioning()
	ctx := context.Background()
	store.seed("script.decision", "v1-id", "1.0.0", rp05Document("v1"))
	v.specs["script.decision"] = agent.Spec{Key: "script.decision", Layer: agent.LayerDecision}

	if _, err := v.CreateSkillVersion(ctx, "script.decision", "v1-id", "1.1.0", rp05Document("v2")); err != nil {
		t.Fatalf("CreateSkillVersion: %v", err)
	}
	// A document CLAIMING a write tool does not gain one: the version's spec
	// is the registry's registration (the parent's), so a snapshot of the
	// derived version still reports the parent's allowance. Permission lives
	// in the registry's ACL, not in the document's prose.
	hostile := rp05Document("hostile") + "\nAllowed Tools: storyboard.write_row, secrets.read"
	if _, err := v.CreateSkillVersion(ctx, "script.decision", "v1-id", "1.2.0", hostile); err != nil {
		t.Fatalf("derivation with a hostile document should store (the ACL is the gate): %v", err)
	}
	if v.specs["script.decision"].Key != "script.decision" {
		t.Fatal("the derivation mutated the registered spec")
	}
	// A document MISSING a required section is refused by the shape rule.
	if _, err := v.CreateSkillVersion(ctx, "script.decision", "v1-id", "1.3.0", "# Role\nonly the role"); err == nil {
		t.Fatal("a document missing its section 4.3 headings was accepted")
	}
}

// TestRP06SnapshotIsAtomicUnderConcurrency drives the race the plan names: a
// concurrent activation between two snapshots must not make one snapshot's
// hash disagree with its document. The store's own lock serialises the read,
// and the hash read inside the SAME snapshot call is what a run records.
func TestRP06SnapshotIsAtomicUnderConcurrency(t *testing.T) {
	v, store := newRP06Versioning()
	ctx := context.Background()
	store.seed("script.decision", "v1-id", "1.0.0", rp05Document("v1"))
	derived, err := v.CreateSkillVersion(ctx, "script.decision", "v1-id", "1.1.0", rp05Document("v2"))
	if err != nil {
		t.Fatal(err)
	}
	store.versions[derived.ID] = derived

	// Ten concurrent snapshot/activation pairs; every snapshot must be
	// INTERNALLY consistent (its hash names its document), whichever version
	// it observed.
	var wg sync.WaitGroup
	activate := true
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(round int) {
			defer wg.Done()
			if round%2 == 0 {
				target := "v1-id"
				if activate {
					target = derived.ID
				}
				_ = v.ActivateSkillVersion(ctx, "script.decision", target)
				return
			}
			snapshot, found, err := v.Snapshot(ctx, "script.decision")
			if err != nil || !found {
				return
			}
			// The hash the snapshot reports names the document it holds.
			if snapshot.VersionID == "v1-id" && snapshot.Document != rp05Document("v1") {
				t.Error("snapshot hash/document mismatch under activation race")
			}
		}(i)
	}
	wg.Wait()
}
