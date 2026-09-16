package database

import (
	"context"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/provider"
)

func openProviderRepo(t *testing.T) *ProviderRepository {
	t.Helper()
	handle, err := open(context.Background(), filepath.Join(t.TempDir(), "app.db"), filepath.Join(t.TempDir(), "snapshots"), wp03Migrations(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := handle.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if handle.Mode() != ModeReady {
		t.Fatalf("database not ready: %v", handle.Err())
	}
	return NewProviderRepository(handle.SQL())
}

func TestProviderRepositoryConfigRoundTrip(t *testing.T) {
	repo := openProviderRepo(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	config := provider.Config{
		ID:            "prov-1",
		Kind:          provider.KindOpenAICompatible,
		DisplayName:   "Main",
		BaseURL:       "https://api.example.com",
		SecretRef:     provider.SecretRefValue("prov-1"),
		LocalApproved: false,
		Enabled:       true,
		Revision:      1,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := repo.SaveConfig(ctx, config); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got, err := repo.GetConfig(ctx, "prov-1")
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if got.ID != config.ID || got.Kind != config.Kind || got.DisplayName != config.DisplayName ||
		got.BaseURL != config.BaseURL || got.SecretRef != config.SecretRef || !got.Enabled || got.Revision != 1 {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	list, err := repo.ListConfigs(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListConfigs: %v %d", err, len(list))
	}
}

func TestProviderRepositoryGetMissingIsConfigurationError(t *testing.T) {
	repo := openProviderRepo(t)
	_, err := repo.GetConfig(context.Background(), "nope")
	if err == nil {
		t.Fatal("missing config returned nil error")
	}
	providerErr, ok := provider.AsProviderError(err)
	if !ok || providerErr.Category != provider.CategoryConfiguration {
		t.Fatalf("expected configuration error, got %v", err)
	}
}

func TestProviderRepositoryUpdateAndDelete(t *testing.T) {
	repo := openProviderRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()
	first := provider.Config{ID: "prov-1", Kind: provider.KindOpenAICompatible, DisplayName: "v1", BaseURL: "https://a.example.com", SecretRef: provider.SecretRefValue("prov-1"), Revision: 1, CreatedAt: now, UpdatedAt: now}
	if err := repo.SaveConfig(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.DisplayName = "v2"
	second.Revision = 2
	if err := repo.SaveConfig(ctx, second); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetConfig(ctx, "prov-1")
	if err != nil || got.DisplayName != "v2" || got.Revision != 2 {
		t.Fatalf("update not applied: %+v %v", got, err)
	}
	if err := repo.DeleteConfig(ctx, "prov-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetConfig(ctx, "prov-1"); err == nil {
		t.Fatal("deleted config still readable")
	}
}

func TestProviderRepositoryRequestRecordPersists(t *testing.T) {
	repo := openProviderRepo(t)
	ctx := context.Background()
	record := provider.RequestRecord{
		ID:            "req-1",
		ProviderID:    "prov-1",
		Capability:    provider.CapabilityText,
		Model:         "gpt-test",
		Status:        provider.StatusSucceeded,
		HTTPStatus:    200,
		LatencyMS:     42,
		RequestID:     "resp-abc",
		InputUnits:    120,
		OutputUnits:   34,
		EstimatedCost: "0.0012",
		CreatedAt:     time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC),
	}
	if err := repo.SaveRequestRecord(ctx, record); err != nil {
		t.Fatalf("SaveRequestRecord: %v", err)
	}
	// Success is verified by database read-back (AGENTS §10), including the
	// audit unit/cost fields FR-140 requires.
	var (
		model, requestID, estimatedCost     string
		httpStatus, inputUnits, outputUnits int64
	)
	err := repo.db.QueryRowContext(ctx, `SELECT model, request_id, estimated_cost, http_status, input_units, output_units
		FROM provider_requests WHERE id = ?`, "req-1").
		Scan(&model, &requestID, &estimatedCost, &httpStatus, &inputUnits, &outputUnits)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if model != "gpt-test" || requestID != "resp-abc" || estimatedCost != "0.0012" {
		t.Fatalf("unexpected audit row: %s %s %s", model, requestID, estimatedCost)
	}
	if httpStatus != 200 || inputUnits != 120 || outputUnits != 34 {
		t.Fatalf("unit fields = %d/%d/%d", httpStatus, inputUnits, outputUnits)
	}
}

func TestProviderRepositoryUpdateSecretStatus(t *testing.T) {
	repo := openProviderRepo(t)
	ctx := context.Background()
	if err := repo.UpdateSecretStatus(ctx, "prov-9", "configured", "****abcd"); err != nil {
		t.Fatalf("UpdateSecretStatus: %v", err)
	}
	states, err := repo.ListHealth(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, state := range states {
		if state.ProviderID == "prov-9" {
			found = true
			if !state.Healthy || state.Detail != "configured" {
				t.Fatalf("unexpected state: %+v", state)
			}
		}
	}
	if !found {
		t.Fatal("secret status row not listed")
	}
}

func TestProviderRepositoryNilDatabaseFailsClosed(t *testing.T) {
	var repo *ProviderRepository
	if err := repo.SaveConfig(context.Background(), provider.Config{}); err == nil {
		t.Fatal("nil repo SaveConfig should fail")
	}
	if _, err := repo.ListConfigs(context.Background()); err == nil {
		t.Fatal("nil repo ListConfigs should fail")
	}
	if _, err := repo.GetConfig(context.Background(), "x"); err == nil {
		t.Fatal("nil repo GetConfig should fail")
	}
	if err := repo.DeleteConfig(context.Background(), "x"); err == nil {
		t.Fatal("nil repo DeleteConfig should fail")
	}
	if err := repo.SaveRequestRecord(context.Background(), provider.RequestRecord{}); err == nil {
		t.Fatal("nil repo SaveRequestRecord should fail")
	}
	if _, err := repo.ListHealth(context.Background()); err == nil {
		t.Fatal("nil repo ListHealth should fail")
	}
	if err := repo.UpdateSecretStatus(context.Background(), "x", "configured", ""); err == nil {
		t.Fatal("nil repo UpdateSecretStatus should fail")
	}
}

// Ensure the fstest import stays used when wp02Migrations helper is shared.
var _ = fstest.MapFS{}

// TestProviderRepositoryRequestRecordLinksToJob proves the audit row records
// which job caused the call, so a provider request can be traced back through
// the job that issued it (FR-140 traceability).
func TestProviderRepositoryRequestRecordLinksToJob(t *testing.T) {
	repo := openProviderRepo(t)
	ctx := context.Background()
	record := provider.RequestRecord{
		ID:         "req-link",
		JobID:      "job-42",
		ProviderID: "prov-1",
		Capability: provider.CapabilityImage,
		Model:      "gpt-image-1",
		Status:     provider.StatusSucceeded,
		HTTPStatus: 200,
		LatencyMS:  10,
		CreatedAt:  time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC),
	}
	if err := repo.SaveRequestRecord(ctx, record); err != nil {
		t.Fatalf("SaveRequestRecord: %v", err)
	}
	var jobID string
	if err := repo.db.QueryRowContext(ctx, `SELECT job_id FROM provider_requests WHERE id = 'req-link'`).Scan(&jobID); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if jobID != "job-42" {
		t.Fatalf("job_id = %q, want job-42", jobID)
	}
}

// TestProviderRepositoryRequestRecordWithoutJob proves a job-less call (health
// probe, interactive text) stores an empty link rather than a bogus one.
func TestProviderRepositoryRequestRecordWithoutJob(t *testing.T) {
	repo := openProviderRepo(t)
	ctx := context.Background()
	record := provider.RequestRecord{
		ID:         "req-nojob",
		ProviderID: "prov-1",
		Capability: provider.CapabilityText,
		Model:      "gpt-test",
		Status:     provider.StatusSucceeded,
		LatencyMS:  5,
		CreatedAt:  time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC),
	}
	if err := repo.SaveRequestRecord(ctx, record); err != nil {
		t.Fatal(err)
	}
	var jobID string
	if err := repo.db.QueryRowContext(ctx, `SELECT job_id FROM provider_requests WHERE id = 'req-nojob'`).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	if jobID != "" {
		t.Fatalf("job_id = %q, want empty", jobID)
	}
}
