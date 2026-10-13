package vcs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRepositoryPathForInstance(t *testing.T) {
	for _, tc := range []struct{ instance, repo, want string }{
		{"https://git.example/base", "https://git.example/base/team/repo.git", "team/repo"},
		{"https://git.example", "git@git.example:group/sub/repo.git", "group/sub/repo"},
		{"https://git.example", "https://git.example.evil/team/repo", ""},
		{"https://git.example/base", "https://git.example/baseball/team/repo", ""},
		{"https://git.example", "https://git.example:8443/team/repo", ""},
		{"https://git.example", "http://git.example/team/repo", ""},
		{"https://git.example", "https://token@git.example/team/repo", ""},
		{"https://git.example", "https://git.example/team/%2e%2e/repo", ""},
		{"https://git.example", "https://git.example/team/repo?secret=1", ""},
	} {
		got, _ := RepositoryPathForInstance(tc.instance, tc.repo)
		if got != tc.want {
			t.Errorf("%s: got %q want %q", tc.repo, got, tc.want)
		}
	}
}

func TestListRepositoryCommits(t *testing.T) {
	for _, kind := range []Kind{KindGitLab, KindGitea, KindForgejo} {
		t.Run(string(kind), func(t *testing.T) {
			sha := strings.Repeat("a", 40)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if kind == KindGitLab {
					if r.URL.EscapedPath() != "/base/api/v4/projects/group%2Fsub%2Frepo/repository/commits" || r.URL.Query().Get("ref_name") != "feature/a&b" || r.Header.Get("PRIVATE-TOKEN") != "test-token" {
						t.Errorf("wrong GitLab request: %s", r.URL)
					}
					w.Header().Set("X-Next-Page", "3")
					_ = json.NewEncoder(w).Encode([]any{map[string]any{"id": sha, "message": "fix\nbody", "author_name": "alice", "created_at": "2026-10-13T00:00:00Z"}})
				} else {
					if r.URL.Path != "/base/api/v1/repos/group/repo/commits" || r.URL.Query().Get("sha") != "feature/a&b" || r.Header.Get("Authorization") != "token test-token" {
						t.Errorf("wrong Forgejo request: %s", r.URL)
					}
					w.Header().Set("Link", `<https://ignored.example?page=3>; rel="next"`)
					_ = json.NewEncoder(w).Encode([]any{map[string]any{"sha": sha, "commit": map[string]any{"message": "fix\nbody", "author": map[string]any{"name": "alice", "date": "2026-10-13T00:00:00Z"}}}})
				}
				if r.URL.Query().Get("page") != "2" {
					t.Errorf("missing page")
				}
			}))
			defer srv.Close()
			repository := "group/repo"
			if kind == KindGitLab {
				repository = "group/sub/repo"
			}
			page, err := ListRepositoryCommits(context.Background(), kind, srv.URL+"/base", "test-token", repository, "feature/a&b", 2)
			if err != nil || len(page.Commits) != 1 || page.Commits[0].SHA != sha || page.Commits[0].Author != "alice" || page.NextCursor != "3" {
				t.Fatalf("page=%+v error=%v", page, err)
			}
		})
	}
}

func TestListRepositoryCommitsRefusesRedirectAndBadSHA(t *testing.T) {
	for _, redirect := range []bool{true, false} {
		var leaked bool
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = true }))
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if redirect {
				http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
				return
			}
			_, _ = w.Write([]byte(`[{"sha":"bad"}]`))
		}))
		_, err := ListRepositoryCommits(context.Background(), KindForgejo, srv.URL, "test-token", "group/repo", "main", 1)
		srv.Close()
		target.Close()
		if err == nil || leaked {
			t.Fatalf("invalid response/redirect accepted; err=%v leaked=%v", err, leaked)
		}
	}
}
