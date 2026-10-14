package vcs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Commit struct {
	SHA       string    `json:"sha"`
	Message   string    `json:"message"`
	Author    string    `json:"author"`
	CreatedAt time.Time `json:"created_at"`
}
type CommitPage struct {
	Commits    []Commit `json:"commits"`
	NextCursor string   `json:"next_cursor,omitempty"`
}

var commitRepositoryPart = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
var fullCommitSHA = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)

func validCommitRepository(repository string) bool {
	parts := strings.Split(repository, "/")
	if len(parts) < 2 || len(repository) > 512 {
		return false
	}
	for _, part := range parts {
		if part == "." || part == ".." || !commitRepositoryPart.MatchString(part) {
			return false
		}
	}
	return true
}

// RepositoryPathForInstance admits only repositories belonging to an already
// connected instance. It never derives a credential destination from the repo.
// Self-hosted connections intentionally support the administrator's private
// network endpoint; browsing cannot select any other endpoint or follow redirects.
func RepositoryPathForInstance(instanceURL, repositoryURL string) (string, bool) {
	base, err := url.Parse(NormalizeInstanceURL(instanceURL))
	if err != nil || (base.Scheme != "https" && base.Scheme != "http") || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return "", false
	}
	if strings.HasPrefix(repositoryURL, "git@") && !strings.Contains(repositoryURL, "://") {
		parts := strings.SplitN(strings.TrimPrefix(repositoryURL, "git@"), ":", 2)
		if len(parts) != 2 {
			return "", false
		}
		repositoryURL = "ssh://git@" + parts[0] + "/" + parts[1]
	}
	repo, err := url.Parse(repositoryURL)
	if err != nil || !strings.EqualFold(repo.Hostname(), base.Hostname()) || repo.RawQuery != "" || repo.Fragment != "" {
		return "", false
	}
	if repo.Scheme == "ssh" {
		if repo.User == nil || repo.User.String() != "git" || (repo.Port() != "" && repo.Port() != "22") {
			return "", false
		}
	} else if repo.Scheme != base.Scheme || !strings.EqualFold(repo.Host, base.Host) || repo.User != nil {
		return "", false
	}
	prefix := strings.TrimRight(base.Path, "/") + "/"
	if !strings.HasPrefix(repo.Path, prefix) {
		return "", false
	}
	repository := strings.TrimSuffix(strings.TrimPrefix(repo.Path, prefix), ".git")
	if !validCommitRepository(repository) {
		return "", false
	}
	return repository, true
}

// ListRepositoryCommits implements the providers' documented read-only APIs:
// https://docs.gitlab.com/api/commits/ and
// https://docs.gitea.com/api/operations/repo-get-all-commits/ (Forgejo-compatible).
func ListRepositoryCommits(ctx context.Context, kind Kind, instanceURL, token, repository, ref string, page int) (CommitPage, error) {
	result := CommitPage{Commits: []Commit{}}
	base, err := url.Parse(NormalizeInstanceURL(instanceURL))
	if err != nil || base.Host == "" || (base.Scheme != "https" && base.Scheme != "http") || base.User != nil || base.RawQuery != "" || base.Fragment != "" || !kind.Valid() || !validCommitRepository(repository) || len(ref) > 256 || page < 1 || page > 10000 {
		return result, errors.New("invalid repository commit request")
	}
	query := url.Values{"page": {strconv.Itoa(page)}}
	var endpoint string
	if kind == KindGitLab {
		query.Set("per_page", "30")
		if ref != "" {
			query.Set("ref_name", ref)
		}
		endpoint = base.String() + "/api/v4/projects/" + url.PathEscape(repository) + "/repository/commits"
	} else {
		parts := strings.Split(repository, "/")
		if len(parts) != 2 {
			return result, errors.New("invalid repository namespace")
		}
		query.Set("limit", "30")
		query.Set("stat", "false")
		if ref != "" {
			query.Set("sha", ref)
		}
		endpoint = base.String() + "/api/v1/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/commits"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+query.Encode(), nil)
	if err != nil {
		return result, errors.New("invalid repository API endpoint")
	}
	req.Header.Set("Accept", "application/json")
	if kind == KindGitLab {
		req.Header.Set("PRIVATE-TOKEN", token)
	} else {
		req.Header.Set("Authorization", "token "+token)
	}
	client := *httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("repository API redirects are not allowed")
	}
	resp, err := client.Do(req)
	if err != nil {
		return result, errors.New("repository API request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return result, ErrUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("repository API returned status %d; verify repository access and ref", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if err != nil || len(raw) > 4<<20 {
		return result, errors.New("repository API response exceeds limit")
	}
	if kind == KindGitLab {
		var rows []struct {
			ID        string    `json:"id"`
			Message   string    `json:"message"`
			Author    string    `json:"author_name"`
			CreatedAt time.Time `json:"created_at"`
		}
		if err = json.Unmarshal(raw, &rows); err != nil || len(rows) > 30 {
			return result, errors.New("invalid repository API response")
		}
		for _, row := range rows {
			result.Commits = append(result.Commits, Commit{row.ID, row.Message, row.Author, row.CreatedAt})
		}
	} else {
		var rows []struct {
			SHA    string `json:"sha"`
			Commit struct {
				Message string `json:"message"`
				Author  struct {
					Name string    `json:"name"`
					Date time.Time `json:"date"`
				} `json:"author"`
			} `json:"commit"`
		}
		if err = json.Unmarshal(raw, &rows); err != nil || len(rows) > 30 {
			return result, errors.New("invalid repository API response")
		}
		for _, row := range rows {
			result.Commits = append(result.Commits, Commit{row.SHA, row.Commit.Message, row.Commit.Author.Name, row.Commit.Author.Date})
		}
	}
	for _, commit := range result.Commits {
		if !fullCommitSHA.MatchString(commit.SHA) {
			return CommitPage{}, errors.New("repository API returned invalid commit SHA")
		}
	}
	if page < 10000 && (resp.Header.Get("X-Next-Page") == strconv.Itoa(page+1) || strings.Contains(resp.Header.Get("Link"), `rel="next"`)) {
		result.NextCursor = strconv.Itoa(page + 1)
	}
	return result, nil
}
