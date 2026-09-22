import io

p = "internal/application/script/service.go"
s = io.open(p, encoding="utf-8").read()

anchor = "// CreateSceneRequest adds one scene to a script version."
new = '''// ApproveStorySkeletonVersionRequest approves one skeleton version.
//
// It carries no expected revision, for the reason the script version's request does not: the concurrency
// token is the version's own STATUS, which the repository's compare-and-swap matches, plus the schema's
// partial unique index on the approved row.
type ApproveStorySkeletonVersionRequest struct {
	VersionID string
	// TraceID correlates the approval's event with the action that caused it. Optional: a manual
	// approval has no run to correlate with.
	TraceID string
}

// ApproveStorySkeletonVersion makes one version the episode's approved skeleton.
//
// It is the command AC-SCRIPT-001's last clause needs ("approved 唯一"), and it existed as a REPOSITORY
// method since WP-05 with no service command on top of it — so nothing could approve a skeleton at all.
// The canary found it: the pipeline's gate passed the STAGE and the episode still had no approved
// version, because moving a stage is not the same act as approving an artifact.
//
// The order is the script version's own: check the machine may approve, build the governance event (so a
// build with no recorder refuses rather than approving unrecorded), then switch — supersede the previous
// approval and approve this one in one transaction.
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
	episode, err := s.repository.GetEpisode(ctx, version.EpisodeID)
	if err != nil {
		return scriptdomain.StorySkeletonVersion{}, err
	}
	record, err := s.approveEvent(ctx, event.StorySkeletonVersionApproved, event.AggregateStorySkeleton,
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

// ApproveAdaptationStrategyVersion makes one version the episode's approved strategy, for the reasons
// the skeleton's twin records.
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
	record, err := s.approveEvent(ctx, event.AdaptationStrategyVersionApproved, event.AggregateAdaptationStrategy,
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

// CreateSceneRequest adds one scene to a script version.'''
assert anchor in s, "anchor"
s = s.replace(anchor, new, 1)
io.open(p, "w", encoding="utf-8", newline="\n").write(s)
print("service ok")
