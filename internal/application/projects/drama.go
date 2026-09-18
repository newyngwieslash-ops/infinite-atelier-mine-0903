package projects

import (
	"context"
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/project"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// This file holds the drama project's configuration commands: the settings,
// rules, style guides and model policies of DOMAIN_MODEL §4 and PRD FR-020.
//
// They live in the projects service because they are the project's own
// configuration rather than a separate aggregate, and they fail closed when the
// SettingsRepository was not composed: a project that cannot store its settings
// must say so rather than appearing to accept them.

func (s *Service) settingsRepository() (SettingsRepository, error) {
	if s == nil || s.settings == nil {
		return nil, storageFailure()
	}
	return s.settings, nil
}

// CreateDramaProjectRequest is a caller's request to create a drama project.
type CreateDramaProjectRequest struct {
	Name        string
	Description string
	Language    string
	// Settings carries the PRD FR-020 fields the creation wizard collects.
	Settings DramaSettingsInput
}

// DramaSettingsInput is the subset of §4.3 the wizard asks for at creation.
//
// It is a separate type from project.Settings because creation takes the fields
// a user can answer on a form, while the stored settings carry identity and
// revision columns the caller must not supply.
type DramaSettingsInput struct {
	TargetPlatform             string
	AspectRatio                string
	Resolution                 string
	ExpectedEpisodeCount       int
	DefaultEpisodeDurationSecs int
	Audience                   string
	ContentRating              string
	AdaptationMode             project.AdaptationMode
}

// CreateDramaProject creates a drama project with its settings and its default
// canvas.
//
// The order matters and the failure mode of each step is different:
//
//  1. The project row is written first, because everything else references it.
//  2. The settings row is written next. A drama project without settings
//     cannot be planned, so a failure here is reported rather than swallowed —
//     the project exists and the caller can retry the settings.
//  3. The canvas is written last, matching CreateProject's treatment: a project
//     whose canvas list is empty is readable, so that failure is retryable.
//
// The three writes are deliberately not one transaction: the schema's foreign
// keys allow the project to exist without the other two, and a transaction would
// require passing one through the repository interfaces that the single-write
// commands do not need. The cost is that a failure leaves a partial project; the
// benefit is that each partial state is readable and repairable, and the error
// says which step failed.
func (s *Service) CreateDramaProject(ctx context.Context, request CreateDramaProjectRequest) (project.Project, error) {
	if !s.Available() {
		return project.Project{}, storageFailure()
	}
	if !project.IsValidProjectType(project.ProjectDrama) {
		return project.Project{}, project.InvalidError("Drama projects are not supported.")
	}
	if err := project.ValidateProjectName(request.Name); err != nil {
		return project.Project{}, err
	}
	settingsRepository, err := s.settingsRepository()
	if err != nil {
		return project.Project{}, err
	}

	workspaceID := project.DefaultLocalWorkspaceID
	if _, err := s.projects.GetWorkspace(ctx, workspaceID); err != nil {
		return project.Project{}, err
	}

	now := s.now()
	id, err := s.ids.New()
	if err != nil {
		return project.Project{}, storageFailure()
	}
	record := project.Project{
		ID:          id,
		WorkspaceID: workspaceID,
		Type:        project.ProjectDrama,
		Name:        strings.TrimSpace(request.Name),
		Description: request.Description,
		Language:    defaultLanguage(request.Language),
		Status:      project.ProjectActive,
		CreatedAt:   now,
		UpdatedAt:   now,
		Revision:    1,
	}
	if err := s.projects.CreateProject(ctx, record); err != nil {
		return project.Project{}, err
	}

	settings := project.DefaultSettings(record.ID, record.Language)
	settings.TargetPlatform = strings.TrimSpace(request.Settings.TargetPlatform)
	settings.AspectRatio = strings.TrimSpace(request.Settings.AspectRatio)
	settings.Resolution = strings.TrimSpace(request.Settings.Resolution)
	settings.ExpectedEpisodeCount = request.Settings.ExpectedEpisodeCount
	settings.DefaultEpisodeDurationSecs = request.Settings.DefaultEpisodeDurationSecs
	settings.Audience = strings.TrimSpace(request.Settings.Audience)
	settings.ContentRating = strings.TrimSpace(request.Settings.ContentRating)
	if request.Settings.AdaptationMode != "" {
		settings.AdaptationMode = request.Settings.AdaptationMode
	}
	settings.CreatedAt = now
	settings.UpdatedAt = now
	if err := settings.Validate(); err != nil {
		// The project row is already written. Report the validation failure so
		// the caller fixes the input rather than retrying a command that will
		// keep refusing it.
		return record, err
	}
	if err := settingsRepository.CreateSettings(ctx, settings); err != nil {
		return record, err
	}

	if s.canvas != nil {
		documentID, idErr := s.ids.New()
		if idErr != nil {
			return record, storageFailure()
		}
		document := project.CanvasDocument{
			ID:        documentID,
			ProjectID: record.ID,
			Name:      record.Name,
			// A drama project opens on its drama canvas rather than a free one,
			// because the studio's storyboard-canvas section projects drama
			// entities and the free canvas is a separate surface (CANVAS_KINDS).
			Kind:      project.CanvasDrama,
			Viewport:  project.Viewport{K: 1},
			CreatedAt: now,
			UpdatedAt: now,
			Revision:  1,
		}
		if err := s.canvas.CreateDocument(ctx, document); err != nil {
			return record, err
		}
	}
	return record, nil
}

// GetSettings returns a project's drama settings.
func (s *Service) GetSettings(ctx context.Context, projectID string) (project.Settings, error) {
	if !s.Available() {
		return project.Settings{}, storageFailure()
	}
	settingsRepository, err := s.settingsRepository()
	if err != nil {
		return project.Settings{}, err
	}
	return settingsRepository.GetSettings(ctx, projectID)
}

// UpdateSettingsRequest is a caller's request to change a project's settings.
type UpdateSettingsRequest struct {
	ProjectID                  string
	TargetPlatform             string
	AspectRatio                string
	Resolution                 string
	ExpectedEpisodeCount       int
	DefaultEpisodeDurationSecs int
	Audience                   string
	ContentRating              string
	AdaptationMode             project.AdaptationMode
	// Revision is the settings revision the caller read.
	Revision int64
}

// UpdateSettings persists a settings change.
//
// Every field is replaced rather than merged, and the caller states the
// revision it read, so two windows editing the same project cannot silently
// overwrite one another (DOMAIN_MODEL §2.3).
func (s *Service) UpdateSettings(ctx context.Context, request UpdateSettingsRequest) (project.Settings, error) {
	if !s.Available() {
		return project.Settings{}, storageFailure()
	}
	settingsRepository, err := s.settingsRepository()
	if err != nil {
		return project.Settings{}, err
	}
	record, err := settingsRepository.GetSettings(ctx, request.ProjectID)
	if err != nil {
		return project.Settings{}, err
	}
	if request.Revision != record.Revision {
		return project.Settings{}, project.RevisionMismatchError()
	}
	record.TargetPlatform = strings.TrimSpace(request.TargetPlatform)
	record.AspectRatio = strings.TrimSpace(request.AspectRatio)
	record.Resolution = strings.TrimSpace(request.Resolution)
	record.ExpectedEpisodeCount = request.ExpectedEpisodeCount
	record.DefaultEpisodeDurationSecs = request.DefaultEpisodeDurationSecs
	record.Audience = strings.TrimSpace(request.Audience)
	record.ContentRating = strings.TrimSpace(request.ContentRating)
	if request.AdaptationMode != "" {
		record.AdaptationMode = request.AdaptationMode
	}
	record.SettingsVersion++
	record.UpdatedAt = s.now()
	if err := record.Validate(); err != nil {
		return project.Settings{}, err
	}
	if err := settingsRepository.UpdateSettings(ctx, record, request.Revision); err != nil {
		return project.Settings{}, err
	}
	record.Revision = request.Revision + 1
	return record, nil
}

// CreateRuleRequest is a caller's request to add a rule.
type CreateRuleRequest struct {
	ProjectID  string
	Category   project.RuleCategory
	Name       string
	Content    string
	Strength   project.RuleStrength
	SourceType project.RuleSourceType
	SourceID   string
	// Writer is who is asking. It is part of the request rather than a UI
	// convention because §4.4 makes the writer part of what a rule means.
	Writer project.WriterKind
}

// CreateRule stores a project rule.
//
// §4.4 allows an agent to create a suggestion but not to make one binding, so a
// non-user writer is refused when it asks for required or immutable. The rule is
// still stored at the strength asked for if the writer is allowed to ask.
func (s *Service) CreateRule(ctx context.Context, request CreateRuleRequest) (project.Rule, error) {
	if !s.Available() {
		return project.Rule{}, storageFailure()
	}
	settingsRepository, err := s.settingsRepository()
	if err != nil {
		return project.Rule{}, err
	}
	if !project.IsValidWriterKind(request.Writer) {
		return project.Rule{}, project.InvalidError("The writer is not recognised.")
	}
	strength := request.Strength
	if strength == "" {
		strength = project.RuleAdvisory
	}
	sourceType := request.SourceType
	if sourceType == "" {
		if request.Writer == project.WriterUser {
			sourceType = project.RuleSourceUser
		} else {
			sourceType = project.RuleSourceAgentSuggested
		}
	}
	// An agent may create a suggestion, not a binding rule. The check compares
	// the requested strength against advisory rather than against a stored
	// value, because this is a creation.
	if err := project.CanEscalate(request.Writer, project.RuleAdvisory, strength); err != nil {
		return project.Rule{}, err
	}
	id, err := s.ids.New()
	if err != nil {
		return project.Rule{}, storageFailure()
	}
	now := s.now()
	record := project.Rule{
		ID:         id,
		ProjectID:  request.ProjectID,
		Category:   request.Category,
		Name:       strings.TrimSpace(request.Name),
		Content:    request.Content,
		Strength:   strength,
		Status:     project.RuleActive,
		SourceType: sourceType,
		SourceID:   request.SourceID,
		CreatedAt:  now,
		UpdatedAt:  now,
		Revision:   1,
	}
	if err := record.Validate(); err != nil {
		return project.Rule{}, err
	}
	if err := settingsRepository.CreateRule(ctx, record); err != nil {
		return project.Rule{}, err
	}
	return record, nil
}

// ListRules returns a project's rules.
func (s *Service) ListRules(ctx context.Context, projectID string, includeDeleted bool) ([]project.Rule, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	settingsRepository, err := s.settingsRepository()
	if err != nil {
		return nil, err
	}
	return settingsRepository.ListRules(ctx, projectID, includeDeleted)
}

// UpdateRuleRequest is a caller's request to change a rule.
type UpdateRuleRequest struct {
	RuleID   string
	Name     string
	Content  string
	Strength project.RuleStrength
	Status   project.RuleStatus
	Writer   project.WriterKind
	// LockedByUser sets the lock. Only a user may set it, which the domain
	// enforces through CanModify.
	LockedByUser bool
	// Revision is the rule revision the caller read.
	Revision int64
}

// UpdateRule persists a rule change.
//
// Two §4.4 protections are applied before the write: a locked or immutable rule
// accepts only a user command (Rule.CanModify), and only a user may change how
// binding a rule is (CanEscalate). Both are checked against the stored rule
// rather than the request, so a caller cannot unlock a rule by omitting the
// flag.
func (s *Service) UpdateRule(ctx context.Context, request UpdateRuleRequest) (project.Rule, error) {
	if !s.Available() {
		return project.Rule{}, storageFailure()
	}
	settingsRepository, err := s.settingsRepository()
	if err != nil {
		return project.Rule{}, err
	}
	record, err := settingsRepository.GetRule(ctx, request.RuleID)
	if err != nil {
		return project.Rule{}, err
	}
	if err := record.CanModify(request.Writer); err != nil {
		return project.Rule{}, err
	}
	strength := request.Strength
	if strength == "" {
		strength = record.Strength
	}
	if err := project.CanEscalate(request.Writer, record.Strength, strength); err != nil {
		return project.Rule{}, err
	}
	// A non-user writer may not change the lock, in either direction. Unlocking
	// would undo the user's protection; locking would let an agent block the
	// user's own future edits.
	if request.Writer != project.WriterUser && request.LockedByUser != record.LockedByUser {
		return project.Rule{}, project.ConflictError("Only a user command can change whether a rule is locked.")
	}
	status := request.Status
	if status == "" {
		status = record.Status
	}
	record.Name = strings.TrimSpace(request.Name)
	record.Content = request.Content
	record.Strength = strength
	record.Status = status
	record.LockedByUser = request.LockedByUser
	record.UpdatedAt = s.now()
	if err := record.Validate(); err != nil {
		return project.Rule{}, err
	}
	if err := settingsRepository.UpdateRule(ctx, record, request.Revision); err != nil {
		return project.Rule{}, err
	}
	record.Revision = request.Revision + 1
	return record, nil
}

// CreateStyleGuideRequest is a caller's request to add a style guide version.
type CreateStyleGuideRequest struct {
	ProjectID           string
	BasedOnVersionID    string
	VisualStyle         string
	Palette             string
	Lighting            string
	Composition         string
	CameraLanguage      string
	NegativeConstraints string
	SoundDirection      string
	ChangeReason        string
	CreatedByType       string
}

// CreateStyleGuide stores a new style guide version.
//
// The version number comes from the stored maximum, not a count: the schema has
// a unique constraint on (project_id, version_number), and a count would reuse
// a number after a deletion (the same reasoning as the asset versions).
func (s *Service) CreateStyleGuide(ctx context.Context, request CreateStyleGuideRequest) (project.StyleGuide, error) {
	if !s.Available() {
		return project.StyleGuide{}, storageFailure()
	}
	settingsRepository, err := s.settingsRepository()
	if err != nil {
		return project.StyleGuide{}, err
	}
	highest, err := settingsRepository.MaxStyleGuideVersion(ctx, request.ProjectID)
	if err != nil {
		return project.StyleGuide{}, err
	}
	id, err := s.ids.New()
	if err != nil {
		return project.StyleGuide{}, storageFailure()
	}
	createdBy := versioning.CreatedByType(request.CreatedByType)
	if !versioning.IsValidCreatedByType(createdBy) {
		createdBy = versioning.CreatedByUser
	}
	guide := project.StyleGuide{
		ID:                  id,
		ProjectID:           request.ProjectID,
		VersionNumber:       highest + 1,
		Status:              versioning.StatusDraft,
		BasedOnVersionID:    request.BasedOnVersionID,
		VisualStyle:         request.VisualStyle,
		Palette:             request.Palette,
		Lighting:            request.Lighting,
		Composition:         request.Composition,
		CameraLanguage:      request.CameraLanguage,
		NegativeConstraints: request.NegativeConstraints,
		SoundDirection:      request.SoundDirection,
		CreatedByType:       createdBy,
		ChangeReason:        request.ChangeReason,
		CreatedAt:           s.now(),
	}
	if err := guide.Validate(); err != nil {
		return project.StyleGuide{}, err
	}
	if err := settingsRepository.CreateStyleGuide(ctx, guide); err != nil {
		return project.StyleGuide{}, err
	}
	return guide, nil
}

// ListStyleGuides returns a project's style guide versions.
func (s *Service) ListStyleGuides(ctx context.Context, projectID string) ([]project.StyleGuide, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	settingsRepository, err := s.settingsRepository()
	if err != nil {
		return nil, err
	}
	return settingsRepository.ListStyleGuides(ctx, projectID)
}

// SetProviderPolicyRequest is a caller's request to set a model policy.
type SetProviderPolicyRequest struct {
	ProjectID  string
	Layer      project.ProviderPolicyLayer
	PolicyJSON string
}

// SetProviderPolicy stores a project's model policy for one layer.
func (s *Service) SetProviderPolicy(ctx context.Context, request SetProviderPolicyRequest) (project.ProviderPolicy, error) {
	if !s.Available() {
		return project.ProviderPolicy{}, storageFailure()
	}
	settingsRepository, err := s.settingsRepository()
	if err != nil {
		return project.ProviderPolicy{}, err
	}
	layer := request.Layer
	if layer == "" {
		layer = project.PolicyDefault
	}
	id, err := s.ids.New()
	if err != nil {
		return project.ProviderPolicy{}, storageFailure()
	}
	now := s.now()
	policy := project.ProviderPolicy{
		ID:         id,
		ProjectID:  request.ProjectID,
		Layer:      layer,
		PolicyJSON: request.PolicyJSON,
		CreatedAt:  now,
		UpdatedAt:  now,
		Revision:   1,
	}
	if err := policy.Validate(); err != nil {
		return project.ProviderPolicy{}, err
	}
	if err := settingsRepository.UpsertProviderPolicy(ctx, policy); err != nil {
		return project.ProviderPolicy{}, err
	}
	return policy, nil
}

// ListProviderPolicies returns a project's model policies.
func (s *Service) ListProviderPolicies(ctx context.Context, projectID string) ([]project.ProviderPolicy, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	settingsRepository, err := s.settingsRepository()
	if err != nil {
		return nil, err
	}
	return settingsRepository.ListProviderPolicies(ctx, projectID)
}
