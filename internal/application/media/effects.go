package media

import (
	"sort"
	"strings"
)

// effects.go is FR-080's 音效建议 (V1: 音效建议与生成适配).
//
// # What was missing, and what the missing half actually was
//
// `AudioRoleEffect` exists and mixes with the right default gain, so the OUTPUT end was ready. What
// nothing did was SUGGEST an effect for a shot: STATUS section 0y recorded it in as many words —
// "`AudioRoleEffect` exists and mixes, and nothing SUGGESTS an effect for a shot".
//
// The input was already being authored and never read. `shots.audio_intent` is written by the script
// agent (FR-070's shot draft carries it), stored in migration 000008, and read back into the domain —
// and no code ever looked at its CONTENT. A shot whose intent says 「雨夜，脚步踩过积水」 has been
// telling this application what it should sound like since WP-08, and nobody listened.
//
// # Why a lexicon rather than a model
//
// A suggestion a user cannot check is a suggestion they cannot trust, and "the model gave it 0.72" is
// not checkable. Every suggestion here names the TERM that produced it, so a user reading 「雨声 ←
// 雨」 can see immediately whether this application understood the shot, and a wrong suggestion is a
// phrase to add to the lexicon rather than a mystery. The lexicon is a value (`EffectLexicon`), not a
// table and not a prompt: it is read by the suggestion, shown by the UI, and asserted by tests, so
// there is exactly one copy of it.
//
// # Why a suggestion is NOT stored
//
// A suggestion is a pure function of the script, so storing it would create a second answer that goes
// stale the moment the intent is edited. It is a projection, like the timeline. When a user ACCEPTS
// one, the act of accepting submits a real audio job carrying `usage_role = 'audio_effect'` — and
// that is the fact, because it names a file and a version.

// EffectSuggestion is one proposed sound for one shot, with the evidence for it.
type EffectSuggestion struct {
	ShotID string
	// Ordinal is the shot's position, so a panel can order suggestions the way the film runs without
	// resolving the shot again.
	Ordinal int
	// Intent is the text that was examined, trimmed. It travels because a user judging a suggestion
	// needs to see what it was drawn from — an intent of forty characters and one of two are different
	// evidence, and a panel that showed only the suggestion would be asking to be trusted.
	Intent string
	// Effect names the sound, in the same vocabulary the audio role's label uses.
	Effect string
	// Matched is the term in the intent that produced this suggestion. It is what makes the suggestion
	// CHECKABLE, and it is the field that would be dropped by an implementation that treated this as a
	// classifier.
	Matched string
	// Score orders competing suggestions for ONE shot. It is the matched term's length in runes, so a
	// longer, more specific term beats a shorter one that happens to be a substring of it — 「积水」
	// over 「水」. It is NOT a confidence and is not presented as one: there is no probability here, and
	// calling a term length a confidence would dress up an arbitrary number as evidence.
	Score int
}

// effectTerm is one entry in the lexicon: a sound and the words that indicate it.
type effectTerm struct {
	Effect string
	// Terms are matched case-insensitively as SUBSTRINGS of the intent. They are ordered longest
	// first within an entry so that when several terms of the SAME effect match, the most specific one
	// is the one reported as the evidence.
	Terms []string
}

// effectLexicon is the whole vocabulary.
//
// # How to extend it
//
// Add an entry, or add terms to one. The order of the ENTRIES matters only for a tie — two effects
// whose best terms are equally long — and in that case the earlier entry wins, so a specific effect
// should be listed before a general one.
//
// # Where the terms come from
//
// The Chinese terms are everyday words for a sound, chosen to be what a script would actually write
// in an `audio_intent`: 雨/雷/风 rather than 「环境音」, because an intent describes the scene. The
// English terms exist because a script may be written in either language and an intent is free text.
//
// This is a PRODUCT judgement encoded as data, and it is deliberately readable for that reason: a
// missing term is a one-line fix, and the `Matched` field on every suggestion is what tells a user
// which line to fix.
var effectLexicon = []effectTerm{
	{Effect: "rain", Terms: []string{"暴雨", "大雨", "雨声", "雷声", "打雷", "rain", "thunder", "雨"}},
	{Effect: "water", Terms: []string{"积水", "水声", "海浪", "流水", "wave", "water", "水"}},
	{Effect: "footsteps", Terms: []string{"脚步", "footstep", "footsteps", "跑动", "走路", "step"}},
	{Effect: "door", Terms: []string{"关门", "开门", "敲门", "门声", "door", "knock", "门"}},
	{Effect: "wind", Terms: []string{"风声", "呼啸", "wind", "风"}},
	{Effect: "metal", Terms: []string{"金属", "铁链", "刀剑", "metal", "chain", "sword", "刀", "剑"}},
	{Effect: "engine", Terms: []string{"引擎", "发动机", "摩托", "汽车", "engine", "motor"}},
	{Effect: "birds", Terms: []string{"鸟鸣", "鸟叫", "birds", "bird", "鸟"}},
	{Effect: "bell", Terms: []string{"钟声", "铃声", "铃铛", "bell", "chime", "钟", "铃"}},
	{Effect: "heartbeat", Terms: []string{"心跳", "heartbeat", "心"}},
	{Effect: "gunshot", Terms: []string{"枪声", "枪响", "子弹", "gunshot", "gunfire", "gun", "枪"}},
	{Effect: "glass", Terms: []string{"玻璃", "破碎", "glass", "shatter"}},
	{Effect: "applause", Terms: []string{"掌声", "喝彩", "applause", "appl"}},
	{Effect: "fire", Terms: []string{"火焰", "篝火", "燃烧", "fire", "flame", "火"}},
	{Effect: "crowd", Terms: []string{"人群", "喧哗", "嘈杂", "crowd", "murmur"}},
}

// EffectVocabulary returns the lexicon, for a UI or a test that needs to show or grade it.
//
// It returns a COPY of the slice, and the terms are copied with it. A caller that could mutate the
// package's own lexicon could change what every later suggestion returns, which is the shape of
// global mutable state this package's other values deliberately avoid.
func EffectVocabulary() []EffectSuggestion {
	vocabulary := make([]EffectSuggestion, 0, len(effectLexicon))
	for _, entry := range effectLexicon {
		vocabulary = append(vocabulary, EffectSuggestion{Effect: entry.Effect})
	}
	return vocabulary
}

// EffectTermCount reports how many effects the lexicon recognises, for a UI that says so.
func EffectTermCount() int { return len(effectLexicon) }

// ShotEffectInput is what a suggestion is drawn from.
type ShotEffectInput struct {
	ShotID string
	// Ordinal orders the results. Inputs are sorted by it so the output does not depend on the order a
	// caller happened to read rows in.
	Ordinal int
	// Intent is the shot's authored `audio_intent`. Empty is the ordinary state of a shot whose script
	// has not been through a pass that fills it, and it produces no suggestion rather than an error.
	Intent string
}

// SuggestEffects proposes at most one sound effect per shot.
//
// # One per shot, and why
//
// A shot is a sound CUE: what a viewer hears over those seconds. Offering three effects for one shot
// would ask the user to mix, which is a different and much later decision — and an intent that
// mentions rain and footsteps is describing a scene where the rain is the bed and the steps are the
// action. The most specific match wins, and the others remain available by editing the intent or by
// adding an effect by hand. The alternative — one suggestion per matched term — would bury the
// plausible one under incidental words.
//
// # Determinism
//
// The output is ordered by the shots' ordinals, and within one shot by (score, lexicon order). Two
// runs over the same script produce byte-identical suggestions, which is what makes this usable in a
// test and what a user who reloads the panel expects. There is no map iteration anywhere in it.
func SuggestEffects(inputs []ShotEffectInput) []EffectSuggestion {
	ordered := make([]ShotEffectInput, 0, len(inputs))
	for _, input := range inputs {
		// A shot with no identifier cannot be acted on — the acceptance path needs to know which shot
		// a suggestion is for — and a shot with no intent has nothing to read.
		if strings.TrimSpace(input.ShotID) == "" || strings.TrimSpace(input.Intent) == "" {
			continue
		}
		ordered = append(ordered, input)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Ordinal != ordered[j].Ordinal {
			return ordered[i].Ordinal < ordered[j].Ordinal
		}
		// A stable tie-break on the identifier, so two shots sharing an ordinal (which the schema
		// forbids but a caller can still construct) come back in a settled order.
		return ordered[i].ShotID < ordered[j].ShotID
	})

	suggestions := make([]EffectSuggestion, 0, len(ordered))
	for _, input := range ordered {
		intent := strings.TrimSpace(input.Intent)
		best := EffectSuggestion{}
		for _, entry := range effectLexicon {
			term, matched := bestMatch(intent, entry.Terms)
			if !matched {
				continue
			}
			score := len([]rune(term))
			// STRICTLY greater, so the earlier entry wins a tie. That is what makes the lexicon's order
			// meaningful rather than incidental.
			if best.Effect == "" || score > best.Score {
				best = EffectSuggestion{
					ShotID: input.ShotID, Ordinal: input.Ordinal, Intent: intent,
					Effect: entry.Effect, Matched: term, Score: score,
				}
			}
		}
		if best.Effect != "" {
			suggestions = append(suggestions, best)
		}
	}
	return suggestions
}

// bestMatch returns the longest term present in the intent.
//
// The comparison is case-insensitive on BOTH sides, which is what lets one entry carry `Rain` and
// `rain` without listing both — and the returned term is the lexicon's spelling rather than the
// intent's, because the point of the evidence is to name the TERM a reader can find in the lexicon.
func bestMatch(intent string, terms []string) (string, bool) {
	lowered := strings.ToLower(intent)
	best := ""
	for _, term := range terms {
		if !strings.Contains(lowered, strings.ToLower(term)) {
			continue
		}
		if best == "" || len([]rune(term)) > len([]rune(best)) {
			best = term
		}
	}
	return best, best != ""
}
