package desktop

import (
	"context"
	"errors"
	"strings"
	"testing"

	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
)

// video_batch_test.go grades the batch's own decisions: what it refuses, what it reports, and that one
// shot's failure does not take another's submission with it.
//
// # Why a double rather than a database
//
// The batch's job is to turn a LIST of shots into several `Submit` calls and to report each one. Every
// claim it makes is about that loop — the bound, the duplicate, the per-item refusal, the scope the key
// is built from — and a database would only add a second source of failure to the same assertions. The
// path from `Submit` to a stored row is the job service's own suite's business.

// recordingSubmitter records the requests it was given and can refuse specific shots.
type recordingSubmitter struct {
	requests []appjobs.SubmitRequest
	// refuse maps a shot id to the error its submission returns.
	refuse map[string]error
	// existing maps a shot id to a job that is already there, which is the duplicate case.
	existing map[string]job.Job
}

func (s *recordingSubmitter) Submit(_ context.Context, request appjobs.SubmitRequest) (job.Job, bool, error) {
	s.requests = append(s.requests, request)
	if failure, refused := s.refuse[request.EntityID]; refused {
		return job.Job{}, false, failure
	}
	if records, ok := s.existing[request.EntityID]; ok {
		return records, true, nil
	}
	return job.Job{
		ID: "job-" + request.EntityID, ProjectID: request.ProjectID,
		EntityType: request.EntityType, EntityID: request.EntityID,
		JobType: request.JobType, Status: job.StatusQueued,
	}, false, nil
}

// runBatch drives the batch's loop over a recording submitter.
//
// It reaches the unexported body rather than the Wails method, and that is the point: the exported
// command's only extra work is fetching the service from the binding, and going through it would make
// every test here a binding-attachment test instead of a test of the loop's decisions.
func runBatch(t *testing.T, submitter *recordingSubmitter, request SubmitVideoBatchRequest) (SubmitVideoBatchResultDTO, error) {
	t.Helper()
	return submitVideoBatch(context.Background(), submitter, request)
}

func batchRequest(shotIDs ...string) SubmitVideoBatchRequest {
	return SubmitVideoBatchRequest{
		ProjectID: "project-1", EpisodeID: "episode-1",
		ProviderID: "chan-1", Model: "video-1", Prompt: "a lantern on the ferry",
		Seconds: 4, ShotIDs: shotIDs,
	}
}

// TestABatchSubmitsOneJobPerShot is the happy path, and it asserts the SCOPE as well as the count.
//
// The scope is what the idempotency key is built from: a batch that submitted under a different scope
// than the single command would produce two jobs for one request, which is the defect the shared
// `submitOneVideo` exists to prevent.
func TestABatchSubmitsOneJobPerShot(t *testing.T) {
	submitter := &recordingSubmitter{}
	result, err := runBatch(t, submitter, batchRequest("shot-1", "shot-2", "shot-3"))
	if err != nil {
		t.Fatalf("SubmitVideoBatch: %v", err)
	}
	if len(result.Submitted) != 3 || len(result.Refused) != 0 {
		t.Fatalf("submitted %d refused %d", len(result.Submitted), len(result.Refused))
	}
	if len(submitter.requests) != 3 {
		t.Fatalf("%d submissions reached the service", len(submitter.requests))
	}
	for index, request := range submitter.requests {
		if request.EntityID != []string{"shot-1", "shot-2", "shot-3"}[index] {
			t.Fatalf("submission %d is for %q", index, request.EntityID)
		}
		if request.Scope != "submit-video-job" {
			t.Fatalf("submission %d uses the scope %q", index, request.Scope)
		}
		if request.EntityType != "shot" {
			t.Fatalf("submission %d names the entity type %q", index, request.EntityType)
		}
		if request.JobType != job.JobTypeVideoGeneration {
			t.Fatalf("submission %d is a %q job", index, request.JobType)
		}
	}
}

// TestABatchRefusesMoreThanTheBound is the cost limit.
//
// A video is billed by the second and takes minutes; the bound exists so one command cannot queue an
// unbounded amount of that. The refusal is a BINDING error rather than a per-item one, because the
// whole request is refused before anything is submitted — a batch that submitted the first six and
// refused the rest would leave work running under a report that said "refused".
func TestABatchRefusesMoreThanTheBound(t *testing.T) {
	shots := make([]string, 0, maxVideoBatch+1)
	for index := 0; index <= maxVideoBatch; index++ {
		shots = append(shots, "shot-"+itoaTest(index))
	}
	submitter := &recordingSubmitter{}
	if _, err := runBatch(t, submitter, batchRequest(shots...)); err == nil {
		t.Fatal("an oversized batch was accepted")
	}
	if len(submitter.requests) != 0 {
		t.Fatalf("an oversized batch submitted %d jobs before refusing", len(submitter.requests))
	}
}

func TestABatchRefusesAnEmptySelection(t *testing.T) {
	submitter := &recordingSubmitter{}
	// A user who pressed the button with nothing selected asked a question, and "0 of 0" is not an
	// answer — so this is refused rather than reported as a success that did nothing.
	if _, err := runBatch(t, submitter, batchRequest()); err == nil {
		t.Fatal("an empty batch was accepted")
	}
}

// TestARepeatedShotIsRefusedAndCountedOnce is the duplicate rule.
//
// The core's idempotency would turn a second identical submission into the same job, so the COUNT would
// still be right — but the second report row would claim a submission that never happened separately.
// Refusing it keeps the report meaning what it says.
func TestARepeatedShotIsRefusedAndCountedOnce(t *testing.T) {
	submitter := &recordingSubmitter{}
	result, err := runBatch(t, submitter, batchRequest("shot-1", "shot-1"))
	if err != nil {
		t.Fatalf("SubmitVideoBatch: %v", err)
	}
	if len(result.Submitted) != 1 {
		t.Fatalf("%d submissions for a repeated shot", len(result.Submitted))
	}
	if len(result.Refused) != 1 || result.Refused[0].ShotID != "shot-1" {
		t.Fatalf("the repetition was not refused: %+v", result.Refused)
	}
	if len(submitter.requests) != 1 {
		t.Fatalf("%d submissions reached the service", len(submitter.requests))
	}
}

// TestABatchItemNamingNoShotIsRefused keeps a whitespace id out of the service.
func TestABatchItemNamingNoShotIsRefused(t *testing.T) {
	submitter := &recordingSubmitter{}
	result, err := runBatch(t, submitter, batchRequest("shot-1", "", "   "))
	if err != nil {
		t.Fatalf("SubmitVideoBatch: %v", err)
	}
	if len(result.Submitted) != 1 {
		t.Fatalf("%d submissions", len(result.Submitted))
	}
	if len(result.Refused) != 2 {
		t.Fatalf("%d refusals: %+v", len(result.Refused), result.Refused)
	}
	if len(submitter.requests) != 1 {
		t.Fatalf("%d requests reached the service", len(submitter.requests))
	}
}

// TestOneShotsFailureDoesNotUndoAnothers is the per-item reporting rule.
//
// Aborting on the first refusal would leave the earlier submissions RUNNING with a report that said the
// batch failed — a user would see a failure and three jobs in the queue with nothing connecting them.
func TestOneShotsFailureDoesNotUndoAnothers(t *testing.T) {
	submitter := &recordingSubmitter{refuse: map[string]error{
		"shot-2": apperror.New("JOB_REFUSED", "invalid_input", false, "That shot cannot be generated.", nil),
	}}
	result, err := runBatch(t, submitter, batchRequest("shot-1", "shot-2", "shot-3"))
	if err != nil {
		t.Fatalf("SubmitVideoBatch: %v", err)
	}
	// The two shots AFTER the failure were submitted, which is what "does not undo" means: a loop that
	// stopped at the first refusal would have submitted shot-1 only.
	if len(result.Submitted) != 2 {
		t.Fatalf("%d submissions: %+v", len(result.Submitted), result.Submitted)
	}
	if len(result.Refused) != 1 {
		t.Fatalf("%d refusals", len(result.Refused))
	}
	// The refusal carries the application's SAFE message rather than a wrapper's wording.
	if result.Refused[0].Refused != "That shot cannot be generated." {
		t.Fatalf("the refusal reads %q", result.Refused[0].Refused)
	}
	if len(submitter.requests) != 3 {
		t.Fatalf("%d requests reached the service", len(submitter.requests))
	}
}

// TestAnUnclassifiedFailureIsReportedGenerically is the redaction rule at the batch's boundary.
//
// A raw error's text is not safe to render: it can carry a URL, a header or a provider's request
// payload. `toAppError` is the conversion that guarantees a user-facing message, and this asserts the
// batch goes through it rather than reading `err.Error()`.
func TestAnUnclassifiedFailureIsReportedGenerically(t *testing.T) {
	submitter := &recordingSubmitter{refuse: map[string]error{
		"shot-1": errors.New("dial tcp 10.0.0.7:443: connection refused"),
	}}
	result, err := runBatch(t, submitter, batchRequest("shot-1"))
	if err != nil {
		t.Fatalf("SubmitVideoBatch: %v", err)
	}
	if len(result.Refused) != 1 {
		t.Fatalf("%d refusals", len(result.Refused))
	}
	message := result.Refused[0].Refused
	if strings.Contains(message, "10.0.0.7") || strings.Contains(message, "connection refused") {
		t.Fatalf("the refusal leaked the underlying error: %q", message)
	}
	if message == "" {
		t.Fatal("the refusal carries no message")
	}
}

// TestADuplicateSubmissionIsReportedRatherThanHidden is the idempotency report.
//
// A user who submitted one shot, then the batch containing it, gets the SAME job back — that is what the
// shared idempotency key means — and the report says so rather than presenting an existing job as new
// work.
func TestADuplicateSubmissionIsReportedRatherThanHidden(t *testing.T) {
	submitter := &recordingSubmitter{existing: map[string]job.Job{
		"shot-1": {ID: "job-existing", Status: job.StatusRunning, EntityID: "shot-1"},
	}}
	result, err := runBatch(t, submitter, batchRequest("shot-1"))
	if err != nil {
		t.Fatalf("SubmitVideoBatch: %v", err)
	}
	if len(result.Submitted) != 1 {
		t.Fatalf("%d submissions", len(result.Submitted))
	}
	item := result.Submitted[0]
	if !item.Duplicate {
		t.Fatal("an existing job was reported as a new submission")
	}
	if item.JobID != "job-existing" || item.Status != string(job.StatusRunning) {
		t.Fatalf("the duplicate reports %+v", item)
	}
}

func TestABatchRequiresItsIdentifiers(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(SubmitVideoBatchRequest) SubmitVideoBatchRequest
	}{
		{"no project", func(r SubmitVideoBatchRequest) SubmitVideoBatchRequest { r.ProjectID = " "; return r }},
		{"no episode", func(r SubmitVideoBatchRequest) SubmitVideoBatchRequest { r.EpisodeID = ""; return r }},
		{"no provider", func(r SubmitVideoBatchRequest) SubmitVideoBatchRequest { r.ProviderID = ""; return r }},
		{"no model", func(r SubmitVideoBatchRequest) SubmitVideoBatchRequest { r.Model = "  "; return r }},
		{"too many seconds", func(r SubmitVideoBatchRequest) SubmitVideoBatchRequest {
			r.Seconds = maxVideoSeconds + 1
			return r
		}},
	}
	for _, testCase := range cases {
		submitter := &recordingSubmitter{}
		if _, err := runBatch(t, submitter, testCase.mutate(batchRequest("shot-1"))); err == nil {
			t.Fatalf("%s was accepted", testCase.name)
		}
		if len(submitter.requests) != 0 {
			t.Fatalf("%s reached the service", testCase.name)
		}
	}
}

// TestABatchWithoutAPromptUsesTheDocumentedDefault keeps a provider from being asked to invent.
//
// The single-shot command REFUSES an empty prompt, and the batch cannot: one prompt covers several
// shots, so a caller may reasonably state none. The default says what the request is, and this asserts
// that a provider never receives an empty one.
func TestABatchWithoutAPromptUsesTheDocumentedDefault(t *testing.T) {
	submitter := &recordingSubmitter{}
	request := batchRequest("shot-1")
	request.Prompt = "   "
	if _, err := runBatch(t, submitter, request); err != nil {
		t.Fatalf("SubmitVideoBatch: %v", err)
	}
	if len(submitter.requests) != 1 {
		t.Fatalf("%d requests", len(submitter.requests))
	}
	if !strings.Contains(submitter.requests[0].InputJSON, defaultBatchVideoPrompt) {
		t.Fatalf("the stored input carries no prompt: %s", submitter.requests[0].InputJSON)
	}
}

// itoaTest renders a small integer without importing strconv into this file's imports.
func itoaTest(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}
