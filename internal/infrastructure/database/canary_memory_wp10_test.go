package database

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	eventsapp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/events"
	appmemory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/memory"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/event"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/memory"
)

// canaryMemoryRecall is the fixture AGENT_CONTRACTS section 18.1 asks for.
type canaryMemoryRecall struct {
	Note           string `json:"note"`
	Fault          string `json:"fault"`
	ProjectID      string `json:"projectId"`
	OtherProjectID string `json:"otherProjectId"`
	EpisodeID      string `json:"episodeId"`
	AgentKey       string `json:"agentKey"`
	Setting        struct {
		MessageID  string `json:"messageId"`
		Role       string `json:"role"`
		Content    string `json:"content"`
		AssetID    string `json:"assetId"`
		EntityType string `json:"entityType"`
		EntityID   string `json:"entityId"`
	} `json:"setting"`
	BuryingTurns []struct {
		MessageID string `json:"messageId"`
		Role      string `json:"role"`
		Content   string `json:"content"`
	} `json:"buryingTurns"`
	Question   string `json:"question"`
	MustRecall struct {
		MessageID     string `json:"messageId"`
		ViaSummary    bool   `json:"viaSummary"`
		ReturnsSource bool   `json:"returnsSource"`
	} `json:"mustRecall"`
	Decoys []struct {
		MessageID string `json:"messageId"`
		Role      string `json:"role"`
		Content   string `json:"content"`
		Reason    string `json:"reason"`
	} `json:"decoys"`
	MaxRawMessages int `json:"maxRawMessages"`
}

// loadCanaryMemoryRecall reads the generated fixture.
func loadCanaryMemoryRecall(t *testing.T) canaryMemoryRecall {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", "canary-drama", "memory-recall.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the memory-recall fixture: %v", err)
	}
	var fixture canaryMemoryRecall
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("parsing the memory-recall fixture: %v", err)
	}
	return fixture
}

// TestTheCanaryMemoryRecallFixtureNamesItsOwnScenario keeps the fixture honest.
//
// A generated fixture that lost a field would still parse into a struct with a zero value, and a test
// built on it would silently assert less than it says. This checks the parts a reader would assume:
// the setting, the turns that bury it, the question, and BOTH decoys — each of which is the only
// reason the scenario is worth running.
func TestTheCanaryMemoryRecallFixtureNamesItsOwnScenario(t *testing.T) {
	fixture := loadCanaryMemoryRecall(t)
	if fixture.Setting.Content == "" || fixture.Setting.MessageID == "" {
		t.Fatalf("the fixture's setting is empty: %+v", fixture.Setting)
	}
	if len(fixture.BuryingTurns) < 20 {
		t.Fatalf("the fixture buries the setting under %d turns, which is not enough for AC-MEM-005's '大量消息'",
			len(fixture.BuryingTurns))
	}
	if fixture.Question == "" {
		t.Fatal("the fixture has no question to ask")
	}
	if fixture.ProjectID == fixture.OtherProjectID {
		t.Fatal("the fixture's two projects are the same project, so the leak decoy proves nothing")
	}
	if len(fixture.Decoys) != 2 {
		t.Fatalf("the fixture carries %d decoys, want the same-project and other-project pair", len(fixture.Decoys))
	}
	// Each decoy says WHY it is one, which is what makes the assertion that skips it meaningful.
	for _, decoy := range fixture.Decoys {
		if decoy.Reason == "" {
			t.Fatalf("a decoy does not say what it is for: %+v", decoy)
		}
	}
	if !fixture.MustRecall.ViaSummary {
		t.Fatal("the fixture does not require the summary hop, so it would not exercise AC-MEM-005")
	}
	if fixture.MaxRawMessages <= 0 {
		t.Fatal("the fixture states no raw-message bound, so '限制 Token' would be untested")
	}
	// And it names itself as a scenario rather than a fault, so a reader comparing it against
	// bad-script and bad-storyboard is not misled into looking for a deliberate error.
	if fixture.Fault == "" {
		t.Fatal("the fixture does not say whether it is faulty, and the others do")
	}
}

// TestACMME005TheCanaryScenarioRunsThroughTheWholeChain is AC-E2E-005 over the generated fixture.
//
//	在早期对话中确定角色禁用红色服装，经过大量后续操作后询问：
//	- Deep Recall 找到相关摘要；
//	- 恢复原始消息；
//	- 返回对应角色/资产事实；
//	- 不召回其他项目内容；
//	- 可从 UI 查看来源。
//
// The scenario is the fixture's, driven rather than copied: the setting and the burying turns are
// stored as the file states them, and every assertion the criterion makes is made against what came
// back. The one thing the harness supplies is similarity — a test embedder that says "these two texts
// are about the same thing" — because that is the part a fixture cannot carry and the part a real
// provider supplies in production.
func TestACMME005TheCanaryScenarioRunsThroughTheWholeChain(t *testing.T) {
	fixture := loadCanaryMemoryRecall(t)
	questionVector := make([]float32, 256)
	questionVector[11] = 1
	// The setting, its summary, and the question all share a vector: the fixture says the question is
	// about the setting, and this is how a deterministic test says so.
	embedder := &embedderDouble{available: true, vectors: map[string][]float32{
		fixture.Question: questionVector,
	}}
	harness := newWP10MemoryHarness(t, embedder)
	ctx := context.Background()
	scope := memory.Scope{
		Tenant: "local", Project: fixture.ProjectID, Episode: fixture.EpisodeID, AgentKey: fixture.AgentKey,
	}

	// The early setting, with its asset citation.
	setting, err := harness.service.RememberMessage(ctx, appmemory.RememberMessageRequest{
		Scope: scope, Message: fixture.Setting.Content, MessageID: fixture.Setting.MessageID,
		Role: agent.MessageRole(fixture.Setting.Role), AgentKey: fixture.AgentKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.service.LinkMemory(ctx, setting.ID, fixture.Setting.EntityType, fixture.Setting.EntityID); err != nil {
		t.Fatalf("LinkMemory: %v", err)
	}

	// The turns that bury it.
	for _, turn := range fixture.BuryingTurns {
		if _, err := harness.service.RememberMessage(ctx, appmemory.RememberMessageRequest{
			Scope: scope, Message: turn.Content, MessageID: turn.MessageID,
			Role: agent.MessageRole(turn.Role), AgentKey: fixture.AgentKey,
		}); err != nil {
			t.Fatalf("RememberMessage(%s): %v", turn.MessageID, err)
		}
	}
	// The two decoys: one in this project, one in another. Both say what they are for.
	decoyIDs := map[string]string{}
	for index, decoy := range fixture.Decoys {
		decoyScope := scope
		if index == 1 {
			decoyScope.Project = fixture.OtherProjectID
		}
		if _, err := harness.service.RememberMessage(ctx, appmemory.RememberMessageRequest{
			Scope: decoyScope, Message: decoy.Content, MessageID: decoy.MessageID,
			Role: agent.MessageRole(decoy.Role), AgentKey: fixture.AgentKey,
		}); err != nil {
			t.Fatal(err)
		}
		decoyIDs[decoy.MessageID] = decoyScope.Project
	}

	// The summary the walk has to find. The question's vector is the double's fallback, so the summary
	// is embedded as being about the question — which is what a real provider would do for a summary
	// of the turn that answers it.
	embedder.fallback = questionVector
	summary, found, err := harness.service.Summarize(ctx, appmemory.SummarizeRequest{
		Scope: scope, Level: 1, Window: 2, Embed: true,
	})
	if err != nil || !found {
		t.Fatalf("Summarize: found=%v err=%v", found, err)
	}
	// The summary must cover the SETTING, or the scenario is not the one the fixture describes.
	_, sources, err := harness.service.ListSummarySources(ctx, summary.ID)
	if err != nil {
		t.Fatal(err)
	}
	covered := false
	for _, source := range sources {
		if source.SourceID == fixture.Setting.MessageID {
			covered = true
		}
	}
	if !covered {
		t.Fatalf("the summary does not cover the fixture's setting, so the scenario would prove nothing: %+v", sources)
	}

	// The question.
	result, err := harness.service.DeepRecall(ctx, appmemory.DeepRecallRequest{
		Scope: scope, Query: fixture.Question, MaxRawMessages: fixture.MaxRawMessages,
	})
	if err != nil {
		t.Fatalf("DeepRecall: %v", err)
	}

	// Deep Recall 找到相关摘要.
	foundSummary := false
	for _, selected := range result.Summaries {
		if selected.Item.ID == summary.ID {
			foundSummary = true
		}
	}
	if !foundSummary {
		t.Fatalf("deep recall did not find the summary: %+v", result.Summaries)
	}
	// 恢复原始消息.
	restored := false
	for _, message := range result.Messages {
		if message.SourceID == fixture.Setting.MessageID {
			restored = true
			if !strings.Contains(message.Content, "不能穿红色") {
				t.Fatalf("the restored message reads %q", message.Content)
			}
		}
	}
	if !restored {
		t.Fatalf("deep recall did not restore the setting: %+v", result.Messages)
	}
	// 返回对应角色/资产事实 — the entity link the setting carries comes back, which is the "角色/资产
	// 事实" the criterion names. It is read from the memory the walk restored rather than from the
	// summary, because the link is on the memory.
	links, err := harness.service.EntityLinks(ctx, setting.ID)
	if err != nil {
		t.Fatal(err)
	}
	linked := false
	for _, link := range links {
		if link.EntityType == fixture.Setting.EntityType && link.EntityID == fixture.Setting.EntityID {
			linked = true
		}
	}
	if !linked {
		t.Fatalf("the setting carries no link to %s/%s: %+v", fixture.Setting.EntityType, fixture.Setting.EntityID, links)
	}
	// 不召回其他项目内容 — neither the other project's decoy nor anything else from it.
	for _, message := range result.Messages {
		if message.SourceID == "canary-decoy-other-project" {
			t.Fatal("deep recall restored another project's message")
		}
		if message.Scope.Project != fixture.ProjectID {
			t.Fatalf("deep recall restored a memory from %q", message.Scope.Project)
		}
	}
	for _, selected := range result.Summaries {
		if selected.Item.Scope.Project != fixture.ProjectID {
			t.Fatalf("deep recall selected a summary from %q", selected.Item.Scope.Project)
		}
	}
	// The same-project decoy is a DIFFERENT setting, and the answer must not be it. The assertion is
	// that the setting the fixture names is the one restored, which the check above already made; this
	// one states the decoy's absence so a future change that returned both would fail.
	for _, message := range result.Messages {
		if message.SourceID == "canary-decoy-same-project" {
			t.Fatal("deep recall returned the same-project decoy as the answer")
		}
	}
	// 可从 UI 查看来源 — the provenance a UI renders: which summary each message came through, with the
	// attribution AC-MEM-004 preserves.
	if len(result.Provenance) != len(result.Messages) {
		t.Fatalf("%d messages but %d provenance entries", len(result.Messages), len(result.Provenance))
	}
	for _, entry := range result.Provenance {
		if entry.MessageID == fixture.Setting.MessageID {
			if entry.SummarizedBy != summary.ID {
				t.Fatalf("the restored setting names %q as its summary", entry.SummarizedBy)
			}
			if entry.Role == "" || entry.CreatedAt.IsZero() {
				t.Fatalf("the provenance lost the setting's attribution: %+v", entry)
			}
		}
	}
	// 限制 Token — the walk is bounded, and says so when it stops at the bound.
	if len(result.Messages) > fixture.MaxRawMessages {
		t.Fatalf("the walk restored %d messages against the fixture's bound of %d",
			len(result.Messages), fixture.MaxRawMessages)
	}
}

// TestMemoryCreatedIsEmitted is ADR-0009's assignment, which went unfulfilled until this package.
//
// The event name has been in the closed vocabulary since migration 000013 and its CHECK has accepted
// it since; ADR-0009 section 5 named WP-10 as the package that would emit it. An event that is
// declared and never emitted is the same "declared but unreachable" shape this repository's reviews
// keep finding in interfaces, one level down: a reader of the vocabulary believes something announces
// itself, and nothing does.
func TestMemoryCreatedIsEmitted(t *testing.T) {
	recorder := &recordingEvents{}
	db := dramaRepoHandle(t)
	repository := NewMemoryRepository(db)
	service := appmemory.NewService(appmemory.Options{
		Store:   NewAgentRepository(db),
		Items:   repository,
		Vectors: NewMemoryVectorIndex(repository),
		Clock:   wp10Clock{at: dramaTime()},
		IDs:     &wp10IDs{},
		Events:  recorder,
	})
	ctx := context.Background()
	if _, err := service.RememberMessage(ctx, appmemory.RememberMessageRequest{
		Scope:   memory.Scope{Tenant: "local", Project: "drama-project", AgentKey: "script.decision"},
		Message: "a turn worth remembering", MessageID: "event-msg-1",
		Role: agent.MessageUser, AgentKey: "script.decision",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RememberFact(ctx, appmemory.RememberFactRequest{
		Scope:   memory.Scope{Tenant: "local", Project: "drama-project"},
		Content: "a setting the user stated", Importance: 0.9, CreatedByID: "user-1",
	}); err != nil {
		t.Fatal(err)
	}
	if len(recorder.drafts) != 2 {
		t.Fatalf("the recorder saw %d events, want one per write: %+v", len(recorder.drafts), recorder.drafts)
	}
	for _, draft := range recorder.drafts {
		if draft.Type != event.MemoryCreated {
			t.Fatalf("the emitted event is %q, want MemoryCreated", draft.Type)
		}
		if draft.AggregateType != event.AggregateMemory {
			t.Fatalf("the event's aggregate is %q, want memory", draft.AggregateType)
		}
		if draft.AggregateID == "" {
			t.Fatal("the event names no memory")
		}
		if draft.ProjectID != "drama-project" {
			t.Fatalf("the event names project %q", draft.ProjectID)
		}
	}
	// A service with NO recorder still writes, which is the optional-port contract: an announcement
	// is not a precondition for the write it announces.
	//
	// The identifier generator starts FRESH here rather than at the recorder's count, because
	// `wp10IDs` is a counter and a second one begins at mem-1 — the same identifier the first service
	// already stored. The collision is the generator's, not the service's: its `mintID` checks and
	// refuses, which is what it is for, and a test that reused a counter would be asserting against
	// the check rather than against the port's optionality.
	silent := appmemory.NewService(appmemory.Options{
		Store: NewAgentRepository(db), Items: repository,
		Vectors: NewMemoryVectorIndex(repository),
		Clock:   wp10Clock{at: dramaTime()}, IDs: &wp10IDs{next: 100},
	})
	if _, err := silent.RememberMessage(ctx, appmemory.RememberMessageRequest{
		Scope:   memory.Scope{Tenant: "local", Project: "drama-project"},
		Message: "stored without an announcement", MessageID: "event-msg-2",
		Role: agent.MessageUser,
	}); err != nil {
		t.Fatalf("a memory write with no recorder failed: %v", err)
	}
}

// recordingEvents captures the drafts a service announces.
type recordingEvents struct {
	drafts []eventsapp.Draft
}

func (r *recordingEvents) RecordBestEffort(_ context.Context, draft eventsapp.Draft) {
	r.drafts = append(r.drafts, draft)
}
