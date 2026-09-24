package jobs

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
)

// provider_concurrency_test.go is FR-150's second acceptance criterion: 「同供应商并发不超过配置上限」.
//
// # What each test is for
//
// The criterion has four ways to be wrong and the tests below are one per way:
//
//  1. The limit is not enforced at all — `TestSchedulerHonoursPerProviderConcurrency` catches that,
//     and it also asserts the OTHER half of the ruling: a provider at its ceiling must not stop a
//     different provider's work, which is what the admission check being in `dispatch` rather than
//     in `Claim` buys.
//  2. A job abandoned by an expired lease counts against the limit — `TestAnExpiredLeaseDoesNotCount`
//     catches that. Counting ghosts would permanently reduce a provider's throughput after one crash.
//  3. The count lives in memory — `TestARestartedSchedulerStillHonoursTheLimit` catches that. The whole
//     job design exists because an application may be force-closed mid-flight (AC-E2E-003), and a
//     counter that starts at zero would admit a second full set of work against a provider already
//     serving the first.
//  4. Zero is read as "run nothing" — `TestZeroMeansUnlimited` catches that, and it is the reading
//     every upgraded installation depends on (ADR-0018).

// concurrencyProbe records the highest number of that provider's jobs running at once.
//
// It measures rather than infers: the scheduler's own books could agree with a broken check, so the
// observation is taken inside the runner, at the moment the work is actually in flight.
type concurrencyProbe struct {
	mu      sync.Mutex
	inFly   map[string]int
	highest map[string]int
	// hold is how long each run occupies its slot, which is what gives a broken check time to
	// over-admit: with no delay two jobs for one provider could be sequential by luck.
	hold time.Duration
}

func newConcurrencyProbe(hold time.Duration) *concurrencyProbe {
	return &concurrencyProbe{inFly: map[string]int{}, highest: map[string]int{}, hold: hold}
}

func (p *concurrencyProbe) Run(_ context.Context, record job.Job) (Outcome, error) {
	p.mu.Lock()
	p.inFly[record.ProviderConfigID]++
	if p.inFly[record.ProviderConfigID] > p.highest[record.ProviderConfigID] {
		p.highest[record.ProviderConfigID] = p.inFly[record.ProviderConfigID]
	}
	p.mu.Unlock()

	time.Sleep(p.hold)

	p.mu.Lock()
	p.inFly[record.ProviderConfigID]--
	p.mu.Unlock()
	return Outcome{Status: job.StatusSucceeded}, nil
}

func (p *concurrencyProbe) peak(providerID string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.highest[providerID]
}

// TestSchedulerHonoursPerProviderConcurrency is the criterion, and it asserts both directions.
//
// The two directions matter equally and only one of them is the criterion's wording. The first —
// each provider stays at or under its limit — is 「同供应商并发不超过配置上限」. The second — the
// provider with no limit runs MORE than either limited one — is what proves the check DEPLOYS the
// queue rather than throttling all of it, and it is the assertion that would fail if the admission
// were written as a global gate: a build that stopped everything when any provider was full would
// satisfy the first direction and starve the rest.
func TestSchedulerHonoursPerProviderConcurrency(t *testing.T) {
	probe := newConcurrencyProbe(40 * time.Millisecond)
	service, repository, _, _ := newTestService(t, probe)
	// A is allowed one at a time, B two, and C is left unlimited.
	repository.setProviderLimits(map[string]int{"relay-a": 1, "relay-b": 2})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Enough workers that the limits, not the pool, are what bounds each provider.
	service.Start(ctx, 6, 5*time.Millisecond)

	submitFor := func(providerID string, count int) []string {
		t.Helper()
		ids := make([]string, 0, count)
		for index := 0; index < count; index++ {
			request := submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`)
			request.ProviderConfigID = providerID
			// The idempotency key is derived from the scope, so each submission needs its own.
			request.Scope = providerID + "-" + string(rune('a'+index))
			record, _, err := service.Submit(ctx, request)
			if err != nil {
				t.Fatalf("submitting for %s: %v", providerID, err)
			}
			ids = append(ids, record.ID)
		}
		return ids
	}
	all := append(append(submitFor("relay-a", 3), submitFor("relay-b", 4)...), submitFor("relay-c", 4)...)

	for _, id := range all {
		waitForStatus(t, repository, id, job.StatusSucceeded, 10*time.Second)
	}

	if peak := probe.peak("relay-a"); peak > 1 {
		t.Fatalf("relay-a ran %d jobs at once and its limit is 1", peak)
	}
	if peak := probe.peak("relay-b"); peak > 2 {
		t.Fatalf("relay-b ran %d jobs at once and its limit is 2", peak)
	}
	// The unlimited provider must have exceeded BOTH limits, which is the proof that the others
	// were not throttling it. Asserting only "<= workers" would pass for a global gate.
	cPeak := probe.peak("relay-c")
	if cPeak <= 2 {
		t.Fatalf("relay-c ran at most %d jobs at once, and it has no limit: a build that stops work whenever ANY provider is full would look like this", cPeak)
	}
	t.Logf("peaks: relay-a=%d (limit 1), relay-b=%d (limit 2), relay-c=%d (unlimited)", probe.peak("relay-a"), probe.peak("relay-b"), cPeak)

	if err := service.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

// TestAnExpiredLeaseDoesNotCount is ADR-0018 ruling 4.
//
// A job whose holder died holds no provider slot: `ClaimableCandidates` already offers it to the
// next worker, so counting it against the ceiling would mean a single crash permanently reduced that
// provider's throughput. The test stages exactly that state and asserts the provider still admits.
func TestAnExpiredLeaseDoesNotCount(t *testing.T) {
	probe := newConcurrencyProbe(20 * time.Millisecond)
	service, repository, clock, _ := newTestService(t, probe)
	repository.setProviderLimits(map[string]int{"relay-a": 1})
	ctx := context.Background()

	// A job left behind by a dead process: running, leased, and the lease already past.
	ghost := ghostJob("ghost-1", "relay-a", clock.Now().Add(-time.Hour))
	if err := repository.Insert(ctx, ghost); err != nil {
		t.Fatal(err)
	}

	counts, err := repository.ActiveProviderCounts(ctx, clock.Now())
	if err != nil {
		t.Fatalf("ActiveProviderCounts: %v", err)
	}
	if counts["relay-a"] != 0 {
		t.Fatalf("the expired lease counted as %d in-flight jobs, and its holder is gone", counts["relay-a"])
	}

	// And the provider still admits work, which is the consequence that matters.
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	service.Start(runCtx, 2, 5*time.Millisecond)
	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, repository, record.ID, job.StatusSucceeded, 5*time.Second)
}

// TestALiveLeaseDoesCount is the other half of ruling 4, and it is what keeps the test above from
// passing for the wrong reason: a count that returned zero for everything would satisfy it.
func TestALiveLeaseDoesCount(t *testing.T) {
	_, repository, clock, _ := newTestService(t, &scriptedRunner{})
	ctx := context.Background()

	live := ghostJob("live-1", "relay-a", clock.Now().Add(time.Hour))
	if err := repository.Insert(ctx, live); err != nil {
		t.Fatal(err)
	}
	counts, err := repository.ActiveProviderCounts(ctx, clock.Now())
	if err != nil {
		t.Fatalf("ActiveProviderCounts: %v", err)
	}
	if counts["relay-a"] != 1 {
		t.Fatalf("a job holding a live lease counted as %d, want 1", counts["relay-a"])
	}
}

// TestARestartedSchedulerStillHonoursTheLimit is ADR-0018 ruling 3.
//
// The staged state is what a force-closed application leaves: a job running with a lease that has
// not yet expired. A scheduler whose count lived in memory would start from zero and admit a full
// additional set against the same provider — this test is the one that fails if the count is not
// read from the store, and it is the reason the check is a query rather than a counter.
func TestARestartedSchedulerStillHonoursTheLimit(t *testing.T) {
	probe := newConcurrencyProbe(20 * time.Millisecond)
	// A SECOND service over the same repository is the restart: nothing of the first survives but
	// the rows.
	service, repository, clock, _ := newTestService(t, probe)
	repository.setProviderLimits(map[string]int{"relay-a": 1})
	ctx := context.Background()

	// What the force-closed process left: one job in flight with a live lease.
	inFlight := ghostJob("survivor-1", "relay-a", clock.Now().Add(time.Hour))
	if err := repository.Insert(ctx, inFlight); err != nil {
		t.Fatal(err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	service.Start(runCtx, 4, 5*time.Millisecond)

	// A new job for the same provider is submitted and must WAIT: its provider is already at its
	// ceiling even though THIS service is new and has run nothing.
	record, _, err := service.Submit(ctx, submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond) // several dispatch passes
	waiting, err := repository.Get(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if waiting.Status != job.StatusQueued {
		t.Fatalf("the restarted scheduler claimed a job for a provider already at its ceiling (status %q)", waiting.Status)
	}
	if peak := probe.peak("relay-a"); peak != 0 {
		t.Fatalf("the restarted scheduler ran %d jobs, and the surviving lease already filled the limit", peak)
	}

	// When the survivor's lease expires, the waiting job is admitted — the limit is a ceiling on
	// work in flight, not a permanent block.
	expired, err := repository.Get(ctx, inFlight.ID)
	if err != nil {
		t.Fatal(err)
	}
	expired.LeaseExpiresAt = clock.Now().Add(-time.Minute)
	expired.Status = job.StatusSucceeded
	expired.FinishedAt = clock.Now()
	if err := repository.Update(ctx, expired, expired.Revision); err != nil {
		t.Fatalf("settling the survivor: %v", err)
	}
	waitForStatus(t, repository, record.ID, job.StatusSucceeded, 5*time.Second)
}

// TestZeroMeansUnlimited is ADR-0018 ruling 1, and it is the one whose failure mode is an upgrade.
//
// Every existing `provider_configs` row gains the column with 0, so a build that read 0 as "run
// nothing" would stop image, video and audio work on every installation that had not set a limit.
func TestZeroMeansUnlimited(t *testing.T) {
	probe := newConcurrencyProbe(30 * time.Millisecond)
	service, repository, _, _ := newTestService(t, probe)
	// Zero explicitly, and an absent entry (which the real repository reports as zero too).
	repository.setProviderLimits(map[string]int{"relay-a": 0})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service.Start(ctx, 4, 5*time.Millisecond)

	var ids []string
	submitFor := func(providerID, scope string) {
		t.Helper()
		request := submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`)
		request.ProviderConfigID = providerID
		request.Scope = scope
		record, _, err := service.Submit(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, record.ID)
	}
	for index, scope := range []string{"a", "b", "c"} {
		submitFor("relay-a", scope)
		_ = index
	}
	submitFor("relay-absent", "d")

	for _, id := range ids {
		waitForStatus(t, repository, id, job.StatusSucceeded, 5*time.Second)
	}
	if peak := probe.peak("relay-a"); peak < 2 {
		t.Fatalf("a provider whose limit is 0 ran at most %d job at once, and 0 must mean unlimited", peak)
	}
	if peak := probe.peak("relay-absent"); peak < 1 {
		t.Fatal("a provider with no limit row ran nothing")
	}

	// A job with no provider at all is not constrained either: there is no ceiling to apply, and
	// refusing it would strand work the queue had accepted.
	request := submitRequest(job.JobTypeImageGeneration, `{"prompt":"a cat"}`)
	request.ProviderConfigID = ""
	request.Scope = "no-provider"
	record, _, err := service.Submit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, repository, record.ID, job.StatusSucceeded, 5*time.Second)
}

// ghostJob stages a job as a force-closed process leaves it: in flight, leased, and not settled.
//
// It is written through the repository rather than produced by a run, because a live runner always
// settles or hands back its claim — `returnClaim` exists for exactly that — so this state is one
// only a crash can leave, and a crash is what a test cannot perform on itself.
func ghostJob(id, providerID string, leaseUntil time.Time) job.Job {
	return job.Job{
		ID:               id,
		ProjectID:        "proj-1",
		EntityType:       "canvas_node",
		EntityID:         "node-1",
		JobType:          job.JobTypeImageGeneration,
		Status:           job.StatusRunning,
		Priority:         0,
		IdempotencyKey:   "ghost-" + id,
		ProviderConfigID: providerID,
		InputJSON:        `{"prompt":"left behind"}`,
		LeaseOwner:       "dead-worker",
		LeaseExpiresAt:   leaseUntil,
		AttemptCount:     1,
		MaxAttempts:      3,
		CreatedAt:        time.Now().UTC().Add(-2 * time.Hour),
		UpdatedAt:        time.Now().UTC().Add(-2 * time.Hour),
		StartedAt:        time.Now().UTC().Add(-2 * time.Hour),
		Revision:         1,
	}
}
