package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	appimporting "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/importing"
	applegacy "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/legacy"
	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	domainmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/media"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/database"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/filestore"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/jobs"
)

// local_job_handlers.go is T06's composition: the four LOCAL job types FR-150
// names get REAL handlers over the services the startup pass already composed.
//
// # Why composition and not a service inside the runner
//
// The handlers are thin adapters: each one decodes the job's subject, calls
// the SAME application command a binding calls, and reports the result as the
// job's outcome. The application layer owns what a command means; this file
// owns which service answers which job type — a mapping, not a second
// implementation.
//
// # The thumbnail case, and why it is here at all
//
// ADR-0025 dropped MONOFORM's thumbnail path, which left `thumbnail` as a job
// type the schema accepted and nothing ran. The media engine derives a small
// image from the version's primary file, the file store commits it, and the
// version gains a `thumbnail` file link whose unique constraint makes a
// retried write a no-op — the resume story.
//
// # The resume semantics, per type
//
//   - thumbnail: the only side effects are a store put and a DO-NOTHING link,
//     so a requeued run re-derives the same rows.
//   - import: the importer refuses a duplicate source hash (FR-020), so a
//     requeued run is refused with the report the caller already saw.
//   - export: a retried export creates a new export version — the caller's
//     idempotency key is what makes an identical re-submission return the
//     original job instead of running twice.
//   - migration: the legacy import is idempotent by fingerprint and writes in
//     one transaction, so a requeued run either completes or reports the
//     earlier completion.

// composeLocalJobHandlers builds the four handlers over the composed stacks.
// Any stack that is nil leaves the handler it owns nil, which the runner
// reports as an honest refusal for that one type.
func composeLocalJobHandlers(
	handle *database.Handle,
	drama *dramaWiring,
	media *mediaWiring,
	projects *projectWiring,
	store *filestore.Store,
) *jobs.LocalHandlers {
	if handle == nil || handle.SQL() == nil {
		return nil
	}
	handlers := &jobs.LocalHandlers{}

	// THUMBNAIL: the media engine scales the version's primary file.
	if media != nil && media.engine != nil && store != nil {
		handlers.Thumbnail = thumbnailHandler(handle, media, store)
	}

	// EXPORT: one episode's film through the same command the binding calls.
	if media != nil && media.exports != nil {
		handlers.Export = exportHandler(media)
	}

	// MIGRATION: a legacy snapshot import, idempotent by fingerprint.
	if projects != nil && projects.imports != nil && store != nil {
		handlers.Migration = migrationHandler(projects, store)
	}

	// IMPORT: a novel document, imported from its stored source-document
	// bytes — the importer's duplicate refusal makes a requeued run a no-op.
	// The handler is composed LAST because the drama stack owns the importer.
	if drama != nil && drama.importing != nil {
		handlers.Import = importHandler(drama)
	}

	return handlers
}

// thumbnailHandler derives one thumbnail image for one media version.
func thumbnailHandler(handle *database.Handle, media *mediaWiring, store *filestore.Store) jobs.ThumbnailHandler {
	return func(ctx context.Context, request jobs.ThumbnailRequest) (jobs.ThumbnailResult, error) {
		var fileHash string
		if err := handle.SQL().QueryRowContext(ctx,
			`SELECT file_hash FROM asset_files WHERE asset_version_id = ? AND role = 'primary' LIMIT 1`,
			request.AssetVersionID).Scan(&fileHash); err != nil {
			return jobs.ThumbnailResult{}, appmedia.InvalidError(
				"That version has no primary file to derive a thumbnail from.")
		}
		reader, err := store.Open(ctx, fileHash)
		if err != nil {
			return jobs.ThumbnailResult{}, err
		}
		source, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			return jobs.ThumbnailResult{}, err
		}
		width := request.MaxWidthPixels
		if width <= 0 {
			width = 320
		}
		height := request.MaxHeightPixels
		if height <= 0 {
			height = 180
		}
		png, err := media.engine.Thumbnail(ctx, source, width, height)
		if err != nil {
			return jobs.ThumbnailResult{}, err
		}
		object, err := store.Put(ctx, "thumbnail-"+request.AssetVersionID+".png", bytes.NewReader(png))
		if err != nil {
			return jobs.ThumbnailResult{}, err
		}
		// The version gains the link; the schema's unique (version, hash,
		// role) constraint makes a retried write a no-op.
		if _, err := handle.SQL().ExecContext(ctx,
			`INSERT INTO asset_files (id, asset_version_id, file_hash, role, ordinal, created_at)
			 VALUES (?, ?, ?, 'thumbnail', 0, strftime('%Y-%m-%dT%H:%M:%fZ','now'))
			 ON CONFLICT(asset_version_id, file_hash, role) DO NOTHING`,
			"thumb-"+request.AssetVersionID, request.AssetVersionID, object.Hash); err != nil {
			return jobs.ThumbnailResult{}, err
		}
		return jobs.ThumbnailResult{Hash: object.Hash, Size: int64(len(png))}, nil
	}
}

// exportHandler composes one episode's film.
func exportHandler(media *mediaWiring) jobs.ExportHandler {
	return func(ctx context.Context, request jobs.ExportJobRequest) (jobs.ExportJobResult, error) {
		quality := domainmedia.ExportQuality(request.Quality)
		if quality == "" {
			quality = domainmedia.QualityPreview
		}
		record, _, err := media.exports.Export(ctx, appmedia.ExportRequest{
			EpisodeID:     request.EpisodeID,
			Quality:       quality,
			FPS:           request.FPS,
			CreatedByType: "user",
		})
		if err != nil {
			return jobs.ExportJobResult{}, err
		}
		return jobs.ExportJobResult{ExportID: record.ID, Duration: record.DurationMS}, nil
	}
}

// migrationHandler runs one legacy snapshot import.
func migrationHandler(projects *projectWiring, store *filestore.Store) jobs.MigrationHandler {
	return func(ctx context.Context, request jobs.MigrationJobRequest) (jobs.MigrationJobResult, error) {
		snapshot, err := loadSnapshotFromStore(store, ctx, request.SnapshotID)
		if err != nil {
			return jobs.MigrationJobResult{}, err
		}
		if _, err := projects.imports.Import(ctx, snapshot, applegacy.ImportOptions{
			Mode: "initial", SourceCase: "job:" + request.SnapshotID,
		}); err != nil {
			return jobs.MigrationJobResult{}, err
		}
		return jobs.MigrationJobResult{}, nil
	}
}

// importHandler imports one stored document. The importing service's request
// carries the document bytes, so a job that names an EXISTING document id
// continues it with a new version — the importer's own path.
func importHandler(drama *dramaWiring) jobs.ImportHandler {
	return func(ctx context.Context, request jobs.ImportJobRequest) (jobs.ImportJobResult, error) {
		result, err := drama.importing.Import(ctx, appimporting.ImportRequest{
			ProjectID:  request.ProjectID,
			DocumentID: request.DocumentID,
		})
		if err != nil {
			return jobs.ImportJobResult{}, err
		}
		return jobs.ImportJobResult{
			StoryEntities: len(result.Chapters),
			Episodes:      1,
		}, nil
	}
}

// loadSnapshotFromStore reads one legacy snapshot by its content hash — JSON
// on its own, or a zip bundle whose manifest.json carries the envelope.
func loadSnapshotFromStore(store *filestore.Store, ctx context.Context, hash string) (applegacy.Snapshot, error) {
	reader, err := store.Open(ctx, hash)
	if err != nil {
		return applegacy.Snapshot{}, err
	}
	defer reader.Close()
	body, err := io.ReadAll(reader)
	if err != nil {
		return applegacy.Snapshot{}, err
	}
	var snapshot applegacy.Snapshot
	if err := json.Unmarshal(body, &snapshot); err == nil && snapshot.ManifestVersion > 0 {
		return snapshot, nil
	}
	archive, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return applegacy.Snapshot{}, fmt.Errorf("the snapshot is neither JSON nor a zip bundle")
	}
	for _, file := range archive.File {
		if file.Name != "manifest.json" {
			continue
		}
		opened, err := file.Open()
		if err != nil {
			return applegacy.Snapshot{}, err
		}
		manifest, err := io.ReadAll(opened)
		opened.Close()
		if err != nil {
			return applegacy.Snapshot{}, err
		}
		if err := json.Unmarshal(manifest, &snapshot); err != nil {
			return applegacy.Snapshot{}, err
		}
		return snapshot, nil
	}
	return applegacy.Snapshot{}, fmt.Errorf("the bundle names no manifest.json")
}

// the local handlers' clock and id source, matching the wiring's conventions.
var _ = time.Now
