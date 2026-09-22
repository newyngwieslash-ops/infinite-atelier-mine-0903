import io

p = "agent_wiring.go"
s = io.open(p, encoding="utf-8").read()

old = '''func (g *textGenerator) choice(ctx context.Context, named string) (string, error) {
	if trimmed := strings.TrimSpace(named); trimmed != "" {'''
new = '''// choice resolves which provider serves one call.
//
// THREE CASES, in this order, and the order is the policy rather than a preference:
//
//  1. The request NAMED a provider, so that one is used and it must be enabled.
//  2. The request states a LAYER, and the project's policy for that layer names a provider. Section 13
//     exists because the layers have different needs — a supervisor on a different provider is what makes
//     its review independent, and "低成本阶段不默认使用最昂贵模型" is a per-stage rule — so this is the
//     case that makes the section reachable.
//  3. Neither, so the first enabled provider is used.
//
// THE THIRD CASE IS A FALLBACK AND NOT THE DESIGN, which is worth stating because it was the whole
// implementation until now: every call went to whichever provider was enabled first, so the per-layer
// policy the SCHEMA already carried could not affect anything. WP-08 closes that, and FR-140's per-STAGE
// keys (`script_execution_model` and its siblings) remain deferred — a stage is not a layer, and stating
// the tables now would be inventing a shape the specification does not give.
func (g *textGenerator) choice(ctx context.Context, named string, layer agent.AgentLayer, projectID string) (string, error) {
	if trimmed := strings.TrimSpace(named); trimmed != "" {'''
assert old in s, "choice head"
s = s.replace(old, new, 1)

old = '''		if enabled != 1 {
			return "", agent.InvalidError("The provider this run named is disabled.")
		}
		return trimmed, nil
	}
	var id string
	err := g.db.QueryRowContext(ctx,
		`SELECT id FROM provider_configs WHERE enabled = 1 ORDER BY id ASC LIMIT 1`).Scan(&id)'''
new = '''		if enabled != 1 {
			return "", agent.InvalidError("The provider this run named is disabled.")
		}
		return trimmed, nil
	}
	// The layer's policy, which a project may state. A policy whose provider is not enabled is a
	// CONFIGURATION a user has to fix, so it is reported rather than skipped: falling through to another
	// provider would send this project's prompts somewhere its owner did not choose, which is the one
	// thing section 13's rules are about.
	if resolved, ok := g.policyProvider(ctx, layer, projectID); ok {
		return resolved, nil
	}
	var id string
	err := g.db.QueryRowContext(ctx,
		`SELECT id FROM provider_configs WHERE enabled = 1 ORDER BY id ASC LIMIT 1`).Scan(&id)'''
assert old in s, "fallback"
s = s.replace(old, new, 1)

old = '''// validateWith adapts the validation package's function to the runtime's Validator type.'''
new = '''// policyProvider reads the provider a project's policy names for one layer.
//
// The lookup walks the layer FIRST and then `default`, which is what the `default` row is for: a project
// states its ordinary provider once and overrides the layers that differ. The boolean is false when
// neither row names one, which is the ordinary state of a project whose policy is a policy about MODELS
// rather than about providers — the policy's own `primaryModelId` is a model, and a project that names
// only a model has said nothing about which provider should serve it.
//
// A policy naming a provider that is not enabled is REFUSED rather than skipped, and the refusal names
// both: a project whose supervisor was pinned to a provider that has since been disabled must not quietly
// run its supervisor on another one, because section 13's whole point is that the reviewer differ.
func (g *textGenerator) policyProvider(ctx context.Context, layer agent.AgentLayer, projectID string) (string, bool, error) {
	project := strings.TrimSpace(projectID)
	name := strings.TrimSpace(string(layer))
	if project == "" || name == "" {
		return "", nil
	}
	rows, err := g.db.QueryContext(ctx, `SELECT layer, policy_json FROM project_provider_policies
		WHERE project_id = ? AND layer IN (?, 'default')`, project, name)
	if err != nil {
		return "", agent.StorageError("The project's model policy could not be read.", err)
	}
	defer rows.Close()
	policies := map[string]string{}
	for rows.Next() {
		var layerName, policyJSON string
		if err := rows.Scan(&layerName, &policyJSON); err != nil {
			return "", agent.StorageError("The project's model policy could not be read.", err)
		}
		policies[layerName] = policyJSON
	}
	if err := rows.Err(); err != nil {
		return "", agent.StorageError("The project's model policy could not be read.", err)
	}
	// The layer's own row wins over `default`, and the map makes that a lookup rather than an ordering
	// dependency on the query.
	for _, candidate := range []string{name, "default"} {
		policyJSON, ok := policies[candidate]
		if !ok {
			continue
		}
		providerID := providerFromPolicy(policyJSON)
		if providerID == "" {
			continue
		}
		var enabled int
		if err := g.db.QueryRowContext(ctx,
			`SELECT enabled FROM provider_configs WHERE id = ?`, providerID).Scan(&enabled); err != nil {
			if err == sql.ErrNoRows {
				return "", agent.InvalidError(
					"The provider this project's policy names is not configured.")
			}
			return "", agent.StorageError("The provider configuration could not be read.", err)
		}
		if enabled != 1 {
			// Refused rather than skipped, so a disabled provider is a visible configuration problem
			// instead of a silent fallback to one the project did not choose.
			return "", agent.InvalidError("The provider this project's policy names is disabled.")
		}
		return providerID, nil
	}
	return "", nil
}

// providerFromPolicy reads `providerId` out of a policy document.
//
// It returns "" for anything it cannot read, and that is the SAFE direction here rather than the lax one:
// an unreadable policy means "this row states no provider", which sends the caller to the next row in the
// chain and ends at the enabled-provider fallback. Refusing instead would make a policy whose JSON this
// build does not understand stop every run, which is worse than not honouring a field it cannot read —
// and the project service's own validation keeps a stored policy a JSON object, which is the only thing
// this function depends on.
//
// The field is `providerId` because that is what the project service's policy editor writes. Section 13's
// own example shows `primaryModelId` and no provider at all, so a project may legitimately omit this — and
// a document written to the section's example exactly is then handled by the fallback rather than refused.
func providerFromPolicy(policyJSON string) string {
	trimmed := strings.TrimSpace(policyJSON)
	if trimmed == "" {
		return ""
	}
	var document struct {
		ProviderID string `json:"providerId"`
	}
	if err := json.Unmarshal([]byte(trimmed), &document); err != nil {
		return ""
	}
	return strings.TrimSpace(document.ProviderID)
}

// validateWith adapts the validation package's function to the runtime's Validator type.'''
assert old in s, "policyProvider anchor"
s = s.replace(old, new, 1)

# The call site passes the layer and the project.
old = '''	id, err := g.choice(ctx, request.ProviderID)'''
new = '''	id, err := g.choice(ctx, request.ProviderID, request.Layer, request.ProjectID)'''
if old in s:
    s = s.replace(old, new, 1)
io.open(p, "w", encoding="utf-8", newline="\n").write(s)
print("wiring patched")
