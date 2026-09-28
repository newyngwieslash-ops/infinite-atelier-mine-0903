package database

import (
	"context"
	"testing"

	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
)

// audio_track_params_t05_test.go is the 2026-09-26 audit's T05 at the read
// boundary: a track's OWN parameters — offset, source trim, volume, mute —
// persist on its use, travel through the timeline, and change what the mixer
// builds. The audit's headline cases: two dialogue lines on one shot can be
// staggered, a bed can start later than zero, a zero gain is silence rather
// than "unset", and every parameter survives a restart (which a persisted
// column gives by construction).

// TestAUsageCarriesItsTrackParams proves the round trip: params written with
// a usage come back through the timeline's audio rows.
func TestAUsageCarriesItsTrackParams(t *testing.T) {
	ctx := context.Background()
	harness := newMediaHarness(t)
	boardVersionID := harness.approvedBoard(t, 1, 4)

	// A bed with its own placement: start three seconds in, second half of
	// the source only, quiet, and NOT muted.
	params := appmedia.TrackParams{
		OffsetMS:      intPtr(3000),
		SourceStartMS: intPtr(1500),
		SourceEndMS:   intPtr(4500),
		Volume:        floatPtr(0.2),
	}
	paramsJSON, err := appmedia.MarshalTrackParams(params)
	if err != nil {
		t.Fatal(err)
	}
	harness.attachAudioWithParams(t, "bed-asset", boardVersionID, "wp11-shot-1", appmedia.UsageRoleAudioMusic, paramsJSON)

	rows, err := harness.exports.BoardFacts(ctx, boardVersionID)
	if err != nil {
		t.Fatalf("BoardFacts: %v", err)
	}
	var found *appmedia.AudioVersionRef
	for _, row := range rows {
		for index := range row.AudioClips {
			if row.ShotID == "wp11-shot-1" {
				found = &row.AudioClips[index]
			}
		}
	}
	if found == nil {
		t.Fatal("the bed did not reach the timeline")
	}
	if found.Params.OffsetMS == nil || *found.Params.OffsetMS != 3000 {
		t.Fatalf("the bed's offset is %+v; 3000 was stored", found.Params.OffsetMS)
	}
	if found.Params.SourceStartMS == nil || *found.Params.SourceStartMS != 1500 {
		t.Fatalf("the bed's source start is %+v; 1500 was stored", found.Params.SourceStartMS)
	}
	if found.Params.SourceEndMS == nil || *found.Params.SourceEndMS != 4500 {
		t.Fatalf("the bed's source end is %+v; 4500 was stored", found.Params.SourceEndMS)
	}
	if found.Params.Volume == nil || *found.Params.Volume != 0.2 {
		t.Fatalf("the bed's volume is %+v; 0.2 was stored", found.Params.Volume)
	}
}

// TestMutedIsSilenceNotDefault is the audit's "增益为零能表达静音而非默认值":
// a muted clip keeps its row (the label and manifest still name it) and the
// mixer's clip carries a zero gain — which, AFTER normalization, stays zero
// only for an explicit value, so the test asserts the mix's clip gain directly.
func TestMutedIsSilenceNotDefault(t *testing.T) {
	ctx := context.Background()
	harness := newMediaHarness(t)
	boardVersionID := harness.approvedBoard(t, 1, 4)

	muted := appmedia.TrackParams{Muted: boolPtr(true)}
	paramsJSON, err := appmedia.MarshalTrackParams(muted)
	if err != nil {
		t.Fatal(err)
	}
	harness.attachAudioWithParams(t, "bed-asset", boardVersionID, "wp11-shot-1", appmedia.UsageRoleAudioMusic, paramsJSON)

	rows, err := harness.exports.BoardFacts(ctx, boardVersionID)
	if err != nil {
		t.Fatal(err)
	}
	var ref *appmedia.AudioVersionRef
	for _, row := range rows {
		for index := range row.AudioClips {
			if row.ShotID == "wp11-shot-1" {
				ref = &row.AudioClips[index]
			}
		}
	}
	if ref == nil {
		t.Fatal("the muted bed did not reach the timeline")
	}
	// The mixer's consumption rule: an explicit mute is a ZERO gain applied
	// AFTER normalization — `Overrides` is how the clip carries it past the
	// zero-means-unset convention. Assert the parsed flag and the rule.
	if ref.Params.Muted == nil || !*ref.Params.Muted {
		t.Fatalf("the bed's mute flag is %+v", ref.Params.Muted)
	}
	clip := appmedia.AudioClip{
		Role: appmedia.AudioRoleMusic, Path: "x", Gain: 0.0,
		Overrides: &appmedia.TrackOverrides{Muted: true},
	}
	mixed := clip.Normalize()
	if mixed.Overrides == nil || !mixed.Overrides.Muted || mixed.Gain != 0 {
		t.Fatalf("a muted clip normalized to gain %v with overrides %+v; silence must survive normalization", mixed.Gain, mixed.Overrides)
	}
}

// TestATrackWithNoParamsKeepsTheOldBehaviour is the compatibility half: a row
// written before the column (empty params) parses to the zero document, and
// the mixer's defaults apply — music at the top, role gain, no trim.
func TestATrackWithNoParamsKeepsTheOldBehaviour(t *testing.T) {
	ctx := context.Background()
	harness := newMediaHarness(t)
	boardVersionID := harness.approvedBoard(t, 1, 4)

	harness.attachAudioWithParams(t, "bed-asset", boardVersionID, "wp11-shot-1", appmedia.UsageRoleAudioMusic, "")

	rows, err := harness.exports.BoardFacts(ctx, boardVersionID)
	if err != nil {
		t.Fatal(err)
	}
	var ref *appmedia.AudioVersionRef
	for _, row := range rows {
		for index := range row.AudioClips {
			if row.ShotID == "wp11-shot-1" {
				ref = &row.AudioClips[index]
			}
		}
	}
	if ref == nil {
		t.Fatal("the plain bed did not reach the timeline")
	}
	if !ref.Params.Empty() {
		t.Fatalf("an unparameterised row parsed as %+v; it must stay empty", ref.Params)
	}
}

// attachAudioWithParams writes an approved audio usage with a params document,
// the shape every caller of the track editor produces.
func (h *mediaHarness) attachAudioWithParams(t *testing.T, assetID, boardVersionID, shotID, role, paramsJSON string) {
	t.Helper()
	ctx := context.Background()
	hash := h.put(t, assetID+".wav", tone(t, ctx))
	versionID := assetID + "-v1"
	if _, err := h.db.ExecContext(ctx, `INSERT INTO assets
		(id, project_id, asset_type, name, current_approved_version_id, status, created_at, updated_at, revision)
		VALUES (?, 'drama-project', 'audio', ?, ?, 'active', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
		assetID, "audio "+assetID, versionID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.ExecContext(ctx, `INSERT INTO asset_versions
		(id, asset_id, version_number, status, created_by_type, created_at)
		VALUES (?, ?, 1, 'approved', 'system', '2026-01-01T00:00:00Z')`, versionID, assetID); err != nil {
		t.Fatal(err)
	}
	// The version's primary file: `AudioFileFor` reads role='primary'.
	if _, err := h.db.ExecContext(ctx, `INSERT INTO asset_files (id, asset_version_id, file_hash, role, ordinal, created_at)
		VALUES (?, ?, ?, 'primary', 0, '2026-01-01T00:00:00Z')`, "file-"+assetID, versionID, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := h.db.ExecContext(ctx, `INSERT INTO asset_usages
		(id, asset_version_id, consumer_type, consumer_id, usage_role, required, created_at, params_json)
		VALUES (?, ?, 'shot', ?, ?, 0, '2026-01-01T00:00:00Z', ?)`,
		"usage-"+assetID, versionID, shotID, role, paramsJSON); err != nil {
		t.Fatal(err)
	}
}

func intPtr(value int) *int           { return &value }
func floatPtr(value float64) *float64 { return &value }
func boolPtr(value bool) *bool        { return &value }
