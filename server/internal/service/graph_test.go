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

// A runtime that dies mid-attempt leaves a terminal attempt with no stop
// receipt, and the platform blocks the node as execution_unknown to ask a person
// about it. That block is the question; clearing it is the answer, so the person
// must not be refused by the same missing receipt - the run would have no way to
// advance and no way to be cancelled.
func TestGraphResolveBlockAnswersExecutionUnknownWithoutReceipt(t *testing.T) {
	node := GraphNodeRunRecord{State: "blocked", BlockReason: "execution_unknown", MaxAttempts: 2, Attempts: []GraphAttemptRecord{{State: "failed"}}}
	if err := graphResolveBlockAllowed(node); err != nil {
		t.Fatalf("person could not answer the platform's own question: %v", err)
	}
	if err := graphRetryAllowed(node); err == nil {
		t.Fatal("the agent's own retry still requires the physical stop receipt")
	}
	// A task the platform has not declared terminal is a live process, and no
	// decision may start a second attempt beside it.
	for _, state := range []string{"running", "dispatched", "queued", "deferred"} {
		live := node
		live.Attempts = []GraphAttemptRecord{{State: state}}
		if err := graphResolveBlockAllowed(live); err == nil {
			t.Fatalf("%s attempt treated as stopped", state)
		}
	}
}

func TestGraphGrantUnblockOnlyWidensASpentBudget(t *testing.T) {
	node := GraphNodeRunRecord{State: "blocked", MaxAttempts: 2, Attempts: []GraphAttemptRecord{{State: "failed"}}}
	graphGrantUnblock(&node, "member-1", "environment was fixed")
	if node.GrantedAttempts != 0 || node.attemptBudget() != 2 {
		t.Fatalf("budget widened with attempts left: %+v", node)
	}
	if node.HumanNote == nil || node.HumanNote.Reason != "environment was fixed" || node.HumanNote.MemberID != "member-1" {
		t.Fatalf("person's reason was not kept: %+v", node.HumanNote)
	}
	// The node the run dead-ended on: every attempt the agent was allowed is
	// spent, so the release has to grant the next one or the scheduler refuses
	// to create it and the run fails instead of continuing.
	spent := GraphNodeRunRecord{State: "blocked", MaxAttempts: 2, Attempts: []GraphAttemptRecord{{State: "failed"}, {State: "failed"}}}
	if err := graphResolveBlockAllowed(spent); err != nil {
		t.Fatal(err)
	}
	graphGrantUnblock(&spent, "member-1", "")
	if spent.GrantedAttempts != 1 || spent.attemptBudget() != 3 {
		t.Fatalf("spent budget was not widened: %+v", spent)
	}
	if spent.HumanNote != nil {
		t.Fatal("an empty reason replayed a stale instruction")
	}
	admission := graph.Admission{RunState: graph.RunActive, ControlState: graph.ControlRun, NodeState: graph.NodeReady, Authorized: true, CapabilitiesPresent: true, PreviousExecutionStopped: true, MaxAttempts: spent.MaxAttempts, Granted: spent.GrantedAttempts, Attempts: len(spent.Attempts)}
	if err := graph.CanSchedule(admission); err != nil {
		t.Fatalf("granted attempt refused by the scheduler: %v", err)
	}
	// Without the grant the same node is exactly the dead end the person hit.
	admission.Granted = 0
	if err := graph.CanSchedule(admission); err == nil {
		t.Fatal("spent budget accepted a further attempt")
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
