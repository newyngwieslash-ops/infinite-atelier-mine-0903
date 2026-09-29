// Package agentassembly turns the built-in skill packs and the tool table into a live
// agent registry, and owns the skill-version records the runs cite.
//
// It exists so the composition root has one call to make and the runtime has nothing to
// learn about packs. Three things happen here that must happen in one place:
//
//   - THE PACKS ARE EMBEDDED, not read from disk. A packaged desktop build has no
//     repository tree beside it, so a runtime path lookup would work in development and
//     fail in the installer — the worst moment to find out. schemas/embed.go makes the
//     same choice for the same reason.
//   - THE MANIFESTS ARE VALIDATED AGAINST THE REAL TOOL TABLE. AGENT_CONTRACTS section
//     4.2's "Tool Key 必须来自内置注册表" is checked by the loader against the keys this
//     build actually registers, so a pack naming a tool that was renamed is refused at
//     startup rather than at the stage that needed it.
//   - THE SKILL VERSIONS ARE REGISTERED. Section 4.2 requires a run to name the exact
//     version it ran, so every load mints a version row whose content hash is the hash
//     of the manifest AND the documents. Loading the same bytes twice is the same
//     version, which is what makes a run reproducible.
package agentassembly

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/files"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/skill"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/schemas"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/skills"
)

// BuiltinPacks are the pack directories this build carries, from AGENT_CONTRACTS
// section 19's inventory.
//
// The packs themselves are embedded by the skills package, which mirrors the shape of
// schemas/embed.go: an embed directive cannot reach outside its own package's directory,
// so the tree lives in the package that declares it and this one reads through it.
var BuiltinPacks = skills.PackNames

// Loaded is a pack that was read and validated, with the version row it produced.
type Loaded struct {
	Name string
	// Pack is what the loader read: the manifest, the documents and the specs.
	Pack skill.LoadedPack
	// SkillVersionID is the row minted for this load, which a run cites.
	SkillVersionID string
}

// SkillVersionStore is the write and read surface this package needs for versions.
//
// One method each: an assembly registers what it loaded and can look one up again. The
// repository implements it; a test passes a double.
type SkillVersionStore interface {
	RegisterSkillVersion(ctx context.Context, version agent.SkillVersion) error
	SkillVersionByKey(ctx context.Context, skillKey string) (agent.SkillVersion, error)
}

// ContentStore stores a pack's documents and returns their content address.
//
// It is the file service's import, declared here as an interface rather than imported as
// the concrete service, because this package needs exactly one method and a narrower
// dependency is what keeps the assembly testable without a file store. The value it
// returns is a CONTENT HASH, which is what makes the reference stable: importing the same
// bytes twice produces the same identifier, so a version's documents do not accumulate a
// copy per start.
type ContentStore interface {
	Import(ctx context.Context, displayName string, body io.Reader) (files.Object, error)
}

// Options configures an assembly.
type Options struct {
	Tools *agentruntime.Tools
	// Files stores each pack's documents. A nil store is refused rather than worked
	// around: a skill version whose ContentFileID was invented would cite a file that
	// does not exist, and the domain refuses an empty one for exactly that reason.
	Files ContentStore
	// Versions stores the skill versions. A nil store is refused by Build rather than
	// silently skipping the registration: a run that could not cite a version would
	// fail at CreateRun, and saying so here names the cause.
	Versions SkillVersionStore
	// Clock and IDs are the determinism ports every application service here has.
	Clock agentruntime.Clock
	IDs   agentruntime.IDGenerator
}

// Assembly is the built registry and the packs behind it.
type Assembly struct {
	registry *agentruntime.Registry
	packs    map[string]Loaded
	// toolKeys is the table's key set, kept so a caller can ask what a pack may name.
	toolKeys map[string]bool
	// disabled is the management surface's stop state (T09), keyed on agent
	// key and guarded like every other mutable field. A disabled agent's
	// SkillDocument answers false, which is the gate the runtime already
	// reads.
	mu       sync.RWMutex
	disabled map[string]bool
	// skillVersioning is RP-06.1's version-management surface (create from
	// version, rollback by pointer switch, one-call run snapshot). Nil for an
	// assembly built without the management store: the surface answers
	// "unavailable" rather than pretending.
	skillVersioning *skillVersioning
}

// Build loads every built-in pack, registers its skill versions, and assembles the
// registry.
//
// It refuses the whole build on any invalid pack rather than dropping it. That is the
// fail-closed direction and the one section 3 asks for: a build whose script pack failed
// to load must not start with a decision agent that is silently absent, because the
// stage that needed it would then run with no agent at all.
func Build(ctx context.Context, options Options) (*Assembly, error) {
	if options.Tools == nil {
		return nil, agent.UnavailableError()
	}
	if options.Versions == nil {
		return nil, agent.InvalidError("The skill versions have nowhere to be registered.")
	}
	if options.Files == nil {
		return nil, agent.InvalidError("The skill documents have nowhere to be stored.")
	}
	if options.IDs == nil || options.Clock == nil {
		return nil, agent.InvalidError("An assembly needs a clock and an identifier source.")
	}
	known := map[string]bool{}
	for _, key := range options.Tools.Keys() {
		known[key] = true
	}
	assembly := &Assembly{packs: map[string]Loaded{}, toolKeys: known, disabled: map[string]bool{}}
	specs := make([]agent.Spec, 0, 16)
	for _, name := range BuiltinPacks {
		sub, err := skills.Sub(name)
		if err != nil {
			return nil, agent.InvalidError("The built-in pack " + name + " is not embedded.")
		}
		loaded, err := skill.Load(skill.LoadOptions{
			Source:       skill.FS(sub),
			ManifestPath: "manifest.json",
			// The tool table this build registers is what a manifest may name. Section
			// 4.2's rule is enforced here, against the keys that exist rather than
			// against a list written in the manifest.
			KnownToolKeys: known,
			// The schema paths a manifest names must be embedded. The loader checks them
			// against this set so a manifest pointing at a contract this build does not
			// carry is refused now rather than by a run whose output nothing validated.
			KnownSchemaPaths: embeddedAgentSchemaSet(),
		})
		if err != nil {
			return nil, err
		}
		contentFileID, err := storeContent(ctx, options, name, loaded)
		if err != nil {
			return nil, err
		}
		versionID, err := registerVersion(ctx, options, name, loaded, contentFileID)
		if err != nil {
			return nil, err
		}
		assembly.packs[name] = Loaded{Name: name, Pack: loaded, SkillVersionID: versionID}
		specs = append(specs, loaded.Specs...)
	}
	registry, err := agentruntime.NewRegistry(specs, options.Tools)
	if err != nil {
		return nil, err
	}
	assembly.registry = registry
	return assembly, nil
}

// registerVersion writes one pack's skill version and returns its identifier.
//
// The content hash is what makes a run reproducible: it covers the manifest AND every
// document, in a stable order the loader fixes. Registering the same bytes twice is
// reported as a conflict by the repository, which is not an error here — a second start
// of the same build is the normal case, and the row that exists IS the row this load
// produced. So a conflict is resolved by reading the existing version back, and the
// caller gets the identifier either way.
func registerVersion(ctx context.Context, options Options, name string, loaded skill.LoadedPack, contentFileID string) (string, error) {
	id, err := options.IDs.New()
	if err != nil {
		return "", agent.StorageError("The skill version could not be identified.", err)
	}
	version := agent.SkillVersion{
		ID:            id,
		SkillKey:      name,
		Version:       loaded.Manifest.Metadata.Version,
		ContentHash:   loaded.ContentHash,
		ManifestJSON:  manifestJSONOf(loaded),
		ContentFileID: contentFileID,
		Status:        agent.SkillActive,
		CreatedAt:     options.Clock.Now().UTC(),
	}
	if err := version.Validate(); err != nil {
		return "", err
	}
	if err := options.Versions.RegisterSkillVersion(ctx, version); err != nil {
		if domainErr, ok := agent.AsError(err); ok && domainErr.Category == agent.CategoryConflict {
			// The version is already registered. The one that exists is what a run must
			// cite, so it is read back rather than a second row being forced.
			existing, readErr := options.Versions.SkillVersionByKey(ctx, name)
			if readErr != nil {
				return "", readErr
			}
			if existing.ContentHash != loaded.ContentHash {
				// The same (key, version) pair with a DIFFERENT hash means the pack's
				// contents changed without its version being bumped. That is a defect
				// rather than a repeat load: two different documents would be cited as
				// one version, and a run's record could not tell which it ran.
				return "", agent.ConflictError(
					"The built-in pack " + name + " changed contents without its version being raised.")
			}
			return existing.ID, nil
		}
		return "", err
	}
	return id, nil
}

// storeContent puts a pack's documents in the file store and returns their address.
//
// The loader already concatenated the documents into one byte slice, in a stable order
// it fixes, which is what makes this address meaningful: the same pack bytes produce the
// same hash, so the reference a run cites names a specific set of documents rather than
// "whatever the pack said at the time".
//
// The display name is derived from the pack name and version rather than taken from a
// file path, because AGENTS section 8.4 forbids using a user's filename as a final path
// and a display name is what a person reads in a listing.
func storeContent(ctx context.Context, options Options, name string, loaded skill.LoadedPack) (string, error) {
	displayName := "skillpack-" + name + "-" + loaded.Manifest.Metadata.Version + ".md"
	object, err := options.Files.Import(ctx, displayName, bytes.NewReader(loaded.Content))
	if err != nil {
		return "", agent.StorageError("The skill documents could not be stored.", err)
	}
	if strings.TrimSpace(object.Hash) == "" {
		// A store that returned no hash would leave the version citing nothing, and the
		// domain refuses that. Reporting it here names the store rather than the version.
		return "", agent.StorageError("The skill documents were stored without an address.", nil)
	}
	return object.Hash, nil
}

// manifestJSONOf renders the manifest for storage.
//
// The loader publishes the PARSED manifest rather than its bytes, and it refuses any
// field it does not know (DisallowUnknownFields), so re-rendering the struct cannot drop
// something the pack declared: a field the struct does not have is a field the loader
// would have refused. That is why this is a re-render rather than a copy, and it is
// stated here because a copy would otherwise look like the safer choice.
func manifestJSONOf(loaded skill.LoadedPack) string {
	encoded, err := json.Marshal(loaded.Manifest)
	if err != nil {
		// The struct is a fixed shape of strings and slices, so this cannot fail; the
		// fallback is an empty object rather than a panic, because a panic here would
		// take the process down for a reason nothing else depends on.
		return "{}"
	}
	return strings.TrimSpace(string(encoded))
}

// Registry returns the assembled registry.
func (a *Assembly) Registry() *agentruntime.Registry {
	if a == nil {
		return nil
	}
	return a.registry
}

// Available reports whether the assembly can run agents.
func (a *Assembly) Available() bool {
	return a != nil && a.registry != nil && len(a.packs) > 0
}

// Pack returns one loaded pack by name.
func (a *Assembly) Pack(name string) (Loaded, bool) {
	if a == nil {
		return Loaded{}, false
	}
	pack, ok := a.packs[name]
	return pack, ok
}

// PackNames returns the loaded pack names, sorted by BuiltinPacks' order.
func (a *Assembly) PackNames() []string {
	if a == nil {
		return nil
	}
	names := make([]string, 0, len(a.packs))
	for _, name := range BuiltinPacks {
		if _, ok := a.packs[name]; ok {
			names = append(names, name)
		}
	}
	return names
}

// SkillDocument returns the document text for one agent key.
//
// The runtime does not read files, so the caller passes the text to an Invocation. This
// is where that text comes from: the pack's loaded documents, keyed by agent.
func (a *Assembly) SkillDocument(agentKey string) (string, bool) {
	if a == nil {
		return "", false
	}
	// THE STOP SWITCH (T09): a disabled agent answers false at the one read
	// the runtime already guards on, so a stopped agent's stage refuses at
	// the assembly rather than needing a second mechanism in the runtime.
	a.mu.RLock()
	disabled := a.disabled[agentKey]
	a.mu.RUnlock()
	if disabled {
		return "", false
	}
	for _, name := range BuiltinPacks {
		pack, ok := a.packs[name]
		if !ok {
			continue
		}
		if document, ok := pack.Pack.Skills[agentKey]; ok {
			return document, true
		}
	}
	return "", false
}

// SkillVersionOf returns the version identifier a run should cite for an agent.
//
// A run cites the version of the PACK that contains its agent, which is what makes the
// citation meaningful: the pack's hash covers every document in it, so a change to one
// document changes the version the whole pack's agents cite.
func (a *Assembly) SkillVersionOf(agentKey string) (string, bool) {
	if a == nil {
		return "", false
	}
	for _, name := range BuiltinPacks {
		pack, ok := a.packs[name]
		if !ok {
			continue
		}
		if _, ok := pack.Pack.Skills[agentKey]; ok {
			return pack.SkillVersionID, true
		}
	}
	return "", false
}

// embeddedAgentSchemaSet is the schema paths this build embeds, as a set.
//
// The loader checks a manifest's Input and Output against it, so a pack naming a
// contract this build does not carry is refused at startup rather than by a run whose
// output nothing validated. It reads schemas.AgentPaths — the same list the validation
// package compiles — so the two cannot disagree about what exists.
func embeddedAgentSchemaSet() map[string]bool {
	set := make(map[string]bool, len(schemas.AgentPaths))
	for _, path := range schemas.AgentPaths {
		set[path] = true
	}
	return set
}

// SetAgentEnabled starts or stops one agent by key (T09, FR-090).
//
// The state is in-memory for THIS build: FR-090's management surface asks the
// USER to stop and start agents, and a stopped agent resumes on the next app
// start unless the user stops it again — recorded here rather than persisted,
// because a persisted kill switch is a policy decision this work package does
// not make. An unknown key is refused against the registered set.
func (a *Assembly) SetAgentEnabled(agentKey string, enabled bool) error {
	if a == nil {
		return agent.InvalidError("The agent registry is unavailable.")
	}
	known := false
	for _, name := range BuiltinPacks {
		if pack, ok := a.packs[name]; ok {
			if _, ok := pack.Pack.Skills[agentKey]; ok {
				known = true
			}
		}
	}
	if !known {
		return agent.InvalidError("That agent key is not registered in this build.")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if enabled {
		delete(a.disabled, agentKey)
	} else {
		a.disabled[agentKey] = true
	}
	return nil
}

// AgentEnabled reports whether one agent is currently enabled (T09's readback).
func (a *Assembly) AgentEnabled(agentKey string) bool {
	if a == nil {
		return false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return !a.disabled[agentKey]
}

// DisabledAgents lists the stopped keys.
func (a *Assembly) DisabledAgents() []string {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	keys := make([]string, 0, len(a.disabled))
	for key := range a.disabled {
		keys = append(keys, key)
	}
	return keys
}
