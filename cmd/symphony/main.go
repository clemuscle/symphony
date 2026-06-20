package main

import (
	"log"
	"net/http"
	"os"

	"github.com/joho/godotenv"
	"github.com/yourorg/symphony/internal/api"
	"github.com/yourorg/symphony/internal/catalog"
	"github.com/yourorg/symphony/internal/database"
	"github.com/yourorg/symphony/internal/gitops"
	"github.com/yourorg/symphony/internal/templates"
	gitlabregistry "github.com/yourorg/symphony/internal/providers/artifacts/gitlabregistry"
	gitlabci "github.com/yourorg/symphony/internal/providers/ci/gitlabci"
	dockerdeploy "github.com/yourorg/symphony/internal/providers/deploy/docker"
	gitlabscm "github.com/yourorg/symphony/internal/providers/scm/gitlab"
)

func main() {
	godotenv.Load()

	// Base de données
	db, err := database.Connect()
	if err != nil {
		log.Fatalf("DB: %v", err)
	}
	if err := db.Migrate(); err != nil {
		log.Fatalf("migration: %v", err)
	}
	log.Println("✅ PostgreSQL connecté")

	// Providers
	gitlabURL := getEnv("GITLAB_URL", "http://gitlab.local:8929")
	gitlabToken := os.Getenv("GITLAB_TOKEN")
	if gitlabToken == "" {
		log.Fatal("GITLAB_TOKEN manquant")
	}
	configRepo := getEnv("CONFIG_REPO_PATH", "root/symphony-config")

	scm, err := gitlabscm.New(gitlabURL, gitlabToken)
	if err != nil {
		log.Fatalf("gitlab scm: %v", err)
	}
	ci, err := gitlabci.New(gitlabURL, gitlabToken, configRepo)
	if err != nil {
		log.Fatalf("gitlab ci: %v", err)
	}
	registry, err := gitlabregistry.New(gitlabURL, getEnv("REGISTRY_URL", "gitlab.local:5050"), gitlabToken)
	if err != nil {
		log.Fatalf("gitlab registry: %v", err)
	}
	deploy, err := dockerdeploy.New(getEnv("DOCKER_SOCKET", "/var/run/docker.sock"))
	if err != nil {
		log.Fatalf("docker deploy: %v", err)
	}

	runnerName := getEnv("RUNNER_NAME", "symphony-runner")
	runnerExecutor := getEnv("RUNNER_EXECUTOR_TYPE", "docker")
	if err := ci.EnsureRunner(runnerName, runnerExecutor); err != nil {
		log.Printf("⚠️  EnsureRunner: %v — les pipelines déclenchés échoueront jusqu'à résolution manuelle", err)
	} else {
		log.Println("✅ runner CI disponible")
	}

	// Catalogue + GitOps sync
	store := catalog.NewStore()
	syncer := gitops.NewSyncer(gitlabURL, gitlabToken, configRepo, store)

	// Chargement initial + sync en arrière-plan
	go syncer.Start()

	// Template loader
	tmplRepo := getEnv("TEMPLATES_REPO_PATH", "root/symphony-templates")
	tmplLoader := templates.NewLoader(gitlabURL, gitlabToken, tmplRepo)
	if err := tmplLoader.Load(); err != nil {
		log.Printf("⚠️  templates: %v", err)
	} else {
		log.Printf("✅ %d golden path(s) chargé(s)", len(tmplLoader.GetGoldenPaths()))
	}

	addr := ":" + getEnv("PORT", "8080")
	log.Printf("🎼 Symphony démarré sur %s", addr)
	log.Fatal(http.ListenAndServe(addr, api.NewServer(store, db, scm, ci, registry, deploy, tmplLoader)))
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
