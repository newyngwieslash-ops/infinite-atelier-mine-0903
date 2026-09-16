// Package id generates the entity identifiers described by ADR-0005.
//
// The format is UUID version 7 (RFC 9562): a 48-bit big-endian Unix
// millisecond timestamp, a version nibble, 12 bits of randomness, the RFC 4122
// variant bits, and 62 more bits of randomness. It is assembled here from
// crypto/rand rather than pulled from a library so the module gains no
// dependency for 30 lines of bit layout.
//
// Two properties matter to callers:
//
//   - The canonical string form sorts in creation order (the timestamp is the
//     high-order field), so "newest first" needs no secondary index.
//   - Identifiers are generated in the application layer, never by a
//     repository, per docs/DOMAIN_MODEL.md §2.1.
package id

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// Generator produces entity identifiers. It is safe for concurrent use: it
// holds no mutable state.
type Generator struct {
	// clock is the time source. A nil clock means time.Now.
	clock func() time.Time
}

// NewGenerator builds a generator reading the wall clock.
func NewGenerator() *Generator {
	return &Generator{}
}

// NewGeneratorWithClock builds a generator over a supplied clock, so tests can
// pin the timestamp field.
func NewGeneratorWithClock(clock func() time.Time) *Generator {
	return &Generator{clock: clock}
}

// New returns one UUIDv7 in canonical lowercase hyphenated form.
//
// A failure to read random bytes is reported rather than papered over: an
// identifier with zeroed randomness is not a valid unique id, and silently
// returning one would let two entities collide.
func (g *Generator) New() (string, error) {
	value, err := g.NewBytes()
	if err != nil {
		return "", err
	}
	return Format(value), nil
}

// NewBytes returns the 16 raw bytes of a UUIDv7.
func (g *Generator) NewBytes() ([16]byte, error) {
	var value [16]byte
	millis := g.now().UnixMilli()
	if millis < 0 {
		millis = 0
	}
	// Bytes 0-5: 48-bit big-endian timestamp.
	value[0] = byte(millis >> 40)
	value[1] = byte(millis >> 32)
	value[2] = byte(millis >> 24)
	value[3] = byte(millis >> 16)
	value[4] = byte(millis >> 8)
	value[5] = byte(millis)
	if _, err := rand.Read(value[6:]); err != nil {
		return [16]byte{}, err
	}
	// Byte 6 high nibble: version 7. Byte 8 high two bits: RFC 4122 variant.
	value[6] = (value[6] & 0x0f) | 0x70
	value[8] = (value[8] & 0x3f) | 0x80
	return value, nil
}

func (g *Generator) now() time.Time {
	if g == nil || g.clock == nil {
		return time.Now().UTC()
	}
	return g.clock().UTC()
}

// Format renders 16 bytes as the canonical lowercase hyphenated form.
func Format(value [16]byte) string {
	encoded := make([]byte, 36)
	hex.Encode(encoded[0:8], value[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], value[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], value[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], value[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], value[10:16])
	return string(encoded)
}

// Valid reports whether a string is a canonical UUIDv7.
//
// It rejects other UUID versions: accepting arbitrary UUIDs would let a v4
// identifier into a column whose ordering guarantee callers rely on.
func Valid(value string) bool {
	parsed, ok := parse(value)
	if !ok {
		return false
	}
	if parsed[6]>>4 != 0x7 {
		return false
	}
	return parsed[8]>>6 == 0b10
}

// Timestamp returns the creation time encoded in a UUIDv7. The second result is
// false when the value is not a valid UUIDv7.
func Timestamp(value string) (time.Time, bool) {
	parsed, ok := parse(value)
	if !ok || parsed[6]>>4 != 0x7 {
		return time.Time{}, false
	}
	if parsed[8]>>6 != 0b10 {
		return time.Time{}, false
	}
	var millis int64
	for index := 0; index < 6; index++ {
		millis = millis<<8 | int64(parsed[index])
	}
	return time.UnixMilli(millis).UTC(), true
}

// parse decodes the canonical form. It is strict: 36 characters, hyphens in the
// canonical positions, lowercase hex only.
func parse(value string) ([16]byte, bool) {
	var parsed [16]byte
	if len(value) != 36 {
		return parsed, false
	}
	if value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return parsed, false
	}
	compact := make([]byte, 0, 32)
	for index := 0; index < len(value); index++ {
		switch index {
		case 8, 13, 18, 23:
			continue
		}
		character := value[index]
		switch {
		case character >= '0' && character <= '9':
		case character >= 'a' && character <= 'f':
		default:
			// Uppercase hex is rejected so one identifier has exactly one
			// spelling; otherwise the same id could be stored two ways.
			return parsed, false
		}
		compact = append(compact, character)
	}
	if _, err := hex.Decode(parsed[:], compact); err != nil {
		return parsed, false
	}
	return parsed, true
}
