package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/joho/godotenv"
	"github.com/yourorg/symphony/internal/api"
	"github.com/yourorg/symphony/internal/catalogue"
	"github.com/yourorg/symphony/internal/config"
	"github.com/yourorg/symphony/internal/database"
	"github.com/yourorg/symphony/internal/gitops"
	"github.com/yourorg/symphony/internal/provider"
	gitlabscm "github.com/yourorg/symphony/internal/provider/scm/gitlab"
	githubscm "github.com/yourorg/symphony/internal/provider/scm/github"
	gitlabci "github.com/yourorg/symphony/internal/provider/ci/gitlabci"
	githubci "github.com/yourorg/symphony/internal/provider/ci/githubactions"
	dockerrt "github.com/yourorg/symphony/internal/provider/runtime/docker"
	gitlabregistry "github.com/yourorg/symphony/internal/provider/registry/gitlabregistry"
	k8srt "github.com/yourorg/symphony/internal/provider/runtime/kubernetes"
)

func main() {
	// Logger structuré JSON
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// Charger .env
	if err := godotenv.Load(); err != nil {
		slog.Warn("pas de .env trouvé, utilisation des variables d'environnement")
	}

	// Charger la config : bootstrap local, puis repo Git s'il en désigne un
	cfg, err := loadConfig(getEnv("SYMPHONY_CONFIG", "symphony.yaml"))
	if err != nil {
		slog.Error("chargement config", "error", err)
		os.Exit(1)
	}

	// Base de données
	db, err := database.Connect()
	if err != nil {
		slog.Error("connexion DB", "error", err)
		os.Exit(1)
	}
	if err := db.Migrate(); err != nil {
		slog.Error("migration DB", "error", err)
		os.Exit(1)
	}

	// Provider registry
	providers := provider.NewRegistry()

	// Enregistrer les providers depuis la config
	for _, def := range cfg.Providers.SCM {
		p, err := newSCM(def)
		if err != nil {
			slog.Warn("provider SCM ignoré", "id", def.ID, "error", err)
			continue
		}
		providers.RegisterSCM(p, def.Default)
		slog.Info("provider SCM enregistré", "id", def.ID, "type", def.Type)
	}

	for _, def := range cfg.Providers.CI {
		switch def.Type {
		case "gitlab_ci":
			p := gitlabci.New(def.ID, def.Config["url"], gitlabToken(def))
			providers.RegisterCI(p, def.Default)
			slog.Info("provider CI enregistré", "id", def.ID, "type", def.Type)

		case "github_actions":
			token := os.Getenv(def.Config["token_env"])
			p := githubci.New(def.ID, token)
			providers.RegisterCI(p, def.Default)
			slog.Info("provider CI enregistré", "id", def.ID, "type", def.Type)
		}
	}

	for _, def := range cfg.Providers.Registry {
		switch def.Type {
		case "gitlab_registry":
			p := gitlabregistry.New(def.ID, def.Config["url"], def.Config["registry_host"], gitlabToken(def))
			providers.RegisterRegistry(p, def.Default)
			slog.Info("provider registry enregistré", "id", def.ID, "type", def.Type)
		}
	}

	for _, def := range cfg.Providers.Runtime {
		switch def.Type {
		case "docker":
			socket := def.Config["socket"]
			if socket == "" {
				socket = "/var/run/docker.sock"
			}
			p := dockerrt.New(def.ID, socket)
			providers.RegisterRuntime(p, def.Default)
			slog.Info("provider runtime enregistré", "id", def.ID, "type", def.Type)

		case "kubernetes":
			p := k8srt.New(def.ID)
			providers.RegisterRuntime(p, def.Default)
			slog.Info("provider runtime enregistré (stub)", "id", def.ID, "type", def.Type)
		}
	}

	// Catalogue + GitOps syncer
	store := catalogue.NewStore()
	defaultSCM, err := providers.SCM("")
	if err != nil {
		slog.Error("pas de provider SCM par défaut", "error", err)
		os.Exit(1)
	}

	syncer := gitops.NewSyncer(
		defaultSCM,
		store,
		cfg.Catalogue.Repo,
		getEnv("CATALOGUE_BRANCH", "main"),
		cfg.Catalogue.Path,
	)
	go syncer.Start()

	// Golden paths : rechargés depuis le repo de templates
	tmplLoader := gitops.NewTemplateLoader(
		defaultSCM,
		providers,
		orDefault(cfg.Templates.Repo, "root/symphony-templates"),
		orDefault(cfg.Templates.Branch, "main"),
		orDefault(cfg.Templates.Path, "templates"),
	)
	go tmplLoader.Start()

	// Démarrer le serveur
	addr := ":" + getEnv("PORT", "8080")
	slog.Info("Symphony démarré", "addr", addr)

	if err := http.ListenAndServe(addr, api.NewServer(providers, store, db, syncer, tmplLoader)); err != nil {
		slog.Error("serveur HTTP", "error", err)
		os.Exit(1)
	}
}

// loadConfig lit le fichier local. S'il contient config_source, c'est un
// bootstrap : la configuration est lue dans le repo Git qu'il désigne, avec un
// client SCM dédié (les providers ne sont pas encore connus à ce stade).
func loadConfig(path string) (*config.Config, error) {
	local, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	src := local.ConfigSource
	if src == nil {
		slog.Info("configuration chargée", "source", path)
		return local, nil
	}

	if src.Repo == "" {
		return nil, fmt.Errorf("config_source.repo requis dans %s", path)
	}
	if src.SCM.ID == "" {
		src.SCM.ID = "config-source"
	}
	scm, err := newSCM(src.SCM)
	if err != nil {
		return nil, fmt.Errorf("config_source.scm: %w", err)
	}
	branch := orDefault(src.Branch, "main")
	file := orDefault(src.Path, "symphony.yaml")

	content, err := scm.GetFileContent(src.Repo, branch, file)
	if err != nil {
		return nil, fmt.Errorf("lecture de la configuration %s:%s dans %s: %w", file, branch, src.Repo, err)
	}
	cfg, err := config.Parse([]byte(content))
	if err != nil {
		return nil, fmt.Errorf("configuration %s dans %s: %w", file, src.Repo, err)
	}
	if cfg.ConfigSource != nil {
		return nil, fmt.Errorf("configuration %s dans %s: config_source n'est autorisé que dans le bootstrap local", file, src.Repo)
	}
	slog.Info("configuration chargée depuis git", "bootstrap", path, "repo", src.Repo, "branch", branch, "path", file)
	return cfg, nil
}

// newSCM crée un provider SCM à partir de sa définition
func newSCM(def config.ProviderDef) (provider.SCMProvider, error) {
	switch def.Type {
	case "gitlab":
		token := os.Getenv(def.Config["token_env"])
		if token == "" {
			token = def.Config["token"]
		}
		return gitlabscm.New(def.ID, def.Config["url"], token), nil

	case "github":
		return githubscm.New(def.ID, os.Getenv(def.Config["token_env"]), def.Config["org"]), nil
	}
	return nil, fmt.Errorf("type de provider SCM %q inconnu", def.Type)
}

// gitlabToken : token_env, sinon <scm_ref>_TOKEN, sinon GITLAB_TOKEN
// (réutilise le token du SCM GitLab référencé)
func gitlabToken(def config.ProviderDef) string {
	if token := os.Getenv(def.Config["token_env"]); token != "" {
		return token
	}
	if token := os.Getenv(def.Config["scm_ref"] + "_TOKEN"); token != "" {
		return token
	}
	return os.Getenv("GITLAB_TOKEN")
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func orDefault(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}
