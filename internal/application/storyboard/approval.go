package storyboard

import (
	"context"

	eventsapp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/events"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// The approval commands for the director plan and the storyboard version.
//
// They are the §2.5 switch, and the shape (read, CanApprove, supersede then
// approve in one transaction with the event) lives in the repository because all
// eight families share it. What is decided here is the parent a family groups by
// and the §17 event it emits.
//
// The event is recorded inside the transaction: §16 asks for both, and an
// approval with no record of it is the one outcome a governance stream must not
// have. A service with no recorder therefore refuses the approval rather than
// granting it quietly.

// approveEvent builds the event an approval emits.
func (s *Service) approveEvent(ctx context.Context, eventType event.Type, aggregateType event.AggregateType, versionID, projectID, traceID string) (event.Event, error) {
	if s.events == nil {
		return event.Event{}, storyboard.StorageError("The approval cannot be recorded, so it was not applied.", nil)
	}
	record, err := s.events.Build(ctx, eventsapp.Draft{
		Type:          eventType,
		AggregateType: aggregateType,
		AggregateID:   versionID,
		ProjectID:     projectID,
		TraceID:       traceID,
	})
	if err != nil {
		return event.Event{}, err
	}
	return record, nil
}

// ApproveDirectorPlanVersionRequest approves one director plan version.
type ApproveDirectorPlanVersionRequest struct {
	VersionID string
	TraceID   string
}

// ApproveDirectorPlanVersion switches which plan version is in force.
func (s *Service) ApproveDirectorPlanVersion(ctx context.Context, request ApproveDirectorPlanVersionRequest) (storyboard.DirectorPlanVersion, error) {
	if !s.Available() {
		return storyboard.DirectorPlanVersion{}, storageFailure()
	}
	version, err := s.directorPlans.GetDirectorPlanVersion(ctx, request.VersionID)
	if err != nil {
		return storyboard.DirectorPlanVersion{}, err
	}
	if err := versioning.CanApprove(version.Status, versioning.StatusApproved); err != nil {
		return storyboard.DirectorPlanVersion{}, err
	}
	// The episode carries the project the event is filed under, so it is read
	// rather than assumed.
	projectID, err := s.directorPlans.ProjectOfEpisode(ctx, version.EpisodeID)
	if err != nil {
		return storyboard.DirectorPlanVersion{}, err
	}
	record, err := s.approveEvent(ctx, event.DirectorPlanApproved, event.AggregateDirectorPlan,
		version.ID, projectID, request.TraceID)
	if err != nil {
		return storyboard.DirectorPlanVersion{}, err
	}
	if err := s.directorPlans.ApproveDirectorPlanVersion(ctx, version.ID, version.EpisodeID, version.Status, record); err != nil {
		return storyboard.DirectorPlanVersion{}, err
	}
	version.Status = versioning.StatusApproved
	return version, nil
}

// ApproveStoryboardVersionRequest approves one storyboard version.
type ApproveStoryboardVersionRequest struct {
	VersionID string
	TraceID   string
}

// ApproveStoryboardVersion switches which storyboard version is in force.
func (s *Service) ApproveStoryboardVersion(ctx context.Context, request ApproveStoryboardVersionRequest) (storyboard.StoryboardVersion, error) {
	if !s.Available() {
		return storyboard.StoryboardVersion{}, storageFailure()
	}
	version, err := s.storyboards.GetStoryboardVersion(ctx, request.VersionID)
	if err != nil {
		return storyboard.StoryboardVersion{}, err
	}
	if err := versioning.CanApprove(version.Status, versioning.StatusApproved); err != nil {
		return storyboard.StoryboardVersion{}, err
	}
	// A storyboard version names its storyboard, and the storyboard names the
	// episode; the project comes from the episode.
	board, err := s.storyboards.GetStoryboard(ctx, version.StoryboardID)
	if err != nil {
		return storyboard.StoryboardVersion{}, err
	}
	projectID, err := s.storyboards.ProjectOfStoryboard(ctx, board.ID)
	if err != nil {
		return storyboard.StoryboardVersion{}, err
	}
	record, err := s.approveEvent(ctx, event.StoryboardVersionApproved, event.AggregateStoryboard,
		version.ID, projectID, request.TraceID)
	if err != nil {
		return storyboard.StoryboardVersion{}, err
	}
	if err := s.storyboards.ApproveStoryboardVersion(ctx, version.ID, version.StoryboardID, version.Status, record); err != nil {
		return storyboard.StoryboardVersion{}, err
	}
	version.Status = versioning.StatusApproved
	return version, nil
}

// ApprovedDirectorPlanVersionID returns the episode's approved plan version id,
// or "" when none is approved.
func (s *Service) ApprovedDirectorPlanVersionID(ctx context.Context, episodeID string) (string, error) {
	if !s.Available() {
		return "", storageFailure()
	}
	return s.directorPlans.CurrentApprovedDirectorPlanVersionID(ctx, episodeID)
}

// ApprovedStoryboardVersionID returns the storyboard's approved version id, or
// "" when none is approved.
func (s *Service) ApprovedStoryboardVersionID(ctx context.Context, storyboardID string) (string, error) {
	if !s.Available() {
		return "", storageFailure()
	}
	return s.storyboards.CurrentApprovedStoryboardVersionID(ctx, storyboardID)
}
