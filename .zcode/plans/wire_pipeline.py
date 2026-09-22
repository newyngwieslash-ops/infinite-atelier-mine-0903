import io

# 1. Compose the pipeline in drama_wiring.go.
p = "drama_wiring.go"
s = io.open(p, encoding="utf-8").read()

s = s.replace('''	importing  *appimporting.Service
	extraction *appextraction.Service
''', '''	importing  *appimporting.Service
	extraction *appextraction.Service
	// pipeline drives the three script stages. WP-08's scope includes the stage orchestration, and until
	// this field existed the package was reachable only from tests: the composition root did not build it,
	// so no user command could run a script stage.
	pipeline *appscriptpipeline.Service
''', 1)

s = s.replace('''	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"''',
'''	appscript "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/script"
	appscriptpipeline "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/scriptpipeline"''', 1)

# The runtime and engine the pipeline needs are composed by agent_wiring.go, not here, so the pipeline
# arrives as a parameter like the canvas writer does.
old = '''func composeDrama(handle *database.Handle, store *filestore.Store, canvas *appprojects.Service) *dramaWiring {
	if handle == nil || handle.SQL() == nil {
		return nil
	}'''
new = '''func composeDrama(handle *database.Handle, store *filestore.Store, canvas *appprojects.Service, agents *agentStack) *dramaWiring {
	if handle == nil || handle.SQL() == nil {
		return nil
	}'''
assert old in s, "signature"
s = s.replace(old, new, 1)

old = '''	return &dramaWiring{
		story: storyService,'''
new = '''	// The pipeline, composed over THIS stack's services and the agent stack's engine and runtime. It is
	// assembled here rather than in its own wiring file because every dependency it drives is already in
	// scope: composing it elsewhere would mean passing five services across a module boundary to reach a
	// package that needs all of them.
	var pipeline *appscriptpipeline.Service
	if agents != nil && agents.engine != nil && agents.runtime != nil && agents.assembly != nil {
		pipeline = appscriptpipeline.New(appscriptpipeline.Options{
			Engine:   agents.engine,
			Runtime:  agents.runtime,
			Script:   scriptService,
			Workflow: workflowService,
			Assembly: agents.assembly,
			Runs:     agents.runs,
		})
	}
	return &dramaWiring{
		story:    storyService,
		pipeline: pipeline,'''
assert old in s, "return"
s = s.replace(old, new, 1)
io.open(p, "w", encoding="utf-8", newline="\n").write(s)
print("drama ok")
