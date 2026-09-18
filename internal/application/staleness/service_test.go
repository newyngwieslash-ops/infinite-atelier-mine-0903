package staleness

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/staleness"
)

// testClock is a deterministic clock.
type testClock struct {
	now time.Time
}

func (c testClock) Now() time.Time { return c.now }

// counterIDs mints deterministic identifiers.
type counterIDs struct {
	mu     sync.Mutex
	prefix string
	count  int
}

func (g *counterIDs) New() (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.count++
	return g.prefix + "-" + itoa(g.count), nil
}

func itoa(value int) string {
	if value <= 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

// memoryMarks is an in-memory double for the mark repository.
//
// It models the primary key the real table has: one row per (artifact_type,
// artifact_id). An upsert of an existing pair replaces the row rather than
// adding a second one, which is the property the production repository's
// ON CONFLICT clause provides and the reason the application treats marking as
// an upsert.
type memoryMarks struct {
	mu        sync.Mutex
	marks     map[string]staleness.Mark
	batches   int
	writeFail error
}

func newMemoryMarks() *memoryMarks {
	return &memoryMarks{marks: map[string]staleness.Mark{}}
}

func markKey(artifactType staleness.ArtifactType, artifactID string) string {
	return string(artifactType) + "\x00" + artifactID
}

func (r *memoryMarks) UpsertMark(_ context.Context, mark staleness.Mark, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.writeFail != nil {
		return r.writeFail
	}
	r.marks[markKey(mark.ArtifactType, mark.ArtifactID)] = mark
	return nil
}

func (r *memoryMarks) UpsertMarks(_ context.Context, marks []staleness.Mark, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.writeFail != nil {
		return r.writeFail
	}
	r.batches++
	for _, mark := range marks {
		r.marks[markKey(mark.ArtifactType, mark.ArtifactID)] = mark
	}
	return nil
}

func (r *memoryMarks) GetMark(_ context.Context, artifactType staleness.ArtifactType, artifactID string) (staleness.Mark, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	mark, ok := r.marks[markKey(artifactType, artifactID)]
	return mark, ok, nil
}

func (r *memoryMarks) ListMarks(_ context.Context, projectID string) ([]staleness.Mark, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	marks := make([]staleness.Mark, 0)
	for _, mark := range r.marks {
		if mark.ProjectID == projectID {
			marks = append(marks, mark)
		}
	}
	return marks, nil
}

func (r *memoryMarks) ListOpenMarks(_ context.Context, projectID string) ([]staleness.Mark, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	marks := make([]staleness.Mark, 0)
	for _, mark := range r.marks {
		if mark.ProjectID == projectID && mark.ClearedAt == "" {
			marks = append(marks, mark)
		}
	}
	return marks, nil
}

func (r *memoryMarks) ClearMark(_ context.Context, artifactType staleness.ArtifactType, artifactID string, clearedAt time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := markKey(artifactType, artifactID)
	mark, ok := r.marks[key]
	if !ok {
		return false, nil
	}
	mark.ClearedAt = clearedAt.UTC().Format(time.RFC3339Nano)
	r.marks[key] = mark
	return true, nil
}

func (r *memoryMarks) WaiveMark(_ context.Context, artifactType staleness.ArtifactType, artifactID, decisionID, reason string, _ time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := markKey(artifactType, artifactID)
	mark, ok := r.marks[key]
	if !ok {
		return false, nil
	}
	mark.Waived = true
	mark.WaivedByDecisionID = decisionID
	mark.WaivedReason = reason
	r.marks[key] = mark
	return true, nil
}

func (r *memoryMarks) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.marks)
}

// memoryFinder is an in-memory double for the dependent finder and the project
// resolver. It holds a fixed table of "which rows of type T reference upstream
// row U", so a test states the concrete dependents it expects to be found.
type memoryFinder struct {
	// dependents is keyed by "artifactType|upstreamType|upstreamID".
	dependents map[string][]DependentRef
	// owners is keyed by "artifactType|artifactID".
	owners map[string]string
	// asks records every lookup, so a test can prove a missing mapping is not
	// treated as an error.
	asks []string
}

func newMemoryFinder() *memoryFinder {
	return &memoryFinder{dependents: map[string][]DependentRef{}, owners: map[string]string{}}
}

func (f *memoryFinder) add(artifactType, upstreamType staleness.ArtifactType, upstreamID, artifactID, projectID string) {
	key := string(artifactType) + "|" + string(upstreamType) + "|" + upstreamID
	f.dependents[key] = append(f.dependents[key], DependentRef{ArtifactType: artifactType, ArtifactID: artifactID})
	f.owners[string(artifactType)+"|"+artifactID] = projectID
}

func (f *memoryFinder) FindDependents(_ context.Context, artifactType, upstreamType staleness.ArtifactType, upstreamID string) ([]DependentRef, error) {
	f.asks = append(f.asks, string(artifactType)+"|"+string(upstreamType)+"|"+upstreamID)
	key := string(artifactType) + "|" + string(upstreamType) + "|" + upstreamID
	return append([]DependentRef{}, f.dependents[key]...), nil
}

func (f *memoryFinder) ProjectFor(_ context.Context, artifactType staleness.ArtifactType, artifactID string) (string, bool, error) {
	owner, ok := f.owners[string(artifactType)+"|"+artifactID]
	return owner, ok, nil
}

func newTestService(marks *memoryMarks, finder *memoryFinder) *Service {
	return NewService(Options{
		Marks:      marks,
		Dependents: finder,
		Projects:   finder,
		Clock:      testClock{now: time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)},
		IDs:        &counterIDs{prefix: "id"},
	})
}

// TestAvailableIsFalseWithoutARepository proves the fail-closed rule.
func TestAvailableIsFalseWithoutARepository(t *testing.T) {
	service := NewService(Options{Clock: testClock{}, IDs: &counterIDs{prefix: "id"}})
	if service.Available() {
		t.Fatal("a service with no repository reports available")
	}
	if _, err := service.MarkStale(context.Background(), MarkStaleRequest{}); err == nil {
		t.Fatal("MarkStale on an unattached service succeeded")
	}
	if _, err := service.PropagateFrom(context.Background(), PropagateRequest{}); err == nil {
		t.Fatal("PropagateFrom on an unattached service succeeded")
	}
	var nilService *Service
	if nilService.Available() {
		t.Fatal("a nil service reports available")
	}
}

// TestMarkStaleIsAnUpsert covers the primary key's consequence: marking an
// artifact twice leaves one row, carrying the newer severity and reason.
func TestMarkStaleIsAnUpsert(t *testing.T) {
	marks := newMemoryMarks()
	service := newTestService(marks, newMemoryFinder())
	ctx := context.Background()

	if _, err := service.MarkStale(ctx, MarkStaleRequest{
		ArtifactType: staleness.ArtifactScriptVersion, ArtifactID: "sv-1", ProjectID: "p-1",
		Severity: staleness.SeverityInformational, Reason: "first", UpstreamType: staleness.ArtifactChapter, UpstreamID: "ch-1",
	}); err != nil {
		t.Fatalf("MarkStale: %v", err)
	}
	updated, err := service.MarkStale(ctx, MarkStaleRequest{
		ArtifactType: staleness.ArtifactScriptVersion, ArtifactID: "sv-1", ProjectID: "p-1",
		Severity: staleness.SeverityBreaking, Reason: "second", UpstreamType: staleness.ArtifactChapter, UpstreamID: "ch-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if marks.count() != 1 {
		t.Fatalf("%d rows after re-marking, want 1", marks.count())
	}
	stored, found, err := service.marks.GetMark(ctx, staleness.ArtifactScriptVersion, "sv-1")
	if err != nil || !found {
		t.Fatalf("GetMark: found=%v err=%v", found, err)
	}
	if stored.Severity != staleness.SeverityBreaking || stored.Reason != "second" {
		t.Fatalf("stored mark = %+v", stored)
	}
	if updated.Severity != staleness.SeverityBreaking {
		t.Fatalf("returned mark = %+v", updated)
	}
	// The domain's Validate is what refuses an unusable mark.
	if _, err := service.MarkStale(ctx, MarkStaleRequest{ArtifactID: "x", ProjectID: "p-1", Severity: staleness.SeverityBreaking}); err == nil {
		t.Fatal("a mark with no artifact type was accepted")
	}
	if _, err := service.MarkStale(ctx, MarkStaleRequest{
		ArtifactType: staleness.ArtifactScriptVersion, ArtifactID: "sv-2", ProjectID: "p-1", Severity: "critical",
	}); err == nil {
		t.Fatal("an unrecognised severity was accepted")
	}
	if _, err := service.MarkStale(ctx, MarkStaleRequest{
		ArtifactType: staleness.ArtifactScriptVersion, ArtifactID: "sv-3", ProjectID: "p-1", Severity: staleness.SeverityBreaking,
		UpstreamType: "nonsense",
	}); err == nil {
		t.Fatal("an unrecognised upstream type was accepted")
	}
}

// TestClearMarkKeepsTheRowAndHidesItFromTheOpenList covers the cleared_at
// semantics: the mark stops being open but stays readable.
func TestClearMarkKeepsTheRowAndHidesItFromTheOpenList(t *testing.T) {
	marks := newMemoryMarks()
	service := newTestService(marks, newMemoryFinder())
	ctx := context.Background()

	if _, err := service.MarkStale(ctx, MarkStaleRequest{
		ArtifactType: staleness.ArtifactStoryEvent, ArtifactID: "ev-1", ProjectID: "p-1", Severity: staleness.SeverityReviewRequired,
	}); err != nil {
		t.Fatal(err)
	}
	open, err := service.ListOpenMarks(ctx, "p-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 {
		t.Fatalf("%d open marks, want 1", len(open))
	}
	if err := service.ClearMark(ctx, ClearMarkRequest{ArtifactType: staleness.ArtifactStoryEvent, ArtifactID: "ev-1"}); err != nil {
		t.Fatalf("ClearMark: %v", err)
	}
	open, err = service.ListOpenMarks(ctx, "p-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 0 {
		t.Fatalf("a cleared mark is still open: %+v", open)
	}
	all, err := service.ListMarks(ctx, "p-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("the cleared mark was removed, %d rows left", len(all))
	}
	if all[0].ClearedAt == "" {
		t.Fatal("the cleared mark carries no cleared_at")
	}
	// Clearing something that was never marked is refused, because the caller
	// asked for a change that cannot happen.
	if err := service.ClearMark(ctx, ClearMarkRequest{ArtifactType: staleness.ArtifactStoryEvent, ArtifactID: "never"}); err == nil {
		t.Fatal("clearing an unmarked artifact succeeded")
	}
}

// TestWaiveMarkRequiresBothADecisionAndAReason is section 15.3: keeping a stale
// artifact needs a UserGateDecision and a recorded reason. Both are refused by
// the domain, and neither refusal writes the waiver.
func TestWaiveMarkRequiresBothADecisionAndAReason(t *testing.T) {
	marks := newMemoryMarks()
	service := newTestService(marks, newMemoryFinder())
	ctx := context.Background()

	if _, err := service.MarkStale(ctx, MarkStaleRequest{
		ArtifactType: staleness.ArtifactStoryboardVersion, ArtifactID: "sbv-1", ProjectID: "p-1",
		Severity: staleness.SeverityReviewRequired,
	}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		request WaiveMarkRequest
	}{
		{
			name:    "no decision",
			request: WaiveMarkRequest{ArtifactType: staleness.ArtifactStoryboardVersion, ArtifactID: "sbv-1", Reason: "we keep it"},
		},
		{
			name:    "no reason",
			request: WaiveMarkRequest{ArtifactType: staleness.ArtifactStoryboardVersion, ArtifactID: "sbv-1", DecisionID: "decision-1"},
		},
		{
			name:    "neither",
			request: WaiveMarkRequest{ArtifactType: staleness.ArtifactStoryboardVersion, ArtifactID: "sbv-1"},
		},
		{
			name:    "a blank reason is not a reason",
			request: WaiveMarkRequest{ArtifactType: staleness.ArtifactStoryboardVersion, ArtifactID: "sbv-1", DecisionID: "decision-1", Reason: "   "},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := service.WaiveMark(ctx, testCase.request); err == nil {
				t.Fatal("a waiver with a missing field was accepted")
			}
			stored, found, err := marks.GetMark(ctx, staleness.ArtifactStoryboardVersion, "sbv-1")
			if err != nil || !found {
				t.Fatal(err)
			}
			if stored.Waived {
				t.Fatal("a refused waiver wrote the row")
			}
		})
	}

	waived, err := service.WaiveMark(ctx, WaiveMarkRequest{
		ArtifactType: staleness.ArtifactStoryboardVersion, ArtifactID: "sbv-1",
		DecisionID: "decision-1", Reason: "the upstream edit does not change this shot's content",
	})
	if err != nil {
		t.Fatalf("WaiveMark: %v", err)
	}
	if !waived.Waived || waived.WaivedByDecisionID != "decision-1" {
		t.Fatalf("waived mark = %+v", waived)
	}
	stored, found, err := marks.GetMark(ctx, staleness.ArtifactStoryboardVersion, "sbv-1")
	if err != nil || !found {
		t.Fatal(err)
	}
	if !stored.Waived || stored.WaivedReason == "" {
		t.Fatalf("stored waiver = %+v", stored)
	}
	if err := stored.Validate(); err != nil {
		t.Fatalf("the stored waiver does not satisfy the domain: %v", err)
	}
	// Waiving an artifact that has no mark is refused.
	if _, err := service.WaiveMark(ctx, WaiveMarkRequest{
		ArtifactType: staleness.ArtifactStoryboardVersion, ArtifactID: "never", DecisionID: "d", Reason: "r",
	}); err == nil {
		t.Fatal("waiving an unmarked artifact succeeded")
	}
}

// TestPropagateFromMarksDirectAndTransitiveDependents is the core of the
// propagation contract: a chapter change marks the story events and entities
// that reference it review_required, and records the scene that hangs off one
// of those events as informational.
//
// The severity is decided against the artifact that CHANGED, not against the
// hop the walk took. That is why the scene is informational even though the
// edge from the event to the scene is itself a direct dependency: the scene
// does not reference the chapter, so the chapter's edit reaches it through the
// event.
func TestPropagateFromMarksDirectAndTransitiveDependents(t *testing.T) {
	marks := newMemoryMarks()
	finder := newMemoryFinder()
	// Two events reference the chapter; a scene references one of those events.
	finder.add(staleness.ArtifactStoryEvent, staleness.ArtifactChapter, "ch-1", "ev-1", "p-1")
	finder.add(staleness.ArtifactStoryEvent, staleness.ArtifactChapter, "ch-1", "ev-2", "p-1")
	// story_entity is the other direct dependent of a chapter.
	finder.add(staleness.ArtifactStoryEntity, staleness.ArtifactChapter, "ch-1", "ent-1", "p-1")
	finder.add(staleness.ArtifactScene, staleness.ArtifactStoryEvent, "ev-1", "scene-1", "p-1")
	service := newTestService(marks, finder)
	ctx := context.Background()

	result, err := service.PropagateFrom(ctx, PropagateRequest{
		ChangedType: staleness.ArtifactChapter, ChangedID: "ch-1", ProjectID: "p-1", Reason: "chapter 3 was edited",
	})
	if err != nil {
		t.Fatalf("PropagateFrom: %v", err)
	}
	if result.ReviewRequired != 3 {
		t.Fatalf("ReviewRequired = %d, want 3 (two events and one entity)", result.ReviewRequired)
	}
	if result.Informational != 1 {
		t.Fatalf("Informational = %d, want 1 (the scene reached through an event)", result.Informational)
	}
	if result.Marked != 4 || len(result.Marks) != 4 {
		t.Fatalf("Marked = %d with %d marks, want 4", result.Marked, len(result.Marks))
	}

	// The expected artifact is among the marks, with the change as its upstream
	// and the original change's severity.
	foundEvent, foundScene := false, false
	for _, mark := range result.Marks {
		switch {
		case mark.ArtifactType == staleness.ArtifactStoryEvent && mark.ArtifactID == "ev-1":
			foundEvent = true
			if mark.Severity != staleness.SeverityReviewRequired {
				t.Fatalf("ev-1 severity = %q, want review_required", mark.Severity)
			}
			if mark.UpstreamType != staleness.ArtifactChapter || mark.UpstreamID != "ch-1" {
				t.Fatalf("ev-1 upstream = %s/%s, want chapter/ch-1", mark.UpstreamType, mark.UpstreamID)
			}
			if mark.ProjectID != "p-1" || mark.Reason == "" {
				t.Fatalf("ev-1 mark = %+v", mark)
			}
		case mark.ArtifactType == staleness.ArtifactScene && mark.ArtifactID == "scene-1":
			foundScene = true
			// The severity comes from the original change, so a scene reached
			// through an event is a notice rather than a re-review demand.
			if mark.Severity != staleness.SeverityInformational {
				t.Fatalf("scene-1 severity = %q, want informational", mark.Severity)
			}
			// Its upstream is the artifact it actually references, not the
			// chapter it has no link to.
			if mark.UpstreamType != staleness.ArtifactStoryEvent || mark.UpstreamID != "ev-1" {
				t.Fatalf("scene-1 upstream = %s/%s, want story_event/ev-1", mark.UpstreamType, mark.UpstreamID)
			}
		}
	}
	if !foundEvent {
		t.Fatal("the story event was not marked")
	}
	if !foundScene {
		t.Fatal("the transitively reached scene was not marked")
	}

	// Both the direct and the transitive marks reached the store.
	if _, found, err := marks.GetMark(ctx, staleness.ArtifactScene, "scene-1"); err != nil || !found {
		t.Fatalf("the scene mark was not stored (found=%v err=%v)", found, err)
	}
	if len(marks.marks) != 4 {
		t.Fatalf("%d marks stored, want 4", len(marks.marks))
	}
	// One batched write, so a propagation is atomic.
	if marks.batches != 1 {
		t.Fatalf("the marks were written in %d calls, want 1", marks.batches)
	}
	// The second event has no dependents, so nothing continues from it. That is
	// not truncation: the walk finished with an empty frontier.
	if result.Truncated {
		t.Fatal("a completed walk reported itself truncated")
	}
}

// TestPropagateFromSkipsDependentsOfAnotherProject covers the same-project
// check: a mark filed under the wrong project would be invisible where it
// belongs.
func TestPropagateFromSkipsDependentsOfAnotherProject(t *testing.T) {
	marks := newMemoryMarks()
	finder := newMemoryFinder()
	finder.add(staleness.ArtifactStoryEvent, staleness.ArtifactChapter, "ch-1", "ev-here", "p-1")
	finder.add(staleness.ArtifactStoryEvent, staleness.ArtifactChapter, "ch-1", "ev-elsewhere", "p-2")
	service := newTestService(marks, finder)

	result, err := service.PropagateFrom(context.Background(), PropagateRequest{
		ChangedType: staleness.ArtifactChapter, ChangedID: "ch-1", ProjectID: "p-1",
	})
	if err != nil {
		t.Fatalf("PropagateFrom: %v", err)
	}
	if result.ReviewRequired != 1 {
		t.Fatalf("ReviewRequired = %d, want 1", result.ReviewRequired)
	}
	if len(result.Marks) != 1 || result.Marks[0].ArtifactID != "ev-here" {
		t.Fatalf("marks = %+v", result.Marks)
	}
	if _, found, _ := marks.GetMark(context.Background(), staleness.ArtifactStoryEvent, "ev-elsewhere"); found {
		t.Fatal("a dependent of another project was marked")
	}
}

// TestPropagateFromIsIdempotent covers the upsert again at the propagation
// level: propagating the same change twice leaves one mark per dependent.
func TestPropagateFromIsIdempotent(t *testing.T) {
	marks := newMemoryMarks()
	finder := newMemoryFinder()
	finder.add(staleness.ArtifactStoryEvent, staleness.ArtifactChapter, "ch-1", "ev-1", "p-1")
	service := newTestService(marks, finder)
	ctx := context.Background()

	for attempt := 0; attempt < 2; attempt++ {
		if _, err := service.PropagateFrom(ctx, PropagateRequest{
			ChangedType: staleness.ArtifactChapter, ChangedID: "ch-1", ProjectID: "p-1",
		}); err != nil {
			t.Fatalf("PropagateFrom %d: %v", attempt, err)
		}
	}
	if marks.count() != 1 {
		t.Fatalf("%d marks after two propagations, want 1", marks.count())
	}
	if marks.batches != 2 {
		t.Fatalf("%d batched writes, want one per propagation", marks.batches)
	}
}

// TestPropagateFromWithNothingToMark covers the empty result: a type with no
// finder mapping returns counts of zero rather than an error or an empty batch.
func TestPropagateFromWithNothingToMark(t *testing.T) {
	marks := newMemoryMarks()
	finder := newMemoryFinder()
	service := newTestService(marks, finder)
	ctx := context.Background()

	result, err := service.PropagateFrom(ctx, PropagateRequest{
		ChangedType: staleness.ArtifactSourceDocumentVersion, ChangedID: "sdv-1", ProjectID: "p-1",
	})
	if err != nil {
		t.Fatalf("PropagateFrom: %v", err)
	}
	if result.ReviewRequired != 0 || len(result.Marks) != 0 {
		t.Fatalf("result = %+v, want no marks", result)
	}
	if result.Marked != 0 {
		t.Fatalf("Marked = %d, want 0", result.Marked)
	}
	if result.Truncated {
		t.Fatal("an empty walk reported itself truncated")
	}
	if marks.batches != 0 {
		t.Fatal("an empty propagation issued a write")
	}
	// Every direct dependent type was still asked about, so "nothing found" is
	// a real answer rather than a skipped lookup.
	if len(finder.asks) != len(staleness.DirectDependents(staleness.ArtifactSourceDocumentVersion)) {
		t.Fatalf("%d lookups, want one per direct dependent", len(finder.asks))
	}
}

// TestPropagateFromRefusesBadInput covers the input guards.
func TestPropagateFromRefusesBadInput(t *testing.T) {
	service := newTestService(newMemoryMarks(), newMemoryFinder())
	ctx := context.Background()
	cases := []PropagateRequest{
		{ChangedType: "nonsense", ChangedID: "x", ProjectID: "p-1"},
		{ChangedType: staleness.ArtifactChapter, ProjectID: "p-1"},
		{ChangedType: staleness.ArtifactChapter, ChangedID: "ch-1"},
	}
	for _, request := range cases {
		if _, err := service.PropagateFrom(ctx, request); err == nil {
			t.Fatalf("a bad propagation request was accepted: %+v", request)
		}
	}
	// The service refuses to propagate at all without the project resolver,
	// because it could not keep a mark inside the project that asked for it.
	withoutResolver := NewService(Options{
		Marks: newMemoryMarks(), Dependents: newMemoryFinder(), IDs: &counterIDs{prefix: "id"},
	})
	if _, err := withoutResolver.PropagateFrom(ctx, PropagateRequest{
		ChangedType: staleness.ArtifactChapter, ChangedID: "ch-1", ProjectID: "p-1",
	}); err == nil {
		t.Fatal("propagation without a project resolver succeeded")
	}
}
