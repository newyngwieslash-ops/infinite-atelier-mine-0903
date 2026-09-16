package desktop

import (
	"context"
	"sync"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/health"
)

// HealthBinding is the only WP-01 Wails method surface.
type HealthBinding struct {
	mu      sync.RWMutex
	ctx     context.Context
	service *health.Service
}

// Get returns the current health snapshot. It has no setter and exposes no SQL.
func (b *HealthBinding) Get() health.Snapshot {
	if b == nil {
		return health.Snapshot{Database: "safe", SafeMode: true}
	}
	b.mu.RLock()
	ctx := b.ctx
	service := b.service
	b.mu.RUnlock()
	if ctx == nil || service == nil {
		return health.Snapshot{Database: "safe", SafeMode: true}
	}
	return service.Snapshot(ctx)
}

// Attach stores the Wails startup context and health service. It is not a Wails method.
func Attach(binding *HealthBinding, ctx context.Context, service *health.Service) {
	if binding == nil {
		return
	}
	binding.attach(ctx, service)
}

func (b *HealthBinding) attach(ctx context.Context, service *health.Service) {
	b.mu.Lock()
	b.ctx = ctx
	b.service = service
	b.mu.Unlock()
}
