package database

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appassets "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/assets"
	appmemory "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/memory"
	appprojects "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/projects"
	appproviders "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/providers"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/asset"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/memory"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/project"
	infraproviders "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/infrastructure/providers"
)

// scale_wp12_test.go is the load the WP-12 scale items are measured on.
//
//	## WP-12 scope, items 3 and 4
//	3. 1,000 node/2,000 edge 性能
//	4. 10k asset/Memory benchmark
//
// # What this file is, and what it deliberately is not
//
// It BUILDS the two loads and hands them to the measurements in
// scale_canvas_wp12_test.go and scale_records_wp12_test.go. There is no assertion in it.
//
// The loads are built THROUGH THE REAL REPOSITORIES over a REAL MIGRATED DATABASE
// (wp05Migrations, the head set) rather than by bulk INSERT statements. That is slower —
// ten thousand rows through `CreateItem` take seconds rather than milliseconds — and it is
// the whole point: a fixture that wrote rows directly would measure a table, while what the
// WP-12 items ask about is a PATH. A bug in a repository's write, a column the mapper drops,
// an index a query cannot use — none of those would show up in a load written by `INSERT`.
// WP-11's own `canary_media_wp11_test.go` and WP-10's harness build their loads the same way
// for the same reason.
//
// # The frontend half of item 3 cannot be measured here, and that is recorded rather than
// implied
//
// Item 3 says "1,000 node/2,000 edge 性能". The canvas a USER experiences is a React surface
// in `web/src/pages/canvas/`, and its render cost at 1,000 nodes is a browser measurement
// that no Go test can make. This repository's Playwright suite
// (`web/e2e/canvas-regression.spec.ts`) runs the canvas in BROWSER mode against Vite, and its
// own header says so: "The desktop shell is not launched: a Wails window cannot be driven
// from a test runner, so the suite covers the browser-mode canvas and the Go-side behaviour
// is covered by the Go tests." Browser mode persists through the legacy adapter into
// IndexedDB, not through the Go core (ADR-BASE-004), so a Playwright run at 1,000 nodes
// would measure the LEGACY adapter's IndexedDB writes and the React renderer — a different
// storage path from the one these benchmarks measure, and not a Go core measurement.
//
// SO: what is measured below is the GO CORE at 1,000 nodes and 2,000 edges. The claim these
// measurements support is "the Go core's canvas reads and writes at scale are bounded". The
// claim they do NOT support, and which nothing in this repository currently supports, is
// "the canvas renders 1,000 nodes at 60fps". Stating the second from these numbers would be
// a false claim, so it is left open here rather than papered over.

// --- the canvas load ---------------------------------------------------------------------

// wp12CanvasNodeCount and wp12CanvasEdgeCount are item 3's numbers, named so a reader sees
// the requirement in the code rather than a bare 1000.
const (
	wp12CanvasNodeCount = 1000
	wp12CanvasEdgeCount = 2000
	// wp12MoveBatch is how many nodes one MoveNodes call moves. It is 100 rather than the
	// full 1000 because that is the shape the canvas actually produces: a drag moves the
	// SELECTION, and `MaxBatchMoves` (5000) is a ceiling rather than a typical call. The
	// bound test also measures the worst case a single call may express, which is the
	// ceiling itself.
	wp12MoveBatch = 100
	// wp12ExportNodes is a canvas big enough that a quadratic regression in the export
	// projection would show, and small enough to keep the test quick.
	wp12ExportNodes = 500
)

// wp12Canvas is a migrated database holding one project with a 1,000-node, 2,000-edge
// canvas, plus the ids the measurements address.
type wp12Canvas struct {
	db         *sql.DB
	projects   *ProjectRepository
	canvas     *CanvasRepository
	service    *appprojects.Service
	documentID string
	projectID  string
	// nodeIDs is every node in creation order, and edgeIDs every connection, so a
	// measurement can address a specific row without a query of its own.
	nodeIDs []string
	edgeIDs []string
}

// wp12CanvasFixture builds item 3's load: 1,000 nodes and 2,000 edges.
func wp12CanvasFixture(t testing.TB) *wp12Canvas {
	t.Helper()
	return wp12CanvasOfSize(t, wp12CanvasNodeCount, wp12CanvasEdgeCount)
}

// wp12CanvasOfSize builds the load at a chosen size, so the SHAPE comparison in
// scale_canvas_wp12_test.go can build the same canvas at 250 nodes and at 1,000 and compare
// the two reads. It is one function rather than two fixtures because a shape comparison
// between two implementations would be comparing the implementations.
//
// The nodes are written in ONE transaction. A thousand separate transactions would make this
// fixture take minutes rather than a second, and a transaction is the same mechanism the asset
// gap reports and the export path use for their bulk writes — so the node write still goes
// through `CreateNode`, through the repository's own transaction binding.
//
// The edges are NOT in that transaction: each is created through `Service.CreateEdge`, which
// validates the relation against an endpoint's projection before storing it. That is the path
// the canvas uses, and it is deliberately the slow one — it reads two nodes and the document
// per edge — so a fixture that skipped it by writing rows directly would be measuring a write
// the product does not perform.
func wp12CanvasOfSize(t testing.TB, nodeCount, edgeCount int) *wp12Canvas {
	t.Helper()
	ctx := context.Background()
	handle, err := open(ctx, filepath.Join(t.TempDir(), "app.db"),
		filepath.Join(t.TempDir(), "snapshots"), wp05Migrations(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := handle.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	db := handle.SQL()
	generator := newTestIDGenerator()
	now := fixedClock()()

	projects := NewProjectRepository(db)
	canvas := NewCanvasRepository(db)
	record := sampleProject(t, generator)
	if err := projects.CreateProject(ctx, record); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	documentID, err := generator.New()
	if err != nil {
		t.Fatal(err)
	}
	document := project.CanvasDocument{
		ID: documentID, ProjectID: record.ID, Name: record.Name, Kind: project.CanvasFree,
		Viewport: project.Viewport{K: 1}, CreatedAt: now, UpdatedAt: now, Revision: 1,
	}
	if err := canvas.CreateDocument(ctx, document); err != nil {
		t.Fatalf("CreateDocument: %v", err)
	}

	load := &wp12Canvas{
		db: db, projects: projects, canvas: canvas,
		documentID: documentID, projectID: record.ID,
		nodeIDs: make([]string, 0, nodeCount),
		edgeIDs: make([]string, 0, edgeCount),
	}

	// The nodes are laid out on a grid rather than stacked at the origin, so the values are
	// the ones a real canvas would carry and not a column of zeroes a bug could not be
	// distinguished from.
	nodeIDs := make([]string, 0, nodeCount)
	for index := 0; index < nodeCount; index++ {
		id, err := generator.New()
		if err != nil {
			t.Fatal(err)
		}
		nodeIDs = append(nodeIDs, id)
	}
	if err := wp12WriteNodes(ctx, db, canvas, documentID, nodeIDs, now); err != nil {
		t.Fatalf("writing %d nodes: %v", nodeCount, err)
	}
	load.nodeIDs = nodeIDs

	// The service is composed the way the binding composes it, so the edges below travel the
	// path the canvas uses.
	load.service = appprojects.NewService(appprojects.Options{
		Projects: projects, Canvas: canvas,
		Clock: fixedClockProvider{}, IDs: generator,
	})
	// A ring plus chords: every node sends one edge to its successor, and the nodes beyond
	// the first ring send a second to a node further along. The set is deterministic and both
	// endpoints are standalone nodes, so nothing here depends on a projection that does not
	// exist.
	for index := 0; index < edgeCount; index++ {
		from := index % nodeCount
		var to int
		if index < nodeCount {
			to = (from + 1) % nodeCount
		} else {
			to = (from + 7) % nodeCount
		}
		if from == to {
			continue
		}
		edge, err := load.service.CreateEdge(ctx, appprojects.CreateEdgeRequest{
			DocumentID:   documentID,
			FromNodeID:   nodeIDs[from],
			ToNodeID:     nodeIDs[to],
			RelationType: project.RelationGeneric,
		})
		if err != nil {
			t.Fatalf("CreateEdge %d (%s -> %s): %v", index, nodeIDs[from], nodeIDs[to], err)
		}
		load.edgeIDs = append(load.edgeIDs, edge.ID)
	}

	// The load is asserted before it is handed out. A measurement on a database that is not
	// the size it claims would report a number about nothing, which is worse than no number.
	if count, err := canvas.CountNodes(ctx, documentID); err != nil || count != nodeCount {
		t.Fatalf("the canvas holds %d nodes, want %d (err %v)", count, nodeCount, err)
	}
	if count, err := canvas.CountEdges(ctx, documentID); err != nil || count != len(load.edgeIDs) {
		t.Fatalf("the canvas holds %d edges, want %d (err %v)", count, len(load.edgeIDs), err)
	}
	return load
}

// wp12NodeType alternates two builtin node types.
//
// 'text' and 'image' rather than one repeated type: a canvas of identical rows hides a
// per-type filter that reads the wrong column, and the alternation costs nothing.
func wp12NodeType(index int) string {
	if index%2 == 0 {
		return "text"
	}
	return "image"
}

// wp12WriteNodes writes the node rows in one transaction, through the repository.
func wp12WriteNodes(ctx context.Context, db *sql.DB, canvas *CanvasRepository, documentID string, ids []string, now time.Time) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	repo := canvas.WithinTx(tx)
	for index, id := range ids {
		column, row := index%40, index/40
		node := project.Node{
			ID: id, CanvasDocumentID: documentID,
			NodeType:  wp12NodeType(index),
			Title:     fmt.Sprintf("Node %d", index),
			PositionX: float64(column) * 320,
			PositionY: float64(row) * 240,
			Width:     280, Height: 180,
			ZIndex: index,
			// A ui_state that is neither empty nor one fixed string, so a read that dropped
			// the column would be visible in what comes back.
			UIState:   fmt.Sprintf(`{"status":"done","index":%d}`, index),
			CreatedAt: now, UpdatedAt: now, Revision: 1,
		}
		if err := repo.CreateNode(ctx, node); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("CreateNode %d: %w", index, err)
		}
	}
	return tx.Commit()
}

// wp12Batch builds one MoveNodes payload, offsetting each node by the given step so a
// repeated call is a different write rather than an idempotent one.
//
// The revisions travel with the positions because `MoveNodes` guards each node on its own
// revision: a call that sent a stale one would apply ZERO moves and return success, which is
// exactly the shape of a benchmark that measures nothing. The caller passes the nodes it just
// read, so the revisions are the stored ones.
func wp12Batch(nodes []project.Node, offset float64) []appprojects.NodePosition {
	positions := make([]appprojects.NodePosition, 0, len(nodes))
	for _, node := range nodes {
		positions = append(positions, appprojects.NodePosition{
			ID: node.ID, X: node.PositionX + offset, Y: node.PositionY, Revision: node.Revision,
		})
	}
	return positions
}

// wp12ReadNodes returns the whole node list, which is what a move batch needs to carry the
// revisions the guard compares against.
func (load *wp12Canvas) wp12ReadNodes(t testing.TB) []project.Node {
	t.Helper()
	nodes, err := load.canvas.ListNodes(context.Background(), load.documentID)
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(nodes) != wp12CanvasNodeCount {
		t.Fatalf("ListNodes returned %d nodes, want %d", len(nodes), wp12CanvasNodeCount)
	}
	return nodes
}

// wp12Viewport is the viewport a save writes, varied by the caller so repeated saves are
// distinct writes rather than no-ops.
func (load *wp12Canvas) wp12Viewport(step float64) project.Viewport {
	return project.Viewport{X: step, Y: -step, K: 1.25}
}

// --- the 10k record load -----------------------------------------------------------------

// wp12RecordCount is item 4's number.
const wp12RecordCount = 10000

// wp12VectorDimensions is the width of the vectors the memory index is measured over.
//
// It is `providers.MockEmbeddingDimensions` rather than a number chosen here, because that is
// the width THIS BUILD actually stores: PRD FR-120's keyword fallback is the mock adapter, and
// its 256 slots are what `memory_items.embedding_blob` holds when no provider is configured.
// A benchmark at a different width would report a cost no installation incurs.
const wp12VectorDimensions = infraproviders.MockEmbeddingDimensions

// The embedding provenance the measured vectors carry. They are the mock adapter's own
// constants, so the search below selects by the model and version a real row would name.
const (
	wp12Model   = infraproviders.MockEmbeddingModel
	wp12Version = infraproviders.MockEmbeddingVersion
)

// wp12Records is a migrated database with 10,000 assets, 10,000 embedded memories and 10,000
// transcript messages.
type wp12Records struct {
	db        *sql.DB
	assets    *AssetRepository
	assetSvc  *appassets.Service
	memories  *MemoryRepository
	index     *MemoryVectorIndex
	memorySvc *appmemory.Service
	agents    *AgentRepository
	projectID string
	// assetCount and memoryCount are the sizes THIS load was built at, so the assertions
	// below state what they expect rather than reading a constant that a differently sized
	// fixture would disagree with.
	assetCount  int
	memoryCount int
	scope       appmemory.Scope
	memoryIDs   []string
	assetIDs    []string
	embedder    *infraproviders.MockEmbeddingAdapter
	newestName  string
}

// wp12RecordsFixture builds item 4's load: 10,000 assets and 10,000 embedded memories.
func wp12RecordsFixture(t testing.TB) *wp12Records {
	t.Helper()
	return wp12RecordsOfSize(t, wp12RecordCount)
}

// wp12RecordsOfSize builds the load at a chosen size, so the SHAPE comparison in
// scale_records_wp12_test.go can build the same store at 2,500 rows and at 10,000 and compare
// the two reads. It is one function rather than two fixtures because a shape comparison
// between two implementations would be comparing the implementations.
//
// THE VECTORS ARE REAL VECTORS, produced by the same adapter a build with no provider
// configured uses. Seeding a blob of zeroes would make the scan score every candidate
// identically and measure a dot product over a table of zeroes — a shape that cannot fail,
// which is not a measurement of anything. The texts are distinct so the hashing trick spreads
// them over the dimensions, which is what makes the scan's per-candidate cost the one a real
// project pays.
func wp12RecordsOfSize(t testing.TB, count int) *wp12Records {
	t.Helper()
	ctx := context.Background()
	handle, err := open(ctx, filepath.Join(t.TempDir(), "app.db"),
		filepath.Join(t.TempDir(), "snapshots"), wp05Migrations(t), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := handle.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	db := handle.SQL()
	dramaSeedParents(t, db)
	projectID := "drama-project"
	generator := newTestIDGenerator()
	now := fixedClock()()

	// `agent_runs.skill_version_id` is a foreign key, and a run that cited a version which
	// was never registered is refused rather than stored. `dramaSeedParents` carries the
	// workspace, project, episode, script and workflow run; the skill version is what
	// `newAgentFixture` adds on top of it for the same reason, and it is added here rather
	// than there so no other test's fixture changes.
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO skill_versions
		(id, skill_key, version, content_hash, status, created_at)
		VALUES ('%s', 'script', '1.0.0', 'wp12-hash', 'active', '2026-01-01T00:00:00Z')`,
		agentTestSkill)); err != nil {
		t.Fatalf("seeding the skill version: %v", err)
	}

	load := &wp12Records{
		db: db, assets: NewAssetRepository(db), memories: NewMemoryRepository(db),
		projectID: projectID, assetCount: count, memoryCount: count,
		scope: appmemory.ScopeFor(projectID, "drama-episode", "script.decision"),
	}
	load.index = NewMemoryVectorIndex(load.memories)
	load.embedder = infraproviders.NewMockEmbeddingAdapter()
	load.agents = NewAgentRepository(db)
	load.memorySvc = appmemory.NewService(appmemory.Options{
		Store:    load.agents,
		Items:    load.memories,
		Vectors:  load.index,
		Embedder: wp12EmbedderBridge{adapter: load.embedder},
		Clock:    fixedClockProvider{},
		// The generator is passed straight in: `*id.Generator` already has the
		// `New() (string, error)` the memory port declares, so the fixture and the service
		// mint from ONE generator and a fixture id cannot collide with a service id.
		IDs: generator,
	})
	load.assetSvc = appassets.NewService(appassets.Options{
		Repository: load.assets, Clock: fixedClockProvider{}, IDs: generator,
	})

	// The transcript rows the Recent channel reads, written through the runtime's own
	// repository so the scope columns and the encoded key are the runtime's rather than a
	// fixture's guess at them. One run carries them all: a run is a parent row for its
	// messages, and a thousand runs would be a thousand extra rows with nothing to say.
	if err := load.seedTranscript(ctx, count, now, generator); err != nil {
		t.Fatal(err)
	}

	// THE ASSETS. The type cycles the full documented vocabulary, which is what makes a
	// paged read's `LIMIT ? OFFSET ?` walk a table whose rows differ rather than one page of
	// identical shapes.
	types := asset.Types
	for index := 0; index < count; index++ {
		id := fmt.Sprintf("wp12-asset-%05d", index)
		record := asset.Asset{
			ID: id, ProjectID: projectID, Type: types[index%len(types)],
			Name: fmt.Sprintf("Asset %05d", index),
			// A description of a realistic length, because the row's size is part of what a
			// paged read costs to scan.
			Description: strings.Repeat("asset ", 12),
			Status:      asset.StatusActive,
			CreatedAt:   now, UpdatedAt: now, Revision: 1,
		}
		if err := load.assets.CreateAsset(ctx, record); err != nil {
			t.Fatalf("CreateAsset %d: %v", index, err)
		}
		load.assetIDs = append(load.assetIDs, id)
	}

	// THE MEMORIES, every one embedded. The write goes through `CreateItem` and then
	// `AssignEmbedding` — the two calls the store's own write path uses — so a memory that a
	// search cannot find would be a defect in this load rather than in the fixture.
	//
	// The embedding is computed in ONE batch. `MockEmbeddingAdapter.vectorFor` is pure, so
	// batching changes nothing about the vectors, and thousands of separate `Embed` calls
	// would spend the fixture's time in the adapter rather than in what is being measured.
	texts := make([]string, 0, count)
	for index := 0; index < count; index++ {
		texts = append(texts, wp12MemoryText(index))
	}
	embedding := load.embedTexts(t, texts)
	if len(embedding.Vectors) != count {
		t.Fatalf("the embedder returned %d vectors for %d texts", len(embedding.Vectors), count)
	}
	for index := 0; index < count; index++ {
		id := fmt.Sprintf("wp12-memory-%05d", index)
		item := memory.MemoryItem{
			ID: id, Type: memory.TypeSemantic, Scope: load.scope,
			Role: agent.MessageUser, AgentKey: load.scope.AgentKey,
			Content: wp12MemoryText(index),
			// A distinct importance per row, so the pinned channel and any ordering by
			// importance have something to order.
			Importance: float64(index%10) / 10,
			Confidence: memory.DefaultConfidence,
			// No source columns: a memory the user typed has no upstream row, and
			// `Validate` refuses a half citation (`SourceType` without `SourceID`).
			SourceType: memory.SourceUnset,
			CreatedAt:  now, UpdatedAt: now,
			// The schema's CHECK is `revision >= 1`, so a stored row starts at one.
			Revision: 1,
		}
		if err := load.memories.CreateItem(ctx, item); err != nil {
			domainErr, ok := memory.AsError(err)
			if ok {
				t.Fatalf("CreateItem %d: %v (cause: %v)", index, err, domainErr.Cause)
			}
			t.Fatalf("CreateItem %d: %v", index, err)
		}
		if err := load.memories.AssignEmbedding(ctx, id,
			memory.EncodeVector(memory.Normalize(embedding.Vectors[index])),
			embedding.Model, embedding.Version, now); err != nil {
			t.Fatalf("AssignEmbedding %d: %v", index, err)
		}
		load.memoryIDs = append(load.memoryIDs, id)
	}

	// The load is asserted. A benchmark over an empty table reports a fast number about
	// nothing, which is the failure mode this check exists to make impossible.
	if count, err := load.assets.CountAssets(ctx, projectID); err != nil || count != load.assetCount {
		t.Fatalf("the project holds %d assets, want %d (err %v)", count, load.assetCount, err)
	}
	embedded, err := load.memories.VectorCandidates(ctx, load.scope, wp12Model, wp12Version, 0)
	if err != nil {
		t.Fatalf("VectorCandidates: %v", err)
	}
	// `MaxCandidates` caps this read by design, so the check is that the cap is REACHED —
	// or, at the smaller size the shape comparison builds, that every row comes back. A scan
	// bounded at 500 candidates over a ten-thousand-row table is the behaviour SECURITY
	// section 7.5 asks for, and it is what the benchmarks measure.
	wantCandidates := appmemory.MaxCandidates
	if count < wantCandidates {
		wantCandidates = count
	}
	if len(embedded) != wantCandidates {
		t.Fatalf("the scope returned %d embedded candidates, want %d", len(embedded), wantCandidates)
	}
	// The transcript must actually hold what the Recent channel will read, because a recall
	// over an empty table is the same "fast number about nothing" this fixture exists to
	// avoid.
	recent, err := load.memorySvc.BuildRecent(ctx, appmemory.RecallRequest{
		Scope: load.scope, Limit: appmemory.RecentWindow,
	})
	if err != nil {
		t.Fatalf("BuildRecent: %v", err)
	}
	if len(recent) != appmemory.RecentWindow {
		t.Fatalf("the transcript returned %d recent messages, want %d", len(recent), appmemory.RecentWindow)
	}
	// The newest asset is what a paged read's first page should lead with, so the read has a
	// row to be checked against rather than a bare count.
	load.newestName = fmt.Sprintf("Asset %05d", load.assetCount-1)
	list, err := load.assetSvc.ListAssets(ctx, appassets.ListFilter{ProjectID: projectID, Limit: 10})
	if err != nil {
		t.Fatalf("ListAssets: %v", err)
	}
	if len(list) != 10 || list[0].Name != load.newestName {
		t.Fatalf("the first page returned %d assets leading with %q, want 10 leading with %q",
			len(list), list[0].Name, load.newestName)
	}
	return load
}

// seedTranscript writes the agent_messages rows the Recent channel reads.
//
// One run carries them all: a message names the run it belongs to, and thousands of runs would
// be thousands of extra rows with nothing to say about the read being measured. The scope key
// is the six parts joined by "|" — the shape `agentFixture.seedMessage` and the WP-10 harness
// write — from which `RecordMessage` derives the columns. A fixture that got the parts in the
// wrong order is caught by the recall assertion in the fixture, because the recall filters on
// those columns and would come back empty.
func (load *wp12Records) seedTranscript(ctx context.Context, count int, now time.Time, generator interface{ New() (string, error) }) error {
	runID, err := generator.New()
	if err != nil {
		return err
	}
	run := agent.AgentRun{
		ID: runID, ProjectID: load.projectID, Layer: agent.LayerDecision,
		AgentKey: load.scope.AgentKey, SkillVersionID: agentTestSkill,
		Status: agent.RunSucceeded, StartedAt: now, Revision: 1,
	}
	if err := load.agents.CreateRun(ctx, run); err != nil {
		return fmt.Errorf("CreateRun: %w", err)
	}
	// DOMAIN_MODEL section 14.4's order: tenant, workspace, project, episode, agent, session.
	scopeKey := strings.Join([]string{
		load.scope.Tenant, load.scope.Workspace, load.scope.Project,
		load.scope.Episode, load.scope.AgentKey, load.scope.Session,
	}, "|")
	return wp12WriteMessages(ctx, load.db, load.agents, runID, scopeKey, count, now)
}

// wp12WriteMessages writes the transcript rows in one transaction, through the repository.
func wp12WriteMessages(ctx context.Context, db *sql.DB, agents *AgentRepository, runID, scopeKey string, count int, now time.Time) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	repo := agents.WithinTx(tx)
	for index := 0; index < count; index++ {
		role := agent.MessageUser
		if index%2 == 1 {
			role = agent.MessageAssistant
		}
		message := agent.AgentMessage{
			ID: fmt.Sprintf("wp12-message-%05d", index), AgentRunID: runID,
			ScopeKey: scopeKey, Role: role,
			Content: fmt.Sprintf("第 %d 轮：场景 %d 的对话", index, index%211),
			// A 64-character lowercase hex digest, which is what the domain's own check
			// requires of a non-empty hash.
			ContentHash: fmt.Sprintf("%064x", index),
			// Each row is a second later than the last, so the newest-first ordering the
			// recall relies on has something to order by.
			CreatedAt: now.Add(time.Duration(index) * time.Second),
		}
		if err := repo.RecordMessage(ctx, message); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("RecordMessage %d: %w", index, err)
		}
	}
	return tx.Commit()
}

// wp12MemoryText is one memory's content. The index makes every row distinct, which is what
// spreads the hashed terms over the vector's dimensions.
func wp12MemoryText(index int) string {
	return fmt.Sprintf("场景 %d 的设定：角色 Mira 在第 %d 场说了台词 %d", index, index%97, index)
}

// embedTexts runs one batch through the adapter and maps it to the port's result.
func (load *wp12Records) embedTexts(t testing.TB, texts []string) appmemory.EmbeddingResult {
	t.Helper()
	result, err := load.embedder.Embed(context.Background(), appproviders.EmbeddingRequest{
		ProviderID: wp12ProviderID, Model: wp12Model, Texts: texts,
	})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	return appmemory.EmbeddingResult{
		Model: result.Model, Version: result.Version, Vectors: result.Vectors,
	}
}

// wp12EmbedderBridge adapts the infrastructure mock to the memory port.
//
// The port takes a project id and the adapter takes a provider id, which is the only
// difference: the adapter is resolved per provider at the composition root, while the port is
// asked "can THIS project embed". The bridge supplies the mock's own kindless provider id,
// which is what `ValidKindlessID` requires of it.
//
// `Available` answers true because this build HAS the keyword fallback. PRD FR-120 requires
// that a build with no embedding provider still offers the semantic channel through the local
// adapter, so a false here would make `SemanticAvailable` false and the scored channels
// unreachable — a different product from the one being measured.
type wp12EmbedderBridge struct {
	adapter *infraproviders.MockEmbeddingAdapter
}

func (b wp12EmbedderBridge) Available(context.Context, string) bool { return true }

func (b wp12EmbedderBridge) Embed(ctx context.Context, _ string, request appmemory.EmbeddingRequest) (appmemory.EmbeddingResult, error) {
	if b.adapter == nil {
		return appmemory.EmbeddingResult{}, memory.InvalidError("No embedding adapter is composed.")
	}
	result, err := b.adapter.Embed(ctx, appproviders.EmbeddingRequest{
		ProviderID: wp12ProviderID, Model: wp12Model, Texts: request.Texts,
	})
	if err != nil {
		return appmemory.EmbeddingResult{}, err
	}
	return appmemory.EmbeddingResult{
		Model: result.Model, Version: result.Version, Vectors: result.Vectors,
	}, nil
}

// wp12ProviderID is the kindless id the mock adapter's requests carry. It is lowercase
// letters and a dash because `provider.ValidKindlessID` accepts nothing else.
const wp12ProviderID = "mock-embedding"

// The memory port's identifier generator needs no adapter: `*id.Generator` already has the
// `New() (string, error)` the port declares, so `newTestIDGenerator()` is passed straight in
// and the fixture and the application mint from one generator — which is what keeps a fixture
// id from colliding with a service-minted one.

// --- timing helpers ----------------------------------------------------------------------

// wp12Measure runs one operation and reports how long it took, failing the test when it did
// not succeed. It exists so a bound and a benchmark measure the SAME call: the number in the
// test's failure message and the number `go test -bench` prints come from one code path.
//
// THE OPERATION IS REPEATED until the elapsed time clears the host clock's resolution, and
// the total is divided by the repetitions. This is not padding around the measurement: on this
// host a single indexed read can finish inside one tick of `time.Now()`, and the first version
// of this helper reported such a read as "0ns" — not a measurement but the absence of one.
// That number would have hidden exactly the regression these bounds exist for, because a slow
// call divided by nothing is not a ratio.
//
// The loop is bounded so a genuinely slow operation is measured once, at its full cost: it
// stops as soon as the elapsed time clears `wp12ClockResolution`, so nothing at or above that
// value is ever divided.
// wp12MeasureOnce times one operation exactly once.
//
// It exists for an operation that CANNOT be repeated: `MoveNodes` advances every node's
// revision, so a second identical call applies nothing and reports zero — a measurement of an
// operation that did no work. Repeating a state-changing operation to average away noise is
// wrong for the same reason, and this is the honest alternative: one call, one figure, and the
// caller's bound set with that single sample in mind.
func wp12MeasureOnce(t testing.TB, name string, work func() error) time.Duration {
	t.Helper()
	started := time.Now()
	if err := work(); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return time.Since(started)
}

// wp12Measure times one operation, and reports the FASTEST of its repetitions.
//
// # Why the minimum rather than a mean
//
// These figures feed two kinds of assertion: a bound, and a SHAPE comparison between a small
// load and a large one. The shape comparison is the one that needs stability, because it
// divides one timing by another and a hiccup in either is a false N+1 — this check failed once
// at 22.5x for an 8x load and passed at 10.0x on every isolated re-run afterwards.
//
// Noise can only ever make an operation look SLOWER than it is, so the minimum is the closest
// estimate of the true cost and the mean is a measure of the machine as much as of the code. A
// minimum is also what a bound wants: it answers "can this be this fast", which is the
// question a regression changes.
//
// The repetitions below the clock resolution exist so that a very fast operation is still
// measured above the timer's tick, and the cap keeps that loop bounded.
func wp12Measure(t testing.TB, name string, work func() error) time.Duration {
	t.Helper()
	// The whole loop is timed ONCE and divided, rather than each call being timed
	// individually, because this host's clock granularity is coarse enough that a
	// single fast operation measures as ZERO. That is not a fast result, it is no
	// measurement at all — an earlier version of this function reported 0ns for a
	// canvas read that takes ~2.7ms, and the SHAPE check then divided by zero.
	//
	// Timing the batch and dividing keeps the minimum's stability: the fastest
	// BATCH is the one measured, and the per-call figure comes from a total well
	// above the clock tick.
	best := time.Duration(0)
	for round := 0; round < wp12MaxRepetitions; round++ {
		repetitions := 1
		started := time.Now()
		for {
			if err := work(); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			elapsed := time.Since(started)
			if elapsed >= wp12ClockResolution {
				perCall := elapsed / time.Duration(repetitions)
				if best == 0 || perCall < best {
					best = perCall
				}
				break
			}
			repetitions++
		}
	}
	return best
}

// wp12ClockResolution is the elapsed time below which one measurement is not trusted and the
// operation is repeated. 8ms is comfortably above this host's clock tick and still far below
// every bound in these files, so the loop cannot mask a real cost.
const wp12ClockResolution = 8 * time.Millisecond

// wp12MaxRepetitions bounds that loop. At 64 an operation below the resolution is reported as
// one sixty-fourth of the total elapsed, which is still a real per-call figure.
const wp12MaxRepetitions = 64

// wp12Shape is what a doubling of the load did to one operation.
//
// It is what turns "fast today" into "linear", and it is here because a FIXED BOUND cannot
// see the two defects these items are really about. An O(n²) scan over a canvas of a hundred
// nodes is milliseconds, and an N+1 read of a hundred rows is a hundred queries nobody
// notices; both hide below any ceiling while the load is small. At the sizes WP-12 names they
// are obvious — but only if something measures the SHAPE rather than the level.
type wp12Shape struct {
	name string
	// small and large are the same operation's cost at N and at 2N.
	small, large time.Duration
	// rows is how many more rows the large run touched, so perRow is readable as a number.
	rows int
}

// wp12ShapeOf records a comparison of the same operation at two sizes.
func wp12ShapeOf(name string, small, large time.Duration, rows int) wp12Shape {
	return wp12Shape{name: name, small: small, large: large, rows: rows}
}

// ratio is what the large run cost relative to the small one. Linear work lands near 2;
// quadratic work lands near 4.
func (s wp12Shape) ratio() float64 {
	if s.small <= 0 {
		return 0
	}
	return float64(s.large) / float64(s.small)
}

// perRow is the cost of each extra row, as a float, so a sub-nanosecond-per-row figure is
// reported as a fraction rather than truncated to zero by integer division.
func (s wp12Shape) perRow() string {
	if s.rows <= 0 {
		return "n/a"
	}
	micros := float64((s.large - s.small).Nanoseconds()) / 1000 / float64(s.rows)
	return fmt.Sprintf("%.3fµs", micros)
}

// String renders the comparison for a failure message or a log line.
//
// The two figures are labelled "small" and "large" rather than "N" and "2N" because the two
// comparisons here grow by different factors — the node list by 4x and the edge list by 8x —
// and a label that named one of them would be wrong on the other.
func (s wp12Shape) String() string {
	return fmt.Sprintf("%s: small=%s large=%s ratio=%.2fx per-extra-row=%s",
		s.name, wp12Milliseconds(s.small), wp12Milliseconds(s.large), s.ratio(), s.perRow())
}

// wp12Milliseconds renders a duration for a failure message.
//
// The unit is chosen from the value, because `time.Duration.String()` renders a
// sub-microsecond duration as "0s" and a plain two-decimal millisecond format renders a
// sub-microsecond one as "0.00ms" — either of which would make a failure message that reports
// 0.4 µs read as "no time at all".
func wp12Milliseconds(value time.Duration) string {
	switch {
	case value < time.Microsecond:
		return fmt.Sprintf("%dns", value.Nanoseconds())
	case value < time.Millisecond:
		return fmt.Sprintf("%.1fµs", float64(value.Nanoseconds())/1000)
	default:
		return fmt.Sprintf("%.2fms", float64(value.Microseconds())/1000)
	}
}
