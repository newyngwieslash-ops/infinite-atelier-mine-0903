package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

// app_settings.go is RP-06.3 + RP-09.2's storage: the key-value settings
// table migration 000033 holds, with the two operations both features need.
//
// # The revision contract
//
// GetSettingRevision returns the revision a caller read; SetSetting refuses
// when it no longer matches — the same lost-update rule the provider config's
// revision enforces, at the same boundary. A setting absent from the table
// means "code default", so reading one that was never written is a normal
// miss, not an error.

// GetSetting returns one setting's value, with found=false when the setting
// has never been written (the caller's code default applies).
func (r *AgentRepository) GetSetting(ctx context.Context, key string) (string, int64, bool, error) {
	conn := r.conn()
	if conn == nil {
		return "", 0, false, storageError("AGENT_STORE_UNAVAILABLE", "The agent store is unavailable.", nil)
	}
	var value string
	var revision int64
	err := conn.QueryRowContext(ctx, `SELECT value, revision FROM app_settings WHERE key = ?`, key).
		Scan(&value, &revision)
	if err == sql.ErrNoRows {
		return "", 0, false, nil
	}
	if err != nil {
		return "", 0, false, storageError("AGENT_READ_FAILED", "The setting could not be read.", err)
	}
	return value, revision, true, nil
}

// SetSetting writes one setting under its revision guard. Creating a setting
// that does not exist yet requires expectedRevision 0 — the "no row" state.
func (r *AgentRepository) SetSetting(ctx context.Context, key, value string, expectedRevision int64) error {
	conn := r.conn()
	if conn == nil {
		return storageError("AGENT_STORE_UNAVAILABLE", "The agent store is unavailable.", nil)
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return storageError("AGENT_WRITE_FAILED", "A setting needs a key.", nil)
	}
	if expectedRevision == 0 {
		// The create path: an INSERT that loses to a concurrent creator is a
		// conflict, the same verdict an update revision mismatch gets.
		result, err := conn.ExecContext(ctx,
			`INSERT INTO app_settings (key, value, revision, updated_at)
			 VALUES (?, ?, 1, strftime('%Y-%m-%dT%H:%M:%fZ','now'))
			 ON CONFLICT(key) DO NOTHING`, key, value)
		if err != nil {
			return storageError("AGENT_WRITE_FAILED", "The setting could not be saved.", err)
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			return storageError("AGENT_CONFLICT", "That setting changed since it was read; reload and retry.", nil)
		}
		return nil
	}
	result, err := conn.ExecContext(ctx,
		`UPDATE app_settings SET value = ?, revision = revision + 1, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
		 WHERE key = ? AND revision = ?`, value, key, expectedRevision)
	if err != nil {
		return storageError("AGENT_WRITE_FAILED", "The setting could not be saved.", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return storageError("AGENT_CONFLICT", "That setting changed since it was read; reload and retry.", nil)
	}
	return nil
}

// agentEnabledKey is the app_settings key the agent enable/disable switch
// persists under (RP-06.3). The value is a JSON object of disabled agent
// keys, so one setting carries the whole switch state and a multi-window
// writer conflicts at the setting level rather than corrupting the map.
const agentEnabledKey = "agent.disabled_keys"

// PersistDisabledAgents stores the disabled set. The whole set travels so a
// reader after a restart sees exactly what the user left.
func (r *AgentRepository) PersistDisabledAgents(ctx context.Context, disabledKeys []string) error {
	value := "[]"
	if len(disabledKeys) > 0 {
		encoded, err := json.Marshal(disabledKeys)
		if err != nil {
			return storageError("AGENT_WRITE_FAILED", "The agent switches could not be encoded.", err)
		}
		value = string(encoded)
	}
	_, revision, found, err := r.GetSetting(ctx, agentEnabledKey)
	if err != nil {
		return err
	}
	if !found {
		revision = 0
	}
	return r.SetSetting(ctx, agentEnabledKey, value, revision)
}

// LoadDisabledAgents reads the persisted disabled set, with found=false when
// no user has touched the switches (every agent enabled is then the truth).
func (r *AgentRepository) LoadDisabledAgents(ctx context.Context) ([]string, bool, error) {
	value, _, found, err := r.GetSetting(ctx, agentEnabledKey)
	if err != nil || !found {
		return nil, false, err
	}
	var keys []string
	if err := json.Unmarshal([]byte(value), &keys); err != nil {
		// A value that does not parse is a DEFAULTS-with-alarm situation, not
		// a refusal: the user's agents come back enabled, and the corrupt
		// value is overwritten on the next save.
		return nil, false, nil
	}
	return keys, true, nil
}
