package id

import (
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestGeneratedIDsAreCanonicalUUIDv7 covers the RFC layout ADR-0005 fixes:
// version nibble, variant bits, canonical form, and timestamp round-trip.
func TestGeneratedIDsAreCanonicalUUIDv7(t *testing.T) {
	clock := time.Date(2026, 9, 16, 12, 34, 56, 789_000_000, time.UTC)
	generator := NewGeneratorWithClock(func() time.Time { return clock })

	value, err := generator.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(value) != 36 {
		t.Fatalf("id %q has length %d, want 36", value, len(value))
	}
	if strings.ToLower(value) != value {
		t.Fatalf("id %q is not lowercase", value)
	}
	if !Valid(value) {
		t.Fatalf("generated id %q does not validate", value)
	}
	// Version nibble.
	if value[14] != '7' {
		t.Fatalf("version nibble = %q, want '7'", value[14])
	}
	// Variant bits: first character of the fourth group is 8, 9, a or b.
	switch value[19] {
	case '8', '9', 'a', 'b':
	default:
		t.Fatalf("variant nibble = %q, want one of 8/9/a/b", value[19])
	}
	// Timestamp field round-trips to the supplied clock.
	stamp, ok := Timestamp(value)
	if !ok {
		t.Fatal("Timestamp rejected a generated id")
	}
	if !stamp.Equal(clock.Truncate(time.Millisecond)) {
		t.Fatalf("timestamp = %s, want %s", stamp, clock.Truncate(time.Millisecond))
	}
}

// TestGeneratedIDsSortByCreationTime proves the property callers rely on:
// the canonical string form orders identifiers by creation time.
func TestGeneratedIDsSortByCreationTime(t *testing.T) {
	base := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clock := base
	generator := NewGeneratorWithClock(func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		value := clock
		clock = clock.Add(time.Millisecond)
		return value
	})

	generated := make([]string, 0, 200)
	for index := 0; index < 200; index++ {
		value, err := generator.New()
		if err != nil {
			t.Fatal(err)
		}
		generated = append(generated, value)
	}
	// Generate in the same millisecond as well: those must still be distinct.
	for index := 0; index < 50; index++ {
		value, err := generator.New()
		if err != nil {
			t.Fatal(err)
		}
		generated = append(generated, value)
	}

	if !sort.StringsAreSorted(generated) {
		t.Fatal("identifiers are not in lexicographic creation order")
	}
	seen := map[string]bool{}
	for _, value := range generated {
		if seen[value] {
			t.Fatalf("duplicate identifier %q", value)
		}
		seen[value] = true
	}
}

// TestConcurrentGenerationIsUniqueAndSafe is the race/duplicate probe: many
// goroutines generating at once must produce distinct, valid identifiers.
func TestConcurrentGenerationIsUniqueAndSafe(t *testing.T) {
	generator := NewGenerator()
	const workers = 16
	const perWorker = 250

	var mu sync.Mutex
	seen := map[string]bool{}
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := 0; index < perWorker; index++ {
				value, err := generator.New()
				if err != nil {
					errs <- err
					return
				}
				if !Valid(value) {
					errs <- errInvalid(value)
					return
				}
				mu.Lock()
				if seen[value] {
					mu.Unlock()
					errs <- errDuplicate(value)
					return
				}
				seen[value] = true
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if len(seen) != workers*perWorker {
		t.Fatalf("generated %d unique identifiers, want %d", len(seen), workers*perWorker)
	}
}

type errInvalid string

func (e errInvalid) Error() string { return "invalid identifier: " + string(e) }

type errDuplicate string

func (e errDuplicate) Error() string { return "duplicate identifier: " + string(e) }

// TestValidationRejectsOtherVersionsAndShapes proves a foreign identifier
// cannot enter a UUIDv7 column: the ordering guarantee only holds for v7.
func TestValidationRejectsOtherVersionsAndShapes(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{"empty", ""},
		{"too short", "018f2b3c-4d5e"},
		{"uuid v4", "9f1c2b3a-4d5e-4f6a-8b7c-1d2e3f4a5b6c"},
		{"uuid v1", "9f1c2b3a-4d5e-1f6a-8b7c-1d2e3f4a5b6c"},
		{"uppercase", "018F2B3C-4D5E-7F6A-8B7C-1D2E3F4A5B6C"},
		{"bad variant", "018f2b3c-4d5e-7f6a-0b7c-1d2e3f4a5b6c"},
		{"non hex", "018f2b3c-4d5e-7f6a-8b7c-1d2e3f4a5b6z"},
		{"no hyphens", "018f2b3c4d5e7f6a8b7c1d2e3f4a5b6c"},
		{"hyphens misplaced", "018f2b3c-4d5e-7f6a-8b7c1-d2e3f4a5b6c"},
		{"trailing space", "018f2b3c-4d5e-7f6a-8b7c-1d2e3f4a5b6c "},
		{"nil uuid", "00000000-0000-0000-0000-000000000000"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if Valid(testCase.value) {
				t.Fatalf("Valid(%q) = true, want false", testCase.value)
			}
			if _, ok := Timestamp(testCase.value); ok {
				t.Fatalf("Timestamp(%q) succeeded, want failure", testCase.value)
			}
		})
	}
}

// TestFormatMatchesGeneratorBytes pins the byte-to-string mapping, so a change
// in either half is caught.
func TestFormatMatchesGeneratorBytes(t *testing.T) {
	raw := [16]byte{
		0x01, 0x8f, 0x2b, 0x3c, 0x4d, 0x5e, 0x7f, 0x6a,
		0x8b, 0x7c, 0x1d, 0x2e, 0x3f, 0x4a, 0x5b, 0x6c,
	}
	const want = "018f2b3c-4d5e-7f6a-8b7c-1d2e3f4a5b6c"
	if got := Format(raw); got != want {
		t.Fatalf("Format = %q, want %q", got, want)
	}
	if !Valid(want) {
		t.Fatal("the pinned example must validate")
	}
}
