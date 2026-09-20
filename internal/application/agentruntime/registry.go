package agentruntime

import (
	"sort"
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
)

// Registry is the set of agents this build can run (AGENT_CONTRACTS section 3).
//
// It is built once at composition from the loaded skill packs and is read-only
// afterwards, so a run cannot add an agent to it. Section 3's startup validation
// list is Registry's constructor: every one of its six checks runs there, and a
// registry that fails any of them is not built at all.
//
// The six, from section 3:
//
//	Key 唯一            — two agents under one key would make the record ambiguous
//	Skill 存在          — the loader refuses a manifest naming a document it cannot read
//	Schema 存在         — the loader refuses a schema path this build does not embed
//	Tool 存在           — the loader refuses a tool the table does not register
//	Supervisor 无未批准写工具 — the ACL's matrix, asserted here as well
//	最大调用和时长有界   — agent.Limits.Validate
//	模型策略可解析       — the policy layer is one of section 13's four
type Registry struct {
	byKey map[string]agent.Spec
	keys  []string
}

// NewRegistry validates a set of registrations and returns them.
//
// It refuses the whole set on any invalid entry rather than dropping the bad one.
// That is the fail-closed direction and the one section 3 asks for: a build whose
// supervisor registration is wrong must not start with a supervisor that is
// silently absent, because the stage that needed it would then run unsupervised.
func NewRegistry(specs []agent.Spec, tools *Tools) (*Registry, error) {
	if tools == nil {
		return nil, agent.UnavailableError()
	}
	registry := &Registry{byKey: make(map[string]agent.Spec, len(specs))}
	for _, spec := range specs {
		if err := spec.Validate(); err != nil {
			return nil, err
		}
		if _, exists := registry.byKey[spec.Key]; exists {
			return nil, agent.InvalidError("The agent " + spec.Key + " is registered twice.")
		}
		// The tool check, against the table that actually exists.
		for _, key := range spec.AllowedTools {
			tool, ok := tools.Lookup(key)
			if !ok {
				return nil, agent.InvalidError("The agent " + spec.Key + " names a tool that is not registered.")
			}
			// This is the check section 3 names outright ("Supervisor 无未批准写工具"),
			// and it runs at registration as well as per call. The two are not
			// redundant: this one refuses to START a build whose supervisor could
			// write, so the defect is found by whoever wrote the manifest rather than
			// by a user who hit a refusal at run time.
			if err := (Authorizer{}).Authorize(AuthorizeRequest{Spec: spec, Tool: tool.Spec}); err != nil {
				return nil, err
			}
		}
		registry.byKey[spec.Key] = spec
	}
	registry.keys = make([]string, 0, len(registry.byKey))
	for key := range registry.byKey {
		registry.keys = append(registry.keys, key)
	}
	sort.Strings(registry.keys)
	return registry, nil
}

// Lookup returns a registration by key.
func (r *Registry) Lookup(key string) (agent.Spec, bool) {
	if r == nil {
		return agent.Spec{}, false
	}
	spec, ok := r.byKey[key]
	return spec, ok
}

// Keys returns every registered key, sorted.
func (r *Registry) Keys() []string {
	if r == nil {
		return nil
	}
	return append([]string(nil), r.keys...)
}

// OfLayer returns the registrations at one layer, sorted by key.
//
// It is what the engine asks when it needs "an execution agent for this stage":
// the choice of which agent runs a stage belongs to the workflow's configuration
// rather than to a model, so the engine looks the agent up rather than being told
// by one.
func (r *Registry) OfLayer(layer agent.AgentLayer) []agent.Spec {
	if r == nil {
		return nil
	}
	out := make([]agent.Spec, 0, len(r.keys))
	for _, key := range r.keys {
		if r.byKey[key].Layer == layer {
			out = append(out, r.byKey[key])
		}
	}
	return out
}

// ForStage returns the execution agent registered for one stage name, if any.
//
// Section 19's inventory names its execution agents after their stages
// (`script.execution.story_skeleton`), so the match is on the key's last segment.
// The lookup is by exact segment rather than by substring so a stage named `story`
// cannot match the agent for `story_skeleton`.
func (r *Registry) ForStage(stage string) (agent.Spec, bool) {
	if r == nil {
		return agent.Spec{}, false
	}
	trimmed := strings.TrimSpace(stage)
	if trimmed == "" {
		return agent.Spec{}, false
	}
	for _, key := range r.keys {
		spec := r.byKey[key]
		if spec.Layer != agent.LayerExecution {
			continue
		}
		segments := strings.Split(spec.Key, ".")
		if len(segments) > 0 && segments[len(segments)-1] == trimmed {
			return spec, true
		}
	}
	return agent.Spec{}, false
}

// SupervisionFor returns the supervision agent for a stage, if any.
//
// It is the same lookup at the other layer, kept separate rather than parameterised
// because the two answer different questions: an execution agent is chosen by the
// stage it executes, and a supervisor by the artifact family it reviews.
func (r *Registry) SupervisionFor(stage string) (agent.Spec, bool) {
	if r == nil {
		return agent.Spec{}, false
	}
	trimmed := strings.TrimSpace(stage)
	if trimmed == "" {
		return agent.Spec{}, false
	}
	for _, key := range r.keys {
		spec := r.byKey[key]
		if spec.Layer != agent.LayerSupervision {
			continue
		}
		segments := strings.Split(spec.Key, ".")
		if len(segments) > 0 && segments[len(segments)-1] == trimmed {
			return spec, true
		}
	}
	return agent.Spec{}, false
}

// KeysForLayer is a convenience for a UI listing the registered agents, so the
// frontend does not have to filter what the binding returns.
func (r *Registry) KeysForLayer(layer agent.AgentLayer) []string {
	specs := r.OfLayer(layer)
	out := make([]string, 0, len(specs))
	for _, spec := range specs {
		out = append(out, spec.Key)
	}
	return out
}
