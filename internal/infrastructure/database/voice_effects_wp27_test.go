package database

import (
	"context"
	"testing"

	appmedia "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/media"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/media"
)

// voice_effects_wp27_test.go walks FR-080's two V1 audio clauses over the REAL schema.
//
// # Why a walk rather than more unit tests
//
// The unit suites grade each piece: the resolution policy, the repository, the suggestion, the role
// that reaches the mix. What none of them can say is whether the pieces are CONNECTED — a service whose
// `ResolveVoice` is perfect and which nothing calls is exactly the "interface with no real path" shape
// this repository's reviews keep finding, and it is invisible to every test that supplies its own
// caller.
//
// So this walk drives the path a desktop command drives: an entity, a casting decision, a line's
// resolution, a shot's intent, a suggestion, and an accepted suggestion attached as an `audio_effect`
// usage that the export then mixes at the effect gain.

// TestTheCastingAndSuggestionPathRunsEndToEnd is the package's acceptance walk.
func TestTheCastingAndSuggestionPathRunsEndToEnd(t *testing.T) {
	harness := newMediaHarness(t)
	ctx := context.Background()
	repository := NewVoiceRepository(harness.db)
	voices := appmedia.NewVoiceMappingService(repository)

	// --- 1. TWO CHARACTERS, because one character cannot demonstrate a mapping at all.
	// The defect this package closes was that a project had ONE voice for everybody, so a walk with a
	// single character would pass against the old behaviour.
	if _, err := harness.db.ExecContext(ctx, `INSERT INTO story_entities
		(id, project_id, entity_type, canonical_name, status, source_scope, created_at, updated_at, revision)
		VALUES ('walk-char-a', 'drama-project', 'character', '阿澈', 'accepted', 'user',
		 '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := harness.db.ExecContext(ctx, `INSERT INTO story_entities
		(id, project_id, entity_type, canonical_name, status, source_scope, created_at, updated_at, revision)
		VALUES ('walk-char-b', 'drama-project', 'character', '念念', 'accepted', 'user',
		 '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 1)`); err != nil {
		t.Fatal(err)
	}

	// --- 2. THE CAST. Two characters, two voices, and the provider/model travel with them because a
	// voice name is not portable between channels.
	for _, cast := range []struct{ id, character, voice string }{
		{"walk-voice-a", "walk-char-a", "alloy"},
		{"walk-voice-b", "walk-char-b", "nova"},
	} {
		if _, err := voices.AssignVoice(ctx, appmedia.AssignVoiceRequest{
			ID: cast.id, ProjectID: "drama-project", CharacterEntityID: cast.character,
			ProviderConfigID: "walk-chan", Model: "walk-tts", Voice: cast.voice,
		}); err != nil {
			t.Fatalf("casting %s: %v", cast.character, err)
		}
	}
	projectDefault := media.VoiceChoice{ProviderConfigID: "proj-chan", Model: "proj-tts", Voice: "echo"}

	// --- 3. THE LINES RESOLVE DIFFERENTLY, which is the whole of 多角色声线映射.
	for character, want := range map[string]string{"walk-char-a": "alloy", "walk-char-b": "nova"} {
		choice, err := voices.ResolveVoice(ctx, "drama-project", character, projectDefault)
		if err != nil {
			t.Fatalf("resolving %s: %v", character, err)
		}
		if choice.Voice != want {
			t.Fatalf("%s resolves to %q, want %q", character, choice.Voice, want)
		}
		if choice.Source != media.VoiceSourceCharacter {
			t.Fatalf("%s resolved from %q", character, choice.Source)
		}
		if choice.ProviderConfigID != "walk-chan" || choice.Model != "walk-tts" {
			t.Fatalf("%s lost its channel: %+v", character, choice)
		}
	}
	// And a character NOBODY cast still answers, from the project, so an uncast walk-on part is not an
	// error state.
	uncast, err := voices.ResolveVoice(ctx, "drama-project", "walk-char-uncast", projectDefault)
	if err != nil {
		t.Fatalf("resolving an uncast character: %v", err)
	}
	if uncast.Voice != "echo" || uncast.Source != media.VoiceSourceProject {
		t.Fatalf("an uncast character resolved to %+v", uncast)
	}

	// --- 4. THE SUGGESTION, from the shot's own authored intent.
	suggestions := appmedia.SuggestEffects([]appmedia.ShotEffectInput{
		{ShotID: "wp11-shot-1", Ordinal: 1, Intent: "雨夜，脚步踩过积水"},
		{ShotID: "wp11-shot-2", Ordinal: 2, Intent: "镜头缓缓推近，人物沉默"},
	})
	if len(suggestions) != 1 {
		t.Fatalf("%d suggestions: %+v", len(suggestions), suggestions)
	}
	if suggestions[0].ShotID != "wp11-shot-1" || suggestions[0].Matched == "" {
		t.Fatalf("the suggestion names no shot or no evidence: %+v", suggestions[0])
	}

	// --- 5. ACCEPTING IT. The suggestion is not stored — it is a projection — so accepting one is
	// what creates the fact: an audio version attached with `audio_effect`, which is the role the
	// export mixes at the effect gain.
	harness.approvedBoard(t, 1, 4)
	effectVersion := harness.attachAudioWithRole(t, ctx, "wp11-shot-1", "walk-effect",
		appmedia.UsageRoleForAudio(appmedia.AudioRoleEffect))

	// --- 6. THE MIX CARRIES IT AS AN EFFECT, which is what closes the loop: a suggestion that reached
	// a table but mixed as dialogue would leave `DefaultGainFor(AudioRoleEffect)` as unreachable as the
	// music default was.
	timeline, err := harness.timeline.Read(ctx, appmedia.TimelineRequest{EpisodeID: "drama-episode"})
	if err != nil {
		t.Fatalf("reading the timeline: %v", err)
	}
	found := false
	for _, shot := range timeline.Shots {
		for _, clip := range shot.AudioClips {
			if clip.VersionID != effectVersion {
				continue
			}
			found = true
			if clip.Role != appmedia.AudioRoleEffect {
				t.Fatalf("the accepted suggestion mixes as %q", clip.Role)
			}
		}
	}
	if !found {
		t.Fatal("the accepted suggestion did not reach the timeline")
	}

	// --- 7. AND THE CAST SURVIVES A RELOAD, because a mapping that only lived in the service would
	// have been the same as not storing it.
	listed, err := voices.ListVoices(ctx, "drama-project")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 {
		t.Fatalf("%d mappings after the walk", len(listed))
	}
	names := map[string]string{}
	for _, mapping := range listed {
		names[mapping.CharacterName] = mapping.Voice
	}
	if names["阿澈"] != "alloy" || names["念念"] != "nova" {
		t.Fatalf("the reloaded cast is %+v", names)
	}
}
