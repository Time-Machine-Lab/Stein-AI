package graph

import "testing"

func testAdmission() Admission {
	return Admission{RunState: RunActive, ControlState: ControlRun, NodeState: NodeReady, Attempts: 1, MaxAttempts: 3, PreviousExecutionStopped: true, Authorized: true, CapabilitiesPresent: true}
}
func testIdentity() AttemptIdentity {
	return AttemptIdentity{RunID: "run-1", NodeRunID: "node-1", TaskID: "task-1", AttemptNo: 1, ExecutionGeneration: 1, InputHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
}

func TestExecutionHonorsBudgetsAboveThree(t *testing.T) {
	for _, budget := range []int{16, 100} {
		a := testAdmission()
		a.MaxAttempts = budget
		a.Attempts = 0
		if err := CanSchedule(a); err != nil {
			t.Fatalf("budget %d: first attempt rejected: %v", budget, err)
		}
		a.Attempts = budget - 1
		if err := CanRetry(a); err != nil {
			t.Fatalf("budget %d: last retry rejected: %v", budget, err)
		}
		c := Completion{RunState: RunActive, ControlState: ControlRun, TaskState: "failed", ExecutionStopped: true, Retryable: true, Attempts: budget - 1, MaxAttempts: budget}
		if got := DecideCompletion(c); !got.Retry || got.NodeState != NodeReady {
			t.Fatalf("budget %d: retryable failure did not retry: %+v", budget, got)
		}
		a.NodeState = NodeActive
		a.Attempts = budget
		if err := CanStart(a); err != nil {
			t.Fatalf("budget %d: final attempt could not start: %v", budget, err)
		}
		a.NodeState = NodeReady
		if err := CanRetry(a); err == nil {
			t.Fatalf("budget %d: exhausted budget allowed another retry", budget)
		}
		c.Attempts = budget
		if got := DecideCompletion(c); got.Retry || got.RunState != RunFailed || got.Reason != "budget_exhausted" {
			t.Fatalf("budget %d: exhausted budget did not fail: %+v", budget, got)
		}
		a.NodeState = NodeActive
		a.Attempts = budget + 1
		if err := CanStart(a); err == nil {
			t.Fatalf("budget %d: attempt beyond budget could start", budget)
		}
	}
}

func TestAdmissionFailsClosedForPauseCancelUnknownAndBudget(t *testing.T) {
	if err := CanRetry(testAdmission()); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		modify func(*Admission)
	}{
		{"paused", func(a *Admission) { a.ControlState = ControlPause }},
		{"cancel", func(a *Admission) { a.ControlState = ControlCancelRequested }},
		{"unknown_execution", func(a *Admission) { a.PreviousExecutionStopped = false }},
		{"exhausted", func(a *Admission) { a.Attempts = 3 }},
		{"accepted", func(a *Admission) { a.OutputAccepted = true }},
		{"unauthorized", func(a *Admission) { a.Authorized = false }},
		{"old_daemon", func(a *Admission) { a.CapabilitiesPresent = false }},
		{"terminal", func(a *Admission) { a.RunState = RunSucceeded }},
		{"deadline", func(a *Admission) { a.DeadlineExceeded = true }},
		{"unknown_control", func(a *Admission) { a.ControlState = "something_new" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := testAdmission()
			tt.modify(&a)
			if err := CanRetry(a); err == nil {
				t.Fatal("retry unexpectedly permitted")
			}
		})
	}
}

func TestResultAcceptanceFencesStaleTaskAndInputButAllowsPausedCompletion(t *testing.T) {
	expected := testIdentity()
	if err := CanAcceptResult(RunActive, ControlPause, expected, expected); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*AttemptIdentity){func(i *AttemptIdentity) { i.TaskID = "old-task" }, func(i *AttemptIdentity) { i.ExecutionGeneration++ }, func(i *AttemptIdentity) {
		i.InputHash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	}, func(i *AttemptIdentity) { i.AttemptNo++ }, func(i *AttemptIdentity) { i.RunID = "other-run" }} {
		actual := expected
		mutate(&actual)
		if err := CanAcceptResult(RunActive, ControlRun, expected, actual); err == nil {
			t.Fatal("stale result accepted")
		}
	}
	if err := CanAcceptResult(RunActive, ControlCancelRequested, expected, expected); err == nil {
		t.Fatal("cancelled run accepted result")
	}
}

func TestCompletionRequiresStoppedExecutorFinalSubmissionAndDelivery(t *testing.T) {
	result := Result{Identity: testIdentity(), Outcome: OutcomePass}
	base := Completion{RunState: RunActive, ControlState: ControlRun, Expected: testIdentity(), Result: &result, TaskState: "completed", ExecutionStopped: true, DeliveryStatus: "delivered", OutputValid: true, ArtifactsValid: true, Attempts: 1, MaxAttempts: 3}
	if got := DecideCompletion(base); got.NodeState != NodeSucceeded || !got.Advance {
		t.Fatalf("pass: %+v", got)
	}
	tests := []struct {
		name   string
		change func(*Completion)
		node   NodeState
		reason string
	}{
		{"execution_unknown", func(c *Completion) { c.ExecutionStopped = false }, NodeBlocked, "execution_unknown"},
		{"missing_submission", func(c *Completion) { c.Result = nil }, NodeBlocked, "output_invalid"},
		{"invalid_output", func(c *Completion) { c.OutputValid = false }, NodeBlocked, "output_invalid"},
		{"delivery_failed", func(c *Completion) { c.DeliveryStatus = "failed" }, NodeBlocked, "artifact_unavailable"},
		{"bad_artifacts", func(c *Completion) { c.ArtifactsValid = false }, NodeBlocked, "artifact_unavailable"},
		{"external_cancel", func(c *Completion) { c.TaskState = "cancelled" }, NodeBlocked, "external_interrupt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := base
			tt.change(&c)
			got := DecideCompletion(c)
			if got.NodeState != tt.node || got.Reason != tt.reason || got.Advance {
				t.Fatalf("got %+v", got)
			}
		})
	}
	paused := base
	paused.ControlState = ControlPause
	if got := DecideCompletion(paused); got.NodeState != NodeSucceeded || got.Advance {
		t.Fatalf("paused completion: %+v", got)
	}
}

func TestNonPassOutcomesDoNotRequirePassArtifactsAndCanEndPausedRun(t *testing.T) {
	for _, outcome := range []Outcome{OutcomeEnvironmentBlocked, OutcomeNeedsChanges} {
		c := Completion{RunState: RunActive, ControlState: ControlPause, Expected: testIdentity(), Result: &Result{Identity: testIdentity(), Outcome: outcome}, TaskState: "completed", ExecutionStopped: true, OutputValid: true, DeliveryStatus: "failed"}
		got := DecideCompletion(c)
		if got.Advance || (outcome == OutcomeEnvironmentBlocked && got.NodeState != NodeBlocked) || (outcome == OutcomeNeedsChanges && got.RunState != RunRejected) {
			t.Fatalf("outcome %s: %+v", outcome, got)
		}
	}
}

func TestTechnicalRetryIntentSurvivesPauseAndNeverExceedsBudget(t *testing.T) {
	c := Completion{RunState: RunActive, ControlState: ControlPause, Expected: testIdentity(), TaskState: "failed", ExecutionStopped: true, Retryable: true, Attempts: 1, MaxAttempts: 3}
	if got := DecideCompletion(c); !got.Retry || got.Advance {
		t.Fatalf("expected durable retry intent: %+v", got)
	}
	c.Attempts = 3
	if got := DecideCompletion(c); got.Retry || got.RunState != RunFailed {
		t.Fatalf("budget must end run even when paused: %+v", got)
	}
}

func TestApprovalRequiresCurrentDigestAndRunningControl(t *testing.T) {
	digest := testIdentity().InputHash
	if err := CanApprove(RunWaitingApproval, ControlRun, digest, digest); err != nil {
		t.Fatal(err)
	}
	for _, control := range []ControlState{ControlPause, ControlCancelRequested} {
		if err := CanApprove(RunWaitingApproval, control, digest, digest); err == nil {
			t.Fatal("approval permitted during control hold")
		}
	}
	if err := CanApprove(RunWaitingApproval, ControlRun, digest, "stale"); err == nil {
		t.Fatal("stale approval accepted")
	}
}
