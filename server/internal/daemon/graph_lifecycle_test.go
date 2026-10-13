package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/graph"
)

func TestGraphProviderSuccessWithMissingOutputStillCompletesPhysicalTask(t *testing.T) {
	task := graphTestTask(t)
	task.RuntimeID = "runtime-1"
	var completed, failed int
	var receipt *GraphTaskReceipt
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/graph-submission"):
			w.WriteHeader(http.StatusNotFound)
		case strings.HasSuffix(r.URL.Path, "/status"):
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "running"})
		case strings.HasSuffix(r.URL.Path, "/complete"):
			completed++
			var body struct {
				Receipt *GraphTaskReceipt `json:"graph_receipt"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			receipt = body.Receipt
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/fail"):
			failed++
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	d := graphLifecycleDaemon(t, server.URL, task)
	d.runner = taskRunnerFunc(func(context.Context, Task, string, int, *slog.Logger) (TaskResult, error) {
		stopped := true
		return TaskResult{Status: "completed", Comment: "provider finished", GraphExecutionStopped: &stopped}, nil
	})
	d.handleTask(context.Background(), task, 0)
	if completed != 1 || failed != 0 || receipt == nil || receipt.ProviderExit != "success" || receipt.DeliveryStatus != "failed" || receipt.FailureReason != "output_invalid" {
		t.Fatalf("completed=%d failed=%d receipt=%+v", completed, failed, receipt)
	}
}

func TestGraphStoppedReceiptSurvivesCancellationDiscard(t *testing.T) {
	task := graphTestTask(t)
	task.RuntimeID = "runtime-1"
	var stopped *GraphTaskReceipt
	var terminal int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/status"):
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "cancelled"})
		case strings.HasSuffix(r.URL.Path, "/graph-stopped"):
			var receipt GraphTaskReceipt
			if err := json.NewDecoder(r.Body).Decode(&receipt); err != nil {
				t.Error(err)
			}
			stopped = &receipt
			w.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(r.URL.Path, "/complete"), strings.HasSuffix(r.URL.Path, "/fail"):
			terminal++
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	d := graphLifecycleDaemon(t, server.URL, task)
	d.cancelPollInterval = time.Millisecond
	d.runner = taskRunnerFunc(func(ctx context.Context, _ Task, _ string, _ int, _ *slog.Logger) (TaskResult, error) {
		<-ctx.Done()
		stopped := true
		return TaskResult{Status: "cancelled", GraphExecutionStopped: &stopped}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	d.handleTask(ctx, task, 0)
	if terminal != 0 || stopped == nil || stopped.StoppedAt.IsZero() || stopped.ExecutorID != d.client.instanceID {
		t.Fatalf("terminal=%d stopped=%+v", terminal, stopped)
	}
	if err := graph.ValidateIdentity(task.Graph.Identity, stopped.Identity); err != nil {
		t.Fatal(err)
	}
}

func TestGraphCannotFinalizeWithoutPhysicalStopProof(t *testing.T) {
	task := graphTestTask(t)
	d := &Daemon{client: NewClient("http://unused.test")}
	for _, stopped := range []*bool{nil, new(bool)} {
		result := TaskResult{Status: "completed", GraphExecutionStopped: stopped}
		d.finalizeGraphTask(context.Background(), task, &result, nil)
		if result.GraphReceipt != nil {
			t.Fatal("unconfirmed process stop produced physical receipt")
		}
		if result.FailureReason != "execution_unknown" {
			t.Fatalf("reason=%s", result.FailureReason)
		}
	}
}

func TestGraphPauseHoldDoesNotCreateFailureOrConsumeAttempt(t *testing.T) {
	task := graphTestTask(t)
	task.RuntimeID = "runtime-1"
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(http.StatusNoContent) }))
	defer server.Close()
	d := graphLifecycleDaemon(t, server.URL, task)
	d.runner = taskRunnerFunc(func(context.Context, Task, string, int, *slog.Logger) (TaskResult, error) {
		return TaskResult{}, &requestError{StatusCode: http.StatusConflict, Body: `{"code":"run_paused"}`}
	})
	d.handleTask(context.Background(), task, 0)
	if calls != 0 {
		t.Fatalf("hold sent %d unexpected callbacks", calls)
	}
}

func graphLifecycleDaemon(t *testing.T, serverURL string, task Task) *Daemon {
	t.Helper()
	return &Daemon{client: NewClient(serverURL), cfg: Config{WorkspacesRoot: t.TempDir()}, logger: slog.Default(), runtimeIndex: map[string]Runtime{task.RuntimeID: {ID: task.RuntimeID, Provider: "fake"}}, cancelPollInterval: time.Hour}
}
