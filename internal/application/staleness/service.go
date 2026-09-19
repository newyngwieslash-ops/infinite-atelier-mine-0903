package staleness

import (
	"context"
	eventsapp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/events"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
	"strings"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/staleness"
)

// NewService builds the staleness service.
func NewService(options Options) *Service {
	return &Service{
		marks:      options.Marks,
		dependents: options.Dependents,
		projects:   options.Projects,
		clock:      options.Clock,
		ids:        options.IDs,
		events:     options.Events,
	}
}

// Available reports whether the service has the dependencies it needs. An
// unattached binding fails closed rather than panicking.
func (s *Service) Available() bool {
	return s != nil && s.marks != nil && s.dependents != nil && s.ids != nil
}

// recordEvent announces something that happened, if the service has a recorder.
//
// The nil check is not defensive padding: the recorder is an interface, so a
// Service composed without one holds a nil interface and calling a method on it
// panics. This is the one place that check lives, so no emit site has to repeat
// it and no emit site can forget it.
func (s *Service) recordEvent(ctx context.Context, draft eventsapp.Draft) {
	if s == nil || s.events == nil {
		return
	}
	s.events.RecordBestEffort(ctx, draft)
}

func (s *Service) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now().UTC()
	}
	return s.clock.Now().UTC()
}

// storageFailure is the fail-closed error for an unattached service.
//
// The staleness domain declares no storage error of its own — its categories
// are invalid_input and conflict, because a mark is either well formed or in
// conflict with another — so an unattachable store is reported through the
// shared application error, the way the backup service reports one.
func storageFailure() error {
	return apperror.New("STALENESS_UNAVAILABLE", "storage", false, "The staleness store is unavailable.", nil)
}

// MarkStaleRequest records that an artifact's input changed.
type MarkStaleRequest struct {
	ArtifactType staleness.ArtifactType
	ArtifactID   string
	ProjectID    string
	Severity     staleness.Severity
	Reason       string
	// UpstreamType and UpstreamID name the artifact whose change caused this.
	// They may be empty when the mark comes from a manual recompute rather than
	// from one upstream row, which migration 000012 allows.
	UpstreamType staleness.ArtifactType
	UpstreamID   string
}

// MarkStale records one stale mark.
//
// The mark is an upsert because the table is keyed by (artifact_type,
// artifact_id): re-marking an artifact that is already marked replaces the
// severity, reason and upstream rather than adding a second row for the reader
// to disambiguate. The domain's Mark.Validate decides what a mark may say,
// including section 15.3's rule that a waived mark must name the user decision
// that granted the waiver and why the artifact was kept.
func (s *Service) MarkStale(ctx context.Context, request MarkStaleRequest) (staleness.Mark, error) {
	if !s.Available() {
		return staleness.Mark{}, storageFailure()
	}
	mark := staleness.Mark{
		ArtifactType: request.ArtifactType,
		ArtifactID:   strings.TrimSpace(request.ArtifactID),
		ProjectID:    strings.TrimSpace(request.ProjectID),
		Severity:     request.Severity,
		Reason:       request.Reason,
		UpstreamType: request.UpstreamType,
		UpstreamID:   strings.TrimSpace(request.UpstreamID),
	}
	if err := mark.Validate(); err != nil {
		return staleness.Mark{}, err
	}
	if err := s.marks.UpsertMark(ctx, mark, s.now()); err != nil {
		return staleness.Mark{}, err
	}
	// Section 17's ArtifactMarkedStale. Best effort: the row is committed,
	// so the caller must not be told the command failed because the
	// announcement did not land.
	s.recordEvent(ctx, eventsapp.Draft{
		Type:          event.ArtifactMarkedStale,
		AggregateType: event.AggregateStaleness,
		AggregateID:   mark.ArtifactID,
		ProjectID:     mark.ProjectID,
	})
	return mark, nil
}

// ClearMarkRequest clears one artifact's mark.
type ClearMarkRequest struct {
	ArtifactType staleness.ArtifactType
	ArtifactID   string
}

// ClearMark records that a mark no longer applies.
//
// The row is kept and stamped rather than deleted, because migration 000012
// keeps it "so the history of what was invalidated survives". Clearing an
// already-cleared mark is not an error: the caller's intent is satisfied either
// way, and refusing it would make a retried command fail for no reason.
func (s *Service) ClearMark(ctx context.Context, request ClearMarkRequest) error {
	if !s.Available() {
		return storageFailure()
	}
	if _, found, err := s.marks.GetMark(ctx, request.ArtifactType, request.ArtifactID); err != nil {
		return err
	} else if !found {
		return staleness.InvalidError("This artifact has no stale mark to clear.")
	}
	_, err := s.marks.ClearMark(ctx, request.ArtifactType, request.ArtifactID, s.now())
	return err
}

// WaiveMarkRequest keeps a stale artifact on purpose.
type WaiveMarkRequest struct {
	ArtifactType staleness.ArtifactType
	ArtifactID   string
	// DecisionID names the UserGateDecision that granted the waiver. Section
	// 15.3 requires one, and the domain refuses a waiver without it.
	DecisionID string
	// Reason records why the artifact was kept. Section 15.3 requires it too.
	Reason string
}

// WaiveMark records that a user chose to keep a stale artifact.
//
// The waiver is validated by walking the stored mark through the domain's own
// Validate, so the rule lives in one place: a waiver with no decision or no
// reason is refused by section 15.3's check rather than by a copy of it here.
// A refused waiver writes nothing.
func (s *Service) WaiveMark(ctx context.Context, request WaiveMarkRequest) (staleness.Mark, error) {
	if !s.Available() {
		return staleness.Mark{}, storageFailure()
	}
	mark, found, err := s.marks.GetMark(ctx, request.ArtifactType, request.ArtifactID)
	if err != nil {
		return staleness.Mark{}, err
	}
	if !found {
		return staleness.Mark{}, staleness.InvalidError("This artifact has no stale mark to waive.")
	}
	mark.Waived = true
	mark.WaivedByDecisionID = strings.TrimSpace(request.DecisionID)
	mark.WaivedReason = request.Reason
	if err := mark.Validate(); err != nil {
		return staleness.Mark{}, err
	}
	if _, err := s.marks.WaiveMark(ctx, mark.ArtifactType, mark.ArtifactID, mark.WaivedByDecisionID, mark.WaivedReason, s.now()); err != nil {
		return staleness.Mark{}, err
	}
	return mark, nil
}

// ListMarks returns a project's marks, cleared and open alike.
func (s *Service) ListMarks(ctx context.Context, projectID string) ([]staleness.Mark, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	return s.marks.ListMarks(ctx, projectID)
}

// ListOpenMarks returns a project's marks that are not cleared.
func (s *Service) ListOpenMarks(ctx context.Context, projectID string) ([]staleness.Mark, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	return s.marks.ListOpenMarks(ctx, projectID)
}

// PropagateRequest asks what a change invalidates.
type PropagateRequest struct {
	ChangedType staleness.ArtifactType
	ChangedID   string
	ProjectID   string
	Reason      string
}

// PropagationResult reports what a propagation reached.
// PropagationResult reports what one propagation covered.
type PropagationResult struct {
	// ReviewRequired is how many review_required marks this call recorded. Those
	// are the artifacts holding a direct reference to the changed row, which the
	// domain's Classify explains.
	ReviewRequired int
	// Informational is how many informational marks this call recorded. Those
	// are the artifacts the change reaches through one or more intermediates.
	Informational int
	// Marked is how many marks were written in total, so a caller can tell an
	// empty propagation from one that was truncated by MaxPropagationMarks.
	Marked int
	// Truncated reports that the walk stopped early because it reached a bound,
	// so the reported reach is a lower bound rather than the whole blast
	// radius. A caller that cares can propagate again from a marked artifact.
	Truncated bool
	// Marks are the marks this call wrote, direct and transitive.
	Marks []staleness.Mark
}

// Propagation bounds. The dependency graph is acyclic and shallow, so these
// exist to make a future cycle or an unusually wide project terminate with a
// report instead of hanging or writing unbounded rows in one transaction.
const (
	// MaxPropagationDepth caps how many edges the walk follows from the change.
	MaxPropagationDepth = 12
	// MaxPropagationMarks caps how many rows one propagation may write.
	MaxPropagationMarks = 2000
)

// PropagateFrom marks what a change to one artifact invalidates.
//
// The walk moves outward from the changed artifact. At each node it asks the
// domain for that node's DIRECT dependents (staleness.DirectDependents) and
// resolves them to concrete rows through the DependentFinder, so only edges the
// graph actually declares are followed. Each row found becomes a node in the
// next frontier, which is how the closure is covered without asking about types
// nothing links to.
//
// Severity is decided by the domain's Classify against the artifact that
// CHANGED, never against the hop the walk happened to take. That distinction is
// the point: a story event is review_required after a chapter edit because it
// references the chapter, while a scene hanging off that event is informational
// because the chapter's edit reaches it through the event. Classifying per hop
// would mark the scene review_required and demand a re-review the specification
// does not ask for.
//
// A row whose owning project does not match is skipped: propagation can be
// triggered by a deletion, and a mark filed under the wrong project would be
// invisible where it belongs. An artifact already visited is never marked
// twice, so a diamond in the graph costs nothing.
func (s *Service) PropagateFrom(ctx context.Context, request PropagateRequest) (PropagationResult, error) {
	if !s.Available() {
		return PropagationResult{}, storageFailure()
	}
	if s.projects == nil {
		return PropagationResult{}, storageFailure()
	}
	if !staleness.IsValidArtifactType(request.ChangedType) {
		return PropagationResult{}, staleness.InvalidError("The changed artifact type is not recognised.")
	}
	changedID := strings.TrimSpace(request.ChangedID)
	if changedID == "" {
		return PropagationResult{}, staleness.InvalidError("A propagation must name the artifact that changed.")
	}
	projectID := strings.TrimSpace(request.ProjectID)
	if projectID == "" {
		return PropagationResult{}, staleness.InvalidError("A propagation must name the project the change belongs to.")
	}

	// visited keys an artifact the walk has already resolved, so a diamond in
	// the graph cannot mark the same row twice or loop the traversal.
	visited := map[string]bool{propagationKey(request.ChangedType, changedID): true}
	marks := make([]staleness.Mark, 0, 8)
	result := PropagationResult{}

	type node struct {
		artifactType staleness.ArtifactType
		artifactID   string
	}
	frontier := []node{{artifactType: request.ChangedType, artifactID: changedID}}

	for depth := 0; depth < MaxPropagationDepth && len(frontier) > 0; depth++ {
		next := make([]node, 0, len(frontier))
		for _, current := range frontier {
			for _, dependentType := range staleness.DirectDependents(current.artifactType) {
				refs, err := s.dependents.FindDependents(ctx, dependentType, current.artifactType, current.artifactID)
				if err != nil {
					return PropagationResult{}, err
				}
				for _, ref := range refs {
					artifactID := strings.TrimSpace(ref.ArtifactID)
					if artifactID == "" {
						continue
					}
					key := propagationKey(ref.ArtifactType, artifactID)
					if visited[key] {
						continue
					}
					visited[key] = true
					// The severity is the domain's judgement about the original
					// change, so a transitively reached artifact is a notice
					// rather than a re-review demand.
					severity, connected := staleness.Classify(request.ChangedType, ref.ArtifactType)
					if !connected {
						continue
					}
					owner, found, err := s.projects.ProjectFor(ctx, ref.ArtifactType, artifactID)
					if err != nil {
						return PropagationResult{}, err
					}
					if !found || owner != projectID {
						continue
					}
					if len(marks) >= MaxPropagationMarks {
						result.Truncated = true
						continue
					}
					mark := staleness.Mark{
						ArtifactType: ref.ArtifactType,
						ArtifactID:   artifactID,
						ProjectID:    projectID,
						Severity:     severity,
						Reason:       request.Reason,
						UpstreamType: current.artifactType,
						UpstreamID:   current.artifactID,
					}
					if err := mark.Validate(); err != nil {
						return PropagationResult{}, err
					}
					marks = append(marks, mark)
					next = append(next, node{artifactType: ref.ArtifactType, artifactID: artifactID})
				}
			}
		}
		frontier = next
	}
	if len(frontier) > 0 {
		// The loop hit MaxPropagationDepth with work still queued.
		result.Truncated = true
	}

	result.Marks = marks
	result.Marked = len(marks)
	if len(marks) > 0 {
		// One call, one transaction: a propagation that wrote half of its marks
		// and then failed would report a blast radius the store does not hold.
		if err := s.marks.UpsertMarks(ctx, marks, s.now()); err != nil {
			return PropagationResult{}, err
		}
	}
	for _, mark := range marks {
		switch mark.Severity {
		case staleness.SeverityReviewRequired:
			result.ReviewRequired++
		case staleness.SeverityInformational:
			result.Informational++
		}
	}
	return result, nil
}

func propagationKey(artifactType staleness.ArtifactType, artifactID string) string {
	return string(artifactType) + "\x00" + artifactID
}
