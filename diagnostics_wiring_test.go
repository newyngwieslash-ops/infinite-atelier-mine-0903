package main

import (
	"context"
	"path/filepath"
	"testing"

	appdiagnostics "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/diagnostics"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/database"
)

// TestComposeDiagnosticsSuppliesAWorkingReader is the wiring assertion for FR-180's bundle.
//
// The service's reader is OPTIONAL by design — a build with no store must still show the interface —
// which means nothing forces a composition root to fill it. That is the shape this package's reviews
// keep finding: an interface with an implementation, a test, and no production caller. So this builds
// the real reader over a real database and asserts a bundle can be ASSEMBLED, not merely that the
// service exists.
func TestComposeDiagnosticsSuppliesAWorkingReader(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	handle, err := database.Open(ctx, filepath.Join(dir, "studio.db"), filepath.Join(dir, "snapshots"))
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}
	t.Cleanup(func() { _ = handle.Close(context.Background()) })

	// The reader the composition root builds, over the directories it has.
	reader := database.NewDiagnosticsReader(handle.SQL(), dir)
	service := appdiagnostics.NewService(appdiagnostics.Options{
		Reader:     reader,
		AppVersion: "test",
		Platform:   "windows/amd64",
	})
	if !service.Available() {
		t.Fatal("the composed service reports itself unavailable")
	}
	// A bundle over a REAL schema: the plan must be producible, which is what a reader that could not
	// answer a query would fail.
	plan, err := service.Plan(ctx, nil)
	if err != nil {
		t.Fatalf("planning a bundle over a real database: %v", err)
	}
	if len(plan.Entries) != len(appdiagnostics.Sections) {
		t.Fatalf("the plan lists %d sections, want %d", len(plan.Entries), len(appdiagnostics.Sections))
	}
	bundle, err := service.Assemble(ctx, nil)
	if err != nil {
		t.Fatalf("assembling a bundle over a real database: %v", err)
	}
	// Every included section is present, INCLUDING the log one — which is empty on a fresh
	// installation and must still be a file, or a reader would not know the section ran.
	for _, entry := range bundle.Manifest.Entries {
		if !entry.Included {
			continue
		}
		if _, present := bundle.Files[entry.Section]; !present {
			t.Fatalf("the bundle is missing the included section %q", entry.Section)
		}
	}
	// And the migrations section reports the real schema version rather than zero.
	migrations := string(bundle.Files[appdiagnostics.SectionMigrations])
	if !contains(migrations, "schema_version: ") {
		t.Fatalf("the migrations section carries no version:\n%s", migrations)
	}
	if contains(migrations, "schema_version: 0\n") {
		t.Fatalf("the migrations section reports version 0 over a migrated database:\n%s", migrations)
	}
}

// contains is a small helper so this file does not import strings for one call.
func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
