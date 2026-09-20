package script

import (
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// structure.go carries the whole-content payload of one script version, and the field-lock
// vocabulary that AC-SCRIPT-002's "锁定字段不变" is enforced with.
//
// WHY ONE PAYLOAD RATHER THAN THREE COMMANDS. A script version is an immutable artifact, and
// PRD FR-040's S3 is one stage producing one version. Writing a version's scenes, then its
// lines, then its shots across three commands would leave the version observably HALF
// WRITTEN between them — and "duration" is a sum over the scenes, so it could not be computed
// until the last one arrived. One payload in one transaction makes both properties true by
// construction: a version either exists whole or does not exist. ADR-0012 records the ruling
// and its cost (a very long episode's payload needs a different transport, which is out of
// scope).
//
// WHY LOCKS ARE A SEPARATE TABLE. AC-SCRIPT-002's scenario is a FIX on a STORY SKELETON whose
// ending hook is missing: the user pins the fields the model got right, the model rewrites
// only what the review named. So the lock must attach to a FIELD of a version, not to a line
// — and `dialogue_lines.locked` (§7.7's per-line flag) therefore covers only one part of the
// requirement. Three families of version share one lock table rather than three columns,
// because a lock is the same fact about all of them.

// ScriptStructure is one script version's content: its scenes, in order, each with its
// dialogue lines and its shots.
//
// It is the shape a single write carries and the shape a diff compares. The nesting mirrors
// the schema's ownership chain (version → scene → line, and version → scene → shot) rather
// than flattening into three lists, because a flat list of lines with a scene id would let a
// payload name a scene it does not contain — a document that validates field by field and
// describes nothing.
type ScriptStructure struct {
	// ScriptVersionID is the version this content belongs to.
	ScriptVersionID string
	// Scenes are the version's scenes in play order. Their ordinals must be 1..n with no gap
	// and no repeat: §7.6 makes the ordinal unique within the version, and a gap would mean
	// the version has a hole a reader cannot see.
	Scenes []SceneStructure
}

// SceneStructure is one scene with the lines and shots that belong to it.
type SceneStructure struct {
	Scene
	// DialogueLines are the scene's lines in order, ordinals 1..n.
	DialogueLines []DialogueLine
	// Shots are the scene's camera setups in order, ordinals 1..n.
	Shots []Shot
}

// MaxScenesPerVersion bounds one payload's scene count.
//
// The bound exists so a runaway model cannot ask the core to write an unbounded structure, and
// it is generous: an episode with more scenes than this is not a script, it is a document that
// arrived in the wrong shape. Over the bound the write is REFUSED rather than truncated, for
// the reason the tool layer refuses an over-budget call: a truncated script would look
// complete.
const MaxScenesPerVersion = 200

// MaxLinesPerScene bounds one scene's lines.
const MaxLinesPerScene = 500

// MaxShotsPerScene bounds one scene's shots.
const MaxShotsPerScene = 200

// TotalDurationSeconds sums the estimated durations of a version's scenes.
//
// It is computed from the SCENES rather than from the lines or the shots, because §7.6's scene
// estimate is the unit a script's duration is expressed in and a shot's estimate refines it
// rather than adding to it. AGENT_CONTRACTS §17 puts "时长求和" in the code's column and not
// the model's, which is why this is a function here and why no tool takes a duration field:
// a declared total and a computed one can disagree, and only one of them is checkable.
func (s ScriptStructure) TotalDurationSeconds() int {
	total := 0
	for _, scene := range s.Scenes {
		total += scene.EstimatedDurationSeconds
	}
	return total
}

// Validate checks the whole structure before anything is written.
//
// It returns the FIRST problem rather than a list, because the caller refuses the write
// entirely: a payload with two faults produces one refusal and the second is only worth
// reporting once the first is fixed. The checks are the ones the schema also enforces, plus
// the two relations a schema cannot express — the ordinals form a gapless sequence, and a
// line's scene id names the scene it is nested under.
func (s ScriptStructure) Validate() error {
	if strings.TrimSpace(s.ScriptVersionID) == "" {
		return InvalidError("A structure must belong to a script version.")
	}
	if len(s.Scenes) == 0 {
		return InvalidError("A script version must contain at least one scene.")
	}
	if len(s.Scenes) > MaxScenesPerVersion {
		return InvalidError("That script version has more scenes than this build can write at once.")
	}
	for index, scene := range s.Scenes {
		// The ordinal is checked against its POSITION rather than against the previous one,
		// because that is the property that matters: 1..n with no gap. Comparing neighbours
		// would accept 1,2,3,3 and refuse 1,3,2 without saying which rule either broke.
		if scene.Ordinal != index+1 {
			return InvalidError("Scene ordinals must run from one without a gap.")
		}
		if err := scene.Scene.Validate(); err != nil {
			return err
		}
		if err := validateLines(scene); err != nil {
			return err
		}
		if err := validateShots(scene); err != nil {
			return err
		}
	}
	return nil
}

// validateLines checks one scene's lines: their ordinals, their own validation, and that each
// names the scene it is nested under.
func validateLines(scene SceneStructure) error {
	if len(scene.DialogueLines) > MaxLinesPerScene {
		return InvalidError("That scene has more dialogue lines than this build can write at once.")
	}
	for index, line := range scene.DialogueLines {
		if line.Ordinal != index+1 {
			return InvalidError("Dialogue line ordinals must run from one without a gap.")
		}
		if err := line.Validate(); err != nil {
			return err
		}
		// The nesting is what makes a payload coherent: a line whose SceneID names a
		// different scene would be written into the scene the write path chose, and the
		// document would describe one thing while the database held another.
		if line.SceneID != scene.ID {
			return InvalidError("A dialogue line must name the scene it is nested under.")
		}
	}
	return nil
}

// validateShots checks one scene's shots the same way.
func validateShots(scene SceneStructure) error {
	if len(scene.Shots) > MaxShotsPerScene {
		return InvalidError("That scene has more shots than this build can write at once.")
	}
	for index, shot := range scene.Shots {
		if shot.Ordinal != index+1 {
			return InvalidError("Shot ordinals must run from one without a gap.")
		}
		if err := shot.Validate(); err != nil {
			return err
		}
		if shot.SceneID != scene.ID {
			return InvalidError("A shot must name the scene it is nested under.")
		}
	}
	return nil
}

// LockableField names a version field a user may pin (AC-SCRIPT-002's "锁定字段").
//
// The vocabulary is per FAMILY because the three artifacts have different fields, and a single
// flat list would let a caller lock a strategy field on a script version. Each constant is
// spelled as the JSON field an agent's output uses, so a lock recorded from an API call and a
// lock reported in a diff read the same.
type LockableField string

// The story skeleton's lockable fields, from DOMAIN_MODEL §7.4.
const (
	LockSkeletonOpeningHook    LockableField = "openingHook"
	LockSkeletonCoreConflict   LockableField = "coreConflict"
	LockSkeletonTurningPoints  LockableField = "turningPoints"
	LockSkeletonClimax         LockableField = "climax"
	LockSkeletonEndingHook     LockableField = "endingHook"
	LockSkeletonSelectedEvents LockableField = "selectedEventIds"
)

// The adaptation strategy's lockable fields, from DOMAIN_MODEL §7.5.
const (
	LockStrategySummary      LockableField = "strategySummary"
	LockStrategyMode         LockableField = "adaptationMode"
	LockStrategyMergedEvents LockableField = "mergedEventGroups"
	LockStrategyOriginalAdds LockableField = "originalAdditions"
	LockStrategyRationale    LockableField = "rationale"
	LockStrategyRisks        LockableField = "risks"
)

// The script version's lockable fields.
//
// A version's CONTENT is its structure, so there are only the two fields a version row itself
// carries that a user would pin. Pin a scene or a line by setting its own flag, which is what
// `dialogue_lines.locked` and the scene diff carry.
const (
	LockScriptSummary   LockableField = "summary"
	LockScriptStructure LockableField = "structure"
)

// VersionFamily names which artifact a version belongs to.
//
// It is the discriminator the lock table and the diff dispatcher both need, and it is a
// domain value rather than a string typed at each call site because the two must agree about
// the same three names.
type VersionFamily string

const (
	FamilyStorySkeleton      VersionFamily = "story_skeleton"
	FamilyAdaptationStrategy VersionFamily = "adaptation_strategy"
	FamilyScript             VersionFamily = "script"
)

// VersionFamilies lists the three families in pipeline order.
func VersionFamilies() []VersionFamily {
	return []VersionFamily{FamilyStorySkeleton, FamilyAdaptationStrategy, FamilyScript}
}

// IsValidVersionFamily reports whether a family name is recognised.
func IsValidVersionFamily(value VersionFamily) bool {
	for _, candidate := range VersionFamilies() {
		if candidate == value {
			return true
		}
	}
	return false
}

// LockableFields returns the fields one family's versions may lock.
//
// It is the closed set the API validates against, so a caller cannot record a lock on a field
// no diff would ever report — a lock nothing reads is worse than no lock, because it reads as
// a protection that is not in force.
func LockableFields(family VersionFamily) []LockableField {
	switch family {
	case FamilyStorySkeleton:
		return []LockableField{
			LockSkeletonOpeningHook, LockSkeletonCoreConflict, LockSkeletonTurningPoints,
			LockSkeletonClimax, LockSkeletonEndingHook, LockSkeletonSelectedEvents,
		}
	case FamilyAdaptationStrategy:
		return []LockableField{
			LockStrategySummary, LockStrategyMode, LockStrategyMergedEvents,
			LockStrategyOriginalAdds, LockStrategyRationale, LockStrategyRisks,
		}
	case FamilyScript:
		return []LockableField{LockScriptSummary, LockScriptStructure}
	default:
		return nil
	}
}

// IsLockableField reports whether one field may be locked on one family's version.
func IsLockableField(family VersionFamily, field LockableField) bool {
	for _, candidate := range LockableFields(family) {
		if candidate == field {
			return true
		}
	}
	return false
}

// FieldLock is one user's pin on one field of one version.
//
// It hangs off the VERSION rather than off the artifact's identity, because a new version is a
// new row: a lock on skeleton v1 says nothing about v2 unless the write path carries it
// forward, and carrying it forward is deliberately the model's job rather than an automatic
// inheritance. That is what AC-SCRIPT-002 tests: a FIX produces a NEW version, and the locked
// fields of that new version must equal the old one's BECAUSE the model was told to keep them
// and the write path refused anything else.
type FieldLock struct {
	VersionID string
	// Family is which artifact the version belongs to, denormalized so a lock can be read
	// without a second lookup and so the field vocabulary can be validated on write.
	Family    VersionFamily
	Field     LockableField
	LockedBy  string
	CreatedAt string
}

// Validate checks a lock before it is stored.
func (l FieldLock) Validate() error {
	if strings.TrimSpace(l.VersionID) == "" {
		return InvalidError("A lock must name the version it protects.")
	}
	if !IsValidVersionFamily(l.Family) {
		return InvalidError("The version family is not recognised.")
	}
	if !IsLockableField(l.Family, l.Field) {
		return InvalidError("That field cannot be locked on this version family.")
	}
	return nil
}

// ContentIsFrozen reports whether a version's status forbids rewriting its content.
//
// It is `versioning.IsContentFrozen` under this package's name, and the indirection is
// deliberate: the rule is §2.5's and lives in that package, while the CALLER that must obey it
// is this aggregate's write path. WP-05 wrote the predicate and recorded that no command
// called it yet ("WP-08 introduces the script-edit path that will call it"); this is where it
// is called.
func ContentIsFrozen(status versioning.Status) bool {
	return versioning.IsContentFrozen(status)
}
