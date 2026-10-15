// Package keycloak : provider d'identité Keycloak. La connexion est de
// l'OIDC (issuer et découverte déduits de l'URL et du realm) ; l'annuaire
// passe par l'API d'administration, avec un client de service (client
// credentials) qui a les rôles query-groups et view-users de realm-management.
package keycloak

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/clemuscle/symphony/internal/provider"
)

type Config struct {
	URL       string // Keycloak vu par Symphony (ex. http://keycloak:8080)
	PublicURL string // vu par le navigateur, défaut : URL
	Realm     string

	// Client de service de l'annuaire ; vide : pas d'annuaire
	DirectoryClientID     string
	DirectoryClientSecret string
}

// groupsTTL : durée pendant laquelle la liste des groupes est réutilisée
const groupsTTL = 30 * time.Second

type Provider struct {
	id       string
	cfg      Config
	settings provider.OIDCSettings
	client   *http.Client

	mu       sync.Mutex
	token    string
	expires  time.Time
	groups   []provider.Group
	groupsAt time.Time
}

// New : settings vient de la configuration commune aux providers identity
// (client_id, client_secret, redirect_url, groups_claim) ; issuer et
// découverte sont déduits de l'URL et du realm s'ils n'y sont pas.
func New(id string, settings provider.OIDCSettings, cfg Config) (*Provider, error) {
	if cfg.URL == "" || cfg.Realm == "" {
		return nil, errors.New("url et realm requis")
	}
	if settings.ClientID == "" {
		return nil, errors.New("client_id requis")
	}
	if cfg.DirectoryClientID != "" && cfg.DirectoryClientSecret == "" {
		return nil, errors.New("directory_client_secret requis avec directory_client_id")
	}
	cfg.URL = strings.TrimRight(cfg.URL, "/")
	cfg.PublicURL = strings.TrimRight(cfg.PublicURL, "/")
	if cfg.PublicURL == "" {
		cfg.PublicURL = cfg.URL
	}
	realm := "/realms/" + url.PathEscape(cfg.Realm)
	if settings.Issuer == "" {
		settings.Issuer = cfg.PublicURL + realm
	}
	if settings.DiscoveryURL == "" && cfg.URL != cfg.PublicURL {
		settings.DiscoveryURL = cfg.URL + realm
	}
	return &Provider{
		id:       id,
		cfg:      cfg,
		settings: settings,
		client:   &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (p *Provider) ID() string                  { return p.id }
func (p *Provider) Type() string                { return "keycloak" }
func (p *Provider) OIDC() provider.OIDCSettings { return p.settings }

// ListGroups : tous les groupes du realm, sous-groupes compris, triés par chemin
func (p *Provider) ListGroups() ([]provider.Group, error) {
	if p.cfg.DirectoryClientID == "" {
		return nil, provider.ErrUnsupported
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.groups != nil && time.Since(p.groupsAt) < groupsTTL {
		return p.groups, nil
	}
	token, err := p.adminToken()
	if err != nil {
		return nil, err
	}
	var tree []kcGroup
	if err := p.get(token, "/groups?briefRepresentation=true&max=1000", &tree); err != nil {
		return nil, err
	}
	var groups []provider.Group
	var walk func([]kcGroup) error
	walk = func(gs []kcGroup) error {
		for _, g := range gs {
			groups = append(groups, provider.Group{Name: g.Name, Path: g.Path})
			children := g.SubGroups
			// Keycloak 23+ ne détaille plus les sous-groupes : il les compte
			if len(children) == 0 && g.SubGroupCount > 0 {
				if err := p.get(token, "/groups/"+url.PathEscape(g.ID)+"/children?briefRepresentation=true&max=1000", &children); err != nil {
					return err
				}
			}
			if err := walk(children); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(tree); err != nil {
		return nil, err
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Path < groups[j].Path })
	if groups == nil {
		groups = []provider.Group{}
	}
	p.groups, p.groupsAt = groups, time.Now()
	return groups, nil
}

type kcGroup struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Path          string    `json:"path"`
	SubGroupCount int       `json:"subGroupCount"`
	SubGroups     []kcGroup `json:"subGroups"`
}

// adminToken : jeton du client de service, réutilisé jusqu'à son expiration
// (marge de 10 %). Appelé sous p.mu.
func (p *Provider) adminToken() (string, error) {
	if p.token != "" && time.Now().Before(p.expires) {
		return p.token, nil
	}
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {p.cfg.DirectoryClientID},
		"client_secret": {p.cfg.DirectoryClientSecret},
	}
	resp, err := p.client.PostForm(p.cfg.URL+"/realms/"+url.PathEscape(p.cfg.Realm)+"/protocol/openid-connect/token", form)
	if err != nil {
		return "", fmt.Errorf("keycloak: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("keycloak: jeton du client %s : status %d — %s", p.cfg.DirectoryClientID, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" {
		return "", errors.New("keycloak: pas de jeton dans la réponse")
	}
	p.token = tok.AccessToken
	p.expires = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second * 9 / 10)
	return p.token, nil
}

// get appelle l'API d'administration du realm
func (p *Provider) get(token, path string, v any) error {
	req, err := http.NewRequest("GET", p.cfg.URL+"/admin/realms/"+url.PathEscape(p.cfg.Realm)+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("keycloak: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode == http.StatusUnauthorized {
		p.token = "" // jeton révoqué : nouvelle demande au prochain appel
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("keycloak: GET %s : status %d — %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.Unmarshal(body, v)
}
