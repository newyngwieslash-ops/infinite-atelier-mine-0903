package script

import "strings"

// diff.go compares two versions of one artifact. It is pure: two values in, a change set out,
// no clock and no I/O.
//
// WHY A DOMAIN FUNCTION AND NOT A QUERY. ROADMAP scope item 9 asks for a "Version Diff" and
// AC-SCRIPT-003 lists "版本 diff", while PRD FR-040 requires REDO to keep "比较信息". Nothing in
// the specification says where a diff is computed, and the answer follows from what a diff IS:
// it is a reading of two immutable artifacts, so it needs no database state beyond the two
// rows, and computing it in Go means it is testable as a table of inputs and outputs rather
// than as a pair of fixtures whose equality depends on insertion order.
//
// WHAT IT DOES NOT DO. It does not try to match a scene in one version to "the same" scene in
// another by content. Two versions of a script are two different documents, and an alignment
// heuristic would be a guess presented as a fact — worse than no alignment, because a reader
// would act on it. What it reports is what an ordinary text diff reports: which ORDINALS exist
// on each side, and for the ones that exist on both, which fields differ.

// ChangeKind is what happened to one item between two versions.
type ChangeKind string

const (
	// ChangeAdded is an item the newer version has and the older one did not.
	ChangeAdded ChangeKind = "added"
	// ChangeRemoved is an item the older version had and the newer one does not. REDO keeps
	// the old version precisely so this can be reported rather than lost.
	ChangeRemoved ChangeKind = "removed"
	// ChangeModified is an item both versions have at some position, with fields that differ.
	ChangeModified ChangeKind = "modified"
	// ChangeUnchanged is an item identical on both sides. It is reported rather than omitted
	// so a reader can tell "this survived the revision" from "nobody looked" — which is what
	// AC-SCRIPT-002's "原版本保留" and "锁定字段不变" are read against.
	ChangeUnchanged ChangeKind = "unchanged"
)

// FieldChange is one field-level difference between two versions of one item.
//
// The values travel, because a diff that named a field without showing what changed would send
// a reader to open both versions — which is the work the diff exists to save. These are the
// USER'S OWN artifacts and not a document's text, so unlike a validation violation there is
// nothing untrusted here to avoid echoing.
type FieldChange struct {
	Field  string `json:"field"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// ItemChange is what happened to one item.
type ItemChange struct {
	Kind ChangeKind `json:"kind"`
	// Ordinal is the item's position on the NEWER side when it exists there, and on the older
	// side when it was removed. A reader locating the change uses it with the version it
	// belongs to, which Kind says.
	Ordinal int `json:"ordinal"`
	// ID is the item's identifier on whichever side it exists, so a caller can open it.
	ID string `json:"id,omitempty"`
	// Fields are the differences, in the order the domain declares the fields. Empty for an
	// added or removed item: everything about it is new or gone.
	Fields []FieldChange `json:"fields,omitempty"`
	// Locked reports whether the item is protected from regeneration. A reader looking at a
	// diff after a FIX wants to see which parts the model was not allowed to touch, and which
	// of those it left alone anyway.
	Locked bool `json:"locked,omitempty"`
}

// VersionDiff is what changed between two versions of one artifact.
type VersionDiff struct {
	Family VersionFamily `json:"family"`
	FromID string        `json:"fromId"`
	ToID   string        `json:"toId"`
	// Fields are the artifact's own top-level differences: a skeleton's ending hook, a
	// strategy's mode. It is empty for a script version, whose content is its structure rather
	// than a field.
	Fields []FieldChange `json:"fields,omitempty"`
	// Items are the per-scene or per-line changes, in ordinal order.
	Items []ItemChange `json:"items,omitempty"`
	// Summary counts the changes, so a list view does not have to walk the items.
	Added     int `json:"added"`
	Removed   int `json:"removed"`
	Modified  int `json:"modified"`
	Unchanged int `json:"unchanged"`
}

// Changed reports whether anything differs at all.
//
// A revision that changed nothing is a real outcome — a FIX told to address an issue the model
// had already satisfied produces exactly that — and a caller showing "no changes" needs to be
// able to ask rather than infer it from four zeroes.
func (d VersionDiff) Changed() bool {
	return d.Added > 0 || d.Removed > 0 || d.Modified > 0 || len(d.Fields) > 0
}

// DiffSkeleton compares two story skeleton versions (DOMAIN_MODEL §7.4).
func DiffSkeleton(from, to StorySkeletonVersion) VersionDiff {
	diff := VersionDiff{Family: FamilyStorySkeleton, FromID: from.ID, ToID: to.ID}
	diff.Fields = appendFieldChange(diff.Fields, "openingHook", from.OpeningHook, to.OpeningHook)
	diff.Fields = appendFieldChange(diff.Fields, "coreConflict", from.CoreConflict, to.CoreConflict)
	diff.Fields = appendFieldChange(diff.Fields, "turningPoints", from.TurningPointsJSON, to.TurningPointsJSON)
	diff.Fields = appendFieldChange(diff.Fields, "climax", from.Climax, to.Climax)
	diff.Fields = appendFieldChange(diff.Fields, "endingHook", from.EndingHook, to.EndingHook)
	diff.Fields = appendFieldChange(diff.Fields, "estimatedDurationSeconds",
		itoa(from.EstimatedDurationSeconds), itoa(to.EstimatedDurationSeconds))
	return diff
}

// DiffStrategy compares two adaptation strategy versions (DOMAIN_MODEL §7.5).
func DiffStrategy(from, to AdaptationStrategyVersion) VersionDiff {
	diff := VersionDiff{Family: FamilyAdaptationStrategy, FromID: from.ID, ToID: to.ID}
	diff.Fields = appendFieldChange(diff.Fields, "strategySummary", from.StrategySummary, to.StrategySummary)
	diff.Fields = appendFieldChange(diff.Fields, "adaptationMode", string(from.AdaptationMode), string(to.AdaptationMode))
	diff.Fields = appendFieldChange(diff.Fields, "mergedEventGroups", from.MergedEventGroupsJSON, to.MergedEventGroupsJSON)
	diff.Fields = appendFieldChange(diff.Fields, "originalAdditions", from.OriginalAdditions, to.OriginalAdditions)
	diff.Fields = appendFieldChange(diff.Fields, "rationale", from.Rationale, to.Rationale)
	diff.Fields = appendFieldChange(diff.Fields, "risks", from.Risks, to.Risks)
	return diff
}

// DiffScriptStructure compares two whole script versions by SCENE ORDINAL.
//
// A scene present at an ordinal on both sides is compared field by field; one present only on
// the newer side is added and only on the older side is removed. A scene that MOVED — same
// content, different ordinal — reads as a removal and an addition, which is what a positional
// diff of any kind reports and is the honest answer for an artifact whose meaning is its
// order. The alternative would be content matching, which is the guess this file's comment
// refuses.
//
// Each scene's own diff carries its lines and its shots as nested items, so a reader sees the
// whole change in one place rather than three lists that must be joined by scene id.
func DiffScriptStructure(from, to ScriptStructure) VersionDiff {
	diff := VersionDiff{Family: FamilyScript, FromID: from.ScriptVersionID, ToID: to.ScriptVersionID}
	byOrdinal := make(map[int]SceneStructure, len(from.Scenes))
	for _, scene := range from.Scenes {
		byOrdinal[scene.Ordinal] = scene
	}
	seen := make(map[int]bool, len(to.Scenes))
	for _, after := range to.Scenes {
		seen[after.Ordinal] = true
		before, existed := byOrdinal[after.Ordinal]
		if !existed {
			diff.Items = append(diff.Items, ItemChange{
				Kind: ChangeAdded, Ordinal: after.Ordinal, ID: after.ID,
			})
			diff.Added++
			continue
		}
		change := ItemChange{Ordinal: after.Ordinal, ID: after.ID}
		change.Fields = sceneFieldChanges(before.Scene, after.Scene)
		change.Fields = append(change.Fields, nestedLineChanges(before, after)...)
		change.Fields = append(change.Fields, nestedShotChanges(before, after)...)
		if len(change.Fields) == 0 {
			change.Kind = ChangeUnchanged
			diff.Unchanged++
		} else {
			change.Kind = ChangeModified
			diff.Modified++
		}
		diff.Items = append(diff.Items, change)
	}
	// The removals are collected after the additions so the items read in ordinal order for a
	// reader walking the NEWER version, with what it lost at the end. A removal has no ordinal
	// on the newer side, so it cannot be interleaved meaningfully.
	for _, before := range from.Scenes {
		if seen[before.Ordinal] {
			continue
		}
		diff.Items = append(diff.Items, ItemChange{
			Kind: ChangeRemoved, Ordinal: before.Ordinal, ID: before.ID,
		})
		diff.Removed++
	}
	return diff
}

// sceneFieldChanges compares two scenes' own fields.
func sceneFieldChanges(from, to Scene) []FieldChange {
	var changes []FieldChange
	changes = appendFieldChange(changes, "slugline", from.Slugline, to.Slugline)
	changes = appendFieldChange(changes, "sceneNumber", from.SceneNumber, to.SceneNumber)
	changes = appendFieldChange(changes, "interiorExterior", string(from.InteriorExterior), string(to.InteriorExterior))
	changes = appendFieldChange(changes, "locationEntityId", from.LocationEntityID, to.LocationEntityID)
	changes = appendFieldChange(changes, "timeOfDay", from.TimeOfDay, to.TimeOfDay)
	changes = appendFieldChange(changes, "summary", from.Summary, to.Summary)
	changes = appendFieldChange(changes, "dramaticGoal", from.DramaticGoal, to.DramaticGoal)
	changes = appendFieldChange(changes, "estimatedDurationSeconds",
		itoa(from.EstimatedDurationSeconds), itoa(to.EstimatedDurationSeconds))
	changes = appendFieldChange(changes, "sourceStoryEventId", from.SourceStoryEventID, to.SourceStoryEventID)
	changes = appendFieldChange(changes, "isOriginalAdaptation",
		boolText(from.IsOriginalAdaptation), boolText(to.IsOriginalAdaptation))
	return changes
}

// nestedLineChanges compares two scenes' dialogue lines by ordinal.
//
// The line's LOCK is part of what is reported, because AC-SCRIPT-002 asks whether a locked
// field survived a revision: a reader seeing a modified line that was locked is looking at a
// bug, and this is the field that says so.
func nestedLineChanges(from, to SceneStructure) []FieldChange {
	var changes []FieldChange
	byOrdinal := make(map[int]DialogueLine, len(from.DialogueLines))
	for _, line := range from.DialogueLines {
		byOrdinal[line.Ordinal] = line
	}
	for _, after := range to.DialogueLines {
		before, existed := byOrdinal[after.Ordinal]
		if !existed {
			changes = append(changes, FieldChange{
				Field: "line." + itoa(after.Ordinal), Before: "", After: after.Text,
			})
			continue
		}
		var lineChanges []FieldChange
		lineChanges = appendFieldChange(lineChanges, "text", before.Text, after.Text)
		lineChanges = appendFieldChange(lineChanges, "characterEntityId", before.CharacterEntityID, after.CharacterEntityID)
		lineChanges = appendFieldChange(lineChanges, "type", string(before.Type), string(after.Type))
		lineChanges = appendFieldChange(lineChanges, "emotion", before.Emotion, after.Emotion)
		lineChanges = appendFieldChange(lineChanges, "performanceNote", before.PerformanceNote, after.PerformanceNote)
		lineChanges = appendFieldChange(lineChanges, "locked", boolText(before.Locked), boolText(after.Locked))
		for _, change := range lineChanges {
			changes = append(changes, FieldChange{
				Field:  "line." + itoa(after.Ordinal) + "." + change.Field,
				Before: change.Before, After: change.After,
			})
		}
		// The line is consumed, so what remains in the map at the end is what the newer
		// version no longer has.
		delete(byOrdinal, before.Ordinal)
	}
	for ordinal, line := range byOrdinal {
		changes = append(changes, FieldChange{
			Field: "line." + itoa(ordinal), Before: line.Text, After: "",
		})
	}
	return changes
}

// nestedShotChanges compares two scenes' shots by ordinal.
func nestedShotChanges(from, to SceneStructure) []FieldChange {
	var changes []FieldChange
	byOrdinal := make(map[int]Shot, len(from.Shots))
	for _, shot := range from.Shots {
		byOrdinal[shot.Ordinal] = shot
	}
	for _, after := range to.Shots {
		before, existed := byOrdinal[after.Ordinal]
		if !existed {
			changes = append(changes, FieldChange{
				Field: "shot." + itoa(after.Ordinal), Before: "", After: after.VisualDescription,
			})
			continue
		}
		var shotChanges []FieldChange
		shotChanges = appendFieldChange(shotChanges, "visualDescription", before.VisualDescription, after.VisualDescription)
		shotChanges = appendFieldChange(shotChanges, "actionDescription", before.ActionDescription, after.ActionDescription)
		shotChanges = appendFieldChange(shotChanges, "shotSize", before.ShotSize, after.ShotSize)
		shotChanges = appendFieldChange(shotChanges, "cameraAngle", before.CameraAngle, after.CameraAngle)
		shotChanges = appendFieldChange(shotChanges, "cameraMovement", before.CameraMovement, after.CameraMovement)
		shotChanges = appendFieldChange(shotChanges, "audioIntent", before.AudioIntent, after.AudioIntent)
		shotChanges = appendFieldChange(shotChanges, "continuityNotes", before.ContinuityNotes, after.ContinuityNotes)
		for _, change := range shotChanges {
			changes = append(changes, FieldChange{
				Field:  "shot." + itoa(after.Ordinal) + "." + change.Field,
				Before: change.Before, After: change.After,
			})
		}
		delete(byOrdinal, before.Ordinal)
	}
	for ordinal, shot := range byOrdinal {
		changes = append(changes, FieldChange{
			Field: "shot." + itoa(ordinal), Before: shot.VisualDescription, After: "",
		})
	}
	return changes
}

// appendFieldChange records a difference when the two values differ.
//
// An equal pair produces nothing, which is what makes a diff a list of CHANGES rather than of
// fields: a reader asking "what did the revision touch" is not helped by thirty unchanged
// fields.
func appendFieldChange(changes []FieldChange, field, before, after string) []FieldChange {
	if before == after {
		return changes
	}
	return append(changes, FieldChange{Field: field, Before: before, After: after})
}

// boolText renders a flag the way the diff's other values are rendered.
func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

// itoa renders an integer without importing strconv for three call sites, so the reason is
// visible where it is used.
func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	if negative {
		return "-" + digits
	}
	return digits
}

// trimmedEqual compares two strings ignoring surrounding whitespace.
//
// It is exported for the write path's lock enforcement, which must compare a locked field's
// old and new values the same way the diff does: a model that returned the same sentence with
// a trailing newline has not changed the field, and refusing it would make a lock impossible
// to satisfy.
func trimmedEqual(left, right string) bool {
	return strings.TrimSpace(left) == strings.TrimSpace(right)
}

// LockedFieldsOf returns the fields of one version that are locked, given a lock set.
//
// The write path and the API both need this reading, and it is here rather than in either so
// they cannot disagree about what "locked" means.
func LockedFieldsOf(locks []FieldLock) map[LockableField]bool {
	locked := make(map[LockableField]bool, len(locks))
	for _, lock := range locks {
		locked[lock.Field] = true
	}
	return locked
}

// LocksEqual reports whether a locked field's value survived a revision.
//
// It is the enforcement point AC-SCRIPT-002 turns on, and it is a comparison rather than a
// list of fields so the caller cannot check five of six. Which values to compare is the
// caller's business — this file knows nothing about where a field's text is read from — but
// WHETHER two values are equal is a rule, and it is stated once here.
func LocksEqual(field LockableField, before, after string) bool {
	return trimmedEqual(before, after)
}
