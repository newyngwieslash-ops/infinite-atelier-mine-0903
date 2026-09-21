package database

import (
	"context"
	"database/sql"

	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
)

// gap.go is the storage of the Asset Gap Report.
//
// A REPORT AND ITS ITEMS ARE WRITTEN IN ONE TRANSACTION, which is the rule migration
// 000018's shape implies rather than a nicety: an approved report with no lines is a
// report whose "no required asset is missing" is vacuously true, and the batch gate
// that reads it would pass on nothing. The same transaction writes the governance
// event, following the pattern the four version families already use.

// withinTx runs fn inside a transaction of this repository's database.
//
// The same helper `script_structure.go` states for the script repository, repeated
// because the two repositories are separate types over the same connection and Go has
// no shared base class. It is NOT a second implementation of a rule: the rule is
// "begin, run, commit" and there is nothing to disagree about.
func (r *AssetRepository) withinTx(ctx context.Context, fn func(repo *AssetRepository) error) error {
	if r.db == nil {
		return storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return storageError("ASSET_WRITE_FAILED", "The gap report could not be saved.", err)
	}
	if err := fn(r.WithinTx(tx)); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return storageError("ASSET_WRITE_FAILED", "The gap report could not be saved.", err)
	}
	return nil
}

const gapReportSelectColumns = `SELECT id, episode_id, script_version_id, version_number, status,
	based_on_version_id, source_agent_run_id, approval_trace_id, summary, created_by_type,
	created_by_id, change_reason, created_at, updated_at, revision FROM asset_gap_reports`

const gapItemSelectColumns = `SELECT id, report_id, ordinal, asset_type, story_entity_id,
	story_entity_name, asset_id, status, usage_role, required, notes, created_at
	FROM asset_gap_items`

// CreateGapReportWithItems stores a report and its lines in one transaction.
//
// One transaction because an approved report with no lines is a report whose "no
// required asset is missing" is vacuously true, and the batch gate that reads it would
// pass on nothing.
//
// There is no event parameter, and its absence is the reason ADR-0013 gives: section
// 17's event list is closed and has no name for this artifact, so the approval is
// governed through the user gate's `UserGateDecided` on the workflow stream instead of
// a name this build would have had to invent.
func (r *AssetRepository) CreateGapReportWithItems(ctx context.Context, report asset.GapReport, items []asset.GapItem) error {
	if err := report.Validate(); err != nil {
		return err
	}
	for _, item := range items {
		if err := item.Validate(); err != nil {
			return err
		}
		// The report's own identity is what the line must belong to, so a caller that
		// built lines against a different report learns it here rather than by writing
		// rows nothing will read.
		if item.ReportID != report.ID {
			return asset.InvalidError("A gap item belongs to a different report than the one being written.")
		}
	}
	return r.withinTx(ctx, func(repo *AssetRepository) error {
		if err := repo.createGapReport(ctx, report); err != nil {
			return err
		}
		for _, item := range items {
			if err := repo.createGapItem(ctx, item); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *AssetRepository) createGapReport(ctx context.Context, report asset.GapReport) error {
	conn := r.conn()
	if conn == nil {
		return storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO asset_gap_reports
		(id, episode_id, script_version_id, version_number, status, based_on_version_id,
		 source_agent_run_id, approval_trace_id, summary, created_by_type, created_by_id,
		 change_reason, created_at, updated_at, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		report.ID, report.EpisodeID, report.ScriptVersionID, report.VersionNumber,
		string(report.Status), report.BasedOnVersionID, report.SourceAgentRunID,
		report.ApprovalTraceID, report.Summary,
		string(report.CreatedByType), report.CreatedByID, report.ChangeReason,
		formatTime(report.CreatedAt), formatTime(report.UpdatedAt), report.Revision)
	if err != nil {
		if isUniqueViolation(err) {
			return asset.ConflictError("That gap report version number is already used for this episode.")
		}
		if isForeignKeyViolation(err) {
			return asset.ConflictError("A gap report must name an existing episode and script version.")
		}
		return storageError("ASSET_WRITE_FAILED", "The gap report could not be saved.", err)
	}
	return nil
}

func (r *AssetRepository) createGapItem(ctx context.Context, item asset.GapItem) error {
	conn := r.conn()
	if conn == nil {
		return storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO asset_gap_items
		(id, report_id, ordinal, asset_type, story_entity_id, story_entity_name, asset_id,
		 status, usage_role, required, notes, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.ReportID, item.Ordinal, string(item.AssetType), item.StoryEntityID,
		item.StoryEntityName, item.AssetID, string(item.Status), item.UsageRole,
		boolInt(item.Required), item.Notes, formatTime(item.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return asset.ConflictError("That gap item position is already used in this report.")
		}
		if isForeignKeyViolation(err) {
			return asset.ConflictError("A gap item must belong to an existing report.")
		}
		return storageError("ASSET_WRITE_FAILED", "The gap item could not be saved.", err)
	}
	return nil
}

// GetGapReport returns one report.
func (r *AssetRepository) GetGapReport(ctx context.Context, id string) (asset.GapReport, error) {
	conn := r.conn()
	if conn == nil {
		return asset.GapReport{}, storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, gapReportSelectColumns+` WHERE id = ?`, id)
	report, err := scanGapReport(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return asset.GapReport{}, asset.NotFoundError()
		}
		return asset.GapReport{}, storageError("ASSET_READ_FAILED", "The gap report could not be read.", err)
	}
	return report, nil
}

// ListGapReports returns an episode's reports newest version first.
func (r *AssetRepository) ListGapReports(ctx context.Context, episodeID string) ([]asset.GapReport, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, gapReportSelectColumns+
		` WHERE episode_id = ? ORDER BY version_number DESC`, episodeID)
	if err != nil {
		return nil, storageError("ASSET_READ_FAILED", "The gap reports could not be read.", err)
	}
	defer rows.Close()
	reports := []asset.GapReport{}
	for rows.Next() {
		report, err := scanGapReport(rows)
		if err != nil {
			return nil, storageError("ASSET_READ_FAILED", "The gap reports could not be read.", err)
		}
		reports = append(reports, report)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("ASSET_READ_FAILED", "The gap reports could not be read.", err)
	}
	return reports, nil
}

// MaxGapReportVersionNumber reports the highest version number an episode's reports
// reach, or zero when it has none.
func (r *AssetRepository) MaxGapReportVersionNumber(ctx context.Context, episodeID string) (int, error) {
	conn := r.conn()
	if conn == nil {
		return 0, storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	var max sql.NullInt64
	err := conn.QueryRowContext(ctx,
		`SELECT MAX(version_number) FROM asset_gap_reports WHERE episode_id = ?`, episodeID).Scan(&max)
	if err != nil {
		return 0, storageError("ASSET_READ_FAILED", "The gap report version number could not be read.", err)
	}
	if !max.Valid {
		return 0, nil
	}
	return int(max.Int64), nil
}

// CurrentApprovedGapReport returns the episode's approved report, or found=false.
//
// False rather than an error when none is approved: the schema's partial unique index
// means there is at most one, and "not approved yet" is the ordinary state of an
// analysis still under review. It is the read AC-BOARD-001's gate makes.
func (r *AssetRepository) CurrentApprovedGapReport(ctx context.Context, episodeID string) (asset.GapReport, bool, error) {
	conn := r.conn()
	if conn == nil {
		return asset.GapReport{}, false, storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	row := conn.QueryRowContext(ctx, gapReportSelectColumns+
		` WHERE episode_id = ? AND status = 'approved'`, episodeID)
	report, err := scanGapReport(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return asset.GapReport{}, false, nil
		}
		return asset.GapReport{}, false, storageError("ASSET_READ_FAILED", "The gap report could not be read.", err)
	}
	return report, true, nil
}

// ListGapItems returns a report's lines in ordinal order.
func (r *AssetRepository) ListGapItems(ctx context.Context, reportID string) ([]asset.GapItem, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, gapItemSelectColumns+
		` WHERE report_id = ? ORDER BY ordinal`, reportID)
	if err != nil {
		return nil, storageError("ASSET_READ_FAILED", "The gap items could not be read.", err)
	}
	defer rows.Close()
	items := []asset.GapItem{}
	for rows.Next() {
		item, err := scanGapItem(rows)
		if err != nil {
			return nil, storageError("ASSET_READ_FAILED", "The gap items could not be read.", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("ASSET_READ_FAILED", "The gap items could not be read.", err)
	}
	return items, nil
}

// ApproveGapReport switches which report is approved, recording the decision's trace on
// the row in the same transaction.
//
// One transaction, for the reason the four version families use one: a report whose
// status says approved and whose trace identifier was never written is an approval with
// nothing connecting it to the user gate decision that made it — and a trace with no
// status change is a link to something that did not happen.
//
// The replaced report is superseded FIRST so the partial unique index never sees two
// approved rows, which is the same order the storyboard repository's approval uses.
func (r *AssetRepository) ApproveGapReport(ctx context.Context, reportID, episodeID string, expectedStatus asset.VersionStatus, traceID string, at time.Time) error {
	return r.withinTx(ctx, func(repo *AssetRepository) error {
		conn := repo.conn()
		if conn == nil {
			return storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
		}
		_, err := conn.ExecContext(ctx, `UPDATE asset_gap_reports
			SET status = 'superseded', updated_at = ?, revision = revision + 1
			WHERE episode_id = ? AND status = 'approved'`, formatTime(at), episodeID)
		if err != nil {
			return storageError("ASSET_WRITE_FAILED", "The previous gap report could not be superseded.", err)
		}
		result, err := conn.ExecContext(ctx, `UPDATE asset_gap_reports
			SET status = 'approved', approval_trace_id = ?, updated_at = ?, revision = revision + 1
			WHERE id = ? AND status = ?`,
			traceID, formatTime(at), reportID, string(expectedStatus))
		if err != nil {
			return storageError("ASSET_WRITE_FAILED", "The gap report could not be approved.", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return storageError("ASSET_WRITE_FAILED", "The gap report could not be approved.", err)
		}
		if affected == 0 {
			// Nothing moved, so the caller's copy of the status was stale. Refused
			// rather than reported as success: an approval that changed no row is not
			// an approval.
			return asset.ConflictError("This gap report changed in another window. Reload it and try again.")
		}
		return nil
	})
}

// AddUsages records several usages in one transaction.
//
// It exists because the batch that produces a panel's candidates writes one usage per
// candidate, and a partial set would make "which images belong to this panel" depend on
// how far the batch got before it failed.
func (r *AssetRepository) AddUsages(ctx context.Context, usages []asset.Usage) error {
	if len(usages) == 0 {
		return nil
	}
	return r.withinTx(ctx, func(repo *AssetRepository) error {
		for _, usage := range usages {
			if err := repo.AddUsage(ctx, usage); err != nil {
				return err
			}
		}
		return nil
	})
}

// AddRelations records several lineage edges in one transaction.
func (r *AssetRepository) AddRelations(ctx context.Context, relations []asset.Relation) error {
	if len(relations) == 0 {
		return nil
	}
	return r.withinTx(ctx, func(repo *AssetRepository) error {
		for _, relation := range relations {
			if err := repo.AddRelation(ctx, relation); err != nil {
				return err
			}
		}
		return nil
	})
}

// scanGapReport reads one report row.
func scanGapReport(row rowScanner) (asset.GapReport, error) {
	var report asset.GapReport
	var status, createdBy string
	var createdAt, updatedAt string
	err := row.Scan(&report.ID, &report.EpisodeID, &report.ScriptVersionID, &report.VersionNumber,
		&status, &report.BasedOnVersionID, &report.SourceAgentRunID, &report.ApprovalTraceID,
		&report.Summary, &createdBy, &report.CreatedByID, &report.ChangeReason,
		&createdAt, &updatedAt, &report.Revision)
	if err != nil {
		return asset.GapReport{}, err
	}
	report.Status = asset.VersionStatus(status)
	report.CreatedByType = asset.CreatedByType(createdBy)
	report.CreatedAt = parseTime(createdAt)
	report.UpdatedAt = parseTime(updatedAt)
	return report, nil
}

// scanGapItem reads one item row.
func scanGapItem(row rowScanner) (asset.GapItem, error) {
	var item asset.GapItem
	var assetType, status, createdAt string
	var required int
	err := row.Scan(&item.ID, &item.ReportID, &item.Ordinal, &assetType, &item.StoryEntityID,
		&item.StoryEntityName, &item.AssetID, &status, &item.UsageRole, &required,
		&item.Notes, &createdAt)
	if err != nil {
		return asset.GapItem{}, err
	}
	item.AssetType = asset.Type(assetType)
	item.Status = asset.GapStatus(status)
	item.Required = required != 0
	item.CreatedAt = parseTime(createdAt)
	return item, nil
}
