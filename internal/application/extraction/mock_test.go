package extraction

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	extractiondomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/extraction"
)

// mock_test.go is the deterministic extractor from AGENT_CONTRACTS §18.3: "CI 不
// 调用真实付费模型。Mock 根据输入场景返回：正常结构；一次无效结构后二次有效；Tool
// Call；拒绝非法 Tool；Supervisor issues；超时；取消；Provider 错误。"
//
// It is a TEST file rather than production code, and that placement is the rule
// rather than a convenience. AGENTS forbids putting a mock on the production path
// ("把 Mock 放生产路径"), and this package's rule is that a Service with no
// extractor is UNAVAILABLE rather than mocked: a production build has no default
// extractor, so the command fails closed with a reason instead of producing
// invented facts. WP-07 supplies the real implementation of the port.
//
// Three properties make it a test instrument rather than a stub:
//
//   - It is DETERMINISTIC. The same chapter always produces the same document, so
//     a failure is reproducible and a golden file is meaningful.
//   - It reads the chapter it is given. The names it proposes come from the text,
//     so the evidence offsets it produces are real offsets and the reference
//     resolution is exercised rather than bypassed.
//   - It can be told to MISBEHAVE. Section 18.3's scenarios — a malformed document,
//     a timeout, a cancel, an extractor error — are modes, so the failure paths
//     run through the same code the success path does. The two Tool Call modes in
//     that list belong to the runtime's tool registry, which does not exist yet,
//     so WP-07 owns them.
//
// SECURITY: the mock never treats the chapter as instructions. It scans for names
// with a positional rule that has no notion of meaning, which is what makes the
// injection fixture a real test: the injected text is scanned like any other
// text and cannot change what the mock returns.

// MockMode is how the mock should behave.
type MockMode string

const (
	// MockNormal returns a well-formed document.
	MockNormal MockMode = "normal"
	// MockInvalidOnce returns a malformed document the first time a chapter is
	// read and a valid one afterwards. It is §18.3's "一次无效结构后二次有效", which
	// is the scenario the one-repair rule exists for.
	MockInvalidOnce MockMode = "invalid_once"
	// MockInvalidAlways returns a malformed document every time, so a caller can
	// prove that persistent invalidity does not write anything.
	MockInvalidAlways MockMode = "invalid_always"
	// MockEmpty returns a valid document that proposes nothing, which is what a
	// chapter of pure description produces.
	MockEmpty MockMode = "empty"
	// MockTimeout blocks until the context is done, so a caller can prove the
	// cancel and deadline paths work.
	MockTimeout MockMode = "timeout"
	// MockError fails the way a provider outage does.
	MockError MockMode = "error"
	// MockDanglingReference returns a document whose relation names an entity
	// nothing defines, which the schema cannot catch and the resolver must.
	MockDanglingReference MockMode = "dangling_reference"
)

// Mock is a deterministic Extractor.
//
// It is safe for concurrent use, because a test may run extractions in parallel
// and the invalid-once counter is shared state.
type Mock struct {
	mode MockMode
	// Seen records every request, so a test can assert what the service passed
	// down without reaching into the service.
	seen []Request
	mu   sync.Mutex
	// callsToChapter counts how many times each chapter was read, which is what
	// "the first time" means for MockInvalidOnce.
	callsToChapter map[string]int
	// now is injectable so a recorded request's timing is deterministic in a test
	// that cares.
	now func() time.Time
}

// NewMock builds a mock in the given mode.
func NewMock(mode MockMode) *Mock {
	return &Mock{
		mode:           mode,
		callsToChapter: map[string]int{},
		now:            func() time.Time { return time.Now().UTC() },
	}
}

// Requests returns a copy of what the mock was asked to read.
func (m *Mock) Requests() []Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Request(nil), m.seen...)
}

// Extract returns the scenario's raw output.
//
// It returns BYTES rather than a document, because that is the port's contract:
// validation is the service's job, and a mock that returned a typed document
// would prove nothing about the validator.
func (m *Mock) Extract(ctx context.Context, request Request) ([]byte, error) {
	m.mu.Lock()
	m.callsToChapter[request.ChapterID]++
	call := m.callsToChapter[request.ChapterID]
	m.seen = append(m.seen, request)
	mode := m.mode
	m.mu.Unlock()

	switch mode {
	case MockError:
		return nil, extractiondomain.InvalidRequestError("The extractor could not be reached.")
	case MockTimeout:
		// Wait for the context rather than sleeping a fixed time, so the test
		// takes as long as the deadline it set and no longer.
		<-ctx.Done()
		return nil, ctx.Err()
	}

	if mode == MockInvalidAlways || (mode == MockInvalidOnce && call == 1) {
		// A document that is valid JSON and violates the contract: the entity
		// type is one the schema refuses. This is the shape a real model failure
		// takes, and it is the one the repair round is for.
		return []byte(`{"schemaVersion":1,"entities":[{"ref":"x","type":"vehicle","canonicalName":"n"}],"events":[],"relations":[]}`), nil
	}
	if mode == MockEmpty {
		return []byte(`{"schemaVersion":1,"summary":"Nothing happens in this chapter.","entities":[],"events":[],"relations":[]}`), nil
	}
	if mode == MockDanglingReference {
		return []byte(`{"schemaVersion":1,"entities":[],"events":[],"relations":[{"sourceRef":"ghost","targetRef":"phantom","relationType":"knows"}]}`), nil
	}

	// The normal path reads the text it was given.
	return []byte(buildMockDocument(request)), nil
}

// buildMockDocument produces a document derived from the chapter's own text.
//
// The derivation is simple on purpose: it looks for the first two Chinese names
// by a fixed rule and one place, then proposes an event and a relation joining
// them. What matters is that every name it proposes APPEARS in the text, so the
// evidence offsets are real and a test can assert where they point. A mock that
// invented names would leave the offset logic untested.
func buildMockDocument(request Request) string {
	names := scanNames(request.Text)
	location := ""
	if len(names) > 2 {
		location = names[2]
	}
	summary := "A deterministic reading of " + request.Title + "."
	if strings.TrimSpace(request.Title) == "" {
		summary = "A deterministic reading of an untitled chapter."
	}

	entities := make([]string, 0, len(names))
	for index, name := range names {
		// The first three become entity refs; the third is also the location, so
		// an event's locationRef resolves to an entity that exists.
		if index >= 3 {
			break
		}
		entities = append(entities, `{"ref":`+jsonString("e"+itoa(index))+`,"type":`+
			jsonString(entityTypeFor(index))+`,"canonicalName":`+jsonString(name)+`}`)
	}
	// A chapter with nothing recognisable still produces a valid, empty reading
	// rather than a document with an unresolved reference.
	if len(entities) == 0 {
		return `{"schemaVersion":1,"summary":` + jsonString(summary) + `,"entities":[],"events":[],"relations":[]}`
	}

	event := `{"ref":"ev1","name":` + jsonString("Something happens in "+request.Title) + `,` +
		`"eventType":"scene","storyTimeOrder":1,` +
		`"confidence":0.5,"participants":[{"entityRef":"e0","role":"actor"}]`
	if location != "" {
		event += `,"locationRef":"e2"`
	}
	event += `}`

	// The relation array is built from a slice rather than by prefixing a comma to
	// a string. The string form produced `[,{...}]` when there was nothing to
	// prefix, which is invalid JSON — a defect the mock's own validation test
	// caught, and the reason that test exists.
	relationList := []string{}
	if len(entities) > 1 {
		relationList = append(relationList,
			`{"sourceRef":"e0","targetRef":"e1","relationType":"knows","validFromEventRef":"ev1","confidence":0.4}`)
	}
	return `{"schemaVersion":1,"summary":` + jsonString(summary) + `,"entities":[` +
		strings.Join(entities, ",") + `],"events":[` + event + `],"relations":[` +
		strings.Join(relationList, ",") + `]}`
}

// entityTypeFor assigns the schema's kinds to the scanned names by position.
//
// The first name is a person and the rest are places, which is a fixed rule
// rather than a guess about the text. The point is that the kinds are
// deterministic and include the ones the entity vocabulary was widened for at
// least once in the fixture set.
func entityTypeFor(index int) string {
	if index == 0 {
		return "character"
	}
	return "location"
}

// scanNames finds name-like spans in a chapter.
//
// The rule is deliberately crude and it is stated rather than disguised: it takes
// the FIRST TWO CHARACTERS of each run of Han script, and the first Latin word of
// each run of letters. In a Chinese sentence the subject usually leads, so the
// leading two characters of a clause are often its subject's name. Often is not
// always, and the mock does not care: what it needs is a string that (a) is
// deterministic, (b) actually appears in the text so the evidence offsets are
// real, and (c) cannot be influenced by what the text MEANS.
//
// That last property is why the rule is positional rather than a dictionary. A
// mock that recognised names would have to read the text for meaning, and then
// the injection fixture would be testing the mock's comprehension rather than the
// boundary around it.
func scanNames(text string) []string {
	var names []string
	seen := map[string]bool{}
	runes := []rune(text)
	add := func(name string) bool {
		if len([]rune(name)) < 2 || seen[name] {
			return false
		}
		seen[name] = true
		names = append(names, name)
		return len(names) == 3
	}
	for index := 0; index < len(runes); {
		switch {
		case isHan(runes[index]):
			start := index
			for index < len(runes) && isHan(runes[index]) {
				index++
			}
			// Two characters from the start of the run: enough for a name, short
			// enough that a sentence-opener is not returned whole.
			run := runes[start:index]
			if len(run) >= 2 {
				if add(string(run[:2])) {
					return names
				}
			}
		case isLetter(runes[index]):
			start := index
			for index < len(runes) && isLetter(runes[index]) {
				index++
			}
			if add(string(runes[start:index])) {
				return names
			}
		default:
			index++
		}
	}
	return names
}

// isLetter reports whether a rune is an ASCII letter.
//
// Digits are excluded on purpose. A word-like run that contains a digit is
// usually an identifier rather than a name, and the mock's own refs ("e0", "e1")
// would otherwise be scanned as names if the text ever contained them.
func isLetter(value rune) bool {
	return (value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z')
}

// isHan reports whether a rune is in the CJK unified ideographs block.
func isHan(value rune) bool {
	return value >= 0x4E00 && value <= 0x9FFF
}

// jsonString renders a Go string as a JSON string.
//
// It escapes through a small switch rather than importing an encoder, so the
// mock's output does not depend on the standard library's escaping choices: a
// golden test that pinned those would fail on a toolchain upgrade for a reason
// that has nothing to do with the contract.
func jsonString(value string) string {
	var builder strings.Builder
	builder.WriteByte('"')
	for _, character := range value {
		switch character {
		case '"':
			builder.WriteString(`\"`)
		case '\\':
			builder.WriteString(`\\`)
		case '\n':
			builder.WriteString(`\n`)
		case '\r':
			builder.WriteString(`\r`)
		case '\t':
			builder.WriteString(`\t`)
		default:
			if character < 0x20 {
				fmt.Fprintf(&builder, `\u%04x`, character)
				continue
			}
			builder.WriteRune(character)
		}
	}
	builder.WriteByte('"')
	return builder.String()
}
