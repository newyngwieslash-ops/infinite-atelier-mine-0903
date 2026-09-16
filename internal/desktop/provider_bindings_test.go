package desktop

import (
	"context"
	"errors"
	"strings"
	"testing"

	appproviders "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/providers"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/secrets"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// leakyStore is a Store whose error text contains a secret-looking value; the
// binding layer must never surface it.
type leakyStore struct{ available bool }

func (s *leakyStore) Put(context.Context, string, []byte) error { return nil }
func (s *leakyStore) Resolve(context.Context, string) ([]byte, error) {
	return nil, errors.New("credential target InfiniteAtelier:provider:p1 leaked")
}
func (s *leakyStore) Delete(context.Context, string) error { return nil }
func (s *leakyStore) Exists(context.Context, string) (bool, error) {
	return false, errors.New("backend failure sk-live-secret-0001")
}
func (s *leakyStore) DisplayHint(context.Context, string) (string, error) { return "", nil }
func (s *leakyStore) Available() bool                                     { return s.available }

func TestSecretsBindingFailsClosedWhenUnattached(t *testing.T) {
	binding := &SecretsBinding{}
	status := binding.Status("p1")
	if status.Configured || status.Available {
		t.Fatalf("unattached binding reported availability: %+v", status)
	}
	if err := binding.Set(SetSecretRequest{ProviderID: "p1", Value: "v"}); err == nil {
		t.Fatal("unattached Set succeeded")
	}
	if err := binding.Delete("p1"); err == nil {
		t.Fatal("unattached Delete succeeded")
	}
}

func TestSecretsBindingStatusDoesNotLeakBackendError(t *testing.T) {
	store := &leakyStore{available: true}
	service := secrets.NewService(store)
	binding := &SecretsBinding{}
	AttachSecrets(binding, context.Background(), service)
	status := binding.Status("p1")
	if strings.Contains(status.DisplayHint, "sk-live") || strings.Contains(status.ProviderID, "sk-live") {
		t.Fatalf("status leaked backend error text: %+v", status)
	}
	if status.Configured {
		t.Fatal("backend failure must report configured=false")
	}
}

func TestSecretsBindingRejectsEmptyInput(t *testing.T) {
	service := secrets.NewService(&leakyStore{available: true})
	binding := &SecretsBinding{}
	AttachSecrets(binding, context.Background(), service)
	if err := binding.Set(SetSecretRequest{ProviderID: "", Value: "v"}); err == nil {
		t.Fatal("empty provider ID accepted")
	}
	if err := binding.Set(SetSecretRequest{ProviderID: "p1", Value: ""}); err == nil {
		t.Fatal("empty value accepted")
	}
}

func TestProvidersBindingFailsClosedWhenUnattached(t *testing.T) {
	binding := &ProvidersBinding{}
	if _, err := binding.ListConfigs(); err == nil {
		t.Fatal("unattached ListConfigs succeeded")
	}
	if _, err := binding.SaveConfig(ProviderConfigRequest{}); err == nil {
		t.Fatal("unattached SaveConfig succeeded")
	}
	if _, err := binding.GenerateText(TextRequestDTO{}); err == nil {
		t.Fatal("unattached GenerateText succeeded")
	}
	if _, err := binding.StreamText(TextRequestDTO{}); err == nil {
		t.Fatal("unattached StreamText succeeded")
	}
	if err := binding.CancelStream("x"); err == nil {
		t.Fatal("unattached CancelStream succeeded")
	}
	if _, err := binding.CheckHealth("p1"); err == nil {
		t.Fatal("unattached CheckHealth succeeded")
	}
}

// staticResolver returns a failing port to prove error mapping at the binding.
type failingResolver struct{}

func (failingResolver) TextPortFor(context.Context, string) (appproviders.TextPort, error) {
	return nil, provider.NewUnauthorizedError()
}

type staticRepository struct{}

func (staticRepository) SaveConfig(context.Context, provider.Config) error { return nil }
func (staticRepository) ListConfigs(context.Context) ([]provider.Config, error) {
	return []provider.Config{{
		ID:          "p1",
		Kind:        provider.KindOpenAICompatible,
		DisplayName: "Main",
		BaseURL:     "https://api.example.com",
		SecretRef:   provider.SecretRefValue("p1"),
		Revision:    3,
	}}, nil
}
func (staticRepository) GetConfig(context.Context, string) (provider.Config, error) {
	return provider.Config{ID: "p1", Kind: provider.KindOpenAICompatible, BaseURL: "https://api.example.com"}, nil
}
func (staticRepository) DeleteConfig(context.Context, string) error { return nil }
func (staticRepository) SaveRequestRecord(context.Context, provider.RequestRecord) error {
	return nil
}
func (staticRepository) ListHealth(context.Context) ([]provider.HealthState, error) { return nil, nil }

func TestProvidersBindingConfigDTOHasNoSecretValue(t *testing.T) {
	service := appproviders.NewService(staticRepository{}, failingResolver{}, nil)
	binding := &ProvidersBinding{}
	AttachProviders(binding, context.Background(), service, nil)
	configs, err := binding.ListConfigs()
	if err != nil {
		t.Fatalf("ListConfigs: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("configs = %+v", configs)
	}
	if configs[0].SecretRef != "InfiniteAtelier:provider:p1" {
		t.Fatalf("secret ref = %q", configs[0].SecretRef)
	}
	// The DTO type has no value field; assert the literal has no secret data.
	if strings.Contains(configs[0].BaseURL, "sk-") || configs[0].SecretRef == "" {
		t.Fatalf("unexpected DTO: %+v", configs[0])
	}
}

func TestProvidersBindingMapsProviderErrorSafely(t *testing.T) {
	service := appproviders.NewService(staticRepository{}, failingResolver{}, nil)
	requests := appproviders.NewRequestService(failingResolver{}, nil)
	binding := &ProvidersBinding{}
	AttachProviders(binding, context.Background(), service, requests)
	_, err := binding.GenerateText(TextRequestDTO{
		ProviderID: "p1",
		Model:      "m",
		Messages:   []appproviders.TextMessage{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	// The stable code lives in the structured error; Error() returns only the
	// safe user-facing message and must not contain internals.
	var appErr *apperror.Error
	if !errors.As(err, &appErr) {
		t.Fatalf("error is not an apperror: %T", err)
	}
	if appErr.Code != "PROVIDER_UNAUTHORIZED" {
		t.Fatalf("code = %q", appErr.Code)
	}
	if appErr.Category != "provider" || appErr.Retriable {
		t.Fatalf("unexpected classification: %+v", appErr)
	}
	if strings.Contains(appErr.Error(), "sk-") || appErr.Diagnostic == "" {
		t.Fatalf("unsafe or undiagnosable error: %+v", appErr)
	}
}

func TestProviderErrorCodeHelper(t *testing.T) {
	if got := upperCategory(provider.CategoryRateLimited); got != "RATE_LIMITED" {
		t.Fatalf("upperCategory = %q", got)
	}
}
