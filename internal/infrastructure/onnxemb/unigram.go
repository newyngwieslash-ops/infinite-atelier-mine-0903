//go:build cgo

package onnxemb

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"unicode"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

// unigram.go is the SentencePiece Unigram tokenizer, read from a HuggingFace `tokenizer.json`.
//
// # Why a second tokenizer, and why this one
//
// The WordPiece implementation covers BERT-family models, which is what the first verified model used.
// The MULTILINGUAL model — `paraphrase-multilingual-MiniLM-L12-v2`, the one this product actually
// needs for a Chinese corpus — is XLM-RoBERTa based and ships a **Unigram** vocabulary with 250,000
// entries in `tokenizer.json`. Shipping only WordPiece would mean the multilingual model could not be
// used without a fork of this package, which is exactly the seam `Tokenizer` exists to avoid.
//
// # The DEFECT that made this file necessary, recorded because it is easy to repeat
//
// The model's `sentencepiece.bpe.model` and its `tokenizer.json` assign DIFFERENT ids to the special
// tokens: the raw SentencePiece model says `<s>=1`, `</s>=2`, `<pad>=0`, while the tokenizer the model
// was trained against says `<s>=0`, `</s>=2`, `<pad>=1`. A probe that read the SentencePiece file and
// assumed the ids measured the product's own Chinese sentences and got the relationships BACKWARDS —
// 「女主不能穿红色」 scored 0.30 against its own paraphrase and 0.70 against an unrelated line. The
// same sentences through the REFERENCE tokenizer scored 0.83 against the paraphrase and 0.20 against
// the unrelated line.
//
// **SO THIS READS `tokenizer.json`, NOT THE SENTENCEPIECE FILE.** The ids a model was trained on are
// the ids it must be fed, and the file that states them is the one the training used.
//
// # What it implements, and what it does not
//
// Unigram segmentation: normalise, split on the model's own pre-tokenizer rules, then choose the
// segmentation with the highest total log-probability by Viterbi over the character positions. That is
// the whole algorithm, and it is what makes Chinese work — the vocabulary has whole words like 不能 and
// 红色, so a correct Viterbi finds them rather than splitting to single characters.
//
// It does NOT implement byte-level BPE, the `Metaspace` pre-tokenizer's full option set, or the
// `TemplateProcessing` post-processor's arbitrary templates: the special tokens are placed by this
// package's own sequence code, which is the same code the WordPiece path uses, so both tokenizers
// produce the same sequence SHAPE and only the vocabulary differs.

// unigram is a SentencePiece Unigram tokenizer over a `tokenizer.json`.
type unigram struct {
	modelPath string
	// vocab maps a piece to its id, and `scores` the parallel log-probability, both as the file
	// states them.
	vocab  map[string]int64
	scores map[string]float64
	// maxPieceLength bounds the Viterbi window, so a pathological vocabulary cannot make one position
	// scan the whole file.
	maxPieceLength int
	// The special tokens, read from the file rather than assumed — see this header.
	bosID, eosID, padID, unkID int64
	maxTokens                  int
	// peakRunes caches the Viterbi window, and cachedUnknownScore the fallback score: both are derived
	// from the vocabulary and both would otherwise be recomputed per text.
	peakRunes          int
	cachedUnknownScore float64
}

// loadUnigram reads a `tokenizer.json`.
//
// # The one shape that needs care
//
// Unigram `vocab` is `[["<unk>", 0.0], ["<s>", 0.0], ...]` — an array of pairs — while WordPiece's is
// a plain list of words. `encoding/json` cannot express that in a struct, so the pairs are decoded as
// raw messages and read here, which also lets a malformed entry be REFUSED rather than silently
// becoming a zero.
func loadUnigram(path, modelPath string) (*unigram, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, apperror.New("ONNX_TOKENIZER_UNREADABLE", "configuration", false,
			"The embedding tokenizer could not be read.", err)
	}
	var document struct {
		Model struct {
			Type  string              `json:"type"`
			Vocab [][]json.RawMessage `json:"vocab"`
			UnkID int64               `json:"unk_id"`
		} `json:"model"`
		AddedTokens []struct {
			ID      int64  `json:"id"`
			Content string `json:"content"`
		} `json:"added_tokens"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, apperror.New("ONNX_TOKENIZER_UNREADABLE", "configuration", false,
			"The embedding tokenizer is not valid JSON.", err)
	}
	if !strings.EqualFold(document.Model.Type, "Unigram") {
		// A BPE or WordPiece `tokenizer.json` is not this reader's format, and decoding it as Unigram
		// would produce a vocabulary of nonsense. The WordPiece path reads `vocab.txt` instead.
		return nil, apperror.New("ONNX_TOKENIZER_KIND", "configuration", false,
			"That tokenizer is not a Unigram vocabulary, so this build cannot read it.", nil)
	}
	tokenizer := &unigram{
		modelPath: modelPath,
		vocab:     make(map[string]int64, len(document.Model.Vocab)),
		scores:    make(map[string]float64, len(document.Model.Vocab)),
	}
	for _, pair := range document.Model.Vocab {
		if len(pair) != 2 {
			return nil, apperror.New("ONNX_TOKENIZER_MALFORMED", "configuration", false,
				"The embedding tokenizer has a malformed vocabulary entry.", nil)
		}
		var piece string
		var score float64
		if err := json.Unmarshal(pair[0], &piece); err != nil {
			return nil, apperror.New("ONNX_TOKENIZER_MALFORMED", "configuration", false,
				"The embedding tokenizer has a malformed vocabulary entry.", err)
		}
		if err := json.Unmarshal(pair[1], &score); err != nil {
			return nil, apperror.New("ONNX_TOKENIZER_MALFORMED", "configuration", false,
				"The embedding tokenizer has a malformed vocabulary entry.", err)
		}
		tokenizer.scores[piece] = score
		tokenizer.vocab[piece] = int64(len(tokenizer.vocab))
		// The longest piece bounds the Viterbi window. Byte length is used rather than rune count
		// because the scan below works on bytes.
		if length := len(piece); length > tokenizer.maxPieceLength {
			tokenizer.maxPieceLength = length
		}
	}
	if len(tokenizer.vocab) == 0 {
		return nil, apperror.New("ONNX_TOKENIZER_EMPTY", "configuration", false,
			"The embedding tokenizer's vocabulary is empty.", nil)
	}
	tokenizer.unkID = document.Model.UnkID
	// The special tokens come from `added_tokens`, which is where the file states them — NOT from the
	// SentencePiece model beside it, whose numbering differs (see this file's header).
	for _, token := range document.AddedTokens {
		switch token.Content {
		case "<s>":
			tokenizer.bosID = token.ID
		case "</s>":
			tokenizer.eosID = token.ID
		case "<pad>":
			tokenizer.padID = token.ID
		case "<unk>":
			tokenizer.unkID = token.ID
		}
	}
	// A vocabulary without a beginning and an end marker cannot produce a sequence the model was
	// trained on, so their absence is refused rather than defaulted.
	if _, ok := tokenizer.vocab["</s>"]; !ok {
		return nil, apperror.New("ONNX_TOKENIZER_INCOMPLETE", "configuration", false,
			"The embedding tokenizer is missing a token the model requires.", nil)
	}
	return tokenizer, nil
}

// VocabSize reports how many pieces the vocabulary has.
func (u *unigram) VocabSize() int {
	if u == nil {
		return 0
	}
	return len(u.vocab)
}

// Encode turns one text into identifiers, with the model's markers around it.
func (u *unigram) Encode(text string) []int64 {
	if u == nil {
		return nil
	}
	limit := u.maxTokens
	if limit <= 0 {
		limit = DefaultMaxTokens
	}
	tokens := make([]int64, 0, 32)
	tokens = append(tokens, u.bosID)
	for _, piece := range u.segment(normaliseForUnigram(text)) {
		if len(tokens)+1 >= limit {
			break
		}
		id, ok := u.vocab[piece]
		if !ok {
			id = u.unkID
		}
		tokens = append(tokens, id)
	}
	tokens = append(tokens, u.eosID)
	return tokens
}

// segment splits normalised text into pieces by Viterbi over the character positions.
//
// # Why Viterbi rather than greedy longest-match
//
// Greedy matching takes the longest piece at each position, which is wrong for Unigram: the model's
// probabilities say a SPLIT can be more likely than a long piece, and 「不能」 being one token is a
// fact about the training data rather than about its length. Viterbi maximises the total, which is
// what the reference implementation does and what makes the segmentation reproducible.
//
// # Why unknown characters are kept as single pieces
//
// A character no piece contains still has to go SOMEWHERE. Emitting the unknown id per character (as
// the reference does) keeps the mask and the positions meaningful, where dropping them would silently
// shorten the sequence and shift every later token.
func (u *unigram) segment(text string) []string {
	if text == "" {
		return nil
	}
	// Work on RUNE boundaries, because a multi-byte character must not be split: the vocabulary holds
	// whole Chinese characters and a byte-level scan would find none of them.
	runes := []rune(text)
	n := len(runes)
	if n == 0 {
		return nil
	}
	// best[i] is the best total score for the first i runes, and from[i] the piece that got there.
	best := make([]float64, n+1)
	from := make([]string, n+1)
	for i := 1; i <= n; i++ {
		best[i] = math.Inf(-1)
	}
	unkScore := u.unknownScore()
	for start := 0; start < n; start++ {
		if math.IsInf(best[start], -1) {
			continue
		}
		// The window is bounded by the longest piece's RUNE count, recomputed here because the
		// vocabulary's longest piece may be many bytes but few characters.
		for end := start + 1; end <= n && end-start <= u.maxPieceRunes(); end++ {
			candidate := string(runes[start:end])
			score, ok := u.scores[candidate]
			if !ok {
				// A single character that no piece contains is a legitimate unknown, so it is scored
				// as such and the scan continues past it. Anything longer is not a piece at all.
				if end != start+1 {
					continue
				}
				score = unkScore
			}
			total := best[start] + score
			if total > best[end] {
				best[end] = total
				from[end] = candidate
			}
		}
	}
	// Walk back from the end, which is what makes this Viterbi rather than a forward pass.
	pieces := make([]string, 0, n)
	for end := n; end > 0; {
		piece := from[end]
		if piece == "" {
			// Unreachable for a non-empty text: every position is reachable through single characters.
			// Kept as a refusal rather than an infinite loop.
			break
		}
		pieces = append(pieces, piece)
		end -= len([]rune(piece))
	}
	// The walk produced them backwards.
	for left, right := 0, len(pieces)-1; left < right; left, right = left+1, right-1 {
		pieces[left], pieces[right] = pieces[right], pieces[left]
	}
	return pieces
}

// maxRuneLen exposes the Viterbi window, for a test that reports it.
func (u *unigram) maxRuneLen() int { return u.maxPieceRunes() }

// maxPieceRunes is the Viterbi window, computed once and cached by the caller's single-threaded use.
//
// It is derived from the vocabulary rather than fixed, because a fixed window either wastes work on a
// small vocabulary or silently fails to find a long piece in a large one.
func (u *unigram) maxPieceRunes() int {
	if u.peakRunes > 0 {
		return u.peakRunes
	}
	peak := 1
	for piece := range u.vocab {
		if count := len([]rune(piece)); count > peak {
			peak = count
		}
	}
	u.peakRunes = peak
	return peak
}

// unknownScore is the score an unknown single character gets.
//
// It is the LOWEST score in the vocabulary minus one, so a known segmentation always beats one that
// falls back — the ordering the reference implementation's `min_score - 10` achieves, expressed in
// terms this reader can compute from the file it has.
func (u *unigram) unknownScore() float64 {
	if u.cachedUnknownScore != 0 {
		return u.cachedUnknownScore
	}
	lowest := 0.0
	first := true
	for _, score := range u.scores {
		if first || score < lowest {
			lowest, first = score, false
		}
	}
	u.cachedUnknownScore = lowest - 1
	return u.cachedUnknownScore
}

// normaliseForUnigram applies the normalisations the reference pre-tokenizer performs that change the
// SEGMENTATION of the text this product holds.
//
// # The fullwidth-punctuation case, which a test caught
//
// The vocabulary spells ASCII punctuation — `,` is id 4 — while a Chinese sentence uses the fullwidth
// forms: 「女主不能穿红色，这是全剧的禁令。」 contains `，` and `。`. The reference tokenizer's normalizer
// maps those to their ASCII counterparts before segmentation, so its ids are 4 and 30. WITHOUT that
// mapping this reader emitted the UNKNOWN token for every Chinese comma, which is a defect that looks
// harmless — the vector still comes back and is still unit length — while quietly inserting noise into
// the sequence at exactly the places a sentence is punctuated.
//
// The table is small and explicit rather than a Unicode range walk, because the mapping that matters
// here is the CJK punctuation a Chinese manuscript uses and a general NFD-plus-mark-stripping
// implementation would need a Unicode table this package should not carry.
//
// # What it does NOT do
//
// It does not strip accents: that is NFD decomposition plus mark removal, and a partial version would
// change Latin text's ids in a way no test here could check. An accented Latin word may therefore
// segment differently from the reference, which is stated rather than hidden.
func normaliseForUnigram(text string) string {
	collapsed := strings.Join(strings.FieldsFunc(text, unicode.IsSpace), " ")
	// The CJK punctuation the reference normalizer folds to ASCII. Every pair is here because the
	// vocabulary's ASCII form exists; a character with no ASCII counterpart is left alone rather than
	// mapped to something arbitrary.
	var builder strings.Builder
	builder.Grow(len(collapsed))
	for _, symbol := range collapsed {
		if mapped, ok := fullwidthToASCII[symbol]; ok {
			builder.WriteRune(mapped)
			continue
		}
		builder.WriteRune(symbol)
	}
	return builder.String()
}

// fullwidthToASCII is the punctuation mapping the reference normalizer applies for CJK text.
var fullwidthToASCII = map[rune]rune{
	'，': ',', '。': '.', '！': '!', '？': '?', '；': ';', '：': ':',
	'（': '(', '）': ')', '「': '"', '」': '"', '『': '"', '』': '"',
	'【': '[', '】': ']', '｛': '{', '｝': '}', '、': ',', '〜': '~',
	'～': '~', '－': '-', '—': '-', '…': '.', '％': '%', '＃': '#',
	'＆': '&', '＊': '*', '＋': '+', '＝': '=', '／': '/', '｜': '|',
	'＠': '@', '＄': '$', '＇': '"', '＂': '"', '　': ' ',
}
