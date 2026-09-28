package appdiagnostics

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// diagnostics.go is FR-180's 「一键生成诊断包」 and SECURITY section 14.2's contract.
//
// # The eight things a bundle carries, and where each comes from
//
// SECURITY 14.2 lists them and this package assembles exactly that list:
//
//	app/version/platform  — buildinfo and runtime.GOOS/GOARCH
//	schema version        — the database's user_version
//	provider types/health — kind and enabled only; NEVER a secret or a base URL's credential
//	error codes           — the codes the diagnostics collected
//	redacted logs         — the app.jsonl the redacting handler already wrote
//	migration status      — the schema version and whether migrations are pending
//	file integrity        — object counts and the file store's own size accounting
//	job/workflow summary  — counts by status, not rows
//
// # 「生成前展示清单；用户可取消内容」
//
// The clause has two halves and the shape of the API is what satisfies them:
//
//   - `Plan` is a READ. It returns one entry per SECTION with the number of bytes that section
//     would contribute, and it writes nothing — so showing a user the list is safe to do twice.
//   - Each section is NAMED, and `Bundle` takes the set a caller wants. A user who does not want
//     their logs in the bundle omits the section, which is what 「可取消内容」 means.
//
// # Redaction is applied HERE, not only at log time
//
// SECURITY 14.1 requires it 「在格式化前和导出前双层执行」 — before formatting AND before export.
// The log file is already redacted by `logging.NewRedactingJSONHandler`, and this package redacts
// AGAIN when it reads those bytes. The second layer is not redundancy for its own sake: the file
// could have been written by an older build, by a different handler, or edited, and a bundle is the
// one artifact a user SENDS TO SOMEBODY ELSE. A leak here leaves the machine.
type Section string

const (
	SectionApplication Section = "application"
	SectionProviders   Section = "providers"
	SectionMigrations  Section = "migrations"
	SectionFileStore   Section = "file_store"
	SectionJobs        Section = "jobs"
	SectionWorkflows   Section = "workflows"
	SectionErrorCodes  Section = "error_codes"
	SectionLogs        Section = "logs"
)

// Sections lists every section a bundle can carry, in the order a manifest shows them.
//
// The list is exported so a UI can offer the choices without knowing the names, and so a test can
// assert the bundle carries exactly this set — a section that appeared in a bundle without being
// listed here would be content a user was never shown.
var Sections = []Section{
	SectionApplication, SectionProviders, SectionMigrations, SectionFileStore,
	SectionJobs, SectionWorkflows, SectionErrorCodes, SectionLogs,
}

// IsValidSection reports whether a section may be requested.
func IsValidSection(value Section) bool {
	for _, candidate := range Sections {
		if candidate == value {
			return true
		}
	}
	return false
}

// ManifestEntry is one line of the preview.
type ManifestEntry struct {
	Section Section `json:"section"`
	// Bytes is what this section would contribute, which is the number a user decides on: a
	// six-megabyte log file and a two-kilobyte summary are different propositions.
	Bytes int `json:"bytes"`
	// Note is the section's own description, so a manifest is readable without the reader knowing
	// what the section names mean.
	Note string `json:"note"`
	// Included reports whether this section is in the bundle the caller asked for.
	Included bool `json:"included"`
}

// Manifest is what a bundle WOULD contain.
type Manifest struct {
	Entries []ManifestEntry `json:"entries"`
	// TotalBytes is the sum of the INCLUDED sections, which is what the user is agreeing to.
	TotalBytes int `json:"totalBytes"`
}

// Reader is the data the diagnostics need.
//
// It is one port with narrow methods rather than several, because every one of them is a COUNT or a
// scalar: a bundle is a summary, and a port that could return rows would invite a bundle that
// carried them.
type Reader interface {
	// SchemaVersion is the database's migration version.
	SchemaVersion(ctx context.Context) (int, error)
	// PendingMigrations reports whether the database is behind the build's migration set.
	PendingMigrations(ctx context.Context) (bool, error)
	// ProviderKinds returns each configured provider's kind and whether it is enabled. It must NOT
	// return a base URL, a secret reference or a credential: the contract is metadata only.
	ProviderKinds(ctx context.Context) ([]ProviderSummary, error)
	// FileStoreSummary reports what the object store holds.
	FileStoreSummary(ctx context.Context) (FileStoreSummary, error)
	// JobCountsByStatus and WorkflowCountsByStatus are the two summaries.
	JobCountsByStatus(ctx context.Context) (map[string]int, error)
	WorkflowCountsByStatus(ctx context.Context) (map[string]int, error)
	// ErrorCodes returns the codes the application has recorded, with their counts, and NO messages:
	// a message can quote a path or a payload.
	ErrorCodes(ctx context.Context) (map[string]int, error)
	// LogBytes returns the redacted log file's content, already truncated to the caller's bound.
	LogBytes(ctx context.Context, limit int) ([]byte, error)
}

// ProviderSummary is one provider's non-secret metadata.
type ProviderSummary struct {
	Kind    string
	Enabled bool
}

// FileStoreSummary is what the object store holds.
type FileStoreSummary struct {
	Objects    int
	TotalBytes int64
}

// Options configures the service.
type Options struct {
	Reader Reader
	Clock  Clock
	// AppVersion names the build. It is passed in rather than read from `buildinfo` here so a test
	// can state it and so this package does not depend on a compile-time constant.
	AppVersion string
	Platform   string
	// MaxLogBytes bounds the log section. Zero takes `DefaultMaxLogBytes`.
	MaxLogBytes int
}

// Clock abstracts time so tests are deterministic.
type Clock interface {
	Now() time.Time
}

// DefaultMaxLogBytes bounds the log section's contribution.
//
// Two megabytes, which is large enough to carry the interesting part of a session and small enough
// that a bundle stays a thing a user can email. A log that has grown past it is TRUNCATED FROM THE
// FRONT — the newest lines are the ones a diagnosis needs — and the manifest says so.
const DefaultMaxLogBytes = 2 << 20

// Service assembles diagnostics bundles.
type Service struct {
	reader      Reader
	clock       Clock
	appVersion  string
	platform    string
	maxLogBytes int
}

// NewService builds the service.
func NewService(options Options) *Service {
	limit := options.MaxLogBytes
	if limit <= 0 {
		limit = DefaultMaxLogBytes
	}
	return &Service{
		reader:      options.Reader,
		clock:       options.Clock,
		appVersion:  options.AppVersion,
		platform:    options.Platform,
		maxLogBytes: limit,
	}
}

// Available reports whether the service can assemble anything.
func (s *Service) Available() bool { return s != nil && s.reader != nil }

func (s *Service) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now().UTC()
	}
	return s.clock.Now().UTC()
}

// sectionNote describes a section for the manifest.
//
// The notes are written for the USER deciding what to send, not for a developer reading the code:
// "which providers are configured, without their addresses or keys" is what a person needs to know
// before they agree to include it.
func sectionNote(section Section) string {
	switch section {
	case SectionApplication:
		return "The application version and the platform it runs on."
	case SectionProviders:
		return "Which providers are configured and whether they are enabled. No addresses, no keys."
	case SectionMigrations:
		return "The database's schema version and whether any migration is pending."
	case SectionFileStore:
		return "How many objects the file store holds and how large it is."
	case SectionJobs:
		return "A count of generation jobs by status."
	case SectionWorkflows:
		return "A count of workflow runs by status."
	case SectionErrorCodes:
		return "The error codes the application has recorded, with counts. No messages."
	case SectionLogs:
		return "The application's log, already redacted. Story text is not in it."
	}
	return ""
}

// Plan reports what a bundle would contain for a chosen set of sections.
//
// It is a READ and writes nothing, which is what makes 「生成前展示清单」 safe to ask twice. A nil or
// empty `wanted` means EVERY section, so a caller that wants the default bundle does not have to
// enumerate the list.
func (s *Service) Plan(ctx context.Context, wanted []Section) (Manifest, error) {
	if !s.Available() {
		return Manifest{}, UnavailableError()
	}
	included := map[Section]bool{}
	if len(wanted) == 0 {
		for _, section := range Sections {
			included[section] = true
		}
	} else {
		for _, section := range wanted {
			if !IsValidSection(section) {
				return Manifest{}, InvalidError("That diagnostics section is not recognised.")
			}
			included[section] = true
		}
	}
	manifest := Manifest{Entries: make([]ManifestEntry, 0, len(Sections))}
	for _, section := range Sections {
		entry := ManifestEntry{Section: section, Note: sectionNote(section), Included: included[section]}
		if entry.Included {
			// The SIZE is measured by BUILDING the section, which is what makes the manifest a
			// statement rather than an estimate: a bound that guessed would be a number a user could
			// not rely on.
			content, err := s.buildSection(ctx, section)
			if err != nil {
				return Manifest{}, err
			}
			entry.Bytes = len(content)
			manifest.TotalBytes += entry.Bytes
		}
		manifest.Entries = append(manifest.Entries, entry)
	}
	return manifest, nil
}

// Bundle is an assembled diagnostics package.
type Bundle struct {
	Manifest Manifest
	// Files maps a section to its rendered content. It is a map rather than a single document
	// because a bundle is a DIRECTORY's worth of text, and a caller writing it decides the file
	// names — this package does not touch the filesystem (AGENTS section 7.2).
	Files map[Section][]byte
	// CreatedAt is when the bundle was assembled.
	CreatedAt time.Time
}

// Assemble builds a bundle for a chosen set of sections.
//
// It re-runs `Plan`, so the manifest a caller was shown and the bundle it receives cannot disagree:
// a section added between the two calls would otherwise appear in a bundle the user never saw.
func (s *Service) Assemble(ctx context.Context, wanted []Section) (Bundle, error) {
	if !s.Available() {
		return Bundle{}, UnavailableError()
	}
	manifest, err := s.Plan(ctx, wanted)
	if err != nil {
		return Bundle{}, err
	}
	bundle := Bundle{
		Manifest:  manifest,
		Files:     map[Section][]byte{},
		CreatedAt: s.now(),
	}
	for _, entry := range manifest.Entries {
		if !entry.Included {
			continue
		}
		content, err := s.buildSection(ctx, entry.Section)
		if err != nil {
			return Bundle{}, err
		}
		bundle.Files[entry.Section] = content
	}
	// The manifest travels IN the bundle, so a reader of one knows what it is without a second file.
	bundle.Files[SectionManifest] = renderManifest(manifest, s.appVersion, bundle.CreatedAt)
	return bundle, nil
}

// SectionManifest is the bundled manifest's own name. It is not in `Sections`: it is not a CHOICE a
// user makes, it is the bundle's label.
const SectionManifest Section = "manifest"

// buildSection renders one section.
func (s *Service) buildSection(ctx context.Context, section Section) ([]byte, error) {
	switch section {
	case SectionApplication:
		return s.buildApplication(ctx)
	case SectionProviders:
		return s.buildProviders(ctx)
	case SectionMigrations:
		return s.buildMigrations(ctx)
	case SectionFileStore:
		return s.buildFileStore(ctx)
	case SectionJobs:
		return s.buildCounts(ctx, "generation jobs", s.reader.JobCountsByStatus)
	case SectionWorkflows:
		return s.buildCounts(ctx, "workflow runs", s.reader.WorkflowCountsByStatus)
	case SectionErrorCodes:
		return s.buildErrorCodes(ctx)
	case SectionLogs:
		return s.buildLogs(ctx)
	}
	return nil, InvalidError("That diagnostics section is not recognised.")
}

func (s *Service) buildApplication(context.Context) ([]byte, error) {
	var out strings.Builder
	out.WriteString("# Application\n")
	fmt.Fprintf(&out, "version: %s\n", s.appVersion)
	fmt.Fprintf(&out, "platform: %s\n", s.platform)
	fmt.Fprintf(&out, "generated: %s\n", s.now().Format(time.RFC3339))
	return []byte(out.String()), nil
}

func (s *Service) buildProviders(ctx context.Context) ([]byte, error) {
	summaries, err := s.reader.ProviderKinds(ctx)
	if err != nil {
		return nil, StorageError("The provider summary could not be read.", err)
	}
	var out strings.Builder
	out.WriteString("# Providers\n")
	// SORTED by kind so two bundles of one state are identical, which is what makes comparing them
	// possible. The summary carries no name, no address and no secret reference: a provider's
	// display name is user text and its reference is a credential target.
	sort.Slice(summaries, func(i, j int) bool { return summaries[i].Kind < summaries[j].Kind })
	for _, provider := range summaries {
		state := "disabled"
		if provider.Enabled {
			state = "enabled"
		}
		fmt.Fprintf(&out, "%s: %s\n", provider.Kind, state)
	}
	fmt.Fprintf(&out, "count: %d\n", len(summaries))
	return []byte(out.String()), nil
}

func (s *Service) buildMigrations(ctx context.Context) ([]byte, error) {
	version, err := s.reader.SchemaVersion(ctx)
	if err != nil {
		return nil, StorageError("The schema version could not be read.", err)
	}
	pending, err := s.reader.PendingMigrations(ctx)
	if err != nil {
		return nil, StorageError("The migration status could not be read.", err)
	}
	var out strings.Builder
	out.WriteString("# Migrations\n")
	fmt.Fprintf(&out, "schema_version: %d\n", version)
	fmt.Fprintf(&out, "pending: %t\n", pending)
	return []byte(out.String()), nil
}

func (s *Service) buildFileStore(ctx context.Context) ([]byte, error) {
	summary, err := s.reader.FileStoreSummary(ctx)
	if err != nil {
		return nil, StorageError("The file store summary could not be read.", err)
	}
	var out strings.Builder
	out.WriteString("# File store\n")
	fmt.Fprintf(&out, "objects: %d\n", summary.Objects)
	fmt.Fprintf(&out, "bytes: %d\n", summary.TotalBytes)
	return []byte(out.String()), nil
}

// buildCounts renders a status summary, shared by the job and workflow sections because the two are
// the same shape and a second renderer would be a second place for the ordering to differ.
func (s *Service) buildCounts(ctx context.Context, label string, read func(context.Context) (map[string]int, error)) ([]byte, error) {
	counts, err := read(ctx)
	if err != nil {
		return nil, StorageError("A "+label+" summary could not be read.", err)
	}
	var out strings.Builder
	fmt.Fprintf(&out, "# %s\n", strings.ToUpper(label[:1])+label[1:])
	keys := make([]string, 0, len(counts))
	for status := range counts {
		keys = append(keys, status)
	}
	sort.Strings(keys)
	for _, status := range keys {
		fmt.Fprintf(&out, "%s: %d\n", status, counts[status])
	}
	return []byte(out.String()), nil
}

func (s *Service) buildErrorCodes(ctx context.Context) ([]byte, error) {
	codes, err := s.reader.ErrorCodes(ctx)
	if err != nil {
		return nil, StorageError("The error codes could not be read.", err)
	}
	var out strings.Builder
	out.WriteString("# Error codes\n")
	keys := make([]string, 0, len(codes))
	for code := range codes {
		keys = append(keys, code)
	}
	sort.Strings(keys)
	for _, code := range keys {
		fmt.Fprintf(&out, "%s: %d\n", code, codes[code])
	}
	return []byte(out.String()), nil
}

// buildLogs reads the log and REDACTES IT AGAIN.
//
// See this file's header for why the second pass is not redundancy: the file may have been written
// by an older build or edited, and a bundle is the one artifact a user sends to somebody else.
func (s *Service) buildLogs(ctx context.Context) ([]byte, error) {
	raw, err := s.reader.LogBytes(ctx, s.maxLogBytes)
	if err != nil {
		return nil, StorageError("The log could not be read.", err)
	}
	redacted := RedactText(string(raw))
	var out strings.Builder
	out.WriteString("# Logs\n")
	fmt.Fprintf(&out, "# redacted at %s\n", s.now().Format(time.RFC3339))
	out.WriteString(redacted)
	return []byte(out.String()), nil
}

// renderManifest writes the bundle's own manifest.
//
// It carries the section list WITH the included flag and the byte counts, so a reader of a bundle
// can tell what is in it and what the user chose to leave out — which is the difference between
// "they did not send their logs" and "there were no logs".
func renderManifest(manifest Manifest, appVersion string, createdAt time.Time) []byte {
	var out strings.Builder
	out.WriteString("# Infinite Atelier diagnostics\n")
	fmt.Fprintf(&out, "app_version: %s\n", appVersion)
	fmt.Fprintf(&out, "created_at: %s\n", createdAt.Format(time.RFC3339))
	fmt.Fprintf(&out, "total_bytes: %d\n", manifest.TotalBytes)
	out.WriteString("\nsections:\n")
	for _, entry := range manifest.Entries {
		state := "omitted by the user"
		if entry.Included {
			state = fmt.Sprintf("%d bytes", entry.Bytes)
		}
		fmt.Fprintf(&out, "- %s: %s (%s)\n", entry.Section, state, entry.Note)
	}
	return []byte(out.String())
}
