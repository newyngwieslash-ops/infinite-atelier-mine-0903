package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	agentassembly "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentassembly"
	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	agenttools "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agenttools"
	appextraction "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/extraction"
	appfiles "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/files"
	appjobs "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/jobs"
	appmemory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/memory"
	appproductionpipeline "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/productionpipeline"
	appprojects "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/projects"
	appproviders "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/providers"
	appscriptpipeline "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/scriptpipeline"
	appvalidation "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/validation"
	appworkflow "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/workflow"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/desktop"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	extractiondomain "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/extraction"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/versioning"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/workflow"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/database"
	infraproviders "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providers"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/platform/id"
)

// agent_wiring.go composes the WP-07 stack: the tool table, the pack assembly, the model
// bridge, the run store and the engine.
//
// It is a separate file from drama_wiring.go because it depends on that stack rather than
// extending it. Every tool's handler calls one of the drama services, so this runs AFTER
// composeDrama and takes its services as its dependencies: the order is the dependency
// direction AGENTS section 7.2 requires, made visible in one place.
//
// Three questions the specification leaves open are answered here, and each is recorded
// in ADR-0011 rather than decided silently:
//
//   - WHICH MODEL A RUN USES. Section 13 gives a per-layer policy object and this build
//     has no settings UI for one yet, so the policy resolves to the project's first
//     enabled provider, ordered by id. A build with none gets an unavailable bridge and
//     every run refuses with a reason rather than reaching a default.
//   - WHICH AGENT RUNS A STAGE. Section 3's registry maps a stage to its execution agent
//     by the key's last segment, and the engine asks the registry rather than a model.
//   - WHETHER THE DETERMINISTIC MOCK CAN BE REACHED. It cannot: KindMockText is refused by
//     the application layer and by the database CHECK, so no configuration a user can
//     persist resolves to it. The canary reaches it through the registry directly, which
//     is what a test does and a user cannot.

// agentWiring holds the composed agent stack.
type agentWiring struct {
	runtime  *agentruntime.Runtime
	engine   *agentruntime.Engine
	assembly *agentassembly.Assembly
	tools    *agentruntime.Tools
	// projectService is composed here rather than shared with the project stack because
	// its only use from this file is a tool's rule read, and the project stack's own
	// service is bound to bindings this layer does not touch.
	projectService *appprojects.Service
	// memoryService is the basic Memory Port of scope item 15.
	memoryService *appmemory.Service
	// pipeline drives the three script stages. It lives in this stack rather than the drama one
	// because it needs the runtime and the ENGINE, which are composed here, and the drama stack's
	// services, which `agentDeps` already carries. Composing it in the drama stack would mean passing
	// the runtime and the engine across a module boundary to reach a package that needs both.
	pipeline   *appscriptpipeline.Service
	production *appproductionpipeline.Service
}

// agentDeps are what composeAgents needs from the other composition roots.
type agentDeps struct {
	Handle *database.Handle
	Drama  *dramaWiring
	// Providers resolves a configured provider to its text adapter.
	Providers *infraproviders.Registry
	// Files stores the packs' documents.
	Files *appfiles.Service
	// Jobs submits the generation work the production pipeline's image batch drives. It is
	// optional: a build without it still runs the five agent stages, and the batch refuses
	// with a reason rather than reporting a submission that never happened.
	Jobs *appjobs.Service
	// Media is the two reads the `final_episode` agent uses. It is optional for the same
	// reason `agenttools.Deps.Media` is, and it is a field here rather than something this
	// function composes because the media stack needs `deps.Drama.script` — a service from
	// the very stack this function is called inside — so the composition root builds it and
	// passes the reader down.
	Media agenttools.MediaReader
}

// composeAgents builds the agent stack over a writable database.
//
// It returns nil when the database is unavailable (safe mode), when the drama stack was
// not composed, or when a tool table will not build — so every agent method fails closed
// rather than running with services its tools cannot call.
func composeAgents(deps agentDeps) *agentWiring {
	if deps.Handle == nil || deps.Handle.SQL() == nil || deps.Drama == nil || deps.Files == nil {
		return nil
	}
	connection := deps.Handle.SQL()
	clock := appprojectsClock{}
	ids := id.NewGenerator()

	// The two services the tool table needs that the drama stack does not carry. Both
	// read tables the drama migrations created, which is why they can be composed here
	// without a second migration story.
	projectService := appprojects.NewService(appprojects.Options{
		Projects: database.NewProjectRepository(connection),
		Canvas:   database.NewCanvasRepository(connection),
		Settings: database.NewDramaSettingsRepository(connection),
		Clock:    clock,
		IDs:      ids,
	})
	// THE MEMORY STACK, both halves of it. The transcript port is what WP-07 shipped and is
	// unchanged; the memory STORE is WP-10's, and it is built here rather than in a second
	// composition because both halves are what `Deps.Memory` needs and because the store's
	// absence is invisible at runtime — a service with no repository reports "no memory store is
	// configured" and every memory command refuses, which is a state that looks like an empty
	// project rather than like a wiring gap.
	//
	// The reviewer found exactly that: the port and the store did not line up, nothing failed to
	// compile because the service takes an interface, and the whole WP-10 surface was unreachable
	// in a composed build. The `var _ appmemory.Repository = (*database.MemoryRepository)(nil)`
	// assertion in the repository is what refuses that shape now; this is the composition that
	// makes the assertion load-bearing.
	memoryRepository := database.NewMemoryRepository(connection)
	memoryService := appmemory.NewService(appmemory.Options{
		Store:    database.NewAgentRepository(connection),
		Items:    memoryRepository,
		Vectors:  database.NewMemoryVectorIndex(memoryRepository),
		Embedder: newProjectEmbedder(deps.Providers, connection),
		Clock:    clock,
		IDs:      ids,
		// THE EVENT RECORDER, which is what makes MemoryCreated reachable. ADR-0009 section 5 assigned
		// the emission to this package, the name has been in the closed vocabulary since migration
		// 000013, and until this the memory store wrote rows and announced nothing — the "declared but
		// never emitted" gap STATUS's event table has carried since WP-05.
		Events: deps.Drama.events,
	})

	tools, err := agenttools.Build(agenttools.Deps{
		Story:      deps.Drama.story,
		Script:     deps.Drama.script,
		Storyboard: deps.Drama.storyboard,
		Workflow:   deps.Drama.workflow,
		Memory:     memoryService,
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
		Projects: projectService,
		// The chapter reader WP-06 declared: the walk from a chapter to the text it
		// indexes, which is the same one the extraction service uses.
		Chapters: desktop.NewChapterTextReader(deps.Drama.story, deps.Drama.importing),
		// The media reads the `final_episode` agent writes its recipe from. It is the table's ONE
		// optional dependency — see `agenttools.Deps.Media` — and the reason it may be nil here is the
		// ORDER: this function is called from inside the drama stack's composition block, and the
		// media stack needs a service from that same block. The composition root therefore builds the
		// media stack first and passes the reader in through `agentDeps.Media`, which `app.go` does.
		Media: deps.Media,
	})
	if err != nil {
		// A table that will not build is a composition defect and there is no degraded
		// mode: an agent with half its tools would produce stages whose writes their own
		// handlers refused. So nothing is composed and every method reports unavailable.
		return nil
	}
	repository := database.NewAgentRepository(connection)
	assembly, err := agentassembly.Build(context.Background(), agentassembly.Options{
		Tools: tools, Versions: repository, Files: deps.Files,
		Clock: clock, IDs: ids,
	})
	if err != nil {
		return nil
	}
	model := agentruntime.NewModelBridge(newTextGenerator(deps.Providers, connection))
	runtime := agentruntime.New(agentruntime.Options{
		Registry: assembly.Registry(),
		Tools:    tools,
		Models:   model,
		Runs:     repository,
		// The real validator: every agent's output is checked against the contract its
		// manifest named, which is what makes AC-AGENT-002 a property of the runtime
		// rather than of a test that happens to call the validator.
		Validate: validateWith(appvalidation.Against),
		// Section 6.1 puts the tool schema check AFTER the ACL and before the handler, and
		// this is it. Without it a model could hand a handler any shape at all: the schema
		// path travelled into the prompt and was never applied to what came back.
		ToolArguments: validateToolArgumentsWith(appvalidation.Against),
		Artifacts:     database.NewArtifactVerifier(connection),
		Clock:         clock,
		IDs:           ids,
		// THE MEMORY PORT, which is what makes section 12.2's order reachable at all: the runtime
		// recalls before it writes the current turn and writes both turns afterwards, and it can only
		// do that if it is handed something to do it with. Before this line the port existed and
		// nothing filled it, so every prompt's memory layer was empty and no user message was ever
		// stored — the "interface with no real path" shape this repository has now found four times.
		Memory: newMemoryBridge(memoryService),
	})
	engine := agentruntime.NewEngine(agentruntime.EngineOptions{
		Runtime: runtime,
		// The transitioner is the WORKFLOW service, which owns stage transitions and
		// writes their audit events in the same transaction (ADR-0009). The
		// repository below is what adds the two capabilities that service does not
		// have: counting an attempt's revisions from the event trail, and finding
		// which run a stage belongs to.
		//
		// Composing it this way is what makes the engine drivable by the REAL
		// services rather than only by a test double — the gap WP-07's engine tests
		// left open, and the reason workflow.Service gained GetRun.
		Transitioner: stageTransitioner{workflow: deps.Drama.workflow, revisions: repository},
		Clock:        clock,
		IDs:          ids,
	})
	// The script pipeline, over this stack's runtime and engine and the drama stack's services.
	//
	// It is composed HERE because every dependency it drives is in scope: the engine and the runtime are
	// this function's, the script and workflow services arrive through `deps.Drama`, and the assembly
	// carries the skills. Composing it anywhere else would mean passing three of those across a module
	// boundary — and until this existed the package was reachable only from tests, so no user command
	// could run a script stage. That is the same shape of gap as the canvas projector: a package with a
	// real implementation and no production caller.
	// THE DETERMINISTIC CHECKER, which the two pipelines' supervisors consult.
	//
	// It is composed HERE because every read its rules make is a repository over this connection,
	// and it is handed to BOTH pipelines: the stage name decides which ruleset runs, and a checker
	// that only knew about storyboards would report nothing for a script stage — which is the correct
	// answer today, since AGENT_CONTRACTS section 11.1's script rules have no mechanical half this
	// build can check without inventing vocabulary the specification does not give.
	//
	// Composing it is what makes section 11.4's "硬规则应尽量用确定性代码先检查" true in a real build
	// rather than only in tests. Until this existed the rules were reachable from nowhere, which is
	// the "interface with no real path" shape this repository's reviews have found five times.
	//
	// THE FINAL RULESET IS ATTACHED TO THE SAME OBJECT, which is what makes AC-MEDIA-003's "Final
	// Supervisor" clause real: the `final_episode` stage's supervisor is preceded by these rules, and
	// a ruleset composed anywhere but here would leave the stage supervised by a model alone. The
	// reader takes the same connection, for the reason the storyboard rules' reads do.
	checker := database.NewStoryboardConsistencyChecker(
		database.NewStoryboardRepository(connection),
		database.NewAssetRepository(connection),
		newScriptReaderSource(database.NewScriptRepository(connection)),
		database.NewStoryRepository(connection),
	).
		WithFinalRuleset(database.NewFinalFactsReader(connection)).
		// The SCRIPT ruleset (section 11.1's mechanical half), attached here for the reason the final
		// ruleset is: a ruleset composed anywhere but at the composition root would leave a stage
		// supervised by a model alone, and the whole point of section 11.4 is that the joins run FIRST.
		WithScriptRuleset(database.NewScriptRepository(connection)).
		// FR-110's SAFETY category's vendor half: the job table's failed rows, which is where a
		// provider's content-policy refusal is recorded. Attached here for the same reason the other
		// two are — a rule composed anywhere but the composition root is a rule no user command runs.
		WithJobFailures(database.NewJobRepository(connection))
	var pipeline *appscriptpipeline.Service
	if deps.Drama != nil && deps.Drama.script != nil && deps.Drama.workflow != nil {
		pipeline = appscriptpipeline.New(appscriptpipeline.Options{
			Engine:   engine,
			Runtime:  runtime,
			Script:   deps.Drama.script,
			Workflow: deps.Drama.workflow,
			Assembly: assembly,
			Runs:     repository,
			Checks:   checker,
		})
	}
	// The production pipeline, over the same runtime and engine and the drama stack's other
	// services. It is composed HERE for the reason the script one is: every dependency it
	// drives is in scope, and until it exists the five production stages have no caller —
	// the "interface with no real path" shape this repository's reviews have found twice.
	//
	// It is a separate OPTIONAL composition because its batch needs more than the stages do:
	// `Jobs` and `Gaps` are what the image batch and its gate read, so a build without them
	// still drives the five agent stages and refuses the batch with a reason.
	var production *appproductionpipeline.Service
	if deps.Drama != nil && deps.Drama.storyboard != nil && deps.Drama.workflow != nil {
		production = appproductionpipeline.New(appproductionpipeline.Options{
			Engine:     engine,
			Runtime:    runtime,
			Storyboard: deps.Drama.storyboard,
			Workflow:   deps.Drama.workflow,
			Assembly:   assembly,
			Runs:       repository,
			Assets:     deps.Drama.assets,
			Gaps:       deps.Drama.gaps,
			Jobs:       deps.Jobs,
			Checks:     checker,
		})
	}
	return &agentWiring{
		runtime: runtime, assembly: assembly, tools: tools,
		projectService: projectService, memoryService: memoryService,
		engine:     engine,
		pipeline:   pipeline,
		production: production,
	}
}

// Memory returns the memory service this stack composed.
//
// It exists because the memory center's binding and the runtime's memory port must be the SAME
// service: a second composition would be a second store, and a memory a user pinned in the memory
// center would not be the memory a run recalls. The accessor is what makes that a property of the
// wiring rather than of two call sites agreeing.
func (w *agentWiring) Memory() *appmemory.Service {
	if w == nil {
		return nil
	}
	return w.memoryService
}

// Production returns the production pipeline, or nil when this stack is not composed.
func (w *agentWiring) Production() *appproductionpipeline.Service {
	if w == nil {
		return nil
	}
	return w.production
}

// Pipeline returns the script pipeline, or nil when this stack is not composed.
func (w *agentWiring) Pipeline() *appscriptpipeline.Service {
	if w == nil {
		return nil
	}
	return w.pipeline
}

// Available reports whether the agent stack is composed.
func (w *agentWiring) Available() bool {
	return w != nil && w.runtime != nil && w.runtime.Available() &&
		w.engine != nil && w.engine.Available() && w.assembly != nil
}

// ExtractionService returns the extraction service with this stack's runtime wired in as
// its Extractor.
//
// This closes the seam WP-06 recorded: "如果 Agent Runtime 尚未完成，Event Extraction 先通过明确的
// Application Service + Mock/Provider 适配实现，WP-07 再接入统一 Runtime". The service keeps
// doing the validating and the writing; what changes is WHO reads the chapter, and the
// answer is the three-layer runtime under the agent contract rather than an adapter that
// happens to produce JSON.
//
// It returns a NEW service rather than mutating the drama stack's, so the composition is
// a value and a reader can see which build has the runtime attached. The drama stack keeps
// its extractor-less service for the paths that do not extract.
func (w *agentWiring) ExtractionService(drama *dramaWiring) *appextraction.Service {
	if w == nil || drama == nil {
		return nil
	}
	return appextraction.NewService(appextraction.Options{
		Reader:    desktop.NewChapterTextReader(drama.story, drama.importing),
		Story:     drama.story,
		Extractor: extractionAgent{wiring: w},
		Clock:     appprojectsClock{},
		IDs:       id.NewGenerator(),
	})
}

// extractionAgent implements appextraction.Extractor by running the extraction agent.
//
// It is the ONLY implementation of that port in a production build, which is the point:
// WP-06 shipped the port with no implementation ("The extractor is deliberately absent"),
// and this is the one WP-07 adds. There is still no default, so a build that failed to
// compose this stack refuses extraction with a reason instead of inventing facts.
type extractionAgent struct {
	wiring *agentWiring
}

// Extract reads one chapter through the runtime.
//
// The chapter's text is the TASK, marked untrusted, which is section 5.2's rule: an
// imported novel is data and the prompt says so. The PROJECT comes from the request, which
// the reader filled from the chapter's own document walk — so the run's scope is a fact
// about the chapter rather than a value this adapter guessed.
func (a extractionAgent) Extract(ctx context.Context, request appextraction.Request) ([]byte, error) {
	if a.wiring == nil || !a.wiring.Available() {
		return nil, extractiondomain.UnavailableError()
	}
	spec, ok := a.wiring.assembly.Registry().Lookup("script.execution.event_extraction")
	if !ok {
		return nil, agent.UnavailableError()
	}
	skill, _ := a.wiring.assembly.SkillDocument(spec.Key)
	skillVersion, _ := a.wiring.assembly.SkillVersionOf(spec.Key)
	outcome, err := a.wiring.runtime.Run(ctx, agentruntime.Invocation{
		AgentKey:        spec.Key,
		ProjectID:       request.ProjectID,
		Skill:           skill,
		SkillVersion:    skillVersion,
		Task:            renderExtractionTask(request),
		TaskIsUntrusted: true,
	})
	if err != nil {
		return nil, err
	}
	// The runtime validated the document before returning it, and the SERVICE validates it
	// again against the same schema. That is deliberate rather than redundant: this port's
	// contract says "unvalidated bytes", so an implementation that relied on the caller
	// trusting it would move the check into a comment — and the service's second check is
	// what keeps the port's contract true.
	return []byte(outcome.Output), nil
}

// renderExtractionTask builds the task text an extraction run is given.
//
// The title and the text travel together because a model uses the heading for context, and
// the LANGUAGE is stated so the reply comes back in the document's own language. Nothing
// else is included: section 6.3's "对大文档使用查询工具按需读取，不把整本小说注入" is why the
// chapter arrives alone rather than with its neighbours.
func renderExtractionTask(request appextraction.Request) string {
	var builder strings.Builder
	if title := strings.TrimSpace(request.Title); title != "" {
		builder.WriteString("Chapter: " + title + "\n\n")
	}
	if language := strings.TrimSpace(request.Language); language != "" {
		builder.WriteString("Write your summary in this language: " + language + "\n\n")
	}
	builder.WriteString(request.Text)
	return builder.String()
}

// RunStage drives one stage: start it, run its execution agent, and report what happened.
//
// This is section 8's call chain, and the ORDER is what matters: the engine starts the
// stage (which is what checks the database's state), the runtime runs the agent (which is
// what validates the output before anything is written), and the caller then applies a
// review or a gate — nothing here decides whether an action is legal, because that is the
// engine's and the domain's.
func (w *agentWiring) RunStage(ctx context.Context, request StageRunRequest) (StageRunResult, error) {
	if !w.Available() {
		return StageRunResult{}, agent.UnavailableError()
	}
	stage, err := w.engine.StartStage(ctx, agentruntime.StartStageRequest{
		WorkflowRunID: request.WorkflowRunID,
		Stage:         workflow.StageName(request.Stage),
		ExecutionKey:  request.ExecutionKey,
		InputJSON:     request.InputJSON,
		Actor:         agentruntime.Actor{Type: "agent", ID: request.AgentRunID},
	})
	if err != nil {
		return StageRunResult{}, err
	}
	spec, ok := w.assembly.Registry().ForStage(request.Stage)
	if !ok {
		// A stage with no registered execution agent cannot run. The attempt is left
		// running rather than cancelled, because the caller's next step is to register one
		// and a cancelled attempt cannot be restarted.
		return StageRunResult{
			StageRunID: stage.ID,
			Failure:    "No execution agent is registered for that stage.",
		}, nil
	}
	result, err := w.runSpec(ctx, spec, agentruntime.Invocation{
		ProjectID:       request.ProjectID,
		EpisodeID:       request.EpisodeID,
		WorkflowRunID:   request.WorkflowRunID,
		StageRunID:      stage.ID,
		WorkflowState:   request.WorkflowState,
		ApprovedFacts:   request.ApprovedFacts,
		Task:            request.Task,
		TaskIsUntrusted: request.TaskIsUntrusted,
		ModelID:         request.ModelID,
		ProviderID:      request.ProviderID,
	})
	result.StageRunID = stage.ID
	return result, err
}

// RunSupervision runs the supervision agent for a stage.
//
// The agent is looked up by the stage's name through SupervisionFor, so which supervisor
// reviews a stage is the registry's answer rather than a caller's choice — which is what
// section 9's "Runtime 必须再次校验" means for the review path.
func (w *agentWiring) RunSupervision(ctx context.Context, request SupervisionRunRequest) (StageRunResult, error) {
	if !w.Available() {
		return StageRunResult{}, agent.UnavailableError()
	}
	stage, err := w.engine.Load(ctx, request.WorkflowRunID)
	if err != nil {
		return StageRunResult{}, err
	}
	attempt, ok := stage.Stages[workflow.StageName(request.Stage)].Latest()
	if !ok {
		return StageRunResult{}, agent.NotFoundError()
	}
	spec, ok := w.assembly.Registry().SupervisionFor(request.Stage)
	if !ok {
		return StageRunResult{
			StageRunID: attempt.ID,
			Failure:    "No supervision agent is registered for that stage.",
		}, nil
	}
	result, err := w.runSpec(ctx, spec, agentruntime.Invocation{
		ProjectID:       request.ProjectID,
		EpisodeID:       request.EpisodeID,
		WorkflowRunID:   request.WorkflowRunID,
		StageRunID:      attempt.ID,
		WorkflowState:   request.WorkflowState,
		ApprovedFacts:   request.ApprovedFacts,
		Task:            request.Task,
		TaskIsUntrusted: request.TaskIsUntrusted,
		ModelID:         request.ModelID,
		ProviderID:      request.ProviderID,
	})
	result.StageRunID = attempt.ID
	return result, err
}

// runSpec runs one registered agent, filling in the skill text and version from the
// assembly.
//
// Both come from the assembly rather than from the caller because a run must cite the
// version it RAN: a caller that passed a document and a version separately could pass two
// that never co-existed, and the record would name a version whose contents were not what
// the model saw.
func (w *agentWiring) runSpec(ctx context.Context, spec agent.Spec, invocation agentruntime.Invocation) (StageRunResult, error) {
	invocation.AgentKey = spec.Key
	invocation.Skill, _ = w.assembly.SkillDocument(spec.Key)
	invocation.SkillVersion, _ = w.assembly.SkillVersionOf(spec.Key)
	outcome, err := w.runtime.Run(ctx, invocation)
	if err != nil {
		// The refusal is reported rather than returned, because the stage row is the
		// record: a caller that got only an error could not look up what happened. The
		// error is still returned alongside, so a caller that wants to branch on its type
		// can.
		return StageRunResult{
			RunID:   outcome.RunID,
			Failure: err.Error(),
		}, err
	}
	return StageRunResult{
		RunID:   outcome.RunID,
		Output:  outcome.Output,
		Summary: outcome.Summary,
	}, nil
}

// StageRunRequest asks for one stage to run.
type StageRunRequest struct {
	ProjectID     string
	EpisodeID     string
	WorkflowRunID string
	Stage         string
	// AgentRunID is the attribution recorded on the stage's audit event. It is empty for
	// a run a person asked for, which is the honest answer: the transition's actor is the
	// local user rather than an agent.
	AgentRunID    string
	ExecutionKey  string
	InputJSON     string
	WorkflowState string
	ApprovedFacts string
	Task          string
	// TaskIsUntrusted marks the task as carrying document text, which section 5.2
	// requires the prompt to say.
	TaskIsUntrusted bool
	ModelID         string
	ProviderID      string
}

// SupervisionRunRequest asks for a stage's output to be reviewed.
type SupervisionRunRequest struct {
	ProjectID       string
	EpisodeID       string
	WorkflowRunID   string
	Stage           string
	WorkflowState   string
	ApprovedFacts   string
	Task            string
	TaskIsUntrusted bool
	ModelID         string
	ProviderID      string
}

// StageRunResult reports what one agent run produced.
type StageRunResult struct {
	StageRunID string
	RunID      string
	Output     json.RawMessage
	Summary    string
	// Failure is set when the run could not complete. It carries the refusal's safe
	// message and nothing else: the error itself is returned beside this value, so a
	// caller that needs the type has it and a caller that needs a string does too.
	Failure string
}

// textGenerator adapts the provider registry to the model bridge's port.
//
// It resolves the provider for EACH call rather than holding one, because a user can change
// the configuration between runs and a bridge that cached an adapter would keep calling the
// old one.
//
// The choice is the project-level policy this build can support today: the first enabled
// provider, ordered by id so the choice is stable across runs. Section 13's per-layer
// policy object — a different model for supervision, which the section requires to avoid
// same-source bias — is what a settings UI will write, and the resolution point for it is
// the one function below. ADR-0011 records this as a placeholder for the POLICY rather than
// for the mechanism.
type textGenerator struct {
	registry *infraproviders.Registry
	db       *sql.DB
}

// newTextGenerator builds the adapter.
func newTextGenerator(registry *infraproviders.Registry, db *sql.DB) *textGenerator {
	return &textGenerator{registry: registry, db: db}
}

// Generate implements the model bridge's TextGenerator port.
func (g *textGenerator) Generate(ctx context.Context, request agentruntime.TextGenerationRequest) (agentruntime.TextGenerationResult, error) {
	if g == nil || g.registry == nil || g.db == nil {
		return agentruntime.TextGenerationResult{}, agent.UnavailableError()
	}
	providerID, err := g.choice(ctx, request.ProviderID, request.Layer, request.ProjectID)
	if err != nil {
		return agentruntime.TextGenerationResult{}, err
	}
	port, err := g.registry.TextPortFor(ctx, providerID)
	if err != nil {
		// A provider that cannot be resolved is a configuration fact, not a transport
		// failure, and the classification is what tells a caller whether to retry.
		return agentruntime.TextGenerationResult{}, agentruntime.ClassifyProviderFailure(err)
	}
	messages := make([]appproviders.TextMessage, 0, len(request.Messages))
	for _, message := range request.Messages {
		messages = append(messages, appproviders.TextMessage{Role: message.Role, Content: message.Content})
	}
	result, err := port.Generate(ctx, appproviders.TextRequest{
		ProviderID: providerID, Model: request.Model, Messages: messages,
	})
	if err != nil {
		return agentruntime.TextGenerationResult{}, agentruntime.ClassifyProviderFailure(err)
	}
	out := agentruntime.TextGenerationResult{Content: result.Content, Model: result.Model}
	for _, call := range result.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, agentruntime.TextGenerationToolCall{
			Key: call.Key, Arguments: call.Arguments,
		})
	}
	return out, nil
}

// choice resolves which provider a call uses.
//
// A caller that named one is honoured after checking it is enabled, which is what lets a
// run pin its own provider — section 13's "Supervision: 独立 Provider" — through the run's
// configuration rather than through this function.
// choice resolves which provider serves one call.
//
// THREE CASES, in this order, and the order is the policy rather than a preference:
//
//  1. The request NAMED a provider, so that one is used and it must be enabled.
//  2. The request states a LAYER, and the project's policy for that layer names a provider. Section 13
//     exists because the layers have different needs — a supervisor on a different provider is what makes
//     its review independent, and "低成本阶段不默认使用最昂贵模型" is a per-stage rule — so this is the
//     case that makes the section reachable.
//  3. Neither, so the first enabled provider is used.
//
// THE THIRD CASE IS A FALLBACK AND NOT THE DESIGN, which is worth stating because it was the whole
// implementation until now: every call went to whichever provider was enabled first, so the per-layer
// policy the SCHEMA already carried could not affect anything. WP-08 closes that, and FR-140's per-STAGE
// keys (`script_execution_model` and its siblings) remain deferred — a stage is not a layer, and stating
// the tables now would be inventing a shape the specification does not give.
func (g *textGenerator) choice(ctx context.Context, named string, layer agent.AgentLayer, projectID string) (string, error) {
	if trimmed := strings.TrimSpace(named); trimmed != "" {
		var enabled int
		err := g.db.QueryRowContext(ctx,
			`SELECT enabled FROM provider_configs WHERE id = ?`, trimmed).Scan(&enabled)
		if err != nil {
			if err == sql.ErrNoRows {
				return "", agent.InvalidError("The provider this run named is not configured.")
			}
			return "", agent.StorageError("The provider configuration could not be read.", err)
		}
		if enabled != 1 {
			return "", agent.InvalidError("The provider this run named is disabled.")
		}
		return trimmed, nil
	}
	// The layer's policy, which a project may state. A policy whose provider is not enabled is a
	// CONFIGURATION a user has to fix, so it is reported rather than skipped: falling through to another
	// provider would send this project's prompts somewhere its owner did not choose, which is the one
	// thing section 13's rules are about.
	if resolved, ok, err := g.policyProvider(ctx, layer, projectID); err != nil {
		return "", err
	} else if ok {
		return resolved, nil
	}
	var id string
	err := g.db.QueryRowContext(ctx,
		`SELECT id FROM provider_configs WHERE enabled = 1 ORDER BY id ASC LIMIT 1`).Scan(&id)
	if err != nil {
		if err == sql.ErrNoRows {
			// No enabled provider: every run refuses with this, which is the honest state
			// for a fresh install rather than reaching some default.
			return "", agent.UnavailableError()
		}
		return "", agent.StorageError("The provider configuration could not be read.", err)
	}
	return id, nil
}

// policyProvider reads the provider a project's policy names for one layer.
//
// The lookup walks the layer FIRST and then `default`, which is what the `default` row is for: a project
// states its ordinary provider once and overrides the layers that differ. The boolean is false when
// neither row names one, which is the ordinary state of a project whose policy is a policy about MODELS
// rather than about providers — the policy's own `primaryModelId` is a model, and a project that names
// only a model has said nothing about which provider should serve it.
//
// A policy naming a provider that is not enabled is REFUSED rather than skipped, and the refusal names
// both: a project whose supervisor was pinned to a provider that has since been disabled must not quietly
// run its supervisor on another one, because section 13's whole point is that the reviewer differ.
func (g *textGenerator) policyProvider(ctx context.Context, layer agent.AgentLayer, projectID string) (string, bool, error) {
	project := strings.TrimSpace(projectID)
	name := strings.TrimSpace(string(layer))
	if project == "" || name == "" {
		return "", false, nil
	}
	rows, err := g.db.QueryContext(ctx, `SELECT layer, policy_json FROM project_provider_policies
		WHERE project_id = ? AND layer IN (?, 'default')`, project, name)
	if err != nil {
		return "", false, agent.StorageError("The project's model policy could not be read.", err)
	}
	defer rows.Close()
	policies := map[string]string{}
	for rows.Next() {
		var layerName, policyJSON string
		if err := rows.Scan(&layerName, &policyJSON); err != nil {
			return "", false, agent.StorageError("The project's model policy could not be read.", err)
		}
		policies[layerName] = policyJSON
	}
	if err := rows.Err(); err != nil {
		return "", false, agent.StorageError("The project's model policy could not be read.", err)
	}
	// The layer's own row wins over `default`, and the map makes that a lookup rather than an ordering
	// dependency on the query.
	for _, candidate := range []string{name, "default"} {
		policyJSON, ok := policies[candidate]
		if !ok {
			continue
		}
		providerID := providerFromPolicy(policyJSON)
		if providerID == "" {
			continue
		}
		var enabled int
		if err := g.db.QueryRowContext(ctx,
			`SELECT enabled FROM provider_configs WHERE id = ?`, providerID).Scan(&enabled); err != nil {
			if err == sql.ErrNoRows {
				return "", false, agent.InvalidError(
					"The provider this project's policy names is not configured.")
			}
			return "", false, agent.StorageError("The provider configuration could not be read.", err)
		}
		if enabled != 1 {
			// Refused rather than skipped, so a disabled provider is a visible configuration problem
			// instead of a silent fallback to one the project did not choose.
			return "", false, agent.InvalidError("The provider this project's policy names is disabled.")
		}
		return providerID, true, nil
	}
	return "", false, nil
}

// providerFromPolicy reads `providerId` out of a policy document.
//
// It returns "" for anything it cannot read, and that is the SAFE direction here rather than the lax one:
// an unreadable policy means "this row states no provider", which sends the caller to the next row in the
// chain and ends at the enabled-provider fallback. Refusing instead would make a policy whose JSON this
// build does not understand stop every run, which is worse than not honouring a field it cannot read —
// and the project service's own validation keeps a stored policy a JSON object, which is the only thing
// this function depends on.
//
// The field is `providerId` because that is what the project service's policy editor writes. Section 13's
// own example shows `primaryModelId` and no provider at all, so a project may legitimately omit this — and
// a document written to the section's example exactly is then handled by the fallback rather than refused.
func providerFromPolicy(policyJSON string) string {
	trimmed := strings.TrimSpace(policyJSON)
	if trimmed == "" {
		return ""
	}
	var document struct {
		ProviderID string `json:"providerId"`
	}
	if err := json.Unmarshal([]byte(trimmed), &document); err != nil {
		return ""
	}
	return strings.TrimSpace(document.ProviderID)
}

// validateWith adapts the validation package's function to the runtime's Validator type.
//
// The two return structurally identical violations from different packages, and the
// conversion is written out rather than made implicit because the two are NOT the same
// type: the extraction domain's Violation and the runtime's both carry a path and a
// message, and a future change to either must not silently change the other. This
// function is where that is noticed.
func validateWith(against func(string, []byte) ([]appvalidation.Violation, error)) agentruntime.Validator {
	return func(schemaPath string, raw []byte) ([]agentruntime.Violation, error) {
		return convertViolations(against, schemaPath, raw)
	}
}

// validateToolArgumentsWith adapts the validation package to the runtime's
// ToolArgumentValidator type.
//
// It exists separately from validateWith because the two are DISTINCT named function types in
// the runtime: one validates an agent's output document and one validates a tool call's
// arguments, and section 6.1 makes them different steps of the chain. Collapsing them into one
// type would let a caller wire the wrong one at a call site the compiler could not question —
// which is exactly the class of mistake the separation catches.
func validateToolArgumentsWith(against func(string, []byte) ([]appvalidation.Violation, error)) agentruntime.ToolArgumentValidator {
	return func(schemaPath string, raw []byte) ([]agentruntime.Violation, error) {
		return convertViolations(against, schemaPath, raw)
	}
}

// convertViolations runs the validator and translates its violations.
func convertViolations(against func(string, []byte) ([]appvalidation.Violation, error), schemaPath string, raw []byte) ([]agentruntime.Violation, error) {
	violations, err := against(schemaPath, raw)
	if err != nil {
		return nil, err
	}
	out := make([]agentruntime.Violation, 0, len(violations))
	for _, violation := range violations {
		out = append(out, agentruntime.Violation{Path: violation.Path, Message: violation.Message})
	}
	return out, nil
}

// Compile-time proof that the adapter satisfies the bridge's port and that the extraction
// agent satisfies WP-06's.
var (
	_ agentruntime.TextGenerator = (*textGenerator)(nil)
	_ appextraction.Extractor    = extractionAgent{}
)

// stageTransitioner adapts the workflow service plus the agent repository to the engine's
// StageTransitioner, RevisionCounter and StageRunLocator.
//
// It exists because the three capabilities live in two places, and neither alone is enough:
//
//   - The workflow SERVICE owns stage transitions and writes their audit events in one
//     transaction (ADR-0009), and it is what CreateStage, TransitionStage, ListStages and
//     GetRun are. An engine writing stage rows around it would produce state changes with
//     no record.
//   - The agent REPOSITORY answers the two questions the workflow tables cannot answer
//     cheaply: how many times an attempt has begun a revision (which is a COUNT over the
//     event trail) and which run a stage belongs to (which the repository already walks
//     for its own scope checks).
//
// The adapter is the composition root's, which is where a dependency knows both sides.
// Putting this shim inside the engine would make the engine depend on the workflow service;
// putting it inside the repository would put a workflow service dependency there. Neither
// is acceptable, and a small type here is what AGENTS section 7.2's direction allows.
type stageTransitioner struct {
	workflow  *appworkflow.Service
	revisions *database.AgentRepository
}

// CreateStage creates an attempt through the workflow service.
func (t stageTransitioner) CreateStage(ctx context.Context, request agentruntime.StageCreationRequest) (workflow.StageRun, error) {
	return t.workflow.CreateStage(ctx, appworkflow.CreateStageRequest{
		WorkflowRunID: request.WorkflowRunID,
		Stage:         request.Stage,
		Attempt:       request.Attempt,
		ExecutionKey:  request.ExecutionKey,
		InputJSON:     request.InputJSON,
		Actor:         appworkflow.Actor{Type: versioning.CreatedByType(request.Actor.Type), ID: request.Actor.ID},
	})
}

// TransitionStage moves an attempt through the workflow service.
func (t stageTransitioner) TransitionStage(ctx context.Context, request agentruntime.StageTransitionRequest) (workflow.StageRun, error) {
	return t.workflow.TransitionStage(ctx, appworkflow.TransitionStageRequest{
		StageRunID: request.StageRunID,
		Status:     request.Status,
		Revision:   request.Revision,
		Actor:      appworkflow.Actor{Type: versioning.CreatedByType(request.Actor.Type), ID: request.Actor.ID},
	})
}

// ListStages returns a run's attempts.
func (t stageTransitioner) ListStages(ctx context.Context, workflowRunID string) ([]workflow.StageRun, error) {
	return t.workflow.ListStages(ctx, workflowRunID)
}

// GetRun returns one workflow run.
func (t stageTransitioner) GetRun(ctx context.Context, runID string) (workflow.WorkflowRun, error) {
	return t.workflow.GetRun(ctx, runID)
}

// RevisionCount counts an attempt's revisions from the event trail.
func (t stageTransitioner) RevisionCount(ctx context.Context, stageRunID string) (int, error) {
	return t.revisions.RevisionCount(ctx, stageRunID)
}

// WorkflowRunOfStage finds which run a stage belongs to.
func (t stageTransitioner) WorkflowRunOfStage(ctx context.Context, stageRunID string) (string, error) {
	return t.revisions.WorkflowRunOfStage(ctx, stageRunID)
}

// Compile-time proof that the adapter satisfies the three interfaces the engine asks for.
// A signature drift in any of them breaks this build rather than producing an engine that
// silently reports a missing capability.
var (
	_ agentruntime.StageTransitioner = stageTransitioner{}
	_ agentruntime.RevisionCounter   = stageTransitioner{}
	_ agentruntime.StageRunLocator   = stageTransitioner{}
)
