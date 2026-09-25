package appdiagnostics

import "errors"

// errors.go keeps this package's failures in the shape the project's other domains use: a stable
// category, a message that is safe to show a user, and a cause that stays for the log rather than
// crossing to a caller.

// ErrorCategory is the stable taxonomy for diagnostics failures.
type ErrorCategory string

const (
	CategoryInvalidInput ErrorCategory = "invalid_input"
	CategoryUnavailable  ErrorCategory = "unavailable"
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

// InvalidError reports input this package refuses.
func InvalidError(message string) *Error {
	return &Error{Category: CategoryInvalidInput, SafeMessage: message}
}

// UnavailableError reports a build with no reader attached.
//
// It is its own category rather than a storage fault because the caller's next step is different:
// a build with no diagnostics composed cannot produce a bundle at all, and saying so is more useful
// than a message about a store.
func UnavailableError() *Error {
	return &Error{
		Category:    CategoryUnavailable,
		SafeMessage: "Diagnostics are not available in this build.",
	}
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
