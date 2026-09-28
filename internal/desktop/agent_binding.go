package desktop

import (
	"context"
	"sync"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
)

// AgentBinding is the WP-07 Wails method surface: the Agent Center's run views and the agent
// inventory the Quality Gate screen shows.
//
// THE QUALITY GATE ITSELF IS NOT HERE, and that is a design decision rather than a gap. The
// stage attempts, the review reports and the findings were WP-05's rows and are already on the
// DramaBinding (ListStageRuns, GetReviewReport, SubmitGateDecision), which is where a screen
// showing a gate reads them. Duplicating those reads here would put two definitions of one
// projection in the build, and the second would be the one nobody updated. What this surface
// ADDS is the agent side of the same screen: which runs produced the artifact under review, and
// which agents this build can run.
//
// Two rules shape it, and both come from the repository's existing bindings:
//
//   - Every method is a READ about state the database already holds. A run happens because an
//     application service asked for one, never because a webview called a binding, so this
//     surface cannot start an agent — which is what keeps "who may run an agent" a server-side
//     question.
//   - A method attached to no service REFUSES rather than returning an empty result. A user
//     whose database is in safe mode must be told that, not shown an empty list that reads as
//     "this project has no runs".
type AgentBinding struct {
	mu        sync.RWMutex
	ctx       context.Context
	inspector *agentruntime.Inspector
	registry  *agentruntime.Registry
	// assembly is the skill source the management surface acts on (T09): its
	// SetAgentEnabled is the stop switch, its SkillDocument the readback. It
	// is optional and its absence fails closed, like every other dependency.
	assembly agentSkillManager
}

// agentSkillManager is the management surface the binding needs from the
// assembly, declared narrow here so the binding does not import the whole
// assembly type's surface. RP-06.2 adds the version-management surface: a
// version history read, a derive command, and a rollback that switches the
// active pointer — all optional, so a build without the management store
// reports "unavailable" rather than pretending.
type agentSkillManager interface {
	SetAgentEnabled(agentKey string, enabled bool) error
	AgentEnabled(agentKey string) bool
	SkillDocument(agentKey string) (string, bool)
}

// agentSkillVersioner is the RP-06.2 version-management surface. It is a
// SEPARATE interface from agentSkillManager so a build that only manages
// enable/disable keeps compiling: the type assertion at the call site decides.
type agentSkillVersioner interface {
	SkillVersionsOf(ctx context.Context, agentKey string) ([]agent.SkillVersion, error)
	CreateSkillVersion(ctx context.Context, agentKey, basedOnVersionID, newVersionLabel, document string) (agent.SkillVersion, error)
	ActivateSkillVersion(ctx context.Context, agentKey, versionID string) error
}

// ListAgentRuns returns a project's runs newest first.
func (b *AgentBinding) ListAgentRuns(projectID string) ([]agentruntime.RunSummary, error) {
	binding, ctx, err := b.ready()
	if err != nil {
		return nil, err
	}
	return binding.inspector.ListRuns(ctx, projectID, 0)
}

// GetAgentRunTrace returns one run with its messages and tool calls.
//
// This is section 16's "UI 不要求显示内部 Chain of Thought" surface, and what it carries is what
// that section lists instead: the reasonSummary the model produced, the tools it called, the
// versions it reported and the error it ended with.
func (b *AgentBinding) GetAgentRunTrace(projectID string, runID string) (agentruntime.RunTrace, error) {
	binding, ctx, err := b.ready()
	if err != nil {
		return agentruntime.RunTrace{}, err
	}
	return binding.inspector.GetTrace(ctx, projectID, runID)
}

// RunStatusCounts reports how many runs a project has in each status.
//
// It is computed from the run list rather than from a separate query, because a second query
// would be a second definition of what a status count means. The list is bounded at the
// inspector's ceiling, which is the window a summary row is about anyway.
func (b *AgentBinding) RunStatusCounts(projectID string) (map[string]int, error) {
	binding, ctx, err := b.ready()
	if err != nil {
		return nil, err
	}
	runs, err := binding.inspector.ListRuns(ctx, projectID, agentruntime.MaxRunLimit)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, run := range runs {
		counts[run.Status]++
	}
	return counts, nil
}

// AgentInventory reports which agents this build can run, by layer.
//
// It reads the ASSEMBLED registry rather than a manifest, which is what makes it the truth: an
// agent whose pack named a tool this build does not register is not in the registry at all,
// because the assembly refuses to load such a pack. So an agent a user sees here is one that can
// actually run.
func (b *AgentBinding) AgentInventory() (AgentInventoryDTO, error) {
	if b == nil {
		return AgentInventoryDTO{}, bindingUnavailable()
	}
	b.mu.RLock()
	registry := b.registry
	b.mu.RUnlock()
	if registry == nil {
		return AgentInventoryDTO{}, bindingUnavailable()
	}
	inventory := AgentInventoryDTO{Agents: []AgentSpecDTO{}}
	for _, layer := range []agent.AgentLayer{
		agent.LayerDecision, agent.LayerExecution, agent.LayerSupervision,
	} {
		for _, spec := range registry.OfLayer(layer) {
			// THE ENABLED FLAG travels per agent (T09), read from the same
			// skill manager the stop switch writes through, so the list and
			// the switch cannot disagree.
			enabled := true
			if b.assembly != nil {
				enabled = b.assembly.AgentEnabled(spec.Key)
			}
			inventory.Agents = append(inventory.Agents, AgentSpecDTO{
				Key: spec.Key, Layer: string(spec.Layer), Skill: spec.Skill,
				AllowedTools:       append([]string(nil), spec.AllowedTools...),
				MaxToolCalls:       spec.Limits.MaxToolCalls,
				MaxDurationSeconds: int(spec.Limits.MaxDuration.Seconds()),
				PolicyLayer:        string(spec.PolicyLayer),
				Enabled:            enabled,
			})
		}
	}
	return inventory, nil
}

// AgentInventoryDTO is the list of agents this build can run.
type AgentInventoryDTO struct {
	Agents []AgentSpecDTO `json:"agents"`
}

// AgentSpecDTO is one registered agent.
//
// The tool KEYS travel because a reviewer checking a stage's behaviour needs to see what the
// agent was ALLOWED to call — that is the question a denial raises. The tool SCHEMAS do not: they
// are large and static, and a screen that showed them would be a second copy of the registry to
// keep in step.
type AgentSpecDTO struct {
	Key          string   `json:"key"`
	Layer        string   `json:"layer"`
	Skill        string   `json:"skill"`
	AllowedTools []string `json:"allowedTools"`
	MaxToolCalls int      `json:"maxToolCalls"`
	// MaxDurationSeconds is seconds rather than a duration, because a JSON duration is either a
	// number of nanoseconds nobody reads or a string each consumer parses differently.
	MaxDurationSeconds int    `json:"maxDurationSeconds"`
	PolicyLayer        string `json:"policyLayer"`
	// Enabled is the management surface's stop state (T09): a disabled agent
	// refuses at the skill read, and the inventory shows it.
	Enabled bool `json:"enabled"`
}

// ready returns the binding and its context, refusing when the stack is absent.
//
// It is the fail-closed rule the other bindings follow: an unattached binding reports unavailable
// rather than an empty result, because an empty list is a CLAIM about the data and "no database
// is open" is not.
func (b *AgentBinding) ready() (*AgentBinding, context.Context, error) {
	if b == nil {
		return nil, nil, bindingUnavailable()
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.ctx == nil || b.inspector == nil || !b.inspector.Available() {
		return nil, nil, bindingUnavailable()
	}
	return b, b.ctx, nil
}

// AttachAgent wires the run views.
func AttachAgent(binding *AgentBinding, ctx context.Context, inspector *agentruntime.Inspector) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.ctx = ctx
	binding.inspector = inspector
	binding.mu.Unlock()
}

// AttachAgentSkills supplies the management surface's skill manager (T09).
func AttachAgentSkills(binding *AgentBinding, assembly agentSkillManager) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.assembly = assembly
	binding.mu.Unlock()
}

// SetAgentEnabled starts or stops one agent (T09, FR-090).
//
// The refusal for an unknown key comes from the assembly, which validates
// against the registered set; the binding's only rule is that the surface
// must exist. The state is this build's in-process stop switch — a stopped
// agent resumes on the next app start, which the Agent Center says.
func (b *AgentBinding) SetAgentEnabled(agentKey string, enabled bool) error {
	if b == nil {
		return bindingUnavailable()
	}
	b.mu.RLock()
	assembly := b.assembly
	b.mu.RUnlock()
	if assembly == nil {
		return bindingUnavailable()
	}
	return assembly.SetAgentEnabled(agentKey, enabled)
}

// AgentEnabled reports whether one agent is enabled.
func (b *AgentBinding) AgentEnabled(agentKey string) (bool, error) {
	if b == nil {
		return false, bindingUnavailable()
	}
	b.mu.RLock()
	assembly := b.assembly
	b.mu.RUnlock()
	if assembly == nil {
		return false, bindingUnavailable()
	}
	return assembly.AgentEnabled(agentKey), nil
}

// AgentSkillDocument returns one agent's skill text for viewing (T09). It is
// prompt MATERIAL, shown read-only: a UI that edited it here would be editing
// an embedded pack's copy, and the version a run cites is the pack's hash —
// a new version is a new pack, not an edit.
func (b *AgentBinding) AgentSkillDocument(agentKey string) (string, error) {
	if b == nil {
		return "", bindingUnavailable()
	}
	b.mu.RLock()
	assembly := b.assembly
	b.mu.RUnlock()
	if assembly == nil {
		return "", bindingUnavailable()
	}
	document, ok := assembly.SkillDocument(agentKey)
	if !ok {
		return "", agent.InvalidError("That agent key is not registered in this build.")
	}
	return document, nil
}

// AttachAgentRegistry supplies the assembled registry the inventory reads.
//
// It is separate from AttachAgent because the two have different requirements: the inventory needs
// only the registry, while a trace needs a database. A safe-mode build therefore lists its agents
// and shows no runs, which is the honest state — the agents exist in the binary whether or not a
// database is open.
func AttachAgentRegistry(binding *AgentBinding, registry *agentruntime.Registry) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.registry = registry
	binding.mu.Unlock()
}

// --- RP-06.2: skill version management on the binding ---

// ListSkillVersions returns one agent's skill version history, newest first,
// with each version's hash and status. A build without the versioning store
// is refused honestly: the UI shows "unavailable" rather than an empty
// history that would read as "no versions".
func (b *AgentBinding) ListSkillVersions(agentKey string) ([]agent.SkillVersion, error) {
	b.mu.RLock()
	assembly := b.assembly
	b.mu.RUnlock()
	if assembly == nil {
		return nil, bindingUnavailable()
	}
	versioner, ok := assembly.(agentSkillVersioner)
	if !ok || versioner == nil {
		return nil, agent.InvalidError("Skill version management is not available in this build.")
	}
	b.mu.RLock()
	ctx := b.ctx
	b.mu.RUnlock()
	return versioner.SkillVersionsOf(ctx, agentKey)
}

// CreateSkillVersionRequest derives one skill version from an existing one.
type CreateSkillVersionRequest struct {
	AgentKey string `json:"agentKey"`
	// BasedOnVersionID is the version the edit starts from. The UI reads its
	// document through AgentSkillDocument/ListSkillVersions and sends the
	// edited text here.
	BasedOnVersionID string `json:"basedOnVersionId"`
	// NewVersionLabel is the derived version's label (semver-like, ≤60 chars,
	// the skill_versions table's own check).
	NewVersionLabel string `json:"newVersionLabel"`
	// Document is the FULL edited document. It must carry every section 4.3
	// heading the loader requires — a derived version meets the same bar a
	// builtin pack does.
	Document string `json:"document"`
}

// CreateSkillVersion stores a derived skill version under its own content
// hash. It does NOT activate it: activation is the separate, explicit
// rollback/switch command, so a user can review the derived version before
// any run uses it.
func (b *AgentBinding) CreateSkillVersion(request CreateSkillVersionRequest) (agent.SkillVersion, error) {
	b.mu.RLock()
	assembly := b.assembly
	b.mu.RUnlock()
	if assembly == nil {
		return agent.SkillVersion{}, bindingUnavailable()
	}
	versioner, ok := assembly.(agentSkillVersioner)
	if !ok || versioner == nil {
		return agent.SkillVersion{}, agent.InvalidError("Skill version management is not available in this build.")
	}
	b.mu.RLock()
	ctx := b.ctx
	b.mu.RUnlock()
	return versioner.CreateSkillVersion(ctx, request.AgentKey, request.BasedOnVersionID, request.NewVersionLabel, request.Document)
}

// ActivateSkillVersion switches an agent's active skill version — rollback is
// a pointer switch to a historical version, never a rewrite of its content.
// Runs already in flight keep the snapshot they took; the switch affects only
// LATER snapshots.
func (b *AgentBinding) ActivateSkillVersion(agentKey, versionID string) error {
	b.mu.RLock()
	assembly := b.assembly
	b.mu.RUnlock()
	if assembly == nil {
		return bindingUnavailable()
	}
	versioner, ok := assembly.(agentSkillVersioner)
	if !ok || versioner == nil {
		return agent.InvalidError("Skill version management is not available in this build.")
	}
	b.mu.RLock()
	ctx := b.ctx
	b.mu.RUnlock()
	return versioner.ActivateSkillVersion(ctx, agentKey, versionID)
}
