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
	"slices"
	"sort"
	"strings"

	"github.com/clemuscle/symphony/internal/auth"
	"github.com/clemuscle/symphony/internal/config"
	"github.com/clemuscle/symphony/internal/gitops"
	"github.com/clemuscle/symphony/internal/provider"
	officialstacks "github.com/clemuscle/symphony/stacks"
)

const validateUsage = `symphony validate : vérifie une configuration et des stacks avant de les pousser

  symphony validate -config symphony.yaml
  symphony validate -stacks ./stacks
  symphony validate -config symphony.yaml -stacks ./stacks

Hors ligne : aucun provider n'est appelé, aucun secret n'est lu (les références
secret:// sont vérifiées dans leur forme et leur provider). Une variable ${VAR}
non définie est laissée telle quelle et signalée. Avec -config, les stacks (du
dossier et officielles exposées) sont vérifiées contre ses providers. Code de
sortie 1 s'il y a une erreur.
`

// runValidate : la commande symphony validate, pour un admin en local ou la
// CI des repos de configuration et de stacks. Toutes les erreurs sont
// rapportées d'un coup.
func runValidate(args []string, out io.Writer) int {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	flags.SetOutput(out)
	flags.Usage = func() { fmt.Fprint(out, validateUsage) }
	configPath := flags.String("config", "", "fichier de configuration (ou bootstrap)")
	stacksDir := flags.String("stacks", "", "dossier des stacks (un sous-dossier par stack)")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *configPath == "" && *stacksDir == "" || flags.NArg() > 0 {
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
	if *stacksDir != "" {
		validateStacks(r, *stacksDir, providers, *configPath != "")
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
// providers construits (pour vérifier les stacks), nil en cas d'erreur.
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

	stacks, warnings, err := stacksSource(cfg)
	if err != nil {
		r.fail("stacks", err)
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
	if discovery, err := buildDiscovery(resolved, providers); err != nil {
		r.fail("découverte", err)
	} else {
		for _, src := range discovery.Sources {
			r.ok("découverte : %s (ci %s, registry %s), tous les %s", src.Key(), src.CI, orDefault(src.Registry, "aucun"), gitops.FormatInterval(discovery.Interval))
		}
	}
	if n, err := buildNotifications(resolved, providers); err != nil {
		r.fail("notifications", err)
	} else if n != nil {
		for _, team := range sortedKeys(n.Teams) {
			r.ok("notifications : équipe %s → %s", team, strings.Join(n.Teams[team], ", "))
		}
		if len(n.Default) > 0 {
			r.ok("notifications : autres équipes → %s", strings.Join(n.Default, ", "))
		}
	}
	for _, w := range warnings {
		r.warn("%s", w)
	}
	if stacks.Repo != "" {
		r.ok("stacks lues dans %s (branche %s, dossier %s)", stacks.Repo, stacks.Branch, stacks.Path)
	}
	validateOfficial(r, stacks.Official, providers)
	return providers
}

// validateOfficial vérifie les stacks officielles exposées contre les
// providers de la configuration (CI de la plateforme du SCM par défaut…)
func validateOfficial(r *report, names []string, providers *provider.Registry) {
	if len(names) == 0 {
		return
	}
	official := officialstacks.FS()
	if slices.Contains(names, "*") {
		names = gitops.OfficialNames(official)
	}
	exists := filesOf(official)
	for _, name := range names {
		content, err := fs.ReadFile(official, path.Join(name, gitops.StackFile))
		if err != nil {
			continue // nom inconnu : déjà signalé par stacksSource
		}
		if _, err := gitops.CheckStack(name, gitops.StackFile, string(content), exists, providers); err != nil {
			r.fail("stack officielle "+name, err)
			continue
		}
		r.ok("stack officielle %s", name)
	}
}

// filesOf : les fichiers de fsys
func filesOf(fsys fs.FS) map[string]bool {
	exists := map[string]bool{}
	fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			exists[p] = true
		}
		return nil
	})
	return exists
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

// validateStacks vérifie chaque stack du dossier dir, comme au chargement ;
// contre les providers de la configuration s'il y en a
func validateStacks(r *report, dir string, providers *provider.Registry, withConfig bool) {
	r.section("Stacks " + dir)
	root := os.DirFS(dir)
	if _, err := fs.Stat(root, "."); err != nil {
		r.fail("lecture", err)
		return
	}
	exists := filesOf(root)
	switch {
	case providers == nil && withConfig:
		r.warn("configuration invalide : providers des stacks non vérifiés")
	case providers == nil:
		r.warn("sans -config : providers des stacks non vérifiés")
	}

	var dirs []string
	for f := range exists {
		d, base := path.Split(f)
		d = strings.TrimSuffix(d, "/")
		if (base == gitops.StackFile || base == gitops.LegacyStackFile) && d != "" && path.Dir(d) == "." &&
			!slices.Contains(dirs, d) {
			dirs = append(dirs, d)
		}
	}
	sort.Strings(dirs)
	if len(dirs) == 0 {
		r.fail("stacks", fmt.Errorf("aucun <nom>/%s dans %s", gitops.StackFile, dir))
		return
	}
	for _, d := range dirs {
		file := gitops.StackFile
		if !exists[path.Join(d, file)] {
			file = gitops.LegacyStackFile
		}
		content, err := fs.ReadFile(root, path.Join(d, file))
		if err != nil {
			r.fail(path.Join(d, file), err)
			continue
		}
		s, err := gitops.CheckStack(d, file, string(content), exists, providers)
		if err != nil {
			r.fail(filepath.Join(dir, d, file), err)
			continue
		}
		r.ok("%s : %s%s", s.ID, s.Name, paramsSummary(s))
		if s.Legacy {
			r.warn("%s : ancien format (template.yaml, kind GoldenPath), renommer en stack.yaml avec kind Stack", filepath.Join(dir, d))
		}
	}
}

// paramsSummary : les paramètres d'une stack, pour le rapport
func paramsSummary(s gitops.Stack) string {
	if len(s.Parameters) == 0 {
		return ""
	}
	names := make([]string, len(s.Parameters))
	for i, p := range s.Parameters {
		names[i] = p.Name
	}
	return " (paramètres : " + strings.Join(names, ", ") + ")"
}

// placeholderSecret : la valeur de toute référence secret:// pour validate
// (assez longue pour les contrôles de longueur, ex. webhooks.secret, et une
// URL valide pour ceux qui en attendent une, ex. webhook_url)
const placeholderSecret = "https://secret-non-lu.invalid/symphony-validate"

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
