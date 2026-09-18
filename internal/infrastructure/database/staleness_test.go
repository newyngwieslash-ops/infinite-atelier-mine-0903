package database

import (
	"context"
	"database/sql"
	"testing"
	"time"

	stalenessapp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/staleness"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/staleness"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// dramaSeedChapter writes a source document with a version and one chapter, and
// returns the chapter id. It is the upstream a propagation test changes.
func dramaSeedChapter(t *testing.T, db *sql.DB, chapterID string) {
	t.Helper()
	statements := []string{
		`INSERT INTO source_documents (id, project_id, document_type, name, created_at, updated_at)
		 VALUES ('drama-document', 'drama-project', 'novel', 'Source', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		`INSERT INTO source_document_versions (id, source_document_id, version_number, created_at)
		 VALUES ('drama-document-version', 'drama-document', 1, '2026-01-01T00:00:00Z')`,
		`INSERT INTO chapters (id, source_document_version_id, ordinal, title, created_at, updated_at)
		 VALUES ('` + chapterID + `', 'drama-document-version', 1, 'Chapter One', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("seeding the chapter fixture failed: %v\n%s", err, statement)
		}
	}
}

// dramaSeedStoryEvent writes one event referencing a chapter, and returns its
// id. The event is the dependent the propagation test expects to be marked.
func dramaSeedStoryEvent(t *testing.T, db *sql.DB, eventID, chapterID string) {
	t.Helper()
	statement := `INSERT INTO story_events (id, project_id, chapter_id, ordinal, name, created_at, updated_at)
		VALUES ('` + eventID + `', 'drama-project', '` + chapterID + `', 1, 'The Turn', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`
	if _, err := db.ExecContext(context.Background(), statement); err != nil {
		t.Fatalf("seeding the story event failed: %v\n%s", err, statement)
	}
}

// TestStalenessRepositoryRoundTrip covers the upsert, the reads and the two
// stamps.
func TestStalenessRepositoryRoundTrip(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewStalenessRepository(db)
	ctx := context.Background()
	now := dramaTime()

	mark := staleness.Mark{
		ArtifactType: staleness.ArtifactStoryEvent, ArtifactID: "drama-event",
		ProjectID: "drama-project", Severity: staleness.SeverityReviewRequired,
		Reason: "the chapter was edited", UpstreamType: staleness.ArtifactChapter,
		UpstreamID: "drama-chapter",
	}
	if err := repo.UpsertMark(ctx, mark, now); err != nil {
		t.Fatalf("UpsertMark: %v", err)
	}
	loaded, found, err := repo.GetMark(ctx, staleness.ArtifactStoryEvent, "drama-event")
	if err != nil || !found {
		t.Fatalf("GetMark: found=%v err=%v", found, err)
	}
	if loaded.Severity != staleness.SeverityReviewRequired || loaded.Reason != mark.Reason {
		t.Fatalf("mark round trip: %+v", loaded)
	}
	if loaded.UpstreamType != staleness.ArtifactChapter || loaded.UpstreamID != "drama-chapter" {
		t.Fatalf("mark lost its upstream: %+v", loaded)
	}
	if loaded.ProjectID != "drama-project" || loaded.Waived || loaded.ClearedAt != "" {
		t.Fatalf("mark defaults wrong: %+v", loaded)
	}
	if err := loaded.Validate(); err != nil {
		t.Fatalf("the stored mark does not satisfy the domain: %v", err)
	}

	marks, err := repo.ListMarks(ctx, "drama-project")
	if err != nil {
		t.Fatalf("ListMarks: %v", err)
	}
	if len(marks) != 1 {
		t.Fatalf("ListMarks returned %d rows, want 1", len(marks))
	}
	open, err := repo.ListOpenMarks(ctx, "drama-project")
	if err != nil {
		t.Fatalf("ListOpenMarks: %v", err)
	}
	if len(open) != 1 {
		t.Fatalf("ListOpenMarks returned %d rows, want 1", len(open))
	}

	// Clearing hides the mark from the open list but keeps the row.
	found, err = repo.ClearMark(ctx, staleness.ArtifactStoryEvent, "drama-event", now.Add(time.Hour))
	if err != nil || !found {
		t.Fatalf("ClearMark: found=%v err=%v", found, err)
	}
	open, err = repo.ListOpenMarks(ctx, "drama-project")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 0 {
		t.Fatalf("a cleared mark is still open: %+v", open)
	}
	all, err := repo.ListMarks(ctx, "drama-project")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].ClearedAt == "" {
		t.Fatalf("the cleared mark was removed: %+v", all)
	}
	// Clearing an artifact that has no mark reports that nothing matched rather
	// than an error.
	found, err = repo.ClearMark(ctx, staleness.ArtifactStoryEvent, "never-marked", now)
	if err != nil {
		t.Fatalf("ClearMark on an unmarked artifact: %v", err)
	}
	if found {
		t.Fatal("clearing an unmarked artifact reported a write")
	}

	// The waiver occupies the three columns section 15.3 requires.
	found, err = repo.WaiveMark(ctx, staleness.ArtifactStoryEvent, "drama-event", "drama-decision", "kept on purpose", now)
	if err != nil || !found {
		t.Fatalf("WaiveMark: found=%v err=%v", found, err)
	}
	waived, found, err := repo.GetMark(ctx, staleness.ArtifactStoryEvent, "drama-event")
	if err != nil || !found {
		t.Fatal(err)
	}
	if !waived.Waived || waived.WaivedByDecisionID != "drama-decision" || waived.WaivedReason != "kept on purpose" {
		t.Fatalf("stored waiver = %+v", waived)
	}
	if err := waived.Validate(); err != nil {
		t.Fatalf("the stored waiver does not satisfy the domain: %v", err)
	}
	if found, err := repo.WaiveMark(ctx, staleness.ArtifactStoryEvent, "never-marked", "d", "r", now); err != nil || found {
		t.Fatalf("waiving an unmarked artifact: found=%v err=%v", found, err)
	}
	foreignKeysClean(t, db)
}

// TestStalenessRepositorySecondMarkUpdates is the UPSERT: migration 000012
// makes (artifact_type, artifact_id) the primary key, so a second mark for one
// artifact must update the row rather than add a second one.
func TestStalenessRepositorySecondMarkUpdates(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewStalenessRepository(db)
	ctx := context.Background()
	now := dramaTime()

	first := staleness.Mark{
		ArtifactType: staleness.ArtifactScriptVersion, ArtifactID: "drama-script-version",
		ProjectID: "drama-project", Severity: staleness.SeverityInformational,
		Reason: "noticed", UpstreamType: staleness.ArtifactChapter, UpstreamID: "drama-chapter",
	}
	if err := repo.UpsertMark(ctx, first, now); err != nil {
		t.Fatalf("first UpsertMark: %v", err)
	}
	createdAt := queryText(t, db, "SELECT created_at FROM artifact_staleness WHERE artifact_type = 'script_version'")
	if createdAt == "" {
		t.Fatal("the first mark stored no created_at")
	}

	second := first
	second.Severity = staleness.SeverityBreaking
	second.Reason = "the script no longer matches"
	second.UpstreamID = "drama-chapter-2"
	if err := repo.UpsertMark(ctx, second, now.Add(time.Hour)); err != nil {
		t.Fatalf("second UpsertMark: %v", err)
	}
	// One row, not two: the primary key is the artifact.
	if count := queryInt(t, db, "SELECT COUNT(*) FROM artifact_staleness"); count != 1 {
		t.Fatalf("%d rows after re-marking, want 1", count)
	}
	loaded, found, err := repo.GetMark(ctx, staleness.ArtifactScriptVersion, "drama-script-version")
	if err != nil || !found {
		t.Fatal(err)
	}
	if loaded.Severity != staleness.SeverityBreaking || loaded.Reason != second.Reason {
		t.Fatalf("the update did not replace the mark: %+v", loaded)
	}
	if loaded.UpstreamID != "drama-chapter-2" {
		t.Fatalf("the upstream was not replaced: %+v", loaded)
	}
	// The row's revision moves with the update, and the first sighting is kept.
	if revision := queryInt(t, db, "SELECT revision FROM artifact_staleness"); revision != 2 {
		t.Fatalf("revision = %d, want 2 after one update", revision)
	}
	if kept := queryText(t, db, "SELECT created_at FROM artifact_staleness"); kept != createdAt {
		t.Fatalf("created_at moved from %q to %q", createdAt, kept)
	}
	// A different artifact is a different row.
	third := first
	third.ArtifactID = "another-script-version"
	if err := repo.UpsertMark(ctx, third, now); err != nil {
		t.Fatal(err)
	}
	if count := queryInt(t, db, "SELECT COUNT(*) FROM artifact_staleness"); count != 2 {
		t.Fatalf("%d rows for two artifacts, want 2", count)
	}
	// A project that does not exist is refused by the foreign key.
	orphan := first
	orphan.ArtifactID = "third"
	orphan.ProjectID = "no-such-project"
	if err := repo.UpsertMark(ctx, orphan, now); err == nil {
		t.Fatal("a mark was stored under a project that does not exist")
	} else if domainErr, ok := staleness.AsError(err); !ok || domainErr.Category != staleness.CategoryInvalidInput {
		t.Fatalf("expected the domain's invalid_input, got %v", err)
	}
	foreignKeysClean(t, db)
}

// TestStalenessRepositoryUpsertMarksIsAtomic proves the batch write is one
// transaction: a failure partway leaves none of the marks behind, so a
// propagation cannot report a blast radius the store does not hold.
func TestStalenessRepositoryUpsertMarksIsAtomic(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	repo := NewStalenessRepository(db)
	ctx := context.Background()
	now := dramaTime()

	marks := []staleness.Mark{
		{
			ArtifactType: staleness.ArtifactStoryEvent, ArtifactID: "event-one",
			ProjectID: "drama-project", Severity: staleness.SeverityReviewRequired,
		},
		{
			// The second mark names a project that does not exist, so its insert
			// fails after the first has already run inside the transaction.
			ArtifactType: staleness.ArtifactStoryEvent, ArtifactID: "event-two",
			ProjectID: "no-such-project", Severity: staleness.SeverityReviewRequired,
		},
	}
	if err := repo.UpsertMarks(ctx, marks, now); err == nil {
		t.Fatal("a batch with an unstorable mark succeeded")
	}
	if count := queryInt(t, db, "SELECT COUNT(*) FROM artifact_staleness"); count != 0 {
		t.Fatalf("%d marks survived a failed batch, so it was not atomic", count)
	}
	// The same batch with a valid project stores both.
	marks[1].ProjectID = "drama-project"
	if err := repo.UpsertMarks(ctx, marks, now); err != nil {
		t.Fatalf("UpsertMarks: %v", err)
	}
	if count := queryInt(t, db, "SELECT COUNT(*) FROM artifact_staleness"); count != 2 {
		t.Fatalf("%d marks stored, want 2", count)
	}
	// An empty batch is a no-op rather than an error.
	if err := repo.UpsertMarks(ctx, nil, now); err != nil {
		t.Fatalf("an empty batch failed: %v", err)
	}
	foreignKeysClean(t, db)
}

// TestDependentFinderReadsTheSchemaColumns covers the mapping from upstream
// types to the columns that reference them: each case seeds a referencing row
// and asserts the finder returns it, so a wrong column would be caught here.
func TestDependentFinderReadsTheSchemaColumns(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	ctx := context.Background()
	finder := NewDependentFinder(db)

	// A chapter referencing a source document version.
	dramaSeedChapter(t, db, "drama-chapter")
	// A story event referencing the chapter.
	dramaSeedStoryEvent(t, db, "drama-event", "drama-chapter")
	// A scene referencing both the script version and the event.
	if _, err := db.ExecContext(ctx, `INSERT INTO scenes (id, script_version_id, ordinal, source_story_event_id, created_at, updated_at)
		VALUES ('drama-scene', 'drama-script-version', 1, 'drama-event', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	// A shot in the scene, and a storyboard item for the shot.
	if _, err := db.ExecContext(ctx, `INSERT INTO shots (id, scene_id, ordinal, created_at, updated_at)
		VALUES ('drama-shot', 'drama-scene', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	// A story entity, its asset, a character state and an event participant.
	if _, err := db.ExecContext(ctx, `INSERT INTO story_entities (id, project_id, entity_type, canonical_name, created_at, updated_at)
		VALUES ('drama-entity', 'drama-project', 'character', 'Mira', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO assets (id, project_id, asset_type, name, story_entity_id, current_approved_version_id, created_at, updated_at)
		VALUES ('drama-asset', 'drama-project', 'character', 'Mira', 'drama-entity', 'drama-asset-version', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO asset_versions (id, asset_id, version_number, status, created_at)
		VALUES ('drama-asset-version', 'drama-asset', 1, 'approved', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO character_states (id, character_entity_id, created_at, updated_at)
		VALUES ('drama-state', 'drama-entity', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO story_event_participants (story_event_id, story_entity_id, role, created_at)
		VALUES ('drama-event', 'drama-entity', 'actor', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	// A stage attempt referencing the workflow run.
	if _, err := db.ExecContext(ctx, `INSERT INTO stage_runs (id, workflow_run_id, stage, attempt, status, created_at)
		VALUES ('drama-stage', 'drama-run', 'story_skeleton', 1, 'pending', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name       string
		dependent  staleness.ArtifactType
		upstream   staleness.ArtifactType
		upstreamID string
		want       []string
	}{
		{"chapter references source_document_version", staleness.ArtifactChapter, staleness.ArtifactSourceDocumentVersion, "drama-document-version", []string{"drama-chapter"}},
		{"story event references chapter", staleness.ArtifactStoryEvent, staleness.ArtifactChapter, "drama-chapter", []string{"drama-event"}},
		{"scene references script_version", staleness.ArtifactScene, staleness.ArtifactScriptVersion, "drama-script-version", []string{"drama-scene"}},
		{"scene references story_event", staleness.ArtifactScene, staleness.ArtifactStoryEvent, "drama-event", []string{"drama-scene"}}, {"shot references scene", staleness.ArtifactShot, staleness.ArtifactScene, "drama-scene", []string{"drama-shot"}},
		{"asset version references story_entity", staleness.ArtifactAssetVersion, staleness.ArtifactStoryEntity, "drama-entity", []string{"drama-asset-version"}},
		{"character state references story_entity", staleness.ArtifactCharacterState, staleness.ArtifactStoryEntity, "drama-entity", []string{"drama-state"}},
		{"event participant references story_entity", staleness.ArtifactStoryEvent, staleness.ArtifactStoryEntity, "drama-entity", []string{"drama-event"}},
		{"stage run references workflow_run", staleness.ArtifactStageRun, staleness.ArtifactWorkflowRun, "drama-run", []string{"drama-stage"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			refs, err := finder.FindDependents(ctx, testCase.dependent, testCase.upstream, testCase.upstreamID)
			if err != nil {
				t.Fatalf("FindDependents: %v", err)
			}
			got := make([]string, 0, len(refs))
			for _, ref := range refs {
				if ref.ArtifactType != testCase.dependent {
					t.Fatalf("a ref names type %q, want %q", ref.ArtifactType, testCase.dependent)
				}
				got = append(got, ref.ArtifactID)
			}
			if len(got) != len(testCase.want) {
				t.Fatalf("found %v, want %v", got, testCase.want)
			}
			for index, value := range testCase.want {
				if got[index] != value {
					t.Fatalf("found %v, want %v", got, testCase.want)
				}
			}
		})
	}

	// A pair with no mapping returns an empty slice, not an error: no column in
	// this schema points at a storyboard panel version.
	refs, err := finder.FindDependents(ctx, staleness.ArtifactCanvasNode, staleness.ArtifactStoryboardPanel, "anything")
	if err != nil {
		t.Fatalf("an unmapped pair reported an error: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("an unmapped pair returned %d refs", len(refs))
	}
	// An upstream whose rows exist but which nothing references returns empty.
	refs, err = finder.FindDependents(ctx, staleness.ArtifactStoryEvent, staleness.ArtifactChapter, "no-such-chapter")
	if err != nil {
		t.Fatalf("FindDependents: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("a chapter nothing references returned %d refs", len(refs))
	}

	// dialogue_lines.source_story_event_id is read as well: a line naming the
	// event points at its scene, and a scene reached both ways is returned once.
	if _, err := db.ExecContext(ctx, `INSERT INTO scenes (id, script_version_id, ordinal, created_at, updated_at)
		VALUES ('drama-scene-2', 'drama-script-version', 2, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO dialogue_lines (id, scene_id, ordinal, text, source_story_event_id, created_at, updated_at)
		VALUES ('drama-line', 'drama-scene-2', 1, 'we leave at dawn', 'drama-event', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	refs, err = finder.FindDependents(ctx, staleness.ArtifactScene, staleness.ArtifactStoryEvent, "drama-event")
	if err != nil {
		t.Fatalf("FindDependents through dialogue_lines: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("found %d scenes, want the direct one and the line's", len(refs))
	}
	// A line whose scene also names the event must not return that scene twice.
	if _, err := db.ExecContext(ctx, `INSERT INTO dialogue_lines (id, scene_id, ordinal, text, source_story_event_id, created_at, updated_at)
		VALUES ('drama-line-2', 'drama-scene', 1, 'the turn', 'drama-event', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	refs, err = finder.FindDependents(ctx, staleness.ArtifactScene, staleness.ArtifactStoryEvent, "drama-event")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 {
		t.Fatalf("a scene reached through both columns was returned %d times, want once", len(refs))
	}
	for index := 1; index < len(refs); index++ {
		if refs[index].ArtifactID == refs[index-1].ArtifactID {
			t.Fatalf("duplicate ref %q", refs[index].ArtifactID)
		}
	}
}

// TestProjectResolverWalksToTheProject proves each query follows a declared
// foreign key to the owning project, including the four-hop storyboard path.
func TestProjectResolverWalksToTheProject(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	ctx := context.Background()
	resolver := NewProjectResolver(db)

	dramaSeedChapter(t, db, "drama-chapter")
	dramaSeedStoryEvent(t, db, "drama-event", "drama-chapter")
	if _, err := db.ExecContext(ctx, `INSERT INTO scenes (id, script_version_id, ordinal, source_story_event_id, created_at, updated_at)
		VALUES ('drama-scene', 'drama-script-version', 1, 'drama-event', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO shots (id, scene_id, ordinal, created_at, updated_at)
		VALUES ('drama-shot', 'drama-scene', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	repo := NewStoryboardRepository(db)
	fixture := dramaSeedStoryboard(t, repo)
	generator := dramaIDGenerator()
	itemID := mustNewID(t, generator)
	now := dramaTime()
	if err := repo.CreateStoryboardItem(ctx, storyboard.StoryboardItem{
		ID: itemID, StoryboardVersionID: fixture.VersionID, ShotID: "drama-shot", Ordinal: 1,
		Status: versioning.StatusDraft, CreatedAt: now, UpdatedAt: now, Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	panelID := mustNewID(t, generator)
	if err := repo.CreatePanelVersion(ctx, storyboard.StoryboardPanelVersion{
		ID: panelID, StoryboardItemID: itemID, VersionNumber: 1, Status: versioning.StatusDraft,
		CreatedByType: versioning.CreatedByUser, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	for _, testCase := range []struct {
		name         string
		artifactType staleness.ArtifactType
		artifactID   string
	}{
		{"chapter", staleness.ArtifactChapter, "drama-chapter"},
		{"story_event", staleness.ArtifactStoryEvent, "drama-event"},
		{"script_version", staleness.ArtifactScriptVersion, "drama-script-version"},
		{"scene", staleness.ArtifactScene, "drama-scene"},
		{"shot", staleness.ArtifactShot, "drama-shot"},
		{"director_plan_version", staleness.ArtifactDirectorPlan, fixture.PlanVersionID},
		{"storyboard_version", staleness.ArtifactStoryboardVersion, fixture.VersionID},
		{"storyboard_item", staleness.ArtifactStoryboardItem, itemID},
		{"storyboard_panel_version", staleness.ArtifactStoryboardPanel, panelID},
		{"workflow_run", staleness.ArtifactWorkflowRun, "drama-run"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			projectID, found, err := resolver.ProjectFor(ctx, testCase.artifactType, testCase.artifactID)
			if err != nil {
				t.Fatalf("ProjectFor: %v", err)
			}
			if !found {
				t.Fatal("the artifact's project was not resolved")
			}
			if projectID != "drama-project" {
				t.Fatalf("project = %q, want drama-project", projectID)
			}
		})
	}
	// A row that does not exist resolves to nothing rather than an error: the
	// propagation reads that as "skip this dependent".
	if _, found, err := resolver.ProjectFor(ctx, staleness.ArtifactShot, "no-such-shot"); err != nil || found {
		t.Fatalf("a missing row reported found=%v err=%v", found, err)
	}
	// A type with no query resolves to nothing too.
	if _, found, err := resolver.ProjectFor(ctx, staleness.ArtifactCanvasNode, "anything"); err != nil || found {
		t.Fatalf("an unmapped type reported found=%v err=%v", found, err)
	}
}

// TestPropagationEndToEndAgainstTheDatabase is the whole chain over real SQL:
// a chapter change is propagated, and the story event that references it is
// marked review_required in the store, through the application service and the
// production repositories.
func TestPropagationEndToEndAgainstTheDatabase(t *testing.T) {
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	ctx := context.Background()

	dramaSeedChapter(t, db, "drama-chapter")
	dramaSeedStoryEvent(t, db, "drama-event", "drama-chapter")
	// A scene referencing the event is a transitive dependent: it holds no
	// chapter reference, so it must not be marked.
	if _, err := db.ExecContext(ctx, `INSERT INTO scenes (id, script_version_id, ordinal, source_story_event_id, created_at, updated_at)
		VALUES ('drama-scene', 'drama-script-version', 1, 'drama-event', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	marks := NewStalenessRepository(db)
	service := stalenessapp.NewService(stalenessapp.Options{
		Marks:      marks,
		Dependents: NewDependentFinder(db),
		Projects:   NewProjectResolver(db),
		Clock:      fixedClockProvider{},
		IDs:        newTestIDGenerator(),
	})
	result, err := service.PropagateFrom(ctx, stalenessapp.PropagateRequest{
		ChangedType: staleness.ArtifactChapter, ChangedID: "drama-chapter",
		ProjectID: "drama-project", Reason: "chapter one was edited",
	})
	if err != nil {
		t.Fatalf("PropagateFrom: %v", err)
	}
	if result.ReviewRequired != 1 {
		t.Fatalf("ReviewRequired = %d, want 1 (the story event references the chapter)", result.ReviewRequired)
	}
	if result.Informational != 1 {
		t.Fatalf("Informational = %d, want 1 (the scene hanging off that event)", result.Informational)
	}
	if result.Truncated {
		t.Fatal("the walk reported itself truncated")
	}
	if len(result.Marks) != 2 {
		t.Fatalf("marks = %+v", result.Marks)
	}

	// The direct dependent is review_required and carries the chapter as its
	// upstream, because that is the edge it holds.
	stored, found, err := marks.GetMark(ctx, staleness.ArtifactStoryEvent, "drama-event")
	if err != nil || !found {
		t.Fatalf("GetMark after propagation: found=%v err=%v", found, err)
	}
	if stored.Severity != staleness.SeverityReviewRequired {
		t.Fatalf("stored severity = %q, want review_required", stored.Severity)
	}
	if stored.UpstreamType != staleness.ArtifactChapter || stored.UpstreamID != "drama-chapter" {
		t.Fatalf("stored upstream = %s/%s", stored.UpstreamType, stored.UpstreamID)
	}
	if stored.ProjectID != "drama-project" || stored.Reason != "chapter one was edited" {
		t.Fatalf("stored mark = %+v", stored)
	}
	if err := stored.Validate(); err != nil {
		t.Fatalf("the stored mark does not satisfy the domain: %v", err)
	}

	// The scene is reached through the event, so its severity is informational
	// and its upstream is the event it actually references, not the chapter it
	// has no link to.
	sceneMark, found, err := marks.GetMark(ctx, staleness.ArtifactScene, "drama-scene")
	if err != nil || !found {
		t.Fatalf("a transitively reached dependent was not marked (found=%v err=%v)", found, err)
	}
	if sceneMark.Severity != staleness.SeverityInformational {
		t.Fatalf("scene severity = %q, want informational", sceneMark.Severity)
	}
	if sceneMark.UpstreamType != staleness.ArtifactStoryEvent || sceneMark.UpstreamID != "drama-event" {
		t.Fatalf("scene upstream = %s/%s, want story_event/drama-event", sceneMark.UpstreamType, sceneMark.UpstreamID)
	}
	if err := sceneMark.Validate(); err != nil {
		t.Fatalf("the stored scene mark does not satisfy the domain: %v", err)
	}

	open, err := service.ListOpenMarks(ctx, "drama-project")
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 {
		t.Fatalf("%d open marks, want 2 (the event and the scene)", len(open))
	}

	// Propagating the same change again updates the rows rather than adding new
	// ones, because the table is keyed by (artifact_type, artifact_id).
	if _, err := service.PropagateFrom(ctx, stalenessapp.PropagateRequest{
		ChangedType: staleness.ArtifactChapter, ChangedID: "drama-chapter",
		ProjectID: "drama-project", Reason: "edited again",
	}); err != nil {
		t.Fatalf("second PropagateFrom: %v", err)
	}
	if count := queryInt(t, db, "SELECT COUNT(*) FROM artifact_staleness"); count != 2 {
		t.Fatalf("%d marks after two propagations, want 2 (no duplicates)", count)
	}
	// Every row was updated, not just one of them.
	if count := queryInt(t, db, "SELECT COUNT(*) FROM artifact_staleness WHERE reason = 'edited again'"); count != 2 {
		t.Fatalf("%d rows carry the new reason, want 2", count)
	}
	// The revision counter advanced once per propagation on every row, which is
	// what the upsert's DO UPDATE clause increments.
	rows, err := db.QueryContext(ctx, "SELECT revision FROM artifact_staleness ORDER BY artifact_type")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var revision int64
		if err := rows.Scan(&revision); err != nil {
			t.Fatal(err)
		}
		seen++
		if revision != 2 {
			t.Fatalf("revision = %d, want 2 after two propagations", revision)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if seen != 2 {
		t.Fatalf("read %d revision rows, want 2", seen)
	}

	// A dependent of another project is not marked: the same event id cannot
	// exist in two projects, so this is exercised by propagating for a project
	// whose id differs from the row's owner.
	result, err = service.PropagateFrom(ctx, stalenessapp.PropagateRequest{
		ChangedType: staleness.ArtifactChapter, ChangedID: "drama-chapter",
		ProjectID: "another-project", Reason: "should not mark",
	})
	if err != nil {
		t.Fatalf("PropagateFrom for another project: %v", err)
	}
	if result.ReviewRequired != 0 {
		t.Fatalf("a propagation for another project marked %d rows", result.ReviewRequired)
	}
	if result.Marked != 0 {
		t.Fatalf("a propagation for another project marked %d artifacts", result.Marked)
	}
	// The two rows the earlier propagations wrote are untouched and no third was
	// added: a mark filed under the wrong project would be invisible where the
	// artifact actually lives, so it is skipped rather than written.
	if count := queryInt(t, db, "SELECT COUNT(*) FROM artifact_staleness"); count != 2 {
		t.Fatalf("%d marks, want the 2 that already existed", count)
	}
	foreignKeysClean(t, db)
}

// fixedClockProvider adapts the package's fixed clock function to the
// application Clock interface, so the propagation fixture writes timestamps the
// table's other tests can recognise.
type fixedClockProvider struct{}

func (fixedClockProvider) Now() time.Time { return fixedClock()() }
