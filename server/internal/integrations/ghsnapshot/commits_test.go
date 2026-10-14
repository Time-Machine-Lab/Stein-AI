package ghsnapshot

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestListCommitsUsesBoundInstallationAndEscapesRef(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/graph/commits" || r.URL.Query().Get("sha") != "release/v2&secret" || r.URL.Query().Get("page") != "2" {
			t.Errorf("wrong request %s", r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("installation token missing")
		}
		w.Header().Set("Link", `<https://api.github.com/repositories/1/commits?page=3>; rel="next"`)
		fmt.Fprint(w, `[{"sha":"0123456789012345678901234567890123456789","commit":{"message":"Verified commit","author":{"name":"Tester","date":"2026-10-13T00:00:00Z"}}}]`)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	client.tokens[7] = cachedToken{token: "test-token", expiry: time.Now().Add(time.Hour)}
	page, err := client.ListCommits(context.Background(), 7, "acme", "graph", "release/v2&secret", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Commits) != 1 || page.Commits[0].Message != "Verified commit" || page.NextCursor != "3" {
		t.Fatalf("bad page %+v", page)
	}
}

func TestListCommitsRejectsRedirectCredentialLeak(t *testing.T) {
	foreign := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("redirect followed") }))
	defer foreign.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, foreign.URL, 302) }))
	defer server.Close()
	client := newTestClient(t, server.URL)
	client.tokens[7] = cachedToken{token: "test-token", expiry: time.Now().Add(time.Hour)}
	if _, err := client.ListCommits(context.Background(), 7, "acme", "graph", "main", 1); err == nil {
		t.Fatal("redirect accepted")
	}
}
