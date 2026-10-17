package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestGraphInputImageDownloadsThroughExistingAttachmentAPI(t *testing.T) {
	store := installProxyModeStorage(t)
	var body bytes.Buffer
	if err := png.Encode(&body, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	id := seedPreviewAttachment(t, store, "graph/design.png", "design.png", "image/png", body.Bytes())
	other := seedPreviewAttachment(t, store, "graph/other.png", "other.png", "image/png", body.Bytes())
	issue := createTestIssue(t, "Graph input download", "todo", "none")
	agent := createHandlerTestAgent(t, "Graph input download", nil)
	tx, err := testPool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	h := *testHandler
	h.DB, h.Queries = tx, testHandler.Queries.WithTx(tx)
	run, node, task := uuid.NewString(), uuid.NewString(), uuid.NewString()
	commands := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO agent_task_queue(id,issue_id,agent_id,runtime_id,status) VALUES($1,$2,$3,$4,'running')`, []any{task, issue, agent, testRuntimeID}},
		{`INSERT INTO graph_run(id,workspace_id,root_issue_id,version_id,initiator_id,state,control_state,document) VALUES($1,$2,$3,$4,$5,'active','run',jsonb_build_object('root_snapshot',jsonb_build_object('attachments',jsonb_build_array(jsonb_build_object('id',$6::text)))))`, []any{run, testWorkspaceID, issue, uuid.NewString(), testUserID, id}},
		{`INSERT INTO graph_node_run(id,workspace_id,run_id,node_key,issue_id,state,current_task_id,document) VALUES($1,$2,$3,'requirements',$4,'active',$5,'{}')`, []any{node, testWorkspaceID, run, issue, task}},
		{`INSERT INTO graph_task_attempt(task_id,workspace_id,run_id,node_run_id,attempt_no,execution_generation,input_hash,manifest,manifest_hash) VALUES($1,$2,$3,$4,1,1,'input','{}','manifest')`, []any{task, testWorkspaceID, run, node}},
	}
	for _, command := range commands {
		if _, err := tx.Exec(context.Background(), command.query, command.args...); err != nil {
			t.Fatal(err)
		}
	}
	metadata := func(attachment string) *httptest.ResponseRecorder {
		req := withURLParam(newRequest(http.MethodGet, "/api/attachments/"+attachment, nil), "id", attachment)
		req.Header.Set("X-Actor-Source", "task_token")
		req.Header.Set("X-Task-ID", task)
		w := httptest.NewRecorder()
		h.GraphTaskTokenScope(http.HandlerFunc(h.GetAttachmentByID)).ServeHTTP(w, req)
		return w
	}
	w := metadata(id)
	if w.Code != http.StatusOK {
		t.Fatalf("Graph input metadata: status %d, %s", w.Code, w.Body.String())
	}
	var attachment AttachmentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &attachment); err != nil {
		t.Fatal(err)
	}
	// The CLI exchanges metadata for a fresh capability, then downloads bytes.
	download := httptest.NewRecorder()
	req := withURLParam(httptest.NewRequest(http.MethodGet, attachment.DownloadURL, nil), "id", id)
	h.DownloadAttachmentWithCapability(download, req)
	if download.Code != http.StatusOK || !bytes.Equal(download.Body.Bytes(), body.Bytes()) {
		t.Fatalf("Graph image bytes were not delivered: status %d", download.Code)
	}
	if _, err := png.Decode(bytes.NewReader(download.Body.Bytes())); err != nil {
		t.Fatalf("downloaded design cannot be decoded: %v", err)
	}
	if denied := metadata(other); denied.Code != http.StatusNotFound {
		t.Fatalf("non-input attachment readable: status %d", denied.Code)
	}
	if _, err := tx.Exec(context.Background(), `UPDATE agent_task_queue SET status='completed' WHERE id=$1`, task); err != nil {
		t.Fatal(err)
	}
	if stopped := metadata(id); stopped.Code != http.StatusConflict {
		t.Fatalf("stopped task retained input access: status %d", stopped.Code)
	}
}
