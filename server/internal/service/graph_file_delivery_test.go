package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/graph"
	"github.com/multica-ai/multica/server/internal/util"
)

func TestGraphPostgresFileDeliveryRequiresVerifiedReceiptAndFreezesReviewInputs(t *testing.T) {
	f := newGraphFixture(t)
	ctx := context.Background()
	d := f.version.Definition
	d.Nodes[1].ExecutionMode = graph.ExecutionProduceFiles
	draft, err := f.svc.CreateDefinition(ctx, f.ws, f.user, d, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	f.version, err = f.svc.Publish(ctx, f.ws, f.user, draft.ID, draft.Revision, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	f.svc.ValidateArtifacts = func(ctx context.Context, tx pgx.Tx, r GraphRunRecord, n GraphNodeRunRecord, result graph.Result, _ GraphTaskReceipt) error {
		if n.Spec.Node.ExecutionMode == graph.ExecutionProduceFiles {
			return ValidateGraphFileDeliveryTx(ctx, tx, r.WorkspaceID, result.Identity.TaskID, result.Artifacts)
		}
		return nil
	}
	run := f.reconcile(t, f.start(t))
	attempt := run.Nodes[1].Attempts[0]
	id := util.MustParseUUID(attempt.TaskID)
	if _, err = f.pool.Exec(ctx, `UPDATE agent_task_queue SET status='dispatched',dispatched_at=now() WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err = f.tasks.StartTask(WithGraphExecutor(ctx, "file-executor"), id); err != nil {
		t.Fatal(err)
	}
	result := graph.Result{Identity: attempt.Manifest.Identity, Outcome: graph.OutcomePass, Output: json.RawMessage(`{"summary":"built page"}`)}
	if _, err = f.svc.Submit(ctx, f.ws, attempt.TaskID, uuid.NewString(), result); err == nil {
		t.Fatal("text-only file producer passed")
	}
	a := graph.Artifact{ID: uuid.NewString(), Type: graph.ArtifactAttachmentRef, ProducerTaskID: attempt.TaskID, MediaType: "application/zip", BundleFormat: "multica-graph-files-v1", Size: 256, ContentHash: strings.Repeat("a", 64)}
	result.Artifacts = []graph.Artifact{a}
	if _, err = f.svc.Submit(ctx, f.ws, attempt.TaskID, uuid.NewString(), result); err == nil {
		t.Fatal("unuploaded files passed")
	}
	if _, err = f.pool.Exec(ctx, `INSERT INTO attachment(id,workspace_id,task_id) VALUES($1,$2,$3)`, a.ID, f.ws, attempt.TaskID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `INSERT INTO graph_attachment_upload(attachment_id,workspace_id,task_id,content_hash,size_bytes,media_type) VALUES($1,$2,$3,$4,$5,$6)`, a.ID, f.ws, attempt.TaskID, a.ContentHash, a.Size, a.MediaType); err != nil {
		t.Fatal(err)
	}
	if _, err = f.svc.Submit(ctx, f.ws, attempt.TaskID, uuid.NewString(), result); err == nil {
		t.Fatal("unverified archive passed")
	}
	if _, err = f.pool.Exec(ctx, `UPDATE graph_attachment_upload SET bundle_verified=true WHERE attachment_id=$1`, a.ID); err != nil {
		t.Fatal(err)
	}
	bad := a
	bad.ID = uuid.NewString()
	if _, err = f.pool.Exec(ctx, `INSERT INTO attachment(id,workspace_id,task_id) VALUES($1,$2,$3)`, bad.ID, f.ws, attempt.TaskID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, `INSERT INTO graph_attachment_upload(attachment_id,workspace_id,task_id,content_hash,size_bytes,media_type) VALUES($1,$2,$3,$4,$5,$6)`, bad.ID, f.ws, attempt.TaskID, bad.ContentHash, bad.Size, bad.MediaType); err != nil {
		t.Fatal(err)
	}
	result.Artifacts = []graph.Artifact{a, bad}
	if _, err = f.svc.Submit(ctx, f.ws, attempt.TaskID, uuid.NewString(), result); err == nil {
		t.Fatal("a genuine bundle hid a second unverified bundle")
	}
	result.Artifacts = []graph.Artifact{a}
	sub, err := f.svc.Submit(ctx, f.ws, attempt.TaskID, uuid.NewString(), result)
	if err != nil {
		t.Fatal(err)
	}
	receipt := GraphTaskReceipt{Identity: attempt.Manifest.Identity, SubmissionID: sub.ID, ProviderExit: "success", DeliveryStatus: "not_required", ExecutorID: "file-executor", StoppedAt: time.Now().UTC()}
	if _, err = f.tasks.CompleteTask(WithGraphReceipt(ctx, receipt), id, []byte(`{"output":"files delivered"}`), "", "", "", false, ""); err != nil {
		t.Fatal(err)
	}
	run = f.reconcile(t, run)
	if run.Nodes[1].State != "succeeded" || len(run.Nodes[2].Attempts) != 1 {
		t.Fatalf("delivery did not advance to independent review: %s %s", run.Nodes[1].State, run.Nodes[2].State)
	}
	current, err := f.svc.TaskContext(ctx, f.ws, run.Nodes[2].Attempts[0].TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Artifacts) != 1 || current.Artifacts[0] != a || len(current.Manifest.NodeSpec.InputArtifacts) != 1 {
		t.Fatalf("reviewer did not receive immutable delivery identity: %+v", current.Artifacts)
	}
}

func TestGraphPostgresTextNodeCannotForgeBundleMarker(t *testing.T) {
	f := newGraphFixture(t)
	ctx := context.Background()
	run := f.reconcile(t, f.start(t))
	a := run.Nodes[1].Attempts[0]
	id := util.MustParseUUID(a.TaskID)
	if _, err := f.pool.Exec(ctx, `UPDATE agent_task_queue SET status='dispatched',dispatched_at=now() WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tasks.StartTask(WithGraphExecutor(ctx, "file-executor"), id); err != nil {
		t.Fatal(err)
	}
	artifact := graph.Artifact{ID: uuid.NewString(), Type: graph.ArtifactAttachmentRef, ProducerTaskID: a.TaskID, MediaType: "application/zip", BundleFormat: "multica-graph-files-v1", Size: 256, ContentHash: strings.Repeat("a", 64)}
	if _, err := f.pool.Exec(ctx, `INSERT INTO attachment(id,workspace_id,task_id) VALUES($1,$2,$3)`, artifact.ID, f.ws, a.TaskID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO graph_attachment_upload(attachment_id,workspace_id,task_id,content_hash,size_bytes,media_type) VALUES($1,$2,$3,$4,$5,$6)`, artifact.ID, f.ws, a.TaskID, artifact.ContentHash, artifact.Size, artifact.MediaType); err != nil {
		t.Fatal(err)
	}
	result := graph.Result{Identity: a.Manifest.Identity, Outcome: graph.OutcomePass, Output: json.RawMessage(`{"summary":"ordinary zip"}`), Artifacts: []graph.Artifact{artifact}}
	if _, err := f.svc.Submit(ctx, f.ws, a.TaskID, uuid.NewString(), result); err == nil {
		t.Fatal("text node forged a verified bundle marker")
	}
	result.Artifacts[0].BundleFormat = ""
	if _, err := f.svc.Submit(ctx, f.ws, a.TaskID, uuid.NewString(), result); err != nil {
		t.Fatalf("ordinary zip attachment rejected: %v", err)
	}
}
