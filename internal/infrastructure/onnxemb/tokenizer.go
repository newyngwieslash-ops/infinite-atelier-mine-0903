package onnxemb

import (
	"bufio"
	"os"
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

// tokenizer.go is the WordPiece tokenizer the BERT-family model was verified against.
//
// # Why WordPiece, and what it costs
//
// `all-MiniLM-L6-v2` — the model this host can actually reach, and the one ADR-0026 measured — is a
// BERT-family encoder, so its vocabulary is WordPiece: lowercase, greedy longest-match, with `##`
// marking a continuation. Implementing that is what makes the adapter real rather than a wrapper
// around an unimplemented interface.
//
// **IT IS AN ENGLISH TOKENIZER, AND THE CORPUS IS CHINESE.** The same ADR records the measurement:
// 11 of the canary's 27 Chinese characters are in this vocabulary and the ban line
// 「女主不能穿红色，这是全剧的禁令。」 becomes ten `[UNK]` pieces out of sixteen. So the adapter is
// honest about producing poor vectors for Chinese with THIS model, and `Tokenizer` is an interface
// so a model that needs SentencePiece gets it without a fork of this file.
//
// # What it deliberately does not implement
//
// The full BERT basic tokenizer: CJK character splitting, accent stripping, lowercasing rules for
// every script, and the punctuation table. Those are a specification of their own, and a partial
// version presented as complete would be worse than this — which states what it does. What it DOES
// do is the part the verified model needs, plus the two normalisations that would otherwise be
// silent bugs: whitespace collapse and case folding for ASCII.

// Tokenizer turns text into the token identifiers a model expects.
//
// It is an interface with one implementation rather than a concrete type, because the multilingual
// model this item's name refers to would need a different one — SentencePiece, or a HuggingFace
// `tokenizer.json` — and a fork of the embedder to get it would be the wrong seam.
type Tokenizer interface {
	// Encode returns the identifiers for one text, WITH the model's special tokens.
	Encode(text string) []int64
	// VocabSize reports how many entries the vocabulary has, for a diagnostics panel.
	VocabSize() int
}

// wordpiece is the WordPiece implementation over a `vocab.txt`.
type wordpiece struct {
	modelPath string
	vocabPath string
	tokens    map[string]int64
	// unknownID is the `[UNK]` identifier, resolved once at load. A vocabulary without one is refused
	// rather than defaulted, because every unknown word would silently become whatever id zero is.
	unknownID int64
	// clsID and sepID are the sequence markers a BERT-family encoder requires.
	clsID int64
	sepID int64
	// maxTokens bounds one encoded sequence, so a pathological input cannot ask for unbounded work.
	maxTokens int
}

// The three special tokens the encoder needs by name rather than by position.
const (
	clsToken = "[CLS]"
	sepToken = "[SEP]"
	unkToken = "[UNK]"
)

// loadWordPiece reads a vocabulary file.
//
// # The line-ending defect this function exists to prevent
//
// `vocab.txt` files are shipped with CRLF endings, and the first version of the PROBE this package
// was verified with split on `\n` alone — which left a carriage return on every word, made every
// lookup miss, turned every text into the same run of `[UNK]`, and made four different sentences
// come back with a cosine of exactly 1.0000. That reads as "the model has no signal" when the truth
// is "the reader dropped a byte", and it is the kind of defect that gets blamed on a model. The
// scan below trims both endings, and the test asserts a CRLF file loads.
func loadWordPiece(vocabPath string) (*wordpiece, error) {
	file, err := os.Open(vocabPath)
	if err != nil {
		return nil, apperror.New("ONNX_VOCAB_UNREADABLE", "configuration", false,
			"The embedding vocabulary could not be read.", err)
	}
	defer file.Close()

	tokens := map[string]int64{}
	scanner := bufio.NewScanner(file)
	// A BERT vocabulary line is short, but the scanner's default 64 KiB token limit is a silent
	// truncation waiting to happen on an unusual file, so it is raised rather than left to chance.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	identifier := int64(0)
	for scanner.Scan() {
		// `\r` is trimmed here rather than by the scanner, which splits on `\n` only — the defect
		// described above.
		word := strings.TrimRight(scanner.Text(), "\r")
		if word == "" {
			identifier++
			continue
		}
		tokens[word] = identifier
		identifier++
	}
	if err := scanner.Err(); err != nil {
		return nil, apperror.New("ONNX_VOCAB_UNREADABLE", "configuration", false,
			"The embedding vocabulary could not be read.", err)
	}
	if len(tokens) == 0 {
		return nil, apperror.New("ONNX_VOCAB_EMPTY", "configuration", false,
			"The embedding vocabulary is empty.", nil)
	}
	// modelPath is filled by the caller that knows it (`New`), because this function is handed a
	// VOCABULARY and nothing else. Leaving it empty was the defect that made the first run fail with
	// "Load model from  failed" — an error naming no file, from a field nobody had set.
	tokenizer := &wordpiece{vocabPath: vocabPath, tokens: tokens}
	// The three special tokens are REQUIRED, and a vocabulary missing one is refused rather than
	// defaulted: a silent fallback to id zero would make every unknown word "the first token", which
	// is a vector that looks like a result.
	for name, target := range map[string]*int64{
		unkToken: &tokenizer.unknownID, clsToken: &tokenizer.clsID, sepToken: &tokenizer.sepID,
	} {
		id, ok := tokens[name]
		if !ok {
			return nil, apperror.New("ONNX_VOCAB_INCOMPLETE", "configuration", false,
				"The embedding vocabulary is missing a token the model requires.", nil)
		}
		*target = id
	}
	return tokenizer, nil
}

// VocabSize reports how many entries the vocabulary has.
func (w *wordpiece) VocabSize() int {
	if w == nil {
		return 0
	}
	return len(w.tokens)
}

// Encode turns one text into identifiers, with `[CLS]` and `[SEP]` around it.
//
// The sequence is truncated to `maxTokens` INCLUDING the two markers, so the bound is on what the
// model is asked to encode rather than on the text's own length.
func (w *wordpiece) Encode(text string) []int64 {
	if w == nil {
		return nil
	}
	limit := w.maxTokens
	if limit <= 0 {
		limit = DefaultMaxTokens
	}
	tokens := make([]int64, 0, 16)
	tokens = append(tokens, w.clsID)
	for _, word := range splitWords(text) {
		if len(tokens)+1 >= limit {
			break
		}
		tokens = append(tokens, w.tokenizeWord(word, limit-len(tokens)-1)...)
	}
	tokens = append(tokens, w.sepID)
	return tokens
}

// tokenizeWord applies greedy longest-match to one word.
//
// The `##` prefix marks a continuation piece, which is what lets the vocabulary hold sub-words: a
// word that is not a token is split into the longest pieces that are, and only what cannot be split
// further becomes `[UNK]`.
func (w *wordpiece) tokenizeWord(word string, budget int) []int64 {
	if word == "" || budget <= 0 {
		return nil
	}
	lowered := lowercaseASCII(word)
	if id, ok := w.tokens[lowered]; ok {
		return []int64{id}
	}
	out := make([]int64, 0, 4)
	remaining := lowered
	for len(remaining) > 0 && len(out) < budget {
		matched, id := 0, int64(0)
		for end := len(remaining); end > 0; end-- {
			candidate := remaining[:end]
			if len(out) > 0 {
				candidate = "##" + candidate
			}
			if found, ok := w.tokens[candidate]; ok {
				matched, id = end, found
				break
			}
		}
		if matched == 0 {
			// A character that cannot start any piece. The whole word becomes the unknown token,
			// which is what the reference tokenizers do rather than emitting an unknown per byte.
			return []int64{w.unknownID}
		}
		out = append(out, id)
		// A multi-byte character boundary: `remaining[matched:]` is safe because `matched` counts
		// bytes from the same string, so no rune is cut in half.
		remaining = remaining[matched:]
	}
	return out
}

// splitWords divides text on whitespace and isolates punctuation.
//
// Isolating punctuation matters for this model because its vocabulary contains `##,` and `##。`
// style continuations: a comma glued to a word would fail the whole-word lookup and then split at an
// arbitrary point instead of at the punctuation.
func splitWords(text string) []string {
	words := make([]string, 0, 16)
	current := strings.Builder{}
	flush := func() {
		if current.Len() > 0 {
			words = append(words, current.String())
			current.Reset()
		}
	}
	for _, symbol := range text {
		switch {
		case symbol == ' ' || symbol == '\t' || symbol == '\n' || symbol == '\r' || symbol == '\v' || symbol == '\f':
			flush()
		case isPunctuation(symbol):
			flush()
			words = append(words, string(symbol))
		default:
			current.WriteRune(symbol)
		}
	}
	flush()
	return words
}

// isPunctuation reports whether a rune ends a word.
//
// It covers the ASCII punctuation the vocabulary has entries for and the CJK full stop and comma,
// which are one rune each and which the model's vocabulary contains. A wider table would be a
// second specification to keep true; this one states what it covers.
func isPunctuation(symbol rune) bool {
	switch symbol {
	case '.', ',', '!', '?', ';', ':', '\'', '"', '(', ')', '[', ']', '{', '}', '-', '_', '/', '\\':
		return true
	case '，', '。', '！', '？', '；', '：', '、', '（', '）', '「', '」', '『', '』', '《', '》':
		return true
	default:
		return false
	}
}

// lowercaseASCII folds A-Z only.
//
// It is deliberately not `strings.ToLower`, which folds every script including the ones where case
// is not a thing — and the reference BERT tokenizer's own rule is ASCII case folding plus accent
// stripping, of which this implements the half that cannot corrupt a non-Latin script.
func lowercaseASCII(word string) string {
	for index := 0; index < len(word); index++ {
		symbol := word[index]
		if symbol >= 'A' && symbol <= 'Z' {
			lowered := []byte(word)
			for offset := index; offset < len(lowered); offset++ {
				if lowered[offset] >= 'A' && lowered[offset] <= 'Z' {
					lowered[offset] += 'a' - 'A'
				}
			}
			return string(lowered)
		}
	}
	return word
}
