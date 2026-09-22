import io
path = "memory_wiring_wp10_test.go"
text = io.open(path, encoding="utf-8", newline="").read()
old = '''		t.Fatalf("opening the file store: %v", err)
	}'''
new = '''		t.Fatalf("opening the file store: %v", err)
	}
	// The files SERVICE, as app.go builds it: composeAgents' tool table needs it, and the store
	// alone is not it.
	files := appfiles.NewService(store, database.NewFileRepository(handle.SQL()))'''
assert old in text, "store anchor"
text = text.replace(old, new, 1)

old = '''		Handle: handle, Drama: drama, Providers: providers.registry, Files: store,'''
new = '''		Handle: handle, Drama: drama, Providers: providers.registry, Files: files,'''
assert old in text, "deps anchor"
text = text.replace(old, new, 1)

old = '''	appmemory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/memory"'''
new = '''	appfiles "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/files"
	appmemory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/memory"'''
assert old in text, "import anchor"
text = text.replace(old, new, 1)
io.open(path, "w", encoding="utf-8", newline="").write(text)
print("OK")
