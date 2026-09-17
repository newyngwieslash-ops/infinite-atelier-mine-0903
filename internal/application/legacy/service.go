package legacy

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/project"
)

// FileCommitter accepts a blob's bytes and returns the FileStore hash.
//
// The bytes are committed before the import transaction opens (ADR-0006 §3):
// a file that cannot be stored fails the import before any metadata row exists,
// so the worst possible outcome is an unreferenced object rather than a broken
// reference.
type FileCommitter interface {
	CommitLegacyFile(ctx context.Context, legacyKey, mimeType string, content []byte) (string, error)
}

// MediaSource supplies a legacy blob's bytes.
//
// The import reads through this rather than receiving bytes inline, so a caller
// with an on-disk fixture and a caller streaming from the webview share one
// implementation.
type MediaSource interface {
	// ReadLegacyMedia returns the bytes and MIME type for a legacy key, and
	// false when the blob is not available.
	ReadLegacyMedia(ctx context.Context, legacyKey string) (content []byte, mimeType string, ok bool, err error)
}

// Clock abstracts time so tests are deterministic.
type Clock interface {
	Now() time.Time
}

// Service runs a legacy import.
type Service struct {
	store ImportStore
	files FileCommitter
	media MediaSource
	ids   IDSource
	clock Clock
}

// Options configures the import service.
type Options struct {
	Store ImportStore
	Files FileCommitter
	Media MediaSource
	IDs   IDSource
	Clock Clock
}

// NewService builds the import service.
func NewService(options Options) *Service {
	return &Service{store: options.Store, files: options.Files, media: options.Media, ids: options.IDs, clock: options.Clock}
}

// Available reports whether the service can run.
func (s *Service) Available() bool {
	return s != nil && s.store != nil && s.files != nil && s.ids != nil
}

func (s *Service) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now().UTC()
	}
	return s.clock.Now().UTC()
}

// mediaIndex is the result of preparing a snapshot's media: the hash of each
// stored object, keyed by legacy key, plus the keys whose bytes never arrived.
type mediaIndex struct {
	hashes  map[string]string
	missing map[string]bool
	// missingList is the sorted form for reporting.
	missingList []string
	// stored counts the distinct objects written.
	stored int
}

// prepareMedia commits every blob the snapshot lists, once each.
//
// Committing before the transform means a file failure stops the import while
// the database is still untouched, and it also gives the transform the hashes
// it needs to point a node at real content. A blob that the manifest promises
// but no source provides is recorded as missing rather than failing the run:
// the project is still worth importing, and the report tells the user which
// file to re-supply.
func (s *Service) prepareMedia(ctx context.Context, snapshot Snapshot) (mediaIndex, error) {
	index := mediaIndex{hashes: map[string]string{}, missing: map[string]bool{}}
	if s.media == nil {
		// No source: every listed blob is unknown, which the report states.
		for _, media := range snapshot.Media {
			if !index.missing[media.LegacyKey] {
				index.missing[media.LegacyKey] = true
				index.missingList = append(index.missingList, media.LegacyKey)
			}
		}
		sort.Strings(index.missingList)
		return index, nil
	}
	for _, media := range snapshot.Media {
		if _, done := index.hashes[media.LegacyKey]; done {
			continue
		}
		if index.missing[media.LegacyKey] {
			continue
		}
		content, mimeType, ok, err := s.media.ReadLegacyMedia(ctx, media.LegacyKey)
		if err != nil {
			return mediaIndex{}, importError("files", "A media file could not be read.", err)
		}
		if !ok {
			index.missing[media.LegacyKey] = true
			index.missingList = append(index.missingList, media.LegacyKey)
			continue
		}
		if mimeType == "" {
			mimeType = media.MIMEType
		}
		hash, err := s.files.CommitLegacyFile(ctx, media.LegacyKey, mimeType, content)
		if err != nil {
			return mediaIndex{}, importError("files", "A media file could not be stored.", err)
		}
		index.hashes[media.LegacyKey] = hash
		index.stored++
	}
	sort.Strings(index.missingList)
	return index, nil
}

// Precheck describes what an import would do, without writing anything.
//
// AC-LEGACY-001 asks the user to see the scope before committing, and this is
// also where an unusable envelope is refused. Import repeats the same
// validation, so a caller that skips the precheck is still protected.
type Precheck struct {
	ManifestVersion int               `json:"manifestVersion"`
	Projects        []PrecheckProject `json:"projects"`
	TotalNodes      int               `json:"totalNodes"`
	TotalEdges      int               `json:"totalEdges"`
	// MissingMedia lists keys the manifest promises but no source provides.
	MissingMedia []string `json:"missingMedia"`
	// Warnings the transform produced, which the user can review up front.
	Warnings []Warning `json:"warnings"`
	// MediaFiles counts the blobs that are available.
	MediaFiles int `json:"mediaFiles"`
}

// PrecheckProject is one project's summary.
type PrecheckProject struct {
	LegacyID string `json:"legacyId"`
	Title    string `json:"title"`
	Nodes    int    `json:"nodes"`
	Edges    int    `json:"edges"`
	// AlreadyImported reports that this project's fingerprint already finished a
	// past import, so the default action is to skip it (AC-LEGACY-002).
	AlreadyImported bool `json:"alreadyImported"`
}

// Precheck validates a snapshot and reports its scope.
//
// It commits the media as a side effect, because a hash is needed to compute a
// fingerprint and the hash only exists after the content-addressed store has
// the bytes. Committing is idempotent and creates no reference, so a precheck
// that is never followed by an import leaves at most unreferenced objects.
func (s *Service) Precheck(ctx context.Context, snapshot Snapshot) (Precheck, error) {
	if !s.Available() {
		return Precheck{}, storageFailure()
	}
	if err := validateSnapshot(snapshot); err != nil {
		return Precheck{}, err
	}
	index, err := s.prepareMedia(ctx, snapshot)
	if err != nil {
		return Precheck{}, err
	}

	result := Precheck{
		ManifestVersion: snapshot.ManifestVersion,
		MediaFiles:      index.stored,
		// The frontend reads these as arrays, so they are never left nil: a nil
		// slice marshals to null and the dialog would fail on .length.
		Projects:     make([]PrecheckProject, 0),
		MissingMedia: index.missingList,
		Warnings:     make([]Warning, 0),
	}
	for _, legacyProject := range snapshot.Projects {
		fingerprint := Fingerprint(legacyProject, index.hashes)
		imported, checkErr := s.store.HasCompletedImport(ctx, fingerprint)
		if checkErr != nil {
			return Precheck{}, importError("precheck", "The import history could not be read.", checkErr)
		}
		bundle, transformErr := TransformProject(legacyProject, TransformOptions{
			IDs: s.ids, Now: s.now(), MediaHashes: index.hashes, MissingMedia: index.missing, Unsupported: snapshot.Unsupported,
		})
		if transformErr != nil {
			return Precheck{}, importError("transform", "The project could not be read.", transformErr)
		}
		result.Projects = append(result.Projects, PrecheckProject{
			LegacyID:        legacyProject.ID,
			Title:           legacyProject.Title,
			Nodes:           len(legacyProject.Nodes),
			Edges:           len(legacyProject.Connections),
			AlreadyImported: imported,
		})
		result.TotalNodes += len(legacyProject.Nodes)
		result.TotalEdges += len(legacyProject.Connections)
		result.Warnings = append(result.Warnings, bundle.Warnings...)
	}
	return result, nil
}

// ImportOptions tunes one import run.
type ImportOptions struct {
	// Mode is "initial" or "copy". "copy" imports again with fresh identifiers;
	// there is no overwrite mode (ADR-0006 §4).
	Mode string
	// SourceCase labels the run for the report (the fixture name, or the
	// user-facing source label).
	SourceCase string
	// LegacyRoot describes where the data came from, for the report. It is a
	// label, not a path this service reads.
	LegacyRoot string
}

// ImportResult reports what an import did.
type ImportResult struct {
	ImportID string `json:"importId"`
	Mode     string `json:"mode"`
	// Imported lists the projects that were written.
	Imported []ImportedProject `json:"imported"`
	// Skipped lists projects a past import already completed.
	Skipped  []ImportedProject `json:"skipped"`
	Warnings []Warning         `json:"warnings"`
	Counts   ImportOutcome     `json:"counts"`
}

// ImportedProject names one project the run touched.
type ImportedProject struct {
	LegacyID string `json:"legacyId"`
	NewID    string `json:"newId"`
	Title    string `json:"title"`
}

// Import validates, stores the media, transforms and writes one snapshot.
//
// The order is the contract: files first, then one transaction over the
// metadata, then the report. A failure at any point leaves either nothing or
// unreferenced bytes, never a half-imported project (AC-LEGACY-003), and a
// failed run is recorded so the user can see what happened.
func (s *Service) Import(ctx context.Context, snapshot Snapshot, options ImportOptions) (ImportResult, error) {
	if !s.Available() {
		return ImportResult{}, storageFailure()
	}
	mode := options.Mode
	if mode != ModeCopy {
		mode = ModeInitial
	}
	if err := validateSnapshot(snapshot); err != nil {
		return ImportResult{}, err
	}

	startedAt := s.now()
	// The request is built before any work so a failure at any stage can be
	// recorded against it: a failed import that leaves no trace is the outcome
	// AC-LEGACY-003 exists to prevent.
	request := ImportRequest{
		Fingerprint: fingerprintOf(snapshot),
		Mode:        mode,
		SourceCase:  options.SourceCase,
		LegacyRoot:  options.LegacyRoot,
		StartedAt:   startedAt,
	}
	index, err := s.prepareMedia(ctx, snapshot)
	if err != nil {
		s.recordFailure(ctx, request, stageOf(err), err, nil)
		return ImportResult{}, err
	}
	// The fingerprint covers the media hashes, which only exist once the bytes
	// are stored, so it is filled in here and used for the record.
	request.Fingerprint = SnapshotFingerprint(snapshot, index.hashes)

	// Transform everything before writing anything, so a malformed project is
	// refused while the database is still untouched.
	bundles := make([]ProjectBundle, 0, len(snapshot.Projects))
	skipped := make([]ImportedProject, 0)
	for _, legacyProject := range snapshot.Projects {
		bundle, transformErr := TransformProject(legacyProject, TransformOptions{
			IDs: s.ids, Now: startedAt, MediaHashes: index.hashes, MissingMedia: index.missing, Unsupported: snapshot.Unsupported,
		})
		if transformErr != nil {
			s.recordFailure(ctx, request, "transform", transformErr, nil)
			return ImportResult{}, importError("transform", "The project could not be converted.", transformErr)
		}
		// A project whose fingerprint already completed is skipped in initial
		// mode. Copy mode imports it again with fresh identifiers, which is the
		// "new copy" AC-LEGACY-002 allows.
		if mode != ModeCopy {
			done, checkErr := s.store.HasCompletedImport(ctx, bundle.Fingerprint)
			if checkErr != nil {
				s.recordFailure(ctx, request, "precheck", checkErr, nil)
				return ImportResult{}, importError("precheck", "The import history could not be read.", checkErr)
			}
			if done {
				skipped = append(skipped, ImportedProject{
					LegacyID: legacyProject.ID, NewID: bundle.Project.ID, Title: legacyProject.Title,
				})
				continue
			}
		}
		bundles = append(bundles, bundle)
	}

	if len(bundles) == 0 {
		// Nothing to write. The run is recorded as already-imported so the user
		// sees the reason rather than an empty success.
		report, _ := json.Marshal(map[string]any{
			"mode": mode, "skipped": len(skipped), "imported": 0,
			"reason": "every project in the snapshot was already imported",
		})
		id, recordErr := s.store.RecordImport(ctx, request, StatusAlreadyImported, string(report), nil)
		if recordErr != nil {
			return ImportResult{}, importError("report", "The import result could not be recorded.", recordErr)
		}
		return ImportResult{ImportID: id, Mode: mode, Skipped: skipped}, nil
	}

	// Attach the legacy asset library to the bundles. The legacy store keeps
	// assets globally, while the domain requires an owner, so each asset is
	// attached to the project whose nodes reference its bytes, and to the first
	// imported project when nothing references it. The choice is reported.
	if err := attachAssets(bundles, snapshot, index, s.ids); err != nil {
		s.recordFailure(ctx, request, "transform", err, collectWarnings(bundles))
		return ImportResult{}, err
	}
	// The generation history is also profile-global in the legacy store, so it is
	// archived under the first imported project and reported as such.
	if err := attachHistory(bundles, snapshot, startedAt, s.ids); err != nil {
		s.recordFailure(ctx, request, "transform", err, collectWarnings(bundles))
		return ImportResult{}, err
	}

	request.Bundles = bundles
	outcome, err := s.store.ImportSnapshot(ctx, request)
	if err != nil {
		// The store's own error is preserved as the cause and the stage is added,
		// so the caller can report where the import stopped (AC-LEGACY-003).
		wrapped := importError("database", "The project could not be saved.", err)
		s.recordFailure(ctx, request, "database", err, collectWarnings(bundles))
		return ImportResult{}, wrapped
	}
	outcome.Media = index.stored

	imported := make([]ImportedProject, 0, len(bundles))
	for _, bundle := range bundles {
		imported = append(imported, ImportedProject{
			LegacyID: legacyProjectIDFor(bundle), NewID: bundle.Project.ID, Title: bundle.Project.Name,
		})
	}
	return ImportResult{
		ImportID: outcome.ImportID,
		Mode:     mode,
		Imported: imported,
		Skipped:  skipped,
		Warnings: collectWarnings(bundles),
		Counts:   outcome,
	}, nil
}

// attachAssets builds one asset bundle per legacy asset and appends it to the
// project that should own it.
func attachAssets(bundles []ProjectBundle, snapshot Snapshot, index mediaIndex, ids IDSource) error {
	if len(bundles) == 0 {
		return nil
	}
	// Which project references which legacy media key, so an asset can follow
	// the content it provides.
	ownerOfKey := map[string]int{}
	for bundleIndex, bundle := range bundles {
		for _, node := range bundle.Nodes {
			for _, key := range mediaKeysInJSON(node.LegacyMetadata) {
				if _, taken := ownerOfKey[key]; !taken {
					ownerOfKey[key] = bundleIndex
				}
			}
			for _, key := range mediaKeysInJSON(node.UIState) {
				if _, taken := ownerOfKey[key]; !taken {
					ownerOfKey[key] = bundleIndex
				}
			}
		}
	}

	for _, legacyAsset := range snapshot.Assets {
		target := 0
		keys := assetMediaKeys(legacyAsset)
		for _, key := range keys {
			if owner, ok := ownerOfKey[key]; ok {
				target = owner
				break
			}
		}
		warnings := make([]Warning, 0, 1)
		if len(keys) == 0 {
			warnings = append(warnings, Warning{
				Code: WarningUnmodelledAssetKind, LegacyID: legacyAsset.ID,
				Detail: "the asset had no local file and was imported without one", Occurrences: 1,
			})
		}
		if _, referenced := ownerOfKey[firstKey(keys)]; !referenced && len(keys) > 0 {
			warnings = append(warnings, Warning{
				Code: WarningUnmodelledAssetKind, LegacyID: legacyAsset.ID,
				Detail:      "the asset's file is not referenced by any node and it was attached to the first imported project",
				Occurrences: 1,
			})
		}

		assetType := mapAssetType(legacyAsset.Kind)
		if legacyAsset.Kind != "image" && legacyAsset.Kind != "video" &&
			legacyAsset.Kind != "audio" && legacyAsset.Kind != "text" {
			warnings = append(warnings, Warning{
				Code: WarningUnmodelledAssetKind, LegacyID: legacyAsset.ID,
				Detail: "the asset kind has no direct equivalent and was imported as an image", Occurrences: 1,
			})
		}

		legacyFields := ""
		if encoded, encodeErr := json.Marshal(map[string]any{
			"legacyId": legacyAsset.ID,
			"kind":     legacyAsset.Kind,
			"tags":     legacyAsset.Tags,
			"source":   legacyAsset.Source,
			"coverUrl": legacyAsset.CoverURL,
			"data":     legacyAsset.Data,
		}); encodeErr == nil {
			legacyFields = string(encoded)
		}

		assetID, idErr := ids.New()
		if idErr != nil {
			return importError("transform", "The import could not continue.", idErr)
		}
		versionID, idErr := ids.New()
		if idErr != nil {
			return importError("transform", "The import could not continue.", idErr)
		}
		assetBundle := AssetBundle{
			Asset: asset.Asset{
				ID:             assetID,
				ProjectID:      bundles[target].Project.ID,
				Type:           assetType,
				Name:           assetDisplayName(legacyAsset),
				Description:    legacyAsset.Note,
				Status:         asset.StatusActive,
				LegacyMetadata: legacyFields,
				CreatedAt:      parseLegacyTime(legacyAsset.CreatedAt, bundles[target].Project.CreatedAt),
				UpdatedAt:      parseLegacyTime(legacyAsset.UpdatedAt, bundles[target].Project.UpdatedAt),
				Revision:       1,
			},
			Warnings: warnings,
		}
		assetBundle.Version = asset.Version{
			ID:            versionID,
			AssetID:       assetBundle.Asset.ID,
			VersionNumber: 1,
			Status:        asset.VersionDraft,
			CreatedByType: asset.CreatedByMigration,
			Metadata:      legacyFields,
			CreatedAt:     assetBundle.Asset.CreatedAt,
		}
		for _, key := range keys {
			hash, ok := index.hashes[key]
			if !ok {
				continue
			}
			assetBundle.Files = append(assetBundle.Files, FileLink{
				VersionID: versionID, FileHash: hash, Role: asset.RolePrimary, LegacyKey: key,
			})
			break
		}
		bundles[target].Assets = append(bundles[target].Assets, assetBundle)
		bundles[target].Mappings = append(bundles[target].Mappings,
			IDMapping{Kind: MapAsset, LegacyID: legacyAsset.ID, NewID: assetBundle.Asset.ID})
	}
	return nil
}

// attachHistory archives the generation history under the first project.
//
// The legacy store keeps one history list for the whole browser profile rather
// than per project, and the domain requires a project owner, so the entries are
// attached to the first project the run imports. The choice is recorded in the
// import report, because a user looking for a prompt should know where it went.
func attachHistory(bundles []ProjectBundle, snapshot Snapshot, now time.Time, ids IDSource) error {
	if len(bundles) == 0 || len(snapshot.History) == 0 {
		return nil
	}
	records, warnings, err := TransformHistory(snapshot.History, TransformOptions{IDs: ids, Now: now})
	if err != nil {
		return importError("transform", "The generation history could not be converted.", err)
	}
	bundles[0].History = records
	bundles[0].Warnings = append(bundles[0].Warnings, warnings...)
	if len(records) > 0 {
		bundles[0].Warnings = append(bundles[0].Warnings, Warning{
			Code: WarningUnsupportedMetadata, LegacyID: bundles[0].Project.ID,
			Detail:      "the generation history is stored per browser profile and was archived under this project",
			Occurrences: len(records),
		})
	}
	return nil
}

// mediaKeysInJSON collects the legacy storage keys inside a JSON blob the
// transform retained, so an asset can be matched to the project that uses it.
func mediaKeysInJSON(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return nil
	}
	keys := make([]string, 0, 2)
	var walk func(node any)
	walk = func(node any) {
		switch typed := node.(type) {
		case map[string]any:
			for key, child := range typed {
				if text, ok := child.(string); ok && isMediaKey(text) {
					switch key {
					case "storageKey", "content", "url", "coverUrl":
						keys = append(keys, text)
					}
				}
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(decoded)
	return keys
}

// firstKey returns the first element or an empty string.
func firstKey(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// recordFailure stores a failed run so the user can see it. A recording failure
// is not propagated: the import error is the one that matters.
func (s *Service) recordFailure(ctx context.Context, request ImportRequest, stage string, cause error, warnings []Warning) {
	report := map[string]any{"stage": stage, "mode": request.Mode}
	if cause != nil {
		report["reason"] = cause.Error()
	}
	encoded, _ := json.Marshal(report)
	if warnings == nil {
		warnings = []Warning{{Code: WarningUnsupportedMetadata, Detail: "the import stopped at " + stage, Occurrences: 1}}
	}
	_, _ = s.store.RecordImport(ctx, request, StatusFailed, string(encoded), warnings)
}

// validateSnapshot refuses an envelope this build cannot read.
func validateSnapshot(snapshot Snapshot) error {
	if strings.TrimSpace(snapshot.App) != "" && snapshot.App != "infinite-canvas" {
		return importError("snapshot", "That file is not a project backup.", nil)
	}
	if snapshot.ManifestVersion != SupportedManifestVersion {
		return importError("snapshot",
			"That backup was written by a different version and cannot be read.", nil)
	}
	return nil
}

// mapAssetType maps a legacy asset kind to a documented type.
//
// The legacy store has three kinds (text, image, video) while the domain has
// nine. An unrecognised kind becomes an image, the most common user-facing
// kind, and the caller reports the substitution.
func mapAssetType(kind string) asset.Type {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "video":
		return asset.TypeVideo
	case "audio":
		return asset.TypeAudio
	case "text", "doc":
		return asset.TypeDoc
	case "image":
		return asset.TypeImage
	default:
		return asset.TypeImage
	}
}

// assetDisplayName picks a name for a migrated asset.
func assetDisplayName(legacyAsset LegacyAsset) string {
	title := strings.TrimSpace(legacyAsset.Title)
	if title == "" {
		return "Imported asset"
	}
	return title
}

// assetMediaKeys lists the keys an asset's bytes may live under.
func assetMediaKeys(legacyAsset LegacyAsset) []string {
	keys := make([]string, 0, 4)
	for _, field := range []string{"storageKey", "url", "dataUrl"} {
		if value, ok := legacyAsset.Data[field].(string); ok && isMediaKey(value) {
			keys = append(keys, value)
		}
	}
	if cover := strings.TrimSpace(legacyAsset.CoverURL); cover != "" && isMediaKey(cover) {
		keys = append(keys, cover)
	}
	return keys
}

// legacyProjectIDFor recovers the legacy project id a bundle came from.
func legacyProjectIDFor(bundle ProjectBundle) string {
	for _, mapping := range bundle.Mappings {
		if mapping.Kind == MapProject {
			return mapping.LegacyID
		}
	}
	return ""
}

// importError wraps a stage failure so the caller learns where it stopped.
func importError(stage, message string, cause error) error {
	return &project.ImportError{Stage: stage, SafeMessage: message, Cause: cause}
}

// storageFailure is the fail-closed error for an unattached service.
func storageFailure() error {
	return project.StorageError("The import service is unavailable.", nil)
}

// collectWarnings flattens the per-bundle and per-asset warnings into one list.
func collectWarnings(bundles []ProjectBundle) []Warning {
	warnings := make([]Warning, 0)
	for _, bundle := range bundles {
		warnings = append(warnings, bundle.Warnings...)
		for _, assetBundle := range bundle.Assets {
			warnings = append(warnings, assetBundle.Warnings...)
		}
	}
	return warnings
}

// stageOf reports the stage an import error names, or "files" when the error
// carries no stage. The media stage is the only one that can fail before a
// request exists, and attributing an unlabelled failure there is more useful
// than reporting a generic failure.
func stageOf(err error) string {
	if importErr, ok := project.AsImportError(err); ok {
		return importErr.Stage
	}
	return "files"
}

// fingerprintOf derives the snapshot fingerprint without media hashes, for the
// failure record written before the bytes are stored.
func fingerprintOf(snapshot Snapshot) string {
	return SnapshotFingerprint(snapshot, nil)
}
