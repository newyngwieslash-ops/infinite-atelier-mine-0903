package database

import (
	"context"
	"database/sql"
	"strings"

	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/media"
)

// timeline.go is the read the export is assembled from.
//
// It is on the EXPORT repository because the two are one concern: a timeline exists to be exported,
// and the reads it needs are the four joins between a board row, the media approved for it, the audio
// its lines produced and the cues that render it. A separate repository type would be a second
// connection holder for the same four queries.
//
// # The join that did not exist before this package
//
// `storyboard_panel_versions.approved_image_asset_version_id` is where a shot's approved frame lives,
// and nothing had ever read it together with the board's rows, the asset's type and the version's
// primary file. That join is what "which shot has media" means, and every clause of AC-MEDIA-003
// depends on it.

// BoardVersion returns one storyboard version.
func (r *ExportRepository) BoardVersion(ctx context.Context, id string) (appmedia.BoardVersion, error) {
	conn := r.conn()
	if conn == nil {
		return appmedia.BoardVersion{}, media.StorageError("The storyboard store is unavailable.", nil)
	}
	var version appmedia.BoardVersion
	err := conn.QueryRowContext(ctx, `SELECT id, storyboard_id, version_number, status, script_version_id
		FROM storyboard_versions WHERE id = ?`, id).
		Scan(&version.ID, new(string), &version.VersionNumber, &version.Status, &version.ScriptVersionID)
	if err != nil {
		if err == sql.ErrNoRows {
			return appmedia.BoardVersion{}, media.NotFoundError()
		}
		return appmedia.BoardVersion{}, media.StorageError("The storyboard version could not be read.", err)
	}
	// The episode comes through the storyboard identity, which is the row that names it.
	if err := conn.QueryRowContext(ctx, `SELECT episode_id FROM storyboards WHERE id =
		(SELECT storyboard_id FROM storyboard_versions WHERE id = ?)`, id).Scan(&version.EpisodeID); err != nil {
		return appmedia.BoardVersion{}, media.StorageError("The storyboard's episode could not be read.", err)
	}
	return version, nil
}

// CurrentBoardVersion returns an episode's approved storyboard version, or found=false.
func (r *ExportRepository) CurrentBoardVersion(ctx context.Context, episodeID string) (appmedia.BoardVersion, bool, error) {
	conn := r.conn()
	if conn == nil {
		return appmedia.BoardVersion{}, false, media.StorageError("The storyboard store is unavailable.", nil)
	}
	var version appmedia.BoardVersion
	err := conn.QueryRowContext(ctx, `SELECT v.id, v.version_number, v.status, v.script_version_id
		FROM storyboard_versions v
		JOIN storyboards s ON s.id = v.storyboard_id
		WHERE s.episode_id = ? AND v.status = 'approved'`, episodeID).
		Scan(&version.ID, &version.VersionNumber, &version.Status, &version.ScriptVersionID)
	if err != nil {
		if err == sql.ErrNoRows {
			return appmedia.BoardVersion{}, false, nil
		}
		return appmedia.BoardVersion{}, false, media.StorageError("The approved storyboard could not be read.", err)
	}
	version.EpisodeID = strings.TrimSpace(episodeID)
	return version, true, nil
}

// BoardFacts returns a board version's rows in ordinal order with the media approved for each.
//
// # One query rather than one per row
//
// The joins are LEFT because a row with no approved panel is the ordinary state of a board under
// construction, and an inner join would silently DROP those rows — making a board of twelve rows with
// four approved report as eight shots, which is a timeline that lost half the episode. The caller
// counts the nulls instead, and that count is what refuses an export.
//
// The audio column counts approved audio versions whose lines fall in the row's scene. It is a
// boolean because the timeline reports COMPLETENESS; the export reads the actual files through the
// same join when it needs them.
func (r *ExportRepository) BoardFacts(ctx context.Context, storyboardVersionID string) ([]appmedia.BoardRow, error) {
	conn := r.conn()
	if conn == nil {
		return nil, media.StorageError("The storyboard store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, `SELECT
			i.id, i.shot_id, i.ordinal, i.duration_seconds,
			COALESCE(p.id, '') AS panel_id,
			COALESCE(p.approved_image_asset_version_id, '') AS approved_version_id,
			COALESCE(a.asset_type, '') AS media_kind,
			COALESCE(f.file_hash, '') AS media_hash,
			(SELECT COUNT(*) FROM asset_usages au
				JOIN assets aa ON aa.id = (SELECT asset_id FROM asset_versions WHERE id = au.asset_version_id)
				WHERE au.consumer_type = 'shot' AND au.consumer_id = i.shot_id
				  AND aa.asset_type = 'audio'
				  AND au.asset_version_id = aa.current_approved_version_id) AS audio_count
		FROM storyboard_items i
		LEFT JOIN storyboard_panel_versions p
			ON p.storyboard_item_id = i.id AND p.status = 'approved'
		LEFT JOIN asset_versions v ON v.id = p.approved_image_asset_version_id
		LEFT JOIN assets a ON a.id = v.asset_id
		LEFT JOIN asset_files f ON f.asset_version_id = v.id AND f.role = 'primary'
		WHERE i.storyboard_version_id = ?
		ORDER BY i.ordinal`, storyboardVersionID)
	if err != nil {
		return nil, media.StorageError("The storyboard rows could not be read.", err)
	}
	defer rows.Close()
	facts := []appmedia.BoardRow{}
	for rows.Next() {
		var row appmedia.BoardRow
		var audioCount int
		if err := rows.Scan(&row.ItemID, &row.ShotID, &row.Ordinal, &row.DurationSecs,
			&row.PanelVersionID, &row.ApprovedVersionID, &row.MediaKind, &row.MediaHash,
			&audioCount); err != nil {
			return nil, media.StorageError("The storyboard rows could not be read.", err)
		}
		row.AudioApproved = audioCount > 0
		facts = append(facts, row)
	}
	if err := rows.Err(); err != nil {
		return nil, media.StorageError("The storyboard rows could not be read.", err)
	}
	return facts, nil
}

// ApprovedCues returns an episode's approved subtitle cues in order, with how many spoken lines the
// track does not cover.
//
// The missing count is computed here rather than by the caller because the join is the same one
// `SubtitleService.Missing` makes, and a second implementation of it would be a second answer to
// "which lines have no subtitle".
func (r *ExportRepository) ApprovedCues(ctx context.Context, episodeID string) ([]media.Cue, int, error) {
	conn := r.conn()
	if conn == nil {
		return nil, 0, media.StorageError("The subtitle store is unavailable.", nil)
	}
	var trackID, scriptVersionID string
	err := conn.QueryRowContext(ctx, `SELECT id, script_version_id FROM subtitle_tracks
		WHERE episode_id = ? AND status = 'approved'`, episodeID).Scan(&trackID, &scriptVersionID)
	if err != nil {
		if err == sql.ErrNoRows {
			// No approved track is a state, not an error: an episode exported without subtitles is a
			// legal export, and the Final Ruleset is where its absence is reported.
			return []media.Cue{}, 0, nil
		}
		return nil, 0, media.StorageError("The approved subtitle track could not be read.", err)
	}
	cues, err := NewSubtitleRepository(r.db).ListCues(ctx, trackID)
	if err != nil {
		return nil, 0, err
	}
	// How many spoken lines the track does not cover. The count is the same rule the subtitle service
	// applies, expressed as SQL so the timeline does not need the script's whole structure.
	var spoken, covered int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM dialogue_lines l
		JOIN scenes s ON s.id = l.scene_id
		WHERE s.script_version_id = ? AND l.line_type IN ('dialogue', 'narration')`,
		scriptVersionID).Scan(&spoken); err != nil {
		return nil, 0, media.StorageError("The script's spoken lines could not be counted.", err)
	}
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(DISTINCT dialogue_line_id) FROM subtitle_cues
		WHERE track_id = ? AND dialogue_line_id <> ''`, trackID).Scan(&covered); err != nil {
		return nil, 0, media.StorageError("The track's covered lines could not be counted.", err)
	}
	missing := spoken - covered
	if missing < 0 {
		missing = 0
	}
	return cues, missing, nil
}
