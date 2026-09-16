// Package backup writes and reads the ordinary project archive described by
// docs/ARCHITECTURE.md §16 and PRD FR-170.
//
// An ordinary backup contains no secret material by construction: the database
// stores only secret references, and this package never reads the credential
// store, so a key cannot be in an archive. The scan AC-BACKUP-001 asks for is
// therefore a check that the mechanism holds, not a filter that might miss a
// case.
//
// The archive is a ZIP built through the hardened reader/writer, which enforces
// the docs/SECURITY.md §9 limits on both directions.
package backup

import (
	"context"
	"time"
)

// ManifestName is the archive's manifest file.
const ManifestName = "manifest.json"

// DatabaseName is the archived database snapshot.
const DatabaseName = "app.sqlite"

// ProviderMetadataName carries provider configuration without secret material.
const ProviderMetadataName = "provider.json"

// FilesPrefix is the directory archived objects live under.
const FilesPrefix = "files/"

// SupportedManifestVersion is the only manifest version this build writes and
// reads. PRD FR-170 requires a versioned format with migration tests for old
// versions, so the field exists from the first release.
const SupportedManifestVersion = 1

// Manifest describes one archive.
type Manifest struct {
	ManifestVersion int    `json:"manifestVersion"`
	App             string `json:"app"`
	// AppVersion is the build that wrote the archive.
	AppVersion    string `json:"appVersion"`
	SchemaVersion int    `json:"schemaVersion"`
	CreatedAt     string `json:"createdAt"`
	// Projects and Assets are counts, so a reader can compare what it extracted
	// against what the writer intended without querying the database first.
	Projects int `json:"projects"`
	Assets   int `json:"assets"`
	Files    int `json:"files"`
	// DatabaseBytes and FileBytes size the payload for a restore precheck.
	DatabaseBytes int64 `json:"databaseBytes"`
	FileBytes     int64 `json:"fileBytes"`
	// HasSecrets records the writer's assertion. It is always false for an
	// ordinary backup, and a reader that finds it true refuses the archive
	// rather than importing something it cannot vouch for.
	HasSecrets bool `json:"hasSecrets"`
}

// FileEntry is one archived object.
type FileEntry struct {
	// Hash is the content hash, which is also the object's storage key.
	Hash string
	// MIME and Size come from the object metadata.
	MIME string
	Size int64
}

// Source supplies what an export needs. Every method reads from the live
// application state; the export never writes to it.
type Source interface {
	// SnapshotDatabase writes a consistent copy of the database into the given
	// directory and returns its path. A writer must use VACUUM INTO or an
	// equivalent point-in-time mechanism, because copying the file while WAL
	// pages are outstanding would archive a torn database.
	SnapshotDatabase(ctx context.Context, directory string) (path string, err error)
	// SchemaVersion reports the applied migration version.
	SchemaVersion(ctx context.Context) (int, error)
	// Counts reports the row counts the manifest advertises.
	Counts(ctx context.Context) (Counts, error)
	// Files lists every stored object the archive should carry.
	Files(ctx context.Context) ([]FileEntry, error)
	// Open returns one stored object's bytes.
	Open(ctx context.Context, hash string) (content []byte, err error)
	// ProviderMetadata returns the non-secret provider configuration.
	ProviderMetadata(ctx context.Context) ([]byte, error)
}

// Counts are the entity counts a manifest records.
type Counts struct {
	Projects int
	Assets   int
}

// Sink receives what a restore produces. The restore never touches live state
// until the whole archive validated, so a Sink implementation can be a staging
// area that the caller promotes atomically.
type Sink interface {
	// StageDatabase places the archived database where it can be verified.
	StageDatabase(ctx context.Context, content []byte) (path string, err error)
	// VerifyDatabase opens the staged database read-only and reports its
	// schema version, refusing one that is corrupt or from the future.
	VerifyDatabase(ctx context.Context, path string) (schemaVersion int, err error)
	// StageFile stores one object's verified bytes.
	StageFile(ctx context.Context, entry FileEntry, content []byte) error
}

// Clock abstracts time for deterministic tests.
type Clock interface {
	Now() time.Time
}

// ExportResult reports what an export wrote.
type ExportResult struct {
	Manifest Manifest
	// Bytes is the finished archive.
	Bytes []byte
}

// RestoreResult reports what a restore staged.
type RestoreResult struct {
	Manifest Manifest
	// Files is how many objects were staged.
	Files int
	// DatabasePath is where the staged database landed.
	DatabasePath string
	// SyntheticSecretsFound lists any secret-shaped strings the reader found.
	// A non-empty list fails the restore: AC-BACKUP-001 requires that a backup
	// never carries a key, so finding one means the archive is not ordinary.
	SyntheticSecretsFound []string
}

// MaxArchiveBytes caps an archive this build will read, so a hostile file
// cannot exhaust memory before the ZIP limits apply.
const MaxArchiveBytes = 4 << 30

// readerLimits bounds one archive read. It exists so a test can exercise the
// ceiling without allocating a multi-gigabyte buffer.
type readerLimits struct {
	maxBytes int64
}

// defaultReaderLimits is the production ceiling.
func defaultReaderLimits() readerLimits {
	return readerLimits{maxBytes: MaxArchiveBytes}
}
