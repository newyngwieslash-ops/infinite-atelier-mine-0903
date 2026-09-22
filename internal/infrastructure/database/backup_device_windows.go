//go:build windows

package database

// syscallDeviceOf always reports nothing on Windows, and that is the honest answer rather
// than a stub: Windows has no POSIX device number, and `deviceOf` takes the path-based
// route there (`windowsVolumeOf`) rather than calling this.
//
// It exists so the Unix file's call site compiles on both platforms without the caller
// branching, and it returning "" is what makes `unixVolumeOf` degrade to "/" if it is ever
// reached on Windows by mistake — which allows the promotion and lets the operating
// system refuse a cross-volume rename, rather than refusing every restore.
func syscallDeviceOf(any) string { return "" }
