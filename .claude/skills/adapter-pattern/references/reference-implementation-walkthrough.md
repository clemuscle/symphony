# Méthode de lecture d'une implémentation de référence

Quel que soit le projet et le langage, la démarche pour comprendre le
pattern d'adaptateur déjà établi est la même :

1. Trouve une implémentation déjà écrite et fonctionnelle d'une des
   interfaces du contrat — c'est ta référence de style concrète.
2. Identifie comment elle gère : la configuration de connexion, le point
   d'entrée centralisé pour l'effet de bord principal, le mapping
   externe→interne, la gestion d'erreur.
3. Repère ce qui est un bon pattern à reproduire vs une lacune
   accidentelle à ne pas copier (voir
   `known-gaps-methodology.md`) — une implémentation de référence n'est
   jamais une référence de perfection, seulement de style.

L'exemple ci-dessous applique cette méthode à Symphony (Go), pour
illustrer concrètement chaque étape.

---

# Exemple appliqué — internal/providers/scm/gitlab/gitlab.go (Symphony, Go)

Ce fichier est la référence de style pour tout nouveau driver Symphony.
Lis-le en parallèle de ce commentaire.

## La struct et le constructeur

```go
type Provider struct {
	BaseURL string
	Token   string
	client  *http.Client
}

func New(baseURL, token string) *Provider {
	return &Provider{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}
```

Pourquoi c'est le bon pattern :
- `BaseURL`/`Token` exportés (utile pour debug/logs structurés côté
  appelant), `client` non exporté (détail d'implémentation).
- `TrimRight(baseURL, "/")` évite que `BaseURL + "/api/v4" + "/projects"`
  ne produise `.../api/v4//projects`. Petit détail, bug classique sans
  ça.
- Timeout *toujours* explicite. Un `http.Client{}` nu sans timeout peut
  bloquer indéfiniment sur un outil tiers qui ne répond plus — dans un
  système qui doit rester stateless et redémarrable proprement, un appel
  qui pend indéfiniment est un risque de ressource bloquée.

À reproduire à l'identique dans tout nouveau driver, en adaptant
uniquement les champs de connexion (ex: un driver Kubernetes aurait
plutôt un `Kubeconfig`/`Namespace` qu'un `Token`).

## Le point d'appel HTTP centralisé

```go
func (p *Provider) api(method, path string, body any) ([]byte, int, error) {
	var buf io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, p.BaseURL+"/api/v4"+path, buf)
	req.Header.Set("PRIVATE-TOKEN", p.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}
```

Pourquoi c'est le bon pattern :
- Toute méthode publique (`CreateRepo`, `PushFile`, `ListRepos`...) passe
  par ce point unique. L'auth, le header `Content-Type`, le préfixe
  `/api/v4` sont définis une seule fois — si l'auth change un jour
  (ex: passage à un header `Authorization: Bearer`), un seul endroit à
  modifier.
- Retourne `([]byte, int, error)` : le body brut, le status code, et
  l'erreur réseau. Chaque méthode publique reste responsable
  d'interpréter le status code et de parser le body selon son besoin
  propre — ce point central ne décide pas ce qui est un succès, il
  transporte juste l'information.

**Lacune à ne pas reproduire** : les erreurs de `json.Marshal(body)` et
de `io.ReadAll(resp.Body)` sont silencieusement ignorées (`_`). Dans un
nouveau driver, gérer ces erreurs explicitement plutôt que de copier ce
raccourci — voir `known-gaps-methodology.md`.

## Une méthode publique typique : CreateRepo

```go
func (p *Provider) CreateRepo(req providers.RepoRequest) (*providers.RepoResult, error) {
	payload := map[string]any{
		"name":                   req.Name,
		"description":            req.Description,
		"initialize_with_readme": true,
		"visibility":             "private",
	}
	if req.Namespace != "" {
		if nsID, err := p.resolveNamespace(req.Namespace); err == nil {
			payload["namespace_id"] = nsID
		}
	}

	data, status, err := p.api("POST", "/projects", payload)
	if err != nil {
		return nil, err
	}
	if status != 201 {
		return nil, fmt.Errorf("gitlab createRepo: %d — %s", status, string(data))
	}

	var project struct {
		ID                int    `json:"id"`
		PathWithNamespace string `json:"path_with_namespace"`
		HTTPURLToRepo     string `json:"http_url_to_repo"`
		SSHURLToRepo      string `json:"ssh_url_to_repo"`
		WebURL            string `json:"web_url"`
		DefaultBranch     string `json:"default_branch"`
	}
	json.Unmarshal(data, &project)

	return &providers.RepoResult{
		ID:            project.ID,
		Path:          project.PathWithNamespace,
		WebURL:        project.WebURL,
		HTTPCloneURL:  project.HTTPURLToRepo,
		SSHCloneURL:   project.SSHURLToRepo,
		DefaultBranch: project.DefaultBranch,
	}, nil
}
```

Décompose ce que fait cette méthode, dans l'ordre — c'est le squelette à
reproduire pour toute méthode d'écriture :

1. Construire le payload spécifique à l'API externe (`map[string]any`,
   format propre à GitLab — `initialize_with_readme`, `visibility`...).
2. Logique optionnelle annexe si besoin (`resolveNamespace` — note :
   l'erreur de résolution de namespace est avalée silencieusement avec
   `if nsID, err := ...; err == nil`, donc si le namespace n'existe pas,
   le repo est créé quand même sans le namespace demandé. À évaluer si
   c'est le comportement voulu ou un bug à corriger).
3. Appeler `p.api(...)`.
4. Vérifier le status code attendu *précisément* (`201`, pas juste
   `>= 200 && < 300`) — chaque opération GitLab a un code de succès
   documenté, s'y fier plutôt que d'accepter large.
5. Décoder dans une struct anonyme locale qui ne reprend QUE les champs
   utiles à Symphony (pas tout ce que l'API GitLab renvoie).
6. Mapper explicitement vers le type `providers.RepoResult`.

**Lacune à ne pas reproduire** : l'erreur de `json.Unmarshal(data,
&project)` est ignorée. Si l'API GitLab renvoie un body inattendu (panne
partielle, format changé), la méthode retournera un `RepoResult` vide
sans erreur — silencieusement faux. Un nouveau driver doit vérifier
cette erreur :

```go
if err := json.Unmarshal(data, &project); err != nil {
	return nil, fmt.Errorf("gitlab createRepo: parse réponse: %w", err)
}
```

## Une méthode de listing : ListRepos

```go
func (p *Provider) ListRepos() ([]providers.Repo, error) {
	data, _, err := p.api("GET", "/projects?membership=true&per_page=50", nil)
	...
}
```

Point d'attention pour un futur driver : `per_page=50` est codé en dur,
sans pagination au-delà de la première page. Si un projet GitLab dépasse
50 dépôts accessibles, `ListRepos` les tronque silencieusement. Pas
bloquant pour un MVP mais à signaler comme limite connue plutôt que
laisser découvrir le problème en usage réel — voir `known-gaps-methodology.md`.
