// Package asset is the domain layer for assets, their versions, and the files
// a version owns.
//
// It owns the vocabulary and invariants of docs/DOMAIN_MODEL.md §8: which asset
// types exist, when a version may be approved, and why a file can only be
// referenced after it was committed. It performs no I/O and never mints an
// identifier (ADR-0005).
package asset

import (
	"strings"
	"time"
)

// Type is the kind of asset a row describes.
type Type string

const (
	TypeCharacter Type = "character"
	TypeLocation  Type = "location"
	TypeProp      Type = "prop"
	TypeCostume   Type = "costume"
	TypeStyle     Type = "style"
	TypeImage     Type = "image"
	TypeVideo     Type = "video"
	TypeAudio     Type = "audio"
	TypeDoc       Type = "doc"
)

// Types lists the documented asset types in the schema's order.
var Types = []Type{TypeCharacter, TypeLocation, TypeProp, TypeCostume, TypeStyle, TypeImage, TypeVideo, TypeAudio, TypeDoc}

// IsValidType reports whether a type may be persisted.
func IsValidType(value Type) bool {
	for _, candidate := range Types {
		if candidate == value {
			return true
		}
	}
	return false
}

// Status is the lifecycle of an asset row.
type Status string

const (
	StatusActive   Status = "active"
	StatusArchived Status = "archived"
	StatusTrashed  Status = "trashed"
)

// IsValidStatus reports whether a status may be persisted.
func IsValidStatus(value Status) bool {
	switch value {
	case StatusActive, StatusArchived, StatusTrashed:
		return true
	default:
		return false
	}
}

// MaxNameLength mirrors the CHECK constraint in migration 000004.
const MaxNameLength = 200

// Asset is an asset aggregate root.
type Asset struct {
	ID          string
	ProjectID   string
	Type        Type
	Name        string
	Description string
	// StoryEntityID is empty until the drama domain exists (WP-05).
	StoryEntityID string
	// CurrentApprovedVersionID is empty when nothing has been approved yet.
	CurrentApprovedVersionID string
	Status                   Status
	DeletedAt                time.Time
	DeletedBy                string
	LegacyMetadata           string
	CreatedAt                time.Time
	UpdatedAt                time.Time
	Revision                 int64
}

// ValidateName rejects a name the schema would reject anyway.
func ValidateName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return InvalidError("An asset name is required.")
	}
	if len([]rune(trimmed)) > MaxNameLength {
		return InvalidError("The asset name is too long.")
	}
	return nil
}

// VersionStatus is the review state of one asset version.
type VersionStatus string

const (
	VersionDraft      VersionStatus = "draft"
	VersionReview     VersionStatus = "review"
	VersionApproved   VersionStatus = "approved"
	VersionRejected   VersionStatus = "rejected"
	VersionSuperseded VersionStatus = "superseded"
	VersionStale      VersionStatus = "stale"
)

// IsValidVersionStatus reports whether a version status may be persisted.
func IsValidVersionStatus(value VersionStatus) bool {
	switch value {
	case VersionDraft, VersionReview, VersionApproved, VersionRejected, VersionSuperseded, VersionStale:
		return true
	default:
		return false
	}
}

// CreatedByType records who produced a version.
type CreatedByType string

const (
	CreatedByUser      CreatedByType = "user"
	CreatedByAgent     CreatedByType = "agent"
	CreatedByMigration CreatedByType = "migration"
	CreatedBySystem    CreatedByType = "system"
)

// IsValidCreatedByType reports whether a producer kind may be persisted.
func IsValidCreatedByType(value CreatedByType) bool {
	switch value {
	case CreatedByUser, CreatedByAgent, CreatedByMigration, CreatedBySystem:
		return true
	default:
		return false
	}
}

// Version is one version of an asset.
type Version struct {
	ID            string
	AssetID       string
	VersionNumber int
	Status        VersionStatus
	// BasedOnVersionID links a revision to the version it edited.
	BasedOnVersionID string
	Prompt           string
	NegativePrompt   string
	ProviderConfigID string
	ModelConfigID    string
	ModelParameters  string
	// GenerationJobID links a version to the job that produced it, when it came
	// from a generation.
	GenerationJobID string
	Metadata        string
	CreatedByType   CreatedByType
	LegacyMetadata  string
	CreatedAt       time.Time
}

// Validate checks the invariants the schema also enforces.
func (v Version) Validate() error {
	if strings.TrimSpace(v.AssetID) == "" {
		return InvalidError("A version must belong to an asset.")
	}
	if v.VersionNumber < 1 {
		return InvalidError("A version number starts at one.")
	}
	if !IsValidVersionStatus(v.Status) {
		return InvalidError("The version status is not recognised.")
	}
	if !IsValidCreatedByType(v.CreatedByType) {
		return InvalidError("The version producer is not recognised.")
	}
	return nil
}

// CanApprove reports whether a version may move to approved.
//
// DOMAIN_MODEL §8.2 requires impact analysis before an approval switch, and
// that analysis belongs to WP-05 (it needs the shot and scene model to compute
// impact). Until then this method encodes only what is decidable today: a
// version must have a committed file, and it must not be superseded or already
// stale. The caller is responsible for the impact analysis that WP-05 adds.
func (v Version) CanApprove(fileCount int) error {
	if !IsValidVersionStatus(v.Status) {
		return InvalidError("The version status is not recognised.")
	}
	switch v.Status {
	case VersionSuperseded, VersionStale:
		return conflictError("A superseded or stale version cannot be approved.")
	case VersionApproved:
		return conflictError("This version is already approved.")
	}
	if fileCount <= 0 {
		return InvalidError("A version needs a committed file before it can be approved.")
	}
	return nil
}

// FileRole is what a file contributes to a version.
type FileRole string

const (
	RolePrimary    FileRole = "primary"
	RoleThumbnail  FileRole = "thumbnail"
	RoleSource     FileRole = "source"
	RoleAttachment FileRole = "attachment"
)

// IsValidFileRole reports whether a role may be persisted.
func IsValidFileRole(value FileRole) bool {
	switch value {
	case RolePrimary, RoleThumbnail, RoleSource, RoleAttachment:
		return true
	default:
		return false
	}
}

// File links a version to a committed object.
//
// It carries a hash rather than a path: DOMAIN_MODEL §16 and ARCHITECTURE §8.3
// require paths to be reachable only through the FileStore, so the domain never
// sees one.
type File struct {
	VersionID string
	FileHash  string
	Role      FileRole
	CreatedAt time.Time
}

// Validate checks the shape of a file link.
func (f File) Validate() error {
	if strings.TrimSpace(f.VersionID) == "" {
		return InvalidError("A file must belong to an asset version.")
	}
	if !IsValidFileRole(f.Role) {
		return InvalidError("The file role is not recognised.")
	}
	// The hash is a SHA-256 hex digest; the schema's foreign key rejects an
	// uncommitted hash, and this rejects a malformed one earlier.
	if len(f.FileHash) != 64 {
		return InvalidError("A file reference needs a content hash.")
	}
	for _, character := range f.FileHash {
		switch {
		case character >= '0' && character <= '9':
		case character >= 'a' && character <= 'f':
		default:
			return InvalidError("A file reference needs a content hash.")
		}
	}
	return nil
}
