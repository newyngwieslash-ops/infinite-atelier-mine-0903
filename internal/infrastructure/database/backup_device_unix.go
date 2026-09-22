//go:build !windows

package database

import "syscall"

// syscallDeviceOf reads a POSIX device number from a `FileInfo.Sys()`.
//
// It lives behind a build tag because `syscall.Stat_t` does not exist on Windows — the
// tagged FILE is the platform difference, and the device-tag choice above is the only
// thing in it. On Windows `syscallDeviceUnavailable` answers instead.
func syscallDeviceOf(sys any) string {
	stat, ok := sys.(*syscall.Stat_t)
	if !ok {
		return ""
	}
	return itoaDevice(uint64(stat.Dev))
}
