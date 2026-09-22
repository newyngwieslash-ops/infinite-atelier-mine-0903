package media

import "errors"

// ErrorCategory is the stable taxonomy for media failures, matching the shape the project,
// asset, job, staleness and memory domains use.
type ErrorCategory string

const (
	CategoryInvalidInput ErrorCategory = "invalid_input"
	CategoryNotFound     ErrorCategory = "not_found"
	CategoryConflict     ErrorCategory = "conflict"
	CategoryStorage      ErrorCategory = "storage"
	// CategoryEngineUnavailable is the machine's answer rather than the request's: no ffmpeg.
	// It is separate from storage so the UI can say "install ffmpeg" rather than "something
	// failed", which is the whole point of ARCHITECTURE's diagnostics rule.
	CategoryEngineUnavailable ErrorCategory = "engine_unavailable"
	// CategoryComposeFailed is the engine refusing or failing to produce the output.
	CategoryComposeFailed ErrorCategory = "compose_failed"
	// CategoryLimitExceeded is a file outside the probe limits — section 8.4's "先 probe".
	CategoryLimitExceeded ErrorCategory = "limit_exceeded"
)

// Error is a domain error with a safe message and no payload.
type Error struct {
	Category    ErrorCategory
	SafeMessage string
	Cause       error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.SafeMessage
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// InvalidError reports input this package refuses.
func InvalidError(message string) *Error {
	return &Error{Category: CategoryInvalidInput, SafeMessage: message}
}

// NotFoundError reports something that is not there.
func NotFoundError() *Error {
	return &Error{Category: CategoryNotFound, SafeMessage: "That record does not exist."}
}

// NotAvailableError reports an engine this machine does not have.
//
// The message says what is missing rather than what failed, because the reader's next step is to
// install it: "先 probe" and the diagnostics rule are about giving a person something to act on.
func NotAvailableError(message string) *Error {
	return &Error{Category: CategoryEngineUnavailable, SafeMessage: message}
}

// ComposeError reports the engine failing to produce the output.
//
// The CAUSE carries the engine's own diagnostics and is deliberately not the SafeMessage: an
// ffmpeg error line can quote a path, and the safe message is what a user reads.
func ComposeError(message string, cause error) *Error {
	return &Error{Category: CategoryComposeFailed, SafeMessage: message, Cause: cause}
}

// LimitError reports a file outside the probe limits.
func LimitError(message string) *Error {
	return &Error{Category: CategoryLimitExceeded, SafeMessage: message}
}

// StorageError reports a failure to reach a store.
func StorageError(message string, cause error) *Error {
	return &Error{Category: CategoryStorage, SafeMessage: message, Cause: cause}
}

// AsError extracts one of these errors from a wrapped chain.
func AsError(err error) (*Error, bool) {
	var target *Error
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}
