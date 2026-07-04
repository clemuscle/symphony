package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gpDir := filepath.Join(dir, "go-test")

	os.MkdirAll(filepath.Join(gpDir, "files"), 0755)
	os.MkdirAll(filepath.Join(gpDir, "ci"), 0755)

	os.WriteFile(filepath.Join(gpDir, "golden-path.yaml"), []byte(`
apiVersion: symphony/v1
kind: GoldenPath
metadata:
  name: go-test
  description: "Test golden path"
  icon: go
spec:
  language: go
  type: rest-api
  default_port: 9090
`), 0644)

	os.WriteFile(filepath.Join(gpDir, "files", "main.go"), []byte(`package {{.ServiceName}}`), 0644)
	os.WriteFile(filepath.Join(gpDir, "ci", "pipeline.yml"), []byte(`image: golang
service: {{.ServiceName}}:{{.Port}}`), 0644)

	return dir
}

func TestLoad_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	l := NewLoader(dir)
	if err := l.Load(); err != nil {
		t.Fatalf("expected no error on empty dir, got: %v", err)
	}
	if len(l.GetGoldenPaths()) != 0 {
		t.Errorf("expected 0 golden paths, got %d", len(l.GetGoldenPaths()))
	}
}

func TestLoad_NonExistent(t *testing.T) {
	l := NewLoader("/does/not/exist/at/all")
	if err := l.Load(); err != nil {
		t.Fatalf("expected no error on non-existent dir, got: %v", err)
	}
}

func TestGetGoldenPaths(t *testing.T) {
	dir := setupFixture(t)
	l := NewLoader(dir)
	if err := l.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	paths := l.GetGoldenPaths()
	if len(paths) != 1 {
		t.Fatalf("expected 1 golden path, got %d", len(paths))
	}
	gp := paths[0]
	if gp.Metadata.Name != "go-test" {
		t.Errorf("expected name 'go-test', got %q", gp.Metadata.Name)
	}
	if gp.Spec.Language != "go" {
		t.Errorf("expected language 'go', got %q", gp.Spec.Language)
	}
	if gp.Spec.DefaultPort != 9090 {
		t.Errorf("expected default_port 9090, got %d", gp.Spec.DefaultPort)
	}
}

func TestRenderFiles(t *testing.T) {
	dir := setupFixture(t)
	l := NewLoader(dir)
	if err := l.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	vars := ScaffoldVars{ServiceName: "my-svc", Port: 8080}
	files, err := l.RenderFiles("go", vars)
	if err != nil {
		t.Fatalf("RenderFiles: %v", err)
	}

	content, ok := files["main.go"]
	if !ok {
		t.Fatal("expected main.go in rendered files")
	}
	if content != "package my-svc" {
		t.Errorf("expected 'package my-svc', got %q", content)
	}
}

func TestRenderFiles_UnknownLanguage(t *testing.T) {
	dir := setupFixture(t)
	l := NewLoader(dir)
	if err := l.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	_, err := l.RenderFiles("rust", ScaffoldVars{})
	if err == nil {
		t.Fatal("expected error for unknown language")
	}
}

func TestRenderCI(t *testing.T) {
	dir := setupFixture(t)
	l := NewLoader(dir)
	if err := l.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	vars := ScaffoldVars{ServiceName: "my-svc", Port: 3000}
	ci, err := l.RenderCI("go", vars)
	if err != nil {
		t.Fatalf("RenderCI: %v", err)
	}

	if !strings.Contains(ci, "my-svc:3000") {
		t.Errorf("expected 'my-svc:3000' in CI output, got:\n%s", ci)
	}
	if !strings.Contains(ci, "image: golang") {
		t.Errorf("expected 'image: golang' in CI output, got:\n%s", ci)
	}
}
