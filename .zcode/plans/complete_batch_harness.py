import io

p = 'batch_wiring_test.go'
s = io.open(p, encoding='utf-8').read()

old = '''	jobService := appjobs.NewService(appjobs.Options{
		Repository: database.NewJobRepository(handle.SQL()),
		Clock:      appprojectsClock{},
		IDs:        appjobsPrefixIDs{inner: id.NewGenerator()},
		Publisher:  discardPublisher{},
	})'''
new = '''	// THE JOB STACK IS COMPLETE HERE rather than a bare service, because these tests are
	// about a batch's jobs EXECUTING: a service with no runner accepts submissions and
	// settles nothing, so every collection assertion would be about a job that never ran.
	//
	// The registry is the harness's own and carries the deterministic image adapter. The
	// production composition deliberately does NOT register one — `IsUserConfigurableKind`
	// refuses to persist `mock_image`, so a config could never select it and the arm would be
	// unreachable — and a harness that wants the mock must therefore build its own, exactly
	// as the mock-text adapter has always required.
	registry := infraproviders.NewRegistry(database.NewProviderRepository(handle.SQL()), refusingSecrets{}, database.NewProviderRepository(handle.SQL()))
	images := infraproviders.NewMockImageAdapter()
	registry.WithMockImageAdapter(images)
	resultStore := infrajobs.NewResultStore(store, database.NewFileObjectRepository(handle.SQL()), database.NewFileReferenceRepository(handle.SQL()))
	runner := infrajobs.NewRunner(registry, resultStore, nil, maxJobResultBytes)
	jobService := appjobs.NewService(appjobs.Options{
		Repository: database.NewJobRepository(handle.SQL()),
		Clock:      appjobsClock{},
		IDs:        appjobsPrefixIDs{inner: id.NewGenerator()},
		Publisher:  discardPublisher{},
		Runner:     runner,
		Policy:     job.DefaultRetryPolicy(),
	})'''
assert old in s, "job service anchor"
s = s.replace(old, new, 1)

# The harness carries the registry and the mock, and a provider config so ids resolve.
old_return = '''	return &batchHarness{
		drama: stack, production: production, jobs: jobService, db: handle.SQL(),
		projectID: seedWiringProject(t, canvasWriter),
	}
}'''
new_return = '''	// A provider config carrying the mock kind is INSERTED DIRECTLY, because the application
	// layer refuses to persist one — that refusal is the guardrail, and a test that could
	// register it through the service would be testing a build that cannot exist. Writing the
	// row is how a harness reaches an adapter a real build reaches through a configured
	// provider.
	const imageProviderID = "canary-image-provider"
	if _, err := handle.SQL().ExecContext(ctx,
		`INSERT INTO provider_configs (id, kind, display_name, base_url, secret_ref, local_approved, enabled, revision, created_at, updated_at)
		 VALUES (?, 'mock_image', 'Canary image', 'https://mock.invalid', '', 1, 1, 1, ?, ?)`,
		imageProviderID, canaryStamp, canaryStamp); err != nil {
		t.Fatalf("seeding the image provider config: %v", err)
	}
	return &batchHarness{
		drama: stack, production: production, jobs: jobService,
		db: handle.SQL(), images: images, imageProviderID: imageProviderID,
		projectID: seedWiringProject(t, canvasWriter),
	}
}'''
assert old_return in s, "return anchor"
s = s.replace(old_return, new_return, 1)

# The struct gains the two fields.
s = s.replace('''	db       *sql.DB
	projectID string''','''	db       *sql.DB
	projectID string
	// images is the deterministic adapter this harness registered, so a test can assert what
	// the batch asked a provider for.
	images *infraproviders.MockImageAdapter
	// imageProviderID is the config row whose kind resolves to that adapter.
	imageProviderID string''')

# The imports.
s = s.replace('''	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"''','''	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	infrajobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/jobs"
	infraproviders "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providers"''')
if '"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"' not in s:
    s = s.replace('''	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"''','''	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/job"''')

io.open(p, 'w', encoding='utf-8').write(s)
print("harness completes the job stack")
