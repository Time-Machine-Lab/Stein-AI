package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/graph"
	"github.com/multica-ai/multica/server/pkg/graphfiles"
)

func TestGraphFilesDeliverThenDownloadIntoIndependentReview(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "index.html"), []byte("<h1>TABELA</h1>"), 0600); err != nil {
		t.Fatal(err)
	}
	manifest := graphCLIManifest()
	manifest.Identity.TaskID = "producer"
	id := "77fdc9c9-ed62-4d45-936d-35dbfa8a4634"
	var uploaded []byte
	var artifact graph.Artifact
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer mat_test" {
			t.Errorf("missing scoped credential on %s", r.URL.Path)
		}
		switch r.URL.Path {
		case "/api/graph/node/context":
			_ = json.NewEncoder(w).Encode(map[string]any{"manifest": manifest})
		case "/api/upload-file":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
				return
			}
			defer r.MultipartForm.RemoveAll()
			if r.FormValue("task_id") != "producer" {
				t.Error("wrong producer")
			}
			f, _, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				return
			}
			defer f.Close()
			uploaded, err = io.ReadAll(f)
			if err != nil {
				t.Error(err)
				return
			}
			if err = graphfiles.Validate(uploaded); err != nil {
				t.Error(err)
			}
			digest := sha256.Sum256(uploaded)
			_ = json.NewEncoder(w).Encode(cli.AttachmentResponse{ID: id, SizeBytes: int64(len(uploaded)), ContentType: "application/zip", ContentHash: hex.EncodeToString(digest[:])})
		case "/api/attachments/" + id:
			_ = json.NewEncoder(w).Encode(cli.AttachmentResponse{ID: id, Filename: "graph-files.zip", SizeBytes: int64(len(uploaded)), ContentType: "application/zip", DownloadURL: "/api/attachments/" + id + "/content"})
		case "/api/attachments/" + id + "/content":
			_, _ = w.Write(uploaded)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := cli.NewAPIClient(server.URL, "ws", "mat_test")
	client.TaskID = "producer"
	var err error
	artifact, err = deliverGraphFiles(context.Background(), client, source)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Identity.TaskID = "reviewer"
	manifest.NodeSpec.InputArtifacts = []graph.Artifact{artifact}
	client.TaskID = "reviewer"
	inputs, err := downloadGraphInputs(context.Background(), client, filepath.Join(t.TempDir(), "inputs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 1 || !inputs[0].Bundle {
		t.Fatalf("missing bundle: %+v", inputs)
	}
	got, err := os.ReadFile(filepath.Join(inputs[0].Path, "index.html"))
	if err != nil || string(got) != "<h1>TABELA</h1>" {
		t.Fatalf("independent reviewer has no implementation: %s %v", got, err)
	}
	// Downloading altered bytes must fail before extraction, even if the server
	// still advertises the original accepted metadata.
	uploaded[len(uploaded)/2] ^= 1
	if _, err = downloadGraphInputs(context.Background(), client, filepath.Join(t.TempDir(), "corrupt")); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("corrupted delivery accepted: %v", err)
	}
}

func TestGraphFilesRejectContextFromAnotherTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatal("unexpected upload")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"manifest": graphCLIManifest()})
	}))
	defer server.Close()
	client := cli.NewAPIClient(server.URL, "ws", "mat_test")
	client.TaskID = "different-task"
	_, err := deliverGraphFiles(context.Background(), client, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "different task") {
		t.Fatalf("wrong task accepted: %v", err)
	}
}

func TestGraphFilesDownloadFrozenImage(t *testing.T) {
	id := "1f924639-2b30-49f1-bdc8-11f4995a8583"
	body := []byte("image-bytes")
	manifest := graphCLIManifest()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/graph/node/context":
			fmt.Fprintf(w, `{"manifest":%s,"root_snapshot":{"attachments":[{"id":%q,"filename":"design.png","media_type":"image/png","size":%d}]}}`, mustGraphJSON(t, manifest), id, len(body))
		case "/api/attachments/" + id:
			_ = json.NewEncoder(w).Encode(cli.AttachmentResponse{ID: id, Filename: "design.png", ContentType: "image/png", SizeBytes: int64(len(body)), DownloadURL: "/image"})
		case "/image":
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	inputs, err := downloadGraphInputs(context.Background(), cli.NewAPIClient(server.URL, "ws", "mat_test"), filepath.Join(t.TempDir(), "inputs"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(inputs[0].Path)
	if err != nil || string(got) != string(body) {
		t.Fatalf("image not delivered: %v", err)
	}
}

func mustGraphJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestGraphFilesOrdinaryZipRemainsAnInputFile(t *testing.T) {
	id := "9a98edc3-68c8-47ce-a788-03480588d0a2"
	body := []byte("ordinary archive without the delivery manifest")
	m := graphCLIManifest()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/graph/node/context":
			fmt.Fprintf(w, `{"manifest":%s,"root_snapshot":{"attachments":[{"id":%q,"filename":"assets.zip","media_type":"application/zip","size":%d}]}}`, mustGraphJSON(t, m), id, len(body))
		case "/api/attachments/" + id:
			_ = json.NewEncoder(w).Encode(cli.AttachmentResponse{ID: id, Filename: "assets.zip", ContentType: "application/zip", SizeBytes: int64(len(body)), DownloadURL: "/archive"})
		case "/archive":
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()
	inputs, err := downloadGraphInputs(context.Background(), cli.NewAPIClient(s.URL, "ws", "mat_test"), filepath.Join(t.TempDir(), "inputs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 1 || inputs[0].Bundle || filepath.Base(inputs[0].Path) != "assets.zip" {
		t.Fatalf("ordinary attachment was mistaken for a delivery: %+v", inputs)
	}
}
