package providers

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// blockingPort streams one delta then blocks until the context is cancelled,
// so the cancel-by-stream-ID path can be exercised.
type blockingPort struct {
	started chan struct{}
	once    sync.Once
}

func (p *blockingPort) Generate(context.Context, TextRequest) (TextResult, error) {
	return TextResult{}, nil
}

func (p *blockingPort) Stream(ctx context.Context, _ TextRequest, sink EventSink) error {
	p.once.Do(func() { close(p.started) })
	sink.OnDelta("first")
	<-ctx.Done()
	return provider.NewCancelledError()
}

type recordingPublisher struct {
	mu     sync.Mutex
	events []StreamEvent
}

func (p *recordingPublisher) PublishStreamEvent(_ context.Context, event StreamEvent) {
	p.mu.Lock()
	p.events = append(p.events, event)
	p.mu.Unlock()
}

func (p *recordingPublisher) snapshot() []StreamEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]StreamEvent, len(p.events))
	copy(out, p.events)
	return out
}

type staticTextResolver struct{ port TextPort }

func (r staticTextResolver) TextPortFor(context.Context, string) (TextPort, error) {
	return r.port, nil
}

// errorResolver always fails, proving resolver errors surface to the caller.
type errorResolver struct{ err error }

func (r errorResolver) TextPortFor(context.Context, string) (TextPort, error) {
	return nil, r.err
}

func TestRequestServiceCancelByStreamID(t *testing.T) {
	port := &blockingPort{started: make(chan struct{})}
	publisher := &recordingPublisher{}
	service := NewRequestService(staticTextResolver{port: port}, publisher)

	streamID, err := service.StartStream(context.Background(), TextRequest{
		ProviderID: "prov-1",
		Model:      "gpt-test",
		Messages:   []TextMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("StartStream: %v", err)
	}
	if streamID == "" {
		t.Fatal("empty stream ID")
	}

	select {
	case <-port.started:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not start")
	}
	if service.ActiveStreams() != 1 {
		t.Fatalf("active streams = %d, want 1", service.ActiveStreams())
	}

	if err := service.CancelStream(streamID); err != nil {
		t.Fatalf("CancelStream: %v", err)
	}
	// The goroutine removes the entry once the port returns.
	deadline := time.Now().Add(2 * time.Second)
	for service.ActiveStreams() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if service.ActiveStreams() != 0 {
		t.Fatalf("stream still active after cancel: %d", service.ActiveStreams())
	}

	// A cancellation event reached the publisher.
	events := publisher.snapshot()
	foundCancelled := false
	for _, event := range events {
		if event.Event.ErrorCode == string(provider.CategoryCancelled) {
			foundCancelled = true
		}
	}
	if !foundCancelled {
		t.Fatalf("no cancellation event published: %+v", events)
	}
}

func TestRequestServiceCancelUnknownStreamIsNoOp(t *testing.T) {
	service := NewRequestService(staticTextResolver{port: &blockingPort{started: make(chan struct{})}}, nil)
	if err := service.CancelStream("does-not-exist"); err != nil {
		t.Fatalf("CancelStream(unknown) = %v", err)
	}
}

func TestRequestServiceCancelAllStopsStreams(t *testing.T) {
	port := &blockingPort{started: make(chan struct{})}
	service := NewRequestService(staticTextResolver{port: port}, nil)
	if _, err := service.StartStream(context.Background(), TextRequest{ProviderID: "p", Model: "m", Messages: []TextMessage{{Role: "user", Content: "x"}}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-port.started:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not start")
	}
	if err := service.CancelAll(); err != nil {
		t.Fatalf("CancelAll: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for service.ActiveStreams() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if service.ActiveStreams() != 0 {
		t.Fatalf("streams still active after CancelAll: %d", service.ActiveStreams())
	}
}

func TestRequestServiceValidationAndResolverErrors(t *testing.T) {
	service := NewRequestService(staticTextResolver{port: &blockingPort{started: make(chan struct{})}}, nil)
	if _, err := service.StartStream(context.Background(), TextRequest{}); err == nil {
		t.Fatal("invalid request accepted")
	}

	failing := NewRequestService(errorResolver{err: provider.NewUnauthorizedError()}, nil)
	if _, err := failing.StartStream(context.Background(), TextRequest{ProviderID: "p", Model: "m", Messages: []TextMessage{{Role: "user", Content: "x"}}}); err == nil {
		t.Fatal("resolver failure ignored")
	}
}

func TestRequestServiceNilResolverFailsClosed(t *testing.T) {
	service := NewRequestService(nil, nil)
	if _, err := service.StartStream(context.Background(), TextRequest{ProviderID: "p", Model: "m", Messages: []TextMessage{{Role: "user", Content: "x"}}}); err == nil {
		t.Fatal("nil resolver accepted a stream")
	}
	if _, err := service.Generate(context.Background(), TextRequest{ProviderID: "p", Model: "m", Messages: []TextMessage{{Role: "user", Content: "x"}}}); err == nil {
		t.Fatal("nil resolver accepted a generate")
	}
}

func TestPublishingSinkMapsErrorCategory(t *testing.T) {
	publisher := &recordingPublisher{}
	sink := &publishingSink{ctx: context.Background(), publisher: publisher, streamID: "s1"}
	sink.OnError(provider.NewTimeoutError())
	events := publisher.snapshot()
	if len(events) != 1 || events[0].Event.ErrorCode != string(provider.CategoryTimeout) {
		t.Fatalf("events = %+v", events)
	}

	// Non-provider errors degrade to the network category, never to raw text.
	sink2 := &publishingSink{ctx: context.Background(), publisher: publisher, streamID: "s2"}
	sink2.OnError(errors.New("some backend detail with sk-live-secret-0001"))
	events = publisher.snapshot()
	if len(events) != 2 {
		t.Fatalf("events = %+v", events)
	}
	if events[1].Event.ErrorCode != string(provider.CategoryNetwork) {
		t.Fatalf("unmapped error category = %q", events[1].Event.ErrorCode)
	}
}

// TestPublishingSinkEmitsOneTerminalEvent proves cancellation and a late
// adapter completion cannot both produce a terminal event.
func TestPublishingSinkEmitsOneTerminalEvent(t *testing.T) {
	publisher := &recordingPublisher{}
	sink := &publishingSink{ctx: context.Background(), publisher: publisher, streamID: "s1"}
	sink.cancelWith(provider.NewCancelledError())
	sink.OnDone(TextResult{Content: "late"})
	sink.OnError(provider.NewTimeoutError())
	events := publisher.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected exactly one terminal event, got %+v", events)
	}
	if events[0].Event.ErrorCode != string(provider.CategoryCancelled) {
		t.Fatalf("terminal event = %+v", events[0])
	}
}

// TestPublishingSinkSuppressesDeltaAfterTerminal proves a chunk read by the
// adapter just before cancellation cannot reach the consumer after the
// terminal event.
func TestPublishingSinkSuppressesDeltaAfterTerminal(t *testing.T) {
	publisher := &recordingPublisher{}
	sink := &publishingSink{ctx: context.Background(), publisher: publisher, streamID: "s1"}
	sink.OnDelta("before")
	sink.cancelWith(provider.NewCancelledError())
	sink.OnDelta("late-delta")
	events := publisher.snapshot()
	if len(events) != 2 {
		t.Fatalf("expected delta + terminal, got %+v", events)
	}
	if events[0].Event.Delta != "before" {
		t.Fatalf("first event = %+v", events[0])
	}
	if events[1].Event.Delta != "" || events[1].Event.ErrorCode == "" {
		t.Fatalf("delta published after terminal: %+v", events[1])
	}
}

// TestPublishingSinkNilPublisherIsSafe proves composition without an event
// bridge does not panic.
func TestPublishingSinkNilPublisherIsSafe(t *testing.T) {
	sink := &publishingSink{ctx: context.Background(), streamID: "s1"}
	sink.OnDelta("x")
	sink.OnDone(TextResult{Content: "x"})
	sink.OnError(provider.NewTimeoutError())
}
