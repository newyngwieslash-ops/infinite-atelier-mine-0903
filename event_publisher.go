package main

import (
	"context"
	"sync"
	"time"

	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	appproviders "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/providers"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/desktop"
)

// eventPublisher adapts Wails event emission to the provider stream and job
// publishers. It emits only the typed envelope shape used by the desktop
// adapter and never logs payload content.
//
// Job events are throttled per job so a burst of progress updates cannot flood
// the UI: NFR-001 caps progress writes at ten per second.
type eventPublisher struct {
	emit        func(context.Context, string, ...interface{})
	newEnvelope func(string, any) (desktop.Envelope, error)

	mu       sync.Mutex
	lastSent map[string]time.Time
}

// jobEventInterval is the minimum spacing between two job events for one job.
const jobEventInterval = 100 * time.Millisecond

// PublishStreamEvent emits a provider:text_stream core event. Emission
// failures are swallowed: the stream result is owned by the caller, and a
// missing event must not corrupt the request lifecycle.
func (p *eventPublisher) PublishStreamEvent(ctx context.Context, event appproviders.StreamEvent) {
	if p == nil || p.emit == nil || p.newEnvelope == nil || ctx == nil {
		return
	}
	envelope, err := p.newEnvelope(appproviders.StreamEventName, event)
	if err != nil {
		return
	}
	p.emit(ctx, desktop.CoreEventName, envelope)
}

// PublishJobChanged implements the application jobs Publisher port.
//
// Terminal transitions are never dropped (the UI must see the final state);
// intermediate progress updates are coalesced to at most one per
// jobEventInterval.
func (p *eventPublisher) PublishJobChanged(ctx context.Context, event appjobs.JobEvent) {
	if p == nil || p.emit == nil || p.newEnvelope == nil || ctx == nil {
		return
	}
	if !p.allow(event) {
		return
	}
	envelope, err := p.newEnvelope(appjobs.JobEventName, event)
	if err != nil {
		return
	}
	p.emit(ctx, desktop.CoreEventName, envelope)
}

// allow applies the per-job throttle. Terminal statuses always pass.
func (p *eventPublisher) allow(event appjobs.JobEvent) bool {
	if isTerminalJobStatus(event.Status) {
		return true
	}
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.lastSent == nil {
		p.lastSent = map[string]time.Time{}
	}
	if last, ok := p.lastSent[event.JobID]; ok && now.Sub(last) < jobEventInterval {
		return false
	}
	p.lastSent[event.JobID] = now
	return true
}

// isTerminalJobStatus reports whether a job status ends the job's life.
func isTerminalJobStatus(status string) bool {
	switch status {
	case "succeeded", "remote_only", "failed", "cancelled", "orphaned":
		return true
	default:
		return false
	}
}
