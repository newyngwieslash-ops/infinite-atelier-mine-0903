package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

type migrationFile struct {
	version  int
	name     string
	sql      string
	checksum string
}

func applyMigrations(ctx context.Context, db *sql.DB, fsys fs.FS) error {
	migrations, err := loadMigrationFiles(fsys)
	if err != nil {
		return migrationError(err)
	}
	applied, err := loadApplied(ctx, db)
	if err != nil {
		return migrationError(err)
	}
	for _, migration := range migrations {
		if checksum, ok := applied[migration.version]; ok {
			if checksum != migration.checksum {
				return migrationError(fmt.Errorf("checksum mismatch for version %d", migration.version))
			}
			continue
		}
		if err := applyOne(ctx, db, migration); err != nil {
			return migrationError(err)
		}
		applied[migration.version] = migration.checksum
	}
	return nil
}

func applyOne(ctx context.Context, db *sql.DB, migration migrationFile) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range splitSQL(migration.sql) {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, checksum, applied_at) VALUES (?, ?, ?)`,
		migration.version, migration.checksum, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", migration.version)); err != nil {
		return err
	}
	return tx.Commit()
}

func loadMigrationFiles(fsys fs.FS) ([]migrationFile, error) {
	names, err := globSQL(fsys)
	if err != nil {
		return nil, err
	}
	files := make([]migrationFile, 0, len(names))
	seen := map[int]string{}
	for _, name := range names {
		version, err := migrationVersion(name)
		if err != nil {
			return nil, err
		}
		if previous, ok := seen[version]; ok {
			return nil, fmt.Errorf("duplicate migration version %d (%s and %s)", version, previous, name)
		}
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(data)
		seen[version] = name
		files = append(files, migrationFile{
			version:  version,
			name:     name,
			sql:      string(data),
			checksum: hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].version < files[j].version })
	return files, nil
}

func globSQL(fsys fs.FS) ([]string, error) {
	for _, pattern := range []string{"migrations/*.sql", "*.sql"} {
		names, err := fs.Glob(fsys, pattern)
		if err != nil {
			return nil, err
		}
		if len(names) > 0 {
			return names, nil
		}
	}
	return nil, nil
}

func migrationVersion(name string) (int, error) {
	base := name
	if i := strings.LastIndex(name, "/"); i >= 0 {
		base = name[i+1:]
	}
	prefix, rest, ok := strings.Cut(base, "_")
	if !ok || !strings.HasSuffix(rest, ".sql") {
		return 0, fmt.Errorf("invalid migration name %s", name)
	}
	version, err := strconv.Atoi(prefix)
	if err != nil || version < 1 {
		return 0, fmt.Errorf("invalid migration version %s", name)
	}
	return version, nil
}

func loadApplied(ctx context.Context, db *sql.DB) (map[int]string, error) {
	applied := map[int]string{}
	var name string
	err := db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name='schema_migrations'`).Scan(&name)
	if err == sql.ErrNoRows {
		return applied, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT version, checksum FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var version int
		var checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			return nil, err
		}
		applied[version] = checksum
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return applied, nil
}

func splitSQL(script string) []string {
	parts := strings.Split(script, ";")
	statements := make([]string, 0, len(parts))
	for _, part := range parts {
		statement := strings.TrimSpace(part)
		if statement != "" {
			statements = append(statements, statement)
		}
	}
	return statements
}

func migrationError(cause error) error {
	var appErr *apperror.Error
	if errors.As(cause, &appErr) {
		return cause
	}
	return apperror.New("DATABASE_MIGRATION_FAILED", "storage", false, "The local database could not be updated.", cause)
}
