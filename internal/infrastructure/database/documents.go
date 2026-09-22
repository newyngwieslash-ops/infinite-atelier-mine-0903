package database

import (
	"context"
	"database/sql"

	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	scriptdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/script"
	shotlist "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/shotlist"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/storyboard"
)

// documents.go is the adapter behind `media.DocumentRepository`: the reads ROADMAP item 11's four
// documents are made of.
//
// # Why the adapter is here rather than in the application layer
//
// The facts live in four aggregates — the episode and its project, the script's structure, the board
// with each row's approved panel, and the export records — and the application services do not all
// expose them: `GetScriptStructure` is on the script repository and not on a service method that
// takes a version the caller does not already hold, and the per-row panel join is a storage question
// with no domain rule attached. The infrastructure layer is where those tables already are, and one
// adapter over them is cheaper than four pass-through methods added to four services for one caller.
//
// # What it does NOT contain
//
// No rule. Whether a document may be written, what a format is, and what a rendered line looks like
// are all in `application/media` and `domain/{screenplay,shotlist}`; this file answers what the
// database holds and nothing else.
type DocumentFactsReader struct {
	db *sql.DB
}

// NewDocumentFactsReader builds the reader over a connection.
func NewDocumentFactsReader(db *sql.DB) *DocumentFactsReader {
	return &DocumentFactsReader{db: db}
}

// The compile-time proof that this satisfies the document service's port.
//
// It is against a locally declared interface rather than `appmedia.DocumentRepository` directly, so a
// signature drift fails the build here — the same assertion the storyboard checker and the final
// reader carry, and for the same reason: an earlier version of one of those was a function VALUE,
// which compiles whatever the signature is.
type documentRepositoryPort interface {
	EpisodeFacts(ctx context.Context, episodeID string) (appmedia.DocumentEpisode, error)
	ScriptStructure(ctx context.Context, scriptVersionID string) (scriptdomain.ScriptStructure, error)
	BoardFactsFor(ctx context.Context, storyboardVersionID string) ([]shotlist.Row, storyboard.StoryboardVersion, error)
	LatestExport(ctx context.Context, episodeID string) (appmedia.ExportRecord, bool, error)
}

var _ documentRepositoryPort = (*DocumentFactsReader)(nil)

// EpisodeFacts returns the episode's identity and the versions in force.
func (r *DocumentFactsReader) EpisodeFacts(ctx context.Context, episodeID string) (appmedia.DocumentEpisode, error) {
	if r == nil || r.db == nil {
		return appmedia.DocumentEpisode{}, documentStoreUnavailable()
	}
	var episode appmedia.DocumentEpisode
	// `current_script_version_id` is READ but not relied on, and the distinction matters: NOTHING in
	// this build writes that column — see `FinalFactsReader.readEpisodeScript` for the full account —
	// so a document that resolved its version through it would find no script for every episode. It is
	// carried on the struct because a caller may want to know what the episode POINTS at, and every
	// decision below goes through `approved_script`, which is the version a build actually produces.
	err := r.db.QueryRowContext(ctx, `
		SELECT e.id, p.name, e.title, e.season_number, e.episode_number,
		       e.current_script_version_id,
		       COALESCE((SELECT id FROM script_versions sv
		                 WHERE sv.script_id = s.id AND sv.status = 'approved'), '') AS approved_script,
		       COALESCE((SELECT version_number FROM script_versions sv
		                 WHERE sv.script_id = s.id AND sv.status = 'approved'), 0) AS approved_script_number
		FROM episodes e
		JOIN projects p ON p.id = e.project_id
		LEFT JOIN scripts s ON s.episode_id = e.id
		WHERE e.id = ?`, episodeID).Scan(
		&episode.ID, &episode.ProjectName, &episode.Title, &episode.SeasonNumber, &episode.EpisodeNumber,
		&episode.CurrentScriptVersionID, &episode.ApprovedScriptVersionID, &episode.ApprovedScriptVersionNum)
	if err != nil {
		if err == sql.ErrNoRows {
			return appmedia.DocumentEpisode{}, appmedia.NotFoundError()
		}
		return appmedia.DocumentEpisode{}, documentReadError(err)
	}
	// The board and the subtitle track are separate reads because they hang off different parents: a
	// board version reaches its episode through `storyboards`, and a track names the episode directly.
	// Neither is required — an episode with no board is the state every episode starts in — so a miss
	// is an empty identifier rather than an error.
	err = r.db.QueryRowContext(ctx, `
		SELECT v.id, v.version_number
		FROM storyboard_versions v
		JOIN storyboards s2 ON s2.id = v.storyboard_id
		WHERE s2.episode_id = ? AND v.status = 'approved'`, episodeID).Scan(
		&episode.ApprovedBoardVersionID, &episode.ApprovedBoardVersionNumber)
	if err != nil && err != sql.ErrNoRows {
		return appmedia.DocumentEpisode{}, documentReadError(err)
	}
	err = r.db.QueryRowContext(ctx,
		`SELECT id FROM subtitle_tracks WHERE episode_id = ? AND status = 'approved'`, episodeID).Scan(
		&episode.ApprovedSubtitleTrackID)
	if err != nil && err != sql.ErrNoRows {
		return appmedia.DocumentEpisode{}, documentReadError(err)
	}
	return episode, nil
}

// ScriptStructure returns one script version's scenes, lines and shots in order.
func (r *DocumentFactsReader) ScriptStructure(ctx context.Context, scriptVersionID string) (scriptdomain.ScriptStructure, error) {
	if r == nil || r.db == nil {
		return scriptdomain.ScriptStructure{}, documentStoreUnavailable()
	}
	return NewScriptRepository(r.db).GetScriptStructure(ctx, scriptVersionID)
}

// BoardFactsFor returns a board's rows with the panel and media approved for each.
//
// THE PANEL JOIN IS A LEFT JOIN, and it is the same one the timeline and the Final Ruleset make: a
// row with nothing approved is a row a scheduler needs to SEE rather than one to drop. The role
// filter is `primary`, which is the file a panel's image IS; a reference or a mask belongs to the
// prompt rather than to the frame.
func (r *DocumentFactsReader) BoardFactsFor(ctx context.Context, storyboardVersionID string) ([]shotlist.Row, storyboard.StoryboardVersion, error) {
	if r == nil || r.db == nil {
		return nil, storyboard.StoryboardVersion{}, documentStoreUnavailable()
	}
	version, err := NewStoryboardRepository(r.db).GetStoryboardVersion(ctx, storyboardVersionID)
	if err != nil {
		return nil, storyboard.StoryboardVersion{}, err
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			i.ordinal, i.shot_id, i.shot_size, i.camera_angle, i.camera_movement,
			i.duration_seconds, i.visual_description, i.action_description,
			i.dialogue_audio_summary, i.continuity_notes,
			i.first_frame_description, i.last_frame_description, i.video_motion_description,
			COALESCE(p.id, '') AS panel_version_id,
			COALESCE(p.approved_image_asset_version_id, '') AS approved_version_id
		FROM storyboard_items i
		LEFT JOIN storyboard_panel_versions p
			ON p.storyboard_item_id = i.id AND p.status = 'approved'
		WHERE i.storyboard_version_id = ?
		ORDER BY i.ordinal`, storyboardVersionID)
	if err != nil {
		return nil, storyboard.StoryboardVersion{}, documentReadError(err)
	}
	defer rows.Close()
	items := []shotlist.Row{}
	for rows.Next() {
		var item storyboard.StoryboardItem
		var panelVersionID, approvedVersionID string
		if err := rows.Scan(
			&item.Ordinal, &item.ShotID, &item.ShotSize, &item.CameraAngle, &item.CameraMovement,
			&item.DurationSeconds, &item.VisualDescription, &item.ActionDescription,
			&item.DialogueAudioSummary, &item.ContinuityNotes,
			&item.FirstFrameDescription, &item.LastFrameDescription, &item.VideoMotionDescription,
			&panelVersionID, &approvedVersionID); err != nil {
			return nil, storyboard.StoryboardVersion{}, documentReadError(err)
		}
		// The row is assembled by the DOMAIN's own constructor, so "this row has no approved media" is
		// decided in one place rather than here: a second answer would eventually disagree with the
		// first about a row the film cannot render.
		items = append(items, shotlist.FromItem(item, panelVersionID, approvedVersionID))
	}
	if err := rows.Err(); err != nil {
		return nil, storyboard.StoryboardVersion{}, documentReadError(err)
	}
	return items, version, nil
}

// LatestExport returns the episode's newest export record.
//
// It reads the LIST and takes the first rather than adding a `LIMIT 1` query, because the list is
// already ordered by version descending and an episode's exports are few: a second statement would be
// a second place to get the ordering wrong, and the one that drifted would hand a user the older
// manifest beside the newer film.
func (r *DocumentFactsReader) LatestExport(ctx context.Context, episodeID string) (appmedia.ExportRecord, bool, error) {
	if r == nil || r.db == nil {
		return appmedia.ExportRecord{}, false, documentStoreUnavailable()
	}
	records, err := NewExportRepository(r.db).ListExports(ctx, episodeID)
	if err != nil {
		return appmedia.ExportRecord{}, false, err
	}
	if len(records) == 0 {
		return appmedia.ExportRecord{}, false, nil
	}
	return records[0], true, nil
}

// The two error helpers, which name the MEDIA package's categories because the rules that consume this
// reader are the document service's: a storage fault must reach the user as "the store could not be
// read" rather than as a document that happened to be blank.
func documentStoreUnavailable() error {
	return appmedia.StorageError("The document store is unavailable.", nil)
}

func documentReadError(cause error) error {
	return appmedia.StorageError("The episode's documents could not be read.", cause)
}
