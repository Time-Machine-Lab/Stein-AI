package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
	"github.com/multica-ai/multica/server/pkg/pluginruntime"
)

func TestGraphPluginSnapshotScopeAndRevocation(t *testing.T) {
	ctx := context.Background()
	pool := graphIntegrationPool(t)
	for _, table := range []struct {
		name  string
		model any
	}{
		{"plugin_workspace_capability_state", db.PluginWorkspaceCapabilityState{}}, {"plugin_capability_snapshot", db.PluginCapabilitySnapshot{}},
		{"plugin_installation", db.PluginInstallation{}}, {"plugin_identity", db.PluginIdentity{}}, {"plugin_release", db.PluginRelease{}},
		{"plugin_contribution", db.PluginContribution{}}, {"plugin_artifact_file", db.PluginArtifactFile{}}, {"plugin_grant", db.PluginGrant{}}, {"plugin_binding", db.PluginBinding{}},
	} {
		if _, err := pool.Exec(ctx, graphTestTable(table.name, table.model)); err != nil {
			t.Fatal(err)
		}
	}
	ws, agentID, otherAgent, snapshotID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	entry, files := graphPluginSkillFixture(t)
	file := files[entry.ArtifactFileID]
	commands := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO plugin_identity(id) VALUES($1)`, []any{entry.PluginID}},
		{`INSERT INTO plugin_release(id,plugin_id,revocation_status) VALUES($1,$2,'active')`, []any{entry.ReleaseID, entry.PluginID}},
		{`INSERT INTO plugin_installation(id,workspace_id,plugin_id,active_release_id,enabled) VALUES($1,$2,$3,$4,true)`, []any{entry.InstallationID, ws, entry.PluginID, entry.ReleaseID}},
		{`INSERT INTO plugin_contribution(id,release_id,type) VALUES($1,$2,'agent.skill.v1')`, []any{entry.ContributionID, entry.ReleaseID}},
		{`INSERT INTO plugin_artifact_file(id,release_id,path,content,digest,size_bytes) VALUES($1,$2,$3,$4,$5,$6)`, []any{file.ID, file.ReleaseID, file.Path, file.Content, file.Digest, file.SizeBytes}},
		{`INSERT INTO plugin_grant(installation_id,capability,decision,grant_revision) VALUES($1,'agent.skill.contribute','granted',1)`, []any{entry.InstallationID}},
		{`INSERT INTO plugin_binding(installation_id,scope_type,scope_id,enabled,binding_revision) VALUES($1,'workspace',$2,true,1)`, []any{entry.InstallationID, ws}},
		{`INSERT INTO plugin_binding(installation_id,scope_type,scope_id,enabled,binding_revision) VALUES($1,'agent',$2,true,1)`, []any{entry.InstallationID, agentID}},
		{`INSERT INTO plugin_workspace_capability_state(workspace_id,active_snapshot_id) VALUES($1,$2)`, []any{ws, snapshotID}},
		{`INSERT INTO plugin_capability_snapshot(id,workspace_id,compiled_entries) VALUES($1,$2,'[]')`, []any{snapshotID, ws}},
	}
	for _, command := range commands {
		if _, err := pool.Exec(ctx, command.sql, command.args...); err != nil {
			t.Fatal(err)
		}
	}
	workspaceEntry := entry
	workspaceEntry.ScopeType, workspaceEntry.ScopeID, workspaceEntry.ExcludedAgentIDs = "workspace", ws, []string{agentID}
	agentEntry := entry
	agentEntry.ScopeType, agentEntry.ScopeID = "agent", agentID
	remoteEntry := entry
	remoteEntry.ScopeType, remoteEntry.ScopeID, remoteEntry.ContributionType = "agent", otherAgent, plugincontract.ContributionRemoteMCPV1
	setEntries := func(entries []pluginruntime.CompiledEntry) {
		t.Helper()
		raw, err := json.Marshal(entries)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `UPDATE plugin_capability_snapshot SET compiled_entries=$2 WHERE id=$1`, snapshotID, raw); err != nil {
			t.Fatal(err)
		}
	}
	setEntries([]pluginruntime.CompiledEntry{remoteEntry, workspaceEntry, agentEntry})
	svc := NewPluginService(db.New(pool), pool)
	wsUUID, agentUUID := util.MustParseUUID(ws), util.MustParseUUID(agentID)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	skills, pins, err := svc.SnapshotGraphSkills(ctx, tx, wsUUID, agentUUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || len(pins) != 1 || skills[0].Content != file.Content {
		t.Fatalf("scope matcher selected wrong plugin content: %d Skills, %d pins", len(skills), len(pins))
	}
	if err = svc.ValidateGraphPluginPins(ctx, tx, util.MustParseUUID(uuid.NewString()), agentUUID, pins); err == nil {
		t.Fatal("accepted plugin pin from another workspace")
	}
	// An ordinary upgrade changes the active pointer, but not the immutable
	// release selected when this Graph version was published.
	if _, err = pool.Exec(ctx, `UPDATE plugin_installation SET active_release_id=$2 WHERE id=$1`, entry.InstallationID, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if err = svc.ValidateGraphPluginPins(ctx, tx, wsUUID, agentUUID, pins); err != nil {
		t.Fatalf("upgrade changed published Skill: %v", err)
	}
	for _, test := range []struct{ name, deny, restore string }{
		{"disabled", `UPDATE plugin_installation SET enabled=false`, `UPDATE plugin_installation SET enabled=true`},
		{"uninstalled", `UPDATE plugin_installation SET uninstalled_at=now()`, `UPDATE plugin_installation SET uninstalled_at=NULL`},
		{"release revoked", `UPDATE plugin_release SET revocation_status='revoked'`, `UPDATE plugin_release SET revocation_status='active'`},
		{"identity retired", `UPDATE plugin_identity SET retired_at=now()`, `UPDATE plugin_identity SET retired_at=NULL`},
		{"grant withdrawn", `UPDATE plugin_grant SET decision='revoked'`, `UPDATE plugin_grant SET decision='granted'`},
		{"agent binding overrides workspace", `UPDATE plugin_binding SET enabled=false WHERE scope_type='agent'`, `UPDATE plugin_binding SET enabled=true WHERE scope_type='agent'`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, test.deny); err != nil {
				t.Fatal(err)
			}
			if err := svc.ValidateGraphPluginPins(ctx, tx, wsUUID, agentUUID, pins); err == nil {
				t.Fatal("accepted revoked plugin")
			}
			if _, err := pool.Exec(ctx, test.restore); err != nil {
				t.Fatal(err)
			}
		})
	}
	// Newer grant and binding revisions, rather than an older affirmative row,
	// define live access after a Graph version was published.
	if _, err = pool.Exec(ctx, `INSERT INTO plugin_grant(installation_id,capability,decision,grant_revision) VALUES($1,'agent.skill.contribute','revoked',2)`, entry.InstallationID); err != nil {
		t.Fatal(err)
	}
	if err = svc.ValidateGraphPluginPins(ctx, tx, wsUUID, agentUUID, pins); err == nil {
		t.Fatal("ignored latest grant revision")
	}
	if _, err = pool.Exec(ctx, `DELETE FROM plugin_grant WHERE grant_revision=2`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO plugin_binding(installation_id,scope_type,scope_id,enabled,binding_revision) VALUES($1,'agent',$2,false,2)`, entry.InstallationID, agentID); err != nil {
		t.Fatal(err)
	}
	if err = svc.ValidateGraphPluginPins(ctx, tx, wsUUID, agentUUID, pins); err == nil {
		t.Fatal("ignored latest binding revision")
	}
	if _, err = pool.Exec(ctx, `DELETE FROM plugin_binding WHERE binding_revision=2`); err != nil {
		t.Fatal(err)
	}
	remoteEntry.ScopeID = agentID
	setEntries([]pluginruntime.CompiledEntry{remoteEntry})
	if _, _, err = svc.SnapshotGraphSkills(ctx, tx, wsUUID, agentUUID); err == nil {
		t.Fatal("silently omitted applicable Remote MCP")
	}
	if _, err = pool.Exec(ctx, `UPDATE plugin_workspace_capability_state SET active_snapshot_id=NULL`); err != nil {
		t.Fatal(err)
	}
	if _, _, err = svc.SnapshotGraphSkills(ctx, tx, wsUUID, agentUUID); err == nil {
		t.Fatal("silently omitted enabled plugins without snapshot")
	}
	if _, err = pool.Exec(ctx, `UPDATE plugin_installation SET enabled=false`); err != nil {
		t.Fatal(err)
	}
	if _, _, err = svc.SnapshotGraphSkills(ctx, tx, wsUUID, agentUUID); err != nil {
		t.Fatalf("empty plugin workspace failed: %v", err)
	}
}
