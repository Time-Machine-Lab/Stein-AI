package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestGraphStartOptionsWorkspaceRepositoriesAndMembership(t *testing.T) {
	ctx := context.Background()
	var previous []byte
	if err := testPool.QueryRow(ctx, `SELECT repos FROM workspace WHERE id=$1`, testWorkspaceID).Scan(&previous); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `UPDATE workspace SET repos=$2 WHERE id=$1`, testWorkspaceID, previous) })
	if _, err := testPool.Exec(ctx, `UPDATE workspace SET repos='[{"url":"https://github.com/acme/graph.git","ref":"main"}]' WHERE id=$1`, testWorkspaceID); err != nil {
		t.Fatal(err)
	}
	h := newGraphGenerationTestHandler(t, "")
	w := httptest.NewRecorder()
	h.GetGraphStartOptions(w, newRequest(http.MethodGet, "/api/graphs/start-options", nil))
	if w.Code != 200 {
		t.Fatalf("options: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Repositories []graphRepositoryOption `json:"repositories"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, repo := range out.Repositories {
		if repo.ID == "https://github.com/acme/graph.git" {
			found = true
			if repo.Provider != "github" || repo.DefaultBranch != "main" {
				t.Fatalf("bad repo %+v", repo)
			}
		}
	}
	if !found {
		t.Fatal("existing workspace repository missing")
	}
	req := newRequest(http.MethodGet, "/api/graphs/start-options", nil)
	req.Header.Set("X-User-ID", uuid.NewString())
	w = httptest.NewRecorder()
	h.GetGraphStartOptions(w, req)
	if w.Code != 403 && w.Code != 404 {
		t.Fatalf("nonmember status %d", w.Code)
	}
}

func TestGraphVCSRepositoryCommitsUseConnectedCredentialsOnly(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t)
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "token tok" || r.URL.Path != "/api/v1/repos/acme/repo/commits" {
			t.Errorf("unexpected provider request %s", r.URL)
		}
		_, _ = w.Write([]byte(`[{"sha":"` + strings.Repeat("a", 40) + `","commit":{"message":"test","author":{"name":"alice","date":"2026-10-13T00:00:00Z"}}}]`))
	}))
	defer srv.Close()
	connID := seedVCSConnection(t, ctx, box, "forgejo", srv.URL)
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM vcs_connection WHERE id=$1`, connID) })
	var previous []byte
	if err := testPool.QueryRow(ctx, `SELECT repos FROM workspace WHERE id=$1`, testWorkspaceID).Scan(&previous); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `UPDATE workspace SET repos=$2 WHERE id=$1`, testWorkspaceID, previous) })
	repository := srv.URL + "/acme/repo.git"
	raw, _ := json.Marshal([]RepoData{{URL: repository, Ref: "main"}})
	if _, err := testPool.Exec(ctx, `UPDATE workspace SET repos=$2 WHERE id=$1`, testWorkspaceID, raw); err != nil {
		t.Fatal(err)
	}
	h := newGraphGenerationTestHandler(t, "")
	h.cfg.VCSIntegrationEnabled = true
	h.VCSSecretBox = box
	request := func(id, user string) *httptest.ResponseRecorder {
		r := newRequest(http.MethodGet, "/api/graphs/repositories/repo/commits?ref=main", nil)
		if user != "" {
			r.Header.Set("X-User-ID", user)
		}
		route := chi.NewRouteContext()
		route.URLParams.Add("repositoryId", id)
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, route))
		w := httptest.NewRecorder()
		h.ListGraphRepositoryCommits(w, r)
		return w
	}
	w := request(repository, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), strings.Repeat("a", 40)) {
		t.Fatalf("commits=%d %s", w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	h.GetGraphStartOptions(w, newRequest(http.MethodGet, "/api/graphs/start-options", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"commits_supported":true`) || !strings.Contains(w.Body.String(), `"provider":"forgejo"`) {
		t.Fatalf("options=%d %s", w.Code, w.Body)
	}
	if w = request(srv.URL+"/unconfigured/repo.git", ""); w.Code != 404 {
		t.Fatalf("unconfigured repository status=%d", w.Code)
	}
	if w = request(repository, uuid.NewString()); w.Code != 403 && w.Code != 404 {
		t.Fatalf("nonmember status=%d", w.Code)
	}
	if requests.Load() != 1 {
		t.Fatalf("unauthorized provider calls=%d", requests.Load())
	}
}

func TestGraphRepositoryCoordinatesRejectCredentialURLs(t *testing.T) {
	for _, raw := range []string{"https://user:secret@github.com/acme/repo", "https://evil.example/acme/repo", "https://github.com/acme/repo?token=x", "https://github.com/acme/../repo"} {
		if _, _, ok := graphGitHubCoordinates(raw); ok {
			t.Fatalf("unsafe repository accepted: %s", raw)
		}
	}
	for _, raw := range []string{"git@github.com:acme/repo.git", "ssh://git@github.com/acme/repo.git", "https://github.com/acme/repo"} {
		owner, repo, ok := graphGitHubCoordinates(raw)
		if !ok || owner != "acme" || repo != "repo" {
			t.Fatalf("valid repository rejected: %s", raw)
		}
	}
}
