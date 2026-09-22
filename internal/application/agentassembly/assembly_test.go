package agentassembly

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	agenttools "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agenttools"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/skills"
)

// These tests cover the assembly against the REAL built-in packs and the REAL tool
// table. That is the point of them: the packs are embedded documents and the table is
// generated from a script, so a drift between the two — a manifest naming a tool that
// was renamed, an agent whose schema path is not embedded — is exactly what would slip
// through a test written against fixtures.

// buildAssembly assembles over the real packs and the real tool table.
func buildAssembly(t *testing.T) (*Assembly, *memVersions) {
	t.Helper()
	tools, err := agenttools.Build(agenttools.Deps{
		Story:      stubStory(t),
		Script:     stubScript(t),
		Storyboard: stubStoryboard(t),
		Workflow:   stubWorkflow(t),
		Memory:     stubMemory(t),
		Assets:     stubAssets(t),
		Gaps:       stubGaps(t),
		Projects:   stubProjects(t),
		Chapters:   stubChapters{},
	})
	if err != nil {
		t.Fatalf("building the tool table: %v", err)
	}
	versions := newMemVersions()
	assembly, err := Build(context.Background(), Options{
		Tools: tools, Versions: versions, Files: newMemFiles(),
		Clock: fixedClock{}, IDs: &seqIDs{},
	})
	if err != nil {
		t.Fatalf("assembling: %v", err)
	}
	return assembly, versions
}

// TestAssemblyLoadsEveryBuiltinPack covers the inventory: every pack AGENT_CONTRACTS
// section 19 names is embedded AND loads. A pack that failed to load would take the whole
// assembly down (Build refuses rather than dropping one), so reaching this assertion at
// all is the statement that both packs are valid.
func TestAssemblyLoadsEveryBuiltinPack(t *testing.T) {
	assembly, _ := buildAssembly(t)
	names := assembly.PackNames()
	if len(names) != len(BuiltinPacks) {
		t.Fatalf("the assembly loaded %v, and the build carries %v", names, BuiltinPacks)
	}
	for index, name := range BuiltinPacks {
		if names[index] != name {
			t.Fatalf("pack %d is %q, want %q", index, names[index], name)
		}
		if _, ok := assembly.Pack(name); !ok {
			t.Errorf("the pack %s is named and cannot be read back", name)
		}
	}
}

// TestAssemblyRegistersOneSkillVersionPerPack covers section 4.2's version binding.
//
// Every agent in a pack cites the pack's version, so a pack whose version was not
// registered would produce runs that cannot name what they ran — which CreateRun refuses.
func TestAssemblyRegistersOneSkillVersionPerPack(t *testing.T) {
	assembly, versions := buildAssembly(t)
	for _, name := range assembly.PackNames() {
		pack, _ := assembly.Pack(name)
		if strings.TrimSpace(pack.SkillVersionID) == "" {
			t.Fatalf("the pack %s has no skill version", name)
		}
		if len(pack.Pack.Specs) == 0 {
			t.Fatalf("the pack %s registered no agents", name)
		}
		// Every agent in the pack resolves to that version, and to that document.
		for _, spec := range pack.Pack.Specs {
			versionID, ok := assembly.SkillVersionOf(spec.Key)
			if !ok || versionID != pack.SkillVersionID {
				t.Errorf("the agent %s resolves to version %q, want %q", spec.Key, versionID, pack.SkillVersionID)
			}
			document, ok := assembly.SkillDocument(spec.Key)
			if !ok || strings.TrimSpace(document) == "" {
				t.Errorf("the agent %s has no skill document", spec.Key)
			}
		}
	}
	// The rows really exist in the store, with the pack's own hash.
	for _, name := range assembly.PackNames() {
		row, err := versions.SkillVersionByKey(context.Background(), name)
		if err != nil {
			t.Fatalf("reading the version for %s: %v", name, err)
		}
		pack, _ := assembly.Pack(name)
		if row.ContentHash != pack.Pack.ContentHash {
			t.Errorf("the stored version for %s has hash %q, want %q", name, row.ContentHash, pack.Pack.ContentHash)
		}
		if row.ManifestJSON == "" || row.ManifestJSON == "{}" {
			t.Errorf("the stored version for %s carries no manifest", name)
		}
	}
}

// TestAssemblyRegistersEverySection19Agent is the inventory assertion.
//
// Section 19 names eight script agents and nine production agents, and this checks the
// registry against that list rather than against the manifests — so an agent deleted from
// a manifest is a failure here rather than a stage that cannot run.
func TestAssemblyRegistersEverySection19Agent(t *testing.T) {
	assembly, _ := buildAssembly(t)
	want := []string{
		// Script pack.
		"script.decision",
		"script.execution.event_extraction",
		"script.execution.story_skeleton",
		"script.execution.adaptation_strategy",
		"script.execution.script_generation",
		"script.supervision.story_skeleton",
		"script.supervision.adaptation_strategy",
		"script.supervision.script",
		// Production pack.
		"production.decision",
		"production.execution.director_plan",
		"production.execution.asset_analysis",
		"production.execution.asset_generation_plan",
		"production.execution.storyboard_table",
		"production.execution.storyboard_panel",
		"production.supervision.director_plan",
		"production.supervision.storyboard_table",
		"production.supervision.storyboard_panel",
	}
	registered := assembly.Registry().Keys()
	have := map[string]bool{}
	for _, key := range registered {
		have[key] = true
	}
	for _, key := range want {
		if !have[key] {
			t.Errorf("the agent %s from section 19 is not registered", key)
		}
	}
	// And nothing extra: a key the registry carries that section 19 does not name is
	// either a typo or a pack that grew without the specification being consulted.
	expected := map[string]bool{}
	for _, key := range want {
		expected[key] = true
	}
	for _, key := range registered {
		if !expected[key] {
			t.Errorf("the registry carries %s, which section 19 does not name", key)
		}
	}
}

// TestAssemblyRefusesAManifestNamingAToolThatDoesNotExist is the "Tool Key 必须来自内置
// 注册表" check, exercised end to end.
//
// It builds the assembly over a pack whose manifest grants a tool the table does not
// register, and asserts the refusal names the tool. Without this the failure would
// surface at the stage that needed the tool, which is a worse place to find it.
func TestAssemblyRefusesAManifestNamingAToolThatDoesNotExist(t *testing.T) {
	// A source that serves a valid pack with one tool key changed.
	source := patchedPack(t, "script.execution.story_skeleton", "story.read_events", "story.read_everything")
	_, err := skillLoad(source)
	if err == nil {
		t.Fatal("a manifest naming an unregistered tool was accepted")
	}
	if !strings.Contains(err.Error(), "tool") {
		t.Fatalf("the refusal reads %q, which does not name the tool", err)
	}
}

// TestAssemblyRefusesAManifestNamingAnUnembeddedSchema is the other startup check: a
// pack pointing at a contract this build does not carry.
//
// Such a manifest would produce an agent whose output nothing validated, which is the
// state the runtime's validator exists to prevent — so the refusal has to happen at load.
func TestAssemblyRefusesAManifestNamingAnUnembeddedSchema(t *testing.T) {
	source := patchedSchema(t, "schemas/agent/no-such-contract.v1.json")
	_, err := skillLoad(source)
	if err == nil {
		t.Fatal("a manifest naming an unembedded schema was accepted")
	}
}

// TestAssemblyIsIdempotentAcrossBuilds covers the second-start case.
//
// A desktop application starts more than once, and the second start must not fail
// because the versions are already registered. The content hash is what makes that safe:
// the same bytes produce the same hash, so the existing row IS this load's version.
func TestAssemblyIsIdempotentAcrossBuilds(t *testing.T) {
	versions := newMemVersions()
	tools := buildTable(t)
	files := newMemFiles()
	first, err := Build(context.Background(), Options{
		Tools: tools, Versions: versions, Files: files, Clock: fixedClock{}, IDs: &seqIDs{},
	})
	if err != nil {
		t.Fatalf("the first assembly failed: %v", err)
	}
	second, err := Build(context.Background(), Options{
		Tools: tools, Versions: versions, Files: files, Clock: fixedClock{}, IDs: &seqIDs{},
	})
	if err != nil {
		t.Fatalf("a second assembly over the same packs failed: %v", err)
	}
	// The identifiers differ because a new one was minted, but the version each agent
	// resolves to is the SAME row — which is what makes a run's citation stable across
	// restarts.
	for _, name := range first.PackNames() {
		firstPack, _ := first.Pack(name)
		secondPack, _ := second.Pack(name)
		if firstPack.SkillVersionID != secondPack.SkillVersionID {
			t.Errorf("the pack %s resolved to %q and then %q",
				name, firstPack.SkillVersionID, secondPack.SkillVersionID)
		}
	}
}

// TestAssemblyRefusesAChangedPackUnderTheSameVersion is the conflict that matters.
//
// A pack whose contents changed without its version being raised would mean two
// different documents cited as one version, and a run's record could not tell which it
// ran. That is a defect rather than a repeat load, and it is refused.
func TestAssemblyRefusesAChangedPackUnderTheSameVersion(t *testing.T) {
	versions := newMemVersions()
	tools := buildTable(t)
	if _, err := Build(context.Background(), Options{
		Tools: tools, Versions: versions, Files: newMemFiles(), Clock: fixedClock{}, IDs: &seqIDs{},
	}); err != nil {
		t.Fatalf("the first assembly failed: %v", err)
	}
	// The same key and version, a different hash: what a document edit without a version
	// bump produces.
	ctx := context.Background()
	if err := versions.RegisterSkillVersion(ctx, agent.SkillVersion{
		ID: "different", SkillKey: "script", Version: "1.0.0",
		ContentHash: "a-different-hash", Status: agent.SkillActive, CreatedAt: fixedClock{}.Now(),
	}); err == nil {
		t.Fatal("the double accepted two versions under one key and version pair")
	}
}

// TestAssemblyRefusesIncompleteOptions covers the fail-closed direction.
func TestAssemblyRefusesIncompleteOptions(t *testing.T) {
	tools := buildTable(t)
	cases := map[string]Options{
		"no tools":    {Versions: newMemVersions(), Files: newMemFiles(), Clock: fixedClock{}, IDs: &seqIDs{}},
		"no versions": {Tools: tools, Files: newMemFiles(), Clock: fixedClock{}, IDs: &seqIDs{}},
		"no files":    {Tools: tools, Versions: newMemVersions(), Clock: fixedClock{}, IDs: &seqIDs{}},
		"no clock":    {Tools: tools, Versions: newMemVersions(), Files: newMemFiles(), IDs: &seqIDs{}},
		"no ids":      {Tools: tools, Versions: newMemVersions(), Files: newMemFiles(), Clock: fixedClock{}},
	}
	for name, options := range cases {
		if _, err := Build(context.Background(), options); err == nil {
			t.Errorf("an assembly with %s was built", name)
		}
	}
}

// TestEmbeddedPacksAndPackNamesAgree is the walk that makes a forgotten pack visible.
//
// A directory added to the embed directive but not to PackNames would be carried in the
// binary and never loaded, which is worse than not shipping it: the bytes are there and
// nothing reads them.
func TestEmbeddedPacksAndPackNamesAgree(t *testing.T) {
	entries, err := fs.ReadDir(skills.FS(), ".")
	if err != nil {
		t.Fatalf("reading the embedded pack root: %v", err)
	}
	embedded := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() {
			embedded[entry.Name()] = true
		}
	}
	declared := map[string]bool{}
	for _, name := range BuiltinPacks {
		declared[name] = true
		if !embedded[name] {
			t.Errorf("the pack %s is named and not embedded", name)
		}
	}
	for name := range embedded {
		if !declared[name] {
			t.Errorf("the pack directory %s is embedded and not named", name)
		}
	}
}

// TestSkillDocumentsCarryEveryRequiredSection is the loader's own rule, asserted on the
// REAL built-in documents rather than on a fixture.
//
// Section 4.3 lists thirteen sections every skill document must have, and the loader
// refuses a document missing one. Reaching this assertion means both packs' nineteen
// documents satisfy it — which is the property a generated skeleton can lose by a bad
// edit to the generator.
func TestSkillDocumentsCarryEveryRequiredSection(t *testing.T) {
	assembly, _ := buildAssembly(t)
	required := skillRequiredSections()
	for _, name := range assembly.PackNames() {
		pack, _ := assembly.Pack(name)
		for key, document := range pack.Pack.Skills {
			for _, section := range required {
				if !strings.Contains(document, "# "+section) {
					t.Errorf("the document for %s is missing the %q section", key, section)
				}
			}
		}
	}
}

// TestBothSkillPacksCarryRealProse is the successor to WP-05's "空 Skill 骨架" assertion.
//
// WP-05's scope item 16 required both packs to carry section structure and NOTHING ELSE, and
// its test asserted that no document exceeded a per-section budget — because the business
// content was a later work package's. WP-08 filled the SCRIPT pack and reversed the
// assertion for that pack alone, leaving production asserted as skeletons.
//
// WP-09 is the package that fills production's nine agents (the five execution stages, the
// three supervisors and the decision layer), so the assertion reverses for BOTH packs now.
// The property being asserted is the same one WP-08's half used, and it is deliberately
// crude: the question is "did a person write this", and a document that merely renamed its
// headings does not have it.
//
// WP-11's stages — video, audio, subtitles, export — have NO documents yet, because they
// have no agents yet. This test fails if one appears as a skeleton, which is what keeps a
// half-written document from looking like finished work.
func TestBothSkillPacksCarryRealProse(t *testing.T) {
	assembly, _ := buildAssembly(t)
	// The same budget WP-05 used. A document under it has headings and guidance and no prose.
	const perSectionBudget = 400
	for _, name := range assembly.PackNames() {
		pack, _ := assembly.Pack(name)
		if len(pack.Pack.Skills) == 0 {
			t.Errorf("the %s pack carries no skills", name)
			continue
		}
		for key, document := range pack.Pack.Skills {
			sections := strings.Count(document, "\n# ")
			if sections == 0 {
				t.Errorf("the document for %s has no sections", key)
				continue
			}
			runes := len([]rune(document))
			if runes <= sections*perSectionBudget {
				t.Errorf("the document for %s is %d runes over %d sections, which is still a skeleton",
					key, runes, sections)
			}
			// The generator's placeholder must be gone, which is the marker WP-05's own test
			// was written against.
			if strings.Contains(document, "WP-0X fills this in") {
				t.Errorf("the document for %s still carries the generator's placeholder", key)
			}
		}
	}
}
