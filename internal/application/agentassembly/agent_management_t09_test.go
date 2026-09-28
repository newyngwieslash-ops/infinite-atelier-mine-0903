package agentassembly

import (
	"testing"
)

// agent_management_t09_test.go is FR-090's management surface at the
// assembly's boundary: the stop switch gates the one read the runtime already
// guards on, the readback agrees with the switch, and an unknown key is
// refused rather than silently stored.

// TestAStoppedAgentRefusesItsSkillDocument drives the switch and asserts the
// gate: disabled means the runtime's skill read answers false, which is where
// the runtime's own refusal happens.
func TestAStoppedAgentRefusesItsSkillDocument(t *testing.T) {
	assembly, _ := buildAssembly(t)
	// Pick any registered agent key from the built-in packs.
	key := ""
	for _, name := range BuiltinPacks {
		if pack, ok := assembly.Pack(name); ok {
			for agentKey := range pack.Pack.Skills {
				key = agentKey
				break
			}
		}
		if key != "" {
			break
		}
	}
	if key == "" {
		t.Fatal("the build registered no agents; the walk cannot pick one")
	}
	if !assembly.AgentEnabled(key) {
		t.Fatalf("a freshly built assembly has %q disabled", key)
	}
	document, ok := assembly.SkillDocument(key)
	if !ok || document == "" {
		t.Fatalf("the enabled agent's skill document read as (%q, %v)", document, ok)
	}

	// STOP.
	if err := assembly.SetAgentEnabled(key, false); err != nil {
		t.Fatalf("stopping the agent: %v", err)
	}
	if assembly.AgentEnabled(key) {
		t.Fatal("a stopped agent still reports enabled")
	}
	if _, ok := assembly.SkillDocument(key); ok {
		t.Fatal("a stopped agent's skill document was served; the runtime would run it")
	}

	// START again.
	if err := assembly.SetAgentEnabled(key, true); err != nil {
		t.Fatalf("starting the agent: %v", err)
	}
	if !assembly.AgentEnabled(key) {
		t.Fatal("a restarted agent still reports disabled")
	}
	if _, ok := assembly.SkillDocument(key); !ok {
		t.Fatal("a restarted agent's skill document did not come back")
	}
}

// TestAnUnknownAgentKeyIsRefused is the management surface's validation: a
// stop switch for a key no pack carries is refused rather than stored.
func TestAnUnknownAgentKeyIsRefused(t *testing.T) {
	assembly, _ := buildAssembly(t)
	if err := assembly.SetAgentEnabled("no-such-agent", false); err == nil {
		t.Fatal("an unknown agent key was accepted")
	}
	if len(assembly.DisabledAgents()) != 0 {
		t.Fatalf("%d agents disabled after a refused stop", len(assembly.DisabledAgents()))
	}
}
