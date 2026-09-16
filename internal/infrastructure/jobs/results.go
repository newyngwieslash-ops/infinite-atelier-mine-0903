// Package jobs hosts the infrastructure pieces of the persistent job manager:
// the result pipeline, the runner that executes a job's stage, and the
// scheduler wiring.
package jobs

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/files"
	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
)

// ResultStore commits verified provider results into managed storage. It is the
// only component that turns provider bytes into a durable asset reference.
//
// Commit order is fixed and matters:
//  1. write the bytes through the content-addressed store;
//  2. validate the detected content type against the capability allowlist (the
//     type comes from the bytes, never from the provider's own header);
//  3. record the object metadata — `file_references.file_hash` has a foreign
//     key to `file_objects(hash)`, so a reference to an unrecorded object is
//     rejected by the database;
//  4. record the owning reference.
//
// A failure at any step leaves either no object or an unreferenced object,
// never a reference to bytes that were not verified.
type ResultStore struct {
	files      ContentStore
	metadata   MetadataStore
	references ReferenceStore
}

// ContentStore is the FileStore surface the pipeline needs. It uses the
// application FileStore port's object shape so there is exactly one definition
// of a stored object.
type ContentStore interface {
	Put(ctx context.Context, displayName string, body io.Reader) (files.Object, error)
	Open(ctx context.Context, storageKey string) (io.ReadCloser, error)
}

// MetadataStore records a stored object's metadata row. It uses the
// application FileRepository's object shape so the job pipeline and the file
// service write identical rows.
type MetadataStore interface {
	UpsertObject(ctx context.Context, object files.Object) error
}

// ReferenceStore records file ownership.
type ReferenceStore interface {
	AddReference(ctx context.Context, ownerType, ownerID, fileHash string) error
}

// NewResultStore builds the pipeline. The metadata store is required, not
// optional: without it a committed result cannot be referenced and the job
// would fail at the last step.
func NewResultStore(files ContentStore, metadata MetadataStore, references ReferenceStore) *ResultStore {
	return &ResultStore{files: files, metadata: metadata, references: references}
}

// AllowedMIME lists the content types a job result may have. Anything else is
// rejected before it is referenced: a provider that returns HTML or an
// executable must not be able to place it in the user's asset library.
//
// The check runs against the *sniffed* type (the FileStore calls
// http.DetectContentType), never against the provider's own header, so every
// entry must be a name the sniffer can actually produce from a payload an
// adapter really returns. An entry the sniffer never emits is a dead rule: it
// protects nothing and hides which payloads are genuinely accepted.
// TestAllowedMIMECoversSnifferNames pins both directions against real bytes.
func AllowedMIME(capability string) []string {
	switch capability {
	case "image":
		return []string{"image/png", "image/jpeg", "image/webp", "image/gif"}
	case "video":
		// video/mp4 for an ftyp box and video/webm for EBML: the two containers
		// the video contract produces.
		return []string{"video/mp4", "video/webm"}
	case "audio":
		// The sniffer reports RIFF/WAVE as audio/wave (not the IANA audio/wav), so
		// accepting only the IANA spelling rejected every real WAV result. An OggS
		// container (the Opus format offered in the UI) is reported as
		// application/ogg rather than audio/ogg, so that name is accepted too.
		//
		// FLAC, AAC and raw PCM have no sniffer signature and are therefore
		// rejected rather than silently stored with an opaque type.
		return []string{"audio/wave", "audio/mpeg", "application/ogg"}
	default:
		return nil
	}
}

// MIMEAllowed reports whether a detected type is acceptable for a capability.
func MIMEAllowed(capability, mimeType string) bool {
	normalized := strings.ToLower(strings.TrimSpace(mimeType))
	if idx := strings.IndexByte(normalized, ';'); idx >= 0 {
		normalized = strings.TrimSpace(normalized[:idx])
	}
	for _, allowed := range AllowedMIME(capability) {
		if normalized == allowed {
			return true
		}
	}
	return false
}

// CommitResult stores a payload and records its metadata plus ownership.
func (s *ResultStore) CommitResult(ctx context.Context, jobID, capability, mimeHint string, body io.Reader) (appjobs.CommittedFile, error) {
	return s.commit(ctx, jobID, capability, displayNameFor(capability, mimeHint), body)
}

// CommitInline decodes base64 (optionally a data URL) and stores the bytes.
func (s *ResultStore) CommitInline(ctx context.Context, jobID, capability, mimeHint, data string) (appjobs.CommittedFile, error) {
	payload, detectedHint, err := decodeInline(data)
	if err != nil {
		return appjobs.CommittedFile{}, err
	}
	if mimeHint == "" {
		mimeHint = detectedHint
	}
	return s.CommitResult(ctx, jobID, capability, mimeHint, bytes.NewReader(payload))
}

// commit performs the shared write path: the single place that decides whether
// bytes become a referenceable artifact.
func (s *ResultStore) commit(ctx context.Context, jobID, capability, displayName string, body io.Reader) (appjobs.CommittedFile, error) {
	if s == nil || s.files == nil {
		return appjobs.CommittedFile{}, job.FailedJobError(job.CategoryStorage, "Result storage is unavailable.")
	}
	stored, err := s.files.Put(ctx, displayName, body)
	if err != nil {
		return appjobs.CommittedFile{}, job.FailedJobError(job.CategoryStorage, "The result could not be stored.")
	}
	if !MIMEAllowed(capability, stored.MIME) {
		return appjobs.CommittedFile{}, job.FailedJobError(
			job.CategoryResponseInvalid,
			"The provider returned a file type that is not allowed for this capability.",
		)
	}
	if err := s.record(ctx, jobID, stored); err != nil {
		return appjobs.CommittedFile{}, err
	}
	return appjobs.CommittedFile{
		StorageKey: stored.StorageKey,
		MIME:       stored.MIME,
		Size:       stored.Size,
		Hash:       stored.Hash,
	}, nil
}

// record writes the object metadata and then the ownership reference. Both are
// required before the bytes count as a result.
func (s *ResultStore) record(ctx context.Context, jobID string, stored files.Object) error {
	if s.metadata == nil {
		return job.FailedJobError(job.CategoryStorage, "Result metadata storage is unavailable.")
	}
	if err := s.metadata.UpsertObject(ctx, files.Object{
		Hash:       stored.Hash,
		StorageKey: stored.StorageKey,
		MIME:       stored.MIME,
		Size:       stored.Size,
	}); err != nil {
		return job.FailedJobError(job.CategoryStorage, "The result metadata could not be recorded.")
	}
	if s.references == nil {
		return job.FailedJobError(job.CategoryStorage, "Result reference storage is unavailable.")
	}
	if err := s.references.AddReference(ctx, "job", jobID, stored.Hash); err != nil {
		return job.FailedJobError(job.CategoryStorage, "The result reference could not be recorded.")
	}
	return nil
}

// DownloadAndCommit streams a provider-supplied URL into storage. The download
// policy (public https only, size capped) is enforced by the downloader; this
// function adds the content-type allowlist and the metadata/reference writes.
//
// Streaming is deliberate: a large media result must never be buffered in
// memory. The pipe is unbuffered, so whichever side stops first must close it:
// the downloader closes on completion or failure, and this function closes the
// reader when the store abandons the write. Both paths therefore unblock the
// other side, and neither the goroutine nor the HTTP body leaks.
func (s *ResultStore) DownloadAndCommit(ctx context.Context, downloader DownloaderPort, jobID, capability, rawURL string, maxBytes int64) (appjobs.CommittedFile, error) {
	if s == nil || s.files == nil {
		return appjobs.CommittedFile{}, job.FailedJobError(job.CategoryStorage, "Result storage is unavailable.")
	}
	if downloader == nil {
		return appjobs.CommittedFile{}, job.FailedJobError(job.CategoryStorage, "Result download is unavailable.")
	}
	reader, writer := io.Pipe()
	downloadErr := make(chan error, 1)
	go func() {
		_, _, err := downloader.Download(ctx, rawURL, writer, maxBytes)
		if err != nil {
			// CloseWithError makes the store abandon the partial write instead
			// of committing a truncated asset.
			_ = writer.CloseWithError(err)
			downloadErr <- err
			return
		}
		_ = writer.Close()
		downloadErr <- nil
	}()

	stored, putErr := s.files.Put(ctx, displayNameFor(capability, ""), reader)
	if putErr != nil {
		// The store stopped reading (cancellation, disk error). Unblock the
		// downloader: it is sitting in Write on an unbuffered pipe, so without
		// this the goroutine and its HTTP response body would leak and this
		// function would then wait forever on downloadErr.
		_ = reader.CloseWithError(putErr)
	}
	// The pipe is now closed from one side or the other, so the downloader
	// always returns.
	if err := <-downloadErr; err != nil {
		return appjobs.CommittedFile{}, err
	}
	if putErr != nil {
		return appjobs.CommittedFile{}, job.FailedJobError(job.CategoryStorage, "The downloaded result could not be stored.")
	}
	if !MIMEAllowed(capability, stored.MIME) {
		return appjobs.CommittedFile{}, job.FailedJobError(
			job.CategoryResponseInvalid,
			"The provider returned a file type that is not allowed for this capability.",
		)
	}
	if err := s.record(ctx, jobID, stored); err != nil {
		return appjobs.CommittedFile{}, err
	}
	return appjobs.CommittedFile{
		StorageKey: stored.StorageKey,
		MIME:       stored.MIME,
		Size:       stored.Size,
		Hash:       stored.Hash,
	}, nil
}

// DownloaderPort is the download surface the pipeline needs.
type DownloaderPort interface {
	Download(ctx context.Context, rawURL string, writer io.Writer, maxBytes int64) (int64, string, error)
}

// decodeInline accepts a bare base64 payload or a data URL.
func decodeInline(data string) ([]byte, string, error) {
	trimmed := strings.TrimSpace(data)
	if trimmed == "" {
		return nil, "", job.FailedJobError(job.CategoryResponseInvalid, "The provider returned an empty result.")
	}
	hint := ""
	if strings.HasPrefix(trimmed, "data:") {
		comma := strings.IndexByte(trimmed, ',')
		if comma < 0 {
			return nil, "", job.FailedJobError(job.CategoryResponseInvalid, "The provider returned malformed image data.")
		}
		header := trimmed[len("data:"):comma]
		if idx := strings.IndexByte(header, ';'); idx >= 0 {
			hint = header[:idx]
		} else {
			hint = header
		}
		trimmed = trimmed[comma+1:]
	}
	decoded, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		// Some providers omit padding.
		decoded, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(trimmed, "="))
		if err != nil {
			return nil, "", job.FailedJobError(job.CategoryResponseInvalid, "The provider returned malformed image data.")
		}
	}
	return decoded, hint, nil
}

// displayNameFor picks a neutral filename. It is a display hint only: the
// FileStore assigns the real content-addressed key.
func displayNameFor(capability, mimeHint string) string {
	extension := "bin"
	normalized := strings.ToLower(mimeHint)
	switch {
	case strings.Contains(normalized, "png"):
		extension = "png"
	case strings.Contains(normalized, "jpeg"), strings.Contains(normalized, "jpg"):
		extension = "jpg"
	case strings.Contains(normalized, "webp"):
		extension = "webp"
	case strings.Contains(normalized, "gif"):
		extension = "gif"
	case strings.Contains(normalized, "avif"):
		extension = "avif"
	case strings.Contains(normalized, "mp4"):
		extension = "mp4"
	case strings.Contains(normalized, "webm"):
		extension = "webm"
	case strings.Contains(normalized, "quicktime"):
		extension = "mov"
	case strings.Contains(normalized, "mpeg"):
		extension = "mp3"
	case strings.Contains(normalized, "wav"):
		extension = "wav"
	case strings.Contains(normalized, "ogg"):
		extension = "ogg"
	case strings.Contains(normalized, "flac"):
		extension = "flac"
	}
	return "result-" + capability + "." + extension
}

// Commit implements the application ResultWriter port for a payload the caller
// already obtained. It applies the same allowlist and metadata rules as the
// other commit paths so there is no unguarded variant.
func (s *ResultStore) Commit(ctx context.Context, jobID, mimeHint string, body io.Reader, declaredSize int64) (appjobs.CommittedFile, error) {
	capability := capabilityFromHint(mimeHint)
	committed, err := s.CommitResult(ctx, jobID, capability, mimeHint, body)
	if err != nil {
		return appjobs.CommittedFile{}, err
	}
	if declaredSize > 0 && committed.Size != declaredSize {
		return appjobs.CommittedFile{}, job.FailedJobError(job.CategoryResponseInvalid, "The provider result size did not match the transfer.")
	}
	return committed, nil
}

// capabilityFromHint infers the capability from a MIME hint.
func capabilityFromHint(mimeHint string) string {
	normalized := strings.ToLower(mimeHint)
	switch {
	case strings.HasPrefix(normalized, "image/"):
		return "image"
	case strings.HasPrefix(normalized, "video/"):
		return "video"
	case strings.HasPrefix(normalized, "audio/"):
		return "audio"
	default:
		return ""
	}
}
