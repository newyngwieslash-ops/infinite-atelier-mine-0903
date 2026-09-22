import io

path = "memory_wiring_wp10_test.go"
text = io.open(path, encoding="utf-8", newline="").read()

old = '''import (
	"context"
	"strings"
	"testing"
	"time"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	appmemory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/memory"
	agentassembly "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentassembly"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/database"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/filestore"
)'''
new = '''import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	appmemory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/memory"
	appprojects "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/projects"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/database"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/filestore"
)'''
assert old in text, "import anchor"
text = text.replace(old, new, 1)

old = '''	// The drama stack is what every tool handler calls, so it must exist first - the same order
	// app.go uses.
	projects := composeProjects(handle, nil, store, nil, nil)
	if projects == nil || projects.service == nil {
		t.Fatal("the project stack did not compose")
	}
	drama := composeDrama(handle, store, projects.service)'''
new = '''	// The drama stack is what every tool handler calls, so it must exist first - the same order
	// app.go uses. The canvas writer is the one piece composeDrama does not build itself.
	canvasWriter := appprojects.NewService(appprojects.Options{
		Projects: database.NewProjectRepository(handle.SQL()),
		Canvas:   database.NewCanvasRepository(handle.SQL()),
		Settings: database.NewDramaSettingsRepository(handle.SQL()),
		Clock:    appprojectsClock{},
		IDs:      &testIDs{},
	})
	drama := composeDrama(handle, store, canvasWriter)'''
if old not in text:
    # the em-dash form
    old = old.replace(" - the same order", " \u2014 the same order")
assert old in text, "compose anchor"
text = text.replace(old, new, 1)

old = '''	ctx := context.Background()
	dir := t.TempDir()
	handle, err := database.Open(ctx, dir+"/app.db", dir+"/snapshots", database.NewMigrationsForTest(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close(ctx) })
	store := filestore.New(dir + "/files")'''
new = '''	ctx := context.Background()
	dir := t.TempDir()
	handle, err := database.Open(ctx, filepath.Join(dir, "studio.db"), filepath.Join(dir, "snapshots"))
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}
	if err := handle.Err(); err != nil {
		t.Fatalf("the database is unusable: %v", err)
	}
	t.Cleanup(func() { _ = handle.Close(context.Background()) })
	store, err := filestore.New(filepath.Join(dir, "files"), filepath.Join(dir, "temp"))
	if err != nil {
		t.Fatalf("opening the file store: %v", err)
	}'''
assert old in text, "open anchor"
text = text.replace(old, new, 1)

old = '''
// The agentassembly import is kept because the stack's assembly is what the pipeline needs; this
// assertion documents that the composed stack carries one.
var _ = func(stack *agentWiring) *agentassembly.Assembly { return stack.assembly }
'''
text = text.replace(old, "", 1)
io.open(path, "w", encoding="utf-8", newline="").write(text)
print("OK")
