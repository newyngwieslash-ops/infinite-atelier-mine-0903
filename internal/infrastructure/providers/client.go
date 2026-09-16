package providers

import (
	"strings"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
	phttp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providerhttp"
)

// endpointFor joins a validated base URL with an API path. The result never
// contains credentials or fragments because validation rejects them.
func endpointFor(config provider.Config, apiPath string) (string, error) {
	normalized, ok := provider.ValidateBaseURL(config.BaseURL)
	if !ok {
		return "", provider.NewConfigurationError()
	}
	basePath := strings.TrimRight(normalized.Path, "/")
	host := normalized.Host
	if normalized.Port != "" {
		host = host + ":" + normalized.Port
	}
	return normalized.Scheme + "://" + host + basePath + apiPath, nil
}

// policyFor builds the request policy for a config. It is the single source of
// the port the guarded client enforces.
func policyFor(config provider.Config) (phttp.Policy, error) {
	normalized, ok := provider.ValidateBaseURL(config.BaseURL)
	if !ok {
		return phttp.Policy{}, provider.NewConfigurationError()
	}
	port := normalized.Port
	if port == "" {
		port = normalized.DefaultPort()
	}
	return phttp.Policy{
		Host:       normalized.Host,
		Port:       port,
		Scheme:     normalized.Scheme,
		AllowLocal: config.LocalApproved,
	}, nil
}

// guardedClient builds the SSRF-guarded HTTP client for a provider config.
// Every adapter and the health checker must use this single constructor so
// policy cannot drift between call paths.
func guardedClient(config provider.Config) (*phttp.Client, error) {
	policy, err := policyFor(config)
	if err != nil {
		return nil, err
	}
	return phttp.NewClient(policy, phttp.NewNetResolver(), phttp.Limits{}), nil
}

func zeroBytes(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
