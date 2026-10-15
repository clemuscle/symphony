// Package vault : provider de secrets HashiCorp Vault (ou OpenBao, même API),
// moteur KV v2 ou v1. Authentification par jeton ou par AppRole.
//
// secret://vault/symphony/gitlab#token lit la clé token du secret
// symphony/gitlab du montage configuré (défaut : secret).
package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/clemuscle/symphony/internal/provider"
)

type Config struct {
	URL       string
	Namespace string // Vault Enterprise / HCP : en-tête X-Vault-Namespace
	Mount     string // montage du moteur KV, défaut secret
	KVVersion int    // 2 (défaut) ou 1

	// Authentification : Token, sinon AppRole (RoleID + SecretID)
	Token        string
	RoleID       string
	SecretID     string
	AppRoleMount string // défaut approle
}

type Provider struct {
	id     string
	cfg    Config
	client *http.Client

	mu      sync.Mutex
	token   string
	expires time.Time // zéro : n'expire pas (jeton fourni)
}

func New(id string, cfg Config) (*Provider, error) {
	if cfg.URL == "" {
		return nil, errors.New("vault: url requise")
	}
	if cfg.Token == "" && (cfg.RoleID == "" || cfg.SecretID == "") {
		return nil, errors.New("vault: un jeton (token) ou un AppRole (role_id et secret_id) requis")
	}
	cfg.URL = strings.TrimRight(cfg.URL, "/")
	if cfg.Mount == "" {
		cfg.Mount = "secret"
	}
	cfg.Mount = strings.Trim(cfg.Mount, "/")
	if cfg.KVVersion == 0 {
		cfg.KVVersion = 2
	}
	if cfg.KVVersion != 1 && cfg.KVVersion != 2 {
		return nil, fmt.Errorf("vault: kv_version %d inconnue (1 ou 2)", cfg.KVVersion)
	}
	if cfg.AppRoleMount == "" {
		cfg.AppRoleMount = "approle"
	}
	return &Provider{
		id:     id,
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
		token:  cfg.Token,
	}, nil
}

func (p *Provider) ID() string   { return p.id }
func (p *Provider) Type() string { return "vault" }

// Get lit le secret path. Sans key, le secret doit n'avoir qu'une clé.
func (p *Provider) Get(path, key string) (string, error) {
	data, err := p.read(path)
	if err != nil {
		return "", err
	}
	if key == "" {
		if len(data) != 1 {
			return "", fmt.Errorf("secret %s : %d clés, préciser laquelle (#%s)", path, len(data), strings.Join(keys(data), ", #"))
		}
		key = keys(data)[0]
	}
	v, ok := data[key]
	if !ok {
		return "", fmt.Errorf("clé %s du secret %s: %w", key, path, provider.ErrNotFound)
	}
	return stringValue(path, key, v)
}

func stringValue(path, key string, v any) (string, error) {
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("secret %s#%s : valeur non textuelle", path, key)
	}
	return s, nil
}

func keys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// read renvoie les données du secret ; un jeton AppRole expiré ou révoqué
// est renouvelé une fois
func (p *Provider) read(path string) (map[string]any, error) {
	apiPath := "/v1/" + p.cfg.Mount + "/" + path
	if p.cfg.KVVersion == 2 {
		apiPath = "/v1/" + p.cfg.Mount + "/data/" + path
	}
	for attempt := 0; ; attempt++ {
		token, err := p.currentToken(attempt > 0)
		if err != nil {
			return nil, err
		}
		body, status, err := p.do("GET", apiPath, token, nil)
		if err != nil {
			return nil, err
		}
		switch {
		case status == http.StatusOK:
			return decodeSecret(body, p.cfg.KVVersion)
		case status == http.StatusNotFound:
			return nil, fmt.Errorf("secret %s: %w", path, provider.ErrNotFound)
		case status == http.StatusForbidden && attempt == 0 && p.cfg.Token == "":
			continue // jeton AppRole périmé : nouvelle connexion
		default:
			return nil, fmt.Errorf("vault: lecture de %s : status %d — %s", path, status, vaultErrors(body))
		}
	}
}

func decodeSecret(body []byte, version int) (map[string]any, error) {
	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("vault: réponse illisible: %w", err)
	}
	if version == 1 {
		return resp.Data, nil
	}
	// KV v2 : {"data": {"data": {...}, "metadata": {...}}}
	inner, _ := resp.Data["data"].(map[string]any)
	if inner == nil {
		// Version supprimée (soft delete) : data vaut null
		return nil, provider.ErrNotFound
	}
	return inner, nil
}

// currentToken : le jeton fourni, ou celui de la dernière connexion AppRole
// tant qu'il n'expire pas (marge de 10 %)
func (p *Provider) currentToken(forceLogin bool) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cfg.Token != "" {
		return p.cfg.Token, nil
	}
	if !forceLogin && p.token != "" && (p.expires.IsZero() || time.Now().Before(p.expires)) {
		return p.token, nil
	}
	body, status, err := p.do("POST", "/v1/auth/"+p.cfg.AppRoleMount+"/login", "",
		map[string]string{"role_id": p.cfg.RoleID, "secret_id": p.cfg.SecretID})
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("vault: connexion AppRole : status %d — %s", status, vaultErrors(body))
	}
	var resp struct {
		Auth struct {
			ClientToken   string `json:"client_token"`
			LeaseDuration int    `json:"lease_duration"`
		} `json:"auth"`
	}
	if err := json.Unmarshal(body, &resp); err != nil || resp.Auth.ClientToken == "" {
		return "", errors.New("vault: connexion AppRole : pas de jeton dans la réponse")
	}
	p.token = resp.Auth.ClientToken
	p.expires = time.Time{}
	if d := resp.Auth.LeaseDuration; d > 0 {
		p.expires = time.Now().Add(time.Duration(d) * time.Second * 9 / 10)
	}
	return p.token, nil
}

func (p *Provider) do(method, path, token string, body any) ([]byte, int, error) {
	var buf io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, p.cfg.URL+path, buf)
	if err != nil {
		return nil, 0, err
	}
	if token != "" {
		req.Header.Set("X-Vault-Token", token)
	}
	if p.cfg.Namespace != "" {
		req.Header.Set("X-Vault-Namespace", p.cfg.Namespace)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("vault: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return data, resp.StatusCode, nil
}

// vaultErrors : les messages d'erreur de Vault ({"errors": [...]})
func vaultErrors(body []byte) string {
	var resp struct {
		Errors []string `json:"errors"`
	}
	if json.Unmarshal(body, &resp) == nil && len(resp.Errors) > 0 {
		return strings.Join(resp.Errors, "; ")
	}
	return strings.TrimSpace(string(body))
}
