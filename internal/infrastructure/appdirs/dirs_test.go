package appdirs

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

func TestEnsureCreatesPrivateLayout(t *testing.T) {
	root := filepath.Join(t.TempDir(), "application root")
	dirs, err := Ensure(root)
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{dirs.Root, dirs.Files, dirs.Temp, dirs.Logs, dirs.Snapshots} {
		info, statErr := os.Stat(path)
		if statErr != nil || !info.IsDir() {
			t.Fatalf("missing directory %s: %v", path, statErr)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("directory %s is not private: %o", path, info.Mode().Perm())
		}
	}

	if filepath.Dir(dirs.Database) != dirs.Root {
		t.Fatalf("database escaped root: %s", dirs.Database)
	}
	if filepath.Base(dirs.Database) != "app.db" {
		t.Fatalf("database filename = %q", filepath.Base(dirs.Database))
	}
}

func TestEnsureRejectsRootThatIsAFileWithoutLeakingPath(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "private-root-name")
	if err := os.WriteFile(root, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Ensure(root)
	if err == nil {
		t.Fatal("Ensure succeeded with a file as its root")
	}

	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error type = %T, want *apperror.Error", err)
	}
	if appErr.Code != "APP_DIR_CREATE_FAILED" || appErr.Retriable {
		t.Fatalf("unexpected stable error fields: %#v", appErr)
	}
	if strings.Contains(err.Error(), root) || strings.Contains(appErr.SafeMessage, root) {
		t.Fatal("safe error exposed an absolute path")
	}
}
