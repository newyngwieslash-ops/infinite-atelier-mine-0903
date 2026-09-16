package providers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// StreamEventName is the Wails event the frontend subscribes to for stream
// deltas. Each event carries the stream ID plus a TextEvent.
const StreamEventName = "provider:text_stream"

// StreamEvent is the event envelope delivered to subscribers.
type StreamEvent struct {
	StreamID string    `json:"streamId"`
	Event    TextEvent `json:"event"`
}

// EventPublisher publishes stream events to the desktop event bridge.
type EventPublisher interface {
	PublishStreamEvent(ctx context.Context, event StreamEvent)
}

// RequestService owns in-flight streaming text requests so the frontend can
// cancel them by ID. Cancellation is Go-side (context cancellation), and the
// service is safe for concurrent use.
type RequestService struct {
	resolver  TextPortResolver
	publisher EventPublisher

	mu      sync.Mutex
	streams map[string]*streamState
}

// streamState tracks one in-flight stream. The sink is shared with the cancel
// path so a cancellation publishes exactly one terminal event no matter which
// side observes the stop first.
type streamState struct {
	cancel context.CancelFunc
	sink   *publishingSink
}

// NewRequestService builds a stream-managing request service over a text port
// resolver. The publisher may be nil (tests, headless composition).
func NewRequestService(resolver TextPortResolver, publisher EventPublisher) *RequestService {
	return &RequestService{resolver: resolver, publisher: publisher, streams: map[string]*streamState{}}
}

// Generate runs a non-streaming request through the resolved adapter.
func (s *RequestService) Generate(ctx context.Context, request TextRequest) (TextResult, error) {
	if s == nil || s.resolver == nil {
		return TextResult{}, provider.NewUnsupportedError()
	}
	if err := request.Validate(); err != nil {
		return TextResult{}, err
	}
	port, err := s.resolver.TextPortFor(ctx, request.ProviderID)
	if err != nil {
		return TextResult{}, err
	}
	return port.Generate(ctx, request)
}

// StartStream begins an asynchronous streaming request and returns its ID.
// Events are published through the EventPublisher; the final event or error
// terminates the stream.
func (s *RequestService) StartStream(parent context.Context, request TextRequest) (string, error) {
	if s == nil || s.resolver == nil {
		return "", provider.NewUnsupportedError()
	}
	if err := request.Validate(); err != nil {
		return "", err
	}
	port, err := s.resolver.TextPortFor(parent, request.ProviderID)
	if err != nil {
		return "", err
	}
	streamID, err := newStreamID()
	if err != nil {
		return "", provider.NewStorageError()
	}
	ctx, cancel := context.WithCancel(parent)
	sink := &publishingSink{ctx: ctx, publisher: s.publisher, streamID: streamID}
	s.mu.Lock()
	s.streams[streamID] = &streamState{cancel: cancel, sink: sink}
	s.mu.Unlock()

	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.streams, streamID)
			s.mu.Unlock()
			cancel()
		}()
		if err := port.Stream(ctx, request, sink); err != nil {
			// The adapter normally notifies through the sink; this guard
			// covers adapters that return without notifying and keeps the
			// frontend from waiting forever on a terminal event.
			sink.failWith(err)
		}
	}()
	return streamID, nil
}

// CancelStream cancels an in-flight stream. Cancelling an unknown or finished
// stream is not an error.
//
// The cancellation is published synchronously so the frontend receives a
// deterministic terminal event even if the adapter is slow to unwind its
// network read.
func (s *RequestService) CancelStream(streamID string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	state, ok := s.streams[streamID]
	if ok {
		delete(s.streams, streamID)
	}
	s.mu.Unlock()
	if !ok {
		return nil
	}
	state.sink.cancelWith(provider.NewCancelledError())
	state.cancel()
	return nil
}

// CancelAll cancels every in-flight stream. It is used by shutdown so no
// stream outlives the database handle.
func (s *RequestService) CancelAll() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	states := make([]*streamState, 0, len(s.streams))
	for id, state := range s.streams {
		states = append(states, state)
		delete(s.streams, id)
	}
	s.mu.Unlock()
	for _, state := range states {
		state.sink.cancelWith(provider.NewCancelledError())
		state.cancel()
	}
	return nil
}

// ActiveStreams reports how many streams are in flight (used by tests and
// shutdown sequencing).
func (s *RequestService) ActiveStreams() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.streams)
}

type publishingSink struct {
	ctx       context.Context
	publisher EventPublisher
	streamID  string
	// mu guards terminal-event delivery so cancellation and the adapter's own
	// completion cannot both emit a terminal event.
	mu       sync.Mutex
	terminal bool
}

// OnDelta publishes an incremental chunk. Deltas are suppressed once a
// terminal event has been emitted: cancellation can race with a chunk the
// adapter already read, and a consumer must never see content after the
// stream was declared finished.
func (p *publishingSink) OnDelta(delta string) {
	if !p.isOpen() {
		return
	}
	p.publish(TextEvent{Delta: delta})
}

func (p *publishingSink) OnDone(result TextResult) {
	if !p.beginTerminal() {
		return
	}
	p.publish(TextEvent{Done: true, Delta: result.Content})
}

func (p *publishingSink) OnError(err error) {
	if !p.beginTerminal() {
		return
	}
	p.publish(errorEvent(err))
}

// failWith emits a terminal error only if the stream has not already ended.
func (p *publishingSink) failWith(err error) {
	if !p.beginTerminal() {
		return
	}
	p.publish(errorEvent(err))
}

// cancelWith emits the cancellation terminal event exactly once.
func (p *publishingSink) cancelWith(err error) {
	if !p.beginTerminal() {
		return
	}
	p.publish(errorEvent(err))
}

// errorEvent converts an error into the terminal event payload. It carries the
// stable category, retriability, and a diagnostic ID — never the error text,
// which could contain provider-supplied content.
func errorEvent(err error) TextEvent {
	event := TextEvent{ErrorCode: errorCodeFor(err)}
	if providerErr, ok := provider.AsProviderError(err); ok {
		event.Retriable = providerErr.Retriable
		if providerErr.Diagnostic != "" {
			event.Diagnostic = providerErr.Diagnostic
		}
	}
	if event.Diagnostic == "" {
		event.Diagnostic = newDiagnosticID()
	}
	return event
}

func (p *publishingSink) beginTerminal() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.terminal {
		return false
	}
	p.terminal = true
	return true
}

// isOpen reports whether deltas may still be published.
func (p *publishingSink) isOpen() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.terminal
}

func (p *publishingSink) publish(event TextEvent) {
	if p.publisher == nil {
		return
	}
	p.publisher.PublishStreamEvent(p.ctx, StreamEvent{StreamID: p.streamID, Event: event})
}

func errorCodeFor(err error) string {
	if providerErr, ok := provider.AsProviderError(err); ok {
		return string(providerErr.Category)
	}
	return string(provider.CategoryNetwork)
}

// newDiagnosticID returns a short correlation ID for errors that did not come
// from the provider domain. It contains no user or provider data.
func newDiagnosticID() string {
	var value [8]byte
	if _, err := rand.Read(value[:]); err != nil {
		return ""
	}
	return "diag-" + hex.EncodeToString(value[:])
}

func newStreamID() (string, error) {
	var value [8]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "stream-" + hex.EncodeToString(value[:]), nil
}
