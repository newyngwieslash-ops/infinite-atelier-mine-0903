package agentruntime

import (
	"errors"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
)

// reject.go classifies the runtime's refusals.
//
// AGENT_CONTRACTS section 14 divides failures into retriable and not, and the
// division is operational: a retry controller that cannot tell them apart either
// retries what will never succeed or gives up on what would. So the classification
// is a type with a method rather than a judgement made at each call site.
//
// Two of section 14.2's entries are the ones this file exists for: "Tool 权限拒绝"
// and "安全错误" are never automatically retried, and both are refusals the ACL
// produces. The others — 401/403, content policy, invalid input, locked-rule
// conflicts, cost overruns, unknown billing and safety errors — are classified by
// the provider and job layers, which own them.

// Refusal is an error that knows whether section 14.1 applies.
type Refusal interface {
	error
	// Category is the section 7.7 taxonomy.
	Category() agent.ErrorCategory
	// Retriable reports whether a retry could succeed without a change to the
	// input, the configuration or the user's decision.
	Retriable() bool
}

// IsRetriable reports whether a failure may be retried automatically.
//
// The default is FALSE, and that direction is deliberate. An unrecognised error
// might be a transient network fault or might be a refusal; retrying the second
// is the behaviour section 14.2 exists to forbid, and the cost of not retrying the
// first is one failed run a user can re-run. Fail closed.
func IsRetriable(err error) bool {
	if err == nil {
		return false
	}
	var refusal Refusal
	if asRefusal(err, &refusal) {
		return refusal.Retriable()
	}
	// A domain error carries a category, and section 14.2 names three categories
	// outright: security, invalid input and (for this runtime) an unavailable
	// service are never retried.
	if domainErr, ok := agent.AsError(err); ok {
		switch domainErr.Category {
		case agent.CategorySecurity, agent.CategoryInvalidInput, agent.CategoryUnavailable:
			return false
		case agent.CategoryStorage, agent.CategoryConflict:
			// A storage failure may be transient and a conflict is resolved by
			// reloading, so both are left to the caller's retry policy rather than
			// classified here.
			return true
		}
	}
	// Everything else is not retriable.
	//
	// An earlier version of this function listed the runtime's own refusals here as
	// concrete types checked with errors.As, and two mutations showed those branches
	// were DEAD: every one of them already satisfies Refusal, so the interface check
	// above returns before any of them runs. Removing them changes no behaviour and
	// removes five statements that read like the rule they do not enforce.
	// TestIsRetriableFollowsSection14 covers each case through the interface instead.
	return false
}

// QuotaError reports that a run exceeded one of its budgets.
//
// It is separate from the tool-call and duration bounds because section 10.2's
// "FIX 超过 2 次转人工" and section 14.2's "成本超限" are both the same idea: this
// attempt has spent what it was allowed, and the answer is a person rather than a
// retry.
type QuotaError struct {
	// Limit names which bound was reached, so the message and the record agree.
	Limit string
	// Allowed is what the budget was, and Used what it reached.
	Allowed int
	Used    int
}

func (e *QuotaError) Error() string {
	if e == nil {
		return ""
	}
	switch e.Limit {
	case "tool_calls":
		return "The agent used all of its tool calls for this attempt."
	case "duration":
		return "The agent ran longer than its time budget for this attempt."
	case "auto_fix":
		return "The stage has been revised as often as it may be without a person deciding."
	default:
		return "The agent reached a budget for this attempt."
	}
}

// Category reports a model-side failure: the agent spent its budget rather than
// misbehaving.
func (e *QuotaError) Category() agent.ErrorCategory { return agent.CategoryModel }

// Retriable is false: a budget is spent, and section 14.2 lists cost overruns
// among the failures not to retry automatically.
func (e *QuotaError) Retriable() bool { return false }

// Code names the quota, so a caller branches on a stable value.
func (e *QuotaError) Code() string {
	if e == nil {
		return ""
	}
	return "agent.quota_" + e.Limit
}

// CancelledError reports that the caller cancelled the run.
//
// It is its own type because a cancellation arrives as an error like any other and
// section 15 requires a cancelled stage to be marked cancelled rather than failed.
// Telling the two apart by comparing against context.Canceled at each site is how
// a cancellation ends up recorded as a failure.
type CancelledError struct {
	Cause error
}

func (e *CancelledError) Error() string {
	if e == nil {
		return ""
	}
	return "The run was cancelled."
}

func (e *CancelledError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *CancelledError) Category() agent.ErrorCategory { return agent.CategoryCancelled }
func (e *CancelledError) Retriable() bool               { return false }

// SchemaError reports that a model's output did not satisfy its contract.
//
// It carries the violations rather than the raw output, for the reason WP-06
// records: the violations describe what to fix without quoting the document, and
// this text goes back into a prompt for the repair round.
type SchemaError struct {
	// Stage names which contract failed, so the record says what was expected.
	Stage string
	// Violations are the value-free descriptions the repair round sends back.
	Violations []Violation
	// Repaired reports whether this failure is the SECOND one, after the round.
	Repaired bool
}

// Violation is one reason an output was refused: a path and a rule, never a value.
type Violation struct {
	Path    string
	Message string
}

func (e *SchemaError) Error() string {
	if e == nil {
		return ""
	}
	if e.Repaired {
		return "The model's output still did not match the contract after one repair."
	}
	return "The model's output did not match the contract."
}

func (e *SchemaError) Category() agent.ErrorCategory { return agent.CategoryModel }

// Retriable is false because the repair round is the retry, and section 14.3
// allows exactly one. A caller that retried this again would be running a second
// repair round, which the contract forbids.
func (e *SchemaError) Retriable() bool { return false }

// Code is the stable identifier section 7.7's example names. It is the same code
// before and after the repair round, because it is the same failure: whether it
// was repairable is what Repaired records, and a caller that needed to branch on
// that would read the field rather than a second code.
func (e *SchemaError) Code() string { return "agent.output_schema_invalid" }

// ArtifactError reports that a stage named something that does not exist.
//
// It is AC-AGENT-003: a model that invents an identifier must fail validation, the
// run must not be marked successful and the workflow must not advance. The error
// carries the reference rather than the artifact's content, because the point is
// that there is no content.
type ArtifactError struct {
	// EntityType and EntityID are what the model claimed it produced.
	EntityType string
	EntityID   string
}

func (e *ArtifactError) Error() string {
	if e == nil {
		return ""
	}
	return "The stage reported an artifact that does not exist."
}

func (e *ArtifactError) Category() agent.ErrorCategory { return agent.CategoryModel }

// Retriable is false: an invented identifier is not a transient fault, and
// retrying would ask the same model for the same answer.
func (e *ArtifactError) Retriable() bool { return false }

// Code is the stable identifier for a hallucinated reference.
func (e *ArtifactError) Code() string { return "agent.artifact_not_found" }

// asRefusal finds a Refusal through a wrapped chain.
//
// It is a function rather than a direct errors.As call so the Refusal interface
// can carry the two methods the classification needs while errors.As still works
// on a pointer target: an interface target needs the same dance as a concrete
// one, and doing it once here keeps IsRetriable readable.
func asRefusal(err error, target *Refusal) bool {
	var refusal Refusal
	if errors.As(err, &refusal) {
		*target = refusal
		return true
	}
	return false
}
