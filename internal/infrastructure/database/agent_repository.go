package database

import (
	"context"
	"database/sql"
	"strings"

	agentruntime "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/agentruntime"
	memoryapp "github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/application/memory"
	"github.com/newyngwieslash-ops/infinite-atelier-mine-0903/internal/domain/agent"
)

// AgentRepository is the SQLite implementation of the agent runtime's ports: the
// run record, the messages, the tool calls, the skill versions, and the revision
// counter the workflow engine reads.
//
// Column names come from migration 000015, which is the authority for every table
// this type touches. It implements four ports and this is deliberate rather than a
// God object: they are four views of ONE aggregate — a run, what it said, what it
// called, and the skill it ran under — and splitting them across four types would
// mean four connections for a record that is written in one transaction per run.
//
// What it does NOT do is decide anything. A run's status, the message roles and a
// tool call's outcome are the runtime's decisions; this type stores them and refuses
// one that would violate the schema's constraints.
type AgentRepository struct {
	db *sql.DB
	tx *sql.Tx
}

// NewAgentRepository builds the repository over a database handle.
func NewAgentRepository(db *sql.DB) *AgentRepository {
	return &AgentRepository{db: db}
}

// WithinTx returns a repository bound to one transaction.
func (r *AgentRepository) WithinTx(tx *sql.Tx) *AgentRepository {
	return &AgentRepository{db: r.db, tx: tx}
}

func (r *AgentRepository) conn() querier {
	if r == nil {
		return nil
	}
	return connection(r.db, r.tx)
}

// Compile-time proof that this type satisfies every port it is composed as. A
// signature drift breaks the build rather than surfacing as a nil interface at the
// composition root.
var (
	_ agentruntime.RunStore        = (*AgentRepository)(nil)
	_ agentruntime.RevisionCounter = (*AgentRepository)(nil)
	_ agentruntime.StageRunLocator = (*AgentRepository)(nil)
	_ memoryapp.Store              = (*AgentRepository)(nil)
)

// CreateRun writes one run.
//
// It is called BEFORE the model is called, so a run that dies mid-flight leaves a row
// saying it started. status is written as given rather than defaulted: the runtime
// writes 'running' and a caller writing 'pending' means it.
func (r *AgentRepository) CreateRun(ctx context.Context, run agent.AgentRun) error {
	conn := r.conn()
	if conn == nil {
		return storageError("AGENT_STORE_UNAVAILABLE", "The agent store is unavailable.", nil)
	}
	// created_at and updated_at take started_at: the row appears when the run starts,
	// and AgentRun carries no clock of its own beyond that pair. Reading the wall clock
	// here would be inventing a second version of a fact the domain already states.
	stamp := formatTime(run.StartedAt)
	_, err := conn.ExecContext(ctx, `INSERT INTO agent_runs
		(id, project_id, workflow_run_id, stage_run_id, agent_layer, agent_key, model_config_id,
		 skill_version_id, status, input_summary, validated_output_json, raw_output_file_id,
		 error_code, started_at, finished_at, created_at, updated_at, revision)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.ProjectID, run.WorkflowRunID, run.StageRunID, string(run.Layer), run.AgentKey,
		run.ModelConfigID, run.SkillVersionID, string(run.Status), run.InputSummary,
		run.ValidatedOutputJSON, run.RawOutputFileID, run.ErrorCode,
		formatTime(run.StartedAt), formatTime(run.FinishedAt),
		stamp, stamp, run.Revision)
	if err != nil {
		if isForeignKeyViolation(err) {
			// The two foreign keys are the project and the skill version. A missing
			// skill version is the one that happens in practice — a caller that has not
			// registered the skill it loaded — so the message says which, because
			// "the run could not be saved" would send someone looking at the project.
			if !r.skillVersionExists(ctx, run.SkillVersionID) {
				return agent.InvalidError("That skill version is not registered, so the run cannot cite it.")
			}
			return agent.InvalidError("That project no longer exists.")
		}
		return storageError("AGENT_RUN_WRITE_FAILED", "The agent run could not be saved.", err)
	}
	return nil
}

// skillVersionExists reports whether a skill version row is present.
//
// It exists so the refusal above names the referent that is actually missing. A
// second query after a failure is acceptable because the path is a refusal already.
func (r *AgentRepository) skillVersionExists(ctx context.Context, id string) bool {
	conn := r.conn()
	if conn == nil || strings.TrimSpace(id) == "" {
		return false
	}
	var one int
	if err := conn.QueryRowContext(ctx, `SELECT 1 FROM skill_versions WHERE id = ?`, id).Scan(&one); err != nil {
		return false
	}
	return true
}

// FinishRun records a run's outcome, guarded by its revision.
//
// The guard is the optimistic-concurrency rule every repository here follows: a run
// that was closed by someone else, or by a recovery pass, must not be silently
// reopened by a writer holding a stale row.
func (r *AgentRepository) FinishRun(ctx context.Context, run agent.AgentRun, expectedRevision int64) error {
	conn := r.conn()
	if conn == nil {
		return storageError("AGENT_STORE_UNAVAILABLE", "The agent store is unavailable.", nil)
	}
	result, err := conn.ExecContext(ctx, `UPDATE agent_runs SET
		status = ?, validated_output_json = ?, raw_output_file_id = ?, error_code = ?,
		finished_at = ?, updated_at = ?, revision = revision + 1
		WHERE id = ? AND revision = ?`,
		string(run.Status), run.ValidatedOutputJSON, run.RawOutputFileID, run.ErrorCode,
		formatTime(run.FinishedAt), formatTime(run.FinishedAt), run.ID, expectedRevision)
	if err != nil {
		return storageError("AGENT_RUN_WRITE_FAILED", "The agent run could not be saved.", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return storageError("AGENT_RUN_WRITE_FAILED", "The agent run could not be saved.", err)
	}
	if affected == 0 {
		// Zero rows means the row moved under us or is not there. The two are
		// distinguished because the caller acts differently: a conflict is retried
		// after a reload, a missing run is a bug.
		if _, err := r.GetRun(ctx, run.ID); err != nil {
			return err
		}
		return agent.ConflictError("The agent run was changed by someone else.")
	}
	return nil
}

// GetRun reads one run.
func (r *AgentRepository) GetRun(ctx context.Context, id string) (agent.AgentRun, error) {
	conn := r.conn()
	if conn == nil {
		return agent.AgentRun{}, storageError("AGENT_STORE_UNAVAILABLE", "The agent store is unavailable.", nil)
	}
	var (
		run                             agent.AgentRun
		layer, status                   string
		startedAt, finishedAt           string
		validated, rawOutput, errorCode string
		createdAt, updatedAt            string
	)
	err := conn.QueryRowContext(ctx, `SELECT id, project_id, workflow_run_id, stage_run_id,
		agent_layer, agent_key, model_config_id, skill_version_id, status, input_summary,
		validated_output_json, raw_output_file_id, error_code, started_at, finished_at,
		created_at, updated_at, revision
		FROM agent_runs WHERE id = ?`, id).Scan(
		&run.ID, &run.ProjectID, &run.WorkflowRunID, &run.StageRunID,
		&layer, &run.AgentKey, &run.ModelConfigID, &run.SkillVersionID, &status, &run.InputSummary,
		&validated, &rawOutput, &errorCode, &startedAt, &finishedAt, &createdAt, &updatedAt, &run.Revision)
	if err != nil {
		if err == sql.ErrNoRows {
			return agent.AgentRun{}, agent.NotFoundError()
		}
		return agent.AgentRun{}, storageError("AGENT_READ_FAILED", "The agent run could not be read.", err)
	}
	run.Layer = agent.AgentLayer(layer)
	run.Status = agent.RunStatus(status)
	run.ValidatedOutputJSON = validated
	run.RawOutputFileID = rawOutput
	run.ErrorCode = errorCode
	run.StartedAt = parseTime(startedAt)
	run.FinishedAt = parseTime(finishedAt)
	// createdAt and updatedAt are selected so the scan positions stay aligned with the
	// statement, and then dropped: AgentRun carries StartedAt and FinishedAt, and a
	// second pair of clocks on one row would be two answers to one question.
	_, _ = createdAt, updatedAt
	return run, nil
}

// ListRuns returns a project's runs newest first.
func (r *AgentRepository) ListRuns(ctx context.Context, projectID string, limit int) ([]agent.AgentRun, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("AGENT_STORE_UNAVAILABLE", "The agent store is unavailable.", nil)
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := conn.QueryContext(ctx, `SELECT id FROM agent_runs
		WHERE project_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`, projectID, limit)
	if err != nil {
		return nil, storageError("AGENT_READ_FAILED", "The agent runs could not be read.", err)
	}
	defer rows.Close()
	ids := make([]string, 0, limit)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, storageError("AGENT_READ_FAILED", "The agent runs could not be read.", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("AGENT_READ_FAILED", "The agent runs could not be read.", err)
	}
	// The ids are read first and the rows hydrated after, so one connection is not
	// held open while a second query runs on it. SQLite would refuse the nest.
	runs := make([]agent.AgentRun, 0, len(ids))
	for _, id := range ids {
		run, err := r.GetRun(ctx, id)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, nil
}

// RecordMessage writes one message of a run.
func (r *AgentRepository) RecordMessage(ctx context.Context, message agent.AgentMessage) error {
	conn := r.conn()
	if conn == nil {
		return storageError("AGENT_STORE_UNAVAILABLE", "The agent store is unavailable.", nil)
	}
	// The scope travels as a key AND as its six parts, which is DOMAIN_MODEL section
	// 14.4's requirement: retrieval filters on the structured columns, and the key is
	// what makes the encoding checkable rather than assumed.
	//
	// The parts are derived from the key here rather than carried beside it, because
	// the domain record has ONE scope field: giving this repository a second source
	// would let the two disagree, and a row whose key and columns disagreed would be
	// silently recalled by whichever one a query happened to read.
	parts := splitScopeKey(message.ScopeKey)
	_, err := conn.ExecContext(ctx, `INSERT INTO agent_messages
		(id, agent_run_id, scope_key, scope_tenant, scope_workspace, scope_project, scope_episode,
		 scope_agent_key, scope_session, role, content, content_hash, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		message.ID, message.AgentRunID, message.ScopeKey,
		parts[0], parts[1], parts[2], parts[3], parts[4], parts[5],
		string(message.Role), message.Content, message.ContentHash, formatTime(message.CreatedAt))
	if err != nil {
		if isForeignKeyViolation(err) {
			return agent.NotFoundError()
		}
		return storageError("AGENT_MESSAGE_WRITE_FAILED", "The agent message could not be saved.", err)
	}
	return nil
}

// RecordToolCall writes one tool call.
func (r *AgentRepository) RecordToolCall(ctx context.Context, call agent.AgentToolCall) error {
	conn := r.conn()
	if conn == nil {
		return storageError("AGENT_STORE_UNAVAILABLE", "The agent store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO agent_tool_calls
		(id, agent_run_id, sequence, tool_key, input_json, output_json, status, error_code,
		 started_at, finished_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		call.ID, call.AgentRunID, call.Sequence, call.ToolKey, call.InputJSON, call.OutputJSON,
		string(call.Status), call.ErrorCode,
		formatTime(call.StartedAt), formatTime(call.FinishedAt), formatTime(call.StartedAt))
	if err != nil {
		if isUniqueViolation(err) {
			// The pair (run, sequence) is unique so a replay cannot insert a second call
			// at the same position. Reporting a conflict says what happened rather than
			// "the write failed".
			return agent.ConflictError("A tool call at that position is already recorded.")
		}
		if isForeignKeyViolation(err) {
			return agent.NotFoundError()
		}
		return storageError("AGENT_TOOLCALL_WRITE_FAILED", "The tool call could not be saved.", err)
	}
	return nil
}

// ListMessages returns one run's messages oldest first.
func (r *AgentRepository) ListMessages(ctx context.Context, runID string) ([]agent.AgentMessage, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("AGENT_STORE_UNAVAILABLE", "The agent store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, `SELECT id, agent_run_id, scope_key, role, content,
		content_hash, created_at FROM agent_messages WHERE agent_run_id = ?
		ORDER BY created_at ASC, id ASC`, runID)
	if err != nil {
		return nil, storageError("AGENT_READ_FAILED", "The agent messages could not be read.", err)
	}
	defer rows.Close()
	messages := make([]agent.AgentMessage, 0, 8)
	for rows.Next() {
		var message agent.AgentMessage
		var role, createdAt string
		if err := rows.Scan(&message.ID, &message.AgentRunID, &message.ScopeKey, &role,
			&message.Content, &message.ContentHash, &createdAt); err != nil {
			return nil, storageError("AGENT_READ_FAILED", "The agent messages could not be read.", err)
		}
		message.Role = agent.MessageRole(role)
		message.CreatedAt = parseTime(createdAt)
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("AGENT_READ_FAILED", "The agent messages could not be read.", err)
	}
	return messages, nil
}

// ListToolCalls returns one run's tool calls in order.
func (r *AgentRepository) ListToolCalls(ctx context.Context, runID string) ([]agent.AgentToolCall, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("AGENT_STORE_UNAVAILABLE", "The agent store is unavailable.", nil)
	}
	rows, err := conn.QueryContext(ctx, `SELECT id, agent_run_id, sequence, tool_key, input_json,
		output_json, status, error_code, started_at, finished_at
		FROM agent_tool_calls WHERE agent_run_id = ? ORDER BY sequence ASC`, runID)
	if err != nil {
		return nil, storageError("AGENT_READ_FAILED", "The tool calls could not be read.", err)
	}
	defer rows.Close()
	calls := make([]agent.AgentToolCall, 0, 4)
	for rows.Next() {
		var call agent.AgentToolCall
		var status, startedAt, finishedAt string
		if err := rows.Scan(&call.ID, &call.AgentRunID, &call.Sequence, &call.ToolKey,
			&call.InputJSON, &call.OutputJSON, &status, &call.ErrorCode,
			&startedAt, &finishedAt); err != nil {
			return nil, storageError("AGENT_READ_FAILED", "The tool calls could not be read.", err)
		}
		call.Status = agent.ToolCallStatus(status)
		call.StartedAt = parseTime(startedAt)
		call.FinishedAt = parseTime(finishedAt)
		calls = append(calls, call)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("AGENT_READ_FAILED", "The tool calls could not be read.", err)
	}
	return calls, nil
}

// RecentMessages implements the memory port's recall.
//
// The scope is matched by its STRUCTURED COLUMNS rather than by the encoded key,
// which is section 14.4's rule: "检索时按结构化字段过滤，不依赖字符串前缀". The key
// column exists so a row's scope is checkable, not so it can be parsed.
//
// The agent key is matched only when the caller names one. That is what makes the
// same query serve both questions section 12.2 asks — "what did THIS agent say" and
// "what happened in this project" — without a second method whose absence would be a
// capability gap.
func (r *AgentRepository) RecentMessages(ctx context.Context, scope [6]string, excludeMessageID string, limit int) ([]memoryapp.Item, error) {
	conn := r.conn()
	if conn == nil {
		return nil, storageError("AGENT_STORE_UNAVAILABLE", "The agent store is unavailable.", nil)
	}
	if limit <= 0 {
		limit = memoryapp.DefaultLimit
	}
	// The project is required by the domain's Scope.Validate, and it is required here
	// too: a query with an empty project would match every project's rows whose other
	// four columns happened to be empty, which is the cross-project leak section 14.4
	// forbids. Refusing is the only safe reading.
	if strings.TrimSpace(scope[2]) == "" {
		return nil, memoryapp.InvalidError("A recall needs a project scope.")
	}
	var builder strings.Builder
	builder.WriteString(`SELECT id, agent_run_id, scope_project, scope_agent_key, role, content,
		content_hash, created_at FROM agent_messages
		WHERE scope_project = ?`)
	args := []any{scope[2]}
	// Each part is matched when the caller stated one. An empty part means "any",
	// which is what a project-wide recall asks for, so it is left out of the WHERE
	// rather than compared against the empty string — a row whose episode is empty
	// would otherwise be matched by a caller asking for no particular episode, which
	// is a coincidence rather than a scope.
	for _, part := range []struct {
		column string
		value  string
	}{
		{"scope_tenant", scope[0]},
		{"scope_workspace", scope[1]},
		{"scope_episode", scope[3]},
		{"scope_session", scope[5]},
	} {
		if strings.TrimSpace(part.value) == "" {
			continue
		}
		builder.WriteString(" AND " + part.column + " = ?")
		args = append(args, part.value)
	}
	if key := strings.TrimSpace(scope[4]); key != "" {
		builder.WriteString(" AND scope_agent_key = ?")
		args = append(args, key)
	}
	if id := strings.TrimSpace(excludeMessageID); id != "" {
		// Section 14.5's "当前消息不召回自身". The exclusion is by id rather than by
		// content, because two identical messages are two messages and excluding the
		// text would drop an earlier one the user did write.
		builder.WriteString(" AND id != ?")
		args = append(args, id)
	}
	builder.WriteString(" ORDER BY created_at DESC, id DESC LIMIT ?")
	args = append(args, limit)

	rows, err := conn.QueryContext(ctx, builder.String(), args...)
	if err != nil {
		return nil, storageError("AGENT_READ_FAILED", "The recalled messages could not be read.", err)
	}
	defer rows.Close()
	items := make([]memoryapp.Item, 0, limit)
	for rows.Next() {
		var (
			item                    memoryapp.Item
			role, project, agentKey string
			contentHash, createdAt  string
		)
		if err := rows.Scan(&item.MessageID, &item.Provenance, &project, &agentKey,
			&role, &item.Content, &contentHash, &createdAt); err != nil {
			return nil, storageError("AGENT_READ_FAILED", "The recalled messages could not be read.", err)
		}
		item.Role = agent.MessageRole(role)
		item.CreatedAt = parseTime(createdAt)
		// The scope columns and the hash are read to keep the scan aligned with the
		// statement. Item carries what a prompt needs — the message, its role, its run
		// and its time — and repeating the scope back would be telling the caller what it
		// just asked for.
		_, _, _ = project, agentKey, contentHash
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, storageError("AGENT_READ_FAILED", "The recalled messages could not be read.", err)
	}
	return items, nil
}

// RevisionCount counts how many times a stage attempt began a revision.
//
// It reads the AUDIT TRAIL rather than a counter column, which is what the engine's
// RevisionCounter documents: every revision is a transition into needs_fix or
// needs_redo, and workflow_events records each one with its stage run. A counter
// column would be a second source of truth that could disagree with the events.
func (r *AgentRepository) RevisionCount(ctx context.Context, stageRunID string) (int, error) {
	conn := r.conn()
	if conn == nil {
		return 0, storageError("AGENT_STORE_UNAVAILABLE", "The agent store is unavailable.", nil)
	}
	var count int
	err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM workflow_events
		WHERE stage_run_id = ? AND to_status IN ('needs_fix', 'needs_redo')`,
		stageRunID).Scan(&count)
	if err != nil {
		return 0, storageError("AGENT_READ_FAILED", "The revision count could not be read.", err)
	}
	return count, nil
}

// WorkflowRunOfStage finds which run a stage attempt belongs to.
func (r *AgentRepository) WorkflowRunOfStage(ctx context.Context, stageRunID string) (string, error) {
	conn := r.conn()
	if conn == nil {
		return "", storageError("AGENT_STORE_UNAVAILABLE", "The agent store is unavailable.", nil)
	}
	var runID string
	err := conn.QueryRowContext(ctx, `SELECT workflow_run_id FROM stage_runs WHERE id = ?`,
		stageRunID).Scan(&runID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", agent.NotFoundError()
		}
		return "", storageError("AGENT_READ_FAILED", "The stage run could not be read.", err)
	}
	return runID, nil
}

// RegisterSkillVersion writes one skill version row.
//
// (skill_key, version) is unique, so registering the same pair twice is a conflict
// rather than a duplicate: two rows for one version would make a run's citation
// ambiguous, which is what section 4.2's version binding is for.
func (r *AgentRepository) RegisterSkillVersion(ctx context.Context, version agent.SkillVersion) error {
	conn := r.conn()
	if conn == nil {
		return storageError("AGENT_STORE_UNAVAILABLE", "The agent store is unavailable.", nil)
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO skill_versions
		(id, skill_key, version, content_hash, manifest_json, content_file_id, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		version.ID, version.SkillKey, version.Version, version.ContentHash,
		version.ManifestJSON, version.ContentFileID, string(version.Status),
		formatTime(version.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return agent.ConflictError("That skill version is already registered.")
		}
		return storageError("AGENT_SKILL_WRITE_FAILED", "The skill version could not be saved.", err)
	}
	return nil
}

// SkillVersionByKey returns the newest active version of a skill key.
func (r *AgentRepository) SkillVersionByKey(ctx context.Context, skillKey string) (agent.SkillVersion, error) {
	conn := r.conn()
	if conn == nil {
		return agent.SkillVersion{}, storageError("AGENT_STORE_UNAVAILABLE", "The agent store is unavailable.", nil)
	}
	var version agent.SkillVersion
	var status, createdAt string
	err := conn.QueryRowContext(ctx, `SELECT id, skill_key, version, content_hash, manifest_json,
		content_file_id, status, created_at FROM skill_versions
		WHERE skill_key = ? AND status = 'active' ORDER BY created_at DESC, id DESC LIMIT 1`,
		skillKey).Scan(&version.ID, &version.SkillKey, &version.Version, &version.ContentHash,
		&version.ManifestJSON, &version.ContentFileID, &status, &createdAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return agent.SkillVersion{}, agent.NotFoundError()
		}
		return agent.SkillVersion{}, storageError("AGENT_READ_FAILED", "The skill version could not be read.", err)
	}
	version.Status = agent.SkillStatus(status)
	version.CreatedAt = parseTime(createdAt)
	return version, nil
}

// splitScopeKey recovers a message's six scope parts from its encoded key.
//
// It is the exact inverse of the runtime's join, and it is here rather than in the
// domain because it is a STORAGE concern: the schema wants the parts in their own
// columns, the domain record has one field, and this is the translation between the
// two. SplitN rather than Split is what keeps a malformed key from shifting a value
// across a column boundary: six pieces is the encoding, and anything after the fifth
// separator belongs inside the last part rather than in a seventh column that does
// not exist.
func splitScopeKey(key string) [6]string {
	pieces := strings.SplitN(key, "|", 6)
	var parts [6]string
	for index := 0; index < 6 && index < len(pieces); index++ {
		parts[index] = pieces[index]
	}
	return parts
}
