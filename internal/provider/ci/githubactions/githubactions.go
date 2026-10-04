package githubactions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/yourorg/symphony/internal/provider"
)

type Provider struct {
	id     string
	token  string
	client *http.Client
}

func New(id, token string) *Provider {
	return &Provider{
		id:     id,
		token:  token,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *Provider) ID() string { return p.id }

func (p *Provider) api(method, path string, body any) ([]byte, int, error) {
	var buf io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, "https://api.github.com"+path, buf)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

func (p *Provider) TriggerPipeline(repoPath, ref string, vars map[string]string) (*provider.Pipeline, error) {
	inputs := make(map[string]string)
	for k, v := range vars {
		inputs[k] = v
	}

	// GitHub Actions: workflow_dispatch
	// repoPath doit contenir le workflow file: "owner/repo/workflow.yml"
	parts := splitRepoPath(repoPath)
	if len(parts) < 3 {
		return nil, fmt.Errorf("githubactions: repoPath doit être 'owner/repo/workflow.yml', reçu: %s", repoPath)
	}
	owner, repo, workflow := parts[0], parts[1], parts[2]

	_, status, err := p.api("POST",
		fmt.Sprintf("/repos/%s/%s/actions/workflows/%s/dispatches", owner, repo, workflow),
		map[string]any{"ref": ref, "inputs": inputs})
	if err != nil {
		return nil, err
	}
	if status != 204 {
		return nil, fmt.Errorf("github triggerWorkflow: status %d", status)
	}

	// GitHub ne retourne pas l'ID du run immédiatement — on retourne un pipeline pending
	return &provider.Pipeline{
		ID:        "pending",
		Status:    "pending",
		WebURL:    fmt.Sprintf("https://github.com/%s/%s/actions", owner, repo),
		Ref:       ref,
		CreatedAt: time.Now(),
	}, nil
}

func (p *Provider) GetPipeline(repoPath, pipelineID string) (*provider.Pipeline, error) {
	parts := splitRepoPath(repoPath)
	if len(parts) < 2 {
		return nil, fmt.Errorf("repoPath invalide: %s", repoPath)
	}
	owner, repo := parts[0], parts[1]

	data, status, err := p.api("GET",
		fmt.Sprintf("/repos/%s/%s/actions/runs/%s", owner, repo, pipelineID), nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("getWorkflowRun: status %d", status)
	}

	var run struct {
		ID         int64      `json:"id"`
		Status     string     `json:"status"`
		Conclusion string     `json:"conclusion"`
		HTMLURL    string     `json:"html_url"`
		HeadBranch string     `json:"head_branch"`
		CreatedAt  time.Time  `json:"created_at"`
		UpdatedAt  *time.Time `json:"updated_at"`
	}
	json.Unmarshal(data, &run)

	// Normaliser le statut GitHub → statut Symphony
	status2 := run.Status
	if run.Status == "completed" {
		status2 = run.Conclusion // success, failure, cancelled...
	}

	return &provider.Pipeline{
		ID:         fmt.Sprintf("%d", run.ID),
		Status:     status2,
		WebURL:     run.HTMLURL,
		Ref:        run.HeadBranch,
		CreatedAt:  run.CreatedAt,
		FinishedAt: run.UpdatedAt,
	}, nil
}

func (p *Provider) ListPipelines(repoPath string, limit int) ([]provider.Pipeline, error) {
	if limit == 0 {
		limit = 20
	}
	parts := splitRepoPath(repoPath)
	if len(parts) < 2 {
		return nil, fmt.Errorf("repoPath invalide: %s", repoPath)
	}
	owner, repo := parts[0], parts[1]

	data, _, err := p.api("GET",
		fmt.Sprintf("/repos/%s/%s/actions/runs?per_page=%d", owner, repo, limit), nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		WorkflowRuns []struct {
			ID         int64     `json:"id"`
			Status     string    `json:"status"`
			Conclusion string    `json:"conclusion"`
			HTMLURL    string    `json:"html_url"`
			HeadBranch string    `json:"head_branch"`
			CreatedAt  time.Time `json:"created_at"`
		} `json:"workflow_runs"`
	}
	json.Unmarshal(data, &result)

	pipelines := make([]provider.Pipeline, len(result.WorkflowRuns))
	for i, r := range result.WorkflowRuns {
		s := r.Status
		if r.Status == "completed" {
			s = r.Conclusion
		}
		pipelines[i] = provider.Pipeline{
			ID:        fmt.Sprintf("%d", r.ID),
			Status:    s,
			WebURL:    r.HTMLURL,
			Ref:       r.HeadBranch,
			CreatedAt: r.CreatedAt,
		}
	}
	return pipelines, nil
}

func splitRepoPath(path string) []string {
	// "owner/repo" ou "owner/repo/workflow.yml"
	parts := make([]string, 0)
	for _, p := range []string{} {
		parts = append(parts, p)
	}
	return append(parts, splitPath(path)...)
}

func splitPath(path string) []string {
	var parts []string
	start := 0
	for i, c := range path {
		if c == '/' {
			if i > start {
				parts = append(parts, path[start:i])
			}
			start = i + 1
		}
	}
	if start < len(path) {
		parts = append(parts, path[start:])
	}
	return parts
}
