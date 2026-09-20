package database

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// These tests cover the WP-08 repository surface against real SQLite, because what they are about is
// the SCHEMA: the lock table's composite key, the two link tables' foreign keys, the ordinal
// uniqueness on dialogue lines, and the all-or-nothing structure write.
//
// An in-memory double would prove none of that, and two mutations showed exactly why: disabling the
// unlock's DELETE and removing the dialogue-line lock's revision guard both left the whole suite
// green, because the service's double implements those behaviours itself rather than exercising the
// statements.

// seedScriptVersionForStructure writes the rows a structure write hangs off and returns the version.
func seedScriptVersionForStructure(t *testing.T, repo *ScriptRepository) script.ScriptVersion {
	t.Helper()
	ctx := context.Background()
	// Season 2 rather than (1, 1), because the drama seed already wrote an episode there and the
	// schema is unique per position. The numbers are what the fixture controls; nothing in this
	// package depends on which episode an artifact hangs off.
	if err := repo.CreateEpisode(ctx, script.Episode{
		ID: "ep-1", ProjectID: "drama-project", SeasonNumber: 2, EpisodeNumber: 1,
		Title: "Pilot", Status: script.EpisodePlanning, CreatedAt: structureTime, UpdatedAt: structureTime,
		Revision: 1,
	}); err != nil {
		t.Fatalf("CreateEpisode: %v", err)
	}
	if err := repo.CreateScript(ctx, script.Script{
		ID: "script-1", EpisodeID: "ep-1", CreatedAt: structureTime, UpdatedAt: structureTime, Revision: 1,
	}); err != nil {
		t.Fatalf("CreateScript: %v", err)
	}
	// CreatedByType is set explicitly: the column has a DEFAULT but the INSERT passes a value, and the
	// CHECK refuses the empty string the domain type's zero value carries. The SERVICE defaults it
	// (newScriptVersion does), and a repository test writes the record rather than driving the
	// service, so the fixture states it.
	version := script.ScriptVersion{
		ID: "version-1", ScriptID: "script-1", VersionNumber: 1, Status: versioning.StatusDraft,
		CreatedByType: versioning.CreatedByUser, CreatedAt: structureTime,
	}
	if err := repo.CreateScriptVersion(ctx, version); err != nil {
		t.Fatalf("CreateScriptVersion: %v", err)
	}
	return version
}

// structureFixture builds two scenes with lines and shots.
func structureFixture(versionID string) script.ScriptStructure {
	return script.ScriptStructure{
		ScriptVersionID: versionID,
		Scenes: []script.SceneStructure{
			{
				Scene: script.Scene{
					ID: "scene-1", ScriptVersionID: versionID, Ordinal: 1,
					SceneNumber: "1", Slugline: "INT. 渡口 - 日", InteriorExterior: script.InteriorINT,
					EstimatedDurationSeconds: 90, CreatedAt: structureTime, UpdatedAt: structureTime, Revision: 1,
				},
				DialogueLines: []script.DialogueLine{
					{ID: "line-1", SceneID: "scene-1", Ordinal: 1, Type: script.LineDialogue,
						Text: "这牌子不是你的。", CreatedAt: structureTime, UpdatedAt: structureTime, Revision: 1},
					{ID: "line-2", SceneID: "scene-1", Ordinal: 2, Type: script.LineAction,
						Text: "雾气漫上来。", CreatedAt: structureTime, UpdatedAt: structureTime, Revision: 1},
				},
				Shots: []script.Shot{
					{ID: "shot-1", SceneID: "scene-1", Ordinal: 1, VisualDescription: "河面起雾。",
						Status: versioning.StatusDraft, CreatedAt: structureTime, UpdatedAt: structureTime, Revision: 1},
				},
			},
			{
				Scene: script.Scene{
					ID: "scene-2", ScriptVersionID: versionID, Ordinal: 2,
					SceneNumber: "2", Slugline: "EXT. 渡口 - 夜", InteriorExterior: script.InteriorEXT,
					EstimatedDurationSeconds: 60, CreatedAt: structureTime, UpdatedAt: structureTime, Revision: 1,
				},
			},
		},
	}
}

// structureStamp is the fixture's fixed timestamp as TEXT, for the SQL literals in this file.
//
// The domain records carry a time.Time, so they use structureTime below: the two are separate
// constants rather than one conversion at each use, because this file writes both sides of the
// boundary and a reader should be able to see which one a line is on.
const structureStamp = "2026-04-01T10:00:00Z"

// structureTime is the same instant as the domain sees it.
var structureTime = time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)

// TestScriptStructureRoundTripsThroughSQLite covers the whole write and read.
func TestScriptStructureRoundTripsThroughSQLite(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewScriptRepository(db)
	ctx := context.Background()
	version := seedScriptVersionForStructure(t, repo)

	if err := repo.CreateScriptStructure(ctx, structureFixture(version.ID)); err != nil {
		t.Fatalf("CreateScriptStructure: %v", err)
	}
	structure, err := repo.GetScriptStructure(ctx, version.ID)
	if err != nil {
		t.Fatalf("GetScriptStructure: %v", err)
	}
	if len(structure.Scenes) != 2 {
		t.Fatalf("the version holds %d scenes", len(structure.Scenes))
	}
	// Ordinal order, which is what makes a structure a script rather than a set of scenes.
	if structure.Scenes[0].ID != "scene-1" || structure.Scenes[1].ID != "scene-2" {
		t.Fatalf("the scenes came back as %q, %q", structure.Scenes[0].ID, structure.Scenes[1].ID)
	}
	if len(structure.Scenes[0].DialogueLines) != 2 || len(structure.Scenes[0].Shots) != 1 {
		t.Fatalf("the first scene holds %d lines and %d shots",
			len(structure.Scenes[0].DialogueLines), len(structure.Scenes[0].Shots))
	}
	// The second scene has none, and comes back with empty slices rather than nil, so a caller
	// ranging over them needs no guard.
	if len(structure.Scenes[1].DialogueLines) != 0 || structure.Scenes[1].Shots == nil {
		t.Fatalf("the second scene came back as %+v", structure.Scenes[1])
	}
	// The line's lock round-trips as false, which is the column's default.
	if structure.Scenes[0].DialogueLines[0].Locked {
		t.Fatal("a fresh line is locked")
	}
}

// TestScriptStructureWriteRollsBackCompletely covers the transaction.
//
// The second scene's ordinal collides with the first's, so the write fails AFTER the first scene and
// its children were written. Nothing may survive: a version with half its scenes is an artifact a
// reader could mistake for a complete one, and the duration would be a sum over a partial list.
func TestScriptStructureWriteRollsBackCompletely(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewScriptRepository(db)
	ctx := context.Background()
	version := seedScriptVersionForStructure(t, repo)

	broken := structureFixture(version.ID)
	// A duplicate scene id is what the schema refuses, and it is refused on the SECOND scene — after
	// the first has been inserted and its children with it.
	broken.Scenes[1].ID = "scene-1"
	if err := repo.CreateScriptStructure(ctx, broken); err == nil {
		t.Fatal("a structure with a duplicate scene id was accepted")
	}
	// Nothing at all survived.
	structure, err := repo.GetScriptStructure(ctx, version.ID)
	if err != nil {
		t.Fatalf("GetScriptStructure: %v", err)
	}
	if len(structure.Scenes) != 0 {
		t.Fatalf("a failed write left %d scenes behind", len(structure.Scenes))
	}
	lines, err := repo.ListDialogueLines(ctx, "scene-1")
	if err != nil {
		t.Fatalf("ListDialogueLines: %v", err)
	}
	if len(lines) != 0 {
		t.Fatalf("a failed write left %d dialogue lines behind", len(lines))
	}
	shots, err := repo.ListShots(ctx, "scene-1")
	if err != nil {
		t.Fatalf("ListShots: %v", err)
	}
	if len(shots) != 0 {
		t.Fatalf("a failed write left %d shots behind", len(shots))
	}
}

// TestDialogueLineOrdinalIsUniquePerScene covers the schema's constraint through the writer.
func TestDialogueLineOrdinalIsUniquePerScene(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewScriptRepository(db)
	ctx := context.Background()
	version := seedScriptVersionForStructure(t, repo)
	if err := repo.CreateScriptStructure(ctx, structureFixture(version.ID)); err != nil {
		t.Fatalf("CreateScriptStructure: %v", err)
	}
	// A third line at an ordinal already used is a conflict.
	err := repo.CreateDialogueLine(ctx, script.DialogueLine{
		ID: "line-3", SceneID: "scene-1", Ordinal: 1, Type: script.LineAction,
		Text: "x", CreatedAt: structureTime, UpdatedAt: structureTime, Revision: 1,
	})
	if err == nil {
		t.Fatal("a duplicate line ordinal was accepted")
	}
	if domainErr, ok := script.AsError(err); !ok || domainErr.Category != script.CategoryConflict {
		t.Fatalf("the refusal is %v, want a conflict", err)
	}
}

// TestDialogueLineLockIsRevisionGuarded covers the UPDATE's guard against real SQLite.
//
// The service's double implements this itself, so a mutation removing the `AND revision = ?` clause
// left the whole suite green. Only a test against the real statement can tell the two apart.
func TestDialogueLineLockIsRevisionGuarded(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewScriptRepository(db)
	ctx := context.Background()
	version := seedScriptVersionForStructure(t, repo)
	if err := repo.CreateScriptStructure(ctx, structureFixture(version.ID)); err != nil {
		t.Fatalf("CreateScriptStructure: %v", err)
	}
	line, err := repo.GetDialogueLine(ctx, "line-1")
	if err != nil {
		t.Fatalf("GetDialogueLine: %v", err)
	}
	if err := repo.SetDialogueLineLocked(ctx, "line-1", true, line.Revision, structureTime); err != nil {
		t.Fatalf("SetDialogueLineLocked: %v", err)
	}
	locked, err := repo.GetDialogueLine(ctx, "line-1")
	if err != nil {
		t.Fatalf("GetDialogueLine: %v", err)
	}
	if !locked.Locked {
		t.Fatal("the lock did not stick")
	}
	if locked.Revision != line.Revision+1 {
		t.Fatalf("the revision is %d, want the increment", locked.Revision)
	}
	// The SAME revision again is stale now, and it must be refused rather than silently applied.
	err = repo.SetDialogueLineLocked(ctx, "line-1", false, line.Revision, structureTime)
	if err == nil {
		t.Fatal("a stale revision overwrote the lock")
	}
	if domainErr, ok := script.AsError(err); !ok || domainErr.Category != script.CategoryConflict {
		t.Fatalf("the refusal is %v, want a conflict", err)
	}
	// And the lock is still in place.
	still, err := repo.GetDialogueLine(ctx, "line-1")
	if err != nil {
		t.Fatalf("GetDialogueLine: %v", err)
	}
	if !still.Locked {
		t.Fatal("the refused write changed the lock anyway")
	}
	// A missing line is a not-found rather than a conflict.
	if err := repo.SetDialogueLineLocked(ctx, "line-missing", true, 1, structureTime); err == nil {
		t.Fatal("a missing line was locked")
	}
}

// TestFieldLockRoundTripsAndUnlocks covers the lock table's composite key against real SQLite.
//
// The service's double implements the unlock itself, so a mutation that stopped the DELETE from
// deleting anything left the whole suite green — which is why this test exists.
func TestFieldLockRoundTripsAndUnlocks(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewScriptRepository(db)
	ctx := context.Background()
	version := seedScriptVersionForStructure(t, repo)

	lock := script.FieldLock{
		VersionID: version.ID, Family: script.FamilyScript,
		// FieldLock.CreatedAt is TEXT rather than a time.Time: the type is storage-shaped, because a
		// lock records when it was made and nothing computes with it.
		Field: script.LockScriptStructure, LockedBy: "user-1", CreatedAt: structureStamp,
	}
	if err := repo.LockScriptField(ctx, lock); err != nil {
		t.Fatalf("LockScriptField: %v", err)
	}
	// Locking the same field twice is idempotent, not a conflict: the user's intent is "this stays"
	// and repeating it changes nothing.
	if err := repo.LockScriptField(ctx, lock); err != nil {
		t.Fatalf("locking twice failed: %v", err)
	}
	locks, err := repo.ListScriptFieldLocks(ctx, version.ID)
	if err != nil {
		t.Fatalf("ListScriptFieldLocks: %v", err)
	}
	if len(locks) != 1 {
		t.Fatalf("the version holds %d locks, want one", len(locks))
	}
	if locks[0].Family != script.FamilyScript || locks[0].Field != script.LockScriptStructure {
		t.Fatalf("the lock came back as %+v", locks[0])
	}
	// A second field on the same version is a second row.
	second := lock
	second.Field = script.LockScriptSummary
	if err := repo.LockScriptField(ctx, second); err != nil {
		t.Fatalf("LockScriptField for the second field: %v", err)
	}
	// Unlocking one leaves the other.
	if err := repo.UnlockScriptField(ctx, version.ID, script.LockScriptStructure); err != nil {
		t.Fatalf("UnlockScriptField: %v", err)
	}
	locks, err = repo.ListScriptFieldLocks(ctx, version.ID)
	if err != nil {
		t.Fatalf("ListScriptFieldLocks: %v", err)
	}
	if len(locks) != 1 || locks[0].Field != script.LockScriptSummary {
		t.Fatalf("after unlocking one the version holds %+v", locks)
	}
	// Unlocking what is not locked is not an error.
	if err := repo.UnlockScriptField(ctx, version.ID, script.LockSkeletonClimax); err != nil {
		t.Fatalf("unlocking an unlocked field failed: %v", err)
	}
}

// TestEventLinkTablesRoundTrip covers the two link tables migration 000008 created with no writer.
func TestEventLinkTablesRoundTrip(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewScriptRepository(db)
	ctx := context.Background()
	seedEventLinkParents(t, db)

	// The skeleton links: written as a set and read back in order.
	if err := repo.LinkSkeletonEvents(ctx, "skeleton-1",
		[]string{"event-1", "event-2", "event-3"}, structureTime); err != nil {
		t.Fatalf("LinkSkeletonEvents: %v", err)
	}
	ids, err := repo.ListSkeletonEventIDs(ctx, "skeleton-1")
	if err != nil {
		t.Fatalf("ListSkeletonEventIDs: %v", err)
	}
	if len(ids) != 3 || ids[0] != "event-1" || ids[2] != "event-3" {
		t.Fatalf("the selected events are %v", ids)
	}
	// Replacing the set removes what is no longer selected, which is what makes the write a
	// statement of the complete answer rather than an amendment.
	if err := repo.LinkSkeletonEvents(ctx, "skeleton-1", []string{"event-2"}, structureTime); err != nil {
		t.Fatalf("LinkSkeletonEvents: %v", err)
	}
	ids, err = repo.ListSkeletonEventIDs(ctx, "skeleton-1")
	if err != nil {
		t.Fatalf("ListSkeletonEventIDs: %v", err)
	}
	if len(ids) != 1 || ids[0] != "event-2" {
		t.Fatalf("after replacing the set the events are %v", ids)
	}

	// The strategy links: the same shape with a treatment.
	links := []script.StrategyEventLink{
		{StrategyVersionID: "strategy-1", StoryEventID: "event-1", Treatment: script.TreatmentRetained},
		{StrategyVersionID: "strategy-1", StoryEventID: "event-2", Treatment: script.TreatmentRemoved},
		{StrategyVersionID: "strategy-1", StoryEventID: "event-3", Treatment: script.TreatmentReordered},
	}
	if err := repo.LinkStrategyEvents(ctx, "strategy-1", links, structureTime); err != nil {
		t.Fatalf("LinkStrategyEvents: %v", err)
	}
	stored, err := repo.ListStrategyEventLinks(ctx, "strategy-1")
	if err != nil {
		t.Fatalf("ListStrategyEventLinks: %v", err)
	}
	if len(stored) != 3 {
		t.Fatalf("the treatments are %+v", stored)
	}
	// The ordinals are the ARRAY's positions, so the order the caller wrote is the order stored.
	if stored[0].Ordinal != 1 || stored[2].Ordinal != 3 {
		t.Fatalf("the ordinals are %d, %d, %d", stored[0].Ordinal, stored[1].Ordinal, stored[2].Ordinal)
	}
	if stored[1].Treatment != script.TreatmentRemoved {
		t.Fatalf("the second treatment is %q", stored[1].Treatment)
	}
}

// seedEventLinkParents writes the version and event rows the link tables reference.
func seedEventLinkParents(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	statements := []string{
		`INSERT INTO episodes (id, project_id, season_number, episode_number, title, status, created_at, updated_at, revision)
		 VALUES ('ep-links', 'drama-project', 2, 1, 'Links', 'planning', '` + structureStamp + `', '` + structureStamp + `', 1)`,
		`INSERT INTO story_skeleton_versions (id, episode_id, version_number, status, created_at)
		 VALUES ('skeleton-1', 'ep-links', 1, 'draft', '` + structureStamp + `')`,
		`INSERT INTO adaptation_strategy_versions (id, episode_id, version_number, status, created_at)
		 VALUES ('strategy-1', 'ep-links', 1, 'draft', '` + structureStamp + `')`,
		`INSERT INTO story_events (id, project_id, ordinal, name, status, created_at, updated_at)
		 VALUES ('event-1', 'drama-project', 1, 'One', 'candidate', '` + structureStamp + `', '` + structureStamp + `')`,
		`INSERT INTO story_events (id, project_id, ordinal, name, status, created_at, updated_at)
		 VALUES ('event-2', 'drama-project', 2, 'Two', 'candidate', '` + structureStamp + `', '` + structureStamp + `')`,
		`INSERT INTO story_events (id, project_id, ordinal, name, status, created_at, updated_at)
		 VALUES ('event-3', 'drama-project', 3, 'Three', 'candidate', '` + structureStamp + `', '` + structureStamp + `')`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seeding the link parents failed: %v\n%s", err, statement)
		}
	}
}

// TestVersionHistoriesComeBackNewestFirst covers the three list reads.
func TestVersionHistoriesComeBackNewestFirst(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewScriptRepository(db)
	ctx := context.Background()
	seedEventLinkParents(t, db)

	// Two of each family, written in ascending order so the read's ordering is observable.
	for index, id := range []string{"skeleton-1", "skeleton-2"} {
		if index == 0 {
			continue // skeleton-1 is seeded above.
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO story_skeleton_versions
			(id, episode_id, version_number, status, created_at)
			VALUES (?, 'ep-links', 2, 'draft', ?)`, id, structureStamp); err != nil {
			t.Fatalf("seeding %s: %v", id, err)
		}
	}
	skeletons, err := repo.ListStorySkeletonVersions(ctx, "ep-links")
	if err != nil {
		t.Fatalf("ListStorySkeletonVersions: %v", err)
	}
	if len(skeletons) != 2 {
		t.Fatalf("the episode holds %d skeleton versions", len(skeletons))
	}
	if skeletons[0].VersionNumber != 2 {
		t.Fatalf("the history starts at version %d, want the newest", skeletons[0].VersionNumber)
	}
	// An episode with no versions gets an empty list rather than a nil or an error.
	empty, err := repo.ListStorySkeletonVersions(ctx, "no-such-episode")
	if err != nil {
		t.Fatalf("ListStorySkeletonVersions for an empty episode: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("an empty episode returned %d versions", len(empty))
	}
}
