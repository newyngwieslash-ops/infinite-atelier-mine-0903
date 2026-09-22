package jobs

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
)

// scriptedVideoRunner stands in for the infrastructure runner: it asserts that
// a resumed job arrives with its remote ID intact and never re-submits.
type scriptedVideoRunner struct {
	submits int
	polls   int
	// lastInput records what the runner was handed, so a test can assert that a resumed job is run
	// against the RECOVERED row rather than a copy taken before recovery.
	lastInput string
}

func (r *scriptedVideoRunner) Run(_ context.Context, record job.Job) (Outcome, error) {
	r.lastInput = record.InputJSON
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

// TestVideoRestartKeepsTheReferencesAJobWasSubmittedWith is ROADMAP item 13 with the case its first
// test did not cover.
//
// `video_restart_test.go`'s first test drives a job whose input carries a prompt and nothing else, so
// it proves the REMOTE ID survives a restart. It says nothing about the references: a resumed job is
// POLLED rather than re-submitted, and a build that dropped `input_json`'s reference fields on the
// way through recovery — or that re-read them from somewhere the restart did not preserve — would
// still pass it, because a poll is not given references at all.
//
// What makes this the case worth covering is the asymmetry FR-080 states: the frames and references
// are sent ONCE, at submission. If a restart caused a re-submission, the provider would be asked to
// make the clip a second time — the duplicate charge the first test exists to prevent — and the two
// tests together are what say the resume path is right for a job that carries them.
func TestVideoRestartKeepsTheReferencesAJobWasSubmittedWith(t *testing.T) {
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

	// The input a job carries when it names a first frame, a last frame and a reference — the three
	// fields ROADMAP item 2 adds and `videoInput` declares.
	input := `{"providerId":"prov-1","model":"video-1","prompt":"a clip","seconds":5,` +
		`"references":["cmVm"],"referenceMimes":["image/jpeg"],` +
		`"firstFrame":"Zmlyc3Q=","firstFrameMime":"image/png",` +
		`"lastFrame":"bGFzdA==","lastFrameMime":"image/png"}`
	submitted, _, err := service.Submit(ctx, SubmitRequest{
		ProjectID:        "proj-1",
		EntityType:       "shot",
		EntityID:         "shot-1",
		JobType:          job.JobTypeVideoGeneration,
		ProviderConfigID: "prov-1",
		InputJSON:        input,
		Scope:            "video",
	})
	if err != nil {
		t.Fatal(err)
	}

	// The process died with the job parked on a remote provider, exactly as the first test stages it.
	stored, err := repository.Get(ctx, submitted.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored.Status = job.StatusWaitingRemote
	stored.RemoteJobID = "remote-with-frames"
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

	// THE INPUT SURVIVES RECOVERY, field by field. This is the assertion the first test could not
	// make: a recovery pass that rebuilt the job from anything other than its own row would lose
	// these, and the loss would be invisible until somebody re-submitted.
	recovered, err := repository.Get(ctx, submitted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.InputJSON != input {
		t.Fatalf("the input changed across recovery:\n before %s\n after  %s", input, recovered.InputJSON)
	}
	for _, field := range []string{"references", "firstFrame", "lastFrame", "firstFrameMime", "lastFrameMime"} {
		if !strings.Contains(recovered.InputJSON, `"`+field+`"`) {
			t.Fatalf("the recovered input lost %q: %s", field, recovered.InputJSON)
		}
	}

	// And the job is POLLED to completion rather than re-submitted, which is the behaviour that makes
	// keeping the references matter: a re-submission would have sent them again and charged for a
	// second clip.
	service.Start(ctx, 1, 5*time.Millisecond)
	waitForStatus(t, repository, submitted.ID, job.StatusSucceeded, 3*time.Second)
	_ = service.Stop(ctx)

	if runner.submits != 0 {
		t.Fatalf("the restart re-submitted a job that carried frames and references (%d times)", runner.submits)
	}
	if runner.polls == 0 {
		t.Fatal("the restart did not poll the existing remote job")
	}
	// The runner was given the RECOVERED row rather than a copy taken before recovery, so what it
	// polls is what the database holds — remote id included.
	if runner.lastInput != input {
		t.Fatalf("the runner ran against a different input than the recovered row holds:\n row     %s\n runner  %s",
			input, runner.lastInput)
	}
}
