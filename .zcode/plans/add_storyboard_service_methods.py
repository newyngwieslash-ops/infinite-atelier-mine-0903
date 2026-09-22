import io

p = 'internal/application/storyboard/service.go'
s = io.open(p, encoding='utf-8').read()

anchor = '''// ProjectOfEpisode returns the project an episode belongs to.'''
addition = '''// ListDirectorPlanVersions returns an episode's plan versions newest first.
func (s *Service) ListDirectorPlanVersions(ctx context.Context, episodeID string) ([]storyboard.DirectorPlanVersion, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	return s.directorPlans.ListDirectorPlanVersions(ctx, strings.TrimSpace(episodeID))
}

// ListStoryboardVersions returns a storyboard's versions newest first.
func (s *Service) ListStoryboardVersions(ctx context.Context, storyboardID string) ([]storyboard.StoryboardVersion, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	return s.storyboards.ListStoryboardVersions(ctx, strings.TrimSpace(storyboardID))
}

// UpdateStoryboardItemRequest changes ONE row of a board.
//
// EVERY FIELD IS POINTED TO, and a nil field means "leave this alone". The distinction
// matters for AC-BOARD-002: a single-shot redo changes the costume on one row, and a
// request that had to state every field would need the caller to read and echo the rest —
// which is how a redo silently rewrites a description nobody asked it to touch.
type UpdateStoryboardItemRequest struct {
	ItemID string
	// ExpectedRevision is the revision the caller read. A stale one is refused.
	ExpectedRevision int64
	// The optional fields. A nil pointer leaves the stored value unchanged.
	ShotSize               *string
	CameraAngle            *string
	CameraMovement         *string
	DurationSeconds        *int
	VisualDescription      *string
	ActionDescription      *string
	DialogueAudioSummary   *string
	ContinuityNotes        *string
	FirstFrameDescription  *string
	LastFrameDescription   *string
	VideoMotionDescription *string
	// Status, when set, must be one the domain admits.
	Status *versioning.Status
}

// UpdateStoryboardItem changes one row and nothing else.
//
// IT DOES NOT REWRITE THE VERSION. AC-BOARD-002's requirement is that a FIX to one shot
// leaves the others unchanged, and the strongest form of that is the one this command
// takes: the other rows are never written at all, so there is nothing for them to change
// through. A version-rewrite path would have to copy every other row, and "unchanged" would
// then rest on a copy being faithful.
func (s *Service) UpdateStoryboardItem(ctx context.Context, request UpdateStoryboardItemRequest) (storyboard.StoryboardItem, error) {
	if !s.Available() {
		return storyboard.StoryboardItem{}, storageFailure()
	}
	itemID := strings.TrimSpace(request.ItemID)
	if itemID == "" {
		return storyboard.StoryboardItem{}, storyboard.InvalidError("A storyboard update must name the row it changes.")
	}
	if request.ExpectedRevision < 1 {
		return storyboard.StoryboardItem{}, storyboard.InvalidError("A storyboard update must name the revision it was read at.")
	}
	item, err := s.items.GetStoryboardItem(ctx, itemID)
	if err != nil {
		return storyboard.StoryboardItem{}, err
	}
	// The stored row must still be the one the caller read, checked BEFORE any field is
	// applied: a conflict discovered halfway would leave a half-updated row to roll back.
	if item.Revision != request.ExpectedRevision {
		return storyboard.StoryboardItem{}, storyboard.ConflictError(
			"This storyboard row changed in another window. Reload it and try again.")
	}
	if request.ShotSize != nil {
		item.ShotSize = *request.ShotSize
	}
	if request.CameraAngle != nil {
		item.CameraAngle = *request.CameraAngle
	}
	if request.CameraMovement != nil {
		item.CameraMovement = *request.CameraMovement
	}
	if request.DurationSeconds != nil {
		item.DurationSeconds = *request.DurationSeconds
	}
	if request.VisualDescription != nil {
		item.VisualDescription = *request.VisualDescription
	}
	if request.ActionDescription != nil {
		item.ActionDescription = *request.ActionDescription
	}
	if request.DialogueAudioSummary != nil {
		item.DialogueAudioSummary = *request.DialogueAudioSummary
	}
	if request.ContinuityNotes != nil {
		item.ContinuityNotes = *request.ContinuityNotes
	}
	if request.FirstFrameDescription != nil {
		item.FirstFrameDescription = *request.FirstFrameDescription
	}
	if request.LastFrameDescription != nil {
		item.LastFrameDescription = *request.LastFrameDescription
	}
	if request.VideoMotionDescription != nil {
		item.VideoMotionDescription = *request.VideoMotionDescription
	}
	if request.Status != nil {
		item.Status = *request.Status
	}
	item.UpdatedAt = s.now()
	if err := item.Validate(); err != nil {
		return storyboard.StoryboardItem{}, err
	}
	// The revision the STORE is handed is the one read above, and the row it returns is the
	// caller's view with the stored revision advanced — so a caller that immediately edits
	// again uses the right number rather than the one it started with.
	stored, err := s.items.GetStoryboardItem(ctx, item.ID)
	if err != nil {
		return storyboard.StoryboardItem{}, err
	}
	item.Revision = stored.Revision
	if err := s.items.UpdateStoryboardItem(ctx, item, stored.Revision); err != nil {
		return storyboard.StoryboardItem{}, err
	}
	item.Revision = stored.Revision + 1
	return item, nil
}

// ProjectOfEpisode returns the project an episode belongs to.'''
assert anchor in s, "service anchor"
s = s.replace(anchor, addition, 1)

io.open(p, 'w', encoding='utf-8').write(s)
print("service methods added")
