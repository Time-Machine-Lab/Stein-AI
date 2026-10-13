package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// graphPendingReport contains terminal facts only, never credentials or an agent configuration.
type graphPendingReport struct {
	Kind          terminalTaskReportKind `json:"kind"`
	TaskID        string                 `json:"task_id"`
	Output        string                 `json:"output,omitempty"`
	ErrorMessage  string                 `json:"error,omitempty"`
	FailureReason string                 `json:"failure_reason,omitempty"`
	BranchName    string                 `json:"branch_name,omitempty"`
	WorkDir       string                 `json:"work_dir,omitempty"`
	Receipt       *GraphTaskReceipt      `json:"graph_receipt"`
}

func (d *Daemon) persistGraphReport(report terminalTaskReport) (string, error) {
	if report.graphReceipt == nil {
		return "", nil
	}
	if d.cfg.WorkspacesRoot == "" {
		return "", fmt.Errorf("Graph terminal outbox requires a managed workspaces root")
	}
	if report.taskID != report.graphReceipt.Identity.TaskID || report.graphReceipt.ExecutorID == "" {
		return "", fmt.Errorf("Graph terminal report lacks execution identity")
	}
	record := graphPendingReport{Kind: report.kind, TaskID: report.taskID, Output: report.output, ErrorMessage: report.errorMessage, FailureReason: report.failureReason, BranchName: report.branchName, WorkDir: report.workDir, Receipt: report.graphReceipt}
	data, err := json.Marshal(record)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	dir := filepath.Join(d.cfg.WorkspacesRoot, ".graph-receipts")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, hex.EncodeToString(digest[:])+".json")
	if _, err := os.Stat(path); err == nil {
		return path, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	file, err := os.CreateTemp(dir, ".pending-")
	if err != nil {
		return "", err
	}
	temp := file.Name()
	defer os.Remove(temp)
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return "", err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return "", err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(temp, path); err != nil {
		return "", err
	}
	return path, nil
}

func (d *Daemon) graphReportLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		d.flushGraphReports(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (d *Daemon) flushGraphReports(ctx context.Context) {
	if d.cfg.WorkspacesRoot == "" {
		return
	}
	dir := filepath.Join(d.cfg.WorkspacesRoot, ".graph-receipts")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var record graphPendingReport
		if len(data) > 4<<20 || json.Unmarshal(data, &record) != nil || record.Receipt == nil || record.Receipt.Identity.TaskID != record.TaskID || record.Receipt.ExecutorID == "" {
			continue
		}
		// A persisted stop was observed by that process. Replay its identity,
		// even when the process delivering the evidence has since restarted.
		client := NewClient(d.client.baseURL)
		client.SetToken(d.client.Token())
		client.SetVersion(d.client.version)
		client.instanceID = record.Receipt.ExecutorID
		requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err = sendGraphPendingReport(requestCtx, client, record)
		cancel()
		client.CloseIdleConnections()
		if err == nil {
			if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
				d.logger.Warn("remove acknowledged Graph report", "error", removeErr)
			}
		}
	}
}

func sendGraphPendingReport(ctx context.Context, client *Client, record graphPendingReport) error {
	path := "/api/daemon/tasks/" + record.TaskID
	if record.Kind == terminalTaskReportGraphStopped {
		return client.postJSON(ctx, path+"/graph-stopped", record.Receipt, nil)
	}
	body := map[string]any{"graph_receipt": record.Receipt, "work_dir": record.WorkDir, "branch_name": record.BranchName}
	switch record.Kind {
	case terminalTaskReportComplete:
		path += "/complete"
		body["output"] = record.Output
	case terminalTaskReportFail:
		path += "/fail"
		body["error"] = record.ErrorMessage
		body["failure_reason"] = record.FailureReason
	default:
		return fmt.Errorf("unknown Graph report kind")
	}
	return client.postJSON(ctx, path, body, nil)
}

func (d *Daemon) reportGraphStopped(ctx context.Context, taskID string, receipt *GraphTaskReceipt) error {
	if receipt == nil {
		return nil
	}
	return d.reportTerminalTask(ctx, terminalTaskReport{kind: terminalTaskReportGraphStopped, taskID: taskID, graphReceipt: receipt})
}
