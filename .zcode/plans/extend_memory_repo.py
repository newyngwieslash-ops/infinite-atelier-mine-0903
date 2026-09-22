import io

# 1. The storyboard service's in-memory double.
p = 'internal/application/storyboard/service_test.go'
s = io.open(p, encoding='utf-8').read()

anchor = '''func (r *memoryRepo) ProjectOfEpisode(_ context.Context, episodeID string) (string, error) {'''
addition = '''func (r *memoryRepo) ListDirectorPlanVersions(_ context.Context, episodeID string) ([]storyboard.DirectorPlanVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	versions := []storyboard.DirectorPlanVersion{}
	for _, version := range r.plans {
		if version.EpisodeID == episodeID {
			versions = append(versions, version)
		}
	}
	// Newest first, which is the order the repository's own query produces.
	sort.Slice(versions, func(i, j int) bool { return versions[i].VersionNumber > versions[j].VersionNumber })
	return versions, nil
}

func (r *memoryRepo) ListStoryboardVersions(_ context.Context, storyboardID string) ([]storyboard.StoryboardVersion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	versions := []storyboard.StoryboardVersion{}
	for _, version := range r.versions {
		if version.StoryboardID == storyboardID {
			versions = append(versions, version)
		}
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].VersionNumber > versions[j].VersionNumber })
	return versions, nil
}

func (r *memoryRepo) UpdateStoryboardItem(_ context.Context, item storyboard.StoryboardItem, expectedRevision int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failNextWrite != nil {
		return r.failNextWrite
	}
	stored, ok := r.items[item.ID]
	if !ok {
		return storyboard.NotFoundError()
	}
	if stored.Revision != expectedRevision {
		return storyboard.ConflictError("This storyboard row changed in another window. Reload it and try again.")
	}
	item.Revision = expectedRevision + 1
	r.items[item.ID] = item
	return nil
}

''' + anchor
assert anchor in s, "memory repo anchor"
s = s.replace(anchor, addition, 1)

if '"sort"' not in s:
    s = s.replace('import (\n\t"context"', 'import (\n\t"context"\n\t"sort"', 1)

io.open(p, 'w', encoding='utf-8').write(s)
print("memory repo extended")
