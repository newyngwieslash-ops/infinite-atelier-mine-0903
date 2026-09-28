package database

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	appassets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	appevents "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/events"
	appimporting "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/importing"
	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	appproduction "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/productionpipeline"
	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/filestore"
	infrajobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/jobs"
	infraproviders "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providers"
)

// acceptance_e2e002_test.go walks PRD section 19's AC-E2E-002 — 小说到分镜 — as ONE scenario.
//
// # Why one walk rather than the halves that already exist
//
// `canary_script_test.go` drives the script stages to an approved script and
// `canary_production_test.go` drives the production stages to an approved board. Both are real and
// both pass. NEITHER reads the artefact this criterion is decided by: the closing clause is
// 「所有结果可追溯到输入、模型、任务和版本」, and the read that grades it is the join the MP4
// export is built from — `storyboard_panel_versions.approved_image_asset_version_id`. The two
// halves therefore covered the criterion's middle and neither of its ends.
//
// The walk is composed the same way the canaries are: it starts from the SCRIPT canary, which
// carries the script chain's helpers, and adds the production pipeline, the importer, a job stack
// and the timeline read over the same database — so every service reads what the previous one
// wrote.
//
// # What running it found
//
// A defect, which is why the walk exists. `ApprovePanelImage` recorded the image and NOTHING wrote
// the panel's `status`, while the export's join carries `AND p.status = 'approved'` — so every
// approval recorded an image no read could see. With a previously approved panel on the row, the
// timeline did not report "no media": it reported the PREVIOUS version, so a user who approved a
// replacement frame would export the old one. The regression test is
// `panel_approval_regression_test.go`; this walk is what made the two ends meet.
//
// # What it does not do
//
// No paid provider: the mock text adapter answers the model calls and the mock image adapter
// produces the batch's bytes. No browser: the UI half is asserted by the frontend suite against the
// same commands through `panel-chain.ts`, which is where this repository splits the two.
func TestE2E002FromNovelToApprovedPanelImages(t *testing.T) {
	ctx := context.Background()
	walk := newE2E002Walk(t)
	episodeID := walk.ids.episode

	// The mock answers each stage with the write that stage owes. `MockScenarioToolCall` is the
	// scenario an EXECUTION agent runs under — it replies with a tool call the runtime then runs —
	// and a supervisor is switched back to `Normal` for its own calls, because a supervisor may not
	// write (AC-AGENT-001). The canary suites set this at their start for the same reason.
	walk.mock.SetScenario(infraproviders.MockScenarioToolCall)

	// --- 导入并拆章 ---------------------------------------------------------------------------
	//
	// The fixture is the canary drama's own text — 32,736 Chinese characters across five chapters —
	// handed to the service as BYTES, which is the path a desktop build takes and the one that
	// performs real decoding and chapter detection.
	source := readCanaryFixture(t, "source.md")
	if runes := len([]rune(string(source))); runes < 30000 {
		t.Fatalf("the fixture carries %d runes and AC-E2E-002 asks for 3 万汉字", runes)
	}
	// DocumentID is EMPTY, which is what creates a new document: a non-empty one CONTINUES an
	// existing document with another version, so naming one that does not exist yet fails on
	// "this story item no longer exists" rather than importing.
	imported, err := walk.imports.Import(ctx, appimporting.ImportRequest{
		ProjectID:    walk.ids.project,
		Name:         "渡口的铜牌",
		Format:       "md",
		DocumentType: "novel",
		Content:      source,
	})
	if err != nil {
		t.Fatalf("clause 1: importing the novel: %v", err)
	}
	if imported.CharCount < 30000 {
		t.Fatalf("clause 1: the import counted %d characters, want at least 30,000", imported.CharCount)
	}
	if len(imported.Chapters) < 3 {
		t.Fatalf("clause 1: the import detected %d chapters, and the criterion asks for at least three", len(imported.Chapters))
	}
	// Confirming the split is a USER act rather than an automatic one, which is what
	// 「章节拆分可人工修正并保存」 requires. The command answers with the confirmed rows, so the
	// read-back is its own answer rather than a second query.
	confirmed, err := walk.imports.ConfirmChapters(ctx, appimporting.ConfirmChaptersRequest{
		SourceDocumentVersionID: imported.Version.ID,
	})
	if err != nil {
		t.Fatalf("clause 1: confirming the chapter split: %v", err)
	}
	if len(confirmed) != len(imported.Chapters) {
		t.Fatalf("clause 1: %d chapters were detected and %d came back confirmed", len(imported.Chapters), len(confirmed))
	}
	t.Logf("clause 1: imported %d characters across %d chapters", imported.CharCount, len(confirmed))

	// --- 事件候选, and three episodes -----------------------------------------------------------
	//
	// The accepted events are written by the script chain's own first step (「事件候选」 is the
	// skeleton stage's input), so they are seeded THERE rather than here: seeding them twice would
	// collide on the ids, and seeding them where they are consumed is the ordering the stage's state
	// layer documents.
	if episodes := walk.createEpisodes(t, 3); len(episodes) != 3 {
		t.Fatalf("clause 3: %d episodes exist, want three", len(episodes))
	}

	// --- 骨架、策略、剧本, each supervised and gated ---------------------------------------------
	scriptVersionID := walk.runScriptChain(t, episodeID)
	if scriptVersionID == "" {
		t.Fatal("clause 4: the script chain produced no version")
	}
	t.Logf("clauses 4 and 5: episode 1's script %s is approved", scriptVersionID)

	// --- 资产清单 ------------------------------------------------------------------------------
	gapReportID := walk.runGapAnalysis(t, episodeID, scriptVersionID)

	// --- 至少 2 个角色、2 个场景 ------------------------------------------------------------------
	characters, scenes := walk.runAssetGeneration(t, episodeID, gapReportID)
	if len(characters) < 2 || len(scenes) < 2 {
		t.Fatalf("clause 7: the walk produced %d characters and %d scenes, and the criterion asks for two of each", len(characters), len(scenes))
	}
	t.Logf("clause 7: %d character assets and %d scene assets", len(characters), len(scenes))

	// --- 导演规划, which the board cites ---------------------------------------------------------
	planVersionID := walk.runDirectorPlan(t, episodeID, scriptVersionID)

	// --- 不少于 12 个镜头的分镜表 ----------------------------------------------------------------
	boardVersionID := walk.runStoryboardTable(t, episodeID, scriptVersionID, planVersionID)
	shots := walk.boardRowCount(t, boardVersionID)
	if shots < 12 {
		t.Fatalf("clause 8: the board has %d shots and the criterion asks for at least twelve", shots)
	}

	// --- 生成分镜图: the chain this walk exists for ------------------------------------------------
	approvedImages := walk.runPanelImageChain(t, episodeID, boardVersionID)
	if len(approvedImages) != shots {
		t.Fatalf("clause 8: %d of %d shots have an approved panel image", len(approvedImages), shots)
	}

	// --- 所有结果可追溯到输入、模型、任务和版本 -------------------------------------------------------
	walk.assertTraceable(t, episodeID, approvedImages)

	t.Logf("the walk produced %d characters, %d scenes, %d shots and %d approved panel images",
		len(characters), len(scenes), shots, len(approvedImages))
}

// readCanaryFixture reads one of the canary drama's files.
//
// The path is relative to this package, three levels below the repository root, and the fixture is
// the one the canary suites already use — so this walk runs on the text those suites were written
// against rather than on a second copy that could drift from it.
func readCanaryFixture(t *testing.T, name string) []byte {
	t.Helper()
	return []byte(readFile(t, filepath.Join("..", "..", "..", "testdata", "canary-drama", name)))
}

// e2e002Walk is the scenario's composition: the script canary plus the production pipeline, the
// importer, a job stack and the timeline read over the same database.
type e2e002Walk struct {
	*scriptCanary
	imports    *appimporting.Service
	production *appproduction.Service
	jobs       *appjobs.Service
	timeline   *appmedia.TimelineService
	assets     *appassets.Service
}

// newE2E002Walk composes the stack.
//
// It starts from `newScriptCanary`, which already wires the mock text adapter, the runtime, the
// engine, the four drama services AND the script pipeline over one migrated database: a second
// composition would be a second thing to keep in step with production. What this adds is what the
// criterion needs beyond that canary — a job stack so the image batch EXECUTES, the production
// pipeline over it, the importer, and the timeline read.
func newE2E002Walk(t *testing.T) *e2e002Walk {
	t.Helper()
	base := newScriptCanary(t)

	// The import service, over a document store that commits through the real file store: the
	// fixture is content-addressed and written the way a desktop build writes it.
	root := t.TempDir()
	store, err := filestore.New(filepath.Join(root, "files"), filepath.Join(root, "temp"))
	if err != nil {
		t.Fatalf("opening the file store: %v", err)
	}
	imports := appimporting.NewService(appimporting.Options{
		Store: e2e002DocumentStore{store: store, files: NewFileRepository(base.db)},
		Story: base.story,
		Events: appevents.NewService(appevents.Options{
			Repository: NewEventRepository(base.db), Clock: canaryClock{}, IDs: newTestIDGenerator(),
		}),
		Clock: canaryClock{},
	})

	// The job stack. The image adapter is supplied through the runner's own `AdapterSource` port
	// rather than through a provider row, because migration 000003's `provider_configs.kind` CHECK
	// admits only openai_compatible, gemini_compatible and mock_media: there is no `mock_image` kind
	// to configure, and that guardrail is worth more than a fixture's convenience. Which adapter
	// answers is not what this walk grades — that an image job RUNS and produces a file the
	// collection can turn into a candidate is.
	images := infraproviders.NewMockImageAdapter()
	resultStore := infrajobs.NewResultStore(store, NewFileRepository(base.db), NewFileReferenceRepository(base.db))
	runner := infrajobs.NewRunner(e2e002AdapterSource{images: images}, resultStore, nil, 512<<20)
	jobService := appjobs.NewService(appjobs.Options{
		Repository: NewJobRepository(base.db),
		Clock:      appjobs.NewClockFunc(func() time.Time { return time.Now().UTC() }),
		IDs:        e2e002JobIDs{inner: newTestIDGenerator()},
		Runner:     runner,
		Policy:     job.DefaultRetryPolicy(),
	})

	// The production pipeline, over the same services the script pipeline uses, so the board it
	// writes cites the script and plan the earlier stages approved — and over the job service, which
	// is what its image batch submits through.
	production := appproduction.New(appproduction.Options{
		Engine: base.engine, Runtime: base.runtime, Workflow: base.workflow,
		Assembly: base.assembly, Runs: base.repo,
		Storyboard: base.storyboard, Assets: base.assets,
		Gaps: appassets.NewGapService(appassets.GapOptions{
			Gaps: NewAssetRepository(base.db), Clock: canaryClock{}, IDs: newTestIDGenerator(),
		}),
		Jobs: jobService,
	})

	return &e2e002Walk{
		scriptCanary: base,
		imports:      imports,
		production:   production,
		jobs:         jobService,
		timeline:     appmedia.NewTimelineService(appmedia.TimelineOptions{Board: NewExportRepository(base.db)}),
		assets:       base.assets,
	}
}

// createEpisodes creates count episodes and returns their ids in order.
//
// 「创建 3 集规划」 is DATA rather than a model's output (ADR-0017 ruling 8): the PRD describes no
// planning artefact for the episode split, and inventing an agent stage to produce one would be
// this test writing a requirement the specification does not state. The ruling is the most
// rebuttable in that record, and changing it touches this function and nothing else.
func (w *e2e002Walk) createEpisodes(t *testing.T, count int) []string {
	t.Helper()
	ctx := context.Background()
	ids := make([]string, 0, count)
	for index := 1; index <= count; index++ {
		// Episode 1 already exists: the drama seed writes it, and the schema's unique constraint
		// on (project, season, episode) would refuse a second for a reason unrelated to this walk.
		if index == 1 {
			ids = append(ids, w.ids.episode)
			continue
		}
		episode, err := w.script.CreateEpisode(ctx, appscript.CreateEpisodeRequest{
			ProjectID: w.ids.project, SeasonNumber: 1, EpisodeNumber: index,
			Title: "第" + itoaE2E002(index) + "集",
		})
		if err != nil {
			t.Fatalf("clause 3: creating episode %d: %v", index, err)
		}
		ids = append(ids, episode.ID)
	}
	return ids
}

// itoaE2E002 renders a small integer, so this file needs no format verb for one.
func itoaE2E002(value int) string {
	if value <= 0 {
		return "0"
	}
	digits := []byte{}
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}

// boardRowCount counts the rows a board version holds, through the real service read.
func (w *e2e002Walk) boardRowCount(t *testing.T, boardVersionID string) int {
	t.Helper()
	items, err := w.storyboard.ListStoryboardItems(context.Background(), boardVersionID)
	if err != nil {
		t.Fatalf("clause 8: counting the board's rows: %v", err)
	}
	return len(items)
}
