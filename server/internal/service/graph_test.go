package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/graph"
)

func TestGraphRetryRequiresPhysicalTermination(t *testing.T) {
	node := GraphNodeRunRecord{State: "blocked", MaxAttempts: 3, Attempts: []GraphAttemptRecord{{State: "failed"}}}
	if err := graphRetryAllowed(node); err == nil {
		t.Fatal("failed queue state is not proof of physical termination")
	}
	now := time.Now()
	node.Attempts[0].StoppedAt = &now
	if err := graphRetryAllowed(node); err != nil {
		t.Fatal(err)
	}
	node.MaxAttempts = 1
	if err := graphRetryAllowed(node); err == nil {
		t.Fatal("manual retry must not exceed the published budget")
	}
}

func TestGraphSnapshotBoundsAtPublish(t *testing.T) {
	for _, config := range []string{"null", "[]", `"string"`, `{"large":"` + strings.Repeat("x", graph.MaxJSONBytes) + `"}`} {
		if err := graphValidateSnapshot(GraphNodeSnapshot{AgentConfig: json.RawMessage(config)}); err == nil {
			t.Fatal("invalid agent snapshot accepted")
		}
	}
	if err := graphValidateSnapshot(GraphNodeSnapshot{AgentConfig: json.RawMessage(`{}`), Skills: make([]graph.SkillSnapshot, 65)}); err == nil {
		t.Fatal("unbounded skill snapshot accepted")
	}
	if err := graphValidateSnapshot(GraphNodeSnapshot{AgentConfig: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
}

func TestGraphPausedRunCannotApprove(t *testing.T) {
	run := GraphRunRecord{State: "waiting_approval", ControlState: "pause", Approval: &GraphApprovalRecord{ID: "request", State: "pending", CandidateDigest: "digest", ApproverID: "member", Revision: 2}}
	if err := graphApprovalAllowed(run, "member", GraphApprovalParams{ApprovalID: "request", CandidateDigest: "digest", Decision: "approved"}); err == nil {
		t.Fatal("paused approval accepted")
	}
	run.ControlState = "run"
	if err := graphApprovalAllowed(run, "member", GraphApprovalParams{ApprovalID: "request", CandidateDigest: "old", Decision: "approved"}); err == nil {
		t.Fatal("stale candidate accepted")
	}
	if err := graphApprovalAllowed(run, "member", GraphApprovalParams{ApprovalID: "request", CandidateDigest: "digest", Decision: "approved"}); err != nil {
		t.Fatal(err)
	}
}

func TestGraphReceiptIdentityIsImmutable(t *testing.T) {
	identity := graph.AttemptIdentity{RunID: "run", NodeRunID: "node", TaskID: "task", AttemptNo: 1, ExecutionGeneration: 1, InputHash: strings.Repeat("a", 64)}
	previous := GraphTaskReceipt{Identity: identity, SubmissionID: "first", DeliveryStatus: "delivered", StoppedAt: time.Now(), ExecutorID: "daemon"}
	replacement := previous
	replacement.SubmissionID = "second"
	if err := graphValidateReceipt(identity, &previous, replacement); err == nil {
		t.Fatal("final submission replaced")
	}
	if err := graphValidateReceipt(identity, &previous, previous); err != nil {
		t.Fatal(err)
	}
	replacement = previous
	replacement.Identity.ExecutionGeneration++
	if err := graphValidateReceipt(identity, nil, replacement); err == nil {
		t.Fatal("stale generation accepted")
	}
}
