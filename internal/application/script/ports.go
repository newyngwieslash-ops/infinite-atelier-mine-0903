// Package script is the application layer for episodes, the three script
// stages and the scene/dialogue/shot model of DOMAIN_MODEL §7. It owns the
// commands and queries and defines the persistence ports infrastructure
// implements. It performs no I/O itself.
package script

import (
	"context"
	"time"

	eventsapp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/events"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"

	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
)

// Clock abstracts time so tests are deterministic.
type Clock interface {
	Now() time.Time
}

// IDGenerator produces entity identifiers. ADR-0005 fixes the format as
// UUIDv7 and requires generation to happen here, in the application layer, so
// every repository receives an identifier it did not mint.
type IDGenerator interface {
	New() (string, error)
}

// EpisodeRepository persists episodes.
type EpisodeRepository interface {
	// CreateEpisode stores an episode. A duplicate (project, season, episode)
	// triple is a conflict, which the schema's unique constraint enforces too.
	CreateEpisode(ctx context.Context, record scriptdomain.Episode) error
	// GetEpisode returns one episode by id.
	GetEpisode(ctx context.Context, id string) (scriptdomain.Episode, error)
	// ListEpisodes returns a project's episodes in season and episode order.
	ListEpisodes(ctx context.Context, projectID string) ([]scriptdomain.Episode, error)
	// CountEpisodesAtPosition reports how many episodes already hold a business
	// key, so a caller can report the clash before the insert.
	CountEpisodesAtPosition(ctx context.Context, projectID string, seasonNumber, episodeNumber int) (int, error)
	// UpdateEpisode persists a change guarded by the expected revision.
	UpdateEpisode(ctx context.Context, record scriptdomain.Episode, expectedRevision int64) error
}

// SkeletonRepository persists story skeleton versions.
type SkeletonRepository interface {
	// CreateStorySkeletonVersion stores one version. A duplicate (episode,
	// version number) pair is a conflict.
	CreateStorySkeletonVersion(ctx context.Context, record scriptdomain.StorySkeletonVersion) error
	// CreateStorySkeletonVersionWithLinks stores one version AND the story events it selected,
	// in ONE transaction.
	//
	// It is a separate method rather than a call the service makes after the insert, because the
	// two are one artifact: DOMAIN_MODEL section 7.4 makes the selected events part of what a
	// skeleton IS, so a version whose links were never written would be a skeleton with no
	// selection — which a reviewer could accept without ever noticing. One transaction makes that
	// state unrepresentable, and it is the same argument the whole-version structure write rests on.
	//
	// A version with an empty selection is legal and writes no link rows: a skeleton that selects
	// nothing is a draft, and refusing it would make the first write of a stage fail for a reason
	// the model cannot act on.
	CreateStorySkeletonVersionWithLinks(ctx context.Context, record scriptdomain.StorySkeletonVersion, eventIDs []string) error
	// GetStorySkeletonVersion returns one version by id.
	GetStorySkeletonVersion(ctx context.Context, id string) (scriptdomain.StorySkeletonVersion, error)
	// MaxStorySkeletonVersionNumber reports the highest version number an
	// episode has, or zero when it has none.
	MaxStorySkeletonVersionNumber(ctx context.Context, episodeID string) (int, error)
	// CurrentApprovedSkeletonVersionID returns the episode's approved version, or
	// "" when none is approved.
	CurrentApprovedSkeletonVersionID(ctx context.Context, episodeID string) (string, error)
	// ApproveStorySkeletonVersion switches which version is approved, recording
	// the event in the same transaction so the approval and its audit land
	// together.
	ApproveStorySkeletonVersion(ctx context.Context, versionID, episodeID string, expectedStatus versioning.Status, record event.Event) error
}

// StrategyRepository persists adaptation strategy versions.
type StrategyRepository interface {
	// CreateAdaptationStrategyVersion stores one version. A duplicate (episode,
	// version number) pair is a conflict.
	CreateAdaptationStrategyVersion(ctx context.Context, record scriptdomain.AdaptationStrategyVersion) error
	// CreateAdaptationStrategyVersionWithLinks stores one version AND its per-event treatments in
	// ONE transaction, for the reason the skeleton's twin states: section 7.5 makes the retained,
	// removed and reordered sets part of what a strategy IS, so a version without them is a
	// strategy that decided nothing.
	CreateAdaptationStrategyVersionWithLinks(ctx context.Context, record scriptdomain.AdaptationStrategyVersion, links []scriptdomain.StrategyEventLink) error
	// GetAdaptationStrategyVersion returns one version by id.
	GetAdaptationStrategyVersion(ctx context.Context, id string) (scriptdomain.AdaptationStrategyVersion, error)
	// MaxAdaptationStrategyVersionNumber reports the highest version number an
	// episode has, or zero when it has none.
	MaxAdaptationStrategyVersionNumber(ctx context.Context, episodeID string) (int, error)
	// CurrentApprovedStrategyVersionID returns the episode's approved strategy
	// version, or "" when none is approved.
	CurrentApprovedStrategyVersionID(ctx context.Context, episodeID string) (string, error)
	// ApproveAdaptationStrategyVersion switches which version is approved,
	// recording the event in the same transaction.
	ApproveAdaptationStrategyVersion(ctx context.Context, versionID, episodeID string, expectedStatus versioning.Status, record event.Event) error
}

// ScriptRepository persists scripts and script versions.
type ScriptRepository interface {
	// CreateScript stores the stable script identity of an episode. A second
	// script for the same episode is a conflict, which the schema enforces with
	// a unique constraint on episode_id.
	CreateScript(ctx context.Context, record scriptdomain.Script) error
	// GetScript returns one script by id.
	GetScript(ctx context.Context, id string) (scriptdomain.Script, error)
	// GetScriptByEpisode returns the episode's script, or not-found when the
	// episode has none yet.
	GetScriptByEpisode(ctx context.Context, episodeID string) (scriptdomain.Script, error)

	// CreateScriptVersion stores one version. A duplicate (script, version
	// number) pair is a conflict.
	CreateScriptVersion(ctx context.Context, version scriptdomain.ScriptVersion) error
	// GetScriptVersion returns one version by id.
	GetScriptVersion(ctx context.Context, id string) (scriptdomain.ScriptVersion, error)
	// MaxScriptVersionNumber reports the highest version number a script has, or
	// zero when it has none.
	MaxScriptVersionNumber(ctx context.Context, scriptID string) (int, error)
	// SetScriptVersionTotals writes the summed duration and the summary onto a version row.
	//
	// It is a narrow UPDATE rather than a general one because those two are the only fields a
	// version's CONTENT changes after it is created: its scenes and lines are written once by the
	// structure write, and its status moves by approval. A general update would have to decide
	// what else it may change, and the answer for this build is nothing.
	SetScriptVersionTotals(ctx context.Context, versionID string, totalDurationSeconds int, summary string) error
	// CurrentApprovedScriptVersion returns the script's approved version. The
	// boolean is false rather than an error when none is approved yet, because
	// "no approval" is the ordinary state of a new script.
	CurrentApprovedScriptVersion(ctx context.Context, scriptID string) (scriptdomain.ScriptVersion, bool, error)
	// ApproveScriptVersion switches a script's approval in one transaction and
	// records the governance event with it: supersede the previous approval,
	// then approve the target, then write the section 17 event, or none of the
	// three. It is the same switch the other seven version families use, with
	// the same guarantee that an approval nobody can audit does not happen.
	ApproveScriptVersion(ctx context.Context, versionID, scriptID string, expectedStatus versioning.Status, record event.Event) error
}

// SceneRepository persists scenes of a script version.
type SceneRepository interface {
	// CreateScene stores a scene. A duplicate (version, ordinal) pair is a
	// conflict.
	CreateScene(ctx context.Context, record scriptdomain.Scene) error
	// GetScene returns one scene by id.
	GetScene(ctx context.Context, id string) (scriptdomain.Scene, error)
	// ListScenes returns a version's scenes in script order.
	ListScenes(ctx context.Context, scriptVersionID string) ([]scriptdomain.Scene, error)
	// CountScenesAtOrdinal reports how many scenes already hold a position in a
	// version.
	CountScenesAtOrdinal(ctx context.Context, scriptVersionID string, ordinal int) (int, error)
}

// ShotRepository persists shots of a scene.
type ShotRepository interface {
	// CreateShot stores a shot. A duplicate (scene, ordinal) pair is a conflict.
	CreateShot(ctx context.Context, record scriptdomain.Shot) error
	// GetShot returns one shot by id.
	GetShot(ctx context.Context, id string) (scriptdomain.Shot, error)
	// ListShots returns a scene's shots in order.
	ListShots(ctx context.Context, sceneID string) ([]scriptdomain.Shot, error)
	// CountShotsAtOrdinal reports how many shots already hold a position in a
	// scene.
	CountShotsAtOrdinal(ctx context.Context, sceneID string, ordinal int) (int, error)
}

// DialogueRepository persists dialogue lines of a scene.
//
// It was missing until WP-08: migration 000008 created `dialogue_lines`, the domain type existed
// with its validation, and NOTHING could write one. A script without its lines is not a script —
// FR-040's S3 output lists 对白 and 旁白 as first-class — so this port is what makes the third
// stage's artifact real rather than a version row with a summary.
type DialogueRepository interface {
	// CreateDialogueLine stores a line. A duplicate (scene, ordinal) pair is a conflict.
	CreateDialogueLine(ctx context.Context, record scriptdomain.DialogueLine) error
	// GetDialogueLine returns one line by id.
	GetDialogueLine(ctx context.Context, id string) (scriptdomain.DialogueLine, error)
	// ListDialogueLines returns a scene's lines in order.
	ListDialogueLines(ctx context.Context, sceneID string) ([]scriptdomain.DialogueLine, error)
	// SetDialogueLineLocked records whether a line is protected from regeneration. It is a
	// command of its own rather than a field of a general update, because a lock is the only
	// thing a user changes about a line in this build — a line's TEXT is rewritten wholesale by
	// a new version, which is what makes a version immutable.
	//
	// updatedAt is the caller's, like every other timestamp in this package: a repository that
	// read its own clock would be a second source of truth for the same fact.
	SetDialogueLineLocked(ctx context.Context, lineID string, locked bool, expectedRevision int64, updatedAt time.Time) error
}

// StructureRepository writes a whole script version's content in ONE transaction.
//
// It exists rather than three separate loops over SceneRepository, DialogueRepository and
// ShotRepository because the three are one unit of work: PRD FR-040's S3 produces one version,
// and a version that existed with half its scenes would be an artifact nobody could judge —
// including its own duration, which is a sum over those scenes. The single transaction is the
// point of the port, so a caller cannot write a version in pieces by accident.
type StructureRepository interface {
	// CreateScriptStructure writes a version's scenes, lines and shots, in one transaction,
	// replacing nothing: the version is new or this fails.
	CreateScriptStructure(ctx context.Context, structure scriptdomain.ScriptStructure) error
	// GetScriptStructure reads a version's whole content: its scenes with their lines and shots.
	GetScriptStructure(ctx context.Context, scriptVersionID string) (scriptdomain.ScriptStructure, error)
}

// FieldLockRepository persists the fields a user pinned (AC-SCRIPT-002).
//
// The family travels with the version id because the three version families live in three
// tables, so a lock cannot be a foreign key and the field vocabulary has to be validated against
// the right one. The write path reads the version row first and states its family here, which is
// what keeps the table honest.
type FieldLockRepository interface {
	// LockScriptField records a lock. Locking a field twice is idempotent rather than a
	// conflict: the user's intent is "this stays", and repeating it changes nothing.
	LockScriptField(ctx context.Context, record scriptdomain.FieldLock) error
	// UnlockScriptField removes a lock. Unlocking an unlocked field is idempotent for the same
	// reason.
	UnlockScriptField(ctx context.Context, versionID string, field scriptdomain.LockableField) error
	// ListScriptFieldLocks returns a version's locks, in the order the family declares its
	// fields so a caller sees a stable list.
	ListScriptFieldLocks(ctx context.Context, versionID string) ([]scriptdomain.FieldLock, error)
}

// EventLinkRepository READS the two link tables migration 000008 created.
//
// DOMAIN_MODEL section 7.4 says a skeleton's selected events are a LINK TABLE and not JSON
// ("Lists must be link tables or controlled structures, not Markdown"), and section 7.5 says the
// same for a strategy's retained/removed/reordered sets. Both tables existed since WP-05 and both
// had no writer until WP-08, so FR-040's "忠实保留、合并、删减和新增项" had nowhere to land.
//
// # Why the WRITES are not here
//
// A link set is meaningless without the version it belongs to, so the two are one unit of work and
// are declared as one: `CreateStorySkeletonVersionWithLinks` on the skeleton port and
// `CreateAdaptationStrategyVersionWithLinks` on the strategy port. A `LinkSkeletonEvents` callable
// on its own would be a second way to state the same fact — and the one a caller could reach for a
// version that does not exist, or reach for a version and then never write its links. What is left
// here is the reading half, which is what a diff and a UI need.
type EventLinkRepository interface {
	// ListSkeletonEventIDs returns the events one skeleton version selected, in the order it
	// recorded them.
	ListSkeletonEventIDs(ctx context.Context, versionID string) ([]string, error)
	// ListStrategyEventLinks returns one strategy version's treatments.
	ListStrategyEventLinks(ctx context.Context, versionID string) ([]scriptdomain.StrategyEventLink, error)
}

// StoryReferenceRepository answers whether the story-graph rows a script names actually exist.
//
// AGENT_CONTRACTS section 17 puts "引用存在性" (reference existence) in the code's column, and
// AC-SCRIPT-003 asks for "source event 引用" as a formal relation. The schema cannot enforce it:
// `scenes.source_story_event_id` and `dialogue_lines.source_story_event_id` are TEXT columns with
// no foreign key, because deleting a story event must not delete a scene that dramatized it — the
// citation is provenance, and provenance outlives the row. So the check is here, and it is a
// REFUSAL rather than a repair: a scene citing an event that does not exist is a citation a reader
// would follow into nothing, which is what AC-AGENT-003 makes fail a stage.
//
// The methods return what is MISSING rather than a yes/no, because the refusal has to name the
// identifiers: a stage told only "a reference is wrong" would re-run and produce the same wrong
// reference, which is the loop the repair budget exists to stop.
type StoryReferenceRepository interface {
	// MissingStoryEventIDs returns the subset of eventIDs that do not exist in the project, in
	// the order it was given them.
	MissingStoryEventIDs(ctx context.Context, projectID string, eventIDs []string) ([]string, error)
	// MissingStoryEntityIDs returns the subset of entityIDs that do not exist in the project.
	MissingStoryEntityIDs(ctx context.Context, projectID string, entityIDs []string) ([]string, error)
}

// VersionListRepository reads a version family's history for one episode.
//
// The Agent Center and the Script UI both need "every version of this artifact", and neither the
// existing ports nor the bindings had it: only MAX and CURRENT-APPROVED were ever asked for,
// which is enough to number the next version and not enough to show a history or to diff two of
// them.
type VersionListRepository interface {
	// ListStorySkeletonVersions returns an episode's skeleton versions newest first.
	ListStorySkeletonVersions(ctx context.Context, episodeID string) ([]scriptdomain.StorySkeletonVersion, error)
	// ListAdaptationStrategyVersions returns an episode's strategy versions newest first.
	ListAdaptationStrategyVersions(ctx context.Context, episodeID string) ([]scriptdomain.AdaptationStrategyVersion, error)
	// ListScriptVersions returns a script's versions newest first.
	ListScriptVersions(ctx context.Context, scriptID string) ([]scriptdomain.ScriptVersion, error)
}

// Repository is everything the script service drives. Infrastructure supplies
// one implementation of each family over the same database handle.
type Repository interface {
	EpisodeRepository
	SkeletonRepository
	StrategyRepository
	ScriptRepository
	SceneRepository
	ShotRepository
	DialogueRepository
	StructureRepository
	FieldLockRepository
	EventLinkRepository
	StoryReferenceRepository
	VersionListRepository
}

// Service holds the episode and script commands and queries.
// EventRecorder builds a domain event for a command that records it inside its
// own transaction.
//
// It is the narrow surface §16's "event" requirement needs, and it is optional:
// a Service composed without one still serves every command, and the approval
// commands fail closed rather than approving without a record. That direction is
// deliberate — an approval that is not recorded is worse than an approval that
// is refused, because the governance record is what a later audit reads.
type EventRecorder interface {
	Build(ctx context.Context, draft eventsapp.Draft) (event.Event, error)
	RecordBestEffort(ctx context.Context, draft eventsapp.Draft)
}

type Service struct {
	repository Repository
	clock      Clock
	ids        IDGenerator
	events     EventRecorder
	// projector writes canvas nodes for projected entities. It is optional, and its absence is a
	// REFUSAL for the projection command rather than a silent no-op: a caller asking for a
	// projection and getting success would have no way to tell it did not happen.
	projector CanvasProjector
}

// Options configures a Service.
type Options struct {
	Repository Repository
	Clock      Clock
	IDs        IDGenerator
	// Events enables the commands that must record a domain event. A nil value
	// leaves those commands failing closed; every other command still works.
	Events EventRecorder
	// Projector enables the canvas-projection command. A nil value leaves that one
	// command failing closed, which is what a build with no canvas wants.
	Projector CanvasProjector
}
