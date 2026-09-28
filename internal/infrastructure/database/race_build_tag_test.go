//go:build race

package database

// raceBuild is true when the tests compile under `go test -race` — the
// toolchain defines the <race> tag for that build only.
const raceBuild = true
