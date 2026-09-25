package desktop

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

// monoform_binding_test.go covers the previs snapshot transfer (WP-21, FR-060's 预览图 clause).
//
// # What this file exists for
//
// The clause 「保存后可在 Shot 中看到摄像机参数和预览图」 was half-delivered: the bridge carried a
// thumbnail from WP-09 on, the panel forwarded it, and the studio's own handler NARROWED IT AWAY —
// its parameter type named the shot and the camera and nothing else. No studio component mentioned a
// thumbnail at all, because no binding could turn one into a stored file.
//
// So these tests are about the transfer's BOUNDS and its ORDER, which are the two places a file
// arriving from an embedded frame can go wrong:
//
//   - the bounds stop an upload larger than the caller declared, and stop one that arrives in chunks
//     bigger than the protocol allows;
//   - the order matters because 「失败时不影响主项目数据」 is an acceptance clause: the bytes are
//     committed before the link, so a failure leaves an unreferenced object rather than a version
//     advertising bytes that are not there.
//
// The store is a DOUBLE rather than the real one: what is under test here is the transfer's
// bookkeeping, and a real store would make a failure in it look like a failure in the protocol.

// snapshotDouble records what the binding asked it to do.
type snapshotDouble struct {
	stored    [][]byte
	names     []string
	linked    []string
	storeErr  error
	linkErr   error
	storeCall int
}

func (d *snapshotDouble) Store(_ context.Context, displayName string, body []byte) (StoredBytes, error) {
	d.storeCall++
	if d.storeErr != nil {
		return StoredBytes{}, d.storeErr
	}
	copied := make([]byte, len(body))
	copy(copied, body)
	d.stored = append(d.stored, copied)
	d.names = append(d.names, displayName)
	return StoredBytes{Hash: strings.Repeat("a", 64), StorageKey: strings.Repeat("b", 64), Size: int64(len(body))}, nil
}

func (d *snapshotDouble) Link(_ context.Context, versionID, fileHash, role string) error {
	if d.linkErr != nil {
		return d.linkErr
	}
	d.linked = append(d.linked, versionID+"|"+fileHash+"|"+role)
	return nil
}

func newMonoformHarness(t *testing.T) (*MonoformBinding, *snapshotDouble) {
	t.Helper()
	store := &snapshotDouble{}
	binding := &MonoformBinding{}
	next := 0
	AttachMonoform(binding, context.Background(), store, func() (string, error) {
		next++
		return "upload-" + itoaDesktop(next), nil
	})
	return binding, store
}

// itoaDesktop renders a small integer, so this file needs no strconv for one call site.
func itoaDesktop(value int) string {
	if value == 0 {
		return "0"
	}
	digits := []byte{}
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}

// begin starts a transfer of the given size in the given format.
func begin(t *testing.T, binding *MonoformBinding, total int, mimeType string) BeginSnapshotUploadResult {
	t.Helper()
	result, err := binding.BeginSnapshotUpload(BeginSnapshotUploadRequest{
		VersionID: "version-1", MIMEType: mimeType, TotalBytes: total,
	})
	if err != nil {
		t.Fatalf("BeginSnapshotUpload: %v", err)
	}
	return result
}

func TestASnapshotArrivesWholeAndIsLinkedAsAReference(t *testing.T) {
	binding, store := newMonoformHarness(t)
	image := []byte("this stands in for a PNG")
	started := begin(t, binding, len(image), "image/png")

	if started.ChunkBytes != importChunkBytes {
		t.Fatalf("the chunk bound is %d, and it must be the import's so there is one figure", started.ChunkBytes)
	}
	// One chunk, because the double's payload is small; the bound's own test is below.
	if err := binding.AppendSnapshotChunk(AppendSnapshotChunkRequest{
		UploadID: started.UploadID, Chunk: base64.StdEncoding.EncodeToString(image),
	}); err != nil {
		t.Fatalf("AppendSnapshotChunk: %v", err)
	}
	finished, err := binding.FinishSnapshotUpload(FinishSnapshotUploadRequest{
		UploadID: started.UploadID, DisplayName: "previs shot 1",
	})
	if err != nil {
		t.Fatalf("FinishSnapshotUpload: %v", err)
	}
	if finished.Bytes != len(image) {
		t.Fatalf("the stored snapshot is %d bytes and %d arrived", finished.Bytes, len(image))
	}
	if len(store.stored) != 1 || string(store.stored[0]) != string(image) {
		t.Fatalf("the store received %q", store.stored)
	}
	if len(store.names) != 1 || store.names[0] != "previs shot 1" {
		t.Fatalf("the stored object's label is %q", store.names)
	}
	// THE LINK, and the role is the domain's own vocabulary: a snapshot is a picture the shot
	// REFERS to, not the picture being shot.
	if len(store.linked) != 1 {
		t.Fatalf("the snapshot was linked %d times", len(store.linked))
	}
	if !strings.HasSuffix(store.linked[0], "|reference") {
		t.Fatalf("the link is %q, and a snapshot is a reference", store.linked[0])
	}
	if !strings.HasPrefix(store.linked[0], "version-1|") {
		t.Fatalf("the link names the wrong version: %q", store.linked[0])
	}
	// The transfer leaves the map, so a replayed Finish cannot attach the same snapshot twice.
	if binding.pendingUploads() != 0 {
		t.Fatalf("%d uploads are still pending after a finish", binding.pendingUploads())
	}
	if _, err := binding.FinishSnapshotUpload(FinishSnapshotUploadRequest{UploadID: started.UploadID}); err == nil {
		t.Fatal("a replayed finish was accepted")
	}
	if len(store.linked) != 1 {
		t.Fatalf("the replay attached a second link: %v", store.linked)
	}
}

func TestASnapshotLargerThanDeclaredIsRefusedBeforeItIsBuffered(t *testing.T) {
	binding, store := newMonoformHarness(t)
	image := []byte(strings.Repeat("x", 200))
	// The caller DECLARES 100 bytes and sends 200: the transfer refuses the chunk that would cross
	// the promise, so nothing past the bound is ever resident.
	started := begin(t, binding, 100, "image/png")
	err := binding.AppendSnapshotChunk(AppendSnapshotChunkRequest{
		UploadID: started.UploadID, Chunk: base64.StdEncoding.EncodeToString(image),
	})
	if err == nil {
		t.Fatal("a chunk past the declared size was accepted")
	}
	// A finish then fails too, because what arrived is not what was declared.
	if _, err := binding.FinishSnapshotUpload(FinishSnapshotUploadRequest{UploadID: started.UploadID}); err == nil {
		t.Fatal("a short transfer was committed")
	}
	if len(store.stored) != 0 {
		t.Fatalf("a refused transfer reached the store: %v", store.stored)
	}
}

func TestASnapshotThatStopsShortIsRefused(t *testing.T) {
	binding, store := newMonoformHarness(t)
	// Declared 100, sent 50, then finished: the size is VERIFIED rather than trusted, so a caller
	// that understates the total does not get a truncated snapshot accepted as a whole one.
	started := begin(t, binding, 100, "image/png")
	half := []byte(strings.Repeat("y", 50))
	if err := binding.AppendSnapshotChunk(AppendSnapshotChunkRequest{
		UploadID: started.UploadID, Chunk: base64.StdEncoding.EncodeToString(half),
	}); err != nil {
		t.Fatalf("the first half should be accepted: %v", err)
	}
	if _, err := binding.FinishSnapshotUpload(FinishSnapshotUploadRequest{UploadID: started.UploadID}); err == nil {
		t.Fatal("a 50-byte transfer declared as 100 was committed")
	}
	if len(store.stored) != 0 {
		t.Fatalf("a short transfer reached the store: %v", store.stored)
	}
}

func TestOnlyImagesMayBeSnapshots(t *testing.T) {
	binding, _ := newMonoformHarness(t)
	// A CLOSED set, checked at the door: a client discovers the problem on the first call rather
	// than after uploading megabytes.
	for _, rejected := range []string{"video/mp4", "text/plain", "", "application/pdf", "image/gif", "image/svg+xml"} {
		if _, err := binding.BeginSnapshotUpload(BeginSnapshotUploadRequest{
			VersionID: "version-1", MIMEType: rejected, TotalBytes: 10,
		}); err == nil {
			t.Fatalf("the type %q was accepted as a snapshot", rejected)
		}
	}
	// And the three the studio can actually produce are accepted, WITH a parameter, because a canvas
	// produces `image/png` and some producers append a charset that is meaningless for an image and
	// must not be the difference between stored and refused.
	for _, accepted := range []string{"image/png", "image/jpeg", "image/webp", "image/png; charset=binary"} {
		if _, err := binding.BeginSnapshotUpload(BeginSnapshotUploadRequest{
			VersionID: "version-1", MIMEType: accepted, TotalBytes: 10,
		}); err != nil {
			t.Fatalf("the type %q was refused: %v", accepted, err)
		}
	}
}

func TestAnUnknownUploadIsRefusedRatherThanCreated(t *testing.T) {
	binding, store := newMonoformHarness(t)
	// An append with no Begin is refused: this binding never invents a transfer from a chunk, so a
	// caller that skipped Begin gets a refusal rather than a transfer whose bounds nobody declared.
	if err := binding.AppendSnapshotChunk(AppendSnapshotChunkRequest{
		UploadID: "no-such-upload", Chunk: base64.StdEncoding.EncodeToString([]byte("x")),
	}); err == nil {
		t.Fatal("an append with no begin was accepted")
	}
	if _, err := binding.FinishSnapshotUpload(FinishSnapshotUploadRequest{UploadID: "no-such-upload"}); err == nil {
		t.Fatal("a finish with no begin was accepted")
	}
	if len(store.stored) != 0 {
		t.Fatalf("an unknown upload reached the store: %v", store.stored)
	}
	// And an ABORT of something that is not there is NOT an error: a caller that aborts twice, or
	// aborts a transfer the core already forgot, has got what it wanted.
	if err := binding.AbortSnapshotUpload("no-such-upload"); err != nil {
		t.Fatalf("aborting an unknown upload failed: %v", err)
	}
}

func TestAnAbortedTransferIsGone(t *testing.T) {
	binding, store := newMonoformHarness(t)
	started := begin(t, binding, 10, "image/png")
	if binding.pendingUploads() != 1 {
		t.Fatalf("%d uploads pending after a begin", binding.pendingUploads())
	}
	if err := binding.AbortSnapshotUpload(started.UploadID); err != nil {
		t.Fatalf("AbortSnapshotUpload: %v", err)
	}
	if binding.pendingUploads() != 0 {
		t.Fatalf("%d uploads pending after an abort", binding.pendingUploads())
	}
	// The abort means the bytes are gone rather than merely hidden: a chunk after it is refused
	// because the transfer is not there.
	if err := binding.AppendSnapshotChunk(AppendSnapshotChunkRequest{
		UploadID: started.UploadID, Chunk: base64.StdEncoding.EncodeToString([]byte("x")),
	}); err == nil {
		t.Fatal("a chunk was accepted after an abort")
	}
	if len(store.stored) != 0 {
		t.Fatalf("an aborted transfer reached the store: %v", store.stored)
	}
}

func TestAChunkBiggerThanTheProtocolAllowIsRefused(t *testing.T) {
	binding, _ := newMonoformHarness(t)
	// THE CHUNK BOUND, which is what makes the transfer bounded rather than merely chunked: a client
	// that sent the whole image in one message would get the payload materialised on the webview's
	// main thread, which is what chunking exists to avoid.
	oversized := make([]byte, importChunkBytes+1)
	started := begin(t, binding, len(oversized), "image/png")
	if err := binding.AppendSnapshotChunk(AppendSnapshotChunkRequest{
		UploadID: started.UploadID, Chunk: base64.StdEncoding.EncodeToString(oversized),
	}); err == nil {
		t.Fatal("a chunk larger than the protocol allows was accepted")
	}
}

func TestAFailureToStoreLeavesNothingLinked(t *testing.T) {
	// 「失败时不影响主项目数据」, stated as the order the two writes happen in: the bytes are committed
	// FIRST and the link second, so a store failure leaves the version untouched. The reverse order
	// would leave a version advertising bytes that are not there.
	binding, store := newMonoformHarness(t)
	store.storeErr = context.DeadlineExceeded
	image := []byte("an image that will not store")
	started := begin(t, binding, len(image), "image/png")
	if err := binding.AppendSnapshotChunk(AppendSnapshotChunkRequest{
		UploadID: started.UploadID, Chunk: base64.StdEncoding.EncodeToString(image),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := binding.FinishSnapshotUpload(FinishSnapshotUploadRequest{UploadID: started.UploadID}); err == nil {
		t.Fatal("a store failure was reported as success")
	}
	if len(store.linked) != 0 {
		t.Fatalf("a version was linked despite a store failure: %v", store.linked)
	}
	// And the transfer is gone, so a retry starts cleanly rather than resuming a broken one.
	if binding.pendingUploads() != 0 {
		t.Fatalf("%d uploads pending after a failed finish", binding.pendingUploads())
	}
}

func TestAFailureToLinkReportsIt(t *testing.T) {
	// The other half of the same clause: if the link fails the caller must HEAR about it, because a
	// snapshot that stored but did not attach is one the user will not find in the shot.
	binding, store := newMonoformHarness(t)
	store.linkErr = context.DeadlineExceeded
	image := []byte("an image that will store but not link")
	started := begin(t, binding, len(image), "image/png")
	if err := binding.AppendSnapshotChunk(AppendSnapshotChunkRequest{
		UploadID: started.UploadID, Chunk: base64.StdEncoding.EncodeToString(image),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := binding.FinishSnapshotUpload(FinishSnapshotUploadRequest{UploadID: started.UploadID}); err == nil {
		t.Fatal("a link failure was reported as success")
	}
	if len(store.stored) != 1 {
		t.Fatalf("the bytes should still be stored, for a retry to reuse: %v", store.stored)
	}
}

func TestAnUnattachedBindingRefusesEveryCall(t *testing.T) {
	// A safe-mode build declares the binding so it exists on the Wails surface, and its store is
	// attached only when the composition root builds one. Every method must refuse rather than
	// panic, which is the discipline every binding in this package keeps.
	unattached := &MonoformBinding{}
	if _, err := unattached.BeginSnapshotUpload(BeginSnapshotUploadRequest{
		VersionID: "v", MIMEType: "image/png", TotalBytes: 10,
	}); err == nil {
		t.Fatal("an unattached binding began an upload")
	}
	if err := unattached.AppendSnapshotChunk(AppendSnapshotChunkRequest{UploadID: "x", Chunk: "eA=="}); err == nil {
		t.Fatal("an unattached binding accepted a chunk")
	}
	if _, err := unattached.FinishSnapshotUpload(FinishSnapshotUploadRequest{UploadID: "x"}); err == nil {
		t.Fatal("an unattached binding finished an upload")
	}
	// Abort is the ONE exception, and it is deliberate: an unattached binding has no map to remove
	// from, so "the transfer is not there" is already true and reporting failure would make a cleanup
	// path fail on the one case it exists for. The assertion is that it RETURNS rather than panics.
	if err := unattached.AbortSnapshotUpload("x"); err != nil {
		t.Fatalf("aborting on an unattached binding returned %v", err)
	}
}

func TestTheUploadIdentifierIsMintedHereAndNeverFromTheCaller(t *testing.T) {
	// SECURITY: the identifier is generated by this binding, so no chunk can steer a write anywhere.
	// The test states it as the property a reader can check: two transfers get different identifiers
	// that the caller never supplied.
	binding, _ := newMonoformHarness(t)
	first := begin(t, binding, 10, "image/png")
	second := begin(t, binding, 10, "image/png")
	if first.UploadID == second.UploadID {
		t.Fatalf("two transfers share the identifier %q", first.UploadID)
	}
	if strings.Contains(first.UploadID, "version-1") {
		t.Fatalf("the identifier %q was derived from the caller's input", first.UploadID)
	}
	if binding.pendingUploads() != 2 {
		t.Fatalf("%d uploads pending, want two independent transfers", binding.pendingUploads())
	}
}

func TestTheStartTimeIsRecorded(t *testing.T) {
	// A transfer's age is what makes a stale one identifiable, and the clock is injected rather than
	// read here so a test can hold it still.
	binding := &MonoformBinding{}
	store := &snapshotDouble{}
	at := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	binding.now = func() time.Time { return at }
	AttachMonoform(binding, context.Background(), store, func() (string, error) { return "upload-1", nil })
	started := begin(t, binding, 10, "image/png")
	binding.mu.Lock()
	upload := binding.uploads[started.UploadID]
	binding.mu.Unlock()
	if upload == nil {
		t.Fatal("the transfer is not in the map")
	}
	if !upload.startedAt.Equal(at) {
		t.Fatalf("the transfer started at %v, and the clock said %v", upload.startedAt, at)
	}
}
