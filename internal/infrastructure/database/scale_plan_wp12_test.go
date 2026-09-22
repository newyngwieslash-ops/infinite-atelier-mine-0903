package database

import (
	"context"
	"testing"
)

// TestWP12QueryPlansUseTheNewIndexes is the check that the 000021 indexes actually do
// something.
//
// A migration that adds an index nobody uses is worse than no migration: it costs a write on
// every insert and buys nothing, and it looks like a fix. The scale benchmarks measured
// roughly the same figures before and after, which is exactly the signal that the index was
// not being chosen — so this asserts the PLAN rather than the timing.
//
// `EXPLAIN QUERY PLAN` is what SQLite actually does, not what it should do. A plan that says
// `USE TEMP B-TREE FOR ORDER BY` means it read the rows and sorted them, which is the defect
// the benchmark found.
func TestWP12QueryPlansUseTheNewIndexes(t *testing.T) {
	ctx := context.Background()
	harness := newMediaHarness(t)
	_ = harness

	// The plans are asserted on a migrated database, which is what a user has.
	plan := func(t *testing.T, query string, args ...any) []string {
		t.Helper()
		rows, err := harness.db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+query, args...)
		if err != nil {
			t.Fatalf("EXPLAIN: %v", err)
		}
		defer rows.Close()
		lines := []string{}
		for rows.Next() {
			var id, parent, notused int
			var detail string
			if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
				t.Fatal(err)
			}
			lines = append(lines, detail)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return lines
	}
	// sortedByTable reports whether any step says the engine must sort.
	needsSort := func(lines []string) bool {
		for _, line := range lines {
			if containsFold(line, "USE TEMP B-TREE FOR ORDER BY") {
				return true
			}
		}
		return false
	}

	cases := []struct {
		name  string
		query string
		args  []any
	}{
		{
			// The vector search's candidate read: the one whose 62 ms the benchmark found.
			name: "VectorCandidates",
			query: `SELECT id FROM memory_items
				WHERE scope_project = ? AND embedding_model = ? AND embedding_version = ?
				  AND embedding_blob IS NOT NULL AND deleted_at = ''
				ORDER BY created_at DESC, id DESC LIMIT ?`,
			args: []any{"p", "m", "v", 500},
		},
		{
			name: "ListItems",
			query: `SELECT id FROM memory_items
				WHERE scope_project = ? AND deleted_at = ''
				ORDER BY created_at DESC, id DESC LIMIT ?`,
			args: []any{"p", 100},
		},
		{
			// The asset list, which the UI makes on every visit to the library.
			//
			// `deleted_at = ''` is IN the query because `ListAssets` always adds it when the
			// caller did not ask for deleted rows, and the column is in the index for that
			// reason. An earlier version of this case omitted it and reported that the index
			// was not used — the test was wrong rather than the index, which is worth stating
			// because the opposite conclusion would have been a migration edited to satisfy a
			// query no code makes.
			name: "ListAssets",
			query: `SELECT id FROM assets
				WHERE project_id = ? AND deleted_at = ''
				ORDER BY created_at DESC, id DESC LIMIT ?`,
			args: []any{"p", 200},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			lines := plan(t, testCase.query, testCase.args...)
			for _, line := range lines {
				t.Logf("  %s", line)
			}
			// The assertion is the ABSENCE of a temp B-tree. A plan may scan an index
			// forwards or backwards and both are fine — what is not fine is sorting after
			// reading, because that is O(n log n) on every page where the index is O(1).
			if needsSort(lines) {
				t.Fatalf("%s still sorts its whole result set, so the 000021 index does not serve it:\n%s",
					testCase.name, joinLines(lines))
			}
		})
	}
}

// containsFold is a case-insensitive substring check, so a plan's wording change does not
// make this test pass by accident.
func containsFold(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	h, n := lowerASCII(haystack), lowerASCII(needle)
	for index := 0; index+len(n) <= len(h); index++ {
		if h[index:index+len(n)] == n {
			return true
		}
	}
	return false
}

func lowerASCII(value string) string {
	out := make([]byte, 0, len(value))
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character >= 'A' && character <= 'Z' {
			character += 'a' - 'A'
		}
		out = append(out, character)
	}
	return string(out)
}

func joinLines(lines []string) string {
	out := ""
	for _, line := range lines {
		out += "  " + line + "\n"
	}
	return out
}
