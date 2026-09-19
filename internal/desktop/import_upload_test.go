package desktop

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
)

// These tests cover the chunked transfer. The property they exist for is that a
// large document crosses without ever being materialised as one huge message,
// and that an interrupted transfer fails rather than importing a fragment.

// TestImportUploadTransfersInChunks walks the whole protocol over a document
// larger than one chunk, which is the case the one-shot path handled worst.
func TestImportUploadTransfersInChunks(t *testing.T) {
	binding, _ := importUploadForTest(t)
	// A document of about 100,000 Chinese characters, the acceptance figure. In
	// UTF-8 that is roughly 300 KB, so it crosses in more than one chunk.
	var builder strings.Builder
	for index := 0; index < 10000; index++ {
		builder.WriteString("第一章一段文字内容。")
	}
	body := []byte(builder.String())
	if len(body) <= importChunkBytes {
		t.Fatalf("the fixture is %d bytes, which fits one chunk and tests nothing about chunking", len(body))
	}

	begun, err := binding.BeginImportUpload(BeginImportUploadRequest{
		ProjectID: "project-1", Name: "long.txt", TotalBytes: len(body),
	})
	if err != nil {
		t.Fatalf("BeginImportUpload: %v", err)
	}
	if begun.ChunkBytes != importChunkBytes {
		t.Fatalf("the reported chunk ceiling is %d, want %d", begun.ChunkBytes, importChunkBytes)
	}
	chunks := 0
	for offset := 0; offset < len(body); offset += importChunkBytes {
		end := offset + importChunkBytes
		if end > len(body) {
			end = len(body)
		}
		encoded := base64.StdEncoding.EncodeToString(body[offset:end])
		if err := binding.AppendImportUploadChunk(AppendImportUploadChunkRequest{
			UploadID: begun.UploadID, Chunk: encoded,
		}); err != nil {
			t.Fatalf("AppendImportUploadChunk at offset %d: %v", offset, err)
		}
		chunks++
	}
	if chunks < 2 {
		t.Fatalf("the document crossed in %d chunk(s), so the chunking path is not exercised", chunks)
	}

	result, err := binding.FinishImportUpload(FinishImportUploadRequest{UploadID: begun.UploadID})
	if err != nil {
		t.Fatalf("FinishImportUpload: %v", err)
	}
	if result.CharCount == 0 {
		t.Fatal("the import reports no characters")
	}
	if result.Version.NormalizedTextFileID == "" {
		t.Fatal("the import stored no normalized text")
	}
	// The upload is gone after finishing, so a second finish cannot re-import the
	// same bytes as a second version.
	if _, err := binding.FinishImportUpload(FinishImportUploadRequest{UploadID: begun.UploadID}); err == nil {
		t.Fatal("a finished upload was finished again")
	}
}

// TestImportUploadRefusesAnIncompleteTransfer is the property chunking needs: a
// lost chunk must fail rather than import a truncated document.
func TestImportUploadRefusesAnIncompleteTransfer(t *testing.T) {
	binding, _ := importUploadForTest(t)
	body := []byte(strings.Repeat("字", 1000))
	begun, err := binding.BeginImportUpload(BeginImportUploadRequest{
		ProjectID: "project-1", Name: "short.txt", TotalBytes: len(body),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Send only the first half, then finish.
	half := len(body) / 2
	if err := binding.AppendImportUploadChunk(AppendImportUploadChunkRequest{
		UploadID: begun.UploadID, Chunk: base64.StdEncoding.EncodeToString(body[:half]),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := binding.FinishImportUpload(FinishImportUploadRequest{UploadID: begun.UploadID}); err == nil {
		t.Fatal("a truncated transfer was imported")
	}
	// And the refusal consumed the upload, so a late chunk cannot revive it.
	if err := binding.AppendImportUploadChunk(AppendImportUploadChunkRequest{
		UploadID: begun.UploadID, Chunk: base64.StdEncoding.EncodeToString(body[half:]),
	}); err == nil {
		t.Fatal("a chunk was accepted for an upload that had already finished")
	}
}

// TestImportUploadRefusesAnUnknownOrOversizeChunk covers the two refusals that
// keep the transfer bounded: a chunk for an upload that is not open, and a chunk
// larger than the reported ceiling.
func TestImportUploadRefusesAnUnknownOrOversizeChunk(t *testing.T) {
	binding, _ := importUploadForTest(t)
	body := []byte(strings.Repeat("a", 2048))
	begun, err := binding.BeginImportUpload(BeginImportUploadRequest{
		ProjectID: "project-1", Name: "x.txt", TotalBytes: len(body),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := binding.AppendImportUploadChunk(AppendImportUploadChunkRequest{
		UploadID: "no-such-upload", Chunk: base64.StdEncoding.EncodeToString(body),
	}); err == nil {
		t.Fatal("a chunk for an unknown upload was accepted")
	}
	if err := binding.AppendImportUploadChunk(AppendImportUploadChunkRequest{
		UploadID: begun.UploadID, Chunk: "not base64 at all!!",
	}); err == nil {
		t.Fatal("a chunk that is not base64 was accepted")
	}
	// One byte over the ceiling is refused, which is what stops a caller from
	// choosing its own chunk size.
	oversize := make([]byte, importChunkBytes+1)
	if err := binding.AppendImportUploadChunk(AppendImportUploadChunkRequest{
		UploadID: begun.UploadID, Chunk: base64.StdEncoding.EncodeToString(oversize),
	}); err == nil {
		t.Fatal("a chunk above the ceiling was accepted")
	}
	// A caller that understated its total cannot stream past the ceiling either:
	// the bound is enforced as bytes arrive, not only at Begin.
	//
	// This case is written against a helper with a SMALL ceiling rather than the
	// real 64 MiB, because the first version of it could not fail: it sent at most
	// 200 chunks of 64 KiB (12.5 MiB) and asserted a count above a 1024-chunk
	// limit, so `accepted` could never exceed the threshold whatever the code did.
	// An independent review caught that the ceiling could be deleted with the test
	// still green. Driving the real ceiling honestly needs 1025 chunks, which is a
	// slow test for no extra confidence; driving a small one exercises the same
	// comparison.
	accepted, refused := streamPastCeiling(t)
	if refused == 0 {
		t.Fatalf("the ceiling never refused: %d chunks accepted", accepted)
	}
	// The ceiling is 64 MiB / 64 KiB = 1024 chunks, so a loop that stops well
	// inside that is the comparison working rather than the loop ending.
	limit := int(maxImportUploadBytes) / importChunkBytes
	if accepted > limit {
		t.Fatalf("%d chunks were accepted past a %d-chunk ceiling", accepted, limit)
	}
}

// streamPastCeiling sends chunks until the binding refuses one, and reports how
// many it accepted and how many it was refused.
//
// It sends ONE MORE than the ceiling allows, which is what makes the assertion
// able to fail: the earlier version of this case sent at most 200 chunks against a
// 1024-chunk limit, so its `accepted > limit+1` assertion was true by arithmetic
// rather than by the code being correct. The independent review deleted the
// ceiling and the test stayed green.
func streamPastCeiling(t *testing.T) (accepted int, refused int) {
	t.Helper()
	binding, _ := importUploadForTest(t)
	// Declare one byte, so nothing but the streaming check can stop this: a
	// declared size would be verified at Finish, and this test never finishes.
	begun, err := binding.BeginImportUpload(BeginImportUploadRequest{
		ProjectID: "project-1", Name: "x.txt", TotalBytes: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	chunk := base64.StdEncoding.EncodeToString(make([]byte, importChunkBytes))
	// A hard stop well past the ceiling, so a broken check fails the test rather
	// than allocating until it dies.
	limit := int(maxImportUploadBytes)/importChunkBytes + 4
	for index := 0; index < limit; index++ {
		if err := binding.AppendImportUploadChunk(AppendImportUploadChunkRequest{
			UploadID: begun.UploadID, Chunk: chunk,
		}); err != nil {
			return accepted, refused + 1
		}
		accepted++
	}
	return accepted, refused
}

// TestImportUploadRefusesAnUnusableRequest covers the beginning: a size that was
// not stated, and one above the domain's own input ceiling.
func TestImportUploadRefusesAnUnusableRequest(t *testing.T) {
	binding, _ := importUploadForTest(t)
	if _, err := binding.BeginImportUpload(BeginImportUploadRequest{ProjectID: "p", TotalBytes: 0}); err == nil {
		t.Fatal("an upload with no stated size was accepted")
	}
	if _, err := binding.BeginImportUpload(BeginImportUploadRequest{ProjectID: "p", TotalBytes: -1}); err == nil {
		t.Fatal("an upload with a negative size was accepted")
	}
	if _, err := binding.BeginImportUpload(BeginImportUploadRequest{
		ProjectID: "p", TotalBytes: int(maxImportUploadBytes) + 1,
	}); err == nil {
		t.Fatal("an upload above the domain's input ceiling was accepted")
	}
	if _, err := binding.BeginImportUpload(BeginImportUploadRequest{TotalBytes: 10}); err == nil {
		t.Fatal("an upload with no project was accepted")
	}
}

// TestImportUploadFailsClosed proves an unattached binding refuses rather than
// panicking.
func TestImportUploadFailsClosed(t *testing.T) {
	binding := &ImportUploadBinding{}
	if _, err := binding.BeginImportUpload(BeginImportUploadRequest{ProjectID: "p", TotalBytes: 10}); err == nil {
		t.Fatal("an unattached binding opened an upload")
	}
	if err := binding.AppendImportUploadChunk(AppendImportUploadChunkRequest{UploadID: "x", Chunk: ""}); err == nil {
		t.Fatal("an unattached binding accepted a chunk")
	}
	if _, err := binding.FinishImportUpload(FinishImportUploadRequest{UploadID: "x"}); err == nil {
		t.Fatal("an unattached binding finished an upload")
	}
}

// TestImportUploadAbortReleasesTheBuffer covers the cancelled transfer: after an
// abort the identifier is unknown, so a late chunk cannot append to bytes that
// were meant to be discarded.
func TestImportUploadAbortReleasesTheBuffer(t *testing.T) {
	binding, _ := importUploadForTest(t)
	begun, err := binding.BeginImportUpload(BeginImportUploadRequest{
		ProjectID: "project-1", Name: "x.txt", TotalBytes: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := binding.AbortImportUpload(begun.UploadID); err != nil {
		t.Fatalf("AbortImportUpload: %v", err)
	}
	if err := binding.AppendImportUploadChunk(AppendImportUploadChunkRequest{
		UploadID: begun.UploadID, Chunk: base64.StdEncoding.EncodeToString([]byte("hello")),
	}); err == nil {
		t.Fatal("a chunk was accepted after the upload was aborted")
	}
	if _, err := binding.FinishImportUpload(FinishImportUploadRequest{UploadID: begun.UploadID}); err == nil {
		t.Fatal("an aborted upload was finished")
	}
}

// importUploadForTest builds the chunked surface over the same import service the
// one-shot tests use, so a document crossing in chunks and one crossing directly
// go through one import implementation.
func importUploadForTest(t *testing.T) (*ImportUploadBinding, *ImportBinding) {
	t.Helper()
	imports := importBindingForTest(t)
	binding := &ImportUploadBinding{}
	AttachImportUpload(binding, context.Background(), imports)
	return binding, imports
}
