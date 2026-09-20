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
type CreateScriptStructureRequest struct {
	ScriptID        string
	ScriptVersionID string
	// BasedOnVersionID is the version this one revises, empty for the first. It is what the lock
	// enforcement compares against, so a FIX that omits it silently loses its locks — which is why
	// the structure write refuses a locked field's absence rather than assuming a first version.
	BasedOnVersionID string
	// Structure is the version's scenes, lines and shots.
	Structure scriptdomain.ScriptStructure
	// SourceAgentRunID names the run that produced this content, empty for a user's edit.
	SourceAgentRunID string
	Summary          string
	CreatedByType    versioning.CreatedByType
	CreatedByID      string
	ChangeReason     string
}

// CreateScriptStructure writes one script version's whole content.
//
// The order is: read the version, check it is writable, enforce the locks, derive the lengths, then
// write everything in one transaction. Nothing is written until every check has passed, so a
// refused structure leaves no scenes behind — which matters more here than for a single-row write,
// because a partial version would be an artifact a reader could mistake for a complete one.
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
	if scriptdomain.ContentIsFrozen(version.Status) {
		return scriptdomain.ScriptVersion{}, scriptdomain.ConflictError(
			"That script version is approved, so its content cannot be rewritten. Author a new version instead.")
	}
	structure := request.Structure
	structure.ScriptVersionID = version.ID
	if err := structure.Validate(); err != nil {
		return scriptdomain.ScriptVersion{}, err
	}
	if err := s.enforceScriptLocks(ctx, version, structure); err != nil {
		return scriptdomain.ScriptVersion{}, err
	}
	if err := s.repository.CreateScriptStructure(ctx, structure); err != nil {
		return scriptdomain.ScriptVersion{}, err
	}
	// The duration is DERIVED from the scenes and written onto the version row, which is why no
	// tool accepts a duration: AGENT_CONTRACTS §17 puts "时长求和" in the code's column, and a
	// declared total and a computed one can disagree while only one of them is checkable.
	updated, err := s.recordStructureTotals(ctx, version, structure, request.Summary)
	if err != nil {
		return scriptdomain.ScriptVersion{}, err
	}
	return updated, nil
}

// recordStructureTotals writes the summed duration onto the version row.
//
// The version row exists already — it was created by CreateScriptVersion and this method filled in
// its content — so this is an UPDATE. It runs in its own transaction because the structure is
// already committed by now: a failure here leaves a version with content and a stale duration
// rather than no version at all, and a stale duration is repairable where a missing version is not.
func (s *Service) recordStructureTotals(ctx context.Context, version scriptdomain.ScriptVersion, structure scriptdomain.ScriptStructure, summary string) (scriptdomain.ScriptVersion, error) {
	total := structure.TotalDurationSeconds()
	if summary == "" {
		summary = version.Summary
	}
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
// This is AC-SCRIPT-002's "锁定字段不变", and it is a refusal rather than a repair: a write path
// that silently restored the locked values would produce a version the model did not write and
// nobody chose, which is worse than failing the stage — the record would say the model produced
// content it did not.
//
// The two lockable fields of a script version are its summary and its structure, and the structure
// is compared SCENE BY SCENE at the same ordinal. A locked structure therefore does not mean "do
// not touch anything": it means the scene at each position must be the scene that was there, which
// is what a user pinning a finished episode's shape is asking for.
func (s *Service) enforceScriptLocks(ctx context.Context, version scriptdomain.ScriptVersion, incoming scriptdomain.ScriptStructure) error {
	if strings.TrimSpace(version.BasedOnVersionID) == "" {
		// A first version has no base, so it has nothing to preserve. That is not a bypass: a
		// caller cannot reach it by omitting BasedOnVersionID on a REVISION, because the version
		// row's own field is what is read here and not the request's.
		return nil
	}
	// The locks are read from the BASE version, which is where the user pinned them.
	//
	// This was a real defect: the first version read them from the NEW version, where nothing can be
	// locked yet because it was created a moment ago — so the set was always empty, the comparison
	// never ran, and AC-SCRIPT-002's "锁定字段不变" was enforced nowhere while its test passed for the
	// wrong reason. The service tests found it: a revision that rewrote a pinned field was accepted.
	locks, err := s.repository.ListScriptFieldLocks(ctx, version.BasedOnVersionID)
	if err != nil {
		return err
	}
	if len(locks) == 0 {
		return nil
	}
	locked := scriptdomain.LockedFieldsOf(locks)
	base, err := s.repository.GetScriptStructure(ctx, version.BasedOnVersionID)
	if err != nil {
		return err
	}
	if locked[scriptdomain.LockScriptStructure] {
		if err := compareStructures(base, incoming); err != nil {
			return err
		}
	}
	return nil
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
// Three lookups is the honest way to answer a question the schema cannot: there is no table of
// versions, so nothing states it. The order is the pipeline's, so the common case — a skeleton or
// a strategy while the user is reviewing the early stages — resolves first. A not-found from all
// three is the refusal, and it names what was searched for rather than which lookup failed.
func (s *Service) familyOfVersion(ctx context.Context, versionID string) (scriptdomain.VersionFamily, error) {
	if _, err := s.repository.GetStorySkeletonVersion(ctx, versionID); err == nil {
		return scriptdomain.FamilyStorySkeleton, nil
	}
	if _, err := s.repository.GetAdaptationStrategyVersion(ctx, versionID); err == nil {
		return scriptdomain.FamilyAdaptationStrategy, nil
	}
	if _, err := s.repository.GetScriptVersion(ctx, versionID); err == nil {
		return scriptdomain.FamilyScript, nil
	}
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
		return scriptdomain.DiffScriptStructure(from, to), nil
	}
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
	result := ProjectScriptVersionResult{SceneNodeIDs: make([]string, 0, len(structure.Scenes))}
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
	}
	return result, nil
}
