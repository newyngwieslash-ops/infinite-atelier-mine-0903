package database

import (
	"context"
	"path/filepath"
	"testing"
)

// agent_enabled_persistence_rp06_test.go is RP-06.3's persistence contract:
// the user's agent enable/disable switches survive a restart. The in-memory
// switch T09 built resets on restart by design; the persisted layer is what
// the startup assembly reads back. The RED case the plan names is
// "禁用 → 重启 → 仍禁用", driven here against a real database across TWO
// handle lifetimes sharing only the file.

// TestRP06AgentDisableSurvivesRestart drives two opens over one database:
// the first disables an agent and closes; the second reads the switch back
// and must see the same set.
func TestRP06AgentDisableSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "app.db")

	// LIFETIME ONE: open, disable, close.
	handleOne, err := Open(context.Background(), dbPath, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repoOne := NewAgentRepository(handleOne.SQL())
	if err := repoOne.PersistDisabledAgents(context.Background(), []string{"script.decision", "script.supervisor"}); err != nil {
		t.Fatalf("persist disable: %v", err)
	}
	disabled, found, err := repoOne.LoadDisabledAgents(context.Background())
	if err != nil || !found {
		t.Fatalf("read-back in lifetime one: found=%v err=%v", found, err)
	}
	if len(disabled) != 2 {
		t.Fatalf("lifetime one disabled = %v", disabled)
	}
	if err := handleOne.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	// LIFETIME TWO: a fresh handle over the same file — the restart.
	handleTwo, err := Open(context.Background(), dbPath, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer handleTwo.Close(context.Background())
	repoTwo := NewAgentRepository(handleTwo.SQL())
	disabledAfter, foundAfter, err := repoTwo.LoadDisabledAgents(context.Background())
	if err != nil || !foundAfter {
		t.Fatalf("the restart lost the switches: found=%v err=%v", foundAfter, err)
	}
	if len(disabledAfter) != 2 || disabledAfter[0] != "script.decision" {
		t.Fatalf("after the restart the disabled set is %v, want the two persisted keys", disabledAfter)
	}
}

// TestRP06AgentSwitchRevisionConflict refuses a lost update: two writers
// read the same revision, the first writes, and the second must fail with a
// conflict rather than silently overwrite.
func TestRP06AgentSwitchRevisionConflict(t *testing.T) {
	handle, err := Open(context.Background(), filepath.Join(t.TempDir(), "app.db"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close(context.Background())
	repo := NewAgentRepository(handle.SQL())
	ctx := context.Background()

	if _, _, found, err := repo.GetSetting(ctx, agentEnabledKey); err != nil || found {
		t.Fatalf("a fresh database should have no switch row: found=%v err=%v", found, err)
	}
	// Writer A creates the setting (expectedRevision 0 is the create path).
	if err := repo.SetSetting(ctx, agentEnabledKey, `["a"]`, 0); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Writer B also "read" the pre-create state (revision 0) and writes: a
	// second create against an existing row must conflict.
	if err := repo.SetSetting(ctx, agentEnabledKey, `["b"]`, 0); err == nil {
		t.Fatal("a second create against an existing row was accepted")
	}
	// A normal update under the CURRENT revision succeeds.
	_, revision, _, err := repo.GetSetting(ctx, agentEnabledKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SetSetting(ctx, agentEnabledKey, `["a","b"]`, revision); err != nil {
		t.Fatalf("update at the current revision: %v", err)
	}
	// And an update at the STALE revision refuses.
	if err := repo.SetSetting(ctx, agentEnabledKey, `["c"]`, revision); err == nil {
		t.Fatal("a stale-revision update was accepted")
	}
}
