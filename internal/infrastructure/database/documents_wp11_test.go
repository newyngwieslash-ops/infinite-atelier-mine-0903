package database

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"

	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	domainmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/media"
)

// documents_wp11_test.go grades ROADMAP item 11's four documents over the REAL schema.
//
//  11. Script/Storyboard/Subtitle/Manifest Export；
//
// The subtitle half is covered by `subtitle_wp11_test.go`; this file is the other three — the script,
// the shot list, and the manifest as a file a user can take away — driven through the service and the
// adapter that the composition root builds, not through a double.
//
// # Why the adapter is exercised rather than the renderers alone
//
// `domain/screenplay` and `domain/shotlist` have their own tests, and they grade a FORMAT. What they
// cannot see is whether the facts reach them: which version is in force, which panel a row points at,
// and what the newest export recorded. An earlier package in this repository shipped a Final Ruleset
// whose SQL named columns that do not exist, with every renderer-adjacent test green — so these go
// through the joins.
func newDocumentHarness(t *testing.T) (*appmedia.DocumentService, *mediaHarness) {
	t.Helper()
	harness := newMediaHarness(t)
	service := appmedia.NewDocumentService(appmedia.DocumentOptions{
		Repository: NewDocumentFactsReader(harness.db),
	})
	return service, harness
}

// TestACDOC001TheScriptExportsOverTheRealSchema is the script half.
func TestACDOC001TheScriptExportsOverTheRealSchema(t *testing.T) {
	ctx := context.Background()
	service, harness := newDocumentHarness(t)
	// A version with one scene, four line types and a shot, written through the real repositories so
	// the joins are the ones a production uses.
	seedScriptForDocuments(t, harness)

	document, err := service.ExportScript(ctx, appmedia.ScriptRequest{
		DocumentRequest: appmedia.DocumentRequest{EpisodeID: "drama-episode"},
		Format:          "txt",
	})
	if err != nil {
		// THE ASSERTION THIS FILE EXISTS FOR: a statement that names a column the schema does not have
		// fails here, which is the shape that was missing when the Final Ruleset shipped inert.
		t.Fatalf("ExportScript: %v", err)
	}
	if document.Name != "script" || document.Extension != ".txt" {
		t.Fatalf("the document is %q with extension %q", document.Name, document.Extension)
	}
	if !strings.Contains(document.SuggestedName, "script") || strings.ContainsAny(document.SuggestedName, `/\`) {
		t.Fatalf("the suggested name is %q, which is either not a script or not a bare filename", document.SuggestedName)
	}
	for _, want := range []string{
		// The project and episode the join reached, and the version number that came off the approved
		// row rather than off a constant.
		"渡口", "第 1 季 第 1 集 试点", "版本 v1", "场景 1 个",
		// The slugline, built from the scene's own marking.
		"1. EXT. 渡口 - 夜",
		// Every line type, which is what says the structure's lines arrived rather than only its scenes.
		"沈砚", "灯还亮着。", "旁白：那年的冬天格外长。", "CUT TO:", "[注] 此处需要补拍。", "煤灯在风里晃。",
	} {
		if !strings.Contains(document.Text, want) {
			t.Fatalf("the script does not contain %q:\n%s", want, document.Text)
		}
	}

	// The Fountain format is a different document from the same facts, and it is the machine-readable
	// one: the forced heading is what a screenwriting tool reads.
	fountain, err := service.ExportScript(ctx, appmedia.ScriptRequest{
		DocumentRequest: appmedia.DocumentRequest{EpisodeID: "drama-episode"},
		Format:          "fountain",
	})
	if err != nil {
		t.Fatalf("ExportScript(fountain): %v", err)
	}
	if fountain.Extension != ".fountain" {
		t.Fatalf("the Fountain document's extension is %q", fountain.Extension)
	}
	if !strings.Contains(fountain.Text, ".1. EXT. 渡口 - 夜") {
		t.Fatalf("the Fountain document has no forced scene heading:\n%s", fountain.Text)
	}
}

// TestACDOC001AScriptWithNoApprovedVersionIsRefused covers the guard, and it is the refactor that
// matters: "no approved script" and "a blank script" must not render the same document.
func TestACDOC001AScriptWithNoApprovedVersionIsRefused(t *testing.T) {
	ctx := context.Background()
	service, harness := newDocumentHarness(t)
	// An episode with a script VERSION that is still a draft. `dramaSeedParents` writes exactly that.
	_ = harness
	_, err := service.ExportScript(ctx, appmedia.ScriptRequest{
		DocumentRequest: appmedia.DocumentRequest{EpisodeID: "drama-episode"},
		Format:          "txt",
	})
	if err == nil {
		t.Fatal("an episode with no approved script version produced a document")
	}
	mediaErr, isMedia := appmedia.AsError(err)
	if !isMedia {
		t.Fatalf("the refusal is a %T, want a media error", err)
	}
	// The CATEGORY is the assertion, and it is the one that distinguishes this refusal from a broken
	// read: `invalid_input` says the episode has no approved script, and `storage` would say the store
	// could not be reached — a different problem with a different fix.
	if mediaErr.Category != appmedia.CategoryInvalidInput {
		t.Fatalf("the refusal is %s, want an invalid-input refusal rather than a storage failure", mediaErr.Category)
	}
	if !strings.Contains(mediaErr.SafeMessage, "approved") {
		t.Fatalf("the refusal reads %q, which does not say what is missing", mediaErr.SafeMessage)
	}

	// An episode that does not exist is a NOT-FOUND rather than a blank document, which is the other
	// half of the same distinction.
	if _, err := service.ExportScript(ctx, appmedia.ScriptRequest{
		DocumentRequest: appmedia.DocumentRequest{EpisodeID: "no-such-episode"},
		Format:          "txt",
	}); err == nil {
		t.Fatal("an episode that does not exist produced a document")
	}

	// And a format this build does not write is refused before anything is read.
	if _, err := service.ExportScript(ctx, appmedia.ScriptRequest{
		DocumentRequest: appmedia.DocumentRequest{EpisodeID: "drama-episode"},
		Format:          "pdf",
	}); err == nil {
		t.Fatal("a PDF script was accepted")
	}
	// The empty format too: a caller that named none has not said what to write.
	if _, err := service.ExportScript(ctx, appmedia.ScriptRequest{
		DocumentRequest: appmedia.DocumentRequest{EpisodeID: "drama-episode"},
	}); err == nil {
		t.Fatal("a script with no format was accepted")
	}
}

// TestACDOC001TheShotListExportsOverTheRealSchema is the storyboard half.
func TestACDOC001TheShotListExportsOverTheRealSchema(t *testing.T) {
	ctx := context.Background()
	service, harness := newDocumentHarness(t)
	harness.approvedBoard(t, 3, 4)

	document, err := service.ExportShotList(ctx, appmedia.ShotListRequest{
		DocumentRequest: appmedia.DocumentRequest{EpisodeID: "drama-episode"},
		Format:          "csv",
	})
	if err != nil {
		t.Fatalf("ExportShotList: %v", err)
	}
	if document.Extension != ".csv" {
		t.Fatalf("the CSV document's extension is %q", document.Extension)
	}
	records, err := csv.NewReader(strings.NewReader(document.Text)).ReadAll()
	if err != nil {
		t.Fatalf("the shot list is not readable CSV: %v", err)
	}
	if len(records) != 4 {
		t.Fatalf("the shot list has %d rows, want a header and three shots", len(records))
	}
	// The PANEL VERSION and the approved media travel, which is what makes a printed row traceable
	// back to the frame it refers to. The fixture approves a panel for every row, so none is missing.
	header := records[0]
	columns := map[string]int{}
	for index, name := range header {
		columns[name] = index
	}
	for _, required := range []string{"ordinal", "shotId", "durationSeconds", "panelVersionId", "approvedVersionId", "missingMedia"} {
		if _, present := columns[required]; !present {
			t.Fatalf("the shot list has no %s column", required)
		}
	}
	for index, record := range records[1:] {
		if record[columns["approvedVersionId"]] == "" {
			t.Fatalf("shot %d has no approved version in the document, although the fixture approves one", index+1)
		}
		if record[columns["missingMedia"]] != "false" {
			t.Fatalf("shot %d reports missingMedia=%q", index+1, record[columns["missingMedia"]])
		}
		// The board's own row length arrived rather than a constant.
		if record[columns["durationSeconds"]] != "4" {
			t.Fatalf("shot %d has duration %q, want the board's 4", index+1, record[columns["durationSeconds"]])
		}
	}
	// The ordinals are the board's own order, which is the property AC-MEDIA-003's first clause is
	// about and the one a scheduler depends on.
	for index, record := range records[1:] {
		if record[columns["ordinal"]] != itoaWP10(index+1) {
			t.Fatalf("row %d carries ordinal %q", index, record[columns["ordinal"]])
		}
	}

	// The TEXT format is a different rendering of the same rows, and it states the totals.
	text, err := service.ExportShotList(ctx, appmedia.ShotListRequest{
		DocumentRequest: appmedia.DocumentRequest{EpisodeID: "drama-episode"},
		Format:          "txt",
	})
	if err != nil {
		t.Fatalf("ExportShotList(txt): %v", err)
	}
	if !strings.Contains(text.Text, "镜头 3 个，合计 12 秒") {
		t.Fatalf("the text shot list does not total the board's rows:\n%s", text.Text)
	}

	// An episode with no approved board is refused rather than producing an empty table.
	if _, err := service.ExportShotList(ctx, appmedia.ShotListRequest{
		DocumentRequest: appmedia.DocumentRequest{EpisodeID: "episode-with-no-board"},
		Format:          "csv",
	}); err == nil {
		t.Fatal("an episode with no board produced a shot list")
	}
}

// TestACDOC001TheManifestExportsAsAFile is AC-MEDIA-003's traceability clause as a document.
//
// The manifest was reachable only as a block of text in the UI's expandable row; `save_dialog.go`'s
// `.json` filter had NO caller, which is the shape this test closes. It is the assertion that a user
// can hand the manifest to somebody.
func TestACDOC001TheManifestExportsAsAFile(t *testing.T) {
	ctx := context.Background()
	service, harness := newDocumentHarness(t)
	boardVersionID := harness.approvedBoard(t, 2, 4)
	requireMediaEngine(t, harness)
	if _, _, err := harness.service.Export(ctx, exportRequestFor(boardVersionID)); err != nil {
		t.Fatalf("Export: %v", err)
	}

	document, err := service.ExportManifest(ctx, appmedia.DocumentRequest{EpisodeID: "drama-episode"})
	if err != nil {
		t.Fatalf("ExportManifest: %v", err)
	}
	if document.Extension != ".json" || document.Name != "manifest" {
		t.Fatalf("the document is %q with extension %q", document.Name, document.Extension)
	}
	// The bytes are a MANIFEST, decoded with the domain's own reader: a file whose shape this build
	// cannot read would be refused rather than written out looking official.
	manifest, err := domainmedia.DecodeManifest(document.Text)
	if err != nil {
		t.Fatalf("the exported manifest is not readable: %v", err)
	}
	if manifest.EpisodeID != "drama-episode" {
		t.Fatalf("the manifest names episode %q", manifest.EpisodeID)
	}
	if manifest.SchemaVersion != domainmedia.ManifestSchemaVersion {
		t.Fatalf("the manifest's schema version is %d", manifest.SchemaVersion)
	}
	// It is JSON, and it parses as a document rather than as a string wrapping one.
	var decoded map[string]any
	if err := json.Unmarshal([]byte(document.Text), &decoded); err != nil {
		t.Fatalf("the manifest is not a JSON object: %v", err)
	}
	if _, present := decoded["references"]; !present {
		t.Fatalf("the manifest carries no references: %v", decoded)
	}

	// An episode with no export is refused, because "no export" and "an export with a blank manifest"
	// are different situations and only one of them is fixable by exporting.
	if _, err := service.ExportManifest(ctx, appmedia.DocumentRequest{EpisodeID: "episode-with-no-export"}); err == nil {
		t.Fatal("an episode with no export produced a manifest")
	}
}

// TestTheDocumentServiceFailsClosedWhenUnattached covers the composition guard.
func TestTheDocumentServiceFailsClosedWhenUnattached(t *testing.T) {
	ctx := context.Background()
	service := appmedia.NewDocumentService(appmedia.DocumentOptions{})
	if service.Available() {
		t.Fatal("a service with no repository reports itself available")
	}
	if _, err := service.ExportScript(ctx, appmedia.ScriptRequest{
		DocumentRequest: appmedia.DocumentRequest{EpisodeID: "e"}, Format: "txt",
	}); err == nil {
		t.Fatal("an unattached service exported a script")
	}
	if _, err := service.ExportShotList(ctx, appmedia.ShotListRequest{
		DocumentRequest: appmedia.DocumentRequest{EpisodeID: "e"}, Format: "csv",
	}); err == nil {
		t.Fatal("an unattached service exported a shot list")
	}
	if _, err := service.ExportManifest(ctx, appmedia.DocumentRequest{EpisodeID: "e"}); err == nil {
		t.Fatal("an unattached service exported a manifest")
	}
	// And an empty episode identifier is refused before a repository is touched, which is the guard
	// that keeps a blank request from reading every episode.
	attached := appmedia.NewDocumentService(appmedia.DocumentOptions{Repository: NewDocumentFactsReader(nil)})
	if _, err := attached.ExportScript(ctx, appmedia.ScriptRequest{Format: "txt"}); err == nil {
		t.Fatal("an export with no episode identifier was accepted")
	}
}

// seedScriptForDocuments writes an approved script version with one scene and every line type.
//
// It goes through the repositories rather than through raw INSERTs for the parents, because the
// service's joins are what this file grades: a fixture that wrote its own rows would prove the test
// can write, not that the adapter can read.
func seedScriptForDocuments(t *testing.T, harness *mediaHarness) {
	t.Helper()
	ctx := context.Background()
	statements := []string{
		// The episode points at the version, and the version is APPROVED, which is the pair the
		// adapter resolves.
		`INSERT INTO scenes (id, script_version_id, ordinal, scene_number, slugline, interior_exterior,
			time_of_day, summary, dramatic_goal, estimated_duration_seconds, created_at, updated_at, revision)
		 VALUES ('doc-scene', 'drama-script-version', 1, '1', '渡口', 'EXT', '夜',
			'沈砚回到渡口。', '让他决定留下。', 4, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
	}
	lines := []struct{ id, kind, character, text, note string }{
		{"doc-line-1", "action", "", "煤灯在风里晃。", ""},
		{"doc-line-2", "dialogue", "沈砚", "灯还亮着。", "低声"},
		{"doc-line-3", "narration", "", "那年的冬天格外长。", ""},
		{"doc-line-4", "transition", "", "CUT TO:", ""},
		{"doc-line-5", "note", "", "此处需要补拍。", ""},
	}
	for index, line := range lines {
		statements = append(statements, `INSERT INTO dialogue_lines
			(id, scene_id, ordinal, line_type, character_entity_id, text, performance_note, created_at, updated_at, revision)
			VALUES ('`+line.id+`', 'doc-scene', `+itoaWP10(index+1)+`, '`+line.kind+`', '`+line.character+
			`', '`+line.text+`', '`+line.note+`', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`)
	}
	statements = append(statements,
		`INSERT INTO shots (id, scene_id, ordinal, shot_number, shot_size, camera_angle, camera_movement,
			estimated_duration_seconds, visual_description, created_at, updated_at, revision)
		 VALUES ('doc-shot-1', 'doc-scene', 1, '1', 'MS', 'eye level', 'static', 4,
			'煤灯在画面左侧。', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
		// The APPROVAL is what the readers join through: `current_script_version_id` is written by no
		// production path, so a fixture that set it would be testing a state no build produces.
		`UPDATE script_versions SET status = 'approved', estimated_duration_seconds = 4
		 WHERE id = 'drama-script-version'`,
		`UPDATE episodes SET title = '试点' WHERE id = 'drama-episode'`,
		`UPDATE projects SET name = '渡口' WHERE id = 'drama-project'`,
	)
	for _, statement := range statements {
		if _, err := harness.db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seeding the document fixture failed: %v\n%s", err, statement)
		}
	}
}
