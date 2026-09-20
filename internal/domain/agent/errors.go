package agent

import "errors"

// ErrorCategory is the stable taxonomy for agent-runtime failures, matching the
// shape the project, workflow, asset and job domains use so the desktop layer maps
// them all the same way.
//
// CategorySecurity is the one this taxonomy adds over its siblings, and it is not
// cosmetic. AGENT_CONTRACTS section 14.2 lists 安全错误 among the failures that are
// never retried automatically, and AC-AGENT-001 requires an illegal tool call to
// come back as a distinct code. A refusal that arrived as invalid_input would be
// retried by anything that retries invalid input, which is exactly what the
// specification forbids.
type ErrorCategory string

const (
	CategoryInvalidInput ErrorCategory = "invalid_input"
	CategoryNotFound     ErrorCategory = "not_found"
	CategoryConflict     ErrorCategory = "conflict"
	CategoryStorage      ErrorCategory = "storage"
	CategorySecurity     ErrorCategory = "security"
	// CategoryUnavailable means the runtime is not composed. It is separate from
	// storage because it is a configuration fact rather than a persistence failure,
	// and section 14.2 does not retry it.
	CategoryUnavailable ErrorCategory = "unavailable"
	// CategoryModel covers the failures that are the model's: an output that does
	// not match its contract, a budget the attempt spent, an identifier it invented.
	// Section 7.7 lists it, and its members are the ones a repair round or a person
	// addresses rather than a transport retry.
	CategoryModel ErrorCategory = "model"
	// CategoryTool covers a tool that ran and failed, which is different from a call
	// the ACL refused (that is CategorySecurity).
	CategoryTool ErrorCategory = "tool"
	// CategoryCancelled reports a caller's cancellation, which section 15 requires
	// to be recorded as cancelled rather than failed.
	CategoryCancelled ErrorCategory = "cancelled"
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

// NotFoundError reports a missing run, message, tool call or skill version.
func NotFoundError() *Error {
	return &Error{Category: CategoryNotFound, SafeMessage: "The requested agent record no longer exists."}
}

// ConflictError reports a state conflict the caller may resolve by reloading.
func ConflictError(message string) *Error {
	return &Error{Category: CategoryConflict, SafeMessage: message}
}

// StorageError wraps a persistence failure without exposing it.
func StorageError(message string, cause error) *Error {
	return &Error{Category: CategoryStorage, SafeMessage: message, Cause: cause}
}

// SecurityError reports a refusal the specification says is never retried.
//
// The tool ACL raises this, and so does a run that would cross a project boundary.
// Its message is deliberately about the rule rather than about the attempt: a
// caller must not be able to learn what a tool would have done by reading the
// refusal.
func SecurityError(message string) *Error {
	return &Error{Category: CategorySecurity, SafeMessage: message}
}

// UnavailableError reports that no runtime is composed for this build.
func UnavailableError() *Error {
	return &Error{
		Category:    CategoryUnavailable,
		SafeMessage: "The agent runtime is not available in this build.",
	}
}

// AsError extracts a domain error from a wrapped chain.
func AsError(err error) (*Error, bool) {
	var target *Error
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}
