package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	appconsistency "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/consistency"
	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	domainmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/media"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/staleness"
)

// final_reader.go is the adapter behind `consistency.FinalReader`: the ONE call that answers
// everything AGENT_CONTRACTS section 11.4's eight clauses compare.
//
// # Why one call rather than a method per rule
//
// Eight reads would see eight moments, and an episode changing between the first and the last would
// produce a report naming a fault that had already been fixed — or, worse, MISSING one that had
// appeared. One call takes one consistent look at the episode.
//
// # What it reads, and what it cannot
//
// Seven of the eight clauses read rows: the board's rows with the media approved for each, the
// approved subtitle track and the lines it does not cover, the file objects the cited hashes name,
// the open staleness marks, the export record with its manifest. The eighth — 黑帧/空帧/静音异常 —
// is partially answerable: the file's SIZE and MIME are rows, so a placeholder and a truncated
// download are visible, while a black frame inside a well-formed video is not, and
// `checkEmptyMedia` states that limitation in its own comment.
//
// The licence clause has no storage in this build at all, so `LicensesChecked` is false and the
// ruleset reports the GAP rather than a clean result. See the field's own comment.

// FinalFactsReader reads one episode's final state.
type FinalFactsReader struct {
	db *sql.DB
	// files reads the file_objects rows the cited hashes name.
	files *FileRepository
}

// NewFinalFactsReader builds the reader over a connection.
func NewFinalFactsReader(db *sql.DB) *FinalFactsReader {
	return &FinalFactsReader{db: db, files: NewFileRepository(db)}
}

// The compile-time proof that this satisfies the ruleset's port.
var _ appconsistency.FinalReader = (*FinalFactsReader)(nil)

// FinalFacts reads everything the Final Ruleset compares, about one episode.
func (r *FinalFactsReader) FinalFacts(ctx context.Context, episodeID string) (appconsistency.FinalFacts, error) {
	if r == nil || r.db == nil {
		return appconsistency.FinalFacts{}, mediaStoreUnavailable()
	}
	episodeID = strings.TrimSpace(episodeID)
	if episodeID == "" {
		return appconsistency.FinalFacts{}, domainmedia.InvalidError("A final review must name its episode.")
	}
	facts := appconsistency.FinalFacts{
		EpisodeID: episodeID,
		// Explicitly false, because this build has nowhere to read a licence from. Setting it here
		// rather than leaving it to a zero value that reads the same is a statement: the ruleset's
		// licence rule branches on it, and a future build that adds the storage flips this line and
		// the per-asset rule starts running.
		LicensesChecked: false,
		Licenses:        map[string]appconsistency.FinalLicense{},
		Files:           map[string]appconsistency.FinalFile{},
	}
	// The episode's own script version, which the duration rule compares against. It comes from the
	// episode row rather than from the board, because an episode whose board was never approved still
	// has a script and the rule about total length is about the EPISODE.
	if err := r.readEpisodeScript(ctx, episodeID, &facts); err != nil {
		return appconsistency.FinalFacts{}, err
	}
	// The approved board. No approved board means there is no ordered film, and the ruleset's own
	// findings are the answer — so this read returning nothing is not an error, it is the state
	// `checkShotMedia` reports as an empty shot list.
	boardID, boardScriptID, found, err := r.readApprovedBoard(ctx, episodeID)
	if err != nil {
		return appconsistency.FinalFacts{}, err
	}
	if found {
		facts.BoardVersionID = boardID
		facts.ScriptVersionID = boardScriptID
		if err := r.readShots(ctx, boardID, &facts); err != nil {
			return appconsistency.FinalFacts{}, err
		}
	}
	// The approved subtitle track and the lines it does not cover.
	if err := r.readSubtitles(ctx, episodeID, &facts); err != nil {
		return appconsistency.FinalFacts{}, err
	}
	// The open staleness marks on this episode and on its artifacts.
	if err := r.readStaleMarks(ctx, episodeID, &facts); err != nil {
		return appconsistency.FinalFacts{}, err
	}
	// The newest export, with its manifest decoded.
	if err := r.readExport(ctx, episodeID, &facts); err != nil {
		return appconsistency.FinalFacts{}, err
	}
	// The file objects every cited hash names. It runs LAST, so it sees every hash the reads above
	// recorded — one query for the whole set rather than one per citation.
	if err := r.readFiles(ctx, &facts); err != nil {
		return appconsistency.FinalFacts{}, err
	}
	return facts, nil
}

// readEpisodeScript records the episode's own script version and the duration it estimates.
func (r *FinalFactsReader) readEpisodeScript(ctx context.Context, episodeID string, facts *appconsistency.FinalFacts) error {
	// `script_versions.estimated_duration_seconds` is the estimate the storyboard ruleset also
	// compares a board's total against, so the two rulesets agree about what the script says. The
	// join goes through the episode's CURRENT script version when one is recorded, which is the
	// version the episode is being made from.
	var estimate sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT sv.estimated_duration_seconds
		FROM episodes e
		JOIN script_versions sv ON sv.id = e.current_script_version_id
		WHERE e.id = ?`, episodeID).Scan(&estimate)
	if err != nil {
		if err == sql.ErrNoRows {
			// An episode with no current script version is a state an episode is in before a script
			// is approved. It is not an error here: the duration rule reports nothing when it has no
			// estimate to compare against, which is the honest answer.
			return nil
		}
		return mediaStorageError(err)
	}
	if estimate.Valid {
		facts.ScriptDurationMS = int(estimate.Int64) * 1000
	}
	return nil
}

// readApprovedBoard returns the episode's approved board and the script version it renders.
func (r *FinalFactsReader) readApprovedBoard(ctx context.Context, episodeID string) (id, scriptVersionID string, found bool, err error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, script_version_id FROM storyboard_versions
		WHERE episode_id = ? AND status = 'approved'`, episodeID)
	if err := row.Scan(&id, &scriptVersionID); err != nil {
		if err == sql.ErrNoRows {
			return "", "", false, nil
		}
		return "", "", false, mediaStorageError(err)
	}
	return id, scriptVersionID, true, nil
}

// readShots records the board's rows with the media and audio approved for each.
//
// The media join is the one `ExportRepository.BoardFacts` makes, repeated here rather than called
// because this read needs four more fields than that one returns — the file's size and MIME, the
// audio's own duration and hash — and widening a port another caller shares would make this
// package's needs that caller's. The two agree by construction about the PANEL join, which is the
// part that could drift: see the comment on `ExportRepository.BoardFacts` for why it is a LEFT join.
func (r *FinalFactsReader) readShots(ctx context.Context, boardVersionID string, facts *appconsistency.FinalFacts) error {
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			i.id, i.shot_id, i.ordinal, i.duration_seconds,
			COALESCE(p.approved_image_asset_version_id, '') AS media_version_id,
			COALESCE(f.file_hash, '') AS media_hash,
			COALESCE(fo.mime_type, '') AS media_mime,
			COALESCE(fo.size_bytes, 0) AS media_bytes,
			COALESCE(a.asset_type, '') AS media_kind,
			COALESCE(au.asset_version_id, '') AS audio_version_id,
			COALESCE(af.file_hash, '') AS audio_hash,
			COALESCE(afo.mime_type, '') AS audio_mime,
			COALESCE(afo.size_bytes, 0) AS audio_bytes
		FROM storyboard_items i
		LEFT JOIN storyboard_panel_versions p
			ON p.storyboard_item_id = i.id AND p.status = 'approved'
		LEFT JOIN asset_versions v ON v.id = p.approved_image_asset_version_id
		LEFT JOIN assets a ON a.id = v.asset_id
		LEFT JOIN asset_files f ON f.asset_version_id = v.id AND f.role = 'primary'
		LEFT JOIN file_objects fo ON fo.hash = f.file_hash
		-- The audio approved for a line in this row's scene. It is a correlated subquery rather than
		-- a second LEFT JOIN because a shot can have several lines and the join would multiply the
		-- rows: this picks ONE approved audio version, which is what "this shot has sound" asks.
		LEFT JOIN asset_usages au ON au.id = (
			SELECT au2.id FROM asset_usages au2
			JOIN asset_versions av2 ON av2.id = au2.asset_version_id
			JOIN assets aa2 ON aa2.id = av2.asset_id
			WHERE au2.consumer_type = 'shot' AND au2.consumer_id = i.shot_id
			  AND aa2.asset_type = 'audio'
			  AND au2.asset_version_id = aa2.current_approved_version_id
			ORDER BY au2.id LIMIT 1)
		LEFT JOIN asset_files af ON af.asset_version_id = au.asset_version_id AND af.role = 'primary'
		LEFT JOIN file_objects afo ON afo.hash = af.file_hash
		WHERE i.storyboard_version_id = ?
		ORDER BY i.ordinal`, boardVersionID)
	if err != nil {
		return mediaStorageError(err)
	}
	defer rows.Close()
	facts.Shots = []appconsistency.FinalShot{}
	for rows.Next() {
		var shot appconsistency.FinalShot
		// The board stores whole seconds and every rule downstream compares milliseconds, so the
		// conversion happens once, here, at the boundary between the schema and the ruleset.
		var durationSecs int
		if err := rows.Scan(&shot.ItemID, &shot.ShotID, &shot.Ordinal, &durationSecs,
			&shot.MediaVersionID, &shot.MediaHash, &shot.VideoMIME, &shot.VideoBytes, &shot.MediaKind,
			&shot.AudioVersionID, &shot.AudioHash, &shot.AudioMIME, &shot.AudioBytes); err != nil {
			return mediaStorageError(err)
		}
		shot.DurationMS = durationSecs * 1000
		// EVERY BOARDED SHOT IS REQUIRED, and that is a statement rather than a default: this build's
		// schema has no per-shot "may be skipped" flag, so a shot a director deliberately left out is
		// expressed by not boarding it. The field exists for a build whose schema does have one, and
		// setting it here is where that build would change its mind.
		shot.Required = true
		facts.Shots = append(facts.Shots, shot)
		facts.TotalDurationMS += shot.DurationMS
	}
	if err := rows.Err(); err != nil {
		return mediaStorageError(err)
	}
	return nil
}

// readSubtitles records the approved track and the spoken lines it does not cover.
func (r *FinalFactsReader) readSubtitles(ctx context.Context, episodeID string, facts *appconsistency.FinalFacts) error {
	var trackID, scriptVersionID string
	err := r.db.QueryRowContext(ctx, `
		SELECT id, script_version_id FROM subtitle_tracks
		WHERE episode_id = ? AND status = 'approved'`, episodeID).Scan(&trackID, &scriptVersionID)
	if err != nil {
		if err == sql.ErrNoRows {
			// No approved track is the state the subtitle rule reports, so it is not an error here.
			return nil
		}
		return mediaStorageError(err)
	}
	facts.SubtitleTrack = trackID
	if err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM subtitle_cues WHERE track_id = ?`, trackID).Scan(&facts.CueCount); err != nil {
		return mediaStorageError(err)
	}
	// The missing lines are the same join `SubtitleService.Missing` makes, run here so the rule has
	// the lines rather than only a count: a finding that says WHICH line is uncovered is a finding a
	// person can act on, and one that says "two lines are missing" is not.
	rows, err := r.db.QueryContext(ctx, `
		SELECT l.id, l.line_type, l.text
		FROM dialogue_lines l
		WHERE l.script_version_id = ?
		  AND l.line_type IN ('dialogue', 'narration')
		  AND l.id NOT IN (SELECT dialogue_line_id FROM subtitle_cues
		                   WHERE track_id = ? AND dialogue_line_id <> '')
		ORDER BY l.ordinal`, scriptVersionID, trackID)
	if err != nil {
		return mediaStorageError(err)
	}
	defer rows.Close()
	facts.MissingLines = []appconsistency.FinalLine{}
	for rows.Next() {
		var line appconsistency.FinalLine
		if err := rows.Scan(&line.LineID, &line.Type, &line.Text); err != nil {
			return mediaStorageError(err)
		}
		facts.MissingLines = append(facts.MissingLines, line)
	}
	if err := rows.Err(); err != nil {
		return mediaStorageError(err)
	}
	return nil
}

// readStaleMarks records the open marks on this episode and its artifacts.
//
// # The three states, and how the table says them
//
// `artifact_staleness` has no status column: a mark is OPEN while `cleared_at` is empty, CLEARED once
// it is not, and WAIVED when the `waived` flag is set. The two are independent — a waived mark is
// still open until somebody clears it — so this read returns an open mark and reports the waiver as
// its own field, which is what section 15.3's "最终导出显示 waiver" needs: a cleared mark was resolved,
// a waived one was accepted, and only the second belongs in a report about the export.
//
// The scope is the episode and the artifacts that belong to it: the marks on the episode row itself,
// on its board versions, on its script versions and on its subtitle tracks. It deliberately does NOT
// walk `artifact_staleness.project_id`, because a project's other episodes' marks are not this
// export's problem.
func (r *FinalFactsReader) readStaleMarks(ctx context.Context, episodeID string, facts *appconsistency.FinalFacts) error {
	rows, err := r.db.QueryContext(ctx, `
		SELECT m.id, m.artifact_type, m.artifact_id, m.waived
		FROM artifact_staleness m
		WHERE m.cleared_at = ''
		  AND (m.artifact_id = ?
		       OR m.artifact_id IN (SELECT id FROM storyboard_versions WHERE episode_id = ?)
		       OR m.artifact_id IN (SELECT id FROM script_versions WHERE episode_id = ?)
		       OR m.artifact_id IN (SELECT id FROM subtitle_tracks WHERE episode_id = ?))
		ORDER BY m.artifact_type, m.artifact_id`, episodeID, episodeID, episodeID, episodeID)
	if err != nil {
		return mediaStorageError(err)
	}
	defer rows.Close()
	facts.StaleMarks = []appconsistency.FinalStaleMark{}
	for rows.Next() {
		var mark appconsistency.FinalStaleMark
		var artifactType string
		var waived int
		if err := rows.Scan(&mark.ID, &artifactType, &mark.EntityID, &waived); err != nil {
			return mediaStorageError(err)
		}
		// The identifier this row's primary key is made of is the pair, and the finding names both:
		// a reader who wants the row needs the type as well as the id.
		mark.ID = artifactType + ":" + mark.EntityID
		mark.Artifact = artifactType
		// The entity type the finding names is the staleness domain's own vocabulary, so a reader who
		// knows `MarkStale` finds the row it is about.
		mark.EntityType = string(staleness.ArtifactType(artifactType))
		mark.Waived = waived != 0
		facts.StaleMarks = append(facts.StaleMarks, mark)
	}
	if err := rows.Err(); err != nil {
		return mediaStorageError(err)
	}
	return nil
}

// readExport records the newest export and decodes its manifest.
func (r *FinalFactsReader) readExport(ctx context.Context, episodeID string, facts *appconsistency.FinalFacts) error {
	var export appconsistency.FinalExport
	var manifestJSON, outputHash, subtitleTrack string
	var width, height, durationMS int
	err := r.db.QueryRowContext(ctx, `
		SELECT id, version_number, quality, width, height, duration_ms, output_file_hash,
		       subtitle_track_id, manifest_json
		FROM episode_exports
		WHERE episode_id = ? AND status <> 'superseded'
		ORDER BY version_number DESC LIMIT 1`, episodeID).Scan(
		&export.ID, &export.VersionNumber, &export.Quality, &width, &height, &durationMS,
		&outputHash, &subtitleTrack, &manifestJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			// No export yet. The ruleset's traceability rule reports that, so it is not an error.
			return nil
		}
		return mediaStorageError(err)
	}
	export.Width, export.Height, export.DurationMS = width, height, durationMS
	export.OutputHash, export.SubtitleTrack = outputHash, subtitleTrack
	export.ManifestVersions = map[string]string{}
	export.ManifestHashes = map[string]string{}
	// The manifest is DECODED rather than compared as text, because the traceability rule compares
	// individual references: a string compare would call a manifest wrong because a timestamp moved.
	//
	// A manifest that will not decode leaves the maps empty and the export in place, which the
	// parameter rule still reads. It does NOT invent references, so the traceability rule sees an
	// export whose shots the manifest does not name and reports nothing about them — which is the
	// honest answer to a document this build cannot read.
	if strings.TrimSpace(manifestJSON) != "" {
		var manifest domainmedia.Manifest
		if err := json.Unmarshal([]byte(manifestJSON), &manifest); err == nil {
			export.ManifestEpisodeID = manifest.EpisodeID
			export.Quality = chooseString(manifest.Quality, export.Quality)
			export.SubtitleMode = manifest.SubtitleMode
			if manifest.Width > 0 {
				export.Width = manifest.Width
			}
			if manifest.Height > 0 {
				export.Height = manifest.Height
			}
			export.FPS = manifest.FPS
			export.ManifestVersions["script"] = manifestReferenceID(manifest, domainmedia.RefScript)
			export.ManifestVersions["board"] = manifestReferenceID(manifest, domainmedia.RefStoryboard)
			export.ManifestVersions["plan"] = manifestReferenceID(manifest, domainmedia.RefDirectorPlan)
			for _, reference := range manifest.References {
				if reference.Kind != domainmedia.RefAssetVersion && reference.Kind != domainmedia.RefPanel {
					continue
				}
				if reference.ShotID == "" {
					continue
				}
				// A panel reference names the PANEL version and carries the asset version in its own
				// hash, so a shot's media is keyed by the shot and its hash by the same key. The
				// ruleset compares both against what is approved now.
				export.ManifestVersions["shot:"+reference.ShotID] = reference.ID
				export.ManifestHashes["shot:"+reference.ShotID] = reference.Hash
			}
			if trackID := manifestReferenceID(manifest, domainmedia.RefSubtitleTrack); trackID != "" {
				export.ManifestVersions["subtitle"] = trackID
			}
		}
	}
	facts.Export = &export
	return nil
}

// readFiles records whether each cited hash has a row, and what that row says.
func (r *FinalFactsReader) readFiles(ctx context.Context, facts *appconsistency.FinalFacts) error {
	hashes := map[string]bool{}
	for _, shot := range facts.Shots {
		if shot.MediaHash != "" {
			hashes[shot.MediaHash] = true
		}
		if shot.AudioHash != "" {
			hashes[shot.AudioHash] = true
		}
	}
	if facts.Export != nil && facts.Export.OutputHash != "" {
		hashes[facts.Export.OutputHash] = true
	}
	// Every cited hash gets an entry, present or not: the ruleset reads `Files[hash].Present` and a
	// missing key must mean the same thing as an absent row. Recording both here keeps the rule from
	// having to know which of the two it is looking at.
	for hash := range hashes {
		facts.Files[hash] = appconsistency.FinalFile{Hash: hash}
	}
	if len(hashes) == 0 {
		return nil
	}
	// ONE query for the whole set, with the hashes bound as parameters. The alternative — a query per
	// hash — would make an episode's cost proportional to its shot count for no benefit, and a
	// concatenated IN list would be SQL built from data, which AGENTS section 8.3 forbids.
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(hashes)), ",")
	args := make([]any, 0, len(hashes))
	for hash := range hashes {
		args = append(args, hash)
	}
	rows, err := r.db.QueryContext(ctx,
		`SELECT hash, mime_type, size_bytes FROM file_objects WHERE hash IN (`+placeholders+`)`, args...)
	if err != nil {
		return mediaStorageError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var hash, mime string
		var size int64
		if err := rows.Scan(&hash, &mime, &size); err != nil {
			return mediaStorageError(err)
		}
		facts.Files[hash] = appconsistency.FinalFile{Hash: hash, MIME: mime, Size: size, Present: true}
	}
	if err := rows.Err(); err != nil {
		return mediaStorageError(err)
	}
	return nil
}

// manifestReferenceID returns the first identifier of one kind in a manifest.
func manifestReferenceID(manifest domainmedia.Manifest, kind domainmedia.ReferenceKind) string {
	for _, reference := range manifest.References {
		if reference.Kind == kind {
			return reference.ID
		}
	}
	return ""
}

// chooseString returns the first non-empty value.
func chooseString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// The two error helpers. They name the MEDIA package's categories because the rules that consume
// this reader are the media ruleset's, and a storage fault must reach the user as "the store could
// not be read" rather than as a finding about the episode.
func mediaStoreUnavailable() error {
	return appmedia.StorageError("The final review's store is unavailable.", nil)
}

func mediaStorageError(cause error) error {
	return appmedia.StorageError("The episode's final state could not be read.", cause)
}
