import io

# The storyboard service command.
p = 'internal/application/storyboard/service.go'
s = io.open(p, encoding='utf-8').read()
anchor = '''// ProjectOfEpisode returns the project an episode belongs to.'''
addition = '''// SetShotOverridesRequest replaces one plan version's per-shot overrides document.
type SetShotOverridesRequest struct {
	VersionID string
	// OverridesJSON is the whole document. It REPLACES what is there rather than merging,
	// because a merge would need to know the document's shape — and section 9.1 leaves that
	// shape to the caller.
	OverridesJSON string
	// ExpectedRevision is the revision the caller read, so a stale write is refused.
	ExpectedRevision int64
}

// SetShotOverrides writes the per-shot overrides of one plan version.
//
// IT IS WHAT FR-060's "保存后可在 Shot 中看到摄像机参数" HAS TO REACH: a previs camera belongs
// to a shot, a plan's `shot_overrides_json` is where section 9.1 puts per-shot overrides,
// and this is the command that writes them. It touches THAT COLUMN and nothing else, so a
// camera write-back cannot carry the plan's prose along with it.
//
// It does NOT approve anything and does not move a stage: a camera is an edit to a plan a
// person already accepted, and writing it is not a second acceptance.
func (s *Service) SetShotOverrides(ctx context.Context, request SetShotOverridesRequest) (storyboard.DirectorPlanVersion, error) {
	if !s.Available() {
		return storyboard.DirectorPlanVersion{}, storageFailure()
	}
	versionID := strings.TrimSpace(request.VersionID)
	if versionID == "" {
		return storyboard.DirectorPlanVersion{}, storyboard.InvalidError("A shot override must name the plan version it changes.")
	}
	if request.ExpectedRevision < 1 {
		return storyboard.DirectorPlanVersion{}, storyboard.InvalidError("A shot override must name the revision it was read at.")
	}
	// The document must be JSON, checked HERE rather than at the column: the column is TEXT,
	// so a malformed document would store happily and fail when something tried to read it —
	// a failure far from its cause.
	if trimmed := strings.TrimSpace(request.OverridesJSON); trimmed != "" {
		if !json.Valid([]byte(trimmed)) {
			return storyboard.DirectorPlanVersion{}, storyboard.InvalidError("The shot overrides must be a JSON document.")
		}
	}
	if err := s.directorPlans.UpdateDirectorPlanOverrides(ctx, versionID, request.OverridesJSON, request.ExpectedRevision, s.now()); err != nil {
		return storyboard.DirectorPlanVersion{}, err
	}
	// The row is read back so the caller holds what the STORE holds rather than what it
	// sent, which is the rule every write in this package follows.
	return s.directorPlans.GetDirectorPlanVersion(ctx, versionID)
}

// ProjectOfEpisode returns the project an episode belongs to.'''
assert anchor in s, "service anchor"
s = s.replace(anchor, addition, 1)
if '"encoding/json"' not in s:
    s = s.replace('import (\n\t"context"', 'import (\n\t"context"\n\t"encoding/json"', 1)
io.open(p, 'w', encoding='utf-8').write(s)
print("service command added")

# The binding.
p2 = 'internal/desktop/drama_binding.go'
s2 = io.open(p2, encoding='utf-8').read()
anchor2 = '''// ApproveStoryboardVersion switches which board version is in force.'''
addition2 = '''// SetShotOverridesRequest writes a plan's per-shot overrides document.
type SetShotOverridesRequest struct {
	VersionID string `json:"versionId"`
	// OverridesJSON replaces the stored document rather than merging into it.
	OverridesJSON string `json:"overridesJson"`
	// ExpectedRevision is the revision the caller read.
	ExpectedRevision int64 `json:"expectedRevision"`
}

// SetShotOverrides writes the per-shot overrides of one plan version.
//
// It is how a previs camera reaches the shot it belongs to: the studio reports the camera,
// this binding composes the document, and this command stores it. FR-060's "保存后可在 Shot 中
// 看到摄像机参数" is the round trip this closes.
func (b *DramaBinding) SetShotOverrides(request SetShotOverridesRequest) (DirectorPlanVersionDTO, error) {
	service := b.storyboardService()
	if service == nil {
		return DirectorPlanVersionDTO{}, bindingUnavailable()
	}
	record, err := service.SetShotOverrides(b.context(), appstoryboard.SetShotOverridesRequest{
		VersionID:        request.VersionID,
		OverridesJSON:    request.OverridesJSON,
		ExpectedRevision: request.ExpectedRevision,
	})
	if err != nil {
		return DirectorPlanVersionDTO{}, toDramaError(err)
	}
	return toDirectorPlanVersionDTO(record), nil
}

''' + anchor2
assert anchor2 in s2, "binding anchor"
s2 = s2.replace(anchor2, addition2, 1)
io.open(p2, 'w', encoding='utf-8').write(s2)
print("binding added")
