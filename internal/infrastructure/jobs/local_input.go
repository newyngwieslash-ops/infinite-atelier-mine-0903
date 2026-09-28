package jobs

import (
	"encoding/json"
	"strings"
)

// local_input.go is RP-04.1's typed, versioned input documents for the four
// local job types FR-150 names.
//
// # What replaced the envelope, and why
//
// The T06 handler read a two-field envelope (`projectId`, `subject`) and
// hard-coded everything else: a thumbnail got no size choice, an export got
// no quality or FPS, an import result claimed a fixed "1 episode" and
// reported chapter counts as story entities. A job whose input cannot say
// what it wants cannot be audited, retried faithfully, or parameterised by a
// caller that knows better.
//
// Each document now carries its own typed fields AND a `version` — so a
// queued row from an older build states which shape it was written with, and
// a runner that no longer decodes that shape refuses it with an invalid-input
// error rather than misreading the bytes.
//
// # The unknown-field rule
//
// Every document decodes with `DisallowUnknownFields` (RP-04.1's RED case):
// an input carrying a field this version does not know is refused rather
// than silently dropped — a dropped field is a request the job half-heard,
// and the remedy is a new input version, not a shrug.

const (
	// LocalInputVersion is the input-document version the current runner decodes.
	LocalInputVersion = 1
)

// localInputHeader is the fields every versioned document carries.
type localInputHeader struct {
	// ProjectID files the job under the project it works on. A job whose
	// subject lives in another project is refused at the ownership check
	// (RP-04.1) rather than executed against whatever the bytes name.
	ProjectID string `json:"projectId"`
	// Version states which input shape the submitter wrote.
	Version int `json:"version"`
}

// ThumbnailInput is a thumbnail job's typed document.
type ThumbnailInput struct {
	ProjectID       string `json:"projectId"`
	Version         int    `json:"version"`
	AssetVersionID  string `json:"assetVersionId"`
	MaxWidthPixels  int    `json:"maxWidthPixels,omitempty"`
	MaxHeightPixels int    `json:"maxHeightPixels,omitempty"`
}

// ImportInput is a document-import job's typed document.
type ImportInput struct {
	ProjectID  string `json:"projectId"`
	Version    int    `json:"version"`
	DocumentID string `json:"documentId"`
	// Format is the stored document's format vocabulary (txt, markdown, docx,
	// pdf). Empty means "the stored document's own format", which is what a
	// job submitted from the import flow states.
	Format string `json:"format,omitempty"`
	// OriginalName is the file name the user uploaded, carried for the audit
	// trail. It is NEVER a path the job reads from — the document's bytes
	// come from the managed store by ID.
	OriginalName string `json:"originalName,omitempty"`
}

// ExportInput is an export job's typed document.
type ExportInput struct {
	ProjectID string `json:"projectId"`
	Version   int    `json:"version"`
	EpisodeID string `json:"episodeId"`
	// Quality is the export quality vocabulary (preview/final). Empty is
	// preview, the exporter's own default.
	Quality string `json:"quality,omitempty"`
	// FPS is the composed film's frame rate. Zero is the exporter's default.
	FPS int `json:"fps,omitempty"`
	// ApprovedBoardVersionID pins the board version the export composes from.
	// Empty means the episode's current approved board — the same default the
	// interactive export uses.
	ApprovedBoardVersionID string `json:"approvedBoardVersionId,omitempty"`
}

// MigrationInput is a legacy-snapshot import job's typed document.
type MigrationInput struct {
	ProjectID string `json:"projectId"`
	Version   int    `json:"version"`
	// SnapshotHash is the managed store hash of the uploaded snapshot. The
	// job reads from the store by hash, never from an OS path the submitter
	// named.
	SnapshotHash string `json:"snapshotHash"`
	// Fingerprint is the snapshot's identity for the idempotent precheck —
	// the same fingerprint the legacy importer keys its HasCompletedImport
	// answer on.
	Fingerprint string `json:"fingerprint,omitempty"`
	// ImportMode is the legacy importer's mode vocabulary (initial/copy).
	// Empty is "initial".
	ImportMode string `json:"importMode,omitempty"`
}

func decodeVersioned(data string, version int, target any) error {
	decoder := json.NewDecoder(strings.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	header := localInputHeader{}
	if err := json.Unmarshal([]byte(data), &header); err != nil {
		return err
	}
	if header.Version != version {
		return errInputVersion{stated: header.Version, supported: version}
	}
	return nil
}

// errInputVersion is the refusal for a document written in a shape this
// runner no longer decodes.
type errInputVersion struct {
	stated    int
	supported int
}

func (e errInputVersion) Error() string {
	return "the job input states version " + itoaInput(e.stated) +
		" but this runner decodes version " + itoaInput(e.supported)
}

func itoaInput(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	negative := value < 0
	if negative {
		value = -value
	}
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	if negative {
		return "-" + digits
	}
	return digits
}
