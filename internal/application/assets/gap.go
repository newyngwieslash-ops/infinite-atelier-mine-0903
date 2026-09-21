package assets

import (
	"context"
	"strings"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
)

// gap.go is the Asset Gap Report's commands and queries.
//
// The report is what AC-BOARD-001's "必需资产缺失时阻止批量" reads, and it is an
// AGENT's analysis that a USER approves — §10.1 gives asset_analysis `userGate:
// required` and `supervision: false`. Those two halves are why this file has both a
// model-facing write path and a human approval path, and why they are separate
// commands: a model may say what it found, and only a person may put it in force.
//
// # Why there is no event here
//
// Every other approval in this build writes a `domain_events` row in the same
// transaction, and this one cannot: DOMAIN_MODEL §17's event list is CLOSED — the
// domain's own `IsValidType` enforces it — and it has no name for this artifact. The
// list carries AssetVersionCreated/Approved, DirectorPlanApproved and
// StoryboardVersionApproved, and nothing for a gap report.
//
// Inventing a name would be worse than omitting one. A consumer filtering the stream
// by type would have to know a vocabulary the specification does not define, and the
// closed list exists precisely so that a typo is a failure rather than a silent hole.
// So the approval is governed where the OTHER user gates are: §10.1's user gate on the
// asset_analysis stage records a `UserGateDecided` on the workflow's stream with the
// user, the stage and the time. What this service stores is the LINK — the decision's
// trace identifier on the report's own row — so a reader can find the decision that
// put the report in force rather than matching timestamps. ADR-0013 records the ruling
// and its cost.

// GapRepository is the persistence this service needs, declared beside its use.
//
// It is separate from `Repository` because a gap report is a different aggregate from
// an asset: the two share a store and nothing else, and folding these methods into the
// asset port would make every asset fake implement report reads it never serves.
type GapRepository interface {
	// CreateGapReportWithItems writes the report and its lines in one transaction, so a
	// report is never stored without the lines its approval is about.
	CreateGapReportWithItems(ctx context.Context, report asset.GapReport, items []asset.GapItem) error
	GetGapReport(ctx context.Context, id string) (asset.GapReport, error)
	ListGapReports(ctx context.Context, episodeID string) ([]asset.GapReport, error)
	MaxGapReportVersionNumber(ctx context.Context, episodeID string) (int, error)
	// CurrentApprovedGapReport returns the report in force, or found=false.
	CurrentApprovedGapReport(ctx context.Context, episodeID string) (asset.GapReport, bool, error)
	ListGapItems(ctx context.Context, reportID string) ([]asset.GapItem, error)
	ApproveGapReport(ctx context.Context, reportID, episodeID string, expectedStatus asset.VersionStatus, traceID string, at time.Time) error
}

// GapOptions configures the gap service.
//
// A separate constructor from `NewService` because the two need different ports: an
// asset service is composed in builds with no episode or script store, and a gap
// service is not. One option set for both would make every asset-only build carry a
// reader it never uses.
type GapOptions struct {
	Gaps  GapRepository
	Clock Clock
	IDs   IDGenerator
}

// GapService holds the gap report's commands and queries.
type GapService struct {
	gaps  GapRepository
	clock Clock
	ids   IDGenerator
}

// NewGapService builds the gap service.
func NewGapService(options GapOptions) *GapService {
	return &GapService{gaps: options.Gaps, clock: options.Clock, ids: options.IDs}
}

// Available reports whether the service can operate.
func (s *GapService) Available() bool {
	return s != nil && s.gaps != nil && s.ids != nil && s.clock != nil
}

func (s *GapService) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Time{}
	}
	return s.clock.Now().UTC()
}

// CreateGapReportRequest is one analysis being recorded.
type CreateGapReportRequest struct {
	EpisodeID       string
	ScriptVersionID string
	Summary         string
	// BasedOnVersionID links a re-analysis to the report it replaced.
	BasedOnVersionID string
	// SourceAgentRunID is the run that produced the analysis, so a reviewer can read
	// the reasoning behind a claim that something is missing.
	SourceAgentRunID string
	// Items are the analysis's lines, in the order the analyzer stated them. Their
	// ordinals are ASSIGNED here from the slice's order rather than accepted from the
	// caller: §17's "ID、顺序和唯一性" is the code's job, and a caller that supplied its
	// own ordinals could leave a hole or repeat one.
	Items []GapItemInput
	// CreatedByType and CreatedByID say who produced the analysis. An agent's write
	// passes `agent`; a user's own edit passes `user`.
	CreatedByType asset.CreatedByType
	CreatedByID   string
	ChangeReason  string
}

// GapItemInput is one line as a caller states it, before the service assigns ids and
// ordinals.
type GapItemInput struct {
	AssetType       asset.Type
	StoryEntityID   string
	StoryEntityName string
	AssetID         string
	Status          asset.GapStatus
	UsageRole       string
	Required        bool
	Notes           string
}

// CreateGapReport records an analysis as a new version.
//
// The version number comes from the stored MAXIMUM rather than a count, for the reason
// `AddVersion` states: a superseded or rejected report must not let a new one reuse its
// number, and the schema's unique constraint would refuse it.
func (s *GapService) CreateGapReport(ctx context.Context, request CreateGapReportRequest) (asset.GapReport, []asset.GapItem, error) {
	if !s.Available() {
		return asset.GapReport{}, nil, storageFailure()
	}
	episodeID := strings.TrimSpace(request.EpisodeID)
	scriptVersionID := strings.TrimSpace(request.ScriptVersionID)
	if episodeID == "" {
		return asset.GapReport{}, nil, asset.InvalidError("A gap report must name the episode it analyses.")
	}
	if scriptVersionID == "" {
		return asset.GapReport{}, nil, asset.InvalidError("A gap report must name the script version it analysed.")
	}
	createdBy := request.CreatedByType
	if createdBy == "" {
		createdBy = asset.CreatedByAgent
	}
	reportID, err := s.ids.New()
	if err != nil {
		return asset.GapReport{}, nil, storageFailure()
	}
	highest, err := s.gaps.MaxGapReportVersionNumber(ctx, episodeID)
	if err != nil {
		return asset.GapReport{}, nil, err
	}
	now := s.now()
	report := asset.GapReport{
		ID:               reportID,
		EpisodeID:        episodeID,
		ScriptVersionID:  scriptVersionID,
		VersionNumber:    highest + 1,
		Status:           asset.VersionDraft,
		BasedOnVersionID: strings.TrimSpace(request.BasedOnVersionID),
		SourceAgentRunID: strings.TrimSpace(request.SourceAgentRunID),
		Summary:          request.Summary,
		CreatedByType:    createdBy,
		CreatedByID:      strings.TrimSpace(request.CreatedByID),
		ChangeReason:     request.ChangeReason,
		CreatedAt:        now,
		UpdatedAt:        now,
		Revision:         1,
	}
	if err := report.Validate(); err != nil {
		return asset.GapReport{}, nil, err
	}
	items := make([]asset.GapItem, 0, len(request.Items))
	for index, input := range request.Items {
		itemID, err := s.ids.New()
		if err != nil {
			return asset.GapReport{}, nil, storageFailure()
		}
		role := strings.TrimSpace(input.UsageRole)
		if role == "" {
			role = "reference"
		}
		item := asset.GapItem{
			ID:              itemID,
			ReportID:        report.ID,
			Ordinal:         index + 1,
			AssetType:       input.AssetType,
			StoryEntityID:   strings.TrimSpace(input.StoryEntityID),
			StoryEntityName: strings.TrimSpace(input.StoryEntityName),
			AssetID:         strings.TrimSpace(input.AssetID),
			Status:          input.Status,
			UsageRole:       role,
			Required:        input.Required,
			Notes:           input.Notes,
			CreatedAt:       now,
		}
		if err := item.Validate(); err != nil {
			return asset.GapReport{}, nil, err
		}
		items = append(items, item)
	}
	// An analysis with NO lines is refused here rather than at approval, so the model
	// learns it before a user is shown a report that claims to have analysed nothing.
	// The refusal is stated once, in the domain, because the approval path makes it too.
	if len(items) == 0 {
		return asset.GapReport{}, nil, asset.InvalidError("A gap report must analyse at least one story fact.")
	}
	if err := s.gaps.CreateGapReportWithItems(ctx, report, items); err != nil {
		return asset.GapReport{}, nil, err
	}
	return report, items, nil
}

// ListGapReports returns an episode's reports newest first.
func (s *GapService) ListGapReports(ctx context.Context, episodeID string) ([]asset.GapReport, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	return s.gaps.ListGapReports(ctx, strings.TrimSpace(episodeID))
}

// GetGapReport returns one report with its lines.
func (s *GapService) GetGapReport(ctx context.Context, reportID string) (asset.GapReport, []asset.GapItem, error) {
	if !s.Available() {
		return asset.GapReport{}, nil, storageFailure()
	}
	report, err := s.gaps.GetGapReport(ctx, strings.TrimSpace(reportID))
	if err != nil {
		return asset.GapReport{}, nil, err
	}
	items, err := s.gaps.ListGapItems(ctx, report.ID)
	if err != nil {
		return asset.GapReport{}, nil, err
	}
	return report, items, nil
}

// CurrentApprovedGapReport returns the report in force for an episode.
//
// The boolean is false when none is approved, which is the ordinary state of an
// episode whose analysis is still under review.
func (s *GapService) CurrentApprovedGapReport(ctx context.Context, episodeID string) (asset.GapReport, bool, error) {
	if !s.Available() {
		return asset.GapReport{}, false, storageFailure()
	}
	return s.gaps.CurrentApprovedGapReport(ctx, strings.TrimSpace(episodeID))
}

// UnresolvedRequiredItems answers AC-BOARD-001's question for an episode.
//
// IT REFUSES WHEN NO REPORT IS APPROVED, which is the direction the batch gate needs:
// "there is no analysis" is not "nothing is missing", and a gate that treated the two
// as the same would let a batch run against a script nobody had analysed. The refusal
// says which of the two situations it is so a caller can act.
func (s *GapService) UnresolvedRequiredItems(ctx context.Context, episodeID string) ([]asset.GapItem, error) {
	if !s.Available() {
		return nil, storageFailure()
	}
	episode := strings.TrimSpace(episodeID)
	if episode == "" {
		return nil, asset.InvalidError("An episode is required to check for missing assets.")
	}
	report, found, err := s.gaps.CurrentApprovedGapReport(ctx, episode)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, asset.ConflictError("This episode has no approved gap report, so nothing says which assets its script needs.")
	}
	items, err := s.gaps.ListGapItems(ctx, report.ID)
	if err != nil {
		return nil, err
	}
	return asset.UnresolvedRequiredItems(items), nil
}

// ApproveGapReportRequest approves one report.
type ApproveGapReportRequest struct {
	ReportID string
	// TraceID links the report to the user gate decision that approved it. §10.1 gives
	// the asset_analysis stage a required user gate, and that decision is recorded on
	// the workflow's stream as `UserGateDecided` — this is the identifier that connects
	// the two, which is why it is stored rather than discarded.
	TraceID string
}

// ApproveGapReport puts a report in force.
//
// The refusals are the domain's and each names a different next step: a report already
// approved is not approved twice, a superseded one cannot come back, and one with a
// required asset still missing cannot be approved — because approving it would record a
// decision the batch gate refuses anyway, and §15.3's waiver is the route for a user who
// wants to proceed regardless.
func (s *GapService) ApproveGapReport(ctx context.Context, request ApproveGapReportRequest) (asset.GapReport, error) {
	if !s.Available() {
		return asset.GapReport{}, storageFailure()
	}
	report, items, err := s.GetGapReport(ctx, request.ReportID)
	if err != nil {
		return asset.GapReport{}, err
	}
	if err := asset.CanApproveGapReport(report.Status, items); err != nil {
		return asset.GapReport{}, err
	}
	traceID := strings.TrimSpace(request.TraceID)
	if err := s.gaps.ApproveGapReport(ctx, report.ID, report.EpisodeID, report.Status, traceID, s.now()); err != nil {
		return asset.GapReport{}, err
	}
	report.Status = asset.VersionApproved
	report.ApprovalTraceID = traceID
	return report, nil
}
