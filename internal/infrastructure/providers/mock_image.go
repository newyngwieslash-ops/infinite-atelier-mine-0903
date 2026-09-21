package providers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"hash/fnv"
	"image"
	"image/color"
	"image/png"
	"strings"
	"sync"

	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// MockImageAdapter implements the image port without contacting any provider.
//
// # Why it exists
//
// AGENT_CONTRACTS §18.3 forbids a CI run from calling a paid provider, and
// AC-BOARD-003 is a test about MULTIPLE image jobs: two candidates a shot, a
// concurrency limit, a cancellation, a retry and an approval. None of that can be
// exercised against a real adapter in CI, and the existing mock media adapter answers
// the VIDEO port rather than this one — a different interface with a different
// contract, which is why reusing it would not compile and would not be honest if it
// did.
//
// # The three guardrails, the same three KindMockText has
//
//  1. It is resolved ONLY for `provider.KindMockImage`, through the registry's kind
//     switch. No other kind reaches it.
//  2. `KindMockImage` is refused by `provider.IsUserConfigurableKind`, which the
//     application layer checks before it will persist a configuration — so no client can
//     register one, and the kind cannot appear in `provider_configs` at all.
//  3. It is registered by an explicit `WithMockImageAdapter` call at the composition
//     root, so a build that did not opt in reports "unsupported" rather than being
//     answered by a mock that appeared anyway.
//
// # The bytes are a real PNG
//
// The adapter produces a DECODABLE PNG rather than a fixed byte string, and the
// difference matters: the job pipeline sniffs a result's MIME type and the FileStore
// stores bytes it must be able to hash and serve. A payload that were merely
// "image/png-ish" would pass a length check and fail the first real reader, which is the
// shape of a mock that makes a broken pipeline look healthy.
//
// The image is DETERMINISTIC in its prompt: the same prompt produces the same pixels,
// so a test can assert that two candidates differ from each other and that a retry
// reproduces the first attempt exactly.
type MockImageAdapter struct {
	mu sync.Mutex
	// calls records what was asked for, so a test can assert that a cancelled job
	// stopped calling and that a retry sent the same request.
	calls []appjobs.ImageRequest
	// failures is a queue of prompts that should fail once, so a test can exercise the
	// retry path without a real transient fault.
	failures map[string]int
	// size is the pixel dimension of every produced image.
	size int
}

// NewMockImageAdapter builds the mock.
func NewMockImageAdapter() *MockImageAdapter {
	return &MockImageAdapter{failures: map[string]int{}, size: 32}
}

// Generate produces one deterministic PNG per requested image.
//
// It answers INLINE rather than with a remote job, because the image contract's
// asynchronous arm is what the video port is for: a real image provider returns bytes or
// a URL in the response, and an adapter that pretended otherwise would make every test
// of the download path test the wrong thing.
func (m *MockImageAdapter) Generate(_ context.Context, request appjobs.ImageRequest) (appjobs.ImageOutcome, error) {
	if m == nil {
		return appjobs.ImageOutcome{}, provider.NewUnsupportedError()
	}
	prompt := strings.TrimSpace(request.Prompt)
	if prompt == "" {
		return appjobs.ImageOutcome{}, provider.NewInvalidInputError()
	}
	m.mu.Lock()
	if remaining := m.failures[prompt]; remaining > 0 {
		m.failures[prompt] = remaining - 1
		m.calls = append(m.calls, request)
		m.mu.Unlock()
		// A TRANSIENT failure, which is the category the job pipeline retries. It uses
		// the taxonomy's remote-transient constructor rather than an invented category,
		// so the runner's classification sees the same shape a real 5xx would produce.
		return appjobs.ImageOutcome{}, provider.NewRemoteTransientError()
	}
	m.calls = append(m.calls, request)
	m.mu.Unlock()

	count := request.Count
	if count <= 0 {
		count = 1
	}
	results := make([]appjobs.ImageResult, 0, count)
	for index := 0; index < count; index++ {
		// THE PROMPT IS THE CONTENT, and the index is the variation: two candidates of
		// one shot carry the same prompt and must DIFFER, or a test asserting that the
		// batch produced two candidates would pass against one image copied twice.
		pixels, err := m.render(prompt, index)
		if err != nil {
			// A render failure is the mock's OWN bug, and the taxonomy has no category for
			// that: it is reported as an invalid response, which is the closest honest
			// description of "the adapter produced bytes it could not encode".
			return appjobs.ImageOutcome{}, provider.NewResponseInvalidError()
		}
		results = append(results, appjobs.ImageResult{
			Data:     base64.StdEncoding.EncodeToString(pixels),
			MIMEType: "image/png",
			// A revised prompt is provider-reported and informational; the mock reports
			// the one it actually drew, so a caller reading it sees the truth.
			RevisedPrompt: prompt,
		})
	}
	return appjobs.ImageOutcome{Results: results}, nil
}

// render draws one image.
func (m *MockImageAdapter) render(prompt string, index int) ([]byte, error) {
	dimension := m.size
	if dimension <= 0 {
		dimension = 32
	}
	// The seed is the PROMPT's hash plus the candidate index, so the same request
	// reproduces the same bytes and a different candidate does not.
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(prompt))
	seed := hasher.Sum64() + uint64(index)*2654435761

	canvas := image.NewRGBA(image.Rect(0, 0, dimension, dimension))
	for y := 0; y < dimension; y++ {
		for x := 0; x < dimension; x++ {
			// A small deterministic pattern rather than noise: a solid colour would make
			// two candidates look identical to a pixel-diff assertion that used only the
			// first byte, and a real PNG compresses either way.
			value := byte((seed>>uint((x+y)%8))&0xFF) ^ byte((x*7+y*13)%251)
			canvas.Set(x, y, color.RGBA{
				R: value,
				G: byte((uint64(value) + seed>>8) & 0xFF),
				B: byte((uint64(value) + uint64(x) + uint64(y)) & 0xFF),
				A: 0xFF,
			})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, canvas); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// FailNext makes the next `count` calls for a prompt fail transiently.
//
// It is the seam AC-BOARD-003's "重试失败项" needs: a test asks the mock to fail once and
// then asserts that the retry succeeded, which is a statement about the runner's
// classification rather than about this adapter.
func (m *MockImageAdapter) FailNext(prompt string, count int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failures[strings.TrimSpace(prompt)] = count
}

// Calls returns what the adapter was asked for, oldest first.
func (m *MockImageAdapter) Calls() []appjobs.ImageRequest {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]appjobs.ImageRequest{}, m.calls...)
}

// CallCount reports how many calls reached the adapter.
func (m *MockImageAdapter) CallCount() int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// PNGSignature reports whether a payload is a PNG by its first eight bytes.
//
// It is exported because a test asserting the mock produced a real image needs the same
// check the FileStore's sniffer makes, and a test that inlined the magic bytes would be
// a second copy of a constant.
func PNGSignature(payload []byte) bool {
	magic := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	if len(payload) < len(magic) {
		return false
	}
	return bytes.Equal(payload[:len(magic)], magic)
}

// pngDimensions reads a PNG's width and height from its header.
//
// It is here rather than in a test because the header's layout is the format's, and a
// test that hard-coded the offsets would be asserting against its own copy of the
// specification rather than against the bytes the encoder wrote.
func pngDimensions(payload []byte) (int, int, bool) {
	// 8 magic bytes, then a 4-byte length, "IHDR", then width and height as big-endian
	// uint32 each.
	const headerEnd = 8 + 4 + 4 + 8
	if !PNGSignature(payload) || len(payload) < headerEnd {
		return 0, 0, false
	}
	width := binary.BigEndian.Uint32(payload[16:20])
	height := binary.BigEndian.Uint32(payload[20:24])
	return int(width), int(height), true
}
