import io

# 1. The plan repository port gains the overrides write.
p = 'internal/application/storyboard/ports.go'
s = io.open(p, encoding='utf-8').read()
old = '''	// ListDirectorPlanVersions returns an episode's plans newest first, for the same
	// reason ListStoryboardVersions exists: a user looking at a plan needs its history,
	// and reading each version by an id they do not have is a query nobody can make.
	ListDirectorPlanVersions(ctx context.Context, episodeID string) ([]storyboard.DirectorPlanVersion, error)'''
new = '''	// ListDirectorPlanVersions returns an episode's plans newest first, for the same
	// reason ListStoryboardVersions exists: a user looking at a plan needs its history,
	// and reading each version by an id they do not have is a query nobody can make.
	ListDirectorPlanVersions(ctx context.Context, episodeID string) ([]storyboard.DirectorPlanVersion, error)
	// UpdateDirectorPlanOverrides replaces one plan version's shot overrides document,
	// guarded by the revision the caller read.
	//
	// It is a NARROW write rather than a full update, and that is section 9.1's shape: a
	// per-shot camera override lives in `shot_overrides_json`, and replacing that document
	// is the only thing this command does. A full update would let a caller that read a
	// plan write back its prose too, and a camera write-back has no business touching the
	// camera language.
	UpdateDirectorPlanOverrides(ctx context.Context, versionID string, overridesJSON string, expectedRevision int64, updatedAt time.Time) error'''
assert old in s, "plan port anchor"
s = s.replace(old, new, 1)
if '"time"' not in s:
    s = s.replace('import (\n\t"context"', 'import (\n\t"context"\n\t"time"', 1)
io.open(p, 'w', encoding='utf-8').write(s)
print("port extended")

# 2. The repository implementation.
p2 = 'internal/infrastructure/database/storyboard.go'
s2 = io.open(p2, encoding='utf-8').read()
anchor = '''// ListDirectorPlanVersions returns an episode's plan versions newest first.'''
addition = '''// UpdateDirectorPlanOverrides replaces one plan version's shot overrides document.
//
// The revision guard is the point: a camera write-back reads the plan, composes an edit and
// writes it, and a second window doing the same would otherwise overwrite the first. The
// column is the ONE this command touches, so a camera from the previs studio cannot carry
// the plan's prose along with it.
func (r *StoryboardRepository) UpdateDirectorPlanOverrides(ctx context.Context, versionID string, overridesJSON string, expectedRevision int64, updatedAt time.Time) error {
	conn := r.conn()
	if conn == nil {
		return storageError("STORYBOARD_STORE_UNAVAILABLE", "The storyboard store is unavailable.", nil)
	}
	result, err := conn.ExecContext(ctx, `UPDATE director_plan_versions
		SET shot_overrides_json = ?, updated_at = ?, revision = revision + 1
		WHERE id = ? AND revision = ?`,
		overridesJSON, formatTime(updatedAt), versionID, expectedRevision)
	if err != nil {
		return storageError("STORYBOARD_WRITE_FAILED", "The plan's shot overrides could not be saved.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError("STORYBOARD_WRITE_FAILED", "The plan's shot overrides could not be saved.", err)
	}
	if affected == 0 {
		if _, readErr := r.GetDirectorPlanVersion(ctx, versionID); readErr != nil {
			return readErr
		}
		return storyboard.ConflictError("This plan changed in another window. Reload it and try again.")
	}
	return nil
}

''' + anchor
assert anchor in s2, "plan repo anchor"
s2 = s2.replace(anchor, addition, 1)
io.open(p2, 'w', encoding='utf-8').write(s2)
print("repository implemented")
