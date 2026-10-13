package service

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/graph"
)

// ValidateGraphAttachmentTx holds the attachment row until the accepted artifact
// is retained in the same transaction. A concurrent delete must either precede
// validation or observe the committed retention record after waiting for us.
func ValidateGraphAttachmentTx(ctx context.Context, tx pgx.Tx, workspaceID, taskID string, artifact graph.Artifact) error {
	var id string
	err := tx.QueryRow(ctx, `SELECT a.id::text FROM graph_attachment_upload u JOIN attachment a ON a.id=u.attachment_id
WHERE u.attachment_id=$1 AND u.workspace_id=$2 AND u.task_id=$3
AND u.content_hash=$4 AND u.size_bytes=$5 AND u.media_type=$6
AND a.workspace_id=u.workspace_id AND a.task_id=u.task_id FOR UPDATE OF a`, artifact.ID, workspaceID, taskID, artifact.ContentHash, artifact.Size, artifact.MediaType).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return graphError("artifact_unavailable", "attachment bytes do not match the submitted artifact")
	}
	return err
}
