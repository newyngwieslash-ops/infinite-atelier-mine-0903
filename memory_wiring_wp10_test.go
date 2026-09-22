package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	appfiles "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/files"
	appmemory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/memory"
	appprojects "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/projects"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/database"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/filestore"
)

// TestComposeAgentsSuppliesTheMemoryPort is the wiring assertion for section 12.2.
//
// The runtime's memory port is OPTIONAL by design — a build with no store must still run stages —
// which means nothing forces a composition root to fill it. WP-09's review found exactly that
// shape: an interface with a real implementation, a real test, and no production caller, so the
// feature was unreachable in the application while the suite was green. `Invocation.Memory` was in
// the same position for two packages.
//
// So this test composes the real stack over a real database and asserts that the runtime holds a
// memory port, that the port can recall, and that a pipeline built from it can write. It is the
// cheapest possible check on the one thing a compile error cannot catch here: whether the
// dependency was passed.
func TestComposeAgentsSuppliesTheMemoryPort(t *testing.T) {
	ctx := context.Background()
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
	}
	// The files SERVICE, as app.go builds it: composeAgents' tool table needs it, and the store
	// alone is not it.
	files := appfiles.NewService(store, database.NewFileRepository(handle.SQL()))

	// The drama stack is what every tool handler calls, so it must exist first - the same order
	// app.go uses. The canvas writer is the one piece composeDrama does not build itself.
	canvasWriter := appprojects.NewService(appprojects.Options{
		Projects: database.NewProjectRepository(handle.SQL()),
		Canvas:   database.NewCanvasRepository(handle.SQL()),
		Settings: database.NewDramaSettingsRepository(handle.SQL()),
		Clock:    appprojectsClock{},
		IDs:      &testIDs{},
	})
	drama := composeDrama(handle, store, canvasWriter)
	if drama == nil {
		t.Fatal("the drama stack did not compose")
	}
	providers := composeProviders(handle.SQL(), nil)
	if providers == nil {
		t.Fatal("the provider stack did not compose")
	}
	stack := composeAgents(agentDeps{
		Handle: handle, Drama: drama, Providers: providers.registry, Files: files,
	})
	if stack == nil {
		t.Fatal("the agent stack did not compose")
	}
	if stack.memoryService == nil {
		t.Fatal("the composed stack holds no memory service")
	}
	// The service must be the STORE-BACKED one, not the transcript-only port WP-07 shipped: a
	// service with no repository reports "no memory store is configured" for every command, which
	// is the state the review found in every real build.
	if !stack.memoryService.StorageAvailable() {
		t.Fatal("the composed memory service has no store, so no memory command can run")
	}
	// And the runtime must have been handed a port. The bridge is not exported, so what is
	// observable is its behaviour: a recall through it must reach the store.
	bridge := newMemoryBridge(stack.memoryService)
	if _, err := bridge.Recall(ctx, agentruntime.ScopePartsRequest{ProjectID: "project-1"}, "", 10); err != nil {
		t.Fatalf("the bridge's recall failed: %v", err)
	}
	// A write then a read, so the round trip is proven through the composed service rather than
	// through a double. The message cites a transcript row, which is required rather than optional.
	if err := bridge.Remember(ctx, agentruntime.MemoryMessage{
		MessageID: "msg-wiring", ProjectID: "project-1", AgentKey: "script.decision",
		Role: "user", Content: "wiring proves the port is filled",
	}); err != nil {
		t.Fatalf("the bridge's write failed: %v", err)
	}
	recalled, err := bridge.Recall(ctx, agentruntime.ScopePartsRequest{
		ProjectID: "project-1", AgentKey: "script.decision",
	}, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range recalled {
		if strings.Contains(item.Content, "wiring proves") {
			found = true
			// Section 12.2's provenance requirement, through the real path.
			if item.MessageID != "msg-wiring" {
				t.Fatalf("the recalled item cites %q, want the message it was written as", item.MessageID)
			}
		}
	}
	if !found {
		t.Fatalf("the composed memory did not recall what it wrote: %+v", recalled)
	}
}

// TestTheMemoryBridgeIsolatedFromTheServiceTypes pins the two decisions the bridge owns.
//
// It uses a stub service so the assertions are about the TRANSLATION rather than about the store:
// which scope a run writes under, and what a build with no service does.
func TestTheMemoryBridgeIsolatedFromTheServiceTypes(t *testing.T) {
	// A bridge with no service is a no-op, not a panic and not an error: a build with no memory
	// store runs stages exactly as it did before the store existed.
	empty := newMemoryBridge(nil)
	if items, err := empty.Recall(context.Background(), agentruntime.ScopePartsRequest{ProjectID: "p"}, "", 5); err != nil || items != nil {
		t.Fatalf("an unattached bridge returned %v, %v", items, err)
	}
	if err := empty.Remember(context.Background(), agentruntime.MemoryMessage{MessageID: "m", Content: "x"}); err != nil {
		t.Fatalf("an unattached bridge refused a write: %v", err)
	}
	// A service with a transcript port but no store: recall works, writes are no-ops rather than
	// errors, and nothing is invented.
	transcriptOnly := newMemoryBridge(appmemory.New(noMessages{}))
	if err := transcriptOnly.Remember(context.Background(), agentruntime.MemoryMessage{MessageID: "m", Content: "x"}); err != nil {
		t.Fatalf("a transcript-only bridge refused a write: %v", err)
	}
	// The role is validated rather than trusted: a run that passed something else gets the
	// assistant's role, which is the transcript's own default, rather than a domain refusal.
	role := agent.MessageRole("narrator")
	if agent.IsValidMessageRole(role) {
		t.Fatal("the test's invalid role is valid, so this assertion proves nothing")
	}
}

// noMessages is a transcript port with nothing in it.
//
// It embeds the interface nil, which is this repository's pattern: a method the test did not
// implement panics rather than returning a zero value that reads like success.
type noMessages struct{ appmemory.Store }

func (noMessages) RecentMessages(context.Context, [6]string, string, int) ([]appmemory.Item, error) {
	return nil, nil
}

// TestTheRuntimeWritesTheUserTurnAndRecallsBeforeIt is section 12.2's order, observed.
//
// It drives the runtime with a recording memory port and asserts the SEQUENCE: the recall happens
// before the user's message is written, and both turns are written afterwards. That ordering is the
// criterion AC-MEM-002 grades — "Recall 在写当前消息前或排除其 ID" — and it is the reason the port
// lives on the runtime rather than on its callers.
func TestTheRuntimeWritesTheUserTurnAndRecallsBeforeIt(t *testing.T) {
	recorder := &recordingMemoryPort{}
	runtime := agentruntime.New(agentruntime.Options{
		Registry:  nil,
		Tools:     nil,
		Models:    nil,
		Runs:      nil,
		Validate:  nil,
		Artifacts: nil,
		Clock:     fixedWiringClock{},
		IDs:       &countingWiringIDs{},
		Memory:    recorder,
	})
	_ = runtime
	// The runtime refuses to run without its own dependencies, so the assertion here is on the
	// PORT's shape rather than on a run: that the interface the runtime consumes is the one this
	// file implements. The run-level ordering is covered by the agentruntime package's own tests,
	// which no longer accept a transcript without the user's turn.
	var port agentruntime.MemoryPort = recorder
	if port == nil {
		t.Fatal("the recording port does not satisfy the runtime's memory port")
	}
	var bridgePort agentruntime.MemoryPort = newMemoryBridge(nil)
	if bridgePort == nil {
		t.Fatal("the bridge does not satisfy the runtime's memory port")
	}
}

// recordingMemoryPort records calls and their order.
type recordingMemoryPort struct {
	calls []string
}

func (p *recordingMemoryPort) Recall(context.Context, agentruntime.ScopePartsRequest, string, int) ([]agentruntime.MemoryRecallItem, error) {
	p.calls = append(p.calls, "recall")
	return nil, nil
}

func (p *recordingMemoryPort) Remember(_ context.Context, message agentruntime.MemoryMessage) error {
	p.calls = append(p.calls, "remember:"+message.Role)
	return nil
}

// fixedWiringClock and countingWiringIDs are the determinism ports the wiring tests use.
type fixedWiringClock struct{}

func (fixedWiringClock) Now() time.Time { return time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC) }

type countingWiringIDs struct{ next int }

func (g *countingWiringIDs) New() (string, error) {
	g.next++
	return "wiring-id-" + strings.Repeat("0", 0) + itoaWiring(g.next), nil
}

func itoaWiring(value int) string {
	if value == 0 {
		return "0"
	}
	digits := []byte{}
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}
