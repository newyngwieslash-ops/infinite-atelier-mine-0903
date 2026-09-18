package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// openWP05ScriptRepo opens the full WP-05 migration set over a temporary
// database and seeds the project and episode the script rows hang off, so the
// constraints under test are the ones a user's database has.
func openWP05ScriptRepo(t *testing.T) (*ScriptRepository, *sql.DB) {
	t.Helper()
	handle, err := open(context.Background(), filepath.Join(t.TempDir(), "app.db"),
		filepath.Join(t.TempDir(), "snapshots"), wp05Migrations(t), fixedClock())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := handle.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if handle.Mode() != ModeReady {
		t.Fatalf("database not ready: %v", handle.Err())
	}
	db := handle.SQL()
	seedWP05Parents(t, db)
	return NewScriptRepository(db), db
}

// sampleEpisode builds an episode with every field the table stores.
func sampleEpisode(t *testing.T) script.Episode {
	t.Helper()
	now := fixedClock()()
	return script.Episode{
		ID:                    "episode-2",
		ProjectID:             "project-1",
		SeasonNumber:          1,
		EpisodeNumber:         2,
		Title:                 "The Harbour",
		Status:                script.EpisodePlanning,
		SourceChapterStartID:  "chapter-1",
		SourceChapterEndID:    "chapter-4",
		TargetDurationSeconds: 120,
		CreatedAt:             now,
		UpdatedAt:             now,
		Revision:              1,
	}
}

// TestScriptRepositoryEpisodeRoundTrip covers create, read, list, the business
// key count and the revision guard.
func TestScriptRepositoryEpisodeRoundTrip(t *testing.T) {
	repo, db := openWP05ScriptRepo(t)
	ctx := context.Background()
	record := sampleEpisode(t)

	if err := repo.CreateEpisode(ctx, record); err != nil {
		t.Fatalf("CreateEpisode: %v", err)
	}
	loaded, err := repo.GetEpisode(ctx, record.ID)
	if err != nil {
		t.Fatalf("GetEpisode: %v", err)
	}
	if loaded.SeasonNumber != 1 || loaded.EpisodeNumber != 2 || loaded.Title != "The Harbour" {
		t.Fatalf("round trip changed the row: %+v", loaded)
	}
	if loaded.SourceChapterStartID != "chapter-1" || loaded.SourceChapterEndID != "chapter-4" {
		t.Fatalf("chapter bounds round-tripped wrong: %+v", loaded)
	}
	if loaded.TargetDurationSeconds != 120 || loaded.Status != script.EpisodePlanning || loaded.Revision != 1 {
		t.Fatalf("round trip changed the row: %+v", loaded)
	}
	if !loaded.CreatedAt.Equal(record.CreatedAt) {
		t.Fatalf("created_at = %s, want %s", loaded.CreatedAt, record.CreatedAt)
	}
	if loaded.Key() != "S1E2" {
		t.Fatalf("key = %q, want S1E2", loaded.Key())
	}

	// The seeded episode plus this one, in season and episode order.
	list, err := repo.ListEpisodes(ctx, "project-1")
	if err != nil {
		t.Fatalf("ListEpisodes: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListEpisodes returned %d rows, want 2", len(list))
	}
	if list[0].EpisodeNumber > list[1].EpisodeNumber {
		t.Fatalf("episodes are not in order: %d then %d", list[0].EpisodeNumber, list[1].EpisodeNumber)
	}

	taken, err := repo.CountEpisodesAtPosition(ctx, "project-1", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if taken != 1 {
		t.Fatalf("CountEpisodesAtPosition = %d, want 1", taken)
	}
	if taken, err = repo.CountEpisodesAtPosition(ctx, "project-1", 2, 1); err != nil {
		t.Fatal(err)
	} else if taken != 0 {
		t.Fatalf("an unused position reports %d, want 0", taken)
	}

	// The revision guard: the first update lands, the replay does not.
	record.Status = script.EpisodeWriting
	record.UpdatedAt = fixedClock()()
	if err := repo.UpdateEpisode(ctx, record, 1); err != nil {
		t.Fatalf("UpdateEpisode: %v", err)
	}
	after, err := repo.GetEpisode(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != script.EpisodeWriting || after.Revision != 2 {
		t.Fatalf("after one update: %+v", after)
	}
	stale := after
	stale.Status = script.EpisodeCompleted
	if err := repo.UpdateEpisode(ctx, stale, 1); err == nil {
		t.Fatal("a stale revision was accepted")
	} else if domainErr, ok := script.AsError(err); !ok || domainErr.Category != script.CategoryConflict {
		t.Fatalf("expected a conflict, got %v", err)
	}
	reRead, err := repo.GetEpisode(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reRead.Status != script.EpisodeWriting {
		t.Fatalf("a rejected update wrote the row: %q", reRead.Status)
	}

	// The business key is unique per project, so the same position is a conflict
	// even though the identifier is new.
	duplicate := sampleEpisode(t)
	duplicate.ID = "episode-3"
	if err := repo.CreateEpisode(ctx, duplicate); err == nil {
		t.Fatal("a duplicate (project, season, episode) triple was accepted")
	} else if domainErr, ok := script.AsError(err); !ok || domainErr.Category != script.CategoryConflict {
		t.Fatalf("expected a conflict, got %v", err)
	}
	// The schema's foreign key rejects an episode in a project that does not
	// exist, which is what makes the project reference real rather than a scope
	// value.
	orphan := sampleEpisode(t)
	orphan.ID = "episode-orphan"
	orphan.ProjectID = "missing-project"
	if err := repo.CreateEpisode(ctx, orphan); err == nil {
		t.Fatal("an episode was created in a project that does not exist")
	}

	foreignKeysClean(t, db)
}

// TestScriptRepositoryVersionStagesRoundTrip covers the story skeleton and the
// adaptation strategy, including the version number those rows are stamped
// with and the maximum each family reports.
func TestScriptRepositoryVersionStagesRoundTrip(t *testing.T) {
	repo, db := openWP05ScriptRepo(t)
	ctx := context.Background()
	now := fixedClock()()

	skeleton := script.StorySkeletonVersion{
		ID: "skeleton-1", EpisodeID: "episode-1", VersionNumber: 1,
		Status: versioning.StatusDraft, BasedOnVersionID: "skeleton-0",
		OpeningHook: "A letter arrives.", CoreConflict: "She must answer it.",
		TurningPointsJSON: `["the letter","the refusal"]`, Climax: "She answers.",
		EndingHook: "The reply is not hers.", EstimatedDurationSeconds: 90,
		SourceAgentRunID: "run-1", CreatedByType: versioning.CreatedByAgent,
		CreatedByID: "agent-1", ChangeReason: "first pass",
		LegacyMetadata: `{"note":"kept"}`, CreatedAt: now,
	}
	if err := repo.CreateStorySkeletonVersion(ctx, skeleton); err != nil {
		t.Fatalf("CreateStorySkeletonVersion: %v", err)
	}
	loadedSkeleton, err := repo.GetStorySkeletonVersion(ctx, skeleton.ID)
	if err != nil {
		t.Fatalf("GetStorySkeletonVersion: %v", err)
	}
	if loadedSkeleton.OpeningHook != skeleton.OpeningHook || loadedSkeleton.CoreConflict != skeleton.CoreConflict {
		t.Fatalf("skeleton hooks round-tripped wrong: %+v", loadedSkeleton)
	}
	if loadedSkeleton.TurningPointsJSON != skeleton.TurningPointsJSON || loadedSkeleton.Climax != skeleton.Climax {
		t.Fatalf("skeleton structure round-tripped wrong: %+v", loadedSkeleton)
	}
	if loadedSkeleton.EndingHook != skeleton.EndingHook || loadedSkeleton.EstimatedDurationSeconds != 90 {
		t.Fatalf("skeleton fields round-tripped wrong: %+v", loadedSkeleton)
	}
	if loadedSkeleton.Status != versioning.StatusDraft || loadedSkeleton.CreatedByType != versioning.CreatedByAgent {
		t.Fatalf("skeleton vocabulary round-tripped wrong: %+v", loadedSkeleton)
	}
	if loadedSkeleton.BasedOnVersionID != "skeleton-0" || loadedSkeleton.CreatedByID != "agent-1" ||
		loadedSkeleton.ChangeReason != "first pass" || loadedSkeleton.LegacyMetadata != `{"note":"kept"}` {
		t.Fatalf("skeleton provenance round-tripped wrong: %+v", loadedSkeleton)
	}

	secondSkeleton := skeleton
	secondSkeleton.ID = "skeleton-2"
	secondSkeleton.VersionNumber = 2
	if err := repo.CreateStorySkeletonVersion(ctx, secondSkeleton); err != nil {
		t.Fatal(err)
	}
	highest, err := repo.MaxStorySkeletonVersionNumber(ctx, "episode-1")
	if err != nil {
		t.Fatal(err)
	}
	if highest != 2 {
		t.Fatalf("MaxStorySkeletonVersionNumber = %d, want 2", highest)
	}
	if highest, err = repo.MaxStorySkeletonVersionNumber(ctx, "episode-nothing"); err != nil {
		t.Fatal(err)
	} else if highest != 0 {
		t.Fatalf("an episode with no skeletons reports %d, want 0", highest)
	}
	// The (episode, version number) pair is unique.
	clash := skeleton
	clash.ID = "skeleton-3"
	if err := repo.CreateStorySkeletonVersion(ctx, clash); err == nil {
		t.Fatal("a reused skeleton version number was accepted")
	} else if domainErr, ok := script.AsError(err); !ok || domainErr.Category != script.CategoryConflict {
		t.Fatalf("expected a conflict, got %v", err)
	}

	strategy := script.AdaptationStrategyVersion{
		ID: "strategy-1", EpisodeID: "episode-1", VersionNumber: 1,
		Status: versioning.StatusDraft, BasedOnVersionID: "strategy-0",
		StrategySummary:       "Keep the letter, merge the neighbours.",
		AdaptationMode:        script.AdaptationAggressive,
		MergedEventGroupsJSON: `[["event-1","event-2"]]`, OriginalAdditions: "The harbour bell.",
		Rationale: "The source repeats itself.", Risks: "Two beats may blur together.",
		SourceAgentRunID: "run-2", CreatedByType: versioning.CreatedByUser,
		CreatedByID: "user-1", ChangeReason: "tighten the middle",
		LegacyMetadata: `{"kept":true}`, CreatedAt: now,
	}
	if err := repo.CreateAdaptationStrategyVersion(ctx, strategy); err != nil {
		t.Fatalf("CreateAdaptationStrategyVersion: %v", err)
	}
	loadedStrategy, err := repo.GetAdaptationStrategyVersion(ctx, strategy.ID)
	if err != nil {
		t.Fatalf("GetAdaptationStrategyVersion: %v", err)
	}
	if loadedStrategy.StrategySummary != strategy.StrategySummary || loadedStrategy.AdaptationMode != script.AdaptationAggressive {
		t.Fatalf("strategy round-tripped wrong: %+v", loadedStrategy)
	}
	if loadedStrategy.MergedEventGroupsJSON != strategy.MergedEventGroupsJSON ||
		loadedStrategy.OriginalAdditions != strategy.OriginalAdditions {
		t.Fatalf("strategy additions round-tripped wrong: %+v", loadedStrategy)
	}
	if loadedStrategy.Rationale != strategy.Rationale || loadedStrategy.Risks != strategy.Risks {
		t.Fatalf("strategy reasoning round-tripped wrong: %+v", loadedStrategy)
	}
	if loadedStrategy.Status != versioning.StatusDraft || loadedStrategy.CreatedByType != versioning.CreatedByUser {
		t.Fatalf("strategy vocabulary round-tripped wrong: %+v", loadedStrategy)
	}
	highest, err = repo.MaxAdaptationStrategyVersionNumber(ctx, "episode-1")
	if err != nil {
		t.Fatal(err)
	}
	if highest != 1 {
		t.Fatalf("MaxAdaptationStrategyVersionNumber = %d, want 1", highest)
	}

	foreignKeysClean(t, db)
}

// TestScriptRepositoryScriptRoundTrip covers the stable identity, the version
// numbering, the approved-version lookup and the two unique constraints.
func TestScriptRepositoryScriptRoundTrip(t *testing.T) {
	repo, db := openWP05ScriptRepo(t)
	ctx := context.Background()
	now := fixedClock()()

	// The fixture already holds script-1 for episode-1, so this is a read of a
	// stored row rather than a create.
	existing, err := repo.GetScriptByEpisode(ctx, "episode-1")
	if err != nil {
		t.Fatalf("GetScriptByEpisode: %v", err)
	}
	if existing.ID != "script-1" || existing.Revision != 1 || existing.CurrentVersionID != "" {
		t.Fatalf("seeded script read back as %+v", existing)
	}
	if _, err := repo.GetScriptByEpisode(ctx, "episode-2"); err == nil {
		t.Fatal("an episode with no script reported one")
	} else if domainErr, ok := script.AsError(err); !ok || domainErr.Category != script.CategoryNotFound {
		t.Fatalf("expected not_found, got %v", err)
	}

	// A second script for the same episode is refused by the unique constraint
	// on episode_id, which is what makes EnsureScript's race resolvable.
	duplicate := script.Script{ID: "script-duplicate", EpisodeID: "episode-1", CreatedAt: now, UpdatedAt: now, Revision: 1}
	if err := repo.CreateScript(ctx, duplicate); err == nil {
		t.Fatal("a second script was stored for one episode")
	} else if domainErr, ok := script.AsError(err); !ok || domainErr.Category != script.CategoryConflict {
		t.Fatalf("expected a conflict, got %v", err)
	}

	version := script.ScriptVersion{
		ID: "script-version-2", ScriptID: existing.ID, VersionNumber: 2,
		Status: versioning.StatusDraft, BasedOnVersionID: "script-version-1",
		StorySkeletonVersionID: "skeleton-1", AdaptationStrategyVersionID: "strategy-1",
		EstimatedDurationSeconds: 95, Summary: "Second pass.",
		SourceAgentRunID: "run-3", CreatedByType: versioning.CreatedByAgent,
		CreatedByID: "agent-2", ChangeReason: "the middle sagged",
		LegacyMetadata: `{"kept":1}`, CreatedAt: now,
	}
	if err := repo.CreateScriptVersion(ctx, version); err != nil {
		t.Fatalf("CreateScriptVersion: %v", err)
	}
	loaded, err := repo.GetScriptVersion(ctx, version.ID)
	if err != nil {
		t.Fatalf("GetScriptVersion: %v", err)
	}
	if loaded.VersionNumber != 2 || loaded.ScriptID != existing.ID {
		t.Fatalf("script version round-tripped wrong: %+v", loaded)
	}
	if loaded.StorySkeletonVersionID != "skeleton-1" || loaded.AdaptationStrategyVersionID != "strategy-1" {
		t.Fatalf("upstream references round-tripped wrong: %+v", loaded)
	}
	if loaded.Summary != "Second pass." || loaded.EstimatedDurationSeconds != 95 {
		t.Fatalf("script version content round-tripped wrong: %+v", loaded)
	}
	if loaded.Status != versioning.StatusDraft || loaded.CreatedByType != versioning.CreatedByAgent {
		t.Fatalf("script version vocabulary round-tripped wrong: %+v", loaded)
	}
	if !loaded.CreatedAt.Equal(now) {
		t.Fatalf("created_at = %s, want %s", loaded.CreatedAt, now)
	}

	highest, err := repo.MaxScriptVersionNumber(ctx, existing.ID)
	if err != nil {
		t.Fatal(err)
	}
	if highest != 2 {
		t.Fatalf("MaxScriptVersionNumber = %d, want 2", highest)
	}
	clash := version
	clash.ID = "script-version-3"
	if err := repo.CreateScriptVersion(ctx, clash); err == nil {
		t.Fatal("a reused script version number was accepted")
	} else if domainErr, ok := script.AsError(err); !ok || domainErr.Category != script.CategoryConflict {
		t.Fatalf("expected a conflict, got %v", err)
	}

	// No version is approved yet, which is an answer rather than an error.
	if _, approved, err := repo.CurrentApprovedScriptVersion(ctx, existing.ID); err != nil {
		t.Fatal(err)
	} else if approved {
		t.Fatal("a script with no approval reported one")
	}

	foreignKeysClean(t, db)
}

// TestScriptRepositoryApproveAndSupersedeIsAtomic is DOMAIN_MODEL §2.5's
// supersede rule against the real schema: approving a second version leaves
// exactly one approved row, and the first is kept as superseded.
func TestScriptRepositoryApproveAndSupersedeIsAtomic(t *testing.T) {
	repo, db := openWP05ScriptRepo(t)
	ctx := context.Background()
	now := fixedClock()()
	const scriptID = "script-1"

	first, err := repo.GetScriptVersion(ctx, "script-version-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.ApproveScriptVersion(ctx, first, ""); err != nil {
		t.Fatalf("approving the first version: %v", err)
	}
	if got := approvedCount(t, db, scriptID); got != 1 {
		t.Fatalf("approved count = %d, want 1 after the first approval", got)
	}
	current, approved, err := repo.CurrentApprovedScriptVersion(ctx, scriptID)
	if err != nil {
		t.Fatal(err)
	}
	if !approved || current.ID != first.ID {
		t.Fatalf("the approved version is %+v", current)
	}

	second := script.ScriptVersion{
		ID: "script-version-2", ScriptID: scriptID, VersionNumber: 2,
		Status: versioning.StatusDraft, StorySkeletonVersionID: "skeleton-1",
		AdaptationStrategyVersionID: "strategy-1", CreatedByType: versioning.CreatedByUser,
		CreatedAt: now,
	}
	if err := repo.CreateScriptVersion(ctx, second); err != nil {
		t.Fatal(err)
	}
	if err := repo.ApproveScriptVersion(ctx, second, first.ID); err != nil {
		t.Fatalf("approving the second version: %v", err)
	}
	// Exactly one approved row, and it is the new one.
	if got := approvedCount(t, db, scriptID); got != 1 {
		t.Fatalf("approved count = %d, want exactly 1", got)
	}
	current, approved, err = repo.CurrentApprovedScriptVersion(ctx, scriptID)
	if err != nil {
		t.Fatal(err)
	}
	if !approved || current.ID != second.ID {
		t.Fatalf("the approved version is %+v, want %s", current, second.ID)
	}
	superseded, err := repo.GetScriptVersion(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if superseded.Status != versioning.StatusSuperseded {
		t.Fatalf("the previous approval is %q, want superseded", superseded.Status)
	}

	// A second approval of the same row against the status it no longer holds is
	// refused, and it changes nothing: the guard is the compare-and-swap.
	staleSecond := second
	staleSecond.Status = versioning.StatusUnderReview
	if err := repo.ApproveScriptVersion(ctx, staleSecond, first.ID); err == nil {
		t.Fatal("a version whose status moved was approved anyway")
	} else if domainErr, ok := script.AsError(err); !ok || domainErr.Category != script.CategoryConflict {
		t.Fatalf("expected a conflict, got %v", err)
	}
	if got := approvedCount(t, db, scriptID); got != 1 {
		t.Fatalf("a refused approval left %d approved rows", got)
	}
	// The supersede half did not run either, so the row it would have touched is
	// still superseded rather than doubly written.
	reRead, err := repo.GetScriptVersion(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reRead.Status != versioning.StatusSuperseded {
		t.Fatalf("a refused approval rewrote the previous version: %q", reRead.Status)
	}

	// A supersede that names a version that is not approved is refused before
	// the approval lands, so the pair really is one transaction.
	third := script.ScriptVersion{
		ID: "script-version-3", ScriptID: scriptID, VersionNumber: 3,
		Status: versioning.StatusDraft, StorySkeletonVersionID: "skeleton-1",
		AdaptationStrategyVersionID: "strategy-1", CreatedByType: versioning.CreatedByUser,
		CreatedAt: now,
	}
	if err := repo.CreateScriptVersion(ctx, third); err != nil {
		t.Fatal(err)
	}
	if err := repo.ApproveScriptVersion(ctx, third, first.ID); err == nil {
		t.Fatal("a version that is not the current approval was superseded")
	} else if domainErr, ok := script.AsError(err); !ok || domainErr.Category != script.CategoryConflict {
		t.Fatalf("expected a conflict, got %v", err)
	}
	if got := approvedCount(t, db, scriptID); got != 1 {
		t.Fatalf("the failed transaction left %d approved rows", got)
	}
	if current, _, err := repo.CurrentApprovedScriptVersion(ctx, scriptID); err != nil {
		t.Fatal(err)
	} else if current.ID != second.ID {
		t.Fatalf("the failed transaction changed the approval to %q", current.ID)
	}

	foreignKeysClean(t, db)
}

// approvedCount reads the number of approved rows for one script. It is the
// check the partial unique index makes meaningful: the index allows at most one,
// and this proves the writer leaves exactly one.
func approvedCount(t *testing.T, db *sql.DB, scriptID string) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM script_versions WHERE script_id = ? AND status = 'approved'`,
		scriptID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// TestScriptRepositorySceneAndShotRoundTrip covers both creates, their ordering
// and the two ordinal constraints.
func TestScriptRepositorySceneAndShotRoundTrip(t *testing.T) {
	repo, db := openWP05ScriptRepo(t)
	ctx := context.Background()
	now := fixedClock()()

	scene := script.Scene{
		ID: "scene-1", ScriptVersionID: "script-version-1", Ordinal: 1, SceneNumber: "1",
		Slugline: "INT. HARBOUR - DAY", InteriorExterior: script.InteriorINT,
		LocationEntityID: "entity-1", TimeOfDay: "day", Summary: "She arrives.",
		DramaticGoal: "Meet the sender.", EstimatedDurationSeconds: 45,
		SourceStoryEventID: "event-1", IsOriginalAdaptation: true,
		CreatedAt: now, UpdatedAt: now, Revision: 1,
	}
	if err := repo.CreateScene(ctx, scene); err != nil {
		t.Fatalf("CreateScene: %v", err)
	}
	loadedScene, err := repo.GetScene(ctx, scene.ID)
	if err != nil {
		t.Fatalf("GetScene: %v", err)
	}
	if loadedScene.Slugline != scene.Slugline || loadedScene.InteriorExterior != script.InteriorINT {
		t.Fatalf("scene round-tripped wrong: %+v", loadedScene)
	}
	if loadedScene.LocationEntityID != "entity-1" || loadedScene.TimeOfDay != "day" {
		t.Fatalf("scene placement round-tripped wrong: %+v", loadedScene)
	}
	if loadedScene.Summary != scene.Summary || loadedScene.DramaticGoal != scene.DramaticGoal {
		t.Fatalf("scene prose round-tripped wrong: %+v", loadedScene)
	}
	if !loadedScene.IsOriginalAdaptation {
		t.Fatal("the original-adaptation flag did not survive the round trip")
	}
	if loadedScene.SourceStoryEventID != "event-1" || loadedScene.EstimatedDurationSeconds != 45 {
		t.Fatalf("scene provenance round-tripped wrong: %+v", loadedScene)
	}

	// A faithful scene stays unflagged, so the flag is a real distinction rather
	// than a column that always reads true.
	second := scene
	second.ID = "scene-2"
	second.Ordinal = 2
	second.IsOriginalAdaptation = false
	second.SourceStoryEventID = ""
	if err := repo.CreateScene(ctx, second); err != nil {
		t.Fatal(err)
	}
	loadedSecond, err := repo.GetScene(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loadedSecond.IsOriginalAdaptation {
		t.Fatal("a faithful scene read back as an original adaptation")
	}
	scenes, err := repo.ListScenes(ctx, "script-version-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(scenes) != 2 || scenes[0].Ordinal != 1 || scenes[1].Ordinal != 2 {
		t.Fatalf("ListScenes returned %+v", scenes)
	}
	taken, err := repo.CountScenesAtOrdinal(ctx, "script-version-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if taken != 1 {
		t.Fatalf("CountScenesAtOrdinal = %d, want 1", taken)
	}
	clashingScene := scene
	clashingScene.ID = "scene-3"
	if err := repo.CreateScene(ctx, clashingScene); err == nil {
		t.Fatal("a reused scene ordinal was accepted")
	} else if domainErr, ok := script.AsError(err); !ok || domainErr.Category != script.CategoryConflict {
		t.Fatalf("expected a conflict, got %v", err)
	}

	shot := script.Shot{
		ID: "shot-1", SceneID: scene.ID, Ordinal: 1, ShotNumber: "1", ShotSize: "wide",
		CameraAngle: "eye level", CameraMovement: "slow push",
		EstimatedDurationSeconds: 8, VisualDescription: "The harbour at dawn.",
		ActionDescription: "She steps off the boat.", AudioIntent: "gulls, wind",
		ContinuityNotes: "Same coat as scene 1.", Status: versioning.StatusDraft,
		CreatedAt: now, UpdatedAt: now, Revision: 1,
	}
	if err := repo.CreateShot(ctx, shot); err != nil {
		t.Fatalf("CreateShot: %v", err)
	}
	loadedShot, err := repo.GetShot(ctx, shot.ID)
	if err != nil {
		t.Fatalf("GetShot: %v", err)
	}
	if loadedShot.ShotNumber != "1" || loadedShot.ShotSize != "wide" || loadedShot.CameraAngle != "eye level" {
		t.Fatalf("shot machinery round-tripped wrong: %+v", loadedShot)
	}
	if loadedShot.CameraMovement != "slow push" || loadedShot.VisualDescription != "The harbour at dawn." {
		t.Fatalf("shot description round-tripped wrong: %+v", loadedShot)
	}
	if loadedShot.ActionDescription != shot.ActionDescription || loadedShot.AudioIntent != shot.AudioIntent {
		t.Fatalf("shot direction round-tripped wrong: %+v", loadedShot)
	}
	if loadedShot.ContinuityNotes != shot.ContinuityNotes || loadedShot.EstimatedDurationSeconds != 8 {
		t.Fatalf("shot continuity round-tripped wrong: %+v", loadedShot)
	}
	if loadedShot.Status != versioning.StatusDraft || loadedShot.Revision != 1 {
		t.Fatalf("shot state round-tripped wrong: %+v", loadedShot)
	}
	shots, err := repo.ListShots(ctx, scene.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(shots) != 1 {
		t.Fatalf("ListShots returned %d rows, want 1", len(shots))
	}
	if taken, err = repo.CountShotsAtOrdinal(ctx, scene.ID, 1); err != nil {
		t.Fatal(err)
	} else if taken != 1 {
		t.Fatalf("CountShotsAtOrdinal = %d, want 1", taken)
	}
	clashingShot := shot
	clashingShot.ID = "shot-2"
	if err := repo.CreateShot(ctx, clashingShot); err == nil {
		t.Fatal("a reused shot ordinal was accepted")
	} else if domainErr, ok := script.AsError(err); !ok || domainErr.Category != script.CategoryConflict {
		t.Fatalf("expected a conflict, got %v", err)
	}
	// A shot in a scene that does not exist is refused by the foreign key.
	orphan := shot
	orphan.ID = "shot-orphan"
	orphan.SceneID = "missing-scene"
	if err := repo.CreateShot(ctx, orphan); err == nil {
		t.Fatal("a shot was created in a scene that does not exist")
	}

	foreignKeysClean(t, db)
}

// TestScriptRepositoryMissingRowIsNotFound proves a missing row maps to the
// domain's not-found category rather than a raw SQL error.
func TestScriptRepositoryMissingRowIsNotFound(t *testing.T) {
	repo, _ := openWP05ScriptRepo(t)
	ctx := context.Background()
	const missing = "00000000-0000-7000-8000-00000000dead"
	cases := []struct {
		name string
		read func() error
	}{
		{"episode", func() error { _, err := repo.GetEpisode(ctx, missing); return err }},
		{"skeleton", func() error { _, err := repo.GetStorySkeletonVersion(ctx, missing); return err }},
		{"strategy", func() error { _, err := repo.GetAdaptationStrategyVersion(ctx, missing); return err }},
		{"script", func() error { _, err := repo.GetScript(ctx, missing); return err }},
		{"script version", func() error { _, err := repo.GetScriptVersion(ctx, missing); return err }},
		{"scene", func() error { _, err := repo.GetScene(ctx, missing); return err }},
		{"shot", func() error { _, err := repo.GetShot(ctx, missing); return err }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.read()
			if err == nil {
				t.Fatalf("reading a missing %s succeeded", testCase.name)
			}
			domainErr, ok := script.AsError(err)
			if !ok || domainErr.Category != script.CategoryNotFound {
				t.Fatalf("expected not_found, got %v", err)
			}
		})
	}
}

// TestScriptRepositoryUnattachedFailsClosed proves a repository with no
// connection reports the store as unavailable rather than panicking.
func TestScriptRepositoryUnattachedFailsClosed(t *testing.T) {
	repo := NewScriptRepository(nil)
	ctx := context.Background()
	if _, err := repo.GetEpisode(ctx, "episode-1"); err == nil {
		t.Fatal("an unattached repository read an episode")
	}
	if err := repo.CreateEpisode(ctx, sampleEpisode(t)); err == nil {
		t.Fatal("an unattached repository wrote an episode")
	}
	if err := repo.ApproveScriptVersion(ctx, script.ScriptVersion{ID: "script-version-1"}, ""); err == nil {
		t.Fatal("an unattached repository ran an approval")
	}
	// A copy bound to a transaction keeps the handle it was given.
	handle, err := openDriver(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = handle.Close() }()
	tx, err := handle.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	bound := NewScriptRepository(handle).WithinTx(tx)
	if bound.conn() == nil {
		t.Fatal("WithinTx produced a repository with no connection")
	}
}
