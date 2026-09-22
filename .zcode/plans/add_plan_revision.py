import io

# 1. The domain gains the field the schema has always had.
p = 'internal/domain/storyboard/storyboard.go'
s = io.open(p, encoding='utf-8').read()
old = '''	ChangeReason      string
	LegacyMetadata    string
	CreatedAt         time.Time
}

// Validate checks a director plan version'''
new = '''	ChangeReason      string
	LegacyMetadata    string
	CreatedAt         time.Time
	// UpdatedAt and Revision are the optimistic-concurrency pair every mutable row in this
	// schema carries, and the plan's `revision` column has existed since migration 000010
	// with nothing reading it — the same shape of gap WP-09 found in `asset_versions`,
	// where five columns had no field. They matter now because a camera write-back is a
	// guarded update: without the revision there is nothing to guard on.
	UpdatedAt time.Time
	Revision  int64
}

// Validate checks a director plan version'''
assert old in s, "domain anchor"
s = s.replace(old, new, 1)
io.open(p, 'w', encoding='utf-8').write(s)
print("domain extended")

# 2. The mapper reads and writes them.
p2 = 'internal/infrastructure/database/storyboard.go'
s2 = io.open(p2, encoding='utf-8').read()
old2 = '''	audio_direction, shot_overrides_json, source_agent_run_id, created_by_type, created_by_id,
	change_reason, legacy_metadata_json, created_at FROM director_plan_versions`'''
new2 = '''	audio_direction, shot_overrides_json, source_agent_run_id, created_by_type, created_by_id,
	change_reason, legacy_metadata_json, created_at, updated_at, revision FROM director_plan_versions`'''
assert old2 in s2, "select anchor"
s2 = s2.replace(old2, new2, 1)
io.open(p2, 'w', encoding='utf-8').write(s2)
print("select extended")
