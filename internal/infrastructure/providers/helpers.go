package providers

import (
	"encoding/base64"
	"strconv"
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// itoa renders a small integer without importing strconv at each call site.
func itoa(value int) string { return strconv.Itoa(value) }

// encodeBase64 renders bytes the way the ASYNC video protocol's two directions need them: the
// references it sends become data URLs, and the inline result it receives becomes a data payload.
//
// The two call sites are both in openai_video.go, and the mock does NOT go through it — the mock
// writes `base64.StdEncoding` itself, which is why this comment does not claim to have unified them.
// What earns the helper its place is that the ENCODING is part of a wire protocol rather than an
// implementation detail: a provider expects standard base64 with padding, and a reader comparing this
// implementation against another should find that choice in one place instead of inferring it from
// which variant happened to be imported.
func encodeBase64(payload []byte) string { return base64.StdEncoding.EncodeToString(payload) }

// decodeImageData accepts a data URL or bare base64 payload and returns the raw
// bytes plus the declared MIME type when the payload carried one.
func decodeImageData(data string) ([]byte, string, error) {
	trimmed := strings.TrimSpace(data)
	if trimmed == "" {
		return nil, "", provider.NewInvalidInputError()
	}
	mimeType := ""
	if strings.HasPrefix(trimmed, "data:") {
		comma := strings.IndexByte(trimmed, ',')
		if comma < 0 {
			return nil, "", provider.NewInvalidInputError()
		}
		header := trimmed[len("data:"):comma]
		if idx := strings.IndexByte(header, ';'); idx >= 0 {
			mimeType = header[:idx]
		} else {
			mimeType = header
		}
		trimmed = trimmed[comma+1:]
	}
	if decoded, err := base64.StdEncoding.DecodeString(trimmed); err == nil {
		return decoded, mimeType, nil
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(trimmed, "=")); err == nil {
		return decoded, mimeType, nil
	}
	return nil, "", provider.NewInvalidInputError()
}
