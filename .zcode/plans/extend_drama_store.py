import io

p = 'internal/desktop/drama_binding_test.go'
s = io.open(p, encoding='utf-8').read()

anchor = '''func (s *dramaStore) ProjectOfEpisode(_ context.Context, episodeID string) (string, error) {'''
addition = '''func (s *dramaStore) ListDirectorPlanVersions(_ context.Context, episodeID string) ([]storyboard.DirectorPlanVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	versions := []storyboard.DirectorPlanVersion{}
	for _, version := range s.plans {
		if version.EpisodeID == episodeID {
			versions = append(versions, version)
		}
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].VersionNumber > versions[j].VersionNumber })
	return versions, nil
}

func (s *dramaStore) ListStoryboardVersions(_ context.Context, storyboardID string) ([]storyboard.StoryboardVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	versions := []storyboard.StoryboardVersion{}
	for _, version := range s.boards {
		if version.StoryboardID == storyboardID {
			versions = append(versions, version)
		}
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].VersionNumber > versions[j].VersionNumber })
	return versions, nil
}

func (s *dramaStore) UpdateStoryboardItem(_ context.Context, item storyboard.StoryboardItem, expectedRevision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, ok := s.items[item.ID]
	if !ok {
		return storyboard.NotFoundError()
	}
	if stored.Revision != expectedRevision {
		return storyboard.ConflictError("This storyboard row changed in another window. Reload it and try again.")
	}
	item.Revision = expectedRevision + 1
	s.items[item.ID] = item
	return nil
}

''' + anchor
assert anchor in s, "dramaStore anchor"
s = s.replace(anchor, addition, 1)

if '"sort"' not in s:
    s = s.replace('import (\n\t"context"', 'import (\n\t"context"\n\t"sort"', 1)

io.open(p, 'w', encoding='utf-8').write(s)
print("dramaStore extended")
