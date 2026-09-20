package memory

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
)

// These tests cover the two invariants of DOMAIN_MODEL section 14.5 that a recall
// implementation would violate silently, and the scope isolation section 14.4
// requires to be structural rather than prefix-based.

// memoryStore holds messages per scope, keyed by the scope's parts.
type memoryStore struct {
	items map[string][]Item
	// seen records what scopes were asked for, so a test can assert the isolation.
	seen [][6]string
}

func newMemoryStore() *memoryStore {
	return &memoryStore{items: map[string][]Item{}}
}

func scopeKeyOf(parts [6]string) string { return strings.Join(parts[:], "\x00") }

func (s *memoryStore) seed(parts [6]string, items ...Item) {
	key := scopeKeyOf(parts)
	s.items[key] = append(s.items[key], items...)
}

func (s *memoryStore) RecentMessages(_ context.Context, parts [6]string, exclude string, limit int) ([]Item, error) {
	s.seen = append(s.seen, parts)
	items := s.items[scopeKeyOf(parts)]
	out := make([]Item, 0, len(items))
	// Newest first, which is the store's contract.
	for index := len(items) - 1; index >= 0; index-- {
		item := items[index]
		if item.MessageID == exclude {
			continue
		}
		out = append(out, item)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

// partsFor builds the scope parts a request would produce.
func partsFor(projectID, agentKey string) [6]string {
	scope := ScopeFor(projectID, "", agentKey)
	return scope.Parts()
}

func item(id, content string, role agent.MessageRole, at time.Time) Item {
	return Item{MessageID: id, Content: content, Role: role, Provenance: "run-" + id, CreatedAt: at}
}

// TestBuildContextExcludesTheCurrentMessage is section 14.5's first invariant:
// 当前消息不召回自身.
//
// It matters because section 12.2's order is "recall previous memory excluding
// current turn": a turn that recalled itself would show a model its own question as
// though it were history, and a model answering its own question is a different
// task from the one it was given.
func TestBuildContextExcludesTheCurrentMessage(t *testing.T) {
	store := newMemoryStore()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	parts := partsFor("project-1", "script.decision")
	store.seed(parts,
		item("m1", "the first thing", agent.MessageUser, now),
		item("m2", "the reply", agent.MessageAssistant, now),
		item("m3", "the current question", agent.MessageUser, now),
	)
	service := New(store)
	items, err := service.BuildContext(context.Background(), RecallRequest{
		Scope:            ScopeFor("project-1", "", "script.decision"),
		ExcludeMessageID: "m3",
	})
	if err != nil {
		t.Fatalf("BuildContext: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("the recall returned %d items, want 2: %+v", len(items), items)
	}
	for _, recalled := range items {
		if recalled.MessageID == "m3" {
			t.Fatal("the recall returned the message it was answering")
		}
	}
	// Oldest first, which is how a prompt reads a conversation.
	if items[0].MessageID != "m1" || items[1].MessageID != "m2" {
		t.Fatalf("the recall is not oldest first: %+v", items)
	}
	// The provenance travels with each item, because section 12.2 requires recalled
	// context to say where it came from.
	for _, recalled := range items {
		if recalled.Provenance == "" {
			t.Fatalf("a recalled item carries no provenance: %+v", recalled)
		}
	}
}

// TestBuildContextCannotRecallAnotherProject is section 14.5's second invariant:
// 其他项目内容不可召回.
//
// The store is asked by the scope's PARTS rather than by a prefix, so the isolation
// is structural. The test seeds two projects whose identifiers share a prefix —
// which is exactly the case a prefix-matching store would leak — and asserts the
// recall never sees the other one.
func TestBuildContextCannotRecallAnotherProject(t *testing.T) {
	store := newMemoryStore()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store.seed(partsFor("project-1", "script.decision"),
		item("a1", "project one's memory", agent.MessageUser, now))
	// A project whose identifier BEGINS with the other's.
	store.seed(partsFor("project-10", "script.decision"),
		item("b1", "project ten's memory", agent.MessageUser, now))

	service := New(store)
	items, err := service.BuildContext(context.Background(), RecallRequest{
		Scope: ScopeFor("project-1", "", "script.decision"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].MessageID != "a1" {
		t.Fatalf("the recall returned %+v, want only project one's message", items)
	}
	// The store was asked for the exact parts, which is what makes the isolation
	// structural rather than a property of this store's implementation.
	if len(store.seen) != 1 {
		t.Fatalf("the store was asked %d times", len(store.seen))
	}
	if store.seen[0][2] != "project-1" {
		t.Fatalf("the store was asked for project %q", store.seen[0][2])
	}
	// The scope key separates them too, in case anything downstream uses it.
	if ScopeFor("project-1", "", "script.decision").Key() == ScopeFor("project-10", "", "script.decision").Key() {
		t.Fatal("two projects produced the same scope key")
	}
	// And a different AGENT in the same project is a different scope, so a
	// supervisor does not recall a decision agent's conversation as its own.
	if ScopeFor("project-1", "", "script.decision").Key() == ScopeFor("project-1", "", "script.supervision").Key() {
		t.Fatal("two agents produced the same scope key")
	}
}

// TestScopeIsValidatedBeforeItIsUsed covers the fail-closed check: a scope with no
// project would recall across projects, so it is refused rather than defaulted.
func TestScopeIsValidatedBeforeItIsUsed(t *testing.T) {
	store := newMemoryStore()
	service := New(store)
	_, err := service.BuildContext(context.Background(), RecallRequest{Scope: Scope{AgentKey: "script.decision"}})
	if err == nil {
		t.Fatal("a scope with no project was used to recall")
	}
	var memoryErr *Error
	if !errors.As(err, &memoryErr) {
		t.Fatalf("the refusal is a %T, want a memory error", err)
	}
	if len(store.seen) != 0 {
		t.Fatal("the store was asked despite the scope being refused")
	}
	// A control character is refused too, because it would let one scope render to
	// the same key as another.
	if err := (Scope{Project: "p\x00q"}).Validate(); err == nil {
		t.Fatal("a scope containing a control character was accepted")
	}
	// A complete scope is accepted, and the tenant is defaulted by ScopeFor rather
	// than left empty.
	good := ScopeFor("project-1", "episode-1", "script.decision")
	if err := good.Validate(); err != nil {
		t.Fatalf("a well-formed scope was refused: %v", err)
	}
	if good.Tenant != "local" {
		t.Fatalf("ScopeFor left the tenant as %q, want the local build's value", good.Tenant)
	}
}

// TestBuildContextBoundsTheRecall covers the window: a caller cannot ask for an
// unbounded amount, because section 5.3 puts memory seventh of nine priorities and
// a large recall would crowd out the layers above it.
func TestBuildContextBoundsTheRecall(t *testing.T) {
	store := newMemoryStore()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	parts := partsFor("project-1", "script.decision")
	for index := 0; index < MaxLimit+50; index++ {
		store.seed(parts, item("m"+itoa(index), "message", agent.MessageUser, now))
	}
	service := New(store)
	// A zero limit uses the default.
	items, err := service.BuildContext(context.Background(), RecallRequest{Scope: ScopeFor("project-1", "", "script.decision")})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != DefaultLimit {
		t.Fatalf("a default recall returned %d items, want %d", len(items), DefaultLimit)
	}
	// A limit above the ceiling is clamped rather than honoured.
	items, err = service.BuildContext(context.Background(), RecallRequest{
		Scope: ScopeFor("project-1", "", "script.decision"), Limit: MaxLimit * 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != MaxLimit {
		t.Fatalf("an oversized recall returned %d items, want the ceiling %d", len(items), MaxLimit)
	}
	// A small limit is honoured.
	items, err = service.BuildContext(context.Background(), RecallRequest{
		Scope: ScopeFor("project-1", "", "script.decision"), Limit: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("a recall of 3 returned %d items", len(items))
	}
}

// TestBuildContextFailsClosedWhenUnattached covers the composition case.
func TestBuildContextFailsClosedWhenUnattached(t *testing.T) {
	service := New(nil)
	if service.Available() {
		t.Fatal("a service with no store reports itself available")
	}
	if _, err := service.BuildContext(context.Background(), RecallRequest{Scope: ScopeFor("p", "", "a")}); err == nil {
		t.Fatal("a service with no store recalled something")
	}
}

// TestRecallFailureIsReportedRatherThanSwallowed covers the choice this package
// deliberately does not make: a caller that cannot recall may continue without
// memory or stop, and that is the caller's decision.
func TestRecallFailureIsReportedRatherThanSwallowed(t *testing.T) {
	service := New(failingStore{})
	_, err := service.BuildContext(context.Background(), RecallRequest{Scope: ScopeFor("p", "", "a")})
	if err == nil {
		t.Fatal("a failing recall reported success")
	}
	// The message is a sentence rather than a code, because it is what a user would
	// read; the assertion is that the failure came back at all and is recognisable as
	// this package's.
	var memoryErr *Error
	if !errors.As(err, &memoryErr) {
		t.Fatalf("the failure is a %T, want a memory error", err)
	}
	if !strings.Contains(memoryErr.SafeMessage, "memory store") {
		t.Fatalf("the failure reads %q, which does not say what is missing", memoryErr.SafeMessage)
	}
}

type failingStore struct{}

func (failingStore) RecentMessages(context.Context, [6]string, string, int) ([]Item, error) {
	return nil, UnavailableError()
}

// itoa renders a small non-negative integer.
func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}
