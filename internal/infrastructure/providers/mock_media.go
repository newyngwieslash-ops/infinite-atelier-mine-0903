package providers

import (
	"context"
	"encoding/base64"
	"strings"
	"sync"
	"time"

	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// The MIME values the mock payloads sniff as. They are named constants so the
// allowlist and the mock cannot drift apart silently.
const (
	detectedMockVideoMIME = "video/mp4"
	detectedMockAudioMIME = "audio/wave"
)

// MockVideoAdapter implements the async video contract without contacting any
// provider. WP-03 requires the contract and a mock so the whole job pipeline —
// submit, poll, download, verify, cancel, recover — is exercisable end to end;
// the real video adapter belongs to the media work package.
//
// The mock is deliberately in production code but is only reachable through an
// explicit provider kind, so a real deployment cannot silently fall back to it.
type MockVideoAdapter struct {
	mu sync.Mutex
	// jobs tracks submissions so Poll can report progress deterministically.
	jobs map[string]mockVideoJob
	// now allows tests to control completion timing.
	now func() time.Time
	// pollCount is how many polls a job needs before it finishes.
	pollTarget int
}

type mockVideoJob struct {
	job       appjobs.RemoteJob
	request   appjobs.VideoRequest
	polls     int
	cancelled bool
	failed    bool
}

// NewMockVideoAdapter builds the mock.
func NewMockVideoAdapter() *MockVideoAdapter {
	return &MockVideoAdapter{jobs: map[string]mockVideoJob{}, now: time.Now, pollTarget: 2}
}

// Submit accepts a job and returns a synthetic remote ID.
func (m *MockVideoAdapter) Submit(_ context.Context, request appjobs.VideoRequest) (appjobs.RemoteJob, error) {
	if m == nil {
		return appjobs.RemoteJob{}, provider.NewUnsupportedError()
	}
	if strings.TrimSpace(request.Prompt) == "" {
		return appjobs.RemoteJob{}, provider.NewInvalidInputError()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	remote := appjobs.RemoteJob{ProviderID: request.ProviderID, ID: "mock-video-" + request.JobID}
	m.jobs[remote.ID] = mockVideoJob{job: remote, request: request}
	return remote, nil
}

// Poll reports progress and eventually completion or failure.
func (m *MockVideoAdapter) Poll(_ context.Context, remote appjobs.RemoteJob) (appjobs.RemoteStatus, error) {
	if m == nil {
		return appjobs.RemoteStatus{}, provider.NewUnsupportedError()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.jobs[remote.ID]
	if !ok {
		return appjobs.RemoteStatus{}, provider.NewRemotePermanentError()
	}
	if entry.cancelled {
		return appjobs.RemoteStatus{Done: true, Failed: true, Message: "cancelled"}, nil
	}
	entry.polls++
	m.jobs[remote.ID] = entry
	if entry.failed {
		return appjobs.RemoteStatus{Done: true, Failed: true, Message: "mock failure"}, nil
	}
	progress := 50
	if entry.polls >= m.pollTarget {
		done := 100
		return appjobs.RemoteStatus{Done: true, Progress: &done}, nil
	}
	return appjobs.RemoteStatus{Done: false, Progress: &progress}, nil
}

// Fetch returns a deterministic payload so the download/verify path is real.
func (m *MockVideoAdapter) Fetch(_ context.Context, remote appjobs.RemoteJob) (appjobs.MediaOutcome, error) {
	if m == nil {
		return appjobs.MediaOutcome{}, provider.NewUnsupportedError()
	}
	m.mu.Lock()
	_, ok := m.jobs[remote.ID]
	m.mu.Unlock()
	if !ok {
		return appjobs.MediaOutcome{}, provider.NewRemotePermanentError()
	}
	// A minimal MP4 ftyp box that Go's content sniffer accepts as video/mp4.
	//
	// The layout matters: the sniffer reads the 4-byte box size, requires it to
	// be a multiple of four and within the buffer, then scans 4-byte-aligned
	// brands for one containing "mp4" while skipping the major-brand slot. A box
	// whose brands are not aligned like this is reported as
	// application/octet-stream and rejected by the result allowlist, which would
	// make the mock unusable end to end.
	ftyp := []byte{
		0x00, 0x00, 0x00, 0x18, // box size: 24 bytes
		'f', 't', 'y', 'p',
		'i', 's', 'o', 'm', // major brand (skipped by the sniffer)
		0x00, 0x00, 0x02, 0x00, // minor version
		'm', 'p', '4', '1', // compatible brand containing "mp4"
		'i', 's', 'o', 'm', // second compatible brand
	}
	return appjobs.MediaOutcome{
		Data:     base64.StdEncoding.EncodeToString(ftyp),
		MIMEType: detectedMockVideoMIME,
	}, nil
}

// Cancel marks the job cancelled. The real contract cannot stop every provider,
// which is why the job itself records the orphan candidate rather than assuming
// success.
func (m *MockVideoAdapter) Cancel(_ context.Context, remote appjobs.RemoteJob) error {
	if m == nil {
		return provider.NewUnsupportedError()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.jobs[remote.ID]
	if !ok {
		return nil
	}
	entry.cancelled = true
	m.jobs[remote.ID] = entry
	return nil
}

// MockAudioAdapter implements the audio contract without contacting a provider.
type MockAudioAdapter struct {
	// now allows tests to observe ordering without sleeping.
	now func() time.Time
}

// NewMockAudioAdapter builds the mock.
func NewMockAudioAdapter() *MockAudioAdapter {
	return &MockAudioAdapter{now: time.Now}
}

// GenerateAudio returns a small deterministic WAV payload.
func (m *MockAudioAdapter) GenerateAudio(_ context.Context, request appjobs.AudioRequest) (appjobs.AudioOutcome, error) {
	if m == nil {
		return appjobs.AudioOutcome{}, provider.NewUnsupportedError()
	}
	if strings.TrimSpace(request.Text) == "" {
		return appjobs.AudioOutcome{}, provider.NewInvalidInputError()
	}
	// A 44-byte canonical WAV header with no samples: valid magic bytes,
	// negligible size, and no copyrighted content.
	header := []byte{
		'R', 'I', 'F', 'F', 36, 0, 0, 0, 'W', 'A', 'V', 'E',
		'f', 'm', 't', ' ', 16, 0, 0, 0, 1, 0, 1, 0,
		0x40, 0x1F, 0, 0, 0x80, 0x3E, 0, 0, 2, 0, 16, 0,
		'd', 'a', 't', 'a', 0, 0, 0, 0,
	}
	return appjobs.AudioOutcome{
		Data:     base64.StdEncoding.EncodeToString(header),
		MIMEType: detectedMockAudioMIME,
	}, nil
}

// Compile-time proof that the mocks satisfy the capability ports they stand in
// for. A signature drift breaks the build rather than surfacing at runtime.
var (
	_ appjobs.VideoPort = (*MockVideoAdapter)(nil)
	_ appjobs.AudioPort = (*MockAudioAdapter)(nil)
)

// NewMockVideoRequest builds a video request for tests and local exercises.
func NewMockVideoRequest(jobID, providerID string) appjobs.VideoRequest {
	return appjobs.VideoRequest{JobID: jobID, ProviderID: providerID, Prompt: "mock", Seconds: 1}
}

// NewMockAudioRequest builds an audio request for tests and local exercises.
func NewMockAudioRequest(jobID, providerID string) appjobs.AudioRequest {
	return appjobs.AudioRequest{JobID: jobID, ProviderID: providerID, Text: "mock"}
}
