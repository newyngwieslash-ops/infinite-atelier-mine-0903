package project

import "testing"

// TestRelationRegistryCoversTheSpecifiedRelations pins the registry against
// both lists it is built from: DOMAIN_MODEL §10.4 and PRD FR-130's additions.
func TestRelationRegistryCoversTheSpecifiedRelations(t *testing.T) {
	// §10.4's initial list, word for word.
	domainModel := []RelationType{
		"contains", "adapts_to", "references", "derived_from", "continues_from",
		"generated_by", "reviewed_by", "supersedes", "first_frame_of", "last_frame_of",
		"appears_in", "located_in", "uses_asset", "causes", "precedes", "contradicts",
	}
	// PRD FR-130's list, which overlaps but is not identical.
	fr130 := []RelationType{
		"contains", "adapts_to", "references", "derived_from", "continues_from",
		"generated_by", "reviewed_by", "supersedes", "first_frame_of", "last_frame_of",
		"uses_character", "uses_location", "uses_prop",
	}
	for _, relation := range domainModel {
		if _, ok := Relation(relation); !ok {
			t.Fatalf("DOMAIN_MODEL §10.4 relation %q is not registered", relation)
		}
	}
	for _, relation := range fr130 {
		if _, ok := Relation(relation); !ok {
			t.Fatalf("PRD FR-130 relation %q is not registered", relation)
		}
	}
	// The names WP-04 already allowed must still be allowed, or the migration
	// checks and the stored edges disagree.
	for _, relation := range RelationTypes {
		if relation == RelationGeneric {
			continue
		}
		if _, ok := Relation(relation); !ok {
			t.Fatalf("WP-04 relation %q has no registry entry, so stored edges cannot be validated", relation)
		}
	}
	// generic is registered and is the only unconstrained one.
	generic, ok := Relation(RelationGeneric)
	if !ok {
		t.Fatal("the generic fallback must be registered")
	}
	if len(generic.AllowedSource) != 0 || len(generic.AllowedTarget) != 0 {
		t.Fatal("generic must stay unconstrained, or imported untyped links would be rejected")
	}
	if _, ok := Relation("not_a_relation"); ok {
		t.Fatal("an unregistered relation must not resolve")
	}
}

// TestRelationDefinitionsCarryEveryDocumentedField proves each entry fills the
// seven fields §10.4 lists. A half-filled entry would validate nothing.
func TestRelationDefinitionsCarryEveryDocumentedField(t *testing.T) {
	definitions := RelationDefinitions()
	if len(definitions) == 0 {
		t.Fatal("the registry is empty")
	}
	seen := map[RelationType]bool{}
	for _, definition := range definitions {
		if seen[definition.Type] {
			t.Fatalf("relation %q is registered twice", definition.Type)
		}
		seen[definition.Type] = true
		if !IsValidRelationType(definition.Type) {
			t.Fatalf("registry entry %q is not a documented relation type", definition.Type)
		}
		if definition.Directionality != Directional && definition.Directionality != Bidirectional {
			t.Fatalf("relation %s has an empty directionality", definition.Type)
		}
		if definition.Cardinality != CardinalityOneToOne && definition.Cardinality != CardinalityOneToMany {
			t.Fatalf("relation %s has an empty cardinality", definition.Type)
		}
		switch definition.ValidationHandler {
		case HandlerEndpointTypes, HandlerVersionOwnership, HandlerSameProject:
		default:
			t.Fatalf("relation %s has no validation handler", definition.Type)
		}
		// Every declared endpoint kind must be a real reference kind.
		for _, kind := range definition.AllowedSource {
			if !IsValidEntityRefType(kind) {
				t.Fatalf("relation %s allows source %q, which is not a reference kind", definition.Type, kind)
			}
		}
		for _, kind := range definition.AllowedTarget {
			if !IsValidEntityRefType(kind) {
				t.Fatalf("relation %s allows target %q, which is not a reference kind", definition.Type, kind)
			}
		}
		// Only generic may leave both lists open.
		if definition.Type != RelationGeneric && (len(definition.AllowedSource) == 0 || len(definition.AllowedTarget) == 0) {
			t.Fatalf("relation %s constrains nothing, so it could never be invalid", definition.Type)
		}
	}
}

// TestValidateEdgeRelationAcceptsALegalReferences is the first half of
// AC-CANVAS-002: "合法 references 成功".
func TestValidateEdgeRelationAcceptsALegalReferences(t *testing.T) {
	result := ValidateEdgeRelation(EdgeValidationRequest{
		RelationType: "references",
		From: EdgeEndpoint{
			NodeID:          "node-shot",
			EntityType:      EntityShot,
			EntityID:        "shot-1",
			EntityVersionID: "shot-1-v1",
			ProjectID:       "project-1",
		},
		To: EdgeEndpoint{
			NodeID:          "node-character",
			EntityType:      EntityAssetVersion,
			EntityID:        "asset-version-1",
			EntityVersionID: "asset-version-1",
			ProjectID:       "project-1",
		},
	})
	if result.Status != EdgeValid {
		t.Fatalf("a shot referencing a character version must be valid, got %q (%s)", result.Status, result.Reason)
	}
	if result.Reason != "" {
		t.Fatalf("a valid edge must carry no reason, got %q", result.Reason)
	}
}

// TestValidateEdgeRelationRejectsIllegalEndpoints is the second half of
// AC-CANVAS-002: "非法 source/target 拒绝".
func TestValidateEdgeRelationRejectsIllegalEndpoints(t *testing.T) {
	valid := EdgeEndpoint{
		NodeID:          "node-a",
		EntityType:      EntityShot,
		EntityID:        "shot-1",
		EntityVersionID: "shot-1-v1",
		ProjectID:       "project-1",
	}
	target := EdgeEndpoint{
		NodeID:          "node-b",
		EntityType:      EntityAssetVersion,
		EntityID:        "asset-version-1",
		EntityVersionID: "asset-version-1",
		ProjectID:       "project-1",
	}
	cases := []struct {
		name    string
		request EdgeValidationRequest
		want    EdgeValidationStatus
		why     string
	}{
		{
			name: "an unregistered relation",
			request: EdgeValidationRequest{
				RelationType: "invented",
				From:         valid,
				To:           target,
			},
			want: EdgeInvalid,
			why:  "an unknown meaning must be refused rather than stored",
		},
		{
			name: "a source whose kind the relation forbids",
			request: EdgeValidationRequest{
				RelationType: "references",
				From: EdgeEndpoint{
					NodeID: "node-run", EntityType: EntityWorkflowRun,
					EntityID: "run-1", EntityVersionID: "run-1", ProjectID: "project-1",
				},
				To: EdgeEndpoint{
					NodeID: "node-asset", EntityType: EntityAssetVersion,
					EntityID: "av-1", EntityVersionID: "av-1", ProjectID: "project-1",
				},
			},
			want: EdgeInvalid,
			why:  "a workflow run is orchestration, not content: it is related by generated_by or reviewed_by",
		},
		{
			name: "a target whose kind the relation forbids",
			request: EdgeValidationRequest{
				RelationType: "uses_character",
				From:         valid,
				To: EdgeEndpoint{
					NodeID: "node-chapter", EntityType: EntityChapter,
					EntityID: "ch-1", EntityVersionID: "ch-1", ProjectID: "project-1",
				},
			},
			want: EdgeInvalid,
			why:  "a chapter is not a character version",
		},
		{
			name: "an endpoint that projects nothing",
			request: EdgeValidationRequest{
				RelationType: "references",
				From:         valid,
				To:           EdgeEndpoint{NodeID: "node-text"},
			},
			want: EdgeInvalid,
			why:  "a semantic relation needs an entity at both ends",
		},
		{
			name: "a versioned endpoint that names no version",
			request: EdgeValidationRequest{
				RelationType: "uses_character",
				From:         valid,
				To: EdgeEndpoint{
					// An asset_version with no version id: the entity kind is
					// versioned, so omitting its version leaves the relation
					// unable to say WHICH costume the shot uses.
					NodeID: "node-asset", EntityType: EntityAssetVersion,
					EntityID: "av-1", ProjectID: "project-1",
				},
			},
			want: EdgeInvalid,
			why:  "an asset version must name its version",
		},
		{
			name: "two entities from different projects",
			request: EdgeValidationRequest{
				RelationType: "references",
				From:         valid,
				To: EdgeEndpoint{
					NodeID: "node-asset", EntityType: EntityAssetVersion,
					EntityID: "av-1", EntityVersionID: "av-1", ProjectID: "project-2",
				},
			},
			want: EdgeInvalid,
			why:  "a relation must not silently link two dramas",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := ValidateEdgeRelation(testCase.request)
			if result.Status != testCase.want {
				t.Fatalf("status = %q, want %q (%s)", result.Status, testCase.want, testCase.why)
			}
			if testCase.want == EdgeInvalid && result.Reason == "" {
				t.Fatal("a rejected edge must explain itself")
			}
		})
	}
}

// TestValidateEdgeRelationAcceptsAVersionOnTheVersionedSideOnly covers the rule
// that the version demand applies where versions exist.
//
// A shot has no version column (§7.8), so demanding a version of it would make
// `uses_character` — the relation PRD FR-130 exists for — impossible to satisfy:
// every shot-to-costume link would be rejected. The asset side is where the
// version lives, and it is still required there.
func TestValidateEdgeRelationAcceptsAVersionOnTheVersionedSideOnly(t *testing.T) {
	shot := EdgeEndpoint{
		// No EntityVersionID: a shot is not versioned.
		NodeID: "node-shot", EntityType: EntityShot, EntityID: "shot-1", ProjectID: "project-1",
	}
	costume := EdgeEndpoint{
		NodeID: "node-costume", EntityType: EntityAssetVersion,
		EntityID: "av-1", EntityVersionID: "av-1-v3", ProjectID: "project-1",
	}
	result := ValidateEdgeRelation(EdgeValidationRequest{
		RelationType: "uses_character", From: shot, To: costume,
	})
	if result.Status != EdgeValid {
		t.Fatalf("a shot using a costume version must be valid, got %q (%s)", result.Status, result.Reason)
	}
	// first_frame_of is the same shape: the frame carries the version, the shot
	// does not. It was equally unusable before the rule was made per-endpoint.
	frame := EdgeEndpoint{
		NodeID: "node-frame", EntityType: EntityAssetVersion,
		EntityID: "av-frame", EntityVersionID: "av-frame-v1", ProjectID: "project-1",
	}
	result = ValidateEdgeRelation(EdgeValidationRequest{
		RelationType: "first_frame_of", From: frame, To: shot,
	})
	if result.Status != EdgeValid {
		t.Fatalf("a first-frame version pointing at a shot must be valid, got %q (%s)", result.Status, result.Reason)
	}
	// A relation whose versioned side names nothing is still refused.
	result = ValidateEdgeRelation(EdgeValidationRequest{
		RelationType: "first_frame_of",
		From:         EdgeEndpoint{NodeID: "node-frame", EntityType: EntityAssetVersion, EntityID: "av-frame", ProjectID: "project-1"},
		To:           shot,
	})
	if result.Status != EdgeInvalid {
		t.Fatalf("a frame version without its version must be refused, got %q", result.Status)
	}
}

// TestValidateEdgeRelationKeepsGenericUnknown covers PRD FR-130's requirement
// that migrated untyped connections survive. They must be stored, and they must
// be reported as unvalidated rather than as valid, because nothing about them
// was checked.
func TestValidateEdgeRelationKeepsGenericUnknown(t *testing.T) {
	// Two free nodes with no entities at all: the least checkable case, and the
	// one an imported free-canvas connection looks like.
	result := ValidateEdgeRelation(EdgeValidationRequest{
		RelationType: RelationGeneric,
		From:         EdgeEndpoint{NodeID: "node-a"},
		To:           EdgeEndpoint{NodeID: "node-b"},
	})
	if result.Status != EdgeUnknown {
		t.Fatalf("a generic edge must be stored as unknown, got %q (%s)", result.Status, result.Reason)
	}
	// Even with entities present, generic stays unknown: it carries no meaning to
	// validate against.
	result = ValidateEdgeRelation(EdgeValidationRequest{
		RelationType: RelationGeneric,
		From:         EdgeEndpoint{NodeID: "node-a", EntityType: EntityShot, EntityID: "shot-1"},
		To:           EdgeEndpoint{NodeID: "node-b", EntityType: EntityAssetVersion, EntityID: "av-1"},
	})
	if result.Status != EdgeUnknown {
		t.Fatalf("generic must not be validated against endpoint kinds, got %q", result.Status)
	}
	// An empty relation type is refused outright rather than defaulted here:
	// the default lives in the create command, so an empty value reaching this
	// function means the caller skipped it.
	result = ValidateEdgeRelation(EdgeValidationRequest{
		RelationType: "",
		From:         EdgeEndpoint{NodeID: "node-a"},
		To:           EdgeEndpoint{NodeID: "node-b"},
	})
	if result.Status != EdgeInvalid {
		t.Fatalf("an empty relation type must be refused, got %q", result.Status)
	}
}

// TestValidateEdgeRelationSkipsUnresolvedProject covers the deliberate
// tolerance: a caller that could not resolve a project must not have its edge
// rejected, because "unknown" is not "mismatched".
func TestValidateEdgeRelationSkipsUnresolvedProject(t *testing.T) {
	result := ValidateEdgeRelation(EdgeValidationRequest{
		RelationType: "references",
		From: EdgeEndpoint{
			NodeID: "node-shot", EntityType: EntityShot,
			EntityID: "shot-1", EntityVersionID: "shot-1-v1",
		},
		To: EdgeEndpoint{
			NodeID: "node-asset", EntityType: EntityAssetVersion,
			EntityID: "av-1", EntityVersionID: "av-1",
		},
	})
	if result.Status != EdgeValid {
		t.Fatalf("an unresolved project must not fail the check, got %q (%s)", result.Status, result.Reason)
	}
}

// TestEntityRefVocabulary pins the reference kinds, including the ones the
// studio shell and the projection writer address.
func TestEntityRefVocabulary(t *testing.T) {
	for _, kind := range []EntityRefType{
		EntityEpisode, EntityScene, EntityShot, EntityAssetVersion, EntityStoryEntity,
		EntityStoryEvent, EntityStoryboardItem, EntityStoryboardPanel, EntityScriptVersion,
	} {
		if !IsValidEntityRefType(kind) {
			t.Fatalf("documented reference kind %q is not recognised", kind)
		}
	}
	for _, kind := range []EntityRefType{"", "scene ", "Scene", "unknown_thing"} {
		if IsValidEntityRefType(kind) {
			t.Fatalf("undocumented reference kind %q accepted", kind)
		}
	}
	seen := map[EntityRefType]bool{}
	for _, kind := range EntityRefTypes {
		if seen[kind] {
			t.Fatalf("reference kind %q is listed twice", kind)
		}
		seen[kind] = true
	}
}
