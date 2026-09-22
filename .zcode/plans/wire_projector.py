import io

p = "drama_wiring.go"
s = io.open(p, encoding="utf-8").read()

# 1. The projector adapter, appended to the file.
s += '''
// canvasProjector adapts the projects service's canvas writer to the script service's port.
//
// IT EXISTS BECAUSE THE PORT HAD NO IMPLEMENTATION OUTSIDE A TEST.  `ProjectScriptVersion` was written in
// WP-08 with an interface and a refusal for a build without one, and `composeDrama` did not supply it — so
// every projection from the desktop refused with "this build cannot project onto a canvas", and the
// `CanvasProjectionCreated` event never fired.  The canary's own assertion used a double, which is why the
// gap survived it: a test double that satisfies the port cannot notice that production does not.
//
// The adapter is this thin on purpose. The projects service owns what a projection IS — the relation
// registry's validation, the create-or-move semantics, the node's box — and this only translates a
// projector call into that command.
type canvasProjector struct {
	projects *appprojects.Service
}

// ProjectEntity writes or re-labels one entity's canvas node.
func (p *canvasProjector) ProjectEntity(ctx context.Context, projectID, entityType, entityID, label string) (string, error) {
	if p == nil || p.projects == nil {
		return "", apperror.New("CANVAS_UNAVAILABLE", "storage", false,
			"This build cannot project onto a canvas.", nil)
	}
	node, err := p.projects.CreateCanvasProjection(ctx, appprojects.ProjectionRequest{
		ProjectID: projectID,
		EntityType: entityType,
		EntityID:   entityID,
		Title:      label,
	})
	if err != nil {
		return "", err
	}
	return node.ID, nil
}

// Compile-time proof that the adapter satisfies what the script service asks for.
var _ appscript.CanvasProjector = (*canvasProjector)(nil)
'''

# 2. Compose it and pass it to the script service.
old = '''	return &dramaWiring{
		story: storyService,
		script: appscript.NewService(appscript.Options{
			Repository: scriptRepository,
			Clock:      clock,
			IDs:        ids,
			Events:     eventService,
		}),'''
new = '''	// The projector the script service's projection command needs. Composed HERE rather than left
	// optional, because a build without one refuses every projection — a refusal that is correct and
	// that no user should meet in a build that ships a canvas.
	projector := &canvasProjector{projects: projectService}
	return &dramaWiring{
		story: storyService,
		script: appscript.NewService(appscript.Options{
			Repository: scriptRepository,
			Clock:      clock,
			IDs:        ids,
			Events:     eventService,
			Projector:  projector,
		}),'''
assert old in s, "options"
s = s.replace(old, new, 1)
io.open(p, "w", encoding="utf-8", newline="\n").write(s)
print("ok")
