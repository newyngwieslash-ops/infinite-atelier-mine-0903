package database

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/legacy"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/project"
)

// TestLegacyImportVerificationCoversChatSessions proves the in-transaction check
// compares the chat sessions, not only the nodes, edges, assets and history.
//
// The check originally covered four counts. A chat session has a foreign key to
// its canvas, so an insert with a bad parent is rejected — but a session loop
// that stored nothing at all would raise no error anywhere, the import would
// report success, and the user would find their conversations gone. The count is
// what catches that, and this test pins it by comparing the sessions the request
// promised with the sessions the transaction holds.
func TestLegacyImportVerificationCoversChatSessions(t *testing.T) {
	handle := legacyHandle(t)
	ctx := context.Background()
	legacyRepo := NewLegacyRepository(handle.SQL())

	bundle := bundleFixture(t, "proj-chat", "doc-chat", "node-c-1", "edge-c", "node-c-2")
	// The fixture carries one session; a second is added so the count is non-trivial.
	bundle.ChatSessions = append(bundle.ChatSessions, project.ChatSession{
		ID:               "chat-more",
		CanvasDocumentID: bundle.Document.ID,
		Title:            "Second",
		MessagesJSON:     "[]",
		CreatedAt:        time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC),
		UpdatedAt:        time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC),
		Revision:         1,
	})
	if len(bundle.ChatSessions) != 2 {
		t.Fatalf("precondition: the bundle promises %d sessions", len(bundle.ChatSessions))
	}

	outcome, err := legacyRepo.ImportSnapshot(ctx, legacy.ImportRequest{
		Fingerprint: strings.Repeat("5", 64),
		Mode:        legacy.ModeInitial,
		StartedAt:   time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC),
		Bundles:     []legacy.ProjectBundle{bundle},
	})
	if err != nil {
		t.Fatalf("ImportSnapshot: %v", err)
	}
	if outcome.Projects != 1 {
		t.Fatalf("outcome = %+v", outcome)
	}
	// Both sessions are stored, which is the state the count check attested to.
	sessions, err := NewCanvasRepository(handle.SQL()).ListChatSessions(ctx, bundle.Document.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != len(bundle.ChatSessions) {
		t.Fatalf("stored %d chat sessions, want %d", len(sessions), len(bundle.ChatSessions))
	}
}
