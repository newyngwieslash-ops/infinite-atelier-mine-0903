import io

IMPL = '''func (r *memoryRepo) UpdateDirectorPlanOverrides(_ context.Context, versionID string, overridesJSON string, expectedRevision int64, updatedAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	version, ok := r.plans[versionID]
	if !ok {
		return storyboard.NotFoundError()
	}
	if version.Revision != expectedRevision {
		return storyboard.ConflictError("This plan changed in another window. Reload it and try again.")
	}
	version.ShotOverridesJSON = overridesJSON
	version.Revision = expectedRevision + 1
	version.UpdatedAt = updatedAt
	r.plans[versionID] = version
	return nil
}

'''

p = 'internal/application/storyboard/service_test.go'
s = io.open(p, encoding='utf-8').read()
anchor = 'func (r *memoryRepo) ListDirectorPlanVersions('
assert anchor in s, "memory repo anchor"
s = s.replace(anchor, IMPL + anchor, 1)
if '"time"' not in s:
    s = s.replace('import (\n\t"context"', 'import (\n\t"context"\n\t"time"', 1)
io.open(p, 'w', encoding='utf-8').write(s)
print("memory repo extended")

IMPL2 = '''func (s *dramaStore) UpdateDirectorPlanOverrides(_ context.Context, versionID string, overridesJSON string, expectedRevision int64, updatedAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	version, ok := s.plans[versionID]
	if !ok {
		return storyboard.NotFoundError()
	}
	if version.Revision != expectedRevision {
		return storyboard.ConflictError("This plan changed in another window. Reload it and try again.")
	}
	version.ShotOverridesJSON = overridesJSON
	version.Revision = expectedRevision + 1
	version.UpdatedAt = updatedAt
	s.plans[versionID] = version
	return nil
}

'''
p2 = 'internal/desktop/drama_binding_test.go'
s2 = io.open(p2, encoding='utf-8').read()
anchor2 = 'func (s *dramaStore) ListDirectorPlanVersions('
assert anchor2 in s2, "drama store anchor"
s2 = s2.replace(anchor2, IMPL2 + anchor2, 1)
io.open(p2, 'w', encoding='utf-8').write(s2)
print("drama store extended")
