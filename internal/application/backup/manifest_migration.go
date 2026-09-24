package backup

// manifest_migration.go is FR-170's 「备份格式包含版本，旧版本有迁移测试」.
//
// # Why a migration rather than a comparison
//
// The reader used to refuse every version but its own: `manifestVersion != 1` was a flat refusal, so
// a v0 archive written by an earlier release could not be restored and there was no hook where an
// upgrade could be written. PRD FR-170 asks for both halves — a versioned format AND migration
// tests — and the first half alone is what the row carried for three packages.
//
// # What "version 0" is, stated exactly
//
// Version 1 is the FIRST version this build writes (`SupportedManifestVersion`), so the migrations
// here are for archives older than the build's own format. The concrete case the repository has is
// the one `legacy.Snapshot` writes: the browser-era envelope carries no `manifestVersion` at all,
// which decodes as 0. A 0 archive is therefore "written before the field existed", and its migration
// is exactly "assert what the older writer guaranteed and stamp the current version".
//
// # The rule a migration must keep
//
// A migration upgrades a DOCUMENT, and it must never invent a fact the archive does not carry. Each
// step below is one of two kinds, and both are honest:
//
//   - it FILLS a field the older format did not have, with the value its absence MEANT (the
//     documented default), or
//   - it REFUSES, naming what the archive lacks, when absence does not imply a value.
//
// A step that guessed would turn a readable archive into a silently wrong restore, which is worse
// than the refusal it replaced — the data would arrive, and nothing would say so.

// ManifestMigration upgrades one older manifest to the next version.
//
// It returns the upgraded document and true, or an error. A migration that cannot upgrade REFUSES
// rather than returning a partial document: a half-migrated manifest would be restored as though the
// missing fields were the older writer's intent.
type ManifestMigration func(Manifest) (Manifest, error)

// manifestMigrations maps a SOURCE version to the step that upgrades it to source+1.
//
// The map is keyed by source rather than by target so a chain is unambiguous: migrating from 0 runs
// the step registered at 0, and if that step lands at 1 while the build reads 1, the walk stops. A
// gap — a source with no step — is a REFUSAL rather than a skip, because skipping a version would
// apply later steps to a document they were not written for.
var manifestMigrations = map[int]ManifestMigration{
	0: migrateManifestFromZero,
}

// migrateManifestFromZero upgrades an archive written before the manifest carried a version.
//
// WHAT IT CAN ASSERT, and why each is a fact about the older format rather than a guess:
//
//   - `manifestVersion` becomes 1. That is the migration's whole purpose.
//   - `app` must already be `"infinite-canvas"`. The field existed before the version field did, and
//     an archive that names another application is not one this build can upgrade — the refusal is
//     the same one `validateManifest` gives, reached one step earlier.
//   - `schemaVersion` must already be positive. A v0 writer always recorded it (the column has been
//     required since the first backup format), so a zero here means the archive is malformed rather
//     than old, and the migration refuses rather than inventing a version that would then be trusted
//     by the schema check below.
//
// WHAT IT DELIBERATELY DOES NOT DO: it does not touch `hasSecrets`, `projects`, `assets`, `files` or
// the byte counts. Filling a count from anything but the archive could make it disagree with the
// contents, and the reader compares those counts against what it extracted — so a guessed number
// would either fail the comparison or, worse, pass it while being wrong.
func migrateManifestFromZero(manifest Manifest) (Manifest, error) {
	if manifest.App != "infinite-canvas" {
		return Manifest{}, importError("manifest",
			"That file is not an Infinite Atelier backup.", nil)
	}
	if manifest.SchemaVersion <= 0 {
		return Manifest{}, importError("manifest",
			"That archive records no database version, so it cannot be migrated.", nil)
	}
	manifest.ManifestVersion = 1
	return manifest, nil
}

// migrateManifest walks a manifest up to the version this build reads.
//
// It is the ONE place a version comparison becomes a migration, and `validateManifest` calls it
// before its own checks — so every rule below operates on a document of the current version.
//
// A version NEWER than this build's is refused here: an archive written by a later release may carry
// fields this build would drop on restore, and "it happened to parse" is not a reason to import it.
func migrateManifest(manifest Manifest) (Manifest, error) {
	if manifest.ManifestVersion > SupportedManifestVersion {
		return Manifest{}, importError("manifest",
			"That backup was written by a newer version and cannot be restored.", nil)
	}
	// Bounded by the number of versions rather than by a `for {}`: a cycle in the table (which a
	// future edit could introduce) must not hang the reader.
	for version := manifest.ManifestVersion; version < SupportedManifestVersion; version++ {
		step, ok := manifestMigrations[version]
		if !ok {
			return Manifest{}, importError("manifest",
				"That backup was written by a version this build cannot upgrade.", nil)
		}
		upgraded, err := step(manifest)
		if err != nil {
			return Manifest{}, err
		}
		// A step that did not advance the version would make this loop spin until the bound; the
		// check turns a programming error into a refusal that names it.
		if upgraded.ManifestVersion <= version {
			return Manifest{}, importError("manifest",
				"That backup could not be upgraded.", nil)
		}
		manifest = upgraded
	}
	return manifest, nil
}
