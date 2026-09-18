// Package storyboard is the application layer for director plans, storyboards,
// storyboard versions, their items and panel versions. It owns the commands and
// queries of DOMAIN_MODEL section 9 and defines the persistence ports that
// infrastructure implements. It performs no I/O itself.
package storyboard

import (
	"context"
	"time"

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
type Service struct {
	directorPlans DirectorPlanRepository
	storyboards   StoryboardRepository
	items         StoryboardItemRepository
	panels        PanelRepository
	clock         Clock
	ids           IDGenerator
}

// Options configures a Service.
type Options struct {
	DirectorPlans DirectorPlanRepository
	Storyboards   StoryboardRepository
	Items         StoryboardItemRepository
	Panels        PanelRepository
	Clock         Clock
	IDs           IDGenerator
}
