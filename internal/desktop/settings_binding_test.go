package desktop

import (
	"context"
	"sync"
	"testing"
)

// settings_binding_test.go is RP-09.2's binding contract: the settings
// surface reads the scheduler's documented defaults before any user writes,
// refuses hostile values at the boundary, and a save under the CURRENT
// revision persists while a stale revision conflicts.

type rp09SettingsStore struct {
	mu       sync.Mutex
	values   map[string]string
	revision map[string]int64
}

func newRP09SettingsStore() *rp09SettingsStore {
	return &rp09SettingsStore{values: map[string]string{}, revision: map[string]int64{}}
}

func (s *rp09SettingsStore) get(_ context.Context, key string) (string, int64, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[key]
	if !ok {
		return "", 0, false, nil
	}
	return value, s.revision[key], true, nil
}

func (s *rp09SettingsStore) set(_ context.Context, key, value string, expectedRevision int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.revision[key]
	if !ok {
		current = 0
	}
	if current != expectedRevision {
		return context.Canceled // any error: the caller maps it to a conflict
	}
	s.values[key] = value
	s.revision[key] = current + 1
	return nil
}

func newRP09SettingsBinding() (*SettingsBinding, *rp09SettingsStore) {
	store := newRP09SettingsStore()
	binding := &SettingsBinding{}
	AttachSettings(binding, context.Background(), store.get, store.set)
	return binding, store
}

// TestRP09AutoBackupDefaultsBeforeAnyWrite: a fresh install answers the
// scheduler's documented defaults (daily, keep 3) with revision 0, which is
// the create-path revision the first save uses.
func TestRP09AutoBackupDefaultsBeforeAnyWrite(t *testing.T) {
	binding, _ := newRP09SettingsBinding()
	settings, revision, err := binding.GetAutoBackup()
	if err != nil {
		t.Fatal(err)
	}
	if settings.IntervalHours != 24 || settings.Retain != 3 {
		t.Fatalf("defaults = %+v, want 24h / 3", settings)
	}
	if revision != 0 {
		t.Fatalf("revision = %d, want 0 for the never-written setting", revision)
	}
}

// TestRP09AutoBackupSetThenGetPersisted drives save → read-back.
func TestRP09AutoBackupSetThenGetPersisted(t *testing.T) {
	binding, _ := newRP09SettingsBinding()

	if err := binding.SetAutoBackup(AutoBackupSettings{IntervalHours: 12, Retain: 5}, 0); err != nil {
		t.Fatalf("create: %v", err)
	}
	settings, _, err := binding.GetAutoBackup()
	if err != nil {
		t.Fatal(err)
	}
	if settings.IntervalHours != 12 || settings.Retain != 5 {
		t.Fatalf("read-back = %+v, want 12h / 5", settings)
	}
}

// TestRP09AutoBackupRefusesHostileValues is the boundary validation: a
// negative interval, a zero retention and an absurd retention are all
// refused BEFORE the store sees them.
func TestRP09AutoBackupRefusesHostileValues(t *testing.T) {
	binding, store := newRP09SettingsBinding()

	hostile := []AutoBackupSettings{
		{IntervalHours: -5, Retain: 3},
		{IntervalHours: 24, Retain: 0},
		{IntervalHours: 24, Retain: 1000},
		{IntervalHours: 24 * 365 * 2, Retain: 3},
	}
	for _, settings := range hostile {
		if err := binding.SetAutoBackup(settings, 0); err == nil {
			t.Fatalf("hostile settings %+v were accepted", settings)
		}
	}
	if len(store.values) != 0 {
		t.Fatalf("hostile settings persisted rows: %v", store.values)
	}
}

// TestRP09AutoBackupRevisionConflict refuses a lost update: two saves from
// the same read, the second must conflict rather than silently overwrite.
func TestRP09AutoBackupRevisionConflict(t *testing.T) {
	binding, _ := newRP09SettingsBinding()

	if err := binding.SetAutoBackup(AutoBackupSettings{IntervalHours: 24, Retain: 3}, 0); err != nil {
		t.Fatal(err)
	}
	_, revision, err := binding.GetAutoBackup()
	if err != nil {
		t.Fatal(err)
	}
	// A save at the CURRENT revision succeeds; a second save at the SAME
	// (now stale) revision conflicts.
	if err := binding.SetAutoBackup(AutoBackupSettings{IntervalHours: 12, Retain: 4}, revision); err != nil {
		t.Fatalf("update at the current revision: %v", err)
	}
	if err := binding.SetAutoBackup(AutoBackupSettings{IntervalHours: 6, Retain: 2}, revision); err == nil {
		t.Fatal("a stale-revision save was accepted")
	}
}

// TestRP09ParseAutoBackupDisabledAndHostile covers the scheduler-side loader:
// a disabled setting yields a zero interval, a hostile one falls back to the
// documented defaults, and a missing one is the defaults too.
func TestRP09ParseAutoBackupDisabledAndHostile(t *testing.T) {
	interval, retain := ParseAutoBackupSettings(func() (string, bool) { return "", false })
	if interval != 24*3600*1e9 || retain != 3 {
		t.Fatalf("missing setting = %v / %d, want the daily / 3 defaults", interval, retain)
	}
	interval, retain = ParseAutoBackupSettings(func() (string, bool) { return `{"intervalHours":0,"retain":4}`, true })
	if interval != 0 {
		t.Fatalf("a disabled setting = %v, want zero (disabled)", interval)
	}
	if retain != 4 {
		t.Fatalf("retain = %d, want the user's 4", retain)
	}
	interval, retain = ParseAutoBackupSettings(func() (string, bool) { return `{"intervalHours":-3,"retain":0}`, true })
	if interval != 24*3600*1e9 || retain != 3 {
		t.Fatalf("a hostile setting = %v / %d, want the safe defaults", interval, retain)
	}
}
