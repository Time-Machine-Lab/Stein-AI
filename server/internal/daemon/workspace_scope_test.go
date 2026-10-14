package daemon

import (
	"context"
	"testing"
)

func TestGraphAcceptanceDaemonOnlyRegistersAndPollsExplicitWorkspace(t *testing.T) {
	fx := newBatchFixture(t)
	d := fx.daemon
	d.cfg.Agents = map[string]AgentEntry{"claude": {Path: "/fake/claude"}}
	d.cfg.WorkspaceIDs = []string{"vega"}
	fx.setWorkspaces(WorkspaceInfo{ID: "tml", Name: "TML"}, WorkspaceInfo{ID: "vega", Name: "Vega"})
	if err := d.syncWorkspacesFromAPI(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if len(d.workspaces) != 1 || d.workspaces["vega"] == nil {
		t.Fatalf("daemon registered outside its explicit scope: %v", d.workspaces)
	}
	if len(d.allRuntimeIDs()) != len(d.workspaces["vega"].runtimeIDs) {
		t.Fatal("heartbeat/poll includes an out-of-scope runtime")
	}
	fx.mu.Lock()
	defer fx.mu.Unlock()
	for _, call := range fx.registered {
		if call.workspaceID != "vega" {
			t.Fatalf("out-of-scope registration request: %+v", call)
		}
	}
}

func TestGraphAcceptanceDaemonWorkspaceScopePrecedence(t *testing.T) {
	ids, err := daemonWorkspaceIDs(nil, " one, two,one ")
	if err != nil || len(ids) != 2 {
		t.Fatalf("parse: %v %v", ids, err)
	}
	ids, err = daemonWorkspaceIDs([]string{"vega"}, "other")
	if err != nil || len(ids) != 1 || ids[0] != "vega" {
		t.Fatalf("override: %v %v", ids, err)
	}
	if _, err = daemonWorkspaceIDs([]string{""}, ""); err == nil {
		t.Fatal("explicit empty ID widened daemon scope")
	}
	if _, err = daemonWorkspaceIDs(nil, "vega,"); err == nil {
		t.Fatal("invalid scope accepted")
	}
	if !((Config{}).permitsWorkspace("old-default")) {
		t.Fatal("legacy daemon no longer discovers all workspaces")
	}
}
