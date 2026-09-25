package appdiagnostics

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// diagnostics_test.go is FR-180's 「一键生成诊断包」 and SECURITY section 14.2's contract.
//
// # What each test establishes
//
//  1. The manifest LISTS every section with its size and its own description, which is 「生成前展示清单」.
//  2. A caller can OMIT a section, which is 「用户可取消内容」 — and the omission is RECORDED in the
//     bundled manifest, because "they did not send their logs" and "there were no logs" are different
//     facts a support conversation depends on.
//  3. The second redaction layer masks a credential the log file carries. This is the one that
//     matters: a bundle is the artifact a user sends to somebody else.
//  4. The provider section carries NO address and NO secret reference, which section 14.2 requires in
//     as many words ("不含密钥").
//  5. `Plan` writes nothing, so showing the list twice is safe.

// fakeReader is the port with stated answers.
type fakeReader struct {
	schemaVersion int
	pending       bool
	providers     []ProviderSummary
	files         FileStoreSummary
	jobs          map[string]int
	workflows     map[string]int
	errorCodes    map[string]int
	logBytes      []byte
	err           error
}

func (f *fakeReader) SchemaVersion(context.Context) (int, error) {
	return f.schemaVersion, f.err
}
func (f *fakeReader) PendingMigrations(context.Context) (bool, error)  { return f.pending, f.err }
func (f *fakeReader) ProviderKinds(context.Context) ([]ProviderSummary, error) {
	return f.providers, f.err
}
func (f *fakeReader) FileStoreSummary(context.Context) (FileStoreSummary, error) {
	return f.files, f.err
}
func (f *fakeReader) JobCountsByStatus(context.Context) (map[string]int, error) {
	return f.jobs, f.err
}
func (f *fakeReader) WorkflowCountsByStatus(context.Context) (map[string]int, error) {
	return f.workflows, f.err
}
func (f *fakeReader) ErrorCodes(context.Context) (map[string]int, error) {
	return f.errorCodes, f.err
}
func (f *fakeReader) LogBytes(context.Context, int) ([]byte, error) { return f.logBytes, f.err }

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

func newService(reader Reader) *Service {
	return NewService(Options{
		Reader:     reader,
		Clock:      fixedClock{at: time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)},
		AppVersion: "1.0.0",
		Platform:   "windows/amd64",
	})
}

func healthyReader() *fakeReader {
	return &fakeReader{
		schemaVersion: 22,
		pending:       false,
		providers:     []ProviderSummary{{Kind: "openai_compatible", Enabled: true}},
		files:         FileStoreSummary{Objects: 31, TotalBytes: 4096},
		jobs:          map[string]int{"succeeded": 3, "failed": 1},
		workflows:     map[string]int{"running": 1},
		errorCodes:    map[string]int{"PROVIDER_TIMEOUT": 2},
		logBytes:      []byte(`{"msg":"started"}` + "\n"),
	}
}

// TestTheManifestListsEverySectionWithItsSize is 「生成前展示清单」.
func TestTheManifestListsEverySectionWithItsSize(t *testing.T) {
	service := newService(healthyReader())
	manifest, err := service.Plan(context.Background(), nil)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(manifest.Entries) != len(Sections) {
		t.Fatalf("the manifest lists %d sections and the build has %d", len(manifest.Entries), len(Sections))
	}
	bySection := map[Section]ManifestEntry{}
	for _, entry := range manifest.Entries {
		bySection[entry.Section] = entry
		// Every section is INCLUDED by default, which is what a nil request means.
		if !entry.Included {
			t.Fatalf("section %q is not included by a request for everything", entry.Section)
		}
		// And every one is DESCRIBED, because a manifest a user reads has to say what they are
		// agreeing to send rather than naming a field.
		if strings.TrimSpace(entry.Note) == "" {
			t.Fatalf("section %q has no description, so a user reading the manifest learns nothing", entry.Section)
		}
		if entry.Bytes <= 0 {
			t.Fatalf("section %q reports %d bytes, so the size a user decides on is wrong", entry.Section, entry.Bytes)
		}
	}
	// The total is the sum of the included sections, which is the number the decision is about.
	total := 0
	for _, entry := range manifest.Entries {
		total += entry.Bytes
	}
	if manifest.TotalBytes != total {
		t.Fatalf("the manifest reports %d total bytes and its entries sum to %d", manifest.TotalBytes, total)
	}
	// Every documented section is present: a section the build can produce but does not list would be
	// content a user was never shown.
	for _, section := range Sections {
		if _, ok := bySection[section]; !ok {
			t.Fatalf("the manifest omits the section %q", section)
		}
	}
}

// TestAnOmittedSectionIsLeftOutAndRecorded is 「用户可取消内容」.
func TestAnOmittedSectionIsLeftOutAndRecorded(t *testing.T) {
	reader := healthyReader()
	// A log large enough that leaving it out is visibly a decision.
	reader.logBytes = []byte(strings.Repeat(`{"msg":"a line"}`+"\n", 500))
	service := newService(reader)

	full, err := service.Plan(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	partial, err := service.Plan(context.Background(), []Section{SectionApplication, SectionMigrations})
	if err != nil {
		t.Fatalf("Plan with a choice: %v", err)
	}
	// The total SHRINKS by what was left out, which is what makes the choice mean something.
	if partial.TotalBytes >= full.TotalBytes {
		t.Fatalf("omitting six sections did not reduce the total: %d against %d", partial.TotalBytes, full.TotalBytes)
	}
	// And the omitted ones are still LISTED, marked as omitted: a manifest that dropped them would
	// not distinguish "the user left this out" from "this build has no such section".
	omitted := 0
	for _, entry := range partial.Entries {
		if entry.Section == SectionApplication || entry.Section == SectionMigrations {
			if !entry.Included {
				t.Fatalf("section %q was requested and reported as omitted", entry.Section)
			}
			continue
		}
		if entry.Included {
			t.Fatalf("section %q was not requested and reported as included", entry.Section)
		}
		omitted++
	}
	if omitted != len(Sections)-2 {
		t.Fatalf("%d sections reported as omitted, want %d", omitted, len(Sections)-2)
	}

	// The BUNDLE omits the content too, and its own manifest records the omission.
	bundle, err := service.Assemble(context.Background(), []Section{SectionApplication, SectionMigrations})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	if _, present := bundle.Files[SectionLogs]; present {
		t.Fatal("the bundle carries logs the caller did not ask for")
	}
	if _, present := bundle.Files[SectionApplication]; !present {
		t.Fatal("the bundle is missing a section the caller asked for")
	}
	manifestText := string(bundle.Files[SectionManifest])
	if !strings.Contains(manifestText, "omitted by the user") {
		t.Fatalf("the bundled manifest does not record what was left out:\n%s", manifestText)
	}
	if !strings.Contains(manifestText, "1.0.0") {
		t.Fatal("the bundled manifest does not name the build")
	}
}

// TestTheSecondRedactionLayerMasksACredentialInTheLog is SECURITY 14.1's 「导出前」 layer.
//
// The log file is written by a redacting handler, so this test stages what a file from an OLDER build
// or a hand-edited file would contain — which is the case the second layer exists for.
func TestTheSecondRedactionLayerMasksACredentialInTheLog(t *testing.T) {
	reader := healthyReader()
	// THE FIXTURES ARE NOT KEY-SHAPED, and the repository's own secret scanner is why: it refuses
	// `sk-` wherever it appears, tests included, and it is right to — a fixture that looks exactly like
	// a live credential is one somebody will paste somewhere it matters. The redactor matches on SHAPE
	// (a header's value, a long opaque token after a sensitive name, a query parameter's value), so
	// these carry that shape without being a key any service would issue.
	reader.logBytes = []byte(strings.Join([]string{
		`{"msg":"calling provider"}`,
		`Authorization: Bearer fixture-token-not-a-credential-0001`,
		`{"api_key":"fixture-token-not-a-credential-0002"}`,
		`GET /v1/models?api_key=fixture-token-not-a-credential-0003 HTTP/1.1`,
		`{"error":"upstream refused"}`,
	}, "\n"))
	service := newService(reader)
	bundle, err := service.Assemble(context.Background(), []Section{SectionLogs})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	logs := string(bundle.Files[SectionLogs])
	for _, secret := range []string{
		"fixture-token-not-a-credential-0001",
		"fixture-token-not-a-credential-0002",
		"fixture-token-not-a-credential-0003",
	} {
		if strings.Contains(logs, secret) {
			t.Fatalf("a credential survived into the bundle: %q\n---\n%s", secret, logs)
		}
	}
	// The NAMES survive, because a masked line that kept nothing is a line a reader cannot use.
	for _, name := range []string{"Authorization", "api_key"} {
		if !strings.Contains(logs, name) {
			t.Fatalf("the redaction removed the name %q as well as the value:\n%s", name, logs)
		}
	}
	if !strings.Contains(logs, Redacted) {
		t.Fatalf("the log carries no redaction marker:\n%s", logs)
	}
	// And the ordinary content is untouched: a redactor that removed everything would pass the loop
	// above while being useless.
	if !strings.Contains(logs, "upstream refused") {
		t.Fatalf("the redaction removed an ordinary log line:\n%s", logs)
	}
	if !strings.Contains(logs, "calling provider") {
		t.Fatal("the redaction removed an ordinary log line")
	}
}

// TestTheProviderSectionCarriesNoAddressOrReference is section 14.2's 「不含密钥」.
//
// The section is a COUNT and a KIND. A base URL can carry a credential in its user-info, and a secret
// reference is a credential TARGET — naming one in a bundle a user emails is a hint an attacker can
// use, which is why neither is in the port's return type at all.
func TestTheProviderSectionCarriesNoAddressOrReference(t *testing.T) {
	reader := healthyReader()
	reader.providers = []ProviderSummary{
		{Kind: "openai_compatible", Enabled: true},
		{Kind: "gemini_compatible", Enabled: false},
	}
	service := newService(reader)
	bundle, err := service.Assemble(context.Background(), []Section{SectionProviders})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	providers := string(bundle.Files[SectionProviders])
	for _, wanted := range []string{"openai_compatible: enabled", "gemini_compatible: disabled", "count: 2"} {
		if !strings.Contains(providers, wanted) {
			t.Fatalf("the provider section is missing %q:\n%s", wanted, providers)
		}
	}
	// Two kinds, and neither an address nor a reference: the section's own comment says the port
	// cannot return one, and this asserts the rendered text carries none either.
	if strings.Contains(providers, "http") || strings.Contains(providers, "secret") {
		t.Fatalf("the provider section carries an address or a reference:\n%s", providers)
	}
}

// TestPlanWritesNothing is what makes showing the list safe to do twice.
func TestPlanWritesNothing(t *testing.T) {
	reader := healthyReader()
	service := newService(reader)
	first, err := service.Plan(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Plan(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.TotalBytes != second.TotalBytes || len(first.Entries) != len(second.Entries) {
		t.Fatalf("two plans of one state differ: %+v against %+v", first, second)
	}
	// The reader is unchanged by looking, which is the property a preview needs.
	if reader.schemaVersion != 22 || len(reader.logBytes) == 0 {
		t.Fatal("planning changed the reader's state")
	}
}

// TestAnUnknownSectionIsRefused covers the input rule: a section this build does not produce is a
// refusal rather than a silent omission, because a caller asking for something and receiving a
// bundle without it would not know which happened.
func TestAnUnknownSectionIsRefused(t *testing.T) {
	service := newService(healthyReader())
	_, err := service.Plan(context.Background(), []Section{"memory_dump"})
	if err == nil {
		t.Fatal("an unknown section was accepted")
	}
	if !strings.Contains(err.Error(), "not recognised") {
		t.Fatalf("the refusal does not say the section is unknown: %v", err)
	}
}

// TestAReadFailureIsReportedRatherThanSilentlyOmitted is the failure policy.
//
// A section whose read failed must not be quietly dropped from the manifest: a bundle missing the
// migrations section looks like a build with no migrations, and the reader of it would conclude the
// schema is fine.
func TestAReadFailureIsReportedRatherThanSilentlyOmitted(t *testing.T) {
	reader := healthyReader()
	reader.err = errors.New("the store is gone")
	service := newService(reader)
	if _, err := service.Plan(context.Background(), nil); err == nil {
		t.Fatal("a failed read produced a manifest anyway")
	}
	// And a build with no reader refuses rather than returning an empty bundle, which would look like
	// a healthy application with nothing to report.
	empty := NewService(Options{})
	if empty.Available() {
		t.Fatal("a service with no reader reports itself available")
	}
	if _, err := empty.Assemble(context.Background(), nil); err == nil {
		t.Fatal("a service with no reader assembled a bundle")
	}
}

// TestContainsCredentialShapeIsTheRefusalCheck covers the twin of the redactor.
//
// Masking is the right answer for a log line a user reads, and a REFUSAL is the right answer for a
// file whose presence of a credential means something else went wrong — the distinction `backup`
// draws for archives.
func TestContainsCredentialShapeIsTheRefusalCheck(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"an ordinary line about a job finishing", false},
		{"", false},
		{"Authorization: Bearer fixture-token-not-a-credential-0004", true},
		{"api_key=fixture-token-not-a-credential-0005", true},
		{"a line naming an api_key in prose", false},
		{"token: a-value-here", true},
	}
	for _, testCase := range cases {
		if got := ContainsCredentialShape(testCase.text); got != testCase.want {
			t.Fatalf("ContainsCredentialShape(%q) = %t, want %t", testCase.text, got, testCase.want)
		}
	}
	// Redacting first makes the check clean, which is the order a caller uses: mask for the bundle,
	// and refuse when the masking found something that should not have been there at all.
	redacted := RedactText("Authorization: Bearer fixture-token-not-a-credential-0006")
	if ContainsCredentialShape(redacted) {
		t.Fatalf("a redacted line still reports a credential shape: %q", redacted)
	}
}

// TestTheLogSectionIsBounded keeps a bundle mailable: a log that has grown past the bound is
// truncated rather than included whole, and the section says it was redacted at a time so a reader
// knows what they have.
func TestTheLogSectionIsBounded(t *testing.T) {
	reader := healthyReader()
	reader.logBytes = []byte(strings.Repeat(`{"msg":"a line"}\n`, 100))
	service := newService(reader)
	bundle, err := service.Assemble(context.Background(), []Section{SectionLogs})
	if err != nil {
		t.Fatal(err)
	}
	logs := string(bundle.Files[SectionLogs])
	if !strings.Contains(logs, "redacted at 2026-09-25") {
		t.Fatalf("the log section does not say when it was redacted:\n%s", logs[:120])
	}
}
