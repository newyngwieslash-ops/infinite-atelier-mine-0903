// Package script owns the episode, skeleton, adaptation-strategy and script
// vocabulary and invariants of docs/DOMAIN_MODEL.md §7.
//
// Section 7 describes the drama production pipeline: an Episode is the unit
// that is planned and delivered, a StorySkeletonVersion and an
// AdaptationStrategyVersion are the two structured artifacts PRD FR-040's S1
// and S2 produce, and a Script holds the stable identity of the structured
// screenplay whose content lives in ScriptVersion rows of scenes, dialogue and
// shots.
//
// The package performs no I/O, mints no identifier and reads no clock: the
// application layer supplies identifiers and timestamps, and §2.3's revision
// counters are carried but not validated here because the write path assigns
// them. The version statuses are not declared again — they are the shared
// vocabulary of §2.5, used through internal/domain/versioning, because eight
// per-family copies of one status set would drift apart.
//
// The closed vocabularies below are the exact sets migration 000008 pins in SQL
// CHECK constraints, so a value admitted here is one the database will also
// store and a value refused here is one SQL refuses too.
package script

import (
	"fmt"
	"strings"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// EpisodeStatus is where an episode stands in production
// (DOMAIN_MODEL §7.1).
//
// The five values are §7.1's list, ordered as the schema's CHECK lists them.
// The column belongs to the episode row rather than to a version, so it records
// where the instalment as a whole stands; a new script version's review state is
// a separate question answered by that version's §2.5 status.
type EpisodeStatus string

const (
	EpisodePlanning   EpisodeStatus = "planning"
	EpisodeWriting    EpisodeStatus = "writing"
	EpisodeApproved   EpisodeStatus = "approved"
	EpisodeProduction EpisodeStatus = "production"
	EpisodeCompleted  EpisodeStatus = "completed"
)

// EpisodeStatuses lists the documented episode statuses in the schema's order.
var EpisodeStatuses = []EpisodeStatus{
	EpisodePlanning, EpisodeWriting, EpisodeApproved, EpisodeProduction, EpisodeCompleted,
}

// IsValidEpisodeStatus reports whether an episode status may be persisted.
func IsValidEpisodeStatus(value EpisodeStatus) bool {
	for _, candidate := range EpisodeStatuses {
		if candidate == value {
			return true
		}
	}
	return false
}

// Episode is one instalment of a drama project (DOMAIN_MODEL §7.1).
//
// The season and episode numbers are the episode's business key: §7.1 requires
// (season_number, episode_number) to be unique within a project, which the
// schema enforces with a UNIQUE constraint, and Episode.Key renders the pair
// for messages and headings.
type Episode struct {
	ID           string
	ProjectID    string
	SeasonNumber int
	// EpisodeNumber is the position within the season, starting at one.
	EpisodeNumber int
	Title         string
	Status        EpisodeStatus
	// SourceChapterStartID and SourceChapterEndID bound the material the
	// episode adapts. They are a range rather than a list because §7.1 gives
	// two columns, and a contiguous span is what the adaptation order implies.
	SourceChapterStartID string
	SourceChapterEndID   string
	// TargetDurationSeconds is the planned length. Zero means the episode has
	// no target yet, which §7.1's default of 0 allows; a negative one is a
	// malformed plan and is refused.
	TargetDurationSeconds              int
	CurrentStorySkeletonVersionID      string
	CurrentAdaptationStrategyVersionID string
	CurrentScriptVersionID             string
	DeletedAt                          time.Time
	DeletedBy                          string
	CreatedAt                          time.Time
	UpdatedAt                          time.Time
	Revision                           int64
}

// Validate checks an episode before it is stored.
func (e Episode) Validate() error {
	if strings.TrimSpace(e.ProjectID) == "" {
		return InvalidError("An episode must belong to a project.")
	}
	if e.SeasonNumber < 1 {
		return InvalidError("A season number starts at one.")
	}
	if e.EpisodeNumber < 1 {
		return InvalidError("An episode number starts at one.")
	}
	if !IsValidEpisodeStatus(e.Status) {
		return InvalidError("The episode status is not recognised.")
	}
	if e.TargetDurationSeconds < 0 {
		return InvalidError("A target duration cannot be negative.")
	}
	return nil
}

// Key renders the episode's business key, such as "S1E3" for season 1 episode
// 3.
//
// §7.1 makes the pair unique per project, so this is the shortest string that
// names one episode unambiguously. It is used in uniqueness messages and in the
// UI's episode picker; the identifier remains the thing other rows reference.
func (e Episode) Key() string {
	return fmt.Sprintf("S%dE%d", e.SeasonNumber, e.EpisodeNumber)
}

// StorySkeletonVersion is one version of an episode's story skeleton
// (DOMAIN_MODEL §7.2).
//
// The status is the shared §2.5 vocabulary rather than a local set, because
// §7.2 gives the field no values of its own and the migration's CHECK is the
// same eight-value list every other version family uses. At most one version of
// an episode may be approved, which the schema enforces with a partial unique
// index.
type StorySkeletonVersion struct {
	ID            string
	EpisodeID     string
	VersionNumber int
	Status        versioning.Status
	// BasedOnVersionID links a revision to the version it was edited from,
	// which is what makes AC-SCRIPT-002's "原版本保留" auditable.
	BasedOnVersionID string
	OpeningHook      string
	CoreConflict     string
	// TurningPointsJSON is a controlled structure (§2.6's "受控结构"), not a
	// queryable column: the selected story events are a link table instead.
	TurningPointsJSON string
	Climax            string
	EndingHook        string
	// EstimatedDurationSeconds is this version's own estimate. It is separate
	// from the episode's target so a version can be compared against the plan.
	EstimatedDurationSeconds int
	SourceAgentRunID         string
	CreatedByType            versioning.CreatedByType
	CreatedByID              string
	ChangeReason             string
	LegacyMetadata           string
	CreatedAt                time.Time
}

// Validate checks a story skeleton version before it is stored.
func (s StorySkeletonVersion) Validate() error {
	if strings.TrimSpace(s.EpisodeID) == "" {
		return InvalidError("A story skeleton version must belong to an episode.")
	}
	if s.VersionNumber < 1 {
		return InvalidError("A version number starts at one.")
	}
	if !versioning.IsValidStatus(s.Status) {
		return InvalidError("The story skeleton status is not recognised.")
	}
	if !versioning.IsValidCreatedByType(s.CreatedByType) {
		return InvalidError("The story skeleton producer is not recognised.")
	}
	if s.EstimatedDurationSeconds < 0 {
		return InvalidError("An estimated duration cannot be negative.")
	}
	return nil
}

// AdaptationMode is how freely a strategy may depart from the source
// (DOMAIN_MODEL §7.3).
//
// It is declared here rather than imported from project.AdaptationMode: §7.3
// gives an adaptation strategy version its own adaptation_mode field, and the
// script domain must not depend on the project aggregate to state it — §7 is
// reachable without §4. The two types happen to carry the same three strings
// because migration 000006 and migration 000008 pin the same CHECK list, and
// that is a coupling this package expresses in its migration-pinning test
// rather than by importing the other domain.
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

// AdaptationStrategyVersion is one version of an episode's adaptation strategy
// (DOMAIN_MODEL §7.3).
//
// §7.3 says the event lists must be relations or controlled structures rather
// than Markdown ("列表使用关联表或受控结构，不能只保存 Markdown"): the retained,
// removed and reordered events live in the adaptation_strategy_event_links
// table, and MergedEventGroupsJSON holds only the merge groups, which are not
// individually addressable facts.
type AdaptationStrategyVersion struct {
	ID            string
	EpisodeID     string
	VersionNumber int
	Status        versioning.Status
	// BasedOnVersionID links this strategy to the one it revised.
	BasedOnVersionID string
	StrategySummary  string
	AdaptationMode   AdaptationMode
	// MergedEventGroupsJSON groups several story events into one dramatized
	// beat. Which events were retained, removed or reordered is a link table,
	// because §2.6 forbids carrying a queryable state in JSON.
	MergedEventGroupsJSON string
	OriginalAdditions     string
	Rationale             string
	Risks                 string
	SourceAgentRunID      string
	CreatedByType         versioning.CreatedByType
	CreatedByID           string
	ChangeReason          string
	LegacyMetadata        string
	CreatedAt             time.Time
}

// Validate checks an adaptation strategy version before it is stored.
func (a AdaptationStrategyVersion) Validate() error {
	if strings.TrimSpace(a.EpisodeID) == "" {
		return InvalidError("An adaptation strategy version must belong to an episode.")
	}
	if a.VersionNumber < 1 {
		return InvalidError("A version number starts at one.")
	}
	if !versioning.IsValidStatus(a.Status) {
		return InvalidError("The adaptation strategy status is not recognised.")
	}
	if !IsValidAdaptationMode(a.AdaptationMode) {
		return InvalidError("The adaptation mode is not recognised.")
	}
	if !versioning.IsValidCreatedByType(a.CreatedByType) {
		return InvalidError("The adaptation strategy producer is not recognised.")
	}
	return nil
}

// Script is the stable identity of an episode's screenplay
// (DOMAIN_MODEL §7.4).
//
// §7.4 separates identity from content on purpose: the script row is what
// scenes and downstream artifacts hang off, and it survives every re-write of
// the text. The schema enforces one script per episode with a UNIQUE
// constraint, so there is no episode field combination to validate beyond the
// episode reference itself.
type Script struct {
	ID        string
	EpisodeID string
	// CurrentVersionID is empty until a version is created, and it names the
	// version that is in force rather than replacing the older ones.
	CurrentVersionID string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Revision         int64
}

// Validate checks a script before it is stored.
func (s Script) Validate() error {
	if strings.TrimSpace(s.EpisodeID) == "" {
		return InvalidError("A script must belong to an episode.")
	}
	return nil
}

// ScriptVersion is one version of a script's content (DOMAIN_MODEL §7.5).
//
// The two upstream references are required by §7.5 and by AC-SCRIPT-003: a
// script version is adapted from a specific skeleton and a specific strategy,
// and those references are what §15.2's staleness chain walks when either
// upstream changes. Without them a regenerated skeleton could not mark the
// scripts that were built on it.
type ScriptVersion struct {
	ID            string
	ScriptID      string
	VersionNumber int
	Status        versioning.Status
	// BasedOnVersionID links a revision to the version a user edited.
	BasedOnVersionID string
	// StorySkeletonVersionID and AdaptationStrategyVersionID name the two
	// inputs this version was produced from.
	StorySkeletonVersionID      string
	AdaptationStrategyVersionID string
	EstimatedDurationSeconds    int
	Summary                     string
	SourceAgentRunID            string
	CreatedByType               versioning.CreatedByType
	CreatedByID                 string
	ChangeReason                string
	LegacyMetadata              string
	CreatedAt                   time.Time
}

// Validate checks a script version before it is stored.
func (v ScriptVersion) Validate() error {
	if strings.TrimSpace(v.ScriptID) == "" {
		return InvalidError("A script version must belong to a script.")
	}
	if v.VersionNumber < 1 {
		return InvalidError("A version number starts at one.")
	}
	if !versioning.IsValidStatus(v.Status) {
		return InvalidError("The script version status is not recognised.")
	}
	if strings.TrimSpace(v.StorySkeletonVersionID) == "" {
		return InvalidError("A script version must name the story skeleton it was adapted from.")
	}
	if strings.TrimSpace(v.AdaptationStrategyVersionID) == "" {
		return InvalidError("A script version must name the adaptation strategy it was adapted from.")
	}
	if v.EstimatedDurationSeconds < 0 {
		return InvalidError("An estimated duration cannot be negative.")
	}
	if !versioning.IsValidCreatedByType(v.CreatedByType) {
		return InvalidError("The script version producer is not recognised.")
	}
	return nil
}

// InteriorExterior is a scene's interior/exterior marking
// (DOMAIN_MODEL §7.6).
//
// The four values are the schema's, spelled in the uppercase a slugline uses.
// INT_EXT is one value rather than two flags because a scene that moves between
// inside and outside is a single scene with one location heading.
type InteriorExterior string

const (
	InteriorINT    InteriorExterior = "INT"
	InteriorEXT    InteriorExterior = "EXT"
	InteriorINTEXT InteriorExterior = "INT_EXT"
	InteriorOTHER  InteriorExterior = "OTHER"
)

// InteriorExteriors lists the documented markings in the schema's order.
var InteriorExteriors = []InteriorExterior{InteriorINT, InteriorEXT, InteriorINTEXT, InteriorOTHER}

// IsValidInteriorExterior reports whether a marking may be persisted.
func IsValidInteriorExterior(value InteriorExterior) bool {
	for _, candidate := range InteriorExteriors {
		if candidate == value {
			return true
		}
	}
	return false
}

// Scene is one scene of a script version (DOMAIN_MODEL §7.6).
//
// §7.6 puts the scene inside a specific ScriptVersion, so the ordinal is unique
// within that version rather than within the script. LocationEntityID is a
// reference into the story graph rather than a copied name, which is what lets
// a renamed location update every scene that uses it.
type Scene struct {
	ID              string
	ScriptVersionID string
	Ordinal         int
	// SceneNumber is the number shown in the slugline. It is text rather than
	// an integer because a revised script writes numbers like "12A".
	SceneNumber      string
	Slugline         string
	InteriorExterior InteriorExterior
	LocationEntityID string
	TimeOfDay        string
	Summary          string
	DramaticGoal     string
	// EstimatedDurationSeconds is the estimate that AC-SCRIPT-003's duration
	// check sums over a version.
	EstimatedDurationSeconds int
	// SourceStoryEventID names the story event this scene dramatizes, empty for
	// an invention of the adaptation.
	SourceStoryEventID string
	// IsOriginalAdaptation marks a scene that is not in the source material,
	// which is what AC-SCRIPT-003's "原创改编标记" asks for. A scene that is
	// original but not marked would be indistinguishable from a faithful one.
	IsOriginalAdaptation bool
	CreatedAt            time.Time
	UpdatedAt            time.Time
	Revision             int64
}

// Validate checks a scene before it is stored.
func (s Scene) Validate() error {
	if strings.TrimSpace(s.ScriptVersionID) == "" {
		return InvalidError("A scene must belong to a script version.")
	}
	if s.Ordinal < 1 {
		return InvalidError("A scene ordinal starts at one.")
	}
	if !IsValidInteriorExterior(s.InteriorExterior) {
		return InvalidError("The interior/exterior marking is not recognised.")
	}
	if s.EstimatedDurationSeconds < 0 {
		return InvalidError("An estimated duration cannot be negative.")
	}
	return nil
}

// LineType is what a dialogue line holds (DOMAIN_MODEL §7.7).
type LineType string

const (
	LineDialogue   LineType = "dialogue"
	LineNarration  LineType = "narration"
	LineAction     LineType = "action"
	LineTransition LineType = "transition"
	LineNote       LineType = "note"
)

// LineTypes lists the documented line types in the schema's order.
var LineTypes = []LineType{LineDialogue, LineNarration, LineAction, LineTransition, LineNote}

// IsValidLineType reports whether a line type may be persisted.
func IsValidLineType(value LineType) bool {
	for _, candidate := range LineTypes {
		if candidate == value {
			return true
		}
	}
	return false
}

// DialogueLine is one ordered line of a scene (DOMAIN_MODEL §7.7).
//
// The type carries the shape: a spoken line names a character entity while
// action and narration do not, and Locked marks a line a user protected from
// regeneration. §7.7 gives no rule forcing CharacterEntityID on dialogue,
// because an off-screen voice or a crowd line legitimately has none.
type DialogueLine struct {
	ID      string
	SceneID string
	Ordinal int
	Type    LineType
	// CharacterEntityID is empty for a line no character speaks.
	CharacterEntityID  string
	Text               string
	Emotion            string
	PerformanceNote    string
	SourceStoryEventID string
	// Locked is set by the user. §7.7 carries it on the line rather than on the
	// scene, so a regeneration pass can declare which lines it must leave
	// alone; AC-SCRIPT-002's "锁定字段不变" is the same protection applied to a
	// FIX attempt.
	Locked    bool
	CreatedAt time.Time
	UpdatedAt time.Time
	Revision  int64
}

// Validate checks a dialogue line before it is stored.
func (d DialogueLine) Validate() error {
	if strings.TrimSpace(d.SceneID) == "" {
		return InvalidError("A dialogue line must belong to a scene.")
	}
	if d.Ordinal < 1 {
		return InvalidError("A dialogue line ordinal starts at one.")
	}
	if !IsValidLineType(d.Type) {
		return InvalidError("The line type is not recognised.")
	}
	return nil
}

// Shot is one camera setup inside a scene (DOMAIN_MODEL §7.8).
//
// §7.8 lets a shot be created at the draft stage and refined later by the
// storyboard workflow, so the row carries its own revision and status. Its
// asset references are AssetUsage rows (§8.6) rather than columns here, which
// is what §2.6's ban on "Shot 与资产引用" in JSON requires.
type Shot struct {
	ID      string
	SceneID string
	Ordinal int
	// ShotNumber is the label a storyboard shows, empty while the shot is a
	// draft that has not been numbered.
	ShotNumber               string
	ShotSize                 string
	CameraAngle              string
	CameraMovement           string
	EstimatedDurationSeconds int
	VisualDescription        string
	ActionDescription        string
	AudioIntent              string
	ContinuityNotes          string
	Status                   versioning.Status
	CreatedAt                time.Time
	UpdatedAt                time.Time
	Revision                 int64
}

// Validate checks a shot before it is stored.
func (s Shot) Validate() error {
	if strings.TrimSpace(s.SceneID) == "" {
		return InvalidError("A shot must belong to a scene.")
	}
	if s.Ordinal < 1 {
		return InvalidError("A shot ordinal starts at one.")
	}
	if !versioning.IsValidStatus(s.Status) {
		return InvalidError("The shot status is not recognised.")
	}
	if s.EstimatedDurationSeconds < 0 {
		return InvalidError("An estimated duration cannot be negative.")
	}
	return nil
}

// ValidateForStoryboard checks that a shot is complete enough to draw.
//
// DOMAIN_MODEL §9.4 gives StoryboardItem a shot_id and §9.5 makes the link
// binding ("StoryboardItem 必须唯一对应本版本的 Shot"), while §9.5's
// StoryboardPanelVersion carries a visual_prompt built from the shot. Both
// presume the shot says which setup it is and what is in frame, so a shot
// destined for a board needs a ShotNumber to be referenced by and a
// VisualDescription to become the prompt. This is the storyboard precondition
// rather than a general shot rule, which is why it is a separate method — a
// draft shot with neither field is a legitimate row, and only the storyboard
// workflow needs the stronger guarantee.
//
// The storyboard aggregate itself, and the panel obligations §9.5 lists, belong
// to the storyboard domain.
func (s Shot) ValidateForStoryboard() error {
	if err := s.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(s.ShotNumber) == "" {
		return InvalidError("A shot needs a number before it can be storyboarded.")
	}
	if strings.TrimSpace(s.VisualDescription) == "" {
		return InvalidError("A shot needs a visual description before it can be storyboarded.")
	}
	return nil
}
