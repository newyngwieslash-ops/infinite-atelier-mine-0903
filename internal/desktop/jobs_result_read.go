package desktop

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

// maxResultFileBytes bounds one artifact read into a data URL. The canvas
// displays images through data URLs, so the whole object is materialised; the
// cap keeps a pathological file from exhausting memory in the webview.
const maxResultFileBytes = 64 << 20

// readResultFile loads one stored artifact and encodes it for the webview.
//
// The FileStore validates the key shape again and confines the path to its own
// root, so this function adds only the size cap and the data-URL encoding.
func readResultFile(ctx context.Context, reader ResultReader, storageKey string) (JobResultFileContent, error) {
	if reader == nil {
		return JobResultFileContent{}, bindingUnavailable()
	}
	body, err := reader.Open(ctx, storageKey)
	if err != nil {
		return JobResultFileContent{}, apperror.New(
			"JOB_RESULT_READ_FAILED",
			"storage",
			false,
			"The job result could not be read.",
			err,
		)
	}
	defer body.Close()

	limited := io.LimitReader(body, maxResultFileBytes+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return JobResultFileContent{}, apperror.New(
			"JOB_RESULT_READ_FAILED",
			"storage",
			false,
			"The job result could not be read.",
			err,
		)
	}
	if len(payload) > maxResultFileBytes {
		return JobResultFileContent{}, apperror.New(
			"JOB_RESULT_TOO_LARGE",
			"storage",
			false,
			"The job result is too large to display.",
			nil,
		)
	}
	mimeType := detectResultMIME(payload)
	return JobResultFileContent{
		MIME:    mimeType,
		DataURL: "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(payload),
		Size:    int64(len(payload)),
	}, nil
}

// detectResultMIME sniffs the leading bytes. It is deliberately a small local
// helper rather than an import of the FileStore's internal detector, because
// the display MIME and the storage MIME must agree on the types the UI can
// render.
func detectResultMIME(payload []byte) string {
	switch {
	case bytes.HasPrefix(payload, []byte{0x89, 'P', 'N', 'G'}):
		return "image/png"
	case bytes.HasPrefix(payload, []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg"
	case len(payload) >= 12 && bytes.HasPrefix(payload, []byte("RIFF")) && bytes.Equal(payload[8:12], []byte("WEBP")):
		return "image/webp"
	case bytes.HasPrefix(payload, []byte("GIF8")):
		return "image/gif"
	case len(payload) >= 12 && bytes.Equal(payload[4:8], []byte("ftyp")):
		return "video/mp4"
	case bytes.HasPrefix(payload, []byte("ID3")):
		return "audio/mpeg"
	case len(payload) >= 12 && bytes.HasPrefix(payload, []byte("RIFF")) && bytes.Equal(payload[8:12], []byte("WAVE")):
		return "audio/wav"
	default:
		return "application/octet-stream"
	}
}
