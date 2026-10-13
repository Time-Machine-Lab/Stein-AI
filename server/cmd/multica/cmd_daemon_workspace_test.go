package main

import (
	"github.com/spf13/cobra"
	"strings"
	"testing"
)

func TestGraphAcceptanceDaemonWorkspaceFlagSurvivesBackgroundStart(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("workspace-id", "default-workspace", "")
	if explicitDaemonWorkspaceIDs(cmd) != nil || strings.Contains(strings.Join(buildDaemonStartArgs(cmd), " "), "workspace-id") {
		t.Fatal("implicit default changed legacy daemon scope")
	}
	if err := cmd.Flags().Set("workspace-id", "vega"); err != nil {
		t.Fatal(err)
	}
	if got := explicitDaemonWorkspaceIDs(cmd); len(got) != 1 || got[0] != "vega" {
		t.Fatalf("wrong override: %v", got)
	}
	if !strings.Contains(strings.Join(buildDaemonStartArgs(cmd), " "), "--workspace-id vega") {
		t.Fatal("child lost workspace allowlist")
	}
}
