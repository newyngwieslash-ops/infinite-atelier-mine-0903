//go:build !race

package database

// raceBuild is false in a normal build; performance bounds are only
// meaningful here (T24).
const raceBuild = false
