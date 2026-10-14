package graph

import "fmt"

// PlanReady reads persisted node states and branch decisions. It never evaluates
// a branch again, starts work, retries blocked executions, or assumes success.
func PlanReady(d Definition, states map[string]NodeState, selectedPorts map[string]string) (ReadinessPlan, error) {
	plan := ReadinessPlan{Ready: []string{}, Skipped: []string{}, Blocked: map[string]string{}}
	ordered, err := TopologicalNodes(d)
	if err != nil {
		return plan, err
	}
	nodes := map[string]Node{}
	incoming := map[string][]Edge{}
	resolved := map[string]NodeState{}
	for _, n := range ordered {
		nodes[n.Key] = n
		resolved[n.Key] = NodePending
	}
	for _, e := range d.Edges {
		incoming[e.To] = append(incoming[e.To], e)
	}
	for key, state := range states {
		if _, ok := nodes[key]; !ok {
			return plan, invalid("states", "node does not exist: "+key)
		}
		switch state {
		case NodePending, NodeReady, NodeActive, NodeValidating, NodeWaitingApproval, NodeBlocked, NodeSucceeded, NodeSkipped, NodeRejected, NodeFailed, NodeCancelled:
		default:
			return plan, invalid("states", "unknown node state")
		}
		resolved[key] = state
	}
	for key, port := range selectedPorts {
		if nodes[key].Type != NodeTypeBranch || (port != "true" && port != "false") {
			return plan, invalid("selected_ports", "requires a branch and a true/false port")
		}
	}
	for _, n := range ordered {
		if resolved[n.Key] != NodePending && resolved[n.Key] != NodeReady {
			continue
		}
		if n.Type == NodeTypeStart {
			plan.Ready = append(plan.Ready, n.Key)
			continue
		}
		waiting, selected, blocked := false, 0, ""
		for _, edge := range incoming[n.Key] {
			state := resolved[edge.From]
			if state == NodeSkipped {
				continue
			}
			if state == NodeSucceeded {
				if nodes[edge.From].Type == NodeTypeBranch {
					port, ok := selectedPorts[edge.From]
					if !ok {
						blocked = "branch_decision_missing"
						continue
					}
					if port != edge.Port {
						continue
					}
				}
				selected++
				continue
			}
			if reason := plan.Blocked[edge.From]; reason != "" {
				blocked = "dependency_blocked"
			} else {
				switch state {
				case NodeFailed, NodeRejected, NodeCancelled, NodeBlocked:
					blocked = "dependency_" + string(state)
				default:
					waiting = true
				}
			}
		}
		if blocked != "" {
			plan.Blocked[n.Key] = blocked
			continue
		}
		if waiting {
			continue
		}
		if selected == 0 {
			resolved[n.Key] = NodeSkipped
			plan.Skipped = append(plan.Skipped, n.Key)
		} else {
			plan.Ready = append(plan.Ready, n.Key)
		}
	}
	return plan, nil
}

// WalkNodes visits immutable definition paths, distinct from execution instance
// keys: loopKey/body/child or subgraphKey/definition/child.
func WalkNodes(d Definition, visitor func(path string, node Node) error) error {
	if visitor == nil {
		return fmt.Errorf("node visitor is required")
	}
	if err := ValidateDefinition(d); err != nil {
		return err
	}
	var walk func(Definition, string) error
	walk = func(current Definition, prefix string) error {
		nodes, err := orderedNodes(current)
		if err != nil {
			return err
		}
		for _, node := range nodes {
			path := prefix + node.Key
			if err := visitor(path, node); err != nil {
				return err
			}
			if node.Loop != nil {
				if err := walk(node.Loop.Body, path+"/body/"); err != nil {
					return err
				}
			}
			if node.Subgraph != nil {
				if err := walk(node.Subgraph.Definition, path+"/definition/"); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(d, "")
}
