package providers

import (
	"context"
	"net/http"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
	phttp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providerhttp"
)

// HealthChecker performs a bounded reachability probe against a provider's
// /models endpoint. It never logs or returns response bodies.
type HealthChecker struct {
	registry      *Registry
	clientFactory func(config provider.Config) (*phttp.Client, error)
}

// NewHealthChecker builds the health checker.
func NewHealthChecker(registry *Registry) *HealthChecker {
	return &HealthChecker{registry: registry}
}

// Check probes the provider and returns a safe health state. The probe is
// recorded in the redacted audit trail like any other provider call
// (FR-140: every call is traceable, with no sensitive headers).
func (h *HealthChecker) Check(ctx context.Context, providerID string) (provider.HealthState, error) {
	if h == nil || h.registry == nil || h.registry.configs == nil {
		return provider.HealthState{}, provider.NewUnsupportedError()
	}
	config, err := h.registry.configs.GetConfig(ctx, providerID)
	if err != nil {
		return provider.HealthState{}, err
	}
	started := time.Now()
	state, probeErr := h.probe(ctx, config)
	h.record(ctx, config, started, state, probeErr)
	return state, nil
}

func (h *HealthChecker) probe(ctx context.Context, config provider.Config) (provider.HealthState, error) {
	client, err := h.clientFor(config)
	if err != nil {
		return provider.HealthState{}, err
	}
	endpoint, err := endpointFor(config, modelsPath)
	if err != nil {
		return provider.HealthState{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return provider.HealthState{}, provider.NewConfigurationError()
	}
	if h.registry.secrets != nil {
		secret, secretErr := h.registry.secrets.ResolveInternal(ctx, config.ID)
		if secretErr != nil {
			return provider.HealthState{
				ProviderID: config.ID,
				Healthy:    false,
				CheckedAt:  time.Now().UTC(),
				Detail:     "credentials unavailable",
			}, nil
		}
		defer zeroBytes(secret)
		if len(secret) > 0 {
			request.Header.Set("Authorization", "Bearer "+string(secret))
		}
	}
	response, err := client.Do(ctx, request)
	if err != nil {
		return provider.HealthState{
			ProviderID: config.ID,
			Healthy:    false,
			CheckedAt:  time.Now().UTC(),
			Detail:     detailForError(err),
		}, nil
	}
	defer response.Body.Close()
	// Drain a bounded amount so the connection can be reused, but never
	// surface the body.
	drainBounded(response, 4096)
	healthy := response.StatusCode == http.StatusOK ||
		response.StatusCode == http.StatusUnauthorized ||
		response.StatusCode == http.StatusForbidden
	detail := "reachable"
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		detail = "reachable, credentials rejected"
	}
	if !healthy {
		detail = detailForStatus(response.StatusCode)
	}
	return provider.HealthState{
		ProviderID: config.ID,
		Healthy:    healthy,
		CheckedAt:  time.Now().UTC(),
		Detail:     detail,
	}, nil
}

// record writes the redacted audit row. Audit failures never change the probe
// result the caller sees.
func (h *HealthChecker) record(ctx context.Context, config provider.Config, started time.Time, state provider.HealthState, probeErr error) {
	if h.registry == nil || h.registry.audit == nil {
		return
	}
	status := provider.StatusSucceeded
	if probeErr != nil {
		status = statusForError(probeErr)
	} else if !state.Healthy {
		status = provider.StatusFailed
	}
	record := provider.RequestRecord{
		ID:         newRecordID(),
		ProviderID: config.ID,
		Capability: provider.CapabilityText,
		Status:     status,
		LatencyMS:  time.Since(started).Milliseconds(),
		CreatedAt:  time.Now().UTC(),
	}
	if probeErr != nil {
		if providerErr, ok := provider.AsProviderError(probeErr); ok {
			record.ErrorCode = string(providerErr.Category)
		}
	}
	_ = h.registry.audit.SaveRequestRecord(ctx, record)
}

func (h *HealthChecker) clientFor(config provider.Config) (*phttp.Client, error) {
	if h.clientFactory != nil {
		return h.clientFactory(config)
	}
	return guardedClient(config)
}

// drainBounded reads and discards up to limit bytes so the connection can be
// reused, without ever exposing provider response content to a caller.
func drainBounded(response *http.Response, limit int64) {
	buffer := make([]byte, 512)
	var total int64
	for total < limit {
		n, err := response.Body.Read(buffer)
		total += int64(n)
		if err != nil {
			return // EOF or transport end is fine for a probe
		}
	}
}

func detailForError(err error) string {
	if providerErr, ok := provider.AsProviderError(err); ok {
		switch providerErr.Category {
		case provider.CategorySecurity:
			return "blocked by policy"
		case provider.CategoryTimeout:
			return "timed out"
		case provider.CategoryCancelled:
			return "cancelled"
		case provider.CategoryNetwork:
			return "unreachable"
		case provider.CategoryConfiguration:
			return "configuration invalid"
		}
	}
	return "unreachable"
}

func detailForStatus(status int) string {
	switch {
	case status == http.StatusTooManyRequests:
		return "rate limited"
	case status >= 500:
		return "provider error"
	case status >= 400:
		return "request rejected"
	default:
		return "unexpected response"
	}
}
