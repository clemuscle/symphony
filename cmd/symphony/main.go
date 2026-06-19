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

	scm := gitlabscm.New(gitlabURL, gitlabToken)
	ci := gitlabci.New(gitlabURL, gitlabToken)
	registry := gitlabregistry.New(gitlabURL, getEnv("REGISTRY_URL", "gitlab.local:5050"), gitlabToken)
	deploy := dockerdeploy.New(getEnv("DOCKER_SOCKET", "/var/run/docker.sock"))

	// Catalogue + GitOps sync
	store := catalog.NewStore()
	configRepo := getEnv("CONFIG_REPO_PATH", "root/symphony-config")
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
