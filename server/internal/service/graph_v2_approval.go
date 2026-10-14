package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/graph"
)

func graphCurrentApprovalV2(run *GraphRunRecord) {
	run.Approval = nil
	for i := range run.Approvals {
		if run.Approvals[i].State == "pending" {
			copy := run.Approvals[i]
			run.Approval = &copy
			return
		}
	}
}
func graphRequestApprovalV2(ctx context.Context, tx pgx.Tx, run *GraphRunRecord, index int, node graph.Node, version GraphVersionRecord) error {
	n := &run.Nodes[index]
	for _, a := range run.Approvals {
		if a.NodeRunID == n.ID && a.State == "pending" {
			n.State = "waiting_approval"
			return nil
		}
	}
	config := graph.ApprovalConfig{ApproverIDs: []string{node.ApproverID}, Mode: "all"}
	if node.Approval != nil {
		config = *node.Approval
	}
	scope := run.Scopes[graphScopeIndexV2(*run, n.ScopeID)]
	evaluation := graphScopeContextV2(*run, scope)
	input, err := graph.ResolveScopedInputs(node.Inputs, evaluation)
	if err != nil {
		graphBlockNodeV2(run, index, "input_invalid", err)
		return nil
	}
	n.Input = input
	_, n.InputHash, err = graph.CanonicalJSON(input)
	if err != nil {
		return err
	}
	candidate := map[string]any{"version_hash": version.ContentHash, "input_hash": run.InputHash, "node_run_id": n.ID, "inputs": json.RawMessage(input), "outputs": evaluation.Outputs, "artifacts": run.Artifacts}
	digest, err := graphHash(candidate)
	if err != nil {
		return err
	}
	raw, err := graphJSON(candidate)
	if err != nil {
		return err
	}
	// The compatibility `approval` field may carry a second copy of this candidate.
	if !graphPayloadFitsV2(*run, 2*len(raw)+8192) {
		n.State = "failed"
		graphTerminateV2(run, "failed", "storage_budget_exhausted")
		return nil
	}
	now := time.Now().UTC()
	run.Approvals = append(run.Approvals, GraphApprovalRecord{ID: uuid.NewString(), NodeRunID: n.ID, ApproverID: config.ApproverIDs[0], ApproverIDs: config.ApproverIDs, Mode: config.Mode, Quorum: config.Quorum, CandidateDigest: digest, Candidate: raw, State: "pending", Revision: 1, CreatedAt: now, Votes: []GraphApprovalVote{}})
	n.State = "waiting_approval"
	n.StartedAt = &now
	graphEvent(run, "approval.requested", "", n.InstanceKey)
	return graphCheckApprovalsV2(ctx, tx, run)
}

func graphCheckApprovalsV2(ctx context.Context, tx pgx.Tx, run *GraphRunRecord) error {
	for _, a := range run.Approvals {
		if a.State != "pending" {
			continue
		}
		index := graphNodeIndexV2(*run, a.NodeRunID)
		if index < 0 {
			return graphError("scope_invalid", "approval node is missing")
		}
		votes := map[string]string{}
		possible := 0
		for _, v := range a.Votes {
			votes[v.MemberID] = v.Decision
			if v.Decision == "approved" {
				possible++
			}
		}
		for _, member := range a.ApproverIDs {
			if votes[member] != "" {
				continue
			}
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM member WHERE workspace_id=$1 AND id=$2)`, run.WorkspaceID, member).Scan(&exists); err != nil {
				return err
			}
			if exists {
				possible++
			}
		}
		required := len(a.ApproverIDs)
		if a.Mode == "any" {
			required = 1
		} else if a.Mode == "quorum" {
			required = a.Quorum
		}
		if possible < required {
			if run.Nodes[index].BlockReason != "approver_unavailable" {
				graphBlockNodeV2(run, index, "approver_unavailable", nil)
			}
		} else {
			run.Nodes[index].State = "waiting_approval"
			run.Nodes[index].BlockReason = ""
		}
	}
	return nil
}

func (s *GraphService) approveV2(ctx context.Context, tx pgx.Tx, run GraphRunRecord, member, user string, p GraphApprovalParams, hash string) (GraphRunRecord, error) {
	if graphTerminal(run.State) || run.ControlState != "run" {
		return run, graphError("invalid_state", "approval requires an unpaused nonterminal run")
	}
	if run.Deadline != nil && !run.Deadline.After(time.Now()) {
		return run, graphError("deadline_exceeded", "run deadline reached")
	}
	if p.Decision != "approved" && p.Decision != "rejected" {
		return run, graphError("invalid_decision", "decision must be approved or rejected")
	}
	index := -1
	for i, a := range run.Approvals {
		if a.ID == p.ApprovalID {
			index = i
			break
		}
	}
	if index < 0 {
		return run, graphError("stale_approval", "approval no longer exists")
	}
	a := &run.Approvals[index]
	if a.State != "pending" || a.CandidateDigest != p.CandidateDigest {
		return run, graphError("stale_approval", "approval request or candidate changed")
	}
	allowed := false
	for _, id := range a.ApproverIDs {
		if id == member {
			allowed = true
		}
	}
	if !allowed {
		return run, graphError("forbidden", "only designated approvers can decide")
	}
	for _, vote := range a.Votes {
		if vote.MemberID == member {
			return run, graphError("already_decided", "this member already voted")
		}
	}
	nodeIndex := graphNodeIndexV2(run, a.NodeRunID)
	if nodeIndex < 0 || run.Nodes[nodeIndex].State != "waiting_approval" {
		return run, graphError("stale_approval", "approval node is not waiting")
	}
	now := time.Now().UTC()
	vote := GraphApprovalVote{MemberID: member, UserID: user, Decision: p.Decision, Reason: p.Reason, CreatedAt: now}
	voteJSON, err := graphJSON(vote)
	if err != nil {
		return run, err
	}
	if !graphPayloadFitsV2(run, 3*len(voteJSON)+4096) {
		return run, graphError("storage_budget_exhausted", "approval decision exceeds run storage budget")
	}
	a.Votes = append(a.Votes, vote)
	a.Revision++
	approved := 0
	for _, v := range a.Votes {
		if v.Decision == "approved" {
			approved++
		}
	}
	required := len(a.ApproverIDs)
	if a.Mode == "any" {
		required = 1
	} else if a.Mode == "quorum" {
		required = a.Quorum
	}
	if approved >= required {
		a.State = "approved"
		a.DecidedBy = user
		a.DecidedAt = &now
		a.Reason = p.Reason
		graphSucceedNodeV2(&run.Nodes[nodeIndex])
		run.State = "active"
		run.BlockReason = ""
	} else if approved+len(a.ApproverIDs)-len(a.Votes) < required {
		a.State = "rejected"
		a.DecidedBy = user
		a.DecidedAt = &now
		a.Reason = p.Reason
		run.Nodes[nodeIndex].State = "rejected"
		run.Nodes[nodeIndex].FinishedAt = &now
		graphTerminateV2(&run, "rejected", p.Reason)
	}
	graphEvent(&run, "approval.vote", user, a.ID+":"+p.Decision)
	graphCurrentApprovalV2(&run)
	if err := graphOperation(ctx, tx, run, fmt.Sprintf("approval:%s:%d", a.ID, a.Revision), "reconcile"); err != nil {
		return run, err
	}
	if err := graphPersistRun(ctx, tx, &run); err != nil {
		return run, err
	}
	if err := graphSaveCommand(ctx, tx, run.WorkspaceID, p.IdempotencyKey, hash, run); err != nil {
		return run, err
	}
	if err := tx.Commit(ctx); err != nil {
		return run, err
	}
	if s.NotifyRun != nil {
		s.NotifyRun(ctx, run)
	}
	return run, nil
}

func graphReplaceApproverV2(ctx context.Context, tx pgx.Tx, run *GraphRunRecord, user string, p GraphControlParams) error {
	if err := graphUUIDs(p.ApproverID); err != nil {
		return err
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM member WHERE workspace_id=$1 AND id=$2)`, run.WorkspaceID, p.ApproverID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return graphError("approver_unavailable", "replacement member is unavailable")
	}
	index := -1
	for i, a := range run.Approvals {
		if a.State == "pending" && (p.NodeRunID == "" || a.NodeRunID == p.NodeRunID) {
			if index >= 0 {
				return graphError("approval_required", "select the exact approval node")
			}
			index = i
		}
	}
	if index < 0 {
		return graphError("invalid_state", "there is no pending approval for this node")
	}
	old := run.Approvals[index]
	oldJSON, err := graphJSON(old)
	if err != nil {
		return err
	}
	if !graphPayloadFitsV2(*run, len(oldJSON)+8192) {
		return graphError("storage_budget_exhausted", "approval replacement exceeds run storage budget")
	}
	previous := p.PreviousApproverID
	if previous == "" {
		if len(old.ApproverIDs) != 1 {
			return graphError("previous_approver_required", "select the exact member being replaced")
		}
		previous = old.ApproverIDs[0]
	}
	roster := append([]string(nil), old.ApproverIDs...)
	found := false
	for i, id := range roster {
		if id == p.ApproverID {
			return graphError("invalid_approver", "replacement is already an approver")
		}
		if id == previous {
			roster[i] = p.ApproverID
			found = true
		}
	}
	if !found {
		return graphError("invalid_approver", "previous member is not in this approval roster")
	}
	run.Approvals[index].State = "invalidated"
	now := time.Now().UTC()
	run.Approvals[index].DecidedAt = &now
	old.ID = uuid.NewString()
	old.ApproverIDs = roster
	old.ApproverID = roster[0]
	old.Votes = []GraphApprovalVote{}
	old.Revision++
	old.CreatedAt = now
	old.State = "pending"
	old.Reason = ""
	old.DecidedBy = ""
	old.DecidedAt = nil
	run.Approvals = append(run.Approvals, old)
	nodeIndex := graphNodeIndexV2(*run, old.NodeRunID)
	if nodeIndex < 0 {
		return graphError("scope_invalid", "approval node missing")
	}
	run.Nodes[nodeIndex].State = "waiting_approval"
	run.Nodes[nodeIndex].BlockReason = ""
	graphCurrentApprovalV2(run)
	graphAggregateStateV2(run)
	graphEvent(run, "approval.reassigned", user, old.ID+": prior votes invalidated; "+previous+" -> "+p.ApproverID)
	return nil
}
