package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/graph"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// GraphService serializes a run through its database row. Notifications only
// reduce latency: the coordinator always recovers from committed state.
type GraphService struct {
	Queries           *db.Queries
	TxStarter         TxStarter
	PrepareNode       func(context.Context, pgx.Tx, string, string, graph.Node) (GraphNodeSnapshot, error)
	PrepareRun        func(context.Context, pgx.Tx, string, string, json.RawMessage, *graph.RepositoryInput) (json.RawMessage, error)
	AuthorizeRun      func(context.Context, pgx.Tx, string, string, graph.Definition, map[string]string) error
	AuthorizeSnapshot func(context.Context, pgx.Tx, string, graph.Node, GraphNodeSnapshot) error
	CreateTask        func(context.Context, pgx.Tx, GraphTaskCreateParams) (db.AgentTaskQueue, error)
	NotifyTask        func(context.Context, db.AgentTaskQueue)
	NotifyRun         func(context.Context, GraphRunRecord)
	RequestStop       func(context.Context, string) error
	// ValidateArtifacts verifies workspace ownership, availability and receipts.
	// A result containing artifacts is never accepted without this adapter.
	ValidateArtifacts func(context.Context, pgx.Tx, GraphRunRecord, GraphNodeRunRecord, graph.Result, GraphTaskReceipt) error
	NewRunsEnabled    bool
	AdvanceEnabled    bool
	ExecuteHTTP       func(context.Context, graph.HTTPConfig, map[string]string, json.RawMessage, string) (json.RawMessage, error)
	effectOnce        sync.Once
	effectSlots       chan struct{}
}

func NewGraphService(q *db.Queries, tx TxStarter) *GraphService {
	return &GraphService{Queries: q, TxStarter: tx, NewRunsEnabled: true, AdvanceEnabled: true}
}

func (s *GraphService) authorizeVersion(ctx context.Context, tx pgx.Tx, ws, user string, version GraphVersionRecord, bindings map[string]string) error {
	if err := s.AuthorizeRun(ctx, tx, ws, user, version.Definition, bindings); err != nil {
		return err
	}
	if s.AuthorizeSnapshot != nil {
		return graph.WalkNodes(version.Definition, func(path string, node graph.Node) error {
			if node.Type != graph.NodeTypeAgent && node.Type != graph.NodeTypeScript {
				return nil
			}
			err := s.AuthorizeSnapshot(ctx, tx, ws, node, version.Snapshots[path])
			var failure *GraphError
			if errors.As(err, &failure) {
				contextual := *failure
				contextual.NodePath, contextual.NodeLabel = path, node.Label
				return &contextual
			}
			return err
		})
	}
	return nil
}

type GraphError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	NodePath  string `json:"node_path,omitempty"`
	NodeLabel string `json:"node_label,omitempty"`
	AgentName string `json:"agent_name,omitempty"`
}

func (e *GraphError) Error() string         { return e.Code + ": " + e.Message }
func graphError(code, message string) error { return &GraphError{Code: code, Message: message} }

type GraphNodeSnapshot struct {
	AgentConfig json.RawMessage       `json:"agent_config"`
	Skills      []graph.SkillSnapshot `json:"skills"`
}
type GraphDefinitionRecord struct {
	ID          string               `json:"id"`
	WorkspaceID string               `json:"workspace_id"`
	CreatorID   string               `json:"creator_id"`
	Name        string               `json:"name"`
	Revision    int64                `json:"revision"`
	Definition  graph.Definition     `json:"definition"`
	Versions    []GraphVersionRecord `json:"versions,omitempty"`
	ArchivedAt  *time.Time           `json:"archived_at,omitempty"`
	CreatedAt   time.Time            `json:"created_at"`
	UpdatedAt   time.Time            `json:"updated_at"`
}
type GraphVersionRecord struct {
	ID          string                       `json:"id"`
	WorkspaceID string                       `json:"workspace_id"`
	GraphID     string                       `json:"graph_id"`
	Version     int                          `json:"version"`
	Definition  graph.Definition             `json:"definition"`
	Snapshots   map[string]GraphNodeSnapshot `json:"snapshots"`
	ContentHash string                       `json:"content_hash"`
	CreatedAt   time.Time                    `json:"created_at"`
}
type GraphRunRecord struct {
	SchemaVersion     int                    `json:"schema_version,omitempty"`
	GraphID           string                 `json:"graph_id,omitempty"`
	GraphName         string                 `json:"graph_name,omitempty"`
	Definition        *graph.Definition      `json:"definition,omitempty"`
	Output            json.RawMessage        `json:"output,omitempty"`
	FinishedAt        *time.Time             `json:"finished_at,omitempty"`
	Scopes            []GraphScopeRecord     `json:"scopes,omitempty"`
	Approvals         []GraphApprovalRecord  `json:"approvals,omitempty"`
	TerminationState  string                 `json:"termination_state,omitempty"`
	ID                string                 `json:"id"`
	WorkspaceID       string                 `json:"workspace_id"`
	RootIssueID       string                 `json:"root_issue_id"`
	VersionID         string                 `json:"version_id"`
	PreviousRunID     string                 `json:"previous_run_id,omitempty"`
	InitiatorID       string                 `json:"initiator_id"`
	State             string                 `json:"state"`
	ControlState      string                 `json:"control_state"`
	Revision          int64                  `json:"revision"`
	Input             json.RawMessage        `json:"input"`
	InputHash         string                 `json:"input_hash"`
	RootSnapshot      json.RawMessage        `json:"root_snapshot"`
	RuntimeBindings   map[string]string      `json:"runtime_bindings"`
	Repository        *graph.RepositoryInput `json:"repository,omitempty"`
	Nodes             []GraphNodeRunRecord   `json:"nodes"`
	Artifacts         []graph.Artifact       `json:"artifacts"`
	Approval          *GraphApprovalRecord   `json:"approval,omitempty"`
	Events            []GraphRunEvent        `json:"events"`
	BlockReason       string                 `json:"block_reason,omitempty"`
	TerminationReason string                 `json:"termination_reason,omitempty"`
	CanDetach         bool                   `json:"can_detach"`
	Deadline          *time.Time             `json:"deadline,omitempty"`
	ArchivedAt        *time.Time             `json:"archived_at,omitempty"`
	CreatedAt         time.Time              `json:"created_at"`
	UpdatedAt         time.Time              `json:"updated_at"`
}
type GraphNodeRunRecord struct {
	InstanceKey          string                  `json:"instance_key,omitempty"`
	ScopeID              string                  `json:"scope_id,omitempty"`
	DefinitionPath       string                  `json:"definition_path,omitempty"`
	Iteration            int                     `json:"iteration,omitempty"`
	SelectedPort         string                  `json:"selected_port,omitempty"`
	StartedAt            *time.Time              `json:"started_at,omitempty"`
	FinishedAt           *time.Time              `json:"finished_at,omitempty"`
	WakeAt               *time.Time              `json:"wake_at,omitempty"`
	Effect               *GraphEffectRecord      `json:"effect,omitempty"`
	ID                   string                  `json:"id"`
	NodeKey              string                  `json:"node_key"`
	Label                string                  `json:"label,omitempty"`
	NodeType             string                  `json:"node_type"`
	State                string                  `json:"state"`
	IssueID              string                  `json:"issue_id,omitempty"`
	MaxAttempts          int                     `json:"max_attempts"`
	Input                json.RawMessage         `json:"input,omitempty"`
	InputHash            string                  `json:"input_hash,omitempty"`
	Spec                 *graph.NodeSpec         `json:"spec,omitempty"`
	Attempts             []GraphAttemptRecord    `json:"attempts"`
	Submissions          []GraphSubmissionRecord `json:"submissions"`
	AcceptedSubmissionID string                  `json:"accepted_submission_id,omitempty"`
	Output               json.RawMessage         `json:"output,omitempty"`
	BlockReason          string                  `json:"block_reason,omitempty"`
	CanRetry             bool                    `json:"can_retry"`
	CanResolveBlock      bool                    `json:"can_resolve_block"`
	NextAttemptAt        *time.Time              `json:"next_attempt_at,omitempty"`
	// GrantedAttempts counts the attempts people authorized by clearing this
	// node's block once the published budget was already spent. They sit outside
	// MaxAttempts, which bounds the agent's own repeats: a person deciding the
	// node should continue is not the agent repeating itself, and must not be
	// refused by a budget the platform picked for the agent.
	GrantedAttempts int `json:"granted_attempts,omitempty"`
	// HumanNote is the latest instruction a person left while clearing this
	// node's block. The next attempt receives it and clears it, because a node's
	// frozen input never changes.
	HumanNote *graph.HumanNote `json:"human_note,omitempty"`
}

// attemptBudget is the highest attempt number this node may reach: the published
// max_attempts plus the ones people granted by clearing a block.
func (n GraphNodeRunRecord) attemptBudget() int { return n.MaxAttempts + n.GrantedAttempts }

type GraphAttemptRecord struct {
	TaskID       string                `json:"task_id"`
	AttemptNo    int                   `json:"attempt_no"`
	Generation   int64                 `json:"generation"`
	State        string                `json:"state"`
	Manifest     graph.AttemptManifest `json:"manifest"`
	ManifestHash string                `json:"manifest_hash"`
	Receipt      *GraphTaskReceipt     `json:"receipt,omitempty"`
	StoppedAt    *time.Time            `json:"stopped_at,omitempty"`
	CreatedAt    time.Time             `json:"created_at"`
}
type GraphSubmissionRecord struct {
	ID          string       `json:"id"`
	TaskID      string       `json:"task_id"`
	ContentHash string       `json:"content_hash"`
	Result      graph.Result `json:"result"`
	CreatedAt   time.Time    `json:"created_at"`
}
type GraphApprovalRecord struct {
	NodeRunID       string              `json:"node_run_id,omitempty"`
	ApproverIDs     []string            `json:"approver_ids,omitempty"`
	Mode            string              `json:"mode,omitempty"`
	Quorum          int                 `json:"quorum,omitempty"`
	Votes           []GraphApprovalVote `json:"decisions,omitempty"`
	Candidate       json.RawMessage     `json:"candidate,omitempty"`
	ID              string              `json:"id"`
	ApproverID      string              `json:"approver_id"`
	CandidateDigest string              `json:"candidate_digest"`
	State           string              `json:"state"`
	Revision        int64               `json:"revision"`
	Reason          string              `json:"reason,omitempty"`
	DecidedBy       string              `json:"decided_by,omitempty"`
	CreatedAt       time.Time           `json:"created_at"`
	DecidedAt       *time.Time          `json:"decided_at,omitempty"`
}

type GraphScopeRecord struct {
	ID              string          `json:"id"`
	ParentNodeRunID string          `json:"parent_node_run_id,omitempty"`
	DefinitionPath  string          `json:"definition_path"`
	Iteration       int             `json:"iteration"`
	Input           json.RawMessage `json:"input"`
	Output          json.RawMessage `json:"output,omitempty"`
	State           string          `json:"state"`
}
type GraphApprovalVote struct {
	MemberID  string    `json:"member_id"`
	UserID    string    `json:"user_id"`
	Decision  string    `json:"decision"`
	Reason    string    `json:"reason,omitempty"`
	CreatedAt time.Time `json:"decided_at"`
}
type GraphEffectRecord struct {
	InvocationID string            `json:"invocation_id"`
	ClaimToken   string            `json:"claim_token"`
	State        string            `json:"state"`
	LeaseUntil   time.Time         `json:"lease_until"`
	Attempts     int               `json:"attempts"`
	Headers      map[string]string `json:"headers,omitempty"`
	Body         json.RawMessage   `json:"body,omitempty"`
}
type GraphRunEvent struct {
	ID        string    `json:"id"`
	Sequence  int64     `json:"sequence"`
	Type      string    `json:"type"`
	ActorID   string    `json:"actor_id,omitempty"`
	Message   string    `json:"message,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
type GraphStartParams struct {
	VersionID       string                 `json:"version_id"`
	RootIssueID     string                 `json:"root_issue_id"`
	PreviousRunID   string                 `json:"previous_run_id,omitempty"`
	Input           json.RawMessage        `json:"input"`
	RuntimeBindings map[string]string      `json:"runtime_bindings"`
	Repository      *graph.RepositoryInput `json:"repository,omitempty"`
	Deadline        *time.Time             `json:"deadline,omitempty"`
	IdempotencyKey  string                 `json:"idempotency_key"`
}
type GraphControlParams struct {
	PreviousApproverID string `json:"previous_approver_id,omitempty"`
	ExpectedRevision   int64  `json:"expected_revision"`
	IdempotencyKey     string `json:"idempotency_key"`
	NodeRunID          string `json:"node_run_id,omitempty"`
	Reason             string `json:"reason,omitempty"`
	ApproverID         string `json:"approver_id,omitempty"`
}
type GraphApprovalParams struct {
	ExpectedRevision int64  `json:"expected_revision"`
	IdempotencyKey   string `json:"idempotency_key"`
	ApprovalID       string `json:"approval_id"`
	CandidateDigest  string `json:"candidate_digest"`
	Decision         string `json:"decision"`
	Reason           string `json:"reason,omitempty"`
}
type GraphTaskCreateParams struct {
	WorkspaceID, RunID, NodeRunID, TaskID, IssueID, AgentID, RuntimeID, InitiatorID string
	AttemptNo                                                                       int
	Generation                                                                      int64
	PreviousTaskID                                                                  string
	Manifest                                                                        graph.AttemptManifest
	ManifestJSON                                                                    json.RawMessage
	ManifestHash                                                                    string
}
type GraphTaskReceipt struct {
	Identity       graph.AttemptIdentity `json:"identity"`
	SubmissionID   string                `json:"submission_id,omitempty"`
	ProviderExit   string                `json:"provider_exit"`
	DeliveryStatus string                `json:"delivery_status"`
	FailureReason  string                `json:"failure_reason,omitempty"`
	Retryable      bool                  `json:"retryable"`
	ExecutorID     string                `json:"executor_id"`
	StoppedAt      time.Time             `json:"stopped_at"`
	Artifacts      json.RawMessage       `json:"artifacts,omitempty"`
}
type GraphTaskContext struct {
	IssueID      string                  `json:"issue_id"`
	WorkspaceID  string                  `json:"workspace_id"`
	RunID        string                  `json:"run_id"`
	NodeRunID    string                  `json:"node_run_id"`
	State        string                  `json:"state"`
	ControlState string                  `json:"control_state"`
	Manifest     graph.AttemptManifest   `json:"manifest"`
	ManifestHash string                  `json:"manifest_hash"`
	RootSnapshot json.RawMessage         `json:"root_snapshot"`
	Submissions  []GraphSubmissionRecord `json:"submissions"`
	Artifacts    []graph.Artifact        `json:"artifacts,omitempty"`
}

func graphJSON(v any) ([]byte, error) { return json.Marshal(v) }
func graphHash(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.UseNumber()
	if err = decoder.Decode(&value); err != nil {
		return "", err
	}
	b, err = json.Marshal(value)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
func graphUUIDs(ids ...string) error {
	for _, id := range ids {
		if _, err := uuid.Parse(id); err != nil {
			return graphError("invalid_id", "invalid Graph resource UUID")
		}
	}
	return nil
}
func graphScan(row pgx.Row, value any) error {
	var data []byte
	if err := row.Scan(&data); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return graphError("not_found", "Graph resource not found")
		}
		return err
	}
	return json.Unmarshal(data, value)
}
func graphMember(ctx context.Context, tx pgx.Tx, workspaceID, userID string) (string, string, error) {
	if err := graphUUIDs(workspaceID, userID); err != nil {
		return "", "", err
	}
	if err := graphLockWorkspace(ctx, tx, workspaceID); err != nil {
		return "", "", err
	}
	var memberID, role string
	err := tx.QueryRow(ctx, `SELECT id::text, role FROM member WHERE workspace_id=$1 AND user_id=$2`, workspaceID, userID).Scan(&memberID, &role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", graphError("forbidden", "workspace membership required")
	}
	return memberID, role, err
}

func graphLockWorkspace(ctx context.Context, tx pgx.Tx, workspaceID string) error {
	var id string
	err := tx.QueryRow(ctx, `SELECT id::text FROM workspace WHERE id=$1 FOR KEY SHARE`, workspaceID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return graphError("not_found", "workspace not found")
	}
	return err
}

// The advisory command lock also serializes duplicate requests before a resource
// exists. A reused key with different content cannot silently replay old work.
func graphCommand(ctx context.Context, tx pgx.Tx, ws, key string, request any, out any) (bool, string, error) {
	if strings.TrimSpace(key) == "" || len(key) > 200 {
		return false, "", graphError("invalid_idempotency_key", "an idempotency key of 1 to 200 characters is required")
	}
	hash, err := graphHash(request)
	if err != nil {
		return false, "", err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, ws+":"+key); err != nil {
		return false, "", err
	}
	var oldHash string
	var response []byte
	err = tx.QueryRow(ctx, `SELECT request_hash,response FROM graph_command WHERE workspace_id=$1 AND command_key=$2`, ws, key).Scan(&oldHash, &response)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, hash, nil
	}
	if err != nil {
		return false, "", err
	}
	if oldHash != hash {
		return false, "", graphError("idempotency_conflict", "idempotency key was used for different content")
	}
	return true, hash, json.Unmarshal(response, out)
}
func graphSaveCommand(ctx context.Context, tx pgx.Tx, ws, key, hash string, value any) error {
	b, err := graphJSON(value)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO graph_command(workspace_id,command_key,request_hash,response) VALUES($1,$2,$3,$4)`, ws, key, hash, b)
	return err
}
func graphLoadRun(ctx context.Context, tx pgx.Tx, ws, id string, lock bool) (GraphRunRecord, error) {
	var run GraphRunRecord
	if err := graphUUIDs(ws, id); err != nil {
		return run, err
	}
	query := `SELECT document FROM graph_run WHERE workspace_id=$1 AND id=$2`
	if lock {
		query += " FOR UPDATE"
	}
	err := graphScan(tx.QueryRow(ctx, query, ws, id), &run)
	return run, err
}
func graphLoadVersion(ctx context.Context, tx pgx.Tx, ws, id string) (GraphVersionRecord, error) {
	var v GraphVersionRecord
	if err := graphUUIDs(ws, id); err != nil {
		return v, err
	}
	err := graphScan(tx.QueryRow(ctx, `SELECT document FROM graph_version WHERE workspace_id=$1 AND id=$2`, ws, id), &v)
	return v, err
}
func graphTerminal(state string) bool { return graph.IsTerminalRun(graph.RunState(state)) }
func graphEvent(run *GraphRunRecord, kind, actor, message string) {
	sequence := int64(1)
	if len(run.Events) > 0 {
		sequence = run.Events[len(run.Events)-1].Sequence + 1
	}
	if len(message) > 2048 {
		message = message[:2048]
	}
	run.Events = append(run.Events, GraphRunEvent{ID: uuid.NewString(), Sequence: sequence, Type: kind, ActorID: actor, Message: message, CreatedAt: time.Now().UTC()})
}
func graphPersistRun(ctx context.Context, tx pgx.Tx, run *GraphRunRecord) error {
	if _, err := tx.Exec(ctx, `SELECT set_config('multica.graph_run_id',$1,true)`, run.ID); err != nil {
		return err
	}
	run.UpdatedAt = time.Now().UTC()
	run.Revision++
	events := run.Events
	if run.SchemaVersion == 2 && len(run.Events) > 500 {
		run.Events = run.Events[len(run.Events)-500:]
	}
	b, err := graphJSON(run)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE graph_run SET state=$3,control_state=$4,revision=$5,document=$6,updated_at=$7,archived_at=$8 WHERE workspace_id=$1 AND id=$2`, run.WorkspaceID, run.ID, run.State, run.ControlState, run.Revision, b, run.UpdatedAt, run.ArchivedAt)
	if err != nil {
		return err
	}
	for _, n := range run.Nodes {
		nb, e := graphJSON(n)
		if e != nil {
			return e
		}
		var taskID any
		if len(n.Attempts) > 0 {
			taskID = n.Attempts[len(n.Attempts)-1].TaskID
		}
		if _, err = tx.Exec(ctx, `UPDATE graph_node_run SET state=$3,current_task_id=$4,input_hash=$5,document=$6 WHERE workspace_id=$1 AND id=$2`, run.WorkspaceID, n.ID, n.State, taskID, n.InputHash, nb); err != nil {
			return err
		}
		if n.IssueID != "" {
			if _, err = tx.Exec(ctx, `UPDATE issue SET status=$3,updated_at=now() WHERE workspace_id=$1 AND id=$2 AND EXISTS(SELECT 1 FROM graph_issue_owner o WHERE o.workspace_id=$1 AND o.issue_id=$2 AND o.run_id=$4 AND o.node_run_id=$5)`, run.WorkspaceID, n.IssueID, graphNodeIssueStatus(n.State), run.ID, n.ID); err != nil {
				return err
			}
		}
	}
	for _, event := range events {
		eb, e := graphJSON(event)
		if e != nil {
			return e
		}
		if _, err = tx.Exec(ctx, `INSERT INTO graph_run_event(id,workspace_id,run_id,sequence,document) VALUES($1,$2,$3,$4,$5) ON CONFLICT(run_id,sequence) DO NOTHING`, event.ID, run.WorkspaceID, run.ID, event.Sequence, eb); err != nil {
			return err
		}
	}
	if run.SchemaVersion == 2 {
		for _, approval := range run.Approvals {
			ab, e := graphJSON(approval)
			if e != nil {
				return e
			}
			if _, err = tx.Exec(ctx, `INSERT INTO graph_approval(id,workspace_id,run_id,node_run_id,state,document) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(id) DO UPDATE SET state=EXCLUDED.state,document=EXCLUDED.document`, approval.ID, run.WorkspaceID, run.ID, approval.NodeRunID, approval.State, ab); err != nil {
				return err
			}
		}
	} else if run.Approval != nil {
		ab, e := graphJSON(run.Approval)
		if e != nil {
			return e
		}
		if _, err = tx.Exec(ctx, `INSERT INTO graph_approval(id,workspace_id,run_id,state,document) VALUES($1,$2,$3,$4,$5) ON CONFLICT(id) DO UPDATE SET state=EXCLUDED.state,document=EXCLUDED.document`, run.Approval.ID, run.WorkspaceID, run.ID, run.Approval.State, ab); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE issue SET status=$3,updated_at=now() WHERE workspace_id=$1 AND id=$2 AND EXISTS(SELECT 1 FROM graph_issue_owner o WHERE o.workspace_id=$1 AND o.issue_id=$2 AND o.run_id=$4 AND o.node_run_id IS NULL)`, run.WorkspaceID, run.RootIssueID, graphRunIssueStatus(run.State), run.ID)
	return err
}
func graphRunIssueStatus(state string) string {
	switch state {
	case "waiting_approval":
		return "in_review"
	case "blocked", "failed":
		return "blocked"
	case "succeeded":
		return "done"
	case "rejected":
		return "todo"
	case "cancelled":
		return "cancelled"
	default:
		return "in_progress"
	}
}
func graphNodeIssueStatus(state string) string {
	switch state {
	case "active", "validating":
		return "in_progress"
	case "blocked", "failed":
		return "blocked"
	case "succeeded":
		return "done"
	case "cancelled", "skipped":
		return "cancelled"
	default:
		return "todo"
	}
}
func graphOperation(ctx context.Context, tx pgx.Tx, run GraphRunRecord, key, kind string) error {
	_, err := tx.Exec(ctx, `INSERT INTO graph_operation(operation_key,workspace_id,run_id,kind) VALUES($1,$2,$3,$4) ON CONFLICT(operation_key) DO UPDATE SET consumed_at=NULL,available_at=now()`, key, run.WorkspaceID, run.ID, kind)
	return err
}
func graphFinish(run *GraphRunRecord, state, reason string) {
	now := time.Now().UTC()
	run.FinishedAt = &now
	run.State = state
	run.BlockReason = ""
	run.TerminationReason = reason
	for i := range run.Scopes {
		if run.Scopes[i].State == "active" {
			run.Scopes[i].State = state
		}
	}
	for i := range run.Nodes {
		n := &run.Nodes[i]
		if n.State != "succeeded" && n.State != "rejected" && n.State != "failed" && n.State != "skipped" {
			n.State = "cancelled"
		}
		if n.FinishedAt == nil {
			n.FinishedAt = &now
		}
	}
	if run.Approval != nil && run.Approval.State == "pending" {
		run.Approval.State = "invalidated"
	}
	for i := range run.Approvals {
		if run.Approvals[i].State == "pending" {
			run.Approvals[i].State = "invalidated"
		}
	}
	graphEvent(run, "run."+state, "", reason)
}

// graphRetryAllowed reports whether a blocked node may take another attempt on
// its published budget. This is the gate for the agent's own repeats, so it is
// where max_attempts applies.
func graphRetryAllowed(node GraphNodeRunRecord) error {
	return graphUnblockAllowed(node, false)
}

// graphResolveBlockAllowed reports whether a person may clear a blocked node.
//
// People are deliberately not bound by the attempt budget. max_attempts bounds
// how often the agent repeats by itself; clearing a block is a human decision
// about a node the agent has stopped on and asked about, and refusing it leaves
// the run with no way forward at all - the human sees the reason form, the
// button is gone, and no server-side path produces another attempt. The attempt
// a person unlocks is therefore granted outside the published budget; the
// control action records the grant before the node becomes ready again.
func graphResolveBlockAllowed(node GraphNodeRunRecord) error {
	return graphUnblockAllowed(node, true)
}

func graphUnblockAllowed(node GraphNodeRunRecord, human bool) error {
	if node.AcceptedSubmissionID != "" || node.State != "blocked" {
		return graphError("invalid_state", "only a blocked, unaccepted node can retry")
	}
	if !human && len(node.Attempts) >= node.attemptBudget() {
		return graphError("budget_exhausted", "node attempt budget exhausted")
	}
	if len(node.Attempts) > 0 {
		a := node.Attempts[len(node.Attempts)-1]
		if a.State != "completed" && a.State != "failed" && a.State != "cancelled" {
			return graphError("invalid_state", "previous task is not terminal")
		}
		// A person clearing the block supplies the missing stop themselves; see
		// graphConfirmStoppedTx. Making the receipt a precondition here refused
		// the one action the platform's own execution_unknown block exists to
		// ask for, leaving the run unable to advance or even be cancelled.
		if a.StoppedAt == nil && !human {
			return graphError("execution_unknown", "previous process must be confirmed stopped")
		}
	}
	return nil
}

// graphGrantUnblock records what a person decided when they cleared a node's
// block, so both halves of that decision reach the attempt it unlocks.
//
// The attempt: when the published budget is already spent, the attempt this
// unblocks has to be granted on top of it, or the scheduler's own budget check
// would refuse to create it and the run would fail instead of continuing.
//
// The reason: a node's input is frozen for the node's whole life, so the attempt
// that follows has no other way to learn what the person said. It rides on the
// manifest and is cleared when that attempt is created.
func graphGrantUnblock(n *GraphNodeRunRecord, member, reason string) {
	if len(n.Attempts) >= n.MaxAttempts {
		n.GrantedAttempts++
	}
	n.HumanNote = nil
	// An empty reason is a legitimate unblock - the person reacted to something
	// outside the platform - so it clears the previous note rather than
	// replaying a stale instruction into the next attempt.
	if note := strings.TrimSpace(reason); note != "" {
		n.HumanNote = &graph.HumanNote{Reason: note, MemberID: member, At: time.Now().UTC()}
	}
}
func graphApprovalAllowed(run GraphRunRecord, memberID string, p GraphApprovalParams) error {
	if run.Deadline != nil && !run.Deadline.After(time.Now()) {
		return graphError("deadline_exceeded", "run deadline reached")
	}
	if run.State != "waiting_approval" || run.ControlState != "run" {
		return graphError("invalid_state", "approval requires an unpaused waiting run")
	}
	a := run.Approval
	if a == nil || a.ID != p.ApprovalID || a.State != "pending" || a.CandidateDigest != p.CandidateDigest {
		return graphError("stale_approval", "approval request or candidate changed")
	}
	if a.ApproverID != memberID {
		return graphError("forbidden", "only the designated approver can decide")
	}
	if p.Decision != "approved" && p.Decision != "rejected" {
		return graphError("invalid_decision", "decision must be approved or rejected")
	}
	return nil
}
func graphValidateReceipt(expected graph.AttemptIdentity, previous *GraphTaskReceipt, receipt GraphTaskReceipt) error {
	if err := graph.ValidateIdentity(expected, receipt.Identity); err != nil {
		return graphError("stale_attempt", err.Error())
	}
	if receipt.StoppedAt.IsZero() || receipt.ExecutorID == "" {
		return graphError("execution_unknown", "executor identity and process stop time are required")
	}
	if receipt.StoppedAt.After(time.Now().Add(5 * time.Minute)) {
		return graphError("invalid_receipt", "stop time is in the future")
	}
	if previous != nil {
		before, err := graphHash(previous)
		if err != nil {
			return err
		}
		after, err := graphHash(receipt)
		if err != nil {
			return err
		}
		if before != after {
			return graphError("receipt_conflict", "terminal receipt is immutable")
		}
	}
	return nil
}
