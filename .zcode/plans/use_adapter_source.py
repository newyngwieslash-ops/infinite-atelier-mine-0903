import io

p = 'batch_wiring_test.go'
s = io.open(p, encoding='utf-8').read()

old = '''	// THE JOB STACK IS COMPLETE HERE rather than a bare service, because these tests are
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
	resultStore := infrajobs.NewResultStore(store, database.NewFileRepository(handle.SQL()), database.NewFileReferenceRepository(handle.SQL()))
	runner := infrajobs.NewRunner(registry, resultStore, nil, maxJobResultBytes)
	jobService := appjobs.NewService(appjobs.Options{
		Repository: database.NewJobRepository(handle.SQL()),
		Clock:      appjobs.NewClockFunc(func() time.Time { return time.Now().UTC() }),
		IDs:        appjobsPrefixIDs{inner: id.NewGenerator()},
		Publisher:  discardPublisher{},
		Runner:     runner,
		Policy:     job.DefaultRetryPolicy(),
	})'''
new = '''	// THE JOB STACK IS COMPLETE HERE rather than a bare service, because these tests are about
	// a batch's jobs EXECUTING: a service with no runner accepts submissions and settles
	// nothing, so every collection assertion would be about a job that never ran.
	//
	// The image adapter is supplied DIRECTLY to the runner, through the `AdapterSource` port
	// the runner declares for exactly this. Going through the registry could not work: the
	// registry resolves a provider by reading its CONFIG and dispatching on kind, so the mock
	// needs a `provider_configs` row carrying `mock_image` — and the schema's CHECK does not
	// admit that kind, which is the guardrail that stops a real configuration selecting it.
	// A harness that wanted one would have to change the schema, and the guardrail is worth
	// more than the fixture's convenience.
	//
	// What matters for these tests is that an image job RUNS and produces a file the
	// collection can turn into a candidate; which adapter answered is not their subject.
	images := infraproviders.NewMockImageAdapter()
	resultStore := infrajobs.NewResultStore(store, database.NewFileRepository(handle.SQL()), database.NewFileReferenceRepository(handle.SQL()))
	runner := infrajobs.NewRunner(batchAdapterSource{images: images}, resultStore, nil, maxJobResultBytes)
	jobService := appjobs.NewService(appjobs.Options{
		Repository: database.NewJobRepository(handle.SQL()),
		Clock:      appjobs.NewClockFunc(func() time.Time { return time.Now().UTC() }),
		IDs:        appjobsPrefixIDs{inner: id.NewGenerator()},
		Publisher:  discardPublisher{},
		Runner:     runner,
		Policy:     job.DefaultRetryPolicy(),
	})'''
assert old in s, "job stack anchor"
s = s.replace(old, new, 1)

old_return = '''	// A provider config carrying the mock kind is INSERTED DIRECTLY, because the application
	// layer refuses to persist one — that refusal is the guardrail, and a test that could
	// register it through the service would be testing a build that cannot exist. Writing the
	// row is how a harness reaches an adapter a real build reaches through a configured
	// provider.
	const imageProviderID = "canary-image-provider"
	if _, err := handle.SQL().ExecContext(ctx,
		`INSERT INTO provider_configs (id, kind, display_name, base_url, secret_ref, local_approved, enabled, revision, created_at, updated_at)
		 VALUES (?, 'mock_image', 'Canary image', 'https://mock.invalid', '', 1, 1, 1, ?, ?)`,
		imageProviderID, newTestStamp(), newTestStamp()); err != nil {
		t.Fatalf("seeding the image provider config: %v", err)
	}
	return &batchHarness{'''
new_return = '''	// The provider id the batch is asked to submit to. It needs no config row, because the
	// adapter source below answers for every id: what a real build resolves through a
	// configured provider is resolved here directly.
	const imageProviderID = "canary-image-provider"
	return &batchHarness{'''
assert old_return in s, "return anchor"
s = s.replace(old_return, new_return, 1)

# The adapter source, replacing the secrets double that is no longer needed.
old_secrets = s[s.index('// refusingSecrets is the SecretResolver this harness'):]
new_secrets = '''// batchAdapterSource answers the runner's adapter lookup with the deterministic image mock.
//
// It is the seam `infrastructure/jobs` declares for a caller that supplies its own adapters —
// the same port the registry implements in a real build — and it exists here because the
// registry CANNOT answer for the mock: the schema's kind CHECK does not admit `mock_image`,
// which is the guardrail that keeps a real configuration from selecting it.
type batchAdapterSource struct {
	images *infraproviders.MockImageAdapter
}

func (s batchAdapterSource) ImagePortFor(context.Context, string) (appjobs.ImagePort, error) {
	if s.images == nil {
		return nil, provider.NewUnsupportedError()
	}
	return s.images, nil
}

// newTestStamp is the timestamp this harness stamps its seeded rows with.
func newTestStamp() string { return time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC).Format(time.RFC3339) }
'''
s = s.replace(old_secrets, new_secrets)
s = s.replace('''
// newTestStamp is the timestamp this harness stamps its seeded rows with.
func newTestStamp() string { return time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC).Format(time.RFC3339) }
''', '\n', 1)

io.open(p, 'w', encoding='utf-8').write(s)
print("harness uses the adapter source")
