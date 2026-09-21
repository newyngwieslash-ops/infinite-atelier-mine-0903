package database

import (
	"context"
	"database/sql"
	"strings"
	"sync"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
)

// ArtifactVerifier checks that the artifact references a model reported exist.
//
// It is AC-AGENT-003's mechanism: "模型返回不存在的 ID → Runtime 验证失败；不标记 success；
// 记录错误；Workflow 不前进". The runtime calls VerifyArtifacts after a document validates,
// and a reference that names nothing fails the stage — so a model that invented an
// identifier cannot make a workflow advance on the strength of it.
//
// WHY A TABLE PER ENTITY TYPE. The entities a stage can produce live in six tables that
// share no parent, so there is no single query that answers "does this id exist". A map of
// type to statement is the honest shape: it says out loud which references this build can
// check, and a type that is not in it is REFUSED rather than assumed to exist — which is
// the fail-closed direction, because a verifier that returned "fine" for an unknown type
// would let exactly the invention this exists to catch through.
//
// The statements are constants rather than built from the type name. A constructed table
// name would be SQL assembled from a model's output, which AGENTS section 8.3 forbids for
// reasons that apply here more than anywhere: the input is untrusted by construction.
type ArtifactVerifier struct {
	db *sql.DB

	// statements is resolved once from the package-level table, so a lookup cannot fail
	// partway through a run.
	statements map[string]string
	once       sync.Once
}

// NewArtifactVerifier builds the verifier over a database handle.
func NewArtifactVerifier(db *sql.DB) *ArtifactVerifier {
	return &ArtifactVerifier{db: db}
}

// artifactStatements maps an entity type to the statement that counts it.
//
// Every statement is `SELECT COUNT(*)` against a primary key, so it returns 0 or 1 and
// cannot be made to return a row of data. The type names are the ones the write tools
// return in their `artifacts` field (agenttools.artifactResult), which is what keeps the
// two ends of this contract in step: a tool that renamed its entityType would fail here
// rather than pass a check that never ran.
var artifactStatements = map[string]string{
	"story_skeleton_version":      `SELECT COUNT(*) FROM story_skeleton_versions WHERE id = ?`,
	"adaptation_strategy_version": `SELECT COUNT(*) FROM adaptation_strategy_versions WHERE id = ?`,
	"script_version":              `SELECT COUNT(*) FROM script_versions WHERE id = ?`,
	"director_plan_version":       `SELECT COUNT(*) FROM director_plan_versions WHERE id = ?`,
	"storyboard_version":          `SELECT COUNT(*) FROM storyboard_versions WHERE id = ?`,
	"storyboard_panel_version":    `SELECT COUNT(*) FROM storyboard_panel_versions WHERE id = ?`,
	"asset_version":               `SELECT COUNT(*) FROM asset_versions WHERE id = ?`,
	// The gap report, which WP-09's write tool reports as its artifact. It is a REPORT
	// rather than a version of one of the four families: it has its own table, its own
	// approval and its own precondition, so it has its own entry here.
	"asset_gap_report": `SELECT COUNT(*) FROM asset_gap_reports WHERE id = ?`,
	// A story skeleton's inputs are story facts rather than versions, and a stage reports
	// them the same way, so the same vocabulary covers them.
	"story_entity": `SELECT COUNT(*) FROM story_entities WHERE id = ? AND deleted_at = ''`,
	"story_event":  `SELECT COUNT(*) FROM story_events WHERE id = ?`,
}

// VerifiedTypes returns the entity types this build can check, sorted.
//
// It is exported so a test can compare it against the types the write tools report: a
// tool whose entityType is not in this set would produce a reference the verifier refuses,
// which would turn a successful write into a failed stage.
func VerifiedTypes() []string {
	types := make([]string, 0, len(artifactStatements))
	for name := range artifactStatements {
		types = append(types, name)
	}
	return types
}

// VerifyArtifacts reports the first reference that does not exist, or nil.
//
// The FIRST rather than all of them, because the refusal stops the run: a model told about
// six invented identifiers at once would still have to fix them one at a time, and the
// stage fails on the first either way. The error names the reference so the record says
// which one, and carries no content — the point is that there is none.
func (v *ArtifactVerifier) VerifyArtifacts(ctx context.Context, refs []agentruntime.ArtifactRef) error {
	if v == nil || v.db == nil {
		// No database is a refusal rather than a pass: a verifier that could not check
		// must not report that everything is fine.
		return agent.UnavailableError()
	}
	if len(refs) == 0 {
		return nil
	}
	v.once.Do(func() { v.statements = artifactStatements })
	for _, ref := range refs {
		entityType := strings.TrimSpace(ref.EntityType)
		entityID := strings.TrimSpace(ref.EntityID)
		if entityID == "" {
			return &agentruntime.ArtifactError{EntityType: entityType, EntityID: ""}
		}
		statement, known := v.statements[entityType]
		if !known {
			// An unknown type is refused rather than skipped. A model that named
			// "script_version_v2" has named something this build cannot check, and
			// treating an unverifiable reference as verified is the fail-open direction
			// AC-AGENT-003 exists to close.
			return &agentruntime.ArtifactError{EntityType: entityType, EntityID: entityID}
		}
		var count int
		if err := v.db.QueryRowContext(ctx, statement, entityID).Scan(&count); err != nil {
			// A query that failed is not a missing artifact: reporting it as one would
			// make a storage fault look like a hallucination, and a caller would stop
			// looking for the real cause.
			return agent.StorageError("An artifact reference could not be checked.", err)
		}
		if count == 0 {
			return &agentruntime.ArtifactError{EntityType: entityType, EntityID: entityID}
		}
	}
	return nil
}

// Compile-time proof that this type is what the runtime expects.
var _ agentruntime.ArtifactVerifier = (*ArtifactVerifier)(nil)
