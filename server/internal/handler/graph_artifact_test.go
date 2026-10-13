package handler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/graph"
	"github.com/multica-ai/multica/server/internal/service"
)

func TestGraphProducedArtifactUsesImmediateInputCommit(t *testing.T) {
	base, candidate, produced := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
	run := service.GraphRunRecord{ID: "run", WorkspaceID: "workspace"}
	node := service.GraphNodeRunRecord{NodeKey: "second", Spec: &graph.NodeSpec{Repository: &graph.RepositoryInput{RepositoryID: "repo", BaseSHA: base, CommitSHA: candidate}}}
	artifact := graph.Artifact{ID: "artifact", Type: graph.ArtifactGitCommit, RepositoryID: "repo", BaseSHA: candidate, CommitSHA: produced, ManagedRef: "refs/heads/multica/graphs/run/second/1", ProducerTaskID: "task"}
	result := graph.Result{Identity: graph.AttemptIdentity{TaskID: "task", AttemptNo: 1}, Artifacts: []graph.Artifact{artifact}}
	receipt := service.GraphTaskReceipt{Artifacts: json.RawMessage(`[{"artifact_id":"artifact","repository_id":"repo","commit_sha":"` + produced + `","managed_ref":"refs/heads/multica/graphs/run/second/1","remote_verified":true}]`)}
	h := &Handler{}
	if err := h.validateGraphArtifacts(context.Background(), nil, run, node, result, receipt); err != nil {
		t.Fatalf("second producer rejected immediate input: %v", err)
	}
	result.Artifacts[0].BaseSHA = base
	if err := h.validateGraphArtifacts(context.Background(), nil, run, node, result, receipt); err == nil {
		t.Fatal("stale run base accepted for second producer")
	}
}
