package gitops

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/clemuscle/symphony/internal/catalogue"
	"github.com/clemuscle/symphony/internal/provider"
	"gopkg.in/yaml.v3"
)

// StackFile est le nom du fichier qui décrit une stack dans son dossier ;
// LegacyStackFile l'ancien nom (golden path, kind GoldenPath), encore lu
const (
	StackFile       = "stack.yaml"
	LegacyStackFile = "template.yaml"
)

// Stack : un modèle de projet (squelette, CI, paramètres, manifests
// Kubernetes) lu dans le repo de stacks du client ou dans le catalogue
// officiel. Ses chemins (Dir, Skeleton, CITemplates) sont relatifs à sa
// source : le repo Repo sur la branche Branch, ou le catalogue officiel.
type Stack struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Language    string `json:"language"`
	Type        string `json:"type"`
	// Providers résolus au chargement : vides dans stack.yaml, ce sont ceux
	// par défaut de la configuration
	SCMProvider string `json:"scm_provider"`
	CIProvider  string `json:"ci_provider"`
	RegProvider string `json:"registry_provider"`
	RTProvider  string `json:"runtime_provider"`
	// Official : stack du catalogue officiel (embarqué dans Symphony)
	Official bool   `json:"official"`
	Repo     string `json:"repo,omitempty"`
	Branch   string `json:"branch,omitempty"`
	Dir      string `json:"-"`
	Skeleton string `json:"-"`
	// CITemplate : la CI du repo créé, pour la plateforme du CI de la stack
	CITemplate string `json:"ci_template"`
	// ciTemplates : plateforme (gitlab, github) → template ; "" pour un
	// ci_template unique
	ciTemplates map[string]string
	// Environments : runtime par environnement qui remplace celui de la
	// configuration
	Environments map[string]string `json:"environments,omitempty"`
	// Parameters : le formulaire de la stack ; Schema : le même en JSON Schema
	Parameters []Parameter     `json:"parameters"`
	Schema     json.RawMessage `json:"schema"`
	params     *paramSchema
	// Files : fichiers du squelette créés sous condition
	Files []FileRule `json:"files,omitempty"`
	// Kubernetes : manifests fournis par la stack (chemins dans le repo créé)
	Kubernetes *Kubernetes `json:"kubernetes,omitempty"`
	// Infra : l'infra du service (manifests d'opérateurs), appliquée avec lui
	Infra *catalogue.Infra `json:"infra,omitempty"`
	// Legacy : décrite par template.yaml (kind GoldenPath), à renommer
	Legacy bool `json:"-"`
}

// FileRule : le fichier ou dossier Path du squelette n'est créé que si les
// paramètres remplissent When
type FileRule struct {
	Path string `yaml:"path" json:"path"`
	When When   `yaml:"when" json:"when"`
}

// Kubernetes : chart Helm ou base Kustomize du service, dans son repo. Le
// runtime argocd s'en sert à la place des manifests qu'il génère.
type Kubernetes struct {
	Helm      string `yaml:"helm,omitempty"      json:"helm,omitempty"`
	Kustomize string `yaml:"kustomize,omitempty" json:"kustomize,omitempty"`
}

// stackDoc est le format de <dossier>/stack.yaml
type stackDoc struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name        string `yaml:"name"`
		Title       string `yaml:"title"`
		Description string `yaml:"description"`
		Icon        string `yaml:"icon"`
	} `yaml:"metadata"`
	Spec struct {
		Language  string `yaml:"language"`
		Type      string `yaml:"type"`
		Providers struct {
			SCM      string `yaml:"scm"`
			CI       string `yaml:"ci"`
			Registry string `yaml:"registry"`
			Runtime  string `yaml:"runtime"`
		} `yaml:"providers"`
		Skeleton string `yaml:"skeleton"`
		// Une CI pour la plateforme du CI de la stack (ci_template), ou une
		// par plateforme (ci_templates) pour une stack qui suit le SCM par
		// défaut, quel qu'il soit
		CITemplate  string            `yaml:"ci_template"`
		CITemplates map[string]string `yaml:"ci_templates"`
		// Runtime par environnement, à la place de celui de la configuration
		// (ex. staging: argocd pour une API sur Kubernetes dès staging)
		Environments map[string]string `yaml:"environments"`
		Parameters   []Parameter       `yaml:"parameters"`
		Files        []FileRule        `yaml:"files"`
		Kubernetes   *Kubernetes       `yaml:"kubernetes"`
		Infra        *catalogue.Infra  `yaml:"infra"`
	} `yaml:"spec"`
}

// Plateformes des CI (provider.SCMProvider.Platform)
var ciPlatforms = []string{"gitlab", "github"}

var stackNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// parseStack lit le stack.yaml (ou template.yaml) du dossier dir. Le nom de
// la stack doit être celui du dossier, ce qui garantit des id uniques.
func parseStack(dir, file, content string) (Stack, error) {
	var doc stackDoc
	dec := yaml.NewDecoder(strings.NewReader(content))
	dec.KnownFields(true) // une faute de frappe dans une clé est une erreur, pas un champ ignoré
	if err := dec.Decode(&doc); err != nil {
		return Stack{}, fmt.Errorf("parse YAML: %w", err)
	}
	legacy := file == LegacyStackFile
	switch {
	case legacy && doc.Kind != "GoldenPath":
		return Stack{}, fmt.Errorf("kind %q, attendu GoldenPath dans %s (ou renommer en %s, kind Stack)", doc.Kind, LegacyStackFile, StackFile)
	case !legacy && doc.Kind != "Stack":
		return Stack{}, fmt.Errorf("kind %q, attendu Stack", doc.Kind)
	}
	name := doc.Metadata.Name
	if !stackNameRe.MatchString(name) {
		return Stack{}, fmt.Errorf("metadata.name %q invalide (minuscules, chiffres, tirets)", name)
	}
	if name != path.Base(dir) {
		return Stack{}, fmt.Errorf("metadata.name %q différent du dossier %q", name, path.Base(dir))
	}

	ciTemplates := map[string]string{}
	switch {
	case doc.Spec.CITemplate != "" && len(doc.Spec.CITemplates) > 0:
		return Stack{}, fmt.Errorf("spec.ci_template et spec.ci_templates : l'un ou l'autre")
	case doc.Spec.CITemplate != "":
		ciTemplates[""] = doc.Spec.CITemplate
	case len(doc.Spec.CITemplates) > 0:
		for platform, file := range doc.Spec.CITemplates {
			if !contains(ciPlatforms, platform) {
				return Stack{}, fmt.Errorf("spec.ci_templates.%s : plateforme inconnue (%s)", platform, strings.Join(ciPlatforms, ", "))
			}
			ciTemplates[platform] = file
		}
	default:
		return Stack{}, fmt.Errorf("spec.ci_template (ou ci_templates) requis")
	}
	for platform, file := range ciTemplates {
		p, err := stackPath(dir, file)
		if err != nil {
			return Stack{}, fmt.Errorf("spec.ci_template%s: %w", platformSuffix(platform), err)
		}
		ciTemplates[platform] = p
	}

	skeleton := doc.Spec.Skeleton
	if skeleton == "" {
		skeleton = "skeleton"
	}
	skeletonPath, err := stackPath(dir, skeleton)
	if err != nil {
		return Stack{}, fmt.Errorf("spec.skeleton: %w", err)
	}

	params, err := compileParameters(doc.Spec.Parameters)
	if err != nil {
		return Stack{}, fmt.Errorf("spec.parameters: %w", err)
	}
	for i, rule := range doc.Spec.Files {
		if err := checkRepoPath(rule.Path); err != nil {
			return Stack{}, fmt.Errorf("spec.files[%d].path: %w", i, err)
		}
		if len(rule.When) == 0 {
			return Stack{}, fmt.Errorf("spec.files[%d] : when requis (sinon le fichier est toujours créé)", i)
		}
		if err := params.checkWhen(rule.When, len(doc.Spec.Parameters)); err != nil {
			return Stack{}, fmt.Errorf("spec.files[%d].when: %w", i, err)
		}
	}
	if k := doc.Spec.Kubernetes; k != nil {
		switch {
		case (k.Helm == "") == (k.Kustomize == ""):
			return Stack{}, fmt.Errorf("spec.kubernetes : helm ou kustomize, l'un des deux")
		case k.Helm != "":
			err = checkRepoPath(k.Helm)
		default:
			err = checkRepoPath(k.Kustomize)
		}
		if err != nil {
			return Stack{}, fmt.Errorf("spec.kubernetes: %w", err)
		}
		k.Helm, k.Kustomize = strings.Trim(k.Helm, "/"), strings.Trim(k.Kustomize, "/")
	}
	if in := doc.Spec.Infra; in != nil {
		if err := CheckInfra(in); err != nil {
			return Stack{}, fmt.Errorf("spec.infra: %w", err)
		}
		if k := doc.Spec.Kubernetes; k != nil && k.Helm != "" {
			return Stack{}, fmt.Errorf("spec.infra : avec un chart Helm, l'infra est dans le chart")
		}
	}

	title := doc.Metadata.Title
	if title == "" {
		title = name
	}
	parameters := doc.Spec.Parameters
	if parameters == nil {
		parameters = []Parameter{}
	}
	return Stack{
		ID:           name,
		Name:         title,
		Description:  doc.Metadata.Description,
		Icon:         doc.Metadata.Icon,
		Language:     doc.Spec.Language,
		Type:         doc.Spec.Type,
		SCMProvider:  doc.Spec.Providers.SCM,
		CIProvider:   doc.Spec.Providers.CI,
		RegProvider:  doc.Spec.Providers.Registry,
		RTProvider:   doc.Spec.Providers.Runtime,
		Dir:          dir,
		Skeleton:     skeletonPath,
		ciTemplates:  ciTemplates,
		Environments: doc.Spec.Environments,
		Parameters:   parameters,
		Schema:       params.raw,
		params:       params,
		Files:        doc.Spec.Files,
		Kubernetes:   doc.Spec.Kubernetes,
		Infra:        doc.Spec.Infra,
		Legacy:       legacy,
	}, nil
}

func platformSuffix(platform string) string {
	if platform == "" {
		return ""
	}
	return "s." + platform
}

// stackPath résout un chemin relatif au dossier de la stack, sans permettre
// d'en sortir (une stack ne lit que ses propres fichiers)
func stackPath(dir, rel string) (string, error) {
	if path.IsAbs(rel) {
		return "", fmt.Errorf("chemin %q absolu, attendu relatif à la stack", rel)
	}
	p := path.Join(dir, rel)
	if p == dir || !strings.HasPrefix(p, dir+"/") {
		return "", fmt.Errorf("chemin %q hors du dossier de la stack", rel)
	}
	return p, nil
}

// checkRepoPath : un chemin relatif dans le repo créé, sans en sortir
func checkRepoPath(rel string) error {
	if rel == "" || path.IsAbs(rel) {
		return fmt.Errorf("chemin %q : relatif au repo créé, non vide", rel)
	}
	if c := path.Clean(rel); c == "." || c == ".." || strings.HasPrefix(c, "../") {
		return fmt.Errorf("chemin %q hors du repo créé", rel)
	}
	return nil
}

// ciTemplateFor : la CI de la stack pour la plateforme platform
func (s Stack) ciTemplateFor(platform string) (string, error) {
	if t, ok := s.ciTemplates[""]; ok {
		return t, nil
	}
	if t, ok := s.ciTemplates[platform]; ok {
		return t, nil
	}
	have := make([]string, 0, len(s.ciTemplates))
	for p := range s.ciTemplates {
		have = append(have, p)
	}
	sort.Strings(have)
	return "", fmt.Errorf("pas de CI pour la plateforme %s (ci_templates : %s)", platform, strings.Join(have, ", "))
}

// resolveProviders vérifie que les providers de la stack existent, que son
// CI tourne sur son SCM et qu'elle a une CI pour cette plateforme. Renvoie la
// stack avec les providers résolus (les vides désignent ceux par défaut ; le
// registry reste vide s'il n'y en a pas).
func resolveProviders(s Stack, providers *provider.Registry) (Stack, error) {
	scm, err := providers.SCM(s.SCMProvider)
	if err != nil {
		return s, fmt.Errorf("scm provider: %w", err)
	}
	ci, err := providers.CI(s.CIProvider)
	if err != nil {
		return s, fmt.Errorf("ci provider: %w", err)
	}
	if err := provider.CheckCompatible(scm, ci); err != nil {
		return s, err
	}
	if s.CITemplate, err = s.ciTemplateFor(scm.Platform()); err != nil {
		return s, err
	}
	s.SCMProvider, s.CIProvider = scm.ID(), ci.ID()
	if reg, err := providers.GetRegistry(s.RegProvider); err == nil {
		s.RegProvider = reg.ID()
	} else if s.RegProvider != "" {
		return s, fmt.Errorf("registry provider: %w", err)
	}
	rt, err := providers.Runtime(s.RTProvider)
	if err != nil {
		return s, fmt.Errorf("runtime provider: %w", err)
	}
	s.RTProvider = rt.ID()
	for env, id := range s.Environments {
		if _, err := providers.Runtime(id); err != nil {
			return s, fmt.Errorf("environments.%s: runtime provider: %w", env, err)
		}
	}
	return s, nil
}

// checkFiles vérifie que le squelette (non vide), chaque CI et les manifests
// Kubernetes existent
func checkFiles(s Stack, exists map[string]bool) error {
	for _, platform := range append([]string{""}, ciPlatforms...) {
		if t, ok := s.ciTemplates[platform]; ok && !exists[t] {
			return fmt.Errorf("ci_template %s introuvable", t)
		}
	}
	if !hasPrefix(exists, s.Skeleton+"/") {
		return fmt.Errorf("skeleton %s introuvable ou vide", s.Skeleton)
	}
	if k := s.Kubernetes; k != nil {
		dir, marker := k.Kustomize, "kustomization.yaml"
		if k.Helm != "" {
			dir, marker = k.Helm, "Chart.yaml"
		}
		want := path.Join(dir, marker)
		found := false
		for f := range exists {
			if strings.HasPrefix(f, s.Skeleton+"/") && RepoPath(s, f) == want {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("spec.kubernetes : %s introuvable dans le squelette", want)
		}
	}
	if in := s.Infra; in != nil {
		found := false
		for f := range exists {
			if strings.HasPrefix(f, s.Skeleton+"/") && strings.HasPrefix(RepoPath(s, f), in.Kubernetes+"/") {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("spec.infra : dossier %s introuvable ou vide dans le squelette", in.Kubernetes)
		}
	}
	return nil
}

func hasPrefix(exists map[string]bool, prefix string) bool {
	for f := range exists {
		if strings.HasPrefix(f, prefix) {
			return true
		}
	}
	return false
}

// CheckStack lit et vérifie la stack du dossier dir : format de son fichier
// (stack.yaml ou template.yaml), fichiers présents dans exists, et providers
// existants et compatibles si providers n'est pas nil (symphony validate sans
// configuration). La stack renvoyée a ses providers résolus.
func CheckStack(dir, file, content string, exists map[string]bool, providers *provider.Registry) (Stack, error) {
	s, err := parseStack(dir, file, content)
	if err == nil {
		err = checkFiles(s, exists)
	}
	if err == nil && providers != nil {
		s, err = resolveProviders(s, providers)
	}
	return s, err
}

// RuntimeFor : le runtime de la stack pour l'environnement env, sinon celui
// de la configuration (fallback)
func (s Stack) RuntimeFor(env, fallback string) string {
	if rt := s.Environments[env]; rt != "" {
		return rt
	}
	return fallback
}

// Resolve complète et valide les valeurs des paramètres saisies à la
// création d'un projet (défauts, valeurs verrouillées, conditions)
func (s Stack) Resolve(values map[string]any) (map[string]any, error) {
	if s.params == nil {
		return nil, fmt.Errorf("stack %s : paramètres non compilés", s.ID)
	}
	return s.params.resolve(values)
}

// Keep : le fichier p du squelette (chemin relatif au squelette) est-il créé
// avec ces valeurs de paramètres ?
func (s Stack) Keep(p string, values map[string]any) bool {
	for _, rule := range s.Files {
		r := strings.Trim(path.Clean(rule.Path), "/")
		if (p == r || strings.HasPrefix(p, r+"/")) && !rule.When.Match(values) {
			return false
		}
	}
	return true
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

var (
	envNameRe    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	secretNameRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
)

// CheckInfra vérifie l'infra d'une stack (ou d'une fiche) : un dossier du
// repo, et des variables lues dans les secrets que cette infra crée
func CheckInfra(in *catalogue.Infra) error {
	if err := checkRepoPath(in.Kubernetes); err != nil {
		return fmt.Errorf("kubernetes: %w", err)
	}
	in.Kubernetes = strings.Trim(path.Clean(in.Kubernetes), "/")
	for name, ref := range in.Env {
		if !envNameRe.MatchString(name) {
			return fmt.Errorf("env.%s : nom de variable invalide", name)
		}
		if !secretNameRe.MatchString(ref.Secret) || ref.Key == "" {
			return fmt.Errorf("env.%s : secret (nom DNS, sans le préfixe du déploiement) et key requis", name)
		}
	}
	return nil
}

// InfraKept : l'infra de la stack, si son dossier est dans les fichiers du
// repo créé (un paramètre peut l'omettre)
func (s Stack) InfraKept(params map[string]any) *catalogue.Infra {
	if s.Infra == nil || !s.Keep(s.Infra.Kubernetes, params) {
		return nil
	}
	in := *s.Infra
	return &in
}
