package githubactions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/clemuscle/symphony/internal/provider"
)

type Provider struct {
	id     string
	token  string
	apiURL string
	client *http.Client
}

func New(id, token string) *Provider {
	return &Provider{
		id:     id,
		token:  token,
		apiURL: "https://api.github.com",
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

// WithAPIURL : autre API GitHub (tests, GitHub Enterprise)
func (p *Provider) WithAPIURL(u string) *Provider {
	p.apiURL = strings.TrimRight(u, "/")
	return p
}

func (p *Provider) ID() string { return p.id }

func (p *Provider) Platform() string { return "github" }

// workflowFile : workflow poussé à la création du projet, déclenché par défaut
const workflowFile = "ci.yml"

func (p *Provider) ConfigFile() string { return ".github/workflows/" + workflowFile }

func (p *Provider) PipelinesURL(repoWebURL string) string { return repoWebURL + "/actions" }

func (p *Provider) api(method, path string, body any) ([]byte, int, error) {
	var buf io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, p.apiURL+path, buf)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("github api: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return data, resp.StatusCode, nil
}

// run : un workflow run de l'API GitHub
type run struct {
	ID         int64     `json:"id"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	HTMLURL    string    `json:"html_url"`
	HeadBranch string    `json:"head_branch"`
	Event      string    `json:"event"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (r run) pipeline() provider.Pipeline {
	pl := provider.Pipeline{
		ID:        strconv.FormatInt(r.ID, 10),
		Status:    normalize(r.Status, r.Conclusion),
		WebURL:    r.HTMLURL,
		Ref:       r.HeadBranch,
		CreatedAt: r.CreatedAt,
	}
	if r.Status == "completed" {
		finished := r.UpdatedAt
		pl.FinishedAt = &finished
	}
	return pl
}

// normalize traduit statut et conclusion GitHub en statut Symphony (ceux de
// GitLab CI) : pending, running, success, failed, canceled, skipped
func normalize(status, conclusion string) string {
	switch status {
	case "completed":
		switch conclusion {
		case "success":
			return "success"
		case "cancelled":
			return "canceled"
		case "skipped", "neutral":
			return "skipped"
		default: // failure, timed_out, startup_failure, action_required, stale
			return "failed"
		}
	case "in_progress":
		return "running"
	default: // queued, requested, waiting, pending
		return "pending"
	}
}

// splitRepoPath : "owner/repo[/workflow.yml]" → owner, repo, workflow
// (défaut : le workflow créé par Symphony)
func splitRepoPath(repoPath string) (owner, repo, workflow string, err error) {
	parts := strings.Split(strings.Trim(repoPath, "/"), "/")
	switch len(parts) {
	case 2:
		return parts[0], parts[1], workflowFile, nil
	case 3:
		return parts[0], parts[1], parts[2], nil
	}
	return "", "", "", fmt.Errorf("githubactions: repoPath doit être 'owner/repo[/workflow.yml]', reçu: %s", repoPath)
}

// dispatchWait : durée pendant laquelle on cherche le run créé par un
// workflow_dispatch quand l'API ne renvoie pas son id
var dispatchWait = 10 * time.Second

// TriggerPipeline lance le workflow (workflow_dispatch) avec vars en entrées :
// le workflow doit les déclarer toutes, sinon GitHub refuse (422). Le run
// créé est renvoyé ; s'il n'apparaît pas à temps, son ID est vide et
// l'appelant attend la prochaine pipeline du repo.
func (p *Provider) TriggerPipeline(repoPath, ref string, vars map[string]string) (*provider.Pipeline, error) {
	owner, repo, workflow, err := splitRepoPath(repoPath)
	if err != nil {
		return nil, err
	}
	inputs := make(map[string]string, len(vars))
	for k, v := range vars {
		inputs[k] = v
	}
	path := fmt.Sprintf("/repos/%s/%s/actions/workflows/%s/dispatches", owner, repo, url.PathEscape(workflow))
	before := time.Now()
	// return_run_details : l'API renvoie alors l'id du run (200) ; une API
	// plus ancienne qui ne le connaît pas répond 204 ou le refuse (422)
	data, status, err := p.api("POST", path, map[string]any{"ref": ref, "inputs": inputs, "return_run_details": true})
	if err == nil && status == http.StatusUnprocessableEntity && strings.Contains(string(data), "return_run_details") {
		data, status, err = p.api("POST", path, map[string]any{"ref": ref, "inputs": inputs})
	}
	if err != nil {
		return nil, err
	}
	pending := &provider.Pipeline{
		Status:    "pending",
		WebURL:    fmt.Sprintf("https://github.com/%s/%s/actions", owner, repo),
		Ref:       ref,
		CreatedAt: before,
	}
	switch status {
	case http.StatusOK:
		var resp struct {
			WorkflowRunID int64  `json:"workflow_run_id"`
			HTMLURL       string `json:"html_url"`
		}
		if json.Unmarshal(data, &resp) == nil && resp.WorkflowRunID != 0 {
			pending.ID = strconv.FormatInt(resp.WorkflowRunID, 10)
			if resp.HTMLURL != "" {
				pending.WebURL = resp.HTMLURL
			}
			return pending, nil
		}
	case http.StatusNoContent:
	default:
		return nil, fmt.Errorf("github workflow_dispatch %s: status %d — %s", workflow, status, strings.TrimSpace(string(data)))
	}

	// Pas d'id dans la réponse : le run du dispatch apparaît dans la liste
	// au bout de quelques secondes
	deadline := time.Now().Add(dispatchWait)
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		runs, err := p.runs(owner, repo, "event=workflow_dispatch&branch="+url.QueryEscape(ref)+"&per_page=5")
		if err != nil {
			break
		}
		for _, r := range runs {
			if !r.CreatedAt.Before(before.Add(-5 * time.Second)) {
				pl := r.pipeline()
				return &pl, nil
			}
		}
	}
	return pending, nil
}

func (p *Provider) runs(owner, repo, query string) ([]run, error) {
	data, status, err := p.api("GET", fmt.Sprintf("/repos/%s/%s/actions/runs?%s", owner, repo, query), nil)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, provider.ErrNotFound
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("github listRuns: status %d — %s", status, strings.TrimSpace(string(data)))
	}
	var result struct {
		WorkflowRuns []run `json:"workflow_runs"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("github listRuns: %w", err)
	}
	return result.WorkflowRuns, nil
}

func (p *Provider) GetPipeline(repoPath, pipelineID string) (*provider.Pipeline, error) {
	owner, repo, _, err := splitRepoPath(repoPath)
	if err != nil {
		return nil, err
	}
	data, status, err := p.api("GET", fmt.Sprintf("/repos/%s/%s/actions/runs/%s", owner, repo, url.PathEscape(pipelineID)), nil)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return nil, provider.ErrNotFound
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("github getWorkflowRun: status %d", status)
	}
	var r run
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("github getWorkflowRun: %w", err)
	}
	pl := r.pipeline()
	return &pl, nil
}

// ListPipelines : runs les plus récents d'abord
func (p *Provider) ListPipelines(repoPath string, limit int) ([]provider.Pipeline, error) {
	if limit <= 0 {
		limit = 20
	}
	owner, repo, _, err := splitRepoPath(repoPath)
	if err != nil {
		return nil, err
	}
	runs, err := p.runs(owner, repo, "per_page="+strconv.Itoa(limit))
	if err != nil {
		return nil, err
	}
	pipelines := make([]provider.Pipeline, len(runs))
	for i, r := range runs {
		pipelines[i] = r.pipeline()
	}
	return pipelines, nil
}
