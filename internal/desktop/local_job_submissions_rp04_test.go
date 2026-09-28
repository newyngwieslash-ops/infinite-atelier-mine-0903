package desktop

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
)

// local_job_submissions_rp04_test.go is RP-04.4's binding contract: the four
// narrow commands build the VERSIONED input documents the runner decodes,
// refuse inputs that do not name their subject BEFORE persisting, and give
// each type its own idempotency scope so a re-submission returns the original
// job. The assertions read the PERSISTED rows back — the input the runner will
// decode is exactly what the repository now holds.

// rp04JobRepository is the in-memory job store the binding's submit tests run
// against. It implements the application's full Repository port, recording
// rows for read-back; the scheduler paths it does not exercise panic rather
// than answer, so an accidental use is loud.
type rp04JobRepository struct {
	mu   sync.Mutex
	jobs map[string]job.Job
	deps map[string][]job.Dependency
}

func newRP04JobRepository() *rp04JobRepository {
	return &rp04JobRepository{jobs: map[string]job.Job{}, deps: map[string][]job.Dependency{}}
}

func (r *rp04JobRepository) Insert(_ context.Context, record job.Job) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.jobs {
		if existing.ProjectID == record.ProjectID && existing.IdempotencyKey == record.IdempotencyKey {
			return job.DuplicateJobError()
		}
	}
	r.jobs[record.ID] = record
	return nil
}

func (r *rp04JobRepository) Get(_ context.Context, id string) (job.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.jobs[id]
	if !ok {
		return job.Job{}, job.JobNotFoundError(id)
	}
	return record, nil
}

func (r *rp04JobRepository) GetByIdempotencyKey(_ context.Context, projectID, key string) (job.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, record := range r.jobs {
		if record.ProjectID == projectID && record.IdempotencyKey == key {
			return record, nil
		}
	}
	return job.Job{}, job.JobNotFoundError(key)
}

func (r *rp04JobRepository) List(_ context.Context, filter appjobs.ListFilter) ([]job.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []job.Job
	for _, record := range r.jobs {
		if len(filter.Statuses) > 0 {
			matched := false
			for _, status := range filter.Statuses {
				if record.Status == status {
					matched = true
				}
			}
			if !matched {
				continue
			}
		}
		out = append(out, record)
	}
	return out, nil
}

func (r *rp04JobRepository) Claim(_ context.Context, id, workerID string, leaseUntil time.Time, now time.Time) (job.Job, error) {
	panic("not reachable from the submit tests")
}

func (r *rp04JobRepository) Update(_ context.Context, record job.Job, expectedRevision int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	existing, ok := r.jobs[record.ID]
	if !ok {
		return job.JobNotFoundError(record.ID)
	}
	if existing.Revision != expectedRevision {
		return job.DuplicateJobError()
	}
	record.Revision = expectedRevision + 1
	r.jobs[record.ID] = record
	return nil
}

func (r *rp04JobRepository) RequestCancel(_ context.Context, id string, now time.Time) (job.Job, error) {
	panic("not reachable from the submit tests")
}

func (r *rp04JobRepository) ClaimableCandidates(_ context.Context, now time.Time, limit int) ([]job.Job, error) {
	return nil, nil
}

func (r *rp04JobRepository) ActiveCounts(_ context.Context) (map[job.Status]int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	counts := map[job.Status]int{}
	for _, record := range r.jobs {
		counts[record.Status]++
	}
	return counts, nil
}

func (r *rp04JobRepository) ProviderConcurrency(_ context.Context, providerConfigID string) (int, error) {
	return 0, nil
}

func (r *rp04JobRepository) ActiveProviderCounts(_ context.Context, now time.Time) (map[string]int, error) {
	return map[string]int{}, nil
}

func (r *rp04JobRepository) ProviderRateLimit(_ context.Context, providerConfigID string) (int, error) {
	return 0, nil
}

func (r *rp04JobRepository) ProviderWindowCount(_ context.Context, providerConfigID string, now time.Time) (int, error) {
	return 0, nil
}

func (r *rp04JobRepository) StartAttempt(_ context.Context, attempt job.Attempt) error { return nil }

func (r *rp04JobRepository) FinishAttempt(_ context.Context, attempt job.Attempt) error { return nil }

func (r *rp04JobRepository) ListAttempts(_ context.Context, jobID string) ([]job.Attempt, error) {
	return nil, nil
}

func (r *rp04JobRepository) AddDependency(_ context.Context, dependency job.Dependency) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deps[dependency.JobID] = append(r.deps[dependency.JobID], dependency)
	return nil
}

func (r *rp04JobRepository) ListDependencies(_ context.Context, jobID string) ([]job.Dependency, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.deps[jobID], nil
}

func (r *rp04JobRepository) RecordProviderRequest(_ context.Context, providerConfigID string, now time.Time) error {
	return nil
}

// newRP04JobsService builds the jobs service over the in-memory store, the
// same wiring AttachJobs performs in production minus the database.
func newRP04JobsService(t *testing.T) (*JobsBinding, *rp04JobRepository) {
	t.Helper()
	repository := newRP04JobRepository()
	service := appjobs.NewService(appjobs.Options{
		Repository: repository,
		Clock:      appjobs.NewClockFunc(func() time.Time { return time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC) }),
		IDs:        &rp04IDs{},
		Runner:     rp04Runner{},
		Policy:     job.RetryPolicy{BaseDelay: time.Minute, MaxDelay: time.Hour, MaxAttempts: 3},
	})
	binding := &JobsBinding{}
	AttachJobs(binding, context.Background(), service)
	return binding, repository
}

// TestRP04NarrowSubmissionsBuildVersionedInputs drives all four commands and
// reads the persisted rows back: each input decodes as its typed document
// with the version stamp, and each subject travels under its own entity.
func TestRP04NarrowSubmissionsBuildVersionedInputs(t *testing.T) {
	binding, repository := newRP04JobsService(t)
	ctx := context.Background()

	// THUMBNAIL
	thumbnail, err := binding.SubmitThumbnailJob(SubmitThumbnailJobRequest{
		ProjectID: "p1", AssetVersionID: "mv-1", MaxWidthPixels: 480, MaxHeightPixels: 270,
	})
	if err != nil {
		t.Fatalf("SubmitThumbnailJob: %v", err)
	}
	stored, err := repository.Get(ctx, thumbnail.ID)
	if err != nil {
		t.Fatalf("Get thumbnail: %v", err)
	}
	var decodedThumbnail struct {
		ProjectID       string `json:"projectId"`
		Version         int    `json:"version"`
		AssetVersionID  string `json:"assetVersionId"`
		MaxWidthPixels  int    `json:"maxWidthPixels"`
		MaxHeightPixels int    `json:"maxHeightPixels"`
	}
	if err := json.Unmarshal([]byte(stored.InputJSON), &decodedThumbnail); err != nil {
		t.Fatalf("thumbnail input is not the typed document: %v", err)
	}
	if decodedThumbnail.Version != 1 || decodedThumbnail.AssetVersionID != "mv-1" || decodedThumbnail.MaxWidthPixels != 480 {
		t.Fatalf("thumbnail input = %+v", decodedThumbnail)
	}
	if stored.EntityID != "mv-1" || stored.EntityType != "asset_version" {
		t.Fatalf("thumbnail entity = %s/%s", stored.EntityType, stored.EntityID)
	}

	// IMPORT
	imported, err := binding.SubmitImportJob(SubmitImportJobRequest{ProjectID: "p1", DocumentID: "doc-1"})
	if err != nil {
		t.Fatalf("SubmitImportJob: %v", err)
	}
	stored, err = repository.Get(ctx, imported.ID)
	if err != nil {
		t.Fatalf("Get import: %v", err)
	}
	if !strings.Contains(stored.InputJSON, `"documentId":"doc-1"`) || !strings.Contains(stored.InputJSON, `"version":1`) {
		t.Fatalf("import input = %s", stored.InputJSON)
	}
	if stored.EntityID != "doc-1" || stored.EntityType != "source_document" {
		t.Fatalf("import entity = %s/%s", stored.EntityType, stored.EntityID)
	}

	// EXPORT
	exported, err := binding.SubmitExportJob(SubmitExportJobRequest{
		ProjectID: "p1", EpisodeID: "ep-1", Quality: "final", FPS: 24,
	})
	if err != nil {
		t.Fatalf("SubmitExportJob: %v", err)
	}
	stored, err = repository.Get(ctx, exported.ID)
	if err != nil {
		t.Fatalf("Get export: %v", err)
	}
	if !strings.Contains(stored.InputJSON, `"episodeId":"ep-1"`) || !strings.Contains(stored.InputJSON, `"quality":"final"`) || !strings.Contains(stored.InputJSON, `"fps":24`) {
		t.Fatalf("export input = %s", stored.InputJSON)
	}
	if stored.EntityType != "episode" {
		t.Fatalf("export entity = %s/%s", stored.EntityType, stored.EntityID)
	}

	// MIGRATION
	migrated, err := binding.SubmitMigrationJob(SubmitMigrationJobRequest{
		ProjectID: "p1", SnapshotHash: strings.Repeat("a", 64), Fingerprint: "fp-1", ImportMode: "copy",
	})
	if err != nil {
		t.Fatalf("SubmitMigrationJob: %v", err)
	}
	stored, err = repository.Get(ctx, migrated.ID)
	if err != nil {
		t.Fatalf("Get migration: %v", err)
	}
	if !strings.Contains(stored.InputJSON, `"snapshotHash":"`+strings.Repeat("a", 64)+`"`) || !strings.Contains(stored.InputJSON, `"importMode":"copy"`) {
		t.Fatalf("migration input = %s", stored.InputJSON)
	}
}

// TestRP04NarrowSubmissionsRefuseUnnamedSubjects is the pre-persist negative
// suite: a subject-less input is refused at the binding and NOTHING lands in
// the store — no row the Job Center would show as queued forever.
func TestRP04NarrowSubmissionsRefuseUnnamedSubjects(t *testing.T) {
	binding, repository := newRP04JobsService(t)

	calls := []struct {
		name string
		call func() error
	}{
		{"thumbnail without version", func() error {
			_, err := binding.SubmitThumbnailJob(SubmitThumbnailJobRequest{ProjectID: "p1"})
			return err
		}},
		{"import without document", func() error {
			_, err := binding.SubmitImportJob(SubmitImportJobRequest{ProjectID: "p1"})
			return err
		}},
		{"export without episode", func() error {
			_, err := binding.SubmitExportJob(SubmitExportJobRequest{ProjectID: "p1"})
			return err
		}},
		{"export with an invented quality", func() error {
			_, err := binding.SubmitExportJob(SubmitExportJobRequest{ProjectID: "p1", EpisodeID: "e", Quality: "ultra"})
			return err
		}},
		{"migration without snapshot", func() error {
			_, err := binding.SubmitMigrationJob(SubmitMigrationJobRequest{ProjectID: "p1"})
			return err
		}},
		{"migration with an invented mode", func() error {
			_, err := binding.SubmitMigrationJob(SubmitMigrationJobRequest{ProjectID: "p1", SnapshotHash: strings.Repeat("a", 64), ImportMode: "delete"})
			return err
		}},
		{"thumbnail without project", func() error {
			_, err := binding.SubmitThumbnailJob(SubmitThumbnailJobRequest{AssetVersionID: "mv-1"})
			return err
		}},
	}
	for _, testCase := range calls {
		t.Run(testCase.name, func(t *testing.T) {
			if err := testCase.call(); err == nil {
				t.Fatalf("%s was accepted", testCase.name)
			}
		})
	}
	if len(repository.jobs) != 0 {
		t.Fatalf("refused submissions persisted %d rows", len(repository.jobs))
	}
}

// TestRP04NarrowSubmissionsAreIdempotent proves a re-submission of the SAME
// narrow command returns the original job rather than queueing a second one.
func TestRP04NarrowSubmissionsAreIdempotent(t *testing.T) {
	binding, repository := newRP04JobsService(t)

	first, err := binding.SubmitThumbnailJob(SubmitThumbnailJobRequest{ProjectID: "p1", AssetVersionID: "mv-9"})
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}
	second, err := binding.SubmitThumbnailJob(SubmitThumbnailJobRequest{ProjectID: "p1", AssetVersionID: "mv-9"})
	if err != nil {
		t.Fatalf("second submit: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("the re-submission created a new job %s vs %s", first.ID, second.ID)
	}
	if len(repository.jobs) != 1 {
		t.Fatalf("%d rows persisted for two identical submissions", len(repository.jobs))
	}
}

// rp04IDs mints sequential ids; the counter is what makes each submit unique.
type rp04IDs struct{ counter int }

func (g *rp04IDs) NewID(prefix string) string {
	g.counter++
	return prefix + "-rp04-" + strings.Repeat("0", 3) + string(rune('a'+g.counter-1))
}

// rp04Runner is the runner the submit tests never reach: a queued job in these
// tests is only ever submitted, never executed.
type rp04Runner struct{}

func (rp04Runner) Run(ctx context.Context, record job.Job) (appjobs.Outcome, error) {
	return appjobs.Outcome{Status: job.StatusSucceeded}, nil
}

// --- RP-06.2 binding tests live here too: they reuse the in-memory stores. ---

// TestRP06SkillVersionsRefuseWithoutVersioner pins the fail-closed rule: a
// binding whose assembly does not carry the versioning surface answers
// "not available" rather than an empty history.
func TestRP06SkillVersionsRefuseWithoutVersioner(t *testing.T) {
	binding := &AgentBinding{}
	if _, err := binding.ListSkillVersions("script.decision"); err == nil {
		t.Fatal("a binding with no assembly listed skill versions")
	}
}
