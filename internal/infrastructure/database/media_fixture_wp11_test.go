package database

import (
	"bytes"
	"context"
	"database/sql"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	domainmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/media"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/storyboard"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// media_fixture_wp11_test.go holds the helpers the AC-MEDIA-003 walk builds its episode with.
//
// They are here rather than in the test file because they are FIXTURE CODE — writing a board, a PNG, an
// asset version — and the acceptance walk should read as the criterion rather than as the setup.

// bytesReader wraps a byte slice as a reader.
func bytesReader(content []byte) *bytes.Reader {
	return bytes.NewReader(content)
}

// pngFixture renders a real PNG of the given colour.
//
// Real bytes rather than a byte pattern: the export composes these through ffmpeg, and a fixture that
// was not decodable would fail at the first frame — a different test, about the fixture, than the one
// AC-MEDIA-003 asks for.
func pngFixture(t *testing.T, width, height int, shade byte) []byte {
	t.Helper()
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	fill := color.RGBA{R: shade, G: shade, B: shade, A: 255}
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			canvas.Set(x, y, fill)
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, canvas); err != nil {
		t.Fatalf("rendering the fixture PNG: %v", err)
	}
	return buffer.Bytes()
}

// writeAssetWithVersion writes an image asset, its approved version and the version's primary file.
//
// The file row is the join `asset_files.file_hash` needs, and the approved pointer is what the
// timeline reads: a version without either would look to the timeline like a shot with no media.
func writeAssetWithVersion(ctx context.Context, db *sql.DB, assetID, versionID, hash string, ordinal int) error {
	if _, err := db.ExecContext(ctx, `INSERT INTO assets
		(id, project_id, asset_type, name, story_entity_id, current_approved_version_id, status,
		 created_at, updated_at, revision)
		VALUES (?, 'drama-project', 'image', ?, '', '', 'active',
		 '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
		assetID, "shot frame "+itoaWP10(ordinal)); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO asset_versions
		(id, asset_id, version_number, status, created_by_type, created_at)
		VALUES (?, ?, 1, 'approved', 'user', '2026-01-01T00:00:00Z')`, versionID, assetID); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO asset_files
		(id, asset_version_id, file_hash, role, ordinal, created_at)
		VALUES (?, ?, ?, 'primary', 0, '2026-01-01T00:00:00Z')`,
		"file-"+versionID, versionID, hash); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `UPDATE assets SET current_approved_version_id = ? WHERE id = ?`,
		versionID, assetID)
	return err
}

// writeDirectorPlan writes the plan a board version cites.
//
// A storyboard version names the plan it was drawn from as a FIELD rather than as provenance —
// DOMAIN_MODEL section 9.3's "a board that names neither is a board of nothing" — and the repository's
// foreign key refuses a board without one. The fixture writes a real row rather than relaxing the
// citation, because the manifest's traceability depends on that field pointing at something.
func writeDirectorPlan(ctx context.Context, db *sql.DB, id string, now time.Time) error {
	_, err := db.ExecContext(ctx, `INSERT INTO director_plan_versions
		(id, episode_id, version_number, status, script_version_id, created_by_type, created_at)
		VALUES (?, 'drama-episode', 1, 'approved', 'drama-script-version', 'user', ?)`,
		id, formatTimeForFixture(now))
	return err
}

// formatTimeForFixture renders a timestamp the storage format accepts.
func formatTimeForFixture(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

// storyboardRecord, storyboardVersionRecord, storyboardItemRecord and storyboardPanelRecord build
// the rows the board is written from.
//
// They are functions rather than literals at each call site so a change to a required field is one
// edit, which is what the storyboard repository's own tests do for the same reason.
func storyboardRecord(id string, now time.Time) storyboard.Storyboard {
	return storyboard.Storyboard{
		ID: id, EpisodeID: "drama-episode", CreatedAt: now, UpdatedAt: now, Revision: 1,
	}
}

func storyboardVersionRecord(id, boardID string, now time.Time) storyboard.StoryboardVersion {
	return storyboard.StoryboardVersion{
		ID: id, StoryboardID: boardID, VersionNumber: 1, Status: versioning.StatusDraft,
		ScriptVersionID: "drama-script-version", DirectorPlanVersionID: "wp11-plan",
		CreatedByType: versioning.CreatedByUser, CreatedAt: now,
	}
}

func storyboardItemRecord(id, versionID, shotID string, ordinal, durationSeconds int, now time.Time) storyboard.StoryboardItem {
	return storyboard.StoryboardItem{
		ID: id, StoryboardVersionID: versionID, ShotID: shotID, Ordinal: ordinal,
		ShotSize: "MS", DurationSeconds: durationSeconds, Status: versioning.StatusDraft,
		CreatedAt: now, UpdatedAt: now, Revision: 1,
	}
}

// storyboardPanelRecord is one panel version, APPROVED with its image pointer set.
//
// The approval and the pointer are written in the same row because that is what the timeline joins on:
// a panel that was approved without naming an asset version would be a shot the timeline reports as
// having no media.
func storyboardPanelRecord(id, itemID, assetVersionID string, now time.Time) storyboard.StoryboardPanelVersion {
	return storyboardPanelRecordAt(id, itemID, assetVersionID, 1, now)
}

// storyboardPanelRecordAt is the same row at a stated version number, for the fixture that REPLACES a
// panel: a row's version numbers are unique, so the replacement is version two rather than a second
// row numbered one.
func storyboardPanelRecordAt(id, itemID, assetVersionID string, versionNumber int, now time.Time) storyboard.StoryboardPanelVersion {
	return storyboard.StoryboardPanelVersion{
		ID: id, StoryboardItemID: itemID, VersionNumber: versionNumber, Status: versioning.StatusApproved,
		ApprovedImageAssetVersionID: assetVersionID,
		CreatedByType:               versioning.CreatedByUser, CreatedAt: now,
	}
}

// rows returns the board's rows, or an empty slice when the version id is empty.
func (h *mediaHarness) rows(t *testing.T, versionID string) []storyboard.StoryboardItem {
	t.Helper()
	if versionID == "" {
		// A caller that did not name a version wants the episode's approved board.
		approved, found, err := h.exports.CurrentBoardVersion(context.Background(), "drama-episode")
		if err != nil || !found {
			return nil
		}
		versionID = approved.ID
	}
	items, err := NewStoryboardRepository(h.db).ListStoryboardItems(context.Background(), versionID)
	if err != nil {
		t.Fatalf("ListStoryboardItems: %v", err)
	}
	return items
}

// draftAndApprove writes a subtitle track covering the named lines and puts it in force.
//
// It writes the cues directly rather than going through the generator, because the export's subject is
// what happens to a track that EXISTS: the generator has its own tests, and this fixture's job is to
// give the export a track whose cues cite the lines it expects.
func (h *mediaHarness) draftAndApprove(t *testing.T, ctx context.Context, lineIDs []string) string {
	t.Helper()
	trackID := "wp11-track"
	track := domainmedia.Track{
		ID: trackID, EpisodeID: "drama-episode", ScriptVersionID: "drama-script-version",
		VersionNumber: 1, Status: "draft", CreatedByType: "user", CreatedAt: h.now,
	}
	cues := make([]domainmedia.Cue, 0, len(lineIDs))
	for index, lineID := range lineIDs {
		cues = append(cues, domainmedia.Cue{
			ID:      "wp11-cue-" + itoaWP10(index+1),
			TrackID: trackID,
			Ordinal: index + 1,
			// Two seconds a line, so a three-line track covers six seconds — which is what the
			// timeline's shots total in these fixtures.
			Start:          domainmedia.Timecode(index * 2000),
			End:            domainmedia.Timecode((index + 1) * 2000),
			Text:           "line " + itoaWP10(index+1),
			DialogueLineID: lineID,
			Status:         domainmedia.CueGenerated,
			CreatedAt:      h.now,
		})
	}
	if err := h.subtitles.CreateTrackWithCues(ctx, track, cues); err != nil {
		t.Fatalf("CreateTrackWithCues: %v", err)
	}
	if _, err := h.db.ExecContext(ctx,
		`UPDATE subtitle_tracks SET status = 'under_review' WHERE id = ?`, trackID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.subtitle.Approve(ctx, trackID, "drama-episode", "trace-subs"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	return trackID
}

// The export service's own interface proofs, so a drift between the harness and the ports fails the
// build rather than a test.
var _ appmedia.FileStore = recordingFileStore{}
var _ appmedia.TempDir = scratchTemp{}
