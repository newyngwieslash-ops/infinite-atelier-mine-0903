// Package provider defines the provider configuration domain used by the
// application layer. It is pure business vocabulary: no Wails, SQLite, HTTP
// client, provider SDK, or OS API imports are allowed here.
package provider

import "time"

// Kind identifies the wire protocol family an adapter speaks.
type Kind string

const (
	// KindOpenAICompatible is an OpenAI-compatible HTTP API (chat completions,
	// images, audio and related endpoints).
	KindOpenAICompatible Kind = "openai_compatible"
	// KindGeminiCompatible is a Gemini-compatible HTTP API.
	KindGeminiCompatible Kind = "gemini_compatible"
	// KindMockMedia is the deterministic in-process media adapter used by tests
	// and by local pipeline exercises. It is not offered in the configuration
	// UI, so a real provider configuration cannot select it.
	KindMockMedia Kind = "mock_media"
)

// ValidKinds returns the provider kinds trusted by the current registry.
func ValidKinds() []Kind {
	return []Kind{KindOpenAICompatible, KindGeminiCompatible, KindMockMedia}
}

// IsValidKind reports whether the kind is accepted for persisted configs.
func IsValidKind(kind Kind) bool {
	for _, candidate := range ValidKinds() {
		if candidate == kind {
			return true
		}
	}
	return false
}

// Capability is a provider-callable capability. WP-02 implements text only.
type Capability string

const (
	// CapabilityText is synchronous or streaming text generation.
	CapabilityText Capability = "text"
	// CapabilityImage is image generation and editing.
	CapabilityImage Capability = "image"
	// CapabilityVideo is asynchronous video generation.
	CapabilityVideo Capability = "video"
	// CapabilityAudio is speech synthesis.
	CapabilityAudio Capability = "audio"
)

// IsValidCapability reports whether the capability is registered in Go.
func IsValidCapability(capability Capability) bool {
	switch capability {
	case CapabilityText, CapabilityImage, CapabilityVideo, CapabilityAudio:
		return true
	default:
		return false
	}
}

// ValidKindlessID enforces the ref-safe ID shape used by provider configs:
// lowercase alphanumeric with hyphens, 1..64 characters. Shared with the
// secrets application layer so references cannot be forged.
func ValidKindlessID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return false
		}
	}
	return true
}

// SecretRefValue returns the stable, non-secret secret-store reference for a
// provider ID. The reference is safe to persist; only the OS-backed store can
// resolve it.
func SecretRefValue(providerID string) string {
	return "InfiniteAtelier:provider:" + providerID
}

// Config is a persisted, non-secret provider configuration. The secret value
// never appears here; SecretRef points at the OS-backed SecretStore entry.
type Config struct {
	ID            string
	Kind          Kind
	DisplayName   string
	BaseURL       string
	SecretRef     string
	LocalApproved bool
	Enabled       bool
	Revision      int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// ConfigInput is the validated, untrusted creation/update payload shape used
// by the application layer. It carries no revision or timestamps.
type ConfigInput struct {
	ID           string
	Kind         Kind
	DisplayName  string
	BaseURL      string
	LocalApprove bool
	Enabled      bool
}

// RequestRecord is the redacted audit entry persisted per provider call. It
// must never contain Authorization headers or secret values.
type RequestRecord struct {
	ID string
	// JobID links the call to the generation job that caused it. It is empty
	// for calls with no job (health probes, interactive text).
	JobID      string
	ProviderID string
	Capability Capability
	Model      string
	Status     string
	HTTPStatus int
	LatencyMS  int64
	RequestID  string
	ErrorCode  string
	// Unit and cost fields carry provider-reported values. WP-02 records what
	// the adapter can observe (or leaves them zero/empty) rather than
	// inventing a pricing table; budget policy belongs to a later package.
	InputUnits    int64
	OutputUnits   int64
	EstimatedCost string
	CreatedAt     time.Time
}

// HealthState summarizes the last known reachability of a provider.
type HealthState struct {
	ProviderID string
	Healthy    bool
	CheckedAt  time.Time
	Detail     string
}

// RequestStatus values used in RequestRecord.Status.
const (
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// IsUserConfigurableKind reports whether a kind may be registered through the
// application configuration path. The deterministic in-process mock kind is
// valid for the registry (tests and local pipeline exercises opt in through
// code) but must never be persistable as if it were a real provider.
func IsUserConfigurableKind(kind Kind) bool {
	switch kind {
	case KindOpenAICompatible, KindGeminiCompatible:
		return true
	default:
		return false
	}
}
