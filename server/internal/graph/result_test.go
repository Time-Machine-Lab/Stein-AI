package graph

import (
	"encoding/json"
	"testing"
	"time"
)

func testNodeSpec(t *testing.T) NodeSpec {
	t.Helper()
	input := json.RawMessage(`{"goal":"build"}`)
	_, hash, err := CanonicalJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	return NodeSpec{ProtocolVersion: 1, Node: testDefinition().Nodes[1], Input: input, InputHash: hash, AgentConfig: json.RawMessage(`{"model":"frozen-model"}`)}
}

func TestAttemptManifestKeepsBusinessHashAndBindsNewTask(t *testing.T) {
	spec := testNodeSpec(t)
	first := testIdentity()
	first.InputHash = spec.InputHash
	a, err := NewAttemptManifest(spec, first)
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.TaskID = "task-2"
	second.AttemptNo = 2
	second.ExecutionGeneration = 2
	b, err := NewAttemptManifest(spec, second)
	if err != nil {
		t.Fatal(err)
	}
	_, ah, err := CanonicalManifest(a)
	if err != nil {
		t.Fatal(err)
	}
	_, bh, err := CanonicalManifest(b)
	if err != nil {
		t.Fatal(err)
	}
	if a.NodeSpecHash != b.NodeSpecHash || ah == bh || a.Identity.InputHash != b.Identity.InputHash {
		t.Fatal("attempt identity must change manifest hash, not business input/spec hash")
	}
	spec.Input[0] = '['
	spec.Node.Inputs["new"] = InputBinding{Source: BindingConstant, Value: json.RawMessage(`1`)}
	if _, _, err := CanonicalManifest(a); err != nil {
		t.Fatalf("manifest retained mutable aliases: %v", err)
	}
	a.Identity.InputHash = testIdentity().InputHash
	if _, _, err := CanonicalManifest(a); err == nil {
		t.Fatal("manifest accepted mismatched input hash")
	}
}

func TestResultValidatesPassContractButAcceptsNonPassDiagnostics(t *testing.T) {
	n := testDefinition().Nodes[1]
	if err := ValidateResult(n, Result{Identity: testIdentity(), Outcome: OutcomePass, Output: json.RawMessage(`{"summary":"done"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateResult(n, Result{Identity: testIdentity(), Outcome: OutcomePass, Output: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("invalid pass output accepted")
	}
	n.ExecutionMode = ExecutionProduceCommit
	if err := ValidateResult(n, Result{Identity: testIdentity(), Outcome: OutcomePass, Output: json.RawMessage(`{"summary":"done"}`)}); err == nil {
		t.Fatal("produce_commit pass without commit accepted")
	}
	for _, outcome := range []Outcome{OutcomeEnvironmentBlocked, OutcomeNeedsChanges} {
		if err := ValidateResult(n, Result{Identity: testIdentity(), Outcome: outcome, Diagnostics: json.RawMessage(`{"reason":"credentials missing"}`)}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStopReceiptRequiresExactExecutorAttempt(t *testing.T) {
	r := StopReceipt{Identity: testIdentity(), DaemonInstanceID: "daemon-1", StoppedAt: time.Now().UTC()}
	if err := ValidateStopReceipt(testIdentity(), r); err != nil {
		t.Fatal(err)
	}
	r.Identity.ExecutionGeneration++
	if err := ValidateStopReceipt(testIdentity(), r); err == nil {
		t.Fatal("stale generation stop accepted")
	}
	r.Identity = testIdentity()
	r.StoppedAt = time.Time{}
	if err := ValidateStopReceipt(testIdentity(), r); err == nil {
		t.Fatal("missing stop evidence accepted")
	}
}

func TestNodeSpecAcceptsContentAddressedSkillBundleHashes(t *testing.T) {
	spec := testNodeSpec(t)
	spec.Skills = []SkillSnapshot{{ID: "skill-1", Hash: "sha256:" + spec.InputHash, BundleRef: "skill-1@" + spec.InputHash}}
	if _, _, err := CanonicalNodeSpec(spec); err != nil {
		t.Fatal(err)
	}
}

func TestCanonicalJSONPreservesLargeNumbersAndNormalizesSpelling(t *testing.T) {
	a, ah, err := CanonicalJSON(json.RawMessage(`{"big":9007199254740993,"number":1.200e2}`))
	if err != nil {
		t.Fatal(err)
	}
	_, bh, err := CanonicalJSON(json.RawMessage(`{"number":120,"big":9007199254740993}`))
	if err != nil {
		t.Fatal(err)
	}
	if ah != bh || string(a) != `{"big":9007199254740993,"number":120}` {
		t.Fatalf("lost numeric identity: %s", a)
	}
}
