package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/integrations/ghsnapshot"
	"github.com/multica-ai/multica/server/internal/integrations/vcs"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type graphRepositoryOption struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Provider         string `json:"provider"`
	URL              string `json:"url"`
	DefaultBranch    string `json:"default_branch,omitempty"`
	ProjectID        string `json:"project_id,omitempty"`
	CommitsSupported bool   `json:"commits_supported"`
}

var graphRepositoryName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func graphGitHubCoordinates(raw string) (string, string, bool) {
	if strings.HasPrefix(raw, "git@github.com:") {
		raw = "ssh://git@github.com/" + strings.TrimPrefix(raw, "git@github.com:")
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Hostname(), "github.com") || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", "", false
	}
	if u.Scheme != "https" && u.Scheme != "ssh" {
		return "", "", false
	}
	if u.User != nil && (u.Scheme != "ssh" || u.User.String() != "git") {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 {
		return "", "", false
	}
	owner, repo := parts[0], strings.TrimSuffix(parts[1], ".git")
	if !graphRepositoryName.MatchString(owner) || !graphRepositoryName.MatchString(repo) || owner == "." || owner == ".." || repo == "." || repo == ".." {
		return "", "", false
	}
	return owner, repo, true
}
func graphRepositoryOptionFor(id, raw, ref, label, projectID string) (graphRepositoryOption, bool) {
	if !isValidGitRepoURL(raw) {
		return graphRepositoryOption{}, false
	}
	if u, err := url.Parse(raw); err == nil {
		if u.RawQuery != "" || u.Fragment != "" {
			return graphRepositoryOption{}, false
		}
		if u.User != nil {
			if _, password := u.User.Password(); password || u.User.Username() != "git" {
				return graphRepositoryOption{}, false
			}
		}
	}
	provider := "git"
	if owner, repo, ok := graphGitHubCoordinates(raw); ok {
		provider = "github"
		if label == "" {
			label = owner + "/" + repo
		}
	}
	if label == "" {
		label = raw
	}
	return graphRepositoryOption{ID: id, Name: label, Provider: provider, URL: raw, DefaultBranch: ref, ProjectID: projectID, CommitsSupported: provider == "github"}, true
}

func (h *Handler) graphRepositories(ctx context.Context, ws, rootIssueID string) ([]graphRepositoryOption, error) {
	result := []graphRepositoryOption{}
	workspace, err := h.Queries.GetWorkspace(ctx, parseUUID(ws))
	if err != nil {
		return nil, err
	}
	var repos []RepoData
	if len(workspace.Repos) > 0 {
		if err = json.Unmarshal(workspace.Repos, &repos); err != nil {
			return nil, err
		}
	}
	for _, repo := range repos {
		if option, ok := graphRepositoryOptionFor(repo.URL, repo.URL, repo.Ref, "", ""); ok {
			result = append(result, option)
		}
	}
	var projectID any
	if rootIssueID != "" {
		id, err := util.ParseUUID(rootIssueID)
		if err != nil {
			return nil, &service.GraphError{Code: "invalid_id", Message: "invalid root_issue_id"}
		}
		issue, err := h.Queries.GetIssue(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) || err == nil && uuidToString(issue.WorkspaceID) != ws {
			return nil, &service.GraphError{Code: "not_found", Message: "root issue is unavailable"}
		}
		if err != nil {
			return nil, err
		}
		if !issue.ProjectID.Valid {
			return result, nil
		}
		projectID = uuidToString(issue.ProjectID)
	}
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT r.id,r.project_id,r.resource_ref,COALESCE(r.label,'') FROM project_resource r JOIN project p ON p.id=r.project_id AND p.workspace_id=r.workspace_id WHERE r.workspace_id=$1 AND r.resource_type='github_repo' AND ($2::uuid IS NULL OR r.project_id=$2) ORDER BY p.title,r.position,r.id`, ws, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, project, label string
		var raw []byte
		if err = rows.Scan(&id, &project, &raw, &label); err != nil {
			return nil, err
		}
		var ref githubRepoRef
		if err = json.Unmarshal(raw, &ref); err != nil {
			return nil, err
		}
		branch := ref.Ref
		if branch == "" {
			branch = ref.DefaultBranchHint
		}
		if option, ok := graphRepositoryOptionFor(id, ref.URL, branch, label, project); ok {
			result = append(result, option)
		}
	}
	return result, rows.Err()
}

func (h *Handler) GetGraphStartOptions(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := h.graphHuman(w, r)
	if !ok {
		return
	}
	if _, ok = h.workspaceMember(w, r, ws); !ok {
		return
	}
	repositories, err := h.graphRepositories(r.Context(), ws, r.URL.Query().Get("root_issue_id"))
	if err != nil {
		writeGraphError(w, err)
		return
	}
	if h.isVCSAvailable() && h.isVCSConfigured() {
		connections, err := h.Queries.ListVCSConnectionsByWorkspace(r.Context(), parseUUID(ws))
		if err != nil {
			writeGraphError(w, err)
			return
		}
		for i := range repositories {
			if repositories[i].Provider == "github" {
				continue
			}
			matched := 0
			for _, connection := range connections {
				if _, ok := vcs.RepositoryPathForInstance(connection.InstanceUrl, repositories[i].URL); ok && vcs.Kind(connection.Provider).Valid() {
					matched++
					repositories[i].Provider = connection.Provider
				}
			}
			repositories[i].CommitsSupported = matched == 1
		}
	}
	writeJSON(w, 200, map[string]any{"repositories": repositories})
}

func (h *Handler) ListGraphRepositoryCommits(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := h.graphHuman(w, r)
	if !ok {
		return
	}
	if _, ok = h.workspaceMember(w, r, ws); !ok {
		return
	}
	requested := chi.URLParam(r, "repositoryId")
	if unescaped, err := url.PathUnescape(requested); err == nil {
		requested = unescaped
	}
	repos, err := h.graphRepositories(r.Context(), ws, r.URL.Query().Get("root_issue_id"))
	if err != nil {
		writeGraphError(w, err)
		return
	}
	var selected *graphRepositoryOption
	for i := range repos {
		if repos[i].ID == requested {
			selected = &repos[i]
			break
		}
	}
	if selected == nil {
		writeErrorCode(w, 404, "not_found", "Repository is unavailable in this workspace or root issue project.")
		return
	}
	ref := strings.TrimSpace(r.URL.Query().Get("q"))
	if ref == "" {
		ref = strings.TrimSpace(r.URL.Query().Get("ref"))
	}
	if ref == "" {
		ref = selected.DefaultBranch
	}
	page := 1
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		page, err = strconv.Atoi(raw)
	}
	if err != nil || page < 1 || page > 10000 || len(ref) > 256 {
		writeErrorCode(w, 400, "invalid_request", "Invalid commit search ref or cursor.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	owner, repo, ok := graphGitHubCoordinates(selected.URL)
	if !ok {
		h.listGraphVCSCommits(ctx, w, ws, selected.URL, ref, page)
		return
	}
	installations, err := h.Queries.ListGitHubInstallationsByWorkspace(ctx, parseUUID(ws))
	if err != nil {
		writeGraphError(w, err)
		return
	}
	var installationID int64
	for _, installation := range installations {
		if strings.EqualFold(installation.AccountLogin, owner) {
			installationID = installation.InstallationID
			break
		}
	}
	result, err := h.GraphGitHubClient.ListCommits(ctx, installationID, owner, repo, ref, page)
	if err != nil {
		var limited *ghsnapshot.RateLimitError
		if errors.As(err, &limited) {
			w.Header().Set("Retry-After", strconv.Itoa(int(limited.RetryAfter.Seconds())))
			writeErrorCode(w, 429, "repository_rate_limited", "Repository browsing is rate limited. Try again after the indicated delay.")
			return
		}
		writeErrorCode(w, 502, "repository_unavailable", err.Error())
		return
	}
	writeJSON(w, 200, result)
}

func (h *Handler) listGraphVCSCommits(ctx context.Context, w http.ResponseWriter, ws, repositoryURL, ref string, page int) {
	if !h.isVCSAvailable() || !h.isVCSConfigured() {
		writeErrorCode(w, 409, "commit_browser_unavailable", "Connect this Git provider in workspace settings, or enter a full commit SHA.")
		return
	}
	connections, err := h.Queries.ListVCSConnectionsByWorkspace(ctx, parseUUID(ws))
	if err != nil {
		writeGraphError(w, err)
		return
	}
	var selected *db.VcsConnection
	var repository string
	for i := range connections {
		candidate := &connections[i]
		if !vcs.Kind(candidate.Provider).Valid() {
			continue
		}
		if relative, matches := vcs.RepositoryPathForInstance(candidate.InstanceUrl, repositoryURL); matches {
			if selected != nil {
				writeErrorCode(w, 409, "repository_connection_ambiguous", "More than one connected Git instance matches this repository. Use its exact HTTPS repository URL.")
				return
			}
			selected, repository = candidate, relative
		}
	}
	if selected == nil {
		writeErrorCode(w, 409, "commit_browser_unavailable", "Connect this Git provider in workspace settings, or enter a full commit SHA.")
		return
	}
	token, err := h.openVCSSecret(selected.AccessTokenEncrypted)
	if err != nil {
		writeErrorCode(w, 503, "repository_credentials_unavailable", "Git provider credentials are unavailable. Reconnect the instance in workspace settings.")
		return
	}
	result, err := vcs.ListRepositoryCommits(ctx, vcs.Kind(selected.Provider), selected.InstanceUrl, token, repository, ref, page)
	if err != nil {
		writeErrorCode(w, 502, "repository_unavailable", err.Error())
		return
	}
	writeJSON(w, 200, result)
}
