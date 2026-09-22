// Package storyboard is the application layer for director plans, storyboards,
// storyboard versions, their items and panel versions. It owns the commands and
// queries of DOMAIN_MODEL section 9 and defines the persistence ports that
// infrastructure implements. It performs no I/O itself.
package storyboard

import (
	"context"
	"time"

	eventsapp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/events"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/storyboard"
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

// DirectorPlanRepository persists director plan versions.
type DirectorPlanRepository interface {
	CreateDirectorPlanVersion(ctx context.Context, version storyboard.DirectorPlanVersion) error
	GetDirectorPlanVersion(ctx context.Context, id string) (storyboard.DirectorPlanVersion, error)
	// MaxDirectorPlanVersionNumber reports the highest version number an
	// episode's plans have, or zero when the episode has none. The next version
	// number is derived from the maximum rather than a count, because the schema
	// has UNIQUE (episode_id, version_number) and a reused number is rejected.
	MaxDirectorPlanVersionNumber(ctx context.Context, episodeID string) (int, error)
	// CurrentApprovedDirectorPlanVersionID returns the episode's approved plan
	// version, or "" when none is approved.
	CurrentApprovedDirectorPlanVersionID(ctx context.Context, episodeID string) (string, error)
	// ApproveDirectorPlanVersion switches which version is approved, recording
	// the event in the same transaction.
	ApproveDirectorPlanVersion(ctx context.Context, versionID, episodeID string, expectedStatus versioning.Status, record event.Event) error
	// ListDirectorPlanVersions returns an episode's plans newest first, for the same
	// reason ListStoryboardVersions exists: a user looking at a plan needs its history,
	// and reading each version by an id they do not have is a query nobody can make.
	ListDirectorPlanVersions(ctx context.Context, episodeID string) ([]storyboard.DirectorPlanVersion, error)
	// UpdateDirectorPlanOverrides replaces one plan version's shot overrides document.
	//
	// It is a NARROW write rather than a full update, and that is section 9.1's shape: a
	// per-shot camera override lives in `shot_overrides_json`, and replacing that document
	// is the only thing this command does. A full update would let a caller that read a
	// plan write back its prose too, and a camera write-back has no business touching the
	// camera language.
	//
	// There is no revision guard because the TABLE has no revision column: migration 000010
	// gives `director_plan_versions` a `created_at` and nothing else. A guard needs a value
	// to guard on, and inventing one here would be a check that reads like a protection and
	// protects nothing.
	UpdateDirectorPlanOverrides(ctx context.Context, versionID string, overridesJSON string) error
	// ProjectOfEpisode returns the project an episode belongs to.
	//
	// It exists because the storyboard tables have no project column: a director
	// plan names an episode and the episode names the project, so the read that
	// resolves it belongs beside the join rather than in the service, which would
	// otherwise need the episode aggregate it does not own.
	ProjectOfEpisode(ctx context.Context, episodeID string) (string, error)
}

// StoryboardRepository persists storyboard identities and their versions.
type StoryboardRepository interface {
	// GetStoryboardByEpisode returns an episode's stable storyboard identity. A
	// missing row is a domain not-found error, which is what lets EnsureStoryboard
	// tell "not created yet" from "cannot read".
	GetStoryboardByEpisode(ctx context.Context, episodeID string) (storyboard.Storyboard, error)
	// GetStoryboard returns one storyboard identity, so a caller that already
	// holds the id gets a domain not-found rather than a foreign-key failure.
	GetStoryboard(ctx context.Context, id string) (storyboard.Storyboard, error)
	CreateStoryboard(ctx context.Context, record storyboard.Storyboard) error
	// GetStoryboardVersion returns one storyboard version.
	GetStoryboardVersion(ctx context.Context, id string) (storyboard.StoryboardVersion, error)
	// MaxStoryboardVersionNumber reports the highest version number a storyboard
	// has, or zero when it has none.
	MaxStoryboardVersionNumber(ctx context.Context, storyboardID string) (int, error)
	CreateStoryboardVersion(ctx context.Context, version storyboard.StoryboardVersion) error
	// CurrentApprovedStoryboardVersionID returns the storyboard's approved
	// version, or "" when none is approved.
	CurrentApprovedStoryboardVersionID(ctx context.Context, storyboardID string) (string, error)
	// ApproveStoryboardVersion switches which version is approved, recording the
	// event in the same transaction.
	ApproveStoryboardVersion(ctx context.Context, versionID, storyboardID string, expectedStatus versioning.Status, record event.Event) error
	// ProjectOfStoryboard returns the project a storyboard belongs to, resolved
	// through its episode for the same reason as ProjectOfEpisode.
	ProjectOfStoryboard(ctx context.Context, storyboardID string) (string, error)
	// ListStoryboardVersions returns a storyboard's versions newest first.
	//
	// WP-09 added it for the storyboard TABLE: a user looking at a board needs its
	// history to see what changed, and the alternative — reading each version by an id the
	// user does not have — is a query nobody can make.
	ListStoryboardVersions(ctx context.Context, storyboardID string) ([]storyboard.StoryboardVersion, error)
}

// StoryboardItemRepository persists the per-shot rows of a storyboard version.
type StoryboardItemRepository interface {
	CreateStoryboardItem(ctx context.Context, item storyboard.StoryboardItem) error
	// GetStoryboardItem returns one item, for the panel commands that guard
	// against the item's revision.
	GetStoryboardItem(ctx context.Context, id string) (storyboard.StoryboardItem, error)
	// FindStoryboardItemByShot returns the item a version holds for a shot.
	// found is false when the version has no row for it.
	FindStoryboardItemByShot(ctx context.Context, storyboardVersionID, shotID string) (found storyboard.StoryboardItem, ok bool, err error)
	// FindStoryboardItemByOrdinal returns the item at one position of a version.
	FindStoryboardItemByOrdinal(ctx context.Context, storyboardVersionID string, ordinal int) (found storyboard.StoryboardItem, ok bool, err error)
	// ListStoryboardItems returns a version's items in ordinal order.
	ListStoryboardItems(ctx context.Context, storyboardVersionID string) ([]storyboard.StoryboardItem, error)
	// UpdateStoryboardItem persists a change guarded by the expected revision.
	//
	// It exists because AC-BOARD-002's single-shot redo must change ONE row and leave the
	// others untouched, and a version-rewrite path would have to copy every other row —
	// which is the "其他 Shot 不变" requirement resting on a copy rather than on the row
	// never having been written. The revision guard is what stops two windows overwriting
	// each other's edit.
	UpdateStoryboardItem(ctx context.Context, item storyboard.StoryboardItem, expectedRevision int64) error
}

// PanelRepository persists panel versions.
type PanelRepository interface {
	CreatePanelVersion(ctx context.Context, panel storyboard.StoryboardPanelVersion) error
	GetPanelVersion(ctx context.Context, id string) (storyboard.StoryboardPanelVersion, error)
	// MaxPanelVersionNumber reports the highest version number an item's panels
	// have, or zero when it has none.
	MaxPanelVersionNumber(ctx context.Context, storyboardItemID string) (int, error)
	// ListPanelVersions returns an item's panels in version order.
	ListPanelVersions(ctx context.Context, storyboardItemID string) ([]storyboard.StoryboardPanelVersion, error)
	// ApprovePanelImage stores the asset version a panel's image is approved
	// from, guarded by the revision of the panel's parent storyboard item. The
	// guard is taken there because storyboard_panel_versions carries no revision
	// column: migration 000010 stores one immutable row per panel version, and
	// the only mutating column is approved_image_asset_version_id. A caller
	// whose expected revision no longer matches the item is refused rather than
	// approving an image against a version list that has since changed.
	ApprovePanelImage(ctx context.Context, panelVersionID, approvedImageAssetVersionID string, expectedItemRevision int64) error
}

// Service holds the storyboard commands and queries.
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
	directorPlans DirectorPlanRepository
	storyboards   StoryboardRepository
	items         StoryboardItemRepository
	panels        PanelRepository
	clock         Clock
	ids           IDGenerator
	events        EventRecorder
}

// Options configures a Service.
type Options struct {
	DirectorPlans DirectorPlanRepository
	Storyboards   StoryboardRepository
	Items         StoryboardItemRepository
	Panels        PanelRepository
	Clock         Clock
	IDs           IDGenerator
	// Events enables the approval commands. A nil value leaves them failing
	// closed.
	Events EventRecorder
}
