package importing

import (
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/unicode/norm"
)

// detect.go holds the decoding and detection rules. Everything here is a pure
// function over bytes: the caller supplies the content, and nothing is read
// from anywhere else.
//
// The order of operations matters and is deliberate:
//
//  1. A byte order mark decides UTF-8 or UTF-16 outright. It is the only
//     unambiguous signal, because UTF-16 without one is indistinguishable from
//     arbitrary binary.
//  2. Valid UTF-8 is accepted as UTF-8. A Chinese novel in UTF-8 is valid
//     UTF-8, and so is a novel in GBK only when it happens to contain no
//     multi-byte characters, which a real one does.
//  3. GB18030 is tried before GBK, because GBK is a subset: a real GB18030
//     document that decodes as GBK would be misreported, while the reverse
//     cannot happen.
//
// Every decode is total: a candidate that produces replacement characters is
// rejected rather than accepted with damage. That is what makes "detection" mean
// something instead of "guess".

// DetectEncoding decides how to decode a document's bytes.
//
// It returns the encoding and the decoded text. The text is normalized on the
// way out, so a caller that only wants the text does not have to remember to
// normalize separately. Format detection is separate (DetectFormat) because a
// DOCX's bytes are a ZIP, not text.
func DetectEncoding(data []byte) (Encoding, string, error) {
	if len(data) == 0 {
		return "", "", InvalidError("The document is empty.")
	}
	if int64(len(data)) > MaxInputBytes {
		return "", "", InvalidError("The document is larger than an import accepts.")
	}

	// 1. A byte order mark is authoritative.
	if text, ok := trimUTF8BOM(data); ok {
		return EncodingUTF8, normalize(text), nil
	}
	if encoding, text, ok := decodeUTF16WithBOM(data); ok {
		return encoding, normalize(text), nil
	}

	// 2. Valid UTF-8 is accepted as UTF-8.
	if utf8.Valid(data) {
		return EncodingUTF8, normalize(string(data)), nil
	}

	// 3. GB18030 before GBK, because GBK's byte ranges are a subset.
	if text, ok := decodeWithoutLoss(simplifiedchinese.GB18030.NewDecoder(), data); ok {
		return EncodingGB18030, normalize(text), nil
	}
	if text, ok := decodeWithoutLoss(simplifiedchinese.GBK.NewDecoder(), data); ok {
		return EncodingGBK, normalize(text), nil
	}

	// Nothing decoded cleanly. Reporting this as invalid input rather than
	// security is deliberate: the bytes may be a corrupt file or a binary a
	// user renamed, and neither is an attack. The caller decides what to say.
	return "", "", InvalidError("The document's encoding is not recognised.")
}

// trimUTF8BOM removes a UTF-8 byte order mark and reports whether one was there.
func trimUTF8BOM(data []byte) (string, bool) {
	const bom = "\xef\xbb\xbf"
	if len(data) >= len(bom) && string(data[:len(bom)]) == bom {
		return string(data[len(bom):]), true
	}
	return "", false
}

// decodeUTF16WithBOM decodes UTF-16 when a byte order mark names the byte order.
//
// UTF-16 without a mark is not attempted: the same bytes decode to different
// text either way, and picking one would be a coin flip presented as a decision.
func decodeUTF16WithBOM(data []byte) (Encoding, string, bool) {
	if len(data) < 2 || len(data)%2 != 0 {
		return "", "", false
	}
	var encoding Encoding
	switch {
	case data[0] == 0xff && data[1] == 0xfe:
		encoding = EncodingUTF16LE
	case data[0] == 0xfe && data[1] == 0xff:
		encoding = EncodingUTF16BE
	default:
		return "", "", false
	}
	body := data[2:]
	runes := make([]rune, 0, len(body)/2)
	for index := 0; index+1 < len(body); index += 2 {
		var value uint16
		if encoding == EncodingUTF16LE {
			value = uint16(body[index]) | uint16(body[index+1])<<8
		} else {
			value = uint16(body[index])<<8 | uint16(body[index+1])
		}
		// A surrogate pair is two units. An unpaired surrogate is damage.
		if value >= 0xd800 && value <= 0xdbff {
			if index+3 >= len(body) {
				return "", "", false
			}
			var low uint16
			if encoding == EncodingUTF16LE {
				low = uint16(body[index+2]) | uint16(body[index+3])<<8
			} else {
				low = uint16(body[index+2])<<8 | uint16(body[index+3])
			}
			if low < 0xdc00 || low > 0xdfff {
				return "", "", false
			}
			runes = append(runes, 0x10000+(rune(value-0xd800)<<10)+rune(low-0xdc00))
			index += 2
			continue
		}
		if value >= 0xdc00 && value <= 0xdfff {
			return "", "", false
		}
		runes = append(runes, rune(value))
	}
	return encoding, string(runes), true
}

// decodeWithoutLoss runs a decoder and rejects the result if it looks like
// damaged text.
//
// Three checks, and each one earns its place:
//
//   - the decoder must not fail outright;
//   - the result must not contain a replacement character, because a lenient
//     decoder turns undecodable bytes into U+FFFD rather than reporting them;
//   - the result must not contain a NUL or a C0 control character other than
//     tab, newline and carriage return.
//
// The third check is what stops UTF-16 without a byte order mark from being
// accepted: those bytes are full of NULs, GB18030 happily consumes them, and
// without this the "decoded" text is the original bytes interleaved with NULs.
// Real prose has no NUL in it, so refusing one costs nothing and closes the
// path where any binary file could masquerade as a text document.
func decodeWithoutLoss(decoder interface {
	Bytes([]byte) ([]byte, error)
}, data []byte) (string, bool) {
	decoded, err := decoder.Bytes(data)
	if err != nil {
		return "", false
	}
	text := string(decoded)
	if !utf8.ValidString(text) {
		return "", false
	}
	for _, character := range text {
		switch {
		case character == utf8.RuneError:
			return "", false
		case character == 0:
			return "", false
		case character < 0x20 && character != '\t' && character != '\n' && character != '\r':
			return "", false
		}
	}
	return text, true
}

// normalize applies the transformations that make offsets comparable across
// imports: line endings, then Unicode composition.
//
// NFC is not cosmetic. A macOS-authored file may store a character decomposed,
// and a composed one elsewhere; without normalization the same visible text has
// two different byte lengths, so an offset recorded on one machine would be
// wrong on another.
func normalize(text string) string {
	return norm.NFC.String(unifyLineEndings(text))
}

// unifyLineEndings rewrites CRLF and lone CR to LF.
//
// Every offset in the schema is measured against the normalized text, so the
// unifier has to run before any boundary is computed and only once.
func unifyLineEndings(text string) string {
	if !containsCarriageReturn(text) {
		return text
	}
	result := make([]byte, 0, len(text))
	for index := 0; index < len(text); index++ {
		if text[index] != '\r' {
			result = append(result, text[index])
			continue
		}
		result = append(result, '\n')
		if index+1 < len(text) && text[index+1] == '\n' {
			index++
		}
	}
	return string(result)
}

func containsCarriageReturn(text string) bool {
	for index := 0; index < len(text); index++ {
		if text[index] == '\r' {
			return true
		}
	}
	return false
}
