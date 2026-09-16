package health

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type pingProbe struct{ err error }

func (p pingProbe) PingContext(context.Context) error { return p.err }

func TestSnapshotReady(t *testing.T) {
	svc := New("1.0.0", `C:\\Users\\example\\InfiniteAtelier`, pingProbe{}, false, "secret-cause")
	got := svc.Snapshot(context.Background())
	if got.Version != "1.0.0" || got.Database != "ready" || got.SafeMode || got.Diagnostic != "" || got.DataDirectory == "" {
		t.Fatalf("%+v", got)
	}
	if strings.Contains(strings.ToLower(got.DataDirectory), "app.db") {
		t.Fatal("database path leaked")
	}
}

func TestSnapshotSafeMode(t *testing.T) {
	svc := New("1.0.0", "/data", pingProbe{err: errors.New("sql: disk I/O C:\\private.db")}, true, "abc123")
	got := svc.Snapshot(context.Background())
	if !got.SafeMode || got.Database != "safe" || got.Diagnostic != "abc123" {
		t.Fatalf("%+v", got)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "private.db") || strings.Contains(string(raw), "sql:") {
		t.Fatalf("unsafe payload %s", raw)
	}
}
