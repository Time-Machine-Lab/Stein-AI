package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/graph"
)

func (s *GraphService) CreateDefinition(ctx context.Context, ws, user string, definition graph.Definition, key string) (GraphDefinitionRecord, error) {
	var result GraphDefinitionRecord
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if _, _, err = graphMember(ctx, tx, ws, user); err != nil {
		return result, err
	}
	replayed, hash, err := graphCommand(ctx, tx, ws, key, []any{"create_definition", user, definition}, &result)
	if err != nil {
		return result, err
	}
	if replayed {
		return result, tx.Commit(ctx)
	}
	if err = graphDraftSize(definition); err != nil {
		return result, err
	}
	now := time.Now().UTC()
	result = GraphDefinitionRecord{ID: uuid.NewString(), WorkspaceID: ws, CreatorID: user, Name: definition.Name, Revision: 1, Definition: definition, CreatedAt: now, UpdatedAt: now}
	b, err := graphJSON(result)
	if err != nil {
		return result, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO graph_definition(id,workspace_id,creator_id,revision,document) VALUES($1,$2,$3,$4,$5)`, result.ID, ws, user, 1, b)
	if err != nil {
		return result, err
	}
	if err = graphSaveCommand(ctx, tx, ws, key, hash, result); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
func graphDraftSize(definition graph.Definition) error {
	b, err := json.Marshal(definition)
	if err != nil {
		return err
	}
	if len(b) > 1024*1024 {
		return graphError("definition_too_large", "definition cannot exceed 1 MiB")
	}
	if len(definition.Name) == 0 || len(definition.Name) > 200 {
		return graphError("invalid_name", "name must contain 1 to 200 characters")
	}
	return nil
}
func graphEditable(ctx context.Context, tx pgx.Tx, ws, user, id string) (GraphDefinitionRecord, error) {
	var result GraphDefinitionRecord
	if err := graphUUIDs(id); err != nil {
		return result, err
	}
	_, role, err := graphMember(ctx, tx, ws, user)
	if err != nil {
		return result, err
	}
	err = graphScan(tx.QueryRow(ctx, `SELECT document FROM graph_definition WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, ws, id), &result)
	if err != nil {
		return result, err
	}
	if result.CreatorID != user && role != "owner" && role != "admin" {
		return result, graphError("forbidden", "only the creator or a workspace administrator can edit this Graph")
	}
	if result.ArchivedAt != nil {
		return result, graphError("archived", "Graph is archived")
	}
	return result, nil
}
func (s *GraphService) UpdateDefinition(ctx context.Context, ws, user, id string, definition graph.Definition, revision int64, key string) (GraphDefinitionRecord, error) {
	var result GraphDefinitionRecord
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if _, _, err = graphMember(ctx, tx, ws, user); err != nil {
		return result, err
	}
	replayed, hash, err := graphCommand(ctx, tx, ws, key, []any{"update", user, id, definition, revision}, &result)
	if err != nil {
		return result, err
	}
	if replayed {
		return result, tx.Commit(ctx)
	}
	result, err = graphEditable(ctx, tx, ws, user, id)
	if err != nil {
		return result, err
	}
	if result.Revision != revision {
		return result, graphError("revision_conflict", "draft changed; reload before saving")
	}
	if err = graphDraftSize(definition); err != nil {
		return result, err
	}
	result.Definition = definition
	result.Name = definition.Name
	result.Revision++
	result.UpdatedAt = time.Now().UTC()
	result.Versions = nil
	b, err := graphJSON(result)
	if err != nil {
		return result, err
	}
	_, err = tx.Exec(ctx, `UPDATE graph_definition SET revision=$3,document=$4,updated_at=$5 WHERE workspace_id=$1 AND id=$2`, ws, id, result.Revision, b, result.UpdatedAt)
	if err != nil {
		return result, err
	}
	if err = graphSaveCommand(ctx, tx, ws, key, hash, result); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
func (s *GraphService) ListDefinitions(ctx context.Context, ws, user string) ([]GraphDefinitionRecord, error) {
	result := []GraphDefinitionRecord{}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, _, err = graphMember(ctx, tx, ws, user); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT document FROM graph_definition WHERE workspace_id=$1 AND archived_at IS NULL ORDER BY updated_at DESC LIMIT 100`, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var row GraphDefinitionRecord
		if err = graphScan(rows, &row); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}
func (s *GraphService) GetDefinition(ctx context.Context, ws, user, id string) (GraphDefinitionRecord, error) {
	var result GraphDefinitionRecord
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if _, _, err = graphMember(ctx, tx, ws, user); err != nil {
		return result, err
	}
	if err = graphUUIDs(id); err != nil {
		return result, err
	}
	if err = graphScan(tx.QueryRow(ctx, `SELECT document FROM graph_definition WHERE workspace_id=$1 AND id=$2`, ws, id), &result); err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT document FROM graph_version WHERE workspace_id=$1 AND graph_id=$2 ORDER BY version DESC`, ws, id)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	result.Versions = []GraphVersionRecord{}
	for rows.Next() {
		var v GraphVersionRecord
		if err = graphScan(rows, &v); err != nil {
			return result, err
		}
		result.Versions = append(result.Versions, v)
	}
	return result, rows.Err()
}
func (s *GraphService) Publish(ctx context.Context, ws, user, id string, revision int64, key string) (GraphVersionRecord, error) {
	var result GraphVersionRecord
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	_, role, err := graphMember(ctx, tx, ws, user)
	if err != nil {
		return result, err
	}
	if role != "owner" && role != "admin" {
		return result, graphError("forbidden", "only a workspace administrator can publish Graph versions")
	}
	replayed, hash, err := graphCommand(ctx, tx, ws, key, []any{"publish", user, id, revision}, &result)
	if err != nil {
		return result, err
	}
	if replayed {
		return result, tx.Commit(ctx)
	}
	draft, err := graphEditable(ctx, tx, ws, user, id)
	if err != nil {
		return result, err
	}
	if draft.Revision != revision {
		return result, graphError("revision_conflict", "draft changed; reload before publishing")
	}
	if err = graph.ValidateDefinition(draft.Definition); err != nil {
		return result, graphError("invalid_definition", err.Error())
	}
	canonical, _, err := graph.CanonicalDefinition(draft.Definition)
	if err != nil {
		return result, err
	}
	// Decode into a fresh value: unmarshalling over the draft would reuse its node
	// slots, and canonical ordering can place a node where a differently configured
	// one used to be, leaving stale optional config (e.g. a loop node with http).
	var normalized graph.Definition
	if err = json.Unmarshal(canonical, &normalized); err != nil {
		return result, err
	}
	draft.Definition = normalized
	if s.PrepareNode == nil {
		return result, graphError("capability_missing", "Graph snapshot adapter is unavailable")
	}
	result = GraphVersionRecord{ID: uuid.NewString(), WorkspaceID: ws, GraphID: id, Definition: draft.Definition, Snapshots: map[string]GraphNodeSnapshot{}, CreatedAt: time.Now().UTC()}
	err = graph.WalkNodes(draft.Definition, func(path string, node graph.Node) error {
		if node.Type == graph.NodeTypeAgent || node.Type == graph.NodeTypeScript {
			if err = graphUUIDs(node.AgentID); err != nil {
				return err
			}
			snapshot, e := s.PrepareNode(ctx, tx, ws, user, node)
			if e != nil {
				return e
			}
			if e = graphValidateSnapshot(snapshot); e != nil {
				return e
			}
			result.Snapshots[path] = snapshot
		}
		if string(node.Type) == "approval" {
			members := []string{node.ApproverID}
			if node.Approval != nil {
				members = node.Approval.ApproverIDs
			}
			for _, member := range members {
				if err = graphUUIDs(member); err != nil {
					return err
				}
				var found bool
				if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM member WHERE workspace_id=$1 AND id=$2)`, ws, member).Scan(&found); err != nil {
					return err
				}
				if !found {
					return graphError("approver_unavailable", "approval member does not exist in this workspace")
				}
			}
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	if err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM graph_version WHERE graph_id=$1`, id).Scan(&result.Version); err != nil {
		return result, err
	}
	result.ContentHash, err = graphHash([]any{result.Definition, result.Snapshots})
	if err != nil {
		return result, err
	}
	b, err := graphJSON(result)
	if err != nil {
		return result, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO graph_version(id,workspace_id,graph_id,version,document,content_hash) VALUES($1,$2,$3,$4,$5,$6)`, result.ID, ws, id, result.Version, b, result.ContentHash)
	if err != nil {
		return result, err
	}
	if err = graphSaveCommand(ctx, tx, ws, key, hash, result); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func graphValidateSnapshot(snapshot GraphNodeSnapshot) error {
	if len(snapshot.AgentConfig) > graph.MaxJSONBytes {
		return graphError("snapshot_invalid", "agent configuration exceeds the Graph size limit")
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(snapshot.AgentConfig, &config); err != nil || config == nil {
		return graphError("snapshot_invalid", "agent configuration must be a JSON object")
	}
	if len(snapshot.Skills) > 64 {
		return graphError("snapshot_invalid", "an agent snapshot cannot include more than 64 skills")
	}
	return nil
}
func (s *GraphService) ArchiveDefinition(ctx context.Context, ws, user, id string, revision int64, key string) (GraphDefinitionRecord, error) {
	var result GraphDefinitionRecord
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if _, _, err = graphMember(ctx, tx, ws, user); err != nil {
		return result, err
	}
	replayed, hash, err := graphCommand(ctx, tx, ws, key, []any{"archive_definition", user, id, revision}, &result)
	if err != nil {
		return result, err
	}
	if replayed {
		return result, tx.Commit(ctx)
	}
	result, err = graphEditable(ctx, tx, ws, user, id)
	if err != nil {
		return result, err
	}
	if result.Revision != revision {
		return result, graphError("revision_conflict", "draft changed")
	}
	now := time.Now().UTC()
	result.ArchivedAt = &now
	result.UpdatedAt = now
	result.Revision++
	result.Versions = nil
	b, err := graphJSON(result)
	if err != nil {
		return result, err
	}
	_, err = tx.Exec(ctx, `UPDATE graph_definition SET archived_at=$3,revision=$4,document=$5,updated_at=$3 WHERE workspace_id=$1 AND id=$2`, ws, id, now, result.Revision, b)
	if err != nil {
		return result, err
	}
	if err = graphSaveCommand(ctx, tx, ws, key, hash, result); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
