// Package workflow is the application layer for durable workflow runs, their
// stage attempts, review reports and user gate decisions. It owns the commands
// and queries of DOMAIN_MODEL section 11 and PRD FR-100/FR-110, and defines the
// persistence ports that infrastructure implements. It performs no I/O itself.
//
// The engine that decides when a stage advances belongs to WP-07. What is here
// is the writer the schema already fixes: every state change is validated
// against the domain's state machines and recorded as an audit event in the
// same transaction as the change.
package workflow

import (
	"context"

	eventsapp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/events"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
)

// Clock abstracts time so tests are deterministic.
type Clock interface {
	Now() time.Time
}

// IDGenerator produces entity identifiers. ADR-0005 fixes the format as
// UUIDv7 and requires generation to happen here, in the application layer, so
// every repository receives an identifier it did not mint.
type IDGenerator interface {
	New() (string, error)
}

// Actor is who caused a state change.
//
// It reuses section 2.5's producer vocabulary because that is what the schema's
// actor_type and created_by columns accept, and because a run's history should
// be readable next to the version history it produced.
type Actor struct {
	Type versioning.CreatedByType
	ID   string
}

// CreatedByType names the author of a decision or an event.
//
// It is an alias for the shared section 2.5 producer vocabulary rather than a
// second set of values, so a caller of this package can fill in a request
// without importing the versioning package and a stored value means the same
// thing everywhere.
type CreatedByType = versioning.CreatedByType

// RunRepository persists workflow runs together with their audit events.
//
// The create and update calls take the event that records them because PRD
// FR-100 requires "每次状态变化写入审计事件" and the event must commit with the
// change it describes: a run whose status moved with no event, or an event for
// a change that rolled back, would both be wrong. The implementation writes
// both rows in one transaction; the application layer still mints the event's
// identifier (ADR-0005).
type RunRepository interface {
	CreateRun(ctx context.Context, record workflow.WorkflowRun, event workflow.WorkflowEvent) error
	GetRun(ctx context.Context, id string) (workflow.WorkflowRun, error)
	// UpdateRun persists a change guarded by the expected revision, with the
	// event recording it.
	UpdateRun(ctx context.Context, record workflow.WorkflowRun, expectedRevision int64, event workflow.WorkflowEvent) error
	// ListRuns returns a project's runs newest first.
	ListRuns(ctx context.Context, projectID string) ([]workflow.WorkflowRun, error)
}

// StageRepository persists stage attempts together with their audit events.
type StageRepository interface {
	CreateStage(ctx context.Context, record workflow.StageRun, event workflow.WorkflowEvent) error
	GetStage(ctx context.Context, id string) (workflow.StageRun, error)
	// UpdateStage persists a change guarded by the expected revision, with the
	// event recording it.
	UpdateStage(ctx context.Context, record workflow.StageRun, expectedRevision int64, event workflow.WorkflowEvent) error
	// ListStages returns a run's attempts oldest first.
	ListStages(ctx context.Context, workflowRunID string) ([]workflow.StageRun, error)
}

// ReviewRepository persists review reports and their issues.
type ReviewRepository interface {
	// CreateReport writes a report and its issues in one transaction, so a
	// report is never stored without the findings it is about.
	CreateReport(ctx context.Context, report workflow.ReviewReport, issues []workflow.ReviewIssue) error
	// GetReportForStage returns the newest report for a stage attempt. The
	// schema allows several (one per review), so the newest is the one the gate
	// acts on; found is false when the attempt has not been reviewed.
	GetReportForStage(ctx context.Context, stageRunID string) (report workflow.ReviewReport, issues []workflow.ReviewIssue, found bool, err error)
}

// DecisionRepository persists user gate decisions.
type DecisionRepository interface {
	CreateDecision(ctx context.Context, decision workflow.UserGateDecision) error
	// LatestDecisionForStage returns the newest decision recorded for one stage attempt.
	//
	// It exists because the decision row IS the FIX instruction, and the reason is not convenience:
	// section 12.2 stores the findings a FIX names on the DECISION (`issue_ids_json`), and the
	// pipeline that builds the next attempt reads them back HERE rather than receiving them as an
	// argument. A value threaded from the gate command to the re-run would be lost by a restart
	// between the two, and the re-run would then be a FIX that fixed nothing — while still being
	// recorded as one.
	//
	// The boolean is false when a stage has no decision yet, which is the ordinary state of an
	// attempt nobody has reviewed.
	LatestDecisionForStage(ctx context.Context, stageRunID string) (workflow.UserGateDecision, bool, error)
}

// EventRepository persists workflow audit events on their own, for the queries
// that read a run's history.
type EventRepository interface {
	// ListEvents returns a run's events oldest first.
	ListEvents(ctx context.Context, workflowRunID string) ([]workflow.WorkflowEvent, error)
}

// EventRecorder builds and writes domain events for the commands that emit them.
//
// Two paths, and the difference is deliberate (ADR-0009):
//
//   - Build assembles an event for a command that records it inside its own
//     transaction. An approval uses this, and refuses without a recorder,
//     because its event is a governance record rather than a notification.
//   - RecordBestEffort writes a notification event and reports no error. The
//     command's own write has already succeeded, so failing it because the
//     announcement did not land would be the wrong trade.
//
// The port is optional: a Service composed without a recorder still serves every
// command, and only the transactional path refuses.
type EventRecorder interface {
	Build(ctx context.Context, draft eventsapp.Draft) (event.Event, error)
	RecordBestEffort(ctx context.Context, draft eventsapp.Draft)
}

// Service holds the workflow commands and queries.
type Service struct {
	runs      RunRepository
	stages    StageRepository
	reviews   ReviewRepository
	decisions DecisionRepository
	events    EventRepository
	clock     Clock
	ids       IDGenerator
	recorder  EventRecorder
}

// Options configures a Service.
type Options struct {
	Runs      RunRepository
	Stages    StageRepository
	Reviews   ReviewRepository
	Decisions DecisionRepository
	Events    EventRepository
	Clock     Clock
	IDs       IDGenerator
	// Recorder enables the section 17 domain events. A nil value leaves the
	// commands working and their announcements unwritten.
	Recorder EventRecorder
}
