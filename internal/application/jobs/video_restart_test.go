package jobs

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
)

// scriptedVideoRunner stands in for the infrastructure runner: it asserts that
// a resumed job arrives with its remote ID intact and never re-submits.
type scriptedVideoRunner struct {
	submits int
	polls   int
}

func (r *scriptedVideoRunner) Run(_ context.Context, record job.Job) (Outcome, error) {
	if record.RemoteJobID == "" {
		r.submits++
		return Outcome{Status: job.StatusWaitingRemote, RemoteJobID: "remote-persisted"}, nil
	}
	r.polls++
	encoded, err := json.Marshal(map[string]any{"mode": "downloaded"})
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{Status: job.StatusSucceeded, ResultJSON: string(encoded), RemoteJobID: record.RemoteJobID}, nil
}

// TestVideoRestartResumesPollingWithoutResubmitting covers AC-MEDIA-001's
// "restart" item: a job waiting on a remote provider survives a restart, keeps
// its remote ID, and is polled rather than re-submitted. Re-submitting would
// risk a duplicate charge.
func TestVideoRestartResumesPollingWithoutResubmitting(t *testing.T) {
	repository := newMemoryRepository()
	clock := newFakeClock()
	runner := &scriptedVideoRunner{}
	service := NewService(Options{
		Repository: repository,
		Clock:      clock,
		IDs:        &counterIDs{},
		Runner:     runner,
		Policy:     job.RetryPolicy{BaseDelay: 0, MaxAttempts: 3},
	})
	ctx := context.Background()

	submitted, _, err := service.Submit(ctx, SubmitRequest{
		ProjectID:        "proj-1",
		EntityType:       "shot",
		EntityID:         "shot-1",
		JobType:          job.JobTypeVideoGeneration,
		ProviderConfigID: "prov-1",
		InputJSON:        `{"providerId":"prov-1","model":"video-1","prompt":"a clip"}`,
		Scope:            "video",
	})
	if err != nil {
		t.Fatal(err)
	}

	// The job was accepted and parked in waiting_remote before the restart, and
	// its worker's lease went stale when the process died.
	stored, err := repository.Get(ctx, submitted.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored.Status = job.StatusWaitingRemote
	stored.RemoteJobID = "remote-persisted"
	stored.LeaseOwner = "dead-worker"
	stored.LeaseExpiresAt = clock.Now().Add(-time.Hour)
	if err := repository.Update(ctx, stored, stored.Revision); err != nil {
		t.Fatal(err)
	}

	report, err := service.Recover(ctx)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if report.Resumed != 1 {
		t.Fatalf("report = %+v, want one resume", report)
	}
	recovered, err := repository.Get(ctx, submitted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.RemoteJobID != "remote-persisted" {
		t.Fatalf("remote id lost across restart: %q", recovered.RemoteJobID)
	}
	if recovered.Status != job.StatusWaitingRemote {
		t.Fatalf("status = %q, want waiting_remote", recovered.Status)
	}

	// Drive one scheduler pass through the service so the runner is invoked.
	service.Start(ctx, 1, 5*time.Millisecond)
	waitForStatus(t, repository, submitted.ID, job.StatusSucceeded, 3*time.Second)
	_ = service.Stop(ctx)

	if runner.submits != 0 {
		t.Fatalf("restart re-submitted the job %d time(s)", runner.submits)
	}
	if runner.polls == 0 {
		t.Fatal("restart did not poll the existing remote job")
	}
}
