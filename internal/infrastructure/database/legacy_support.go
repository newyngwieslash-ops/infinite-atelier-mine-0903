package database

import (
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/platform/id"
)

// defaultIDs is the identifier source this package uses when a repository has
// to mint a row identifier of its own (an import record, a mapping row).
//
// Entities a user owns get their identifier from the application layer per
// ADR-0005; these bookkeeping rows are created by the store as part of one
// atomic write and have no separate command behind them, so the store mints
// them.
var defaultIDs = id.NewGenerator()

// timeNow is the package's wall clock, indirected so a test can pin it.
var timeNow = func() time.Time { return time.Now().UTC() }
