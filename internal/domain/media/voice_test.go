package media

import (
	"strings"
	"testing"
)

// voice_test.go grades the casting policy before a database or a service exists.
//
// # Why the resolution is tested here and not through the service
//
// `ResolveVoiceChoice` decides which voice a line is rendered with, and it takes both inputs as
// arguments. That is deliberate: the three-level order is a POLICY, and a test that reached it
// through a store would be unable to tell "the mapping won" from "the store returned the project
// default because the mapping was not found". Each level is asserted against a distinct input here, so
// a change that collapsed two levels is visible rather than lucky.

func TestACharacterMappingWinsOverTheProjectDefault(t *testing.T) {
	mapping := CharacterVoice{
		CharacterEntityID: "char-1", ProviderConfigID: "chan-cast", Model: "tts-cast", Voice: "alloy",
	}
	project := VoiceChoice{ProviderConfigID: "chan-proj", Model: "tts-proj", Voice: "echo"}
	choice := ResolveVoiceChoice(mapping, true, project)
	if choice.Voice != "alloy" {
		t.Fatalf("the resolved voice is %q", choice.Voice)
	}
	if choice.ProviderConfigID != "chan-cast" || choice.Model != "tts-cast" {
		t.Fatalf("the channel or model did not come from the mapping: %+v", choice)
	}
	if choice.Source != VoiceSourceCharacter {
		t.Fatalf("the source is %q", choice.Source)
	}
	if !choice.IsSet() {
		t.Fatal("a resolved voice reports itself unset")
	}
}

func TestTheProjectDefaultIsUsedWhenTheCharacterHasNoMapping(t *testing.T) {
	project := VoiceChoice{ProviderConfigID: "chan-proj", Model: "tts-proj", Voice: "echo"}
	// `found` is false, which is what a character with no row looks like. The mapping value is
	// deliberately NOT zero: a caller that passed a stale struct with found=false must still get the
	// project default, because that is what the store said.
	choice := ResolveVoiceChoice(CharacterVoice{Voice: "stale"}, false, project)
	if choice.Voice != "echo" {
		t.Fatalf("the resolved voice is %q", choice.Voice)
	}
	if choice.Source != VoiceSourceProject {
		t.Fatalf("the source is %q", choice.Source)
	}
}

func TestAnUnmappedCharacterInAProjectWithNoDefaultResolvesToUnset(t *testing.T) {
	choice := ResolveVoiceChoice(CharacterVoice{}, false, VoiceChoice{})
	if choice.IsSet() {
		t.Fatalf("an empty configuration resolved a voice: %+v", choice)
	}
	if choice.Source != VoiceSourceUnset {
		t.Fatalf("the source is %q", choice.Source)
	}
	// An unset choice carries nothing: a caller sends no voice field at all, so the provider applies
	// its own default rather than receiving an empty string.
	if choice.ProviderConfigID != "" || choice.Model != "" || choice.Voice != "" {
		t.Fatalf("an unset choice carries fields: %+v", choice)
	}
}

// TestAnIncompleteMappingInheritsTheChannelAndModel is the case the whole shape is for.
//
// A user casts a voice and does not choose a channel, because they have one channel and no reason to
// repeat it. Refusing would make the cast unusable, and replacing the voice with the project's would
// discard the decision they made — so the missing half is inherited and the chosen half is kept.
func TestAnIncompleteMappingInheritsTheChannelAndModel(t *testing.T) {
	cases := []struct {
		name    string
		mapping CharacterVoice
		project VoiceChoice
		want    VoiceChoice
	}{
		{
			name:    "no channel or model",
			mapping: CharacterVoice{Voice: "alloy"},
			project: VoiceChoice{ProviderConfigID: "chan-proj", Model: "tts-proj", Voice: "echo"},
			want: VoiceChoice{
				ProviderConfigID: "chan-proj", Model: "tts-proj", Voice: "alloy",
				Source: VoiceSourceCharacter,
			},
		},
		{
			name:    "no model only",
			mapping: CharacterVoice{Voice: "alloy", ProviderConfigID: "chan-cast"},
			project: VoiceChoice{ProviderConfigID: "chan-proj", Model: "tts-proj", Voice: "echo"},
			want: VoiceChoice{
				ProviderConfigID: "chan-cast", Model: "tts-proj", Voice: "alloy",
				Source: VoiceSourceCharacter,
			},
		},
		{
			// The mapping's own channel is kept even when the project has none: partial inheritance
			// must not overwrite a stated value with an empty one.
			name:    "a mapping channel survives a project with nothing",
			mapping: CharacterVoice{Voice: "alloy", ProviderConfigID: "chan-cast"},
			project: VoiceChoice{},
			want: VoiceChoice{
				ProviderConfigID: "chan-cast", Voice: "alloy", Source: VoiceSourceCharacter,
			},
		},
	}
	for _, testCase := range cases {
		got := ResolveVoiceChoice(testCase.mapping, true, testCase.project)
		if got != testCase.want {
			t.Fatalf("%s: resolved %+v, want %+v", testCase.name, got, testCase.want)
		}
	}
}

// TestAMappingWithoutAVoiceIsNotAMapping pins the guard on the mapping's own field.
//
// The schema refuses a row with an empty voice, so this is defence against a caller that built the
// struct by hand — but the failure it prevents is the one that matters: a mapping row whose voice is
// blank would otherwise WIN over the project default and resolve to silence, which is the opposite of
// what a user who cast a voice asked for.
func TestAMappingWithoutAVoiceIsNotAMapping(t *testing.T) {
	project := VoiceChoice{ProviderConfigID: "chan-proj", Model: "tts-proj", Voice: "echo"}
	for _, blank := range []string{"", "   ", "\t"} {
		choice := ResolveVoiceChoice(CharacterVoice{Voice: blank}, true, project)
		if choice.Voice != "echo" {
			t.Fatalf("a mapping with the voice %q resolved %q", blank, choice.Voice)
		}
		if choice.Source != VoiceSourceProject {
			t.Fatalf("a mapping with the voice %q reports the source %q", blank, choice.Source)
		}
	}
}

func TestValidateRefusesAMappingThatNamesNothing(t *testing.T) {
	base := CharacterVoice{
		ProjectID: "proj-1", CharacterEntityID: "char-1", Voice: "alloy", Revision: 1,
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("a complete mapping was refused: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(CharacterVoice) CharacterVoice
	}{
		{"no project", func(c CharacterVoice) CharacterVoice { c.ProjectID = "  "; return c }},
		{"no character", func(c CharacterVoice) CharacterVoice { c.CharacterEntityID = ""; return c }},
		{"no voice", func(c CharacterVoice) CharacterVoice { c.Voice = "   "; return c }},
		{"no revision", func(c CharacterVoice) CharacterVoice { c.Revision = 0; return c }},
		{"an over-long voice", func(c CharacterVoice) CharacterVoice {
			c.Voice = strings.Repeat("v", maxVoiceLength+1)
			return c
		}},
		{"an over-long model", func(c CharacterVoice) CharacterVoice {
			c.Model = strings.Repeat("m", maxVoiceLength+1)
			return c
		}},
	}
	for _, testCase := range cases {
		if err := testCase.mutate(base).Validate(); err == nil {
			t.Fatalf("%s was accepted", testCase.name)
		}
	}
}

// TestNormalizedTrimsWhatIsStored keeps the stored value equal to the compared value.
//
// Without it, a mapping stored as " alloy " and a lookup keyed on "alloy" would disagree, and the
// disagreement would look like "the cast was ignored".
func TestNormalizedTrimsWhatIsStored(t *testing.T) {
	normalized := CharacterVoice{
		CharacterEntityID: " char-1 ", ProviderConfigID: " chan ", Model: " model ", Voice: " alloy ",
	}.Normalized()
	if normalized.CharacterEntityID != "char-1" || normalized.ProviderConfigID != "chan" ||
		normalized.Model != "model" || normalized.Voice != "alloy" {
		t.Fatalf("normalized to %+v", normalized)
	}
}
