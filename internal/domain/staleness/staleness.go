// Package staleness owns the artifact-staleness vocabulary and the propagation
// rules of DOMAIN_MODEL section 15.
//
// Section 15.1 lists what can invalidate something downstream, section 15.2
// names the chain those changes travel along and classifies how severe the
// consequence is, and section 15.3 defines what a waiver must record. This
// package holds those three things as data plus pure functions, so the chain
// can be reasoned about and tested without a database.
//
// The analyzer that decides whether a specific edit is breaking or merely
// informational, and the regeneration workflows, belong to WP-07 and WP-10.
// What is here is the "stale 传播基础" the roadmap asks WP-05 for.
//
// The package performs no I/O and never mints an identifier (ADR-0005).
package staleness

import (
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// ArtifactType names a node of the section 15.2 chain.
type ArtifactType string

const (
	ArtifactSourceDocumentVersion ArtifactType = "source_document_version"
	ArtifactChapter               ArtifactType = "chapter"
	ArtifactStoryEntity           ArtifactType = "story_entity"
	ArtifactStoryEvent            ArtifactType = "story_event"
	ArtifactStoryRelation         ArtifactType = "story_relation"
	ArtifactCharacterState        ArtifactType = "character_state"
	ArtifactStorySkeleton         ArtifactType = "story_skeleton_version"
	ArtifactAdaptationStrategy    ArtifactType = "adaptation_strategy_version"
	ArtifactScriptVersion         ArtifactType = "script_version"
	ArtifactScene                 ArtifactType = "scene"
	ArtifactShot                  ArtifactType = "shot"
	ArtifactAssetVersion          ArtifactType = "asset_version"
	ArtifactDirectorPlan          ArtifactType = "director_plan_version"
	ArtifactStoryboardVersion     ArtifactType = "storyboard_version"
	ArtifactStoryboardItem        ArtifactType = "storyboard_item"
	ArtifactStoryboardPanel       ArtifactType = "storyboard_panel_version"
	ArtifactWorkflowRun           ArtifactType = "workflow_run"
	ArtifactStageRun              ArtifactType = "stage_run"
	ArtifactCanvasNode            ArtifactType = "canvas_node"
)

// Chain is the section 15.2 propagation order, earliest upstream first.
//
// The order is the specification's, and it gives the canonical sequence for
// reporting and for a deterministic walk. It is deliberately NOT the definition
// of what depends on what: the section 15.2 diagram is coarser than the schema,
// and several artifacts consume a much earlier one directly (a story event
// carries a chapter reference, so a chapter edit invalidates it immediately
// rather than one hop away). Direct dependencies are the graph below.
var Chain = []ArtifactType{
	ArtifactSourceDocumentVersion,
	ArtifactChapter,
	ArtifactStoryEntity,
	ArtifactStoryEvent,
	ArtifactStoryRelation,
	ArtifactCharacterState,
	ArtifactStorySkeleton,
	ArtifactAdaptationStrategy,
	ArtifactScriptVersion,
	ArtifactScene,
	ArtifactShot,
	ArtifactAssetVersion,
	ArtifactDirectorPlan,
	ArtifactStoryboardVersion,
	ArtifactStoryboardItem,
	ArtifactStoryboardPanel,
	ArtifactWorkflowRun,
	ArtifactStageRun,
	ArtifactCanvasNode,
}

// dependsOn lists, per artifact, the artifacts it consumes directly.
//
// Every edge corresponds to a column the schema actually enforces, so the graph
// is checkable rather than aspirational:
//
//	chapter              source_document_version_id
//	story_entity         source_document_versions of the document it was read
//	                     from, and story_entity_aliases.source_chapter_id
//	story_event          story_events.chapter_id, and location_entity_id plus
//	                     story_event_participants.story_entity_id
//	story_relation       source_entity_id and target_entity_id
//	character_state      character_entity_id and the event order it spans
//	skeleton / strategy  story_skeleton_event_links and
//	                     adaptation_strategy_event_links
//	scene                script_version_id and source_story_event_id
//	shot                 scene_id
//	asset_version        story_entity_id of the asset it depicts
//	director_plan        script_version_id
//	storyboard_version   script_version_id and director_plan_version_id
//	storyboard_item      storyboard_version_id and shot_id
//	storyboard_panel     storyboard_item_id
//	stage_run            workflow_run_id
//
// canvas_node has no entry because a projection's dependency is whatever its
// entity reference names, which is a per-row fact rather than a type rule. The
// projection writer marks the node from the entity it projects.
var dependsOn = map[ArtifactType][]ArtifactType{
	ArtifactChapter:            {ArtifactSourceDocumentVersion},
	ArtifactStoryEntity:        {ArtifactSourceDocumentVersion, ArtifactChapter},
	ArtifactStoryEvent:         {ArtifactChapter, ArtifactStoryEntity},
	ArtifactStoryRelation:      {ArtifactStoryEntity, ArtifactStoryEvent},
	ArtifactCharacterState:     {ArtifactStoryEntity, ArtifactStoryEvent},
	ArtifactStorySkeleton:      {ArtifactStoryEvent},
	ArtifactAdaptationStrategy: {ArtifactStoryEvent},
	ArtifactScriptVersion:      {ArtifactStorySkeleton, ArtifactAdaptationStrategy},
	ArtifactScene:              {ArtifactScriptVersion, ArtifactStoryEvent},
	ArtifactShot:               {ArtifactScene},
	ArtifactAssetVersion:       {ArtifactStoryEntity},
	ArtifactDirectorPlan:       {ArtifactScriptVersion},
	ArtifactStoryboardVersion:  {ArtifactScriptVersion, ArtifactDirectorPlan},
	ArtifactStoryboardItem:     {ArtifactStoryboardVersion, ArtifactShot},
	ArtifactStoryboardPanel:    {ArtifactStoryboardItem},
	ArtifactStageRun:           {ArtifactWorkflowRun},
}

// IsValidArtifactType reports whether an artifact kind may be persisted.
func IsValidArtifactType(value ArtifactType) bool {
	for _, candidate := range Chain {
		if candidate == value {
			return true
		}
	}
	return false
}

// Severity is how bad it is that an artifact went stale.
type Severity string

const (
	// SeverityBreaking prevents the artifact from being used in formal output.
	SeverityBreaking Severity = "breaking"
	// SeverityReviewRequired lets the artifact stay in place but demands a
	// re-review before it counts as current.
	SeverityReviewRequired Severity = "review_required"
	// SeverityInformational only records a notice.
	SeverityInformational Severity = "informational"
)

// Severities lists the documented severities in the schema's order.
var Severities = []Severity{SeverityBreaking, SeverityReviewRequired, SeverityInformational}

// IsValidSeverity reports whether a severity may be persisted.
func IsValidSeverity(value Severity) bool {
	for _, candidate := range Severities {
		if candidate == value {
			return true
		}
	}
	return false
}

// rank returns an artifact's position in the chain, or -1 when it is not a node.
func rank(value ArtifactType) int {
	for index, candidate := range Chain {
		if candidate == value {
			return index
		}
	}
	return -1
}

// OrchestrationTypes are the artifacts that sit outside the content chain.
//
// A workflow run records that something executed, a stage run records one
// attempt, and a canvas node is a projection of an entity. None of them is
// derived from a document in the way a script version is, so a content change
// does not reach them by following references: they are marked directly by the
// code that knows why (the projection writer marks a node when its entity
// changes, WP-07 marks a run when its inputs move). They are still valid
// ArtifactTypes so such a mark can be stored.
var OrchestrationTypes = []ArtifactType{ArtifactWorkflowRun, ArtifactStageRun, ArtifactCanvasNode}

// IsOrchestrationType reports whether an artifact sits outside the content chain.
func IsOrchestrationType(value ArtifactType) bool {
	for _, candidate := range OrchestrationTypes {
		if candidate == value {
			return true
		}
	}
	return false
}

// DirectDependents returns the artifacts that consume the given one directly.
//
// This is the review_required set: each of them holds a reference to the changed
// artifact, so the change is about their own input.
func DirectDependents(from ArtifactType) []ArtifactType {
	dependents := make([]ArtifactType, 0)
	for _, candidate := range Chain {
		for _, dependency := range dependsOn[candidate] {
			if dependency == from {
				dependents = append(dependents, candidate)
				break
			}
		}
	}
	return dependents
}

// Downstream returns every artifact reachable from the given one by following
// direct dependencies, in chain order and without duplicates.
//
// This is the informational set. The specification's own example of a source
// change travelling to the end of the chain means transitivity has to be
// followed, but a transitive artifact is not a direct consumer, which is why it
// is reported separately from DirectDependents.
func Downstream(from ArtifactType) []ArtifactType {
	if rank(from) < 0 {
		return nil
	}
	reachable := map[ArtifactType]bool{}
	frontier := []ArtifactType{from}
	for len(frontier) > 0 {
		next := make([]ArtifactType, 0, len(frontier))
		for _, current := range frontier {
			for _, dependent := range DirectDependents(current) {
				if dependent == from || reachable[dependent] {
					continue
				}
				reachable[dependent] = true
				next = append(next, dependent)
			}
		}
		frontier = next
	}
	ordered := make([]ArtifactType, 0, len(reachable))
	for _, candidate := range Chain {
		if reachable[candidate] {
			ordered = append(ordered, candidate)
		}
	}
	return ordered
}

// Classify decides how severe a mark is for an artifact that depends on the
// artifact that changed.
//
// Severity is about the dependency, not about distance along a list:
//
//   - a direct consumer is review_required, because the changed artifact is its
//     own input and section 15.2 says such an artifact "允许保留但必须重新审核".
//     This is the case PRD FR-030's acceptance describes ("删除或修改章节后，受影响
//     事实被标记为待复核"), where the affected facts hold the chapter reference;
//   - a transitive consumer is informational: the change reaches it through
//     intermediate artifacts, and those going stale is what carries the signal.
//     Marking the whole transitive closure review_required would make a single
//     chapter edit demand a re-review of every shot and panel in the project,
//     which the specification does not ask for and users would waive wholesale;
//   - a caller who knows a specific transitive artifact is load-bearing can
//     record SeverityBreaking explicitly, which is what that level is for.
func Classify(from, to ArtifactType) (Severity, bool) {
	if rank(from) < 0 || rank(to) < 0 || from == to {
		return "", false
	}
	for _, dependent := range DirectDependents(from) {
		if dependent == to {
			return SeverityReviewRequired, true
		}
	}
	for _, dependent := range Downstream(from) {
		if dependent == to {
			return SeverityInformational, true
		}
	}
	return "", false
}

// Mark is one staleness record.
type Mark struct {
	ArtifactType ArtifactType
	ArtifactID   string
	ProjectID    string
	Severity     Severity
	Reason       string
	UpstreamType ArtifactType
	UpstreamID   string
	// Waived records that a user chose to keep the artifact despite the mark.
	Waived             bool
	WaivedByDecisionID string
	WaivedReason       string
	// ClearedAt is non-empty once the mark no longer applies.
	ClearedAt string
}

// Validate checks the invariants the schema also enforces, plus the waiver rule
// from section 15.3.
func (m Mark) Validate() error {
	if !IsValidArtifactType(m.ArtifactType) {
		return InvalidError("The artifact type is not recognised.")
	}
	if strings.TrimSpace(m.ArtifactID) == "" {
		return InvalidError("A stale mark must name the artifact it marks.")
	}
	if strings.TrimSpace(m.ProjectID) == "" {
		return InvalidError("A stale mark must belong to a project.")
	}
	if !IsValidSeverity(m.Severity) {
		return InvalidError("The severity is not recognised.")
	}
	if m.UpstreamType != "" && !IsValidArtifactType(m.UpstreamType) {
		return InvalidError("The upstream artifact type is not recognised.")
	}
	// Section 15.3: keeping a stale artifact requires a UserGateDecision, a
	// reason, and a visible waiver. A waiver that names no decision would let
	// the artifact be presented as acceptable with nothing recording who
	// accepted it.
	if m.Waived {
		if strings.TrimSpace(m.WaivedByDecisionID) == "" {
			return InvalidError("A waiver must name the user decision that granted it.")
		}
		if strings.TrimSpace(m.WaivedReason) == "" {
			return InvalidError("A waiver must record why the stale artifact was kept.")
		}
	}
	return nil
}

// ValidateReReview reports whether a status change on a stale artifact respects
// section 15.2's re-review requirement.
//
// This is where the staleness package and the version vocabulary meet: a
// review_required or breaking mark means the artifact cannot simply be declared
// current again, so the only route back to approved runs through under_review.
func ValidateReReview(mark Mark, from, to versioning.Status) error {
	if mark.ClearedAt != "" {
		// The mark no longer applies, so it does not constrain the transition.
		return nil
	}
	if mark.Severity == SeverityInformational {
		return nil
	}
	if mark.Waived {
		// A waived mark still shows in the final review, but it does not block
		// the artifact from being worked on.
		return nil
	}
	if to == versioning.StatusApproved && from == versioning.StatusStale {
		return ConflictError("A stale artifact must be re-reviewed before it can be approved again.")
	}
	return nil
}
