package main

import (
	"context"
	"database/sql"
	"time"

	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/desktop"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/database"
	infrajobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/jobs"
	infraproviders "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providers"
)

// jobWiring holds the composed job stack. Like providerWiring it exists so
// app.go stays a thin lifecycle owner rather than a dependency container.
type jobWiring struct {
	binding *desktop.JobsBinding
	service *appjobs.Service
	// resultReader serves stored artifacts to the canvas by content hash.
	resultReader desktop.ResultReader
}

// Default limits for job execution in WP-03.
const (
	// maxJobResultBytes caps a downloaded media result. A 512 MiB ceiling
	// accommodates a high-resolution video while bounding disk exposure.
	maxJobResultBytes = 512 << 20
	// jobWorkerCount keeps concurrent provider calls modest: a local desktop
	// app rarely benefits from more in-flight generations, and a smaller pool
	// keeps per-provider rate limits meaningful.
	jobWorkerCount = 3
	// jobPollInterval is how often the scheduler looks for runnable work.
	jobPollInterval = 250 * time.Millisecond
	// jobRemotePollInterval is the wait between two polls of one remote job.
	// The scheduler's own 250 ms pass must not decide how often a provider is
	// asked about a long-running remote job; five seconds keeps a video poll
	// cadence reasonable while staying responsive.
	jobRemotePollInterval = 5 * time.Second
)

// composeJobs builds the job manager over a writable database and the provider
// registry, then runs the startup recovery scan. It returns nil when the
// database is unavailable so the binding stays unattached and fails closed.
//
// The recovery scan runs before the scheduler starts: a job left mid-flight by
// a previous process must be reconciled before new work is dispatched.
func composeJobs(db *sql.DB, registry *infraproviders.Registry, files *infrajobs.ResultStore, downloads infrajobs.DownloaderPort, publisher appjobs.Publisher, resultReader desktop.ResultReader) *jobWiring {
	if db == nil {
		return nil
	}
	repository := database.NewJobRepository(db)
	runner := infrajobs.NewRunner(registry, files, downloads, maxJobResultBytes)
	service := appjobs.NewService(appjobs.Options{
		Repository:         repository,
		Clock:              appjobs.NewClockFunc(func() time.Time { return time.Now().UTC() }),
		IDs:                newIDGenerator(),
		Publisher:          publisher,
		Runner:             runner,
		Policy:             job.DefaultRetryPolicy(),
		RemotePollInterval: jobRemotePollInterval,
	}).WithRemoteCanceller(&remoteCanceller{registry: registry})
	return &jobWiring{binding: &desktop.JobsBinding{}, service: service, resultReader: resultReader}
}

// remoteCanceller stops provider-side video work when the user cancels a job.
// Only providers that expose an async cancel contract can be stopped; for
// everything else the error reports "unsupported", which the service records
// as an unconfirmed remote cancellation rather than a success.
type remoteCanceller struct {
	registry *infraproviders.Registry
}

// CancelRemote implements the application RemoteCanceller port.
func (c *remoteCanceller) CancelRemote(ctx context.Context, record job.Job) error {
	if c == nil || c.registry == nil || record.RemoteJobID == "" {
		return nil
	}
	adapter, err := c.registry.VideoPortFor(ctx, record.ProviderConfigID)
	if err != nil {
		// The provider has no cancellable remote contract.
		return err
	}
	return adapter.Cancel(ctx, appjobs.RemoteJob{ProviderID: record.ProviderConfigID, ID: record.RemoteJobID})
}

// start performs the startup recovery scan and launches the scheduler.
func (w *jobWiring) start(ctx context.Context) {
	if w == nil || w.service == nil {
		return
	}
	// Recovery must complete before workers run so a job cannot be executed
	// while its state is still unknown.
	if _, err := w.service.Recover(ctx); err != nil {
		// A recovery failure is reported through the health/status surface; the
		// scheduler still starts so new work is not blocked indefinitely.
		logJobWarning("job recovery did not complete", err)
	}
	w.service.Start(ctx, jobWorkerCount, jobPollInterval)
}

// attach publishes the startup context and service into the binding.
func (w *jobWiring) attach(ctx context.Context) {
	if w == nil || w.binding == nil {
		return
	}
	desktop.AttachJobs(w.binding, ctx, w.service)
	desktop.AttachJobResultReader(w.binding, w.resultReader)
}

// shutdown stops the scheduler before the database closes. In-flight attempts
// observe cancellation; the process does not wait indefinitely for a remote
// operation (ARCHITECTURE §6.2).
func (w *jobWiring) shutdown() {
	if w == nil || w.service == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := w.service.Stop(ctx); err != nil {
		logJobWarning("job scheduler did not stop cleanly", err)
	}
}
