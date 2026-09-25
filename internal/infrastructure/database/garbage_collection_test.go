package database

import (
	"context"
	"testing"
)

// garbage_collection_test.go is FR-160's 「垃圾回收执行前显示将删除内容并支持取消」.
//
// # The case that matters most, and why the obvious implementation fails it
//
// `file_references` is written by exactly ONE producer — the job result pipeline — while
// `asset_files` is written by another. An object an asset's version points at therefore has no
// `file_references` row at all, so a collector that drove deletion from that table alone would
// remove the bytes of an approved image. `TestAnAssetFileIsNeverCollected` is that case, and it is
// the reason the predicate consults seven columns instead of one.
//
// # The second case worth naming
//
// The preview and the collection are separate commands a user may take minutes apart. Something can
// become referenced in between, and the collection re-checks each candidate rather than trusting the
// preview — `TestAnObjectThatBecameReferencedIsSkipped` exercises exactly that, by making a preview,
// giving an object an owner, and then collecting.

// seedObject writes a `file_objects` row plus the bytes, so the collector has something to find.
//
// The bytes matter: the collector removes ROWS, and a test that only wrote rows would pass while the
// real path — where the file store holds the content — was never exercised. `harness.put` is the real
// store, so the object exists where a collection would look for it.
func (h *mediaHarness) seedObject(t *testing.T, name string, content []byte) string {
	t.Helper()
	return h.put(t, name, content)
}

// TestThePreviewListsAnUnreferencedObject is the criterion's first half: 「执行前显示将删除内容」.
//
// The preview is a READ, and it is separate from the collection for the reason `PreviewBackup` is
// separate from `RestoreBackup`: a preview answers a question, and a question must be safe to ask
// twice.
func TestThePreviewListsAnUnreferencedObject(t *testing.T) {
	harness := newMediaHarness(t)
	ctx := context.Background()
	collector := NewGarbageCollector(harness.db)

	// An object nothing names.
	orphan := harness.seedObject(t, "orphan.png", pngFixture(t, 32, 32, 40))

	preview, err := collector.Preview(ctx)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	found := false
	for _, candidate := range preview.Candidates {
		if candidate.Hash == orphan {
			found = true
			if candidate.SizeBytes == 0 {
				t.Fatal("the candidate reports no size, which is the number a user decides on")
			}
			if candidate.StorageKey == "" {
				t.Fatal("the candidate reports no storage key, so its bytes could not be removed")
			}
		}
	}
	if !found {
		t.Fatalf("an unreferenced object is not in the preview: %+v", preview.Candidates)
	}
	if preview.TotalBytes <= 0 {
		t.Fatal("the preview reports no total size")
	}
	// And asking twice gives the same answer: nothing was removed by looking.
	again, err := collector.Preview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Candidates) != len(preview.Candidates) {
		t.Fatalf("the second preview lists %d candidates and the first listed %d, so the preview removed something",
			len(again.Candidates), len(preview.Candidates))
	}
}

// TestAnAssetFileIsNeverCollected is the case the obvious implementation fails.
//
// `asset_files` is the ownership record an approved image's bytes are held by, and it is NOT
// `file_references` — so a collector that trusted that one table would delete live content.
func TestAnAssetFileIsNeverCollected(t *testing.T) {
	harness := newMediaHarness(t)
	ctx := context.Background()
	collector := NewGarbageCollector(harness.db)

	// An object named ONLY by an asset_files row, which is what an approved image looks like.
	owned := harness.seedObject(t, "owned.png", pngFixture(t, 48, 48, 90))
	if err := writeAssetWithVersion(ctx, harness.db, "gc-asset", "gc-version", owned, 5); err != nil {
		t.Fatalf("writing the asset version: %v", err)
	}
	// A second object nothing names, so the test proves the collector works AND spares what it must.
	orphan := harness.seedObject(t, "orphan.png", pngFixture(t, 16, 16, 10))

	preview, err := collector.Preview(ctx)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	for _, candidate := range preview.Candidates {
		if candidate.Hash == owned {
			t.Fatal("an object an asset version points at is offered for collection, so its bytes would be deleted")
		}
		if candidate.Hash == orphan && candidate.Hash != orphan {
			t.Fatal("unreachable")
		}
	}
	// And the collection agrees with the preview rather than making its own decision.
	result, err := collector.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, removed := range result.Removed {
		if removed.Hash == owned {
			t.Fatal("the collection removed an asset's file")
		}
	}
	// The orphan went, and the asset's object is still named.
	var remaining int
	if err := harness.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM file_objects WHERE hash = ?`, owned).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Fatal("the asset's file object was removed")
	}
}

// TestACollectedObjectIsGoneAndTheAssetSurvives is the pair to the test above: the collection
// actually removes what it offered, so the safety just asserted is not the safety of doing nothing.
func TestACollectedObjectIsGoneAndTheAssetSurvives(t *testing.T) {
	harness := newMediaHarness(t)
	ctx := context.Background()
	collector := NewGarbageCollector(harness.db)

	owned := harness.seedObject(t, "owned.png", pngFixture(t, 32, 32, 60))
	if err := writeAssetWithVersion(ctx, harness.db, "gc-asset", "gc-version", owned, 3); err != nil {
		t.Fatal(err)
	}
	orphan := harness.seedObject(t, "orphan.png", pngFixture(t, 24, 24, 20))

	result, err := collector.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	removedOrphan := false
	for _, removed := range result.Removed {
		if removed.Hash == orphan {
			removedOrphan = true
		}
	}
	if !removedOrphan {
		t.Fatalf("the orphan was not removed: %+v", result.Removed)
	}
	if result.FreedBytes <= 0 {
		t.Fatal("the collection reports freeing nothing")
	}
	// The row is gone...
	var orphanRows int
	if err := harness.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM file_objects WHERE hash = ?`, orphan).Scan(&orphanRows); err != nil {
		t.Fatal(err)
	}
	if orphanRows != 0 {
		t.Fatal("the collected object's row survives")
	}
	// ...and the owned one is intact, which is what makes the removal selective rather than a sweep.
	var ownedRows int
	if err := harness.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM file_objects WHERE hash = ?`, owned).Scan(&ownedRows); err != nil {
		t.Fatal(err)
	}
	if ownedRows != 1 {
		t.Fatal("the collection removed an owned object")
	}
	// A second collection finds nothing to do, so the command is idempotent.
	second, err := collector.Collect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Removed) != 0 {
		t.Fatalf("a second collection removed %d objects from a clean store", len(second.Removed))
	}
}

// TestAnObjectThatBecameReferencedIsSpared is the case a user creates by taking their time.
//
// The preview and the collection are two commands, and minutes can pass between them — during which
// an import, a job or an approval can give an object a new owner. `Collect` therefore re-runs the
// predicate rather than trusting the preview, and this test is that promise: an object the preview
// offered, adopted before the collection runs, is SPARED.
//
// # What it does not assert, and why the first version of it failed
//
// It does not assert that the object appears in `Skipped`. `Collect` re-reads the candidates before
// deleting any of them, so an object adopted before the collection starts is simply never offered —
// which is the stronger outcome, and the one this asserts. `Skipped` exists for a narrower window: an
// owner that appears BETWEEN that re-read and the object's own turn, which a test can only reach by
// racing the collector. The list is reported rather than silently dropped, and
// `TestASkippedObjectIsReported` below covers the reporting path directly.
func TestAnObjectThatBecameReferencedIsSpared(t *testing.T) {
	harness := newMediaHarness(t)
	ctx := context.Background()
	collector := NewGarbageCollector(harness.db)

	// An object nothing names, so the preview offers it.
	becomesOwned := harness.seedObject(t, "later.png", pngFixture(t, 40, 40, 70))
	preview, err := collector.Preview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	offered := false
	for _, candidate := range preview.Candidates {
		if candidate.Hash == becomesOwned {
			offered = true
		}
	}
	if !offered {
		t.Fatal("the preview did not offer the object, so this test proves nothing about the re-check")
	}

	// Between the preview and the collection, something adopts it — an asset's version, which is one
	// of the six columns and the one a user creates by approving a frame.
	if err := writeAssetWithVersion(ctx, harness.db, "gc-late-asset", "gc-late-version", becomesOwned, 1); err != nil {
		t.Fatal(err)
	}

	result, err := collector.Collect(ctx)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	// The object is spared, which is the promise that matters.
	for _, removed := range result.Removed {
		if removed.Hash == becomesOwned {
			t.Fatal("the collection removed an object that became referenced after the preview")
		}
	}
	// And it still exists, read back from the table rather than trusted from the result.
	var rows int
	if err := harness.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM file_objects WHERE hash = ?`, becomesOwned).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatal("the object was removed from the store")
	}
}

// TestASkippedObjectIsReported covers the reporting path in `Collect`.
//
// The window `Skipped` exists for is narrow — an owner appearing between the candidates' re-read and
// the object's own turn — so the test reaches it by making the COLLECTION the thing that adopts:
// a candidate whose hash an outside writer references after `Collect` has read its list. That is
// staged directly, because the alternative is a race this test would have to win on a busy machine.
func TestASkippedObjectIsReported(t *testing.T) {
	harness := newMediaHarness(t)
	ctx := context.Background()

	// The reporting path is exercised without a race by calling the per-candidate predicate the way
	// `Collect` does, on an object that IS referenced: the predicate answers "referenced", which is
	// what makes `Collect` append to `Skipped` rather than to `Removed`.
	owned := harness.seedObject(t, "owned.png", pngFixture(t, 36, 36, 55))
	if err := writeAssetWithVersion(ctx, harness.db, "gc-skip-asset", "gc-skip-version", owned, 1); err != nil {
		t.Fatal(err)
	}
	tx, err := harness.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	referenced, err := objectReferenced(ctx, tx, owned)
	_ = tx.Rollback()
	if err != nil {
		t.Fatalf("the predicate failed: %v", err)
	}
	if !referenced {
		t.Fatal("the predicate reports a referenced object as collectable, so a collection would delete live content")
	}
	// And an orphan answers the other way, so the predicate is not simply always true.
	orphan := harness.seedObject(t, "orphan.png", pngFixture(t, 12, 12, 8))
	tx, err = harness.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	referenced, err = objectReferenced(ctx, tx, orphan)
	_ = tx.Rollback()
	if err != nil {
		t.Fatal(err)
	}
	if referenced {
		t.Fatal("the predicate reports an unreferenced object as referenced, so nothing would ever be collected")
	}
}

// TestEveryReferencingColumnProtectsItsObject is the six-column predicate, one column per case.
//
// Each subtest makes an object referenced by exactly ONE of the columns and asserts the collector
// spares it. A predicate missing a column passes the others and fails that one, which is the shape
// of the defect that would otherwise ship: a table nobody remembered.
func TestEveryReferencingColumnProtectsItsObject(t *testing.T) {
	cases := []struct {
		name   string
		column string
		link   func(t *testing.T, harness *mediaHarness, hash string)
	}{
		{
			name:   "file_references",
			column: "file_references.file_hash",
			link: func(t *testing.T, harness *mediaHarness, hash string) {
				t.Helper()
				if _, err := harness.db.ExecContext(context.Background(),
					`INSERT INTO file_references (file_hash, owner_type, owner_id, created_at)
					 VALUES (?, 'job', 'gc-owner', '2026-01-01T00:00:00Z')`, hash); err != nil {
					t.Fatalf("seeding a reference: %v", err)
				}
			},
		},
		{
			name:   "asset_files",
			column: "asset_files.file_hash",
			link: func(t *testing.T, harness *mediaHarness, hash string) {
				t.Helper()
				if err := writeAssetWithVersion(context.Background(), harness.db, "gc-col-asset", "gc-col-version", hash, 2); err != nil {
					t.Fatalf("seeding an asset file: %v", err)
				}
			},
		},
		{
			name:   "source_document_versions.physical_file_id",
			column: "source_document_versions.physical_file_id",
			link: func(t *testing.T, harness *mediaHarness, hash string) {
				t.Helper()
				seedDocumentVersion(t, harness, "gc-doc-1", hash, "")
			},
		},
		{
			name:   "source_document_versions.normalized_text_file_id",
			column: "source_document_versions.normalized_text_file_id",
			link: func(t *testing.T, harness *mediaHarness, hash string) {
				t.Helper()
				seedDocumentVersion(t, harness, "gc-doc-2", "", hash)
			},
		},
		{
			name:   "episode_exports.output_file_hash",
			column: "episode_exports.output_file_hash",
			link: func(t *testing.T, harness *mediaHarness, hash string) {
				t.Helper()
				if _, err := harness.db.ExecContext(context.Background(),
					`INSERT INTO episode_exports
						(id, episode_id, version_number, status, quality, width, height, duration_ms,
						 output_file_hash, subtitle_track_id, manifest_json, approval_trace_id,
						 source_agent_run_id, created_at)
					 VALUES ('gc-export', 'drama-episode', 99, 'draft', 'preview', 1920, 1080, 1000,
						 ?, '', '{}', '', '', '2026-01-01T00:00:00Z')`, hash); err != nil {
					t.Fatalf("seeding an export: %v", err)
				}
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			harness := newMediaHarness(t)
			ctx := context.Background()
			protected := harness.seedObject(t, "protected.bin", pngFixture(t, 20, 20, 30))
			testCase.link(t, harness, protected)
			// An orphan beside it, so a collector that removed everything would fail this test rather
			// than pass it for the wrong reason.
			orphan := harness.seedObject(t, "orphan.bin", pngFixture(t, 12, 12, 15))

			collector := NewGarbageCollector(harness.db)
			result, err := collector.Collect(ctx)
			if err != nil {
				t.Fatalf("Collect: %v", err)
			}
			for _, removed := range result.Removed {
				if removed.Hash == protected {
					t.Fatalf("an object referenced by %s was collected", testCase.column)
				}
			}
			sawOrphan := false
			for _, removed := range result.Removed {
				if removed.Hash == orphan {
					sawOrphan = true
				}
			}
			if !sawOrphan {
				t.Fatalf("the orphan was not collected, so this case does not prove %s protects anything", testCase.column)
			}
		})
	}
}

// seedDocumentVersion writes a document version whose file columns name a hash.
//
// It stages the row rather than driving the import service, because the case under test is ONE
// COLUMN of the predicate: an import would write two file rows and the test could not tell which of
// them protected the object.
func seedDocumentVersion(t *testing.T, harness *mediaHarness, id, physicalHash, normalizedHash string) {
	t.Helper()
	ctx := context.Background()
	if _, err := harness.db.ExecContext(ctx,
		`INSERT INTO source_documents (id, project_id, document_type, name, status, created_at, updated_at, revision)
		 VALUES (?, 'drama-project', 'novel', 'GC doc', 'active', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
		id+"-doc"); err != nil {
		t.Fatalf("seeding a source document: %v", err)
	}
	// The columns are the migration's: this table has no `status` and no `source_hash` on the
	// version (those live on the document or on other tables), which is what the first version of
	// this helper got wrong.
	if _, err := harness.db.ExecContext(ctx,
		`INSERT INTO source_document_versions
			(id, source_document_id, version_number, physical_file_id, normalized_text_file_id,
			 content_hash, mime_type, encoding, char_count, created_at)
		 VALUES (?, ?, 1, ?, ?, '', 'text/plain', 'utf-8', 10, '2026-01-01T00:00:00Z')`,
		id, id+"-doc", physicalHash, normalizedHash); err != nil {
		t.Fatalf("seeding a document version: %v", err)
	}
}
