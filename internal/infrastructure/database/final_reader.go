package database

import (
	"context"
	"database/sql"
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
		// THE LICENCE RECORD (T17): migration 000031 gave the asset aggregate
		// its rights columns, so the per-asset licence rule now reads real
		// rows. An asset whose record is still empty reports Present=false —
		// the same "unknown" the audit asks to be visible — while an asset
		// that states a licence carries it with its source.
		LicensesChecked: true,
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
	if err := r.readLicenses(ctx, &facts); err != nil {
		return appconsistency.FinalFacts{}, err
	}
	// The file objects every cited hash names. It runs LAST, so it sees every hash the reads above
	// recorded — one query for the whole set rather than one per citation.
	if err := r.readFiles(ctx, &facts); err != nil {
		return appconsistency.FinalFacts{}, err
	}
	return facts, nil
}

// readEpisodeScript records the script version the episode is being made from, and the duration it
// estimates.
//
// # Why this reads the APPROVED version rather than the episode's pointer
//
// `episodes.current_script_version_id` looks like the field to join through and is not: **nothing in
// this build ever writes it.** `UpdateEpisode` carries it, but the only caller that reaches that
// statement is `UpdateEpisodeStatus`, which sets the STATUS and preserves the pointer it read — so the
// column holds the empty string its migration defaulted to, and a join through it finds no rows for
// every episode. `ApproveScriptVersion` does not touch the episode at all.
//
// The consequence is worth stating because it was silent: a version of this method joined through the
// pointer, so the estimate was never read and the duration rule reported NOTHING — a rule that never
// fires looks exactly like a rule with nothing to say. The `final_episode` walk's assertion that the
// ruleset reports nothing about the walk's own work is what surfaced it, because the duration finding
// appeared where the fixture expected none.
//
// The approved version is what the rule actually wants — "the script this episode is being made
// from" — and it is reachable: `script_versions.status = 'approved'`, which the four version families
// all maintain. An episode with no approved script is the ordinary state before a gate, so a miss is
// not an error.
func (r *FinalFactsReader) readEpisodeScript(ctx context.Context, episodeID string, facts *appconsistency.FinalFacts) error {
	// `script_versions.estimated_duration_seconds` is the estimate the storyboard ruleset also
	// compares a board's total against, so the two rulesets agree about what the script says.
	var estimate sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT sv.estimated_duration_seconds
		FROM episodes e
		JOIN scripts s ON s.episode_id = e.id
		JOIN script_versions sv ON sv.script_id = s.id AND sv.status = 'approved'
		WHERE e.id = ?`, episodeID).Scan(&estimate)
	if err != nil {
		if err == sql.ErrNoRows {
			// No approved script is a state an episode is in before its gate. The duration rule
			// reports nothing when it has no estimate to compare against, which is the honest answer.
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
//
// THE EPISODE IS REACHED THROUGH `storyboards`, and getting that wrong is why this file's first
// version was inert. `storyboard_versions` has no `episode_id`: migration 000010 puts it on
// `storyboards`, and a version names its board. The original statement selected `episode_id` from the
// versions table, which fails to prepare — and because this read is UNCONDITIONAL, it failed for
// every episode, `FinalFacts` returned an error every time, and the stage machine's `if err == nil`
// discarded it. Every one of section 11.4's eight clauses contributed nothing in a real build while
// the ruleset's own unit tests, which drive a double, stayed green.
//
// So this is the join the schema actually has, and `TestFinalFactsReaderReadsTheRealSchema` is the
// test that would have caught the original.
func (r *FinalFactsReader) readApprovedBoard(ctx context.Context, episodeID string) (id, scriptVersionID string, found bool, err error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT v.id, v.script_version_id
		FROM storyboard_versions v
		JOIN storyboards s ON s.id = v.storyboard_id
		WHERE s.episode_id = ? AND v.status = 'approved'`, episodeID)
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
			COALESCE(afo.size_bytes, 0) AS audio_bytes,
			COALESCE(i.video_motion_description, '') AS video_motion,
			COALESCE(v.asset_id, '') AS media_asset_id
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
		var videoMotion, mediaAssetID string
		if err := rows.Scan(&shot.ItemID, &shot.ShotID, &shot.Ordinal, &durationSecs,
			&shot.MediaVersionID, &shot.MediaHash, &shot.VideoMIME, &shot.VideoBytes, &shot.MediaKind,
			&shot.AudioVersionID, &shot.AudioHash, &shot.AudioMIME, &shot.AudioBytes,
			&videoMotion, &mediaAssetID); err != nil {
			return mediaStorageError(err)
		}
		shot.DurationMS = durationSecs * 1000
		// THE MOTION MARK (T15): a row the board authored with a video motion
		// description is a shot the export must MOVE, so its approved media
		// must be a video rather than a frame.
		shot.RequiresVideo = strings.TrimSpace(videoMotion) != ""
		// THE LICENCE SUBJECT (T17): the shot's approved media asset is the
		// primary asset whose rights the export needs — its derivation
		// parents (characters, locations) ride the licence chain later; this
		// record is the floor.
		shot.LicenseAssetIDs = []string{}
		if mediaAssetID != "" {
			shot.LicenseAssetIDs = append(shot.LicenseAssetIDs, mediaAssetID)
		}
		// EVERY BOARDED SHOT IS REQUIRED, and in THIS build that is a constant rather than a reading.
		//
		// The schema has no per-shot "may be skipped" column, so a shot a director left out is
		// expressed by not boarding it, and `FinalShot.Required` is therefore always true here. The
		// ruleset branches on the field rather than assuming, which is what keeps the decision in one
		// place — but the field's own comment describes a build whose schema HAS such a column, and
		// this one does not. Saying so here is the difference between a field that is constant and a
		// field that looks configurable and is not.
		shot.Required = true
		facts.Shots = append(facts.Shots, shot)
		facts.TotalDurationMS += shot.DurationMS
	}
	if err := rows.Err(); err != nil {
		return mediaStorageError(err)
	}
	// PER-LINE AUDIO COVERAGE (T15): for each spoken line of the episode's
	// script version, does an approved audio take exist whose asset was keyed
	// on THAT line? The link is the audio asset the collection created per
	// line instance (T01's isolation rule named for the line's job entity),
	// matched here by the line's own text-to-asset naming — but names are
	// prose, not keys, so the durable link is the USAGE: an approved dialogue
	// usage on the line's SHOT whose version's job entity IS the line. The
	// job row records which line it rendered (`entity_type='dialogue_line'`),
	// and the version carries that job id.
	if facts.ScriptVersionID != "" {
		if err := r.readLineCoverage(ctx, facts.ScriptVersionID, &facts.Shots); err != nil {
			return err
		}
	}
	return nil
}

// readLineCoverage counts, per shot, the spoken lines with and without their
// own approved audio, naming the uncovered ones.
//
// One query, one pass: the lines of the script version join their SHOT
// through the scene's ordinal mapping the board carries is NOT available in
// the schema (shots and lines meet only through scenes), so coverage is
// attached at the EPISODE level instead of the shot level — the audit's
// requirement is per-LINE locating, which `UncoveredLines` delivers without
// inventing a shot-line mapping the schema does not state.
func (r *FinalFactsReader) readLineCoverage(ctx context.Context, scriptVersionID string, shots *[]appconsistency.FinalShot) error {
	rows, err := r.db.QueryContext(ctx, `
		SELECT l.id, l.line_type, l.text, EXISTS (
			SELECT 1
			FROM asset_versions av
			JOIN generation_jobs j ON j.id = av.generation_job_id
			JOIN assets aa ON aa.id = av.asset_id
			JOIN asset_usages au2 ON au2.asset_version_id = av.id
			WHERE j.entity_type = 'dialogue_line' AND j.entity_id = l.id
			  AND aa.asset_type = 'audio'
			  AND au2.consumer_type = 'shot'
			  AND av.status = 'approved'
		) AS has_audio
		FROM dialogue_lines l
		JOIN scenes sc ON sc.id = l.scene_id
		WHERE sc.script_version_id = ?
		  AND l.line_type IN ('dialogue', 'narration')
		ORDER BY sc.ordinal, l.ordinal`, scriptVersionID)
	if err != nil {
		return mediaStorageError(err)
	}
	defer rows.Close()
	type lineRef struct {
		line   appconsistency.FinalLine
		hasOne bool
	}
	var lines []lineRef
	for rows.Next() {
		var ref lineRef
		if err := rows.Scan(&ref.line.LineID, &ref.line.Type, &ref.line.Text, &ref.hasOne); err != nil {
			return mediaStorageError(err)
		}
		lines = append(lines, ref)
	}
	if err := rows.Err(); err != nil {
		return mediaStorageError(err)
	}
	if len(lines) == 0 {
		return nil
	}
	// The episode-level counts ride the FIRST shot's record when there is
	// one: `SpokenLineCount` and `CoveredLineCount` are per-EPISODE facts in
	// this schema, and the rule reads them there. The uncovered lines are
	// every line without its own take, so a finding names the line wherever
	// the shot mapping is not stated.
	if len(*shots) > 0 {
		spoken, coveredCount := 0, 0
		uncovered := []appconsistency.FinalLine{}
		for _, ref := range lines {
			spoken++
			if ref.hasOne {
				coveredCount++
				continue
			}
			uncovered = append(uncovered, ref.line)
		}
		(*shots)[0].SpokenLineCount = spoken
		(*shots)[0].CoveredLineCount = coveredCount
		(*shots)[0].UncoveredLines = uncovered
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
	//
	// THE LINE REACHES ITS SCRIPT VERSION THROUGH ITS SCENE, and the first version of this statement
	// selected `l.script_version_id`, a column `dialogue_lines` does not have — migration 000008 puts
	// the version on `scenes` and the line names its scene. The statement therefore failed to prepare.
	// `staleness.go`'s dependent queries make the same join, which is where the correct shape is
	// written down.
	rows, err := r.db.QueryContext(ctx, `
		SELECT l.id, l.line_type, l.text
		FROM dialogue_lines l
		JOIN scenes sc ON sc.id = l.scene_id
		WHERE sc.script_version_id = ?
		  AND l.line_type IN ('dialogue', 'narration')
		  AND l.id NOT IN (SELECT dialogue_line_id FROM subtitle_cues
		                   WHERE track_id = ? AND dialogue_line_id <> '')
		ORDER BY sc.ordinal, l.ordinal`, scriptVersionID, trackID)
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
// # There is no `id` COLUMN, and the first version selected one
//
// Migration 000012's primary key is the PAIR `(artifact_type, artifact_id)` — the same shape the four
// version families use for their own identifiers. This statement selected `m.id`, which does not
// exist, so it failed to prepare and every one of the eight clauses was dead before the adapter's
// first read was even reached. The mark's identity is composed below from the two columns that are
// its key.
//
// The scope is the episode and the artifacts that belong to it: the marks on the episode row itself,
// on its board versions, on its script versions and on its subtitle tracks. It deliberately does NOT
// walk `artifact_staleness.project_id`, because a project's other episodes' marks are not this
// export's problem.
func (r *FinalFactsReader) readStaleMarks(ctx context.Context, episodeID string, facts *appconsistency.FinalFacts) error {
	rows, err := r.db.QueryContext(ctx, `
		SELECT m.artifact_type, m.artifact_id, m.waived
		FROM artifact_staleness m
		WHERE m.cleared_at = ''
		  AND (m.artifact_id = ?
		       OR m.artifact_id IN (SELECT v.id FROM storyboard_versions v
		                            JOIN storyboards s ON s.id = v.storyboard_id
		                            WHERE s.episode_id = ?)
		       OR m.artifact_id IN (SELECT sv2.id FROM script_versions sv2
		                            JOIN scripts sc2 ON sc2.id = sv2.script_id
		                            WHERE sc2.episode_id = ?)
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
		if err := rows.Scan(&artifactType, &mark.EntityID, &waived); err != nil {
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
	// The manifest is read through the DOMAIN'S OWN reader rather than a bare `json.Unmarshal`.
	//
	// # Why that matters, and what was wrong before
	//
	// An earlier version decoded it here with `json.Unmarshal` and swallowed the error. The domain has
	// `DecodeManifest`, written for exactly this caller and documented as such ("the caller is about to
	// compare it against what is currently approved"), and it refuses the three documents a bare decode
	// accepts silently: an empty string, `null` or `{}` — which unmarshal into a ZERO struct with no
	// error — and a document whose schema version is absent, which is the field that distinguishes a
	// real manifest from a zero one. Bypassing it meant every one of those guards was skipped and the
	// traceability rule compared against nothing while looking like it had compared.
	//
	// A manifest that will not decode is still not an error for this READ: the export row exists and
	// the parameter rule reads it. What changes is that the failure is now VISIBLE — `ManifestError`
	// carries the refusal's own sentence, and the traceability rule reports it rather than quietly
	// finding no references to check.
	if strings.TrimSpace(manifestJSON) != "" {
		manifest, err := domainmedia.DecodeManifest(manifestJSON)
		if err != nil {
			export.ManifestError = err.Error()
		} else {
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
			// `ReferencesOf` is the domain's own accessor, so the kinds this reader looks for are the
			// domain's list rather than a loop written here that could drift from it.
			export.ManifestVersions["script"] = firstReferenceID(manifest.ReferencesOf(domainmedia.RefScript))
			export.ManifestVersions["board"] = firstReferenceID(manifest.ReferencesOf(domainmedia.RefStoryboard))
			export.ManifestVersions["plan"] = firstReferenceID(manifest.ReferencesOf(domainmedia.RefDirectorPlan))
			// A SHOT'S MEDIA TRAVELS AS TWO REFERENCES, and the kinds are kept SEPARATE.
			//
			// `ExportService` writes an `asset_version` reference (the media the film was made from)
			// AND a `panel` reference (the panel that media was approved for) for every shot, both
			// carrying the shot's identifier. An earlier version of this loop appended the two lists
			// and keyed them by shot, so the PANEL overwrote the ASSET VERSION — and the traceability
			// rule then compared a panel id against the media version a shot currently approves and
			// reported every export as stale. The walk in `acceptance_wp11_e2e_test.go` is what found
			// it: no unit test compared the two kinds, because each was correct on its own.
			//
			// The ruleset compares the MEDIA version, so `shot:` is the asset version and `panel:` is
			// the panel, each in its own key space.
			for _, reference := range manifest.ReferencesOf(domainmedia.RefAssetVersion) {
				if reference.ShotID == "" {
					continue
				}
				export.ManifestVersions["shot:"+reference.ShotID] = reference.ID
				export.ManifestHashes["shot:"+reference.ShotID] = reference.Hash
			}
			for _, reference := range manifest.ReferencesOf(domainmedia.RefPanel) {
				if reference.ShotID == "" {
					continue
				}
				export.ManifestVersions["panel:"+reference.ShotID] = reference.ID
			}
			if trackID := firstReferenceID(manifest.ReferencesOf(domainmedia.RefSubtitleTrack)); trackID != "" {
				export.ManifestVersions["subtitle"] = trackID
			}
		}
	}
	facts.Export = &export
	return nil
}

// firstReferenceID returns the first reference's identifier, or empty.
func firstReferenceID(references []domainmedia.ManifestReference) string {
	if len(references) == 0 {
		return ""
	}
	return references[0].ID
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

// readLicenses loads the licence record of every asset this episode's shots
// reference (T17). The map is keyed by asset id — the same key the licence
// rule looks its per-asset findings up with — and an asset whose record was
// never filled still appears, with Present=false, so "unknown" is a row the
// report carries rather than a silence.
func (r *FinalFactsReader) readLicenses(ctx context.Context, facts *appconsistency.FinalFacts) error {
	ids := map[string]bool{}
	for _, shot := range facts.Shots {
		for _, assetID := range shot.LicenseAssetIDs {
			if assetID != "" {
				ids[assetID] = true
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	for assetID := range ids {
		var name, license, source, allows string
		err := r.db.QueryRowContext(ctx, `
			SELECT name, license, license_source, allows_export_use
			FROM assets WHERE id = ?`, assetID).Scan(&name, &license, &source, &allows)
		if err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			return mediaStorageError(err)
		}
		present := license != ""
		facts.Licenses[assetID] = appconsistency.FinalLicense{
			AssetID:   assetID,
			Name:      name,
			License:   license,
			Source:    source,
			Present:   present,
			AllowsUse: allows == "1",
		}
	}
	return nil
}
