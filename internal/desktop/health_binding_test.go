package desktop

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/health"
)

type okProbe struct{}

func (okProbe) PingContext(context.Context) error { return nil }

type countingProbe struct{ calls int }

func (p *countingProbe) PingContext(context.Context) error {
	p.calls++
	return nil
}

func TestHealthBindingGetReadyAndSafe(t *testing.T) {
	binding := &HealthBinding{}
	Attach(binding, context.Background(), health.New("1.0.0", "/data", okProbe{}, false, ""))
	ready := binding.Get()
	if ready.SafeMode || ready.Database != "ready" || ready.Version != "1.0.0" {
		t.Fatalf("%+v", ready)
	}
	Attach(binding, context.Background(), health.New("1.0.0", "/data", nil, true, "diag-1"))
	safe := binding.Get()
	if !safe.SafeMode || safe.Database != "safe" || safe.Diagnostic != "diag-1" {
		t.Fatalf("%+v", safe)
	}
	raw, _ := json.Marshal(safe)
	for _, leaked := range []string{"app.db", "SELECT", "sql.DB", "Cause"} {
		if strings.Contains(string(raw), leaked) {
			t.Fatalf("leaked %s: %s", leaked, raw)
		}
	}
}

func TestHealthBindingWithoutStartupContextDoesNotProbe(t *testing.T) {
	probe := &countingProbe{}
	binding := &HealthBinding{service: health.New("1.0.0", "/data", probe, false, "")}

	got := binding.Get()

	if !got.SafeMode || got.Database != "safe" {
		t.Fatalf("snapshot = %+v", got)
	}
	if probe.calls != 0 {
		t.Fatalf("probe calls = %d, want 0", probe.calls)
	}
}
