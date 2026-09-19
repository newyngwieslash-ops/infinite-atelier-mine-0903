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
	// CurrentApprovedScriptVersion returns the script's approved version. The
	// boolean is false rather than an error when none is approved yet, because
	// "no approval" is the ordinary state of a new script.
	CurrentApprovedScriptVersion(ctx context.Context, scriptID string) (scriptdomain.ScriptVersion, bool, error)
	// ApproveScriptVersion switches a script's approval in one transaction:
	// supersededVersionID (empty when nothing was approved) becomes
	// 'superseded' and target becomes 'approved'.
	//
	// The target write is guarded by the status the caller read, which is the
	// only concurrency token this row has: migration 000008 gives script_versions
	// no revision column, so the guard matches status. Both writes are one
	// transaction because the schema's partial unique index
	// (script_id WHERE status = 'approved') admits one approved row per script,
	// so the supersede has to land before the approval: without that order the
	// index would reject the approval, and without the transaction a failure
	// between the two would leave the script with no approved version.
	ApproveScriptVersion(ctx context.Context, target scriptdomain.ScriptVersion, supersededVersionID string) error
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

// Repository is everything the script service drives. Infrastructure supplies
// one implementation of each family over the same database handle.
type Repository interface {
	EpisodeRepository
	SkeletonRepository
	StrategyRepository
	ScriptRepository
	SceneRepository
	ShotRepository
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
}

// Options configures a Service.
type Options struct {
	Repository Repository
	Clock      Clock
	IDs        IDGenerator
	// Events enables the commands that must record a domain event. A nil value
	// leaves those commands failing closed; every other command still works.
	Events EventRecorder
}
