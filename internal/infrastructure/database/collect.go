package database

import (
	"context"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
)

// collect.go is the storage side of audio collection: a job's result becomes an
// APPROVED version with its usage in one transaction.
//
// # Why the repository and not the service
//
// The collection used to be three service commands — AttachJobResult, then
// ApproveVersion, then AddUsage — each committing on its own. Three commits are
// three chances to stop halfway, and every halfway state is a row set the mix
// cannot read: a candidate with no approval, an approval that shadows the old
// version with no usage of its own, or a usage pointing at a version the asset
// no longer names. The fix is not "retry more", because a caller that retries
// the same three commands can still die between them; the fix is ONE write.
//
// This is the same shape the eight version families use — read, guard, then the
// status moves in one transaction with the event — and the same shape
// ApprovePanelImage uses, which also has to move a status and a pointer
// together. The repository owns it because a transaction is a storage concern:
// the application layer decides WHAT must be true when the command ends, this
// layer decides THAT it ends in one step.
//
// # The repeat rule
//
// A restarted collection asks about a job it already collected. The answer is
// the existing version — but only when the earlier collection COMPLETED. A job
// row that exists with the version still a candidate (the halfway state above)
// is finished here: the version is approved and the usage recorded, and the
// caller is told it was a repeat rather than a fresh write, so the two are
// distinguishable by the boolean this returns.

// assetCollectRequest is the full storage shape of one collection, as the
// application layer's `collect.go` passes it: the collect facts, the
// identifiers minted by the application (ADR-0005), and the governance events
// recorded in the same transaction when the version is new.
type assetCollectRequest = assets.CollectStorageRequest

// AssetTransactions is the adapter that satisfies the application port
// `assets.TransactionRunner` over an AssetRepository.
//
// A separate type is required because the repository itself already carries a
// `WithinTx(*sql.Tx)` — the raw binding primitive the import's
// cross-repository writes use — and one type cannot hold two methods of one
// name. The adapter exists to name the difference: the primitive binds a
// caller's transaction, this one OWNS the transaction's lifecycle.
type AssetTransactions struct {
	repository *AssetRepository
}

// NewAssetTransactions builds the adapter over one repository.
func NewAssetTransactions(repository *AssetRepository) *AssetTransactions {
	return &AssetTransactions{repository: repository}
}

// WithinTx runs fn with the adapter's repository bound to one fresh
// transaction, which is how the asset service's atomic collection command
// (`collect.go`) scopes its write.
func (a *AssetTransactions) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if a == nil || a.repository == nil || a.repository.db == nil {
		return storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	tx, err := a.repository.db.BeginTx(ctx, nil)
	if err != nil {
		return storageError("ASSET_TX_FAILED", "The transaction could not be started.", err)
	}
	if err := fn(context.WithValue(ctx, collectContextKey{}, a.repository.WithinTx(tx))); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return storageError("ASSET_TX_FAILED", "The transaction could not be committed.", err)
	}
	return nil
}

// collectContext carries the transaction-bound repository a scoped context
// holds.
type collectContextKey struct{}

// transactionBound returns the transaction-bound repository a context carries,
// or nil when the context is not scoped.
func transactionBound(ctx context.Context) *AssetRepository {
	bound, _ := ctx.Value(collectContextKey{}).(*AssetRepository)
	return bound
}

// CollectJobResultVersion turns a job's result into an approved version with
// its usage, in one transaction, and reports whether the version is NEW.
//
// True means a version was created (the first collection of this job); false
// means the job had already produced a version of this asset (a repeat). A
// repeat that had to be FINISHED — the halfway state — is still false, but its
// approval and usage are complete before the call returns.
//
// The partial unique index on approved versions and the unique
// (asset_version_id, consumer_type, consumer_id, usage_role) constraint on
// usages are the schema's own backstop: this function's order (supersede,
// approve, pointer) keeps the index from ever seeing two approved rows, and a
// concurrent collection of the same job is serialized by SQLite's single
// writer rather than duplicating.
func (r *AssetRepository) CollectJobResultVersion(ctx context.Context, request assetCollectRequest) (bool, string, int, error) {
	if r == nil || r.db == nil {
		return false, "", 0, storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	// Inside the service's transaction: write through the bound repository.
	// `WithinTx` above puts the bound repository on the context it hands fn, so
	// the service's own command lands here with the scope on its context.
	if bound := transactionBound(ctx); bound != nil {
		return bound.collectInTx(ctx, request)
	}
	// A repository already bound to a caller's transaction writes through it —
	// the rule ApprovePanelImage states: SQLite has one writer, so opening a
	// second transaction would deadlock.
	if r.tx != nil {
		return r.collectInTx(ctx, request)
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, "", 0, storageError("ASSET_TX_FAILED", "The collection could not be started.", err)
	}
	created, versionID, number, inner := r.WithinTx(tx).collectInTx(ctx, request)
	if inner != nil {
		_ = tx.Rollback()
		return false, "", 0, inner
	}
	if err := tx.Commit(); err != nil {
		return false, "", 0, storageError("ASSET_TX_FAILED", "The collection could not be committed.", err)
	}
	return created, versionID, number, nil
}

// collectInTx performs the whole collection through the transaction-bound
// repository the caller opened — the shared body of the scoped and unscoped
// paths.
func (r *AssetRepository) collectInTx(ctx context.Context, request assetCollectRequest) (bool, string, int, error) {
	repo := r
	// The asset is read FIRST so a stale identifier is a not-found rather than
	// a foreign-key failure, and so the pointer's CAS guard and the supersede
	// read from one consistent snapshot.
	record, err := repo.GetAsset(ctx, request.AssetID)
	if err != nil {
		return false, "", 0, err
	}
	// A job produces ONE version per asset. The check reads inside the
	// transaction, so a concurrent collection of the same job is serialized by
	// SQLite's single writer and the second one sees the first's rows.
	existing, err := repo.ListVersions(ctx, request.AssetID)
	if err != nil {
		return false, "", 0, err
	}
	for _, version := range existing {
		if version.GenerationJobID == request.JobID {
			// The job was collected before. Finish it rather than repeat it: a
			// halfway state is repaired, a complete one is a no-op, and both
			// are reported as "already there".
			if err := repo.finishCollectedVersion(ctx, record, version, request); err != nil {
				return false, "", 0, err
			}
			return false, version.ID, version.VersionNumber, nil
		}
	}

	// The version's number comes from the stored maximum, like every other
	// version write, so a superseded version's number is never reused.
	highest, err := repo.MaxVersionNumber(ctx, request.AssetID)
	if err != nil {
		return false, "", 0, err
	}
	version := asset.Version{
		ID:               request.VersionID,
		AssetID:          request.AssetID,
		VersionNumber:    highest + 1,
		Status:           asset.VersionCandidate,
		Prompt:           request.Prompt,
		ProviderConfigID: request.ProviderConfigID,
		ModelConfigID:    request.ModelConfigID,
		ModelParameters:  request.ModelParameters,
		GenerationJobID:  request.JobID,
		SourceAgentRunID: request.SourceAgentRunID,
		CreatedByID:      request.CreatedByID,
		// The producer is the AGENT when a run is named and the SYSTEM
		// otherwise — the same rule AttachJobResult applies.
		CreatedByType: asset.CreatedBySystem,
		CreatedAt:     request.Usage.CreatedAt,
	}
	if request.SourceAgentRunID != "" {
		version.CreatedByType = asset.CreatedByAgent
	}
	if err := version.Validate(); err != nil {
		return false, "", 0, err
	}
	if err := repo.CreateVersion(ctx, version); err != nil {
		return false, "", 0, err
	}
	// The files are attached inside the same transaction, so a failure after
	// the version row cannot leave a version without its primary file — the
	// state CanApprove refuses and the mix silently skips.
	if err := repo.attachCollectFiles(ctx, version.ID, request.Files, request.Usage.CreatedAt); err != nil {
		return false, "", 0, err
	}

	// The approval switch, in the order the partial unique index requires:
	// supersede first, approve second, then the asset's pointer moves with a
	// CAS guard on the revision the read above took.
	if err := repo.approveWithinTx(ctx, record, version.ID); err != nil {
		return false, "", 0, err
	}

	// The usage is the row the mix's join reads, and it is part of the same
	// transaction: a version approved with no usage is the "collected but
	// silent" state the audit names.
	if err := repo.writeCollectUsage(ctx, request.Usage); err != nil {
		return false, "", 0, err
	}
	// The governance records, same transaction: an approval with no record of
	// it is the one outcome a §16 stream must not have.
	if err := repo.writeCollectEvent(ctx, request.CreatedEvent); err != nil {
		return false, "", 0, err
	}
	if err := repo.writeCollectEvent(ctx, request.ApprovedEvent); err != nil {
		return false, "", 0, err
	}
	return true, version.ID, version.VersionNumber, nil
}

// attachCollectFiles links the job's committed objects to a version, with the
// first file primary when the caller did not say otherwise — the same default
// AttachJobResult applies.
func (r *AssetRepository) attachCollectFiles(ctx context.Context, versionID string, files []assets.AttachJobFile, at time.Time) error {
	for index, input := range files {
		role := input.Role
		if role == "" {
			role = asset.RoleReference
			if index == 0 {
				role = asset.RolePrimary
			}
		}
		file := asset.File{
			VersionID: versionID, FileHash: input.FileHash, Role: role,
			Ordinal: input.Ordinal, CreatedAt: at,
		}
		if err := file.Validate(); err != nil {
			return err
		}
		if err := r.AddFile(ctx, file); err != nil {
			return err
		}
	}
	return nil
}

// approveWithinTx performs the approval switch on the transaction this
// repository is bound to: supersede the previous approval, approve the new
// version, move the asset's pointer with a CAS guard. The caller wrote the
// version's files already, so the file-count precondition is discharged by
// construction.
func (r *AssetRepository) approveWithinTx(ctx context.Context, record asset.Asset, versionID string) error {
	conn := r.conn()
	if conn == nil {
		return storageError("ASSET_STORE_UNAVAILABLE", "The asset store is unavailable.", nil)
	}
	if previous := record.CurrentApprovedVersionID; previous != "" && previous != versionID {
		if _, err := conn.ExecContext(ctx, `UPDATE asset_versions SET status = 'superseded'
			WHERE id = ? AND status = 'approved'`, previous); err != nil {
			return storageError("ASSET_WRITE_FAILED", "The previous approval could not be superseded.", err)
		}
	}
	if _, err := conn.ExecContext(ctx, `UPDATE asset_versions SET status = 'approved' WHERE id = ?`, versionID); err != nil {
		return storageError("ASSET_WRITE_FAILED", "The version could not be approved.", err)
	}
	result, err := conn.ExecContext(ctx, `UPDATE assets
		SET current_approved_version_id = ?, updated_at = ?, revision = revision + 1
		WHERE id = ? AND revision = ?`,
		versionID, formatTime(record.UpdatedAt), record.ID, record.Revision)
	if err != nil {
		return storageError("ASSET_WRITE_FAILED", "The asset could not be updated.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError("ASSET_WRITE_FAILED", "The asset could not be updated.", err)
	}
	if affected == 0 {
		return asset.ConflictError("This asset changed in another window. Reload it and try again.")
	}
	return nil
}

// writeCollectUsage inserts the usage row, refusing an invalid one and letting
// the schema's unique constraint reject a duplicate (which the caller's repeat
// path treats as success before it gets here).
func (r *AssetRepository) writeCollectUsage(ctx context.Context, usage asset.Usage) error {
	if usage.ID == "" {
		return asset.InvalidError("A collected version's usage needs an identifier.")
	}
	if err := usage.Validate(); err != nil {
		return err
	}
	return r.AddUsage(ctx, usage)
}

// writeCollectEvent records one governance event inside the transaction,
// skipping an empty record (a build without a recorder collects without
// events).
func (r *AssetRepository) writeCollectEvent(ctx context.Context, record assets.CollectEvent) error {
	if record.Empty() {
		return nil
	}
	return AppendEvent(ctx, r.conn(), record.Record())
}

// collectEventRecord is the event this file receives from the application
// layer. It is an alias so the storage signature does not restate the event
// package's own type twice.

// finishCollectedVersion completes a collection an earlier run left halfway.
//
// The three writes used to be three transactions, so databases written by the
// old collector can hold a job whose version exists but was never approved, or
// was approved but never given its usage. A repeat collection repairs both,
// because the alternative is a row set the mix skips forever with no user path
// to see why.
func (r *AssetRepository) finishCollectedVersion(ctx context.Context, record asset.Asset, version asset.Version, request assetCollectRequest) error {
	// The files a halfway collection missed are attached here too. The unique
	// (version, hash, role) constraint makes a complete set a no-op.
	if err := r.attachCollectFiles(ctx, version.ID, request.Files, request.Usage.CreatedAt); err != nil {
		return err
	}
	switch version.Status {
	case asset.VersionApproved:
		// Complete as far as the status goes; only the usage can be missing.
	default:
		// candidate, draft, rejected… — the approval switch runs on the same
		// rule the service's ApproveVersion applies, with the file-count
		// precondition discharged by the writes above.
		if err := versioning.CanApprove(version.Status, versioning.StatusApproved); err != nil {
			// A superseded or stale version means another take was approved
			// after this job's collection. That is a user decision that
			// already happened; this repeat must not reverse it.
			return nil
		}
		if err := r.approveWithinTx(ctx, record, version.ID); err != nil {
			return err
		}
	}
	// The usage names the version that EXISTS — the one the interrupted first
	// attempt wrote, not the identifier this repeat minted (a minted id names
	// a row that will never exist, and a usage pointing at it is a FK the
	// schema rightly refuses). The usage itself is idempotent by the schema's
	// unique constraint, and a conflict here is a SUCCESS: the repeat's job is
	// to make the row exist.
	usage := request.Usage
	usage.AssetVersionID = version.ID
	if err := r.AddUsage(ctx, usage); err != nil {
		if domainErr, ok := asset.AsError(err); !ok || domainErr.Category != asset.CategoryConflict {
			return err
		}
	}
	return nil
}
