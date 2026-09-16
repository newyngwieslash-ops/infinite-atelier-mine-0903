package main

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"sync/atomic"
)

// idGenerator produces unique identifiers for jobs, attempts and workers. A
// random suffix avoids collisions without a shared database round trip, and a
// monotonic counter keeps IDs ordered within one process.
type idGenerator struct {
	sequence atomic.Uint64
}

func newIDGenerator() *idGenerator { return &idGenerator{} }

// NewID implements the application jobs IDGenerator port.
func (g *idGenerator) NewID(prefix string) string {
	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		// A random failure must not stop job creation: fall back to the
		// counter alone, which is still unique within this process.
		return prefix + "-" + itoaUint(g.sequence.Add(1))
	}
	return prefix + "-" + hex.EncodeToString(suffix[:]) + "-" + itoaUint(g.sequence.Add(1))
}

func itoaUint(value uint64) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}

// logJobWarning reports a non-fatal job lifecycle problem. The underlying error
// is deliberately reduced to its message via the redacting handler; a job error
// never carries provider payloads or secrets.
func logJobWarning(message string, err error) {
	logger := slog.Default()
	if logger == nil {
		return
	}
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	logger.Warn(message, slog.String("component", "jobs"), slog.String("detail", detail))
}
