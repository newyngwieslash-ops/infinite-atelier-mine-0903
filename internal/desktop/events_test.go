package desktop

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestEnvelopeJSONContract(t *testing.T) {
	envelope, err := NewEnvelope("health.changed", map[string]string{"database": "ready"})
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Version != 1 || envelope.ID == "" || envelope.Type != "health.changed" {
		t.Fatalf("%+v", envelope)
	}
	parsed, err := time.Parse(time.RFC3339Nano, envelope.Time)
	if err != nil || parsed.Location() != time.UTC {
		t.Fatalf("time %s: %v", envelope.Time, err)
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"\"version\"", "\"id\"", "\"type\"", "\"time\"", "\"payload\""} {
		if !strings.Contains(string(raw), key) {
			t.Fatalf("missing %s in %s", key, raw)
		}
	}
	if CoreEventName != "core:event" {
		t.Fatal(CoreEventName)
	}
}

func TestNewEnvelopeFailsClosedWhenEntropyFails(t *testing.T) {
	sentinel := errors.New("entropy unavailable")
	got, err := newEnvelope(failingReader{err: sentinel}, "health.changed", nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want %v", err, sentinel)
	}
	if got != (Envelope{}) {
		t.Fatalf("envelope emitted after entropy failure: %+v", got)
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }
