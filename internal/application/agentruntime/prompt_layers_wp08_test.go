package agentruntime

import (
	"context"
	"strings"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
)

// These tests cover the two layers WP-08 added to the prompt: what a user pinned, and the findings a
// FIX must address.
//
// Both are TRUSTED — they come from the version table's lock rows and from a user's gate decision —
// so the property that matters is that they render OUTSIDE the untrusted boundary. A lock rendered
// inside it would be a prohibition the model was told it could ignore.

// TestPromptRendersLockedRefsAsATrustedLayer covers section 7.3's lockedRefs.
func TestPromptRendersLockedRefsAsATrustedLayer(t *testing.T) {
	prompt := Assemble(AssembleRequest{
		Spec: validSpec("script.execution.story_skeleton", agent.LayerExecution, "story.read_events"),
		Task: "Write the skeleton.",
		LockedRefs: []LockedRef{
			{EntityType: "story_skeleton_version", EntityID: "v1", Field: "endingHook", Label: "the hook the user wrote"},
			{EntityType: "scene", EntityID: "scene-1", Label: "the pinned opening scene"},
		},
	})
	var layer Message
	for _, message := range prompt.Messages {
		if message.Region == RegionLockedRefs {
			layer = message
		}
	}
	if layer.Region == "" {
		t.Fatalf("no locked-refs layer was rendered: %+v", prompt.Messages)
	}
	// Both refs are named, with the field where there is one.
	if !strings.Contains(layer.Content, "endingHook") || !strings.Contains(layer.Content, "the hook the user wrote") {
		t.Fatalf("the first ref is missing from %q", layer.Content)
	}
	if !strings.Contains(layer.Content, "scene-1") {
		t.Fatalf("the second ref is missing from %q", layer.Content)
	}
	// TRUSTED: the layer must not sit inside the untrusted boundary. A model told its prohibitions are
	// document text would be told they are data it may disregard.
	if strings.Contains(layer.Content, UntrustedOpen) {
		t.Fatalf("the pinned refs were rendered as untrusted content: %q", layer.Content)
	}
	if layer.Untrusted {
		t.Fatal("the pinned refs were marked untrusted")
	}
	// And it must come BEFORE the untrusted task, because a rule stated after the material it governs
	// reads as an afterthought.
	var lockedIndex, taskIndex int
	for index, message := range prompt.Messages {
		switch message.Region {
		case RegionLockedRefs:
			lockedIndex = index
		case RegionTaskInput:
			taskIndex = index
		}
	}
	if lockedIndex > taskIndex {
		t.Fatalf("the pinned refs render at %d, after the task at %d", lockedIndex, taskIndex)
	}
}

// TestPromptOmitsEmptyLockedRefsAndIssues covers the omission rule.
//
// An empty layer is omitted rather than rendered blank, because a blank heading suggests the layer
// exists and says nothing — the assembler's existing rule for every other layer.
func TestPromptOmitsEmptyLockedRefsAndIssues(t *testing.T) {
	prompt := Assemble(AssembleRequest{
		Spec: validSpec("script.execution.story_skeleton", agent.LayerExecution, "story.read_events"),
		Task: "Write the skeleton.",
	})
	for _, message := range prompt.Messages {
		if message.Region == RegionLockedRefs || message.Region == RegionFixIssues {
			t.Fatalf("an empty layer was rendered: %+v", message)
		}
	}
}

// TestPromptRendersFixIssuesAsItsOwnLayer covers section 7.3's fixIssueIds.
//
// It is a separate layer from the locks because a prohibition and a request are different
// instructions: a model told only what NOT to change would not know what to change.
func TestPromptRendersFixIssuesAsItsOwnLayer(t *testing.T) {
	prompt := Assemble(AssembleRequest{
		Spec: validSpec("script.execution.story_skeleton", agent.LayerExecution, "story.read_events"),
		Task: "Write the skeleton.",
		LockedRefs: []LockedRef{
			{EntityType: "story_skeleton_version", EntityID: "v1", Field: "climax"},
		},
		FixIssueIDs: []string{"issue-1", "issue-2"},
	})
	var locked, issues Message
	for _, message := range prompt.Messages {
		switch message.Region {
		case RegionLockedRefs:
			locked = message
		case RegionFixIssues:
			issues = message
		}
	}
	if issues.Region == "" {
		t.Fatal("no fix-issues layer was rendered")
	}
	for _, id := range []string{"issue-1", "issue-2"} {
		if !strings.Contains(issues.Content, id) {
			t.Fatalf("the issue %s is missing from %q", id, issues.Content)
		}
	}
	// Both layers exist and are distinct, so a model sees the prohibition and the request separately.
	if locked.Content == issues.Content {
		t.Fatal("the two layers render the same content")
	}
	// And the fixes are trusted for the same reason the locks are: a user's decision, not a document.
	if strings.Contains(issues.Content, UntrustedOpen) || issues.Untrusted {
		t.Fatal("the review findings were rendered as untrusted content")
	}
}

// TestLockedRefLabelIsBounded covers the ceiling on a label.
//
// The label is a user's own words from a decision record, so it is trusted — but a layer that can grow
// without bound is a layer that can crowd out the tool contract, and section 5.3 puts that contract
// above everything.
func TestLockedRefLabelIsBounded(t *testing.T) {
	long := strings.Repeat("测", MaxLockedRefLabelRunes*2)
	prompt := Assemble(AssembleRequest{
		Spec:       validSpec("script.execution.story_skeleton", agent.LayerExecution, "story.read_events"),
		Task:       "Write the skeleton.",
		LockedRefs: []LockedRef{{EntityType: "scene", EntityID: "s1", Label: long}},
	})
	var layer Message
	for _, message := range prompt.Messages {
		if message.Region == RegionLockedRefs {
			layer = message
		}
	}
	if len([]rune(layer.Content)) > MaxLockedRefLabelRunes*2 {
		t.Fatalf("the layer is %d runes for a bounded label", len([]rune(layer.Content)))
	}
	if !strings.Contains(layer.Content, "…") {
		t.Fatal("the label was not marked as truncated")
	}
}

// TestInvocationCarriesTheLocksIntoThePrompt is the wiring assertion: a field on Invocation that the
// runner forgot to pass would leave the layer unreachable, and only a test through Run can see that.
func TestInvocationCarriesTheLocksIntoThePrompt(t *testing.T) {
	harness := newHarness(t, []string{`{"summary":"x"}`}, nil)
	invocation := invocationFor("script.execution.x")
	invocation.LockedRefs = []LockedRef{
		{EntityType: "story_skeleton_version", EntityID: "v1", Field: "endingHook", Label: "pinned"},
	}
	invocation.FixIssueIDs = []string{"issue-9"}
	if _, err := harness.runtime.Run(context.Background(), invocation); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// What the model received is read from the harness's own record of the request, so this asserts
	// the PROMPT rather than the invocation.
	var sawLock, sawIssue bool
	for _, call := range harness.model.calls {
		for _, message := range call.Messages {
			if strings.Contains(message.Content, "endingHook") && strings.Contains(message.Content, "pinned") {
				sawLock = true
			}
			if strings.Contains(message.Content, "issue-9") {
				sawIssue = true
			}
		}
	}
	if !sawLock {
		t.Fatal("the pinned ref never reached the prompt")
	}
	if !sawIssue {
		t.Fatal("the review finding never reached the prompt")
	}
}
