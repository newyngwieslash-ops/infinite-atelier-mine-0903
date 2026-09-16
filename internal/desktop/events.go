package desktop

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"time"
)

// Envelope is the v1 Wails event payload. The only emitted event name is core:event.
type Envelope struct {
	Version int    `json:"version"`
	ID      string `json:"id"`
	Type    string `json:"type"`
	Time    string `json:"time"`
	Payload any    `json:"payload"`
}

// CoreEventName is the only Wails event name emitted by the desktop adapter.
const CoreEventName = "core:event"

// NewEnvelope builds a version-1 event with a random ID and UTC RFC3339Nano timestamp.
func NewEnvelope(eventType string, payload any) (Envelope, error) {
	return newEnvelope(rand.Reader, eventType, payload)
}

func newEnvelope(entropy io.Reader, eventType string, payload any) (Envelope, error) {
	var id [16]byte
	if _, err := io.ReadFull(entropy, id[:]); err != nil {
		return Envelope{}, err
	}
	return Envelope{
		Version: 1,
		ID:      hex.EncodeToString(id[:]),
		Type:    eventType,
		Time:    time.Now().UTC().Format(time.RFC3339Nano),
		Payload: payload,
	}, nil
}
