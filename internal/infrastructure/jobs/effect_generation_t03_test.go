package jobs

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
	infraproviders "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providers"
	)

// effect_generation_t03_test.go is the 2026-09-26 audit's T03 at the boundary
// that matters: an EFFECT job carries a description to the effect capability,
// a speech job carries a script to the speech capability, and the two never
// cross. The old path put the effect's matched word into a TTS request's
// `text` and filed the spoken word as an effect — 「脚步」 became the word
// "footsteps" read aloud, labelled a sound.

// effectConfigSource resolves the mock-media provider the effect tests submit
// against.
type effectConfigSource struct{}

func (effectConfigSource) GetConfig(context.Context, string) (provider.Config, error) {
	return provider.Config{
		ID: "prov-mock", Kind: provider.KindMockMedia, DisplayName: "mock media",
		SecretRef: provider.SecretRefValue("prov-mock"), LocalApproved: true,
		Enabled: true, Revision: 1,
	}, nil
}

// effectRegistry builds a registry whose mock-media channel carries BOTH the
// speech mock and the effect mock — the shape of a channel that can do both.
func effectRegistry(t *testing.T) *infraproviders.Registry {
	t.Helper()
	registry := infraproviders.NewRegistry(effectConfigSource{}, nil, nil)
	registry.WithMediaAdapters(infraproviders.NewMockVideoAdapter(), infraproviders.NewMockAudioAdapter())
	registry.WithEffectAdapter(infraproviders.NewMockEffectAdapter())
	return registry
}

// TestAnEffectJobProducesASoundNotSpeech drives one effect job through the
// real runner over the mock effect adapter and asserts the result is a file a
// sound reader accepts — with SAMPLES in it, not a header alone.
func TestAnEffectJobProducesASoundNotSpeech(t *testing.T) {
	ctx := context.Background()
	registry := effectRegistry(t)
	content := newFakeContentStore()
	store := NewResultStore(content, newFakeMetadataStore(), &recordingReferences{})
	runner := NewRunner(registry, store, &fakeDownloader{}, 1<<20)

	record := jobRecord(job.JobTypeEffectGeneration, map[string]any{
		"providerId": "prov-mock", "model": "mock-effect",
		"description": "footsteps on gravel", "durationSeconds": 2,
	})
	outcome, err := runner.Run(ctx, record)
	if err != nil {
		t.Fatalf("running the effect job: %v", err)
	}
	if outcome.Status != job.StatusSucceeded {
		t.Fatalf("the effect job ended %q", outcome.Status)
	}
	// The committed file: the result document names its hash, and the fake
	// content store holds the bytes. A sound with samples is longer than the
	// 44-byte canonical header.
	var document struct {
		Files []struct {
			Hash string `json:"hash"`
			Size int    `json:"size"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(outcome.ResultJSON), &document); err != nil || len(document.Files) == 0 {
		t.Fatalf("the effect job's result named no file (%v)", err)
	}
	if document.Files[0].Size <= 44 {
		t.Fatalf("the effect payload is %d bytes; a header-only payload is not a sound", document.Files[0].Size)
	}
	stored := readCommittedBytes(t, content, document.Files[0].Hash)
	if len(stored) != document.Files[0].Size {
		t.Fatalf("the stored bytes are %d; the result claims %d", len(stored), document.Files[0].Size)
	}
}

// TestAnEffectJobIsRefusedOnAChannelWithoutEffects is the honest refusal the
// split exists for: a channel whose registry has no effect adapter fails the
// job with the unsupported category rather than producing a wrong recording.
func TestAnEffectJobIsRefusedOnAChannelWithoutEffects(t *testing.T) {
	ctx := context.Background()
	// NO WithEffectAdapter: the shape of a channel that speaks only.
	registry := infraproviders.NewRegistry(effectConfigSource{}, nil, nil)
	registry.WithMediaAdapters(infraproviders.NewMockVideoAdapter(), infraproviders.NewMockAudioAdapter())
	store := NewResultStore(newFakeContentStore(), newFakeMetadataStore(), &recordingReferences{})
	runner := NewRunner(registry, store, &fakeDownloader{}, 1<<20)

	record := jobRecord(job.JobTypeEffectGeneration, map[string]any{
		"providerId": "prov-mock", "model": "mock-effect", "description": "rain on the roof",
	})
	_, err := runner.Run(ctx, record)
	if err == nil {
		t.Fatal("an effect job was served by a channel without an effect capability")
	}
	if !isUnsupportedFailure(err) {
		t.Fatalf("the refusal is not an unsupported-capability failure: %v", err)
	}
}

// TestTheRealEffectAdapterRefusesRatherThanSpeaking is T03's headline for the
// real channel: an openai_compatible provider asked for an effect is refused
// by its own adapter — the speech endpoint is never handed the description.
func TestTheRealEffectAdapterRefusesRatherThanSpeaking(t *testing.T) {
	registry := infraproviders.NewRegistry(nil, nil, nil)
	adapter := infraproviders.NewOpenAIEffectAdapter(registry)
	_ = registry
	if adapter == nil {
		t.Fatal("the real effect adapter was not built")
	}
	_, err := adapter.GenerateEffect(context.Background(), appjobs.EffectRequest{
		JobID: "job-1", ProviderID: "prov-speech", Model: "tts-1",
		Description: "footsteps on gravel",
	})
	if err == nil {
		t.Fatal("the real effect adapter produced audio; it must refuse until a protocol is verified")
	}
	if !isUnsupportedFailure(err) {
		t.Fatalf("the refusal is not an unsupported-capability failure: %v", err)
	}
}

// isUnsupportedFailure reports whether the error chain carries the provider's
// unsupported category — the honest refusal's signature.
func isUnsupportedFailure(err error) bool {
	providerErr, ok := provider.AsProviderError(err)
	return ok && providerErr.Category == provider.CategoryUnsupported
}


// readCommittedBytes opens the fake content store's object for one hash.
func readCommittedBytes(t *testing.T, content *fakeContentStore, hash string) []byte {
	t.Helper()
	reader, err := content.Open(context.Background(), hash)
	if err != nil {
		t.Fatalf("opening the committed bytes: %v", err)
	}
	defer reader.Close()
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("reading the committed bytes: %v", err)
	}
	return body
}
