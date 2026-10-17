package service

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/graph"
	"github.com/multica-ai/multica/server/pkg/graphfiles"
)

// ValidateGraphAttachmentTx holds the attachment row until the accepted artifact
// is retained in the same transaction. A concurrent delete must either precede
// validation or observe the committed retention record after waiting for us.
func ValidateGraphAttachmentTx(ctx context.Context, tx pgx.Tx, workspaceID, taskID string, artifact graph.Artifact) error {
	var id string
	var bundleVerified bool
	err := tx.QueryRow(ctx, `SELECT a.id::text,u.bundle_verified FROM graph_attachment_upload u JOIN attachment a ON a.id=u.attachment_id
WHERE u.attachment_id=$1 AND u.workspace_id=$2 AND u.task_id=$3
AND u.content_hash=$4 AND u.size_bytes=$5 AND u.media_type=$6
AND a.workspace_id=u.workspace_id AND a.task_id=u.task_id FOR UPDATE OF a`, artifact.ID, workspaceID, taskID, artifact.ContentHash, artifact.Size, artifact.MediaType).Scan(&id, &bundleVerified)
	if errors.Is(err, pgx.ErrNoRows) {
		return graphError("artifact_unavailable", "attachment bytes do not match the submitted artifact")
	}
	if err == nil && artifact.BundleFormat != "" && (artifact.BundleFormat != graphfiles.Format || artifact.MediaType != "application/zip" || !bundleVerified) {
		return graphError("artifact_unavailable", "declared file bundle has no matching server verification receipt")
	}
	return err
}

func ValidateGraphFileDeliveryTx(ctx context.Context, tx pgx.Tx, workspaceID, taskID string, artifacts []graph.Artifact) error {
	found := false
	for _, artifact := range artifacts {
		if artifact.Type != graph.ArtifactAttachmentRef || artifact.ProducerTaskID != taskID || artifact.MediaType != "application/zip" || artifact.BundleFormat != graphfiles.Format {
			continue
		}
		if err := ValidateGraphAttachmentTx(ctx, tx, workspaceID, taskID, artifact); err != nil {
			return err
		}
		found = true
	}
	if found {
		return nil
	}
	return graphError("artifact_unavailable", "produce_files requires a server-verified file bundle; run multica graph node deliver <directory> before submitting")
}
