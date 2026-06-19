package gitlabci

import (
	"fmt"
	"github.com/yourorg/symphony/internal/providers"
)

var languageImages = map[string]string{
	"go":     "golang:1.22-alpine",
	"node":   "node:20-alpine",
	"python": "python:3.12-slim",
	"java":   "maven:3.9-eclipse-temurin-21",
}

var testCommands = map[string]string{
	"go":     "go test ./...",
	"node":   "npm ci && npm test",
	"python": "pip install -r requirements.txt && pytest",
	"java":   "mvn test",
}

func pipelineFiles(cfg providers.PipelineConfig) map[string]string {
	image := languageImages[cfg.Language]
	if image == "" {
		image = "alpine:latest"
	}
	testCmd := testCommands[cfg.Language]
	if testCmd == "" {
		testCmd = "echo 'no tests configured'"
	}

	// Un seul .gitlab-ci.yml avec tous les stages
	pipeline := fmt.Sprintf(`image: %s

stages:
  - test
  - build
  - deploy
  - register

variables:
  IMAGE_NAME: %s
  REGISTRY: $CI_REGISTRY

# ── TEST ──────────────────────────────────────────
test:
  stage: test
  script:
    - %s
  only:
    - merge_requests
    - main

# ── BUILD ─────────────────────────────────────────
build:
  stage: build
  image: docker:24
  services:
    - docker:24-dind
  script:
    - docker login -u $CI_REGISTRY_USER -p $CI_REGISTRY_PASSWORD $CI_REGISTRY
    - docker build -t $REGISTRY/$IMAGE_NAME:$CI_COMMIT_SHORT_SHA .
    - docker push $REGISTRY/$IMAGE_NAME:$CI_COMMIT_SHORT_SHA
    - docker tag $REGISTRY/$IMAGE_NAME:$CI_COMMIT_SHORT_SHA $REGISTRY/$IMAGE_NAME:latest
    - docker push $REGISTRY/$IMAGE_NAME:latest
  tags:
    - docker
  only:
    - main

# ── DEPLOY ────────────────────────────────────────
deploy:
  stage: deploy
  image: docker:24
  script:
    - docker stop $IMAGE_NAME || true
    - docker rm $IMAGE_NAME || true
    - docker run -d
        --name $IMAGE_NAME
        --restart unless-stopped
        $REGISTRY/$IMAGE_NAME:latest
  tags:
    - docker
  only:
    - main

# ── REGISTER — Feedback GitOps vers Symphony ──────
register-service:
  stage: register
  image: alpine:latest
  before_script:
    - apk add --no-cache curl git
    - git config --global user.email "symphony-bot@gitlab.local"
    - git config --global user.name "Symphony Bot"
  script:
    - git clone http://root:$SYMPHONY_TOKEN@gitlab.local:8929/root/symphony-config.git /tmp/symphony-config
    - cd /tmp/symphony-config
    - |
      cat > services/%s.yaml << YAML
apiVersion: symphony/v1
kind: Service
metadata:
  name: "%s"
  owner: "${GITLAB_USER_LOGIN:-symphony-bot}"
  tags:
    - "%s"
    - "auto-registered"
spec:
  type: "%s"
  links:
    - title: "Repository"
      url: "$CI_PROJECT_URL"
    - title: "Registry"
      url: "$CI_REGISTRY_IMAGE"
    - title: "Pipelines"
      url: "$CI_PROJECT_URL/-/pipelines"
YAML
    - git add services/%s.yaml
    - git diff --staged --quiet && echo "Pas de changement" || git commit -m "feat: register %s [symphony-bot]"
    - git push
  only:
    - main
  allow_failure: true
`, image, cfg.Name, testCmd,
		cfg.Name, cfg.Name, cfg.Language, cfg.Type,
		cfg.Name, cfg.Name)

	return map[string]string{
		".gitlab-ci.yml": pipeline,
	}
}
