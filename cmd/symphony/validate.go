package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/clemuscle/symphony/internal/auth"
	"github.com/clemuscle/symphony/internal/config"
	"github.com/clemuscle/symphony/internal/gitops"
	"github.com/clemuscle/symphony/internal/provider"
)

const validateUsage = `symphony validate : vérifie une configuration et des golden paths avant de les pousser

  symphony validate -config symphony.yaml
  symphony validate -templates ./templates
  symphony validate -config symphony.yaml -templates ./templates

Hors ligne : aucun provider n'est appelé, aucun secret n'est lu (les références
secret:// sont vérifiées dans leur forme et leur provider). Une variable ${VAR}
non définie est laissée telle quelle et signalée. Avec -config, les golden
paths sont vérifiés contre ses providers. Code de sortie 1 s'il y a une erreur.
`

// runValidate : la commande symphony validate, pour un admin en local ou la
// CI des repos de configuration et de templates. Toutes les erreurs sont
// rapportées d'un coup.
func runValidate(args []string, out io.Writer) int {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	flags.SetOutput(out)
	flags.Usage = func() { fmt.Fprint(out, validateUsage) }
	configPath := flags.String("config", "", "fichier de configuration (ou bootstrap)")
	templatesDir := flags.String("templates", "", "dossier des golden paths (un sous-dossier par golden path)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *configPath == "" && *templatesDir == "" || flags.NArg() > 0 {
		flags.Usage()
		return 2
	}

	// Les journaux des providers construits n'ont rien à faire dans le rapport
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	r := &report{out: out}
	var providers *provider.Registry
	if *configPath != "" {
		providers = validateConfig(r, *configPath)
	}
	if *templatesDir != "" {
		validateTemplates(r, *templatesDir, providers, *configPath != "")
	}
	fmt.Fprintln(out)
	if r.errors > 0 {
		fmt.Fprintf(out, "✗ %d erreur%s\n", r.errors, plural(r.errors))
		return 1
	}
	fmt.Fprintln(out, "✓ aucune erreur")
	return 0
}

// report écrit le résultat, une ligne par vérification
type report struct {
	out    io.Writer
	errors int
}

func (r *report) section(title string) { fmt.Fprintf(r.out, "\n%s\n", title) }
func (r *report) ok(format string, a ...any) {
	fmt.Fprintf(r.out, "  ✓ %s\n", fmt.Sprintf(format, a...))
}
func (r *report) warn(format string, a ...any) {
	fmt.Fprintf(r.out, "  ! %s\n", fmt.Sprintf(format, a...))
}

// fail : une erreur ; celles réunies par errors.Join sont listées une par ligne
func (r *report) fail(what string, err error) {
	r.errors++
	lines := strings.Split(err.Error(), "\n")
	if len(lines) == 1 {
		fmt.Fprintf(r.out, "  ✗ %s : %s\n", what, lines[0])
		return
	}
	r.errors += len(lines) - 1
	fmt.Fprintf(r.out, "  ✗ %s :\n", what)
	for _, l := range lines {
		fmt.Fprintf(r.out, "      - %s\n", l)
	}
}

// validateConfig vérifie la configuration comme au chargement : décodage
// strict, schéma de chaque provider, références de secrets, construction des
// providers, environnements, authentification et webhooks. Renvoie les
// providers construits (pour vérifier les golden paths), nil en cas d'erreur.
func validateConfig(r *report, file string) *provider.Registry {
	r.section("Configuration " + file)
	data, err := os.ReadFile(file)
	if err != nil {
		r.fail("lecture", err)
		return nil
	}
	var unset []string
	cfg, err := config.ParseWith(data, func(name string) string {
		if v, ok := os.LookupEnv(name); ok {
			return v
		}
		unset = append(unset, name)
		return "${" + name + "}"
	})
	// Une variable citée dans un commentaire n'est pas utilisée
	used := referencedVars(data)
	for _, name := range dedupe(unset) {
		if used[name] {
			r.warn("variable %s non définie : laissée telle quelle", name)
		}
	}
	if err != nil {
		r.fail("format", err)
		return nil
	}

	if cfg.ConfigSource != nil {
		validateBootstrap(r, cfg)
		return nil
	}

	providers, resolved, err := assembleProviders(cfg, registerPlaceholderSecrets)
	if err != nil {
		r.fail("providers", err)
		return nil
	}
	for _, e := range providers.Entries() {
		if e.Kind == "secrets" && e.ID == envSecretsID && len(cfg.Providers.Secrets) == 0 {
			continue
		}
		def := ""
		if e.Default {
			def = " (par défaut)"
		}
		r.ok("%s %s%s", e.Kind, e.ID, def)
	}
	if len(resolved.Environments) > 0 {
		names := make([]string, len(resolved.Environments))
		for i, env := range resolved.Environments {
			names[i] = env.Name
			if env.Protected {
				names[i] += " (protégé)"
			}
		}
		r.ok("environnements : %s", strings.Join(names, " → "))
	}

	codec, _, _ := auth.NewCodec("")
	if _, err := buildPolicy(resolved, providers, codec); err != nil {
		r.fail("auth", err)
	} else if cfg.Auth == nil {
		r.warn("pas de section auth : authentification désactivée, tout le monde est admin")
	} else {
		r.ok("auth : rôles %s", strings.Join(sortedKeys(cfg.Auth.Roles), ", "))
	}
	if _, err := buildWebhooks(resolved); err != nil {
		r.fail("webhooks", err)
	} else if cfg.Webhooks == nil {
		r.warn("pas de section webhooks : les pipelines sont suivies par sondage")
	} else {
		r.ok("webhooks : %s", cfg.Webhooks.URL)
	}
	repo, branch, dir := templatesSource(cfg)
	r.ok("golden paths lus dans %s (branche %s, dossier %s)", repo, branch, dir)
	return providers
}

// validateBootstrap : un bootstrap ne porte que config_source et ses
// providers de secrets ; la configuration elle-même est dans Git
func validateBootstrap(r *report, cfg *config.Config) {
	src := cfg.ConfigSource
	var errs []error
	if src.Repo == "" {
		errs = append(errs, fmt.Errorf("config_source.repo requis"))
	}
	if err := validateProviders(config.ProvidersConfig{Secrets: cfg.Providers.Secrets}); err != nil {
		errs = append(errs, err)
	}
	if err := validateDef("scm", src.SCM); err != nil {
		errs = append(errs, fmt.Errorf("config_source.scm : %w", err))
	}
	if len(errs) > 0 {
		for _, err := range errs {
			r.fail("bootstrap", err)
		}
		return
	}
	r.ok("bootstrap : configuration lue dans %s:%s (branche %s), à valider dans son repo",
		src.Repo, orDefault(src.Path, "symphony.yaml"), orDefault(src.Branch, "main"))
}

// validateTemplates vérifie chaque golden path du dossier dir, comme au
// chargement ; contre les providers de la configuration s'il y en a
func validateTemplates(r *report, dir string, providers *provider.Registry, withConfig bool) {
	r.section("Golden paths " + dir)
	root := os.DirFS(dir)
	exists := map[string]bool{}
	err := fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			exists[p] = true
		}
		return nil
	})
	if err != nil {
		r.fail("lecture", err)
		return
	}
	switch {
	case providers == nil && withConfig:
		r.warn("configuration invalide : providers des golden paths non vérifiés")
	case providers == nil:
		r.warn("sans -config : providers des golden paths non vérifiés")
	}

	var files []string
	for f := range exists {
		if path.Base(f) == gitops.TemplateFile && path.Dir(path.Dir(f)) == "." && path.Dir(f) != "." {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		r.fail("golden paths", fmt.Errorf("aucun <nom>/%s dans %s", gitops.TemplateFile, dir))
		return
	}
	for _, f := range files {
		content, err := fs.ReadFile(root, f)
		if err != nil {
			r.fail(f, err)
			continue
		}
		gp, err := gitops.CheckTemplate(path.Dir(f), string(content), exists, providers)
		if err != nil {
			r.fail(filepath.Join(dir, f), err)
			continue
		}
		r.ok("%s : %s", gp.ID, gp.Name)
	}
}

// placeholderSecret : la valeur de toute référence secret:// pour validate
// (assez longue pour les contrôles de longueur, ex. webhooks.secret)
const placeholderSecret = "secret-non-lu-par-symphony-validate"

// placeholderSecrets : un provider de secrets qui ne lit rien
type placeholderSecrets struct{ id, typ string }

func (p placeholderSecrets) ID() string                      { return p.id }
func (p placeholderSecrets) Type() string                    { return p.typ }
func (p placeholderSecrets) Get(_, _ string) (string, error) { return placeholderSecret, nil }
func (p placeholderSecrets) Check() (string, error)          { return "hors ligne", nil }

// registerPlaceholderSecrets : registerSecrets hors ligne. Chaque provider de
// secrets est construit (sa définition est vérifiée), puis remplacé par un
// substitut : les références vers lui sont acceptées sans être lues.
func registerPlaceholderSecrets(providers *provider.Registry, defs []config.ProviderDef) error {
	providers.RegisterSecrets(placeholderSecrets{envSecretsID, "env"}, false)
	for _, def := range defs {
		if def.ID == "" || def.ID == envSecretsID {
			return fmt.Errorf("provider de secrets : id vide ou réservé (%q)", def.ID)
		}
		resolved, err := resolveDef(def, providers.Secrets)
		if err != nil {
			return fmt.Errorf("provider de secrets %s: %w", def.ID, err)
		}
		if _, err := newSecrets(resolved); err != nil {
			return fmt.Errorf("provider de secrets %s: %w", def.ID, err)
		}
		providers.RegisterSecrets(placeholderSecrets{def.ID, def.Type}, def.Default)
	}
	return nil
}

var varRe = regexp.MustCompile(`\$\{?([A-Za-z_][A-Za-z0-9_]*)`)

// referencedVars : variables citées hors des commentaires YAML (approché :
// ce qui suit « # » en début de ligne ou après une espace)
func referencedVars(data []byte) map[string]bool {
	used := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if i := strings.Index(line, " #"); i >= 0 {
			line = line[:i]
		}
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		for _, m := range varRe.FindAllStringSubmatch(line, -1) {
			used[m[1]] = true
		}
	}
	return used
}

func dedupe(s []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func plural(n int) string {
	if n > 1 {
		return "s"
	}
	return ""
}
