import io

# 1. The two repository ports gain the reads a UI needs.
p = 'internal/application/storyboard/ports.go'
s = io.open(p, encoding='utf-8').read()

old = '''	// ProjectOfStoryboard returns the project a storyboard belongs to, resolved
	// through its episode for the same reason as ProjectOfEpisode.
	ProjectOfStoryboard(ctx context.Context, storyboardID string) (string, error)
}'''
new = '''	// ProjectOfStoryboard returns the project a storyboard belongs to, resolved
	// through its episode for the same reason as ProjectOfEpisode.
	ProjectOfStoryboard(ctx context.Context, storyboardID string) (string, error)
	// ListStoryboardVersions returns a storyboard's versions newest first.
	//
	// WP-09 added it for the storyboard TABLE: a user looking at a board needs its
	// history to see what changed, and the alternative — reading each version by an id the
	// user does not have — is a query nobody can make.
	ListStoryboardVersions(ctx context.Context, storyboardID string) ([]storyboard.StoryboardVersion, error)
}

// DirectorPlanRepository persists the plan versions an episode is shot from.
type DirectorPlanRepository interface {
	CreateDirectorPlanVersion(ctx context.Context, version storyboard.DirectorPlanVersion) error
	GetDirectorPlanVersion(ctx context.Context, id string) (storyboard.DirectorPlanVersion, error)
	// ListDirectorPlanVersions returns an episode's plans newest first, for the same
	// reason ListStoryboardVersions exists.
	ListDirectorPlanVersions(ctx context.Context, episodeID string) ([]storyboard.DirectorPlanVersion, error)
	MaxDirectorPlanVersionNumber(ctx context.Context, episodeID string) (int, error)
	CurrentApprovedDirectorPlanVersionID(ctx context.Context, episodeID string) (string, error)
	ApproveDirectorPlanVersion(ctx context.Context, versionID, episodeID string, expectedStatus versioning.Status, record event.Event) error
	ProjectOfEpisode(ctx context.Context, episodeID string) (string, error)
}'''
assert old in s, "storyboard port anchor"
s = s.replace(old, new, 1)

old_item = '''	// ListStoryboardItems returns a version's items in ordinal order.
	ListStoryboardItems(ctx context.Context, storyboardVersionID string) ([]storyboard.StoryboardItem, error)
}'''
new_item = '''	// ListStoryboardItems returns a version's items in ordinal order.
	ListStoryboardItems(ctx context.Context, storyboardVersionID string) ([]storyboard.StoryboardItem, error)
	// UpdateStoryboardItem persists a change guarded by the expected revision.
	//
	// It exists because AC-BOARD-002's single-shot redo must change ONE row and leave the
	// others untouched, and a version-rewrite path would have to copy every other row —
	// which is the "其他 Shot 不变" requirement resting on a copy rather than on the row
	// never having been written. The revision guard is what stops two windows overwriting
	// each other's edit.
	UpdateStoryboardItem(ctx context.Context, item storyboard.StoryboardItem, expectedRevision int64) error
}'''
assert old_item in s, "item port anchor"
s = s.replace(old_item, new_item, 1)

io.open(p, 'w', encoding='utf-8').write(s)
print("ports extended")
