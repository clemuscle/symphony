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
	BaseURL        string
	Token          string
	ConfigRepoPath string
	client         *http.Client
}

func New(baseURL, token, configRepoPath string) (*Provider, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("gitlabci: baseURL is required")
	}
	if token == "" {
		return nil, fmt.Errorf("gitlabci: token is required")
	}
	return &Provider{
		BaseURL:        strings.TrimRight(baseURL, "/"),
		Token:          token,
		ConfigRepoPath: configRepoPath,
		client:         &http.Client{Timeout: 15 * time.Second},
	}, nil
}

func (p *Provider) api(method, path string, body any) ([]byte, int, error) {
	var buf io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("gitlabci %s %s: marshal request: %w", method, path, err)
		}
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, p.BaseURL+"/api/v4"+path, buf)
	if err != nil {
		return nil, 0, fmt.Errorf("gitlabci %s %s: build request: %w", method, path, err)
	}
	req.Header.Set("PRIVATE-TOKEN", p.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("gitlabci %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("gitlabci %s %s: read response: %w", method, path, err)
	}
	return data, resp.StatusCode, nil
}

func (p *Provider) SetupPipeline(projectPath string, cfg providers.PipelineConfig) error {
	if cfg.Content == "" {
		return nil
	}
	encoded := url.PathEscape(projectPath)
	filename := ".gitlab-ci.yml"
	payload := map[string]any{
		"branch":         "main",
		"content":        cfg.Content,
		"commit_message": "ci: add pipeline [symphony]",
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
	return nil
}

func (p *Provider) SetProjectVariable(projectPath, key, value string) error {
	encoded := url.PathEscape(projectPath)
	payload := map[string]any{"key": key, "value": value, "masked": true, "protected": false}
	_, status, err := p.api("POST",
		fmt.Sprintf("/projects/%s/variables", encoded), payload)
	if err != nil {
		return err
	}
	if status == 400 {
		// Variable déjà existante — mise à jour
		_, status, err = p.api("PUT",
			fmt.Sprintf("/projects/%s/variables/%s", encoded, url.PathEscape(key)), payload)
		if err != nil {
			return err
		}
	}
	if status != 200 && status != 201 {
		return fmt.Errorf("setProjectVariable %s: status %d", key, status)
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
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("gitlabci triggerPipeline: decode response: %w", err)
	}
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
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("gitlabci getPipelineStatus: decode response: %w", err)
	}
	return result.Status, nil
}

