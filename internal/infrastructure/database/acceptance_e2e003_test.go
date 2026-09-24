package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	appassets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	appproduction "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/productionpipeline"
	appprojects "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/projects"
	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	appstoryboard "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/storyboard"
	appworkflow "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/workflow"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/filestore"
	infrajobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/jobs"
	infraproviders "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providers"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/platform/id"
)

// acceptance_e2e003_test.go walks AC-E2E-003 (PRD section 19) as ONE forced restart.
//
//	## AC-E2E-003 中断恢复
//
//	在视频任务和 Storyboard 工作流运行时强制关闭应用：
//
//	- 重启后项目可打开；
//	- Workflow 显示真实阶段；
//	- 可恢复任务继续查询；
//	- 不可恢复任务明确失败并可重试；
//	- 不产生重复资产。
//
// # Why the package needed this, and why the parts' own tests could not replace it
//
// All five clauses are covered IN PIECES, and each piece is green: `video_restart_test.go` stages a
// `waiting_remote` job on an in-memory repository and proves it is polled rather than re-submitted;
// `recovery_test.go` walks `recoveryDisposition` state by state; `result_pipeline_test.go` proves a
// committed result leaves one object and one owner reference. What none of them does is walk the FIVE
// TOGETHER over the REAL SCHEMA and the assembled job stack, and that is where this criterion's defects
// live: the clauses are one chain through one restart, so a change that moved a row correctly and
// consumed it wrongly would leave every part's suite green. `acceptance_wp11_e2e_test.go` makes the same
// argument for the media chain, and this walk follows its shape — real services, real repositories,
// assertions on what the DATABASE holds afterwards.
//
// # What "a forced close" can and cannot be from inside one test
//
// The subject is the DATABASE FILE and the rows in it, so the crash is modelled the only way it can be
// modelled honestly from a live process: the work happens over a real database file, the process that
// owns it is then ended — the scheduler stopped and the handle closed, which is what a process exit does
// to a SQLite database — and everything afterwards is composed FRESH over that file the way
// `composeJobs` composes at startup: recovery first, then the scheduler.
//
// The rows the restart finds are produced by REAL code rather than staged, with two exceptions that are
// named where they are written:
//
//   - the parked video job is submitted, accepted and parked by the REAL runner against the REAL mock
//     adapter resolved through the REAL registry, so its remote handle and its stored input are the
//     provider's own answers;
//   - both stage attempts are driven by the REAL workflow service through its own state machine;
//   - the dead attempt under the unrecoverable job, and that job's own state, are staged — a live
//     process cannot leave them, because the worker that owns a claimed row always settles it or hands
//     it back. See the fixture's comment.

// e2e003World is what SURVIVES the restart and is deliberately not rebuilt with it.
//
// The file store's bytes are the user's files on disk, and the mock adapter stands in for a REMOTE
// provider, which does not restart when the desktop app does. The counting wrapper therefore outlives
// the process it observes, which is exactly what makes "never re-submitted" a statement about the whole
// scenario rather than about one process's view of it.
type e2e003World struct {
	dbPath    string
	snapshots string
	store     *filestore.Store
	video     *e2e003CountingVideo
	ids       *id.Generator
	// mediaProviderID is the provider configuration the video jobs name.
	mediaProviderID string
}

// e2e003Process is the stack of ONE process: what the composition root builds, rebuilt over the same
// file after the restart.
type e2e003Process struct {
	world      *e2e003World
	handle     *Handle
	db         *sql.DB
	jobs       *appjobs.Service
	jobRepo    *JobRepository
	projects   *appprojects.Service
	script     *appscript.Service
	workflow   *appworkflow.Service
	storyboard *appstoryboard.Service
	assets     *appassets.Service
	production *appproduction.Service
}

// e2e003CountingVideo counts the calls the job stack makes to the provider.
//
// It wraps the REAL adapter and changes none of its answers. A count is the only honest way to grade
// "可恢复任务继续查询": a resumed job must POLL, and a job that was re-submitted is a second billable
// generation, which is the failure the whole recovery path exists to prevent.
type e2e003CountingVideo struct {
	inner appjobs.VideoPort

	mu      sync.Mutex
	submits int
	polls   int
}

func (c *e2e003CountingVideo) Submit(ctx context.Context, request appjobs.VideoRequest) (appjobs.RemoteJob, error) {
	c.mu.Lock()
	c.submits++
	c.mu.Unlock()
	return c.inner.Submit(ctx, request)
}

func (c *e2e003CountingVideo) Poll(ctx context.Context, remote appjobs.RemoteJob) (appjobs.RemoteStatus, error) {
	c.mu.Lock()
	c.polls++
	c.mu.Unlock()
	return c.inner.Poll(ctx, remote)
}

func (c *e2e003CountingVideo) Fetch(ctx context.Context, remote appjobs.RemoteJob) (appjobs.MediaOutcome, error) {
	return c.inner.Fetch(ctx, remote)
}

func (c *e2e003CountingVideo) Cancel(ctx context.Context, remote appjobs.RemoteJob) error {
	return c.inner.Cancel(ctx, remote)
}

// counts reports the totals so far, across every process the world has hosted.
func (c *e2e003CountingVideo) counts() (submits, polls int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.submits, c.polls
}

// e2e003PrefixIDs adapts the platform generator to the job service's port, as the composition root's
// own `idGenerator` does.
type e2e003PrefixIDs struct{ inner *id.Generator }

func (g e2e003PrefixIDs) NewID(prefix string) string {
	value, err := g.inner.New()
	if err != nil {
		// The only failure is entropy, and an identifier with zeroed randomness would be a collision
		// rather than a degraded id.
		panic(err)
	}
	return prefix + "-" + value
}

// e2e003Clock is the fixed clock the drama services are composed with.
//
// The JOB stack deliberately uses the wall clock instead: its leases and its poll pacing are DURATIONS,
// and a frozen clock would leave a parked job unclaimable and a crashed lease unexpired forever.
type e2e003Clock struct{}

func (e2e003Clock) Now() time.Time { return time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC) }

// e2e003PollInterval paces a parked video poll.
//
// It is shorter than the production five seconds purely to keep the walk quick; the pacing itself is the
// production mechanism (`next_retry_at`), and it is long enough that the first process cannot poll a
// parked job to completion before the crash.
const e2e003PollInterval = 1 * time.Second

// newE2E003World builds the two things a restart does not recreate.
func newE2E003World(t *testing.T) *e2e003World {
	t.Helper()
	root := t.TempDir()
	store, err := filestore.New(filepath.Join(root, "files"), filepath.Join(root, "temp"))
	if err != nil {
		t.Fatalf("opening the file store: %v", err)
	}
	return &e2e003World{
		dbPath:          filepath.Join(root, "app.db"),
		snapshots:       filepath.Join(root, "snapshots"),
		store:           store,
		video:           &e2e003CountingVideo{inner: infraproviders.NewMockVideoAdapter()},
		ids:             newTestIDGenerator(),
		mediaProviderID: "e2e003-media",
	}
}

// open composes one process over the world's database file.
//
// It goes through `Open`, the production entry point, so the integrity check and the migration gate a
// restarted application performs are the ones this walk performs.
func (w *e2e003World) open(t *testing.T) *e2e003Process {
	t.Helper()
	ctx := context.Background()
	handle, err := Open(ctx, w.dbPath, w.snapshots)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if handle.Mode() != ModeReady {
		t.Fatalf("the database reopened in %q mode: %v", handle.Mode(), handle.Err())
	}
	db := handle.SQL()
	clock := e2e003Clock{}

	// The provider the video jobs run against: a REAL configuration row of the mock kind, resolved by the
	// REAL registry, so the runner reaches an adapter exactly as a build does. `mock_media` is the one
	// media kind migration 000003's CHECK admits and the kind the composition root registers; the mock
	// adapter itself is the same one `provider_wiring.go` passes to `WithMediaAdapters`.
	providers := NewProviderRepository(db)
	if err := providers.SaveConfig(ctx, provider.Config{
		ID: w.mediaProviderID, Kind: provider.KindMockMedia, DisplayName: "E2E mock media",
		BaseURL: "mock://media", SecretRef: "e2e003-secret", Enabled: true,
		Revision: 1, CreatedAt: clock.Now(), UpdatedAt: clock.Now(),
	}); err != nil {
		t.Fatalf("saving the media provider config: %v", err)
	}
	registry := infraproviders.NewRegistry(providers, refusingSecrets{}, providers).
		WithMediaAdapters(w.video, infraproviders.NewMockAudioAdapter())

	resultStore := infrajobs.NewResultStore(w.store, NewFileRepository(db), NewFileReferenceRepository(db))
	jobService := appjobs.NewService(appjobs.Options{
		Repository:         NewJobRepository(db),
		Clock:              appjobs.NewClockFunc(func() time.Time { return time.Now().UTC() }),
		IDs:                e2e003PrefixIDs{inner: w.ids},
		Runner:             infrajobs.NewRunner(registry, resultStore, nil, 512<<20),
		Policy:             job.DefaultRetryPolicy(),
		RemotePollInterval: e2e003PollInterval,
	})

	projectService := appprojects.NewService(appprojects.Options{
		Projects: NewProjectRepository(db), Canvas: NewCanvasRepository(db), Clock: clock, IDs: w.ids,
	})
	scriptService := appscript.NewService(appscript.Options{
		Repository: NewScriptRepository(db), Clock: clock, IDs: w.ids,
	})
	workflowService := appworkflow.NewService(appworkflow.Options{
		Runs: NewWorkflowRepository(db), Stages: NewWorkflowRepository(db),
		Reviews: NewWorkflowRepository(db), Decisions: NewWorkflowRepository(db),
		Events: NewWorkflowRepository(db), Clock: clock, IDs: w.ids,
	})
	storyboardService := appstoryboard.NewService(appstoryboard.Options{
		DirectorPlans: NewStoryboardRepository(db), Storyboards: NewStoryboardRepository(db),
		Items: NewStoryboardRepository(db), Panels: NewStoryboardRepository(db),
		Clock: clock, IDs: w.ids,
	})
	assetService := appassets.NewService(appassets.Options{
		Repository: NewAssetRepository(db), Clock: clock, IDs: w.ids,
	})
	production := appproduction.New(appproduction.Options{
		Storyboard: storyboardService,
		Gaps: appassets.NewGapService(appassets.GapOptions{
			Gaps: NewAssetRepository(db), Clock: clock, IDs: w.ids,
		}),
		Jobs: jobService, Assets: assetService,
	})
	return &e2e003Process{
		world: w, handle: handle, db: db, jobs: jobService, jobRepo: NewJobRepository(db),
		projects: projectService, script: scriptService, workflow: workflowService,
		storyboard: storyboardService, assets: assetService, production: production,
	}
}

// recover runs the startup scan where `jobWiring.start` runs it: before the scheduler, so a job left
// mid-flight by a previous process is reconciled before new work is dispatched.
func (p *e2e003Process) recover(t *testing.T) appjobs.RecoveryReport {
	t.Helper()
	report, err := p.jobs.Recover(context.Background())
	if err != nil {
		t.Fatalf("the startup recovery scan failed: %v", err)
	}
	return report
}

// close ends the process: scheduler first, then the database, the order `jobWiring.shutdown` uses.
func (p *e2e003Process) close(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.jobs.Stop(ctx); err != nil {
		t.Fatalf("the job scheduler did not stop: %v", err)
	}
	if err := p.handle.Close(ctx); err != nil {
		t.Fatalf("closing the database: %v", err)
	}
}

// job returns one job row from the REAL repository.
func (p *e2e003Process) job(t *testing.T, id string) job.Job {
	t.Helper()
	record, err := p.jobRepo.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("reading job %s: %v", id, err)
	}
	return record
}

// waitForJobStatus polls the real repository until the job reaches the wanted status.
func (p *e2e003Process) waitForJobStatus(t *testing.T, id string, want job.Status) job.Job {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var last job.Job
	for time.Now().Before(deadline) {
		last = p.job(t, id)
		if last.Status == want {
			return last
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("job %s reached %q, not %q, within thirty seconds", id, last.Status, want)
	return job.Job{}
}

// parkVideoJob submits a video job and lets the REAL scheduler drive it to `waiting_remote`, then stops
// handing out work and waits for the row to settle.
//
// The pause before the stop is what makes the fixture deterministic: a parked job with no
// `next_retry_at` is immediately reclaimable, so without it the scheduler could claim the row again and
// be interrupted mid-poll by the shutdown, which would settle the job as `cancelled` instead of leaving
// it parked for the restart to resume.
func (p *e2e003Process) parkVideoJob(t *testing.T, request appjobs.SubmitRequest) job.Job {
	t.Helper()
	ctx := context.Background()
	submitted, _, err := p.jobs.Submit(ctx, request)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	p.jobs.Start(ctx, 1, 5*time.Millisecond)
	parked := p.waitForJobStatus(t, submitted.ID, job.StatusWaitingRemote)
	p.jobs.Pause()
	deadline := time.Now().Add(5 * time.Second)
	for {
		settled := p.job(t, submitted.ID)
		if settled.Status == job.StatusWaitingRemote && settled.LeaseOwner == "" {
			parked = settled
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the parked job never settled: %+v", settled)
		}
		time.Sleep(2 * time.Millisecond)
	}
	if err := p.jobs.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	return parked
}

// driveStage drives one stage attempt to the wanted status, one legal edge at a time, through the REAL
// workflow service, and returns the row the machine wrote.
//
// What is driven is the ATTEMPT's lifecycle, not an agent run: this package has no model double for the
// agent chain (the canary in this package owns that, and its subject is the agent chain rather than a
// restart). A stage left at `running` with a `started_at` is exactly the row `stagepipeline.RunStage`
// leaves when the process dies between moving the attempt to running and parking it for review, so the
// state the restart reads is the production one even though the model call in the middle is absent.
func (p *e2e003Process) driveStage(t *testing.T, runID, stage string, attempt int, to ...workflow.StageStatus) workflow.StageRun {
	t.Helper()
	ctx := context.Background()
	record, err := p.workflow.CreateStage(ctx, appworkflow.CreateStageRequest{
		WorkflowRunID: runID, Stage: workflow.StageName(stage), Attempt: attempt,
		ExecutionKey: stage,
	})
	if err != nil {
		t.Fatalf("CreateStage(%s): %v", stage, err)
	}
	for _, status := range to {
		record, err = p.workflow.TransitionStage(ctx, appworkflow.TransitionStageRequest{
			StageRunID: record.ID, Status: status, Revision: record.Revision,
		})
		if err != nil {
			t.Fatalf("TransitionStage(%s -> %s): %v", stage, status, err)
		}
	}
	return record
}

// e2e003StagedCrash stages the row a crash leaves under a job whose remote handle was never recorded.
//
// It is the one fixture this walk cannot produce with live code. A worker that has claimed a job always
// settles it or hands it back — `returnClaim` exists for exactly that — so `waiting_remote` with no
// remote id is a state only a crash can leave, and a crash is what a test cannot perform on itself. The
// job is submitted through the SERVICE, so its row, its idempotency key and its identity are real; what
// is staged is the state the process died in, written through the same repository with the same fields
// a worker writes minus the outcome, plus the attempt row a crash leaves behind with the job's counter
// still reading zero.
func e2e003StagedCrash(t *testing.T, p *e2e003Process, request appjobs.SubmitRequest) string {
	t.Helper()
	ctx := context.Background()
	submitted, _, err := p.jobs.Submit(ctx, request)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	record, err := p.jobRepo.Get(ctx, submitted.ID)
	if err != nil {
		t.Fatalf("reading the staged job: %v", err)
	}
	// The provider accepted the work and the process died before the handle reached the row: nothing to
	// poll, and re-submitting would pay for a second generation.
	record.Status = job.StatusWaitingRemote
	record.RemoteJobID = ""
	record.LeaseOwner = "e2e003-dead-worker"
	record.LeaseExpiresAt = time.Now().UTC().Add(-time.Hour)
	record.AttemptCount = 0
	if err := p.jobRepo.Update(ctx, record, record.Revision); err != nil {
		t.Fatalf("staging the crashed job: %v", err)
	}
	if err := p.jobRepo.StartAttempt(ctx, job.Attempt{
		ID: "e2e003-crashed-attempt", JobID: record.ID, AttemptNumber: 1,
		Status: job.AttemptRunning, StartedAt: time.Now().UTC().Add(-2 * time.Minute),
	}); err != nil {
		t.Fatalf("staging the crashed attempt: %v", err)
	}
	return record.ID
}

// assetCounts counts what one asset holds: versions, the files those versions cite, and usages.
func (p *e2e003Process) assetCounts(t *testing.T, assetID string) (versions, files, usages int) {
	t.Helper()
	ctx := context.Background()
	if err := p.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM asset_versions WHERE asset_id = ?`, assetID).Scan(&versions); err != nil {
		t.Fatalf("counting versions: %v", err)
	}
	if err := p.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM asset_files
		WHERE asset_version_id IN (SELECT id FROM asset_versions WHERE asset_id = ?)`, assetID).Scan(&files); err != nil {
		t.Fatalf("counting files: %v", err)
	}
	if err := p.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM asset_usages
		WHERE asset_version_id IN (SELECT id FROM asset_versions WHERE asset_id = ?)`, assetID).Scan(&usages); err != nil {
		t.Fatalf("counting usages: %v", err)
	}
	return versions, files, usages
}

// e2e003VideoInput builds the input document a video job carries, in the shape `SubmitVideoJob` writes.
//
// It names a first frame and a reference as well as a prompt, because the references are the fields a
// restart is most likely to lose: a resumed job is POLLED, and a poll is given no references at all, so
// the only thing that can keep them is the job's own stored input.
func e2e003VideoInput(prompt, shotID string) string {
	encoded, err := json.Marshal(map[string]any{
		"providerId":     "e2e003-media",
		"model":          "video-1",
		"prompt":         prompt,
		"seconds":        4,
		"references":     []string{"cmVmZXJlbmNl"},
		"referenceMimes": []string{"image/png"},
		"firstFrame":     "Zmlyc3RmcmFtZQ==",
		"firstFrameMime": "image/png",
		"shotId":         shotID,
	})
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

// e2e003ResultHash reads the file hash a succeeded job's result document names.
func e2e003ResultHash(t *testing.T, resultJSON string) string {
	t.Helper()
	var document struct {
		Files []struct {
			Hash string `json:"hash"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(resultJSON), &document); err != nil {
		t.Fatalf("the job's result document is unreadable: %v", err)
	}
	if len(document.Files) == 0 || document.Files[0].Hash == "" {
		t.Fatalf("the job's result document names no file: %s", resultJSON)
	}
	return document.Files[0].Hash
}

// TestE2E003ForcedRestartResumesWhatItCanAndFailsWhatItCannot is AC-E2E-003 in full.
func TestE2E003ForcedRestartResumesWhatItCanAndFailsWhatItCannot(t *testing.T) {
	ctx := context.Background()
	world := newE2E003World(t)

	// =========================================================================
	// BEFORE THE CRASH: a project, a storyboard workflow mid-stage, a video job
	// parked on the provider, and a job whose remote handle was never recorded.
	// =========================================================================
	first := world.open(t)

	// --- clause 1's fixture: the project the restart must still open, written through the REAL project
	// service so its rows are the ones a user's project is made of.
	project, err := first.projects.CreateProject(ctx, appprojects.CreateProjectRequest{
		Type: "drama", Name: "中断恢复", Language: "zh-CN",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	snapshot, err := first.projects.LoadCanvas(ctx, project.ID)
	if err != nil {
		t.Fatalf("LoadCanvas: %v", err)
	}
	if _, err := first.projects.CreateNode(ctx, snapshot.Document.ID, appprojects.NodeInput{
		NodeType: "text", Title: "灯还亮着。", PositionX: 10, PositionY: 20,
	}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	episode, err := first.script.CreateEpisode(ctx, appscript.CreateEpisodeRequest{
		ProjectID: project.ID, SeasonNumber: 1, EpisodeNumber: 1, Title: "第一集",
	})
	if err != nil {
		t.Fatalf("CreateEpisode: %v", err)
	}

	// The asset the video job's result becomes a version of. It exists BEFORE the crash, so clause 5 can
	// say what the interrupted work ADDED rather than only what it left behind.
	videoAsset, draftVersion, err := first.assets.CreateAsset(ctx, appassets.CreateAssetRequest{
		ProjectID: project.ID, Type: asset.TypeVideo, Name: "Shot A video",
	})
	if err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}
	const (
		shotA = "e2e003-shot-a"
		shotB = "e2e003-shot-b"
	)

	// --- clause 2's fixture: a run with one stage that REACHED its user gate and one that was still
	// executing. Both moves go through the workflow service's own state machine, so what the restart
	// reads is what the machine wrote.
	run, err := first.workflow.CreateRun(ctx, appworkflow.CreateRunRequest{
		ProjectID: project.ID, EpisodeID: episode.ID, WorkflowType: "episode_production",
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if _, err := first.workflow.TransitionRun(ctx, appworkflow.TransitionRunRequest{
		RunID: run.ID, Status: workflow.RunRunning, Revision: run.Revision,
	}); err != nil {
		t.Fatalf("TransitionRun: %v", err)
	}
	gateStage := first.driveStage(t, run.ID, "storyboard_table", 1,
		workflow.StageRunning, workflow.StageReviewing, workflow.StageWaitingUser)
	midStage := first.driveStage(t, run.ID, "storyboard_panel_generation", 1, workflow.StageRunning)
	if midStage.StartedAt.IsZero() {
		t.Fatal("a stage in play was left with no started_at, so the restart could not tell it from one that never began")
	}

	// --- clause 3's fixture: a video job the real scheduler submitted and parked on the provider.
	videoJob := first.parkVideoJob(t, appjobs.SubmitRequest{
		ProjectID: project.ID, EntityType: "shot", EntityID: shotA,
		JobType: job.JobTypeVideoGeneration, ProviderConfigID: world.mediaProviderID,
		InputJSON: e2e003VideoInput("a lantern in the rain", shotA), Scope: "video",
	})
	if videoJob.RemoteJobID == "" {
		t.Fatal("the job parked with no remote handle, which is clause 4's case rather than the one this fixture stages")
	}
	// The scheduler is not running for the next step, so nothing can claim the staged row before the
	// crash does.
	crashedJobID := e2e003StagedCrash(t, first, appjobs.SubmitRequest{
		ProjectID: project.ID, EntityType: "shot", EntityID: shotB,
		JobType: job.JobTypeVideoGeneration, ProviderConfigID: world.mediaProviderID,
		InputJSON: e2e003VideoInput("a ledger in the salt store", shotB), Scope: "video",
	})

	versionsBefore, filesBefore, usagesBefore := first.assetCounts(t, videoAsset.ID)
	if versionsBefore != 1 || filesBefore != 0 || usagesBefore != 0 {
		t.Fatalf("a fresh asset holds %d versions, %d files and %d usages", versionsBefore, filesBefore, usagesBefore)
	}
	submitsBeforeCrash, pollsBeforeCrash := world.video.counts()
	if submitsBeforeCrash != 1 {
		t.Fatalf("the video job was submitted %d times before the crash, want exactly one", submitsBeforeCrash)
	}

	// =========================================================================
	// THE FORCED CLOSE, and the restart that follows it.
	// =========================================================================
	first.close(t)

	second := world.open(t)
	report := second.recover(t)

	// --- 1. 重启后项目可打开 ---
	//
	// Read back through the same real services rather than by a raw SELECT: "the project opens" is the
	// read path the UI walks, which is the project row and then the canvas that renders it.
	reopened, err := second.projects.GetProject(ctx, project.ID)
	if err != nil {
		t.Fatalf("clause 1: the project cannot be opened after the restart: %v", err)
	}
	if reopened.Name != project.Name || reopened.Type != project.Type {
		t.Fatalf("clause 1: the project came back as %q/%q, not %q/%q",
			reopened.Name, reopened.Type, project.Name, project.Type)
	}
	canvas, err := second.projects.LoadCanvas(ctx, project.ID)
	if err != nil {
		t.Fatalf("clause 1: the restarted project's canvas did not load: %v", err)
	}
	if len(canvas.Nodes) != 1 || canvas.Nodes[0].Title != "灯还亮着。" {
		t.Fatalf("clause 1: the canvas came back with %d nodes: %+v", len(canvas.Nodes), canvas.Nodes)
	}
	if canvas.Project.ID != reopened.ID {
		t.Fatalf("clause 1: the canvas belongs to project %q, not %q", canvas.Project.ID, reopened.ID)
	}
	if _, err := second.script.GetEpisode(ctx, episode.ID); err != nil {
		t.Fatalf("clause 1: the episode the run is against did not survive: %v", err)
	}

	// --- 2. Workflow 显示真实阶段 ---
	//
	// The assertion is on the STAGE ROWS, because they are the durable record: the attempt that reached
	// the user gate must still be at the gate, and the attempt that was executing must still be executing
	// with the started_at the machine stamped before the crash. A restart that reset either — to pending,
	// or to some "unknown, queued again" state — would satisfy a summary while losing the difference
	// between a stage waiting for a person and one that was cut off mid-run, and the two need different
	// things from the user.
	//
	// The restart also has nothing to DO to the stages, and that is the finding rather than a gap: no
	// startup scan touches `stage_runs` (jobs.Recover covers `generation_jobs` only), so the stage rows
	// come back correct because nothing wrote them, not because a recovery path restored them. Clause 2
	// therefore holds exactly as long as the workflow state stays in the database and nothing resets it.
	stages, err := second.workflow.ListStages(ctx, run.ID)
	if err != nil {
		t.Fatalf("clause 2: ListStages: %v", err)
	}
	if len(stages) != 2 {
		t.Fatalf("clause 2: the run came back with %d stage attempts, want 2: %+v", len(stages), stages)
	}
	byID := map[string]workflow.StageRun{}
	for _, stage := range stages {
		byID[stage.ID] = stage
	}
	gate, found := byID[gateStage.ID]
	if !found {
		t.Fatalf("clause 2: the attempt that reached the gate is gone: %+v", stages)
	}
	if gate.Status != workflow.StageWaitingUser {
		t.Fatalf("clause 2: the stage that reached the user gate came back %q, not %q", gate.Status, workflow.StageWaitingUser)
	}
	if gate.Stage != gateStage.Stage || gate.Attempt != gateStage.Attempt || !gate.StartedAt.Equal(gateStage.StartedAt) {
		t.Fatalf("clause 2: the gate attempt came back as %+v against the one the machine wrote: %+v", gate, gateStage)
	}
	mid, found := byID[midStage.ID]
	if !found {
		t.Fatalf("clause 2: the attempt that was executing is gone: %+v", stages)
	}
	if mid.Status != workflow.StageRunning {
		t.Fatalf("clause 2: the stage that was executing came back %q, so the restart guessed rather than read", mid.Status)
	}
	if !mid.StartedAt.Equal(midStage.StartedAt) {
		t.Fatalf("clause 2: the executing attempt's started_at moved across the restart: %s -> %s",
			midStage.StartedAt, mid.StartedAt)
	}
	if mid.Attempt != midStage.Attempt || mid.ExecutionAgentKey != midStage.ExecutionAgentKey {
		t.Fatalf("clause 2: the executing attempt came back as attempt %d under %q, not attempt %d under %q",
			mid.Attempt, mid.ExecutionAgentKey, midStage.Attempt, midStage.ExecutionAgentKey)
	}
	// The RUN-level projection is the other half of "the workflow shows the stage it actually
	// reached": the Studio's runs table renders `currentStage` (`sections.tsx`, the stageLabel column)
	// and `activeStageRunId` beside it, both filled by `ListWorkflowRuns`, and this is the pair a reader
	// of the run list sees. The assertion is HERE rather than after the close-out below because this is
	// the state the RESTART recovered — the moment the criterion is about. Once the interrupted attempt
	// is failed and a second attempt created, the run is legitimately on the second one, and asserting
	// the first would be asserting that the retry did not happen.
	//
	// The pair is written by `WorkflowRepository.projectRunStage`, inside the same transaction as the
	// stage change it describes, using `workflow.ProjectRunStage`. It was NOT: until this was built,
	// `UpdateRun` was the only statement touching either column and its only production caller
	// (`TransitionRun`) copied them from the row it had just read, so a run driven from its first stage
	// to its last kept the schema's empty default and the table rendered "—" for a run demonstrably at
	// `storyboard_panel_generation`. The STAGE rows were always correct, which is why the criterion held
	// for a reader who opened the storyboard view and not for one who read the run list.
	runRow, err := second.workflow.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("clause 2: GetRun: %v", err)
	}
	if runRow.CurrentStage != mid.Stage || runRow.ActiveStageRunID != mid.ID {
		t.Fatalf("clause 2 (run projection): the run reports currentStage=%q activeStageRunId=%q, and the stage it actually reached is %q (attempt %s)",
			runRow.CurrentStage, runRow.ActiveStageRunID, mid.Stage, mid.ID)
	}
	// And the run list — the read path the table walks — carries the same pair, so the assertion is
	// about what the UI receives rather than about a single-row read.
	runs, err := second.workflow.ListRuns(ctx, project.ID)
	if err != nil {
		t.Fatalf("clause 2: ListRuns: %v", err)
	}
	listed := false
	for _, listedRun := range runs {
		if listedRun.ID != run.ID {
			continue
		}
		listed = true
		if listedRun.CurrentStage != mid.Stage || listedRun.ActiveStageRunID != mid.ID {
			t.Fatalf("clause 2 (run list): the run list reports currentStage=%q activeStageRunId=%q, want %q and %s",
				listedRun.CurrentStage, listedRun.ActiveStageRunID, mid.Stage, mid.ID)
		}
	}
	if !listed {
		t.Fatalf("clause 2: the run is missing from its project's run list: %+v", runs)
	}
	// The audit trail is the other half of "not a guess": the events the machine wrote are still there
	// and still say which transitions happened, so a reader can reconstruct how the stage got where it is.
	events, err := second.workflow.ListEvents(ctx, run.ID)
	if err != nil {
		t.Fatalf("clause 2: ListEvents: %v", err)
	}
	reached := map[string]int{}
	for _, event := range events {
		reached[event.ToStatus]++
	}
	for _, wanted := range []string{"running", "reviewing", "waiting_user"} {
		if reached[wanted] == 0 {
			t.Fatalf("clause 2: the audit trail lost the stage's move to %q: %+v", wanted, events)
		}
	}
	// And the run is still DRIVABLE from where it really is, which is what knowing the real stage is for:
	// the interrupted attempt is closed out and the stage is tried again.
	failed, err := second.workflow.TransitionStage(ctx, appworkflow.TransitionStageRequest{
		StageRunID: mid.ID, Status: workflow.StageFailed, Revision: mid.Revision,
	})
	if err != nil {
		t.Fatalf("clause 2: the interrupted attempt could not be closed out after the restart: %v", err)
	}
	retry, err := second.workflow.CreateStage(ctx, appworkflow.CreateStageRequest{
		WorkflowRunID: run.ID, Stage: mid.Stage, Attempt: failed.Attempt + 1,
		ExecutionKey: mid.ExecutionAgentKey,
	})
	if err != nil {
		t.Fatalf("clause 2: the stage could not be retried after the restart: %v", err)
	}
	if retry.Attempt != 2 || retry.Status != workflow.StagePending {
		t.Fatalf("clause 2: the retry came back as attempt %d in %q", retry.Attempt, retry.Status)
	}

	// --- 3. 可恢复任务继续查询 ---
	//
	// The parked job kept its remote handle and was RESUMED. The report is read BEFORE the scheduler
	// starts, so it is the scan's own classification rather than the poll that follows hiding it.
	if report.Resumed != 1 {
		t.Fatalf("clause 3: the recovery scan reported %+v, want exactly one resumed job", report)
	}
	resumed := second.job(t, videoJob.ID)
	if resumed.Status != job.StatusWaitingRemote || resumed.RemoteJobID != videoJob.RemoteJobID {
		t.Fatalf("clause 3: the resumed job is %q with remote handle %q, not %q with %q",
			resumed.Status, resumed.RemoteJobID, job.StatusWaitingRemote, videoJob.RemoteJobID)
	}
	if resumed.InputJSON != videoJob.InputJSON {
		t.Fatalf("clause 3: the resumed job's input changed across the restart:\n before %s\n after  %s",
			videoJob.InputJSON, resumed.InputJSON)
	}

	// The scheduler is started ONCE here and left running through the rest of the walk. `Service.Stop`
	// is not reversible — it clears the start flag — so a second `Start` is a no-op, and a walk that
	// stopped and restarted it would be asserting against a scheduler that never ran. The process is
	// closed at the end instead, which is also what releases the database file.
	second.jobs.Start(ctx, 1, 5*time.Millisecond)
	finished := second.waitForJobStatus(t, videoJob.ID, job.StatusSucceeded)

	if finished.RemoteJobID != videoJob.RemoteJobID {
		t.Fatalf("clause 3: the job finished against remote handle %q, not the one it was parked on (%q)",
			finished.RemoteJobID, videoJob.RemoteJobID)
	}
	// The references survive on the row the restart reads, which is what makes the poll safe: a poll is
	// given no references, so the only thing that can keep them is the job's own stored input.
	if finished.InputJSON != videoJob.InputJSON {
		t.Fatalf("clause 3: the input the job finished with is not the one it was submitted with")
	}
	submitsAfter, pollsAfter := world.video.counts()
	if submitsAfter != submitsBeforeCrash {
		t.Fatalf("clause 3: the restart re-submitted the parked job: %d submission(s) before the crash, %d after",
			submitsBeforeCrash, submitsAfter)
	}
	if pollsAfter <= pollsBeforeCrash {
		t.Fatalf("clause 3: the resumed job was never polled: %d poll(s) before the crash, %d after",
			pollsBeforeCrash, pollsAfter)
	}

	// --- 4. 不可恢复任务明确失败并可重试 ---
	//
	// A job the provider may already be billing for, whose remote handle was never recorded, cannot be
	// continued safely: there is nothing to poll, and re-running it could pay for a second generation.
	// `recoveryDisposition` is the rule that decides this — a `waiting_remote` row with no remote id
	// reaches `orphaned` — and the clause is that the decision is made VISIBLY, with a reason a user can
	// read, and that the work can then be sent back.
	if report.Orphaned != 1 {
		t.Fatalf("clause 4: the recovery scan reported %+v, want exactly one orphaned job", report)
	}
	orphaned := second.job(t, crashedJobID)
	if orphaned.Status != job.StatusOrphaned {
		t.Fatalf("clause 4: the unrecoverable job is %q, so it survived the restart without an outcome", orphaned.Status)
	}
	if !orphaned.Status.IsTerminal() {
		t.Fatalf("clause 4: %q is not a terminal status, so the job is neither resumed nor finished", orphaned.Status)
	}
	if orphaned.ErrorCode == "" || orphaned.ErrorMessage == "" {
		t.Fatalf("clause 4: the failure is not readable: code=%q message=%q", orphaned.ErrorCode, orphaned.ErrorMessage)
	}
	if orphaned.FinishedAt.IsZero() {
		t.Fatal("clause 4: an explicitly failed job was left with no finish time")
	}
	if orphaned.LeaseOwner != "" {
		t.Fatalf("clause 4: the dead worker's lease survived the scan: %q", orphaned.LeaseOwner)
	}
	// The scan also reconciled the attempt row the crash left with the counter still reading zero, which
	// is what stops the retry below colliding with it on (generation_job_id, attempt_number).
	if orphaned.AttemptCount != 1 {
		t.Fatalf("clause 4: the scan left attempt_count at %d against the attempt row the crash left", orphaned.AttemptCount)
	}

	// The retry the Job Center's own command performs, and then the scheduler: the job must RUN again
	// rather than merely change status.
	retried, err := second.jobs.RetryFailed(ctx, []string{crashedJobID})
	if err != nil {
		t.Fatalf("clause 4: RetryFailed: %v", err)
	}
	if retried != 1 {
		t.Fatalf("clause 4: the retry moved %d job(s), want one", retried)
	}
	requeued := second.job(t, crashedJobID)
	if requeued.Status != job.StatusQueued || requeued.ErrorCode != "" || requeued.ErrorMessage != "" {
		t.Fatalf("clause 4: the retried job is %q still carrying %q/%q",
			requeued.Status, requeued.ErrorCode, requeued.ErrorMessage)
	}
	second.waitForJobStatus(t, crashedJobID, job.StatusSucceeded)
	if submits, _ := world.video.counts(); submits != submitsAfter+1 {
		t.Fatalf("clause 4: the retry produced %d new submission(s), want exactly one", submits-submitsAfter)
	}

	// --- 5. 不产生重复资产 ---
	//
	// The resumed job's result is collected the way a restarted UI collects it, twice: the first call
	// turns the succeeded job into ONE candidate version, and the second is the naive "just collect it
	// again" a crash invites. The second must report the job as already collected rather than add a
	// second version, file or usage for the same shot — and no second submission may be needed to
	// produce it, which is what the submission count above already established.
	collected, err := second.production.CollectBatchResults(ctx, appproduction.CollectBatchResultsRequest{
		AssetByItem: map[string]string{shotA: videoAsset.ID},
		JobIDs:      []string{videoJob.ID},
		UsageRole:   "video",
	})
	if err != nil {
		t.Fatalf("clause 5: CollectBatchResults: %v", err)
	}
	if len(collected) != 1 || collected[0].Duplicate {
		t.Fatalf("clause 5: the resumed job produced %+v, want one fresh candidate", collected)
	}
	if collected[0].VersionNumber != draftVersion.VersionNumber+1 {
		t.Fatalf("clause 5: the candidate is version %d against the draft at %d",
			collected[0].VersionNumber, draftVersion.VersionNumber)
	}
	versionsAfter, filesAfter, usagesAfter := second.assetCounts(t, videoAsset.ID)
	if versionsAfter != versionsBefore+1 || filesAfter != 1 || usagesAfter != 1 {
		t.Fatalf("clause 5: after one collection the asset holds %d versions/%d files/%d usages, against %d/%d/%d before",
			versionsAfter, filesAfter, usagesAfter, versionsBefore, filesBefore, usagesBefore)
	}
	// The version is the JOB's output rather than one that merely appeared: it names the job, and the
	// file it carries is the object the job's own result document names.
	version, err := second.assets.ListVersions(ctx, videoAsset.ID)
	if err != nil {
		t.Fatalf("clause 5: ListVersions: %v", err)
	}
	var produced *asset.Version
	for index := range version {
		if version[index].GenerationJobID == videoJob.ID {
			produced = &version[index]
		}
	}
	if produced == nil {
		t.Fatalf("clause 5: no version of the asset names the resumed job (%s): %+v", videoJob.ID, version)
	}
	if produced.ID != collected[0].VersionID {
		t.Fatalf("clause 5: the collected candidate is %s and the version naming the job is %s", collected[0].VersionID, produced.ID)
	}
	files, err := second.assets.ListFiles(ctx, produced.ID)
	if err != nil {
		t.Fatalf("clause 5: ListFiles: %v", err)
	}
	if len(files) != 1 || files[0].FileHash != e2e003ResultHash(t, finished.ResultJSON) {
		t.Fatalf("clause 5: the candidate's files are %+v against the job's own result %s", files, finished.ResultJSON)
	}

	// The second collection, which is the re-run a crash invites.
	again, err := second.production.CollectBatchResults(ctx, appproduction.CollectBatchResultsRequest{
		AssetByItem: map[string]string{shotA: videoAsset.ID},
		JobIDs:      []string{videoJob.ID},
		UsageRole:   "video",
	})
	if err != nil {
		t.Fatalf("clause 5: the second collection failed rather than reporting a duplicate: %v", err)
	}
	if len(again) != 1 || !again[0].Duplicate || again[0].VersionID != "" {
		t.Fatalf("clause 5: the second collection produced %+v, want one duplicate report", again)
	}
	versionsAgain, filesAgain, usagesAgain := second.assetCounts(t, videoAsset.ID)
	if versionsAgain != versionsAfter || filesAgain != filesAfter || usagesAgain != usagesAfter {
		t.Fatalf("clause 5: the second collection duplicated the work: %d/%d/%d -> %d/%d/%d",
			versionsAfter, filesAfter, usagesAfter, versionsAgain, filesAgain, usagesAgain)
	}

	// The bytes themselves are not duplicated either: the interrupted work's payload is one object, and
	// the retried job's identical payload is content-addressed onto the same row rather than stored twice.
	hash := e2e003ResultHash(t, finished.ResultJSON)
	var objects int
	if err := second.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM file_objects WHERE hash = ?`, hash).Scan(&objects); err != nil {
		t.Fatalf("clause 5: counting the stored objects: %v", err)
	}
	if objects != 1 {
		t.Fatalf("clause 5: the interrupted work's bytes are stored %d times", objects)
	}

	// End the second process the same way the first ended, so the database file is released and the
	// walk's teardown is not the thing that fails.
	second.close(t)
}
