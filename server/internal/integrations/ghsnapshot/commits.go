package ghsnapshot

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

var repositoryPart = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
var commitSHA = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)

// ListCommits uses the existing workspace installation token. A nil client and
// installationID zero explicitly mean public, unauthenticated repository access.
func (c *Client) ListCommits(ctx context.Context, installationID int64, owner, repo, ref string, page int) (CommitPage, error) {
	result := CommitPage{Commits: []Commit{}}
	if !repositoryPart.MatchString(owner) || !repositoryPart.MatchString(repo) || owner == "." || owner == ".." || repo == "." || repo == ".." || len(ref) > 256 || page < 1 || page > 10000 {
		return result, errors.New("invalid repository commit request")
	}
	base := defaultAPIBase
	client := http.Client{Timeout: 20 * time.Second}
	var token string
	if c != nil {
		base = c.apiBase
		client = *c.httpClient
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("repository API redirects are not allowed")
	}
	if installationID > 0 {
		if !c.Enabled() {
			return result, errors.New("GitHub App credentials are unavailable")
		}
		var err error
		token, err = c.installationToken(ctx, installationID)
		if err != nil {
			return result, err
		}
	}
	query := url.Values{"per_page": []string{"30"}, "page": []string{strconv.Itoa(page)}}
	if ref != "" {
		query.Set("sha", ref)
	}
	endpoint := strings.TrimRight(base, "/") + "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/commits?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return result, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return result, errors.New("repository API request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden {
		return result, rateLimitFromResponse(resp, time.Now())
	}
	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("repository API returned status %d; verify repository access and ref", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	if err != nil || len(raw) > 4<<20 {
		return result, errors.New("repository API response exceeds limit")
	}
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
		if !commitSHA.MatchString(row.SHA) {
			return result, errors.New("repository API returned invalid commit SHA")
		}
		result.Commits = append(result.Commits, Commit{row.SHA, row.Commit.Message, row.Commit.Author.Name, row.Commit.Author.Date})
	}
	if strings.Contains(resp.Header.Get("Link"), `rel="next"`) {
		result.NextCursor = strconv.Itoa(page + 1)
	}
	return result, nil
}
