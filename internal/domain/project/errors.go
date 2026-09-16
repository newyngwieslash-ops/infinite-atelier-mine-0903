package project

import (
	"errors"
)

// ErrorCategory is the stable taxonomy for project and canvas failures. It
// mirrors the job domain's shape so the desktop layer can map either to one
// application error without special cases.
type ErrorCategory string

const (
	CategoryInvalidInput ErrorCategory = "invalid_input"
	CategoryNotFound     ErrorCategory = "not_found"
	CategoryConflict     ErrorCategory = "conflict"
	CategoryStorage      ErrorCategory = "storage"
	CategoryImport       ErrorCategory = "import"
	CategorySecurity     ErrorCategory = "security"
)

// Error is a domain error: a stable category, a message safe to show a user,
// and an optional diagnostic reference. It never carries SQL text, file paths,
// or payloads.
type Error struct {
	Category    ErrorCategory
	SafeMessage string
	Diagnostic  string
	Cause       error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Diagnostic != "" {
		return e.SafeMessage + " (" + e.Diagnostic + ")"
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

// NotFoundError reports a missing entity. The identifier is not echoed: it may
// be attacker-controlled text, and the caller already knows what it asked for.
func NotFoundError() *Error {
	return &Error{Category: CategoryNotFound, SafeMessage: "The requested item no longer exists."}
}

// ConflictError reports a revision mismatch or a duplicate.
func ConflictError(message string) *Error {
	return &Error{Category: CategoryConflict, SafeMessage: message}
}

// RevisionMismatchError is the conflict a compare-and-swap update reports when
// the row moved under the caller.
func RevisionMismatchError() *Error {
	return &Error{
		Category:    CategoryConflict,
		SafeMessage: "This item changed in another window. Reload it and try again.",
	}
}

// StorageError wraps a persistence failure.
func StorageError(message string, cause error) *Error {
	return &Error{Category: CategoryStorage, SafeMessage: message, Cause: cause}
}

// ImportError names the migration stage that failed, so a partially applied
// import can be reported against the stage rather than as a generic failure
// (AC-LEGACY-003).
type ImportError struct {
	Stage       string
	SafeMessage string
	Cause       error
}

func (e *ImportError) Error() string {
	if e == nil {
		return ""
	}
	return e.SafeMessage + " (stage " + e.Stage + ")"
}

func (e *ImportError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Category satisfies the job domain's CategoryCarrier convention, so an import
// failure crossing into a job-facing error path keeps its category.
func (e *ImportError) Category() ErrorCategory { return CategoryImport }

// AsError extracts a domain error from a wrapped chain.
func AsError(err error) (*Error, bool) {
	var target *Error
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

// AsImportError extracts an import error, which names its stage.
func AsImportError(err error) (*ImportError, bool) {
	var target *ImportError
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}
