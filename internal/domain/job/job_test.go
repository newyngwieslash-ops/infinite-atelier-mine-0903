package job

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

func TestStatusSetMatchesADR0004(t *testing.T) {
	// The stored vocabulary is fixed by docs/adr/0004. Guard it explicitly so
	// a drive-by rename cannot silently desynchronise code from schema.
	expected := []Status{
		"queued", "running", "waiting_remote", "downloading", "verifying",
		"retry_wait", "succeeded", "remote_only", "failed", "cancelled",
		"orphaned", "recovering",
	}
	got := AllStatuses()
	if len(got) != len(expected) {
		t.Fatalf("status count = %d, want %d", len(got), len(expected))
	}
	for index, status := range expected {
		if got[index] != status {
			t.Fatalf("status[%d] = %q, want %q", index, got[index], status)
		}
		if !IsValidStatus(status) {
			t.Fatalf("IsValidStatus(%q) = false", status)
		}
	}
	// The PRD prose spelling is not a stored value.
	if IsValidStatus("awaiting_remote") {
		t.Fatal("awaiting_remote must not be a stored status (ADR-0004)")
	}
	if IsValidStatus("") || IsValidStatus("pending") {
		t.Fatal("unknown status accepted")
	}
}

func TestTerminalStatusesRejectTransitions(t *testing.T) {
	terminal := []Status{StatusSucceeded, StatusRemoteOnly, StatusFailed, StatusCancelled, StatusOrphaned}
	for _, status := range terminal {
		if !status.IsTerminal() {
			t.Errorf("%q should be terminal", status)
		}
		if status.IsActive() {
			t.Errorf("%q should not be active", status)
		}
		// No terminal job may move back into a working state except through an
		// explicit retry transition.
		for _, target := range AllStatuses() {
			if target == status || target == StatusQueued || target == StatusRetryWait {
				continue
			}
			if CanTransition(status, target) {
				t.Errorf("terminal %q transitioned to %q", status, target)
			}
		}
		if !CanTransition(status, StatusQueued) {
			t.Errorf("terminal %q must allow an explicit retry to queued", status)
		}
	}
}

func TestActiveStatuses(t *testing.T) {
	active := []Status{StatusQueued, StatusRunning, StatusWaitingRemote, StatusDownloading, StatusVerifying, StatusRetryWait, StatusRecovering}
	for _, status := range active {
		if !status.IsActive() {
			t.Errorf("%q should be active", status)
		}
		if status.IsTerminal() {
			t.Errorf("%q should not be terminal", status)
		}
	}
}

func TestLifecycleTransitions(t *testing.T) {
	// The happy path from AC-FOUND-006.
	path := []Status{StatusQueued, StatusRunning, StatusSucceeded}
	for index := 0; index+1 < len(path); index++ {
		if !CanTransition(path[index], path[index+1]) {
			t.Fatalf("happy path blocked: %q -> %q", path[index], path[index+1])
		}
	}
	// Async path including the states only the PRD names.
	async := []Status{StatusQueued, StatusRunning, StatusWaitingRemote, StatusDownloading, StatusVerifying, StatusSucceeded}
	for index := 0; index+1 < len(async); index++ {
		if !CanTransition(async[index], async[index+1]) {
			t.Fatalf("async path blocked: %q -> %q", async[index], async[index+1])
		}
	}
	allowed := []struct{ from, to Status }{
		{StatusRunning, StatusRetryWait},
		{StatusRetryWait, StatusQueued},
		{StatusRunning, StatusFailed},
		{StatusRunning, StatusCancelled},
		{StatusRunning, StatusRemoteOnly},
		{StatusRecovering, StatusQueued},
		{StatusRecovering, StatusWaitingRemote},
		{StatusRecovering, StatusOrphaned},
		{StatusQueued, StatusRecovering},
	}
	for _, transition := range allowed {
		if !CanTransition(transition.from, transition.to) {
			t.Errorf("transition %q -> %q should be allowed", transition.from, transition.to)
		}
	}
	// A queued job cannot jump straight to succeeded without running.
	if CanTransition(StatusQueued, StatusSucceeded) {
		t.Error("queued -> succeeded must be rejected")
	}
	if CanTransition(StatusQueued, StatusDownloading) {
		t.Error("queued -> downloading must be rejected")
	}
	// Unknown values never transition.
	if CanTransition("bogus", StatusRunning) || CanTransition(StatusQueued, "bogus") {
		t.Error("unknown status accepted in transition")
	}
	// Self-transition is a no-op, not a failure.
	if !CanTransition(StatusRunning, StatusRunning) {
		t.Error("self transition should be allowed")
	}
}

func TestJobTypesAndCapabilities(t *testing.T) {
	for _, jobType := range AllJobTypes() {
		if !IsValidJobType(jobType) {
			t.Errorf("IsValidJobType(%q) = false", jobType)
		}
	}
	if IsValidJobType("thumbnail") {
		t.Error("unimplemented job type accepted")
	}
	if got := JobTypeImageGeneration.Capability(); got != "image" {
		t.Errorf("image capability = %q", got)
	}
	if got := JobTypeImageEdit.Capability(); got != "image" {
		t.Errorf("image edit capability = %q", got)
	}
	if got := JobTypeVideoGeneration.Capability(); got != "video" {
		t.Errorf("video capability = %q", got)
	}
	if got := JobTypeAudioGeneration.Capability(); got != "audio" {
		t.Errorf("audio capability = %q", got)
	}
}

func TestDependencyConditions(t *testing.T) {
	if !ConditionSuccess.Satisfied(StatusSucceeded) {
		t.Error("success not satisfied by succeeded parent")
	}
	if ConditionSuccess.Satisfied(StatusFailed) || ConditionSuccess.Satisfied(StatusCancelled) {
		t.Error("success satisfied by a non-success parent")
	}
	if !ConditionCompleted.Satisfied(StatusCancelled) || !ConditionCompleted.Satisfied(StatusFailed) {
		t.Error("completed should accept any terminal parent")
	}
	if ConditionCompleted.Satisfied(StatusRunning) {
		t.Error("completed satisfied by a non-terminal parent")
	}
	if !ConditionApproved.Satisfied(StatusSucceeded) {
		t.Error("approved not satisfied by succeeded parent")
	}
	if ConditionApproved.Satisfied(StatusFailed) {
		t.Error("approved satisfied by a failed parent")
	}
	for _, condition := range []DependencyCondition{ConditionSuccess, ConditionCompleted, ConditionApproved} {
		if !IsValidCondition(condition) {
			t.Errorf("IsValidCondition(%q) = false", condition)
		}
	}
	if IsValidCondition("whenever") {
		t.Error("unknown condition accepted")
	}
}

func TestIdempotencyKeyIsDeterministicAndScoped(t *testing.T) {
	first := IdempotencyKey("generate-image", []byte(`{"prompt":"a cat"}`))
	second := IdempotencyKey("generate-image", []byte(`{"prompt":"a cat"}`))
	if first != second {
		t.Fatalf("key not deterministic: %q vs %q", first, second)
	}
	if len(first) == 0 || len(first) > 128 {
		t.Fatalf("unexpected key length: %d", len(first))
	}
	// Different input, same scope → different key.
	if IdempotencyKey("generate-image", []byte(`{"prompt":"a dog"}`)) == first {
		t.Fatal("different input produced the same key")
	}
	// Same input, different scope → different key.
	if IdempotencyKey("edit-image", []byte(`{"prompt":"a cat"}`)) == first {
		t.Fatal("different scope produced the same key")
	}
}

func TestRetryPolicyBackoff(t *testing.T) {
	policy := RetryPolicy{BaseDelay: time.Second, MaxDelay: 10 * time.Second, MaxAttempts: 5}
	cases := []struct {
		next int
		want time.Duration
	}{
		{next: 1, want: 0},
		{next: 2, want: 1 * time.Second},
		{next: 3, want: 2 * time.Second},
		{next: 4, want: 4 * time.Second},
		{next: 5, want: 8 * time.Second},
		{next: 6, want: 10 * time.Second}, // capped
		{next: 20, want: 10 * time.Second},
	}
	for _, testCase := range cases {
		if got := policy.NextDelay(testCase.next); got != testCase.want {
			t.Errorf("NextDelay(%d) = %s, want %s", testCase.next, got, testCase.want)
		}
	}
	// Zero value falls back to defaults rather than panicking.
	zero := RetryPolicy{}
	if zero.Normalize().MaxAttempts != DefaultRetryPolicy().MaxAttempts {
		t.Error("zero policy did not normalize")
	}
}

func TestRetryPolicyShouldRetry(t *testing.T) {
	policy := RetryPolicy{MaxAttempts: 3}
	// Transient categories retry while budget remains.
	if !policy.ShouldRetry(1, CategoryNetwork) || !policy.ShouldRetry(2, CategoryTimeout) {
		t.Error("transient category should retry within budget")
	}
	// Budget exhausted.
	if policy.ShouldRetry(3, CategoryNetwork) {
		t.Error("retry allowed after the attempt budget")
	}
	// Permanent categories never retry, even with budget left.
	for _, category := range []ErrorCategory{CategorySecurity, CategoryConfiguration, CategoryUnauthorized, CategoryInvalidInput, CategoryContentPolicy, CategoryUnsupported} {
		if policy.ShouldRetry(1, category) {
			t.Errorf("category %q should not retry", category)
		}
	}
}

func TestCategoriesRetriability(t *testing.T) {
	retriable := []ErrorCategory{CategoryNetwork, CategoryTimeout, CategoryRemoteTransient, CategoryRateLimited, CategoryStorage}
	for _, category := range retriable {
		if !category.IsRetriable() {
			t.Errorf("%q should be retriable", category)
		}
	}
	permanent := []ErrorCategory{CategorySecurity, CategoryConfiguration, CategoryCancelled, CategoryInvalidInput, CategoryDependency, CategoryRemotePermanent}
	for _, category := range permanent {
		if category.IsRetriable() {
			t.Errorf("%q must not be retriable", category)
		}
	}
}

func TestClassifyBridgesProviderTaxonomy(t *testing.T) {
	// A provider error must map to the identically named job category without
	// the job package importing the provider package.
	cases := []struct {
		providerErr *provider.Error
		want        ErrorCategory
	}{
		{providerErr: provider.NewUnauthorizedError(), want: CategoryUnauthorized},
		{providerErr: provider.NewRateLimitedError(time.Second), want: CategoryRateLimited},
		{providerErr: provider.NewTimeoutError(), want: CategoryTimeout},
		{providerErr: provider.NewSecurityError(), want: CategorySecurity},
		{providerErr: provider.NewResponseInvalidError(), want: CategoryResponseInvalid},
	}
	for _, testCase := range cases {
		if got := Classify(testCase.providerErr); got != testCase.want {
			t.Errorf("Classify(provider %q) = %q, want %q", testCase.providerErr.Category, got, testCase.want)
		}
	}
	// Wrapped provider errors still classify.
	wrapped := wrapErr(provider.NewForbiddenError())
	if got := Classify(wrapped); got != CategoryForbidden {
		t.Errorf("Classify(wrapped) = %q, want forbidden", got)
	}
	// Context errors map to their canonical categories.
	if got := Classify(context.Canceled); got != CategoryCancelled {
		t.Errorf("Classify(context.Canceled) = %q", got)
	}
	if got := Classify(context.DeadlineExceeded); got != CategoryTimeout {
		t.Errorf("Classify(context.DeadlineExceeded) = %q", got)
	}
	if got := Classify(nil); got != CategoryStorage {
		t.Errorf("Classify(nil) = %q", got)
	}
	if got := Classify(errors.New("disk on fire")); got != CategoryStorage {
		t.Errorf("Classify(plain) = %q", got)
	}
}

func TestJobErrorMessagesAreSafe(t *testing.T) {
	// Safe messages must never interpolate a raw identifier.
	raw := "job-../../etc/passwd"
	jobErr := JobNotFoundError(raw)
	if jobErr.Error() == "" {
		t.Fatal("empty safe message")
	}
	if contains(jobErr.Error(), "..") || contains(jobErr.Error(), "/") {
		t.Fatalf("safe message leaked an identifier: %q", jobErr.Error())
	}
	for _, err := range []*Error{FailedJobError(CategoryNetwork, "safe"), CancelledJobError(), DuplicateJobError(), DependencyUnmetError()} {
		if err.SafeMessage == "" {
			t.Error("empty safe message")
		}
		if _, ok := AsJobError(error(err)); !ok {
			t.Error("AsJobError failed on a job error")
		}
	}
}

func TestJobLeaseHelpers(t *testing.T) {
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	held := Job{LeaseOwner: "worker-1", LeaseExpiresAt: now.Add(time.Minute)}
	if !held.LeaseHeld(now) {
		t.Error("live lease not detected")
	}
	expired := Job{LeaseOwner: "worker-1", LeaseExpiresAt: now.Add(-time.Minute)}
	if expired.LeaseHeld(now) {
		t.Error("expired lease reported as held")
	}
	unowned := Job{}
	if unowned.LeaseHeld(now) {
		t.Error("unowned job reported as leased")
	}
	if !(Job{Status: StatusQueued}).IsActive() {
		t.Error("queued job should be active")
	}
	if (Job{Status: StatusSucceeded}).IsActive() {
		t.Error("succeeded job should not be active")
	}
}

type wrapper struct{ err error }

func (w wrapper) Error() string { return "wrapped: " + w.err.Error() }
func (w wrapper) Unwrap() error { return w.err }

func wrapErr(err error) error { return wrapper{err: err} }

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
