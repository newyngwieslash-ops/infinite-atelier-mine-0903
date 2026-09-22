package database

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appassets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	appstoryboard "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// acceptance_wp09_test.go covers AC-ASSET-001, AC-ASSET-002 and AC-BOARD-002 by name.
//
// The production canary next door walks the whole chain; this file is where each CRITERION
// is asserted as a list, because a walk that reaches the end can pass while a clause is
// unmet — the lesson WP-08 recorded when its canary used a test double for the projector.

// loadStoryboardFixture reads one of the storyboard fixtures the generator writes.
func loadStoryboardFixture(t *testing.T, name string) map[string]any {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "canary-drama", name))
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	var document map[string]any
	if err := json.Unmarshal(body, &document); err != nil {
		t.Fatalf("parsing %s: %v", name, err)
	}
	return document
}

// fixtureItems reads a storyboard fixture's rows.
func fixtureItems(t *testing.T, document map[string]any) []map[string]any {
	t.Helper()
	board, ok := document["storyboard"].(map[string]any)
	if !ok {
		t.Fatal("the fixture has no storyboard")
	}
	raw, ok := board["items"].([]any)
	if !ok {
		t.Fatal("the fixture's storyboard has no items")
	}
	items := make([]map[string]any, 0, len(raw))
	for _, entry := range raw {
		item, ok := entry.(map[string]any)
		if !ok {
			t.Fatal("a fixture row is not an object")
		}
		items = append(items, item)
	}
	return items
}

// TestTheBadStoryboardFixtureNamesTheFaultTheCriterionDescribes covers AC-BOARD-002's
// fixture half.
//
// The criterion is a board whose sixth shot wears the wrong costume, and the fixture has to
// SAY that rather than merely contain it: a document whose fault is only implicit cannot be
// checked, because nothing states which row the supervisor was supposed to find.
func TestTheBadStoryboardFixtureNamesTheFaultTheCriterionDescribes(t *testing.T) {
	document := loadStoryboardFixture(t, "bad-storyboard.json")
	faults, ok := document["faults"].([]any)
	if !ok || len(faults) == 0 {
		t.Fatal("the deliberately-wrong storyboard names no fault, so the fixture proves nothing")
	}
	located, _ := document["mustLocateAt"].(string)
	if located == "" {
		t.Fatal("the fixture does not say which shot a supervisor must find")
	}
	items := fixtureItems(t, document)
	if len(items) != 12 {
		t.Fatalf("the board has %d rows, and AC-BOARD-001 asks for at least twelve", len(items))
	}
	// Every other row is CONSISTENT, which is what makes the fault findable rather than one
	// of many: a board where every row contradicted the rule would pass a supervisor that
	// reported the whole board.
	inconsistent := []string{}
	coats := map[string]int{}
	for _, item := range items {
		notes, _ := item["continuityNotes"].(string)
		shotID, _ := item["shotId"].(string)
		if strings.Contains(notes, "夏装") {
			inconsistent = append(inconsistent, shotID)
		}
		if strings.Contains(notes, "冬装") {
			coats["winter"]++
		}
	}
	if len(inconsistent) != 1 || inconsistent[0] != located {
		t.Fatalf("the fixture's inconsistent rows are %v, and it says the fault is at %q", inconsistent, located)
	}
	if coats["winter"] != len(items)-1 {
		t.Fatalf("%d rows wear the consistent costume of %d", coats["winter"], len(items))
	}
	// And the expected fixture is the FIXED board: the same rows with the sixth corrected,
	// so a FIX that changed anything else would differ from it.
	expected := loadStoryboardFixture(t, "expected-storyboard.json")
	expectedItems := fixtureItems(t, expected)
	if len(expectedItems) != len(items) {
		t.Fatalf("the expected board has %d rows and the wrong one has %d", len(expectedItems), len(items))
	}
	for index, item := range expectedItems {
		notes, _ := item["continuityNotes"].(string)
		if strings.Contains(notes, "夏装") {
			t.Errorf("the expected board's row %d still wears the inconsistent costume", index)
		}
		// Every field but the costume is IDENTICAL: that is AC-BOARD-002's "其他 Shot 不变"
		// stated as a fixture, and it is what a single-row FIX has to reproduce.
		for _, field := range []string{"shotId", "shotSize", "cameraAngle", "cameraMovement", "visualDescription"} {
			if item[field] != items[index][field] {
				t.Errorf("row %d's %s differs between the wrong and expected boards", index, field)
			}
		}
	}
	// The expected fixture states FR-070's required fields AND names the three migration
	// 000018 added, so a reader can see they are asserted rather than taking it on trust.
	required, ok := expected["requiredFields"].([]any)
	if !ok || len(required) == 0 {
		t.Fatal("the expected fixture names no required fields")
	}
	fr070, ok := expected["fr070Fields"].([]any)
	if !ok || len(fr070) != 3 {
		t.Fatalf("the expected fixture names %d FR-070 fields, want three", len(fr070))
	}
	stated := map[string]bool{}
	for _, field := range required {
		if name, ok := field.(string); ok {
			stated[name] = true
		}
	}
	for _, field := range fr070 {
		name, _ := field.(string)
		if !stated[name] {
			t.Errorf("FR-070's field %q is not among the required fields", name)
		}
		// And every row actually carries it, so the requirement is met by the fixture rather
		// than merely named by it.
		for index, item := range expectedItems {
			if value, _ := item[name].(string); strings.TrimSpace(value) == "" {
				t.Errorf("the expected board's row %d has an empty %s", index, name)
			}
		}
	}
}

// TestTheSingleRowFixChangesOnlyThatRow covers AC-BOARD-002's second half through the REAL
// service and database.
//
// The criterion is "FIX 只更新 Shot 6/关联版本；其他 Shot 不变", and the assertion is the strong
// one: every other row is byte-identical before and after, because the command writes one
// row and never touches the others.
func TestTheSingleRowFixChangesOnlyThatRow(t *testing.T) {
	canary := newProductionCanary(t)
	ctx := context.Background()
	scriptVersionID, shotIDs := canary.seedScriptWithShots(t, 12)
	_, itemIDs := canary.seedBoardWithRows(t, scriptVersionID, shotIDs)

	// The board as it stands, read back in full.
	before := map[string]string{}
	for _, itemID := range itemIDs {
		row, err := canary.storyboard.GetStoryboardItem(ctx, itemID)
		if err != nil {
			t.Fatalf("reading row %s: %v", itemID, err)
		}
		before[itemID] = storyboardRowFingerprint(row)
	}
	// The sixth row's costume is changed — the FIX.
	target, err := canary.storyboard.GetStoryboardItem(ctx, itemIDs[5])
	if err != nil {
		t.Fatalf("reading the target row: %v", err)
	}
	fixed := "沈砚穿的是冬装。"
	if _, err := canary.storyboard.UpdateStoryboardItem(ctx, appstoryboard.UpdateStoryboardItemRequest{
		ItemID:           target.ID,
		ExpectedRevision: target.Revision,
		ContinuityNotes:  &fixed,
	}); err != nil {
		t.Fatalf("the single-row fix: %v", err)
	}

	// THE ASSERTION. Every other row is unchanged, field for field.
	for index, itemID := range itemIDs {
		row, err := canary.storyboard.GetStoryboardItem(ctx, itemID)
		if err != nil {
			t.Fatalf("reading row %s after the fix: %v", itemID, err)
		}
		after := storyboardRowFingerprint(row)
		if index == 5 {
			if after == before[itemID] {
				t.Fatal("the fix changed nothing on the row it named")
			}
			continue
		}
		if after != before[itemID] {
			t.Errorf("row %d changed when the fix named row 6:\n before %s\n after  %s", index+1, before[itemID], after)
		}
	}
	// A STALE revision is refused rather than overwriting: the row was just written, so the
	// revision the first read saw is one behind.
	if _, err := canary.storyboard.UpdateStoryboardItem(ctx, appstoryboard.UpdateStoryboardItemRequest{
		ItemID:           target.ID,
		ExpectedRevision: target.Revision,
		ContinuityNotes:  &fixed,
	}); err == nil {
		t.Fatal("a stale revision was accepted, so two windows would overwrite each other")
	}
}

// storyboardRowFingerprint renders every field a single-row fix must leave alone.
//
// It INCLUDES the revision, because a fix that rewrote a row would advance it — and a
// fingerprint that omitted the revision would call two different rows identical.
func storyboardRowFingerprint(row storyboard.StoryboardItem) string {
	return strings.Join([]string{
		row.ID, row.StoryboardVersionID, row.ShotID, row.ShotSize, row.CameraAngle,
		row.CameraMovement, row.VisualDescription, row.ActionDescription,
		row.DialogueAudioSummary, row.ContinuityNotes, row.FirstFrameDescription,
		row.LastFrameDescription, row.VideoMotionDescription, string(row.Status),
		itoaAcceptance(int(row.DurationSeconds)), itoaAcceptance(int(row.Ordinal)),
		itoaAcceptance(int(row.Revision)),
	}, "\x1f")
}

// itoaAcceptance renders an integer for the fingerprint.
func itoaAcceptance(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	if negative {
		return "-" + digits
	}
	return digits
}

// TestACAsset001TwoCandidatesThenAVersionSwitch covers AC-ASSET-001's six clauses in order.
//
// The criterion is a numbered list and each step is asserted where it happens rather than
// at the end, because "the chain finished" is not "the chain did what the list says".
func TestACAsset001TwoCandidatesThenAVersionSwitch(t *testing.T) {
	service, repository, _ := openWP05AssetService(t)
	ctx := context.Background()
	// "角色 Asset"
	record, _, err := service.CreateAsset(ctx, appassets.CreateAssetRequest{
		ProjectID: "project-1", Type: asset.TypeCharacter, Name: "沈砚",
	})
	if err != nil {
		t.Fatalf("creating the character asset: %v", err)
	}
	// "两个 candidate versions", written through the command a job's result uses.
	commitAcceptanceFile(t, repository, "aaaa", "bbbb")
	first, _, err := service.AttachJobResult(ctx, appassets.AttachJobResultRequest{
		AssetID: record.ID, JobID: "job-1", Prompt: "沈砚，冬装",
		Files: []appassets.AttachJobFile{{FileHash: acceptanceHash("aaaa"), Role: asset.RolePrimary}},
	})
	if err != nil {
		t.Fatalf("attaching the first candidate: %v", err)
	}
	second, _, err := service.AttachJobResult(ctx, appassets.AttachJobResultRequest{
		AssetID: record.ID, JobID: "job-2", Prompt: "沈砚，夏装",
		Files: []appassets.AttachJobFile{{FileHash: acceptanceHash("bbbb"), Role: asset.RolePrimary}},
	})
	if err != nil {
		t.Fatalf("attaching the second candidate: %v", err)
	}
	for index, version := range []asset.Version{first, second} {
		if version.Status != asset.VersionCandidate {
			t.Fatalf("candidate %d is %q, and the criterion says both are candidates", index+1, version.Status)
		}
	}
	// "批准 v1"
	if _, err := service.ApproveVersion(ctx, appassets.ApproveVersionRequest{
		VersionID: first.ID, ImpactAcknowledged: true,
	}); err != nil {
		t.Fatalf("approving v1: %v", err)
	}
	// "使用 v1 的 Shot" — recorded while v1 IS the version in force, which is the order the
	// criterion describes and the only order in which the usage exists: a shot stops using a
	// version by the switch, not before it.
	if _, err := service.AddUsage(ctx, appassets.AddUsageRequest{
		AssetVersionID: first.ID, ConsumerType: asset.ConsumerShot,
		ConsumerID: "shot-6", UsageRole: "costume", Required: true,
	}); err != nil {
		t.Fatalf("recording the shot's usage of v1: %v", err)
	}
	// "批准 v2 时 v1 superseded" — and the impact is read BEFORE the switch, which is what
	// section 8.2 requires: the analysis is what a user needs in order to decide.
	impact, err := service.ApprovalImpactOf(ctx, second.ID)
	if err != nil {
		t.Fatalf("reading the impact: %v", err)
	}
	found := false
	for _, consumer := range impact.RequiredConsumers {
		if consumer.ConsumerType == asset.ConsumerShot && consumer.ConsumerID == "shot-6" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the impact does not name the shot using v1: %+v", impact.RequiredConsumers)
	}
	if impact.Replaces != first.ID {
		t.Fatalf("the impact says it replaces %q, want v1", impact.Replaces)
	}
	if _, err := service.ApproveVersion(ctx, appassets.ApproveVersionRequest{
		VersionID: second.ID, ImpactAcknowledged: true,
	}); err != nil {
		t.Fatalf("approving v2: %v", err)
	}
	superseded, err := repository.GetVersion(ctx, first.ID)
	if err != nil {
		t.Fatalf("reading v1 after the switch: %v", err)
	}
	if superseded.Status != asset.VersionSuperseded {
		t.Fatalf("v1 is %q after approving v2, and the criterion says superseded", superseded.Status)
	}
	// "v1 不被删除" — the last clause, and the one a naive implementation fails by cleaning
	// up: the row is READ BACK rather than assumed.
	if _, err := repository.GetVersion(ctx, first.ID); err != nil {
		t.Fatalf("v1 is gone after the switch: %v", err)
	}
}

// seedBoardWithRows writes a board with one row per shot, and returns the version's id
// AND its rows'. Both are needed: the fix test names rows, and the shape test lists them.
func (c *productionCanary) seedBoardWithRows(t *testing.T, scriptVersionID string, shotIDs []string) (string, []string) {
	t.Helper()
	ctx := context.Background()
	plan, err := c.storyboard.CreateDirectorPlanVersion(ctx, appstoryboard.CreateDirectorPlanVersionRequest{
		EpisodeID: c.ids.episode, ScriptVersionID: scriptVersionID,
	})
	if err != nil {
		t.Fatalf("creating the plan: %v", err)
	}
	board, err := c.storyboard.EnsureStoryboard(ctx, c.ids.episode)
	if err != nil {
		t.Fatalf("ensuring the board: %v", err)
	}
	version, err := c.storyboard.CreateStoryboardVersion(ctx, appstoryboard.CreateStoryboardVersionRequest{
		StoryboardID: board.ID, ScriptVersionID: scriptVersionID,
		DirectorPlanVersionID: plan.ID, CreatedByType: versioning.CreatedByUser,
	})
	if err != nil {
		t.Fatalf("creating the board version: %v", err)
	}
	ids := make([]string, 0, len(shotIDs))
	for index, shotID := range shotIDs {
		row, err := c.storyboard.CreateStoryboardItem(ctx, appstoryboard.CreateStoryboardItemRequest{
			StoryboardVersionID: version.ID, ShotID: shotID, Ordinal: index + 1,
			ShotSize: "MS", DurationSeconds: 4,
			VisualDescription: "第 " + itoaAcceptance(index+1) + " 个镜头。",
			// The consistent costume is the DEFAULT here, so the fix test can change one row
			// and compare the rest.
			ContinuityNotes:        "沈砚穿的是夏装。",
			FirstFrameDescription:  "首帧",
			LastFrameDescription:   "尾帧",
			VideoMotionDescription: "轻微横移。",
		})
		if err != nil {
			t.Fatalf("creating row %d: %v", index+1, err)
		}
		ids = append(ids, row.ID)
	}
	return version.ID, ids
}

// commitAcceptanceFile stores one file object so an asset link has a hash to cite.
func commitAcceptanceFile(t *testing.T, repository *AssetRepository, seeds ...string) {
	t.Helper()
	db := repository.db
	for _, seed := range seeds {
		hash := acceptanceHash(seed)
		//lint:ignore ST1005 the fixture's own reason
		if _, err := db.ExecContext(context.Background(),
			`INSERT OR IGNORE INTO file_objects (hash, storage_key, mime_type, size_bytes, created_at)
			 VALUES (?, ?, 'image/png', 10, '2026-01-01T00:00:00Z')`, hash, hash); err != nil {
			t.Fatalf("committing %s: %v", hash, err)
		}
	}
}

// acceptanceHash builds a 64-character hash from a seed.
func acceptanceHash(seed string) string {
	out := ""
	for len(out) < 64 {
		out += seed
	}
	return out[:64]
}

// acceptanceTime is the fixtures' fixed timestamp.
func acceptanceTime() time.Time {
	return time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
}
