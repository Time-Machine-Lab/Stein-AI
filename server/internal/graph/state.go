package graph

type RunState string

const (
	RunActive          RunState = "active"
	RunWaitingApproval RunState = "waiting_approval"
	RunBlocked         RunState = "blocked"
	RunSucceeded       RunState = "succeeded"
	RunRejected        RunState = "rejected"
	RunFailed          RunState = "failed"
	RunCancelled       RunState = "cancelled"
)

type ControlState string

const (
	ControlRun             ControlState = "run"
	ControlPause           ControlState = "pause"
	ControlCancelRequested ControlState = "cancel_requested"
)

type NodeState string

const (
	NodePending         NodeState = "pending"
	NodeReady           NodeState = "ready"
	NodeActive          NodeState = "active"
	NodeValidating      NodeState = "validating"
	NodeWaitingApproval NodeState = "waiting_approval"
	NodeBlocked         NodeState = "blocked"
	NodeSucceeded       NodeState = "succeeded"
	NodeRejected        NodeState = "rejected"
	NodeFailed          NodeState = "failed"
	NodeCancelled       NodeState = "cancelled"
)

// Error exposes stable machine-readable reasons independently of HTTP status codes.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string          { return e.Code + ": " + e.Message }
func denied(code, message string) error { return &Error{Code: code, Message: message} }

func IsTerminalRun(state RunState) bool {
	return state == RunSucceeded || state == RunRejected || state == RunFailed || state == RunCancelled
}
func IsTerminalNode(state NodeState) bool {
	return state == NodeSucceeded || state == NodeRejected || state == NodeFailed || state == NodeCancelled || state == NodeSkipped
}

type Admission struct {
	RunState     RunState
	ControlState ControlState
	NodeState    NodeState
	Attempts     int
	MaxAttempts  int
	// Granted counts the attempts people authorized beyond MaxAttempts by
	// clearing a blocked node. They are not the agent's own repeats, so they do
	// not consume the published budget; nor do they raise it, because each one
	// costs a person an explicit decision.
	Granted                  int
	PreviousExecutionStopped bool
	OutputAccepted           bool
	Authorized               bool
	CapabilitiesPresent      bool
	DeadlineExceeded         bool
}

// CanSchedule is checked under the same transaction lock as attempt creation.
func CanSchedule(a Admission) error {
	if a.RunState != RunActive && a.RunState != RunBlocked {
		return denied("run_inactive", "run does not accept scheduling")
	}
	if err := checkControl(a.ControlState); err != nil {
		return err
	}
	if a.DeadlineExceeded {
		return denied("deadline_exceeded", "run deadline has passed")
	}
	if !a.Authorized {
		return denied("authorization_revoked", "execution is not authorized")
	}
	if !a.CapabilitiesPresent {
		return denied("capability_missing", "executor lacks the Graph protocol")
	}
	if a.NodeState != NodeReady && a.NodeState != NodeBlocked {
		return denied("node_not_ready", "node is not ready for an attempt")
	}
	if a.OutputAccepted {
		return denied("output_accepted", "accepted output cannot be replaced")
	}
	if a.MaxAttempts < 1 || a.Granted < 0 || a.Attempts < 0 || a.Attempts >= a.MaxAttempts+a.Granted {
		return denied("budget_exhausted", "no attempt budget remains")
	}
	if a.Attempts > 0 && !a.PreviousExecutionStopped {
		return denied("execution_unknown", "previous execution has not been confirmed stopped")
	}
	return nil
}

func CanRetry(a Admission) error {
	if a.Attempts == 0 {
		return denied("attempt_missing", "retry requires an earlier attempt")
	}
	return CanSchedule(a)
}

// CanStart applies to claiming and starting an existing attempt, without consuming budget again.
func CanStart(a Admission) error {
	if a.RunState != RunActive {
		return denied("run_inactive", "run does not accept execution")
	}
	if err := checkControl(a.ControlState); err != nil {
		return err
	}
	if a.DeadlineExceeded {
		return denied("deadline_exceeded", "run deadline has passed")
	}
	if !a.Authorized {
		return denied("authorization_revoked", "execution is not authorized")
	}
	if !a.CapabilitiesPresent {
		return denied("capability_missing", "executor lacks the Graph protocol")
	}
	if a.NodeState != NodeActive || a.OutputAccepted {
		return denied("stale_attempt", "node does not own an active attempt")
	}
	if a.Attempts < 1 || a.MaxAttempts < 1 || a.Granted < 0 || a.Attempts > a.MaxAttempts+a.Granted {
		return denied("budget_exhausted", "attempt is outside the published budget")
	}
	return nil
}

func checkControl(state ControlState) error {
	switch state {
	case ControlRun:
		return nil
	case ControlPause:
		return denied("run_paused", "run is paused")
	case ControlCancelRequested:
		return denied("cancel_requested", "run cancellation has been requested")
	default:
		return denied("invalid_control_state", "unrecognized run control state")
	}
}

func CanAcceptResult(state RunState, control ControlState, expected, actual AttemptIdentity) error {
	if state != RunActive && state != RunBlocked {
		return denied("run_inactive", "run does not accept results")
	}
	if control != ControlRun && control != ControlPause {
		return denied("cancel_requested", "result acceptance is closed")
	}
	return ValidateIdentity(expected, actual)
}

func CanApprove(state RunState, control ControlState, expectedDigest, actualDigest string) error {
	if state != RunWaitingApproval {
		return denied("approval_inactive", "run is not awaiting approval")
	}
	if err := checkControl(control); err != nil {
		return err
	}
	if !hashPattern.MatchString(expectedDigest) || expectedDigest != actualDigest {
		return denied("candidate_mismatch", "approval must bind the current candidate digest")
	}
	return nil
}

// Completion contains facts verified by the service, never untrusted request booleans.
type Completion struct {
	RunState         RunState
	ControlState     ControlState
	Expected         AttemptIdentity
	Result           *Result
	TaskState        string
	ExecutionStopped bool
	DeliveryStatus   string
	OutputValid      bool
	ArtifactsValid   bool
	Retryable        bool
	Attempts         int
	MaxAttempts      int
	// Granted counts the attempts people authorized beyond MaxAttempts; see
	// Admission.Granted.
	Granted int
}

// Decision is persisted by the service together with any durable scheduling intent.
type Decision struct {
	NodeState NodeState
	RunState  RunState
	Reason    string
	Retry     bool
	Advance   bool
}

func DecideCompletion(c Completion) Decision {
	blocked := func(reason string) Decision {
		return Decision{NodeState: NodeBlocked, RunState: RunBlocked, Reason: reason}
	}
	if IsTerminalRun(c.RunState) {
		return Decision{RunState: c.RunState, Reason: "run_inactive"}
	}
	if c.RunState != RunActive && c.RunState != RunBlocked {
		return blocked("run_inactive")
	}
	if !c.ExecutionStopped {
		return blocked("execution_unknown")
	}
	if c.ControlState == ControlCancelRequested {
		return Decision{NodeState: NodeCancelled, RunState: c.RunState, Reason: "cancel_requested"}
	}
	if c.ControlState != ControlRun && c.ControlState != ControlPause {
		return blocked("invalid_control_state")
	}
	if c.TaskState == "cancelled" {
		return blocked("external_interrupt")
	}
	if c.TaskState == "failed" {
		if c.MaxAttempts < 1 || c.Granted < 0 || c.Attempts < 1 || c.Attempts >= c.MaxAttempts+c.Granted {
			return Decision{NodeState: NodeFailed, RunState: RunFailed, Reason: "budget_exhausted"}
		}
		if c.Retryable {
			return Decision{NodeState: NodeReady, RunState: RunActive, Reason: "technical_retry", Retry: true}
		}
		return Decision{NodeState: NodeFailed, RunState: RunFailed, Reason: "execution_failed"}
	}
	if c.TaskState != "completed" {
		return blocked("execution_unknown")
	}
	if c.Result == nil {
		return blocked("output_invalid")
	}
	if err := CanAcceptResult(c.RunState, c.ControlState, c.Expected, c.Result.Identity); err != nil {
		if e, ok := err.(*Error); ok {
			return blocked(e.Code)
		}
		return blocked("stale_attempt")
	}
	if !c.OutputValid {
		return blocked("output_invalid")
	}
	switch c.Result.Outcome {
	case OutcomeEnvironmentBlocked:
		return blocked("environment_blocked")
	case OutcomeNeedsChanges:
		return Decision{NodeState: NodeRejected, RunState: RunRejected, Reason: "needs_changes"}
	case OutcomePass:
		if !c.ArtifactsValid || (c.DeliveryStatus != "delivered" && c.DeliveryStatus != "not_required") {
			return blocked("artifact_unavailable")
		}
		return Decision{NodeState: NodeSucceeded, RunState: RunActive, Advance: c.ControlState == ControlRun}
	default:
		return blocked("output_invalid")
	}
}

func RunIssueStatus(state RunState) string {
	switch state {
	case RunActive:
		return "in_progress"
	case RunWaitingApproval:
		return "in_review"
	case RunBlocked, RunFailed:
		return "blocked"
	case RunSucceeded:
		return "done"
	case RunRejected:
		return "todo"
	case RunCancelled:
		return "cancelled"
	default:
		return "blocked"
	}
}
func NodeIssueStatus(state NodeState) string {
	switch state {
	case NodePending, NodeReady, NodeRejected:
		return "todo"
	case NodeActive, NodeValidating:
		return "in_progress"
	case NodeBlocked, NodeFailed:
		return "blocked"
	case NodeSucceeded:
		return "done"
	case NodeCancelled, NodeSkipped:
		return "cancelled"
	default:
		return "blocked"
	}
}
