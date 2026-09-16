// Package apperror defines errors that can cross application boundaries safely.
package apperror

import (
	"crypto/rand"
	"encoding/hex"
)

// Error carries stable machine-readable fields without exposing its cause.
type Error struct {
	Code        string
	Category    string
	Retriable   bool
	SafeMessage string
	Diagnostic  string
	Cause       error
}

// New creates an application error with a random diagnostic identifier.
func New(code, category string, retriable bool, safeMessage string, cause error) *Error {
	return &Error{
		Code:        code,
		Category:    category,
		Retriable:   retriable,
		SafeMessage: safeMessage,
		Diagnostic:  newDiagnostic(),
		Cause:       cause,
	}
}

// Error returns only the message that is safe to present to a user.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.SafeMessage
}

// Unwrap exposes the underlying cause for programmatic error inspection.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func newDiagnostic() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(value[:])
}
