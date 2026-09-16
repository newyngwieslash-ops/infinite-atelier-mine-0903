//go:build windows

package secretstore

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

// ownedTestRef builds a unique target prefix for this test run. Tests must
// only create and delete entries under their own prefix and never enumerate
// or touch the user's credentials.
func ownedTestRef(t *testing.T, name string) string {
	t.Helper()
	return fmt.Sprintf("InfiniteAtelier:test:%s:%d:%s", t.Name(), os.Getpid(), name)
}

func TestWindowsStorePutResolveDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("credential manager test requires OS interaction")
	}
	store := NewWindowsStore()
	ctx := context.Background()
	ref := ownedTestRef(t, "roundtrip")
	t.Cleanup(func() {
		_ = store.Delete(ctx, ref)
	})

	if err := store.Put(ctx, ref, []byte("sk-test-credential-value")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	exists, err := store.Exists(ctx, ref)
	if err != nil || !exists {
		t.Fatalf("Exists: %v %v", exists, err)
	}
	value, err := store.Resolve(ctx, ref)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if string(value) != "sk-test-credential-value" {
		t.Fatalf("resolved %q", value)
	}
	hint, err := store.DisplayHint(ctx, ref)
	if err != nil {
		t.Fatalf("DisplayHint: %v", err)
	}
	if !strings.HasPrefix(hint, "****") || len(hint) > 8 {
		t.Fatalf("unexpected hint %q", hint)
	}
	if err := store.Delete(ctx, ref); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	exists, err = store.Exists(ctx, ref)
	if err != nil || exists {
		t.Fatalf("still exists after delete: %v %v", exists, err)
	}
}

func TestWindowsStoreOverwriteReplacesValue(t *testing.T) {
	if testing.Short() {
		t.Skip("credential manager test requires OS interaction")
	}
	store := NewWindowsStore()
	ctx := context.Background()
	ref := ownedTestRef(t, "overwrite")
	t.Cleanup(func() {
		_ = store.Delete(ctx, ref)
	})
	if err := store.Put(ctx, ref, []byte("first-value")); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, ref, []byte("second-value")); err != nil {
		t.Fatal(err)
	}
	value, err := store.Resolve(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if string(value) != "second-value" {
		t.Fatalf("overwrite failed: %q", value)
	}
}

func TestWindowsStoreRejectsInvalidInput(t *testing.T) {
	store := NewWindowsStore()
	ctx := context.Background()
	if err := store.Put(ctx, "", []byte("v")); err == nil {
		t.Fatal("empty ref accepted")
	}
	if err := store.Put(ctx, ownedTestRef(t, "empty"), nil); err == nil {
		t.Fatal("empty value accepted")
	}
	oversized := make([]byte, maxCredentialBlob+1)
	if err := store.Put(ctx, ownedTestRef(t, "big"), oversized); err == nil {
		t.Fatal("oversized value accepted")
	}
}

func TestWindowsStoreMissingResolveFailsClosed(t *testing.T) {
	if testing.Short() {
		t.Skip("credential manager test requires OS interaction")
	}
	store := NewWindowsStore()
	ctx := context.Background()
	ref := ownedTestRef(t, "missing")
	if _, err := store.Resolve(ctx, ref); err == nil {
		t.Fatal("missing credential resolved")
	}
}
