package graph

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const ProtocolVersion = 1

var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var commitPattern = regexp.MustCompile(`^([a-f0-9]{40}|[a-f0-9]{64})$`)
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var instanceSegmentPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,127}$`)

type AttemptIdentity struct {
	RunID               string `json:"run_id"`
	NodeRunID           string `json:"node_run_id"`
	TaskID              string `json:"task_id"`
	AttemptNo           int    `json:"attempt_no"`
	ExecutionGeneration int64  `json:"execution_generation"`
	InputHash           string `json:"input_hash"`
}

func validateAttemptIdentity(i AttemptIdentity) error {
	if strings.TrimSpace(i.RunID) == "" || strings.TrimSpace(i.NodeRunID) == "" || strings.TrimSpace(i.TaskID) == "" || i.AttemptNo < 1 || i.AttemptNo > 3 || i.ExecutionGeneration < 1 {
		return denied("stale_attempt", "attempt identity is incomplete")
	}
	if !hashPattern.MatchString(i.InputHash) {
		return denied("input_mismatch", "input hash must be a SHA-256 digest")
	}
	return nil
}

func ValidateIdentity(expected, actual AttemptIdentity) error {
	if err := validateAttemptIdentity(expected); err != nil {
		return err
	}
	if err := validateAttemptIdentity(actual); err != nil {
		return err
	}
	if expected.InputHash != actual.InputHash {
		return denied("input_mismatch", "attempt input does not match the frozen input")
	}
	if expected != actual {
		return denied("stale_attempt", "result does not belong to the current attempt")
	}
	return nil
}

type SkillSnapshot struct {
	ID        string `json:"id"`
	Hash      string `json:"hash"`
	BundleRef string `json:"bundle_ref"`
}
type RepositoryInput struct {
	RepositoryID string `json:"repository_id"`
	BaseSHA      string `json:"base_sha"`
	CommitSHA    string `json:"commit_sha"`
	ArtifactID   string `json:"artifact_id,omitempty"`
}

// NodeSpec is content-addressed and immutable once persisted. AgentConfig contains nonsecret configuration and credential references only.
type NodeSpec struct {
	ProtocolVersion int              `json:"protocol_version"`
	Node            Node             `json:"node"`
	Input           json.RawMessage  `json:"input"`
	InputHash       string           `json:"input_hash"`
	AgentConfig     json.RawMessage  `json:"agent_config"`
	Skills          []SkillSnapshot  `json:"skills,omitempty"`
	Repository      *RepositoryInput `json:"repository,omitempty"`
	InstanceKey     string           `json:"instance_key,omitempty"`
}

type AttemptManifest struct {
	ProtocolVersion int             `json:"protocol_version"`
	Identity        AttemptIdentity `json:"identity"`
	NodeSpec        NodeSpec        `json:"node_spec"`
	NodeSpecHash    string          `json:"node_spec_hash"`
}

func CanonicalNodeSpec(spec NodeSpec) ([]byte, string, error) {
	if spec.ProtocolVersion != ProtocolVersion {
		return nil, "", invalid("protocol_version", "unsupported Graph protocol")
	}
	if err := validateExecutableNode(spec.Node); err != nil {
		return nil, "", err
	}
	if spec.InstanceKey != "" && !validInstanceKey(spec.InstanceKey) {
		return nil, "", invalid("instance_key", "invalid scoped execution instance key")
	}
	input, hash, err := CanonicalJSON(spec.Input)
	if err != nil {
		return nil, "", err
	}
	if hash != spec.InputHash {
		return nil, "", denied("input_mismatch", "node input does not match its digest")
	}
	if value, err := decodeJSON(input, MaxJSONBytes); err != nil {
		return nil, "", err
	} else if _, ok := value.(map[string]any); !ok {
		return nil, "", invalid("input", "node input must be an object")
	}
	config, err := decodeJSON(spec.AgentConfig, MaxJSONBytes)
	if err != nil {
		return nil, "", invalid("agent_config", err.Error())
	}
	if _, ok := config.(map[string]any); !ok {
		return nil, "", invalid("agent_config", "must be an object")
	}
	if len(spec.Skills) > 64 {
		return nil, "", invalid("skills", "at most 64 skill snapshots are allowed")
	}
	seen := map[string]bool{}
	for _, skill := range spec.Skills {
		if skill.ID == "" || seen[skill.ID] || !hashPattern.MatchString(strings.TrimPrefix(skill.Hash, "sha256:")) || skill.BundleRef == "" {
			return nil, "", invalid("skills", "skills require unique IDs, exact hashes, and immutable bundle references")
		}
		seen[skill.ID] = true
	}
	if spec.Node.ExecutionMode != ExecutionNone && spec.Repository == nil {
		return nil, "", invalid("repository", "code execution requires an immutable repository input")
	}
	if r := spec.Repository; r != nil {
		if r.RepositoryID == "" || !commitPattern.MatchString(r.BaseSHA) || !commitPattern.MatchString(r.CommitSHA) {
			return nil, "", invalid("repository", "requires repository identity, base SHA, and input commit SHA")
		}
	}
	spec.Input = input
	return canonicalValue(spec)
}

// NewAttemptManifest deep-copies the spec so caller mutations cannot rewrite an existing attempt.
func NewAttemptManifest(spec NodeSpec, identity AttemptIdentity) (AttemptManifest, error) {
	var manifest AttemptManifest
	if err := validateAttemptIdentity(identity); err != nil {
		return manifest, err
	}
	if identity.InputHash != spec.InputHash {
		return manifest, denied("input_mismatch", "attempt and node input hashes differ")
	}
	if identity.AttemptNo > spec.Node.MaxAttempts {
		return manifest, denied("budget_exhausted", "attempt exceeds the node budget")
	}
	raw, hash, err := CanonicalNodeSpec(spec)
	if err != nil {
		return manifest, err
	}
	var frozen NodeSpec
	if err := json.Unmarshal(raw, &frozen); err != nil {
		return manifest, err
	}
	return AttemptManifest{ProtocolVersion: ProtocolVersion, Identity: identity, NodeSpec: frozen, NodeSpecHash: hash}, nil
}

func CanonicalManifest(manifest AttemptManifest) ([]byte, string, error) {
	if manifest.ProtocolVersion != ProtocolVersion {
		return nil, "", invalid("protocol_version", "unsupported Graph protocol")
	}
	if err := validateAttemptIdentity(manifest.Identity); err != nil {
		return nil, "", err
	}
	_, hash, err := CanonicalNodeSpec(manifest.NodeSpec)
	if err != nil {
		return nil, "", err
	}
	if hash != manifest.NodeSpecHash {
		return nil, "", denied("spec_mismatch", "attempt spec hash does not match")
	}
	if manifest.Identity.InputHash != manifest.NodeSpec.InputHash {
		return nil, "", denied("input_mismatch", "attempt and node input hashes differ")
	}
	if manifest.Identity.AttemptNo > manifest.NodeSpec.Node.MaxAttempts {
		return nil, "", denied("budget_exhausted", "attempt exceeds the node budget")
	}
	return canonicalValue(manifest)
}

type Outcome string

const (
	OutcomePass               Outcome = "pass"
	OutcomeEnvironmentBlocked Outcome = "environment_blocked"
	OutcomeNeedsChanges       Outcome = "needs_changes"
)

type ArtifactType string

const (
	ArtifactGitCommit     ArtifactType = "git_commit"
	ArtifactAttachmentRef ArtifactType = "attachment_ref"
)

type Artifact struct {
	ID             string       `json:"id"`
	Type           ArtifactType `json:"type"`
	RepositoryID   string       `json:"repository_id,omitempty"`
	BaseSHA        string       `json:"base_sha,omitempty"`
	CommitSHA      string       `json:"commit_sha,omitempty"`
	ManagedRef     string       `json:"managed_ref,omitempty"`
	ProducerTaskID string       `json:"producer_task_id"`
	ContentHash    string       `json:"content_hash,omitempty"`
	Size           int64        `json:"size,omitempty"`
	MediaType      string       `json:"media_type,omitempty"`
}

type Result struct {
	Identity        AttemptIdentity `json:"identity"`
	Outcome         Outcome         `json:"outcome"`
	Output          json.RawMessage `json:"output,omitempty"`
	Artifacts       []Artifact      `json:"artifacts,omitempty"`
	Diagnostics     json.RawMessage `json:"diagnostics,omitempty"`
	TestedCommitSHA string          `json:"tested_commit_sha,omitempty"`
}

// ValidateResult checks shape only. The service must verify artifact ownership, availability, and receipts.
func ValidateResult(node Node, result Result) error {
	if err := validateExecutableNode(node); err != nil {
		return err
	}
	if err := validateAttemptIdentity(result.Identity); err != nil {
		return err
	}
	if result.Outcome != OutcomePass && result.Outcome != OutcomeEnvironmentBlocked && result.Outcome != OutcomeNeedsChanges {
		return invalid("outcome", "unsupported outcome")
	}
	if len(result.Diagnostics) > 0 {
		if _, err := decodeJSON(result.Diagnostics, MaxJSONBytes); err != nil {
			return invalid("diagnostics", err.Error())
		}
	}
	if len(result.Artifacts) > 128 {
		return invalid("artifacts", "at most 128 artifacts are allowed")
	}
	seen := map[string]bool{}
	hasProducedCommit := false
	for _, a := range result.Artifacts {
		if a.ID == "" || seen[a.ID] || a.ProducerTaskID == "" {
			return invalid("artifacts", "requires unique artifact IDs and producer identity")
		}
		// Artifact IDs are stored in a uuid column; rejecting anything else here lets
		// the agent correct it instead of wedging reconciliation after acceptance.
		if !uuidPattern.MatchString(a.ID) {
			return invalid("artifacts", "artifact id must be a UUID (generate a new one; it is not the commit SHA)")
		}
		seen[a.ID] = true
		switch a.Type {
		case ArtifactGitCommit:
			if a.RepositoryID == "" || !commitPattern.MatchString(a.BaseSHA) || !commitPattern.MatchString(a.CommitSHA) || !strings.HasPrefix(a.ManagedRef, "refs/heads/multica/graphs/") {
				return invalid("artifacts", "invalid immutable Git artifact")
			}
			if a.ProducerTaskID == result.Identity.TaskID {
				hasProducedCommit = true
			}
		case ArtifactAttachmentRef:
			if !hashPattern.MatchString(a.ContentHash) || a.Size < 0 || a.MediaType == "" {
				return invalid("artifacts", "invalid attachment artifact")
			}
		default:
			return invalid("artifacts", "unsupported artifact type")
		}
	}
	if result.Outcome == OutcomePass {
		if err := ValidateJSON(node.OutputSchema, result.Output); err != nil {
			return invalid("output", err.Error())
		}
		if node.ExecutionMode == ExecutionProduceCommit && !hasProducedCommit {
			return invalid("artifacts", "produce_commit requires a Git artifact produced by this task")
		}
		if node.ExecutionMode == ExecutionVerifyCommit && !commitPattern.MatchString(result.TestedCommitSHA) {
			return invalid("tested_commit_sha", "verify_commit requires the tested commit SHA")
		}
	} else {
		if len(result.Diagnostics) == 0 {
			return invalid("diagnostics", "non-pass outcomes require diagnostics")
		}
		if len(result.Output) > 0 {
			if _, err := decodeJSON(result.Output, MaxJSONBytes); err != nil {
				return err
			}
		}
	}
	_, _, err := canonicalValue(result)
	return err
}

func validateExecutableNode(node Node) error {
	if node.Type == NodeTypeAgent {
		if hasV2Config(node) {
			return invalid("node", "unexpected execution configuration")
		}
		return validateNode(node)
	}
	if node.Type == NodeTypeScript {
		return validateV2Node(node)
	}
	return invalid("node", "attempts require an agent or script node")
}

func validInstanceKey(key string) bool {
	if len(key) > 512 || key == "" {
		return false
	}
	parts := strings.Split(key, "/")
	for _, part := range parts {
		if !instanceSegmentPattern.MatchString(part) {
			return false
		}
	}
	return true
}

type StopReceipt struct {
	Identity         AttemptIdentity `json:"identity"`
	DaemonInstanceID string          `json:"daemon_instance_id"`
	StoppedAt        time.Time       `json:"stopped_at"`
}

func ValidateStopReceipt(expected AttemptIdentity, receipt StopReceipt) error {
	if err := ValidateIdentity(expected, receipt.Identity); err != nil {
		return err
	}
	if receipt.DaemonInstanceID == "" || receipt.StoppedAt.IsZero() {
		return denied("execution_unknown", "receipt requires executor identity and stop time")
	}
	return nil
}

// ApprovalCandidate includes every accepted result and evidence digest in the review identity.
type ApprovalCandidate struct {
	VersionHash     string            `json:"version_hash"`
	InputHash       string            `json:"input_hash"`
	Artifacts       []Artifact        `json:"artifacts"`
	AcceptedOutputs map[string]string `json:"accepted_outputs"`
	EvidenceHashes  []string          `json:"evidence_hashes"`
}

func ApprovalDigest(candidate ApprovalCandidate) (string, error) {
	if !hashPattern.MatchString(candidate.VersionHash) || !hashPattern.MatchString(candidate.InputHash) {
		return "", fmt.Errorf("approval requires version and input digests")
	}
	for key, hash := range candidate.AcceptedOutputs {
		if !nodeKeyPattern.MatchString(key) || !hashPattern.MatchString(hash) {
			return "", fmt.Errorf("invalid accepted output digest")
		}
	}
	for _, hash := range candidate.EvidenceHashes {
		if !hashPattern.MatchString(hash) {
			return "", fmt.Errorf("invalid evidence digest")
		}
	}
	_, hash, err := canonicalValue(candidate)
	return hash, err
}
