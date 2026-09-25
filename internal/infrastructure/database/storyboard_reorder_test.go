package database

import (
	"context"
	"database/sql"
	"testing"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/storyboard"
)

// storyboard_reorder_test.go is FR-070's 「重新排序 Shot 后编号和上下游关系正确更新」.
//
// # Why the arithmetic needs its own tests
//
// `storyboard_items` has `UNIQUE (storyboard_version_id, ordinal)`, so a reorder cannot be a series of
// single-row updates: the target position is occupied until its occupant moves, and SQLite checks the
// constraint per statement. The implementation shifts the affected rows out of the way and back, and
// the shift's DIRECTION depends on whether the row moved earlier or later — get that wrong and the
// board ends up with a two-position hole that a reader sees as a missing shot rather than as an
// arithmetic error.
//
// So every case below asserts the FULL resulting order rather than only that the move succeeded, and
// `TestEveryReorderIsAPermutation` is the invariant the whole feature rests on: whatever the input
// order, the result is 1..n with no gap and no duplicate.

// reorderBoard stages an approved board of `count` rows.
//
// IT RETURNS THE HARNESS, and that is not a convenience. The first version returned only the version
// and the row ids, so every caller built its reorderer over a SECOND harness and then queried that
// second, empty database with ids from the first — which read as "the row no longer exists" and sent
// me looking at the reorderer rather than at the fixture. A helper that hands back identifiers must
// hand back the handle they belong to.
func reorderBoard(t *testing.T, count int) (*mediaHarness, string, []string) {
	t.Helper()
	harness := newMediaHarness(t)
	versionID := harness.approvedBoard(t, count, 3)
	rows := harness.rows(t, versionID)
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	if len(ids) != count {
		t.Fatalf("the fixture staged %d rows, want %d", len(ids), count)
	}
	return harness, versionID, ids
}

// orderOf reads a board's rows in board order, which is what a reader sees.
func orderOf(t *testing.T, db *sql.DB, versionID string) []string {
	t.Helper()
	rows, err := db.QueryContext(context.Background(),
		`SELECT id FROM storyboard_items WHERE storyboard_version_id = ? ORDER BY ordinal ASC`, versionID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	order := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		order = append(order, id)
	}
	return order
}

// ordinalsOf reads a board's ordinals in order, which is what the invariant is about.
func ordinalsOf(t *testing.T, db *sql.DB, versionID string) []int {
	t.Helper()
	rows, err := db.QueryContext(context.Background(),
		`SELECT ordinal FROM storyboard_items WHERE storyboard_version_id = ? ORDER BY ordinal ASC`, versionID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	ordinals := []int{}
	for rows.Next() {
		var ordinal int
		if err := rows.Scan(&ordinal); err != nil {
			t.Fatal(err)
		}
		ordinals = append(ordinals, ordinal)
	}
	return ordinals
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}

// TestAMoveToTheFrontShiftsTheOthersLater is the earlier case, and it is the direction the shift
// arithmetic gets wrong first.
func TestAMoveToTheFrontShiftsTheOthersLater(t *testing.T) {
	harness, versionID, ids := reorderBoard(t, 4)
	reorderer := NewStoryboardReorderer(harness.db)

	// Row 3 moves to position 1: rows 1 and 2 shift later by one.
	result, err := reorderer.ReorderItems(context.Background(), ids[2], 1)
	if err != nil {
		t.Fatalf("ReorderItems: %v", err)
	}
	want := []string{ids[2], ids[0], ids[1], ids[3]}
	if got := orderOf(t, harness.db, versionID); !equalStrings(got, want) {
		t.Fatalf("the board reads %v, want %v", got, want)
	}
	if !equalInts(ordinalsOf(t, harness.db, versionID), []int{1, 2, 3, 4}) {
		t.Fatalf("the ordinals are %v, want 1..4 with no gap", ordinalsOf(t, harness.db, versionID))
	}
	// Moved counts the rows whose POSITION changed, not the rows renumbered: the last row keeps its
	// place when row 3 moves to the front, so three rows moved. That is the number a user reads as
	// "what changed", and reporting the board's size instead would overstate it.
	if result.Moved != 3 {
		t.Fatalf("the reorder reports %d rows moved, want 3 (the mover and the two it displaced)", result.Moved)
	}
	// The reported order is the board's, so a caller can render the result rather than re-read it.
	if !equalInts(result.Order, []int{1, 2, 3, 4}) {
		t.Fatalf("the result reports the order %v", result.Order)
	}
}

// TestAMoveToTheBackShiftsTheOthersEarlier is the later case.
func TestAMoveToTheBackShiftsTheOthersEarlier(t *testing.T) {
	harness, versionID, ids := reorderBoard(t, 4)
	reorderer := NewStoryboardReorderer(harness.db)

	// Row 1 moves to position 4: rows 2, 3 and 4 shift earlier by one.
	if _, err := reorderer.ReorderItems(context.Background(), ids[0], 4); err != nil {
		t.Fatalf("ReorderItems: %v", err)
	}
	want := []string{ids[1], ids[2], ids[3], ids[0]}
	if got := orderOf(t, harness.db, versionID); !equalStrings(got, want) {
		t.Fatalf("the board reads %v, want %v", got, want)
	}
	if !equalInts(ordinalsOf(t, harness.db, versionID), []int{1, 2, 3, 4}) {
		t.Fatalf("the ordinals are %v, want 1..4", ordinalsOf(t, harness.db, versionID))
	}
}

// TestAMiddleMoveLeavesTheEdgesAlone is the case where a wrong implementation damages rows it should
// not have touched: moving row 2 to position 3 must not renumber row 4.
func TestAMiddleMoveLeavesTheEdgesAlone(t *testing.T) {
	harness, versionID, ids := reorderBoard(t, 5)
	reorderer := NewStoryboardReorderer(harness.db)

	if _, err := reorderer.ReorderItems(context.Background(), ids[1], 3); err != nil {
		t.Fatalf("ReorderItems: %v", err)
	}
	// Rows 3 moves up; row 5 and the head keep their places.
	want := []string{ids[0], ids[2], ids[1], ids[3], ids[4]}
	if got := orderOf(t, harness.db, versionID); !equalStrings(got, want) {
		t.Fatalf("the board reads %v, want %v", got, want)
	}
	// And the CONTENT travelled with the row: the moved row is still its own row rather than a copy
	// of the one it displaced.
	record, err := NewStoryboardRepository(harness.db).GetStoryboardItem(context.Background(), ids[1])
	if err != nil {
		t.Fatal(err)
	}
	if record.ID != ids[1] {
		t.Fatalf("the row read back is %s, want the one that moved", record.ID)
	}
}

// TestEveryReorderIsAPermutation is the invariant: whatever the move, the board's ordinals are 1..n.
//
// It walks EVERY source and destination for a small board, which is the only way to be sure the
// shift arithmetic holds in both directions and at both edges rather than in the cases I happened to
// think of.
func TestEveryReorderIsAPermutation(t *testing.T) {
	harness, versionID, ids := reorderBoard(t, 5)
	reorderer := NewStoryboardReorderer(harness.db)
	ctx := context.Background()

	for from := 0; from < len(ids); from++ {
		for to := 1; to <= len(ids); to++ {
			if to == from+1 {
				continue // the no-op case, covered separately
			}
			if _, err := reorderer.ReorderItems(ctx, ids[from], to); err != nil {
				t.Fatalf("moving row %d to position %d failed: %v", from+1, to, err)
			}
			ordinals := ordinalsOf(t, harness.db, versionID)
			want := []int{1, 2, 3, 4, 5}
			if !equalInts(ordinals, want) {
				t.Fatalf("after moving row %d to position %d the ordinals are %v, want 1..5",
					from+1, to, ordinals)
			}
			// The board still holds the same ROWS: a reorder is a permutation, never a replacement.
			order := orderOf(t, harness.db, versionID)
			if len(order) != len(ids) {
				t.Fatalf("the board holds %d rows after a move, want %d", len(order), len(ids))
			}
			seen := map[string]bool{}
			for _, id := range order {
				if seen[id] {
					t.Fatalf("the row %s appears twice after a reorder", id)
				}
				seen[id] = true
			}
			for _, id := range ids {
				if !seen[id] {
					t.Fatalf("the row %s disappeared from the board", id)
				}
			}
			// Put the board back in its original order for the next case, so each walk starts from
			// the same state rather than from the previous case's result.
			for index, id := range ids {
				if order[index] == id {
					continue
				}
				position := index + 1
				if _, err := reorderer.ReorderItems(ctx, id, position); err != nil {
					t.Fatalf("restoring the order failed: %v", err)
				}
			}
		}
	}
}

// TestAMoveToItsOwnPositionIsANoOp covers the drag that ends where it started, which a user performs
// constantly and which must not be an error.
func TestAMoveToItsOwnPositionIsANoOp(t *testing.T) {
	harness, versionID, ids := reorderBoard(t, 3)
	reorderer := NewStoryboardReorderer(harness.db)

	result, err := reorderer.ReorderItems(context.Background(), ids[1], 2)
	if err != nil {
		t.Fatalf("a no-op move was refused: %v", err)
	}
	if result.Moved != 0 {
		t.Fatalf("a no-op move reports %d rows moved", result.Moved)
	}
	if got := orderOf(t, harness.db, versionID); !equalStrings(got, ids) {
		t.Fatalf("a no-op move changed the board: %v", got)
	}
}

// TestAPositionOutsideTheBoardIsRefused is the range rule.
//
// A position past the last row would push a row beyond the board's end and a position below one would
// leave a gap at the start — and every reader of a board assumes its ordinals are 1..n (the coverage
// rule, the timeline's order, the export's sequence).
func TestAPositionOutsideTheBoardIsRefused(t *testing.T) {
	harness, versionID, ids := reorderBoard(t, 3)
	reorderer := NewStoryboardReorderer(harness.db)
	ctx := context.Background()

	for _, to := range []int{0, -1, 4, 100} {
		_, err := reorderer.ReorderItems(ctx, ids[0], to)
		if err == nil {
			t.Fatalf("a move to position %d was accepted for a board of three rows", to)
		}
	}
	// And the refusal names the range, because a caller's next step is to pick a legal one.
	_, err := reorderer.ReorderItems(ctx, ids[0], 99)
	if err == nil {
		t.Fatal("a move past the end was accepted")
	}
	if _, ok := storyboard.AsError(err); !ok {
		t.Fatalf("the refusal is not a domain error: %v", err)
	}
	// Nothing moved, which is what the refusal is for.
	if got := orderOf(t, harness.db, versionID); !equalStrings(got, ids) {
		t.Fatalf("a refused reorder changed the board: %v", got)
	}
}

// TestAMissingRowIsRefused covers the case a stale UI produces: the row was deleted in another window.
func TestAMissingRowIsRefused(t *testing.T) {
	harness, _, _ := reorderBoard(t, 3)
	reorderer := NewStoryboardReorderer(harness.db)
	if _, err := reorderer.ReorderItems(context.Background(), "no-such-row", 1); err == nil {
		t.Fatal("a reorder naming a missing row was accepted")
	}
}
