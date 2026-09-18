package project

import "testing"

// TestSettingsVocabularyAndDefaults covers §4.3's adaptation mode, which is the
// only closed vocabulary the settings carry, plus the defaults a new drama
// project starts from.
func TestSettingsVocabularyAndDefaults(t *testing.T) {
	for _, mode := range []AdaptationMode{AdaptationFaithful, AdaptationBalanced, AdaptationAggressive} {
		if !IsValidAdaptationMode(mode) {
			t.Fatalf("documented adaptation mode %q rejected", mode)
		}
	}
	for _, mode := range []AdaptationMode{"", "loose", "Faithful", "strict"} {
		if IsValidAdaptationMode(mode) {
			t.Fatalf("undocumented adaptation mode %q accepted", mode)
		}
	}
	settings := DefaultSettings("project-1", "zh-CN")
	if settings.AdaptationMode != AdaptationBalanced {
		t.Fatalf("a new project must start balanced, got %q", settings.AdaptationMode)
	}
	if settings.Language != "zh-CN" {
		t.Fatalf("the default settings must inherit the project language, got %q", settings.Language)
	}
	if err := settings.Validate(); err != nil {
		t.Fatalf("the defaults must be valid: %v", err)
	}
}

// TestSettingsValidate covers the bounds the schema also enforces.
func TestSettingsValidate(t *testing.T) {
	base := DefaultSettings("project-1", "zh-CN")
	cases := []struct {
		name   string
		mutate func(*Settings)
	}{
		{"no project", func(s *Settings) { s.ProjectID = "  " }},
		{"unknown adaptation mode", func(s *Settings) { s.AdaptationMode = "loose" }},
		{"negative episode count", func(s *Settings) { s.ExpectedEpisodeCount = -1 }},
		{"negative duration", func(s *Settings) { s.DefaultEpisodeDurationSecs = -1 }},
		{"zero settings version", func(s *Settings) { s.SettingsVersion = 0 }},
		{"an over-long field", func(s *Settings) { s.AspectRatio = string(make([]rune, MaxTextLength+1)) }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			settings := base
			testCase.mutate(&settings)
			if err := settings.Validate(); err == nil {
				t.Fatal("malformed settings were accepted")
			}
		})
	}
}

// TestRuleVocabularies pins §4.4's four closed sets.
func TestRuleVocabularies(t *testing.T) {
	for _, category := range []RuleCategory{RuleStory, RuleCharacter, RuleVisual, RuleCamera, RuleAudio, RuleSafety, RuleCustom} {
		if !IsValidRuleCategory(category) {
			t.Fatalf("documented rule category %q rejected", category)
		}
	}
	if len(RuleCategories) != 7 {
		t.Fatalf("category set has %d entries, want 7", len(RuleCategories))
	}
	for _, strength := range []RuleStrength{RuleAdvisory, RuleRequired, RuleImmutable} {
		if !IsValidRuleStrength(strength) {
			t.Fatalf("documented rule strength %q rejected", strength)
		}
	}
	if len(RuleStrengths) != 3 {
		t.Fatalf("strength set has %d entries, want 3", len(RuleStrengths))
	}
	for _, source := range []RuleSourceType{RuleSourceUser, RuleSourceImported, RuleSourceAgentSuggested} {
		if !IsValidRuleSourceType(source) {
			t.Fatalf("documented rule source %q rejected", source)
		}
	}
	if len(RuleSourceTypes) != 3 {
		t.Fatalf("source set has %d entries, want 3", len(RuleSourceTypes))
	}
	for _, status := range []RuleStatus{RuleActive, RuleArchived} {
		if !IsValidRuleStatus(status) {
			t.Fatalf("documented rule status %q rejected", status)
		}
	}
	for _, category := range []RuleCategory{"", "Story", "lighting"} {
		if IsValidRuleCategory(category) {
			t.Fatalf("undocumented category %q accepted", category)
		}
	}
	for _, strength := range []RuleStrength{"", "hard", "Required"} {
		if IsValidRuleStrength(strength) {
			t.Fatalf("undocumented strength %q accepted", strength)
		}
	}
}

// TestLockedRuleAcceptsOnlyUserCommands is the "locked rule" acceptance item:
// §4.4's "immutable 或 locked_by_user=true 的规则只有用户命令可修改".
func TestLockedRuleAcceptsOnlyUserCommands(t *testing.T) {
	open := Rule{
		ProjectID:  "project-1",
		Category:   RuleStory,
		Name:       "Keep the ending ambiguous",
		Strength:   RuleAdvisory,
		Status:     RuleActive,
		SourceType: RuleSourceUser,
	}
	if err := open.Validate(); err != nil {
		t.Fatalf("a well-formed rule was rejected: %v", err)
	}
	// An unlocked advisory rule accepts any writer.
	for _, writer := range []WriterKind{WriterUser, WriterAgent, WriterSystem} {
		if err := open.CanModify(writer); err != nil {
			t.Fatalf("%s must be able to modify an unlocked advisory rule: %v", writer, err)
		}
	}

	// Locked by the user: agents and the system are refused, the user is not.
	locked := open
	locked.LockedByUser = true
	if err := locked.CanModify(WriterUser); err != nil {
		t.Fatalf("a user must be able to modify a locked rule: %v", err)
	}
	for _, writer := range []WriterKind{WriterAgent, WriterSystem} {
		if err := locked.CanModify(writer); err == nil {
			t.Fatalf("%s was allowed to modify a rule locked by the user", writer)
		}
	}

	// Immutable has the same protection even when the user never locked it.
	immutable := open
	immutable.Strength = RuleImmutable
	if err := immutable.CanModify(WriterUser); err != nil {
		t.Fatalf("a user must be able to modify an immutable rule: %v", err)
	}
	for _, writer := range []WriterKind{WriterAgent, WriterSystem} {
		if err := immutable.CanModify(writer); err == nil {
			t.Fatalf("%s was allowed to modify an immutable rule", writer)
		}
	}

	// A required but unlocked rule is still modifiable by an agent, because
	// §4.4's protection names immutable and locked_by_user, not required.
	required := open
	required.Strength = RuleRequired
	if err := required.CanModify(WriterAgent); err != nil {
		t.Fatalf("an agent may revise a required rule that is not locked: %v", err)
	}

	if err := open.CanModify(""); err == nil {
		t.Fatal("an unrecognised writer must be refused")
	}
}

// TestRuleStrengthEscalationIsUserOnly covers §4.4's second protection:
// "Agent 可创建建议，但不能把建议自动升级为 required/immutable".
func TestRuleStrengthEscalationIsUserOnly(t *testing.T) {
	// A user may move a rule in either direction.
	for _, from := range RuleStrengths {
		for _, to := range RuleStrengths {
			if err := CanEscalate(WriterUser, from, to); err != nil {
				t.Fatalf("a user must be able to set %s to %s: %v", from, to, err)
			}
		}
	}
	// An agent may leave the strength alone.
	for _, strength := range RuleStrengths {
		if err := CanEscalate(WriterAgent, strength, strength); err != nil {
			t.Fatalf("an agent may edit a %s rule without changing its strength: %v", strength, err)
		}
	}
	// An agent may not raise it, which is the case the specification names.
	if err := CanEscalate(WriterAgent, RuleAdvisory, RuleRequired); err == nil {
		t.Fatal("an agent raised an advisory rule to required")
	}
	if err := CanEscalate(WriterAgent, RuleAdvisory, RuleImmutable); err == nil {
		t.Fatal("an agent raised an advisory rule to immutable")
	}
	if err := CanEscalate(WriterAgent, RuleRequired, RuleImmutable); err == nil {
		t.Fatal("an agent raised a required rule to immutable")
	}
	// Nor lower it: weakening what the user asked for is the same authority
	// problem in the other direction.
	if err := CanEscalate(WriterAgent, RuleImmutable, RuleAdvisory); err == nil {
		t.Fatal("an agent weakened an immutable rule")
	}
	if err := CanEscalate(WriterSystem, RuleRequired, RuleAdvisory); err == nil {
		t.Fatal("the system weakened a required rule")
	}
	if err := CanEscalate(WriterAgent, RuleAdvisory, "hard"); err == nil {
		t.Fatal("an unrecognised strength must be refused")
	}
	if err := CanEscalate("robot", RuleAdvisory, RuleAdvisory); err == nil {
		t.Fatal("an unrecognised writer must be refused")
	}
}

// TestRuleValidate covers the shape checks.
func TestRuleValidate(t *testing.T) {
	base := Rule{
		ProjectID:  "project-1",
		Category:   RuleStory,
		Name:       "A rule",
		Strength:   RuleAdvisory,
		Status:     RuleActive,
		SourceType: RuleSourceAgentSuggested,
	}
	cases := []struct {
		name   string
		mutate func(*Rule)
	}{
		{"no project", func(r *Rule) { r.ProjectID = "" }},
		{"unknown category", func(r *Rule) { r.Category = "lighting" }},
		{"empty name", func(r *Rule) { r.Name = "   " }},
		{"over-long name", func(r *Rule) { r.Name = string(make([]rune, MaxRuleNameLength+1)) }},
		{"unknown strength", func(r *Rule) { r.Strength = "hard" }},
		{"unknown status", func(r *Rule) { r.Status = "deleted" }},
		{"unknown source", func(r *Rule) { r.SourceType = "robot" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			rule := base
			testCase.mutate(&rule)
			if err := rule.Validate(); err == nil {
				t.Fatal("a malformed rule was accepted")
			}
		})
	}
}

// TestStyleGuideValidate covers the §4.5 versioned guide.
func TestStyleGuideValidate(t *testing.T) {
	base := StyleGuide{
		ProjectID:     "project-1",
		VersionNumber: 1,
		Status:        "draft",
		CreatedByType: "user",
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed style guide was rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*StyleGuide)
	}{
		{"no project", func(g *StyleGuide) { g.ProjectID = "" }},
		{"zero version", func(g *StyleGuide) { g.VersionNumber = 0 }},
		{"unknown status", func(g *StyleGuide) { g.Status = "review" }},
		{"unknown producer", func(g *StyleGuide) { g.CreatedByType = "robot" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			guide := base
			testCase.mutate(&guide)
			if err := guide.Validate(); err == nil {
				t.Fatal("a malformed style guide was accepted")
			}
		})
	}
}

// TestProviderPolicyValidate covers the layer vocabulary and the JSON shape.
func TestProviderPolicyValidate(t *testing.T) {
	for _, layer := range []ProviderPolicyLayer{PolicyDefault, PolicyDecision, PolicyExecution, PolicySupervision, PolicyEmbedding} {
		if !IsValidProviderPolicyLayer(layer) {
			t.Fatalf("documented policy layer %q rejected", layer)
		}
	}
	if len(ProviderPolicyLayers) != 5 {
		t.Fatalf("layer set has %d entries, want 5", len(ProviderPolicyLayers))
	}
	for _, layer := range []ProviderPolicyLayer{"", "Default", "planning"} {
		if IsValidProviderPolicyLayer(layer) {
			t.Fatalf("undocumented policy layer %q accepted", layer)
		}
	}

	base := ProviderPolicy{ProjectID: "project-1", Layer: PolicyDefault, PolicyJSON: `{"primaryModelId":"m-1"}`}
	if err := base.Validate(); err != nil {
		t.Fatalf("a well-formed policy was rejected: %v", err)
	}
	// An empty policy is allowed: a project need not pin a model.
	empty := base
	empty.PolicyJSON = ""
	if err := empty.Validate(); err != nil {
		t.Fatalf("an unset policy must be allowed: %v", err)
	}
	// Non-object JSON is refused, because the column is read as an object.
	for _, bad := range []string{"[]", `"text"`, "42", "{unterminated"} {
		policy := base
		policy.PolicyJSON = bad
		if err := policy.Validate(); err == nil {
			t.Fatalf("the non-object policy %q was accepted", bad)
		}
	}
	cases := []struct {
		name   string
		mutate func(*ProviderPolicy)
	}{
		{"no project", func(p *ProviderPolicy) { p.ProjectID = "" }},
		{"unknown layer", func(p *ProviderPolicy) { p.Layer = "planning" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			policy := base
			testCase.mutate(&policy)
			if err := policy.Validate(); err == nil {
				t.Fatal("a malformed policy was accepted")
			}
		})
	}
}
