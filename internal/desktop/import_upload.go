package desktop

import (
	"context"
	"encoding/base64"
	"strings"
	"sync"
	"time"

	importdomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/importing"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/platform/id"
)

// import_upload.go carries a document from the webview to the core in bounded
// chunks.
//
// A Wails binding can only carry text, so binary crosses as base64. The first
// version of the import surface sent the whole document as a byte ARRAY —
// `Array.from(new Uint8Array(buffer))` on the frontend — which turns a
// 100,000-character Chinese novel (about 300 KB of UTF-8) into a 300,000-element
// JSON array, materialised twice: once in the browser and once in the Go decoder.
// That is work proportional to the document's SIZE in characters rather than to
// its bytes, and it happens on the webview's main thread, which is the opposite
// of the "does not block the UI" property the acceptance criterion asks for.
//
// So the transfer follows the pattern the legacy migration already uses for its
// media: begin an upload, append bounded base64 chunks, finish and have the
// assembled bytes verified against a declared size. Nothing is written to the
// story tables until the whole document has arrived and been parsed, so an
// abandoned upload leaves no trace.
//
// SECURITY: the upload's identifier is generated here, never derived from
// anything the caller sends. The bytes live in memory until Finish hands them to
// the import service, which stores them through the content-addressed store;
// nothing in this path touches a filesystem path, so no chunk name can steer a
// write anywhere.

// importChunkBytes bounds one chunk after decoding.
//
// 64 KiB is the same ceiling the paged reader uses for one page of text, and the
// symmetry is deliberate: a chunk and a page are the same amount of document, so
// the largest upload and the largest read are bounded by one figure a reader of
// either file can recognise.
//
// The value must stay BELOW the acceptance document's size, or the chunking would
// not apply to the very case that motivated it. The first draft used 512 KiB,
// which is larger than a 100,000-character Chinese novel (about 300 KB in UTF-8)
// — so the acceptance document crossed in ONE chunk and the whole mechanism was
// inert where it was needed. The test caught it by asserting the fixture crosses
// in more than one chunk.
//
// It is a CONSTANT rather than a client-supplied field: a caller that could choose
// its own chunk size could ask for the whole document in one message, which is the
// thing this exists to avoid.
const importChunkBytes = 64 << 10

// maxImportUploadBytes caps one assembled upload. It mirrors the domain's own
// input ceiling, so a document this accepts is one the import service will not
// refuse for size — the two cannot disagree because one reads the other.
const maxImportUploadBytes = importdomain.MaxInputBytes

// importUpload is one transfer in progress.
type importUpload struct {
	// displayName is used only as the stored object's label, never as a path.
	displayName string
	// projectID and the two optional fields are what the import would be told;
	// they travel with the upload so the frontend does not have to resend them
	// with every chunk.
	projectID        string
	documentID       string
	documentType     string
	confirmDuplicate bool
	// declaredSize is the caller's claim about the decoded byte count. It is
	// checked against what actually arrived: a mismatch means a chunk was lost or
	// duplicated in transit, and importing a truncated document would produce
	// chapters that do not match the file the user chose.
	declaredSize int
	chunks       []byte
	startedAt    time.Time
}

// ImportUploadBinding is the chunked-document upload surface.
//
// It is separate from ImportBinding so that binding's methods stay about the
// import itself: this type owns only the transfer, and its Finish calls through
// to the same service the one-shot path uses.
type ImportUploadBinding struct {
	mu      sync.Mutex
	ctx     context.Context
	imports *ImportBinding
	uploads map[string]*importUpload
	ids     IDGenerator
	// clock is injectable so a test can reason about an upload's age.
	clock Clock
}

// IDGenerator mints upload identifiers.
type IDGenerator interface {
	New() (string, error)
}

// Clock abstracts time so tests are deterministic.
type Clock interface {
	Now() time.Time
}

// AttachImportUpload supplies the import binding an upload finishes into.
//
// A binding with no import binding cannot finish an upload, and says so, rather
// than assembling bytes it has nowhere to put.
func AttachImportUpload(binding *ImportUploadBinding, ctx context.Context, imports *ImportBinding) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.ctx = ctx
	binding.imports = imports
	if binding.uploads == nil {
		binding.uploads = map[string]*importUpload{}
	}
	if binding.ids == nil {
		binding.ids = id.NewGenerator()
	}
	if binding.clock == nil {
		binding.clock = wallClock{}
	}
	binding.mu.Unlock()
}

// wallClock is the production clock.
type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now().UTC() }

func (b *ImportUploadBinding) context() context.Context {
	if b == nil {
		return context.Background()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ctx == nil {
		return context.Background()
	}
	return b.ctx
}

// BeginImportUploadRequest opens an upload.
type BeginImportUploadRequest struct {
	ProjectID string `json:"projectId"`
	// Name is the display name the document is stored under.
	Name string `json:"name,omitempty"`
	// Format is a hint derived from the file name. The bytes decide.
	Format string `json:"format,omitempty"`
	// TotalBytes is the caller's count of the decoded bytes. It is verified on
	// finish rather than trusted.
	TotalBytes int `json:"totalBytes"`
	// DocumentID continues an existing document with a new version. Empty creates
	// one.
	DocumentID string `json:"documentId,omitempty"`
	// DocumentType defaults from the format when empty.
	DocumentType string `json:"documentType,omitempty"`
	// ConfirmDuplicate must be set to import a file whose hash already exists in
	// the project.
	ConfirmDuplicate bool `json:"confirmDuplicate,omitempty"`
}

// BeginImportUploadResult is the upload's identity and its chunk ceiling.
type BeginImportUploadResult struct {
	UploadID string `json:"uploadId"`
	// ChunkBytes is the maximum decoded size one AppendImportUploadChunk may
	// carry. It is reported rather than chosen by the caller, so a frontend cannot
	// send a chunk the core will refuse.
	ChunkBytes int `json:"chunkBytes"`
}

// BeginImportUpload opens a transfer and returns its identifier.
func (b *ImportUploadBinding) BeginImportUpload(request BeginImportUploadRequest) (BeginImportUploadResult, error) {
	if err := b.available(); err != nil {
		return BeginImportUploadResult{}, err
	}
	if strings.TrimSpace(request.ProjectID) == "" {
		return BeginImportUploadResult{}, bindingUnavailable()
	}
	if request.TotalBytes <= 0 {
		// A zero or negative size is refused rather than treated as "unknown":
		// the size is what Finish verifies against, and an unverifiable transfer
		// is one that could silently truncate.
		return BeginImportUploadResult{}, invalidImportRequest("The document's size must be stated.")
	}
	if int64(request.TotalBytes) > maxImportUploadBytes {
		return BeginImportUploadResult{}, invalidImportRequest("The document is larger than an import accepts.")
	}
	uploadID, err := b.ids.New()
	if err != nil {
		return BeginImportUploadResult{}, bindingUnavailable()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.uploads == nil {
		b.uploads = map[string]*importUpload{}
	}
	b.uploads[uploadID] = &importUpload{
		displayName:      request.Name,
		projectID:        request.ProjectID,
		documentID:       request.DocumentID,
		documentType:     request.DocumentType,
		confirmDuplicate: request.ConfirmDuplicate,
		declaredSize:     request.TotalBytes,
		// Preallocated to the declared size: the caller has already been bounded
		// by maxImportUploadBytes, and growing a 64 MiB slice by doubling would
		// copy it several times on the way.
		chunks:    make([]byte, 0, request.TotalBytes),
		startedAt: b.now(),
	}
	return BeginImportUploadResult{UploadID: uploadID, ChunkBytes: importChunkBytes}, nil
}

// AppendImportUploadChunkRequest carries one bounded chunk.
type AppendImportUploadChunkRequest struct {
	UploadID string `json:"uploadId"`
	// Chunk is standard base64 of at most BeginImportUploadResult.ChunkBytes
	// decoded bytes.
	Chunk string `json:"chunk"`
}

// AppendImportUploadChunk adds one chunk to an open upload.
func (b *ImportUploadBinding) AppendImportUploadChunk(request AppendImportUploadChunkRequest) error {
	if err := b.available(); err != nil {
		return err
	}
	decoded, err := base64.StdEncoding.DecodeString(request.Chunk)
	if err != nil {
		return invalidImportRequest("A chunk of the document was not valid base64.")
	}
	if len(decoded) > importChunkBytes {
		return invalidImportRequest("A chunk of the document was larger than the transfer allows.")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	upload, ok := b.uploads[strings.TrimSpace(request.UploadID)]
	if !ok {
		// An unknown id is a refusal rather than a silent no-op: a chunk that
		// lands nowhere would produce a truncated document at Finish, and the
		// caller should learn immediately which upload went missing.
		return invalidImportRequest("That document upload is not open.")
	}
	if len(upload.chunks)+len(decoded) > int(maxImportUploadBytes) {
		// The ceiling is enforced as the bytes arrive, not only at Begin: a
		// caller that understated its size must not be able to stream past it.
		delete(b.uploads, strings.TrimSpace(request.UploadID))
		return invalidImportRequest("The document is larger than an import accepts.")
	}
	upload.chunks = append(upload.chunks, decoded...)
	return nil
}

// FinishImportUploadRequest assembles and imports an upload.
type FinishImportUploadRequest struct {
	UploadID string `json:"uploadId"`
}

// FinishImportUpload verifies the assembled bytes and imports them.
//
// The verification is what makes chunking safe: a lost or duplicated chunk shows
// up as a byte count that disagrees with what the caller declared, and the import
// is refused rather than performed on a truncated document. The upload is removed
// whatever the outcome, so a failed transfer cannot be resumed into a mix of two
// attempts.
func (b *ImportUploadBinding) FinishImportUpload(request FinishImportUploadRequest) (ImportDocumentResult, error) {
	if err := b.available(); err != nil {
		return ImportDocumentResult{}, err
	}
	b.mu.Lock()
	uploadID := strings.TrimSpace(request.UploadID)
	upload, ok := b.uploads[uploadID]
	if ok {
		delete(b.uploads, uploadID)
	}
	imports := b.imports
	b.mu.Unlock()
	if !ok {
		return ImportDocumentResult{}, invalidImportRequest("That document upload is not open.")
	}
	if len(upload.chunks) != upload.declaredSize {
		return ImportDocumentResult{}, invalidImportRequest("The document did not arrive complete, so it was not imported.")
	}
	if len(upload.chunks) == 0 {
		return ImportDocumentResult{}, invalidImportRequest("The document is empty.")
	}
	// The bytes go through the same one-shot path the direct import uses, so
	// there is one implementation of the import rather than two that could
	// disagree about the duplicate check or the chapter write.
	return imports.ImportDocument(ImportDocumentRequest{
		ProjectID:        upload.projectID,
		DocumentID:       upload.documentID,
		DocumentType:     upload.documentType,
		Name:             upload.displayName,
		ConfirmDuplicate: upload.confirmDuplicate,
		Content:          upload.chunks,
	})
}

// AbortImportUpload discards an open upload.
//
// It exists so a cancelled transfer releases its buffer rather than waiting for
// the next Begin to be told the id is gone.
func (b *ImportUploadBinding) AbortImportUpload(uploadID string) error {
	if b == nil {
		return bindingUnavailable()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.uploads, strings.TrimSpace(uploadID))
	return nil
}

// available reports whether the binding can serve a call.
func (b *ImportUploadBinding) available() error {
	if b == nil {
		return bindingUnavailable()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.imports == nil {
		return bindingUnavailable()
	}
	if b.ids == nil {
		b.ids = id.NewGenerator()
	}
	return nil
}

func (b *ImportUploadBinding) now() time.Time {
	if b == nil || b.clock == nil {
		return time.Now().UTC()
	}
	return b.clock.Now()
}

// invalidImportRequest refuses a transfer problem with a message the user can act
// on, using the import domain's invalid-input category so the desktop mapper
// classifies it the same way it classifies every other import refusal.
func invalidImportRequest(message string) error {
	return importdomain.InvalidError(message)
}
