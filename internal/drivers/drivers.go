// Package drivers est le point unique de dispatch type → driver compilé. Il
// importe à la fois internal/providers (interfaces) et les packages driver
// concrets — internal/providers lui-même reste un package feuille (interfaces
// + schéma de config uniquement) pour éviter un cycle d'import, puisque les
// drivers concrets importent déjà providers pour ses types.
//
// Ajouter un nouveau driver (ex: GitHub, Kubernetes) consiste à ajouter une
// entrée dans les maps ci-dessous, jamais à faire du chargement dynamique
// (voir décision d'architecture #2 — drivers compilés, pas de plugins
// runtime).
package drivers

import (
	"fmt"
	"sort"

	"github.com/yourorg/symphony/internal/providers"
	gitlabregistry "github.com/yourorg/symphony/internal/providers/artifacts/gitlabregistry"
	gitlabci "github.com/yourorg/symphony/internal/providers/ci/gitlabci"
	dockerdeploy "github.com/yourorg/symphony/internal/providers/deploy/docker"
	gitlabscm "github.com/yourorg/symphony/internal/providers/scm/gitlab"
)

var scmDrivers = map[string]func(url, token string) (providers.SCMProvider, error){
	"gitlab": func(url, token string) (providers.SCMProvider, error) { return gitlabscm.New(url, token) },
}

var ciDrivers = map[string]func(url, token, configRepo string) (providers.CIProvider, error){
	"gitlabci": func(url, token, configRepo string) (providers.CIProvider, error) {
		return gitlabci.New(url, token, configRepo)
	},
}

var registryDrivers = map[string]func(scmURL, registryURL, token string) (providers.RegistryProvider, error){
	"gitlabregistry": func(scmURL, registryURL, token string) (providers.RegistryProvider, error) {
		return gitlabregistry.New(scmURL, registryURL, token)
	},
}

var deployDrivers = map[string]func(socket string) (providers.DeployProvider, error){
	"docker": func(socket string) (providers.DeployProvider, error) { return dockerdeploy.New(socket) },
}

// AvailableTypes retourne, pour chaque catégorie, la liste des types de
// driver compilés dans ce binaire — utilisé par le wizard pour peupler ses
// sélecteurs sans jamais coder les types en dur côté frontend.
func AvailableTypes() map[string][]string {
	return map[string][]string{
		"scm":      keys(scmDrivers),
		"ci":       keys(ciDrivers),
		"registry": keys(registryDrivers),
		"deploy":   keys(deployDrivers),
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// BuildProviderSet construit les 4 providers actifs à partir de la config
// déclarative, en dispatchant sur le champ Type de chaque catégorie. Erreur
// claire et immédiate si un type n'est pas un driver compilé.
func BuildProviderSet(cfg *providers.IntegrationConfig) (*providers.ProviderSet, error) {
	scmCtor, ok := scmDrivers[cfg.SCM.Type]
	if !ok {
		return nil, fmt.Errorf("scm: type %q non supporté (disponibles: %v)", cfg.SCM.Type, keys(scmDrivers))
	}
	scm, err := scmCtor(cfg.SCM.URL, cfg.SCM.Token)
	if err != nil {
		return nil, fmt.Errorf("scm: %w", err)
	}
	if err := scm.Ping(); err != nil {
		return nil, fmt.Errorf("scm: %w", err)
	}

	ciCtor, ok := ciDrivers[cfg.CI.Type]
	if !ok {
		return nil, fmt.Errorf("ci: type %q non supporté (disponibles: %v)", cfg.CI.Type, keys(ciDrivers))
	}
	ciToken := cfg.CI.Token
	if ciToken == "" {
		ciToken = cfg.SCM.Token
	}
	ci, err := ciCtor(cfg.SCM.URL, ciToken, cfg.CI.ConfigRepo)
	if err != nil {
		return nil, fmt.Errorf("ci: %w", err)
	}

	registryCtor, ok := registryDrivers[cfg.Registry.Type]
	if !ok {
		return nil, fmt.Errorf("registry: type %q non supporté (disponibles: %v)", cfg.Registry.Type, keys(registryDrivers))
	}
	registryToken := cfg.Registry.Token
	if registryToken == "" {
		registryToken = cfg.SCM.Token
	}
	registryURL := cfg.Registry.URL
	if registryURL == "" {
		registryURL = cfg.SCM.URL
	}
	registry, err := registryCtor(cfg.SCM.URL, registryURL, registryToken)
	if err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}

	deployCtor, ok := deployDrivers[cfg.Deploy.Type]
	if !ok {
		return nil, fmt.Errorf("deploy: type %q non supporté (disponibles: %v)", cfg.Deploy.Type, keys(deployDrivers))
	}
	socket := cfg.Deploy.Socket
	if socket == "" {
		socket = "/var/run/docker.sock"
	}
	deploy, err := deployCtor(socket)
	if err != nil {
		return nil, fmt.Errorf("deploy: %w", err)
	}

	return &providers.ProviderSet{
		SCM:          scm,
		CI:           ci,
		Registry:     registry,
		Deploy:       deploy,
		SCMBaseURL:   cfg.SCM.URL,
		SCMToken:     cfg.SCM.Token,
		CIConfigRepo: cfg.CI.ConfigRepo,
	}, nil
}

// Tables de test de connexion, un smoke test par type de driver compilé —
// même principe que les tables de constructeurs ci-dessus : ajouter un
// driver, c'est ajouter une entrée ici aussi. drivers_test.go vérifie que
// chaque type des tables de constructeurs a bien une entrée correspondante
// ici, pour qu'un driver sans test échoue au `go test` plutôt que de dériver
// silencieusement (bouton "Tester" renvoyant "type inconnu" alors que le
// sélecteur du wizard le propose).

var scmTests = map[string]func(config map[string]string) (string, error){
	"gitlab": func(config map[string]string) (string, error) {
		scm, err := gitlabscm.New(config["url"], config["token"])
		if err != nil {
			return "", err
		}
		repos, err := scm.ListRepos()
		if err != nil {
			return "", fmt.Errorf("GitLab inaccessible: %w", err)
		}
		return fmt.Sprintf("GitLab accessible — %d dépôts visibles", len(repos)), nil
	},
}

var ciTests = map[string]func(config map[string]string) (string, error){
	// Le driver CI GitLab parle la même API que le SCM — un token valide
	// capable de lister les projets suffit à confirmer la connectivité.
	"gitlabci": func(config map[string]string) (string, error) {
		scm, err := gitlabscm.New(config["url"], config["token"])
		if err != nil {
			return "", err
		}
		if _, err := scm.ListRepos(); err != nil {
			return "", fmt.Errorf("API GitLab (CI) inaccessible avec ce token: %w", err)
		}
		return "Token CI valide — API GitLab accessible", nil
	},
}

var registryTests = map[string]func(config map[string]string) (string, error){
	// Le driver registre GitLab passe entièrement par l'API GitLab
	// (/api/v4/projects/.../registry/repositories) — jamais un appel direct
	// à l'hôte du registre — donc le même test de token que CI s'applique.
	"gitlabregistry": func(config map[string]string) (string, error) {
		scm, err := gitlabscm.New(config["scm_url"], config["token"])
		if err != nil {
			return "", err
		}
		if _, err := scm.ListRepos(); err != nil {
			return "", fmt.Errorf("API GitLab (registry) inaccessible avec ce token: %w", err)
		}
		return "Token registre valide — API GitLab accessible", nil
	},
}

var deployTests = map[string]func(config map[string]string) (string, error){
	"docker": func(config map[string]string) (string, error) {
		socket := config["socket"]
		if socket == "" {
			socket = "/var/run/docker.sock"
		}
		deploy, err := dockerdeploy.New(socket)
		if err != nil {
			return "", err
		}
		if err := deploy.Ping(); err != nil {
			return "", fmt.Errorf("Docker daemon inaccessible: %w", err)
		}
		return "Docker daemon accessible", nil
	},
}

func testTables() map[string]map[string]func(config map[string]string) (string, error) {
	return map[string]map[string]func(config map[string]string) (string, error){
		"scm":      scmTests,
		"ci":       ciTests,
		"registry": registryTests,
		"deploy":   deployTests,
	}
}

// TestProvider vérifie qu'un provider est joignable avec la config fournie,
// sans construire un ProviderSet complet — utilisé par le bouton "Tester la
// connexion" du wizard, catégorie par catégorie.
func TestProvider(category, providerType string, config map[string]string) (string, error) {
	tests, ok := testTables()[category]
	if !ok {
		return "", fmt.Errorf("catégorie inconnue: %s", category)
	}
	fn, ok := tests[providerType]
	if !ok {
		return "", fmt.Errorf("type inconnu: %s", providerType)
	}
	return fn(config)
}
