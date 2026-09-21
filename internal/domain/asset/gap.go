package asset

import (
	"strings"
	"time"
)

// gap.go is the Asset Gap Report: what the script needs and the asset library does not
// have.
//
// DOMAIN_MODEL section 15.2 puts it in the derivation chain between the script and the
// storyboard, and it is the artifact AC-BOARD-001 reads — "必需资产缺失时阻止批量生成"
// is a question only this report can answer. It is an agent's analysis that a USER
// approves, which is why it has the version/status/approval shape the other families
// have rather than being a computed view: a computed view has nobody to approve it, and
// section 10.1 gives asset_analysis a required user gate.
type GapStatus string

const (
	// GapMissing is a story fact the report found no asset for. It is the ordinary
	// value and the one that blocks a batch when the item is required.
	GapMissing GapStatus = "missing"
	// GapSatisfied is a story fact an asset already covers.
	GapSatisfied GapStatus = "satisfied"
)

// IsValidGapStatus reports whether the status is in the vocabulary.
func IsValidGapStatus(value GapStatus) bool {
	switch value {
	case GapMissing, GapSatisfied:
		return true
	default:
		return false
	}
}

// GapReport is one versioned analysis of what an episode's script needs.
type GapReport struct {
	ID string
	// EpisodeID and ScriptVersionID are the two facts the report is ABOUT, and both
	// are required: a report that named only the episode could not say which revision
	// of the script it analysed, and one that named only the version could not be
	// found without walking to it.
	EpisodeID       string
	ScriptVersionID string
	VersionNumber   int
	Status          VersionStatus
	// BasedOnVersionID links a re-analysis to the report it replaced, which is what
	// makes "the gaps a user already waived" answerable for a later draft.
	BasedOnVersionID string
	// SourceAgentRunID is the run that produced the analysis, so a reviewer can read
	// the reasoning behind a report that claims something is missing.
	SourceAgentRunID string
	// ApprovalTraceID links the report to the user gate decision that approved it.
	//
	// §10.1 gives the asset_analysis stage a required user gate, and that decision is
	// recorded on the WORKFLOW's stream rather than here — §17's event list has no name
	// for this artifact, which ADR-0013 records. This field is the CONNECTION between
	// the two, and it is what lets a reader find the decision instead of matching
	// timestamps.
	ApprovalTraceID string
	Summary         string
	CreatedByType   CreatedByType
	CreatedByID     string
	ChangeReason    string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Revision        int64
}

// Validate checks the invariants the schema also enforces.
func (r GapReport) Validate() error {
	if strings.TrimSpace(r.EpisodeID) == "" {
		return InvalidError("A gap report must belong to an episode.")
	}
	if strings.TrimSpace(r.ScriptVersionID) == "" {
		return InvalidError("A gap report must name the script version it analysed.")
	}
	if r.VersionNumber < 1 {
		return InvalidError("A gap report version number starts at one.")
	}
	if !IsValidVersionStatus(r.Status) {
		return InvalidError("The gap report status is not recognised.")
	}
	if !IsValidCreatedByType(r.CreatedByType) {
		return InvalidError("The gap report producer is not recognised.")
	}
	return nil
}

// GapItem is one line of the report: a story fact, and whether an asset covers it.
type GapItem struct {
	ID       string
	ReportID string
	// Ordinal is the item's position, and it is REQUIRED rather than derived: a
	// reviewer reading an approved report has to see the same order the approver did,
	// and a list recomputed from its rows could come back differently.
	Ordinal int
	// AssetType is which kind of asset this line asks for.
	AssetType Type
	// StoryEntityID names the STORY entity the asset is needed for, and it is what a
	// missing line has INSTEAD of an asset: an asset that does not exist has no id to
	// cite. StoryEntityName travels beside it so a reader does not have to resolve the
	// identifier to understand the line.
	StoryEntityID   string
	StoryEntityName string
	// AssetID is the asset that satisfies the line, or empty when none does.
	AssetID string
	Status  GapStatus
	// UsageRole is what the asset is used AS, matching the usage vocabulary: the same
	// character can be a reference for one scene and the subject of another.
	UsageRole string
	// Required is what a batch's gate reads. A missing required asset stops a batch;
	// a missing optional one is a note. It is a column rather than a rule about the
	// asset type because the same character can be essential in one episode and
	// background in another.
	Required  bool
	Notes     string
	CreatedAt time.Time
}

// Validate checks the line's own invariants.
//
// THE ONE RULE THAT MATTERS is the pairing of Required with Status, and it is stated
// here rather than in the service because a report is read by tools and by a UI that
// never touch the service. A line is only SATISFIED when it names the asset that
// satisfies it: `satisfied` with an empty asset id is a claim no reader can check, and
// it is exactly the shape a model would produce by filling the status in and leaving
// the reference blank.
func (i GapItem) Validate() error {
	if strings.TrimSpace(i.ReportID) == "" {
		return InvalidError("A gap item must belong to a report.")
	}
	if i.Ordinal < 1 {
		return InvalidError("A gap item's ordinal starts at one.")
	}
	if !IsValidType(i.AssetType) {
		return InvalidError("A gap item names an asset type that is not recognised.")
	}
	if !IsValidGapStatus(i.Status) {
		return InvalidError("A gap item's status is not recognised.")
	}
	satisfied := strings.TrimSpace(i.AssetID) != ""
	if i.Status == GapSatisfied && !satisfied {
		return InvalidError("A satisfied gap item must name the asset that satisfies it.")
	}
	if i.Status == GapMissing && satisfied {
		return InvalidError("A missing gap item cannot name an asset, because then it is not missing.")
	}
	if i.Status == GapMissing && strings.TrimSpace(i.StoryEntityID) == "" && strings.TrimSpace(i.StoryEntityName) == "" {
		return InvalidError("A missing gap item must name the story fact the asset is needed for.")
	}
	return nil
}

// Unsatisfied reports whether this line is a gap a batch has to care about.
//
// Both halves are needed: `required` says the production cannot proceed without it,
// and `missing` says it is not there. A required line that an existing asset satisfies
// is not a gap, and a missing optional asset is not a blocker — which is why the
// predicate is one function rather than each caller writing the conjunction.
func (i GapItem) Unsatisfied() bool {
	return i.Required && i.Status == GapMissing
}

// UnresolvedRequiredItems returns the required, unsatisfied lines of a report.
//
// The order is the report's own, so a refusal that names the first few names them in
// the order the approver saw.
func UnresolvedRequiredItems(items []GapItem) []GapItem {
	out := make([]GapItem, 0, len(items))
	for _, item := range items {
		if item.Unsatisfied() {
			out = append(out, item)
		}
	}
	return out
}

// CanApproveGapReport reports whether a report may move to approved.
//
// It is deliberately permissive about CONTENT and strict about the one thing a user
// cannot fix by approving: a report that still has unresolved required items is a
// report whose production is blocked, and approving it would record a decision the
// gate then has to refuse anyway. The rule the user is entitled to override is the
// WAIVER (§15.3), and that is a decision rather than an approval.
//
// A report with no items at all is refused too: an analysis that found nothing is
// either a script with no characters (which the sheet refuses) or a run that did not
// do its work, and both are worth a second look rather than an approval.
func CanApproveGapReport(status VersionStatus, items []GapItem) error {
	if !IsValidVersionStatus(status) {
		return InvalidError("The gap report status is not recognised.")
	}
	switch status {
	case VersionSuperseded, VersionStale:
		return conflictError("A superseded or stale gap report cannot be approved.")
	case VersionApproved:
		return conflictError("This gap report is already approved.")
	}
	if len(items) == 0 {
		return InvalidError("A gap report with no items cannot be approved, because it analysed nothing.")
	}
	if unresolved := UnresolvedRequiredItems(items); len(unresolved) > 0 {
		return conflictError("This report still has required assets missing, so approving it would not unblock the storyboard.")
	}
	return nil
}
