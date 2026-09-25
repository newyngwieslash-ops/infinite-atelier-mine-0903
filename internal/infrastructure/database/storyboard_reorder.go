package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/storyboard"
)

// storyboard_reorder.go is FR-070's 「重新排序 Shot 后编号和上下游关系正确更新」, which is the
// database half of the table-canvas sync.
//
// # Why a reorder needs its own statement rather than a series of updates
//
// `storyboard_items` has `UNIQUE (storyboard_version_id, ordinal)`, so moving a row from position 3
// to position 1 cannot be done by writing the new ordinal: position 1 is occupied until its occupant
// moves, and SQLite checks the constraint per statement rather than per transaction. A loop of
// single-row updates would therefore fail on the first one — and the failure would look like a
// conflict with another window rather than like an ordering problem.
//
// The renumbering is done in ONE transaction with the ordinals SHIFTED OUT OF THE WAY FIRST: every
// affected row is moved to a negative ordinal, then to its final one. Negative values are legal
// (the column's CHECK is `ordinal >= 1` — see below), so this needs a change: the CHECK is enforced,
// which is WHY the shift uses a large offset instead. `ORDINAL_SHIFT` is that offset, and the reason
// it is a named constant is that the two halves of the trick have to agree about it.
type storyboardReorderer struct {
	db *sql.DB
}

// OrdinalShift is the offset the reorder moves rows by while it rearranges them.
//
// It must be larger than any board's row count, because a row shifted by it must not collide with a
// row that has not been shifted — and it must be small enough that the shifted values stay inside the
// column's range. Addressable rows in one board are bounded by the stage's own write limits
// (hundreds), so a million is far above any board and far below any integer limit.
const OrdinalShift = 1_000_000

// NewStoryboardReorderer builds the reorderer over a connection.
func NewStoryboardReorderer(db *sql.DB) *storyboardReorderer {
	return &storyboardReorderer{db: db}
}

// ReorderResult reports what a reorder changed.
type ReorderResult struct {
	// Moved is the number of rows whose ordinal changed.
	Moved int
	// Order is the board's ordinals after the reorder, ascending, so a caller can see the result
	// rather than infer it.
	Order []int
}

// ReorderItems moves one row to a new position and renumbers the rest.
//
// # The contract
//
// `itemID` is the row being MOVED and `toOrdinal` is where it lands. Every row between the old and
// new position shifts by one, which is what makes the result a permutation of the same ordinals
// rather than a board with a gap or a duplicate. The rows keep their CONTENT: this is a reorder, not
// a re-creation, so a row's shot, descriptions and media references are untouched.
//
// # What it refuses
//
// A position outside the board's range. `toOrdinal < 1` or greater than the row count would leave a
// gap at the end or push a row past the last position, and a board whose ordinals are 1..n is what
// every reader of it assumes (the coverage rule, the timeline's ordering, the export's sequence).
func (r *storyboardReorderer) ReorderItems(ctx context.Context, itemID string, toOrdinal int) (ReorderResult, error) {
	if r == nil || r.db == nil {
		return ReorderResult{}, reorderError("STORYBOARD_STORE_UNAVAILABLE", "The storyboard store is unavailable.", nil)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return ReorderResult{}, reorderError("STORYBOARD_TX_FAILED", "The reorder could not be started.", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// The board is read INSIDE the transaction, so the range check and the moves below see one state.
	var versionID string
	var fromOrdinal int
	if err := tx.QueryRowContext(ctx,
		`SELECT storyboard_version_id, ordinal FROM storyboard_items WHERE id = ?`, itemID).Scan(&versionID, &fromOrdinal); err != nil {
		if err == sql.ErrNoRows {
			return ReorderResult{}, storyboard.NotFoundError()
		}
		return ReorderResult{}, reorderError("STORYBOARD_READ_FAILED", "The storyboard row could not be read.", err)
	}
	rows, err := tx.QueryContext(ctx,
		`SELECT ordinal FROM storyboard_items WHERE storyboard_version_id = ? ORDER BY ordinal ASC`, versionID)
	if err != nil {
		return ReorderResult{}, reorderError("STORYBOARD_READ_FAILED", "The board's rows could not be read.", err)
	}
	ordinals := []int{}
	for rows.Next() {
		var ordinal int
		if err := rows.Scan(&ordinal); err != nil {
			_ = rows.Close()
			return ReorderResult{}, reorderError("STORYBOARD_READ_FAILED", "The board's rows could not be read.", err)
		}
		ordinals = append(ordinals, ordinal)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return ReorderResult{}, reorderError("STORYBOARD_READ_FAILED", "The board's rows could not be read.", err)
	}
	_ = rows.Close()

	if toOrdinal < 1 || toOrdinal > len(ordinals) {
		return ReorderResult{}, storyboard.InvalidError(fmt.Sprintf(
			"A board of %d rows has positions 1 to %d.", len(ordinals), len(ordinals)))
	}
	if toOrdinal == fromOrdinal {
		// Nothing to do, and reporting it as a move of zero is more useful than failing: a drag that
		// ends where it started is a no-op a user performs constantly.
		return ReorderResult{Moved: 0, Order: ordinals}, nil
	}

	// THE ORDER IS COMPUTED IN GO AND THE BOARD IS RENUMBERED FROM IT.
	//
	// The first version did this with SQL arithmetic — shift the affected rows out of the way by a
	// large offset, place the mover, bring the shifted rows back — and a simulation of its three
	// statements showed it corrupting the board for every move except a middle one: a shifted row
	// landed where the mover had been rather than where it belonged, and the board lost a row. The
	// unique constraint is what makes the SQL approach tempting (a target position is occupied until
	// its occupant moves) and the arithmetic is what makes it hard to get right.
	//
	// Computing the final order in Go removes the arithmetic: the permutation is two slice operations,
	// which are trivially correct. What remains is the constraint, and that is handled by renumbering
	// through a range the board cannot occupy.
	boardOrder, err := r.boardOrder(ctx, tx, versionID)
	if err != nil {
		return ReorderResult{}, err
	}
	fromIndex := -1
	for index, id := range boardOrder {
		if id == itemID {
			fromIndex = index
		}
	}
	if fromIndex < 0 {
		return ReorderResult{}, storyboard.NotFoundError()
	}
	// `toOrdinal` is a POSITION (1-based) and the target index is computed against the list WITH THE
	// MOVER REMOVED, which is what makes the row land on the position a user dropped it on rather than
	// one short of it when it moves later.
	without := make([]string, 0, len(boardOrder)-1)
	without = append(without, boardOrder[:fromIndex]...)
	without = append(without, boardOrder[fromIndex+1:]...)
	toIndex := toOrdinal - 1
	if toIndex > len(without) {
		toIndex = len(without)
	}
	final := make([]string, 0, len(boardOrder))
	final = append(final, without[:toIndex]...)
	final = append(final, itemID)
	final = append(final, without[toIndex:]...)

	// THE RENUMBER GOES THROUGH A RANGE THE BOARD CANNOT OCCUPY: every row is moved above it first,
	// then numbered 1..n from the order computed above. One statement per row would collide with
	// `UNIQUE (storyboard_version_id, ordinal)` on the way, so the whole board moves out of the way
	// together.
	if _, err := tx.ExecContext(ctx,
		`UPDATE storyboard_items SET ordinal = ordinal + ? WHERE storyboard_version_id = ?`,
		OrdinalShift, versionID); err != nil {
		return ReorderResult{}, reorderError("STORYBOARD_WRITE_FAILED", "The board could not be renumbered.", err)
	}
	for index, id := range final {
		if _, err := tx.ExecContext(ctx,
			`UPDATE storyboard_items SET ordinal = ? WHERE id = ?`, index+1, id); err != nil {
			return ReorderResult{}, reorderError("STORYBOARD_WRITE_FAILED", "The board could not be renumbered.", err)
		}
	}
	movedRows := 0
	for index, id := range final {
		if boardOrder[index] != id {
			movedRows++
		}
	}
	finalOrdinals := make([]int, 0, len(final))
	for index := range final {
		finalOrdinals = append(finalOrdinals, index+1)
	}
	result := ReorderResult{Moved: movedRows, Order: finalOrdinals}
	if err := tx.Commit(); err != nil {
		return ReorderResult{}, reorderError("STORYBOARD_TX_FAILED", "The reorder could not be saved.", err)
	}
	committed = true
	return result, nil
}

// boardOrder reads a board's rows in board order, which is what the permutation is expressed against.
func (r *storyboardReorderer) boardOrder(ctx context.Context, tx *sql.Tx, versionID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT id FROM storyboard_items WHERE storyboard_version_id = ? ORDER BY ordinal ASC`, versionID)
	if err != nil {
		return nil, reorderError("STORYBOARD_READ_FAILED", "The board's order could not be read.", err)
	}
	defer func() { _ = rows.Close() }()
	order := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, reorderError("STORYBOARD_READ_FAILED", "The board's order could not be read.", err)
		}
		order = append(order, id)
	}
	if err := rows.Err(); err != nil {
		return nil, reorderError("STORYBOARD_READ_FAILED", "The board's order could not be read.", err)
	}
	return order, nil
}

// shiftDirection is how far a shifted row moves, given where the moving row went.
//
// It is 2 when the row moves EARLIER (everything between shifts down by the offset minus one, since
// its final position is one lower) and 1 when it moves LATER. The two cases are the reason this is a
// function rather than a constant: getting it wrong produces a board with a two-position hole, which
// a reader would see as a missing shot rather than as an arithmetic error.
func shiftDirection(fromOrdinal, toOrdinal int) int {
	if toOrdinal < fromOrdinal {
		return 2
	}
	return 1
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func reorderError(code, message string, cause error) error {
	return apperror.New(code, "storage", false, message, cause)
}
