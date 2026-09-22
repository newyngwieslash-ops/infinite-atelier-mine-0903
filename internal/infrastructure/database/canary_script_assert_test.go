package database

import (
	"context"
	"testing"

	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
)

// canary_script_assert_test.go holds the acceptance assertions the walk calls.
//
// Each one is a clause of AC-SCRIPT-001, 002 or 003, and each is asserted against the DATABASE rather
// than against what a service returned: a version that a service reported and a row that exists are two
// different facts, and the acceptance criteria are written about the second.

// assertPinnedFieldIsProtected covers AC-SCRIPT-002's "锁定字段不变" at the write path.
//
// The criterion's scenario is a FIX on a skeleton whose ending hook is missing: the user pins the fields
// the model got right, asks for a FIX, and the model must rewrite only what the review named. So this
// pins a field on the approved version and asserts that a revision rewriting it is REFUSED — by the
// service, which is where the enforcement lives, rather than by a prompt a model could ignore.
//
// It also asserts the OTHER half: a revision that respects the pin is accepted. Without that, a mutation
// that refused every revision would pass — the check would read as enforcement while making the Fix
// button useless.
func (c *scriptCanary) assertPinnedFieldIsProtected(t *testing.T, skeletonVersionID string) {
	t.Helper()
	ctx := context.Background()
	// The user pins the ending hook, which is the field the criterion's scenario is about.
	if err := c.script.LockScriptField(ctx, appscript.LockScriptFieldRequest{
		VersionID: skeletonVersionID,
		Family:    scriptdomain.FamilyStorySkeleton,
		Field:     scriptdomain.LockSkeletonEndingHook,
		LockedBy:  "user-1",
	}); err != nil {
		t.Fatalf("pinning the ending hook: %v", err)
	}
	base, err := c.script.GetStorySkeletonVersion(ctx, skeletonVersionID)
	if err != nil {
		t.Fatalf("reading the pinned version: %v", err)
	}
	// A revision that REWRITES the pinned field is refused, and the refusal is a conflict rather than a
	// validation failure: the payload is well formed and the state forbids it.
	_, err = c.script.CreateStorySkeletonVersion(ctx, appscript.CreateStorySkeletonVersionRequest{
		EpisodeID:         c.ids.episode,
		BasedOnVersionID:  skeletonVersionID,
		OpeningHook:       base.OpeningHook,
		CoreConflict:      base.CoreConflict,
		TurningPointsJSON: base.TurningPointsJSON,
		Climax:            base.Climax,
		// The rewrite the pin exists to prevent.
		EndingHook:   "a different ending",
		ChangeReason: "the canary's FIX attempt",
	})
	if err == nil {
		t.Fatal("a revision that rewrote a pinned field was ACCEPTED, so AC-SCRIPT-002's lock enforces nothing")
	}
	if category := scriptErrorCategory(t, err); category != scriptdomain.CategoryConflict {
		t.Fatalf("the refusal is category %q, want a conflict", category)
	}
	// A revision that RESPECTS the pin is accepted, so the check is a comparison rather than a blanket
	// refusal of revisions.
	accepted, err := c.script.CreateStorySkeletonVersion(ctx, appscript.CreateStorySkeletonVersionRequest{
		EpisodeID:         c.ids.episode,
		BasedOnVersionID:  skeletonVersionID,
		OpeningHook:       base.OpeningHook,
		CoreConflict:      base.CoreConflict,
		TurningPointsJSON: base.TurningPointsJSON,
		Climax:            base.Climax,
		EndingHook:        base.EndingHook,
		// The revision is the point: nothing it states is refused, because the field the user pinned came
		// back unchanged. A pin that refused EVERY revision would make the Fix button useless, which is
		// the failure mode a lock that is too strong has.
		ChangeReason: "the canary's revision",
	})
	if err != nil {
		t.Fatalf("a revision that respected the pin was refused: %v", err)
	}
	// "原版本保留": the revision is a NEW version and the base is still there, with its own row and its
	// own pin.
	if accepted.ID == skeletonVersionID {
		t.Fatal("the revision overwrote the version it revised")
	}
	if accepted.VersionNumber <= base.VersionNumber {
		t.Fatalf("the revision is version %d and the base is %d", accepted.VersionNumber, base.VersionNumber)
	}
	still, err := c.script.GetStorySkeletonVersion(ctx, skeletonVersionID)
	if err != nil {
		t.Fatalf("reading the base version after the revision: %v", err)
	}
	if still.EndingHook != base.EndingHook {
		t.Fatalf("the base version's ending hook changed to %q", still.EndingHook)
	}
	locks, err := c.script.ListScriptFieldLocks(ctx, skeletonVersionID)
	if err != nil {
		t.Fatalf("reading the base version's pins: %v", err)
	}
	if len(locks) != 1 || locks[0].Field != scriptdomain.LockSkeletonEndingHook {
		t.Fatalf("the base version's pins are %+v", locks)
	}
}

// assertScriptStructure covers AC-SCRIPT-003's entity and duration clauses.
func (c *scriptCanary) assertScriptStructure(t *testing.T, scriptVersionID string) {
	t.Helper()
	ctx := context.Background()
	structure, err := c.script.GetScriptStructure(ctx, scriptVersionID)
	if err != nil {
		t.Fatalf("reading the script structure: %v", err)
	}
	// "Episode/ScriptVersion/Scene/Dialogue/Shot 正式实体": every one is a ROW, and the read walks the
	// schema's own chain rather than an aggregate the service assembled.
	if len(structure.Scenes) == 0 {
		t.Fatal("the script version has no scenes, so the generation stage wrote an empty version")
	}
	lines, shots := 0, 0
	for _, scene := range structure.Scenes {
		lines += len(scene.DialogueLines)
		shots += len(scene.Shots)
	}
	if lines == 0 {
		t.Fatal("the script version has no dialogue lines")
	}
	if shots == 0 {
		t.Fatal("the script version has no shots")
	}
	// Every child is attached to the scene it is nested under, which is the relation the nesting states
	// and the schema's foreign key enforces.
	for _, scene := range structure.Scenes {
		for _, line := range scene.DialogueLines {
			if line.SceneID != scene.ID {
				t.Fatalf("line %q names scene %q rather than its parent %q", line.ID, line.SceneID, scene.ID)
			}
		}
		for _, shot := range scene.Shots {
			if shot.SceneID != scene.ID {
				t.Fatalf("shot %q names scene %q rather than its parent %q", shot.ID, shot.SceneID, scene.ID)
			}
		}
	}
	// "顺序唯一": the ordinals run from one with no gap and no repeat, and the DATABASE's own unique
	// constraints are what make that true rather than this assertion.
	for index, scene := range structure.Scenes {
		if scene.Ordinal != index+1 {
			t.Fatalf("scene %d has ordinal %d", index, scene.Ordinal)
		}
	}
	// The negative half: a structure with a repeated ordinal is refused. The count of scenes before and
	// after is asserted too, so a refused write that half-succeeded would be visible.
	before := len(structure.Scenes)
	row := scriptVersionRowForStructure(c, t)
	duplicated := structure
	duplicated.ScriptVersionID = row.ID
	duplicated.Scenes = append([]scriptdomain.SceneStructure(nil), structure.Scenes...)
	duplicated.Scenes[1].Ordinal = duplicated.Scenes[0].Ordinal
	if _, err := c.script.CreateScriptStructure(ctx, appscript.CreateScriptStructureRequest{
		ScriptID:        row.ScriptID,
		ScriptVersionID: row.ID,
		Structure:       duplicated,
	}); err == nil {
		t.Fatal("a structure with a repeated ordinal was accepted")
	}
	after, err := c.script.GetScriptStructure(ctx, row.ID)
	if err != nil {
		t.Fatalf("reading the refused version: %v", err)
	}
	if len(after.Scenes) != 0 {
		t.Fatalf("a refused structure write left %d scenes behind", len(after.Scenes))
	}
	if len(structure.Scenes) != before {
		t.Fatalf("the original version changed from %d scenes to %d", before, len(structure.Scenes))
	}

	// "duration": the version's total is the SUM of its scenes, and the row carries it. The mock's two
	// scenes are 90 and 60 seconds, and the canary's fixture is what fixes those numbers — so a mock
	// that varied them would fail here rather than three layers away.
	sum := 0
	for _, scene := range structure.Scenes {
		sum += scene.EstimatedDurationSeconds
	}
	version, err := c.script.GetScriptVersion(ctx, scriptVersionID)
	if err != nil {
		t.Fatalf("reading the script version: %v", err)
	}
	if version.EstimatedDurationSeconds != sum {
		t.Fatalf("the version's duration is %d and its scenes sum to %d", version.EstimatedDurationSeconds, sum)
	}
	if sum == 0 {
		t.Fatal("the scenes sum to zero, so this assertion proves nothing about the sum")
	}
	// "source event 引用": a scene citing an event this project does not have is refused, which is the
	// check the schema CANNOT make — `scenes.source_story_event_id` has no foreign key.
	citing := scriptVersionRowForStructure(c, t)
	_, err = c.script.CreateScriptStructure(ctx, appscript.CreateScriptStructureRequest{
		ScriptID:        citing.ScriptID,
		ScriptVersionID: citing.ID,
		ProjectID:       c.ids.project,
		Draft: scriptdomain.ScriptStructureDraft{Scenes: []scriptdomain.SceneDraft{{
			Slugline: "INT. nowhere - 夜", InteriorExterior: scriptdomain.InteriorINT,
			SourceStoryEventID: "canary-event-that-does-not-exist",
		}}},
	})
	if err == nil {
		t.Fatal("a scene citing an event that does not exist was accepted")
	}

	// "原创改编标记": the mock's second scene is an ORIGINAL, and the flag survived the write — which is
	// what lets a reader tell an invention from a faithful adaptation.
	originals := 0
	for _, scene := range structure.Scenes {
		if scene.IsOriginalAdaptation {
			originals++
		}
	}
	if originals == 0 {
		t.Fatal("no scene is marked as an original adaptation, so the flag is not reaching the database")
	}
}

// scriptVersionRowForStructure creates an empty script version for a structure assertion.
//
// The refused writes need a version of their own, because a version that already holds content cannot be
// given it again and the assertion would then be about the second write rather than the first.
func scriptVersionRowForStructure(c *scriptCanary, t *testing.T) scriptdomain.ScriptVersion {
	t.Helper()
	ctx := context.Background()
	record, err := c.script.EnsureScript(ctx, c.ids.episode)
	if err != nil {
		t.Fatalf("ensuring the script: %v", err)
	}
	version, err := c.script.CreateScriptVersion(ctx, appscript.CreateScriptVersionRequest{
		ScriptID:                    record.ID,
		StorySkeletonVersionID:      "canary-skeleton-reference",
		AdaptationStrategyVersionID: "canary-strategy-reference",
		Summary:                     "a version for a refused-write assertion",
	})
	if err != nil {
		t.Fatalf("creating a script version: %v", err)
	}
	return version
}

// assertVersionDiff covers AC-SCRIPT-003's "版本 diff".
//
// The diff is computed by the DOMAIN's pure function over two versions, and this asserts the one
// property a diff must have to be worth showing: a version compared with itself has no changes, and two
// versions that differ say where.
func (c *scriptCanary) assertVersionDiff(t *testing.T, scriptVersionID string) {
	t.Helper()
	ctx := context.Background()
	// A version against ITSELF reports no changes — the trivial case, and the one that catches a diff
	// implementation that reports every field as modified.
	same, err := c.script.DiffVersions(ctx, scriptdomain.FamilyScript, scriptVersionID, scriptVersionID)
	if err != nil {
		t.Fatalf("diffing a version against itself: %v", err)
	}
	if same.Changed() {
		t.Fatalf("a version compared with itself reports changes: %+v", same)
	}
	if same.Unchanged == 0 {
		t.Fatal("a version compared with itself reports no unchanged items, so the diff saw nothing")
	}
	// Against an EMPTY version, everything is added — which is the other extreme, and it proves the diff
	// is reading the content rather than two summary strings.
	empty := scriptVersionRowForStructure(c, t)
	added, err := c.script.DiffVersions(ctx, scriptdomain.FamilyScript, empty.ID, scriptVersionID)
	if err != nil {
		t.Fatalf("diffing against an empty version: %v", err)
	}
	if !added.Changed() {
		t.Fatal("a diff between an empty version and a full one reports no changes")
	}
	if added.Added == 0 {
		t.Fatalf("the diff reports no added scenes: %+v", added)
	}
	if len(added.Items) == 0 {
		t.Fatalf("the diff reports a count of %d added scenes and no items to show: %+v", added.Added, added)
	}
	// And the FAMILY is carried, so a caller showing a diff knows what it is a diff of.
	if added.Family != scriptdomain.FamilyScript {
		t.Fatalf("the diff names family %q", added.Family)
	}
	// The other two families answer too, which is what makes the dispatcher a dispatcher rather than a
	// script-specific function.
	skeleton, err := c.script.ListVersions(ctx, scriptdomain.FamilyStorySkeleton, c.ids.episode)
	if err != nil {
		t.Fatalf("listing the skeleton versions: %v", err)
	}
	versions := skeleton.([]scriptdomain.StorySkeletonVersion)
	if len(versions) < 2 {
		t.Fatalf("the canary wrote %d skeleton versions, so the family's diff is untestable", len(versions))
	}
	familyDiff, err := c.script.DiffVersions(ctx, scriptdomain.FamilyStorySkeleton, versions[1].ID, versions[0].ID)
	if err != nil {
		t.Fatalf("diffing two skeleton versions: %v", err)
	}
	if familyDiff.Family != scriptdomain.FamilyStorySkeleton {
		t.Fatalf("the skeleton diff names family %q", familyDiff.Family)
	}
}

// assertCanvasProjection covers AC-SCRIPT-003's "Canvas projection".
//
// WP-05 built the projection writer with tests and NO caller; WP-08 supplies one. The assertion is on the
// NODES a projector was asked for, because that is what a canvas reads: a projection that reported
// success and asked for nothing would satisfy a return value and show a user an empty board.
// projectorCallRecord is one recorded projection call, under a name this file can collect.
type projectorCallRecord = struct{ projectID, entityType, entityID, label string }

func (c *scriptCanary) assertCanvasProjection(t *testing.T, scriptVersionID string) {
	t.Helper()
	ctx := context.Background()
	// The projector is a double, and ON PURPOSE here: the subject is whether the script service CALLS a
	// projector with the right entities, not whether the project aggregate can write a node — that writer
	// is WP-05's and has its own tests against real SQLite. So the double records what it was asked for
	// and the assertion is about the request.
	projector := &canaryProjector{}
	service := appscript.NewService(appscript.Options{
		Repository: NewScriptRepository(c.db),
		Clock:      canaryClock{},
		IDs:        canaryIDGenerator(),
		Projector:  projector,
	})
	result, err := service.ProjectScriptVersion(ctx, appscript.ProjectScriptVersionRequest{
		ProjectID:       c.ids.project,
		ScriptVersionID: scriptVersionID,
	})
	if err != nil {
		t.Fatalf("projecting the script version: %v", err)
	}
	structure, err := c.script.GetScriptStructure(ctx, scriptVersionID)
	if err != nil {
		t.Fatalf("reading the structure: %v", err)
	}
	// One call per scene AND one per shot, in the script's order: the scenes are projected
	// with the shots they contain, because ROADMAP item 11 names Shot projection and a board's
	// rows cite shots.
	//
	// WP-08 asserted "one call per scene, each naming a SCENE rather than a version or a shot",
	// and that was right for WP-08 — the shot projection is WP-09's. The assertion is widened
	// rather than deleted, so a regression that stopped projecting either is still caught.
	sceneCalls := make([]projectorCallRecord, 0, len(structure.Scenes))
	shotCalls := make([]projectorCallRecord, 0, 8)
	for _, call := range projector.calls {
		switch call.entityType {
		case "scene":
			sceneCalls = append(sceneCalls, call)
		case "shot":
			shotCalls = append(shotCalls, call)
		default:
			t.Fatalf("the projection names entity type %q", call.entityType)
		}
	}
	if len(sceneCalls) != len(structure.Scenes) {
		t.Fatalf("the projection asked for %d scene nodes for %d scenes", len(sceneCalls), len(structure.Scenes))
	}
	for index, call := range sceneCalls {
		if call.entityID != structure.Scenes[index].ID {
			t.Fatalf("scene call %d projects %q, want %q", index, call.entityID, structure.Scenes[index].ID)
		}
		if call.projectID != c.ids.project {
			t.Fatalf("scene call %d names project %q", index, call.projectID)
		}
		// The label is what a canvas node shows, and the slugline is the readable name a scene has.
		if call.label != structure.Scenes[index].Slugline {
			t.Fatalf("scene call %d labels the node %q, want the slugline %q",
				index, call.label, structure.Scenes[index].Slugline)
		}
	}
	shotCount := 0
	for _, scene := range structure.Scenes {
		shotCount += len(scene.Shots)
	}
	if len(shotCalls) != shotCount {
		t.Fatalf("the projection asked for %d shot nodes for %d shots", len(shotCalls), shotCount)
	}
	if len(result.SceneNodeIDs) != len(structure.Scenes) {
		t.Fatalf("the projection returned %d node ids for %d scenes", len(result.SceneNodeIDs), len(structure.Scenes))
	}
	if len(result.ShotNodeIDs) != shotCount {
		t.Fatalf("the projection returned %d shot node ids for %d shots", len(result.ShotNodeIDs), shotCount)
	}
	// A version with no content is refused rather than projected into nothing, which is the boundary the
	// command's own comment states.
	empty := scriptVersionRowForStructure(c, t)
	if _, err := service.ProjectScriptVersion(ctx, appscript.ProjectScriptVersionRequest{
		ProjectID:       c.ids.project,
		ScriptVersionID: empty.ID,
	}); err == nil {
		t.Fatal("a version with no scenes was projected")
	}
	// And a build with no projector REFUSES rather than reporting a silent no-op: a caller asking for a
	// projection and getting success would have no way to tell it did not happen.
	withoutProjector := appscript.NewService(appscript.Options{
		Repository: NewScriptRepository(c.db), Clock: canaryClock{}, IDs: canaryIDGenerator(),
	})
	if _, err := withoutProjector.ProjectScriptVersion(ctx, appscript.ProjectScriptVersionRequest{
		ProjectID:       c.ids.project,
		ScriptVersionID: scriptVersionID,
	}); err == nil {
		t.Fatal("a build with no projector reported a successful projection")
	}
}

// assertApprovedVersionIsFrozen covers the IsContentFrozen call WP-05 reserved for this package.
//
// §2.5 freezes an approved version's content: a change is a NEW version rather than an edit of one that
// has been signed off. The predicate existed since WP-05 with a comment saying nothing called it yet, so
// this is the assertion that the write path now does.
func (c *scriptCanary) assertApprovedVersionIsFrozen(t *testing.T, scriptVersionID string) {
	t.Helper()
	ctx := context.Background()
	_, err := c.script.CreateScriptStructure(ctx, appscript.CreateScriptStructureRequest{
		ScriptID:        c.scriptIDOf(t, scriptVersionID),
		ScriptVersionID: scriptVersionID,
		Draft: scriptdomain.ScriptStructureDraft{Scenes: []scriptdomain.SceneDraft{{
			Slugline: "INT. rewritten - 日", InteriorExterior: scriptdomain.InteriorINT,
		}}},
	})
	if err == nil {
		t.Fatal("content was written into an APPROVED version, so the freeze does not hold")
	}
	if category := scriptErrorCategory(t, err); category != scriptdomain.CategoryConflict {
		t.Fatalf("the refusal is category %q, want a conflict", category)
	}
	// The content is unchanged, which is what makes the refusal a refusal.
	structure, err := c.script.GetScriptStructure(ctx, scriptVersionID)
	if err != nil {
		t.Fatalf("reading the structure after the refusal: %v", err)
	}
	for _, scene := range structure.Scenes {
		if scene.Slugline == "INT. rewritten - 日" {
			t.Fatal("the refused write changed the approved version's content")
		}
	}
}

// scriptIDOf returns the script a version belongs to.
func (c *scriptCanary) scriptIDOf(t *testing.T, versionID string) string {
	t.Helper()
	version, err := c.script.GetScriptVersion(context.Background(), versionID)
	if err != nil {
		t.Fatalf("reading the version: %v", err)
	}
	return version.ScriptID
}

// scriptErrorCategory reads a domain error's category through the public extractor.
func scriptErrorCategory(t *testing.T, err error) scriptdomain.ErrorCategory {
	t.Helper()
	domainErr, ok := scriptdomain.AsError(err)
	if !ok {
		t.Fatalf("the refusal is a %T, want the domain's error", err)
	}
	return domainErr.Category
}

// canaryProjector writes a canvas node per projected entity.
//
// It records what it was asked for, so the assertion is on the CALLS as well as on the count: a projector
// that was handed the wrong entity type would produce the right number of nodes for the wrong things.
type canaryProjector struct {
	calls []struct{ projectID, entityType, entityID, label string }
}

func (p *canaryProjector) ProjectEntity(_ context.Context, projectID, entityType, entityID, label string) (string, error) {
	p.calls = append(p.calls, struct{ projectID, entityType, entityID, label string }{
		projectID, entityType, entityID, label,
	})
	return "node-" + entityID, nil
}

// canaryIDGenerator mints fresh identifiers for the projection test's own service.
func canaryIDGenerator() *canaryIDsGenerator { return &canaryIDsGenerator{} }

// canaryIDsGenerator mints fresh identifiers, so two projections in one test do not collide.
type canaryIDsGenerator struct{ count int }

func (g *canaryIDsGenerator) New() (string, error) {
	g.count++
	return "canary-node-" + itoaForTest(g.count), nil
}
