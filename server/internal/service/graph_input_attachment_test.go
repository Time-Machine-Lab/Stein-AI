package service

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/graph"
)

func TestGraphPostgresFreezesRootIssueAttachments(t *testing.T) {
	f := newGraphFixture(t)
	ctx := context.Background()
	imageID, commentID, foreignID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, row := range []struct{ id, issue, comment string }{
		{imageID, f.issue, ""}, {commentID, f.issue, uuid.NewString()}, {foreignID, uuid.NewString(), ""},
	} {
		if _, err := f.pool.Exec(ctx, `INSERT INTO attachment(id,workspace_id,issue_id,comment_id,filename,content_type,size_bytes,url) VALUES($1,$2,$3,NULLIF($4,'')::uuid,'design.png','image/png',693272,'/uploads/private-design.png')`, row.id, f.ws, row.issue, row.comment); err != nil {
			t.Fatal(err)
		}
	}
	run := f.start(t)
	var snapshot struct {
		Attachments []struct {
			ID        string `json:"id"`
			Filename  string `json:"filename"`
			MediaType string `json:"media_type"`
			Size      int64  `json:"size"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal(run.RootSnapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Attachments) != 1 || snapshot.Attachments[0].ID != imageID || snapshot.Attachments[0].Filename != "design.png" || snapshot.Attachments[0].MediaType != "image/png" || snapshot.Attachments[0].Size != 693272 {
		t.Fatalf("root input image was not frozen with its metadata: %s", run.RootSnapshot)
	}
	if strings.Contains(string(run.RootSnapshot), "/uploads/") {
		t.Fatal("snapshot stores a storage URL instead of an attachment identity")
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO attachment(id,workspace_id,issue_id) VALUES($1,$2,$3)`, uuid.NewString(), f.ws, f.issue); err != nil {
		t.Fatal(err)
	}
	stored, err := f.svc.GetRun(ctx, f.ws, f.user, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var before, after any
	if err := json.Unmarshal(run.RootSnapshot, &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(stored.RootSnapshot, &after); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("later uploads changed the frozen attachment set")
	}
	if _, err := f.pool.Exec(ctx, `DELETE FROM attachment WHERE id=$1`, imageID); err == nil {
		t.Fatal("a frozen input image could be deleted while the run still references it")
	}
}

func TestGraphPostgresFreezingInputSerializesDeletion(t *testing.T) {
	f := newGraphFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id := uuid.NewString()
	if _, err := f.pool.Exec(ctx, `INSERT INTO attachment(id,workspace_id,issue_id) VALUES($1,$2,$3)`, id, f.ws, f.issue); err != nil {
		t.Fatal(err)
	}
	ready := make(chan uint32, 1)
	release := make(chan struct{})
	f.svc.PrepareRun = func(ctx context.Context, tx pgx.Tx, _, _ string, snapshot json.RawMessage, _ *graph.RepositoryInput) (json.RawMessage, error) {
		ready <- tx.Conn().PgConn().PID()
		select {
		case <-release:
			return snapshot, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	started := make(chan error, 1)
	go func() {
		_, err := f.svc.StartRun(ctx, f.ws, f.user, GraphStartParams{VersionID: f.version.ID, RootIssueID: f.issue, Input: json.RawMessage(`{"goal":"freeze image"}`), RuntimeBindings: map[string]string{"implement": f.runtime, "verify": f.runtime}, IdempotencyKey: uuid.NewString()})
		started <- err
	}()
	var startPID uint32
	select {
	case startPID = <-ready:
	case err := <-started:
		t.Fatalf("start failed before freezing input: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	deleteConn, err := f.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer deleteConn.Release()
	deleted := make(chan error, 1)
	go func() { _, err := deleteConn.Exec(ctx, `DELETE FROM attachment WHERE id=$1`, id); deleted <- err }()
	for {
		var blocked bool
		if err := f.pool.QueryRow(ctx, `SELECT $1::int = ANY(pg_blocking_pids($2))`, int32(startPID), int32(deleteConn.Conn().PgConn().PID())).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case err := <-deleted:
			t.Fatalf("delete bypassed frozen input lock: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	close(release)
	if err := <-started; err != nil {
		t.Fatal(err)
	}
	if err := <-deleted; err == nil {
		t.Fatal("delete removed an image referenced by the committed run")
	}
}

func TestGraphPostgresInputAttachmentReadScope(t *testing.T) {
	f := newGraphFixture(t)
	ctx := context.Background()
	inputID := uuid.NewString()
	if _, err := f.pool.Exec(ctx, `INSERT INTO attachment(id,workspace_id,issue_id,filename,content_type,size_bytes) VALUES($1,$2,$3,'design.png','image/png',42)`, inputID, f.ws, f.issue); err != nil {
		t.Fatal(err)
	}
	run := f.reconcile(t, f.start(t))
	taskID := run.Nodes[1].Attempts[0].TaskID
	laterID, commentID, otherID, foreignID, uploadID, artifactID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, row := range []struct{ id, ws, issue, comment, task string }{
		{laterID, f.ws, f.issue, "", ""},
		{commentID, f.ws, f.issue, uuid.NewString(), ""},
		{otherID, f.ws, uuid.NewString(), "", ""},
		{foreignID, uuid.NewString(), f.issue, "", ""},
		{uploadID, f.ws, "", "", taskID},
		{artifactID, f.ws, "", "", uuid.NewString()},
	} {
		if _, err := f.pool.Exec(ctx, `INSERT INTO attachment(id,workspace_id,issue_id,comment_id,task_id) VALUES($1,$2,NULLIF($3,'')::uuid,NULLIF($4,'')::uuid,NULLIF($5,'')::uuid)`, row.id, row.ws, row.issue, row.comment, row.task); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.pool.Exec(ctx, `INSERT INTO graph_artifact(id,workspace_id,run_id,node_run_id,task_id,content_hash,document) VALUES($1,$2,$3,$4,$5,'test',jsonb_build_object('id',$1::uuid::text,'type','attachment_ref'))`, artifactID, f.ws, run.ID, run.Nodes[1].ID, taskID); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, id, run, ws string
		want              bool
	}{
		{"frozen input", inputID, run.ID, f.ws, true},
		{"later upload", laterID, run.ID, f.ws, false},
		{"comment outside frozen input", commentID, run.ID, f.ws, false},
		{"another issue", otherID, run.ID, f.ws, false},
		{"another workspace", foreignID, run.ID, f.ws, false},
		{"current task upload", uploadID, run.ID, f.ws, true},
		{"accepted artifact", artifactID, run.ID, f.ws, true},
		{"another run", inputID, uuid.NewString(), f.ws, false},
		{"wrong workspace", inputID, run.ID, uuid.NewString(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := GraphTaskCanReadAttachment(ctx, f.pool, tc.ws, tc.run, taskID, tc.id)
			if err != nil || got != tc.want {
				t.Fatalf("allowed = %v, want %v, err = %v", got, tc.want, err)
			}
		})
	}
}
