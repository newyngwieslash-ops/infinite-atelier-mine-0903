// Package storyboard owns the director-plan and storyboard vocabulary and
// invariants of DOMAIN_MODEL section 9: the plan a scene is staged from, the
// storyboard that projects a script's shots into a shootable list, and the panel
// versions an image is generated and approved into.
//
// Section 9 gives the field tables and three invariants, and PRD FR-070 states
// what a storyboard table must carry per shot and how a panel becomes canonical.
// The tables already exist (migration 000010); generating their content belongs
// to WP-09, so what is here is the shape, the vocabularies and the rules the
// writer will have to obey.
//
// Versions reuse the shared status vocabulary of the versioning package rather
// than declaring another copy (section 2.5 defines one set for every versioned
// aggregate).
//
// The package performs no I/O and never mints an identifier (ADR-0005).
package storyboard

import (
	"strings"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// MaxShotCountPerVersion bounds how many storyboard items one storyboard
// version may hold.
//
// A storyboard version is the fan-out point of the drama pipeline: every item
// becomes a panel family, and every approved panel becomes an image job
// (FR-070's "每个 Shot 可有多个面板版本" over "可从一个 Episode 批量生成完整
// Storyboard Table"), so an unbounded item count multiplies into the job queue.
// The bound is a guard against a generation loop that fills a version without
// terminating, not a creative limit: the specification names no maximum, and the
// only figure it gives is the acceptance scenario's minimum of twelve shots
// (PRD AC-E2E-002). The number is therefore this package's choice, and it is
// deliberately far above anything the specifications describe so a legitimate
// episode cannot reach it.
const MaxShotCountPerVersion = 5000

// ValidateShotCount rejects a storyboard version size the writer may not store.
func ValidateShotCount(count int) error {
	if count < 0 {
		return InvalidError("A storyboard version cannot hold a negative number of shots.")
	}
	if count > MaxShotCountPerVersion {
		return InvalidError("This storyboard version holds more shots than a single version may carry.")
	}
	return nil
}

// DirectorPlanVersion is one version of an episode's director plan (§9.1).
//
// It is the versioned statement of how the episode is staged, and §9.1 makes
// script_version_id mandatory because a plan is a reading of a specific script
// version: the schema stores it as a foreign key with ON DELETE RESTRICT so the
// script it interprets cannot disappear underneath it.
//
// Shot overrides live in shot_overrides_json. §9.1 says "场次/镜头覆盖优先通过
// 子表保存，MVP 可使用受控 JSON", so the JSON is a deliberate MVP form rather
// than a substitute for a relation, and v1 normalises it into a child table.
type DirectorPlanVersion struct {
	ID                string
	EpisodeID         string
	VersionNumber     int
	Status            versioning.Status
	BasedOnVersionID  string
	ScriptVersionID   string
	VisualRhythm      string
	CameraLanguage    string
	ColorLighting     string
	Staging           string
	ContinuityRules   string
	AudioDirection    string
	ShotOverridesJSON string
	SourceAgentRunID  string
	CreatedByType     versioning.CreatedByType
	CreatedByID       string
	ChangeReason      string
	LegacyMetadata    string
	CreatedAt         time.Time
	// There is deliberately NO UpdatedAt and NO Revision here, and the reason is the
	// schema rather than a preference: migration 000010 gives `director_plan_versions` a
	// `created_at` and nothing else, so a field would be one nothing could store. The
	// `revision` column that migration does declare belongs to `storyboards`.
	//
	// WP-09's first attempt at the shot-overrides write added both and guarded on the
	// revision — and then this table's DDL was read, which is the check that caught it. A
	// guarded write needs a guard to read; a plan version has none, so its overrides write
	// replaces the document without one. The concurrency the plan versions DO have is their
	// uniqueness per episode, which is what versioning by number gives them.
}

// Validate checks a director plan version before it is stored.
func (p DirectorPlanVersion) Validate() error {
	if strings.TrimSpace(p.EpisodeID) == "" {
		return InvalidError("A director plan must belong to an episode.")
	}
	if p.VersionNumber < 1 {
		return InvalidError("A director plan version starts at one.")
	}
	if !versioning.IsValidStatus(p.Status) {
		return InvalidError("The director plan status is not recognised.")
	}
	if strings.TrimSpace(p.ScriptVersionID) == "" {
		return InvalidError("A director plan must name the script version it stages.")
	}
	if !versioning.IsValidCreatedByType(p.CreatedByType) {
		return InvalidError("The director plan producer is not recognised.")
	}
	return nil
}

// Storyboard is the identity one episode's storyboard hangs off (§9.2).
//
// Section 9.2 gives it no content: the versions carry that. It exists so the
// approved-version switch is a per-episode fact, which is what the schema's
// partial unique index on storyboard_id enforces.
type Storyboard struct {
	ID               string
	EpisodeID        string
	CurrentVersionID string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Revision         int64
}

// Validate checks the storyboard identity row before it is stored.
func (s Storyboard) Validate() error {
	if strings.TrimSpace(s.EpisodeID) == "" {
		return InvalidError("A storyboard must belong to an episode.")
	}
	if s.Revision < 1 {
		return InvalidError("A storyboard revision starts at one.")
	}
	return nil
}

// StoryboardVersion is one version of a storyboard (§9.3).
//
// Section 9.3 makes the version name both the script and the director plan it
// was derived from, and the migration comment says why those are two columns of
// the version rather than one link: "the version names both the script and the
// director plan it was derived from, which is what the stale chain walks".
// Section 15.2 walks Script → Director Plan → Storyboard, so a storyboard built
// from a plan that a later script revision invalidated is detectable only
// because both references are recorded.
type StoryboardVersion struct {
	ID                    string
	StoryboardID          string
	VersionNumber         int
	Status                versioning.Status
	ScriptVersionID       string
	DirectorPlanVersionID string
	BasedOnVersionID      string
	SourceAgentRunID      string
	CreatedByType         versioning.CreatedByType
	CreatedByID           string
	ChangeReason          string
	LegacyMetadata        string
	CreatedAt             time.Time
}

// Validate checks a storyboard version before it is stored.
func (v StoryboardVersion) Validate() error {
	if strings.TrimSpace(v.StoryboardID) == "" {
		return InvalidError("A storyboard version must belong to a storyboard.")
	}
	if v.VersionNumber < 1 {
		return InvalidError("A storyboard version number starts at one.")
	}
	if !versioning.IsValidStatus(v.Status) {
		return InvalidError("The storyboard version status is not recognised.")
	}
	if strings.TrimSpace(v.ScriptVersionID) == "" {
		return InvalidError("A storyboard version must name the script version it was built from.")
	}
	if strings.TrimSpace(v.DirectorPlanVersionID) == "" {
		return InvalidError("A storyboard version must name the director plan it was built from.")
	}
	if !versioning.IsValidCreatedByType(v.CreatedByType) {
		return InvalidError("The storyboard version producer is not recognised.")
	}
	return nil
}

// ValidateAgainst reports whether this version was in fact built from the two
// inputs a caller claims it was.
//
// Section 9.3 lists script_version_id and director_plan_version_id as fields of
// the version rather than as provenance metadata, which makes them part of what
// the version means: "this storyboard is a reading of that script through that
// plan". A caller that hands in different identifiers is either about to record
// a reference the version does not have, or is looking at a version that was
// rebuilt from other inputs. Both are refused rather than reconciled, because
// the recorded pair is the one the stale chain of section 15.2 will walk.
func (v StoryboardVersion) ValidateAgainst(scriptVersionID, directorPlanVersionID string) error {
	if strings.TrimSpace(v.ScriptVersionID) == "" || strings.TrimSpace(v.DirectorPlanVersionID) == "" {
		return InvalidError("A storyboard version must name both the script version and the director plan it was built from.")
	}
	if v.ScriptVersionID != scriptVersionID || v.DirectorPlanVersionID != directorPlanVersionID {
		return ConflictError("This storyboard version was built from different inputs than the ones supplied.")
	}
	return nil
}

// StoryboardItem is one shot's row in a storyboard version (§9.4).
//
// Section 9.4's first invariant is "StoryboardItem 必须唯一对应本版本的 Shot",
// which is why shot_id is required and why the schema carries UNIQUE
// (storyboard_version_id, shot_id): a shot appearing twice in one version would
// give it two durations and two positions in the edit.
//
// PRD FR-070 lists seventeen things a storyboard table must show, and section
// 9.4's field table is narrower than that list: it carries the staging
// properties of the row (景别, 机位, 镜头运动, 预计时长, 视觉与动作描述,
// 对白/旁白摘要, 连续性备注) and nothing else. FR-070's asset references are
// normalised away rather than restated: section 7.8 puts them in
// AssetUsage/ShotAssetReference, so the item cites nothing.
//
// This comment used to defer FR-070's first-frame, last-frame and video-motion
// descriptions the same way, and that was wrong. Migration 000018 adds them, and
// the reason is what the two things ARE: a Shot is a line of the SCRIPT — what
// happens, in what order, said by whom — written before anybody decided what the
// camera does. "The first frame shows X" is not a fact about the script's shot but
// a SHOOTING decision, so there was nowhere to store it until the columns existed.
type StoryboardItem struct {
	ID                   string
	StoryboardVersionID  string
	ShotID               string
	Ordinal              int
	ShotSize             string
	CameraAngle          string
	CameraMovement       string
	DurationSeconds      int
	VisualDescription    string
	ActionDescription    string
	DialogueAudioSummary string
	ContinuityNotes      string
	// FirstFrameDescription, LastFrameDescription and VideoMotionDescription are
	// FR-070's remaining three fields. They are what a video model is given to render
	// the shot's two ends and its motion, which is why they are the item's rather than
	// the script's.
	FirstFrameDescription  string
	LastFrameDescription   string
	VideoMotionDescription string
	Status                 versioning.Status
	CreatedAt              time.Time
	UpdatedAt              time.Time
	Revision               int64
}

// Validate checks a storyboard item before it is stored.
func (i StoryboardItem) Validate() error {
	if strings.TrimSpace(i.StoryboardVersionID) == "" {
		return InvalidError("A storyboard item must belong to a storyboard version.")
	}
	if strings.TrimSpace(i.ShotID) == "" {
		return InvalidError("A storyboard item must name the shot it shows.")
	}
	if i.Ordinal < 1 {
		return InvalidError("A storyboard item position starts at one.")
	}
	if i.DurationSeconds < 0 {
		return InvalidError("A storyboard item duration cannot be negative.")
	}
	if !versioning.IsValidStatus(i.Status) {
		return InvalidError("The storyboard item status is not recognised.")
	}
	if i.Revision < 1 {
		return InvalidError("A storyboard item revision starts at one.")
	}
	return nil
}

// StoryboardPanelVersion is one version of a panel for a storyboard item (§9.5).
//
// Section 9.5's third invariant is "approved image 必须属于该 Panel 的候选或经
// 用户明确关联", and the migration records the consequence: the approved image is
// a column holding an asset version, because §2.6 forbids carrying a version
// relation in JSON ("禁止用 JSON 代替 ... 版本关系"), so reference_policy_json
// stays policy metadata while the approval is a column. A panel accumulates
// candidate images the user chooses between, and approving one that is not among
// them would record a canonical image the panel never offered.
type StoryboardPanelVersion struct {
	ID                          string
	StoryboardItemID            string
	VersionNumber               int
	Status                      versioning.Status
	BasedOnVersionID            string
	VisualPrompt                string
	NegativePrompt              string
	ReferencePolicyJSON         string
	ApprovedImageAssetVersionID string
	SourceAgentRunID            string
	CreatedByType               versioning.CreatedByType
	CreatedByID                 string
	ChangeReason                string
	LegacyMetadata              string
	CreatedAt                   time.Time
}

// Validate checks a panel version before it is stored.
func (p StoryboardPanelVersion) Validate() error {
	if strings.TrimSpace(p.StoryboardItemID) == "" {
		return InvalidError("A panel version must belong to a storyboard item.")
	}
	if p.VersionNumber < 1 {
		return InvalidError("A panel version number starts at one.")
	}
	if !versioning.IsValidStatus(p.Status) {
		return InvalidError("The panel version status is not recognised.")
	}
	if !versioning.IsValidCreatedByType(p.CreatedByType) {
		return InvalidError("The panel version producer is not recognised.")
	}
	return nil
}

// CanApproveImage reports whether an asset version may become this panel's
// approved image.
//
// Section 9.5 requires the approved image to be one of "该 Panel 的候选" or one
// the user explicitly associated. The caller passes the panel's candidate
// versions, because candidacy is a property of the panel's job history rather
// than of the version row. A user associates an image the panel never generated
// by making it a candidate first; after that it satisfies the same check as any
// other candidate, so there is one route to approval rather than two.
func (p StoryboardPanelVersion) CanApproveImage(approvedVersionID string, candidateVersionIDs []string) error {
	if strings.TrimSpace(approvedVersionID) == "" {
		return InvalidError("An approved image must name an asset version.")
	}
	for _, candidate := range candidateVersionIDs {
		if candidate == approvedVersionID {
			return nil
		}
	}
	return ConflictError("The image is not one of this panel's candidate versions. Approve a candidate, or associate the image with the panel first.")
}
