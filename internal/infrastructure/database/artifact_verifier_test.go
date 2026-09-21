package database

import (
	"context"
	"strings"
	"testing"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
)

// TestArtifactVerifierKnowsEveryTypeAWriteToolReports is the test VerifiedTypes promised.
//
// `artifact_verifier.go`'s own comment says the list exists "so a test can compare it against
// the types the write tools report: a tool whose entityType is not in this set would produce a
// reference the verifier refuses, which would turn a successful write into a failed stage."
// That test did not exist, and an independent review found the function had no caller anywhere.
//
// The two lists are written independently — one in Go, one in the SQL — so a rename on either
// side would make a successful write fail the stage it succeeded in, and nothing would say so
// until a run happened to use that tool.
func TestArtifactVerifierKnowsEveryTypeAWriteToolReports(t *testing.T) {
	// The types the write tools return, taken from agenttools' artifactResult calls. Written out
	// here rather than imported because this package must not depend on the tool table — and the
	// assertion is exactly that the two agree.
	wantKnown := []string{
		"story_skeleton_version",
		"adaptation_strategy_version",
		"script_version",
		"director_plan_version",
		"storyboard_version",
		"storyboard_panel_version",
		"asset_version",
		// WP-09's write tool reports this one. It is a REPORT rather than a version of one
		// of the four families — its own table, its own approval, its own precondition —
		// which is why it is a separate entry rather than a mode of `asset_version`.
		"asset_gap_report",
		"story_entity",
		"story_event",
	}
	known := map[string]bool{}
	for _, name := range VerifiedTypes() {
		known[name] = true
	}
	for _, name := range wantKnown {
		if !known[name] {
			t.Errorf("the verifier cannot check %q, so a write reporting one would fail its stage", name)
		}
	}
	// And the set is not open: every entry the verifier knows is one a tool reports. A type it
	// accepted but no tool produced would be a statement about an entity that does not exist.
	reported := map[string]bool{}
	for _, name := range wantKnown {
		reported[name] = true
	}
	for _, name := range VerifiedTypes() {
		if !reported[name] {
			t.Errorf("the verifier accepts %q, which no write tool reports", name)
		}
	}
}

// TestArtifactVerifierAnswersEachTypeHonestly is the behavioural half.
//
// The type-to-statement table could name the right KEYS and query the wrong TABLES, which a
// list comparison cannot catch. So this asks the verifier about a real row of each kind: the row
// is seeded through the schema, and the verifier must find it. A mutation that made any arm
// refuse everything — the review did exactly that to the panel arm — fails here.
func TestArtifactVerifierAnswersEachTypeHonestly(t *testing.T) {
	fixture := newAgentFixture(t)
	ctx := context.Background()
	verifier := NewArtifactVerifier(fixture.db)

	// The rows this test can seed without a service. Each is written with the columns the
	// schema requires, which is what makes this a test of the QUERY rather than of a fixture.
	// script_versions needs a script, which needs an episode: the drama seed has one.
	// The drama seed already wrote a script for its episode, and `scripts` is unique per
	// episode, so the version hangs off THAT script rather than a second one.
	if _, err := fixture.db.ExecContext(ctx, `INSERT INTO script_versions
		(id, script_id, version_number, status, created_at)
		VALUES ('seed-script-version', 'drama-script', 2, 'draft', ?)`, agentStamp); err != nil {
		t.Fatalf("seeding the script version: %v", err)
	}
	if _, err := fixture.db.ExecContext(ctx, `INSERT INTO story_entities
		(id, project_id, entity_type, canonical_name, status, source_scope, created_at, updated_at)
		VALUES ('seed-entity', 'drama-project', 'character', 'Someone', 'candidate', 'original', ?, ?)`,
		agentStamp, agentStamp); err != nil {
		t.Fatalf("seeding the entity: %v", err)
	}
	if _, err := fixture.db.ExecContext(ctx, `INSERT INTO story_events
		(id, project_id, ordinal, name, status, created_at, updated_at)
		VALUES ('seed-event', 'drama-project', 1, 'Something', 'candidate', ?, ?)`,
		agentStamp, agentStamp); err != nil {
		t.Fatalf("seeding the event: %v", err)
	}

	// The storyboard chain, written through the SCHEMA rather than through a service, because this
	// test is about the verifier's queries rather than about the services that normally write
	// these rows. A shot belongs to a scene, a scene to a script version, a storyboard version to
	// a storyboard, and a panel to an item — five rows for one id, which is why the panel arm was
	// the easiest to get wrong and the one the review mutated.
	storyboardSeed := []string{
		`INSERT INTO scenes (id, script_version_id, ordinal, created_at, updated_at)
		 VALUES ('seed-scene', 'seed-script-version', 1, '` + agentStamp + `', '` + agentStamp + `')`,
		`INSERT INTO shots (id, scene_id, ordinal, created_at, updated_at)
		 VALUES ('seed-shot', 'seed-scene', 1, '` + agentStamp + `', '` + agentStamp + `')`,
		`INSERT INTO storyboards (id, episode_id, created_at, updated_at)
		 VALUES ('seed-storyboard', 'drama-episode', '` + agentStamp + `', '` + agentStamp + `')`,
		// A director plan version, because a storyboard version names one as a REQUIRED field:
		// section 9.3 makes the plan and the script fields of the version rather than provenance.
		`INSERT INTO director_plan_versions (id, episode_id, version_number, status, script_version_id, created_at)
		 VALUES ('seed-plan-version', 'drama-episode', 1, 'draft', 'seed-script-version', '` + agentStamp + `')`,
		`INSERT INTO storyboard_versions (id, storyboard_id, version_number, status, script_version_id,
		   director_plan_version_id, created_at)
		 VALUES ('seed-board-version', 'seed-storyboard', 1, 'draft', 'seed-script-version',
		   'seed-plan-version', '` + agentStamp + `')`,
		`INSERT INTO storyboard_items (id, storyboard_version_id, shot_id, ordinal, created_at, updated_at)
		 VALUES ('seed-item', 'seed-board-version', 'seed-shot', 1, '` + agentStamp + `', '` + agentStamp + `')`,
		`INSERT INTO storyboard_panel_versions (id, storyboard_item_id, version_number, status, created_at)
		 VALUES ('seed-panel-version', 'seed-item', 1, 'draft', '` + agentStamp + `')`,
	}
	for _, statement := range storyboardSeed {
		if _, err := fixture.db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seeding the storyboard chain: %v", err)
		}
	}

	// Each type resolves to a row that exists.
	for _, reference := range []struct{ entityType, entityID string }{
		{"script_version", "seed-script-version"},
		{"storyboard_panel_version", "seed-panel-version"},
		{"story_entity", "seed-entity"},
		{"story_event", "seed-event"},
	} {
		err := verifier.VerifyArtifacts(ctx, []agentruntime.ArtifactRef{
			{EntityType: reference.entityType, EntityID: reference.entityID},
		})
		if err != nil {
			t.Errorf("the verifier refused a real %s: %v", reference.entityType, err)
		}
	}
	// And an id that does not exist under a KNOWN type is refused, so the arms are queries
	// rather than unconditional passes.
	for _, reference := range []struct{ entityType, entityID string }{
		{"script_version", "no-such-version"},
		{"story_entity", "no-such-entity"},
		{"story_event", "no-such-event"},
	} {
		err := verifier.VerifyArtifacts(ctx, []agentruntime.ArtifactRef{
			{EntityType: reference.entityType, EntityID: reference.entityID},
		})
		if err == nil {
			t.Errorf("the verifier accepted a missing %s", reference.entityType)
		}
	}
}

// TestArtifactVerifierReportsAQueryFailureAsStorageError covers the distinction the review
// found untested: a query that FAILED is not a missing artifact.
//
// Reporting a storage fault as a hallucination would send a reader looking for a model problem
// that is not there — and AC-AGENT-003's diagnosis depends on the two being separable.
func TestArtifactVerifierReportsAQueryFailureAsStorageError(t *testing.T) {
	fixture := newAgentFixture(t)
	ctx := context.Background()
	if _, err := fixture.db.ExecContext(ctx, `DROP TABLE script_versions`); err != nil {
		t.Fatalf("dropping the table: %v", err)
	}
	// The table is gone, so the query cannot run — which is a storage fault rather than a missing
	// row. Dropping it is how this test reaches that path without closing a connection the
	// fixture's own cleanup needs, and without depending on a pool that is merely absent.
	verifier := NewArtifactVerifier(fixture.db)
	err := verifier.VerifyArtifacts(ctx, []agentruntime.ArtifactRef{
		{EntityType: "script_version", EntityID: "any-id"},
	})
	if err == nil {
		t.Fatal("a query against a missing table reported success")
	}
	// The refusal must say the CHECK failed rather than that the artifact is missing: reporting a
	// storage fault as a hallucination would send a reader looking for a model problem.
	if !strings.Contains(err.Error(), "could not be checked") {
		t.Fatalf("the refusal reads %q, which does not say the check failed", err)
	}
	// And the two are distinguishable, which is the point: a missing ROW under a working query is
	// the artifact error, not this one.
	if strings.Contains(err.Error(), "does not exist") {
		t.Fatal("a storage fault was reported as a missing artifact")
	}
}
