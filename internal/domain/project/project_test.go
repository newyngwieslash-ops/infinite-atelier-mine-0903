package project

import (
	"errors"
	"strings"
	"testing"
)

// TestProjectTypeVocabulary pins the two types DOMAIN_MODEL §8 defines and
// rejects everything else, including the values a legacy store might carry.
func TestProjectTypeVocabulary(t *testing.T) {
	for _, value := range []ProjectType{ProjectFreeCanvas, ProjectDrama} {
		if !IsValidProjectType(value) {
			t.Fatalf("documented project type %q rejected", value)
		}
	}
	for _, value := range []ProjectType{"", "freeCanvas", "FREE_CANVAS", "short_film", "canvas"} {
		if IsValidProjectType(value) {
			t.Fatalf("undocumented project type %q accepted", value)
		}
	}
}

func TestProjectStatusVocabulary(t *testing.T) {
	for _, value := range []ProjectStatus{ProjectActive, ProjectArchived, ProjectTrashed} {
		if !IsValidProjectStatus(value) {
			t.Fatalf("documented status %q rejected", value)
		}
	}
	for _, value := range []ProjectStatus{"", "deleted", "active ", "ACTIVE"} {
		if IsValidProjectStatus(value) {
			t.Fatalf("undocumented status %q accepted", value)
		}
	}
}

// TestProjectTransitions proves a trashed project cannot skip back to archived
// and that an unknown status is refused in either direction.
func TestProjectTransitions(t *testing.T) {
	cases := []struct {
		from ProjectStatus
		to   ProjectStatus
		want bool
	}{
		{ProjectActive, ProjectArchived, true},
		{ProjectActive, ProjectTrashed, true},
		{ProjectActive, ProjectActive, true},
		{ProjectArchived, ProjectActive, true},
		{ProjectArchived, ProjectTrashed, true},
		{ProjectTrashed, ProjectActive, true},
		{ProjectTrashed, ProjectArchived, false},
		{ProjectTrashed, ProjectTrashed, true},
		{ProjectStatus("bogus"), ProjectActive, false},
		{ProjectActive, ProjectStatus("bogus"), false},
	}
	for _, testCase := range cases {
		record := Project{Status: testCase.from}
		if got := record.CanTransition(testCase.to); got != testCase.want {
			t.Errorf("CanTransition(%q -> %q) = %v, want %v", testCase.from, testCase.to, got, testCase.want)
		}
	}
}

func TestValidateProjectName(t *testing.T) {
	if err := ValidateProjectName("A project"); err != nil {
		t.Fatalf("a normal name was rejected: %v", err)
	}
	for _, bad := range []string{"", "   ", "\t\n"} {
		if err := ValidateProjectName(bad); err == nil {
			t.Fatalf("blank name %q accepted", bad)
		}
	}
	tooLong := strings.Repeat("字", MaxProjectNameLength+1)
	if err := ValidateProjectName(tooLong); err == nil {
		t.Fatal("an over-length name was accepted")
	}
	// The limit counts runes, not bytes: a name of exactly the limit in CJK
	// characters is valid even though it is three times as many bytes.
	atLimit := strings.Repeat("字", MaxProjectNameLength)
	if err := ValidateProjectName(atLimit); err != nil {
		t.Fatalf("a name of exactly %d characters was rejected: %v", MaxProjectNameLength, err)
	}
}

// TestNodeProjectionInvariant proves half a reference is refused. The schema has
// the same CHECK; this catches it earlier and with a usable message.
func TestNodeProjectionInvariant(t *testing.T) {
	standalone := Node{NodeType: "text"}
	if err := ValidateNode(standalone); err != nil {
		t.Fatalf("a standalone node was rejected: %v", err)
	}
	if standalone.IsProjection() {
		t.Fatal("a standalone node reports itself as a projection")
	}

	complete := Node{NodeType: "image", EntityType: "storyboard_panel", EntityID: "panel-1"}
	if err := ValidateNode(complete); err != nil {
		t.Fatalf("a complete projection was rejected: %v", err)
	}
	if !complete.IsProjection() {
		t.Fatal("a complete reference does not report itself as a projection")
	}

	halfType := Node{NodeType: "image", EntityType: "storyboard_panel"}
	if err := ValidateNode(halfType); err == nil {
		t.Fatal("a node with an entity type but no id was accepted")
	}
	halfID := Node{NodeType: "image", EntityID: "panel-1"}
	if err := ValidateNode(halfID); err == nil {
		t.Fatal("a node with an entity id but no type was accepted")
	}
	// Whitespace does not count as a reference.
	blank := Node{NodeType: "image", EntityType: "  ", EntityID: "  "}
	if blank.IsProjection() {
		t.Fatal("whitespace was treated as an entity reference")
	}
}

func TestValidateNodeRejectsMalformedInput(t *testing.T) {
	if err := ValidateNode(Node{}); err == nil {
		t.Fatal("a node with no type was accepted")
	}
	if err := ValidateNode(Node{NodeType: "text", Width: -1}); err == nil {
		t.Fatal("a negative width was accepted")
	}
	if err := ValidateNode(Node{NodeType: "text", Height: -1}); err == nil {
		t.Fatal("a negative height was accepted")
	}
	longType := strings.Repeat("x", MaxNodeTypeLength+1)
	if err := ValidateNode(Node{NodeType: longType}); err == nil {
		t.Fatal("an over-length node type was accepted")
	}
	// Plugin node types are open strings, so a namespaced type is valid.
	if err := ValidateNode(Node{NodeType: "acme:custom-widget"}); err != nil {
		t.Fatalf("a plugin node type was rejected: %v", err)
	}
}

// TestRelationRegistry proves the registry matches the schema's CHECK list and
// that "generic" is present, because migrated untyped connections depend on it.
func TestRelationRegistry(t *testing.T) {
	if !IsValidRelationType(RelationGeneric) {
		t.Fatal("the generic relation is missing; migrated connections would have nowhere to go")
	}
	if len(RelationTypes) != 20 {
		t.Fatalf("registry has %d entries, want 20 (generic plus the 19 documented relations)", len(RelationTypes))
	}
	seen := map[RelationType]bool{}
	for _, value := range RelationTypes {
		if seen[value] {
			t.Fatalf("duplicate relation type %q in the registry", value)
		}
		seen[value] = true
		if !IsValidRelationType(value) {
			t.Fatalf("registry entry %q does not validate", value)
		}
	}
	for _, value := range []RelationType{"", "GENERIC", "generic ", "references ", "points_at", "uses"} {
		if IsValidRelationType(value) {
			t.Fatalf("undocumented relation %q accepted", value)
		}
	}
}

func TestEdgeValidationStatusVocabulary(t *testing.T) {
	for _, value := range []EdgeValidationStatus{EdgeValid, EdgeInvalid, EdgeStale, EdgeUnknown} {
		if !IsValidEdgeValidationStatus(value) {
			t.Fatalf("documented status %q rejected", value)
		}
	}
	for _, value := range []EdgeValidationStatus{"", "checked", "TRUE"} {
		if IsValidEdgeValidationStatus(value) {
			t.Fatalf("undocumented status %q accepted", value)
		}
	}
}

func TestCanvasKindVocabulary(t *testing.T) {
	for _, value := range []CanvasKind{CanvasFree, CanvasDrama, CanvasEpisode, CanvasStoryboard, CanvasAsset} {
		if !IsValidCanvasKind(value) {
			t.Fatalf("documented canvas kind %q rejected", value)
		}
	}
	for _, value := range []CanvasKind{"", "timeline", "Free", "story_board"} {
		if IsValidCanvasKind(value) {
			t.Fatalf("undocumented canvas kind %q accepted", value)
		}
	}
}

// TestViewportUsability proves a degenerate transform is reported rather than
// applied: a zero or negative scale would render an invisible canvas.
func TestViewportUsability(t *testing.T) {
	if !(Viewport{X: 10, Y: -20, K: 1.5}).IsUsable() {
		t.Fatal("a normal viewport was rejected")
	}
	for _, value := range []Viewport{{}, {K: 0}, {K: -1}} {
		if value.IsUsable() {
			t.Fatalf("degenerate viewport %+v reported as usable", value)
		}
	}
	nan := 0.0
	nan = nan / nan
	if (Viewport{K: nan}).IsUsable() {
		t.Fatal("a NaN scale reported as usable")
	}
	if (Viewport{X: nan, K: 1}).IsUsable() {
		t.Fatal("a NaN translation reported as usable")
	}
}

// TestErrorCategoriesAreStableAndSafe proves the error type never leaks an
// identifier or a cause message through Error().
func TestErrorCategoriesAreStableAndSafe(t *testing.T) {
	if got := InvalidError("bad input").Category; got != CategoryInvalidInput {
		t.Fatalf("category = %q", got)
	}
	if got := NotFoundError().Category; got != CategoryNotFound {
		t.Fatalf("category = %q", got)
	}
	if got := RevisionMismatchError().Category; got != CategoryConflict {
		t.Fatalf("category = %q", got)
	}
	cause := errors.New("pq: relation \"projects\" does not exist")
	wrapped := StorageError("The item could not be saved.", cause)
	if strings.Contains(wrapped.Error(), "projects") {
		t.Fatalf("the storage error leaked its cause: %q", wrapped.Error())
	}
	if !errors.Is(wrapped, cause) {
		t.Fatal("the cause is not reachable through errors.Is")
	}

	importErr := &ImportError{Stage: "transform", SafeMessage: "The project could not be converted."}
	if !strings.Contains(importErr.Error(), "transform") {
		t.Fatalf("the stage is missing from the message: %q", importErr.Error())
	}
	extracted, ok := AsImportError(importErr)
	if !ok || extracted.Stage != "transform" {
		t.Fatalf("AsImportError returned %+v, %v", extracted, ok)
	}
	domainErr, ok := AsError(errors.Join(errors.New("outer"), InvalidError("inner")))
	if !ok || domainErr.Category != CategoryInvalidInput {
		t.Fatalf("AsError did not find the wrapped domain error: %+v, %v", domainErr, ok)
	}
}
