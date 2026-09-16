package asset

import (
	"errors"
	"strings"
	"testing"
)

func TestTypeVocabulary(t *testing.T) {
	if len(Types) != 9 {
		t.Fatalf("registry has %d types, want the 9 documented in DOMAIN_MODEL §8.1", len(Types))
	}
	seen := map[Type]bool{}
	for _, value := range Types {
		if seen[value] {
			t.Fatalf("duplicate asset type %q", value)
		}
		seen[value] = true
		if !IsValidType(value) {
			t.Fatalf("registry entry %q does not validate", value)
		}
	}
	for _, value := range []Type{"", "sound", "Character", "image "} {
		if IsValidType(value) {
			t.Fatalf("undocumented asset type %q accepted", value)
		}
	}
}

func TestStatusAndProducerVocabulary(t *testing.T) {
	for _, value := range []Status{StatusActive, StatusArchived, StatusTrashed} {
		if !IsValidStatus(value) {
			t.Fatalf("documented asset status %q rejected", value)
		}
	}
	for _, value := range []Status{"", "deleted", "ACTIVE"} {
		if IsValidStatus(value) {
			t.Fatalf("undocumented asset status %q accepted", value)
		}
	}
	for _, value := range []CreatedByType{CreatedByUser, CreatedByAgent, CreatedByMigration, CreatedBySystem} {
		if !IsValidCreatedByType(value) {
			t.Fatalf("documented producer %q rejected", value)
		}
	}
	for _, value := range []CreatedByType{"", "robot", "User"} {
		if IsValidCreatedByType(value) {
			t.Fatalf("undocumented producer %q accepted", value)
		}
	}
}

func TestVersionStatusVocabulary(t *testing.T) {
	for _, value := range []VersionStatus{VersionDraft, VersionReview, VersionApproved, VersionRejected, VersionSuperseded, VersionStale} {
		if !IsValidVersionStatus(value) {
			t.Fatalf("documented version status %q rejected", value)
		}
	}
	for _, value := range []VersionStatus{"", "pending", "APPROVED"} {
		if IsValidVersionStatus(value) {
			t.Fatalf("undocumented version status %q accepted", value)
		}
	}
}

func TestValidateName(t *testing.T) {
	if err := ValidateName("Hero"); err != nil {
		t.Fatalf("a normal name was rejected: %v", err)
	}
	for _, bad := range []string{"", "   "} {
		if err := ValidateName(bad); err == nil {
			t.Fatalf("blank name %q accepted", bad)
		}
	}
	if err := ValidateName(strings.Repeat("字", MaxNameLength+1)); err == nil {
		t.Fatal("an over-length name was accepted")
	}
	if err := ValidateName(strings.Repeat("字", MaxNameLength)); err != nil {
		t.Fatalf("a name of exactly the limit was rejected: %v", err)
	}
}

func TestVersionValidate(t *testing.T) {
	good := Version{AssetID: "a-1", VersionNumber: 1, Status: VersionDraft, CreatedByType: CreatedByMigration}
	if err := good.Validate(); err != nil {
		t.Fatalf("a well-formed version was rejected: %v", err)
	}
	cases := []struct {
		name    string
		version Version
	}{
		{"no asset", Version{VersionNumber: 1, Status: VersionDraft, CreatedByType: CreatedByUser}},
		{"blank asset", Version{AssetID: "  ", VersionNumber: 1, Status: VersionDraft, CreatedByType: CreatedByUser}},
		{"version zero", Version{AssetID: "a", VersionNumber: 0, Status: VersionDraft, CreatedByType: CreatedByUser}},
		{"unknown status", Version{AssetID: "a", VersionNumber: 1, Status: "pending", CreatedByType: CreatedByUser}},
		{"unknown producer", Version{AssetID: "a", VersionNumber: 1, Status: VersionDraft, CreatedByType: "robot"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := testCase.version.Validate(); err == nil {
				t.Fatal("a malformed version was accepted")
			}
		})
	}
}

// TestCanApprove proves the approval preconditions decidable today: a version
// needs a committed file, and a superseded or stale version cannot be approved.
// The impact analysis DOMAIN_MODEL §8.2 also requires belongs to WP-05.
func TestCanApprove(t *testing.T) {
	base := Version{AssetID: "a-1", VersionNumber: 1, CreatedByType: CreatedByUser}

	base.Status = VersionDraft
	if err := base.CanApprove(1); err != nil {
		t.Fatalf("a draft with a file was refused approval: %v", err)
	}
	if err := base.CanApprove(0); err == nil {
		t.Fatal("a version with no committed file was allowed to be approved")
	}

	base.Status = VersionReview
	if err := base.CanApprove(2); err != nil {
		t.Fatalf("a reviewed version with files was refused approval: %v", err)
	}

	base.Status = VersionSuperseded
	if err := base.CanApprove(1); err == nil {
		t.Fatal("a superseded version was allowed to be approved")
	}
	base.Status = VersionStale
	if err := base.CanApprove(1); err == nil {
		t.Fatal("a stale version was allowed to be approved")
	}
	base.Status = VersionApproved
	if err := base.CanApprove(1); err == nil {
		t.Fatal("an approved version was allowed to be approved again")
	}
	base.Status = "bogus"
	if err := base.CanApprove(1); err == nil {
		t.Fatal("a version with an unknown status was allowed to be approved")
	}
}

func TestFileValidate(t *testing.T) {
	hash := strings.Repeat("a", 64)
	good := File{VersionID: "v-1", FileHash: hash, Role: RolePrimary}
	if err := good.Validate(); err != nil {
		t.Fatalf("a well-formed file link was rejected: %v", err)
	}
	cases := []struct {
		name string
		file File
	}{
		{"no version", File{FileHash: hash, Role: RolePrimary}},
		{"no hash", File{VersionID: "v-1", Role: RolePrimary}},
		{"short hash", File{VersionID: "v-1", FileHash: "abc", Role: RolePrimary}},
		{"uppercase hash", File{VersionID: "v-1", FileHash: strings.Repeat("A", 64), Role: RolePrimary}},
		{"non hex hash", File{VersionID: "v-1", FileHash: strings.Repeat("z", 64), Role: RolePrimary}},
		{"unknown role", File{VersionID: "v-1", FileHash: hash, Role: "poster"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := testCase.file.Validate(); err == nil {
				t.Fatal("a malformed file link was accepted")
			}
		})
	}
	// Every documented role is accepted.
	for _, role := range []FileRole{RolePrimary, RoleThumbnail, RoleSource, RoleAttachment} {
		if !IsValidFileRole(role) {
			t.Fatalf("documented role %q rejected", role)
		}
	}
}

func TestErrorsAreSafeAndClassified(t *testing.T) {
	if got := InvalidError("x").Category; got != CategoryInvalidInput {
		t.Fatalf("category = %q", got)
	}
	if got := ConflictError("x").Category; got != CategoryConflict {
		t.Fatalf("category = %q", got)
	}
	if got := NotFoundError().Category; got != CategoryNotFound {
		t.Fatalf("category = %q", got)
	}
	cause := errors.New("disk on fire at /home/user/secret/path")
	wrapped := StorageError("The asset could not be saved.", cause)
	if strings.Contains(wrapped.Error(), "secret") {
		t.Fatalf("the storage error leaked its cause: %q", wrapped.Error())
	}
	if !errors.Is(wrapped, cause) {
		t.Fatal("the cause is not reachable through errors.Is")
	}
	found, ok := AsError(errors.Join(errors.New("outer"), InvalidError("inner")))
	if !ok || found.Category != CategoryInvalidInput {
		t.Fatalf("AsError returned %+v, %v", found, ok)
	}
	// A nil error must not panic the extractor.
	if _, ok := AsError(nil); ok {
		t.Fatal("AsError(nil) reported a domain error")
	}
}
