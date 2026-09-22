package asset

import "errors"

// ErrorCategory is the stable taxonomy for asset failures, matching the shape
// the project domain and the job domain use so the desktop layer maps them all
// the same way.
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
	// Duplicate marks a conflict that says the work was ALREADY DONE rather than that the
	// caller's copy is stale. The two send a caller to different places — one to "continue,
	// this is finished", the other to "reload and try again" — and a message match cannot
	// tell them apart.
	Duplicate bool
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

// conflictError is the internal alias kept for readability at the call sites
// inside the domain, where "conflict" is unambiguous.
func conflictError(message string) *Error {
	return &Error{Category: CategoryConflict, SafeMessage: message}
}

// DuplicateJobVersionError reports that a generation job already produced a version.
//
// IT IS A NAMED SLOT RATHER THAN A MESSAGE a caller matches on, and it exists because the
// alternative is exactly what this repository refuses: a caller deciding "was this a
// duplicate?" by looking for a word in a safe message. That is a check that breaks when
// somebody improves the wording, and it fails OPEN — a reworded message means a duplicate is
// read as a fresh failure.
//
// The category is conflict because the caller's next step is to stop rather than to retry.
func DuplicateJobVersionError() *Error {
	return &Error{
		Category:    CategoryConflict,
		SafeMessage: "That generation job already produced a version of this asset.",
		Duplicate:   true,
	}
}

// ConflictError reports a conflict the caller may be able to resolve by
// reloading, such as a revision mismatch.
func ConflictError(message string) *Error {
	return &Error{Category: CategoryConflict, SafeMessage: message}
}

// NotFoundError reports a missing asset or version.
func NotFoundError() *Error {
	return &Error{Category: CategoryNotFound, SafeMessage: "The requested asset no longer exists."}
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
