package database

import (
	"context"
	"database/sql"
	"testing"

	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/media"
)

// character_voices_wp27_test.go grades the migration and the repository that stores FR-080's
// 多角色声线映射.
//
// # Why the migration is asserted rather than assumed
//
// `migrate_wp05_test.go` names the trap in its own comment: the migration LIST and the head CONSTANT
// move together, and forgetting one is silent — a migration absent from the list is one every test
// database does not have, so a test asserting a table exists passes only if the table was added by
// hand. These tests read the table through the real migration set, which is what makes that trap
// observable rather than latent.

// voiceHarness builds a database at the migration head with a project and two characters to cast.
//
// The handle is returned as well, because one test deletes a character to grade the foreign key's
// cascade — and it must use the SAME handle the repository does, so it sees the same connection.
func voiceHarness(t *testing.T) (*VoiceRepository, *sql.DB, context.Context, string, string) {
	t.Helper()
	db := dramaRepoHandle(t)
	dramaSeedParents(t, db)
	// Two characters, so "the mapping is per character" is a property with something to compare.
	if _, err := db.ExecContext(context.Background(), `INSERT INTO story_entities
		(id, project_id, entity_type, canonical_name, status, source_scope, created_at, updated_at, revision)
		VALUES ('voice-char-a', 'drama-project', 'character', '阿澈', 'accepted', 'user',
		 '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO story_entities
		(id, project_id, entity_type, canonical_name, status, source_scope, created_at, updated_at, revision)
		VALUES ('voice-char-b', 'drama-project', 'character', '念念', 'accepted', 'user',
		 '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`); err != nil {
		t.Fatal(err)
	}
	return NewVoiceRepository(db), db, context.Background(), "drama-project", "voice-char-a"
}

// TestTheMigrationCreatesTheVoiceTable is the head-and-list assertion.
func TestTheMigrationCreatesTheVoiceTable(t *testing.T) {
	repository, _, ctx, projectID, characterID := voiceHarness(t)
	if _, err := repository.AssignVoice(ctx, media.CharacterVoice{
		ID: "voice-1", ProjectID: projectID, CharacterEntityID: characterID, Voice: "alloy", Revision: 1,
	}, 0); err != nil {
		t.Fatalf("the table the migration creates is not usable: %v", err)
	}
}

// TestAssigningTwiceUpdatesOneRow is what makes a mapping a CAST rather than a log.
//
// The UNIQUE index is the mechanism and this asserts its consequence: a character has one voice, and
// assigning a second one replaces the first instead of leaving the table with two answers a reader
// would have to choose between.
func TestAssigningTwiceUpdatesOneRow(t *testing.T) {
	repository, _, ctx, projectID, characterID := voiceHarness(t)

	first, err := repository.AssignVoice(ctx, media.CharacterVoice{
		ID: "voice-1", ProjectID: projectID, CharacterEntityID: characterID,
		ProviderConfigID: "chan-1", Model: "tts-1", Voice: "alloy", Revision: 1,
	}, 0)
	if err != nil {
		t.Fatalf("the first assignment: %v", err)
	}
	if first.Revision != 1 {
		t.Fatalf("a new mapping has revision %d", first.Revision)
	}
	// The second assignment names the revision it saw, which is the guard the store checks.
	second, err := repository.AssignVoice(ctx, media.CharacterVoice{
		ID: "voice-1", ProjectID: projectID, CharacterEntityID: characterID, Voice: "nova", Revision: 1,
	}, first.Revision)
	if err != nil {
		t.Fatalf("the second assignment: %v", err)
	}
	if second.Voice != "nova" {
		t.Fatalf("the stored voice is %q", second.Voice)
	}
	if second.Revision != 2 {
		t.Fatalf("the updated mapping has revision %d", second.Revision)
	}
	// ONE row, and the provider/model are CLEARED by stating nothing: the second assignment is the
	// whole mapping, not a patch. A merge would leave a voice from one channel paired with another.
	listed, err := repository.ListVoices(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("%d mappings for one character", len(listed))
	}
	if listed[0].Voice != "nova" || listed[0].ProviderConfigID != "" || listed[0].Model != "" {
		t.Fatalf("the stored mapping is %+v", listed[0])
	}
}

// TestAStaleAssignmentIsRefused is the revision guard.
//
// A casting panel that saved from a stale tab must not silently overwrite a change somebody else made.
// The refusal is a CONFLICT rather than a storage fault, because the caller's next step is to reload
// and look — not to report a broken store.
func TestAStaleAssignmentIsRefused(t *testing.T) {
	repository, _, ctx, projectID, characterID := voiceHarness(t)
	if _, err := repository.AssignVoice(ctx, media.CharacterVoice{
		ID: "voice-1", ProjectID: projectID, CharacterEntityID: characterID, Voice: "alloy", Revision: 1,
	}, 0); err != nil {
		t.Fatal(err)
	}
	// Revision 7 is one nobody saw.
	_, err := repository.AssignVoice(ctx, media.CharacterVoice{
		ID: "voice-1", ProjectID: projectID, CharacterEntityID: characterID, Voice: "nova", Revision: 1,
	}, 7)
	if err == nil {
		t.Fatal("a stale assignment was accepted")
	}
	if domainErr, ok := media.AsError(err); !ok || domainErr.Category != media.CategoryConflict {
		t.Fatalf("a stale assignment failed as %v rather than a conflict", err)
	}
	// And the stored value is UNCHANGED, which is the point of refusing.
	stored, found, err := repository.GetVoice(ctx, projectID, characterID)
	if err != nil || !found {
		t.Fatalf("reading back: found=%v err=%v", found, err)
	}
	if stored.Voice != "alloy" {
		t.Fatalf("a refused assignment changed the voice to %q", stored.Voice)
	}
}

// TestCharactersAreCastIndependently is the multi-character requirement itself.
func TestCharactersAreCastIndependently(t *testing.T) {
	repository, _, ctx, projectID, first := voiceHarness(t)
	if _, err := repository.AssignVoice(ctx, media.CharacterVoice{
		ID: "voice-a", ProjectID: projectID, CharacterEntityID: first, Voice: "alloy", Revision: 1,
	}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.AssignVoice(ctx, media.CharacterVoice{
		ID: "voice-b", ProjectID: projectID, CharacterEntityID: "voice-char-b", Voice: "nova", Revision: 1,
	}, 0); err != nil {
		t.Fatal(err)
	}
	// THE DEFECT THIS PINS: before this package a project had ONE voice, so two characters sounded
	// the same. Each character's own voice is asserted, not just that two rows exist.
	for characterID, want := range map[string]string{first: "alloy", "voice-char-b": "nova"} {
		record, found, err := repository.GetVoice(ctx, projectID, characterID)
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			t.Fatalf("the character %q has no mapping", characterID)
		}
		if record.Voice != want {
			t.Fatalf("the character %q speaks with %q, want %q", characterID, record.Voice, want)
		}
	}
	// The listing carries the NAMES, because a panel that showed identifiers would be unreadable —
	// and it is ordered by name so two loads agree.
	listed, err := repository.ListVoices(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 {
		t.Fatalf("%d mappings", len(listed))
	}
	names := map[string]bool{}
	for _, mapping := range listed {
		names[mapping.CharacterName] = true
	}
	if !names["阿澈"] || !names["念念"] {
		t.Fatalf("the listing lost a character name: %+v", listed)
	}
}

// TestAnUnmappedCharacterIsNotFoundRatherThanAnError keeps absence out of the error path.
//
// A character with no voice is the ordinary state of a project that has cast nobody, and the caller
// resolves a fallback rather than reporting a fault.
func TestAnUnmappedCharacterIsNotFoundRatherThanAnError(t *testing.T) {
	repository, _, ctx, projectID, _ := voiceHarness(t)
	_, found, err := repository.GetVoice(ctx, projectID, "voice-char-b")
	if err != nil {
		t.Fatalf("an unmapped character produced an error: %v", err)
	}
	if found {
		t.Fatal("an unmapped character reported a mapping")
	}
}

// TestClearingIsIdempotentAndReportsWhatHappened covers the removal path.
func TestClearingIsIdempotentAndReportsWhatHappened(t *testing.T) {
	repository, _, ctx, projectID, characterID := voiceHarness(t)
	if _, err := repository.AssignVoice(ctx, media.CharacterVoice{
		ID: "voice-1", ProjectID: projectID, CharacterEntityID: characterID, Voice: "alloy", Revision: 1,
	}, 0); err != nil {
		t.Fatal(err)
	}
	removed, err := repository.ClearVoice(ctx, projectID, characterID)
	if err != nil {
		t.Fatalf("clearing a mapped character: %v", err)
	}
	if !removed {
		t.Fatal("clearing a mapped character reported nothing removed")
	}
	// A second clear reports that there was nothing, and is NOT an error: a user clearing a character
	// they are unsure about is asking for the state, not for a confirmation.
	removed, err = repository.ClearVoice(ctx, projectID, characterID)
	if err != nil {
		t.Fatalf("clearing an unmapped character: %v", err)
	}
	if removed {
		t.Fatal("clearing an unmapped character reported a removal")
	}
}

// TestDeletingACharacterRemovesItsVoice is the foreign key's consequence.
//
// Without the cascade a deleted character would leave a mapping naming an entity that no longer
// exists, and the listing's join would silently drop it — so the row would be invisible AND
// undeletable through the UI.
func TestDeletingACharacterRemovesItsVoice(t *testing.T) {
	repository, db, ctx, projectID, characterID := voiceHarness(t)
	if _, err := repository.AssignVoice(ctx, media.CharacterVoice{
		ID: "voice-1", ProjectID: projectID, CharacterEntityID: characterID, Voice: "alloy", Revision: 1,
	}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM story_entities WHERE id = ?`, characterID); err != nil {
		t.Fatal(err)
	}
	listed, err := repository.ListVoices(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 0 {
		t.Fatalf("deleting a character left %d mappings", len(listed))
	}
}

// TestAnEmptyVoiceIsRefusedByTheDatabase is the CHECK's proof.
//
// The domain refuses it too, and the schema refusing it as well is what keeps a hand-written row from
// creating a mapping that resolves to silence — which would WIN over the project default.
func TestAnEmptyVoiceIsRefusedByTheDatabase(t *testing.T) {
	repository, _, ctx, projectID, characterID := voiceHarness(t)
	_, err := repository.AssignVoice(ctx, media.CharacterVoice{
		ID: "voice-1", ProjectID: projectID, CharacterEntityID: characterID, Voice: "   ", Revision: 1,
	}, 0)
	if err == nil {
		t.Fatal("a mapping with no voice was accepted")
	}
	if domainErr, ok := media.AsError(err); !ok || domainErr.Category != media.CategoryInvalidInput {
		t.Fatalf("a voiceless mapping failed as %v rather than invalid input", err)
	}
}

// TestTheServiceResolvesThroughAMapping is the service-level walk.
//
// It is the shape the desktop layer uses: assign a voice, then resolve a line's voice with the
// project's own configuration as the default.
func TestTheServiceResolvesThroughAMapping(t *testing.T) {
	repository, _, ctx, projectID, characterID := voiceHarness(t)
	service := appmedia.NewVoiceMappingService(repository)
	if !service.Available() {
		t.Fatal("the service reports itself unavailable over a real repository")
	}
	projectDefault := media.VoiceChoice{ProviderConfigID: "chan-proj", Model: "tts-proj", Voice: "echo"}

	// Before casting: the project's own voice, and the source says so.
	choice, err := service.ResolveVoice(ctx, projectID, characterID, projectDefault)
	if err != nil {
		t.Fatal(err)
	}
	if choice.Voice != "echo" || choice.Source != media.VoiceSourceProject {
		t.Fatalf("before casting the choice is %+v", choice)
	}
	// After casting: the character's voice wins.
	if _, err := service.AssignVoice(ctx, appmedia.AssignVoiceRequest{
		ID: "voice-1", ProjectID: projectID, CharacterEntityID: characterID, Voice: "alloy",
	}); err != nil {
		t.Fatal(err)
	}
	choice, err = service.ResolveVoice(ctx, projectID, characterID, projectDefault)
	if err != nil {
		t.Fatal(err)
	}
	if choice.Voice != "alloy" || choice.Source != media.VoiceSourceCharacter {
		t.Fatalf("after casting the choice is %+v", choice)
	}
	// The channel is inherited, because the cast named only a voice.
	if choice.ProviderConfigID != "chan-proj" || choice.Model != "tts-proj" {
		t.Fatalf("the incomplete mapping did not inherit the channel: %+v", choice)
	}
	// A LINE WITH NO CHARACTER resolves to the project default rather than failing: an action beat or a
	// crowd line is legitimate and has no cast to look up.
	choice, err = service.ResolveVoice(ctx, projectID, "", projectDefault)
	if err != nil {
		t.Fatalf("resolving a line with no character: %v", err)
	}
	if choice.Voice != "echo" || choice.Source != media.VoiceSourceProject {
		t.Fatalf("a line with no character resolved to %+v", choice)
	}
}

// TestAServiceWithoutARepositoryResolvesTheProjectDefault is the degraded build's behaviour.
//
// A build without the drama core can still render speech with the configuration the user set, which
// is what the audio section did before this package. It must NOT error, and it must not pretend a
// character was cast.
func TestAServiceWithoutARepositoryResolvesTheProjectDefault(t *testing.T) {
	service := appmedia.NewVoiceMappingService(nil)
	if service.Available() {
		t.Fatal("a service with no repository reports itself available")
	}
	choice, err := service.ResolveVoice(context.Background(), "proj", "char",
		media.VoiceChoice{ProviderConfigID: "chan", Model: "tts", Voice: "echo"})
	if err != nil {
		t.Fatalf("resolving without a store: %v", err)
	}
	if choice.Voice != "echo" || choice.Source != media.VoiceSourceProject {
		t.Fatalf("the degraded choice is %+v", choice)
	}
	// And with no configuration either, it says so rather than inventing a voice.
	choice, err = service.ResolveVoice(context.Background(), "proj", "char", media.VoiceChoice{})
	if err != nil {
		t.Fatal(err)
	}
	if choice.IsSet() || choice.Source != media.VoiceSourceUnset {
		t.Fatalf("the unconfigured choice is %+v", choice)
	}
	// A command is REFUSED rather than silently dropped: a user who tried to cast a voice must be told
	// that this build cannot store it, not shown a success that did nothing.
	if _, err := service.AssignVoice(context.Background(), appmedia.AssignVoiceRequest{
		ID: "v", ProjectID: "p", CharacterEntityID: "c", Voice: "alloy",
	}); err == nil {
		t.Fatal("a build with no store accepted an assignment")
	}
}
