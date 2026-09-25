package desktop

import (
	"context"
	"encoding/base64"
	"strings"
	"sync"

	appdiagnostics "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/diagnostics"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

// diagnostics_binding.go is FR-180's 「一键生成诊断包」 at the Wails boundary.
//
// SECURITY 14.2's contract has two clauses that shape this surface:
//
//   - 「生成前展示清单」 — `PlanDiagnostics` is a READ that writes nothing, so showing the list is
//     safe to do twice.
//   - 「用户可取消内容」 — the plan takes the set of sections a user wants, and `CollectDiagnostics`
//     takes it again, so what a user was shown and what they receive cannot disagree.
//
// The bundle crosses as a MAP of section to base64 rather than as one archive. The reason is the
// same one `BackupBinding` records for its own shape: a Wails call carries JSON, and a bundle is a
// directory of text files. Assembling them into a zip is the WEBVIEW's act, where the file names and
// the download are — this side does not touch the filesystem for it, and the archive writer stays
// where it already is (`infrastructure/archive`).
type DiagnosticsBinding struct {
	mu      sync.RWMutex
	ctx     context.Context
	service *appdiagnostics.Service
}

// AttachDiagnostics supplies the service. A nil service leaves the binding unattached, and every
// method then fails closed.
func AttachDiagnostics(binding *DiagnosticsBinding, ctx context.Context, service *appdiagnostics.Service) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.ctx = ctx
	binding.service = service
	binding.mu.Unlock()
}

func (b *DiagnosticsBinding) context() context.Context {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.ctx == nil {
		return context.Background()
	}
	return b.ctx
}

func (b *DiagnosticsBinding) diagnostics() *appdiagnostics.Service {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.service
}

// DiagnosticsSectionDTO is one line of the manifest, in the shape a panel renders.
type DiagnosticsSectionDTO struct {
	Section  string `json:"section"`
	Bytes    int    `json:"bytes"`
	Note     string `json:"note"`
	Included bool   `json:"included"`
}

// DiagnosticsPlanDTO is what a bundle would contain.
type DiagnosticsPlanDTO struct {
	Sections   []DiagnosticsSectionDTO `json:"sections"`
	TotalBytes int                     `json:"totalBytes"`
}

// DiagnosticsBundleDTO is an assembled bundle.
type DiagnosticsBundleDTO struct {
	Sections []DiagnosticsSectionDTO `json:"sections"`
	// Files maps a section name to its content, base64-encoded because a Wails call carries JSON and
	// a log is not text a JSON encoder can be trusted with. The keys include `manifest`, which is the
	// bundle's own label rather than a section a user chose.
	Files map[string]string `json:"files"`
	// TotalBytes is what the bundle weighs, so a panel can say so.
	TotalBytes int `json:"totalBytes"`
	// CreatedAt is when it was assembled.
	CreatedAt string `json:"createdAt"`
}

// PlanDiagnostics reports what a bundle would contain, without building one.
//
// `sections` is the set a user wants; an EMPTY list means every section, so a panel's default request
// does not have to enumerate the list. An unknown name is refused rather than ignored: a caller who
// asked for a section and received a bundle without it would not know which had happened.
func (b *DiagnosticsBinding) PlanDiagnostics(sections []string) (DiagnosticsPlanDTO, error) {
	service := b.diagnostics()
	if service == nil {
		return DiagnosticsPlanDTO{}, bindingUnavailable()
	}
	wanted, err := toDiagnosticsSections(sections)
	if err != nil {
		return DiagnosticsPlanDTO{}, err
	}
	manifest, err := service.Plan(b.context(), wanted)
	if err != nil {
		return DiagnosticsPlanDTO{}, err
	}
	return toDiagnosticsPlanDTO(manifest), nil
}

// CollectDiagnostics assembles the bundle a user agreed to.
//
// It re-runs the plan inside the service, so the sections it returns are the ones the plan described
// rather than a set assembled from a stale view: a section that appeared between the two calls would
// otherwise arrive in a bundle the user never saw.
func (b *DiagnosticsBinding) CollectDiagnostics(sections []string) (DiagnosticsBundleDTO, error) {
	service := b.diagnostics()
	if service == nil {
		return DiagnosticsBundleDTO{}, bindingUnavailable()
	}
	wanted, err := toDiagnosticsSections(sections)
	if err != nil {
		return DiagnosticsBundleDTO{}, err
	}
	bundle, err := service.Assemble(b.context(), wanted)
	if err != nil {
		return DiagnosticsBundleDTO{}, err
	}
	files := make(map[string]string, len(bundle.Files))
	for section, content := range bundle.Files {
		files[string(section)] = base64.StdEncoding.EncodeToString(content)
	}
	return DiagnosticsBundleDTO{
		Sections:   toDiagnosticsPlanDTO(bundle.Manifest).Sections,
		Files:      files,
		TotalBytes: bundle.Manifest.TotalBytes,
		CreatedAt:  bundle.CreatedAt.UTC().Format(rfc3339),
	}, nil
}

// toDiagnosticsSections converts the wire names to the typed ones, refusing an unknown name.
func toDiagnosticsSections(sections []string) ([]appdiagnostics.Section, error) {
	if len(sections) == 0 {
		return nil, nil
	}
	wanted := make([]appdiagnostics.Section, 0, len(sections))
	for _, name := range sections {
		trimmed := strings.TrimSpace(name)
		section := appdiagnostics.Section(trimmed)
		if !appdiagnostics.IsValidSection(section) {
			return nil, apperror.New("DIAGNOSTICS_SECTION_INVALID", "invalid_input", false,
				"That diagnostics section is not recognised.", nil)
		}
		wanted = append(wanted, section)
	}
	return wanted, nil
}

func toDiagnosticsPlanDTO(manifest appdiagnostics.Manifest) DiagnosticsPlanDTO {
	sections := make([]DiagnosticsSectionDTO, 0, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		sections = append(sections, DiagnosticsSectionDTO{
			Section: string(entry.Section), Bytes: entry.Bytes,
			Note: entry.Note, Included: entry.Included,
		})
	}
	return DiagnosticsPlanDTO{Sections: sections, TotalBytes: manifest.TotalBytes}
}
