package database

import (
	"context"
	"database/sql"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/media"
)

// subtitle.go is the storage of the subtitle aggregate (migration 000020).
//
// A TRACK AND ITS CUES ARE WRITTEN IN ONE TRANSACTION, which is the rule migration 000020's shape
// implies rather than a nicety: an approved track with no cues is a subtitle file with nothing in it,
// and the Final Ruleset's "每个 dialogue/narration 行有对应 cue" would report every line as missing
// against a track that is merely empty. The same transaction is what makes AC-MEDIA-002's editability
// safe: an edit that replaced a track's cues without writing the new ones would lose the subtitles the
// user was editing.

// SubtitleRepository stores subtitle tracks and their cues.
type SubtitleRepository struct {
	db *sql.DB
	tx *sql.Tx
}

// NewSubtitleRepository builds a repository over a connection.
func NewSubtitleRepository(db *sql.DB) *SubtitleRepository {
	return &SubtitleRepository{db: db}
}

// WithinTx returns a repository bound to a transaction.
func (r *SubtitleRepository) WithinTx(tx *sql.Tx) *SubtitleRepository {
	return &SubtitleRepository{db: r.db, tx: tx}
}

func (r *SubtitleRepository) conn() querier {
	if r == nil {
		return nil
	}
	return connection(r.db, r.tx)
}

// withinTx runs fn inside a transaction of this repository's database.
//
// The same helper the asset and memory repositories state, repeated because the three are separate
// types over the same connection and Go has no shared base class. It is NOT a second implementation
// of a rule: the rule is "begin, run, commit" and there is nothing to disagree about.
func (r *SubtitleRepository) withinTx(ctx context.Context, fn func(repo *SubtitleRepository) error) error {
	if r == nil || r.db == nil {
		return media.StorageError("The subtitle store is unavailable.", nil)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return media.StorageError("The subtitle track could not be saved.", err)
	}
	if err := fn(r.WithinTx(tx)); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return media.StorageError("The subtitle track could not be saved.", err)
	}
	return nil
}

const subtitleTrackSelectColumns = `SELECT id, episode_id, script_version_id, version_number, status,
	based_on_version_id, source_agent_run_id, created_by_type, created_by_id, change_reason,
	created_at FROM subtitle_tracks`

const subtitleCueSelectColumns = `SELECT id, track_id, ordinal, start_ms, end_ms, text,
	character_entity_id, dialogue_line_id, status, created_at FROM subtitle_cues`

// CreateTrackWithCues stores a track and its cues in one transaction.
func (r *SubtitleRepository) CreateTrackWithCues(ctx context.Context, track media.Track, cues []media.Cue) error {
	if err := track.Validate(); err != nil {
		return err
	}
	for _, cue := range cues {
		if err := cue.Validate(); err != nil {
			return err
		}
		// A cue that names a different track than the one being written is a caller mistake, and
		// learning it here is better than writing rows whose parent is elsewhere.
		if cue.TrackID != track.ID {
			return media.InvalidError("A cue belongs to a different track than the one being written.")
		}
	}
	return r.withinTx(ctx, func(repo *SubtitleRepository) error {
		conn := repo.conn()
		if conn == nil {
			return media.StorageError("The subtitle store is unavailable.", nil)
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO subtitle_tracks
			(id, episode_id, script_version_id, version_number, status, based_on_version_id,
			 source_agent_run_id, created_by_type, created_by_id, change_reason, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			track.ID, track.EpisodeID, track.ScriptVersionID, track.VersionNumber, track.Status,
			track.BasedOnVersionID, track.SourceAgentRunID, track.CreatedByType, track.CreatedByID,
			track.ChangeReason, formatTime(track.CreatedAt)); err != nil {
			if isUniqueViolation(err) {
				return media.ConflictError("That subtitle track version already exists for this episode.")
			}
			if isForeignKeyViolation(err) {
				return media.InvalidError("A subtitle track must name an existing episode and script version.")
			}
			return media.StorageError("The subtitle track could not be saved.", err)
		}
		for _, cue := range cues {
			if _, err := conn.ExecContext(ctx, `INSERT INTO subtitle_cues
				(id, track_id, ordinal, start_ms, end_ms, text, character_entity_id, dialogue_line_id,
				 status, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				cue.ID, cue.TrackID, cue.Ordinal, int64(cue.Start), int64(cue.End), cue.Text,
				cue.CharacterEntityID, cue.DialogueLineID, cueStatusOf(cue.Status),
				formatTime(cue.CreatedAt)); err != nil {
				if isUniqueViolation(err) {
					return media.ConflictError("That cue position is already used in this track.")
				}
				return media.StorageError("The subtitle cue could not be saved.", err)
			}
		}
		return nil
	})
}

// GetTrack returns one track.
func (r *SubtitleRepository) GetTrack(ctx context.Context, id string) (media.Track, error) {
	conn := r.conn()
	if conn == nil {
		return media.Track{}, media.StorageError("The subtitle store is unavailable.", nil)
	}
	track, err := scanSubtitleTrack(conn.QueryRowContext(ctx, subtitleTrackSelectColumns+` WHERE id = ?`, id))
	if err != nil {
		if err == sql.ErrNoRows {
			return media.Track{}, media.NotFoundError()
		}
		return media.Track{}, media.StorageError("The subtitle track could not be read.", err)
	}
	return track, nil
}

// ListTracks returns an episode's tracks newest version first.
func (r *SubtitleRepository) ListTracks(ctx context.Context, episodeID string) ([]media.Track, error) {
	conn := r.conn()
	if conn == nil {
		return nil, media.StorageError("The subtitle store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, subtitleTrackSelectColumns+
		` WHERE episode_id = ? ORDER BY version_number DESC`, episodeID)
	if err != nil {
		return nil, media.StorageError("The subtitle tracks could not be read.", err)
	}
	defer rows.Close()
	tracks := []media.Track{}
	for rows.Next() {
		track, err := scanSubtitleTrack(rows)
		if err != nil {
			return nil, media.StorageError("The subtitle tracks could not be read.", err)
		}
		tracks = append(tracks, track)
	}
	if err := rows.Err(); err != nil {
		return nil, media.StorageError("The subtitle tracks could not be read.", err)
	}
	return tracks, nil
}

// ListCues returns a track's cues in ordinal order.
//
// The order is the STORED ordinal rather than a recomputation, because the ordinal is what a
// subtitle file's sequence is: a reader that sorted by start time would produce a different file from
// the one the ordinals describe, and a track with an overlap is exactly where the two disagree.
func (r *SubtitleRepository) ListCues(ctx context.Context, trackID string) ([]media.Cue, error) {
	conn := r.conn()
	if conn == nil {
		return nil, media.StorageError("The subtitle store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, subtitleCueSelectColumns+
		` WHERE track_id = ? ORDER BY ordinal`, trackID)
	if err != nil {
		return nil, media.StorageError("The subtitle cues could not be read.", err)
	}
	defer rows.Close()
	cues := []media.Cue{}
	for rows.Next() {
		cue, err := scanSubtitleCue(rows)
		if err != nil {
			return nil, media.StorageError("The subtitle cues could not be read.", err)
		}
		cues = append(cues, cue)
	}
	if err := rows.Err(); err != nil {
		return nil, media.StorageError("The subtitle cues could not be read.", err)
	}
	return cues, nil
}

// MaxTrackVersionNumber reports the highest version number an episode's tracks reach.
func (r *SubtitleRepository) MaxTrackVersionNumber(ctx context.Context, episodeID string) (int, error) {
	conn := r.conn()
	if conn == nil {
		return 0, media.StorageError("The subtitle store is unavailable.", nil)
	}
	var highest sql.NullInt64
	if err := conn.QueryRowContext(ctx,
		`SELECT MAX(version_number) FROM subtitle_tracks WHERE episode_id = ?`, episodeID).Scan(&highest); err != nil {
		return 0, media.StorageError("The subtitle track version could not be read.", err)
	}
	if !highest.Valid {
		return 0, nil
	}
	return int(highest.Int64), nil
}

// CurrentApprovedTrack returns the episode's approved track, or found=false.
func (r *SubtitleRepository) CurrentApprovedTrack(ctx context.Context, episodeID string) (media.Track, bool, error) {
	conn := r.conn()
	if conn == nil {
		return media.Track{}, false, media.StorageError("The subtitle store is unavailable.", nil)
	}
	track, err := scanSubtitleTrack(conn.QueryRowContext(ctx, subtitleTrackSelectColumns+
		` WHERE episode_id = ? AND status = 'approved'`, episodeID))
	if err != nil {
		if err == sql.ErrNoRows {
			return media.Track{}, false, nil
		}
		return media.Track{}, false, media.StorageError("The subtitle track could not be read.", err)
	}
	return track, true, nil
}

// ApproveTrack switches which track is approved.
//
// The replaced track is superseded FIRST so the partial unique index never sees two approved rows,
// which is the same order the asset gap report's approval uses. One transaction, because a track
// whose status says approved and whose trace was never written is an approval with nothing connecting
// it to the decision that made it.
func (r *SubtitleRepository) ApproveTrack(ctx context.Context, trackID, episodeID, traceID string, at time.Time) error {
	return r.withinTx(ctx, func(repo *SubtitleRepository) error {
		conn := repo.conn()
		if conn == nil {
			return media.StorageError("The subtitle store is unavailable.", nil)
		}
		if _, err := conn.ExecContext(ctx, `UPDATE subtitle_tracks SET status = 'superseded'
			WHERE episode_id = ? AND status = 'approved'`, episodeID); err != nil {
			return media.StorageError("The previous subtitle track could not be superseded.", err)
		}
		result, err := conn.ExecContext(ctx, `UPDATE subtitle_tracks SET status = 'approved'
			WHERE id = ? AND status = 'under_review'`, trackID)
		if err != nil {
			return media.StorageError("The subtitle track could not be approved.", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return media.StorageError("The subtitle track could not be approved.", err)
		}
		if affected == 0 {
			// Nothing moved, so the caller's copy of the status was stale.
			return media.ConflictError("This subtitle track changed in another window. Reload it and try again.")
		}
		return nil
	})
}

// ReplaceCues rewrites a track's cues in one transaction.
//
// It is how an EDIT persists: AC-MEDIA-002 asks that a subtitle be editable, and an edit that
// deleted the old cues and failed before writing the new ones would leave the user with nothing. One
// transaction, so the track either has its old cues or its new ones.
//
// The cues are inserted with their ORDINALS AS GIVEN rather than renumbered from the array, because
// the ordinal is the cue's position in the file and a caller that removed the second of three has to
// say so. ValidateTrack is what refuses a gap.
func (r *SubtitleRepository) ReplaceCues(ctx context.Context, trackID string, cues []media.Cue) error {
	for _, cue := range cues {
		if err := cue.Validate(); err != nil {
			return err
		}
		if cue.TrackID != trackID {
			return media.InvalidError("A cue belongs to a different track than the one being rewritten.")
		}
	}
	for _, issue := range media.ValidateTrack(cues) {
		return media.InvalidError(issue.Problem)
	}
	return r.withinTx(ctx, func(repo *SubtitleRepository) error {
		conn := repo.conn()
		if conn == nil {
			return media.StorageError("The subtitle store is unavailable.", nil)
		}
		if _, err := conn.ExecContext(ctx, `DELETE FROM subtitle_cues WHERE track_id = ?`, trackID); err != nil {
			return media.StorageError("The subtitle cues could not be rewritten.", err)
		}
		for _, cue := range cues {
			if _, err := conn.ExecContext(ctx, `INSERT INTO subtitle_cues
				(id, track_id, ordinal, start_ms, end_ms, text, character_entity_id, dialogue_line_id,
				 status, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				cue.ID, cue.TrackID, cue.Ordinal, int64(cue.Start), int64(cue.End), cue.Text,
				cue.CharacterEntityID, cue.DialogueLineID, cueStatusOf(cue.Status),
				formatTime(cue.CreatedAt)); err != nil {
				if isUniqueViolation(err) {
					return media.ConflictError("Two cues share a position in this track.")
				}
				return media.StorageError("A subtitle cue could not be saved.", err)
			}
		}
		return nil
	})
}

// scanSubtitleTrack reads one track row.
func scanSubtitleTrack(row rowScanner) (media.Track, error) {
	var track media.Track
	var createdAt string
	if err := row.Scan(&track.ID, &track.EpisodeID, &track.ScriptVersionID, &track.VersionNumber,
		&track.Status, &track.BasedOnVersionID, &track.SourceAgentRunID, &track.CreatedByType,
		&track.CreatedByID, &track.ChangeReason, &createdAt); err != nil {
		return media.Track{}, err
	}
	track.CreatedAt = parseTime(createdAt)
	return track, nil
}

// scanSubtitleCue reads one cue row.
func scanSubtitleCue(row rowScanner) (media.Cue, error) {
	var cue media.Cue
	var startMS, endMS int64
	var status, createdAt string
	if err := row.Scan(&cue.ID, &cue.TrackID, &cue.Ordinal, &startMS, &endMS, &cue.Text,
		&cue.CharacterEntityID, &cue.DialogueLineID, &status, &createdAt); err != nil {
		return media.Cue{}, err
	}
	cue.Start = media.Timecode(startMS)
	cue.End = media.Timecode(endMS)
	cue.Status = media.CueStatus(status)
	cue.CreatedAt = parseTime(createdAt)
	return cue, nil
}

// cueStatusOf defaults a cue's status for the row.
//
// The column's DEFAULT only fires when an INSERT omits it, and this statement names every column.
// A caller that left the status empty gets the generator's rather than a constraint failure about a
// field it did not know existed — and a cue with no status is one no regeneration has been told to
// respect, which is the safe reading of "nobody said".
func cueStatusOf(status media.CueStatus) string {
	if status == "" {
		return string(media.CueGenerated)
	}
	return string(status)
}
