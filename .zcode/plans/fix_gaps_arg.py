import io
path = "agent_wiring.go"
text = io.open(path, encoding="utf-8", newline="").read()
old = """		Memory:     memoryService,
		Assets:     deps.Drama.assets,
		Projects:   projectService,"""
new = """		Memory:     memoryService,
		Assets:     deps.Drama.assets,
		// THE GAP SERVICE, which the tool table has required since WP-09 added the two gap tools
		// and which this call never passed. `Deps.Available` checks it, so `agenttools.Build`
		// REFUSED, `composeAgents` returned nil for that reason, and the whole agent stack was
		// unreachable in every composed build: no stage could run, no script pipeline existed, and
		// the application reported "the agent layer is unavailable" while every service below it was
		// present. The wiring test added for WP-10's memory port found it, because that test is the
		// first thing in this repository that composes the agent stack over a real database and
		// asserts the result is non-nil.
		//
		// It is the third time a required dependency was missing from exactly one call site: the
		// shape is invisible to the compiler because `Deps` is a struct of optionals that
		// `Available` validates at runtime.
		Gaps:     deps.Drama.gaps,
		Projects: projectService,"""
assert old in text, "gaps anchor"
text = text.replace(old, new, 1)
io.open(path, "w", encoding="utf-8", newline="").write(text)
print("OK")
