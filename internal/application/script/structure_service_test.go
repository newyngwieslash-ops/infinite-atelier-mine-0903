// structure_test.go covers WP-08's service surface. The lock enforcement is the one that matters
// most: AC-SCRIPT-002 lists "锁定字段不变" among the acceptance criteria, so it cannot be a request
// in a prompt that a model may ignore — it is a refusal at the write path, and these are the tests
// that prove a model which rewrote a pinned field fails.
package script

import (
	"context"
	"strings"
	"testing"

	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// structureFor builds a valid two-scene structure for one version.
func structureFor(versionID string) scriptdomain.ScriptStructure {
	return scriptdomain.ScriptStructure{
		ScriptVersionID: versionID,
		Scenes: []scriptdomain.SceneStructure{
			{
				Scene: scriptdomain.Scene{
					ID: "scene-" + versionID + "-1", ScriptVersionID: versionID, Ordinal: 1,
					SceneNumber: "1", Slugline: "INT. 渡口 - 日", InteriorExterior: scriptdomain.InteriorINT,
					Summary: "白掌柜念出那个名字。", DramaticGoal: "交代铜牌",
					EstimatedDurationSeconds: 90,
				},
				DialogueLines: []scriptdomain.DialogueLine{
					{ID: "line-" + versionID + "-1", SceneID: "scene-" + versionID + "-1", Ordinal: 1,
						Type: scriptdomain.LineDialogue, Text: "这牌子不是你的。"},
				},
				Shots: []scriptdomain.Shot{
					{ID: "shot-" + versionID + "-1", SceneID: "scene-" + versionID + "-1", Ordinal: 1,
						VisualDescription: "河面起雾。", Status: versioning.StatusDraft},
				},
			},
			{
				Scene: scriptdomain.Scene{
					ID: "scene-" + versionID + "-2", ScriptVersionID: versionID, Ordinal: 2,
					SceneNumber: "2", Slugline: "EXT. 渡口 - 夜", InteriorExterior: scriptdomain.InteriorEXT,
					EstimatedDurationSeconds: 60,
				},
			},
		},
	}
}

// seedScriptVersion stores an episode, a script and one draft version, and returns the version.
func seedScriptVersion(t *testing.T, service *Service) scriptdomain.ScriptVersion {
	t.Helper()
	ctx := context.Background()
	episode, err := service.CreateEpisode(ctx, CreateEpisodeRequest{
		ProjectID: "project-1", SeasonNumber: 1, EpisodeNumber: 1, Title: "Pilot",
	})
	if err != nil {
		t.Fatalf("CreateEpisode: %v", err)
	}
	scriptRecord, err := service.EnsureScript(ctx, episode.ID)
	if err != nil {
		t.Fatalf("EnsureScript: %v", err)
	}
	skeleton, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, OpeningHook: "a hook", EndingHook: "a hook too",
	})
	if err != nil {
		t.Fatalf("CreateStorySkeletonVersion: %v", err)
	}
	strategy, err := service.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
		EpisodeID: episode.ID, StrategySummary: "balanced",
	})
	if err != nil {
		t.Fatalf("CreateAdaptationStrategyVersion: %v", err)
	}
	version, err := service.CreateScriptVersion(ctx, CreateScriptVersionRequest{
		ScriptID: scriptRecord.ID, StorySkeletonVersionID: skeleton.ID,
		AdaptationStrategyVersionID: strategy.ID,
	})
	if err != nil {
		t.Fatalf("CreateScriptVersion: %v", err)
	}
	return version
}

// TestCreateScriptStructureWritesAWholeVersion covers the positive path and the derived duration.
func TestCreateScriptStructureWritesAWholeVersion(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	version := seedScriptVersion(t, service)

	updated, err := service.CreateScriptStructure(ctx, CreateScriptStructureRequest{
		ScriptID: version.ScriptID, ScriptVersionID: version.ID,
		Structure: structureFor(version.ID),
	})
	if err != nil {
		t.Fatalf("CreateScriptStructure: %v", err)
	}
	// The duration is SUMMED from the scenes, not taken from the request — no request field carries
	// one. This is what AGENT_CONTRACTS section 17 puts in the code's column.
	if updated.EstimatedDurationSeconds != 150 {
		t.Fatalf("the version duration is %d, want the summed 150", updated.EstimatedDurationSeconds)
	}
	// Everything landed: two scenes, one line, one shot.
	structure, err := service.GetScriptStructure(ctx, version.ID)
	if err != nil {
		t.Fatalf("GetScriptStructure: %v", err)
	}
	if len(structure.Scenes) != 2 {
		t.Fatalf("the version holds %d scenes", len(structure.Scenes))
	}
	if len(structure.Scenes[0].DialogueLines) != 1 || len(structure.Scenes[0].Shots) != 1 {
		t.Fatalf("the first scene holds %d lines and %d shots",
			len(structure.Scenes[0].DialogueLines), len(structure.Scenes[0].Shots))
	}
}

// TestCreateScriptStructureRefusesAFrozenVersion covers the IsContentFrozen call WP-05 reserved for
// this package.
//
// The status is what forbids the write, and the check runs BEFORE the structure is validated or
// written — so an approved version cannot gain content even with a perfectly valid payload.
func TestCreateScriptStructureRefusesAFrozenVersion(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	version := seedScriptVersion(t, service)
	// Approving it freezes its content.
	if _, err := service.ApproveScriptVersion(ctx, ApproveScriptVersionRequest{ScriptVersionID: version.ID}); err != nil {
		t.Fatalf("ApproveScriptVersion: %v", err)
	}
	_, err := service.CreateScriptStructure(ctx, CreateScriptStructureRequest{
		ScriptID: version.ScriptID, ScriptVersionID: version.ID,
		Structure: structureFor(version.ID),
	})
	if err == nil {
		t.Fatal("an approved version accepted content")
	}
	if categoryOfStructureTest(err) != scriptdomain.CategoryConflict {
		t.Fatalf("the refusal is category %q, want a conflict", categoryOfStructureTest(err))
	}
	// And nothing was written, which is what makes the refusal a refusal.
	structure, err := service.GetScriptStructure(ctx, version.ID)
	if err != nil {
		t.Fatalf("GetScriptStructure: %v", err)
	}
	if len(structure.Scenes) != 0 {
		t.Fatalf("a refused write left %d scenes behind", len(structure.Scenes))
	}
}

// TestCreateScriptStructureRefusesAChangedLockedStructure is AC-SCRIPT-002's "锁定字段不变".
//
// This is the test that matters most in this file: the locked structure came back with a scene the
// user pinned changed, and the write is REFUSED rather than repaired. A write path that restored the
// old value silently would produce a version the model did not write and nobody chose, and the
// record would claim the model produced content it did not.
func TestCreateScriptStructureRefusesAChangedLockedStructure(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	base := seedScriptVersion(t, service)
	if _, err := service.CreateScriptStructure(ctx, CreateScriptStructureRequest{
		ScriptID: base.ScriptID, ScriptVersionID: base.ID, Structure: structureFor(base.ID),
	}); err != nil {
		t.Fatalf("writing the base version: %v", err)
	}
	// The user pins the base version's structure.
	if err := service.LockScriptField(ctx, LockScriptFieldRequest{
		VersionID: base.ID, Family: scriptdomain.FamilyScript, Field: scriptdomain.LockScriptStructure,
		LockedBy: "user-1",
	}); err != nil {
		t.Fatalf("LockScriptField: %v", err)
	}
	next, err := service.CreateScriptVersion(ctx, CreateScriptVersionRequest{
		ScriptID: base.ScriptID, BasedOnVersionID: base.ID,
		StorySkeletonVersionID: "skeleton-x", AdaptationStrategyVersionID: "strategy-x",
	})
	if err != nil {
		t.Fatalf("CreateScriptVersion: %v", err)
	}
	// EVERY clause the comparison covers is asserted separately, because a test that broke only the
	// summary left mutations dropping the slugline, the goal and the duration green — the lock would
	// have covered one field of four while reading as if it covered the scene.
	clauses := []struct {
		name   string
		mutate func(*scriptdomain.ScriptStructure)
	}{
		{"the summary", func(s *scriptdomain.ScriptStructure) { s.Scenes[0].Summary = "rewritten" }},
		{"the slugline", func(s *scriptdomain.ScriptStructure) { s.Scenes[0].Slugline = "INT. elsewhere - 日" }},
		{"the dramatic goal", func(s *scriptdomain.ScriptStructure) { s.Scenes[0].DramaticGoal = "rewritten" }},
		{"the scene duration", func(s *scriptdomain.ScriptStructure) { s.Scenes[0].EstimatedDurationSeconds = 999 }},
	}
	for index, clause := range clauses {
		// Each case needs its own version, because a refused write leaves the version empty and a
		// later attempt against the same row would test the second case on a clean version.
		candidate, createErr := service.CreateScriptVersion(ctx, CreateScriptVersionRequest{
			ScriptID: base.ScriptID, BasedOnVersionID: base.ID,
			StorySkeletonVersionID: "skeleton-x", AdaptationStrategyVersionID: "strategy-x",
		})
		if createErr != nil {
			t.Fatalf("CreateScriptVersion for %s: %v", clause.name, createErr)
		}
		changed := structureFor(candidate.ID)
		clause.mutate(&changed)
		_, err = service.CreateScriptStructure(ctx, CreateScriptStructureRequest{
			ScriptID: candidate.ScriptID, ScriptVersionID: candidate.ID,
			BasedOnVersionID: base.ID, Structure: changed,
		})
		if err == nil {
			t.Fatalf("a revision that changed a locked scene's %s was accepted", clause.name)
		}
		if categoryOfStructureTest(err) != scriptdomain.CategoryConflict {
			t.Fatalf("%s: the refusal is category %q, want a conflict", clause.name, categoryOfStructureTest(err))
		}
		// Nothing was written for the refused attempt, so a reader cannot mistake it for a version.
		refused, readErr := service.GetScriptStructure(ctx, candidate.ID)
		if readErr != nil {
			t.Fatalf("GetScriptStructure: %v", readErr)
		}
		if len(refused.Scenes) != 0 {
			t.Fatalf("%s: a refused revision left %d scenes behind", clause.name, len(refused.Scenes))
		}
		_ = index
	}
	// The SAME structure with only an unlocked difference passes, so the check is a comparison
	// rather than a blanket refusal of revisions.
	allowed := structureFor(next.ID)
	if _, err := service.CreateScriptStructure(ctx, CreateScriptStructureRequest{
		ScriptID: next.ScriptID, ScriptVersionID: next.ID,
		BasedOnVersionID: base.ID, Structure: allowed,
	}); err != nil {
		t.Fatalf("a revision that respected the lock was refused: %v", err)
	}
}

// TestCreateScriptStructureRefusesADifferentSceneCountUnderALock covers the count check.
//
// A revision that dropped a scene has changed the pinned shape even if every surviving scene is
// identical, and the check says so rather than reading past the end of one of the lists.
func TestCreateScriptStructureRefusesADifferentSceneCountUnderALock(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	base := seedScriptVersion(t, service)
	if _, err := service.CreateScriptStructure(ctx, CreateScriptStructureRequest{
		ScriptID: base.ScriptID, ScriptVersionID: base.ID, Structure: structureFor(base.ID),
	}); err != nil {
		t.Fatalf("writing the base version: %v", err)
	}
	if err := service.LockScriptField(ctx, LockScriptFieldRequest{
		VersionID: base.ID, Field: scriptdomain.LockScriptStructure,
	}); err != nil {
		t.Fatalf("LockScriptField: %v", err)
	}
	next, err := service.CreateScriptVersion(ctx, CreateScriptVersionRequest{
		ScriptID: base.ScriptID, BasedOnVersionID: base.ID,
		StorySkeletonVersionID: "s1", AdaptationStrategyVersionID: "a1",
	})
	if err != nil {
		t.Fatalf("CreateScriptVersion: %v", err)
	}
	shortened := structureFor(next.ID)
	shortened.Scenes = shortened.Scenes[:1]
	if _, err := service.CreateScriptStructure(ctx, CreateScriptStructureRequest{
		ScriptID: next.ScriptID, ScriptVersionID: next.ID, BasedOnVersionID: base.ID, Structure: shortened,
	}); err == nil {
		t.Fatal("a revision that dropped a pinned scene was accepted")
	}
}

// TestLockScriptFieldRefusesTheWrongFamily covers the check that keeps the lock table honest.
//
// version_id cannot be a foreign key across three tables, so the read-and-confirm is what makes the
// family column true. A lock naming the wrong family is refused rather than recorded, because a
// lock no diff would ever report reads as a protection that is in force and is not.
func TestLockScriptFieldRefusesTheWrongFamily(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	version := seedScriptVersion(t, service)

	// The version is a script version, so a skeleton field is refused — and the refusal NAMES the
	// family mismatch rather than the field.
	//
	// That message is the check's whole observable effect: `FieldLock.Validate` refuses the same
	// input a moment later, because it validates the field against the family this service RESOLVED
	// rather than the one the caller claimed. So the check earns its place by saying which of the two
	// the caller got wrong, and this assertion is what keeps it from being a branch nothing can
	// distinguish — the pattern this repository deleted from `IsRetriable` when mutations showed
	// those branches were unreachable.
	err := service.LockScriptField(ctx, LockScriptFieldRequest{
		VersionID: version.ID, Family: scriptdomain.FamilyStorySkeleton,
		Field: scriptdomain.LockSkeletonEndingHook,
	})
	if err == nil {
		t.Fatal("a skeleton lock was recorded against a script version")
	}
	if !strings.Contains(err.Error(), "family") {
		t.Fatalf("the refusal reads %q, which does not name the family mismatch", err)
	}
	// And an unknown version is a not-found rather than a silent success.
	if err := service.LockScriptField(ctx, LockScriptFieldRequest{
		VersionID: "nope", Field: scriptdomain.LockScriptSummary,
	}); err == nil {
		t.Fatal("a lock on a missing version was recorded")
	}
}

// TestLockAndUnlockRoundTrips covers the lock's own lifecycle.
func TestLockAndUnlockRoundTrips(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	version := seedScriptVersion(t, service)

	if err := service.LockScriptField(ctx, LockScriptFieldRequest{
		VersionID: version.ID, Field: scriptdomain.LockScriptSummary, LockedBy: "user-1",
	}); err != nil {
		t.Fatalf("LockScriptField: %v", err)
	}
	locks, err := service.ListScriptFieldLocks(ctx, version.ID)
	if err != nil {
		t.Fatalf("ListScriptFieldLocks: %v", err)
	}
	if len(locks) != 1 || locks[0].Field != scriptdomain.LockScriptSummary {
		t.Fatalf("the locks are %+v", locks)
	}
	// The family is RESOLVED from the version rather than taken from the request, which is what makes
	// the column trustworthy.
	if locks[0].Family != scriptdomain.FamilyScript {
		t.Fatalf("the lock's family is %q, want the one it was resolved from", locks[0].Family)
	}
	if err := service.UnlockScriptField(ctx, version.ID, scriptdomain.LockScriptSummary); err != nil {
		t.Fatalf("UnlockScriptField: %v", err)
	}
	locks, err = service.ListScriptFieldLocks(ctx, version.ID)
	if err != nil {
		t.Fatalf("ListScriptFieldLocks: %v", err)
	}
	if len(locks) != 0 {
		t.Fatalf("the lock survived its removal: %+v", locks)
	}
	// Unlocking twice is not an error: the caller asked for a state, and it holds.
	if err := service.UnlockScriptField(ctx, version.ID, scriptdomain.LockScriptSummary); err != nil {
		t.Fatalf("unlocking an unlocked field failed: %v", err)
	}
}

// TestDiffVersionsDispatchesByFamily covers the three-way dispatch and the reads behind it.
func TestDiffVersionsDispatchesByFamily(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	version := seedScriptVersion(t, service)
	if _, err := service.CreateScriptStructure(ctx, CreateScriptStructureRequest{
		ScriptID: version.ScriptID, ScriptVersionID: version.ID, Structure: structureFor(version.ID),
	}); err != nil {
		t.Fatalf("writing the first version: %v", err)
	}
	second, err := service.CreateScriptVersion(ctx, CreateScriptVersionRequest{
		ScriptID: version.ScriptID, BasedOnVersionID: version.ID,
		StorySkeletonVersionID: "s1", AdaptationStrategyVersionID: "a1",
	})
	if err != nil {
		t.Fatalf("CreateScriptVersion: %v", err)
	}
	// The revision changes one scene's goal and leaves everything else alone.
	changed := structureFor(second.ID)
	changed.Scenes[0].DramaticGoal = "a different goal"
	if _, err := service.CreateScriptStructure(ctx, CreateScriptStructureRequest{
		ScriptID: second.ScriptID, ScriptVersionID: second.ID, Structure: changed,
	}); err != nil {
		t.Fatalf("writing the second version: %v", err)
	}

	diff, err := service.DiffVersions(ctx, scriptdomain.FamilyScript, version.ID, second.ID)
	if err != nil {
		t.Fatalf("DiffVersions: %v", err)
	}
	if !diff.Changed() {
		t.Fatal("a revision with a change reports none")
	}
	if diff.Modified != 1 {
		t.Fatalf("the diff reports %d modified scenes", diff.Modified)
	}
	// The SCRIPT diff compares CONTENT, so a change the version row alone could not show is
	// reported — which is what makes it a diff of the artifact rather than of two summaries.
	var sawGoal bool
	for _, item := range diff.Items {
		for _, change := range item.Fields {
			if change.Field == "dramaticGoal" {
				sawGoal = true
			}
		}
	}
	if !sawGoal {
		t.Fatal("the changed scene goal was not reported")
	}
	// An unknown family is refused rather than defaulting to one.
	if _, err := service.DiffVersions(ctx, "nonsense", version.ID, second.ID); err == nil {
		t.Fatal("an unknown family was diffed")
	}
}

// TestListVersionsReturnsAHistory covers the read the Script UI needs.
func TestListVersionsReturnsAHistory(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	version := seedScriptVersion(t, service)
	second, err := service.CreateScriptVersion(ctx, CreateScriptVersionRequest{
		ScriptID: version.ScriptID, BasedOnVersionID: version.ID,
		StorySkeletonVersionID: "s1", AdaptationStrategyVersionID: "a1",
	})
	if err != nil {
		t.Fatalf("CreateScriptVersion: %v", err)
	}
	history, err := service.ListVersions(ctx, scriptdomain.FamilyScript, version.ScriptID)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	versions, ok := history.([]scriptdomain.ScriptVersion)
	if !ok {
		t.Fatalf("the history is a %T", history)
	}
	if len(versions) != 2 {
		t.Fatalf("the history holds %d versions", len(versions))
	}
	// NEWEST first, which is what a history view shows.
	if versions[0].ID != second.ID {
		t.Fatalf("the history starts with %q, want the newer version", versions[0].ID)
	}
	// An empty parent is a bad request rather than an empty history.
	if _, err := service.ListVersions(ctx, scriptdomain.FamilyScript, ""); err == nil {
		t.Fatal("an empty parent was accepted")
	}
}

// TestSetDialogueLineLockedRoundTrips covers the per-line half of the lock requirement.
func TestSetDialogueLineLockedRoundTrips(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	version := seedScriptVersion(t, service)
	if _, err := service.CreateScriptStructure(ctx, CreateScriptStructureRequest{
		ScriptID: version.ScriptID, ScriptVersionID: version.ID, Structure: structureFor(version.ID),
	}); err != nil {
		t.Fatalf("CreateScriptStructure: %v", err)
	}
	structure, err := service.GetScriptStructure(ctx, version.ID)
	if err != nil {
		t.Fatalf("GetScriptStructure: %v", err)
	}
	line := structure.Scenes[0].DialogueLines[0]
	if line.Locked {
		t.Fatal("a new line is already locked")
	}
	updated, err := service.SetDialogueLineLocked(ctx, SetDialogueLineLockedRequest{
		LineID: line.ID, Locked: true, Revision: line.Revision,
	})
	if err != nil {
		t.Fatalf("SetDialogueLineLocked: %v", err)
	}
	if !updated.Locked {
		t.Fatal("the lock did not stick")
	}
	// A stale revision is refused, so two windows cannot silently overwrite each other's lock.
	if _, err := service.SetDialogueLineLocked(ctx, SetDialogueLineLockedRequest{
		LineID: line.ID, Locked: false, Revision: line.Revision,
	}); err == nil {
		t.Fatal("a stale revision was accepted")
	}
	if _, err := service.SetDialogueLineLocked(ctx, SetDialogueLineLockedRequest{LineID: ""}); err == nil {
		t.Fatal("an empty line id was accepted")
	}
}

// TestProjectScriptVersionCallsTheProjector covers the projection command.
//
// The writer existed since WP-05 with tests and no caller, which is the "interface with no real path"
// AGENTS section 12 refuses. This asserts the service reaches it, and that a build with no projector
// REFUSES rather than reporting a success that did nothing.
func TestProjectScriptVersionCallsTheProjector(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	version := seedScriptVersion(t, service)
	if _, err := service.CreateScriptStructure(ctx, CreateScriptStructureRequest{
		ScriptID: version.ScriptID, ScriptVersionID: version.ID, Structure: structureFor(version.ID),
	}); err != nil {
		t.Fatalf("CreateScriptStructure: %v", err)
	}
	// With no projector composed, the command fails closed.
	if _, err := service.ProjectScriptVersion(ctx, ProjectScriptVersionRequest{
		ProjectID: "project-1", ScriptVersionID: version.ID,
	}); err == nil {
		t.Fatal("a build with no projector reported a successful projection")
	}

	projector := &recordingProjector{}
	withProjector := NewService(Options{
		Repository: store, Clock: fixedClock{}, IDs: newCounterIDs("script2"),
		Events: testRecorder{}, Projector: projector,
	})
	result, err := withProjector.ProjectScriptVersion(ctx, ProjectScriptVersionRequest{
		ProjectID: "project-1", ScriptVersionID: version.ID,
	})
	if err != nil {
		t.Fatalf("ProjectScriptVersion: %v", err)
	}
	if len(result.SceneNodeIDs) != 2 {
		t.Fatalf("the projection wrote %d nodes, want one per scene", len(result.SceneNodeIDs))
	}
	if len(projector.calls) != 2 {
		t.Fatalf("the projector was called %d times", len(projector.calls))
	}
	// The label is the scene's slugline, which is what a canvas node shows.
	if projector.calls[0].entityType != "scene" || projector.calls[0].label != "INT. 渡口 - 日" {
		t.Fatalf("the first call is %+v", projector.calls[0])
	}
	// An empty project is a bad request rather than a projection into nothing.
	if _, err := withProjector.ProjectScriptVersion(ctx, ProjectScriptVersionRequest{
		ScriptVersionID: version.ID,
	}); err == nil {
		t.Fatal("an empty project was accepted")
	}
}

// recordingProjector records what the service asked it to project.
type recordingProjector struct {
	calls []struct{ projectID, entityType, entityID, label string }
}

func (p *recordingProjector) ProjectEntity(_ context.Context, projectID, entityType, entityID, label string) (string, error) {
	p.calls = append(p.calls, struct{ projectID, entityType, entityID, label string }{projectID, entityType, entityID, label})
	return "node-" + entityID, nil
}

// categoryOfStructureTest reads a domain error's category through the public extractor.
func categoryOfStructureTest(err error) scriptdomain.ErrorCategory {
	domainErr, ok := scriptdomain.AsError(err)
	if !ok {
		return ""
	}
	return domainErr.Category
}
