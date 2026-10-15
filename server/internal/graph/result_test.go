package graph

import (
	"bytes"
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
	a, err := NewAttemptManifest(spec, first, AttemptGrant{})
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.TaskID = "task-2"
	second.AttemptNo = 2
	second.ExecutionGeneration = 2
	b, err := NewAttemptManifest(spec, second, AttemptGrant{})
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

func TestV2ScriptManifestAndResultReuseBoundedTaskContract(t *testing.T) {
	spec := testNodeSpec(t)
	spec.Node = Node{Key: "transform", Type: NodeTypeScript, AgentID: "agent-1", Script: &ScriptConfig{Language: "javascript", Code: "return input;", TimeoutSeconds: 60}, MaxAttempts: 2, TimeoutSeconds: 60, ExecutionMode: ExecutionNone, OutputSchema: json.RawMessage(`{"type":"object"}`)}
	identity := testIdentity()
	identity.InputHash = spec.InputHash
	manifest, err := NewAttemptManifest(spec, identity, AttemptGrant{})
	if err != nil {
		t.Fatalf("valid script manifest rejected: %v", err)
	}
	if _, _, err := CanonicalManifest(manifest); err != nil {
		t.Fatal(err)
	}
	if err := ValidateResult(spec.Node, Result{Identity: identity, Outcome: OutcomePass, Output: json.RawMessage(`{"value":1}`)}); err != nil {
		t.Fatalf("valid script output rejected: %v", err)
	}
	spec.Node.ExecutionMode = ExecutionProduceCommit
	if _, _, err := CanonicalNodeSpec(spec); err == nil {
		t.Fatal("script code delivery accepted")
	}
}

func TestV2NodeSpecFreezesInstanceIdentity(t *testing.T) {
	spec := testNodeSpec(t)
	raw, _ := json.Marshal(spec)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	fields["instance_key"] = json.RawMessage(`"scope-1/implement"`)
	raw, _ = json.Marshal(fields)
	var first NodeSpec
	if err := json.Unmarshal(raw, &first); err != nil {
		t.Fatal(err)
	}
	_, a, err := CanonicalNodeSpec(first)
	if err != nil {
		t.Fatal(err)
	}
	fields["instance_key"] = json.RawMessage(`"scope-2/implement"`)
	raw, _ = json.Marshal(fields)
	var second NodeSpec
	if err := json.Unmarshal(raw, &second); err != nil {
		t.Fatal(err)
	}
	_, b, err := CanonicalNodeSpec(second)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("distinct loop instances share a spec digest")
	}
}

// Artifact identities are persisted in a uuid column. An ID that is not a UUID
// used to pass submission and then fail reconciliation forever, wedging the
// run in an unrecoverable loop; it must be rejected when the result is
// submitted so the agent can correct it.
func TestResultRejectsNonUUIDArtifactIDAtSubmission(t *testing.T) {
	n := testDefinition().Nodes[1]
	n.ExecutionMode = ExecutionProduceCommit
	id := testIdentity()
	sha := "3cbd6fad2097feff64ecd57deaa2d6df4f275d21"
	build := func(artifactID string) Result {
		return Result{Identity: id, Outcome: OutcomePass, Output: json.RawMessage(`{"summary":"done"}`), Artifacts: []Artifact{{
			ID: artifactID, Type: ArtifactGitCommit, RepositoryID: "repo", BaseSHA: "d41e3e2254ced0bd5c6d4c0b5fb82dcb772ff442",
			CommitSHA: sha, ManagedRef: "refs/heads/multica/graphs/run/node/1", ProducerTaskID: id.TaskID,
		}}}
	}
	if err := ValidateResult(n, build("7b0f8f0e-8c53-4a4e-9a58-0d3c1c2f6a11")); err != nil {
		t.Fatalf("uuid artifact id rejected: %v", err)
	}
	for _, bad := range []string{sha, "artifact-1", "not a uuid"} {
		if err := ValidateResult(n, build(bad)); err == nil {
			t.Fatalf("artifact id %q accepted at submission", bad)
		}
	}
}

// A person's release is what makes the attempt after a spent budget legal, and
// what they typed while releasing it has to travel with that attempt: the
// node's input is frozen for its whole life, so the manifest is the only channel
// left. Both fields are optional and stay out of the bytes otherwise - every
// manifest the platform has ever stored is this struct, and an ordinary attempt
// must keep serializing exactly as it did before the fields existed.
func TestAttemptGrantWidensTheCeilingAndCarriesTheNote(t *testing.T) {
	spec := testNodeSpec(t)
	spec.Node.MaxAttempts = 2
	identity := testIdentity()
	identity.InputHash = spec.InputHash
	plain, err := NewAttemptManifest(spec, identity, AttemptGrant{})
	if err != nil {
		t.Fatal(err)
	}
	if plain.Ceiling() != 2 || plain.AttemptCeiling != 0 || plain.HumanNote != nil {
		t.Fatalf("an ordinary attempt carries a grant: %+v", plain)
	}
	raw, _, err := CanonicalManifest(plain)
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"attempt_ceiling", "human_note"} {
		if bytes.Contains(raw, []byte(absent)) {
			t.Errorf("a manifest with no grant names %q", absent)
		}
	}

	// The attempt the release creates: the node had spent 2/2, so this one is
	// authorized by the grant alone.
	next := identity
	next.AttemptNo, next.ExecutionGeneration = 3, 3
	note := &HumanNote{Reason: "credentials rotated; retry the deploy", MemberID: "member-1", At: time.Now().UTC()}
	granted, err := NewAttemptManifest(spec, next, AttemptGrant{Granted: 1, Note: note})
	if err != nil {
		t.Fatal(err)
	}
	if granted.Ceiling() != 3 {
		t.Fatalf("grant did not widen the ceiling: %d", granted.Ceiling())
	}
	if granted.HumanNote == nil || granted.HumanNote.Reason != note.Reason || granted.HumanNote.MemberID != "member-1" {
		t.Fatalf("person's reason was dropped from the manifest: %+v", granted.HumanNote)
	}
	if _, _, err := CanonicalManifest(granted); err != nil {
		t.Fatalf("granted attempt failed its own validation: %v", err)
	}

	// The ceiling is frozen into the manifest, not re-derived, so an attempt
	// numbered past it must be refused even when the spec still matches.
	over := granted
	over.Identity.AttemptNo, over.Identity.ExecutionGeneration, over.Identity.TaskID = 4, 4, "task-4"
	if _, _, err := CanonicalManifest(over); err == nil {
		t.Fatal("granted manifest accepted an attempt past its ceiling")
	}
	// And a grant can never narrow the published budget.
	narrowed := granted
	narrowed.AttemptCeiling = 1
	if _, _, err := CanonicalManifest(narrowed); err == nil {
		t.Fatal("manifest narrowed the published budget")
	}
}
