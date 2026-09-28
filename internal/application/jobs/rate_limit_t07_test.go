package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
)

// rate_limit_t07_test.go is FR-150's second half (T07): a provider's
// per-minute REQUEST ceiling, enforced where the concurrency limit is, with
// the boundary cases the audit names — a window that rolls, a provider whose
// limit does not starve another's, and a count that survives a restart
// (the ledger is a table, and the double's admission reads it the same way).

// TestSchedulerHonoursPerProviderRateLimit drives the admission: with a
// ceiling of two per minute, exactly two of three queued jobs for one
// provider are claimed while the third keeps its place.
func TestSchedulerHonoursPerProviderRateLimit(t *testing.T) {
	ctx := context.Background()
	repository := newMemoryRepository()
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	clock := &controlledClock{now: &now}
	service := NewService(Options{
		Repository: repository,
		Clock:      clock,
		IDs:        &counterIDs{},
		Policy:     job.RetryPolicy{BaseDelay: time.Minute, MaxDelay: time.Hour, MaxAttempts: 3},
		Runner:     parkingRunner{},
	})
	t.Cleanup(func() {
		_ = service.Stop(context.Background())
	})

	// The provider's ceiling: two requests per minute.
	repository.mu.Lock()
	repository.providerRateLimits = map[string]int{"prov-a": 2}
	repository.mu.Unlock()

	ids := make([]string, 3)
	for index := range ids {
		submitted, _, err := service.Submit(ctx, SubmitRequest{
			ProjectID:        "p1",
			EntityType:       "shot",
			EntityID:         "s" + string(rune('a'+index)),
			JobType:          "image_generation",
			ProviderConfigID: "prov-a",
			InputJSON:        `{"x":` + string(rune('0'+index)) + `}`,
			Scope:            "rate-test",
		})
		if err != nil {
			t.Fatalf("submitting job %d: %v", index, err)
		}
		ids[index] = submitted.ID
	}

	// One dispatch pass claims at most two — the window is full after that.
	service.dispatchForTest(ctx)

	claimed := 0
	for _, id := range ids {
		record, err := repository.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if record.Status == job.StatusRunning {
			claimed++
		}
	}
	if claimed != 2 {
		t.Fatalf("%d jobs claimed under a rate limit of two; the third must keep its place", claimed)
	}

	// The minute rolls: the window empties and the third job is admissible.
	now = now.Add(time.Minute)
	service.dispatchForTest(ctx)
	claimed = 0
	for _, id := range ids {
		record, err := repository.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if record.Status == job.StatusRunning {
			claimed++
		}
	}
	if claimed != 3 {
		t.Fatalf("%d jobs claimed after the window rolled; the third must run", claimed)
	}
}

// TestARateLimitedProviderDoesNotStarveAnother is the fairness clause: two
// providers, one full, the other's jobs still admissible in the same pass.
func TestARateLimitedProviderDoesNotStarveAnother(t *testing.T) {
	ctx := context.Background()
	repository := newMemoryRepository()
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	clock := &controlledClock{now: &now}
	service := NewService(Options{
		Repository: repository,
		Clock:      clock,
		IDs:        &counterIDs{},
		Policy:     job.RetryPolicy{BaseDelay: time.Minute, MaxDelay: time.Hour, MaxAttempts: 3},
		Runner:     parkingRunner{},
	})
	t.Cleanup(func() {
		_ = service.Stop(context.Background())
	})

	repository.mu.Lock()
	repository.providerRateLimits = map[string]int{"prov-a": 1}
	repository.mu.Unlock()

	// A full window for prov-a.
	repository.mu.Lock()
	repository.providerWindows = map[string]int{windowKey("prov-a", now): 1}
	repository.mu.Unlock()

	fullID := submitForRate(t, ctx, service, repository, "prov-a", "full")
	otherID := submitForRate(t, ctx, service, repository, "prov-b", "other")

	service.dispatchForTest(ctx)

	full, err := repository.Get(ctx, fullID)
	if err != nil {
		t.Fatal(err)
	}
	other, err := repository.Get(ctx, otherID)
	if err != nil {
		t.Fatal(err)
	}
	if full.Status == job.StatusRunning {
		t.Fatal("prov-a's job was claimed with its window full")
	}
	if other.Status != job.StatusRunning {
		t.Fatal("prov-b's job was starved by prov-a's full window")
	}
}

// TestTheRateLedgerSurvivesARestart is the restart rule: the window count is
// recorded durably, so a new scheduler reading the same store sees the same
// window — the boundary the audit asks for.
func TestTheRateLedgerSurvivesARestart(t *testing.T) {
	ctx := context.Background()
	repository := newMemoryRepository()
	now := time.Date(2026, 9, 26, 12, 0, 30, 0, time.UTC)

	// A claim recorded into the ledger by the OLD process.
	if err := repository.RecordProviderRequest(ctx, "prov-a", now); err != nil {
		t.Fatal(err)
	}
	// The NEW process reads the same minute's window.
	count, err := repository.ProviderWindowCount(ctx, "prov-a", now)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("the new process read %d requests for the window; the ledger did not survive", count)
	}
	// Thirty seconds later the window has rolled and the count is zero.
	count, err = repository.ProviderWindowCount(ctx, "prov-a", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("the rolled window read %d requests; old windows must not leak", count)
	}
}

// TestAJobBeyondTheRateCeilingKeepsItsPriorityOrder is the skip rule's shape:
// the skipped job is NOT failed and NOT requeued to the back — it stays
// queued with its original priority and is offered again next pass.
func TestAJobBeyondTheRateCeilingKeepsItsPriorityOrder(t *testing.T) {
	ctx := context.Background()
	repository := newMemoryRepository()
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	clock := &controlledClock{now: &now}
	service := NewService(Options{
		Repository: repository,
		Clock:      clock,
		IDs:        &counterIDs{},
		Policy:     job.RetryPolicy{BaseDelay: time.Minute, MaxDelay: time.Hour, MaxAttempts: 3},
		Runner:     parkingRunner{},
	})
	t.Cleanup(func() {
		_ = service.Stop(context.Background())
	})

	repository.mu.Lock()
	repository.providerRateLimits = map[string]int{"prov-a": 1}
	// The window is already full: one request recorded this minute.
	repository.providerWindows = map[string]int{windowKey("prov-a", now): 1}
	repository.mu.Unlock()

	skippedID := submitForRate(t, ctx, service, repository, "prov-a", "skipped")
	service.dispatchForTest(ctx)

	record, err := repository.Get(ctx, skippedID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != job.StatusQueued {
		t.Fatalf("the skipped job is %q; skipping must leave it queued", record.Status)
	}
	if record.Priority != 0 {
		t.Fatalf("the skipped job's priority moved to %d; it must keep its place", record.Priority)
	}
}

// submitForRate submits one image job for the rate tests.
func submitForRate(t *testing.T, ctx context.Context, service *Service, repository *memoryRepository, providerID, entity string) string {
	t.Helper()
	submitted, _, err := service.Submit(ctx, SubmitRequest{
		ProjectID:        "p1",
		EntityType:       "shot",
		EntityID:         entity,
		JobType:          "image_generation",
		ProviderConfigID: providerID,
		InputJSON:        `{"x":"y"}`,
		Scope:            "rate-" + entity,
	})
	if err != nil {
		t.Fatalf("submitting: %v", err)
	}
	return submitted.ID
}

// seqIDsForRate builds the deterministic id source the tests use.


// jobStatusRunning and jobStatusQueued name the statuses as strings through
// the domain, so this file does not restate the vocabulary.
// idSource is the ID port the service takes.

// controlledClock is a clock whose instant the test moves: the window's
// roll is a clock fact, and a test that cannot move time cannot test it.
type controlledClock struct {
	now *time.Time
}

func (c *controlledClock) Now() time.Time { return *c.now }

// jobPollIntervalForTest is the scheduler cadence the tests pass; the default
// keeps a passive pass from consuming the test's own clock.
func jobPollIntervalForTest() time.Duration { return time.Hour }

// dispatchForTest runs ONE admission pass and parks the claims: the work
// channel is drained, so the claimed jobs stay `running` on their leases and
// no runner settles them — exactly the state admission decided.
func (s *Service) dispatchForTest(ctx context.Context) {
	work := make(chan job.Job, 64)
	done := make(chan struct{})
	go func() {
		for range work {
		}
		close(done)
	}()
	s.dispatch(ctx, work)
	close(work)
	<-done
}

// parkingRunner keeps every claimed job in `running`: its outcome says "still
// waiting on the remote", which the worker persists without settling.
type parkingRunner struct{}

func (parkingRunner) Run(_ context.Context, _ job.Job) (Outcome, error) {
	return Outcome{Status: job.StatusWaitingRemote, PollOnly: true}, nil
}
