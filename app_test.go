package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/health"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/desktop"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/apperror"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/appdirs"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/database"
	applicationlogging "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/logging"
)

func TestCloseClearsRuntimeContextBeforeShutdownCompletes(t *testing.T) {
	shutdownStarted := make(chan struct{})
	releaseShutdown := make(chan struct{})
	shutdownFinished := make(chan struct{})

	application := newApp(func(context.Context) error {
		close(shutdownStarted)
		<-releaseShutdown
		return errors.New("test shutdown failure")
	})
	application.startup(context.Background())

	go func() {
		application.close(context.Background())
		close(shutdownFinished)
	}()

	waitForSignal(t, shutdownStarted, "shutdown to start")
	application.mu.RLock()
	runtimeContext := application.ctx
	application.mu.RUnlock()
	close(releaseShutdown)
	waitForSignal(t, shutdownFinished, "shutdown to finish")

	if runtimeContext != nil {
		t.Fatal("runtime context remained visible while shutdown was blocked")
	}
}

func TestFocusExistingWindowDoesNotHoldAppMutex(t *testing.T) {
	focusFinished := make(chan struct{})
	var application *app
	application = newAppWithWindowFocus(nil, func(context.Context) {
		application.mu.Lock()
		application.mu.Unlock()
		close(focusFinished)
	})
	application.startup(context.Background())

	go application.focusExistingWindow()
	waitForSignal(t, focusFinished, "window focus without holding the app mutex")
}

func TestFocusExistingWindowWithoutRuntimeContextIsNoOp(t *testing.T) {
	focusCalled := make(chan struct{}, 1)
	application := newAppWithWindowFocus(nil, func(context.Context) {
		focusCalled <- struct{}{}
	})

	application.focusExistingWindow()

	select {
	case <-focusCalled:
		t.Fatal("window focus was called without a runtime context")
	default:
	}
}

func TestStartupOpensDatabaseAfterRuntimeContextIsStored(t *testing.T) {
	dirs, err := appdirs.Ensure(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	application := newApp(nil)
	application.dirs = dirs
	application.health = &desktop.HealthBinding{}
	application.startup(context.Background())
	if application.ctx == nil {
		t.Fatal("runtime context was not stored")
	}
	if application.db == nil || application.db.Mode() != database.ModeReady {
		t.Fatalf("database was not opened after lock-held startup: %+v", application.db)
	}
	if application.files == nil || application.filesStore == nil {
		t.Fatal("file store was not composed after startup")
	}
	snapshot := application.health.Get()
	if snapshot.SafeMode || snapshot.Database != "ready" || snapshot.DataDirectory != dirs.Root {
		t.Fatalf("health snapshot=%+v", snapshot)
	}
	if err := application.closeDatabase(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStartupLogsStructuredReadyState(t *testing.T) {
	dirs, err := appdirs.Ensure(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	application := newAppWithLogger(nil, slog.New(applicationlogging.NewRedactingJSONHandler(&output, nil)))
	application.dirs = dirs
	application.health = &desktop.HealthBinding{}

	application.startup(context.Background())
	defer func() {
		if err := application.closeDatabase(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	if !strings.Contains(output.String(), `"component":"desktop"`) || !strings.Contains(output.String(), `"database":"ready"`) {
		t.Fatalf("startup log is missing structured ready state: %s", output.String())
	}
}

func TestStartupDatabaseOpenFailureKeepsDiagnosableSafeHealth(t *testing.T) {
	dirs, err := appdirs.Ensure(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	application := newApp(nil)
	application.dirs = dirs
	application.dirs.Database = dirs.Root
	application.health = &desktop.HealthBinding{}

	application.startup(context.Background())

	snapshot := application.health.Get()
	if !snapshot.SafeMode || snapshot.Database != "safe" || snapshot.Version == "" || snapshot.DataDirectory != dirs.Root || snapshot.Diagnostic == "" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if strings.Contains(snapshot.Diagnostic, dirs.Root) {
		t.Fatalf("diagnostic leaked data path: %q", snapshot.Diagnostic)
	}
}

func TestStartupFileStoreFailureKeepsDiagnosableSafeHealth(t *testing.T) {
	dirs, err := appdirs.Ensure(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	application := newApp(nil)
	application.dirs = dirs
	application.dirs.Files = dirs.Database
	application.health = &desktop.HealthBinding{}

	application.startup(context.Background())
	defer func() {
		if err := application.closeDatabase(context.Background()); err != nil {
			t.Fatal(err)
		}
	}()

	snapshot := application.health.Get()
	if !snapshot.SafeMode || snapshot.Database != "safe" || snapshot.Version == "" || snapshot.DataDirectory != dirs.Root || snapshot.Diagnostic == "" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if strings.Contains(snapshot.Diagnostic, dirs.Root) {
		t.Fatalf("diagnostic leaked data path: %q", snapshot.Diagnostic)
	}
}

func TestDomReadySkipsEventWhenEnvelopeCreationFails(t *testing.T) {
	var emitted bool
	application := newApp(nil)
	application.health = &desktop.HealthBinding{}
	desktop.Attach(application.health, context.Background(), health.New("1.0.0", "/data", nil, true, "diag"))
	application.emit = func(context.Context, string, ...interface{}) { emitted = true }
	application.newEnvelope = func(string, any) (desktop.Envelope, error) {
		return desktop.Envelope{}, errors.New("entropy unavailable")
	}

	application.domReady(context.Background())

	if emitted {
		t.Fatal("event emitted after envelope creation failed")
	}
}

func TestDomReadyEmitsHealthChangedEnvelope(t *testing.T) {
	var eventName string
	var payload desktop.Envelope
	application := newApp(nil)
	application.health = &desktop.HealthBinding{}
	desktop.Attach(application.health, context.Background(), health.New("1.0.0", "/data", nil, true, "diag"))
	application.emit = func(_ context.Context, name string, values ...interface{}) {
		eventName = name
		if len(values) != 1 {
			t.Fatalf("payload count = %d", len(values))
		}
		var ok bool
		payload, ok = values[0].(desktop.Envelope)
		if !ok {
			t.Fatalf("payload type = %T", values[0])
		}
	}

	application.domReady(context.Background())

	if eventName != desktop.CoreEventName || payload.Type != "health.changed" || payload.Version != 1 {
		t.Fatalf("event = %q payload = %+v", eventName, payload)
	}
}

func waitForSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()

	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

type orderedWriter struct {
	mu     sync.Mutex
	buffer bytes.Buffer
	events *[]string
	closed bool
}

func (w *orderedWriter) Write(value []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, errors.New("write after close")
	}
	*w.events = append(*w.events, "log")
	return w.buffer.Write(value)
}

func (w *orderedWriter) close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	*w.events = append(*w.events, "logger-close")
	w.closed = true
	return nil
}

func (w *orderedWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.String()
}

func TestShutdownSequenceLogsResourceFailureBeforeClosingLogger(t *testing.T) {
	events := make([]string, 0, 3)
	writer := &orderedWriter{events: &events}
	logger := slog.New(applicationlogging.NewRedactingJSONHandler(writer, nil))
	reports := make([]*apperror.Error, 0)
	shutdown := newShutdownSequence(
		func(ctx context.Context) error {
			if ctx == nil {
				t.Fatal("resource shutdown received a nil context")
			}
			events = append(events, "resource")
			return errors.New(`close C:\Users\Alice\private.db: access denied`)
		},
		logger,
		writer.close,
		func(err *apperror.Error) { reports = append(reports, err) },
	)

	if err := shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(events, ","), "resource,log,logger-close"; got != want {
		t.Fatalf("shutdown order = %q, want %q", got, want)
	}
	if got := writer.String(); strings.Contains(got, "private.db") || !strings.Contains(got, "APP_RESOURCE_SHUTDOWN_FAILED") {
		t.Fatalf("resource failure log was unsafe or missing: %s", got)
	}
	if len(reports) != 0 {
		t.Fatalf("resource failure unexpectedly used stderr reporter: %#v", reports)
	}

	if err := shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(events, ","); got != "resource,log,logger-close" {
		t.Fatalf("shutdown sequence was not idempotent: %q", got)
	}
}

func TestShutdownSequenceReportsLoggerCloseFailureToIndependentReporter(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(applicationlogging.NewRedactingJSONHandler(&output, nil))
	var reported *apperror.Error
	shutdown := newShutdownSequence(
		nil,
		logger,
		func() error { return errors.New(`close C:\Users\Alice\app.jsonl: access denied`) },
		func(err *apperror.Error) { reported = err },
	)

	if err := shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if reported == nil || reported.Code != "LOG_CLOSE_FAILED" {
		t.Fatalf("close error was not independently reported: %#v", reported)
	}
	if strings.Contains(reported.Error(), "app.jsonl") || reported.Diagnostic == "" {
		t.Fatalf("close report was unsafe or undiagnosable: %#v", reported)
	}
}

func TestFallbackShutdownUsesBoundedCancelableContext(t *testing.T) {
	var observed context.Context
	called := false
	runFallbackShutdown(
		func(ctx context.Context) error {
			called = true
			observed = ctx
			if ctx == nil {
				t.Fatal("fallback shutdown received nil context")
			}
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("fallback shutdown context has no deadline")
			}
			remaining := time.Until(deadline)
			if remaining <= 0 || remaining > fallbackShutdownTimeout {
				t.Fatalf("fallback deadline remaining = %s", remaining)
			}
			return nil
		},
		func(*apperror.Error) { t.Fatal("unexpected fallback error report") },
	)
	if !called || observed == nil {
		t.Fatal("fallback shutdown was not called")
	}
	select {
	case <-observed.Done():
	default:
		t.Fatal("fallback shutdown context was not cancelled after cleanup")
	}
}

var _ io.Writer = (*orderedWriter)(nil)
