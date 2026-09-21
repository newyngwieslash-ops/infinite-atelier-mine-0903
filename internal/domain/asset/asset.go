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

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// Type is the kind of asset a row describes.
//
// The set is the union of two specification lists that do not agree. DOMAIN_MODEL
// section 8.1 lists the nine general kinds (including image, video, audio and
// doc, which are what a free canvas produces), and PRD FR-050 lists eight
// production kinds for the asset bible. Character, location, prop and costume
// are in both. 'style' is section 8.1's generic style asset and
// 'style_reference' is FR-050's StyleReference; they are different things and
// both are kept. ADR-0007 records the ruling.
type Type string

const (
	TypeCharacter      Type = "character"
	TypeLocation       Type = "location"
	TypeProp           Type = "prop"
	TypeCostume        Type = "costume"
	TypeVehicle        Type = "vehicle"
	TypeCreature       Type = "creature"
	TypeStyle          Type = "style"
	TypeStyleReference Type = "style_reference"
	TypeDerivedAsset   Type = "derived_asset"
	TypeImage          Type = "image"
	TypeVideo          Type = "video"
	TypeAudio          Type = "audio"
	TypeDoc            Type = "doc"
)

// Types lists the documented asset types in the schema's order.
var Types = []Type{
	TypeCharacter, TypeLocation, TypeProp, TypeCostume, TypeVehicle, TypeCreature,
	TypeStyle, TypeStyleReference, TypeDerivedAsset, TypeImage, TypeVideo, TypeAudio, TypeDoc,
}

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
//
// It is an alias of the shared vocabulary rather than a second copy: DOMAIN_MODEL
// section 2.5 defines one set of statuses for every versioned aggregate, and
// eight per-family spellings would drift apart. WP-04 declared its own set with
// 'review' where the specification says 'under_review'; the alias replaces that
// spelling, which migration 000009 also rewrites in stored rows.
type VersionStatus = versioning.Status

const (
	VersionDraft       = versioning.StatusDraft
	VersionCandidate   = versioning.StatusCandidate
	VersionUnderReview = versioning.StatusUnderReview
	VersionApproved    = versioning.StatusApproved
	VersionRejected    = versioning.StatusRejected
	VersionSuperseded  = versioning.StatusSuperseded
	VersionDeprecated  = versioning.StatusDeprecated
	VersionStale       = versioning.StatusStale
)

// IsValidVersionStatus reports whether a version status may be persisted.
func IsValidVersionStatus(value VersionStatus) bool {
	return versioning.IsValidStatus(value)
}

// CreatedByType records who produced a version.
type CreatedByType = versioning.CreatedByType

const (
	CreatedByUser      = versioning.CreatedByUser
	CreatedByAgent     = versioning.CreatedByAgent
	CreatedByMigration = versioning.CreatedByMigration
	CreatedBySystem    = versioning.CreatedBySystem
)

// IsValidCreatedByType reports whether a producer kind may be persisted.
func IsValidCreatedByType(value CreatedByType) bool {
	return versioning.IsValidCreatedByType(value)
}

// Version is one version of an asset.
//
// EVERY COLUMN migration 000009 gave `asset_versions` has a field here, and the five
// that were missing until WP-09 are the ones AC-ASSET-002 names. The statement that
// each generated image must be traceable through "parent refs" and "agent/stage" was
// NOT satisfiable before them: `ParentAssetVersionID` and `VariantType` are the parent
// reference, `SourceAgentRunID` is the agent and stage the run recorded, `Seed` is what
// makes a regeneration reproducible, and `CreatedByID` is which agent or user produced
// it. A version stored with those columns blank is one whose lineage cannot be rebuilt.
type Version struct {
	ID            string
	AssetID       string
	VersionNumber int
	Status        VersionStatus
	// BasedOnVersionID links a revision to the version it edited.
	BasedOnVersionID string
	// ParentAssetVersionID names the version this one was DERIVED from, which is a
	// different relation from BasedOnVersionID: a revision follows a version of the
	// same asset, while a derivation may cross assets — a prop extracted from a
	// character's costume is derived from it without being a revision of it.
	ParentAssetVersionID string
	// VariantType names what kind of derivation this is, so §8.5's lineage edge carries
	// the meaning as well as the link.
	VariantType      string
	Prompt           string
	NegativePrompt   string
	ProviderConfigID string
	ModelConfigID    string
	ModelParameters  string
	// Seed is the provider's reproduction key. An empty seed is a version nothing can
	// reproduce exactly, which is the ordinary state of a provider that returns none.
	Seed string
	// GenerationJobID links a version to the job that produced it, when it came
	// from a generation.
	GenerationJobID string
	// SourceAgentRunID names the agent run that produced this version.
	SourceAgentRunID string
	Metadata         string
	CreatedByType    CreatedByType
	CreatedByID      string
	LegacyMetadata   string
	CreatedAt        time.Time
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
//
// The set is DOMAIN_MODEL section 8.4. WP-04 declared four roles and used
// 'attachment' where the specification has none; migration 000009 rebuilds the
// table to this list. first_frame and last_frame are what a storyboard panel
// needs to describe the two ends of a shot, and mask is what image editing
// writes.
type FileRole string

const (
	RolePrimary    FileRole = "primary"
	RoleThumbnail  FileRole = "thumbnail"
	RoleReference  FileRole = "reference"
	RoleMask       FileRole = "mask"
	RoleFirstFrame FileRole = "first_frame"
	RoleLastFrame  FileRole = "last_frame"
	RoleSource     FileRole = "source"
)

// FileRoles lists the documented roles in the schema's order.
var FileRoles = []FileRole{
	RolePrimary, RoleThumbnail, RoleReference, RoleMask, RoleFirstFrame, RoleLastFrame, RoleSource,
}

// IsValidFileRole reports whether a role may be persisted.
func IsValidFileRole(value FileRole) bool {
	for _, candidate := range FileRoles {
		if candidate == value {
			return true
		}
	}
	return false
}

// File links a version to a committed object.
//
// It carries a hash rather than a path: DOMAIN_MODEL §16 and ARCHITECTURE §8.3
// require paths to be reachable only through the FileStore, so the domain never
// sees one.
type File struct {
	ID        string
	VersionID string
	FileHash  string
	Role      FileRole
	// Ordinal orders files that share a role, which is what lets a panel list
	// several reference images in a stable sequence.
	Ordinal   int
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
	if f.Ordinal < 0 {
		return InvalidError("A file position cannot be negative.")
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

// RelationType is how one asset version relates to another (DOMAIN_MODEL §8.5).
type RelationType string

const (
	// RelationDerivedFrom traces a derived asset to the version it came from,
	// which is what PRD FR-050's "任何派生资产都能追溯父资产及变换原因" needs.
	RelationDerivedFrom RelationType = "derived_from"
	RelationVariantOf   RelationType = "variant_of"
	RelationReplaces    RelationType = "replaces"
	RelationReferences  RelationType = "references"
	RelationSupersedes  RelationType = "supersedes"
)

// RelationTypes lists the documented lineage relations in the schema's order.
var RelationTypes = []RelationType{
	RelationDerivedFrom, RelationVariantOf, RelationReplaces, RelationReferences, RelationSupersedes,
}

// IsValidRelationType reports whether a lineage relation may be persisted.
func IsValidRelationType(value RelationType) bool {
	for _, candidate := range RelationTypes {
		if candidate == value {
			return true
		}
	}
	return false
}

// Relation is one lineage edge between two asset versions.
type Relation struct {
	ID                   string
	SourceAssetVersionID string
	TargetAssetVersionID string
	Type                 RelationType
	CreatedAt            time.Time
}

// Validate checks the shape of a lineage edge.
func (r Relation) Validate() error {
	if strings.TrimSpace(r.SourceAssetVersionID) == "" || strings.TrimSpace(r.TargetAssetVersionID) == "" {
		return InvalidError("A lineage relation needs both ends.")
	}
	if r.SourceAssetVersionID == r.TargetAssetVersionID {
		return InvalidError("An asset version cannot derive from itself.")
	}
	if !IsValidRelationType(r.Type) {
		return InvalidError("The lineage relation type is not recognised.")
	}
	return nil
}

// ConsumerType is what uses an asset version (DOMAIN_MODEL §8.6).
type ConsumerType string

const (
	ConsumerProjectStyle ConsumerType = "project_style"
	ConsumerScene        ConsumerType = "scene"
	ConsumerShot         ConsumerType = "shot"
	// ConsumerStoryboardPanel is a storyboard panel's image reference (§9.5). The name
	// was misspelled "Pane" until WP-09, and it was REFERENCED NOWHERE — which is why
	// nothing caught it: a constant no code reads is a constant no compiler checks for
	// sense. WP-09's batch is its first user.
	ConsumerStoryboardPanel ConsumerType = "storyboard_panel"
	ConsumerJob             ConsumerType = "job"
	ConsumerExport          ConsumerType = "export"
)

// ConsumerTypes lists the documented consumers in the schema's order.
var ConsumerTypes = []ConsumerType{
	ConsumerProjectStyle, ConsumerScene, ConsumerShot, ConsumerStoryboardPanel, ConsumerJob, ConsumerExport,
}

// IsValidConsumerType reports whether a consumer kind may be persisted.
func IsValidConsumerType(value ConsumerType) bool {
	for _, candidate := range ConsumerTypes {
		if candidate == value {
			return true
		}
	}
	return false
}

// Usage records that something uses an asset version.
//
// Required is what makes a deletion safe to refuse: a required usage is a
// dependency the consumer cannot render without, so removing the version would
// break it.
type Usage struct {
	ID             string
	AssetVersionID string
	ConsumerType   ConsumerType
	ConsumerID     string
	UsageRole      string
	Required       bool
	CreatedAt      time.Time
}

// Validate checks the shape of a usage row.
func (u Usage) Validate() error {
	if strings.TrimSpace(u.AssetVersionID) == "" {
		return InvalidError("A usage must name an asset version.")
	}
	if !IsValidConsumerType(u.ConsumerType) {
		return InvalidError("The consumer type is not recognised.")
	}
	if strings.TrimSpace(u.ConsumerID) == "" {
		return InvalidError("A usage must name what consumes the asset.")
	}
	return nil
}
