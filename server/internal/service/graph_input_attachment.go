package service

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// freezeGraphInputAttachments reuses issue attachment identities, not expiring
// download URLs. Comment attachments remain outside the frozen issue input.
func freezeGraphInputAttachments(ctx context.Context, tx pgx.Tx, ws, issue string, raw json.RawMessage) (json.RawMessage, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id::text,filename,content_type,size_bytes FROM attachment WHERE workspace_id=$1 AND issue_id=$2 AND comment_id IS NULL ORDER BY created_at,id FOR SHARE`, ws, issue)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type inputAttachment struct {
		ID        string `json:"id"`
		Filename  string `json:"filename"`
		MediaType string `json:"media_type"`
		Size      int64  `json:"size"`
	}
	attachments := []inputAttachment{}
	for rows.Next() {
		var attachment inputAttachment
		if err := rows.Scan(&attachment.ID, &attachment.Filename, &attachment.MediaType, &attachment.Size); err != nil {
			return nil, err
		}
		attachments = append(attachments, attachment)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	root["attachments"], err = json.Marshal(attachments)
	if err != nil {
		return nil, err
	}
	return json.Marshal(root)
}

// GraphTaskCanReadAttachment supplements the live-attempt middleware fence
// with the run's frozen inputs and existing task uploads/output artifacts.
func GraphTaskCanReadAttachment(ctx context.Context, q db.DBTX, ws, run, task, attachment string) (bool, error) {
	var allowed bool
	err := q.QueryRow(ctx, `SELECT EXISTS(
SELECT 1 FROM attachment f WHERE f.id=$1 AND f.workspace_id=$2 AND (
  f.task_id=$3 OR EXISTS(
    SELECT 1 FROM graph_artifact a WHERE a.run_id=$4 AND a.workspace_id=$2
    AND a.document->>'type'='attachment_ref' AND a.document->>'id'=f.id::text
  ) OR EXISTS(
    SELECT 1 FROM graph_run r WHERE r.id=$4 AND r.workspace_id=$2
    AND r.document#>'{root_snapshot,attachments}' @> jsonb_build_array(jsonb_build_object('id',f.id::text))
  )
))`, attachment, ws, task, run).Scan(&allowed)
	return allowed, err
}
