// Package staleness is the application layer for artifact staleness: recording
// that an artifact's input changed, and propagating that fact along the
// dependency graph of DOMAIN_MODEL section 15.
//
// The graph itself lives in the domain package, which owns DirectDependents,
// Downstream and Classify. This package does not restate it: it asks the domain
// for the affected artifact types and uses one port to find the concrete rows
// that hold the references. That split keeps a single description of the graph
// in the codebase instead of two that drift apart.
package staleness

import (
	"context"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/staleness"
)

// Clock abstracts time so tests are deterministic.
type Clock interface {
	Now() time.Time
}

// IDGenerator produces entity identifiers (ADR-0005).
//
// A mark is keyed by (artifact_type, artifact_id) — migration 000012 makes that
// pair the table's primary key — so this package mints no identifier. The port
// is still part of Options because every application service in this codebase
// takes the same Options{Repository, Clock, IDs} shape and fails closed without
// it (see Available), and a service that silently accepted a half-wired binding
// would be the one exception callers had to remember.
type IDGenerator interface {
	New() (string, error)
}

// MarkRepository persists staleness marks.
//
// The table is keyed by (artifact_type, artifact_id), because "which artifacts
// are stale" has one answer per artifact: two open marks for one artifact would
// leave the reader choosing a severity. Recording a mark therefore upserts, and
// the upsert increments the row's revision.
type MarkRepository interface {
	// UpsertMark stores one mark, replacing any existing mark for the same
	// artifact. updatedAt stamps the row's updated_at column.
	UpsertMark(ctx context.Context, mark staleness.Mark, updatedAt time.Time) error
	// UpsertMarks stores several marks in one transaction, so a propagation
	// either records every dependent it found or none of them.
	UpsertMarks(ctx context.Context, marks []staleness.Mark, updatedAt time.Time) error
	// GetMark returns one artifact's mark. found is false when the artifact has
	// none, so a caller can tell "never marked" from "could not read".
	GetMark(ctx context.Context, artifactType staleness.ArtifactType, artifactID string) (mark staleness.Mark, found bool, err error)
	// ListMarks returns a project's marks.
	ListMarks(ctx context.Context, projectID string) ([]staleness.Mark, error)
	// ListOpenMarks returns a project's marks that are not cleared.
	ListOpenMarks(ctx context.Context, projectID string) ([]staleness.Mark, error)
	// ClearMark stamps cleared_at on one artifact's mark and reports whether a
	// mark was there to clear. The row is kept rather than deleted, because
	// migration 000012 keeps it so the history of what was invalidated survives.
	ClearMark(ctx context.Context, artifactType staleness.ArtifactType, artifactID string, clearedAt time.Time) (found bool, err error)
	// WaiveMark records a waiver on one artifact's mark and reports whether a
	// mark was there to waive. The application validates the waiver first; this
	// call only writes the three columns a waiver occupies.
	WaiveMark(ctx context.Context, artifactType staleness.ArtifactType, artifactID, decisionID, reason string, updatedAt time.Time) (found bool, err error)
}

// ProjectEntityResolver answers which project an artifact instance belongs to.
//
// A stale mark names a project (migration 000012 makes project_id a foreign key
// with ON DELETE CASCADE), and a mark is read back per project, so a mark filed
// under the wrong project would be invisible to the project it concerns and
// visible to one it does not. Propagation therefore checks each dependent it
// finds against the project the caller named before marking it. An artifact
// whose row no longer exists resolves to nothing, which is the same race that
// deleting a chapter creates: propagation is often triggered by exactly that
// deletion, so a missing dependent is skipped rather than treated as a failure.
type ProjectEntityResolver interface {
	// ProjectFor returns the project an artifact instance belongs to. found is
	// false when no such row exists.
	ProjectFor(ctx context.Context, artifactType staleness.ArtifactType, artifactID string) (projectID string, found bool, err error)
}

// DependentRef names one concrete artifact instance that holds a reference to
// an upstream artifact.
type DependentRef struct {
	ArtifactType staleness.ArtifactType
	ArtifactID   string
}

// DependentFinder resolves the concrete instances of a dependent artifact type.
//
// It is deliberately ONE method. Propagation asks "which rows of type T
// reference this upstream row", and the mapping from an upstream artifact type
// to the columns that reference it is a schema fact: it belongs in the
// infrastructure implementation, which knows the columns of chapters,
// story_events, scenes, shots and the rest. A method per artifact family would
// move that table into the application layer and turn every new referencing
// column into an application change.
//
// The implementation returns an empty slice, not an error, for an upstream type
// it has no mapping for. Not every artifact type is referenced by a column (the
// domain's own OrchestrationTypes are marked directly by the code that knows
// why), so "nothing references this" is a legitimate answer.
//
// The finder is called once per (artifact type, upstream type) pair the domain
// graph asks about, so an implementation may execute one query per family.
type DependentFinder interface {
	FindDependents(ctx context.Context, artifactType, upstreamType staleness.ArtifactType, upstreamID string) ([]DependentRef, error)
}

// Service holds the staleness commands and queries.
type Service struct {
	marks      MarkRepository
	dependents DependentFinder
	projects   ProjectEntityResolver
	clock      Clock
	ids        IDGenerator
}

// Options configures a Service.
type Options struct {
	Marks      MarkRepository
	Dependents DependentFinder
	// Projects is what lets propagation keep a mark inside the project that
	// asked for it. It is optional for the single-mark commands, which are
	// given the project by their caller, and required by PropagateFrom, which
	// returns a storage failure rather than filing marks under a project it
	// could not check.
	Projects ProjectEntityResolver
	Clock    Clock
	IDs      IDGenerator
}
