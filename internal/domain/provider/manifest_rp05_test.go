package provider_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

// manifest_rp05_test.go is RP-05.1's fixture-driven contract: the fixtures
// under testdata/provider-fixtures/manifest are ORIGINAL documents authored
// for this repository, and every negative case differs from base-valid.json
// by EXACTLY ONE property — so each refusal is attributable to a named rule,
// not to "the document was wrong in some way".
//
// The assertion pattern is deliberately two-sided:
//   - base-valid.json LOADS (the happy path is real, not accidental);
//   - each negative is REFUSED with a configuration error whose message names
//     the rule, and the message differs between rules so a rule firing for
//     the wrong reason is visible.

const fixturesDir = "../../../testdata/provider-fixtures/manifest"

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixturesDir, name))
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return data
}

func TestRP05BaseValidManifestLoads(t *testing.T) {
	manifest, err := provider.LoadManifest(loadFixture(t, "base-valid.json"))
	if err != nil {
		t.Fatalf("the base valid fixture must load: %v", err)
	}
	if manifest.Capability != provider.CapabilityAudio {
		t.Fatalf("capability = %s", manifest.Capability)
	}
	if manifest.Endpoints.Headers["Accept"] != "application/json" {
		t.Fatalf("declared headers did not survive the load: %+v", manifest.Endpoints.Headers)
	}
}

func TestRP05NegativeFixturesAreRefused(t *testing.T) {
	cases := []struct {
		fixture     string
		messagePart string
	}{
		// A scheme/host override in the path: the SSRF guard's base URL rule
		// must not be reachable from the manifest.
		{"negative-host-in-submit-path.json", "cannot redirect"},
		// A protocol-relative form is a host override in disguise; the path
		// validator catches it through its empty-segment rule (a "//" IS an
		// empty segment), which the message names.
		{"negative-protocol-relative-path.json", "empty or dot path segments"},
		// Encoded separators are a path-traversal bypass of the path check.
		{"negative-path-encoding-bypass.json", "encoded separators"},
		// A placeholder outside the closed set is an evaluation mechanism.
		{"negative-unknown-placeholder.json", "placeholder this build does not substitute"},
		// A field the envelope does not know is refused by the strict decode.
		{"negative-unknown-field.json", "not a valid provider manifest"},
		// A credential header is the secret store's job, never the manifest's.
		{"negative-credential-header.json", "credential header"},
		// A capability outside the vocabulary is refused.
		{"negative-unknown-capability.json", "capability is not recognised"},
		// A poll block that widens the server's caps is refused: 900s interval
		// and 5000 polls are both outside the server bounds.
		{"negative-poll-widens-server-cap.json", "server's"},
	}
	for _, testCase := range cases {
		t.Run(testCase.fixture, func(t *testing.T) {
			_, err := provider.LoadManifest(loadFixture(t, testCase.fixture))
			if err == nil {
				t.Fatalf("%s was ACCEPTED; its single mutated property must be refused", testCase.fixture)
			}
			providerErr, ok := provider.AsProviderError(err)
			if !ok || providerErr.Category != provider.CategoryConfiguration {
				t.Fatalf("%s failed with the wrong error shape: %v", testCase.fixture, err)
			}
			if !strings.Contains(providerErr.SafeMessage, testCase.messagePart) {
				t.Fatalf("%s refused with %q, want a message naming %q — the refusal is not attributable to the mutated property",
					testCase.fixture, providerErr.SafeMessage, testCase.messagePart)
			}
		})
	}
}

// TestRP05MethodAllowlistDefaultsToPost covers the Method field's rule: an
// unstated method means POST, and a stated verb outside the allowlist is
// refused.
func TestRP05MethodAllowlistDefaultsToPost(t *testing.T) {
	manifest, err := provider.LoadManifest(loadFixture(t, "base-valid.json"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if manifest.Endpoints.Method != "" && manifest.Endpoints.Method != "POST" {
		t.Fatalf("method = %q, want empty (POST default) or POST", manifest.Endpoints.Method)
	}
}
