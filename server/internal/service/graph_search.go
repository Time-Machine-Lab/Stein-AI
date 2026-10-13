package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

type GraphSearchParams struct {
	Query, State, GraphID, Cursor string
	Limit                         int
}
type GraphSearchPage[T any] struct {
	Items      []T            `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
	Total      int64          `json:"total"`
	Stats      *GraphRunStats `json:"stats,omitempty"`
}
type GraphRunStats struct {
	Total           int64   `json:"total"`
	Active          int64   `json:"active"`
	WaitingApproval int64   `json:"waiting_approval"`
	Blocked         int64   `json:"blocked"`
	Succeeded       int64   `json:"succeeded"`
	Failed          int64   `json:"failed"`
	Rejected        int64   `json:"rejected"`
	Cancelled       int64   `json:"cancelled"`
	TotalDurationMS int64   `json:"total_duration_ms"`
	TotalCostUSD    float64 `json:"total_cost_usd"`
	TotalTokens     int64   `json:"total_tokens"`
	UnpricedTasks   int64   `json:"unpriced_tasks"`
}
type graphSearchCursor struct {
	At time.Time `json:"at"`
	ID string    `json:"id"`
}

func graphSearchInput(p GraphSearchParams) (GraphSearchParams, graphSearchCursor, error) {
	var cursor graphSearchCursor
	p.Query = strings.TrimSpace(p.Query)
	if len(p.Query) > 200 || len(p.Cursor) > 512 || p.Limit < 0 || p.Limit > 100 {
		return p, cursor, graphError("invalid_request", "invalid Graph search limits")
	}
	if p.Limit == 0 {
		p.Limit = 30
	}
	if p.GraphID != "" {
		if err := graphUUIDs(p.GraphID); err != nil {
			return p, cursor, err
		}
	}
	if p.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(p.Cursor)
		if err != nil || json.Unmarshal(raw, &cursor) != nil || cursor.At.IsZero() || graphUUIDs(cursor.ID) != nil {
			return p, cursor, graphError("invalid_request", "invalid Graph search cursor")
		}
	}
	return p, cursor, nil
}
func graphEncodeCursor(at time.Time, id string) string {
	raw, _ := json.Marshal(graphSearchCursor{at, id})
	return base64.RawURLEncoding.EncodeToString(raw)
}
func graphSearchPattern(q string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
}

func (s *GraphService) SearchDefinitions(ctx context.Context, ws, user string, input GraphSearchParams) (GraphSearchPage[GraphDefinitionRecord], error) {
	result := GraphSearchPage[GraphDefinitionRecord]{Items: []GraphDefinitionRecord{}}
	p, cursor, err := graphSearchInput(input)
	if err != nil {
		return result, err
	}
	switch p.State {
	case "", "draft", "published", "archived":
	default:
		return result, graphError("invalid_request", "unknown Graph definition state")
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ`); err != nil {
		return result, err
	}
	if _, _, err = graphMember(ctx, tx, ws, user); err != nil {
		return result, err
	}
	const filter = ` FROM graph_definition d WHERE d.workspace_id=$1 AND d.document->>'name' ILIKE $2
 AND (($3='archived' AND d.archived_at IS NOT NULL) OR ($3<>'archived' AND d.archived_at IS NULL))
 AND ($3 NOT IN ('draft','published') OR ($3='published')=EXISTS(SELECT 1 FROM graph_version v WHERE v.graph_id=d.id AND v.workspace_id=d.workspace_id))`
	args := []any{ws, graphSearchPattern(p.Query), p.State}
	if err = tx.QueryRow(ctx, `SELECT count(*)`+filter, args...).Scan(&result.Total); err != nil {
		return result, err
	}
	var at any
	var id any
	if p.Cursor != "" {
		at = cursor.At
		id = cursor.ID
	}
	rows, err := tx.Query(ctx, `SELECT d.document,d.created_at,d.id`+filter+` AND ($4::timestamptz IS NULL OR (d.created_at,d.id)<($4,$5::uuid)) ORDER BY d.created_at DESC,d.id DESC LIMIT $6`, append(args, at, id, p.Limit+1)...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	var last graphSearchCursor
	for rows.Next() {
		var raw []byte
		var created time.Time
		var rowID string
		if err = rows.Scan(&raw, &created, &rowID); err != nil {
			return result, err
		}
		if len(result.Items) == p.Limit {
			result.NextCursor = graphEncodeCursor(last.At, last.ID)
			break
		}
		var item GraphDefinitionRecord
		if err = json.Unmarshal(raw, &item); err != nil {
			return result, err
		}
		result.Items = append(result.Items, item)
		last = graphSearchCursor{created, rowID}
	}
	return result, rows.Err()
}

const graphRunSearchFilter = ` FROM graph_run r JOIN graph_version v ON v.id=r.version_id AND v.workspace_id=r.workspace_id
 WHERE r.workspace_id=$1 AND (COALESCE(v.document#>>'{definition,name}','') ILIKE $2 OR COALESCE(r.document#>>'{root_snapshot,title}','') ILIKE $2 OR r.id::text ILIKE $2)
 AND ($3='' OR r.state=$3) AND ($4::uuid IS NULL OR v.graph_id=$4) AND r.archived_at IS NULL`

// The list endpoint must not transport 16 MiB execution snapshots per row.
// Full definitions, inputs, credentials-free manifests and evidence stay on
// the detail endpoint; summary metadata remains compatible with older clients.
const graphRunSummaryProjection = `jsonb_build_object(
 'id',r.id,'workspace_id',r.workspace_id,'root_issue_id',r.root_issue_id,'version_id',r.version_id,
 'state',r.state,'control_state',r.document->'control_state','revision',r.document->'revision',
 'initiator_id',r.document->'initiator_id','schema_version',r.document->'schema_version',
 'previous_run_id',r.document->'previous_run_id','block_reason',r.document->'block_reason',
 'can_detach',r.document->'can_detach','deadline',r.document->'deadline','finished_at',r.document->'finished_at',
 'created_at',r.created_at,'updated_at',r.updated_at,
 'root_snapshot',jsonb_build_object('title',left(r.document#>>'{root_snapshot,title}',300)))`

func (s *GraphService) SearchRuns(ctx context.Context, ws, user string, input GraphSearchParams) (GraphSearchPage[GraphRunRecord], error) {
	result := GraphSearchPage[GraphRunRecord]{Items: []GraphRunRecord{}, Stats: &GraphRunStats{}}
	p, cursor, err := graphSearchInput(input)
	if err != nil {
		return result, err
	}
	switch p.State {
	case "", "active", "waiting_approval", "blocked", "succeeded", "failed", "rejected", "cancelled":
	default:
		return result, graphError("invalid_request", "unknown Graph run state")
	}
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SET TRANSACTION ISOLATION LEVEL REPEATABLE READ`); err != nil {
		return result, err
	}
	if _, _, err = graphMember(ctx, tx, ws, user); err != nil {
		return result, err
	}
	var graphID any
	if p.GraphID != "" {
		graphID = p.GraphID
	}
	args := []any{ws, graphSearchPattern(p.Query), p.State, graphID}
	stats := result.Stats
	err = tx.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE r.state='active'),count(*) FILTER(WHERE r.state='waiting_approval'),count(*) FILTER(WHERE r.state='blocked'),count(*) FILTER(WHERE r.state='succeeded'),count(*) FILTER(WHERE r.state='failed'),count(*) FILTER(WHERE r.state='rejected'),count(*) FILTER(WHERE r.state='cancelled'),COALESCE(SUM(GREATEST(0,EXTRACT(EPOCH FROM(COALESCE((r.document->>'finished_at')::timestamptz,CASE WHEN r.state IN ('succeeded','failed','rejected','cancelled') THEN r.updated_at ELSE now() END)-r.created_at))*1000)),0)::bigint`+graphRunSearchFilter, args...).Scan(&stats.Total, &stats.Active, &stats.WaitingApproval, &stats.Blocked, &stats.Succeeded, &stats.Failed, &stats.Rejected, &stats.Cancelled, &stats.TotalDurationMS)
	if err != nil {
		return result, err
	}
	result.Total = stats.Total
	err = tx.QueryRow(ctx, `WITH selected AS (SELECT r.id`+graphRunSearchFilter+`) SELECT COALESCE(SUM(u.cost_usd_ticks),0)::double precision/10000000000,COALESCE(SUM(u.input_tokens+u.output_tokens+u.cache_read_tokens+u.cache_write_tokens),0)::bigint,COUNT(DISTINCT u.task_id) FILTER(WHERE u.cost_usd_ticks IS NULL) FROM selected s JOIN graph_task_attempt a ON a.run_id=s.id JOIN task_usage u ON u.task_id=a.task_id`, args...).Scan(&stats.TotalCostUSD, &stats.TotalTokens, &stats.UnpricedTasks)
	if err != nil {
		return result, err
	}
	var at any
	var id any
	if p.Cursor != "" {
		at = cursor.At
		id = cursor.ID
	}
	rows, err := tx.Query(ctx, `SELECT `+graphRunSummaryProjection+`,r.created_at,r.id,v.graph_id,COALESCE(v.document#>>'{definition,name}','')`+graphRunSearchFilter+` AND ($5::timestamptz IS NULL OR (r.created_at,r.id)<($5,$6::uuid)) ORDER BY r.created_at DESC,r.id DESC LIMIT $7`, append(args, at, id, p.Limit+1)...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	var last graphSearchCursor
	for rows.Next() {
		var raw []byte
		var created time.Time
		var rowID, definitionID, name string
		if err = rows.Scan(&raw, &created, &rowID, &definitionID, &name); err != nil {
			return result, err
		}
		if len(result.Items) == p.Limit {
			result.NextCursor = graphEncodeCursor(last.At, last.ID)
			break
		}
		var item GraphRunRecord
		if err = json.Unmarshal(raw, &item); err != nil {
			return result, err
		}
		item.GraphID = definitionID
		item.GraphName = name
		result.Items = append(result.Items, item)
		last = graphSearchCursor{created, rowID}
	}
	return result, rows.Err()
}
