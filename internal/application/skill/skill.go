// Package skill loads an Agent Pack's manifest and its skill documents, and
// records the immutable version a run is bound to (AGENT_CONTRACTS section 4).
//
// Three properties the specification requires and this package enforces:
//
//   - A MANIFEST IS DATA, NEVER CODE. Section 4.2's constraints say a manifest may
//     contain only relative paths, no code, no command and no URL that executes.
//     Loading therefore validates the whole manifest and refuses it as a unit
//     rather than skipping what it does not understand.
//   - A TOOL KEY MUST ALREADY EXIST. "Tool Key 必须来自内置注册表" — the loader is
//     given the registry's keys and refuses a manifest naming anything else, so a
//     pack cannot grant itself a capability by naming one.
//   - A RUN IS BOUND TO A VERSION. Section 13.4 makes a skill version immutable and
//     addressed by content hash, and a run names the version it ran. Loading
//     therefore produces a SkillVersion row, and a later edit to the files
//     produces a different hash rather than changing what a past run recorded.
package skill

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
)

// APIVersion is the manifest schema this loader understands.
const APIVersion = "atelier.agent/v1"

// MaxManifestBytes bounds one manifest, so a hostile pack cannot make the loader
// read an unbounded file.
const MaxManifestBytes = 1 << 20

// MaxSkillDocumentBytes bounds one skill document. Section 5.3 makes a skill a
// prompt layer, and a layer larger than the context it assembles is a mistake.
const MaxSkillDocumentBytes = 256 * 1024

// DefaultMaxPackBytes bounds every document of one pack together.
const DefaultMaxPackBytes = 4 << 20

// Manifest is one Agent Pack's declaration (AGENT_CONTRACTS section 4.2).
//
// The field names are the specification's, and the JSON tags are lower camel so
// the file reads the way the specification's example does. Decoding is strict:
// an unknown field is refused rather than ignored, because a pack written against
// a later revision of the format must fail loudly instead of running without the
// field it expected to matter.
type Manifest struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   ManifestMeta    `json:"metadata"`
	Agents     []ManifestAgent `json:"agents"`
}

// ManifestMeta names and versions the pack.
type ManifestMeta struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ManifestAgent is one agent's registration inside a pack.
type ManifestAgent struct {
	Key     string   `json:"key"`
	Layer   string   `json:"layer"`
	Skill   string   `json:"skill"`
	Input   string   `json:"inputSchema"`
	Output  string   `json:"outputSchema"`
	Allowed []string `json:"allowedTools"`
	Limits  Limits   `json:"limits"`
}

// Limits is an agent's declared bounds.
type Limits struct {
	MaxToolCalls   int `json:"maxToolCalls"`
	TimeoutSeconds int `json:"timeoutSeconds"`
}

// LoadedPack is a manifest that passed validation, with its documents read.
type LoadedPack struct {
	Manifest Manifest
	// Skills maps an agent key to the document text its skill field named.
	Skills map[string]string
	// ContentHash is the hash of the manifest and every document, in a stable
	// order, so the same pack bytes always produce the same version hash.
	ContentHash string
	// Content is the concatenated documents, which is what a version's file
	// reference stores.
	Content []byte
	// Specs are the domain registrations the manifest describes, ready for the
	// runtime's registry.
	Specs []agent.Spec
}

// ManifestSource reads a pack from somewhere — a filesystem for the built-in
// packs, an embed.FS for a test. It is an interface so the loader does not care
// which, and so a test does not need a real directory.
type ManifestSource interface {
	// ReadFile returns one file by its path relative to the pack root, or
	// fs.ErrNotExist.
	ReadFile(name string) ([]byte, error)
}

// LoadOptions configures a load.
type LoadOptions struct {
	Source ManifestSource
	// ManifestPath is the manifest's path relative to the source's root.
	ManifestPath string
	// KnownToolKeys is the tool registry's key set. A manifest naming a tool that
	// is not in it is refused, which is section 4.2's "Tool Key 必须来自内置注册表".
	KnownToolKeys map[string]bool
	// KnownSchemaPaths is the set of schema paths this build embeds. A manifest
	// naming a schema that is not there is refused, because an agent whose output
	// is unchecked is worse than one that did not load.
	KnownSchemaPaths map[string]bool
	// MaxPackBytes bounds the documents together. Zero uses DefaultMaxPackBytes.
	MaxPackBytes int
}

// Load reads and validates one pack.
//
// Every failure is a refusal of the whole pack. Section 4.2's "导入 Pack 先静态验证"
// is the reason: a pack that loaded partially would produce agents whose tools or
// schemas are not what their skills say.
func Load(options LoadOptions) (LoadedPack, error) {
	if options.Source == nil {
		return LoadedPack{}, InvalidError("A skill pack needs a source.")
	}
	manifestPath := options.ManifestPath
	if manifestPath == "" {
		manifestPath = "manifest.json"
	}
	if err := validateRelativePath(manifestPath, "manifest"); err != nil {
		return LoadedPack{}, err
	}
	raw, err := options.Source.ReadFile(manifestPath)
	if err != nil {
		return LoadedPack{}, InvalidError("The pack's manifest could not be read.")
	}
	if len(raw) > MaxManifestBytes {
		return LoadedPack{}, InvalidError("The pack's manifest is too large.")
	}
	manifest, err := decodeManifest(raw)
	if err != nil {
		return LoadedPack{}, err
	}
	if err := validateManifest(manifest); err != nil {
		return LoadedPack{}, err
	}

	// Read every document first, so a pack with a missing or oversized file is
	// refused before anything is stored.
	maxPack := options.MaxPackBytes
	if maxPack <= 0 {
		maxPack = DefaultMaxPackBytes
	}
	skills := map[string]string{}
	specs := make([]agent.Spec, 0, len(manifest.Agents))
	total := 0
	for _, entry := range manifest.Agents {
		if err := validateRelativePath(entry.Skill, "skill"); err != nil {
			return LoadedPack{}, err
		}
		document, readErr := options.Source.ReadFile(entry.Skill)
		if readErr != nil {
			return LoadedPack{}, InvalidError("A skill document named by the manifest is missing.")
		}
		if len(document) > MaxSkillDocumentBytes {
			return LoadedPack{}, InvalidError("A skill document is too large.")
		}
		total += len(document)
		if total > maxPack {
			return LoadedPack{}, InvalidError("The pack's documents are larger than a pack may be.")
		}
		// The document must carry the sections section 4.3 requires, or a skill
		// could omit the rules it is supposed to follow.
		if err := validateSkillDocument(string(document)); err != nil {
			return LoadedPack{}, err
		}
		skills[entry.Key] = string(document)

		spec, specErr := specFromManifest(entry, manifest.Metadata.Name, options)
		if specErr != nil {
			return LoadedPack{}, specErr
		}
		specs = append(specs, spec)
	}

	// The hash covers the manifest and the documents in a stable order, so the same
	// content always yields the same version and a different content always yields
	// a different one.
	hasher := sha256.New()
	hasher.Write([]byte("manifest\n"))
	hasher.Write(raw)
	keys := make([]string, 0, len(skills))
	for key := range skills {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	content := make([]byte, 0, total)
	for _, key := range keys {
		hasher.Write([]byte("\nskill:" + key + "\n"))
		hasher.Write([]byte(skills[key]))
		content = append(content, []byte(skills[key])...)
		content = append(content, '\n')
	}
	return LoadedPack{
		Manifest:    manifest,
		Skills:      skills,
		ContentHash: hex.EncodeToString(hasher.Sum(nil)),
		Content:     content,
		Specs:       specs,
	}, nil
}

// decodeManifest parses a manifest strictly.
func decodeManifest(raw []byte) (Manifest, error) {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	// An unknown field is refused rather than dropped: a pack written for a later
	// format must fail loudly instead of running without the field it relies on.
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, InvalidError("The pack's manifest is not a valid manifest.")
	}
	// Trailing content means the file holds something the decoder did not read,
	// which for a manifest is a mistake rather than a comment.
	if decoder.More() {
		return Manifest{}, InvalidError("The pack's manifest has content after the manifest object.")
	}
	return manifest, nil
}

// validateManifest checks the pack-level fields.
func validateManifest(manifest Manifest) error {
	if manifest.APIVersion != APIVersion {
		return InvalidError("The pack's apiVersion is not one this build understands.")
	}
	// The kind is checked rather than ignored so a future pack kind cannot be loaded
	// as this one.
	if manifest.Kind != "AgentPack" {
		return InvalidError("The pack's kind is not an agent pack.")
	}
	if strings.TrimSpace(manifest.Metadata.Name) == "" {
		return InvalidError("The pack needs a name.")
	}
	if err := validatePackName(manifest.Metadata.Name); err != nil {
		return err
	}
	if strings.TrimSpace(manifest.Metadata.Version) == "" {
		return InvalidError("The pack needs a version.")
	}
	if len(manifest.Agents) == 0 {
		return InvalidError("The pack declares no agents.")
	}
	// Agent keys are unique within the pack, because the registry is keyed by them.
	seen := map[string]bool{}
	for _, entry := range manifest.Agents {
		if seen[entry.Key] {
			return InvalidError("The pack declares the same agent key twice.")
		}
		seen[entry.Key] = true
	}
	return nil
}

// validatePackName checks the pack's name, which is used as a skill key and
// therefore as part of a stored hash.
func validatePackName(name string) error {
	trimmed := strings.TrimSpace(name)
	if len([]rune(trimmed)) > agent.MaxSkillKeyLength {
		return InvalidError("The pack's name is too long.")
	}
	for _, character := range trimmed {
		if character >= 'a' && character <= 'z' {
			continue
		}
		if character >= '0' && character <= '9' {
			continue
		}
		if character == '-' || character == '_' {
			continue
		}
		return InvalidError("The pack's name uses lower-case letters, digits, hyphen and underscore.")
	}
	return nil
}

// specFromManifest turns one manifest entry into a domain registration, checking
// the tools and schemas against what the build has.
func specFromManifest(entry ManifestAgent, packName string, options LoadOptions) (agent.Spec, error) {
	layer := agent.AgentLayer(entry.Layer)
	// The tool list is checked before the spec is built, so the refusal names the
	// tool rather than the agent.
	for _, key := range entry.Allowed {
		if !options.KnownToolKeys[key] {
			return agent.Spec{}, InvalidError("The pack names a tool this build does not register.")
		}
	}
	if !options.KnownSchemaPaths[entry.Input] {
		return agent.Spec{}, InvalidError("The pack names an input schema this build does not embed.")
	}
	if !options.KnownSchemaPaths[entry.Output] {
		return agent.Spec{}, InvalidError("The pack names an output schema this build does not embed.")
	}
	spec := agent.Spec{
		Key:          entry.Key,
		Layer:        layer,
		Skill:        entry.Skill,
		Input:        entry.Input,
		Output:       entry.Output,
		AllowedTools: append([]string(nil), entry.Allowed...),
		Limits: agent.Limits{
			MaxToolCalls: entry.Limits.MaxToolCalls,
			MaxDuration:  time.Duration(entry.Limits.TimeoutSeconds) * time.Second,
		},
		// The policy layer defaults to the agent's own layer, which is what section
		// 13's "不同层可使用不同模型" means when a project states nothing per layer. A
		// supervision agent therefore reads the supervision policy rather than the
		// execution one, so a project can point the two at different models without
		// this package having to know why.
		PolicyLayer: policyLayerFor(layer),
	}
	if err := spec.Validate(); err != nil {
		return agent.Spec{}, err
	}
	// The skill key a version is stored under is the pack's name, so one pack is
	// one versioned unit rather than one version per agent: the documents are
	// assembled together and a change to any of them changes the pack.
	if err := validatePackName(packName); err != nil {
		return agent.Spec{}, err
	}
	return spec, nil
}

// policyLayerFor maps a layer to the policy it reads.
//
// It is total over the three layers, and its default is the default policy rather
// than an empty string, so an agent whose layer somehow failed validation cannot
// reach a policy lookup with nothing to look up.
func policyLayerFor(layer agent.AgentLayer) agent.PolicyLayer {
	switch layer {
	case agent.LayerDecision:
		return agent.PolicyDecision
	case agent.LayerExecution:
		return agent.PolicyExecution
	case agent.LayerSupervision:
		return agent.PolicySupervision
	default:
		return agent.PolicyDefault
	}
}

// requiredSections are the headings AGENT_CONTRACTS section 4.3 requires of every
// skill document, in the order it lists them.
//
// They are required rather than merely conventional because each one is a rule the
// agent is supposed to follow: a skill without an Allowed Tools section has not
// said what it may do, and one without Failure Conditions has not said when to
// stop. A document missing any of them is refused at load, which is the only
// moment the omission is cheap to fix.
var requiredSections = []string{
	"Role",
	"Goal",
	"Trusted Context",
	"Untrusted Input",
	"Workflow State",
	"Input Contract",
	"Allowed Tools",
	"Required Procedure",
	"Domain Constraints",
	"Quality Rules",
	"Failure Conditions",
	"Output Contract",
	"Examples",
}

// RequiredSections returns the section list, so a generator and a test can use
// the same one rather than each keeping its own copy.
func RequiredSections() []string {
	return append([]string(nil), requiredSections...)
}

// validateSkillDocument checks that a document carries every required section.
func validateSkillDocument(document string) error {
	for _, section := range requiredSections {
		if !hasHeading(document, section) {
			return InvalidError("A skill document is missing its " + section + " section.")
		}
	}
	// Section 4.3 forbids a skill from demanding private chain of thought. A phrase
	// check is not a proof, but it catches the instruction a hurried author writes,
	// and the rule is about what the document asks for rather than what a model
	// does with it.
	if asksForChainOfThought(document) {
		return InvalidError("A skill document asks for private reasoning, which the specification forbids.")
	}
	return nil
}

// hasHeading reports whether the document carries a `# <name>` heading.
func hasHeading(document, name string) bool {
	return strings.Contains(document, "# "+name)
}

// asksForChainOfThought reports whether a document asks the model to reveal its
// private reasoning.
//
// Section 4.3 requires "要求 reasonSummary，而非内部推理全文", and section 16 keeps the
// record to a reason summary. The check looks for the words that ask for the
// latter, in both the languages this repository's documents are written in.
func asksForChainOfThought(document string) bool {
	lowered := strings.ToLower(document)
	for _, phrase := range []string{
		"chain of thought",
		"chain-of-thought",
		"think step by step and show",
		"show your reasoning in full",
		"私の思考",
		"全文推理",
		"输出你的思考过程",
		"逐步展示你的推理",
	} {
		if strings.Contains(lowered, strings.ToLower(phrase)) {
			return true
		}
	}
	return false
}

// validateRelativePath refuses anything that is not a plain relative path.
//
// Section 4.2's first constraint is "只允许相对路径", and the reason is that a
// manifest naming an absolute path or one with a parent element would let a pack
// choose what the loader reads. The check is deliberately structural: it refuses
// a leading separator, a drive letter, a parent element, a URI scheme and a
// control character, rather than resolving the path and comparing, because a
// refusal that depends on where the process happens to be is a different check.
func validateRelativePath(value, what string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return InvalidError("The manifest names no " + what + " path.")
	}
	if strings.ContainsAny(trimmed, "\x00\r\n") {
		return InvalidError("The manifest's " + what + " path contains a control character.")
	}
	// A scheme would make it a URL, which section 4.2 forbids outright.
	if strings.Contains(trimmed, "://") {
		return InvalidError("The manifest's " + what + " path is a URL.")
	}
	if strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, `\`) {
		return InvalidError("The manifest's " + what + " path is absolute.")
	}
	// A drive letter, including the UNC form.
	if len(trimmed) >= 2 && trimmed[1] == ':' {
		return InvalidError("The manifest's " + what + " path is absolute.")
	}
	normalized := path.Clean(strings.ReplaceAll(trimmed, `\`, "/"))
	if normalized == ".." || strings.HasPrefix(normalized, "../") {
		return InvalidError("The manifest's " + what + " path escapes the pack.")
	}
	if normalized != strings.ReplaceAll(trimmed, `\`, "/") {
		// A path that changes when cleaned has a `.` or a `//` element, which is
		// either a mistake or an attempt to look like something it is not.
		return InvalidError("The manifest's " + what + " path is not a plain relative path.")
	}
	return nil
}

// FS returns a ManifestSource over a filesystem rooted at a directory.
//
// It is a helper for the built-in packs and for tests, and it reads through
// fs.FS's own validation so a path cannot escape the root even if a caller
// bypassed validateRelativePath.
func FS(fsys fs.FS) ManifestSource {
	return fsSource{fsys: fsys}
}

type fsSource struct {
	fsys fs.FS
}

func (s fsSource) ReadFile(name string) ([]byte, error) {
	return fs.ReadFile(s.fsys, name)
}

// Map returns a ManifestSource over an in-memory set of files, for tests.
func Map(files map[string]string) ManifestSource {
	return mapSource{files: files}
}

type mapSource struct {
	files map[string]string
}

func (s mapSource) ReadFile(name string) ([]byte, error) {
	if body, ok := s.files[name]; ok {
		return []byte(body), nil
	}
	return nil, fs.ErrNotExist
}

// Error is a skill-loading refusal.
//
// It is its own type rather than an agent.Error because the two answer different
// questions: an agent error is about a run, and this is about a pack that could
// not be loaded at all. The desktop layer maps both.
type Error struct {
	SafeMessage string
	Cause       error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.SafeMessage
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// InvalidError reports a pack the loader refuses.
func InvalidError(message string) *Error {
	return &Error{SafeMessage: message}
}

// String describes a pack for a log line, without its documents.
func (p LoadedPack) String() string {
	return fmt.Sprintf("pack %s@%s with %d agent(s)", p.Manifest.Metadata.Name, p.Manifest.Metadata.Version, len(p.Specs))
}
