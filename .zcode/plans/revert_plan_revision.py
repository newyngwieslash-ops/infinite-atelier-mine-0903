import io

# The plan table has created_at but NO updated_at and NO revision (the revision at line 49 of
# migration 000010 belongs to `storyboards`). A domain field for a column that does not exist
# is a field nothing can store, so both are reverted here and the overrides write carries no
# revision guard — which is what this commit's earlier reading got wrong.

p = 'internal/domain/storyboard/storyboard.go'
s = io.open(p, encoding='utf-8').read()
old = '''	ChangeReason      string
	LegacyMetadata    string
	CreatedAt         time.Time
	// UpdatedAt and Revision are the optimistic-concurrency pair every mutable row in this
	// schema carries, and the plan's `revision` column has existed since migration 000010
	// with nothing reading it — the same shape of gap WP-09 found in `asset_versions`,
	// where five columns had no field. They matter now because a camera write-back is a
	// guarded update: without the revision there is nothing to guard on.
	UpdatedAt time.Time
	Revision  int64
}'''
new = '''	ChangeReason      string
	LegacyMetadata    string
	CreatedAt         time.Time
	// There is deliberately NO UpdatedAt and NO Revision here, and the reason is the
	// schema rather than a preference: migration 000010 gives `director_plan_versions` a
	// `created_at` and nothing else, so a field would be one nothing could store. The
	// `revision` column that migration does declare belongs to `storyboards`.
	//
	// WP-09's first attempt at the shot-overrides write added both and guarded on the
	// revision — and then this table's DDL was read, which is the check that caught it. A
	// guarded write needs a guard to read; a plan version has none, so its overrides write
	// replaces the document without one. The concurrency the plan versions DO have is their
	// uniqueness per episode, which is what versioning by number gives them.
}'''
assert old in s, "domain anchor"
s = s.replace(old, new, 1)
io.open(p, 'w', encoding='utf-8').write(s)
print("domain reverted to the schema")

# The mapper follows.
p2 = 'internal/infrastructure/database/storyboard.go'
s2 = io.open(p2, encoding='utf-8').read()
s2 = s2.replace('''	change_reason, legacy_metadata_json, created_at, updated_at, revision FROM director_plan_versions`''',
                '''	change_reason, legacy_metadata_json, created_at FROM director_plan_versions`''')
s2 = s2.replace('''	var status, createdByType, createdAt, updatedAt string
	if err := row.Scan(&version.ID, &version.EpisodeID, &version.VersionNumber, &status,
		&version.BasedOnVersionID, &version.ScriptVersionID, &version.VisualRhythm, &version.CameraLanguage,
		&version.ColorLighting, &version.Staging, &version.ContinuityRules, &version.AudioDirection,
		&version.ShotOverridesJSON, &version.SourceAgentRunID, &createdByType, &version.CreatedByID,
		&version.ChangeReason, &version.LegacyMetadata, &createdAt, &updatedAt, &version.Revision); err != nil {
		return storyboard.DirectorPlanVersion{}, err
	}
	version.Status = versioning.Status(status)
	version.CreatedByType = versioning.CreatedByType(createdByType)
	version.CreatedAt = parseTime(createdAt)
	version.UpdatedAt = parseTime(updatedAt)
	return version, nil''','''	var status, createdByType, createdAt string
	if err := row.Scan(&version.ID, &version.EpisodeID, &version.VersionNumber, &status,
		&version.BasedOnVersionID, &version.ScriptVersionID, &version.VisualRhythm, &version.CameraLanguage,
		&version.ColorLighting, &version.Staging, &version.ContinuityRules, &version.AudioDirection,
		&version.ShotOverridesJSON, &version.SourceAgentRunID, &createdByType, &version.CreatedByID,
		&version.ChangeReason, &version.LegacyMetadata, &createdAt); err != nil {
		return storyboard.DirectorPlanVersion{}, err
	}
	version.Status = versioning.Status(status)
	version.CreatedByType = versioning.CreatedByType(createdByType)
	version.CreatedAt = parseTime(createdAt)
	return version, nil''')
io.open(p2, 'w', encoding='utf-8').write(s2)
print("mapper reverted")

# The repository's overrides write loses the guard, and the port's signature follows.
p3 = 'internal/infrastructure/database/storyboard.go'
s3 = io.open(p3, encoding='utf-8').read()
old3 = '''func (r *StoryboardRepository) UpdateDirectorPlanOverrides(ctx context.Context, versionID string, overridesJSON string, expectedRevision int64, updatedAt time.Time) error {
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
}'''
new3 = '''func (r *StoryboardRepository) UpdateDirectorPlanOverrides(ctx context.Context, versionID string, overridesJSON string) error {
	conn := r.conn()
	if conn == nil {
		return storageError("STORYBOARD_STORE_UNAVAILABLE", "The storyboard store is unavailable.", nil)
	}
	result, err := conn.ExecContext(ctx,
		`UPDATE director_plan_versions SET shot_overrides_json = ? WHERE id = ?`,
		overridesJSON, versionID)
	if err != nil {
		return storageError("STORYBOARD_WRITE_FAILED", "The plan's shot overrides could not be saved.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError("STORYBOARD_WRITE_FAILED", "The plan's shot overrides could not be saved.", err)
	}
	if affected == 0 {
		// Nothing moved, and the only reason is that no such version exists: this table has
		// no revision column, so a lost update is not a state it can reach.
		if _, readErr := r.GetDirectorPlanVersion(ctx, versionID); readErr != nil {
			return readErr
		}
		return storageError("STORYBOARD_WRITE_FAILED", "The plan's shot overrides could not be saved.", nil)
	}
	return nil
}'''
assert old3 in s3, "repo anchor"
s3 = s3.replace(old3, new3, 1)
io.open(p3, 'w', encoding='utf-8').write(s3)
print("repository reverted")

# The port.
p4 = 'internal/application/storyboard/ports.go'
s4 = io.open(p4, encoding='utf-8').read()
s4 = s4.replace('''	// UpdateDirectorPlanOverrides replaces one plan version's shot overrides document,
	// guarded by the revision the caller read.
	//
	// It is a NARROW write rather than a full update, and that is section 9.1's shape: a
	// per-shot camera override lives in `shot_overrides_json`, and replacing that document
	// is the only thing this command does. A full update would let a caller that read a
	// plan write back its prose too, and a camera write-back has no business touching the
	// camera language.
	UpdateDirectorPlanOverrides(ctx context.Context, versionID string, overridesJSON string, expectedRevision int64, updatedAt time.Time) error''',
'''	// UpdateDirectorPlanOverrides replaces one plan version's shot overrides document.
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
	UpdateDirectorPlanOverrides(ctx context.Context, versionID string, overridesJSON string) error''')
if '"time"' in s4 and 'time.Time' not in s4.replace('"time"', ''):
    s4 = s4.replace('\t"time"\n', '')
io.open(p4, 'w', encoding='utf-8').write(s4)
print("port reverted")

# The service command and the binding.
p5 = 'internal/application/storyboard/service.go'
s5 = io.open(p5, encoding='utf-8').read()
s5 = s5.replace('''	OverridesJSON string
	// ExpectedRevision is the revision the caller read, so a stale write is refused.
	ExpectedRevision int64
}''','''	OverridesJSON string
}''')
s5 = s5.replace('''	if request.ExpectedRevision < 1 {
		return storyboard.DirectorPlanVersion{}, storyboard.InvalidError("A shot override must name the revision it was read at.")
	}
''','')
s5 = s5.replace('''	if err := s.directorPlans.UpdateDirectorPlanOverrides(ctx, versionID, request.OverridesJSON, request.ExpectedRevision, s.now()); err != nil {''','''	if err := s.directorPlans.UpdateDirectorPlanOverrides(ctx, versionID, request.OverridesJSON); err != nil {''')
io.open(p5, 'w', encoding='utf-8').write(s5)

p6 = 'internal/desktop/drama_binding.go'
s6 = io.open(p6, encoding='utf-8').read()
s6 = s6.replace('''	// OverridesJSON replaces the stored document rather than merging into it.
	OverridesJSON string `json:"overridesJson"`
	// ExpectedRevision is the revision the caller read.
	ExpectedRevision int64 `json:"expectedRevision"`
}''','''	// OverridesJSON replaces the stored document rather than merging into it.
	OverridesJSON string `json:"overridesJson"`
}''')
s6 = s6.replace('''	record, err := service.SetShotOverrides(b.context(), appstoryboard.SetShotOverridesRequest{
		VersionID:        request.VersionID,
		OverridesJSON:    request.OverridesJSON,
		ExpectedRevision: request.ExpectedRevision,
	})''','''	record, err := service.SetShotOverrides(b.context(), appstoryboard.SetShotOverridesRequest{
		VersionID:     request.VersionID,
		OverridesJSON: request.OverridesJSON,
	})''')
io.open(p6, 'w', encoding='utf-8').write(s6)

# The two doubles.
for path in ('internal/application/storyboard/service_test.go', 'internal/desktop/drama_binding_test.go'):
    s7 = io.open(path, encoding='utf-8').read()
    s7 = s7.replace('''func (r *memoryRepo) UpdateDirectorPlanOverrides(_ context.Context, versionID string, overridesJSON string, expectedRevision int64, updatedAt time.Time) error {
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
}''','''func (r *memoryRepo) UpdateDirectorPlanOverrides(_ context.Context, versionID string, overridesJSON string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	version, ok := r.plans[versionID]
	if !ok {
		return storyboard.NotFoundError()
	}
	version.ShotOverridesJSON = overridesJSON
	r.plans[versionID] = version
	return nil
}''')
    s7 = s7.replace('''func (s *dramaStore) UpdateDirectorPlanOverrides(_ context.Context, versionID string, overridesJSON string, expectedRevision int64, updatedAt time.Time) error {
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
}''','''func (s *dramaStore) UpdateDirectorPlanOverrides(_ context.Context, versionID string, overridesJSON string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	version, ok := s.plans[versionID]
	if !ok {
		return storyboard.NotFoundError()
	}
	version.ShotOverridesJSON = overridesJSON
	s.plans[versionID] = version
	return nil
}''')
    io.open(path, 'w', encoding='utf-8').write(s7)
print("doubles reverted")
