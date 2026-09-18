package database

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/projects"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/project"
)

// openWP05SettingsService opens a migrated database and returns a project
// service composed with the drama configuration repository, plus the pieces the
// tests assert against.
func openWP05SettingsService(t *testing.T) (*projects.Service, *DramaSettingsRepository, *ProjectRepository) {
	t.Helper()
	ctx := context.Background()
	handle, err := open(ctx, filepath.Join(t.TempDir(), "app.db"), filepath.Join(t.TempDir(), "snapshots"), wp05Migrations(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close(ctx) })
	db := handle.SQL()
	settings := NewDramaSettingsRepository(db)
	projectsRepo := NewProjectRepository(db)
	service := projects.NewService(projects.Options{
		Projects: projectsRepo,
		Canvas:   NewCanvasRepository(db),
		Settings: settings,
		Clock:    fixedClockProvider{},
		IDs:      newTestIDGenerator(),
	})
	return service, settings, projectsRepo
}

// TestCreateDramaProjectWritesSettingsAndDramaCanvas covers the wizard's path:
// a drama project gets its settings row and a drama canvas, which is what makes
// the studio's storyboard-canvas section open on the right document.
func TestCreateDramaProjectWritesSettingsAndDramaCanvas(t *testing.T) {
	service, _, _ := openWP05SettingsService(t)
	ctx := context.Background()

	record, err := service.CreateDramaProject(ctx, projects.CreateDramaProjectRequest{
		Name:     "The Long Night",
		Language: "zh-CN",
		Settings: projects.DramaSettingsInput{
			TargetPlatform:             "short_video",
			AspectRatio:                "9:16",
			Resolution:                 "1080x1920",
			ExpectedEpisodeCount:       24,
			DefaultEpisodeDurationSecs: 120,
			Audience:                   "young adult",
			ContentRating:              "PG-13",
			AdaptationMode:             project.AdaptationFaithful,
		},
	})
	if err != nil {
		t.Fatalf("CreateDramaProject: %v", err)
	}
	if record.Type != project.ProjectDrama {
		t.Fatalf("project type = %q, want drama", record.Type)
	}

	settings, err := service.GetSettings(ctx, record.ID)
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	// Every field the wizard collected must have survived the write.
	if settings.TargetPlatform != "short_video" || settings.AspectRatio != "9:16" || settings.Resolution != "1080x1920" {
		t.Fatalf("settings = %+v", settings)
	}
	if settings.ExpectedEpisodeCount != 24 || settings.DefaultEpisodeDurationSecs != 120 {
		t.Fatalf("settings counts = %+v", settings)
	}
	if settings.AdaptationMode != project.AdaptationFaithful {
		t.Fatalf("adaptation mode = %q, want faithful", settings.AdaptationMode)
	}
	if settings.Revision != 1 || settings.SettingsVersion != 1 {
		t.Fatalf("settings versions = %+v", settings)
	}

	// The canvas is a drama canvas, not the free one CreateProject makes.
	document, err := service.CanvasFor(ctx, record.ID)
	if err != nil {
		t.Fatalf("CanvasFor: %v", err)
	}
	if document.Kind != project.CanvasDrama {
		t.Fatalf("canvas kind = %q, want drama", document.Kind)
	}
}

// TestCreateDramaProjectRejectsAnUnknownAdaptationMode covers the vocabulary
// gate: a value the schema would reject is refused before the write.
func TestCreateDramaProjectRejectsAnUnknownAdaptationMode(t *testing.T) {
	service, _, _ := openWP05SettingsService(t)
	ctx := context.Background()
	_, err := service.CreateDramaProject(ctx, projects.CreateDramaProjectRequest{
		Name:     "Bad Mode",
		Settings: projects.DramaSettingsInput{AdaptationMode: "loose"},
	})
	if err == nil {
		t.Fatal("an undocumented adaptation mode was accepted")
	}
	if _, ok := project.AsError(err); !ok {
		t.Fatalf("error = %v, want a domain error", err)
	}
	// The project row is written before the settings are validated, so it
	// exists; the failure is reported rather than the row being hidden. This
	// asserts the documented partial state rather than pretending otherwise.
	listed, listErr := service.ListProjects(ctx, projects.ListFilter{})
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(listed) != 1 {
		t.Fatalf("%d projects after a rejected settings write, want 1 (the documented partial state)", len(listed))
	}
}

// TestUpdateSettingsUsesRevisionCAS covers §2.3 on the settings value object.
func TestUpdateSettingsUsesRevisionCAS(t *testing.T) {
	service, _, _ := openWP05SettingsService(t)
	ctx := context.Background()
	record, err := service.CreateDramaProject(ctx, projects.CreateDramaProjectRequest{Name: "CAS"})
	if err != nil {
		t.Fatal(err)
	}
	settings, err := service.GetSettings(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := service.UpdateSettings(ctx, projects.UpdateSettingsRequest{
		ProjectID:            record.ID,
		TargetPlatform:       "streaming",
		AdaptationMode:       project.AdaptationBalanced,
		ExpectedEpisodeCount: 12,
		Revision:             settings.Revision,
	})
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if updated.Revision != settings.Revision+1 {
		t.Fatalf("revision = %d, want %d", updated.Revision, settings.Revision+1)
	}
	if updated.TargetPlatform != "streaming" || updated.ExpectedEpisodeCount != 12 {
		t.Fatalf("update did not persist: %+v", updated)
	}
	// The settings version increments separately from the row revision: §4.3
	// makes settings_version the version of the configuration itself.
	if updated.SettingsVersion != settings.SettingsVersion+1 {
		t.Fatalf("settings version = %d, want %d", updated.SettingsVersion, settings.SettingsVersion+1)
	}

	// A stale revision is refused rather than overwriting the newer state.
	if _, err := service.UpdateSettings(ctx, projects.UpdateSettingsRequest{
		ProjectID:      record.ID,
		TargetPlatform: "should not land",
		Revision:       settings.Revision,
	}); err == nil {
		t.Fatal("a stale settings revision was accepted")
	}
	after, err := service.GetSettings(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.TargetPlatform != "streaming" {
		t.Fatalf("a refused update changed the row: %+v", after)
	}
}

// TestLockedRuleAcceptsOnlyUserCommandsAtTheService is the locked-rule
// acceptance item enforced through the command path rather than only in the
// domain: an agent cannot change a rule a user locked.
func TestLockedRuleAcceptsOnlyUserCommandsAtTheService(t *testing.T) {
	service, _, _ := openWP05SettingsService(t)
	ctx := context.Background()
	record, err := service.CreateDramaProject(ctx, projects.CreateDramaProjectRequest{Name: "Rules"})
	if err != nil {
		t.Fatal(err)
	}

	rule, err := service.CreateRule(ctx, projects.CreateRuleRequest{
		ProjectID: record.ID,
		Category:  project.RuleCharacter,
		Name:      "Mira always wears green",
		Content:   "The costume must stay consistent",
		Strength:  project.RuleRequired,
		Writer:    project.WriterUser,
	})
	if err != nil {
		t.Fatalf("CreateRule: %v", err)
	}
	if rule.SourceType != project.RuleSourceUser {
		t.Fatalf("source = %q, want user", rule.SourceType)
	}

	// The user locks it.
	locked, err := service.UpdateRule(ctx, projects.UpdateRuleRequest{
		RuleID:       rule.ID,
		Name:         rule.Name,
		Content:      rule.Content,
		Strength:     rule.Strength,
		Status:       rule.Status,
		Writer:       project.WriterUser,
		LockedByUser: true,
		Revision:     rule.Revision,
	})
	if err != nil {
		t.Fatalf("locking the rule failed: %v", err)
	}
	if !locked.LockedByUser {
		t.Fatal("the lock was not stored")
	}

	// An agent is refused, and the row is unchanged.
	if _, err := service.UpdateRule(ctx, projects.UpdateRuleRequest{
		RuleID:   locked.ID,
		Name:     "Mira wears red now",
		Content:  "changed by an agent",
		Strength: locked.Strength,
		Status:   locked.Status,
		Writer:   project.WriterAgent,
		Revision: locked.Revision,
	}); err == nil {
		t.Fatal("an agent changed a rule the user locked")
	}
	rules, err := service.ListRules(ctx, record.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].Name != "Mira always wears green" {
		t.Fatalf("the refused update changed the rule: %+v", rules)
	}

	// A non-user writer may not unlock it either, in either direction.
	if _, err := service.UpdateRule(ctx, projects.UpdateRuleRequest{
		RuleID:       locked.ID,
		Name:         locked.Name,
		Content:      locked.Content,
		Strength:     locked.Strength,
		Status:       locked.Status,
		Writer:       project.WriterAgent,
		LockedByUser: false,
		Revision:     locked.Revision,
	}); err == nil {
		t.Fatal("an agent unlocked a rule")
	}

	// The user may still change it.
	if _, err := service.UpdateRule(ctx, projects.UpdateRuleRequest{
		RuleID:       locked.ID,
		Name:         "Mira always wears green",
		Content:      "the costume must stay consistent, including the scarf",
		Strength:     locked.Strength,
		Status:       locked.Status,
		Writer:       project.WriterUser,
		LockedByUser: true,
		Revision:     locked.Revision,
	}); err != nil {
		t.Fatalf("the user was refused their own rule: %v", err)
	}
}

// TestAgentCannotCreateABindingRule covers §4.4's second protection at the
// creation path: an agent may suggest, not bind.
func TestAgentCannotCreateABindingRule(t *testing.T) {
	service, _, _ := openWP05SettingsService(t)
	ctx := context.Background()
	record, err := service.CreateDramaProject(ctx, projects.CreateDramaProjectRequest{Name: "Suggestions"})
	if err != nil {
		t.Fatal(err)
	}
	// An agent may create an advisory suggestion.
	suggestion, err := service.CreateRule(ctx, projects.CreateRuleRequest{
		ProjectID: record.ID,
		Category:  project.RuleVisual,
		Name:      "Warmer light in act two",
		Strength:  project.RuleAdvisory,
		Writer:    project.WriterAgent,
	})
	if err != nil {
		t.Fatalf("an agent was refused an advisory suggestion: %v", err)
	}
	if suggestion.SourceType != project.RuleSourceAgentSuggested {
		t.Fatalf("source = %q, want agent_suggested", suggestion.SourceType)
	}
	// It may not create a binding one.
	for _, strength := range []project.RuleStrength{project.RuleRequired, project.RuleImmutable} {
		if _, err := service.CreateRule(ctx, projects.CreateRuleRequest{
			ProjectID: record.ID,
			Category:  project.RuleSafety,
			Name:      "An agent-made binding rule",
			Strength:  strength,
			Writer:    project.WriterAgent,
		}); err == nil {
			t.Fatalf("an agent created a %s rule", strength)
		}
	}
	// Nor may it escalate its own suggestion afterwards.
	if _, err := service.UpdateRule(ctx, projects.UpdateRuleRequest{
		RuleID:   suggestion.ID,
		Name:     suggestion.Name,
		Strength: project.RuleRequired,
		Status:   suggestion.Status,
		Writer:   project.WriterAgent,
		Revision: suggestion.Revision,
	}); err == nil {
		t.Fatal("an agent escalated its own suggestion to required")
	}
}

// TestStyleGuideVersionsIncrementFromTheStoredMaximum covers the numbering rule
// the schema's unique constraint depends on.
func TestStyleGuideVersionsIncrementFromTheStoredMaximum(t *testing.T) {
	service, _, _ := openWP05SettingsService(t)
	ctx := context.Background()
	record, err := service.CreateDramaProject(ctx, projects.CreateDramaProjectRequest{Name: "Style"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.CreateStyleGuide(ctx, projects.CreateStyleGuideRequest{
		ProjectID: record.ID, VisualStyle: "ink wash",
	})
	if err != nil {
		t.Fatalf("CreateStyleGuide: %v", err)
	}
	if first.VersionNumber != 1 {
		t.Fatalf("first version = %d, want 1", first.VersionNumber)
	}
	if first.Status != "draft" {
		t.Fatalf("status = %q, want draft", first.Status)
	}
	second, err := service.CreateStyleGuide(ctx, projects.CreateStyleGuideRequest{
		ProjectID: record.ID, VisualStyle: "ink wash with gold leaf", BasedOnVersionID: first.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.VersionNumber != 2 {
		t.Fatalf("second version = %d, want 2", second.VersionNumber)
	}
	if second.BasedOnVersionID != first.ID {
		t.Fatal("the second version does not record what it was based on")
	}
	guides, err := service.ListStyleGuides(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Newest first, so the studio's list reads in the order a user expects.
	if len(guides) != 2 || guides[0].VersionNumber != 2 {
		t.Fatalf("guides = %+v", guides)
	}
}

// TestProviderPolicyUpsertsPerLayer covers the one-policy-per-layer rule the
// schema's unique constraint enforces.
func TestProviderPolicyUpsertsPerLayer(t *testing.T) {
	service, _, _ := openWP05SettingsService(t)
	ctx := context.Background()
	record, err := service.CreateDramaProject(ctx, projects.CreateDramaProjectRequest{Name: "Policies"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.SetProviderPolicy(ctx, projects.SetProviderPolicyRequest{
		ProjectID: record.ID, PolicyJSON: `{"primaryModelId":"m-1"}`,
	})
	if err != nil {
		t.Fatalf("SetProviderPolicy: %v", err)
	}
	if first.Layer != project.PolicyDefault {
		t.Fatalf("layer = %q, want default", first.Layer)
	}
	// A second save for the same layer replaces rather than accumulating.
	if _, err := service.SetProviderPolicy(ctx, projects.SetProviderPolicyRequest{
		ProjectID: record.ID, PolicyJSON: `{"primaryModelId":"m-2"}`,
	}); err != nil {
		t.Fatal(err)
	}
	// A different layer is its own row.
	if _, err := service.SetProviderPolicy(ctx, projects.SetProviderPolicyRequest{
		ProjectID: record.ID, Layer: project.PolicySupervision, PolicyJSON: `{"primaryModelId":"m-3"}`,
	}); err != nil {
		t.Fatal(err)
	}
	policies, err := service.ListProviderPolicies(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(policies) != 2 {
		t.Fatalf("%d policies, want 2 (default and supervision)", len(policies))
	}
	for _, policy := range policies {
		if policy.Layer == project.PolicyDefault && policy.PolicyJSON != `{"primaryModelId":"m-2"}` {
			t.Fatalf("the default policy was not replaced: %+v", policy)
		}
	}
	// A non-object policy is refused before the write.
	if _, err := service.SetProviderPolicy(ctx, projects.SetProviderPolicyRequest{
		ProjectID: record.ID, Layer: project.PolicyExecution, PolicyJSON: `["not","an","object"]`,
	}); err == nil {
		t.Fatal("a non-object policy was accepted")
	}
}

// TestSettingsCommandsFailClosedWithoutTheRepository proves the optional port
// really is optional and really does fail closed.
func TestSettingsCommandsFailClosedWithoutTheRepository(t *testing.T) {
	ctx := context.Background()
	handle, err := open(ctx, filepath.Join(t.TempDir(), "app.db"), filepath.Join(t.TempDir(), "snapshots"), wp05Migrations(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close(ctx) })
	db := handle.SQL()
	// A service composed without the settings repository, which is how WP-04
	// composed it.
	service := projects.NewService(projects.Options{
		Projects: NewProjectRepository(db),
		Canvas:   NewCanvasRepository(db),
		Clock:    fixedClockProvider{},
		IDs:      newTestIDGenerator(),
	})
	if _, err := service.CreateDramaProject(ctx, projects.CreateDramaProjectRequest{Name: "No Settings"}); err == nil {
		t.Fatal("a drama project was created without a settings repository")
	}
	if _, err := service.GetSettings(ctx, "any"); err == nil {
		t.Fatal("GetSettings succeeded without a settings repository")
	}
	if _, err := service.CreateRule(ctx, projects.CreateRuleRequest{ProjectID: "any", Category: project.RuleStory, Name: "r", Writer: project.WriterUser}); err == nil {
		t.Fatal("CreateRule succeeded without a settings repository")
	}
	if _, err := service.ListRules(ctx, "any", false); err == nil {
		t.Fatal("ListRules succeeded without a settings repository")
	}
	// The project commands still work, which is the point of the port being
	// optional rather than required.
	if _, err := service.CreateProject(ctx, projects.CreateProjectRequest{Name: "Free canvas", Type: project.ProjectFreeCanvas}); err != nil {
		t.Fatalf("the free canvas path must keep working: %v", err)
	}
}

// TestAgentCannotEditALockedRuleWithoutTouchingTheLock isolates the locked-rule
// guard from the lock-change guard.
//
// UpdateRule has three protections that can all refuse an agent: Rule.CanModify,
// CanEscalate, and the rule that only a user may change the lock. The broader
// test exercises the lock change, so it would still pass if CanModify were
// removed. Here the agent keeps the lock flag exactly as it is and only edits
// the content, which leaves CanModify as the sole thing standing in the way —
// so removing it fails this test and nothing else.
func TestAgentCannotEditALockedRuleWithoutTouchingTheLock(t *testing.T) {
	service, _, _ := openWP05SettingsService(t)
	ctx := context.Background()
	record, err := service.CreateDramaProject(ctx, projects.CreateDramaProjectRequest{Name: "Isolation"})
	if err != nil {
		t.Fatal(err)
	}
	rule, err := service.CreateRule(ctx, projects.CreateRuleRequest{
		ProjectID: record.ID, Category: project.RuleCharacter, Name: "original",
		Strength: project.RuleRequired, Writer: project.WriterUser,
	})
	if err != nil {
		t.Fatal(err)
	}
	locked, err := service.UpdateRule(ctx, projects.UpdateRuleRequest{
		RuleID: rule.ID, Name: rule.Name, Strength: rule.Strength, Status: rule.Status,
		Writer: project.WriterUser, LockedByUser: true, Revision: rule.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	// The lock flag is unchanged, so only CanModify applies.
	if _, err := service.UpdateRule(ctx, projects.UpdateRuleRequest{
		RuleID: locked.ID, Name: "renamed by an agent", Strength: locked.Strength,
		Status: locked.Status, Writer: project.WriterAgent,
		LockedByUser: true, Revision: locked.Revision,
	}); err == nil {
		t.Fatal("an agent edited a locked rule's content")
	}
	// The content is untouched.
	rules, err := service.ListRules(ctx, record.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].Name != "original" {
		t.Fatalf("the refused edit changed the rule: %+v", rules)
	}
}
