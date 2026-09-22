import io

p = 'internal/infrastructure/database/storyboard.go'
s = io.open(p, encoding='utf-8').read()

# 1. ListDirectorPlanVersions, beside the plan's other reads.
anchor_plan = '''func (r *StoryboardRepository) MaxDirectorPlanVersionNumber(ctx context.Context, episodeID string) (int, error) {'''
addition_plan = '''// ListDirectorPlanVersions returns an episode's plan versions newest first.
//
// WP-09 added it for the director plan UI: a user looking at an episode's plans needs the
// history to see what a revision changed, and reading each version by an id they do not
// hold is a query nobody can make. The order is newest first because that is the order the
// list is read in — the version in force is the one a person wants to see.
func (r *StoryboardRepository) ListDirectorPlanVersions(ctx context.Context, episodeID string) ([]storyboard.DirectorPlanVersion, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("STORYBOARD_STORE_UNAVAILABLE", "The storyboard store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, directorPlanSelectColumns+
		` WHERE episode_id = ? ORDER BY version_number DESC`, episodeID)
	if err != nil {
		return nil, storageError("STORYBOARD_READ_FAILED", "The director plan versions could not be read.", err)
	}
	defer rows.Close()
	versions := []storyboard.DirectorPlanVersion{}
	for rows.Next() {
		version, scanErr := scanDirectorPlanVersion(rows)
		if scanErr != nil {
			return nil, storageError("STORYBOARD_READ_FAILED", "The director plan versions could not be read.", scanErr)
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("STORYBOARD_READ_FAILED", "The director plan versions could not be read.", err)
	}
	return versions, nil
}

''' + anchor_plan
assert anchor_plan in s, "plan anchor"
s = s.replace(anchor_plan, addition_plan, 1)

# 2. ListStoryboardVersions, beside the storyboard's other reads.
anchor_sb = '''const storyboardVersionSelectColumns = `SELECT id, storyboard_id, version_number, status, script_version_id,'''
addition_sb = '''// ListStoryboardVersions returns a storyboard's versions newest first, for the reason
// ListDirectorPlanVersions states.
func (r *StoryboardRepository) ListStoryboardVersions(ctx context.Context, storyboardID string) ([]storyboard.StoryboardVersion, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("STORYBOARD_STORE_UNAVAILABLE", "The storyboard store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, storyboardVersionSelectColumns+
		` WHERE storyboard_id = ? ORDER BY version_number DESC`, storyboardID)
	if err != nil {
		return nil, storageError("STORYBOARD_READ_FAILED", "The storyboard versions could not be read.", err)
	}
	defer rows.Close()
	versions := []storyboard.StoryboardVersion{}
	for rows.Next() {
		version, scanErr := scanStoryboardVersion(rows)
		if scanErr != nil {
			return nil, storageError("STORYBOARD_READ_FAILED", "The storyboard versions could not be read.", scanErr)
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("STORYBOARD_READ_FAILED", "The storyboard versions could not be read.", err)
	}
	return versions, nil
}

''' + anchor_sb
assert anchor_sb in s, "storyboard anchor"
s = s.replace(anchor_sb, addition_sb, 1)

# 3. UpdateStoryboardItem, beside GetStoryboardItem.
anchor_item = '''// ListStoryboardItems returns a version's items in ordinal order.'''
addition_item = '''// UpdateStoryboardItem persists one row's change, guarded by its revision.
//
// THE REVISION GUARD IS THE POINT, and it is what makes AC-BOARD-002's single-shot redo a
// change to one row rather than a rewrite of the version: a redo names the shot it revises,
// this writes that row alone, and every other row is untouched because nothing wrote it. A
// caller whose copy of the row is stale gets a conflict rather than overwriting a change
// another window made.
//
// It does NOT write an event: a storyboard row is a projection of the board rather than a
// version of record, and the board's own approval is what records governance. The version
// the row belongs to is where "who approved this" is answered.
func (r *StoryboardRepository) UpdateStoryboardItem(ctx context.Context, item storyboard.StoryboardItem, expectedRevision int64) error {
	conn := r.conn()
	if conn == nil {
		return storageError("STORYBOARD_STORE_UNAVAILABLE", "The storyboard store is unavailable.", nil)
	}
	result, err := conn.ExecContext(ctx, `UPDATE storyboard_items SET
		shot_size = ?, camera_angle = ?, camera_movement = ?, duration_seconds = ?,
		visual_description = ?, action_description = ?, dialogue_audio_summary = ?,
		continuity_notes = ?, first_frame_description = ?, last_frame_description = ?,
		video_motion_description = ?, status = ?, updated_at = ?, revision = revision + 1
		WHERE id = ? AND revision = ?`,
		item.ShotSize, item.CameraAngle, item.CameraMovement, item.DurationSeconds,
		item.VisualDescription, item.ActionDescription, item.DialogueAudioSummary,
		item.ContinuityNotes, item.FirstFrameDescription, item.LastFrameDescription,
		item.VideoMotionDescription, string(item.Status), formatTime(item.UpdatedAt),
		item.ID, expectedRevision)
	if err != nil {
		if isForeignKeyViolation(err) {
			return storyboard.InvalidError("That storyboard version does not exist.")
		}
		return storageError("STORYBOARD_WRITE_FAILED", "The storyboard item could not be saved.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError("STORYBOARD_WRITE_FAILED", "The storyboard item could not be saved.", err)
	}
	if affected == 0 {
		// Nothing moved, which means either the row is gone or another window changed it.
		// The two are different situations, so they are told apart rather than conflated.
		if _, readErr := r.GetStoryboardItem(ctx, item.ID); readErr != nil {
			return readErr
		}
		return storyboard.ConflictError("This storyboard row changed in another window. Reload it and try again.")
	}
	return nil
}

''' + anchor_item
assert anchor_item in s, "item anchor"
s = s.replace(anchor_item, addition_item, 1)

io.open(p, 'w', encoding='utf-8').write(s)
print("repository methods added")
