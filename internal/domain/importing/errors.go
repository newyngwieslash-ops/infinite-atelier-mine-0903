package importing

import "errors"

// ErrorCategory is the stable taxonomy for import failures, matching the shape
// the project, asset and story domains use so the desktop layer maps them all
// the same way.
type ErrorCategory string

const (
	CategoryInvalidInput ErrorCategory = "invalid_input"
	CategoryConflict     ErrorCategory = "conflict"
	CategorySecurity     ErrorCategory = "security"
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

// SecurityError reports input refused for a safety reason.
//
// SECURITY section 17 lists security errors separately from invalid input, and
// the distinction is operational rather than cosmetic: a security refusal is
// never retried automatically ("安全错误默认不可自动重试"), so the category has to
// survive to the caller that decides.
func SecurityError(message string) *Error {
	return &Error{Category: CategorySecurity, SafeMessage: message}
}

// ConflictError reports a document the caller already has.
func ConflictError(message string) *Error {
	return &Error{Category: CategoryConflict, SafeMessage: message}
}

// StorageError wraps a failure that is not the input's fault.
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
