package story

import "errors"

// ErrorCategory is the stable taxonomy for story-graph failures, matching the
// shape the project, asset and job domains use so the desktop layer maps them
// all the same way.
type ErrorCategory string

const (
	CategoryInvalidInput ErrorCategory = "invalid_input"
	CategoryNotFound     ErrorCategory = "not_found"
	CategoryConflict     ErrorCategory = "conflict"
	CategoryStorage      ErrorCategory = "storage"
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

// InvalidError reports input the domain refuses.
func InvalidError(message string) *Error {
	return &Error{Category: CategoryInvalidInput, SafeMessage: message}
}

// NotFoundError reports a missing document, entity, event or fact. The
// identifier is deliberately not echoed: it may be attacker-controlled text
// and the caller already knows what it asked for.
func NotFoundError() *Error {
	return &Error{Category: CategoryNotFound, SafeMessage: "The requested story item no longer exists."}
}

// ConflictError reports a state conflict the caller may be able to resolve by
// reloading or by recording a resolution instead.
func ConflictError(message string) *Error {
	return &Error{Category: CategoryConflict, SafeMessage: message}
}

// StorageError wraps a persistence failure without exposing it.
func StorageError(message string, cause error) *Error {
	return &Error{Category: CategoryStorage, SafeMessage: message, Cause: cause}
}

// AsError extracts a domain error from a wrapped chain.
func AsError(err error) (*Error, bool) {
	var target *Error
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}
