package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/graph"
)

var graphCmd = newGraphCommand()

func newGraphCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "graph", Short: "Manage versioned Graph workflows and scoped node results"}
	cmd.PersistentFlags().String("output", "json", "Output format: json")
	add := func(use, description string, args cobra.PositionalArgs, run func(*cobra.Command, []string) error) *cobra.Command {
		child := &cobra.Command{Use: use, Short: description, Args: args, RunE: run}
		cmd.AddCommand(child)
		return child
	}
	list := add("list", "List graph templates or runs", cobra.NoArgs, runGraphRead)
	list.Flags().Bool("runs", false, "List runs instead of templates")
	list.Flags().String("search", "", "Search graph names or run/root issue titles (returns a paginated page)")
	list.Flags().String("state", "", "Filter by template or run state")
	list.Flags().String("graph-id", "", "Filter runs by graph UUID")
	list.Flags().String("cursor", "", "Opaque next_cursor from the previous page")
	list.Flags().Int("limit", 30, "Search page size (1..100)")
	get := add("get <id>", "Get a run, or a template with --definition", exactArgs(1), runGraphRead)
	get.Flags().Bool("definition", false, "Read a graph definition instead of a run")
	for _, action := range []string{"create", "update"} {
		use, args := action, cobra.NoArgs
		if action == "update" {
			use, args = "update <graph-id>", exactArgs(1)
		}
		child := add(use, "Save a graph definition from a JSON file", args, runGraphSave)
		child.Flags().String("definition-file", "", "Definition JSON file, or - for stdin")
		_ = child.MarkFlagRequired("definition-file")
		graphMutationFlags(child, action == "update")
	}
	add("validate <graph-id>", "Validate a saved graph definition", exactArgs(1), runGraphRead)
	for _, action := range []string{"publish", "archive-definition"} {
		child := add(action+" <graph-id>", "Publish or archive a graph template", exactArgs(1), runGraphDefinitionControl)
		graphMutationFlags(child, true)
	}
	run := add("run", "Start a published version on a parent issue", cobra.NoArgs, runGraphStart)
	graphMutationFlags(run, false)
	run.Flags().String("version-id", "", "Published version UUID")
	run.Flags().String("issue", "", "Parent issue UUID or issue key")
	run.Flags().String("input-file", "", "Run input JSON object file, or - for stdin (default {})")
	run.Flags().StringArray("runtime", nil, "Fixed runtime binding definition_path=runtime_uuid (repeatable; nested example loop/body/agent)")
	run.Flags().String("previous-run-id", "", "Terminal run UUID to link as rework")
	run.Flags().String("repository-id", "", "Repository identity for code steps")
	run.Flags().String("base-sha", "", "Immutable base commit SHA for code steps")
	run.Flags().String("deadline", "", "Optional deadline in RFC3339 format")
	_ = run.MarkFlagRequired("version-id")
	_ = run.MarkFlagRequired("issue")
	for _, action := range []string{"pause", "resume", "cancel", "retry", "resolve-block", "archive", "approver"} {
		child := add(action+" <run-id>", "Control a graph run using its current revision", exactArgs(1), runGraphControl)
		graphMutationFlags(child, true)
		child.Flags().String("node-run-id", "", "Blocked node execution UUID")
		child.Flags().String("reason", "", "Audit reason for the operation")
		if action == "retry" || action == "resolve-block" {
			_ = child.MarkFlagRequired("node-run-id")
		}
		if action == "approver" {
			child.Flags().String("member-id", "", "New approver membership UUID")
			child.Flags().String("previous-member-id", "", "Membership UUID being replaced in a multi-person gate (resets gate votes)")
			_ = child.MarkFlagRequired("member-id")
		}
	}
	for _, decision := range []string{"approve", "reject"} {
		child := add(decision+" <run-id>", "Decide an exact approval request and candidate", exactArgs(1), runGraphApproval)
		graphMutationFlags(child, true)
		child.Flags().String("approval-id", "", "Current approval request UUID")
		child.Flags().String("candidate-digest", "", "Exact candidate digest displayed by graph get")
		child.Flags().String("reason", "", "Decision reason")
		_ = child.MarkFlagRequired("approval-id")
		_ = child.MarkFlagRequired("candidate-digest")
		if decision == "reject" {
			_ = child.MarkFlagRequired("reason")
		}
	}
	detach := add("detach <issue-id>", "Release a completed parent issue from Graph management", exactArgs(1), runGraphDetach)
	graphMutationFlags(detach, false)
	node := &cobra.Command{Use: "node", Short: "Operate only on the current Graph task"}
	cmd.AddCommand(node)
	draft := &cobra.Command{Use: "draft", Short: "Read an AI authoring request and submit a Graph draft (authoring tasks only)", Long: "Commands for the agent that serves an AI Graph authoring request. They only work with the task-scoped credential of that task: read the request with `context`, then hand in one definition with `submit`. A draft is never saved, published or run by these commands."}
	cmd.AddCommand(draft)
	draft.AddCommand(&cobra.Command{Use: "context", Short: "Read the authoring request: contract, requirements, team and limits", Args: cobra.NoArgs, RunE: runGraphDraftContext})
	submit := &cobra.Command{Use: "submit", Short: "Submit one Graph definition for validation", Long: "Validate one Graph definition (schema_version 2). An accepted draft is stored for the requester to review. A rejected one prints the reason and spends one of the limited submissions: fix the file and submit again.", Args: cobra.NoArgs, RunE: runGraphDraftSubmit}
	submit.Flags().String("file", "", "Graph definition JSON file, or - for stdin")
	submit.Flags().String("summary", "", "One sentence saying what the draft does or changes")
	_ = submit.MarkFlagRequired("file")
	draft.AddCommand(submit)
	node.AddCommand(&cobra.Command{Use: "context", Short: "Read the current attempt's fixed input and execution manifest", Args: cobra.NoArgs, RunE: runGraphNodeContext})
	comment := &cobra.Command{Use: "comment", Short: "Post a diagnostic comment on the current Graph node issue", Long: "Post a diagnostic comment on the current Graph node issue using its scoped context. Comments do not change fixed input, schedule work or submit results.", Args: cobra.NoArgs, RunE: runGraphNodeComment}
	comment.Flags().String("body", "", "Diagnostic comment text (decodes \\n, \\r, \\t, \\\\)")
	comment.Flags().String("body-file", "", "Read UTF-8 text from a file inside the task workdir; preserves literal backslashes")
	comment.MarkFlagsMutuallyExclusive("body", "body-file")
	node.AddCommand(comment)
	for _, action := range []string{"submit", "block"} {
		child := &cobra.Command{Use: action, Short: "Stage a result or blocker for the current attempt", Args: cobra.NoArgs, RunE: runGraphNodeSubmit}
		flag := "result-file"
		if action == "block" {
			flag = "reason-file"
		}
		child.Flags().String(flag, "", "JSON file, or - for stdin")
		child.Flags().String("idempotency-key", "", "Replay key (default: stable digest of result and attempt)")
		_ = child.MarkFlagRequired(flag)
		node.AddCommand(child)
	}
	return cmd
}

func graphMutationFlags(cmd *cobra.Command, revision bool) {
	cmd.Flags().String("idempotency-key", "", "Unique operation key; reuse this key when retrying the same request")
	_ = cmd.MarkFlagRequired("idempotency-key")
	if revision {
		cmd.Flags().Int64("expected-revision", -1, "Current server revision, from graph get")
		_ = cmd.MarkFlagRequired("expected-revision")
	}
}

func graphCLIClient(cmd *cobra.Command, task bool) (*cli.APIClient, error) {
	output, _ := cmd.Flags().GetString("output")
	if output != "" && output != "json" {
		return nil, fmt.Errorf("Graph commands support --output json")
	}
	if !task {
		if err := requireHumanLocalCommand("graph " + cmd.Name()); err != nil {
			return nil, err
		}
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return nil, err
	}
	if task && !strings.HasPrefix(client.Token, "mat_") {
		group := "node"
		if parent := cmd.Parent(); parent != nil {
			group = parent.Name()
		}
		return nil, fmt.Errorf("graph %s requires the current task-scoped mat_ credential", group)
	}
	if !task && strings.HasPrefix(client.Token, "mat_") {
		return nil, fmt.Errorf("Graph management requires a human credential")
	}
	if client.WorkspaceID == "" {
		return nil, fmt.Errorf("workspace is required: set --workspace-id or MULTICA_WORKSPACE_ID")
	}
	return client, nil
}

func graphCommandBody(cmd *cobra.Command) (map[string]any, error) {
	key, _ := cmd.Flags().GetString("idempotency-key")
	if strings.TrimSpace(key) == "" {
		return nil, fmt.Errorf("--idempotency-key is required")
	}
	body := map[string]any{"idempotency_key": key}
	if cmd.Flags().Lookup("expected-revision") != nil {
		revision, _ := cmd.Flags().GetInt64("expected-revision")
		if revision < 0 {
			return nil, fmt.Errorf("--expected-revision must be a non-negative integer")
		}
		body["expected_revision"] = revision
	}
	return body, nil
}

func graphPrint(cmd *cobra.Command, response json.RawMessage) error {
	if len(response) == 0 {
		return fmt.Errorf("server returned an empty Graph response")
	}
	return cli.PrintJSON(cmd.OutOrStdout(), response)
}

func runGraphRead(cmd *cobra.Command, args []string) error {
	client, err := graphCLIClient(cmd, false)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(cmd.Context())
	defer cancel()
	path := "/api/graphs"
	var response json.RawMessage
	switch cmd.Name() {
	case "list":
		runs, _ := cmd.Flags().GetBool("runs")
		if runs {
			path = "/api/graph-runs"
		}
		search := false
		for _, flag := range []string{"search", "state", "graph-id", "cursor", "limit"} {
			search = search || cmd.Flags().Changed(flag)
		}
		if search {
			limit, _ := cmd.Flags().GetInt("limit")
			if limit < 1 || limit > 100 {
				return fmt.Errorf("--limit must be between 1 and 100")
			}
			query := url.Values{"limit": {strconv.Itoa(limit)}}
			for flag, key := range map[string]string{"search": "q", "state": "state", "graph-id": "graph_id", "cursor": "cursor"} {
				value, _ := cmd.Flags().GetString(flag)
				if value != "" {
					query.Set(key, value)
				}
			}
			if query.Get("graph_id") != "" && !runs {
				return fmt.Errorf("--graph-id requires --runs")
			}
			path += "/search?" + query.Encode()
		}
	case "get":
		definition, _ := cmd.Flags().GetBool("definition")
		if !definition {
			path = "/api/graph-runs"
		}
		path += "/" + url.PathEscape(args[0])
	case "validate":
		if err := client.PostJSON(ctx, path+"/"+url.PathEscape(args[0])+"/validate", map[string]any{}, &response); err != nil {
			return err
		}
		return graphPrint(cmd, response)
	default:
		return fmt.Errorf("unsupported Graph read operation")
	}
	if err := client.GetJSON(ctx, path, &response); err != nil {
		return err
	}
	return graphPrint(cmd, response)
}

func readGraphJSONFile(cmd *cobra.Command, flag string) ([]byte, error) {
	path, _ := cmd.Flags().GetString(flag)
	if path == "" {
		return nil, fmt.Errorf("--%s is required", flag)
	}
	reader := cmd.InOrStdin()
	if path != "-" {
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("read --%s: %w", flag, err)
		}
		defer file.Close()
		reader = file
	}
	raw, err := io.ReadAll(io.LimitReader(reader, graph.MaxDefinitionBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > graph.MaxDefinitionBytes {
		return nil, fmt.Errorf("Graph JSON file exceeds %d bytes", graph.MaxDefinitionBytes)
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("--%s must contain one JSON value", flag)
	}
	return raw, nil
}

func runGraphSave(cmd *cobra.Command, args []string) error {
	raw, err := readGraphJSONFile(cmd, "definition-file")
	if err != nil {
		return err
	}
	definition, err := graph.ParseDefinition(raw)
	if err != nil {
		return fmt.Errorf("invalid graph definition: %w", err)
	}
	body, err := graphCommandBody(cmd)
	if err != nil {
		return err
	}
	body["definition"] = definition
	client, err := graphCLIClient(cmd, false)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(cmd.Context())
	defer cancel()
	var response json.RawMessage
	if cmd.Name() == "update" {
		err = client.PutJSON(ctx, "/api/graphs/"+url.PathEscape(args[0]), body, &response)
	} else {
		err = client.PostJSON(ctx, "/api/graphs", body, &response)
	}
	if err != nil {
		return err
	}
	return graphPrint(cmd, response)
}

func runGraphDefinitionControl(cmd *cobra.Command, args []string) error {
	body, err := graphCommandBody(cmd)
	if err != nil {
		return err
	}
	action := cmd.Name()
	if action == "archive-definition" {
		action = "archive"
	}
	return graphPostCommand(cmd, "/api/graphs/"+url.PathEscape(args[0])+"/"+action, body)
}

func parseGraphRuntimeBindings(values []string) (map[string]string, error) {
	bindings := map[string]string{}
	for _, value := range values {
		parts := strings.SplitN(value, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, fmt.Errorf("--runtime must be node_key=runtime_uuid")
		}
		key, id := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		if _, exists := bindings[key]; exists {
			return nil, fmt.Errorf("duplicate runtime binding for node %q", key)
		}
		bindings[key] = id
	}
	return bindings, nil
}

func runGraphStart(cmd *cobra.Command, _ []string) error {
	body, err := graphCommandBody(cmd)
	if err != nil {
		return err
	}
	version, _ := cmd.Flags().GetString("version-id")
	issue, _ := cmd.Flags().GetString("issue")
	if version == "" || issue == "" {
		return fmt.Errorf("--version-id and --issue are required")
	}
	values, _ := cmd.Flags().GetStringArray("runtime")
	bindings, err := parseGraphRuntimeBindings(values)
	if err != nil {
		return err
	}
	input := json.RawMessage(`{}`)
	if file, _ := cmd.Flags().GetString("input-file"); file != "" {
		input, err = readGraphJSONFile(cmd, "input-file")
		if err != nil {
			return err
		}
		var object map[string]json.RawMessage
		if json.Unmarshal(input, &object) != nil || object == nil {
			return fmt.Errorf("run input must be a JSON object")
		}
	}
	repositoryID, _ := cmd.Flags().GetString("repository-id")
	baseSHA, _ := cmd.Flags().GetString("base-sha")
	if repositoryID != "" || baseSHA != "" {
		if repositoryID == "" || baseSHA == "" {
			return fmt.Errorf("--repository-id and --base-sha must be supplied together")
		}
		body["repository"] = graph.RepositoryInput{RepositoryID: repositoryID, BaseSHA: strings.ToLower(baseSHA), CommitSHA: strings.ToLower(baseSHA)}
	}
	if deadline, _ := cmd.Flags().GetString("deadline"); deadline != "" {
		parsed, err := time.Parse(time.RFC3339, deadline)
		if err != nil {
			return fmt.Errorf("--deadline must be RFC3339: %w", err)
		}
		body["deadline"] = parsed
	}
	client, err := graphCLIClient(cmd, false)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(cmd.Context())
	defer cancel()
	issueRef, err := resolveIssueRef(ctx, client, issue)
	if err != nil {
		return err
	}
	body["version_id"], body["root_issue_id"], body["input"], body["runtime_bindings"] = version, issueRef.ID, input, bindings
	if previous, _ := cmd.Flags().GetString("previous-run-id"); previous != "" {
		body["previous_run_id"] = previous
	}
	var response json.RawMessage
	if err := client.PostJSON(ctx, "/api/graph-runs", body, &response); err != nil {
		return err
	}
	return graphPrint(cmd, response)
}

func runGraphControl(cmd *cobra.Command, args []string) error {
	body, err := graphCommandBody(cmd)
	if err != nil {
		return err
	}
	if nodeID, _ := cmd.Flags().GetString("node-run-id"); nodeID != "" {
		body["node_run_id"] = nodeID
	}
	if reason, _ := cmd.Flags().GetString("reason"); reason != "" {
		body["reason"] = reason
	}
	if cmd.Name() == "approver" {
		body["approver_id"], _ = cmd.Flags().GetString("member-id")
		if previous, _ := cmd.Flags().GetString("previous-member-id"); previous != "" {
			body["previous_approver_id"] = previous
		}
	}
	return graphPostCommand(cmd, "/api/graph-runs/"+url.PathEscape(args[0])+"/"+cmd.Name(), body)
}

func runGraphApproval(cmd *cobra.Command, args []string) error {
	body, err := graphCommandBody(cmd)
	if err != nil {
		return err
	}
	body["approval_id"], _ = cmd.Flags().GetString("approval-id")
	body["candidate_digest"], _ = cmd.Flags().GetString("candidate-digest")
	body["reason"], _ = cmd.Flags().GetString("reason")
	body["decision"] = "approved"
	if cmd.Name() == "reject" {
		body["decision"] = "rejected"
	}
	return graphPostCommand(cmd, "/api/graph-runs/"+url.PathEscape(args[0])+"/approval", body)
}

func runGraphDetach(cmd *cobra.Command, args []string) error {
	body, err := graphCommandBody(cmd)
	if err != nil {
		return err
	}
	client, err := graphCLIClient(cmd, false)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(cmd.Context())
	defer cancel()
	if err := client.PostJSON(ctx, "/api/issues/"+url.PathEscape(args[0])+"/graph-detach", body, nil); err != nil {
		return err
	}
	return cli.PrintJSON(cmd.OutOrStdout(), map[string]any{"detached": true, "issue_id": args[0]})
}

func graphPostCommand(cmd *cobra.Command, path string, body any) error {
	client, err := graphCLIClient(cmd, false)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(cmd.Context())
	defer cancel()
	var response json.RawMessage
	if err := client.PostJSON(ctx, path, body, &response); err != nil {
		return err
	}
	return graphPrint(cmd, response)
}

func runGraphNodeContext(cmd *cobra.Command, _ []string) error {
	client, err := graphCLIClient(cmd, true)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(cmd.Context())
	defer cancel()
	var response json.RawMessage
	if err := client.GetJSON(ctx, "/api/graph/node/context", &response); err != nil {
		return err
	}
	return graphPrint(cmd, response)
}

func runGraphDraftContext(cmd *cobra.Command, _ []string) error {
	client, err := graphCLIClient(cmd, true)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(cmd.Context())
	defer cancel()
	var response json.RawMessage
	if err := client.GetJSON(ctx, "/api/graph/authoring/context", &response); err != nil {
		return err
	}
	return graphPrint(cmd, response)
}

// graphDraftSubmitBody carries the definition verbatim: the server, not the
// CLI, owns validation, so a draft is never rewritten on its way in.
type graphDraftSubmitBody struct {
	Definition json.RawMessage `json:"definition"`
	Summary    string          `json:"summary,omitempty"`
}

func runGraphDraftSubmit(cmd *cobra.Command, _ []string) error {
	raw, err := readGraphJSONFile(cmd, "file")
	if err != nil {
		return err
	}
	client, err := graphCLIClient(cmd, true)
	if err != nil {
		return err
	}
	summary, _ := cmd.Flags().GetString("summary")
	ctx, cancel := cli.APIContext(cmd.Context())
	defer cancel()
	var response json.RawMessage
	if err := client.PostJSON(ctx, "/api/graph/authoring/draft", graphDraftSubmitBody{Definition: raw, Summary: strings.TrimSpace(summary)}, &response); err != nil {
		return err
	}
	return graphPrint(cmd, response)
}

func runGraphNodeComment(cmd *cobra.Command, _ []string) error {
	body, present, err := resolveTextFlag(cmd, "body")
	if err != nil {
		return err
	}
	if !present || strings.TrimSpace(body) == "" {
		return fmt.Errorf("--body or --body-file must contain a diagnostic comment")
	}
	if !utf8.ValidString(body) || len(body) > graph.MaxJSONBytes {
		return fmt.Errorf("comment must be UTF-8 text no larger than %d bytes", graph.MaxJSONBytes)
	}
	if err := guardLocalPathLinks(body, "Graph diagnostic comment", "Upload evidence with `multica attachment upload <path>` and use its returned durable URL."); err != nil {
		return err
	}
	client, err := graphCLIClient(cmd, true)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(cmd.Context())
	defer cancel()
	var current struct {
		IssueID  string                `json:"issue_id"`
		Manifest graph.AttemptManifest `json:"manifest"`
	}
	if err := client.GetJSON(ctx, "/api/graph/node/context", &current); err != nil {
		return err
	}
	if strings.TrimSpace(current.IssueID) == "" {
		return fmt.Errorf("current Graph context is missing its node issue")
	}
	if err := graph.ValidateIdentity(current.Manifest.Identity, current.Manifest.Identity); err != nil {
		return fmt.Errorf("invalid current Graph context: %w", err)
	}
	if client.TaskID != "" && current.Manifest.Identity.TaskID != client.TaskID {
		return fmt.Errorf("current Graph context does not belong to this task")
	}
	var response json.RawMessage
	if err := client.PostJSON(ctx, "/api/issues/"+url.PathEscape(current.IssueID)+"/comments", map[string]string{"content": body}, &response); err != nil {
		return err
	}
	return graphPrint(cmd, response)
}

type graphNodeSubmitBody struct {
	IdempotencyKey string       `json:"idempotency_key"`
	Result         graph.Result `json:"result"`
}

func runGraphNodeSubmit(cmd *cobra.Command, _ []string) error {
	block := cmd.Name() == "block"
	flag := "result-file"
	if block {
		flag = "reason-file"
	}
	raw, err := readGraphJSONFile(cmd, flag)
	if err != nil {
		return err
	}
	client, err := graphCLIClient(cmd, true)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(cmd.Context())
	defer cancel()
	key, _ := cmd.Flags().GetString("idempotency-key")
	response, err := submitGraphNodeResult(ctx, client, raw, block, key)
	if err != nil {
		return err
	}
	return graphPrint(cmd, response)
}

func submitGraphNodeResult(ctx context.Context, client *cli.APIClient, raw []byte, block bool, key string) (json.RawMessage, error) {
	var current struct {
		Manifest graph.AttemptManifest `json:"manifest"`
	}
	if err := client.GetJSON(ctx, "/api/graph/node/context", &current); err != nil {
		return nil, err
	}
	if err := graph.ValidateIdentity(current.Manifest.Identity, current.Manifest.Identity); err != nil {
		return nil, fmt.Errorf("invalid current Graph context: %w", err)
	}
	var result graph.Result
	path := "/api/graph/node/submissions"
	if block {
		var diagnostic map[string]json.RawMessage
		if json.Unmarshal(raw, &diagnostic) != nil || len(diagnostic) == 0 {
			return nil, fmt.Errorf("block reason must be a non-empty JSON object")
		}
		result = graph.Result{Outcome: graph.OutcomeEnvironmentBlocked, Diagnostics: raw}
		path = "/api/graph/node/block"
	} else {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&result); err != nil {
			return nil, fmt.Errorf("invalid result file: %w", err)
		}
		if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("result file must contain one JSON object")
		}
	}
	if result.Identity == (graph.AttemptIdentity{}) {
		result.Identity = current.Manifest.Identity
	}
	if err := graph.ValidateIdentity(current.Manifest.Identity, result.Identity); err != nil {
		return nil, err
	}
	if err := graph.ValidateResult(current.Manifest.NodeSpec.Node, result); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	_, digest, err := graph.CanonicalJSON(encoded)
	if err != nil {
		return nil, err
	}
	if key == "" {
		key = "graph-result-" + digest
	}
	var response json.RawMessage
	if err := client.PostJSON(ctx, path, graphNodeSubmitBody{IdempotencyKey: key, Result: result}, &response); err != nil {
		return nil, err
	}
	return response, nil
}
