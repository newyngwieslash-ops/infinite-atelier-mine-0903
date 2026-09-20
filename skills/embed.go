// Package skills embeds the built-in Agent Packs.
//
// The packs are embedded rather than read from disk because a packaged desktop build has
// no repository tree beside it: a runtime path lookup would work in development and fail
// in the installer, which is the worst moment to find out. schemas/embed.go makes the
// same choice for the same reason, and this package mirrors its shape deliberately so a
// reader who has seen one has seen both.
//
// The documents are prompt material, not data: AGENT_CONTRACTS section 4.3's thirteen
// sections per agent, and section 19's inventory of which agents each pack carries.
// internal/application/skill loads and validates them, and internal/application/agentassembly
// turns a loaded pack into registry entries and a skill-version row.
package skills

import (
	"embed"
	"io/fs"
)

// packs holds every built-in pack.
//
// The directive covers the whole tree so a document added to a pack travels with the
// build without a second edit here. What names the documents is each pack's manifest,
// and a manifest naming a file that was not embedded is refused by the loader — so a
// forgotten file is a startup refusal rather than a prompt missing a section.
//
//go:embed all:script all:production
var packs embed.FS

// PackNames are the built-in packs, from AGENT_CONTRACTS section 19's inventory.
var PackNames = []string{"script", "production"}

// Sub returns one pack's files, rooted at the pack.
//
// The sub-filesystem is what a caller passes to skill.FS, so a manifest's paths are
// relative to its own pack: `execution/event_extraction.md` means that file in the pack
// it was read from, and two packs may use the same relative name without colliding.
func Sub(name string) (fs.FS, error) {
	return fs.Sub(packs, name)
}

// FS returns the embedded tree, rooted at the directory holding the packs.
//
// It exists so a test can walk everything the build carries: a pack directory added to
// the embed directive and forgotten in PackNames would otherwise be invisible, and this
// is what makes it visible.
func FS() fs.FS {
	return packs
}
