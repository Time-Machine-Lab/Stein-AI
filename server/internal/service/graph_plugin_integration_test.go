package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/graph"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
	"github.com/multica-ai/multica/server/pkg/pluginruntime"
)

func TestGraphPluginMigrationPinsPublishedEntriesAndPreservesOrdinaryTasks(t *testing.T) {
	f := newGraphFixture(t)
	ctx := context.Background()
	for _, table := range []struct {
		name  string
		model any
	}{{"plugin_workspace_capability_state", db.PluginWorkspaceCapabilityState{}}, {"plugin_capability_snapshot", db.PluginCapabilitySnapshot{}}, {"plugin_execution_manifest", db.PluginExecutionManifest{}}} {
		if _, err := f.pool.Exec(ctx, graphTestTable(table.name, table.model)); err != nil {
			t.Fatal(err)
		}
	}
	// The real replacement function, against isolated tables. No application
	// database or current plugin installation is touched.
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "358_graph_plugin_manifest.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `CREATE TRIGGER trg_agent_task_queue_plugin_execution_manifest BEFORE INSERT ON agent_task_queue FOR EACH ROW EXECUTE FUNCTION pin_plugin_execution_manifest()`); err != nil {
		t.Fatal(err)
	}
	entry := map[string]any{"contribution_id": "published-remote", "contribution_type": "tool.remote-mcp.v1", "endpoint": "https://frozen.example.com/mcp"}
	frozen, _ := json.Marshal(map[string]any{"id": f.agent, "plugin_entries": []any{entry}})
	f.svc.PrepareNode = func(context.Context, pgx.Tx, string, string, graph.Node) (GraphNodeSnapshot, error) {
		return GraphNodeSnapshot{AgentConfig: frozen}, nil
	}
	draft, err := f.svc.CreateDefinition(ctx, f.ws, f.user, f.version.Definition, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	f.version, err = f.svc.Publish(ctx, f.ws, f.user, draft.ID, draft.Revision, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	run := f.reconcile(t, f.start(t))
	task := run.Nodes[1].Attempts[0].TaskID
	var raw []byte
	if err = f.pool.QueryRow(ctx, `SELECT ordered_contributions FROM plugin_execution_manifest WHERE task_id=$1`, task).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "frozen.example.com") {
		t.Fatalf("did not pin publication: %s", raw)
	}
	// Ordinary tasks still compose the live workspace snapshot.
	snapshot := uuid.NewString()
	live := `[{"scope_type":"workspace","contribution_id":"live-skill"}]`
	if _, err = f.pool.Exec(ctx, `INSERT INTO plugin_capability_snapshot(id,workspace_id,revision,snapshot_digest,compiled_entries) VALUES($1,$2,1,$3,$4)`, snapshot, f.ws, "sha256:"+strings.Repeat("a", 64), live); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `INSERT INTO plugin_workspace_capability_state(workspace_id,active_snapshot_id) VALUES($1,$2)`, f.ws, snapshot); err != nil {
		t.Fatal(err)
	}
	ordinary := uuid.NewString()
	ordinaryIssue := uuid.NewString()
	if _, err = f.pool.Exec(ctx, `INSERT INTO issue(id,workspace_id,title,status,priority,creator_type,creator_id,number) VALUES($1,$2,'ordinary','todo','none','member',$3,999)`, ordinaryIssue, f.ws, f.user); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `INSERT INTO agent_task_queue(id,agent_id,runtime_id,issue_id,status) VALUES($1,$2,$3,$4,'queued')`, ordinary, f.agent, f.runtime, ordinaryIssue); err != nil {
		t.Fatal(err)
	}
	if err = f.pool.QueryRow(ctx, `SELECT ordered_contributions FROM plugin_execution_manifest WHERE task_id=$1`, ordinary).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "live-skill") {
		t.Fatalf("ordinary composition changed: %s", raw)
	}
	// Downgrade restores the original ordinary-task function.
	rollback, err := os.ReadFile(filepath.Join("..", "..", "migrations", "358_graph_plugin_manifest.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, string(rollback)); err != nil {
		t.Fatal(err)
	}
}

func TestGraphRemoteMCPAuthorizationRevocationAndAttemptFencePostgres(t *testing.T) {
	f := newGraphFixture(t)
	ctx := context.Background()
	for _, table := range []struct {
		name  string
		model any
	}{{"plugin_identity", db.PluginIdentity{}}, {"plugin_release", db.PluginRelease{}}, {"plugin_installation", db.PluginInstallation{}}, {"plugin_contribution", db.PluginContribution{}}, {"plugin_grant", db.PluginGrant{}}, {"plugin_binding", db.PluginBinding{}}, {"plugin_execution_manifest", db.PluginExecutionManifest{}}} {
		if _, err := f.pool.Exec(ctx, graphTestTable(table.name, table.model)); err != nil {
			t.Fatal(err)
		}
	}
	entry, _ := graphPluginSkillFixture(t)
	entry.ContributionType = plugincontract.ContributionRemoteMCPV1
	entry.AuthType = "none"
	frozen, _ := json.Marshal(map[string]any{"id": f.agent, "plugin_entries": []pluginruntime.CompiledEntry{entry}})
	f.svc.PrepareNode = func(context.Context, pgx.Tx, string, string, graph.Node) (GraphNodeSnapshot, error) {
		return GraphNodeSnapshot{AgentConfig: frozen}, nil
	}
	draft, err := f.svc.CreateDefinition(ctx, f.ws, f.user, f.version.Definition, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	f.version, err = f.svc.Publish(ctx, f.ws, f.user, draft.ID, draft.Revision, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	run := f.reconcile(t, f.start(t))
	task := run.Nodes[1].Attempts[0].TaskID
	taskUUID, _ := util.ParseUUID(task)
	entries, _ := json.Marshal([]pluginruntime.CompiledEntry{entry})
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := f.pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO plugin_identity(id) VALUES($1)`, entry.PluginID)
	exec(`INSERT INTO plugin_release(id,plugin_id,revocation_status) VALUES($1,$2,'active')`, entry.ReleaseID, entry.PluginID)
	exec(`INSERT INTO plugin_installation(id,plugin_id,workspace_id,enabled) VALUES($1,$2,$3,true)`, entry.InstallationID, entry.PluginID, f.ws)
	exec(`INSERT INTO plugin_contribution(id,release_id,type) VALUES($1,$2,$3)`, entry.ContributionID, entry.ReleaseID, entry.ContributionType)
	exec(`INSERT INTO plugin_grant(installation_id,capability,decision,grant_revision) VALUES($1,$2,'granted',1)`, entry.InstallationID, plugincontract.CapabilityRemoteMCPConnect)
	exec(`INSERT INTO plugin_binding(installation_id,scope_type,scope_id,enabled,binding_revision) VALUES($1,'workspace',$2,true,1)`, entry.InstallationID, f.ws)
	exec(`INSERT INTO plugin_execution_manifest(task_id,workspace_id,agent_id,runtime_id,composer_version,schema_version,ordered_contributions,diagnostics) VALUES($1,$2,$3,$4,'graph-plugin-composer-v1',1,$5,'[]')`, task, f.ws, f.agent, f.runtime, entries)
	exec(`UPDATE agent_task_queue SET plugin_execution_manifest_id=(SELECT id FROM plugin_execution_manifest WHERE task_id=$1) WHERE id=$1`, task)
	service := NewPluginService(db.New(f.pool), f.pool)
	check := func(want bool) {
		t.Helper()
		err := service.ValidateGraphTaskRemoteMCPAuthorization(ctx, taskUUID, entry.ContributionID)
		if (err == nil) != want {
			t.Fatalf("want authorized=%v; err=%v", want, err)
		}
	}
	check(true)
	exec(`UPDATE plugin_grant SET decision='denied' WHERE installation_id=$1`, entry.InstallationID)
	check(false)
	exec(`UPDATE plugin_grant SET decision='granted' WHERE installation_id=$1`, entry.InstallationID)
	check(true)
	exec(`INSERT INTO plugin_binding(installation_id,scope_type,scope_id,enabled,binding_revision) VALUES($1,'agent',$2,false,2)`, entry.InstallationID, f.agent)
	check(false)
	exec(`UPDATE plugin_binding SET enabled=true WHERE scope_type='agent'`)
	check(true)
	exec(`UPDATE plugin_release SET revocation_status='revoked' WHERE id=$1`, entry.ReleaseID)
	check(false)
	exec(`UPDATE plugin_release SET revocation_status='active' WHERE id=$1`, entry.ReleaseID)
	check(true)
	exec(`UPDATE graph_run SET control_state='cancel_requested' WHERE id=$1`, run.ID)
	check(false)
	exec(`UPDATE graph_run SET control_state='run' WHERE id=$1`, run.ID)
	check(true)
	exec(`UPDATE plugin_execution_manifest SET ordered_contributions='[]' WHERE task_id=$1`, task)
	check(false)
}
