package secrets

import (
	"context"
	"errors"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// fakeStore is a test-only in-memory Store implementation. It must never be
// imported by production code.
type fakeStore struct {
	entries    map[string][]byte
	available  bool
	putErr     error
	resolveErr error
}

func newFakeStore(available bool) *fakeStore {
	return &fakeStore{entries: map[string][]byte{}, available: available}
}

func (f *fakeStore) Put(_ context.Context, ref string, value []byte) error {
	if f.putErr != nil {
		return f.putErr
	}
	stored := make([]byte, len(value))
	copy(stored, value)
	f.entries[ref] = stored
	return nil
}

func (f *fakeStore) Resolve(_ context.Context, ref string) ([]byte, error) {
	if f.resolveErr != nil {
		return nil, f.resolveErr
	}
	value, ok := f.entries[ref]
	if !ok {
		return nil, errors.New("not found")
	}
	out := make([]byte, len(value))
	copy(out, value)
	return out, nil
}

func (f *fakeStore) Delete(_ context.Context, ref string) error {
	delete(f.entries, ref)
	return nil
}

func (f *fakeStore) Exists(_ context.Context, ref string) (bool, error) {
	_, ok := f.entries[ref]
	return ok, nil
}

func (f *fakeStore) DisplayHint(_ context.Context, ref string) (string, error) {
	value, ok := f.entries[ref]
	if !ok {
		return "", nil
	}
	if len(value) < 4 {
		return "****", nil
	}
	return "****" + string(value[len(value)-4:]), nil
}

func (f *fakeStore) Available() bool { return f.available }

func TestSetSecretStoresByReference(t *testing.T) {
	store := newFakeStore(true)
	service := NewService(store)
	if err := service.SetSecret(context.Background(), "prov-1", []byte("sk-test-1234")); err != nil {
		t.Fatalf("SetSecret: %v", err)
	}
	ref := SecretRef("prov-1")
	value, ok := store.entries[ref]
	if !ok || string(value) != "sk-test-1234" {
		t.Fatalf("secret not stored under ref %q", ref)
	}
}

func TestSetSecretRejectsInvalidInput(t *testing.T) {
	service := NewService(newFakeStore(true))
	ctx := context.Background()
	if err := service.SetSecret(ctx, "", []byte("v")); err == nil {
		t.Fatal("empty provider ID accepted")
	}
	if err := service.SetSecret(ctx, "UPPER", []byte("v")); err == nil {
		t.Fatal("uppercase provider ID accepted")
	}
	if err := service.SetSecret(ctx, "prov:../evil", []byte("v")); err == nil {
		t.Fatal("path-like provider ID accepted")
	}
	if err := service.SetSecret(ctx, "prov", nil); err == nil {
		t.Fatal("empty secret accepted")
	}
}

func TestStatusNeverContainsValue(t *testing.T) {
	store := newFakeStore(true)
	service := NewService(store)
	if err := service.SetSecret(context.Background(), "prov-1", []byte("sk-secret-value-9999")); err != nil {
		t.Fatalf("SetSecret: %v", err)
	}
	status := service.Status(context.Background(), "prov-1")
	if !status.Configured {
		t.Fatal("status should be configured")
	}
	if status.DisplayHint == "" {
		t.Fatal("expected display hint from fake store")
	}
	// DisplayHint may contain at most a short suffix, never the full value.
	if len(status.DisplayHint) >= len("sk-secret-value-9999") {
		t.Fatalf("display hint too long: %q", status.DisplayHint)
	}
}

func TestStatusFailClosedWhenUnavailable(t *testing.T) {
	service := NewService(newFakeStore(false))
	status := service.Status(context.Background(), "prov-1")
	if status.Configured {
		t.Fatal("unavailable store must report configured=false")
	}
	if status.Available {
		t.Fatal("available flag must be false")
	}
}

func TestResolveInternalFailClosed(t *testing.T) {
	unavailable := NewService(newFakeStore(false))
	if _, err := unavailable.ResolveInternal(context.Background(), "prov-1"); err == nil {
		t.Fatal("resolve must fail on unavailable store")
	} else if providerErr, _ := provider.AsProviderError(err); providerErr.Category != provider.CategorySecurity {
		t.Fatalf("expected security category, got %q", providerErr.Category)
	}

	empty := NewService(newFakeStore(true))
	if _, err := empty.ResolveInternal(context.Background(), "prov-1"); err == nil {
		t.Fatal("resolve must fail when no secret is configured")
	}
}

func TestResolveInternalReturnsValue(t *testing.T) {
	store := newFakeStore(true)
	service := NewService(store)
	if err := service.SetSecret(context.Background(), "prov-1", []byte("sk-live")); err != nil {
		t.Fatalf("SetSecret: %v", err)
	}
	value, err := service.ResolveInternal(context.Background(), "prov-1")
	if err != nil {
		t.Fatalf("ResolveInternal: %v", err)
	}
	if string(value) != "sk-live" {
		t.Fatalf("resolved %q, want sk-live", value)
	}
}

func TestDeleteSecret(t *testing.T) {
	store := newFakeStore(true)
	service := NewService(store)
	if err := service.SetSecret(context.Background(), "prov-1", []byte("v")); err != nil {
		t.Fatalf("SetSecret: %v", err)
	}
	if err := service.DeleteSecret(context.Background(), "prov-1"); err != nil {
		t.Fatalf("DeleteSecret: %v", err)
	}
	if _, ok := store.entries[SecretRef("prov-1")]; ok {
		t.Fatal("secret still present after delete")
	}
	// Deleting again is not an error.
	if err := service.DeleteSecret(context.Background(), "prov-1"); err != nil {
		t.Fatalf("second DeleteSecret: %v", err)
	}
}

func TestStoreErrorsMapToProviderError(t *testing.T) {
	store := newFakeStore(true)
	store.putErr = errors.New("backend blew up")
	service := NewService(store)
	err := service.SetSecret(context.Background(), "prov-1", []byte("v"))
	if err == nil {
		t.Fatal("expected error")
	}
	providerErr, ok := provider.AsProviderError(err)
	if !ok {
		t.Fatalf("expected provider.Error, got %T", err)
	}
	if providerErr.Category != provider.CategoryStorage {
		t.Fatalf("category = %q, want storage", providerErr.Category)
	}
	if providerErr.SafeMessage == "" || providerErr.SafeMessage == "backend blew up" {
		t.Fatalf("unsafe or empty message: %q", providerErr.SafeMessage)
	}
}

func TestValidProviderID(t *testing.T) {
	valid := []string{"a", "prov-1", "openai-main", "0123456789-abcdef"}
	invalid := []string{"", "UPPER", "with space", "with/slash", "with:colon", "with.dot", string(make([]byte, 65))}
	for _, id := range valid {
		if !ValidProviderID(id) {
			t.Errorf("ValidProviderID(%q) = false, want true", id)
		}
	}
	for _, id := range invalid {
		if ValidProviderID(id) {
			t.Errorf("ValidProviderID(%q) = true, want false", id)
		}
	}
}
