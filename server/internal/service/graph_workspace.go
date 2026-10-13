package service

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// PrepareGraphWorkspaceDeleteTx joins the existing workspace teardown. The
// caller holds the workspace row FOR UPDATE; Graph writers hold KEY SHARE.
// Refuse to discard the only durable evidence of a live or unknown execution.
func PrepareGraphWorkspaceDeleteTx(ctx context.Context, tx pgx.Tx, workspaceID string) error {
	rows, err := tx.Query(ctx, `SELECT id FROM graph_run WHERE workspace_id=$1 ORDER BY id FOR UPDATE`, workspaceID)
	if err != nil {
		return err
	}
	for rows.Next() {
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var active bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM graph_run WHERE workspace_id=$1 AND state NOT IN ('succeeded','rejected','failed','cancelled')) OR EXISTS(SELECT 1 FROM graph_task_attempt WHERE workspace_id=$1 AND stopped_at IS NULL)`, workspaceID).Scan(&active)
	if err != nil {
		return err
	}
	if active {
		return graphError("graph_execution_active", "cancel Graph runs and wait for execution to stop before deleting this workspace")
	}
	// Fixed identifiers only. Remove ownership and retained-artifact references
	// before the existing Issue/attachment deletion steps invoke their fences.
	for _, table := range []string{
		"graph_attachment_upload", "graph_artifact", "graph_approval", "graph_submission",
		"graph_operation", "graph_run_event", "graph_task_attempt", "graph_issue_owner",
		"graph_node_run", "graph_run", "graph_version", "graph_definition", "graph_command",
	} {
		if _, err = tx.Exec(ctx, `DELETE FROM `+table+` WHERE workspace_id=$1`, workspaceID); err != nil {
			return err
		}
	}
	return nil
}
