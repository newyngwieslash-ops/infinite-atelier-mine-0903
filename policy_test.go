package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	appsecrets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/secrets"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/database"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providers"
)

// policy_test.go covers section 13's per-layer model policy, which is what `choice` resolves.
//
// The property that matters is the one the build did NOT have: until WP-08, `choice` returned the first
// enabled provider for every call, so the layer a run belonged to could not affect which provider served
// it. The schema already carried the policy table and the section already required the behaviour, so this
// is the test that keeps the gap closed — a mutation that returned to "the first enabled provider" fails
// here.

// policyFixture is a database with two providers and one project.
type policyFixture struct {
	db *sql.DB
}

// newPolicyFixture opens a migrated database with the rows these tests need.
//
// It goes through the real migration runner rather than creating the two tables by hand, because the
// policy's `layer` CHECK is what makes the vocabulary closed — a hand-written table would accept a layer
// the schema refuses, and these tests would then be asserting about a schema that does not exist.
func newPolicyFixture(t *testing.T) *policyFixture {
	t.Helper()
	ctx := context.Background()
	// The REAL Open path: it creates the file, runs every embedded migration, and fails closed. A fixture
	// that created the two tables by hand would not have the schema's CHECK constraints, and the policy
	// layer vocabulary is one of them.
	dir := t.TempDir()
	handle, err := database.Open(ctx, filepath.Join(dir, "policy.db"), filepath.Join(dir, "snapshots"))
	if err != nil {
		t.Fatalf("opening the test database: %v", err)
	}
	if err := handle.Err(); err != nil {
		t.Fatalf("the test database is unusable: %v", err)
	}
	t.Cleanup(func() { _ = handle.Close(context.Background()) })
	statements := []string{
		`INSERT INTO workspaces (id, name, kind, created_at, updated_at, revision)
		 VALUES ('policy-ws', 'Local', 'local', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
		`INSERT INTO projects (id, workspace_id, project_type, name, language, status, created_at, updated_at, revision)
		 VALUES ('policy-project', 'policy-ws', 'drama', 'Policy', 'zh-CN', 'active', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
		// Two text providers, both enabled, so "the first enabled one" and "the layer's choice" are
		// DIFFERENT answers whenever a policy names the second. That is what makes the test discriminating:
		// with one provider the two behaviours would agree.
		//
		// Each provider needs a SECRET REFERENCE first — the column is NOT NULL with a foreign key, and
		// WP-02's schema is what makes "a provider with no credential" unrepresentable rather than a run
		// that fails later. The reference states `missing`, which is the honest status of a fixture that
		// has no key: `choice` reads the CONFIG, not the secret, so nothing here resolves a credential.
		`INSERT INTO secret_references (id, provider_id, secret_kind, display_hint, status, created_at, updated_at)
		 VALUES ('secret-first', 'provider-first', 'api_key', '', 'missing', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		`INSERT INTO secret_references (id, provider_id, secret_kind, display_hint, status, created_at, updated_at)
		 VALUES ('secret-supervisor', 'provider-supervisor', 'api_key', '', 'missing', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		`INSERT INTO provider_configs (id, kind, display_name, base_url, secret_ref, enabled, created_at, updated_at, revision)
		 VALUES ('provider-first', 'openai_compatible', 'First', 'https://first.invalid', 'secret-first', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
		`INSERT INTO provider_configs (id, kind, display_name, base_url, secret_ref, enabled, created_at, updated_at, revision)
		 VALUES ('provider-supervisor', 'openai_compatible', 'Supervisor', 'https://supervisor.invalid', 'secret-supervisor', 1, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
	}
	for _, statement := range statements {
		if _, err := handle.SQL().ExecContext(ctx, statement); err != nil {
			t.Fatalf("seeding the policy fixture: %v\n%s", err, statement)
		}
	}
	return &policyFixture{db: handle.SQL()}
}

// setPolicy writes one layer's policy for the project.
func (f *policyFixture) setPolicy(t *testing.T, layer, policyJSON string) {
	t.Helper()
	if _, err := f.db.ExecContext(context.Background(), `INSERT INTO project_provider_policies
		(id, project_id, layer, policy_json, created_at, updated_at, revision)
		VALUES (?, 'policy-project', ?, ?, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`,
		"policy-"+layer, layer, policyJSON); err != nil {
		t.Fatalf("writing a %s policy: %v", layer, err)
	}
}

// generator builds the port the tests drive.
//
// The registry is the REAL one over the fixture's database, wired the way `provider_wiring.go` wires it —
// the same repository, the same secret store, the same audit sink. `choice` only decides an ID, but the
// port refuses without a registry, so a nil one would make these tests assert about a refusal that happened
// before the policy was ever read.
func (f *policyFixture) generator(t *testing.T) *textGenerator {
	t.Helper()
	repository := database.NewProviderRepository(f.db)
	// The secret service is the REAL one, wired exactly as provider_wiring.go wires it: the resolver
	// interface is the application service, not the store beneath it. A nil resolver would compile and
	// make the registry refuse at resolve time, which is not what these tests are about.
	secretsService := appsecrets.NewService(newPlatformSecretStore())
	registry := providers.NewRegistry(repository, secretsService, repository)
	return &textGenerator{registry: registry, db: f.db}
}

// TestTheLayerPolicyChoosesTheProvider covers section 13's per-layer rule.
//
// The supervisor's provider is deliberately NOT the first enabled one, so an implementation that returned
// "the first enabled provider" would answer with `provider-first` and fail here.
func TestTheLayerPolicyChoosesTheProvider(t *testing.T) {
	ctx := context.Background()
	fixture := newPolicyFixture(t)
	fixture.setPolicy(t, "supervision", `{"providerId":"provider-supervisor"}`)
	generator := fixture.generator(t)

	// The supervisor's layer resolves to the provider its policy names.
	got, err := generator.choice(ctx, "", agent.LayerSupervision, "policy-project")
	if err != nil {
		t.Fatalf("resolving the supervision provider: %v", err)
	}
	if got != "provider-supervisor" {
		t.Fatalf("the supervisor resolved to %q, want the provider its policy names", got)
	}
	// A layer with NO policy falls back to the first enabled provider, which is the ordinary state of a
	// project that has not configured one.
	got, err = generator.choice(ctx, "", agent.LayerExecution, "policy-project")
	if err != nil {
		t.Fatalf("resolving the execution provider: %v", err)
	}
	if got != "provider-first" {
		t.Fatalf("an unconfigured layer resolved to %q, want the first enabled provider", got)
	}
	// A NAMED provider wins over the layer's policy: a caller that states one has a reason, and the
	// policy is the answer for a caller that did not.
	got, err = generator.choice(ctx, "provider-first", agent.LayerSupervision, "policy-project")
	if err != nil {
		t.Fatalf("honouring a named provider: %v", err)
	}
	if got != "provider-first" {
		t.Fatalf("a named provider resolved to %q", got)
	}
}

// TestTheDefaultPolicyAppliesToEveryLayer covers the `default` row's purpose.
//
// A project states its ordinary provider once and overrides the layers that differ, so a policy stored as
// `default` must serve the decision layer — which is the case a lookup that only read the layer's own row
// would miss.
func TestTheDefaultPolicyAppliesToEveryLayer(t *testing.T) {
	ctx := context.Background()
	fixture := newPolicyFixture(t)
	fixture.setPolicy(t, "default", `{"providerId":"provider-supervisor"}`)
	generator := fixture.generator(t)

	for _, layer := range []agent.AgentLayer{agent.LayerDecision, agent.LayerExecution, agent.LayerSupervision} {
		got, err := generator.choice(ctx, "", layer, "policy-project")
		if err != nil {
			t.Fatalf("%s: resolving the provider: %v", layer, err)
		}
		if got != "provider-supervisor" {
			t.Fatalf("%s: resolved to %q, want the default policy's provider", layer, got)
		}
	}
	// The LAYER'S OWN row wins over the default, which is the whole reason the default exists.
	fixture.setPolicy(t, "supervision", `{"providerId":"provider-first"}`)
	got, err := generator.choice(ctx, "", agent.LayerSupervision, "policy-project")
	if err != nil {
		t.Fatalf("resolving the supervision provider: %v", err)
	}
	if got != "provider-first" {
		t.Fatalf("the layer's own policy did not win over the default: %q", got)
	}
}

// TestAPolicyNamingADisabledProviderIsRefused covers the fail-closed direction.
//
// Section 13's rules are about WHERE a project's prompts go, so a provider that has been disabled must be
// a visible problem rather than a silent fallback: falling through would send this project's supervisor
// prompts to a provider its owner did not choose, which is exactly what the per-layer policy prevents.
func TestAPolicyNamingADisabledProviderIsRefused(t *testing.T) {
	ctx := context.Background()
	fixture := newPolicyFixture(t)
	fixture.setPolicy(t, "supervision", `{"providerId":"provider-supervisor"}`)
	if _, err := fixture.db.ExecContext(ctx,
		`UPDATE provider_configs SET enabled = 0 WHERE id = 'provider-supervisor'`); err != nil {
		t.Fatalf("disabling the provider: %v", err)
	}
	generator := fixture.generator(t)
	if _, err := generator.choice(ctx, "", agent.LayerSupervision, "policy-project"); err == nil {
		t.Fatal("a disabled provider named by the policy was skipped rather than refused")
	}
	// An id that does not exist is refused too, and it is a configuration problem rather than a
	// not-found: the project's policy names it, so the project is what has to change.
	fixture.setPolicy(t, "decision", `{"providerId":"provider-that-does-not-exist"}`)
	if _, err := generator.choice(ctx, "", agent.LayerDecision, "policy-project"); err == nil {
		t.Fatal("a policy naming an unconfigured provider was accepted")
	}
	// And a layer the project has no policy for still works, so the refusals above are about the policy
	// rather than about the lookup being broken.
	if _, err := generator.choice(ctx, "", agent.LayerExecution, "policy-project"); err != nil {
		t.Fatalf("an unconfigured layer was refused: %v", err)
	}
}

// TestAnUnreadablePolicyFallsThroughRatherThanStoppingEveryRun covers the safe direction of a parse
// failure.
//
// A policy whose JSON this build cannot read states no provider, so the caller moves to the next row in
// the chain — and a document written to section 13's own example (which names a MODEL and no provider at
// all) is then handled by the fallback rather than refused. Refusing would make a policy field this build
// does not understand stop every run in the project, which is worse than not honouring it.
func TestAnUnreadablePolicyFallsThroughRatherThanStoppingEveryRun(t *testing.T) {
	ctx := context.Background()
	fixture := newPolicyFixture(t)
	// Section 13's own example: a model and no provider.
	fixture.setPolicy(t, "supervision", `{"primaryModelId":"some-model","temperature":0.2}`)
	generator := fixture.generator(t)
	got, err := generator.choice(ctx, "", agent.LayerSupervision, "policy-project")
	if err != nil {
		t.Fatalf("a policy naming only a model was refused: %v", err)
	}
	if got != "provider-first" {
		t.Fatalf("a policy naming no provider resolved to %q, want the fallback", got)
	}
	// A policy that is not an object at all: the project service refuses to STORE one, so this is a row
	// an older build or a hand edit wrote, and the fallback is the answer rather than a crash.
	fixture.setPolicy(t, "decision", `["not","an","object"]`)
	if _, err := generator.choice(ctx, "", agent.LayerDecision, "policy-project"); err != nil {
		t.Fatalf("an unreadable policy was refused rather than fallen through: %v", err)
	}
}

// TestAProjectWithNoPoliciesUsesTheFirstEnabledProvider states the fallback plainly.
//
// It is the state every fresh install is in, and the behaviour a released build must keep: a project that
// has configured nothing still runs.
func TestAProjectWithNoPoliciesUsesTheFirstEnabledProvider(t *testing.T) {
	ctx := context.Background()
	fixture := newPolicyFixture(t)
	generator := fixture.generator(t)
	for _, layer := range []agent.AgentLayer{agent.LayerDecision, agent.LayerExecution, agent.LayerSupervision} {
		got, err := generator.choice(ctx, "", layer, "policy-project")
		if err != nil {
			t.Fatalf("%s: %v", layer, err)
		}
		if got != "provider-first" {
			t.Fatalf("%s: resolved to %q with no policy at all", layer, got)
		}
	}
	// A project id that names nothing behaves the same way, which is deliberate: an unknown project is a
	// caller error this port cannot report (it has no project service), and the fallback is what a
	// mis-scoped call gets — the providers are the same for every project.
	got, err := generator.choice(ctx, "", agent.LayerExecution, "no-such-project")
	if err != nil {
		t.Fatalf("an unknown project was refused: %v", err)
	}
	if got != "provider-first" {
		t.Fatalf("an unknown project resolved to %q", got)
	}
	// With NO providers enabled at all the answer is a refusal rather than a default, which is the honest
	// state of a fresh install.
	if _, err := fixture.db.ExecContext(ctx, `UPDATE provider_configs SET enabled = 0`); err != nil {
		t.Fatalf("disabling every provider: %v", err)
	}
	if _, err := generator.choice(ctx, "", agent.LayerExecution, "policy-project"); err == nil {
		t.Fatal("a build with no enabled provider answered with a provider")
	}
}
