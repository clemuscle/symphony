package gitlabci

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/clemuscle/symphony/internal/provider"
)

type Provider struct {
	id      string
	baseURL string
	token   string
	client  *http.Client
}

func New(id, baseURL, token string) *Provider {
	return &Provider{
		id:      id,
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *Provider) ID() string { return p.id }

func (p *Provider) Platform() string { return "gitlab" }

func (p *Provider) ConfigFile() string { return ".gitlab-ci.yml" }

func (p *Provider) PipelinesURL(repoWebURL string) string { return repoWebURL + "/-/pipelines" }

func (p *Provider) api(method, path string, body any) ([]byte, int, error) {
	var buf io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, p.baseURL+"/api/v4"+path, buf)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("PRIVATE-TOKEN", p.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

func (p *Provider) TriggerPipeline(repoPath, ref string, vars map[string]string) (*provider.Pipeline, error) {
	encoded := url.PathEscape(repoPath)

	variables := make([]map[string]string, 0, len(vars))
	for k, v := range vars {
		variables = append(variables, map[string]string{"key": k, "value": v})
	}

	data, status, err := p.api("POST",
		fmt.Sprintf("/projects/%s/pipeline", encoded),
		map[string]any{"ref": ref, "variables": variables})
	if err != nil {
		return nil, err
	}
	if status != 201 {
		return nil, fmt.Errorf("triggerPipeline: status %d — %s", status, string(data))
	}

	var result struct {
		ID     int    `json:"id"`
		Status string `json:"status"`
		WebURL string `json:"web_url"`
		Ref    string `json:"ref"`
	}
	json.Unmarshal(data, &result)

	return &provider.Pipeline{
		ID:        fmt.Sprintf("%d", result.ID),
		Status:    result.Status,
		WebURL:    result.WebURL,
		Ref:       result.Ref,
		CreatedAt: time.Now(),
	}, nil
}

func (p *Provider) GetPipeline(repoPath, pipelineID string) (*provider.Pipeline, error) {
	encoded := url.PathEscape(repoPath)
	data, status, err := p.api("GET",
		fmt.Sprintf("/projects/%s/pipelines/%s", encoded, pipelineID), nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("getPipeline: status %d", status)
	}

	var result struct {
		ID         int        `json:"id"`
		Status     string     `json:"status"`
		WebURL     string     `json:"web_url"`
		Ref        string     `json:"ref"`
		CreatedAt  time.Time  `json:"created_at"`
		FinishedAt *time.Time `json:"finished_at"`
	}
	json.Unmarshal(data, &result)

	return &provider.Pipeline{
		ID:         fmt.Sprintf("%d", result.ID),
		Status:     result.Status,
		WebURL:     result.WebURL,
		Ref:        result.Ref,
		CreatedAt:  result.CreatedAt,
		FinishedAt: result.FinishedAt,
	}, nil
}

func (p *Provider) ListPipelines(repoPath string, limit int) ([]provider.Pipeline, error) {
	if limit == 0 {
		limit = 20
	}
	encoded := url.PathEscape(repoPath)
	data, _, err := p.api("GET",
		fmt.Sprintf("/projects/%s/pipelines?per_page=%d&order_by=id&sort=desc", encoded, limit), nil)
	if err != nil {
		return nil, err
	}

	var results []struct {
		ID        int       `json:"id"`
		Status    string    `json:"status"`
		WebURL    string    `json:"web_url"`
		Ref       string    `json:"ref"`
		CreatedAt time.Time `json:"created_at"`
	}
	json.Unmarshal(data, &results)

	pipelines := make([]provider.Pipeline, len(results))
	for i, r := range results {
		pipelines[i] = provider.Pipeline{
			ID:        fmt.Sprintf("%d", r.ID),
			Status:    r.Status,
			WebURL:    r.WebURL,
			Ref:       r.Ref,
			CreatedAt: r.CreatedAt,
		}
	}
	return pipelines, nil
}
