package assets

import (
	"context"
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/staleness"
)

// propagate.go is the impact half of an approval switch.
//
// DOMAIN_MODEL §15.1 lists "AssetVersion 默认批准版本切换" as a staleness TRIGGER, and
// §15.2 makes the propagation reach the storyboard panel and everything below it. WP-05
// built both halves — the graph with its four storyboard artifact types, and the
// propagation service — and nothing joined them: `ApproveVersion` switched the version
// and told nobody, so PRD FR-050's "替换批准版本时，系统列出受影响的分镜和镜头" was a
// rule with no mechanism.
//
// # Why a port rather than a call to the staleness service
//
// The assets package is BELOW the staleness service in the dependency order: the
// staleness service resolves projects through the asset tables, so importing it here
// would be a cycle. The port is declared at the use site, which AGENTS §7.4 asks for,
// and the composition root supplies an adapter over the service it already holds.

// ImpactPropagator announces that an artifact changed and records what it reaches.
//
// It is optional, and its absence is a WARNING rather than a refusal — the opposite of
// the projector's rule, and deliberately. A projection that did not happen is a board
// that does not show the user's work, which the user will notice and cannot reconstruct.
// A propagation that did not happen is a mark that was not written, which the next
// approval or the panel's own read still surfaces. Refusing an approval because the mark
// could not be written would make a build without staleness unable to approve anything,
// which is a worse failure than a missing notice.
type ImpactPropagator interface {
	// PropagateFrom marks everything the change reaches and reports what it marked.
	PropagateFrom(ctx context.Context, request ImpactRequest) error
}

// ImpactRequest is one change to propagate.
//
// It is deliberately not `appstaleness.PropagateRequest`: the assets package cannot
// import that package for the reason above, and a struct with the same four fields costs
// nothing while keeping the direction of the dependency honest.
type ImpactRequest struct {
	ChangedType string
	ChangedID   string
	ProjectID   string
	Reason      string
}

// AnnounceAssetVersionChange tells the impact graph that a version STOPPED being the one
// in force.
//
// # The version it propagates from is the one being REPLACED
//
// This is the part that is easy to get backwards, and the first version of this function
// did: it propagated from the NEWLY approved version. The graph's `PropagateFrom` means
// "this artifact changed; mark what depends on it", and on an approval switch the
// artifact whose meaning changed is the OLD one — a panel that approved v1 is now holding
// a version that is no longer in force. A panel holding v2 is holding the version that IS
// in force, so nothing about it is disturbed.
//
// It is also what PRD FR-050 asks for: "替换批准版本时，系统列出受影响的分镜和镜头" — the
// affected ones are those using the version being replaced, which is the same list
// `ApprovalImpactOf` builds for the user BEFORE they approve.
//
// # The rest
//
// A caller that replaced NOTHING (the first approval of an asset) has no version to
// announce, and `replacedVersionID` is then empty: there is no disturbance, so this is a
// no-op rather than a call that walks an empty graph.
//
// The project is resolved from the ASSET rather than passed in, because a caller that
// supplied the wrong project would have its marks filed under a project that does not
// contain the artifact — and the marks would then be invisible to the reader who needs
// them.
func (s *Service) AnnounceAssetVersionChange(ctx context.Context, replacedVersionID, reason string) error {
	if s == nil || s.propagator == nil {
		// A build with no impact graph: the approval stands and the notice is skipped,
		// which the port's comment explains.
		return nil
	}
	replaced := strings.TrimSpace(replacedVersionID)
	if replaced == "" {
		return nil
	}
	version, err := s.repository.GetVersion(ctx, replaced)
	if err != nil {
		// The version was in force until a moment ago, so a read failure here is a
		// storage fault rather than a stale identifier. Reported rather than swallowed so
		// a caller can tell the two apart; the approval it follows is already committed.
		return err
	}
	record, err := s.repository.GetAsset(ctx, version.AssetID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(record.ProjectID) == "" {
		return asset.InvalidError("An asset with no project cannot propagate an impact.")
	}
	return s.propagator.PropagateFrom(ctx, ImpactRequest{
		ChangedType: string(staleness.ArtifactAssetVersion),
		ChangedID:   version.ID,
		ProjectID:   record.ProjectID,
		Reason:      reason,
	})
}
