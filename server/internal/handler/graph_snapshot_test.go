package handler

import (
	"encoding/json"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"strings"
	"testing"
)

func TestGraphRuntimeCapabilityNegotiationDoesNotAssumeEveryDaemonSupportsNewAdapters(t *testing.T) {
	for _, tc := range []struct {
		provider string
		profile  bool
		metadata string
		want     bool
	}{
		{"codex", false, `{"capabilities":["graph-execution-v1"]}`, true},
		{"claude", false, `{"capabilities":["graph-execution-v1"]}`, false},
		{"claude", false, `{"capabilities":["graph-execution-v1","graph-execution-v2"]}`, true},
		{"codex", true, `{"capabilities":["graph-execution-v1"]}`, false},
		{"codex", true, `{"capabilities":["graph-execution-v2"]}`, true},
		{"unknown", false, `{"capabilities":["graph-execution-v2"]}`, false},
	} {
		runtime := db.AgentRuntime{Provider: tc.provider, Metadata: []byte(tc.metadata), ProfileID: pgtype.UUID{Valid: tc.profile}}
		if got := graphRuntimeCompatible(runtime); got != tc.want {
			t.Fatalf("%+v got %v", tc, got)
		}
	}
}

func TestGraphSnapshotKeepsCredentialReferencesAndFreezesEndpoints(t *testing.T) {
	live := json.RawMessage(`{"mcpServers":{"tools":{"url":"https://tools.example/mcp?api_key=first","headers":{"Authorization":"Bearer secret"},"env":{"TOKEN":"secret"},"command":"fixed"}}}`)
	var value any
	if err := json.Unmarshal(live, &value); err != nil {
		t.Fatal(err)
	}
	var paths [][]string
	frozen, err := json.Marshal(graphFreezeMCP(value, nil, &paths))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(frozen), "secret") || strings.Contains(string(frozen), "first") {
		t.Fatalf("credential persisted: %s", frozen)
	}
	rotated := json.RawMessage(strings.ReplaceAll(strings.ReplaceAll(string(live), "first", "rotated"), "secret", "replacement"))
	hydrated, err := graphHydrateCredentials(frozen, rotated, paths)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hydrated), "replacement") || !strings.Contains(string(hydrated), "rotated") {
		t.Fatalf("credential rotation failed: %s", hydrated)
	}
	changed := json.RawMessage(strings.ReplaceAll(string(rotated), "tools.example", "different.example"))
	if _, err = graphHydrateCredentials(frozen, changed, paths); err == nil {
		t.Fatal("mutable MCP endpoint replaced the published endpoint")
	}
	if _, err = graphHydrateCredentials(frozen, json.RawMessage(`{}`), paths); err == nil {
		t.Fatal("missing credential binding silently omitted")
	}
}

func TestGraphSnapshotArrayCredentialPaths(t *testing.T) {
	var value any
	_ = json.Unmarshal([]byte(`{"servers":[{"token":"sensitive"}]}`), &value)
	var paths [][]string
	frozen, err := json.Marshal(graphFreezeMCP(value, nil, &paths))
	if err != nil {
		t.Fatal(err)
	}
	hydrated, err := graphHydrateCredentials(frozen, json.RawMessage(`{"servers":[{"token":"rotated"}]}`), paths)
	if err != nil || !strings.Contains(string(hydrated), "rotated") {
		t.Fatalf("nested credential binding failed: %s %v", hydrated, err)
	}
}

func TestGraphIssueExecutionFieldsCannotUseOrdinaryUpdate(t *testing.T) {
	for _, key := range []string{"description", "status", "assignee_id", "assignee_type", "stage", "parent_issue_id", "project_id"} {
		if !graphProtectedIssueFields(map[string]json.RawMessage{key: json.RawMessage(`null`)}) {
			t.Fatalf("%s unprotected", key)
		}
	}
	if graphProtectedIssueFields(map[string]json.RawMessage{"title": json.RawMessage(`"display"`), "priority": json.RawMessage(`"high"`)}) {
		t.Fatal("display-only edit blocked")
	}
}

func TestGraphSnapshotEnvironmentPinsBehaviorWithoutPersistingCredentials(t *testing.T) {
	first := graphEnvironmentValueDigests(map[string]string{"ANTHROPIC_BASE_URL": "https://api.deepseek.com/anthropic", "ANTHROPIC_AUTH_TOKEN": "first-secret", "REASONIX_HOME": "/profile/one", "MODE": "strict"})
	rotated := graphEnvironmentValueDigests(map[string]string{"ANTHROPIC_BASE_URL": "https://api.deepseek.com/anthropic", "ANTHROPIC_AUTH_TOKEN": "rotated-secret", "REASONIX_HOME": "/profile/one", "MODE": "strict"})
	raw, _ := json.Marshal(first)
	next, _ := json.Marshal(rotated)
	if string(raw) != string(next) || strings.Contains(string(raw), "secret") || first["ANTHROPIC_AUTH_TOKEN"] != "" {
		t.Fatal("credential rotation changed a behavior snapshot")
	}
	changed := graphEnvironmentValueDigests(map[string]string{"ANTHROPIC_BASE_URL": "https://other.example", "REASONIX_HOME": "/profile/two", "MODE": "unsafe"})
	for _, key := range []string{"ANTHROPIC_BASE_URL", "REASONIX_HOME", "MODE"} {
		if first[key] == changed[key] || first[key] == "" {
			t.Fatalf("mutable setting %s was not pinned", key)
		}
	}
}
