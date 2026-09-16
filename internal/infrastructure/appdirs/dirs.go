// Package appdirs resolves and creates the application's private filesystem layout.
package appdirs

import (
	"os"
	"path/filepath"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

const applicationDirectory = "InfiniteAtelier"

// Dirs contains the managed filesystem locations used by the application.
type Dirs struct {
	Root      string
	Database  string
	Files     string
	Temp      string
	Logs      string
	Snapshots string
}

// Ensure resolves and creates the managed private directory layout.
// The override exists for trusted composition and tests; it is never a frontend input.
func Ensure(override string) (Dirs, error) {
	root := override
	if root == "" {
		configRoot, err := os.UserConfigDir()
		if err != nil {
			return Dirs{}, directoryError(err)
		}
		root = filepath.Join(configRoot, applicationDirectory)
	}

	root = filepath.Clean(root)
	dirs := Dirs{
		Root:      root,
		Database:  filepath.Join(root, "app.db"),
		Files:     filepath.Join(root, "files"),
		Temp:      filepath.Join(root, "temp"),
		Logs:      filepath.Join(root, "logs"),
		Snapshots: filepath.Join(root, "snapshots"),
	}

	for _, path := range []string{dirs.Root, dirs.Files, dirs.Temp, dirs.Logs, dirs.Snapshots} {
		if err := ensurePrivateDirectory(path); err != nil {
			return Dirs{}, directoryError(err)
		}
	}

	return dirs, nil
}

func ensurePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return os.ErrInvalid
	}
	return nil
}

func directoryError(cause error) error {
	return apperror.New(
		"APP_DIR_CREATE_FAILED",
		"storage",
		false,
		"Application storage could not be prepared.",
		cause,
	)
}
