package database

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// deviceOf reports the filesystem a path lives on, as a value that is EQUAL for two paths
// on one filesystem and DIFFERENT across two. It is what `assertSameFilesystem` compares,
// because `os.Rename` is atomic within one filesystem and fails across two.
//
// # Why this is not `os.SameFile`
//
// `os.SameFile` compares inode identity, so it answers "is this the same file". Two
// directories on one disk are not the same file, so a check built on it refuses every
// restore. An earlier draft of `assertSameFilesystem` did exactly that, and the comment
// there records it because the mistake is easy: the question a rename asks is about the
// FILESYSTEM, not about the files.
//
// # Why the answer comes from the PATH and not from a syscall
//
// The obvious field is `syscall.Stat_t.Dev` — and it does not exist on Windows, which is
// the platform this application ships for. `go doc syscall.Stat_t` on this host answers
// "no symbol", so a build-tagged Unix version would leave the Windows half unable to
// answer at all, and an earlier attempt at that shape returned "unknown" there, which
// would have failed every restore on the shipping platform.
//
// A path's volume is instead taken from the path itself: a drive letter or a UNC share
// root on Windows, and the mount point on Unix. That is the same question a rename
// answers, it needs no syscall, and it is testable on any host.
func deviceOf(path string) (string, bool) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	if runtime.GOOS == "windows" {
		return windowsVolumeOf(absolute), true
	}
	return unixVolumeOf(absolute), true
}

// windowsVolumeOf returns a path's volume root: `C:\` or the UNC share root.
//
// Two paths under one share can be renamed between; two shares cannot. A UNC path's
// volume is the `\server\share` prefix, which is what Windows itself compares for a
// cross-volume check.
func windowsVolumeOf(path string) string {
	slashed := strings.ReplaceAll(path, "/", `\`)
	if strings.HasPrefix(slashed, `\`) {
		trimmed := strings.TrimPrefix(slashed, `\`)
		parts := strings.SplitN(trimmed, `\`, 3)
		if len(parts) >= 2 {
			return `\` + parts[0] + `\` + parts[1]
		}
		return `\` + trimmed
	}
	if len(slashed) >= 2 && slashed[1] == ':' {
		return strings.ToUpper(slashed[:2]) + `\`
	}
	return strings.ToUpper(slashed)
}

// unixVolumeOf returns a path's mount root: the shortest prefix that is a mount point.
//
// A mount point is a directory whose parent is on a different device, and `os.Stat`'s
// `Sys()` is a `*syscall.Stat_t` on Unix, where `Dev` is available. Where it is not —
// Windows, or a filesystem that reports nothing — the answer degrades to `/`, which
// compares EQUAL for every path and therefore ALLOWS the promotion rather than refusing
// it. That direction is deliberate: on a platform where this build cannot tell mounts
// apart, refusing every restore would be worse than attempting one that the operating
// system will refuse if the two really are on different volumes.
func unixVolumeOf(path string) string {
	current := path
	for parent := filepath.Dir(current); parent != current; parent = filepath.Dir(current) {
		if statDevice(current) != statDevice(parent) {
			return current
		}
		current = parent
	}
	return "/"
}

// statDevice reads a path's device number where the platform has one, and returns an
// empty string where it does not.
func statDevice(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	// `FileInfo.Sys()` returns one value, which may be nil on a filesystem that reports
	// nothing — the check is for nil rather than for a boolean.
	if sys := info.Sys(); sys != nil {
		return syscallDeviceOf(sys)
	}
	return ""
}

// moveFileSet moves a database AND its write-ahead sidecars, as one set.
//
// # Why the sidecars are part of the set
//
// SQLite in WAL mode keeps recent committed transactions in `<db>-wal` and its shared-memory
// index in `<db>-shm`. The `.db` file alone is the last CHECKPOINT, not the database. A
// promotion that moved only the `.db` would lose everything committed since that checkpoint
// — and worse, it would leave a `-wal` belonging to the OLD database beside the RESTORED
// one, which SQLite would replay against it. That is silent corruption of the data the user
// is trying to recover.
//
// # Why they are moved rather than deleted
//
// The caller closes the pool first, which checkpoints and normally empties the WAL. But a
// checkpoint that could not complete leaves the sidecars as the only copy of the newest
// commits, and this code cannot tell the two cases apart from the file names. Moving them
// preserves both possibilities: a successful checkpoint leaves them small or absent, and a
// failed one keeps the data with the database it belongs to.
//
// # What it does NOT do
//
// It is not a general move: it takes a database path, not a directory, and it ignores a
// sidecar it cannot rename rather than failing the whole promotion on it. A missing sidecar
// is the ordinary state of a cleanly closed database, and refusing a promotion for one
// would refuse the case that works.
func moveFileSet(databasePath, targetBase string) error {
	if err := os.Rename(databasePath, targetBase); err != nil {
		if os.IsNotExist(err) {
			// A live database that is not there is a state rather than a failure: a fresh
			// install has not written one yet, and a promotion displacing nothing is what
			// the caller asked for.
			return nil
		}
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		source := databasePath + suffix
		if !fileExists(source) {
			continue
		}
		// The held name keeps the suffix, so a rollback can put the set back under the names
		// SQLite expects to find.
		if err := os.Rename(source, targetBase+suffix); err != nil {
			// The database itself moved, so the set is half-moved. That is worth reporting:
			// a caller that continued would leave a WAL behind that the next open would
			// replay against the wrong file.
			return err
		}
	}
	return nil
}
