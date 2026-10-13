package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestGraphTerminalOutboxReplaysAfterRestartWithOriginalExecutor(t *testing.T) {
	task := graphTestTask(t)
	var got GraphTaskReceipt
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Receipt GraphTaskReceipt `json:"graph_receipt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		got = payload.Receipt
		if r.Header.Get("X-Daemon-Instance-ID") != got.ExecutorID {
			t.Error("replay changed executor identity")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	d := Daemon{client: NewClient(server.URL), cfg: Config{WorkspacesRoot: t.TempDir()}, logger: slog.Default()}
	receipt := d.graphStoppedReceipt(task, "success")
	path, err := d.persistGraphReport(terminalTaskReport{kind: terminalTaskReportComplete, taskID: task.ID, output: "done", graphReceipt: receipt})
	if err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Fatal("Graph report was not made durable")
	}
	d.client = NewClient(server.URL)
	if d.client.instanceID == receipt.ExecutorID {
		t.Fatal("test requires a restarted executor")
	}
	d.flushGraphReports(context.Background())
	if got.ExecutorID != receipt.ExecutorID || got.Identity.TaskID != task.ID {
		t.Fatalf("wrong replay: %+v", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("acknowledged report not removed: %v", err)
	}
}

func TestGraphTerminalOutboxRetainsUnacknowledgedEvidence(t *testing.T) {
	task := graphTestTask(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer server.Close()
	d := Daemon{client: NewClient(server.URL), cfg: Config{WorkspacesRoot: t.TempDir()}, logger: slog.Default()}
	path, err := d.persistGraphReport(terminalTaskReport{kind: terminalTaskReportFail, taskID: task.ID, errorMessage: "failed", graphReceipt: d.graphStoppedReceipt(task, "failed")})
	if err != nil {
		t.Fatal(err)
	}
	d.flushGraphReports(context.Background())
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("unacknowledged evidence lost: %v", err)
	}
	if filepath.Dir(path) != filepath.Join(d.cfg.WorkspacesRoot, ".graph-receipts") {
		t.Fatal("report escaped managed outbox")
	}
}
