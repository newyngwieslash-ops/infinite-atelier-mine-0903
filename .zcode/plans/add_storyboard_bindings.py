import io

p = 'internal/desktop/drama_binding.go'
s = io.open(p, encoding='utf-8').read()

anchor = '''// ApprovePanelImage records a panel's approved image.'''

addition = '''// ListDirectorPlanVersions returns an episode's plan versions, newest first.
//
// A user looking at a plan needs its history to see what a revision changed, and reading
// each version by an id they do not hold is a query nobody can make. This is that read.
func (b *DramaBinding) ListDirectorPlanVersions(episodeID string) ([]DirectorPlanVersionDTO, error) {
	service := b.storyboardService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.ListDirectorPlanVersions(b.context(), episodeID)
	if err != nil {
		return nil, toDramaError(err)
	}
	versions := make([]DirectorPlanVersionDTO, 0, len(records))
	for _, record := range records {
		versions = append(versions, toDirectorPlanVersionDTO(record))
	}
	return versions, nil
}

// ListStoryboardVersions returns a storyboard's versions, newest first.
func (b *DramaBinding) ListStoryboardVersions(storyboardID string) ([]StoryboardVersionDTO, error) {
	service := b.storyboardService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.ListStoryboardVersions(b.context(), storyboardID)
	if err != nil {
		return nil, toDramaError(err)
	}
	versions := make([]StoryboardVersionDTO, 0, len(records))
	for _, record := range records {
		versions = append(versions, toStoryboardVersionDTO(record))
	}
	return versions, nil
}

// GetStoryboardVersion returns one storyboard version.
func (b *DramaBinding) GetStoryboardVersion(id string) (StoryboardVersionDTO, error) {
	service := b.storyboardService()
	if service == nil {
		return StoryboardVersionDTO{}, bindingUnavailable()
	}
	record, err := service.GetStoryboardVersion(b.context(), id)
	if err != nil {
		return StoryboardVersionDTO{}, toDramaError(err)
	}
	return toStoryboardVersionDTO(record), nil
}

// GetDirectorPlanVersion returns one plan version.
func (b *DramaBinding) GetDirectorPlanVersion(id string) (DirectorPlanVersionDTO, error) {
	service := b.storyboardService()
	if service == nil {
		return DirectorPlanVersionDTO{}, bindingUnavailable()
	}
	record, err := service.GetDirectorPlanVersion(b.context(), id)
	if err != nil {
		return DirectorPlanVersionDTO{}, toDramaError(err)
	}
	return toDirectorPlanVersionDTO(record), nil
}

// ApprovedStoryboardVersionID returns the version in force for a storyboard, or empty.
//
// Empty is the ordinary answer for a board nobody has approved, and it is reported rather
// than as an error: a caller asking "what is approved" about an unapproved board has asked
// a well-formed question.
func (b *DramaBinding) ApprovedStoryboardVersionID(storyboardID string) (string, error) {
	service := b.storyboardService()
	if service == nil {
		return "", bindingUnavailable()
	}
	versionID, err := service.ApprovedStoryboardVersionID(b.context(), storyboardID)
	if err != nil {
		return "", toDramaError(err)
	}
	return versionID, nil
}

// ApproveStoryboardVersion switches which board version is in force.
//
// It is the gate AC-BOARD-001's batch reads: generating images for a board that is not
// approved would spend the user's provider budget on rows a FIX may replace.
func (b *DramaBinding) ApproveStoryboardVersion(request ApproveStoryboardVersionRequest) (StoryboardVersionDTO, error) {
	service := b.storyboardService()
	if service == nil {
		return StoryboardVersionDTO{}, bindingUnavailable()
	}
	record, err := service.ApproveStoryboardVersion(b.context(), appstoryboard.ApproveStoryboardVersionRequest{
		VersionID: request.VersionID,
		TraceID:   request.TraceID,
	})
	if err != nil {
		return StoryboardVersionDTO{}, toDramaError(err)
	}
	return toStoryboardVersionDTO(record), nil
}

// ApproveDirectorPlanVersion switches which plan version is in force.
func (b *DramaBinding) ApproveDirectorPlanVersion(request ApproveDirectorPlanVersionRequest) (DirectorPlanVersionDTO, error) {
	service := b.storyboardService()
	if service == nil {
		return DirectorPlanVersionDTO{}, bindingUnavailable()
	}
	record, err := service.ApproveDirectorPlanVersion(b.context(), appstoryboard.ApproveDirectorPlanVersionRequest{
		VersionID: request.VersionID,
		TraceID:   request.TraceID,
	})
	if err != nil {
		return DirectorPlanVersionDTO{}, toDramaError(err)
	}
	return toDirectorPlanVersionDTO(record), nil
}

// UpdateStoryboardItem changes ONE row of a board, and nothing else.
//
// AC-BOARD-002's requirement is that a FIX to one shot leaves the others unchanged, and the
// strongest form of that is this command's shape: the other rows are never written at all.
// A version-rewrite path would have to copy them, and "unchanged" would then rest on a copy
// being faithful rather than on the row not being touched.
//
// Every field is POINTER so a nil means "leave this alone" — a caller changing a costume
// must not have to echo the descriptions it did not read.
func (b *DramaBinding) UpdateStoryboardItem(request UpdateStoryboardItemRequest) (StoryboardItemDTO, error) {
	service := b.storyboardService()
	if service == nil {
		return StoryboardItemDTO{}, bindingUnavailable()
	}
	record, err := service.UpdateStoryboardItem(b.context(), appstoryboard.UpdateStoryboardItemRequest{
		ItemID:                 request.ItemID,
		ExpectedRevision:       request.ExpectedRevision,
		ShotSize:               request.ShotSize,
		CameraAngle:            request.CameraAngle,
		CameraMovement:         request.CameraMovement,
		DurationSeconds:        request.DurationSeconds,
		VisualDescription:      request.VisualDescription,
		ActionDescription:      request.ActionDescription,
		DialogueAudioSummary:   request.DialogueAudioSummary,
		ContinuityNotes:        request.ContinuityNotes,
		FirstFrameDescription:  request.FirstFrameDescription,
		LastFrameDescription:   request.LastFrameDescription,
		VideoMotionDescription: request.VideoMotionDescription,
		Status:                 request.Status,
	})
	if err != nil {
		return StoryboardItemDTO{}, toDramaError(err)
	}
	return toStoryboardItemDTO(record), nil
}

// ApproveStoryboardVersionRequest approves a board version.
type ApproveStoryboardVersionRequest struct {
	VersionID string `json:"versionId"`
	// TraceID is the audit identifier the governance event carries.
	TraceID string `json:"traceId,omitempty"`
}

// ApproveDirectorPlanVersionRequest approves a plan version.
type ApproveDirectorPlanVersionRequest struct {
	VersionID string `json:"versionId"`
	TraceID   string `json:"traceId,omitempty"`
}

// UpdateStoryboardItemRequest changes one row.
//
// The pointers are the contract: a field that is absent is one the caller does not want
// changed, which is what lets a single-shot redo be a change to one shot.
type UpdateStoryboardItemRequest struct {
	ItemID string `json:"itemId"`
	// ExpectedRevision is the revision the caller read, so a stale edit is refused rather
	// than overwriting a change another window made.
	ExpectedRevision       int64   `json:"expectedRevision"`
	ShotSize               *string `json:"shotSize,omitempty"`
	CameraAngle            *string `json:"cameraAngle,omitempty"`
	CameraMovement         *string `json:"cameraMovement,omitempty"`
	DurationSeconds        *int    `json:"durationSeconds,omitempty"`
	VisualDescription      *string `json:"visualDescription,omitempty"`
	ActionDescription      *string `json:"actionDescription,omitempty"`
	DialogueAudioSummary   *string `json:"dialogueAudioSummary,omitempty"`
	ContinuityNotes        *string `json:"continuityNotes,omitempty"`
	FirstFrameDescription  *string `json:"firstFrameDescription,omitempty"`
	LastFrameDescription   *string `json:"lastFrameDescription,omitempty"`
	VideoMotionDescription *string `json:"videoMotionDescription,omitempty"`
	Status                 *string `json:"status,omitempty"`
}

// ListPanelVersions returns a storyboard row's panel versions.
func (b *DramaBinding) ListPanelVersions(storyboardItemID string) ([]StoryboardPanelVersionDTO, error) {
	service := b.storyboardService()
	if service == nil {
		return nil, bindingUnavailable()
	}
	records, err := service.ListPanels(b.context(), storyboardItemID)
	if err != nil {
		return nil, toDramaError(err)
	}
	panels := make([]StoryboardPanelVersionDTO, 0, len(records))
	for _, record := range records {
		panels = append(panels, toPanelVersionDTO(record))
	}
	return panels, nil
}

''' + anchor
assert anchor in s, "panel anchor"
s = s.replace(anchor, addition, 1)

io.open(p, 'w', encoding='utf-8').write(s)
print("storyboard bindings added")
