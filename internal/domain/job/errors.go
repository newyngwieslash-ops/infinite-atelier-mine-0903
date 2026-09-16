package job

import (
	"context"
	"errors"
	"time"
)

// ErrorCategory is the stable job failure taxonomy. It reuses the provider
// taxonomy vocabulary where the cause comes from a provider call, and adds the
// job-specific categories the scheduler needs to decide retry behavior.
type ErrorCategory string

const (
	CategoryConfiguration   ErrorCategory = "configuration"
	CategoryUnauthorized    ErrorCategory = "unauthorized"
	CategoryForbidden       ErrorCategory = "forbidden"
	CategoryRateLimited     ErrorCategory = "rate_limited"
	CategoryInvalidInput    ErrorCategory = "invalid_input"
	CategoryContentPolicy   ErrorCategory = "content_policy"
	CategoryNetwork         ErrorCategory = "network"
	CategoryTimeout         ErrorCategory = "timeout"
	CategoryRemoteTransient ErrorCategory = "remote_transient"
	CategoryRemotePermanent ErrorCategory = "remote_permanent"
	CategoryCancelled       ErrorCategory = "cancelled"
	CategoryUnsupported     ErrorCategory = "unsupported"
	CategoryResponseInvalid ErrorCategory = "response_invalid"
	CategoryStorage         ErrorCategory = "storage"
	CategorySecurity        ErrorCategory = "security"
	CategoryConflict        ErrorCategory = "conflict"
	// CategoryDependency marks a job whose parent requirement is unmet.
	CategoryDependency ErrorCategory = "dependency"
)

// Error is the job-domain error: a stable category plus a safe message and a
// diagnostic ID. It never carries provider payloads or secrets.
type Error struct {
	Category    ErrorCategory
	SafeMessage string
	Diagnostic  string
	Retriable   bool
}

// Error implements the error interface with only the safe message.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.SafeMessage
}

// AsJobError extracts a *job.Error, returning false otherwise.
func AsJobError(err error) (*Error, bool) {
	var jobErr *Error
	if errors.As(err, &jobErr) {
		return jobErr, true
	}
	return nil, false
}

// IsRetriable reports whether a category may be retried by the scheduler.
// Security, configuration, and policy failures are never retried: retrying
// them cannot succeed and, for security, would repeat a blocked action.
func (c ErrorCategory) IsRetriable() bool {
	switch c {
	case CategoryNetwork, CategoryTimeout, CategoryRemoteTransient, CategoryRateLimited, CategoryStorage:
		return true
	default:
		return false
	}
}

// FailedJobError builds a permanent failure.
func FailedJobError(category ErrorCategory, safeMessage string) *Error {
	return &Error{Category: category, SafeMessage: safeMessage, Retriable: category.IsRetriable()}
}

// CancelledJobError builds the cancellation outcome.
func CancelledJobError() *Error {
	return &Error{Category: CategoryCancelled, SafeMessage: "The job was cancelled.", Retriable: false}
}

// JobNotFoundError builds the not-found outcome for a query.
func JobNotFoundError(id string) *Error {
	return &Error{Category: CategoryConflict, SafeMessage: "The requested job no longer exists.", Diagnostic: safeID(id)}
}

// DuplicateJobError builds the idempotency-conflict outcome. The caller is
// expected to return the existing job instead of failing the user command.
func DuplicateJobError() *Error {
	return &Error{Category: CategoryConflict, SafeMessage: "An identical job was already submitted.", Retriable: false}
}

// DependencyUnmetError builds the outcome for a job whose dependency cannot be
// satisfied (for example a failed parent under a `success` requirement).
func DependencyUnmetError() *Error {
	return &Error{Category: CategoryDependency, SafeMessage: "A required earlier job did not finish successfully.", Retriable: false}
}

// safeID keeps a caller-supplied identifier from reaching a user message
// verbatim; only a short, charset-checked form is echoed.
func safeID(value string) string {
	const limit = 24
	if value == "" {
		return ""
	}
	trimmed := value
	if len(trimmed) > limit {
		trimmed = trimmed[:limit]
	}
	for _, r := range trimmed {
		alphanumeric := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !alphanumeric && r != '-' && r != '_' {
			return ""
		}
	}
	return trimmed
}

// RetryPolicy computes the deterministic backoff schedule. Jitter is
// deliberately absent: WP-03 runs a single local process, and deterministic
// timing is required for reproducible tests.
type RetryPolicy struct {
	// BaseDelay is the wait before the second attempt.
	BaseDelay time.Duration
	// MaxDelay caps the exponential growth.
	MaxDelay time.Duration
	// MaxAttempts is the total attempt budget including the first.
	MaxAttempts int
}

// DefaultRetryPolicy is the WP-03 production policy.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{BaseDelay: 2 * time.Second, MaxDelay: 5 * time.Minute, MaxAttempts: 3}
}

// Normalize fills unset fields with defaults so a zero value is usable.
func (p RetryPolicy) Normalize() RetryPolicy {
	if p.BaseDelay <= 0 {
		p.BaseDelay = DefaultRetryPolicy().BaseDelay
	}
	if p.MaxDelay <= 0 {
		p.MaxDelay = DefaultRetryPolicy().MaxDelay
	}
	if p.MaxAttempts < 1 {
		p.MaxAttempts = DefaultRetryPolicy().MaxAttempts
	}
	return p
}

// NextDelay returns the wait before attempt number `next` (1-based, so next=2
// is the delay before the second attempt). Growth is BaseDelay * 2^(next-2)
// capped at MaxDelay.
func (p RetryPolicy) NextDelay(next int) time.Duration {
	policy := p.Normalize()
	if next <= 1 {
		return 0
	}
	delay := policy.BaseDelay
	for step := 2; step < next; step++ {
		delay *= 2
		if delay >= policy.MaxDelay {
			return policy.MaxDelay
		}
	}
	if delay > policy.MaxDelay {
		return policy.MaxDelay
	}
	return delay
}

// ShouldRetry reports whether another attempt is allowed for a failure
// category at the given attempt count.
func (p RetryPolicy) ShouldRetry(attempts int, category ErrorCategory) bool {
	policy := p.Normalize()
	if attempts >= policy.MaxAttempts {
		return false
	}
	return category.IsRetriable()
}

// CategoryCarrier is implemented by foreign domain errors that expose their
// category as a string. The provider domain implements it, which lets the job
// domain reuse the provider taxonomy without importing that package (and so
// without coupling two domain aggregates).
type CategoryCarrier interface {
	JobErrorCategory() string
}

// Classify maps an arbitrary error to a job category. Errors that carry their
// own category are honoured; context cancellations and deadlines map to their
// canonical categories; everything else is treated as storage/internal so it
// is retried cautiously rather than silently dropped.
func Classify(err error) ErrorCategory {
	if err == nil {
		return CategoryStorage
	}
	var jobErr *Error
	if errors.As(err, &jobErr) {
		return jobErr.Category
	}
	var carrier CategoryCarrier
	if errors.As(err, &carrier) {
		category := ErrorCategory(carrier.JobErrorCategory())
		if category != "" {
			return category
		}
	}
	switch {
	case errors.Is(err, context.Canceled):
		return CategoryCancelled
	case errors.Is(err, context.DeadlineExceeded):
		return CategoryTimeout
	}
	return CategoryStorage
}
