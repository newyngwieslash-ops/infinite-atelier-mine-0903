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

// WireCategory is the category vocabulary of the agent-error contract, which is
// AGENT_CONTRACTS section 7.7's list.
//
// The two vocabularies are NOT the same, and the difference is deliberate rather
// than drift: this package's taxonomy distinguishes states the domain needs to act
// on (a not-found, a conflict, "the runtime is not composed"), while the wire form is
// the smaller set a caller outside the process branches on. What must not happen is
// the two drifting apart silently — which is exactly the failure this repository has
// been bitten by twice, per validation.go's own account — so WireCategories below is
// the closed set and a parity test asserts every domain category maps into it.
type WireCategory string

const (
	WireConfiguration WireCategory = "configuration"
	WireInput         WireCategory = "input"
	WireModel         WireCategory = "model"
	WireTool          WireCategory = "tool"
	WireTimeout       WireCategory = "timeout"
	WireCancelled     WireCategory = "cancelled"
	WireSecurity      WireCategory = "security"
	WireStorage       WireCategory = "storage"
	WireInternal      WireCategory = "internal"
)

// WireCategories is section 7.7's enum, in its order.
func WireCategories() []WireCategory {
	return []WireCategory{
		WireConfiguration, WireInput, WireModel, WireTool,
		WireTimeout, WireCancelled, WireSecurity, WireStorage, WireInternal,
	}
}

// IsValidWireCategory reports whether a value is one the contract accepts.
func IsValidWireCategory(category WireCategory) bool {
	for _, candidate := range WireCategories() {
		if candidate == category {
			return true
		}
	}
	return false
}

// Wire maps a domain category onto the contract's vocabulary.
//
// Each mapping is a decision rather than a rename, and the ones worth stating are
// the ones that lose information:
//
//   - not_found becomes "input": the caller named something that is not there, which
//     is a fact about the request. Section 7.7 has no not-found entry, and "internal"
//     would say the fault is ours when it is the caller's identifier that is stale.
//   - conflict becomes "input" for the same reason: a revision mismatch is a stale
//     caller, and the remedy — reload and retry — is a caller's.
//   - unavailable becomes "configuration", which this package's own comment on that
//     category already says it is: the runtime is not composed in this build.
//
// An unrecognised category maps to "internal", which is the honest answer for a
// category somebody added without deciding where it belongs.
func (c ErrorCategory) Wire() WireCategory {
	switch c {
	case CategoryInvalidInput, CategoryNotFound, CategoryConflict:
		return WireInput
	case CategoryStorage:
		return WireStorage
	case CategorySecurity:
		return WireSecurity
	case CategoryUnavailable:
		return WireConfiguration
	case CategoryModel:
		return WireModel
	case CategoryTool:
		return WireTool
	case CategoryCancelled:
		return WireCancelled
	default:
		return WireInternal
	}
}
