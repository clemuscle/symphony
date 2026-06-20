package gitlab

import (
	"fmt"
	"strings"

	"github.com/yourorg/symphony/internal/providers"
)

// Scaffold pousse les fichiers de démarrage dans le repo
func (p *Provider) Scaffold(repo *providers.RepoResult, cfg providers.ScaffoldConfig) error {
	files := buildScaffold(cfg)
	for path, content := range files {
		err := p.PushFile(repo.Path, repo.DefaultBranch, path, content,
			fmt.Sprintf("chore: scaffold %s [symphony]", path))
		if err != nil {
			return fmt.Errorf("gitlab scaffold %s: %w", path, err)
		}
	}
	return nil
}

func replace(content, name, description string) string {
	content = strings.ReplaceAll(content, "{{SERVICE_NAME}}", name)
	content = strings.ReplaceAll(content, "{{SERVICE_DESCRIPTION}}", description)
	return content
}

func buildScaffold(cfg providers.ScaffoldConfig) map[string]string {
	files := map[string]string{
		"README.md":    replace(readmeTemplate(cfg), cfg.Name, cfg.Description),
		".dockerignore": dockerignoreTemplate(cfg.Language),
	}

	switch cfg.Language {
	case "go":
		files["Dockerfile"] = replace(dockerfileGo(cfg), cfg.Name, cfg.Description)
		files["cmd/main.go"] = replace(mainGo(cfg), cfg.Name, cfg.Description)
		files["cmd/main_test.go"] = replace(testGo(), cfg.Name, cfg.Description)
		files["go.mod"] = replace(goMod(cfg), cfg.Name, cfg.Description)
	case "python":
		files["Dockerfile"] = replace(dockerfilePython(cfg), cfg.Name, cfg.Description)
		files["main.py"] = replace(mainPython(cfg), cfg.Name, cfg.Description)
		files["requirements.txt"] = requirementsPython()
		files["tests/__init__.py"] = ""
		files["tests/test_main.py"] = replace(testPython(cfg), cfg.Name, cfg.Description)
	case "node":
		files["Dockerfile"] = replace(dockerfileNode(cfg), cfg.Name, cfg.Description)
		files["src/index.js"] = replace(mainNode(cfg), cfg.Name, cfg.Description)
		files["package.json"] = replace(packageJSON(cfg), cfg.Name, cfg.Description)
		files["tests/index.test.js"] = replace(testNode(cfg), cfg.Name, cfg.Description)
	case "java":
		files["Dockerfile"] = replace(dockerfileJava(cfg), cfg.Name, cfg.Description)
		files["pom.xml"] = replace(pomXML(cfg), cfg.Name, cfg.Description)
		files["src/main/java/Main.java"] = replace(mainJava(cfg), cfg.Name, cfg.Description)
		files["src/test/java/MainTest.java"] = replace(testJava(cfg), cfg.Name, cfg.Description)
	}
	return files
}

func readmeTemplate(cfg providers.ScaffoldConfig) string {
	return fmt.Sprintf(`# {{SERVICE_NAME}}

> %s

## Stack
- **Langage** : %s
- **Type** : %s  
- **Port** : %d

## Démarrage rapide

`+"```bash"+`
# Lancer en local
docker build -t {{SERVICE_NAME}} .
docker run -p %d:%d {{SERVICE_NAME}}

# Healthcheck
curl http://localhost:%d/healthz

# Métriques
curl http://localhost:%d/metrics
`+"```"+`

## Endpoints
| Endpoint | Description |
|----------|-------------|
| GET / | Info service |
| GET /healthz | Healthcheck |
| GET /metrics | Métriques Prometheus |

## Créé avec [Symphony IDP](http://localhost:8080)
`, cfg.Description, cfg.Language, cfg.Type, cfg.Port,
		cfg.Port, cfg.Port, cfg.Port, cfg.Port)
}

func dockerignoreTemplate(language string) string {
	base := ".git\n.gitlab-ci*.yml\nREADME.md\n"
	extra := map[string]string{
		"go":     "*.test\n*.out\ncoverage.*\n",
		"python": "__pycache__/\n*.pyc\n.pytest_cache/\nvenv/\n.coverage\n",
		"node":   "node_modules/\nnpm-debug.log\ncoverage/\n",
		"java":   "target/\n*.class\n",
	}
	return base + extra[language]
}

func dockerfileGo(cfg providers.ScaffoldConfig) string {
	return fmt.Sprintf(`FROM golang:1.22-alpine AS builder
WORKDIR /app
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o app ./cmd/main.go

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/app /app
EXPOSE %d
ENTRYPOINT ["/app"]
`, cfg.Port)
}

func mainGo(cfg providers.ScaffoldConfig) string {
	return fmt.Sprintf(`package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "service": "{{SERVICE_NAME}}"})
	})

	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("# HELP http_requests_total Total HTTP requests\nhttp_requests_total 0\n"))
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		slog.Info("request", "method", r.Method, "path", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"service": "{{SERVICE_NAME}}", "status": "running"})
	})

	srv := &http.Server{
		Addr: ":%d", Handler: mux,
		ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second,
	}
	slog.Info("starting", "addr", ":%d")
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("failed", "error", err)
		os.Exit(1)
	}
}
`, cfg.Port, cfg.Port)
}

func testGo() string {
	return `package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthz(t *testing.T) {
	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}
`
}

func goMod(cfg providers.ScaffoldConfig) string {
	return "module {{SERVICE_NAME}}\n\ngo 1.22\n"
}

func dockerfilePython(cfg providers.ScaffoldConfig) string {
	return fmt.Sprintf(`FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
EXPOSE %d
CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "%d"]
`, cfg.Port, cfg.Port)
}

func mainPython(cfg providers.ScaffoldConfig) string {
	return fmt.Sprintf(`"""{{SERVICE_NAME}} — API REST Python"""
import logging, os
from fastapi import FastAPI
from fastapi.responses import PlainTextResponse

logging.basicConfig(level=logging.INFO, format='{"time": "%%(asctime)s", "msg": "%%(message)s"}')
logger = logging.getLogger(__name__)

app = FastAPI(title="{{SERVICE_NAME}}", version=os.getenv("VERSION", "dev"))

@app.get("/healthz")
async def healthz():
    return {"status": "ok", "service": "{{SERVICE_NAME}}"}

@app.get("/metrics", response_class=PlainTextResponse)
async def metrics():
    return "# HELP http_requests_total Total requests\nhttp_requests_total 0\n"

@app.get("/")
async def root():
    logger.info("root called")
    return {"service": "{{SERVICE_NAME}}", "status": "running"}
`)
}

func requirementsPython() string {
	return `fastapi>=0.110.0
uvicorn[standard]>=0.29.0
pydantic>=2.6.0

# Tests
pytest>=8.0.0
pytest-asyncio>=0.23.0
httpx>=0.27.0
`
}

func testPython(cfg providers.ScaffoldConfig) string {
	return `"""Tests pour {{SERVICE_NAME}}"""
from fastapi.testclient import TestClient
from main import app

client = TestClient(app)

def test_healthcheck():
    r = client.get("/healthz")
    assert r.status_code == 200
    assert r.json()["status"] == "ok"

def test_root():
    r = client.get("/")
    assert r.status_code == 200
    assert r.json()["status"] == "running"

def test_metrics():
    r = client.get("/metrics")
    assert r.status_code == 200
    assert "http_requests_total" in r.text
`
}

func dockerfileNode(cfg providers.ScaffoldConfig) string {
	return fmt.Sprintf(`FROM node:20-alpine
WORKDIR /app
COPY package*.json ./
RUN npm ci --only=production
COPY . .
EXPOSE %d
CMD ["node", "src/index.js"]
`, cfg.Port)
}

func mainNode(cfg providers.ScaffoldConfig) string {
	return fmt.Sprintf(`const express = require("express");
const app = express();
app.use(express.json());

app.get("/healthz", (req, res) => res.json({ status: "ok", service: "{{SERVICE_NAME}}" }));
app.get("/metrics", (req, res) => {
  res.set("Content-Type", "text/plain");
  res.send("# HELP http_requests_total Total requests\nhttp_requests_total 0\n");
});
app.get("/", (req, res) => res.json({ service: "{{SERVICE_NAME}}", status: "running" }));

const PORT = process.env.PORT || %d;
app.listen(PORT, () => console.log(JSON.stringify({ msg: "started", port: PORT })));
module.exports = app;
`, cfg.Port)
}

func packageJSON(cfg providers.ScaffoldConfig) string {
	return `{
  "name": "{{SERVICE_NAME}}",
  "version": "1.0.0",
  "description": "{{SERVICE_DESCRIPTION}}",
  "main": "src/index.js",
  "scripts": {
    "start": "node src/index.js",
    "test": "jest",
    "test:ci": "jest --ci --forceExit"
  },
  "dependencies": { "express": "^4.19.0" },
  "devDependencies": { "jest": "^29.7.0", "supertest": "^7.0.0" }
}
`
}

func testNode(cfg providers.ScaffoldConfig) string {
	return `const request = require("supertest");
const app = require("../src/index");

test("GET /healthz", async () => {
  const r = await request(app).get("/healthz");
  expect(r.statusCode).toBe(200);
  expect(r.body.status).toBe("ok");
});

test("GET /metrics", async () => {
  const r = await request(app).get("/metrics");
  expect(r.statusCode).toBe(200);
  expect(r.text).toContain("http_requests_total");
});
`
}

func dockerfileJava(cfg providers.ScaffoldConfig) string {
	return fmt.Sprintf(`FROM maven:3.9-eclipse-temurin-21 AS builder
WORKDIR /app
COPY pom.xml .
RUN mvn dependency:go-offline -q
COPY src ./src
RUN mvn package -DskipTests -q

FROM eclipse-temurin:21-jre-alpine
WORKDIR /app
COPY --from=builder /app/target/*.jar app.jar
EXPOSE %d
ENTRYPOINT ["java", "-jar", "app.jar"]
`, cfg.Port)
}

func mainJava(cfg providers.ScaffoldConfig) string {
	return `public class Main {
    public static void main(String[] args) {
        System.out.println("{\"service\": \"{{SERVICE_NAME}}\", \"status\": \"starting\"}");
    }
}
`
}

func testJava(cfg providers.ScaffoldConfig) string {
	return `import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.*;

class MainTest {
    @Test void testPlaceholder() { assertTrue(true); }
}
`
}

func pomXML(cfg providers.ScaffoldConfig) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<project xmlns="http://maven.apache.org/POM/4.0.0"
         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
         xsi:schemaLocation="http://maven.apache.org/POM/4.0.0 http://maven.apache.org/xsd/maven-4.0.0.xsd">
    <modelVersion>4.0.0</modelVersion>
    <groupId>com.symphony</groupId>
    <artifactId>{{SERVICE_NAME}}</artifactId>
    <version>1.0.0</version>
    <properties>
        <maven.compiler.source>21</maven.compiler.source>
        <maven.compiler.target>21</maven.compiler.target>
    </properties>
    <dependencies>
        <dependency>
            <groupId>org.junit.jupiter</groupId>
            <artifactId>junit-jupiter</artifactId>
            <version>5.10.0</version>
            <scope>test</scope>
        </dependency>
    </dependencies>
</project>
`
}
