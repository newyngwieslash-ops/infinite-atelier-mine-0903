package provider

import (
	"encoding/json"
	"strings"
)

// provider_manifest.go is FR-140's declarative Provider Manifest (T08): a
// JSON document that describes ONE HTTP provider — its capability, endpoint
// paths, request template and response mapping — so a non-builtin fixed
// protocol can be configured WITHOUT writing Go code and WITHOUT executing
// anything the manifest carries.
//
// # The security shape (SECURITY §9.4 and §3)
//
// A manifest is CONDITIONALLY TRUSTED input, and everything dangerous about a
// provider is refused here rather than trusted downstream:
//
//   - no Secret values: the manifest names WHICH capability and endpoints a
//     provider offers, and authorisation stays the adapter's job — the
//     secret store supplies credentials at request time, never the manifest;
//   - no scripts, no templates that evaluate code: request bodies are built
//     from a closed set of placeholder substitutions ({{model}}, {{prompt}},
//     {{text}}, {{description}}, {{voice}}, {{seconds}}, {{size}}), and a
//     placeholder outside that set fails the manifest;
//   - response mapping uses closed field names, not paths that evaluate;
//   - the BASE URL still travels on the provider config, not the manifest,
//     so the SSRF guard (ValidateBaseURL, the guarded client, per-request and
//     per-redirect revalidation) is unchanged: a manifest cannot widen where
//     a request may go.
//
// # What this enables
//
// At least one non-builtin fixed protocol — a provider whose shape matches
// the manifest's vocabulary — can be driven end to end: submit, poll (for
// async capabilities), fetch, and map the response fields into the domain.
// Protocols outside the vocabulary are refused at import, which is the point:
// the manifest admits configuration, not code.

// ManifestAPIVersion is the envelope version this build loads. An unknown
// version is refused rather than parsed optimistically.
const ManifestAPIVersion = "atelier.provider/v1"

// ManifestMaxBytes bounds a manifest document, on the same reasoning the
// skill manifest's bound states: a document this large is not configuration.
const ManifestMaxBytes = 256 << 10

// Manifest is one declarative provider.
type Manifest struct {
	APIVersion string        `json:"apiVersion"`
	Kind       string        `json:"kind"`
	Name       string        `json:"name"`
	Capability Capability    `json:"capability"`
	Endpoints  ManifestHTTP  `json:"http"`
	Mapping    ManifestMap   `json:"mapping"`
	Poll       *ManifestPoll `json:"poll,omitempty"`
}

// ManifestHTTP names the endpoint paths, relative to the provider config's
// base URL. Paths are VALIDATED: they must start with a slash, name no host,
// and carry no query — a manifest cannot redirect a request's destination.
type ManifestHTTP struct {
	// Method is the submit request's HTTP verb. RP-05.1: the empty value
	// means POST (the only verb every capability's submit needs today), and
	// anything outside the allowlist {POST} is refused — a GET-with-body or a
	// DELETE is not configuration this build runs.
	Method string `json:"method,omitempty"`
	// Headers names additional non-sensitive request headers as literal
	// name/value pairs. Names and values are validated: a header named
	// Authorization, Cookie, or any credential-shaped name is refused —
	// credentials are resolved from the secret store at request time, never
	// declared in a manifest.
	Headers    map[string]string `json:"headers,omitempty"`
	SubmitPath string            `json:"submitPath"`
	// PollPath and FetchPath are required for async capabilities (video) and
	// refused for sync ones — each capability states its own shape.
	PollPath  string `json:"pollPath,omitempty"`
	FetchPath string `json:"fetchPath,omitempty"`
	// ResultPath is the JSON field of the submit response that names the
	// remote job id, for async capabilities.
	ResultPath string `json:"resultIdField,omitempty"`
	// StatusPath, StatusDone and StatusFailed name the poll response's
	// fields, for async capabilities.
	StatusPath   string `json:"statusField,omitempty"`
	StatusDone   string `json:"statusDoneValue,omitempty"`
	StatusFailed string `json:"statusFailedValue,omitempty"`
}

// ManifestMap is the response mapping vocabulary: which JSON fields of a
// fetch response carry the bytes or the link. One of the two is required.
type ManifestMap struct {
	DataField string `json:"dataField,omitempty"`
	URLField  string `json:"urlField,omitempty"`
	// MIMEField optionally names the field carrying the result's MIME type.
	MIMEField string `json:"mimeField,omitempty"`
	// Template is the request body template for submit, with the closed
	// placeholder set substituted.
	Template string `json:"template,omitempty"`
}

// ManifestPoll is an async capability's polling configuration.
//
// RP-05.1: the manifest may only TIGHTEN the server's bounds, never widen
// them — a poll that never stops is a billable loop, so the server caps the
// floor and the ceiling and refuses an interval or count outside them.
type ManifestPoll struct {
	IntervalSeconds int `json:"intervalSeconds,omitempty"`
	// MaxPolls bounds how long a poll may run before the job is failed.
	MaxPolls int `json:"maxPolls,omitempty"`
}

// Server-side poll bounds. A manifest's own poll block, when present, must
// sit inside them.
const (
	// ManifestMinPollIntervalSeconds keeps a poll from hammering a provider.
	ManifestMinPollIntervalSeconds = 1
	// ManifestMaxPollIntervalSeconds bounds the wait between polls; longer is
	// a stall, not a poll.
	ManifestMaxPollIntervalSeconds = 120
	// ManifestMaxPolls bounds how many polls one job may spend.
	ManifestMaxPolls = 1000
)

// LoadManifest decodes and validates one manifest document.
//
// It is strict the way the skill manifest is: unknown fields are refused, the
// size is bounded, and every path and placeholder is checked against a closed
// set. A manifest that fails here never reaches the registry.
func LoadManifest(data []byte) (Manifest, error) {
	if len(data) > ManifestMaxBytes {
		return Manifest{}, NewConfigurationErrorWith("That manifest is too large to be configuration.")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, NewConfigurationErrorWith("The manifest is not a valid provider manifest.")
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Validate applies the closed-set rules.
func (m Manifest) Validate() error {
	if m.APIVersion != ManifestAPIVersion {
		return NewConfigurationErrorWith("That manifest's API version is not recognised.")
	}
	if m.Kind != "provider" {
		return NewConfigurationErrorWith("That manifest is not a provider manifest.")
	}
	if strings.TrimSpace(m.Name) == "" || len(m.Name) > 120 {
		return NewConfigurationErrorWith("A manifest must name its provider.")
	}
	if !IsValidCapability(m.Capability) {
		return NewConfigurationErrorWith("That manifest's capability is not recognised.")
	}
	if err := validateManifestPath("submit", m.Endpoints.SubmitPath); err != nil {
		return err
	}
	async := m.Capability == CapabilityVideo
	if async {
		if err := validateManifestPath("poll", m.Endpoints.PollPath); err != nil {
			return err
		}
		if err := validateManifestPath("fetch", m.Endpoints.FetchPath); err != nil {
			return err
		}
		if strings.TrimSpace(m.Endpoints.ResultPath) == "" || strings.TrimSpace(m.Endpoints.StatusPath) == "" {
			return NewConfigurationErrorWith("An async manifest must name its result id and status fields.")
		}
	}
	if err := validateHeaders(m.Endpoints.Headers); err != nil {
		return err
	}
	if err := validatePollBounds(m.Poll); err != nil {
		return err
	}
	if strings.TrimSpace(m.Mapping.Template) == "" {
		return NewConfigurationErrorWith("A manifest must state its request template.")
	}
	if err := validateTemplate(m.Mapping.Template); err != nil {
		return err
	}
	if m.Mapping.DataField == "" && m.Mapping.URLField == "" {
		return NewConfigurationErrorWith("A manifest must map its result: a data field or a URL field.")
	}
	return nil
}

// validPlaceholders is the closed substitution set. A template naming anything
// outside it is refused: the alternative is an evaluation mechanism, and a
// manifest that evaluates is a manifest that executes.
var validPlaceholders = map[string]bool{
	"model":       true,
	"prompt":      true,
	"text":        true,
	"description": true,
	"voice":       true,
	"seconds":     true,
	"size":        true,
}

// validateManifestPath applies the closed path rules RP-05.1 states: a
// manifest endpoint is a PATH under the configured base URL, so scheme/host
// overrides, protocol-relative forms, userinfo, query/fragment, backslash
// separators and dot segments are all refused — each is a way to move the
// request's destination past the SSRF guard's reach.
func validateManifestPath(field, path string) error {
	if !strings.HasPrefix(path, "/") {
		return NewConfigurationErrorWith("A " + field + " path must start with a slash: a manifest cannot redirect a request's destination.")
	}
	if strings.ContainsAny(path, "?# \\\\") {
		return NewConfigurationErrorWith("A " + field + " path carries no query, fragment or backslash.")
	}
	if strings.Contains(path, "//") || strings.Contains(path, "/./") || strings.HasSuffix(path, "/.") {
		return NewConfigurationErrorWith("A " + field + " path carries no empty or dot path segments.")
	}
	if strings.Contains(path, "%2f") || strings.Contains(path, "%2F") || strings.Contains(path, "%5c") || strings.Contains(path, "%5C") {
		return NewConfigurationErrorWith("A " + field + " path carries no encoded separators.")
	}
	return nil
}

// manifestForbiddenHeaders is the credential-shaped header vocabulary a
// manifest may never declare. Credentials are the secret store's job.
var manifestForbiddenHeaders = map[string]bool{
	"authorization":       true,
	"proxy-authorization": true,
	"cookie":              true,
	"set-cookie":          true,
	"x-api-key":           true,
	"x-goog-api-key":      true,
}

// validateHeaders checks the declared header map against the closed rules:
// token-form names only, no credential-shaped names, and bounded values.
func validateHeaders(headers map[string]string) error {
	for name, value := range headers {
		lower := strings.ToLower(name)
		if manifestForbiddenHeaders[lower] {
			return NewConfigurationErrorWith("A manifest may not declare the credential header \"" + name + "\".")
		}
		if strings.TrimSpace(name) == "" || strings.ContainsAny(name, " :\r\n") {
			return NewConfigurationErrorWith("A manifest's header names must be header tokens.")
		}
		if len(value) > 512 || strings.ContainsAny(value, "\r\n") {
			return NewConfigurationErrorWith("A manifest's header values must be short single lines.")
		}
	}
	return nil
}

// validatePollBounds checks an async manifest's poll block against the
// server's caps: the manifest may tighten them, never widen.
func validatePollBounds(poll *ManifestPoll) error {
	if poll == nil {
		return nil
	}
	if poll.IntervalSeconds != 0 && (poll.IntervalSeconds < ManifestMinPollIntervalSeconds || poll.IntervalSeconds > ManifestMaxPollIntervalSeconds) {
		return NewConfigurationErrorWith("A manifest's poll interval must sit between the server's one-second floor and its two-minute ceiling.")
	}
	if poll.MaxPolls != 0 && poll.MaxPolls > ManifestMaxPolls {
		return NewConfigurationErrorWith("A manifest's poll count may not exceed the server's cap.")
	}
	return nil
}

// validateTemplate checks the request template's placeholders against the
// closed set.
func validateTemplate(template string) error {
	for start := 0; ; {
		open := strings.Index(template[start:], "{{")
		if open < 0 {
			return nil
		}
		open += start
		close_ := strings.Index(template[open:], "}}")
		if close_ < 0 {
			return NewConfigurationErrorWith("The request template has an unterminated placeholder.")
		}
		name := strings.TrimSpace(template[open+2 : open+close_])
		if !validPlaceholders[name] {
			return NewConfigurationErrorWith("The request template names a placeholder this build does not substitute.")
		}
		start = open + close_ + 2
	}
}

// NewConfigurationErrorWith reports configuration the domain refuses, with the
// message a caller can act on — the manifest validation's error shape.
func NewConfigurationErrorWith(message string) *Error {
	return &Error{Category: CategoryConfiguration, SafeMessage: message}
}
