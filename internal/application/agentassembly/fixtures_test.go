package agentassembly

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	agenttools "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agenttools"
	appassets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	appextraction "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/extraction"
	appmemory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/memory"
	appprojects "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/projects"
	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/skill"
	appstory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/story"
	appstoryboard "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/storyboard"
	appworkflow "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/workflow"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/skills"
)

// fixtures_test.go holds the doubles and the pack-patching helpers the assembly tests
// use.
//
// The services are built through their own constructors with empty options rather than as
// bare structs, because a constructor is what a composition root calls and a bare struct
// would skip any defaulting the service does — the test would then be exercising a shape
// no real build produces.

// buildTable assembles the real tool table over stub services.
func buildTable(t *testing.T) *agentruntime.Tools {
	t.Helper()
	tools, err := agenttools.Build(agenttools.Deps{
		Story:      stubStory(t),
		Script:     stubScript(t),
		Storyboard: stubStoryboard(t),
		Workflow:   stubWorkflow(t),
		Memory:     stubMemory(t),
		Assets:     stubAssets(t),
		Projects:   stubProjects(t),
		Chapters:   stubChapters{},
	})
	if err != nil {
		t.Fatalf("building the tool table: %v", err)
	}
	return tools
}

// The stub service constructors. Each returns a non-nil service, which is what the tool
// table's Build checks: it verifies COMPOSITION rather than reachability, and a handler
// that then failed against a nil database is another test's subject.

func stubStory(t *testing.T) *appstory.Service {
	t.Helper()
	return appstory.NewService(appstory.Options{})
}
func stubScript(t *testing.T) *appscript.Service {
	t.Helper()
	return appscript.NewService(appscript.Options{})
}
func stubStoryboard(t *testing.T) *appstoryboard.Service {
	t.Helper()
	return appstoryboard.NewService(appstoryboard.Options{})
}
func stubWorkflow(t *testing.T) *appworkflow.Service {
	t.Helper()
	return appworkflow.NewService(appworkflow.Options{})
}
func stubMemory(t *testing.T) *appmemory.Service { t.Helper(); return appmemory.New(nil) }
func stubAssets(t *testing.T) *appassets.Service {
	t.Helper()
	return appassets.NewService(appassets.Options{})
}
func stubProjects(t *testing.T) *appprojects.Service {
	t.Helper()
	return appprojects.NewService(appprojects.Options{})
}

// stubChapters satisfies the chapter-reader port without reading anything.
type stubChapters struct{}

func (stubChapters) ChapterWithText(_ context.Context, _ string) (appextraction.ChapterText, error) {
	return appextraction.ChapterText{}, nil
}

// skillLoad loads a patched pack through the skill loader, which is how the tests below
// exercise a manifest the real packs do not have.
//
// It uses the same tool keys and schema paths the assembly does, so a refusal it observes
// is about the PATCH rather than about a missing dependency of the test.
func skillLoad(source skill.ManifestSource) (skill.LoadedPack, error) {
	known := map[string]bool{}
	for _, key := range realToolKeys() {
		known[key] = true
	}
	return skill.Load(skill.LoadOptions{
		Source:           source,
		ManifestPath:     "manifest.json",
		KnownToolKeys:    known,
		KnownSchemaPaths: embeddedAgentSchemaSet(),
	})
}

// realToolKeys reads the registered keys the loader checks against.
//
// The keys are a property of the TABLE rather than of any handler, and this package
// cannot build the table without services — so the list is read from the generator's own
// output, which is what the table is built from and what a test asserts it agrees with
// (agenttools.TestEverySchemaHasARegisteredTool).
func realToolKeys() []string {
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "schemas", "agent", "tools", "KEYS.txt"))
	if err != nil {
		return nil
	}
	keys := []string{}
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			keys = append(keys, trimmed)
		}
	}
	return keys
}

// patchedPack serves one of the real packs with a change applied to it.
//
// The change is a string replacement in the manifest, which is what a renamed tool looks
// like: the pack still names it, and the table no longer carries it.
func patchedPack(t *testing.T, agentKey, oldText, newText string) skill.ManifestSource {
	t.Helper()
	files := packFiles(t, "script")
	manifest := files["manifest.json"]
	if !strings.Contains(manifest, oldText) {
		t.Fatalf("the change does not apply: the manifest does not contain %q", oldText)
	}
	files["manifest.json"] = strings.Replace(manifest, oldText, newText, 1)
	_ = agentKey
	return skill.Map(files)
}

// patchedSchema serves the script pack with its OUTPUT schema path changed.
//
// It patches every agent's outputSchema, because the point is a path this build does not
// embed rather than which agent names it.
func patchedSchema(t *testing.T, schemaPath string) skill.ManifestSource {
	t.Helper()
	files := packFiles(t, "script")
	var document map[string]any
	if err := json.Unmarshal([]byte(files["manifest.json"]), &document); err != nil {
		t.Fatalf("the script manifest is not valid JSON: %v", err)
	}
	agents, _ := document["agents"].([]any)
	if len(agents) == 0 {
		t.Fatal("the script manifest carries no agents")
	}
	first, _ := agents[0].(map[string]any)
	first["outputSchema"] = schemaPath
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("re-rendering the manifest: %v", err)
	}
	files["manifest.json"] = string(encoded)
	return skill.Map(files)
}

// packFiles reads one embedded pack into a map, which is what skill.Map takes.
func packFiles(t *testing.T, name string) map[string]string {
	t.Helper()
	sub, err := skills.Sub(name)
	if err != nil {
		t.Fatalf("reading the pack %s: %v", name, err)
	}
	files := map[string]string{}
	err = fs.WalkDir(sub, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		body, readErr := fs.ReadFile(sub, path)
		if readErr != nil {
			return readErr
		}
		files[path] = string(body)
		return nil
	})
	if err != nil {
		t.Fatalf("walking the pack %s: %v", name, err)
	}
	return files
}

// skillRequiredSections returns the sections the loader demands, by reading the pack that
// already passed validation rather than by listing them a second time.
//
// It parses the loader's own list indirectly: a document the loader accepted HAS every
// required section, so the set is recovered from one of them. That keeps this test from
// carrying a second copy of the list — a copy that would agree with itself and not with
// the loader.
//
// The FIRST heading is skipped, because it is the document's own title
// (`# script/script.decision`) rather than one of the thirteen sections. Reading it as a
// section is what made an earlier version of this test demand a heading no document has.
func skillRequiredSections() []string {
	sub, err := skills.Sub("script")
	if err != nil {
		return nil
	}
	body, err := fs.ReadFile(sub, "decision.md")
	if err != nil {
		return nil
	}
	sections := []string{}
	seenTitle := false
	for _, line := range strings.Split(string(body), "\n") {
		if !strings.HasPrefix(line, "# ") {
			continue
		}
		if !seenTitle {
			seenTitle = true
			continue
		}
		sections = append(sections, strings.TrimSpace(strings.TrimPrefix(line, "# ")))
	}
	return sections
}
