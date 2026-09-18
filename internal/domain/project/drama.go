package project

import (
	"strings"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// Settings is the per-project configuration of DOMAIN_MODEL §4.3.
//
// It is a value object keyed by the project rather than an entity with its own
// identity: there is exactly one settings row per project, which the schema
// enforces by making project_id the primary key.
type Settings struct {
	ProjectID                  string
	TargetPlatform             string
	AspectRatio                string
	Resolution                 string
	ExpectedEpisodeCount       int
	DefaultEpisodeDurationSecs int
	Audience                   string
	ContentRating              string
	AdaptationMode             AdaptationMode
	Language                   string
	Timezone                   string
	SettingsVersion            int
	Revision                   int64
	CreatedAt                  time.Time
	UpdatedAt                  time.Time
}

// AdaptationMode is how freely an adaptation may depart from its source.
type AdaptationMode string

const (
	AdaptationFaithful   AdaptationMode = "faithful"
	AdaptationBalanced   AdaptationMode = "balanced"
	AdaptationAggressive AdaptationMode = "aggressive"
)

// AdaptationModes lists the documented modes in the schema's order.
var AdaptationModes = []AdaptationMode{AdaptationFaithful, AdaptationBalanced, AdaptationAggressive}

// IsValidAdaptationMode reports whether a mode may be persisted.
func IsValidAdaptationMode(value AdaptationMode) bool {
	for _, candidate := range AdaptationModes {
		if candidate == value {
			return true
		}
	}
	return false
}

// MaxTextLength bounds the free-text settings fields, mirroring nothing in the
// schema but keeping a single UI paste from storing a novel in a caption field.
const MaxTextLength = 500

// DefaultSettings returns the settings a new drama project starts with.
//
// The defaults follow PRD FR-020's list of what a drama project must be able to
// state, with the language taken from the project.
func DefaultSettings(projectID, language string) Settings {
	return Settings{
		ProjectID:                  projectID,
		AdaptationMode:             AdaptationBalanced,
		Language:                   language,
		ExpectedEpisodeCount:       0,
		DefaultEpisodeDurationSecs: 0,
		SettingsVersion:            1,
		Revision:                   1,
	}
}

// Validate checks the settings before they are stored.
func (s Settings) Validate() error {
	if strings.TrimSpace(s.ProjectID) == "" {
		return InvalidError("Settings must belong to a project.")
	}
	if !IsValidAdaptationMode(s.AdaptationMode) {
		return InvalidError("The adaptation mode is not recognised.")
	}
	if s.ExpectedEpisodeCount < 0 {
		return InvalidError("The expected episode count cannot be negative.")
	}
	if s.DefaultEpisodeDurationSecs < 0 {
		return InvalidError("The episode duration cannot be negative.")
	}
	if s.SettingsVersion < 1 {
		return InvalidError("The settings version starts at one.")
	}
	for _, field := range []string{s.TargetPlatform, s.AspectRatio, s.Resolution, s.Audience, s.ContentRating, s.Timezone} {
		if len([]rune(field)) > MaxTextLength {
			return InvalidError("A settings field is too long.")
		}
	}
	return nil
}

// RuleCategory is what a project rule governs (DOMAIN_MODEL §4.4).
type RuleCategory string

const (
	RuleStory     RuleCategory = "story"
	RuleCharacter RuleCategory = "character"
	RuleVisual    RuleCategory = "visual"
	RuleCamera    RuleCategory = "camera"
	RuleAudio     RuleCategory = "audio"
	RuleSafety    RuleCategory = "safety"
	RuleCustom    RuleCategory = "custom"
)

// RuleCategories lists the documented categories in the schema's order.
var RuleCategories = []RuleCategory{
	RuleStory, RuleCharacter, RuleVisual, RuleCamera, RuleAudio, RuleSafety, RuleCustom,
}

// IsValidRuleCategory reports whether a category may be persisted.
func IsValidRuleCategory(value RuleCategory) bool {
	for _, candidate := range RuleCategories {
		if candidate == value {
			return true
		}
	}
	return false
}

// RuleStrength is how binding a rule is.
type RuleStrength string

const (
	RuleAdvisory  RuleStrength = "advisory"
	RuleRequired  RuleStrength = "required"
	RuleImmutable RuleStrength = "immutable"
)

// RuleStrengths lists the documented strengths in the schema's order.
var RuleStrengths = []RuleStrength{RuleAdvisory, RuleRequired, RuleImmutable}

// IsValidRuleStrength reports whether a strength may be persisted.
func IsValidRuleStrength(value RuleStrength) bool {
	for _, candidate := range RuleStrengths {
		if candidate == value {
			return true
		}
	}
	return false
}

// RuleSourceType is how a rule came to exist.
type RuleSourceType string

const (
	RuleSourceUser           RuleSourceType = "user"
	RuleSourceImported       RuleSourceType = "imported"
	RuleSourceAgentSuggested RuleSourceType = "agent_suggested"
)

// RuleSourceTypes lists the documented sources in the schema's order.
var RuleSourceTypes = []RuleSourceType{RuleSourceUser, RuleSourceImported, RuleSourceAgentSuggested}

// IsValidRuleSourceType reports whether a source may be persisted.
func IsValidRuleSourceType(value RuleSourceType) bool {
	for _, candidate := range RuleSourceTypes {
		if candidate == value {
			return true
		}
	}
	return false
}

// RuleStatus is whether a rule still applies.
type RuleStatus string

const (
	RuleActive   RuleStatus = "active"
	RuleArchived RuleStatus = "archived"
)

// IsValidRuleStatus reports whether a status may be persisted.
func IsValidRuleStatus(value RuleStatus) bool {
	switch value {
	case RuleActive, RuleArchived:
		return true
	default:
		return false
	}
}

// Rule is a constraint on the production (DOMAIN_MODEL §4.4).
type Rule struct {
	ID           string
	ProjectID    string
	Category     RuleCategory
	Name         string
	Content      string
	Strength     RuleStrength
	Status       RuleStatus
	SourceType   RuleSourceType
	SourceID     string
	LockedByUser bool
	DeletedAt    time.Time
	DeletedBy    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Revision     int64
}

// MaxRuleNameLength mirrors the schema's CHECK.
const MaxRuleNameLength = 200

// Validate checks a rule before it is stored.
func (r Rule) Validate() error {
	if strings.TrimSpace(r.ProjectID) == "" {
		return InvalidError("A rule must belong to a project.")
	}
	if !IsValidRuleCategory(r.Category) {
		return InvalidError("The rule category is not recognised.")
	}
	trimmed := strings.TrimSpace(r.Name)
	if trimmed == "" {
		return InvalidError("A rule needs a name.")
	}
	if len([]rune(trimmed)) > MaxRuleNameLength {
		return InvalidError("The rule name is too long.")
	}
	if !IsValidRuleStrength(r.Strength) {
		return InvalidError("The rule strength is not recognised.")
	}
	if !IsValidRuleStatus(r.Status) {
		return InvalidError("The rule status is not recognised.")
	}
	if !IsValidRuleSourceType(r.SourceType) {
		return InvalidError("The rule source is not recognised.")
	}
	return nil
}

// WriterKind is who is asking to change a rule.
//
// The distinction exists because §4.4 makes it part of the rule's meaning
// rather than a UI convention: "immutable 或 locked_by_user=true 的规则只有用户
// 命令可修改" and "Agent 可创建建议，但不能把建议自动升级为 required/immutable".
type WriterKind string

const (
	WriterUser   WriterKind = "user"
	WriterAgent  WriterKind = "agent"
	WriterSystem WriterKind = "system"
)

// IsValidWriterKind reports whether a writer kind is recognised.
func IsValidWriterKind(value WriterKind) bool {
	switch value {
	case WriterUser, WriterAgent, WriterSystem:
		return true
	default:
		return false
	}
}

// CanModify reports whether a writer may change or delete this rule.
//
// §4.4 gives two protections and this applies both:
//
//   - an immutable rule or one the user locked accepts only user commands, so
//     an agent cannot quietly rewrite a rule the user set;
//   - a system writer is treated like an agent, because neither is the user and
//     the specification's protection is about the user's authority rather than
//     about which automated component is asking.
func (r Rule) CanModify(writer WriterKind) error {
	if !IsValidWriterKind(writer) {
		return InvalidError("The writer is not recognised.")
	}
	if writer == WriterUser {
		return nil
	}
	if r.LockedByUser || r.Strength == RuleImmutable {
		return ConflictError("This rule is locked, so only a user command can change it.")
	}
	return nil
}

// CanEscalate reports whether a writer may raise a rule's strength.
//
// §4.4: "Agent 可创建建议，但不能把建议自动升级为 required/immutable". An agent
// may therefore create and edit advisory rules, and may not make one binding.
// Lowering a strength is also refused for a non-user writer, because a rule that
// an agent weakened is a change to what the user asked for.
func CanEscalate(writer WriterKind, from, to RuleStrength) error {
	if !IsValidWriterKind(writer) {
		return InvalidError("The writer is not recognised.")
	}
	if !IsValidRuleStrength(from) || !IsValidRuleStrength(to) {
		return InvalidError("The rule strength is not recognised.")
	}
	if writer == WriterUser {
		return nil
	}
	if from != to {
		return ConflictError("Only a user command can change how binding a rule is.")
	}
	return nil
}

// StyleGuide is one versioned style guide (DOMAIN_MODEL §4.5).
//
// §4.5 does not give a field table, so the fields are the eight things it lists:
// visual style, palette, lighting, composition, camera language, negative
// constraints, sound direction, and the reference asset versions — the last of
// which lives in the asset_usages table rather than here, because §2.6 forbids
// carrying a version relation in JSON.
type StyleGuide struct {
	ID                  string
	ProjectID           string
	VersionNumber       int
	Status              versioning.Status
	BasedOnVersionID    string
	VisualStyle         string
	Palette             string
	Lighting            string
	Composition         string
	CameraLanguage      string
	NegativeConstraints string
	SoundDirection      string
	CreatedByType       versioning.CreatedByType
	CreatedByID         string
	ChangeReason        string
	LegacyMetadata      string
	CreatedAt           time.Time
}

// Validate checks a style guide before it is stored.
func (g StyleGuide) Validate() error {
	if strings.TrimSpace(g.ProjectID) == "" {
		return InvalidError("A style guide must belong to a project.")
	}
	if g.VersionNumber < 1 {
		return InvalidError("A style guide version starts at one.")
	}
	if !versioning.IsValidStatus(g.Status) {
		return InvalidError("The style guide status is not recognised.")
	}
	if !versioning.IsValidCreatedByType(g.CreatedByType) {
		return InvalidError("The style guide producer is not recognised.")
	}
	return nil
}

// ProviderPolicyLayer is which agent layer a policy governs (AGENT_CONTRACTS §13).
type ProviderPolicyLayer string

const (
	PolicyDefault     ProviderPolicyLayer = "default"
	PolicyDecision    ProviderPolicyLayer = "decision"
	PolicyExecution   ProviderPolicyLayer = "execution"
	PolicySupervision ProviderPolicyLayer = "supervision"
	PolicyEmbedding   ProviderPolicyLayer = "embedding"
)

// ProviderPolicyLayers lists the documented layers in the schema's order.
var ProviderPolicyLayers = []ProviderPolicyLayer{
	PolicyDefault, PolicyDecision, PolicyExecution, PolicySupervision, PolicyEmbedding,
}

// IsValidProviderPolicyLayer reports whether a layer may be persisted.
func IsValidProviderPolicyLayer(value ProviderPolicyLayer) bool {
	for _, candidate := range ProviderPolicyLayers {
		if candidate == value {
			return true
		}
	}
	return false
}

// ProviderPolicy is a project's default model policy (PRD FR-020, AGENT_CONTRACTS §13).
//
// §3 lists ProjectProviderPolicy in the project aggregate but §4 defines no
// field table for it. PRD FR-020 requires drama settings to carry a default
// model policy and AGENT_CONTRACTS §13 gives the policy object a shape, so the
// object is stored as controlled JSON per §2.6's "非核心模型参数" with the layer
// as a queryable column. ADR-0007 records the decision.
type ProviderPolicy struct {
	ID         string
	ProjectID  string
	Layer      ProviderPolicyLayer
	PolicyJSON string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Revision   int64
}

// Validate checks a policy before it is stored.
func (p ProviderPolicy) Validate() error {
	if strings.TrimSpace(p.ProjectID) == "" {
		return InvalidError("A model policy must belong to a project.")
	}
	if !IsValidProviderPolicyLayer(p.Layer) {
		return InvalidError("The policy layer is not recognised.")
	}
	if strings.TrimSpace(p.PolicyJSON) != "" && !isJSONObject(p.PolicyJSON) {
		return InvalidError("The model policy must be a JSON object.")
	}
	return nil
}

// isJSONObject reports whether the text is a JSON object literal.
//
// The check is deliberately shallow: it rejects the shapes that would make the
// column unreadable (an array, a bare scalar, a fragment) without pretending to
// validate the policy's own fields, which AGENT_CONTRACTS §13 owns.
func isJSONObject(value string) bool {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return false
	}
	return true
}
