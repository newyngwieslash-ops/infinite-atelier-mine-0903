package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
)

// These tests cover the loader's refusals, because a pack's static validation is
// the only moment its constraints are cheap to enforce. The built-in packs are
// loaded too, which is what proves the generator and the loader agree about the
// required sections: a document the generator writes but the loader demands
// differently would fail here rather than at startup.

// repoRoot is the repository root as this package sees it: three levels up from
// internal/application/skill.
func repoRoot() string {
	return filepath.Join("..", "..", "..")
}

// knownTools is a registry stand-in. The tests below name tools from it and, in
// the refusal cases, one that is not.
var knownTools = map[string]bool{
	"workflow.read_state":                  true,
	"workflow.request_user_gate":           true,
	"agent.invoke_execution":               true,
	"agent.invoke_supervisor":              true,
	"memory.deep_recall":                   true,
	"script.create_story_skeleton_version": true,
}

// knownSchemas is the embed set these tests allow.
var knownSchemas = map[string]bool{
	"schemas/agent/decision-request.v1.json":  true,
	"schemas/agent/decision-result.v1.json":   true,
	"schemas/agent/execution-request.v1.json": true,
	"schemas/agent/execution-result.v1.json":  true,
}

// validDocument is a skill document carrying every required section exactly once.
//
// The first version wrote a Role heading and then looped over the whole list,
// which wrote Role twice — so the test that removes one section removed the first
// and left the second, and it failed for a reason that had nothing to do with the
// loader.
func validDocument() string {
	var builder strings.Builder
	builder.WriteString("# Title\n\nA pack's document.\n")
	for _, section := range RequiredSections() {
		builder.WriteString("\n# " + section + "\n\nText.\n")
	}
	return builder.String()
}

// validManifest is a one-agent pack manifest.
func validManifest() string {
	return `{
  "apiVersion": "atelier.agent/v1",
  "kind": "AgentPack",
  "metadata": { "name": "script", "version": "1.0.0" },
  "agents": [
    {
      "key": "script.decision",
      "layer": "decision",
      "skill": "decision.md",
      "inputSchema": "schemas/agent/decision-request.v1.json",
      "outputSchema": "schemas/agent/decision-result.v1.json",
      "allowedTools": ["workflow.read_state", "agent.invoke_execution"],
      "limits": { "maxToolCalls": 6, "timeoutSeconds": 180 }
    }
  ]
}`
}

// optionsFor builds a source holding the given files.
func optionsFor(files map[string]string) LoadOptions {
	return LoadOptions{
		Source:           Map(files),
		ManifestPath:     "manifest.json",
		KnownToolKeys:    knownTools,
		KnownSchemaPaths: knownSchemas,
	}
}

// TestLoadAcceptsAWellFormedPack covers the happy path and the derived fields.
func TestLoadAcceptsAWellFormedPack(t *testing.T) {
	pack, err := Load(optionsFor(map[string]string{
		"manifest.json": validManifest(),
		"decision.md":   validDocument(),
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(pack.Specs) != 1 {
		t.Fatalf("the pack produced %d specs", len(pack.Specs))
	}
	spec := pack.Specs[0]
	if spec.Key != "script.decision" || spec.Layer != agent.LayerDecision {
		t.Fatalf("the spec read as %+v", spec)
	}
	if spec.Limits.MaxToolCalls != 6 {
		t.Fatalf("the tool-call budget read as %d", spec.Limits.MaxToolCalls)
	}
	// The timeout arrives in seconds and must become a duration, not a count of
	// seconds interpreted as nanoseconds.
	if spec.Limits.MaxDuration.Seconds() != 180 {
		t.Fatalf("the timeout read as %v, want 180s", spec.Limits.MaxDuration)
	}
	// The policy layer follows the agent's own layer, so a project can point the
	// decision layer at a different model from the execution one.
	if spec.PolicyLayer != agent.PolicyDecision {
		t.Fatalf("the policy layer read as %q", spec.PolicyLayer)
	}
	// The content hash is a digest, and the documents are kept.
	if len(pack.ContentHash) != 64 {
		t.Fatalf("the content hash is %q", pack.ContentHash)
	}
	if !strings.Contains(pack.Skills["script.decision"], "# Role") {
		t.Fatal("the skill document was not kept")
	}
}

// TestLoadIsDeterministicAboutItsHash covers the property a run's reproducibility
// rests on: the same bytes produce the same version hash.
func TestLoadIsDeterministicAboutItsHash(t *testing.T) {
	files := map[string]string{"manifest.json": validManifest(), "decision.md": validDocument()}
	first, err := Load(optionsFor(files))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Load(optionsFor(files))
	if err != nil {
		t.Fatal(err)
	}
	if first.ContentHash != second.ContentHash {
		t.Fatalf("the same pack hashed differently: %s vs %s", first.ContentHash, second.ContentHash)
	}
	// A changed document changes the hash, which is what makes the version a
	// version rather than a label.
	edited := map[string]string{"manifest.json": validManifest(), "decision.md": validDocument() + "\n# Extra\n\nMore.\n"}
	third, err := Load(optionsFor(edited))
	if err != nil {
		t.Fatal(err)
	}
	if third.ContentHash == first.ContentHash {
		t.Fatal("an edited document produced the same hash, so a run could not tell the versions apart")
	}
}

// TestLoadRefusesAManifestThatIsNotOne covers the pack-level checks.
func TestLoadRefusesAManifestThatIsNotOne(t *testing.T) {
	cases := []struct {
		name     string
		manifest string
	}{
		{"not JSON", "this is not a manifest"},
		{"an array", `[{"apiVersion":"atelier.agent/v1"}]`},
		{"an unknown apiVersion", strings.Replace(validManifest(), "atelier.agent/v1", "atelier.agent/v2", 1)},
		{"no apiVersion", strings.Replace(validManifest(), `"apiVersion": "atelier.agent/v1",`, "", 1)},
		{"an unknown kind", strings.Replace(validManifest(), "AgentPack", "ScriptPack", 1)},
		{"no name", strings.Replace(validManifest(), `"name": "script"`, `"name": ""`, 1)},
		{"an upper-case name", strings.Replace(validManifest(), `"name": "script"`, `"name": "Script"`, 1)},
		{"no version", strings.Replace(validManifest(), `"version": "1.0.0"`, `"version": ""`, 1)},
		{"no agents", strings.Replace(validManifest(), `"agents": [`, `"agents": [], "unused": [`, 1)},
		// A field from a later format must fail loudly rather than be ignored.
		{"an unknown field", strings.Replace(validManifest(), `"kind": "AgentPack",`, `"kind": "AgentPack", "sandbox": {},`, 1)},
		// Two agents under one key would make the registry ambiguous.
		{"a repeated agent key", strings.Replace(validManifest(),
			`"limits": { "maxToolCalls": 6, "timeoutSeconds": 180 }`,
			`"limits": { "maxToolCalls": 6, "timeoutSeconds": 180 } }, {
			"key": "script.decision", "layer": "decision", "skill": "decision.md",
			"inputSchema": "schemas/agent/decision-request.v1.json",
			"outputSchema": "schemas/agent/decision-result.v1.json",
			"allowedTools": [], "limits": { "maxToolCalls": 1, "timeoutSeconds": 60 }`, 1)},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := Load(optionsFor(map[string]string{"manifest.json": testCase.manifest, "decision.md": validDocument()}))
			if err == nil {
				t.Fatal("a malformed manifest was accepted")
			}
		})
	}
}

// TestLoadRefusesAToolThatIsNotRegistered is section 4.2's "Tool Key 必须来自内置
// 注册表". A pack that could name its own tool would grant itself a capability.
func TestLoadRefusesAToolThatIsNotRegistered(t *testing.T) {
	manifest := strings.Replace(validManifest(), `"workflow.read_state"`, `"story.delete_everything"`, 1)
	_, err := Load(optionsFor(map[string]string{"manifest.json": manifest, "decision.md": validDocument()}))
	if err == nil {
		t.Fatal("a manifest naming an unregistered tool was accepted")
	}
	if !strings.Contains(err.Error(), "tool") {
		t.Fatalf("the refusal does not say it is about a tool: %v", err)
	}
}

// TestLoadRefusesASchemaThatIsNotEmbedded covers the same rule for schemas: an
// agent whose output is unchecked must not load.
func TestLoadRefusesASchemaThatIsNotEmbedded(t *testing.T) {
	for _, field := range []string{"inputSchema", "outputSchema"} {
		manifest := strings.Replace(validManifest(),
			`"schemas/agent/`+map[string]string{"inputSchema": "decision-request", "outputSchema": "decision-result"}[field]+`.v1.json"`,
			`"schemas/agent/not-a-schema.v1.json"`, 1)
		if _, err := Load(optionsFor(map[string]string{"manifest.json": manifest, "decision.md": validDocument()})); err == nil {
			t.Fatalf("a manifest naming a missing %s was accepted", field)
		}
	}
}

// TestLoadRefusesAnUnsafePath is section 4.2's "只允许相对路径". Each case is a path
// that would let a pack choose what the loader reads.
func TestLoadRefusesAnUnsafePath(t *testing.T) {
	for _, unsafe := range []string{
		"/etc/passwd",
		`\windows\system32\config`,
		"../../outside.md",
		"../sibling.md",
		"C:/absolute.md",
		"C:\\absolute.md",
		"https://example.invalid/skill.md",
		"skill\x00.md",
		"sub//double.md",
		"./leading-dot.md",
	} {
		t.Run(unsafe, func(t *testing.T) {
			manifest := strings.Replace(validManifest(), `"skill": "decision.md"`, `"skill": "`+strings.ReplaceAll(unsafe, `\`, `\\`)+`"`, 1)
			if _, err := Load(optionsFor(map[string]string{"manifest.json": manifest, unsafe: validDocument()})); err == nil {
				t.Fatalf("the path %q was accepted", unsafe)
			}
		})
	}
	// A plain nested path is fine, so the check is not refusing subdirectories.
	nested := strings.Replace(validManifest(), `"skill": "decision.md"`, `"skill": "execution/decision.md"`, 1)
	if _, err := Load(optionsFor(map[string]string{
		"manifest.json":         validManifest(),
		"decision.md":           validDocument(),
		"execution/decision.md": validDocument(),
	})); err != nil {
		t.Fatalf("a plain pack was refused: %v", err)
	}
	if _, err := Load(optionsFor(map[string]string{"manifest.json": nested, "execution/decision.md": validDocument()})); err != nil {
		t.Fatalf("a nested skill path was refused: %v", err)
	}
}

// TestLoadRefusesADocumentMissingASection is section 4.3's section list.
//
// Each section is a rule the agent is meant to follow, so a document without one
// has not stated something it needed to state.
func TestLoadRefusesADocumentMissingASection(t *testing.T) {
	for _, section := range RequiredSections() {
		t.Run(section, func(t *testing.T) {
			document := strings.Replace(validDocument(), "# "+section+"\n", "", 1)
			if document == validDocument() {
				t.Fatalf("the fixture did not contain a %s heading", section)
			}
			_, err := Load(optionsFor(map[string]string{"manifest.json": validManifest(), "decision.md": document}))
			if err == nil {
				t.Fatalf("a document missing its %s section was accepted", section)
			}
		})
	}
}

// TestLoadRefusesASkillThatAsksForPrivateReasoning covers section 4.3's "要求
// reasonSummary，而非内部推理全文".
func TestLoadRefusesASkillThatAsksForPrivateReasoning(t *testing.T) {
	for _, phrase := range []string{
		"Please show your chain of thought.",
		"Show your reasoning in full.",
		"请输出你的思考过程。",
	} {
		document := validDocument() + "\n" + phrase + "\n"
		if _, err := Load(optionsFor(map[string]string{"manifest.json": validManifest(), "decision.md": document})); err == nil {
			t.Fatalf("a document asking for %q was accepted", phrase)
		}
	}
}

// TestLoadRefusesAnOversizedOrMissingDocument covers the bounds and the missing
// file, so a pack cannot make the loader read an unbounded amount or half load.
func TestLoadRefusesAnOversizedOrMissingDocument(t *testing.T) {
	// Missing.
	if _, err := Load(optionsFor(map[string]string{"manifest.json": validManifest()})); err == nil {
		t.Fatal("a manifest naming a missing document was accepted")
	}
	// Oversized on its own.
	huge := validDocument() + strings.Repeat("x", MaxSkillDocumentBytes)
	if _, err := Load(optionsFor(map[string]string{"manifest.json": validManifest(), "decision.md": huge})); err == nil {
		t.Fatal("an oversized document was accepted")
	}
	// Oversized as a pack.
	larger := validDocument() + strings.Repeat("x", 2048)
	options := optionsFor(map[string]string{"manifest.json": validManifest(), "decision.md": larger})
	options.MaxPackBytes = 1024
	if _, err := Load(options); err == nil {
		t.Fatal("a pack over its total bound was accepted")
	}
	// An oversized manifest.
	if _, err := Load(optionsFor(map[string]string{
		"manifest.json": validManifest() + strings.Repeat(" ", MaxManifestBytes),
		"decision.md":   validDocument(),
	})); err == nil {
		t.Fatal("an oversized manifest was accepted")
	}
}

// TestLoadRefusesWithNoSource covers the fail-closed case.
func TestLoadRefusesWithNoSource(t *testing.T) {
	if _, err := Load(LoadOptions{}); err == nil {
		t.Fatal("a load with no source was accepted")
	}
}

// TestBuiltInPacksLoad is the test that keeps the generator and the loader in
// step, and the reason the built-in packs are trustworthy: it loads the packs this
// repository actually ships, from disk, with the same checks a startup would run.
//
// The registry contents are the packs' own tool lists, which is a weaker check
// than the real registry will do (that one compares against the built-in tool
// table) but exactly strong enough to answer "does this pack parse, and does the
// generator write the sections the loader requires".
func TestBuiltInPacksLoad(t *testing.T) {
	for _, packName := range []string{"script", "production"} {
		t.Run(packName, func(t *testing.T) {
			root := filepath.Join(repoRoot(), "skills", packName)
			manifestBody, err := os.ReadFile(filepath.Join(root, "manifest.json"))
			if err != nil {
				t.Fatalf("reading the %s pack: %v", packName, err)
			}
			// The tool and schema sets are taken from the pack itself, because this
			// test is about the documents and the manifest shape rather than about
			// which tools exist — the runtime's own test covers that.
			declared := declaredNames(string(manifestBody))
			options := LoadOptions{
				Source:           FS(os.DirFS(root)),
				ManifestPath:     "manifest.json",
				KnownToolKeys:    declared.tools,
				KnownSchemaPaths: embedSchemaPaths(),
			}
			pack, err := Load(options)
			if err != nil {
				t.Fatalf("the built-in %s pack does not load: %v", packName, err)
			}
			if len(pack.Specs) == 0 {
				t.Fatalf("the %s pack declares no agents", packName)
			}
			for _, spec := range pack.Specs {
				if err := spec.Validate(); err != nil {
					t.Fatalf("the %s pack's agent %s is invalid: %v", packName, spec.Key, err)
				}
			}
			t.Logf("%s: %d agent(s), %d document(s), hash %s", packName, len(pack.Specs), len(pack.Skills), pack.ContentHash[:12])
		})
	}
}

// declaredNames pulls the tool keys and schema paths out of a manifest, so the
// built-in pack test can check its documents without asserting which tools the
// runtime registers.
type declared struct {
	tools   map[string]bool
	schemas map[string]bool
}

func declaredNames(manifest string) declared {
	out := declared{tools: map[string]bool{}, schemas: map[string]bool{}}
	for _, line := range strings.Split(manifest, "\n") {
		trimmed := strings.TrimSpace(line)
		// A tool key line is a quoted dotted name inside allowedTools.
		if strings.HasPrefix(trimmed, `"`) && strings.HasSuffix(trimmed, `",`) || strings.HasSuffix(trimmed, `"`) {
			value := strings.Trim(trimmed, `",`)
			if strings.Contains(value, ".") && !strings.Contains(value, "/") {
				if agent.ValidateToolKey(value) == nil {
					out.tools[value] = true
				}
			}
			if strings.HasPrefix(value, "schemas/") {
				out.schemas[value] = true
			}
		}
	}
	return out
}

// embedSchemaPaths returns the schema paths the build carries. It is defined here
// rather than imported from the embed package so this test compiles before the
// runtime's registry does; the registry's own test asserts the real set.
func embedSchemaPaths() map[string]bool {
	return map[string]bool{
		"schemas/agent/decision-request.v1.json":    true,
		"schemas/agent/decision-result.v1.json":     true,
		"schemas/agent/execution-request.v1.json":   true,
		"schemas/agent/execution-result.v1.json":    true,
		"schemas/agent/supervision-request.v1.json": true,
		"schemas/agent/review-report.v1.json":       true,
		"schemas/agent/agent-error.v1.json":         true,
		"schemas/agent/event_extraction.v1.json":    true,
	}
}
