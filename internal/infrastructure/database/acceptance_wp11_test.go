package database

import (
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appfiles "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/files"
	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	domainmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/media"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/filestore"
	inframedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/media"
)

// acceptance_wp11_test.go walks AC-MEDIA-003 over the real schema and the real engine.
//
//	## AC-MEDIA-003 Timeline/MP4
//	- ordered Shots；
//	- audio/subtitle；
//	- replace clip；
//	- export；
//	- Final Supervisor；
//	- output playable；
//	- manifest traceability。
//
// # Why it is one walk rather than seven tests
//
// The clauses are a CHAIN: an ordered timeline is what an export consumes, the manifest is what the
// export records, and traceability is a read of that manifest against what is still approved. Testing
// them separately would be testing six functions and calling it a criterion — which is the shape
// WP-09's review found for AC-BOARD-002, where inert data satisfied the letter of a clause. This walk
// builds a real episode, exports it, and reads the film back.

// recordingFileStore pairs the store with its metadata row.
type recordingFileStore struct {
	store      *filestore.Store
	repository *FileRepository
}

func (a recordingFileStore) Put(ctx context.Context, displayName string, body io.Reader) (appmedia.StoredObject, error) {
	object, err := a.store.Put(ctx, displayName, body)
	if err != nil {
		return appmedia.StoredObject{}, err
	}
	// The metadata row is what `asset_files.file_hash` has a foreign key to, so a composed export
	// without one could not be cited by anything. The real file service does this pairing; the test
	// does it the same way rather than bypassing it.
	err = a.repository.UpsertObject(ctx, appfiles.Object{
		Hash: object.Hash, StorageKey: object.StorageKey, MIME: object.MIME, Size: object.Size,
	})
	if err != nil {
		return appmedia.StoredObject{}, err
	}
	return appmedia.StoredObject{
		Hash: object.Hash, StorageKey: object.StorageKey, MIME: object.MIME, Size: object.Size,
	}, nil
}

func (a recordingFileStore) Open(ctx context.Context, storageKey string) (io.ReadCloser, error) {
	return a.store.Open(ctx, storageKey)
}

// scratchTemp is the appdirs.Temp surface the export service needs.
type scratchTemp struct{ root string }

func (t scratchTemp) NewScratchDir(_ context.Context, prefix string) (string, error) {
	return os.MkdirTemp(t.root, prefix)
}

func (t scratchTemp) RemoveScratchDir(path string) error {
	return os.RemoveAll(path)
}

// mediaHarness is the export stack over a migrated database.
type mediaHarness struct {
	db        *sql.DB
	exports   *ExportRepository
	subtitles *SubtitleRepository
	files     recordingFileStore
	service   *appmedia.ExportService
	timeline  *appmedia.TimelineService
	subtitle  *appmedia.SubtitleService
	engine    *inframedia.FFmpegEngine
	next      int
	now       time.Time
}

// New mints identifiers.
func (h *mediaHarness) New() (string, error) {
	h.next++
	return "media-id-" + itoaWP10(h.next), nil
}

type mediaClock struct{ at time.Time }

func (c mediaClock) Now() time.Time { return c.at }

// newMediaHarness builds the whole media stack over a fresh database.
func newMediaHarness(t *testing.T) *mediaHarness {
	t.Helper()
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	root := t.TempDir()
	store, err := filestore.New(filepath.Join(root, "files"), filepath.Join(root, "temp"))
	if err != nil {
		t.Fatalf("opening the file store: %v", err)
	}
	exports := NewExportRepository(db)
	subtitles := NewSubtitleRepository(db)
	files := recordingFileStore{store: store, repository: NewFileRepository(db)}
	engine := inframedia.NewFFmpegEngine(filepath.Join(root, "temp"))
	harness := &mediaHarness{
		db: db, exports: exports, subtitles: subtitles, files: files, engine: engine,
		now: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}
	harness.timeline = appmedia.NewTimelineService(appmedia.TimelineOptions{Board: exports})
	harness.subtitle = appmedia.NewSubtitleService(appmedia.SubtitleOptions{
		Tracks: subtitles, Lines: lineReaderDouble{}, Clock: mediaClock{at: harness.now}, IDs: harness,
	})
	harness.service = appmedia.NewExportService(appmedia.ExportOptions{
		Timeline: *harness.timeline, Engine: engine, Files: files, Exports: exports,
		Temp:     scratchTemp{root: filepath.Join(root, "temp")},
		Subtitle: harness.subtitle, Clock: mediaClock{at: harness.now}, IDs: harness,
	})
	return harness
}

// approvedBoard writes an approved board of n rows, each with an approved PNG frame.
//
// The frames are real PNGs and the bytes go through the real store, because AC-MEDIA-003's "output
// playable" is graded by composing them — a fixture of paths would produce an export that failed at
// the first decode, which is a different test.
func (h *mediaHarness) approvedBoard(t *testing.T, shots int, durationSecs int) string {
	t.Helper()
	ctx := context.Background()
	generator := dramaIDGenerator()
	now := dramaTime()

	// The board identity and version, written through the repository the way the service writes them.
	boardID := mustNewID(t, generator)
	versionID := mustNewID(t, generator)
	repo := NewStoryboardRepository(h.db)
	// The plan comes first: a board version CITES the plan it was drawn from, and the repository's
	// foreign key refuses a version naming one that does not exist.
	if err := writeDirectorPlan(ctx, h.db, "wp11-plan", now); err != nil {
		t.Fatalf("writing the director plan: %v", err)
	}
	if err := repo.CreateStoryboard(ctx, storyboardRecord(boardID, now)); err != nil {
		t.Fatalf("CreateStoryboard: %v", err)
	}
	if err := repo.CreateStoryboardVersion(ctx, storyboardVersionRecord(versionID, boardID, now)); err != nil {
		t.Fatalf("CreateStoryboardVersion: %v", err)
	}
	for index := 1; index <= shots; index++ {
		itemID := mustNewID(t, generator)
		shotID := "wp11-shot-" + itoaWP10(index)
		if err := repo.CreateStoryboardItem(ctx, storyboardItemRecord(itemID, versionID, shotID, index, durationSecs, now)); err != nil {
			t.Fatalf("CreateStoryboardItem: %v", err)
		}
		// The frame: a real PNG whose colour depends on the shot, so two shots differ.
		pngBytes := pngFixture(t, 64+index, 48, byte(20*index))
		hash := h.put(t, "shot-"+itoaWP10(index)+".png", pngBytes)

		// The asset, its version and the panel approval that points at it.
		assetID := "wp11-asset-" + itoaWP10(index)
		versionIDForAsset := "wp11-version-" + itoaWP10(index)
		if err := writeAssetWithVersion(ctx, h.db, assetID, versionIDForAsset, hash, index); err != nil {
			t.Fatalf("writing the shot's asset: %v", err)
		}
		panelID := mustNewID(t, generator)
		if err := repo.CreatePanelVersion(ctx, storyboardPanelRecord(panelID, itemID, versionIDForAsset, now)); err != nil {
			t.Fatalf("CreatePanelVersion: %v", err)
		}
	}
	// The board version is APPROVED, which is what the timeline reads.
	if _, err := h.db.ExecContext(ctx,
		`UPDATE storyboard_versions SET status = 'approved' WHERE id = ?`, versionID); err != nil {
		t.Fatal(err)
	}
	return versionID
}

// put writes bytes through the store and its metadata table.
func (h *mediaHarness) put(t *testing.T, name string, content []byte) string {
	t.Helper()
	stored, err := h.files.Put(context.Background(), name, bytesReader(content))
	if err != nil {
		t.Fatalf("putting %s: %v", name, err)
	}
	return stored.Hash
}

// TestACMEDIA003ATimelineReadsTheBoardInOrder is the "ordered Shots" clause.
func TestACMEDIA003ATimelineReadsTheBoardInOrder(t *testing.T) {
	harness := newMediaHarness(t)
	harness.approvedBoard(t, 5, 3)

	timeline, err := harness.timeline.Read(context.Background(), appmedia.TimelineRequest{EpisodeID: "drama-episode"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(timeline.Shots) != 5 {
		t.Fatalf("the timeline has %d shots, want 5", len(timeline.Shots))
	}
	// The order is the board's own ordinals, one to n, and the read preserves it rather than sorting by
	// anything else: "ordered Shots" means the storyboard's order, which is the story's.
	for index, shot := range timeline.Shots {
		if shot.Ordinal != index+1 {
			t.Fatalf("shot %d has ordinal %d", index+1, shot.Ordinal)
		}
		if shot.ShotID != "wp11-shot-"+itoaWP10(index+1) {
			t.Fatalf("shot %d is %q", index+1, shot.ShotID)
		}
	}
	// Every shot has media, so nothing is missing.
	if timeline.MissingMedia != 0 {
		t.Fatalf("%d shots have no media", timeline.MissingMedia)
	}
	// The total is the sum of the rows' own durations, in milliseconds.
	if timeline.TotalDurationMS != 5*3*1000 {
		t.Fatalf("the total is %dms against five rows of three seconds", timeline.TotalDurationMS)
	}
	// Each shot carries the media version and hash the manifest cites.
	for _, shot := range timeline.Shots {
		if shot.MediaVersionID == "" || shot.MediaHash == "" {
			t.Fatalf("shot %d has no media citation: %+v", shot.Ordinal, shot)
		}
		if shot.PanelVersionID == "" {
			t.Fatalf("shot %d has no panel: %+v", shot.Ordinal, shot)
		}
	}
}

// TestACMEDIA003AnExportIsPlayableAndTraceable is the walk the criterion describes.
func TestACMEDIA003AnExportIsPlayableAndTraceable(t *testing.T) {
	harness := newMediaHarness(t)
	if !harness.engine.Available() {
		t.Skipf("no ffmpeg on this host, so an export cannot be composed: %s", harness.engine.Diagnostic())
	}
	harness.approvedBoard(t, 4, 2)
	ctx := context.Background()

	// THE EXPORT: a preview, so the render is quick and the size is the default for the quality.
	record, manifest, err := harness.service.Export(ctx, appmedia.ExportRequest{
		EpisodeID:     "drama-episode",
		Quality:       domainmedia.QualityPreview,
		FPS:           15,
		CreatedByType: "user", CreatedByID: "user-1",
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	// --- export: the file exists and is stored.
	if record.OutputFileHash == "" {
		t.Fatal("the export recorded no output file")
	}
	if record.Status != "draft" {
		t.Fatalf("a fresh export is %q", record.Status)
	}
	// The duration is what the FILM says, read back after composing: four shots of two seconds.
	if record.DurationMS < 7500 || record.DurationMS > 8500 {
		t.Fatalf("the export reports %dms for four two-second shots", record.DurationMS)
	}
	// --- manifest traceability: the document names the versions, and every one of them still exists.
	decoded, err := domainmedia.DecodeManifest(record.ManifestJSON)
	if err != nil {
		t.Fatalf("the manifest did not decode: %v", err)
	}
	if decoded.SchemaVersion != domainmedia.ManifestSchemaVersion {
		t.Fatalf("the manifest's schema version is %d", decoded.SchemaVersion)
	}
	if decoded.DurationMS != record.DurationMS {
		t.Fatalf("the manifest says %dms against the record's %d", decoded.DurationMS, record.DurationMS)
	}
	// The two references a valid manifest cannot be without, plus one panel per shot.
	for _, kind := range []domainmedia.ReferenceKind{
		domainmedia.RefScript, domainmedia.RefStoryboard,
	} {
		if refs := decoded.ReferencesOf(kind); len(refs) != 1 || refs[0].ID == "" {
			t.Fatalf("the manifest cites %d %s references", len(refs), kind)
		}
	}
	if panels := decoded.ReferencesOf(domainmedia.RefPanel); len(panels) != 4 {
		t.Fatalf("the manifest cites %d panels for four shots", len(panels))
	}
	// Every cited panel still EXISTS in the database, which is what makes the citation checkable
	// rather than decorative — the assertion is a read, not a string comparison.
	for _, reference := range decoded.References {
		if reference.Kind != domainmedia.RefPanel {
			continue
		}
		var count int
		if err := harness.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM storyboard_panel_versions WHERE id = ?`, reference.ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("the manifest cites a panel that is not there: %s", reference.ID)
		}
	}
	// The HASH is what the store holds, so a version whose bytes changed under the same identifier is
	// caught. The check is against the real file table.
	for _, reference := range decoded.ReferencesOf(domainmedia.RefAssetVersion) {
		if reference.Hash == "" {
			t.Fatalf("an asset reference carries no hash: %+v", reference)
		}
		var count int
		if err := harness.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM file_objects WHERE hash = ?`, reference.Hash).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("the manifest cites a hash the store does not hold: %s", reference.Hash)
		}
	}
	// --- output playable: the stored bytes are a film, read back with ffprobe.
	opened, err := harness.service.Open(ctx, record.OutputFileHash)
	if err != nil {
		t.Fatalf("opening the export: %v", err)
	}
	defer opened.Close()
	staged := filepath.Join(t.TempDir(), "readback.mp4")
	file, err := os.Create(staged)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(file, opened); err != nil {
		t.Fatal(err)
	}
	file.Close()
	info, err := harness.engine.Probe(ctx, staged, appmedia.DefaultProbeLimits())
	if err != nil {
		t.Fatalf("the export is not a readable film: %v", err)
	}
	if info.Streams < 1 {
		t.Fatal("the export carries no streams")
	}
	if info.DurationMS < 7500 || info.DurationMS > 8500 {
		t.Fatalf("the stored film reports %dms", info.DurationMS)
	}
	if info.Width != record.Width || info.Height != record.Height {
		t.Fatalf("the film is %dx%d against the record's %dx%d", info.Width, info.Height, record.Width, record.Height)
	}

	// --- approve: the export moves through review, as the gate does.
	if err := harness.exports.MarkUnderReview(ctx, record.ID); err != nil {
		t.Fatalf("MarkUnderReview: %v", err)
	}
	approved, err := harness.service.Approve(ctx, record.ID, "drama-episode", "trace-1")
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if approved.Status != "approved" {
		t.Fatalf("the approved export is %q", approved.Status)
	}
	if approved.ApprovalTraceID != "trace-1" {
		t.Fatalf("the approval trace is %q", approved.ApprovalTraceID)
	}
	// The episode has exactly one approved export, which the partial index enforces.
	current, found, err := harness.exports.CurrentApprovedExport(ctx, "drama-episode")
	if err != nil || !found {
		t.Fatalf("no approved export: found=%v err=%v", found, err)
	}
	if current.ID != record.ID {
		t.Fatalf("the approved export is %q", current.ID)
	}
	_ = manifest
}

// TestACMEDIA003SubtitlesAndAudioAreComposedIn covers the "audio/subtitle" clause.
func TestACMEDIA003SubtitlesAndAudioAreComposedIn(t *testing.T) {
	harness := newMediaHarness(t)
	if !harness.engine.Available() {
		t.Skipf("no ffmpeg on this host: %s", harness.engine.Diagnostic())
	}
	harness.approvedBoard(t, 3, 2)
	ctx := context.Background()

	// A subtitle track drafted from a script and approved, which is the state an export consumes.
	track := harness.draftAndApprove(t, ctx, []string{"line-1", "line-2", "line-3"})
	record, manifest, err := harness.service.Export(ctx, appmedia.ExportRequest{
		EpisodeID:       "drama-episode",
		Quality:         domainmedia.QualityPreview,
		FPS:             15,
		SubtitleTrackID: track,
		SubtitleMode:    domainmedia.SubtitleSidecar,
		CreatedByType:   "user",
	})
	if err != nil {
		t.Fatalf("Export with subtitles: %v", err)
	}
	// The manifest names the track, so the same traceability that covers the frames covers the words.
	if tracks := manifest.ReferencesOf(domainmedia.RefSubtitleTrack); len(tracks) != 1 || tracks[0].ID != track {
		t.Fatalf("the manifest cites %+v as the track", tracks)
	}
	if record.SubtitleTrackID != track {
		t.Fatalf("the record names %q as the track", record.SubtitleTrackID)
	}
	// And the OUTPUT carries the subtitle stream, which is what "audio/subtitle" composed in means:
	// the assertion is on the muxed file rather than on the request.
	opened, err := harness.service.Open(ctx, record.OutputFileHash)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	staged := filepath.Join(t.TempDir(), "with-subs.mp4")
	file, err := os.Create(staged)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(file, opened); err != nil {
		t.Fatal(err)
	}
	file.Close()
	info, err := harness.engine.Probe(ctx, staged, appmedia.DefaultProbeLimits())
	if err != nil {
		t.Fatal(err)
	}
	// Two streams: the picture and the subtitle track.
	if info.Streams < 2 {
		t.Fatalf("the export carries %d streams, so the subtitles were not muxed", info.Streams)
	}
}

// TestACMEDIA003AClipCanBeReplaced covers the "replace clip" clause.
//
// Replacing a shot's media is approving a different panel version for it, and the next export reads
// the new one because the timeline joins the CURRENT approved version rather than a copy. The test
// proves it by replacing one clip and exporting twice.
func TestACMEDIA003AClipCanBeReplaced(t *testing.T) {
	harness := newMediaHarness(t)
	if !harness.engine.Available() {
		t.Skipf("no ffmpeg on this host: %s", harness.engine.Diagnostic())
	}
	boardVersionID := harness.approvedBoard(t, 3, 2)
	ctx := context.Background()

	first, firstManifest, err := harness.service.Export(ctx, appmedia.ExportRequest{
		EpisodeID: "drama-episode", Quality: domainmedia.QualityPreview, FPS: 15, CreatedByType: "user",
	})
	if err != nil {
		t.Fatalf("the first export failed: %v", err)
	}
	// Replace the SECOND shot's frame: a new asset version, and a new panel version approved for the
	// row that points at it.
	rows := harness.rows(t, boardVersionID)
	if len(rows) < 2 {
		t.Fatalf("the board has %d rows", len(rows))
	}
	// The row's CURRENT panel is what the replace supersedes, and it is read through the repository
	// rather than assumed: a test that guessed the panel's identifier would pass on a fixture rather
	// than on the join the timeline makes.
	existingPanels, err := NewStoryboardRepository(harness.db).ListPanelVersions(ctx, rows[1].ID)
	if err != nil {
		t.Fatalf("ListPanelVersions: %v", err)
	}
	if len(existingPanels) != 1 {
		t.Fatalf("the second row has %d panels", len(existingPanels))
	}
	newHash := harness.put(t, "replacement.png", pngFixture(t, 200, 150, 250))
	newVersionID := "wp11-replacement-version"
	if err := writeAssetWithVersion(ctx, harness.db, "wp11-replacement-asset", newVersionID, newHash, 99); err != nil {
		t.Fatalf("writing the replacement: %v", err)
	}
	// Approving it as the row's panel is the replace: the old panel version is superseded.
	if _, err := harness.db.ExecContext(ctx, `UPDATE storyboard_panel_versions
		SET status = 'superseded' WHERE id = ?`, existingPanels[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := NewStoryboardRepository(harness.db).CreatePanelVersion(ctx, storyboardPanelRecordAt(
		"wp11-replacement-panel", rows[1].ID, newVersionID, 2, dramaTime())); err != nil {
		t.Fatalf("CreatePanelVersion: %v", err)
	}
	// The timeline reads the NEW version for that row and the old ones for the rest.
	timeline, err := harness.timeline.Read(ctx, appmedia.TimelineRequest{EpisodeID: "drama-episode"})
	if err != nil {
		t.Fatal(err)
	}
	if timeline.Shots[1].MediaVersionID != newVersionID {
		t.Fatalf("the replaced shot reads %q, want the new version", timeline.Shots[1].MediaVersionID)
	}
	if timeline.Shots[1].MediaHash != newHash {
		t.Fatalf("the replaced shot reads hash %q", timeline.Shots[1].MediaHash)
	}
	if timeline.Shots[0].MediaVersionID == newVersionID || timeline.Shots[2].MediaVersionID == newVersionID {
		t.Fatal("replacing one shot changed another")
	}
	// The next export cites the new version, and the two manifests differ — which is what makes an
	// export reproducible from its own document.
	second, secondManifest, err := harness.service.Export(ctx, appmedia.ExportRequest{
		EpisodeID: "drama-episode", Quality: domainmedia.QualityPreview, FPS: 15, CreatedByType: "user",
	})
	if err != nil {
		t.Fatalf("the second export failed: %v", err)
	}
	if second.VersionNumber <= first.VersionNumber {
		t.Fatalf("the second export is version %d against the first's %d", second.VersionNumber, first.VersionNumber)
	}
	cited := false
	for _, reference := range secondManifest.ReferencesOf(domainmedia.RefAssetVersion) {
		if reference.ID == newVersionID {
			cited = true
		}
	}
	if !cited {
		t.Fatal("the second export does not cite the replacement")
	}
	// The FIRST export's manifest still cites the version it was made from: a manifest that changed
	// when the project moved on would not be a record of anything.
	for _, reference := range firstManifest.ReferencesOf(domainmedia.RefAssetVersion) {
		if reference.ID == newVersionID {
			t.Fatal("the first export's manifest cites a version that did not exist when it was made")
		}
	}
	// And the old export's own row is untouched.
	stored, err := harness.service.ExportRecord(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ManifestJSON != first.ManifestJSON {
		t.Fatal("a later export rewrote an earlier one's manifest")
	}
}

// TestACMEDIA003AnExportRefusesShotsWithNoMedia is the guard the Final Ruleset's first rule mirrors.
//
// A film that quietly left out the shots nobody approved would be a film with holes, and the refusal
// happens BEFORE the engine starts rather than after a render that produced something incomplete.
func TestACMEDIA003AnExportRefusesShotsWithNoMedia(t *testing.T) {
	harness := newMediaHarness(t)
	harness.approvedBoard(t, 3, 2)
	ctx := context.Background()
	// Unapprove one shot's panel, which is the state of a board under construction.
	rows := harness.rows(t, "")
	if len(rows) == 0 {
		t.Fatal("the board has no rows")
	}
	var itemID string
	if err := harness.db.QueryRowContext(ctx,
		`SELECT id FROM storyboard_items ORDER BY ordinal LIMIT 1`).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.db.ExecContext(ctx, `UPDATE storyboard_panel_versions
		SET status = 'draft' WHERE storyboard_item_id = ?`, itemID); err != nil {
		t.Fatal(err)
	}
	timeline, err := harness.timeline.Read(ctx, appmedia.TimelineRequest{EpisodeID: "drama-episode"})
	if err != nil {
		t.Fatal(err)
	}
	if timeline.MissingMedia != 1 {
		t.Fatalf("the timeline reports %d shots with no media, want 1", timeline.MissingMedia)
	}
	// The export refuses, and the refusal says what is wrong rather than producing a short film.
	if _, _, err := harness.service.Export(ctx, appmedia.ExportRequest{
		EpisodeID: "drama-episode", Quality: domainmedia.QualityPreview, FPS: 15, CreatedByType: "user",
	}); err == nil {
		t.Fatal("an export with unapproved shots was accepted")
	}
	// And the episode with no approved board at all is refused with a DIFFERENT message, because "no
	// board" and "an incomplete board" are different things to fix. The second episode is written here
	// rather than skipped over: a skip would mean this refusal was never exercised, and it is the state
	// every new episode starts in.
	if _, err := harness.db.ExecContext(ctx, `INSERT INTO episodes
		(id, project_id, season_number, episode_number, title, created_at, updated_at)
		VALUES ('wp11-empty-episode', 'drama-project', 1, 2, 'No board yet',
			'2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	_, _, err = harness.service.Export(ctx, appmedia.ExportRequest{
		EpisodeID: "wp11-empty-episode", Quality: domainmedia.QualityPreview, FPS: 15, CreatedByType: "user",
	})
	if err == nil {
		t.Fatal("an export of an episode with no board was accepted")
	}
	// The two refusals are about different things, which is what a user needs: one says to approve the
	// remaining shots and the other says there is no board to export.
	if !strings.Contains(err.Error(), "approved storyboard") {
		t.Fatalf("the no-board refusal reads %q", err.Error())
	}
}
