package staleness

import "errors"

// ErrorCategory is the stable taxonomy for staleness failures, matching the
// shape the project, asset and job domains use.
type ErrorCategory string

const (
	CategoryInvalidInput ErrorCategory = "invalid_input"
	CategoryConflict     ErrorCategory = "conflict"
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

// ConflictError reports a state conflict the caller can resolve by clearing or
// waiving the mark.
func ConflictError(message string) *Error {
	return &Error{Category: CategoryConflict, SafeMessage: message}
}

// AsError extracts a domain error from a wrapped chain.
func AsError(err error) (*Error, bool) {
	var target *Error
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}
