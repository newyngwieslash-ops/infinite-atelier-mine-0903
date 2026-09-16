package providers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

type memoryRepository struct {
	configs map[string]provider.Config
	records []provider.RequestRecord
	getErr  error
	saveErr error
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{configs: map[string]provider.Config{}}
}

func (r *memoryRepository) SaveConfig(_ context.Context, config provider.Config) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	r.configs[config.ID] = config
	return nil
}

func (r *memoryRepository) ListConfigs(_ context.Context) ([]provider.Config, error) {
	out := make([]provider.Config, 0, len(r.configs))
	for _, config := range r.configs {
		out = append(out, config)
	}
	return out, nil
}

func (r *memoryRepository) GetConfig(_ context.Context, id string) (provider.Config, error) {
	if r.getErr != nil {
		return provider.Config{}, r.getErr
	}
	config, ok := r.configs[id]
	if !ok {
		return provider.Config{}, provider.NewConfigurationError()
	}
	return config, nil
}

func (r *memoryRepository) DeleteConfig(_ context.Context, id string) error {
	delete(r.configs, id)
	return nil
}

func (r *memoryRepository) SaveRequestRecord(_ context.Context, record provider.RequestRecord) error {
	r.records = append(r.records, record)
	return nil
}

func (r *memoryRepository) ListHealth(_ context.Context) ([]provider.HealthState, error) {
	return nil, nil
}

type fakeTextPort struct {
	generateResult TextResult
	generateErr    error
	streamEvents   []TextEvent
	streamErr      error
	lastRequest    TextRequest
}

func (f *fakeTextPort) Generate(_ context.Context, request TextRequest) (TextResult, error) {
	f.lastRequest = request
	return f.generateResult, f.generateErr
}

func (f *fakeTextPort) Stream(_ context.Context, request TextRequest, sink EventSink) error {
	f.lastRequest = request
	for _, event := range f.streamEvents {
		switch {
		case event.ErrorCode != "":
			sink.OnError(errors.New(event.ErrorCode))
		case event.Done:
			sink.OnDone(TextResult{Content: event.Delta})
		default:
			sink.OnDelta(event.Delta)
		}
	}
	return f.streamErr
}

// fakeResolver resolves any provider ID to the fake port, optionally failing.
type fakeResolver struct {
	port TextPort
	err  error
}

func (f *fakeResolver) TextPortFor(_ context.Context, _ string) (TextPort, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.port, nil
}

type fakeHealthPort struct {
	state provider.HealthState
	err   error
}

func (f *fakeHealthPort) Check(_ context.Context, _ string) (provider.HealthState, error) {
	return f.state, f.err
}

func TestCreateConfigValidatesInput(t *testing.T) {
	service := NewService(newMemoryRepository(), nil, nil)
	ctx := context.Background()

	if _, err := service.CreateOrUpdateConfig(ctx, provider.ConfigInput{Kind: "gemini", DisplayName: "x", BaseURL: "https://api.example.com"}); err == nil {
		t.Fatal("unsupported kind accepted")
	}
	if _, err := service.CreateOrUpdateConfig(ctx, provider.ConfigInput{Kind: provider.KindOpenAICompatible, DisplayName: "x", BaseURL: "http://user:pw@h"}); err == nil {
		t.Fatal("URL with credentials accepted")
	}
	if _, err := service.CreateOrUpdateConfig(ctx, provider.ConfigInput{Kind: provider.KindOpenAICompatible, DisplayName: "", BaseURL: "https://api.example.com"}); err == nil {
		t.Fatal("empty display name accepted")
	}
	if _, err := service.CreateOrUpdateConfig(ctx, provider.ConfigInput{ID: "Bad ID", Kind: provider.KindOpenAICompatible, DisplayName: "x", BaseURL: "https://api.example.com"}); err == nil {
		t.Fatal("invalid ID accepted")
	}
}

func TestCreateConfigPersistsSecretRefNotValue(t *testing.T) {
	repository := newMemoryRepository()
	service := NewService(repository, nil, nil)
	config, err := service.CreateOrUpdateConfig(context.Background(), provider.ConfigInput{
		ID:          "prov-1",
		Kind:        provider.KindOpenAICompatible,
		DisplayName: "Main relay",
		BaseURL:     "https://relay.example.com/v1",
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("CreateOrUpdateConfig: %v", err)
	}
	if config.SecretRef != provider.SecretRefValue("prov-1") {
		t.Fatalf("secret ref = %q", config.SecretRef)
	}
	// Config struct must not carry any secret value field at all: compile-time
	// guarantee via the domain type; here we assert the ref is namespaced.
	if config.SecretRef != "InfiniteAtelier:provider:prov-1" {
		t.Fatalf("unexpected ref namespace: %q", config.SecretRef)
	}
	stored, ok := repository.configs["prov-1"]
	if !ok || stored.ID != "prov-1" || !stored.Enabled {
		t.Fatalf("config not persisted correctly: %+v", stored)
	}
}

func TestUpdateConfigIncrementsRevision(t *testing.T) {
	repository := newMemoryRepository()
	service := NewService(repository, nil, nil)
	ctx := context.Background()
	first, err := service.CreateOrUpdateConfig(ctx, provider.ConfigInput{ID: "prov-1", Kind: provider.KindOpenAICompatible, DisplayName: "v1", BaseURL: "https://a.example.com"})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	second, err := service.CreateOrUpdateConfig(ctx, provider.ConfigInput{ID: "prov-1", Kind: provider.KindOpenAICompatible, DisplayName: "v2", BaseURL: "https://b.example.com"})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if second.Revision != first.Revision+1 {
		t.Fatalf("revision = %d, want %d", second.Revision, first.Revision+1)
	}
	if second.DisplayName != "v2" || second.BaseURL != "https://b.example.com" {
		t.Fatalf("update not applied: %+v", second)
	}
}

func TestGenerateValidatesRequest(t *testing.T) {
	port := &fakeTextPort{}
	service := NewService(newMemoryRepository(), &fakeResolver{port: port}, nil)
	ctx := context.Background()

	if _, err := service.Generate(ctx, TextRequest{}); err == nil {
		t.Fatal("empty request accepted")
	}
	if _, err := service.Generate(ctx, TextRequest{ProviderID: "prov-1", Model: "m", Messages: []TextMessage{{Role: "root", Content: "x"}}}); err == nil {
		t.Fatal("invalid role accepted")
	}
	if _, err := service.Generate(ctx, TextRequest{ProviderID: "prov-1", Model: "m", Messages: []TextMessage{{Role: "user"}}}); err == nil {
		t.Fatal("empty content accepted")
	}
	// Valid request flows to the port.
	if _, err := service.Generate(ctx, TextRequest{ProviderID: "prov-1", Model: "gpt-x", Messages: []TextMessage{{Role: "user", Content: "hi"}}}); err != nil {
		t.Fatalf("valid generate: %v", err)
	}
	if port.lastRequest.Model != "gpt-x" {
		t.Fatalf("port did not receive request: %+v", port.lastRequest)
	}
}

func TestStreamDeliversEvents(t *testing.T) {
	port := &fakeTextPort{streamEvents: []TextEvent{{Delta: "he"}, {Delta: "llo"}, {Done: true}}}
	service := NewService(newMemoryRepository(), &fakeResolver{port: port}, nil)
	var deltas []string
	sink := &captureSink{onDelta: func(d string) { deltas = append(deltas, d) }}
	if err := service.Stream(context.Background(), TextRequest{ProviderID: "p", Model: "m", Messages: []TextMessage{{Role: "user", Content: "x"}}}, sink); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if len(deltas) != 2 || deltas[0] != "he" || deltas[1] != "llo" {
		t.Fatalf("deltas = %v", deltas)
	}
	if !sink.doneCalled {
		t.Fatal("sink done not called")
	}
}

func TestServiceFailsClosedWithoutPorts(t *testing.T) {
	service := NewService(newMemoryRepository(), nil, nil)
	ctx := context.Background()
	if _, err := service.Generate(ctx, TextRequest{ProviderID: "p", Model: "m", Messages: []TextMessage{{Role: "user", Content: "x"}}}); err == nil {
		t.Fatal("generate without port should fail")
	}
	if _, err := service.CheckHealth(ctx, "p"); err == nil {
		t.Fatal("health without port should fail")
	}
}

type captureSink struct {
	onDelta    func(string)
	doneCalled bool
	errValue   error
}

func (c *captureSink) OnDelta(delta string) { c.onDelta(delta) }
func (c *captureSink) OnDone(TextResult)    { c.doneCalled = true }
func (c *captureSink) OnError(err error)    { c.errValue = err }

// TestCreateConfigRejectsMockMediaKind proves the deterministic mock cannot be
// persisted as a provider. It is valid for the registry (tests opt in through
// code) but must not be registrable through the configuration path, otherwise a
// synthetic adapter could masquerade as a real provider.
func TestCreateConfigRejectsMockMediaKind(t *testing.T) {
	service := NewService(newMemoryRepository(), nil, nil)
	_, err := service.CreateOrUpdateConfig(context.Background(), provider.ConfigInput{
		ID:          "mock-1",
		Kind:        provider.KindMockMedia,
		DisplayName: "mock",
		BaseURL:     "https://mock.invalid",
	})
	if err == nil {
		t.Fatal("the mock media kind was accepted as a user configuration")
	}
	if providerErr, ok := provider.AsProviderError(err); !ok || providerErr.Category != provider.CategoryUnsupported {
		t.Fatalf("expected unsupported, got %v", err)
	}
	// The real kinds still work.
	for _, kind := range []provider.Kind{provider.KindOpenAICompatible, provider.KindGeminiCompatible} {
		if _, err := service.CreateOrUpdateConfig(context.Background(), provider.ConfigInput{
			ID:          strings.ReplaceAll(string(kind), "_", "-"),
			Kind:        kind,
			DisplayName: "real",
			BaseURL:     "https://api.example.com",
		}); err != nil {
			t.Fatalf("real kind %q rejected: %v", kind, err)
		}
	}
}
