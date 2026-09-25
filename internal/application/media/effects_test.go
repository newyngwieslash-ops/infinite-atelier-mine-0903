package media

import (
	"strings"
	"testing"
)

// effects_test.go grades the suggestion as a pure function, which is where its honesty lives.
//
// Every assertion here is about the EVIDENCE as much as about the effect: a suggestion that named the
// right sound by accident — a substring of another word, a term matched in the wrong case — would
// pass an assertion that only compared the effect name. The `Matched` field is what a user checks, so
// it is what these tests check.

func shot(ordinal int, id, intent string) ShotEffectInput {
	return ShotEffectInput{ShotID: id, Ordinal: ordinal, Intent: intent}
}

func TestASuggestionNamesBothTheEffectAndTheTermItMatched(t *testing.T) {
	suggestions := SuggestEffects([]ShotEffectInput{
		shot(1, "shot-1", "雨夜，长街无人"),
	})
	if len(suggestions) != 1 {
		t.Fatalf("%d suggestions for one shot", len(suggestions))
	}
	got := suggestions[0]
	if got.Effect != "rain" {
		t.Fatalf("the effect is %q", got.Effect)
	}
	// 「雨」 is the term, not the whole phrase: the evidence must be findable in the lexicon.
	if got.Matched != "雨" {
		t.Fatalf("the matched term is %q", got.Matched)
	}
	if got.ShotID != "shot-1" || got.Ordinal != 1 {
		t.Fatalf("the suggestion does not identify its shot: %+v", got)
	}
	// The intent travels so a reader can judge the evidence.
	if got.Intent != "雨夜，长街无人" {
		t.Fatalf("the intent is %q", got.Intent)
	}
}

// TestTheLongestMatchWinsEvenWhenItNamesADifferentEffect records a consequence of the scoring rule
// that a reader should meet deliberately rather than discover.
//
// 「雨夜，脚步踩过积水」 contains rain (雨), footsteps (脚步) and water (积水). The winner is WATER,
// because 积水 is the longest term — and this is worth pinning because the obvious first guess is
// that the earliest or the first-listed effect would win. It is neither: the rule is specificity,
// and the honest reading of a shot about splashing through puddles is that the splash is the sound
// the script named most precisely.
//
// A user who disagrees sees the reason in one glance (「积水」 is shown beside the suggestion) and the
// remedy is to edit the intent or add the effect by hand — which is exactly why the evidence field
// exists and why this is not a classifier.
func TestTheLongestMatchWinsEvenWhenItNamesADifferentEffect(t *testing.T) {
	suggestions := SuggestEffects([]ShotEffectInput{shot(1, "s", "雨夜，脚步踩过积水")})
	if len(suggestions) != 1 {
		t.Fatalf("%d suggestions for one shot", len(suggestions))
	}
	if suggestions[0].Effect != "water" || suggestions[0].Matched != "积水" {
		t.Fatalf("resolved %q via %q, want water via 积水", suggestions[0].Effect, suggestions[0].Matched)
	}
	// The three competing effects are genuinely all present, so this asserts a CHOICE rather than a
	// single match.
	for _, term := range []string{"雨", "脚步", "积水"} {
		if !strings.Contains(suggestions[0].Intent, term) {
			t.Fatalf("the fixture no longer contains %q", term)
		}
	}
}

// TestTheMostSpecificMatchWinsWithinOneEffect is the scoring rule.
//
// 「积水」 and 「水」 are both water, and reporting 「水」 would be true but less useful: it names a
// character the user did not write as a word. The longer term is the one that says what was
// recognised.
func TestTheMostSpecificMatchWinsWithinOneEffect(t *testing.T) {
	suggestions := SuggestEffects([]ShotEffectInput{shot(1, "s", "地上有积水")})
	if len(suggestions) != 1 {
		t.Fatalf("%d suggestions", len(suggestions))
	}
	if suggestions[0].Effect != "water" || suggestions[0].Matched != "积水" {
		t.Fatalf("resolved %q via %q", suggestions[0].Effect, suggestions[0].Matched)
	}
}

// TestAMoreSpecificTermIsChosenAcrossEffectsToo is the same rule applied to the competition BETWEEN
// entries, which is a different code path: within one entry the longest term wins, but two entries
// can both match and the longer term must win regardless of the lexicon's order.
func TestAMoreSpecificTermIsChosenAcrossEffectsToo(t *testing.T) {
	// 「雨声」 is rain (2 runes) and 「声」 alone is not a term; 「门声」 is a door (2 runes). The
	// discriminating case is a phrase where a SHORT term of an EARLIER entry competes with a LONG term
	// of a LATER one.
	suggestions := SuggestEffects([]ShotEffectInput{shot(1, "s", "远处传来钟声")})
	if len(suggestions) != 1 {
		t.Fatalf("%d suggestions", len(suggestions))
	}
	if suggestions[0].Effect != "bell" || suggestions[0].Matched != "钟声" {
		t.Fatalf("resolved %q via %q", suggestions[0].Effect, suggestions[0].Matched)
	}
}

func TestAShotWithNoRecognisableIntentGetsNoSuggestion(t *testing.T) {
	// A shot with an intent full of words the lexicon does not carry produces NOTHING rather than a
	// fallback. A suggestion of "some ambient sound" would be this application inventing a decision the
	// script did not make.
	suggestions := SuggestEffects([]ShotEffectInput{
		shot(1, "a", "镜头缓缓推近，人物沉默"),
		shot(2, "b", ""),
		shot(3, "c", "   "),
	})
	if len(suggestions) != 0 {
		t.Fatalf("%d suggestions for unintelligible intents: %+v", len(suggestions), suggestions)
	}
}

func TestAShotWithoutAnIdentifierIsSkipped(t *testing.T) {
	// The acceptance path needs to know WHICH shot a suggestion is for, so a suggestion with no shot
	// would be unusable. It is skipped rather than returned with an empty id.
	suggestions := SuggestEffects([]ShotEffectInput{
		{ShotID: "", Ordinal: 1, Intent: "雨声"},
		{ShotID: "  ", Ordinal: 2, Intent: "雨声"},
		shot(3, "kept", "雨声"),
	})
	if len(suggestions) != 1 || suggestions[0].ShotID != "kept" {
		t.Fatalf("suggestions: %+v", suggestions)
	}
}

func TestMatchingIsCaseInsensitiveAndReportsTheLexiconSpelling(t *testing.T) {
	for _, intent := range []string{"RAIN on the roof", "Rain on the roof", "rain on the roof"} {
		suggestions := SuggestEffects([]ShotEffectInput{shot(1, "s", intent)})
		if len(suggestions) != 1 {
			t.Fatalf("%q produced %d suggestions", intent, len(suggestions))
		}
		if suggestions[0].Effect != "rain" {
			t.Fatalf("%q resolved %q", intent, suggestions[0].Effect)
		}
		// The evidence is the LEXICON's spelling, not the intent's: a user looking the term up must
		// find it in the lexicon, and the intent's casing is not what the lexicon says.
		if suggestions[0].Matched != "rain" {
			t.Fatalf("%q reported the term %q", intent, suggestions[0].Matched)
		}
	}
}

// TestTheOrderDoesNotDependOnTheInputOrder is the determinism rule.
//
// A panel that reordered itself between two loads would be unusable, and the reads that supply these
// inputs come from SQL whose order is not promised. The output is ordered by ordinal, and two calls
// with the same shots in different input orders must agree.
func TestTheOrderDoesNotDependOnTheInputOrder(t *testing.T) {
	forward := []ShotEffectInput{
		shot(1, "s1", "雨声"),
		shot(2, "s2", "脚步声"),
		shot(3, "s3", "钟声"),
	}
	reversed := []ShotEffectInput{forward[2], forward[1], forward[0]}

	first := SuggestEffects(forward)
	second := SuggestEffects(reversed)
	if len(first) != 3 || len(second) != 3 {
		t.Fatalf("%d and %d suggestions", len(first), len(second))
	}
	for index := range first {
		if first[index].ShotID != second[index].ShotID || first[index].Effect != second[index].Effect {
			t.Fatalf("position %d differs: %+v vs %+v", index, first[index], second[index])
		}
	}
	// And the ordinals are ascending, which is the order the film runs in.
	for index := 1; index < len(first); index++ {
		if first[index].Ordinal < first[index-1].Ordinal {
			t.Fatalf("the ordinals are not ascending: %d then %d", first[index-1].Ordinal, first[index].Ordinal)
		}
	}
}

// TestOneSuggestionPerShotEvenWhenSeveralEffectsMatch is the "at most one" rule.
//
// An intent describing rain AND footsteps is a scene, not two requests. Returning both would ask the
// user to mix, which is a later decision — and a panel showing three suggestions per shot for a
// fifty-shot episode would be a wall of noise.
func TestOneSuggestionPerShotEvenWhenSeveralEffectsMatch(t *testing.T) {
	suggestions := SuggestEffects([]ShotEffectInput{
		shot(1, "s", "雨声里传来脚步和远处的钟声"),
	})
	if len(suggestions) != 1 {
		t.Fatalf("%d suggestions for one shot: %+v", len(suggestions), suggestions)
	}
	// Every competing effect is genuinely present, so this asserts the CHOICE was made rather than that
	// only one entry matched.
	if !strings.Contains(suggestions[0].Intent, "钟声") {
		t.Fatal("the fixture no longer contains the competing terms")
	}
}

func TestASuggestionIsNotStoredAndScoresAreEvidenceLengths(t *testing.T) {
	// The score is the matched term's rune length — NOT a probability. It orders competing suggestions
	// and is never shown as a confidence, so a test pins the meaning: a two-rune term scores 2.
	suggestions := SuggestEffects([]ShotEffectInput{shot(1, "s", "远处传来钟声")})
	if len(suggestions) != 1 {
		t.Fatalf("%d suggestions", len(suggestions))
	}
	if suggestions[0].Score != len([]rune(suggestions[0].Matched)) {
		t.Fatalf("the score is %d for the term %q", suggestions[0].Score, suggestions[0].Matched)
	}
	if suggestions[0].Score != 2 {
		t.Fatalf("「钟声」 scored %d", suggestions[0].Score)
	}
}

// TestTheVocabularyIsACopy keeps a caller from changing what every later suggestion returns.
func TestTheVocabularyIsACopy(t *testing.T) {
	vocabulary := EffectVocabulary()
	if len(vocabulary) != EffectTermCount() {
		t.Fatalf("the vocabulary lists %d of %d effects", len(vocabulary), EffectTermCount())
	}
	first := vocabulary[0].Effect
	if first == "" {
		t.Fatal("the vocabulary's first entry names no effect")
	}
	vocabulary[0].Effect = "mutated"
	again := EffectVocabulary()
	if again[0].Effect != first {
		t.Fatalf("the lexicon was changed through the returned copy: %q", again[0].Effect)
	}
}
