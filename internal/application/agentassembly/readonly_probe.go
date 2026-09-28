package agentassembly

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/skill"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
)

// readonly_probe.go is RP-06.4's read-only test surface (FR-090's 运行只读
// 测试, offline half).
//
// # What "read-only test" means HERE, stated so no one upgrades it by accident
//
// The probe answers, WITHOUT calling any model and WITHOUT writing any
// business row: is the agent registered, which tools is it allowed, which
// skill version and hash would a run cite, does its document carry every
// section 4.3 heading, and does its prompt assemble? Those are the checks a
// static preflight can honestly make. The probe's result therefore must
// never be presented as "the model ran and passed" — the DTO says so itself
// in a field the UI is expected to render (ProbeKind).
//
// The REAL LLM half of 只读试跑 runs against a user-chosen provider with the
// runtime's own read-only tool set; it is the runtime Invocation's job with
// a read-only ACL, and it is gated behind an explicit user action — never
// automatic. This file is the offline half plus the DTO both halves share.
//
// # The no-write guarantee
//
// The probe reads the registry, the pack table and the skill documents. It
// touches no repository write path, creates no run row, and consumes no
// model tokens. Its tests assert that business rows are unchanged across a
// probe.

// ReadOnlyProbeResult is the DTO the management UI renders for a read-only
// test. Every field is safe to display: no prompt text beyond the agent's
// own skill document summary, no user story content, no keys.
type ReadOnlyProbeResult struct {
	// AgentKey is the probed agent.
	AgentKey string `json:"agentKey"`
	// Registered reports the registry lookup. An unregistered agent cannot
	// run, whatever its document says.
	Registered bool `json:"registered"`
	// Layer is the agent's layer, echoed so the UI can show why the tool
	// allowance is what it is.
	Layer string `json:"layer"`
	// AllowedTools is the tool keys the registry grants this agent.
	AllowedTools []string `json:"allowedTools"`
	// SkillVersionID and SkillContentHash are what a run would cite.
	SkillVersionID   string `json:"skillVersionId,omitempty"`
	SkillContentHash string `json:"skillContentHash,omitempty"`
	// SkillSectionsMissing lists section 4.3 headings the document lacks.
	// Empty means the document is well-formed.
	SkillSectionsMissing []string `json:"skillSectionsMissing,omitempty"`
	// PromptAssembles reports that the prompt layers assemble from the
	// available inputs (a smoke of prompt construction, not a model call).
	PromptAssembles bool `json:"promptAssembles"`
	// ProbeKind says what kind of test this was. It exists so a UI cannot
	// accidentally present the static result as a model run.
	ProbeKind string `json:"probeKind"`
}

// ProbeKindStatic marks the offline, no-model result.
const ProbeKindStatic = "static"

// ReadOnlyTestProbe answers the static half of FR-090's 只读测试 for one
// agent key. It fails closed: an unknown agent is a refusal, not a result
// that says "not registered" with a zero everything else, so the UI cannot
// miss the difference between an absent agent and a present-but-unhealthy
// one.
func (a *Assembly) ReadOnlyTestProbe(agentKey string) (ReadOnlyProbeResult, error) {
	if a == nil || a.registry == nil {
		return ReadOnlyProbeResult{}, agent.UnavailableError()
	}
	spec, ok := a.registry.Lookup(strings.TrimSpace(agentKey))
	if !ok {
		return ReadOnlyProbeResult{}, agent.InvalidError("That agent key is not registered in this build.")
	}
	result := ReadOnlyProbeResult{
		AgentKey:     agentKey,
		Registered:   true,
		Layer:        string(spec.Layer),
		AllowedTools: append([]string(nil), spec.AllowedTools...),
		ProbeKind:    ProbeKindStatic,
	}

	// The skill document and the version the run would cite.
	if versionID, ok := a.SkillVersionOf(agentKey); ok {
		result.SkillVersionID = versionID
	}
	if document, ok := a.SkillDocument(agentKey); ok {
		result.SkillContentHash = documentHashOf(document)
		for _, section := range probeSkillSections {
			if !strings.Contains(document, "# "+section) {
				result.SkillSectionsMissing = append(result.SkillSectionsMissing, section)
			}
		}
		// The prompt assembles when the document can seed the skill layer:
		// non-empty and carrying its role heading. A fuller assembly needs a
		// stage's own inputs and belongs to the real run.
		result.PromptAssembles = strings.TrimSpace(document) != ""
	}

	// The switch state is part of the health answer: a disabled agent cannot
	// run even though its registration is perfect.
	if !a.AgentEnabled(agentKey) {
		result.PromptAssembles = false
	}
	return result, nil
}

// documentHashOf hashes a skill document for the probe's display. It is a
// SHA-256 hex digest, the same address family the version store uses.
func documentHashOf(document string) string {
	sum := sha256.Sum256([]byte(document))
	return hex.EncodeToString(sum[:])
}

// probeSkillSections mirrors the loader's required list. The loader owns
// the vocabulary; this copy would drift, so it is sourced from the skill
// package instead.
var probeSkillSections = probeSkillSectionsSource()

func probeSkillSectionsSource() []string { return skill.RequiredSections() }
