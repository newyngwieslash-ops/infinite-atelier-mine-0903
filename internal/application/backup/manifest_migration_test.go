package backup

import (
	"context"
	"strings"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/archive"
)

// manifest_migration_test.go is FR-170's 「备份格式包含版本，旧版本有迁移测试」.
//
// # What the criterion asks for, and what it had
//
// The clause has two halves: a versioned format, and migration tests for old versions. The first was
// true (`manifestVersion` has been written since the first release) and the second was not — the
// reader compared the version for equality and refused everything else, so there was no migration to
// test and no hook where one could be written. `TestRestoreRefusesUnknownManifestVersion` (in
// service_test.go) covered the NEWER version, which is the opposite direction.
//
// # The cases, and why each is one of them
//
//  1. An older manifest is UPGRADED and the upgraded document passes validation. This is the
//     criterion.
//  2. The upgrade FILLS only what absence meant, and leaves every count alone. A migration that
//     guessed a count would either fail the reader's own comparison or — worse — pass it while being
//     wrong.
//  3. A newer manifest is REFUSED. An archive from a later release may carry fields this build
//     would drop on restore.
//  4. A gap in the chain is REFUSED rather than skipped. Skipping a version would apply a later
//     step to a document it was not written for.
//  5. A step that does not advance is REFUSED rather than looping.
//  6. An old manifest that is MALFORMED is refused by the migration, naming what it lacks — the
//     case where absence does not imply a value.

func oldManifest() Manifest {
	return Manifest{
		// ManifestVersion absent, which is what a v0 writer produced and what decodes as 0.
		App:           "infinite-canvas",
		AppVersion:    "0.9.0",
		SchemaVersion: 19,
		CreatedAt:     "2026-06-01T00:00:00Z",
		Projects:      3,
		Assets:        7,
		Files:         12,
		DatabaseBytes: 4096,
		FileBytes:     8192,
	}
}

// TestAnOlderManifestIsMigratedRatherThanRefused is the criterion: the archive a previous release
// wrote can be restored, and what it upgrades TO passes the same validation a current archive does.
func TestAnOlderManifestIsMigratedRatherThanRefused(t *testing.T) {
	migrated, err := migrateManifest(oldManifest())
	if err != nil {
		t.Fatalf("a version-0 archive was refused instead of migrated: %v", err)
	}
	if migrated.ManifestVersion != SupportedManifestVersion {
		t.Fatalf("the migration left the manifest at version %d", migrated.ManifestVersion)
	}
	// And the upgraded document is one the reader accepts, which is what makes the migration useful
	// rather than merely successful.
	validated, err := validateManifest(migrated)
	if err != nil {
		t.Fatalf("the migrated manifest did not validate: %v", err)
	}
	if validated.ManifestVersion != SupportedManifestVersion {
		t.Fatalf("validateManifest returned version %d, so a caller would report the archive's original field", validated.ManifestVersion)
	}
	// The caller's document is not mutated: a migration that wrote through its argument would
	// change a value the caller may still be using.
	original := oldManifest()
	if _, err := migrateManifest(original); err != nil {
		t.Fatal(err)
	}
	if original.ManifestVersion != 0 {
		t.Fatalf("the migration mutated its argument (version is now %d)", original.ManifestVersion)
	}
}

// TestTheMigrationFillsOnlyWhatAbsenceMeant is the second case, and it is the one whose failure
// would be silent: a guessed count arrives as data and nothing says it was invented.
func TestTheMigrationFillsOnlyWhatAbsenceMeant(t *testing.T) {
	before := oldManifest()
	migrated, err := migrateManifest(before)
	if err != nil {
		t.Fatal(err)
	}
	// Every field the older format DID carry is unchanged.
	if migrated.App != before.App || migrated.AppVersion != before.AppVersion ||
		migrated.SchemaVersion != before.SchemaVersion || migrated.CreatedAt != before.CreatedAt ||
		migrated.Projects != before.Projects || migrated.Assets != before.Assets ||
		migrated.Files != before.Files || migrated.DatabaseBytes != before.DatabaseBytes ||
		migrated.FileBytes != before.FileBytes || migrated.HasSecrets != before.HasSecrets {
		t.Fatalf("the migration changed a field the older format already carried:\n before %+v\n after  %+v", before, migrated)
	}
	// And the ONE field it fills is the version.
	if migrated.ManifestVersion == before.ManifestVersion {
		t.Fatal("the migration did not fill the version, which is its whole purpose")
	}
}

// TestANewerManifestIsRefused is the third case: a later release may carry fields this build would
// drop on restore, and "it happened to parse" is not a reason to import it.
func TestANewerManifestIsRefused(t *testing.T) {
	newer := oldManifest()
	newer.ManifestVersion = SupportedManifestVersion + 1
	_, err := migrateManifest(newer)
	if err == nil {
		t.Fatal("a manifest from a newer release was accepted")
	}
	if !strings.Contains(err.Error(), "newer version") {
		t.Fatalf("the refusal does not say the archive is newer: %v", err)
	}
}

// TestAGapInTheMigrationChainIsRefused is the fourth case. The chain is walked by version, and a
// missing step is a refusal rather than a skip: skipping would apply a later step to a document it
// was not written for.
func TestAGapInTheMigrationChainIsRefused(t *testing.T) {
	// A gap is staged by pointing the walk at a version with no step registered. The table has one
	// entry (0), so version 0 with the table EMPTIED is the smallest way to state it — and the
	// table is restored afterwards so the case cannot leak into the others.
	saved := manifestMigrations
	manifestMigrations = map[int]ManifestMigration{}
	defer func() { manifestMigrations = saved }()

	_, err := migrateManifest(oldManifest())
	if err == nil {
		t.Fatal("a manifest whose migration step is missing was accepted")
	}
	if !strings.Contains(err.Error(), "cannot upgrade") {
		t.Fatalf("the refusal does not say the version cannot be upgraded: %v", err)
	}
}

// TestAStepThatDoesNotAdvanceIsRefused is the fifth case: a step that leaves the version where it
// found it would make the walk spin, and the check turns that programming error into a refusal.
func TestAStepThatDoesNotAdvanceIsRefused(t *testing.T) {
	saved := manifestMigrations
	manifestMigrations = map[int]ManifestMigration{
		0: func(manifest Manifest) (Manifest, error) { return manifest, nil },
	}
	defer func() { manifestMigrations = saved }()

	_, err := migrateManifest(oldManifest())
	if err == nil {
		t.Fatal("a migration step that did not advance the version was accepted")
	}
	if !strings.Contains(err.Error(), "could not be upgraded") {
		t.Fatalf("the refusal does not name the stuck upgrade: %v", err)
	}
}

// TestAnOldManifestWithoutADatabaseVersionIsRefusedByTheMigration is the sixth case, and it is the
// one where absence does NOT imply a value: a v0 writer always recorded the schema version, so a zero
// means the archive is malformed rather than old. Inventing one would hand the reader a version to
// trust, and the reader's own check happens after the migration.
func TestAnOldManifestWithoutADatabaseVersionIsRefusedByTheMigration(t *testing.T) {
	broken := oldManifest()
	broken.SchemaVersion = 0
	_, err := migrateManifest(broken)
	if err == nil {
		t.Fatal("a version-0 manifest with no schema version was migrated, which would invent one")
	}
	if !strings.Contains(err.Error(), "cannot be migrated") {
		t.Fatalf("the refusal does not say the archive cannot be migrated: %v", err)
	}

	// And an archive that names another application is refused by the migration too, one step before
	// validateManifest's own check would reach it.
	foreign := oldManifest()
	foreign.App = "some-other-app"
	if _, err := migrateManifest(foreign); err == nil {
		t.Fatal("a manifest from another application was migrated")
	}
}

// TestACurrentManifestPassesThroughTheMigration is the identity case: a manifest at the build's own
// version is returned unchanged, so the migration is a no-op for every archive this build writes.
func TestACurrentManifestPassesThroughTheMigration(t *testing.T) {
	current := Manifest{
		ManifestVersion: SupportedManifestVersion,
		App:             "infinite-canvas",
		AppVersion:      "1.0.0",
		SchemaVersion:   22,
		Projects:        1,
	}
	migrated, err := migrateManifest(current)
	if err != nil {
		t.Fatalf("a current manifest was refused: %v", err)
	}
	if migrated != current {
		t.Fatalf("a current manifest was changed:\n before %+v\n after  %+v", current, migrated)
	}
}

// TestAnArchiveFromTheOlderFormatRestores is the end-to-end half: the unit tests above prove the
// document upgrades, and this proves the READER — which is where a user meets it — restores an
// archive a previous release wrote.
//
// It matters because the unit tests could all pass while the migration was never called on the
// restore path. The archive is built the way the tamper test builds one (a real writer, a real
// manifest, real checksums) with the version field ABSENT, which is what a write from before the
// field existed produced.
func TestAnArchiveFromTheOlderFormatRestores(t *testing.T) {
	// The manifest is written WITHOUT `manifestVersion`, exactly as a v0 writer produced it: the
	// raw JSON is assembled rather than marshalling the struct, because a struct always carries the
	// field and the case under test is its absence.
	manifestJSON := `{
		"app": "infinite-canvas",
		"appVersion": "0.9.0",
		"schemaVersion": 4,
		"createdAt": "2026-06-01T00:00:00Z",
		"projects": 1,
		"assets": 0,
		"files": 0,
		"databaseBytes": 16,
		"fileBytes": 0,
		"hasSecrets": false
	}`

	writer := archive.NewWriter(archive.WriterOptions{})
	if err := writer.Add(ManifestName, []byte(manifestJSON)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Add(DatabaseName, []byte("SQLite format 3\x00")); err != nil {
		t.Fatal(err)
	}
	if err := writer.AddChecksums(); err != nil {
		t.Fatal(err)
	}
	data, err := writer.Finish()
	if err != nil {
		t.Fatal(err)
	}

	sink := newFakeSink()
	// The sink reports version 4, which matches this manifest's schemaVersion, so the schema check
	// passes on its own terms and the restore's success is attributable to the MIGRATION rather than
	// to a laxer schema rule.
	restore := NewRestoreService(sink)
	result, err := restore.Restore(context.Background(), data)
	if err != nil {
		t.Fatalf("an archive from the older format was refused: %v", err)
	}
	// The restore ran, which is the whole point: before the migration this archive was refused with
	// "written by a different version".
	if sink.database == nil {
		t.Fatal("the restore reported success without staging the database")
	}
	if result.Manifest.ManifestVersion != SupportedManifestVersion {
		t.Fatalf("the restored manifest reports version %d, want the current one: the caller should see what the archive WAS migrated to", result.Manifest.ManifestVersion)
	}
}
