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

	"github.com/yourorg/symphony/internal/providers"
)

type Provider struct {
	BaseURL string
	Token   string
	client  *http.Client
}

func New(baseURL, token string) *Provider {
	return &Provider{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *Provider) api(method, path string, body any) ([]byte, int, error) {
	var buf io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, p.BaseURL+"/api/v4"+path, buf)
	req.Header.Set("PRIVATE-TOKEN", p.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

func (p *Provider) SetupPipeline(projectPath string, cfg providers.PipelineConfig) error {
	encoded := url.PathEscape(projectPath)
	files := pipelineFiles(cfg)
	for filename, content := range files {
		payload := map[string]any{
			"branch":         "main",
			"content":        content,
			"commit_message": fmt.Sprintf("ci: add %s pipeline [symphony]", cfg.Type),
		}
		_, status, err := p.api("POST",
			fmt.Sprintf("/projects/%s/repository/files/%s", encoded, url.PathEscape(filename)),
			payload)
		if err != nil {
			return err
		}
		if status == 400 {
			p.api("PUT",
				fmt.Sprintf("/projects/%s/repository/files/%s", encoded, url.PathEscape(filename)),
				payload)
		}
	}
	return nil
}

func (p *Provider) TriggerPipeline(projectPath, ref string, vars map[string]string) (string, error) {
	encoded := url.PathEscape(projectPath)
	variables := []map[string]string{}
	for k, v := range vars {
		variables = append(variables, map[string]string{"key": k, "value": v})
	}
	data, status, err := p.api("POST",
		fmt.Sprintf("/projects/%s/pipeline", encoded),
		map[string]any{"ref": ref, "variables": variables})
	if err != nil {
		return "", err
	}
	if status != 201 {
		return "", fmt.Errorf("triggerPipeline: %d — %s", status, string(data))
	}
	var result struct {
		ID int `json:"id"`
	}
	json.Unmarshal(data, &result)
	return fmt.Sprintf("%d", result.ID), nil
}

func (p *Provider) GetPipelineStatus(projectPath, pipelineID string) (string, error) {
	encoded := url.PathEscape(projectPath)
	data, _, err := p.api("GET",
		fmt.Sprintf("/projects/%s/pipelines/%s", encoded, pipelineID), nil)
	if err != nil {
		return "", err
	}
	var result struct {
		Status string `json:"status"`
	}
	json.Unmarshal(data, &result)
	return result.Status, nil
}

