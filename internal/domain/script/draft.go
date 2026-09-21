package script

// draft.go declares the shape a CALLER supplies when it writes a version's content, as opposed to
// the shape the database holds. The two differ by exactly the fields AGENT_CONTRACTS §17 puts in
// the code's column: identifiers, order and uniqueness.
//
// WHY THIS IS NOT `ScriptStructure` WITH EMPTY FIELDS. A model asked to write a script would
// otherwise invent scene ids and ordinal numbers, and a line whose `SceneID` is another invented
// string is a reference that validates as text and names nothing. Deriving both from the payload's
// own shape makes that impossible rather than discouraged: an id cannot be wrong because the model
// never states one, and an ordinal cannot have a gap because it never states that either.
//
// The nesting is what carries the relation the ordinal used to. A line is written into the scene it
// is nested under, so "which scene is this line in" is answered by the document's structure and not
// by a field that could disagree with it.
//
// Every type here has a materialised twin in script.go, and the twin is what gets validated and
// stored: a draft is checked by being built into one. That is deliberately one validator rather
// than two, because two would drift and the one that drifted would be the one nobody read.

// ScriptStructureDraft is one script version's content as a caller states it.
type ScriptStructureDraft struct {
	// Scenes are the version's scenes in play order. Their ordinals are their positions.
	Scenes []SceneDraft
}

// SceneDraft is one scene as a caller states it, with its lines and shots nested inside.
type SceneDraft struct {
	// SceneNumber is the number a slugline shows, such as "12A". Empty is legal: a draft that has
	// not been numbered is what a first pass produces.
	SceneNumber string
	Slugline    string
	// InteriorExterior is the marking; empty means OTHER, which is the schema's own default.
	InteriorExterior InteriorExterior
	// LocationEntityID names a story-graph entity. It is a reference rather than a copied name, so
	// a renamed location updates every scene that uses it.
	LocationEntityID string
	TimeOfDay        string
	Summary          string
	DramaticGoal     string
	// EstimatedDurationSeconds is this scene's own estimate, and the version's total is the sum of
	// these. That is why the total is not a field here: §17 puts "时长求和" in the code's column.
	EstimatedDurationSeconds int
	// SourceStoryEventID names the story event this scene dramatizes, empty for an invention.
	SourceStoryEventID string
	// IsOriginalAdaptation marks material the source does not contain, which is what AC-SCRIPT-003's
	// "原创改编标记" asks a reader to be able to see.
	IsOriginalAdaptation bool
	// DialogueLines are the scene's lines in order.
	DialogueLines []DialogueLineDraft
	// Shots are the scene's camera setups in order.
	Shots []ShotDraft
}

// DialogueLineDraft is one line as a caller states it.
//
// It has no `Locked` field, and that absence is the rule rather than an omission: a lock is a
// USER's statement about content they own, so a caller that could set one could also CLEAR one by
// writing `false`. The service carries a locked line's protection forward from the version being
// revised (see the write path), which means a model cannot release a pin even by trying.
type DialogueLineDraft struct {
	// Type is the line's kind; empty means dialogue, which is the schema's own default.
	Type LineType
	// CharacterEntityID is empty for a line no character speaks.
	CharacterEntityID  string
	Text               string
	Emotion            string
	PerformanceNote    string
	SourceStoryEventID string
}

// ShotDraft is one camera setup as a caller states it.
//
// It has no `Status` and no `ShotNumber` default: a shot written by a stage is a draft, and the
// number a storyboard shows is assigned when the board is built (§9.5 requires the two to
// correspond, and a number invented before there is a board is a number nothing can correspond to).
type ShotDraft struct {
	ShotNumber               string
	ShotSize                 string
	CameraAngle              string
	CameraMovement           string
	EstimatedDurationSeconds int
	VisualDescription        string
	ActionDescription        string
	AudioIntent              string
	ContinuityNotes          string
}

// SceneCount reports how many scenes a draft states, so a caller can refuse an over-large payload
// before minting anything for it.
func (d ScriptStructureDraft) SceneCount() int { return len(d.Scenes) }
