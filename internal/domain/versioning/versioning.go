// Package versioning owns the version vocabulary and invariants that every
// versioned aggregate shares.
//
// Eight families in the drama domain are versioned: project style guides,
// story skeletons, adaptation strategies, script versions, asset versions,
// director plans, storyboard versions and storyboard panels. DOMAIN_MODEL
// section 2.5 defines one set of statuses and one set of rules for all of them,
// and expressing that set eight times would let the copies drift apart, so it
// lives here and each family refers to it.
//
// The package performs no I/O and never mints an identifier (ADR-0005).
package versioning

// Status is the review state of a version.
type Status string

const (
	// StatusDraft is a version being authored and not yet submitted.
	StatusDraft Status = "draft"
	// StatusCandidate is a version submitted as one of several options.
	StatusCandidate Status = "candidate"
	// StatusUnderReview is a version a reviewer is examining.
	StatusUnderReview Status = "under_review"
	// StatusApproved is the version in force for its parent. At most one version
	// of a parent may hold it, which the schema enforces per family with a
	// partial unique index.
	StatusApproved Status = "approved"
	// StatusRejected is a version a reviewer refused.
	StatusRejected Status = "rejected"
	// StatusSuperseded is a version that was approved and then replaced by a
	// newer approval. It is kept, never deleted.
	StatusSuperseded Status = "superseded"
	// StatusDeprecated is a version still readable but no longer offered for
	// new work.
	StatusDeprecated Status = "deprecated"
	// StatusStale is a version whose upstream changed. Section 2.5 is explicit
	// that stale is not rejected and is not deleted automatically.
	StatusStale Status = "stale"
)

// Statuses lists the documented statuses in the schema's order.
var Statuses = []Status{
	StatusDraft, StatusCandidate, StatusUnderReview, StatusApproved,
	StatusRejected, StatusSuperseded, StatusDeprecated, StatusStale,
}

// IsValidStatus reports whether a status may be persisted.
func IsValidStatus(value Status) bool {
	for _, candidate := range Statuses {
		if candidate == value {
			return true
		}
	}
	return false
}

// CreatedByType records who produced a version.
type CreatedByType string

const (
	CreatedByUser      CreatedByType = "user"
	CreatedByAgent     CreatedByType = "agent"
	CreatedByMigration CreatedByType = "migration"
	CreatedBySystem    CreatedByType = "system"
)

// IsValidCreatedByType reports whether a producer kind may be persisted.
func IsValidCreatedByType(value CreatedByType) bool {
	switch value {
	case CreatedByUser, CreatedByAgent, CreatedByMigration, CreatedBySystem:
		return true
	default:
		return false
	}
}

// IsContentFrozen reports whether a version's content may no longer be edited
// in place.
//
// Section 2.5: "版本内容批准后不可原地编辑，编辑创建新版本". Approved content is
// what downstream artifacts were built from and what an approval switch
// analyses, so rewriting it would silently invalidate every reference to it.
// Superseded content is frozen for the same reason: it stays readable as the
// thing that was in force before.
func IsContentFrozen(status Status) bool {
	return status == StatusApproved || status == StatusSuperseded
}

// CanApprove reports whether a version may move to approved.
//
// The rules come from sections 2.3, 2.5 and 15:
//
//   - an already-approved version is not re-approved;
//   - a superseded version is a former approval and cannot become current again
//     without a new version, which is what "编辑创建新版本" means;
//   - a stale version cannot be approved directly. Section 15.2 classifies the
//     upstream change as review_required ("允许保留但必须重新审核") and section
//     15.3 lets a user keep the artifact with a waiver. Both of those presume
//     the artifact is retained *as stale*: re-affirming it is a re-review, so
//     the path is stale to under_review and then to approved. Allowing the
//     direct edge would let an artifact become current again without the
//     re-review the classification demands.
func CanApprove(current, next Status) error {
	if !IsValidStatus(current) || !IsValidStatus(next) {
		return InvalidError("The version status is not recognised.")
	}
	if next != StatusApproved {
		return InvalidError("CanApprove only decides the approved transition.")
	}
	switch current {
	case StatusApproved:
		return ConflictError("This version is already approved.")
	case StatusSuperseded:
		return ConflictError("A superseded version cannot be approved again. Create a new version instead.")
	case StatusStale:
		return ConflictError("A stale version must be re-reviewed before it can be approved again.")
	}
	return nil
}

// CanTransition reports whether a status change is allowed.
//
// The allowed edges are deliberately permissive within a review cycle and
// strict about the two states that end one:
//
//   - approved is reachable only through approval, which also requires
//     superseding the previous approval (see SupersedePrevious);
//   - an approved version can be superseded by a newer approval or marked
//     stale by an upstream change (section 15.1), and cannot do anything else;
//   - superseded and deprecated are terminal, because a version that was once
//     in force is history and must stay readable as such;
//   - stale may return to draft or under_review, because the response to
//     staleness is regeneration or a re-review. It cannot go straight to
//     approved: that would bypass the re-review section 15.2 requires.
func CanTransition(from, to Status) bool {
	if !IsValidStatus(from) || !IsValidStatus(to) {
		return false
	}
	if from == to {
		return false
	}
	switch from {
	case StatusDraft:
		switch to {
		case StatusCandidate, StatusUnderReview, StatusRejected, StatusDeprecated:
			return true
		}
	case StatusCandidate:
		switch to {
		case StatusUnderReview, StatusRejected, StatusDraft, StatusDeprecated:
			return true
		}
	case StatusUnderReview:
		switch to {
		case StatusApproved, StatusRejected, StatusCandidate, StatusDraft:
			return true
		}
	case StatusApproved:
		switch to {
		case StatusSuperseded:
			// A newer version took over.
			return true
		case StatusStale:
			// Section 15.1 lists "AssetVersion 默认批准版本切换" and the other
			// upstream changes as staleness triggers, and the artifacts they
			// invalidate are typically already approved. A stale mark therefore
			// has to be applicable to an approved version, or the propagation
			// chain could only ever mark drafts and would miss the artifacts
			// that are actually in use.
			return true
		}
	case StatusRejected:
		switch to {
		case StatusDraft, StatusCandidate, StatusDeprecated:
			return true
		}
	case StatusStale:
		switch to {
		// Regeneration and re-review, never a direct re-approval: section 15.2
		// says a review_required change must be re-reviewed, so the approved
		// state is reached through under_review.
		case StatusDraft, StatusUnderReview, StatusRejected, StatusDeprecated:
			return true
		}
	case StatusSuperseded, StatusDeprecated:
		// Terminal: kept as history.
		return false
	}
	return false
}

// SupersedePrevious reports whether approving a version in `next` requires the
// version currently in `current` to become superseded.
//
// Section 2.5: "批准新版本时旧批准版本变为 superseded", and "一个父实体同一时间
// 最多一个 approved 当前版本". The caller applies both in one transaction, which
// is what makes the schema's partial unique index satisfiable.
func SupersedePrevious(current Status) bool {
	return current == StatusApproved
}
