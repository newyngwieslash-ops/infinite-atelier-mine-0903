package memory

import "unicode"

// Token estimation, which AGENT_CONTRACTS section 12.1 asks for and PRD FR-120 grades on:
// "记忆构建符合 Token Budget".
//
// # What this is and what it is not
//
// It is a BOUND, not a tokeniser. The number it returns is deliberately an over-estimate
// rather than an accurate count, because the budget's purpose is to keep the memory layer
// inside its share of section 5.3's nine-layer priority list, and the failure that matters is
// exceeding it. An under-estimate would spend real tokens the layer was not allowed and would
// be invisible until a provider refused the prompt.
//
// The rule:
//
//   - A Han character is one token. Real tokenisers for Chinese often spend one token per
//     character and sometimes more, so this is a fair figure rather than a generous one.
//   - Other text is estimated at one token per three characters, which over-estimates English
//     (whose average is nearer four) and badly over-estimates whitespace and punctuation,
//     which is the safe direction.
//   - Every non-empty string costs at least one, so no content is free.
//
// ADR-0014 records that this is an approximation: a real tokeniser belongs with the provider
// that has one, and this build has no dependency that carries one.

// TokensPerLatinRune estimates how many tokens one non-Han rune costs, expressed as a
// divisor: three runes per token.
const LatinRunesPerToken = 3

// EstimateTokens estimates the tokens a piece of text costs.
//
// The result is stable for the same input, which is what makes a budgeted assembly
// reproducible: the truncation point must not move between two runs over the same content.
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	han := 0
	other := 0
	for _, symbol := range text {
		if isHan(symbol) {
			han++
			continue
		}
		other++
	}
	tokens := han + (other+LatinRunesPerToken-1)/LatinRunesPerToken
	if tokens < 1 {
		// A single character still costs something: a context of free items is a context with
		// no bound at all.
		return 1
	}
	return tokens
}

// isHan reports whether a rune is in the Han script ranges this build treats as one token
// each.
//
// The ranges are the CJK Unified Ideographs block and its extensions that carry text this
// project's inputs can contain: the base block, extension A, and the compatibility block.
// The two extension planes above BMP are omitted on purpose — their runes are outside the
// common repertoire and treating them as Latin would over-estimate them rather than under,
// which is the safe direction.
func isHan(symbol rune) bool {
	switch {
	case symbol >= 0x4E00 && symbol <= 0x9FFF:
		return true
	case symbol >= 0x3400 && symbol <= 0x4DBF:
		return true
	case symbol >= 0xF900 && symbol <= 0xFAFF:
		return true
	default:
		return unicode.Is(unicode.Han, symbol)
	}
}

// WithinBudget reports whether a set of texts fits a token budget, and what it would cost.
//
// It is a helper for the assembly's inner loop, kept here so the arithmetic has one home: the
// context builder walks its channels with this, and a caller previewing a budget uses the same
// function rather than a second implementation of the sum.
func WithinBudget(budget int, texts ...string) (int, bool) {
	used := 0
	for _, text := range texts {
		used += EstimateTokens(text)
		if used > budget {
			return used, false
		}
	}
	return used, true
}
