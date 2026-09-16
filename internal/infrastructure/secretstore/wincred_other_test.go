//go:build !windows

package secretstore

import (
	"context"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// These tests document the non-Windows contract: every secret operation fails
// closed and there is no plaintext fallback. They are cross-compiled and run
// on any non-Windows host; on Windows the WindowsStore contract tests apply.
func TestUnavailableStoreFailsClosed(t *testing.T) {
	store := NewUnavailableStore()
	ctx := context.Background()

	if store.Available() {
		t.Fatal("unavailable store reported Available()=true")
	}
	if err := store.Put(ctx, "InfiniteAtelier:provider:p1", []byte("v")); err == nil {
		t.Fatal("Put succeeded on an unavailable store")
	} else if providerErr, ok := provider.AsProviderError(err); !ok || providerErr.Category != provider.CategorySecurity {
		t.Fatalf("Put error = %v, want security category", err)
	}
	if _, err := store.Resolve(ctx, "InfiniteAtelier:provider:p1"); err == nil {
		t.Fatal("Resolve succeeded on an unavailable store")
	}
	if err := store.Delete(ctx, "InfiniteAtelier:provider:p1"); err == nil {
		t.Fatal("Delete succeeded on an unavailable store")
	}
	exists, err := store.Exists(ctx, "InfiniteAtelier:provider:p1")
	if err != nil || exists {
		t.Fatalf("Exists = %v, %v; want false, nil", exists, err)
	}
	hint, err := store.DisplayHint(ctx, "InfiniteAtelier:provider:p1")
	if err != nil || hint != "" {
		t.Fatalf("DisplayHint = %q, %v; want empty, nil", hint, err)
	}
}
