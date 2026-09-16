package provider

import (
	"errors"
	"time"
)

// ErrorCategory is the stable provider failure taxonomy from
// docs/ARCHITECTURE.md §13.2. Categories are machine-readable; messages are
// safe for users and never contain headers, keys, or full URLs.
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
)

// Error is the provider-domain error. It separates the safe message users see
// from the diagnostic identifier, and carries retry guidance plus optional
// provider metadata (request ID and retry-after) that is itself non-secret.
type Error struct {
	Category    ErrorCategory
	SafeMessage string
	Diagnostic  string
	Retriable   bool
	HTTPStatus  int
	RequestID   string
	// RetryAfter, when non-zero, is the provider-advertised wait duration.
	RetryAfter time.Duration
}

// Error implements the error interface with only the safe message.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.SafeMessage
}

// IsRetriableNow reports whether the caller should retry after RetryAfter.
func (e *Error) IsRetriableNow() bool {
	return e != nil && e.Retriable
}

// AsProviderError extracts a *provider.Error, returning false otherwise.
func AsProviderError(err error) (*Error, bool) {
	var providerErr *Error
	if errors.As(err, &providerErr) {
		return providerErr, true
	}
	return nil, false
}

// IsCancellation reports whether err is a provider cancellation.
func IsCancellation(err error) bool {
	providerErr, ok := AsProviderError(err)
	if !ok {
		return false
	}
	return providerErr.Category == CategoryCancelled
}

// JobErrorCategory exposes this error's category as a plain string so the job
// domain can reuse the taxonomy without importing this package (avoiding a
// domain-to-domain dependency). Provider and job category names are identical
// by contract; see docs/adr/0004.
func (e *Error) JobErrorCategory() string {
	if e == nil {
		return ""
	}
	return string(e.Category)
}

// common safe messages. They intentionally avoid echoing hostnames, URLs,
// headers, or response bodies.
const (
	msgConfiguration   = "The provider configuration is incomplete or invalid."
	msgUnauthorized    = "The provider rejected the configured credentials."
	msgForbidden       = "The provider refused this request."
	msgRateLimited     = "The provider rate limit was reached."
	msgInvalidInput    = "The provider rejected the request payload."
	msgContentPolicy   = "The provider blocked the request content."
	msgNetwork         = "The provider could not be reached."
	msgTimeout         = "The provider request timed out."
	msgRemoteTransient = "The provider reported a temporary failure."
	msgRemotePermanent = "The provider reported a permanent failure."
	msgCancelled       = "The request was cancelled."
	msgUnsupported     = "This capability is not supported by the provider."
	msgResponseInvalid = "The provider returned an invalid response."
	msgStorage         = "Storing the provider result failed."
	msgSecurity        = "The request was blocked by the security policy."
)

// NewConfigurationError builds a configuration-category error.
func NewConfigurationError() *Error {
	return &Error{Category: CategoryConfiguration, SafeMessage: msgConfiguration, Retriable: false}
}

// NewUnauthorizedError builds an unauthorized error, typically for HTTP 401.
func NewUnauthorizedError() *Error {
	return &Error{Category: CategoryUnauthorized, SafeMessage: msgUnauthorized, Retriable: false}
}

// NewForbiddenError builds a forbidden error, typically for HTTP 403.
func NewForbiddenError() *Error {
	return &Error{Category: CategoryForbidden, SafeMessage: msgForbidden, Retriable: false}
}

// NewRateLimitedError builds a rate-limited error carrying retry-after.
func NewRateLimitedError(retryAfter time.Duration) *Error {
	return &Error{Category: CategoryRateLimited, SafeMessage: msgRateLimited, Retriable: true, RetryAfter: retryAfter}
}

// NewInvalidInputError builds an invalid-input error, typically for HTTP 400.
func NewInvalidInputError() *Error {
	return &Error{Category: CategoryInvalidInput, SafeMessage: msgInvalidInput, Retriable: false}
}

// NewContentPolicyError builds a content-policy error for moderation blocks.
func NewContentPolicyError() *Error {
	return &Error{Category: CategoryContentPolicy, SafeMessage: msgContentPolicy, Retriable: false}
}

// NewNetworkError builds a network-layer failure error.
func NewNetworkError() *Error {
	return &Error{Category: CategoryNetwork, SafeMessage: msgNetwork, Retriable: true}
}

// NewTimeoutError builds a timeout error.
func NewTimeoutError() *Error {
	return &Error{Category: CategoryTimeout, SafeMessage: msgTimeout, Retriable: true}
}

// NewRemoteTransientError builds an error for 5xx responses.
func NewRemoteTransientError() *Error {
	return &Error{Category: CategoryRemoteTransient, SafeMessage: msgRemoteTransient, Retriable: true}
}

// NewRemotePermanentError builds an error for unrecoverable remote failures.
func NewRemotePermanentError() *Error {
	return &Error{Category: CategoryRemotePermanent, SafeMessage: msgRemotePermanent, Retriable: false}
}

// NewCancelledError builds a cancellation error.
func NewCancelledError() *Error {
	return &Error{Category: CategoryCancelled, SafeMessage: msgCancelled, Retriable: false}
}

// NewUnsupportedError builds an unsupported-capability error.
func NewUnsupportedError() *Error {
	return &Error{Category: CategoryUnsupported, SafeMessage: msgUnsupported, Retriable: false}
}

// NewResponseInvalidError builds an invalid-response error for bad JSON or
// truncated streams.
func NewResponseInvalidError() *Error {
	return &Error{Category: CategoryResponseInvalid, SafeMessage: msgResponseInvalid, Retriable: false}
}

// NewStorageError builds a local storage failure error.
func NewStorageError() *Error {
	return &Error{Category: CategoryStorage, SafeMessage: msgStorage, Retriable: false}
}

// NewSecurityError builds a security-policy rejection error used by the SSRF
// guard. It is never retriable.
func NewSecurityError() *Error {
	return &Error{Category: CategorySecurity, SafeMessage: msgSecurity, Retriable: false}
}
