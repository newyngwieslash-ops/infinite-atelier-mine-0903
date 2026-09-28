package provider_test

import (
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// manifest_t08_test.go is FR-140's acceptance at the domain boundary: a
// declarative manifest for ONE non-builtin fixed protocol loads, and every
// dangerous shape the SECURITY rules refuse is refused here — before the
// registry ever sees it.

const validManifest = `{
  "apiVersion": "atelier.provider/v1",
  "kind": "provider",
  "name": "acme-media",
  "capability": "audio",
  "http": {
    "submitPath": "/v1/say"
  },
  "mapping": {
    "dataField": "audio_base64",
    "mimeField": "mime",
    "template": "{\"model\":\"{{model}}\",\"text\":\"{{text}}\",\"voice\":\"{{voice}}\"}"
  }
}`

const validAsyncManifest = `{
  "apiVersion": "atelier.provider/v1",
  "kind": "provider",
  "name": "acme-video",
  "capability": "video",
  "http": {
    "submitPath": "/api/renders",
    "pollPath": "/api/renders/{{id}}",
    "fetchPath": "/api/renders/{{id}}/file",
    "resultIdField": "render_id",
    "statusField": "state",
    "statusDoneValue": "done",
    "statusFailedValue": "failed"
  },
  "mapping": {
    "urlField": "file_url",
    "template": "{\"model\":\"{{model}}\",\"prompt\":\"{{prompt}}\",\"seconds\":{{seconds}}}"
  },
  "poll": { "intervalSeconds": 5, "maxPolls": 60 }
}`

// TestAValidManifestLoads is the positive half: one non-builtin fixed
// protocol, described declaratively, passes validation.
func TestAValidManifestLoads(t *testing.T) {
	manifest, err := provider.LoadManifest([]byte(validManifest))
	if err != nil {
		t.Fatalf("a valid manifest was refused: %v", err)
	}
	if manifest.Name != "acme-media" || manifest.Capability != provider.CapabilityAudio {
		t.Fatalf("the manifest decoded as %+v", manifest)
	}
}

// TestAnAsyncManifestLoads proves the poll vocabulary carries an async
// protocol: submit, poll with status fields, fetch — no Go code per provider.
func TestAnAsyncManifestLoads(t *testing.T) {
	if _, err := provider.LoadManifest([]byte(validAsyncManifest)); err != nil {
		t.Fatalf("a valid async manifest was refused: %v", err)
	}
}

// TestAManifestCarryingAHostInItsPathIsRefused is the redirect-by-config
// refusal: a path naming a destination of its own would widen where a request
// may go, and the SSRF guard's answer is that the manifest cannot.
func TestAManifestCarryingAHostInItsPathIsRefused(t *testing.T) {
	hostile := `{
		"apiVersion": "atelier.provider/v1", "kind": "provider", "name": "evil",
		"capability": "audio",
		"http": { "submitPath": "https://169.254.169.254/latest", "template": "{\"t\":\"{{text}}\"}" },
		"mapping": { "dataField": "a" }
	}`
	if _, err := provider.LoadManifest([]byte(hostile)); err == nil {
		t.Fatal("a manifest whose path names a host was accepted")
	}
}

// TestAManifestWithAnUnknownPlaceholderIsRefused is the no-evaluation rule: a
// template naming a placeholder outside the closed set would need a mechanism
// that interprets input, which is a script by another name.
func TestAManifestWithAnUnknownPlaceholderIsRefused(t *testing.T) {
	unknown := `{
		"apiVersion": "atelier.provider/v1", "kind": "provider", "name": "tricky",
		"capability": "audio",
		"http": { "submitPath": "/v1/say", "template": "{\"t\":\"{{system_prompt}}\"}" },
		"mapping": { "dataField": "a" }
	}`
	if _, err := provider.LoadManifest([]byte(unknown)); err == nil {
		t.Fatal("a template with an unknown placeholder was accepted")
	}
}

// TestAManifestWithAnUnknownFieldIsRefused is the strictness rule: unknown
// fields are refused the way the skill manifest's decoder refuses them, so a
// document this build does not understand cannot smuggle semantics past it.
func TestAManifestWithAnUnknownFieldIsRefused(t *testing.T) {
	unknown := `{
		"apiVersion": "atelier.provider/v1", "kind": "provider", "name": "extra",
		"capability": "audio", "executeScript": "alert(1)",
		"http": { "submitPath": "/v1/say", "template": "{\"t\":\"{{text}}\"}" },
		"mapping": { "dataField": "a" }
	}`
	if _, err := provider.LoadManifest([]byte(unknown)); err == nil {
		t.Fatal("a manifest with an unknown field was accepted")
	}
}

// TestAManifestForAnUnknownCapabilityIsRefused keeps the capability list
// closed: a manifest advertising a capability no adapter serves is a promise
// the build cannot keep.
func TestAManifestForAnUnknownCapabilityIsRefused(t *testing.T) {
	unknown := `{
		"apiVersion": "atelier.provider/v1", "kind": "provider", "name": "odd",
		"capability": "telepathy",
		"http": { "submitPath": "/v1/say", "template": "{\"t\":\"{{text}}\"}" },
		"mapping": { "dataField": "a" }
	}`
	if _, err := provider.LoadManifest([]byte(unknown)); err == nil {
		t.Fatal("a manifest for an unknown capability was accepted")
	}
}

// TestAManifestWithoutAResultMappingIsRefused: a manifest whose fetch maps to
// nothing produces bytes nobody can read, so it is refused at import.
func TestAManifestWithoutAResultMappingIsRefused(t *testing.T) {
	unmapped := `{
		"apiVersion": "atelier.provider/v1", "kind": "provider", "name": "blind",
		"capability": "audio",
		"http": { "submitPath": "/v1/say", "template": "{\"t\":\"{{text}}\"}" },
		"mapping": {}
	}`
	if _, err := provider.LoadManifest([]byte(unmapped)); err == nil {
		t.Fatal("a manifest with no result mapping was accepted")
	}
}
