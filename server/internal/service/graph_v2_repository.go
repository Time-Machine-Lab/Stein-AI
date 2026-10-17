package service

import (
	"strings"

	"github.com/multica-ai/multica/server/internal/graph"
)

func graphRepositoryForNodeV2(run GraphRunRecord, root graph.Definition, target GraphNodeRunRecord) (*graph.RepositoryInput, error) {
	if run.Repository == nil {
		return nil, nil
	}
	r := *run.Repository
	before, tasks := graphExecutionOrderV2(run, root)
	var candidates []graph.Artifact
	for _, artifact := range run.Artifacts {
		producer, ok := tasks[artifact.ProducerTaskID]
		if ok && producer.State == "succeeded" && artifact.Type == graph.ArtifactGitCommit && artifact.RepositoryID == r.RepositoryID && before(producer, target, 0) {
			candidates = append(candidates, artifact)
		}
	}
	if len(candidates) == 0 {
		return &r, nil
	}
	for _, candidate := range candidates {
		dominates := true
		for _, other := range candidates {
			if other.CommitSHA != candidate.CommitSHA && !before(tasks[other.ProducerTaskID], tasks[candidate.ProducerTaskID], 0) {
				dominates = false
				break
			}
		}
		if dominates {
			r.CommitSHA = candidate.CommitSHA
			r.ArtifactID = candidate.ID
			return &r, nil
		}
	}
	return nil, graphError("ambiguous_repository_input", "parallel dependencies produced different commits; use one code-producing dependency path or start a new run from the selected commit")
}

// graphExecutionOrderV2 shares scoped ancestry for repository and file inputs.
func graphExecutionOrderV2(run GraphRunRecord, root graph.Definition) (func(GraphNodeRunRecord, GraphNodeRunRecord, int) bool, map[string]GraphNodeRunRecord) {
	nodes := map[string]GraphNodeRunRecord{}
	tasks := map[string]GraphNodeRunRecord{}
	scopes := map[string]GraphScopeRecord{}
	for _, n := range run.Nodes {
		nodes[n.ID] = n
		for _, a := range n.Attempts {
			tasks[a.TaskID] = n
		}
	}
	for _, s := range run.Scopes {
		scopes[s.ID] = s
	}
	var before func(GraphNodeRunRecord, GraphNodeRunRecord, int) bool
	before = func(a, b GraphNodeRunRecord, depth int) bool {
		if depth > 2*(graph.MaxGraphDepth+1) || a.ID == b.ID {
			return false
		}
		sa, oka := scopes[a.ScopeID]
		sb, okb := scopes[b.ScopeID]
		if !oka || !okb {
			return false
		}
		if a.ScopeID == b.ScopeID {
			d, err := scopeDefinitionV2(root, sa.DefinitionPath)
			return err == nil && graphAncestorsV2(d, b.NodeKey)[a.NodeKey]
		}
		if sa.ParentNodeRunID != "" && sa.ParentNodeRunID == sb.ParentNodeRunID {
			return sa.Iteration < sb.Iteration
		}
		level := func(path string) int {
			if path == "" {
				return 0
			}
			return (strings.Count(path, "/") + 1) / 2
		}
		if level(sa.DefinitionPath) >= level(sb.DefinitionPath) {
			parent, ok := nodes[sa.ParentNodeRunID]
			return ok && before(parent, b, depth+1)
		}
		parent, ok := nodes[sb.ParentNodeRunID]
		return ok && before(a, parent, depth+1)
	}
	return before, tasks
}

func graphInputArtifactsV2(run GraphRunRecord, root graph.Definition, target GraphNodeRunRecord) []graph.Artifact {
	before, tasks := graphExecutionOrderV2(run, root)
	var inputs []graph.Artifact
	for _, artifact := range run.Artifacts {
		producer, ok := tasks[artifact.ProducerTaskID]
		if ok && producer.State == "succeeded" && before(producer, target, 0) {
			inputs = append(inputs, artifact)
		}
	}
	return inputs
}
