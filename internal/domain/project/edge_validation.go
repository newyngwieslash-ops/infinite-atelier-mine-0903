package project

import "strings"

// EdgeValidationResult is the outcome of checking one semantic edge.
type EdgeValidationResult struct {
	Status EdgeValidationStatus
	// Reason is a safe message explaining a rejection. It is empty for a valid
	// edge so the common case carries nothing extra across the boundary.
	Reason string
}

// EdgeEndpoint describes one end of a semantic edge.
//
// It carries the kind and identity a node declares, plus whether the node is a
// projection at all. A node that projects nothing cannot take part in a
// semantic relation, because there is no entity to relate.
type EdgeEndpoint struct {
	// NodeID is the canvas node, used only to build messages.
	NodeID string
	// EntityType is empty for a node that projects nothing.
	EntityType EntityRefType
	// EntityID is the projected entity.
	EntityID string
	// EntityVersionID is the version the projection names, when it names one.
	EntityVersionID string
	// ProjectID is the project the projected entity belongs to, when known.
	// It is empty when the caller could not resolve it, in which case the
	// same-project check is skipped rather than failed.
	ProjectID string
}

// IsProjection reports whether the endpoint names an entity.
func (e EdgeEndpoint) IsProjection() bool {
	return strings.TrimSpace(string(e.EntityType)) != "" && strings.TrimSpace(e.EntityID) != ""
}

// EdgeValidationRequest is everything checking one edge needs.
type EdgeValidationRequest struct {
	RelationType RelationType
	From         EdgeEndpoint
	To           EdgeEndpoint
}

// ValidateEdgeRelation checks a semantic edge against the §10.4 registry.
//
// The rules, in the order they are applied:
//
//  1. The relation type must be registered. WP-04 already refuses an unknown
//     type when an edge is created, and this repeats the check because the
//     registry is also consulted when an existing edge's endpoints change.
//  2. generic is exempt from the endpoint rules. PRD FR-130 requires legacy
//     untyped connections to survive as generic, so such an edge is stored and
//     reported as unknown rather than rejected: the import cannot invent a
//     meaning the original data never had.
//  3. Both ends of a non-generic relation must be projections. A semantic edge
//     between two nodes that name no entity would be a line with a meaning and
//     nothing to mean it about.
//  4. The endpoint kinds must be the ones the registry allows.
//  5. requires_version means the projection must name the version it relates,
//     because DOMAIN_MODEL §10.2 says "version_id 必须属于 entity" and a
//     relation against "the character" rather than a specific costume version
//     is not specific enough to be checked later.
//  6. The same-project check runs only when both projects are known; an
//     unresolved project is not treated as a mismatch.
//
// The result's status is valid, invalid or unknown. unknown is what an
// accepted-but-unconstrained edge gets, which is what the generic fallback and
// any unresolved check produce.
func ValidateEdgeRelation(request EdgeValidationRequest) EdgeValidationResult {
	definition, registered := Relation(request.RelationType)
	if !registered {
		return EdgeValidationResult{
			Status: EdgeInvalid,
			Reason: "That connection type is not registered.",
		}
	}
	if request.RelationType == RelationGeneric {
		// Unconstrained by design: an imported connection had no declared
		// meaning, so there is nothing to validate and nothing to reject.
		return EdgeValidationResult{Status: EdgeUnknown}
	}
	if !request.From.IsProjection() || !request.To.IsProjection() {
		return EdgeValidationResult{
			Status: EdgeInvalid,
			Reason: "A semantic connection needs both ends to reference an entity.",
		}
	}
	if !definition.RelationAllowsSource(request.From.EntityType) {
		return EdgeValidationResult{
			Status: EdgeInvalid,
			Reason: "This connection cannot start from that kind of entity.",
		}
	}
	if !definition.AllowsTarget(request.To.EntityType) {
		return EdgeValidationResult{
			Status: EdgeInvalid,
			Reason: "This connection cannot point at that kind of entity.",
		}
	}
	if definition.RequiresVersion {
		if strings.TrimSpace(request.From.EntityVersionID) == "" || strings.TrimSpace(request.To.EntityVersionID) == "" {
			return EdgeValidationResult{
				Status: EdgeInvalid,
				Reason: "This connection must name the versions it relates.",
			}
		}
	}
	if request.From.ProjectID != "" && request.To.ProjectID != "" && request.From.ProjectID != request.To.ProjectID {
		return EdgeValidationResult{
			Status: EdgeInvalid,
			Reason: "This connection would link entities from two different projects.",
		}
	}
	return EdgeValidationResult{Status: EdgeValid}
}
