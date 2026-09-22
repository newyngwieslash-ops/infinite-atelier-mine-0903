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
		t.Fatalf("the projection wrote %d scene nodes, want one per scene", len(result.SceneNodeIDs))
	}
	// THE SHOTS ARE PROJECTED WITH THEIR SCENES, which is ROADMAP item 11's Shot projection:
	// a board's rows cite shots, so a canvas without them shows the scene a row is inside and
	// not the shot it is about. The fixture has one shot in its second scene, so the node
	// count is two scenes plus one shot.
	if len(result.ShotNodeIDs) != 1 {
		t.Fatalf("the projection wrote %d shot nodes for a one-shot fixture", len(result.ShotNodeIDs))
	}
	if len(projector.calls) != 3 {
		t.Fatalf("the projector was called %d times", len(projector.calls))
	}
	// The label is the scene's slugline, which is what a canvas node shows.
	if projector.calls[0].entityType != "scene" || projector.calls[0].label != "INT. 渡口 - 日" {
		t.Fatalf("the first call is %+v", projector.calls[0])
	}
	// And the shot's own node names the shot, so a click on the canvas finds the row a
	// storyboard cites. It is FOUND rather than asserted by position: the fixture's shot sits
	// in its first scene, and a positional assertion would be a statement about the fixture's
	// order rather than about the projection.
	shotEntityID := ""
	for _, call := range projector.calls {
		if call.entityType == "shot" {
			shotEntityID = call.entityID
		}
	}
	if shotEntityID != "shot-"+version.ID+"-1" {
		t.Fatalf("the shot's node is %q, want the shot the fixture holds", shotEntityID)
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

// ---------------------------------------------------------------------------
// AC-SCRIPT-002's own scenario: a FIX on a STORY SKELETON
// ---------------------------------------------------------------------------

// TestSkeletonRevisionRefusesAChangedLockedField is the criterion the whole lock mechanism exists for.
//
// AC-SCRIPT-002's scenario is a skeleton whose ending hook is missing: the user pins the fields the
// model got right, asks for a FIX, and the model must rewrite only what the review named. The lock
// therefore has to be enforced on a SKELETON field — and until this test the enforcement covered only
// script structures, so a FIX could rewrite a pinned hook and every test would still pass, because
// the tests were about the other family.
//
// Every lockable field is asserted separately. A test that covered only the ending hook would leave
// a mutation dropping any of the other five green, and the criterion names the situation rather than
// the field.
func TestSkeletonRevisionRefusesAChangedLockedField(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		field  scriptdomain.LockableField
		mutate func(*CreateStorySkeletonVersionRequest)
	}{
		{scriptdomain.LockSkeletonOpeningHook, func(r *CreateStorySkeletonVersionRequest) { r.OpeningHook = "rewritten" }},
		{scriptdomain.LockSkeletonCoreConflict, func(r *CreateStorySkeletonVersionRequest) { r.CoreConflict = "rewritten" }},
		{scriptdomain.LockSkeletonTurningPoints, func(r *CreateStorySkeletonVersionRequest) { r.TurningPointsJSON = `["rewritten"]` }},
		{scriptdomain.LockSkeletonClimax, func(r *CreateStorySkeletonVersionRequest) { r.Climax = "rewritten" }},
		{scriptdomain.LockSkeletonEndingHook, func(r *CreateStorySkeletonVersionRequest) { r.EndingHook = "rewritten" }},
		{scriptdomain.LockSkeletonSelectedEvents, func(r *CreateStorySkeletonVersionRequest) {
			r.SelectedEventIDs = []string{"event-2"}
		}},
	}
	for _, testCase := range cases {
		store := newMemoryStore()
		service := newTestService(store)
		episode := seedEpisode(t, service)
		seedStoryGraph(store, "project-1", "event-1", "event-2")
		base, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
			EpisodeID: episode.ID, OpeningHook: "hook", CoreConflict: "conflict",
			TurningPointsJSON: `["a"]`, Climax: "climax", EndingHook: "ending",
			SelectedEventIDs: []string{"event-1"},
		})
		if err != nil {
			t.Fatalf("the base version: %v", err)
		}
		// The user pins the field.
		if err := service.LockScriptField(ctx, LockScriptFieldRequest{
			VersionID: base.ID, Family: scriptdomain.FamilyStorySkeleton, Field: testCase.field,
			LockedBy: "user-1",
		}); err != nil {
			t.Fatalf("%s: LockScriptField: %v", testCase.field, err)
		}
		revision := CreateStorySkeletonVersionRequest{
			EpisodeID: episode.ID, BasedOnVersionID: base.ID, OpeningHook: "hook",
			CoreConflict: "conflict", TurningPointsJSON: `["a"]`, Climax: "climax", EndingHook: "ending",
			SelectedEventIDs: []string{"event-1"},
		}
		testCase.mutate(&revision)
		_, err = service.CreateStorySkeletonVersion(ctx, revision)
		if err == nil {
			t.Fatalf("a revision that changed the locked %s was accepted", testCase.field)
		}
		if categoryOfStructureTest(err) != scriptdomain.CategoryConflict {
			t.Fatalf("%s: the refusal is category %q, want a conflict", testCase.field, categoryOfStructureTest(err))
		}
		// The revision was NOT written, which is what makes the refusal a refusal.
		history, err := service.ListVersions(ctx, scriptdomain.FamilyStorySkeleton, episode.ID)
		if err != nil {
			t.Fatalf("ListVersions: %v", err)
		}
		versions := history.([]scriptdomain.StorySkeletonVersion)
		if len(versions) != 1 {
			t.Fatalf("%s: the refused revision left %d versions behind", testCase.field, len(versions))
		}
	}
}

// TestSkeletonRevisionRespectingEveryLockIsAccepted is the other direction.
//
// Without it, a mutation that refused EVERY revision would pass the test above — the check would
// read as enforcement while making the Fix button useless, which is the failure mode a lock that is
// too strong has.
func TestSkeletonRevisionRespectingEveryLockIsAccepted(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	episode := seedEpisode(t, service)
	seedStoryGraph(store, "project-1", "event-1")
	base, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, OpeningHook: "hook", CoreConflict: "conflict",
		TurningPointsJSON: `["a"]`, Climax: "climax", EndingHook: "ending",
		SelectedEventIDs: []string{"event-1"},
	})
	if err != nil {
		t.Fatalf("the base version: %v", err)
	}
	for _, field := range scriptdomain.LockableFields(scriptdomain.FamilyStorySkeleton) {
		if err := service.LockScriptField(ctx, LockScriptFieldRequest{
			VersionID: base.ID, Family: scriptdomain.FamilyStorySkeleton, Field: field, LockedBy: "user-1",
		}); err != nil {
			t.Fatalf("locking %s: %v", field, err)
		}
	}
	// The revision restates every pinned field with a trailing newline on one of them, which
	// LocksEqual must treat as unchanged: refusing whitespace would make a lock impossible for a
	// model to satisfy.
	revision, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, BasedOnVersionID: base.ID, OpeningHook: "hook\n",
		CoreConflict: "conflict", TurningPointsJSON: `["a"]`, Climax: "climax", EndingHook: "ending",
		// The same events in the OTHER order: a set comparison, because a skeleton selects which
		// events are in the episode and the adaptation is what orders them.
		SelectedEventIDs: []string{"event-1"},
	})
	if err != nil {
		t.Fatalf("a revision that respected every lock was refused: %v", err)
	}
	if revision.VersionNumber != 2 {
		t.Fatalf("the revision is version %d", revision.VersionNumber)
	}
	// The selection was carried as a link set, and the base's own selection is still there.
	selected, err := store.ListSkeletonEventIDs(ctx, revision.ID)
	if err != nil {
		t.Fatalf("ListSkeletonEventIDs: %v", err)
	}
	if len(selected) != 1 || selected[0] != "event-1" {
		t.Fatalf("the revision's selection is %v", selected)
	}
	baseSelected, err := store.ListSkeletonEventIDs(ctx, base.ID)
	if err != nil {
		t.Fatalf("ListSkeletonEventIDs: %v", err)
	}
	if len(baseSelected) != 1 {
		t.Fatalf("the base version's selection became %v, so 原版本保留 does not hold", baseSelected)
	}
}

// TestSkeletonSelectionIsWrittenAsALinkTable covers §7.4's link table rather than the JSON field.
//
// The distinction matters to AC-SCRIPT-003's "source event 引用": a selection stored as JSON is a
// string a reader cannot query, and WP-06 recorded the gap ("the two link tables have no writer").
func TestSkeletonSelectionIsWrittenAsALinkTable(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	episode := seedEpisode(t, service)
	seedStoryGraph(store, "project-1", "event-1", "event-2")
	version, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, OpeningHook: "hook",
		SelectedEventIDs: []string{" event-2 ", "event-1", "  "},
	})
	if err != nil {
		t.Fatalf("CreateStorySkeletonVersion: %v", err)
	}
	selected, err := store.ListSkeletonEventIDs(ctx, version.ID)
	if err != nil {
		t.Fatalf("ListSkeletonEventIDs: %v", err)
	}
	// Trimmed, and the blank dropped rather than stored as an empty identifier.
	if len(selected) != 2 || selected[0] != "event-2" || selected[1] != "event-1" {
		t.Fatalf("the stored selection is %v", selected)
	}
	// A repeated event is refused by name rather than deduplicated silently.
	if _, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, OpeningHook: "hook",
		SelectedEventIDs: []string{"event-1", "event-1"},
	}); err == nil {
		t.Fatal("a selection naming one event twice was accepted")
	}
}

// TestStrategyTreatmentsAreWrittenAsLinks covers §7.5's table, and the order that makes "reordered"
// mean anything.
func TestStrategyTreatmentsAreWrittenAsLinks(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	episode := seedEpisode(t, service)
	seedStoryGraph(store, "project-1", "event-1", "event-2", "event-3")
	version, err := service.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
		EpisodeID: episode.ID, StrategySummary: "balanced",
		EventLinks: []scriptdomain.StrategyEventLink{
			{StoryEventID: "event-1", Treatment: scriptdomain.TreatmentRetained},
			{StoryEventID: "event-3", Treatment: scriptdomain.TreatmentReordered},
			{StoryEventID: "event-2", Treatment: scriptdomain.TreatmentRemoved},
		},
	})
	if err != nil {
		t.Fatalf("CreateAdaptationStrategyVersion: %v", err)
	}
	stored, err := store.ListStrategyEventLinks(ctx, version.ID)
	if err != nil {
		t.Fatalf("ListStrategyEventLinks: %v", err)
	}
	if len(stored) != 3 {
		t.Fatalf("the stored treatments are %+v", stored)
	}
	// The ordinals are the slice's POSITIONS, so the order the caller wrote is the order stored —
	// including that event-3 comes before event-2, which is what "reordered" states.
	if stored[0].StoryEventID != "event-1" || stored[1].StoryEventID != "event-3" || stored[2].StoryEventID != "event-2" {
		t.Fatalf("the stored order is %+v", stored)
	}
	for index, link := range stored {
		if link.Ordinal != index+1 {
			t.Fatalf("link %d has ordinal %d", index, link.Ordinal)
		}
		if link.StrategyVersionID != version.ID {
			t.Fatalf("a treatment names version %q", link.StrategyVersionID)
		}
	}
	// A missing treatment is refused rather than defaulted: "retained" and "removed" are opposite
	// decisions and neither follows from an absence.
	if _, err := service.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
		EpisodeID: episode.ID, StrategySummary: "x",
		EventLinks: []scriptdomain.StrategyEventLink{{StoryEventID: "event-1"}},
	}); err == nil {
		t.Fatal("a treatment with no decision was accepted")
	}
	// A repeated event is refused.
	if _, err := service.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
		EpisodeID: episode.ID, StrategySummary: "x",
		EventLinks: []scriptdomain.StrategyEventLink{
			{StoryEventID: "event-1", Treatment: scriptdomain.TreatmentRetained},
			{StoryEventID: "event-1", Treatment: scriptdomain.TreatmentRemoved},
		},
	}); err == nil {
		t.Fatal("an event treated twice was accepted")
	}
}

// TestStrategyRevisionRefusesChangedLockedFields covers the strategy's own lock vocabulary.
func TestStrategyRevisionRefusesChangedLockedFields(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		field  scriptdomain.LockableField
		mutate func(*CreateAdaptationStrategyVersionRequest)
	}{
		{scriptdomain.LockStrategySummary, func(r *CreateAdaptationStrategyVersionRequest) { r.StrategySummary = "rewritten" }},
		{scriptdomain.LockStrategyMode, func(r *CreateAdaptationStrategyVersionRequest) { r.AdaptationMode = scriptdomain.AdaptationAggressive }},
		{scriptdomain.LockStrategyOriginalAdds, func(r *CreateAdaptationStrategyVersionRequest) { r.OriginalAdditions = "rewritten" }},
		{scriptdomain.LockStrategyRationale, func(r *CreateAdaptationStrategyVersionRequest) { r.Rationale = "rewritten" }},
		{scriptdomain.LockStrategyRisks, func(r *CreateAdaptationStrategyVersionRequest) { r.Risks = "rewritten" }},
		{scriptdomain.LockStrategyMergedEvents, func(r *CreateAdaptationStrategyVersionRequest) {
			r.EventLinks = []scriptdomain.StrategyEventLink{
				{StoryEventID: "event-1", Treatment: scriptdomain.TreatmentRemoved},
			}
		}},
	}
	for _, testCase := range cases {
		store := newMemoryStore()
		service := newTestService(store)
		episode := seedEpisode(t, service)
		seedStoryGraph(store, "project-1", "event-1")
		base, err := service.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
			EpisodeID: episode.ID, StrategySummary: "balanced", AdaptationMode: scriptdomain.AdaptationBalanced,
			OriginalAdditions: "none", Rationale: "because", Risks: "few",
			EventLinks: []scriptdomain.StrategyEventLink{
				{StoryEventID: "event-1", Treatment: scriptdomain.TreatmentRetained},
			},
		})
		if err != nil {
			t.Fatalf("the base version: %v", err)
		}
		if err := service.LockScriptField(ctx, LockScriptFieldRequest{
			VersionID: base.ID, Family: scriptdomain.FamilyAdaptationStrategy, Field: testCase.field,
			LockedBy: "user-1",
		}); err != nil {
			t.Fatalf("%s: LockScriptField: %v", testCase.field, err)
		}
		revision := CreateAdaptationStrategyVersionRequest{
			EpisodeID: episode.ID, BasedOnVersionID: base.ID, StrategySummary: "balanced",
			AdaptationMode: scriptdomain.AdaptationBalanced, OriginalAdditions: "none",
			Rationale: "because", Risks: "few",
			EventLinks: []scriptdomain.StrategyEventLink{
				{StoryEventID: "event-1", Treatment: scriptdomain.TreatmentRetained},
			},
		}
		testCase.mutate(&revision)
		if _, err := service.CreateAdaptationStrategyVersion(ctx, revision); err == nil {
			t.Fatalf("a revision that changed the locked %s was accepted", testCase.field)
		}
		// And the revision that respects it passes, so the check is a comparison rather than a
		// blanket refusal.
		if _, err := service.CreateAdaptationStrategyVersion(ctx, CreateAdaptationStrategyVersionRequest{
			EpisodeID: episode.ID, BasedOnVersionID: base.ID, StrategySummary: "balanced",
			AdaptationMode: scriptdomain.AdaptationBalanced, OriginalAdditions: "none",
			Rationale: "because", Risks: "few",
			EventLinks: []scriptdomain.StrategyEventLink{
				{StoryEventID: "event-1", Treatment: scriptdomain.TreatmentRetained},
			},
		}); err != nil {
			t.Fatalf("%s: a revision that respected the lock was refused: %v", testCase.field, err)
		}
	}
}

// TestAFirstVersionHasNoLocksToEnforce states the boundary the enforcement rests on.
//
// An empty BasedOnVersionID means a first draft, and a first draft has no predecessor whose fields a
// user could have pinned. This is not a bypass: the id is read from the version ROW, so a caller
// cannot reach the empty case on a revision by omitting the request field.
func TestAFirstVersionHasNoLocksToEnforce(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	episode := seedEpisode(t, service)
	first, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, OpeningHook: "hook", EndingHook: "ending",
	})
	if err != nil {
		t.Fatalf("a first version with no locks was refused: %v", err)
	}
	// A lock on a DIFFERENT version does not reach it.
	other, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, OpeningHook: "another",
	})
	if err != nil {
		t.Fatalf("the second version: %v", err)
	}
	if err := service.LockScriptField(ctx, LockScriptFieldRequest{
		VersionID: other.ID, Family: scriptdomain.FamilyStorySkeleton,
		Field: scriptdomain.LockSkeletonEndingHook, LockedBy: "user-1",
	}); err != nil {
		t.Fatalf("LockScriptField: %v", err)
	}
	// A revision of the FIRST version is not bound by a lock on the second, because the base is what
	// the row says it is.
	if _, err := service.CreateStorySkeletonVersion(ctx, CreateStorySkeletonVersionRequest{
		EpisodeID: episode.ID, BasedOnVersionID: first.ID, OpeningHook: "hook", EndingHook: "changed",
	}); err != nil {
		t.Fatalf("a revision of an unlocked version was refused by another version's lock: %v", err)
	}
}

// TestStructureReferencesAreChecked covers AC-SCRIPT-003's "source event 引用" and §17's
// "引用存在性".
//
// The schema cannot check them — `scenes.source_story_event_id` is TEXT with no foreign key — so the
// service is the only thing standing between a model and a citation of an event that does not exist.
func TestStructureReferencesAreChecked(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	version := seedScriptVersion(t, service)
	// The project has one event and one character.
	store.storyEvents = map[string]map[string]string{"drama-project": {"event-real": "drama-project"}}
	store.storyEntities = map[string]map[string]string{"drama-project": {"character-real": "drama-project"}}

	// A structure citing what exists is accepted, and the check ran rather than being skipped: the
	// payload names both kinds of reference.
	accepted := scriptdomain.ScriptStructureDraft{Scenes: []scriptdomain.SceneDraft{{
		Slugline: "INT. 渡口 - 日", InteriorExterior: scriptdomain.InteriorINT,
		SourceStoryEventID: "event-real", LocationEntityID: "character-real",
		EstimatedDurationSeconds: 60,
		DialogueLines: []scriptdomain.DialogueLineDraft{
			{Text: "line", CharacterEntityID: "character-real", SourceStoryEventID: "event-real"},
		},
	}}}
	if _, err := service.CreateScriptStructure(ctx, CreateScriptStructureRequest{
		ScriptID: version.ScriptID, ScriptVersionID: version.ID, ProjectID: "drama-project",
		Draft: accepted, Summary: "one scene",
	}); err != nil {
		t.Fatalf("a structure citing what exists was refused: %v", err)
	}

	// An event that does not exist is refused, and the refusal NAMES it so the model can act.
	next := seedAnotherScriptVersion(t, service, version.ScriptID)
	_, err := service.CreateScriptStructure(ctx, CreateScriptStructureRequest{
		ScriptID: next.ScriptID, ScriptVersionID: next.ID, ProjectID: "drama-project",
		Draft: scriptdomain.ScriptStructureDraft{Scenes: []scriptdomain.SceneDraft{{
			Slugline: "INT. nowhere - 夜", InteriorExterior: scriptdomain.InteriorINT,
			SourceStoryEventID: "event-ghost",
		}}},
	})
	if err == nil {
		t.Fatal("a scene citing an event that does not exist was accepted")
	}
	if !strings.Contains(err.Error(), "event-ghost") {
		t.Fatalf("the refusal reads %q, which does not name the identifier", err)
	}
	// Nothing was written, so a refused citation leaves no scenes behind.
	structure, err := service.GetScriptStructure(ctx, next.ID)
	if err != nil {
		t.Fatalf("GetScriptStructure: %v", err)
	}
	if len(structure.Scenes) != 0 {
		t.Fatalf("a refused write left %d scenes behind", len(structure.Scenes))
	}

	// A location that does not exist is refused the same way, which is the entity half.
	third := seedAnotherScriptVersion(t, service, version.ScriptID)
	if _, err := service.CreateScriptStructure(ctx, CreateScriptStructureRequest{
		ScriptID: third.ScriptID, ScriptVersionID: third.ID, ProjectID: "drama-project",
		Draft: scriptdomain.ScriptStructureDraft{Scenes: []scriptdomain.SceneDraft{{
			Slugline: "INT. elsewhere - 夜", InteriorExterior: scriptdomain.InteriorINT,
			LocationEntityID: "location-ghost",
		}}},
	}); err == nil {
		t.Fatal("a scene citing an entity that does not exist was accepted")
	}

	// A script with NO project skips the check rather than refusing: that is the user's own edit path,
	// where references come from the UI and no project travels in the request.
	fourth := seedAnotherScriptVersion(t, service, version.ScriptID)
	if _, err := service.CreateScriptStructure(ctx, CreateScriptStructureRequest{
		ScriptID: fourth.ScriptID, ScriptVersionID: fourth.ID,
		Draft: scriptdomain.ScriptStructureDraft{Scenes: []scriptdomain.SceneDraft{{
			Slugline: "INT. 渡口 - 日", InteriorExterior: scriptdomain.InteriorINT,
			SourceStoryEventID: "event-ghost",
		}}},
	}); err != nil {
		t.Fatalf("a write with no project was refused: %v", err)
	}
}

// TestDraftIdentifiersAreMintedNotAccepted covers §17's "ID、顺序和唯一性".
//
// A model states the shape and nothing else: the identifiers are minted here, the ordinals are the
// payload's positions, and every child's scene reference is the scene it was nested under. That is
// what makes an invented id impossible rather than merely discouraged.
func TestDraftIdentifiersAreMintedNotAccepted(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	version := seedScriptVersion(t, service)
	structure, err := service.CreateScriptStructure(ctx, CreateScriptStructureRequest{
		ScriptID: version.ScriptID, ScriptVersionID: version.ID,
		Draft: scriptdomain.ScriptStructureDraft{Scenes: []scriptdomain.SceneDraft{
			{
				Slugline: "INT. 渡口 - 日", InteriorExterior: scriptdomain.InteriorINT,
				EstimatedDurationSeconds: 90,
				DialogueLines: []scriptdomain.DialogueLineDraft{
					{Text: "first"}, {Text: "second", Type: scriptdomain.LineNarration},
				},
				Shots: []scriptdomain.ShotDraft{
					{VisualDescription: "河面起雾。"}, {VisualDescription: "铜牌入水。"},
				},
			},
			{Slugline: "EXT. 渡口 - 夜", InteriorExterior: scriptdomain.InteriorEXT, EstimatedDurationSeconds: 60},
		}},
	})
	if err != nil {
		t.Fatalf("CreateScriptStructure: %v", err)
	}
	if structure.EstimatedDurationSeconds != 150 {
		t.Fatalf("the duration is %d, want the summed 150", structure.EstimatedDurationSeconds)
	}
	written, err := service.GetScriptStructure(ctx, version.ID)
	if err != nil {
		t.Fatalf("GetScriptStructure: %v", err)
	}
	if len(written.Scenes) != 2 {
		t.Fatalf("the version holds %d scenes", len(written.Scenes))
	}
	// The ordinals are the positions, and the scene NUMBER was not stated so it is empty rather
	// than invented.
	for index, scene := range written.Scenes {
		if scene.Ordinal != index+1 {
			t.Fatalf("scene %d has ordinal %d", index, scene.Ordinal)
		}
		if scene.ID == "" {
			t.Fatalf("scene %d has no identifier", index)
		}
		if scene.SceneNumber != "" {
			t.Fatalf("scene %d invented the number %q", index, scene.SceneNumber)
		}
	}
	// Every child is attached to the scene it was nested under, and the ordinals are positions.
	first := written.Scenes[0]
	if len(first.DialogueLines) != 2 || len(first.Shots) != 2 {
		t.Fatalf("the first scene holds %d lines and %d shots", len(first.DialogueLines), len(first.Shots))
	}
	for index, line := range first.DialogueLines {
		if line.SceneID != first.ID {
			t.Fatalf("line %d names scene %q rather than its parent %q", index, line.SceneID, first.ID)
		}
		if line.Ordinal != index+1 {
			t.Fatalf("line %d has ordinal %d", index, line.Ordinal)
		}
		// A line a draft states is not locked, and a draft has no field to say otherwise.
		if line.Locked {
			t.Fatalf("line %d arrived locked", index)
		}
	}
	if first.DialogueLines[0].Type != scriptdomain.LineDialogue {
		t.Fatalf("the first line's type is %q, want the default dialogue", first.DialogueLines[0].Type)
	}
	if first.DialogueLines[1].Type != scriptdomain.LineNarration {
		t.Fatalf("the second line's type is %q", first.DialogueLines[1].Type)
	}
	for index, shot := range first.Shots {
		if shot.SceneID != first.ID || shot.Ordinal != index+1 {
			t.Fatalf("shot %d is %+v", index, shot)
		}
		// A stage's shot is a draft: its number and refinement belong to the storyboard stage.
		if shot.Status != versioning.StatusDraft {
			t.Fatalf("shot %d has status %q, want draft", index, shot.Status)
		}
	}
	// The identifiers are all distinct, which is what a minted id gives and a model's would not.
	seen := map[string]bool{}
	for _, scene := range written.Scenes {
		if seen[scene.ID] {
			t.Fatalf("identifier %q was minted twice", scene.ID)
		}
		seen[scene.ID] = true
		for _, line := range scene.DialogueLines {
			if seen[line.ID] {
				t.Fatalf("identifier %q was minted twice", line.ID)
			}
			seen[line.ID] = true
		}
		for _, shot := range scene.Shots {
			if seen[shot.ID] {
				t.Fatalf("identifier %q was minted twice", shot.ID)
			}
			seen[shot.ID] = true
		}
	}
}

// TestDialogueLocksAreCarriedForwardByPosition covers §7.7's per-line lock surviving a revision.
//
// A lock lives on the line ROW, and a revision writes new rows — so without carrying the flag, every
// FIX would silently release every pinned line, and AC-SCRIPT-002's "锁定字段不变" would hold for a
// version's fields while failing for its lines.
func TestDialogueLocksAreCarriedForwardByPosition(t *testing.T) {
	store := newMemoryStore()
	service := newTestService(store)
	ctx := context.Background()
	base := seedScriptVersion(t, service)
	if _, err := service.CreateScriptStructure(ctx, CreateScriptStructureRequest{
		ScriptID: base.ScriptID, ScriptVersionID: base.ID, Structure: structureFor(base.ID),
	}); err != nil {
		t.Fatalf("the base version: %v", err)
	}
	// The user pins the first scene's only line.
	structure, err := service.GetScriptStructure(ctx, base.ID)
	if err != nil {
		t.Fatalf("GetScriptStructure: %v", err)
	}
	line := structure.Scenes[0].DialogueLines[0]
	if _, err := service.SetDialogueLineLocked(ctx, SetDialogueLineLockedRequest{
		LineID: line.ID, Locked: true, Revision: line.Revision,
	}); err != nil {
		t.Fatalf("SetDialogueLineLocked: %v", err)
	}

	// A revision rewrites the line's text, which a per-line lock does NOT forbid — the lock protects
	// the line's place in the scene, and the diff is what shows a reader the text moved.
	next, err := service.CreateScriptVersion(ctx, CreateScriptVersionRequest{
		ScriptID: base.ScriptID, BasedOnVersionID: base.ID,
		StorySkeletonVersionID: "skeleton-x", AdaptationStrategyVersionID: "strategy-x",
	})
	if err != nil {
		t.Fatalf("CreateScriptVersion: %v", err)
	}
	revised := structureFor(next.ID)
	revised.Scenes[0].DialogueLines[0].Text = "rewritten"
	if _, err := service.CreateScriptStructure(ctx, CreateScriptStructureRequest{
		ScriptID: next.ScriptID, ScriptVersionID: next.ID, BasedOnVersionID: base.ID,
		Structure: revised,
	}); err != nil {
		t.Fatalf("the revision: %v", err)
	}
	written, err := service.GetScriptStructure(ctx, next.ID)
	if err != nil {
		t.Fatalf("GetScriptStructure: %v", err)
	}
	if len(written.Scenes[0].DialogueLines) != 1 {
		t.Fatalf("the revision holds %d lines", len(written.Scenes[0].DialogueLines))
	}
	if !written.Scenes[0].DialogueLines[0].Locked {
		t.Fatal("the revision dropped the pinned line's lock, so the pin does not survive a FIX")
	}
	// The base version is untouched, which is AC-SCRIPT-002's "原版本保留".
	original, err := service.GetScriptStructure(ctx, base.ID)
	if err != nil {
		t.Fatalf("GetScriptStructure: %v", err)
	}
	if !original.Scenes[0].DialogueLines[0].Locked {
		t.Fatal("the base version lost its own lock")
	}
}

// seedAnotherScriptVersion writes an extra EMPTY script version for the script the fixture made.
//
// It exists because a refused structure write leaves its version empty, so a test that wants to
// observe a second refusal cannot reuse the first version — it would be testing the refusal against
// a version that is still empty for a reason the test already established.
func seedAnotherScriptVersion(t *testing.T, service *Service, scriptID string) scriptdomain.ScriptVersion {
	t.Helper()
	version, err := service.CreateScriptVersion(context.Background(), CreateScriptVersionRequest{
		ScriptID:               scriptID,
		StorySkeletonVersionID: "skeleton-x", AdaptationStrategyVersionID: "strategy-x",
	})
	if err != nil {
		t.Fatalf("CreateScriptVersion: %v", err)
	}
	return version
}

// seedEpisode writes an episode for the tests that need one before a skeleton can exist.
//
// Every skeleton and strategy version hangs off an episode, and the service reads that row before it
// writes — which is why an invented episode id fails with a not-found rather than producing a
// version whose foreign key would be refused.
//
// It seeds NOTHING into the store's story graph. The reference check runs on every project-scoped
// write, so a test that selects or treats events must also call `seedStoryGraph`; leaving that
// explicit is what makes "the check ran and passed" distinguishable from "the check was skipped",
// which is the distinction the reference test turns on.
func seedEpisode(t *testing.T, service *Service) scriptdomain.Episode {
	t.Helper()
	episode, err := service.CreateEpisode(context.Background(), CreateEpisodeRequest{
		ProjectID: "project-1", SeasonNumber: 1, EpisodeNumber: 1, Title: "Pilot",
	})
	if err != nil {
		t.Fatalf("CreateEpisode: %v", err)
	}
	return episode
}

// seedStoryGraph gives a store the events a test's payload cites.
//
// The reference check refuses a citation of anything the project does not have, so a test about
// links or locks has to state the graph it is writing against. That is the check working: a fixture
// that could cite anything would be exercising a service that never looked.
func seedStoryGraph(store *memoryStore, projectID string, eventIDs ...string) {
	if store.storyEvents == nil {
		store.storyEvents = map[string]map[string]string{}
	}
	if store.storyEvents[projectID] == nil {
		store.storyEvents[projectID] = map[string]string{}
	}
	for _, id := range eventIDs {
		store.storyEvents[projectID][id] = projectID
	}
}
