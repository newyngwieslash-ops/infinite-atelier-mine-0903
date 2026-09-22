package script

import (
	"context"
	"strings"
	"time"

	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// structure.go is the service's WP-08 surface: writing a whole script version, enforcing the locks
// a user pinned, and the reads a Script UI and a diff need.
//
// THE LOCK ENFORCEMENT IS THE POINT OF THIS FILE. AC-SCRIPT-002 asks for "锁定字段不变" and lists
// it among the acceptance criteria, so it cannot be a request in a prompt that a model may
// ignore. It is enforced HERE, at the write path, by comparing the incoming version's locked
// fields with the version it is based on and refusing the write when they differ. A model that
// rewrote a locked field fails the stage; a model that respected it succeeds. Nothing about the
// enforcement depends on the model having understood anything.
//
// WHERE THE VALUES COME FROM. The comparison needs the old values, and it reads them from the
// version named by `BasedOnVersionID` rather than from "the current version" or "the approved
// one": a FIX is explicitly a revision OF something, and if the caller did not say what, there is
// nothing to preserve and no locks to enforce. That is also why a first version has no lock
// checks — a version with no predecessor has no field a user could have pinned.

// CreateScriptStructureRequest asks for a whole script version's content.
//
// It carries EITHER a materialised `Structure` (identifiers and ordinals already assigned, which a
// user's edit and the existing tests supply) OR a `Draft` (no identifiers and no ordinals, which a
// model supplies). Exactly one must be present, and stating both is refused rather than merged:
// two payloads for one version would leave the question of which one won to a rule nobody wrote
// down. `draft.go` records why a model is never asked for an id or an ordinal.
type CreateScriptStructureRequest struct {
	ScriptID        string
	ScriptVersionID string
	// BasedOnVersionID is the version this one revises, empty for the first. The lock enforcement
	// reads the BASE from the version ROW rather than from this field, so a revision cannot lose its
	// locks by omitting it here; this field exists for callers that want to name their intent, and
	// for the draft path's relation check.
	BasedOnVersionID string
	// Structure is the version's scenes, lines and shots, already materialised.
	Structure scriptdomain.ScriptStructure
	// Draft is the version's content as a caller states it, with identifiers and ordinals left to
	// the code that writes it (AGENT_CONTRACTS §17).
	Draft scriptdomain.ScriptStructureDraft
	// ProjectID scopes the reference checks: a scene naming a story event or entity can only be
	// checked against a project, and this is the only place the project reaches this method.
	ProjectID string
	// SourceAgentRunID names the run that produced this content, empty for a user's edit.
	SourceAgentRunID string
	Summary          string
	CreatedByType    versioning.CreatedByType
	CreatedByID      string
	ChangeReason     string
}

// CreateScriptStructure writes one script version's whole content.
//
// The order is: read the version, check it is writable, build the payload, validate it, check every
// reference it names, enforce the locks, write, then derive the duration. Nothing is written until
// every check has passed, so a refused structure leaves no scenes behind — which matters more here
// than for a single-row write, because a partial version would be an artifact a reader could
// mistake for a complete one.
func (s *Service) CreateScriptStructure(ctx context.Context, request CreateScriptStructureRequest) (scriptdomain.ScriptVersion, error) {
	if !s.Available() {
		return scriptdomain.ScriptVersion{}, storageFailure()
	}
	version, err := s.repository.GetScriptVersion(ctx, request.ScriptVersionID)
	if err != nil {
		return scriptdomain.ScriptVersion{}, err
	}
	// A version whose content is frozen may not be given content. §2.5 freezes an approved or
	// superseded version's content, and WP-05 wrote the predicate for this package to call.
	if err := assertWritable(version.Status); err != nil {
		return scriptdomain.ScriptVersion{}, err
	}
	structure, err := s.materialiseStructure(ctx, request, version)
	if err != nil {
		return scriptdomain.ScriptVersion{}, err
	}
	if err := structure.Validate(); err != nil {
		return scriptdomain.ScriptVersion{}, err
	}
	// The pinned lines are carried forward HERE rather than inside one of the two payload paths, so
	// the guarantee holds whichever way the content arrived. It was inside the draft builder at
	// first, which made it path-dependent: a revision written through the materialised path — a
	// user's edit today, and any future caller that builds its own structure — would have dropped
	// every pin while the draft path kept them. The test that caught it was the one asserting the
	// pin survives a revision, run through the materialised path.
	if err := s.carryDialogueLocks(ctx, version.BasedOnVersionID, &structure); err != nil {
		return scriptdomain.ScriptVersion{}, err
	}
	// The summary this write will LEAVE on the row, resolved before anything is checked.
	//
	// A revision that states none INHERITS its base's, and the alternative was a real defect: an
	// empty request summary fell back to the revision's OWN row — which a version created a moment ago
	// leaves empty — so a locked summary was compared against "" and refused. That made the lock
	// unsatisfiable for exactly the revision AC-SCRIPT-002 describes, where the model rewrites a
	// structure and has no reason to restate a summary it is not allowed to change.
	//
	// Inheriting is the reading a user's pin implies: "this stays" means the revision carries it,
	// whether or not the payload mentions it. It is also what makes the empty case safe — the value
	// compared is the value that will be stored, so the check cannot pass on one reading and refuse on
	// another.
	summary, err := s.resolveSummary(ctx, version, request.Summary)
	if err != nil {
		return scriptdomain.ScriptVersion{}, err
	}
	if err := s.assertStructureReferences(ctx, request.ProjectID, structure); err != nil {
		return scriptdomain.ScriptVersion{}, err
	}
	if err := s.enforceScriptLocks(ctx, version, structure, summary); err != nil {
		return scriptdomain.ScriptVersion{}, err
	}
	if err := s.repository.CreateScriptStructure(ctx, structure); err != nil {
		return scriptdomain.ScriptVersion{}, err
	}
	// The duration is DERIVED from the scenes and written onto the version row, which is why no
	// tool accepts a duration: AGENT_CONTRACTS §17 puts "时长求和" in the code's column, and a
	// declared total and a computed one can disagree while only one of them is checkable.
	//
	// The summary passed is the one `resolveSummary` chose, so the value written is the value the lock
	// comparison saw. `recordStructureTotals` no longer re-decides it, because two places deciding the
	// same fallback is how a write ends up storing something the check never looked at.
	updated, err := s.recordStructureTotals(ctx, version, structure, summary)
	if err != nil {
		return scriptdomain.ScriptVersion{}, err
	}
	return updated, nil
}

// resolveSummary decides what summary the written version will carry.
//
// The rule is: what the caller stated, or — on a revision — what the version it revises carries. A
// first version with no summary stays empty, which is legal: a draft's summary may not be written
// yet, and §7.5 makes the column a default-empty TEXT rather than a requirement.
func (s *Service) resolveSummary(ctx context.Context, version scriptdomain.ScriptVersion, stated string) (string, error) {
	if strings.TrimSpace(stated) != "" {
		return stated, nil
	}
	base := strings.TrimSpace(version.BasedOnVersionID)
	if base == "" {
		return version.Summary, nil
	}
	inherited, err := s.repository.GetScriptVersion(ctx, base)
	if err != nil {
		return "", err
	}
	return inherited.Summary, nil
}

// materialiseStructure turns the request into the payload the database will hold.
//
// A materialised `Structure` is taken as given — its identifiers are the caller's, which is what a
// user's edit of an existing version produces. A `Draft` is built here, and building it is the
// whole reason §17's "ID、顺序和唯一性" is code's business: every identifier is minted, every
// ordinal is a position, and every child's scene reference is the scene it was nested under.
func (s *Service) materialiseStructure(ctx context.Context, request CreateScriptStructureRequest, version scriptdomain.ScriptVersion) (scriptdomain.ScriptStructure, error) {
	// The two emptiness cases are handled elsewhere and deliberately NOT here. "Both stated" is a
	// contradiction only this method can see, so it is refused here. "Neither stated" produces an
	// empty structure, which `ScriptStructure.Validate` already refuses with its own message — and
	// checking it again would be a second rule stating the same thing, in a place a later change could
	// make disagree with the first.
	hasStructure := len(request.Structure.Scenes) > 0
	hasDraft := len(request.Draft.Scenes) > 0
	switch {
	case hasStructure && hasDraft:
		return scriptdomain.ScriptStructure{}, scriptdomain.InvalidError(
			"State the script version's content once: either a structure or a draft, not both.")
	case hasStructure:
		structure := request.Structure
		structure.ScriptVersionID = version.ID
		return structure, nil
	default:
		built, err := s.buildDraft(ctx, version, request.Draft)
		if err != nil {
			return scriptdomain.ScriptStructure{}, err
		}
		return built, nil
	}
}

// buildDraft assigns the identifiers and ordinals a draft leaves out.
//
// The ordinals come from the ARRAY POSITIONS, so a gap is not expressible: a caller that meant to
// skip a scene cannot, because skipping one would only make the next scene take its place. That is
// the property `ScriptStructure.Validate` also checks, and it holds here by construction — the
// validator is what makes it true for the materialised path, where a caller COULD state a gap.
//
// The identifiers are minted per child, and a failure part-way leaves the minted ids unused rather
// than half a version: nothing has been written yet, because this runs before the write.
func (s *Service) buildDraft(ctx context.Context, version scriptdomain.ScriptVersion, draft scriptdomain.ScriptStructureDraft) (scriptdomain.ScriptStructure, error) {
	now := s.now()
	structure := scriptdomain.ScriptStructure{
		ScriptVersionID: version.ID,
		Scenes:          make([]scriptdomain.SceneStructure, 0, len(draft.Scenes)),
	}
	for sceneIndex, sceneDraft := range draft.Scenes {
		sceneID, err := s.ids.New()
		if err != nil {
			return scriptdomain.ScriptStructure{}, storageFailure()
		}
		interior := sceneDraft.InteriorExterior
		if interior == "" {
			interior = scriptdomain.InteriorOTHER
		}
		entry := scriptdomain.SceneStructure{
			Scene: scriptdomain.Scene{
				ID:              sceneID,
				ScriptVersionID: version.ID,
				// The position, not a stated number: §17 gives order to the code, and a payload that
				// could state both could state two different ones.
				Ordinal:                  sceneIndex + 1,
				SceneNumber:              strings.TrimSpace(sceneDraft.SceneNumber),
				Slugline:                 sceneDraft.Slugline,
				InteriorExterior:         interior,
				LocationEntityID:         strings.TrimSpace(sceneDraft.LocationEntityID),
				TimeOfDay:                sceneDraft.TimeOfDay,
				Summary:                  sceneDraft.Summary,
				DramaticGoal:             sceneDraft.DramaticGoal,
				EstimatedDurationSeconds: sceneDraft.EstimatedDurationSeconds,
				SourceStoryEventID:       strings.TrimSpace(sceneDraft.SourceStoryEventID),
				IsOriginalAdaptation:     sceneDraft.IsOriginalAdaptation,
				CreatedAt:                now,
				UpdatedAt:                now,
				Revision:                 1,
			},
			DialogueLines: make([]scriptdomain.DialogueLine, 0, len(sceneDraft.DialogueLines)),
			Shots:         make([]scriptdomain.Shot, 0, len(sceneDraft.Shots)),
		}
		for lineIndex, lineDraft := range sceneDraft.DialogueLines {
			lineID, err := s.ids.New()
			if err != nil {
				return scriptdomain.ScriptStructure{}, storageFailure()
			}
			lineType := lineDraft.Type
			if lineType == "" {
				lineType = scriptdomain.LineDialogue
			}
			entry.DialogueLines = append(entry.DialogueLines, scriptdomain.DialogueLine{
				ID: lineID,
				// The scene this line was NESTED under, which is the relation the nesting states. A
				// flat list with a scene id per line could name a scene the payload does not contain.
				SceneID:            sceneID,
				Ordinal:            lineIndex + 1,
				Type:               lineType,
				CharacterEntityID:  strings.TrimSpace(lineDraft.CharacterEntityID),
				Text:               lineDraft.Text,
				Emotion:            lineDraft.Emotion,
				PerformanceNote:    lineDraft.PerformanceNote,
				SourceStoryEventID: strings.TrimSpace(lineDraft.SourceStoryEventID),
				// A line a draft states is not locked. The lock is carried forward from the version
				// being revised, below — a caller cannot release a pin by writing `false`, because a
				// draft has no lock field to write.
				Locked:    false,
				CreatedAt: now,
				UpdatedAt: now,
				Revision:  1,
			})
		}
		for shotIndex, shotDraft := range sceneDraft.Shots {
			shotID, err := s.ids.New()
			if err != nil {
				return scriptdomain.ScriptStructure{}, storageFailure()
			}
			entry.Shots = append(entry.Shots, scriptdomain.Shot{
				ID:                       shotID,
				SceneID:                  sceneID,
				Ordinal:                  shotIndex + 1,
				ShotNumber:               strings.TrimSpace(shotDraft.ShotNumber),
				ShotSize:                 shotDraft.ShotSize,
				CameraAngle:              shotDraft.CameraAngle,
				CameraMovement:           shotDraft.CameraMovement,
				EstimatedDurationSeconds: shotDraft.EstimatedDurationSeconds,
				VisualDescription:        shotDraft.VisualDescription,
				ActionDescription:        shotDraft.ActionDescription,
				AudioIntent:              shotDraft.AudioIntent,
				ContinuityNotes:          shotDraft.ContinuityNotes,
				// A shot written by a stage is a draft. Its number and its refinement belong to the
				// storyboard stage (§9.5), so a stage cannot claim a shot is past review.
				Status:    versioning.StatusDraft,
				CreatedAt: now,
				UpdatedAt: now,
				Revision:  1,
			})
		}
		structure.Scenes = append(structure.Scenes, entry)
	}
	return structure, nil
}

// carryDialogueLocks copies the `locked` flags of a base version's lines onto a revised version's.
//
// The match is POSITIONAL — scene ordinal, then line ordinal — because that is the only relation
// two versions of a script are guaranteed to share: there is no line identity across versions, and
// content matching would be a guess presented as a fact. A revision that reordered its scenes
// therefore moves the pins with the positions rather than with the lines, which is the same
// reading `DiffScriptStructure` reports.
//
// A base version with no scenes is not an error: it is a first draft, or a version whose content
// was never written, and there are no flags to carry either way.
func (s *Service) carryDialogueLocks(ctx context.Context, baseVersionID string, structure *scriptdomain.ScriptStructure) error {
	base := strings.TrimSpace(baseVersionID)
	if base == "" {
		return nil
	}
	baseStructure, err := s.repository.GetScriptStructure(ctx, base)
	if err != nil {
		return err
	}
	if len(baseStructure.Scenes) == 0 {
		return nil
	}
	for sceneIndex := range structure.Scenes {
		if sceneIndex >= len(baseStructure.Scenes) {
			break
		}
		baseScene := baseStructure.Scenes[sceneIndex]
		lines := structure.Scenes[sceneIndex].DialogueLines
		for lineIndex := range lines {
			if lineIndex >= len(baseScene.DialogueLines) {
				break
			}
			lines[lineIndex].Locked = baseScene.DialogueLines[lineIndex].Locked
		}
	}
	return nil
}

// recordStructureTotals writes the summed duration and the resolved summary onto the version row.
//
// The version row exists already — it was created by CreateScriptVersion and this method fills in its
// content — so this is an UPDATE. It runs in its own transaction because the structure is already
// committed by now: a failure here leaves a version with content and a stale duration rather than no
// version at all, and a stale duration is repairable where a missing version is not.
//
// `summary` has ALREADY been resolved by `resolveSummary` and compared by the lock enforcement, so
// this method does not re-decide it. The first version did — it fell back to the version's own value
// on an empty string — and that second fallback is a defect waiting to happen: the value the lock
// check read and the value stored would be decided in two places, and the day they disagreed the
// write would store something nobody checked.
func (s *Service) recordStructureTotals(ctx context.Context, version scriptdomain.ScriptVersion, structure scriptdomain.ScriptStructure, summary string) (scriptdomain.ScriptVersion, error) {
	total := structure.TotalDurationSeconds()
	if version.EstimatedDurationSeconds == total && version.Summary == summary {
		return version, nil
	}
	if err := s.repository.SetScriptVersionTotals(ctx, version.ID, total, summary); err != nil {
		return scriptdomain.ScriptVersion{}, err
	}
	version.EstimatedDurationSeconds = total
	version.Summary = summary
	return version, nil
}

// enforceScriptLocks refuses a structure that changed a locked field of its base version.
//
// The two lockable fields of a script version are its summary and its structure, and they are
// checked by two comparisons because they are two KINDS of field: the summary is a string, and the
// structure is a list of scenes compared scene by scene at the same ordinal. A locked structure
// therefore does not mean "do not touch anything" — it means the scene at each position must be the
// scene that was there, which is what a user pinning a finished episode's shape is asking for.
//
// `assertLocksCovered` runs on the whole lock set before either comparison, so a lock on a field
// neither covers refuses the write rather than passing unchecked. That is the failure mode a split
// comparison invites, and it is the one AC-SCRIPT-002 cannot afford: a lock that reads as a
// protection and enforces nothing.
func (s *Service) enforceScriptLocks(ctx context.Context, version scriptdomain.ScriptVersion, incoming scriptdomain.ScriptStructure, incomingSummary string) error {
	locks, err := s.readLocks(ctx, version.BasedOnVersionID)
	if err != nil {
		return err
	}
	if len(locks) == 0 {
		return nil
	}
	// The coverage list is the UNION of the two comparisons below: the string field the generic
	// comparison reads, and the structure this family compares separately. It is written as one list
	// rather than derived, because deriving it would make the check agree with whatever the
	// comparison happened to cover — which is the tautology it exists to break.
	if err := assertLocksCovered(locks, scriptdomain.FamilyScript, []scriptdomain.LockableField{
		scriptdomain.LockScriptSummary,
		scriptdomain.LockScriptStructure,
	}); err != nil {
		return err
	}
	// The base structure and the base version are read once here. Both comparisons need a base, and
	// reading them once means the two sides of the check cannot come from two different moments.
	baseStructure, err := s.repository.GetScriptStructure(ctx, version.BasedOnVersionID)
	if err != nil {
		return err
	}
	base, err := s.repository.GetScriptVersion(ctx, version.BasedOnVersionID)
	if err != nil {
		return err
	}
	// The string-valued lock is compared by the generic loop, and the structure lock — whose two
	// sides are whole documents rather than two strings — is compared by `compareStructures` below.
	// The partition is by FIELD and it is explicit: the coverage list above is what makes "every lock
	// has a comparison" a checked property, and these two comparisons are what it checks against.
	//
	// `compareFieldSets` receives ALL the locks and skips the structure one by NOT CARRYING it in
	// the maps: a field absent from a map is a refusal there, so the structure lock must be filtered
	// out of the list rather than left in it. Filtering is the safe direction — a filter that removed
	// too much would then refuse the write, where a filter that removed too little would be caught by
	// the absent-from-map check.
	stringLocks := make([]scriptdomain.FieldLock, 0, len(locks))
	for _, lock := range locks {
		if lock.Field == scriptdomain.LockScriptSummary {
			stringLocks = append(stringLocks, lock)
		}
	}
	if err := compareFieldSets(stringLocks, scriptdomain.FamilyScript,
		fieldValues{scriptdomain.LockScriptSummary: base.Summary},
		fieldValues{scriptdomain.LockScriptSummary: incomingSummary},
	); err != nil {
		return err
	}
	// The structure lock is not one of the string fields the loop above reads, so it is compared
	// here. `covered` names it, which is what makes "there is a comparison for every lock" a
	// property rather than a hope.
	if hasLock(locks, scriptdomain.LockScriptStructure) {
		if err := compareStructures(baseStructure, incoming); err != nil {
			return err
		}
	}
	return nil
}

// hasLock reports whether one field is in a lock set.
func hasLock(locks []scriptdomain.FieldLock, field scriptdomain.LockableField) bool {
	for _, lock := range locks {
		if lock.Field == field {
			return true
		}
	}
	return false
}

// compareStructures refuses a structure that differs from its base in any locked position.
//
// It compares only what a STRUCTURE lock covers — the scene at each ordinal and that scene's own
// fields. The lines and the shots are compared through their own `locked` flags rather than
// through this lock, because §7.7 puts a line's lock on the line: a user pinning one line of
// dialogue is asking for that line, not for the whole episode's shape.
func compareStructures(base, incoming scriptdomain.ScriptStructure) error {
	if len(base.Scenes) != len(incoming.Scenes) {
		return scriptdomain.ConflictError(
			"A locked structure must keep the same scenes: the revision changed how many there are.")
	}
	for index, before := range base.Scenes {
		after := incoming.Scenes[index]
		// The ordinals are equal by construction (both validated as 1..n and compared by
		// position), so what is compared is the scene's content.
		if !scriptdomain.LocksEqual(scriptdomain.LockScriptStructure, before.Slugline, after.Slugline) ||
			!scriptdomain.LocksEqual(scriptdomain.LockScriptStructure, before.Summary, after.Summary) ||
			!scriptdomain.LocksEqual(scriptdomain.LockScriptStructure, before.DramaticGoal, after.DramaticGoal) ||
			before.EstimatedDurationSeconds != after.EstimatedDurationSeconds {
			return scriptdomain.ConflictError(
				"A locked structure was changed: a scene the user pinned came back different.")
		}
	}
	return nil
}

// assertStructureReferences refuses a structure that cites story-graph rows which do not exist.
//
// AGENT_CONTRACTS §17 puts "引用存在性" in the code's column, AC-SCRIPT-003 asks for "source event
// 引用" as a formal relation, and AC-AGENT-003 makes a stage that reports a reference which does not
// exist FAIL rather than pass. The schema cannot do this — see StoryReferenceRepository — so it is
// done here, and it is done BEFORE the write so a refused structure leaves nothing behind.
//
// An empty project id skips the check rather than refusing the write, and that is not a hole: a
// caller that stated no project has no project-scoped references to check, because the only route
// that supplies them is the tool path, where the run's own project is always present. What the skip
// does not do is let a reference through unexamined while claiming it was checked.
func (s *Service) assertStructureReferences(ctx context.Context, projectID string, structure scriptdomain.ScriptStructure) error {
	project := strings.TrimSpace(projectID)
	if project == "" {
		return nil
	}
	eventIDs := make([]string, 0, len(structure.Scenes))
	entityIDs := make([]string, 0, len(structure.Scenes))
	seenEvent := map[string]bool{}
	seenEntity := map[string]bool{}
	addEvent := func(id string) {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" || seenEvent[trimmed] {
			return
		}
		seenEvent[trimmed] = true
		eventIDs = append(eventIDs, trimmed)
	}
	addEntity := func(id string) {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" || seenEntity[trimmed] {
			return
		}
		seenEntity[trimmed] = true
		entityIDs = append(entityIDs, trimmed)
	}
	for _, scene := range structure.Scenes {
		addEvent(scene.SourceStoryEventID)
		addEntity(scene.LocationEntityID)
		for _, line := range scene.DialogueLines {
			addEvent(line.SourceStoryEventID)
			addEntity(line.CharacterEntityID)
		}
	}
	if len(eventIDs) > 0 {
		if err := s.assertStoryEventsExist(ctx, project, eventIDs); err != nil {
			return err
		}
	}
	if len(entityIDs) > 0 {
		missing, err := s.repository.MissingStoryEntityIDs(ctx, project, entityIDs)
		if err != nil {
			return err
		}
		if len(missing) > 0 {
			return scriptdomain.InvalidError(
				"The script cites story entities this project does not have: " + strings.Join(missing, ", ") +
					". A scene's location and a line's speaker must be entities that exist.")
		}
	}
	return nil
}

// LockScriptFieldRequest asks to pin one field of one version.
type LockScriptFieldRequest struct {
	VersionID string
	Family    scriptdomain.VersionFamily
	Field     scriptdomain.LockableField
	LockedBy  string
}

// LockScriptField pins a field so a later FIX must return it unchanged.
//
// The version is read FIRST and its family checked against the request's, which is what keeps the
// lock table honest: it cannot be a foreign key across three tables, so the read is what confirms
// the version exists and the family is the right one for the field vocabulary. A caller that named
// the wrong family is refused rather than recording a lock no diff would report.
func (s *Service) LockScriptField(ctx context.Context, request LockScriptFieldRequest) error {
	if !s.Available() {
		return storageFailure()
	}
	versionID := strings.TrimSpace(request.VersionID)
	if versionID == "" {
		return scriptdomain.InvalidError("A lock must name the version it protects.")
	}
	family, err := s.familyOfVersion(ctx, versionID)
	if err != nil {
		return err
	}
	if request.Family != "" && request.Family != family {
		return scriptdomain.InvalidError("That version belongs to a different family than the lock names.")
	}
	lock := scriptdomain.FieldLock{
		VersionID: versionID,
		Family:    family,
		Field:     request.Field,
		LockedBy:  request.LockedBy,
		CreatedAt: s.now().UTC().Format(time.RFC3339),
	}
	if err := lock.Validate(); err != nil {
		return err
	}
	return s.repository.LockScriptField(ctx, lock)
}

// UnlockScriptField removes a pin.
func (s *Service) UnlockScriptField(ctx context.Context, versionID string, field scriptdomain.LockableField) error {
	if !s.Available() {
		return storageFailure()
	}
	if _, err := s.familyOfVersion(ctx, versionID); err != nil {
		return err
	}
	return s.repository.UnlockScriptField(ctx, versionID, field)
}

// ListScriptFieldLocks returns one version's pins.
func (s *Service) ListScriptFieldLocks(ctx context.Context, versionID string) ([]scriptdomain.FieldLock, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	return s.repository.ListScriptFieldLocks(ctx, versionID)
}

// familyOfVersion finds which family a version id belongs to, by asking each family in turn.
//
// Three lookups is the honest way to answer a question the schema cannot: there is no table of versions,
// so nothing states it. The order is the pipeline's, so the common case — a skeleton or a strategy while
// the user is reviewing the early stages — resolves first.
//
// A NOT-FOUND AND A STORAGE FAILURE ARE TOLD APART, which the first version of this function did not do:
// it treated every error as "not this family" and fell through to a not-found, so a database that could
// not be read produced "that version does not exist". The distinction matters because the two send a
// caller to different places — a stale identifier versus a broken store — and the second must not be
// reported as the first.
func (s *Service) familyOfVersion(ctx context.Context, versionID string) (scriptdomain.VersionFamily, error) {
	target := strings.TrimSpace(versionID)
	if target == "" {
		return "", scriptdomain.InvalidError("A version is required.")
	}
	for _, candidate := range []struct {
		family scriptdomain.VersionFamily
		lookup func(context.Context, string) error
	}{
		{scriptdomain.FamilyStorySkeleton, func(ctx context.Context, id string) error {
			_, err := s.repository.GetStorySkeletonVersion(ctx, id)
			return err
		}},
		{scriptdomain.FamilyAdaptationStrategy, func(ctx context.Context, id string) error {
			_, err := s.repository.GetAdaptationStrategyVersion(ctx, id)
			return err
		}},
		{scriptdomain.FamilyScript, func(ctx context.Context, id string) error {
			_, err := s.repository.GetScriptVersion(ctx, id)
			return err
		}},
	} {
		err := candidate.lookup(ctx, target)
		if err == nil {
			return candidate.family, nil
		}
		// A NOT-FOUND is expected while searching, so the walk continues. Anything else is the store
		// failing, and continuing would turn it into "no version has that id" after two more lookups.
		if domainErr, ok := scriptdomain.AsError(err); ok && domainErr.Category == scriptdomain.CategoryNotFound {
			continue
		}
		return "", err
	}
	// The refusal does NOT echo the identifier, and that is the domain's own rule rather than an
	// oversight: `NotFoundError` reports the category with a fixed message because the id may be
	// attacker-controlled text and the caller already knows what it asked for. What this function owes a
	// caller is the DISTINCTION between "no version has that id" and "the store failed", and that is
	// what the loop above provides.
	return "", scriptdomain.NotFoundError()
}

// DiffVersions compares two versions of one artifact.
//
// Both versions are read whole — including a script version's content — because a diff of a script
// version that omitted the scenes would compare two summary strings and call it a revision. The
// comparison itself is a pure function in the domain, so this method is a read and a dispatch and
// nothing else.
func (s *Service) DiffVersions(ctx context.Context, family scriptdomain.VersionFamily, fromID, toID string) (scriptdomain.VersionDiff, error) {
	if !s.Available() {
		return scriptdomain.VersionDiff{}, storageFailure()
	}
	if !scriptdomain.IsValidVersionFamily(family) {
		return scriptdomain.VersionDiff{}, scriptdomain.InvalidError("The version family is not recognised.")
	}
	switch family {
	case scriptdomain.FamilyStorySkeleton:
		from, err := s.repository.GetStorySkeletonVersion(ctx, fromID)
		if err != nil {
			return scriptdomain.VersionDiff{}, err
		}
		to, err := s.repository.GetStorySkeletonVersion(ctx, toID)
		if err != nil {
			return scriptdomain.VersionDiff{}, err
		}
		return scriptdomain.DiffSkeleton(from, to), nil
	case scriptdomain.FamilyAdaptationStrategy:
		from, err := s.repository.GetAdaptationStrategyVersion(ctx, fromID)
		if err != nil {
			return scriptdomain.VersionDiff{}, err
		}
		to, err := s.repository.GetAdaptationStrategyVersion(ctx, toID)
		if err != nil {
			return scriptdomain.VersionDiff{}, err
		}
		return scriptdomain.DiffStrategy(from, to), nil
	default:
		from, err := s.repository.GetScriptStructure(ctx, fromID)
		if err != nil {
			return scriptdomain.VersionDiff{}, err
		}
		to, err := s.repository.GetScriptStructure(ctx, toID)
		if err != nil {
			return scriptdomain.VersionDiff{}, err
		}
		// The structure lock is read HERE and passed in, because the domain's diff is a pure function
		// and this is the layer that owns the repository. Without it every item's `Locked` would be
		// false, and a reader looking at a revision of a pinned version would see "nothing was pinned".
		structureLocked, err := s.hasStructureLock(ctx, fromID)
		if err != nil {
			return scriptdomain.VersionDiff{}, err
		}
		return scriptdomain.DiffScriptStructureLocked(from, to, structureLocked), nil
	}
}

// hasStructureLock reports whether a version's structure is pinned.
//
// A lock row is read from the version being COMPARED FROM, which is the one a revision was written
// against: the question a diff answers is "did the revision respect what was pinned", and what was
// pinned is a fact about the older side.
func (s *Service) hasStructureLock(ctx context.Context, versionID string) (bool, error) {
	locks, err := s.repository.ListScriptFieldLocks(ctx, versionID)
	if err != nil {
		return false, err
	}
	for _, lock := range locks {
		if lock.Field == scriptdomain.LockScriptStructure {
			return true, nil
		}
	}
	return false, nil
}

// ListVersions returns one episode's or script's whole version history.
//
// The family decides what the id means: an episode for the two upstream stages and a script for the
// third, which is the schema's own ownership chain rather than a choice made here. A caller with an
// episode id and a question about scripts calls EnsureScript first, which is what the UI does.
func (s *Service) ListVersions(ctx context.Context, family scriptdomain.VersionFamily, parentID string) (any, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	if strings.TrimSpace(parentID) == "" {
		return nil, scriptdomain.InvalidError("A version history needs the artifact it belongs to.")
	}
	switch family {
	case scriptdomain.FamilyStorySkeleton:
		return s.repository.ListStorySkeletonVersions(ctx, parentID)
	case scriptdomain.FamilyAdaptationStrategy:
		return s.repository.ListAdaptationStrategyVersions(ctx, parentID)
	case scriptdomain.FamilyScript:
		return s.repository.ListScriptVersions(ctx, parentID)
	default:
		return nil, scriptdomain.InvalidError("The version family is not recognised.")
	}
}

// ListSkeletonEventIDs returns the story events one skeleton version selected (§7.4's link set).
//
// It is a read the interface needs and the service did not expose: the link table could be WRITTEN through
// `CreateStorySkeletonVersion` and never read back through this layer, so a UI showing "which events does
// this episode contain" had no route to the answer. §7.4 makes the set a link table precisely so that
// question can be asked, and a writer with no reader is the shape AGENTS §12 refuses.
func (s *Service) ListSkeletonEventIDs(ctx context.Context, versionID string) ([]string, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	trimmed := strings.TrimSpace(versionID)
	if trimmed == "" {
		return nil, scriptdomain.InvalidError("A version is required.")
	}
	return s.repository.ListSkeletonEventIDs(ctx, trimmed)
}

// ListStrategyEventLinks returns one strategy version's per-event treatments (§7.5's link set).
func (s *Service) ListStrategyEventLinks(ctx context.Context, versionID string) ([]scriptdomain.StrategyEventLink, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	trimmed := strings.TrimSpace(versionID)
	if trimmed == "" {
		return nil, scriptdomain.InvalidError("A version is required.")
	}
	return s.repository.ListStrategyEventLinks(ctx, trimmed)
}

// GetScriptStructure returns one script version's whole content.
func (s *Service) GetScriptStructure(ctx context.Context, scriptVersionID string) (scriptdomain.ScriptStructure, error) {
	if !s.Available() {
		return scriptdomain.ScriptStructure{}, storageFailure()
	}
	if strings.TrimSpace(scriptVersionID) == "" {
		return scriptdomain.ScriptStructure{}, scriptdomain.InvalidError("A script version is required.")
	}
	return s.repository.GetScriptStructure(ctx, scriptVersionID)
}

// SetDialogueLineLockedRequest asks to pin or release one dialogue line.
type SetDialogueLineLockedRequest struct {
	LineID   string
	Locked   bool
	Revision int64
}

// SetDialogueLineLocked pins or releases a line.
//
// It is the per-line half of AC-SCRIPT-002's lock requirement, and it is a command of its own
// because a lock is the only thing a user changes about an existing line: a line's text is
// rewritten wholesale by a new version, which is what keeps a version immutable. A general update
// would have to decide what else it may change, and the answer for this build is nothing.
func (s *Service) SetDialogueLineLocked(ctx context.Context, request SetDialogueLineLockedRequest) (scriptdomain.DialogueLine, error) {
	if !s.Available() {
		return scriptdomain.DialogueLine{}, storageFailure()
	}
	lineID := strings.TrimSpace(request.LineID)
	if lineID == "" {
		return scriptdomain.DialogueLine{}, scriptdomain.InvalidError("A dialogue line is required.")
	}
	if err := s.repository.SetDialogueLineLocked(ctx, lineID, request.Locked, request.Revision, s.now()); err != nil {
		return scriptdomain.DialogueLine{}, err
	}
	return s.repository.GetDialogueLine(ctx, lineID)
}

// ProjectScriptVersionRequest asks to project a version's scenes onto a canvas.
type ProjectScriptVersionRequest struct {
	ProjectID       string
	ScriptVersionID string
}

// ProjectScriptVersionResult reports what the projection wrote.
type ProjectScriptVersionResult struct {
	// SceneNodeIDs are the canvas nodes written for the version's scenes, in ordinal order.
	SceneNodeIDs []string
	// ShotNodeIDs are the nodes written for the shots those scenes contain, in the script's
	// order. ROADMAP item 11 names Shot projection, and a board's rows cite shots — so a
	// canvas without them would show the scenes a row is inside and not the shot it is about.
	ShotNodeIDs []string
}

// CanvasProjector writes a projection node for one entity.
//
// It is a port rather than a direct call because the projection belongs to the project aggregate:
// this package knows a script version's scenes, and that package knows how a node is written and
// validated against the relation registry. The interface is one method because the projection needs
// one.
type CanvasProjector interface {
	// ProjectEntity writes (or re-labels) one entity's canvas node and returns the node's id.
	// It must be idempotent, which the project service's implementation is.
	ProjectEntity(ctx context.Context, projectID, entityType, entityID, label string) (string, error)
}

// ProjectScriptVersion projects a version's scenes onto the project's canvas.
//
// AC-SCRIPT-003 lists "Canvas projection" and PRD 7.3.7 asks for Scene and Shot drafts "投射到画布".
// The writer existed since WP-05 with tests and NO caller, which is the "interface with no real
// path" AGENTS §12 refuses; this is the caller. It is a command rather than an automatic side
// effect of writing a structure, because projecting is a fact about a project's canvas rather than
// about a version — and a version written by an agent should not silently rearrange a user's board.
//
// The projector is optional, and its absence is a REFUSAL rather than a silent no-op: a caller
// asking for a projection and getting success would have no way to tell it did not happen.
func (s *Service) ProjectScriptVersion(ctx context.Context, request ProjectScriptVersionRequest) (ProjectScriptVersionResult, error) {
	if !s.Available() {
		return ProjectScriptVersionResult{}, storageFailure()
	}
	if s.projector == nil {
		return ProjectScriptVersionResult{}, scriptdomain.InvalidError("This build cannot project onto a canvas.")
	}
	projectID := strings.TrimSpace(request.ProjectID)
	if projectID == "" {
		return ProjectScriptVersionResult{}, scriptdomain.InvalidError("A project is required.")
	}
	structure, err := s.repository.GetScriptStructure(ctx, request.ScriptVersionID)
	if err != nil {
		return ProjectScriptVersionResult{}, err
	}
	if len(structure.Scenes) == 0 {
		return ProjectScriptVersionResult{}, scriptdomain.InvalidError("That version has no scenes to project.")
	}
	result := ProjectScriptVersionResult{
		SceneNodeIDs: make([]string, 0, len(structure.Scenes)),
		ShotNodeIDs:  make([]string, 0, 16),
	}
	for _, scene := range structure.Scenes {
		label := strings.TrimSpace(scene.Slugline)
		if label == "" {
			label = strings.TrimSpace(scene.SceneNumber)
		}
		nodeID, err := s.projector.ProjectEntity(ctx, projectID, "scene", scene.ID, label)
		if err != nil {
			return ProjectScriptVersionResult{}, err
		}
		result.SceneNodeIDs = append(result.SceneNodeIDs, nodeID)
		// THE SHOTS ARE PROJECTED WITH THEIR SCENES, and that is ROADMAP item 11's "Shot
		// projection": a board's rows cite shots, and a user looking at the canvas has to be
		// able to see the shot a row is about. The projector is idempotent, so a re-run moves
		// the existing nodes rather than piling up duplicates — which is what the canvas
		// regression test asserts.
		for _, shot := range scene.Shots {
			shotLabel := strings.TrimSpace(shot.ShotNumber)
			if shotLabel == "" {
				shotLabel = strings.TrimSpace(shot.VisualDescription)
			}
			if shotLabel == "" {
				shotLabel = "shot"
			}
			shotNodeID, err := s.projector.ProjectEntity(ctx, projectID, "shot", shot.ID, shotLabel)
			if err != nil {
				return ProjectScriptVersionResult{}, err
			}
			result.ShotNodeIDs = append(result.ShotNodeIDs, shotNodeID)
		}
	}
	return result, nil
}
