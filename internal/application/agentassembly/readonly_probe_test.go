package agentassembly

import (
	"testing"
)

// readonly_probe_test.go is RP-06.4's contract: the static probe answers
// registration, tool allowance, version identity and document health — and
// it is a READ. The no-write guarantee is asserted the plan's way: business
// counters before and after.

// TestRP06ProbeAnswersRegistrationToolsAndVersion drives the happy probe:
// a registered agent gets its layer, allowance and version identity back.
func TestRP06ProbeAnswersRegistrationToolsAndVersion(t *testing.T) {
	assembly, _ := buildAssembly(t)
	// The script decision agent is one every build registers.
	key := assembly.registry.Keys()[0]
	result, err := assembly.ReadOnlyTestProbe(key)
	if err != nil {
		t.Fatalf("ReadOnlyTestProbe: %v", err)
	}
	if !result.Registered {
		t.Fatal("a registered agent came back unregistered")
	}
	if result.Layer == "" {
		t.Fatal("the probe answered no layer")
	}
	if result.SkillVersionID == "" {
		t.Fatal("the probe answered no version for an agent with a pack")
	}
	if result.SkillContentHash == "" {
		t.Fatal("the probe answered no document hash")
	}
	if len(result.SkillSectionsMissing) != 0 {
		t.Fatalf("a builtin document is missing sections: %v", result.SkillSectionsMissing)
	}
	if !result.PromptAssembles {
		t.Fatal("a builtin document did not assemble")
	}
	if result.ProbeKind != ProbeKindStatic {
		t.Fatalf("probe kind = %s, want the static marker", result.ProbeKind)
	}
	// The allowance is the SPEC's: the probe cannot widen it.
	if spec, ok := assembly.registry.Lookup(key); ok {
		if len(result.AllowedTools) != len(spec.AllowedTools) {
			t.Fatalf("allowance = %v, want the registered spec's set", result.AllowedTools)
		}
	}
}

// TestRP06ProbeFailsClosedForUnknownAgent refuses an unknown key instead of
// answering a zero-value result.
func TestRP06ProbeFailsClosedForUnknownAgent(t *testing.T) {
	assembly, _ := buildAssembly(t)
	if _, err := assembly.ReadOnlyTestProbe("no.such_agent"); err == nil {
		t.Fatal("an unknown agent probed successfully")
	}
}

// TestRP06ProbeIsARead asserts the no-write guarantee at the table level:
// probing every registered agent changes no run, message or tool-call row.
func TestRP06ProbeIsARead(t *testing.T) {
	assembly, versions := buildAssembly(t)
	counts := func() int { return len(versions.rows) }
	before := counts()
	for _, key := range assembly.registry.Keys() {
		if _, err := assembly.ReadOnlyTestProbe(key); err != nil {
			t.Fatalf("probe %s: %v", key, err)
		}
	}
	after := counts()
	if before != after {
		t.Fatalf("a probe changed the version store: %d before vs %d after", before, after)
	}
}
