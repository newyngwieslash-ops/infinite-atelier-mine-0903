package desktop

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
)

// settings_binding.go is RP-09.2's user-facing settings surface: a narrow
// binding over the app_settings store (migration 000033) for the settings
// the plan's FR-180 work names as user-configurable THIS build.
//
// # The keys, and why they are the whole surface
//
// The binding exposes NAMED keys, not a generic get/set pair: a generic
// settings write would let any caller persist any key, and the code that
// READS a setting owns its vocabulary. The first key is the auto-backup
// configuration the scheduler reads at startup — the settings surface
// STATUS recorded as "until the settings surface lands".
//
// Values are validated here (the binding is the boundary) and re-validated
// by the scheduler's loader, so a hand-edited database cannot produce a
// negative interval or a retention that deletes everything.

// SettingsBinding is the narrow Wails surface for application settings.
type SettingsBinding struct {
	mu  sync.RWMutex
	ctx context.Context
	get func(ctx context.Context, key string) (string, int64, bool, error)
	set func(ctx context.Context, key, value string, expectedRevision int64) error
}

// AttachSettings supplies the startup context and the settings store
// accessors. Not a Wails method.
func AttachSettings(binding *SettingsBinding, ctx context.Context,
	get func(ctx context.Context, key string) (string, int64, bool, error),
	set func(ctx context.Context, key, value string, expectedRevision int64) error) {
	if binding == nil {
		return
	}
	binding.mu.Lock()
	binding.ctx = ctx
	binding.get = get
	binding.set = set
	binding.mu.Unlock()
}

// AutoBackupSettings is the auto-backup configuration a user edits.
type AutoBackupSettings struct {
	// EnabledHours: 0 disables the scheduler; otherwise the interval between
	// automatic ordinary backups, in hours. The scheduler's own default is
	// 24. Minimum 1 hour: a zero-or-negative value would either disable or
	// spin, and the UI offers hours, not seconds.
	IntervalHours int `json:"intervalHours"`
	// Retain is how many automatic backups to keep. Minimum 1: 0 would prune
	// every backup including the one just written.
	Retain int `json:"retain"`
}

// validate normalises the settings; the returned error names the rule.
func (s AutoBackupSettings) validate() (AutoBackupSettings, error) {
	interval := s.IntervalHours
	retain := s.Retain
	if interval < 0 {
		return AutoBackupSettings{}, apperror.New("SETTINGS_INVALID_INPUT", "invalid_input", false,
			"The backup interval cannot be negative. Use 0 to disable automatic backups.", nil)
	}
	if interval > 24*365 {
		return AutoBackupSettings{}, apperror.New("SETTINGS_INVALID_INPUT", "invalid_input", false,
			"The backup interval is longer than a year of hours.", nil)
	}
	if retain < 1 || retain > 100 {
		return AutoBackupSettings{}, apperror.New("SETTINGS_INVALID_INPUT", "invalid_input", false,
			"Retention must be between 1 and 100 automatic backups.", nil)
	}
	return AutoBackupSettings{IntervalHours: interval, Retain: retain}, nil
}

// GetAutoBackup reads the persisted auto-backup settings, falling back to the
// scheduler's own defaults when the user has never touched them. The
// revision rides back so a save from the UI conflicts instead of losing.
func (b *SettingsBinding) GetAutoBackup() (AutoBackupSettings, int64, error) {
	b.mu.RLock()
	ctx, get := b.ctx, b.get
	b.mu.RUnlock()
	if ctx == nil || get == nil {
		return AutoBackupSettings{}, 0, apperror.New("DESKTOP_BINDING_UNAVAILABLE", "internal", false,
			"The desktop service is not available.", nil)
	}
	value, revision, found, err := get(ctx, "autobackup")
	if err != nil {
		return AutoBackupSettings{}, 0, err
	}
	if !found {
		// The scheduler's documented defaults (T12): daily, keep 3.
		return AutoBackupSettings{IntervalHours: 24, Retain: 3}, 0, nil
	}
	var settings AutoBackupSettings
	if err := json.Unmarshal([]byte(value), &settings); err != nil {
		// A corrupt value falls back to defaults — the same
		// defaults-with-alarm rule the agent switches use.
		return AutoBackupSettings{IntervalHours: 24, Retain: 3}, revision, nil
	}
	return settings, revision, nil
}

// SetAutoBackup validates and stores the auto-backup settings under the
// revision guard. The scheduler re-reads at next start; a running scheduler's
// reload is the caller's documented behaviour (stop/start the app).
func (b *SettingsBinding) SetAutoBackup(settings AutoBackupSettings, expectedRevision int64) error {
	b.mu.RLock()
	ctx, set := b.ctx, b.set
	b.mu.RUnlock()
	if ctx == nil || set == nil {
		return apperror.New("DESKTOP_BINDING_UNAVAILABLE", "internal", false,
			"The desktop service is not available.", nil)
	}
	validated, err := settings.validate()
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(validated)
	if err != nil {
		return apperror.New("SETTINGS_WRITE_FAILED", "storage", false,
			"The settings could not be encoded.", err)
	}
	return set(ctx, "autobackup", string(encoded), expectedRevision)
}

// settingsReadPrefix is the key namespace the startup pass consults.
const settingsReadPrefix = "autobackup"

// parseAutoBackupValue is the scheduler-side loader: it reads the stored
// value with the same validation, so a hand-edited row cannot produce a
// hostile configuration.
func parseAutoBackupValue(value string, found bool) (time.Duration, int, bool) {
	if !found {
		return 24 * time.Hour, 3, true
	}
	var settings AutoBackupSettings
	if err := json.Unmarshal([]byte(value), &settings); err != nil {
		return 24 * time.Hour, 3, true
	}
	if settings.IntervalHours < 0 {
		// Negative is hostile, not "disabled": safe defaults.
		return 24 * time.Hour, 3, true
	}
	if settings.IntervalHours == 0 {
		// Disabled by the user.
		return 0, settings.Retain, false
	}
	if settings.IntervalHours > 24*365 || settings.Retain < 1 || settings.Retain > 100 {
		// Out of range: defaults, not the hostile values.
		return 24 * time.Hour, 3, true
	}
	return time.Duration(settings.IntervalHours) * time.Hour, settings.Retain, true
}

// ParseAutoBackupSettings is the startup-side loader the scheduler uses: it
// converts the stored (or missing, or hostile) setting into the interval and
// retention the scheduler takes, applying the safe fallbacks documented on
// GetAutoBackup.
func ParseAutoBackupSettings(read func() (value string, found bool)) (time.Duration, int) {
	value, found := read()
	interval, retain, _ := parseAutoBackupValue(value, found)
	return interval, retain
}
