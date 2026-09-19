package script

import (
	"context"

	eventsapp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/events"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// The approval commands for the two script-side version families.
//
// Both are the §2.5 switch: read the version, run the domain's CanApprove, then
// supersede the currently approved version of the same parent before approving
// the target, in one transaction and with the event recorded in that same
// transaction. The shape lives in the repository (version_approval.go) because
// all eight families share it; what differs here is which parent a family groups
// by and which §17 event it emits.
//
// The event is recorded inside the transaction on purpose. §16 asks every
// command for both "transaction" and "event", and for an approval the two are
// one act: a stream that showed an approval that rolled back, or an approval
// with no record of who decided it, would both be wrong. So a service without a
// recorder refuses the approval rather than granting it silently.

// approveEvent builds the event an approval emits.
//
// It is shared by both families because they differ only in the event name and
// the aggregate they report.
func (s *Service) approveEvent(ctx context.Context, eventType event.Type, aggregateType event.AggregateType, versionID, projectID, traceID, payload string) (event.Event, error) {
	if s.events == nil {
		// Refusing is the safe direction: an unrecorded approval is a
		// governance hole, while a refused approval is merely unavailable.
		return event.Event{}, scriptdomain.StorageError("The approval cannot be recorded, so it was not applied.", nil)
	}
	record, err := s.events.Build(ctx, eventsapp.Draft{
		Type:          eventType,
		AggregateType: aggregateType,
		AggregateID:   versionID,
		ProjectID:     projectID,
		TraceID:       traceID,
		Payload:       payload,
	})
	if err != nil {
		return event.Event{}, err
	}
	return record, nil
}

// ApproveStorySkeletonVersionRequest approves one skeleton version.
type ApproveStorySkeletonVersionRequest struct {
	VersionID string
	// TraceID correlates the event with the action that caused it. Optional: a
	// manual approval has no run to correlate with.
	TraceID string
}

// ApproveStorySkeletonVersion switches which skeleton version is in force.
//
// The previous approval is superseded by the repository's transaction, so a
// caller never has to know which version it is replacing.
func (s *Service) ApproveStorySkeletonVersion(ctx context.Context, request ApproveStorySkeletonVersionRequest) (scriptdomain.StorySkeletonVersion, error) {
	if !s.Available() {
		return scriptdomain.StorySkeletonVersion{}, storageFailure()
	}
	version, err := s.repository.GetStorySkeletonVersion(ctx, request.VersionID)
	if err != nil {
		return scriptdomain.StorySkeletonVersion{}, err
	}
	if err := versioning.CanApprove(version.Status, versioning.StatusApproved); err != nil {
		return scriptdomain.StorySkeletonVersion{}, err
	}
	// The parent's project is what scopes the event, so it is resolved before
	// the event is built rather than assumed.
	episode, err := s.repository.GetEpisode(ctx, version.EpisodeID)
	if err != nil {
		return scriptdomain.StorySkeletonVersion{}, err
	}
	record, err := s.approveEvent(ctx, event.StorySkeletonApproved, event.AggregateStorySkeleton,
		version.ID, episode.ProjectID, request.TraceID, "")
	if err != nil {
		return scriptdomain.StorySkeletonVersion{}, err
	}
	if err := s.repository.ApproveStorySkeletonVersion(ctx, version.ID, version.EpisodeID, version.Status, record); err != nil {
		return scriptdomain.StorySkeletonVersion{}, err
	}
	version.Status = versioning.StatusApproved
	return version, nil
}

// ApproveAdaptationStrategyVersionRequest approves one strategy version.
type ApproveAdaptationStrategyVersionRequest struct {
	VersionID string
	TraceID   string
}

// ApproveAdaptationStrategyVersion switches which strategy version is in force.
func (s *Service) ApproveAdaptationStrategyVersion(ctx context.Context, request ApproveAdaptationStrategyVersionRequest) (scriptdomain.AdaptationStrategyVersion, error) {
	if !s.Available() {
		return scriptdomain.AdaptationStrategyVersion{}, storageFailure()
	}
	version, err := s.repository.GetAdaptationStrategyVersion(ctx, request.VersionID)
	if err != nil {
		return scriptdomain.AdaptationStrategyVersion{}, err
	}
	if err := versioning.CanApprove(version.Status, versioning.StatusApproved); err != nil {
		return scriptdomain.AdaptationStrategyVersion{}, err
	}
	episode, err := s.repository.GetEpisode(ctx, version.EpisodeID)
	if err != nil {
		return scriptdomain.AdaptationStrategyVersion{}, err
	}
	record, err := s.approveEvent(ctx, event.AdaptationStrategyApproved, event.AggregateStrategy,
		version.ID, episode.ProjectID, request.TraceID, "")
	if err != nil {
		return scriptdomain.AdaptationStrategyVersion{}, err
	}
	if err := s.repository.ApproveAdaptationStrategyVersion(ctx, version.ID, version.EpisodeID, version.Status, record); err != nil {
		return scriptdomain.AdaptationStrategyVersion{}, err
	}
	version.Status = versioning.StatusApproved
	return version, nil
}

// ApprovedSkeletonVersionID returns the episode's approved skeleton version id,
// or "" when none is approved.
func (s *Service) ApprovedSkeletonVersionID(ctx context.Context, episodeID string) (string, error) {
	if !s.Available() {
		return "", storageFailure()
	}
	return s.repository.CurrentApprovedSkeletonVersionID(ctx, episodeID)
}

// ApprovedStrategyVersionID returns the episode's approved strategy version id,
// or "" when none is approved.
func (s *Service) ApprovedStrategyVersionID(ctx context.Context, episodeID string) (string, error) {
	if !s.Available() {
		return "", storageFailure()
	}
	return s.repository.CurrentApprovedStrategyVersionID(ctx, episodeID)
}
