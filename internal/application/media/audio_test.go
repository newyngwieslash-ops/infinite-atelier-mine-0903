package media

import (
	"strings"
	"testing"
)

// audio_test.go covers FR-080's V1 audio vocabulary: the mix, its roles and its gains.
//
// # What this file is for
//
// The mix replaced a `[]string` of paths that the adapter CONCATENATED. Every decision that made the
// old shape unable to express a mix is a decision here, and each one has a test because each one can
// be wrong in a way that is silent:
//
//   - A clip with no START plays at the beginning, so a line lands under the wrong shot.
//   - A clip with no GAIN plays at unity, so a music bed buries the dialogue.
//   - A role that is not validated lets a typo through to the switch that decides the default gain,
//     where an unknown role would silently get dialogue's level.
//   - An empty mix is legal, and a caller that treated it as an error would refuse to export a silent
//     film — which FR-080 permits and which an episode with no TTS yet produces.
//
// The values are checked here rather than through the engine because none of them needs ffmpeg: this
// is arithmetic and vocabulary, and asserting it through a subprocess would make a wrong default look
// like an encoding failure.

func TestAClipWithoutAStartPlaysAtTheBeginning(t *testing.T) {
	clip := AudioClip{Role: AudioRoleMusic, Path: "/tmp/bed.m4a", DurationMS: 90_000}
	if err := clip.Validate(); err != nil {
		t.Fatalf("a music bed from the beginning was refused: %v", err)
	}
	if clip.StartMS != 0 {
		t.Fatalf("the zero value of StartMS is %d, and a bed starts at zero", clip.StartMS)
	}
}

func TestAClipCannotStartBeforeTheBeginning(t *testing.T) {
	clip := AudioClip{Role: AudioRoleDialogue, Path: "/tmp/line.m4a", StartMS: -1}
	if err := clip.Validate(); err == nil {
		t.Fatal("a clip starting at -1ms was accepted")
	}
}

func TestMusicDefaultsBelowDialogue(t *testing.T) {
	// The one mixing decision this build makes for a user. Music at unity under a spoken line makes
	// the line unintelligible, and a user who just imported a track has no reason to know that.
	music := AudioClip{Role: AudioRoleMusic, Path: "/tmp/bed.m4a"}.Normalize()
	dialogue := AudioClip{Role: AudioRoleDialogue, Path: "/tmp/line.m4a"}.Normalize()
	if music.Gain >= dialogue.Gain {
		t.Fatalf("music defaults to %v and dialogue to %v, so the bed would cover the line",
			music.Gain, dialogue.Gain)
	}
	if dialogue.Gain != 1 {
		t.Fatalf("dialogue defaults to %v, and nothing should quieten an approved performance", dialogue.Gain)
	}
	if music.Gain <= 0 {
		t.Fatalf("music defaults to %v, which is silence rather than a bed", music.Gain)
	}
}

func TestAStatedGainOverridesTheDefault(t *testing.T) {
	// The default is a default, not a rule: a user who wants the music loud sets it loud, and a user
	// who wants it silent sets it to something that is not zero — which is why the unset value is
	// zero and `Normalize` is a separate step from `Validate`.
	loud := AudioClip{Role: AudioRoleMusic, Path: "/tmp/bed.m4a", Gain: 1.5}.Normalize()
	if loud.Gain != 1.5 {
		t.Fatalf("a stated gain of 1.5 became %v", loud.Gain)
	}
	quiet := AudioClip{Role: AudioRoleDialogue, Path: "/tmp/line.m4a", Gain: 0.5}.Normalize()
	if quiet.Gain != 0.5 {
		t.Fatalf("a stated gain of 0.5 became %v", quiet.Gain)
	}
}

func TestAnUnknownRoleIsRefusedRatherThanDefaulted(t *testing.T) {
	clip := AudioClip{Role: "dialog", Path: "/tmp/line.m4a"}
	if err := clip.Validate(); err == nil {
		t.Fatal("the role \"dialog\" was accepted")
	}
	// And the roles that ARE documented are all valid, so the refusal is a vocabulary check rather
	// than a refusal of the feature.
	for _, role := range AudioRoles {
		if !IsValidAudioRole(role) {
			t.Fatalf("the documented role %q was refused", role)
		}
		if err := (AudioClip{Role: role, Path: "/tmp/x.m4a"}).Validate(); err != nil {
			t.Fatalf("a clip with the role %q was refused: %v", role, err)
		}
	}
}

func TestAnEmptyMixIsASilentFilmRatherThanAnError(t *testing.T) {
	// FR-080 permits a silent film, and an episode with no TTS yet produces one. A mix that refused
	// an empty clip list would make "export what I have" impossible before the audio is made, which
	// is the state every project passes through.
	var mix AudioMix
	if err := mix.Validate(); err != nil {
		t.Fatalf("an empty mix was refused: %v", err)
	}
	normalized := mix.Normalized()
	if len(normalized.Clips) != 0 {
		t.Fatalf("normalising an empty mix produced %d clips", len(normalized.Clips))
	}
}

func TestValidateNamesTheClipThatIsWrong(t *testing.T) {
	// A mix of sixty clips where the last one has no path: the refusal must be about THAT clip, or a
	// user with a broken import has no way to find which row to fix. The message carries the role and
	// the failure rather than a positional index, because an index into a list the user did not see
	// is not actionable.
	good := AudioClip{Role: AudioRoleDialogue, Path: "/tmp/line.m4a"}
	mix := AudioMix{Clips: []AudioClip{good, {Role: AudioRoleMusic}}}
	err := mix.Validate()
	if err == nil {
		t.Fatal("a clip with no path was accepted")
	}
	if !strings.Contains(err.Error(), "file") {
		t.Fatalf("the refusal does not say what is missing: %v", err)
	}
}

func TestNormalizeDoesNotMutateTheOriginal(t *testing.T) {
	// The mix is built once and normalised once, and a `Normalize` that wrote through to the shared
	// clip would make the SECOND call to it a no-op with a different answer — the kind of state bug
	// that only shows up when a caller reuses a mix.
	clip := AudioClip{Role: AudioRoleMusic, Path: "/tmp/bed.m4a"}
	mix := AudioMix{Clips: []AudioClip{clip}}
	normalized := mix.Normalized()
	if mix.Clips[0].Gain != 0 {
		t.Fatalf("normalising the mix changed the original clip's gain to %v", mix.Clips[0].Gain)
	}
	if normalized.Clips[0].Gain != DefaultGainFor(AudioRoleMusic) {
		t.Fatalf("the normalised clip's gain is %v", normalized.Clips[0].Gain)
	}
}

func TestTheMixBoundIsStatedOnce(t *testing.T) {
	// The bound is exposed rather than duplicated, so the application layer's check and the engine's
	// argument budget cannot disagree about how many clips are allowed.
	if MaxAudioClips() <= 0 {
		t.Fatalf("the mix bound is %d", MaxAudioClips())
	}
	// It is above a real episode's dialogue count, which is the point: a five-minute scene with a
	// line every ten seconds is thirty.
	if MaxAudioClips() < 32 {
		t.Fatalf("the mix bound is %d, below a real scene's dialogue", MaxAudioClips())
	}
}
