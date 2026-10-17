package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/graph"
)

type GraphTriggerConfig struct {
	Cron            string   `json:"cron,omitempty"`
	Timezone        string   `json:"timezone,omitempty"`
	ProjectID       string   `json:"project_id,omitempty"`
	Statuses        []string `json:"statuses,omitempty"`
	Events          []string `json:"events,omitempty"`
	RootTitle       string   `json:"root_title,omitempty"`
	RootDescription string   `json:"root_description,omitempty"`
}
type GraphTriggerWrite struct {
	Name             string                 `json:"name"`
	VersionID        string                 `json:"version_id"`
	Kind             string                 `json:"kind"`
	Enabled          bool                   `json:"enabled"`
	Config           GraphTriggerConfig     `json:"config"`
	Input            json.RawMessage        `json:"input"`
	RuntimeBindings  map[string]string      `json:"runtime_bindings"`
	Repository       *graph.RepositoryInput `json:"repository,omitempty"`
	ExpectedRevision int64                  `json:"expected_revision,omitempty"`
	IdempotencyKey   string                 `json:"idempotency_key"`
}
type GraphTrigger struct {
	ID              string                 `json:"id"`
	WorkspaceID     string                 `json:"workspace_id"`
	GraphID         string                 `json:"graph_id"`
	CreatorID       string                 `json:"creator_id"`
	Name            string                 `json:"name"`
	VersionID       string                 `json:"version_id"`
	Kind            string                 `json:"kind"`
	Enabled         bool                   `json:"enabled"`
	Config          GraphTriggerConfig     `json:"config"`
	Input           json.RawMessage        `json:"input"`
	RuntimeBindings map[string]string      `json:"runtime_bindings"`
	Repository      *graph.RepositoryInput `json:"repository,omitempty"`
	Revision        int64                  `json:"revision"`
	NextFireAt      *time.Time             `json:"next_fire_at,omitempty"`
	LastError       string                 `json:"last_error,omitempty"`
	WebhookURL      string                 `json:"webhook_url,omitempty"`
	Secret          string                 `json:"secret,omitempty"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
}
type GraphTriggerEvent struct {
	ID        string    `json:"id"`
	State     string    `json:"state"`
	RunID     string    `json:"run_id,omitempty"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func graphTriggerSecret() (string, string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", "", err
	}
	secret := hex.EncodeToString(raw[:])
	hash := sha256.Sum256([]byte(secret))
	return secret, hex.EncodeToString(hash[:]), nil
}
func graphTriggerOwner(ctx context.Context, tx pgx.Tx, ws, user, creator string) error {
	_, role, err := graphMember(ctx, tx, ws, user)
	if err != nil {
		return err
	}
	if user != creator && role != "owner" && role != "admin" {
		return graphError("forbidden", "only the trigger creator or workspace administrator can manage this trigger")
	}
	return nil
}

// Public ingress and background workers have no member request to establish the
// workspace fence. Acquire it before any trigger/event row lock, as teardown
// locks the workspace first and then removes all Graph-owned rows.
func graphLockTriggerWorkspace(ctx context.Context, tx pgx.Tx, id string) error {
	var workspaceID string
	if err := tx.QueryRow(ctx, `SELECT workspace_id FROM graph_trigger WHERE id=$1`, id).Scan(&workspaceID); err != nil {
		return err
	}
	return graphLockWorkspace(ctx, tx, workspaceID)
}
func graphLoadTrigger(ctx context.Context, tx pgx.Tx, ws, id string, lock bool) (GraphTrigger, error) {
	var result GraphTrigger
	if err := graphUUIDs(id); err != nil {
		return result, err
	}
	sql := `SELECT document,next_fire_at,last_error FROM graph_trigger WHERE workspace_id=$1 AND id=$2 AND deleted_at IS NULL`
	if lock {
		sql += ` FOR UPDATE`
	}
	var raw []byte
	var next *time.Time
	var last string
	if err := tx.QueryRow(ctx, sql, ws, id).Scan(&raw, &next, &last); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return result, graphError("not_found", "Graph trigger not found")
		}
		return result, err
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, err
	}
	result.NextFireAt = next
	result.LastError = last
	return result, nil
}
func (s *GraphService) validateTrigger(ctx context.Context, tx pgx.Tx, ws, user, graphID string, p GraphTriggerWrite) (GraphVersionRecord, error) {
	var zero GraphVersionRecord
	if strings.TrimSpace(p.Name) == "" || len(p.Name) > 200 || len(p.Input) > 65536 || len(p.Config.RootTitle) > 300 || len(p.Config.RootDescription) > 32000 || len(p.Config.Statuses) > 16 || len(p.Config.Events) > 2 {
		return zero, graphError("invalid_request", "invalid trigger name or payload limits")
	}
	if p.Kind != "schedule" && p.Kind != "webhook" && p.Kind != "task_event" {
		return zero, graphError("invalid_request", "unknown trigger kind")
	}
	if p.Kind == "task_event" {
		if p.Config.ProjectID == "" || len(p.Config.Events) == 0 {
			return zero, graphError("invalid_request", "task event triggers require a project and event selection")
		}
		for _, event := range p.Config.Events {
			if event != "created" && event != "status_changed" {
				return zero, graphError("invalid_request", "unknown issue event")
			}
		}
	} else if strings.TrimSpace(p.Config.RootTitle) == "" {
		return zero, graphError("invalid_request", "a title for the new root task is required")
	}
	for _, status := range p.Config.Statuses {
		if len(status) == 0 || len(status) > 64 {
			return zero, graphError("invalid_request", "invalid task status filter")
		}
	}
	if p.Config.ProjectID != "" {
		if err := graphUUIDs(p.Config.ProjectID); err != nil {
			return zero, err
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM project WHERE workspace_id=$1 AND id=$2)`, ws, p.Config.ProjectID).Scan(&exists); err != nil {
			return zero, err
		}
		if !exists {
			return zero, graphError("not_found", "trigger project not found")
		}
	}
	if p.Kind == "schedule" {
		if len(strings.Fields(p.Config.Cron)) != 5 || strings.Contains(p.Config.Cron, "TZ=") {
			return zero, graphError("invalid_request", "schedule requires a five-field cron expression")
		}
		next, err := NextOccurrenceAfterUTC(p.Config.Cron, p.Config.Timezone, time.Now())
		if err != nil || next.IsZero() {
			return zero, graphError("invalid_request", "invalid schedule or IANA timezone")
		}
	}
	version, err := graphLoadVersion(ctx, tx, ws, p.VersionID)
	if err != nil {
		return zero, err
	}
	if version.GraphID != graphID {
		return zero, graphError("invalid_request", "published version belongs to a different Graph")
	}
	var input map[string]json.RawMessage
	if err = json.Unmarshal(p.Input, &input); err != nil || input == nil {
		return zero, graphError("invalid_request", "trigger input must be a JSON object")
	}
	if p.Kind != "webhook" {
		if err = graph.ValidateJSON(version.Definition.InputSchema, p.Input); err != nil {
			return zero, graphError("input_invalid", err.Error())
		}
	}
	err = graph.WalkNodes(version.Definition, func(path string, node graph.Node) error {
		if node.Type != graph.NodeTypeAgent && node.Type != graph.NodeTypeScript {
			return nil
		}
		id := p.RuntimeBindings[path]
		if graphUUIDs(id) != nil {
			return graphError("runtime_required", "every executable node requires a runtime binding")
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agent_runtime WHERE workspace_id=$1 AND id=$2)`, ws, id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return graphError("not_found", "runtime does not belong to this workspace")
		}
		if graph.RequiresRepository(node.ExecutionMode) && p.Repository == nil {
			return graphError("repository_required", "code triggers require a frozen repository and commit")
		}
		return nil
	})
	if err != nil {
		return zero, err
	}
	if p.Enabled {
		if s.AuthorizeRun == nil {
			return zero, graphError("capability_missing", "Graph execution adapters unavailable")
		}
		if err = s.authorizeVersion(ctx, tx, ws, user, version, p.RuntimeBindings); err != nil {
			return zero, err
		}
	}
	return version, nil
}

func (s *GraphService) SaveTrigger(ctx context.Context, ws, user, graphID, id string, p GraphTriggerWrite) (GraphTrigger, error) {
	var result GraphTrigger
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if _, _, err = graphMember(ctx, tx, ws, user); err != nil {
		return result, err
	}
	replay, hash, err := graphCommand(ctx, tx, ws, p.IdempotencyKey, []any{"trigger.save", user, graphID, id, p}, &result)
	if err != nil {
		return result, err
	}
	if replay {
		return result, tx.Commit(ctx)
	}
	var definition GraphDefinitionRecord
	if err = graphScan(tx.QueryRow(ctx, `SELECT document FROM graph_definition WHERE workspace_id=$1 AND id=$2 AND archived_at IS NULL`, ws, graphID), &definition); err != nil {
		return result, err
	}
	if err = graphTriggerOwner(ctx, tx, ws, user, definition.CreatorID); err != nil {
		return result, err
	}
	if len(p.Input) == 0 {
		p.Input = json.RawMessage(`{}`)
	}
	if _, err = s.validateTrigger(ctx, tx, ws, user, graphID, p); err != nil {
		return result, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT now()`).Scan(&now); err != nil {
		return result, err
	}
	secret, secretHash := "", ""
	if id == "" {
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM graph_trigger WHERE workspace_id=$1 AND deleted_at IS NULL`, ws).Scan(&count); err != nil {
			return result, err
		}
		if count >= 100 {
			return result, graphError("limit_exceeded", "workspace trigger limit reached")
		}
		result = GraphTrigger{ID: uuid.NewString(), WorkspaceID: ws, GraphID: graphID, CreatorID: user, Revision: 1, CreatedAt: now}
		if p.Kind == "webhook" {
			secret, secretHash, err = graphTriggerSecret()
			if err != nil {
				return result, err
			}
		}
	} else {
		result, err = graphLoadTrigger(ctx, tx, ws, id, true)
		if err != nil {
			return result, err
		}
		if result.GraphID != graphID || result.Kind != p.Kind {
			return result, graphError("invalid_request", "trigger Graph and kind cannot change")
		}
		if err = graphTriggerOwner(ctx, tx, ws, user, result.CreatorID); err != nil {
			return result, err
		}
		if result.Revision != p.ExpectedRevision {
			return result, graphError("revision_conflict", "trigger changed; reload before saving")
		}
		result.Revision++
	}
	result.Name = strings.TrimSpace(p.Name)
	result.VersionID = p.VersionID
	result.Kind = p.Kind
	result.Enabled = p.Enabled
	result.Config = p.Config
	result.Input = p.Input
	result.RuntimeBindings = p.RuntimeBindings
	result.Repository = p.Repository
	result.UpdatedAt = now
	result.LastError = ""
	result.NextFireAt = nil
	if result.Kind == "schedule" && result.Enabled {
		next, _ := NextOccurrenceAfterUTC(p.Config.Cron, p.Config.Timezone, now)
		result.NextFireAt = &next
	}
	if result.Kind == "webhook" {
		result.WebhookURL = "/api/graph-triggers/" + result.ID + "/invoke"
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	if id == "" {
		_, err = tx.Exec(ctx, `INSERT INTO graph_trigger(id,workspace_id,graph_id,version_id,creator_id,kind,enabled,revision,document,secret_hash,next_fire_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, result.ID, ws, graphID, result.VersionID, result.CreatorID, result.Kind, result.Enabled, result.Revision, raw, secretHash, result.NextFireAt)
	} else {
		_, err = tx.Exec(ctx, `UPDATE graph_trigger SET version_id=$3,enabled=$4,revision=$5,document=$6,next_fire_at=$7,last_error='',updated_at=$8 WHERE workspace_id=$1 AND id=$2`, ws, id, result.VersionID, result.Enabled, result.Revision, raw, result.NextFireAt, now)
	}
	if err != nil {
		return result, err
	}
	if err = graphSaveCommand(ctx, tx, ws, p.IdempotencyKey, hash, result); err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, err
	}
	result.Secret = secret
	return result, nil
}

func (s *GraphService) ListTriggers(ctx context.Context, ws, user, graphID string) ([]GraphTrigger, error) {
	result := []GraphTrigger{}
	if err := graphUUIDs(graphID); err != nil {
		return nil, err
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, _, err = graphMember(ctx, tx, ws, user); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT document,next_fire_at,last_error FROM graph_trigger WHERE workspace_id=$1 AND graph_id=$2 AND deleted_at IS NULL ORDER BY created_at DESC`, ws, graphID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var next *time.Time
		var last string
		if err = rows.Scan(&raw, &next, &last); err != nil {
			return nil, err
		}
		var item GraphTrigger
		if err = json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		item.NextFireAt = next
		item.LastError = last
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *GraphService) RotateTriggerSecret(ctx context.Context, ws, user, id string, p GraphControlParams) (GraphTrigger, error) {
	return s.controlTrigger(ctx, ws, user, id, "rotate-secret", p)
}
func (s *GraphService) DeleteTrigger(ctx context.Context, ws, user, id string, p GraphControlParams) error {
	_, err := s.controlTrigger(ctx, ws, user, id, "delete", p)
	return err
}
func (s *GraphService) controlTrigger(ctx context.Context, ws, user, id, action string, p GraphControlParams) (GraphTrigger, error) {
	var result GraphTrigger
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if _, _, err = graphMember(ctx, tx, ws, user); err != nil {
		return result, err
	}
	replay, hash, err := graphCommand(ctx, tx, ws, p.IdempotencyKey, []any{"trigger." + action, user, id, p}, &result)
	if err != nil {
		return result, err
	}
	if replay {
		return result, tx.Commit(ctx)
	}
	result, err = graphLoadTrigger(ctx, tx, ws, id, true)
	if err != nil {
		return result, err
	}
	if err = graphTriggerOwner(ctx, tx, ws, user, result.CreatorID); err != nil {
		return result, err
	}
	if result.Revision != p.ExpectedRevision {
		return result, graphError("revision_conflict", "trigger changed; reload before editing")
	}
	secret, secretHash := "", ""
	if action == "rotate-secret" {
		if result.Kind != "webhook" {
			return result, graphError("invalid_request", "only webhook triggers have secrets")
		}
		secret, secretHash, err = graphTriggerSecret()
		if err != nil {
			return result, err
		}
	} else {
		result.Enabled = false
		result.NextFireAt = nil
	}
	result.Revision++
	result.UpdatedAt = time.Now().UTC()
	raw, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	_, err = tx.Exec(ctx, `UPDATE graph_trigger SET document=$3,revision=$4,enabled=$5,next_fire_at=$6,secret_hash=CASE WHEN $7='' THEN secret_hash ELSE $7 END,deleted_at=CASE WHEN $8='delete' THEN now() ELSE deleted_at END,updated_at=now() WHERE workspace_id=$1 AND id=$2`, ws, id, raw, result.Revision, result.Enabled, result.NextFireAt, secretHash, action)
	if err != nil {
		return result, err
	}
	if err = graphSaveCommand(ctx, tx, ws, p.IdempotencyKey, hash, result); err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, err
	}
	result.Secret = secret
	return result, nil
}

func (s *GraphService) InvokeTrigger(ctx context.Context, id, secret, key string, overrides json.RawMessage) (GraphTriggerEvent, error) {
	var result GraphTriggerEvent
	if graphUUIDs(id) != nil || len(secret) != 64 {
		return result, graphError("forbidden", "invalid webhook credentials")
	}
	if len(key) < 1 || len(key) > 128 || strings.IndexFunc(key, func(r rune) bool { return r < 33 || r > 126 }) >= 0 {
		return result, graphError("invalid_request", "Idempotency-Key must contain 1 to 128 visible ASCII characters")
	}
	if len(overrides) > 65536 {
		return result, graphError("invalid_request", "webhook input exceeds 65536 bytes")
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if err = graphLockTriggerWorkspace(ctx, tx, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return result, graphError("forbidden", "invalid webhook credentials")
		}
		return result, err
	}
	var raw []byte
	var expected string
	var enabled bool
	err = tx.QueryRow(ctx, `SELECT document,secret_hash,enabled FROM graph_trigger WHERE id=$1 AND kind='webhook' AND deleted_at IS NULL FOR UPDATE`, id).Scan(&raw, &expected, &enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, graphError("forbidden", "invalid webhook credentials")
	}
	if err != nil {
		return result, err
	}
	sum := sha256.Sum256([]byte(secret))
	actual := hex.EncodeToString(sum[:])
	if subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) != 1 {
		return result, graphError("forbidden", "invalid webhook credentials")
	}
	if !enabled || !s.NewRunsEnabled {
		return result, graphError("feature_disabled", "trigger is disabled")
	}
	var trigger GraphTrigger
	if err = json.Unmarshal(raw, &trigger); err != nil {
		return result, err
	}
	var input, extra map[string]json.RawMessage
	if err = json.Unmarshal(trigger.Input, &input); err != nil {
		return result, err
	}
	if err = json.Unmarshal(overrides, &extra); err != nil || extra == nil {
		return result, graphError("invalid_request", "webhook input must be an object")
	}
	for name, value := range extra {
		input[name] = value
	}
	merged, err := json.Marshal(input)
	if err != nil {
		return result, err
	}
	version, err := graphLoadVersion(ctx, tx, trigger.WorkspaceID, trigger.VersionID)
	if err != nil {
		return result, err
	}
	if err = graph.ValidateJSON(version.Definition.InputSchema, merged); err != nil {
		return result, graphError("input_invalid", err.Error())
	}
	result, err = graphEnqueueTrigger(ctx, tx, trigger, "webhook:"+key, merged, "")
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func graphEnqueueTrigger(ctx context.Context, tx pgx.Tx, trigger GraphTrigger, key string, input json.RawMessage, rootID string) (GraphTriggerEvent, error) {
	var result GraphTriggerEvent
	hash, err := graphHash(input)
	if err != nil {
		return result, err
	}
	var oldHash string
	err = tx.QueryRow(ctx, `SELECT id,state,COALESCE(run_id::text,''),error,created_at,updated_at,request_hash FROM graph_trigger_event WHERE trigger_id=$1 AND event_key=$2`, trigger.ID, key).Scan(&result.ID, &result.State, &result.RunID, &result.Error, &result.CreatedAt, &result.UpdatedAt, &oldHash)
	if err == nil {
		if oldHash != hash {
			return result, graphError("idempotency_conflict", "event key was used with different input")
		}
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	if rootID == "" {
		rootID = uuid.NewString()
	}
	result.ID = uuid.NewString()
	result.State = "pending"
	snapshot, err := json.Marshal(trigger)
	if err != nil {
		return result, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO graph_trigger_event(id,workspace_id,trigger_id,event_key,request_hash,snapshot,input,root_issue_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING created_at,updated_at`, result.ID, trigger.WorkspaceID, trigger.ID, key, hash, snapshot, input, rootID).Scan(&result.CreatedAt, &result.UpdatedAt)
	return result, err
}

func (s *GraphService) ListTriggerEvents(ctx context.Context, ws, user, id string) ([]GraphTriggerEvent, error) {
	result := []GraphTriggerEvent{}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, _, err = graphMember(ctx, tx, ws, user); err != nil {
		return nil, err
	}
	if _, err = graphLoadTrigger(ctx, tx, ws, id, false); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id,state,COALESCE(run_id::text,''),error,created_at,updated_at FROM graph_trigger_event WHERE workspace_id=$1 AND trigger_id=$2 ORDER BY created_at DESC,id DESC LIMIT 50`, ws, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var event GraphTriggerEvent
		if err = rows.Scan(&event.ID, &event.State, &event.RunID, &event.Error, &event.CreatedAt, &event.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	return result, rows.Err()
}
