package service

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/graph"
)

func TestGraphV2RepositoryFollowsPreviousIterationAndCompletedSubgraph(t *testing.T) {
	body := graph.Definition{SchemaVersion: 2, Nodes: []graph.Node{{Key: "start", Type: graph.NodeTypeStart}, {Key: "work", Type: graph.NodeTypeAgent}, {Key: "end", Type: graph.NodeTypeEnd}}, Edges: []graph.Edge{{From: "start", To: "work"}, {From: "work", To: "end"}}}
	d := graph.Definition{SchemaVersion: 2, Nodes: []graph.Node{{Key: "start", Type: graph.NodeTypeStart}, {Key: "loop", Type: graph.NodeTypeLoop, Loop: &graph.LoopConfig{Body: body}}, {Key: "after", Type: graph.NodeTypeAgent}, {Key: "end", Type: graph.NodeTypeEnd}}, Edges: []graph.Edge{{From: "start", To: "loop"}, {From: "loop", To: "after"}, {From: "after", To: "end"}}}
	run := GraphRunRecord{Repository: &graph.RepositoryInput{RepositoryID: "repo", BaseSHA: strings.Repeat("0", 40), CommitSHA: strings.Repeat("0", 40)}, Scopes: []GraphScopeRecord{{ID: "root"}, {ID: "iteration1", ParentNodeRunID: "loop", DefinitionPath: "loop/body", Iteration: 1}, {ID: "iteration2", ParentNodeRunID: "loop", DefinitionPath: "loop/body", Iteration: 2}}, Nodes: []GraphNodeRunRecord{{ID: "loop", NodeKey: "loop", ScopeID: "root"}, {ID: "one", NodeKey: "work", ScopeID: "iteration1", State: "succeeded", Attempts: []GraphAttemptRecord{{TaskID: "task1"}}}, {ID: "two", NodeKey: "work", ScopeID: "iteration2"}, {ID: "after", NodeKey: "after", ScopeID: "root"}}, Artifacts: []graph.Artifact{{ID: "artifact1", Type: graph.ArtifactGitCommit, RepositoryID: "repo", CommitSHA: strings.Repeat("1", 40), ProducerTaskID: "task1"}}}
	for _, target := range []GraphNodeRunRecord{run.Nodes[2], run.Nodes[3]} {
		repo, err := graphRepositoryForNodeV2(run, d, target)
		if err != nil || repo.CommitSHA != strings.Repeat("1", 40) {
			t.Fatalf("%s did not inherit accepted predecessor commit: %+v %v", target.ID, repo, err)
		}
	}
}

func TestGraphV2RepositoryRejectsAmbiguousParallelOutputs(t *testing.T) {
	d := graph.Definition{SchemaVersion: 2, Nodes: []graph.Node{{Key: "left"}, {Key: "right"}, {Key: "join"}, {Key: "after"}}, Edges: []graph.Edge{{From: "left", To: "join"}, {From: "right", To: "join"}, {From: "join", To: "after"}}}
	run := GraphRunRecord{Repository: &graph.RepositoryInput{RepositoryID: "repo", CommitSHA: strings.Repeat("0", 40)}, Scopes: []GraphScopeRecord{{ID: "root"}}, Nodes: []GraphNodeRunRecord{{ID: "left", NodeKey: "left", ScopeID: "root", State: "succeeded", Attempts: []GraphAttemptRecord{{TaskID: "leftTask"}}}, {ID: "right", NodeKey: "right", ScopeID: "root", State: "succeeded", Attempts: []GraphAttemptRecord{{TaskID: "rightTask"}}}, {ID: "after", NodeKey: "after", ScopeID: "root"}}, Artifacts: []graph.Artifact{{ID: "leftArtifact", Type: graph.ArtifactGitCommit, RepositoryID: "repo", CommitSHA: strings.Repeat("1", 40), ProducerTaskID: "leftTask"}, {ID: "rightArtifact", Type: graph.ArtifactGitCommit, RepositoryID: "repo", CommitSHA: strings.Repeat("2", 40), ProducerTaskID: "rightTask"}}}
	if _, err := graphRepositoryForNodeV2(run, d, run.Nodes[2]); err == nil {
		t.Fatal("parallel commit chosen by completion order")
	}
	repo, err := graphRepositoryForNodeV2(run, d, run.Nodes[1])
	if err != nil || repo.CommitSHA != strings.Repeat("0", 40) {
		t.Fatal("parallel sibling output leaked into branch input")
	}
}
