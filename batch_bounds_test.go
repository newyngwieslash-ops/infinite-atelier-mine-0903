package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	appassets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/productionpipeline"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/database"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/platform/id"
)

// batch_bounds_test.go covers the bounds and lineage facts a mutation run found unasserted.
//
// The run returned thirteen survivors; the routing cluster is `routing_wp09_test.go`'s and
// this file takes the next three by value:
//
//   - `MaxBatchCandidates`: without it a caller asking for a thousand candidates enqueues a
//     thousand jobs from one click, and no test noticed.
//   - the batch's SEMAPHORE: AC-BOARD-003 names "并发限制", and the test that existed asserted
//     only that a limited batch COMPLETED — which is true with or without a limit.
//   - the collection's PROMPT and SEED: AC-ASSET-002 names both, the collection writes both,
//     and nothing read either back.

// TestTheBatchRefusesMoreCandidatesThanTheCeiling covers the bound.
//
// It is a REFUSAL rather than a truncation, and the difference matters: a batch that quietly
// clamped a request of a thousand to eight would submit work the caller did not ask for and
// report success.
func TestTheBatchRefusesMoreCandidatesThanTheCeiling(t *testing.T) {
	ctx := context.Background()
	h := newBatchHarness(t)
	versionID, episodeID := h.seedStoryboardWithShots(t, 2)
	h.approveGapReport(t, episodeID, true)

	_, err := h.production.RunImageBatch(ctx, productionpipeline.RunImageBatchRequest{
		StoryboardVersionID: versionID, EpisodeID: episodeID, ProjectID: h.projectID,
		PerShotCandidates: productionpipeline.MaxBatchCandidates + 1,
		ProviderID:        h.imageProviderID, ModelName: "mock-model",
	})
	if err == nil {
		t.Fatal("a batch asking for more candidates than the ceiling was accepted")
	}
	// NOTHING was submitted, which is what makes this a bound rather than a warning.
	if count := jobCount(t, h.db, h.projectID); count != 0 {
		t.Fatalf("the refused batch submitted %d jobs", count)
	}
	// And the ceiling itself is accepted, so the check is a bound rather than a prohibition.
	if _, err := h.production.RunImageBatch(ctx, productionpipeline.RunImageBatchRequest{
		StoryboardVersionID: versionID, EpisodeID: episodeID, ProjectID: h.projectID,
		PerShotCandidates: productionpipeline.MaxBatchCandidates,
		ShotIDs:           []string{shotIDsOf(t, h, versionID)[0]},
		ProviderID:        h.imageProviderID, ModelName: "mock-model",
	}); err != nil {
		t.Fatalf("a batch at the ceiling was refused: %v", err)
	}
}

// countingJobStore wraps the job repository and records how many submissions overlap.
//
// It is what makes the concurrency limit ASSERTABLE rather than merely present: the limit is
// on submission, so the observable is the peak number of `Insert` calls in flight at once. A
// repository double that holds the first insert open until a second arrives is deterministic —
// no sleeps, no timing races — and under the mutation that removes the semaphore it reports two
// in flight where the limit is one.
type countingJobStore struct {
	// The embedded interface means a call this double does not expect panics rather than
	// returning a zero value that reads like an empty store.
	appjobs.Repository
	inner    appjobs.Repository
	mu       sync.Mutex
	inFlight int
	peak     int
	// release is closed when the test wants the held insert to finish.
	release chan struct{}
	// held records whether the first insert has been held yet.
	held bool
}

func (s *countingJobStore) Insert(ctx context.Context, record job.Job) error {
	s.mu.Lock()
	s.inFlight++
	if s.inFlight > s.peak {
		s.peak = s.inFlight
	}
	first := !s.held
	s.held = true
	s.mu.Unlock()
	if first {
		// The FIRST insert WAITS until the test releases it, so any submission that started
		// concurrently with it is visible in the peak.
		<-s.release
	}
	err := s.inner.Insert(ctx, record)
	s.mu.Lock()
	s.inFlight--
	s.mu.Unlock()
	return err
}

// TestTheBatchSubmitsNoMoreAtOnceThanItsLimit covers AC-BOARD-003's "并发限制".
//
// The criterion asks for a LIMIT, and the assertion has to observe it: a batch that completed
// with a limit set proves nothing, because it would also complete without one. This measures
// the peak number of submissions in flight.
func TestTheBatchSubmitsNoMoreAtOnceThanItsLimit(t *testing.T) {
	ctx := context.Background()
	h := newBatchHarness(t)
	versionID, episodeID := h.seedStoryboardWithShots(t, 4)
	h.approveGapReport(t, episodeID, true)
	store := h.holdFirstInsert(t)

	done := make(chan error, 1)
	go func() {
		_, err := h.production.RunImageBatch(ctx, productionpipeline.RunImageBatchRequest{
			StoryboardVersionID: versionID, EpisodeID: episodeID, ProjectID: h.projectID,
			PerShotCandidates: 2, ProviderID: h.imageProviderID, ModelName: "mock-model",
		})
		done <- err
	}()
	// Long enough for every submission the limit allows to have started and blocked, and for a
	// batch with no limit to have started all eight.
	time.Sleep(200 * time.Millisecond)
	close(store.release)
	if err := <-done; err != nil {
		t.Fatalf("the batch: %v", err)
	}
	// The harness's production pipeline is built with the DEFAULT limit. The peak is asserted
	// against it rather than against a number this test chose, so a change to the default does
	// not silently pass.
	if store.peak > productionpipeline.DefaultMaxImageConcurrency {
		t.Fatalf("%d submissions were in flight at once with a limit of %d",
			store.peak, productionpipeline.DefaultMaxImageConcurrency)
	}
	if store.peak < 2 {
		// A peak of one would mean the submissions never overlapped, which is the shape a
		// SERIAL implementation has — and this test would then be asserting nothing about a
		// limit, because there is nothing to limit.
		t.Logf("peak in-flight submissions was %d; the batch may be serial", store.peak)
	}
}

// holdFirstInsert replaces the harness's job repository with one that holds the first insert.
//
// The BATCH is rebuilt over it rather than the harness's own service being mutated: a job
// service's repository is fixed at construction, and a test that reached in to swap it would
// be asserting about a build shape that does not exist.
func (h *batchHarness) holdFirstInsert(t *testing.T) *countingJobStore {
	t.Helper()
	store := &countingJobStore{
		inner:   database.NewJobRepository(h.db),
		release: make(chan struct{}),
	}
	h.production = productionpipeline.New(productionpipeline.Options{
		Storyboard: h.drama.storyboard,
		Gaps:       h.drama.gaps,
		Jobs: appjobs.NewService(appjobs.Options{
			Repository: store,
			Clock:      appjobs.NewClockFunc(func() time.Time { return time.Now().UTC() }),
			IDs:        appjobsPrefixIDs{inner: id.NewGenerator()},
			Publisher:  discardPublisher{},
		}),
		Assets:   h.drama.assets,
		Workflow: h.drama.workflow,
	})
	return store
}

// TestTheCollectedCandidateKeepsItsPromptAndSeed covers AC-ASSET-002's two facts the collection
// writes and nothing read back.
//
// Both are lineage: the prompt is what was asked for, and the seed is what makes the render
// reproducible. A collection that dropped either would leave a candidate nobody could
// regenerate, and the criterion names both.
func TestTheCollectedCandidateKeepsItsPromptAndSeed(t *testing.T) {
	ctx := context.Background()
	h := newBatchHarness(t)
	versionID, episodeID := h.seedStoryboardWithShots(t, 1)
	h.approveGapReport(t, episodeID, true)
	h.startWorkers(t)

	result, err := h.production.RunImageBatch(ctx, productionpipeline.RunImageBatchRequest{
		StoryboardVersionID: versionID, EpisodeID: episodeID, ProjectID: h.projectID,
		PerShotCandidates: 1, ProviderID: h.imageProviderID, ModelName: "mock-model",
		Seed:         "seed-42",
		PromptSuffix: "winter palette",
	})
	if err != nil {
		t.Fatalf("running the batch: %v", err)
	}
	h.awaitJobs(t, len(result.Submitted))
	submission := result.Submitted[0]
	record, _, err := h.drama.assets.CreateAsset(ctx, appassets.CreateAssetRequest{
		ProjectID: h.projectID, Type: asset.TypeImage, Name: "候选",
	})
	if err != nil {
		t.Fatalf("creating the candidate's asset: %v", err)
	}
	collected, err := h.production.CollectBatchResults(ctx, productionpipeline.CollectBatchResultsRequest{
		AssetByItem: map[string]string{submission.ItemID: record.ID},
		JobIDs:      []string{submission.JobID},
	})
	if err != nil {
		t.Fatalf("collecting: %v", err)
	}
	if len(collected) != 1 || collected[0].VersionID == "" {
		t.Fatalf("the collection produced %+v", collected)
	}
	version, err := h.drama.assets.ListVersions(ctx, record.ID)
	if err != nil {
		t.Fatalf("reading the versions: %v", err)
	}
	var candidate asset.Version
	for _, row := range version {
		if row.ID == collected[0].VersionID {
			candidate = row
		}
	}
	// The PROMPT is the batch's own derived prompt, so the suffix the caller added and the
	// row's descriptions must both appear — a collection that stored an empty prompt, or the
	// suffix alone, is a candidate whose lineage does not say what was rendered.
	if strings.TrimSpace(candidate.Prompt) == "" {
		t.Error("the collected candidate records no prompt")
	}
	if !strings.Contains(candidate.Prompt, "winter palette") {
		t.Errorf("the candidate's prompt is %q, and the batch appended a suffix", candidate.Prompt)
	}
	if candidate.Seed != "seed-42" {
		t.Errorf("the candidate's seed is %q, and the batch was given seed-42", candidate.Seed)
	}
	// And the JOB is recorded, which is what ties the version to the work that produced it.
	if candidate.GenerationJobID != submission.JobID {
		t.Errorf("the candidate's job is %q, want %q", candidate.GenerationJobID, submission.JobID)
	}
}

// shotIDsOf reads a board version's shot ids, in ordinal order.
func shotIDsOf(t *testing.T, h *batchHarness, versionID string) []string {
	t.Helper()
	items, err := h.drama.storyboard.ListStoryboardItems(context.Background(), versionID)
	if err != nil {
		t.Fatalf("listing the board's rows: %v", err)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.ShotID)
	}
	return out
}
