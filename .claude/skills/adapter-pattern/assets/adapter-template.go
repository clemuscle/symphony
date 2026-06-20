// Squelette de départ pour un nouveau driver Symphony.
// Copier dans internal/providers/<categorie>/<outil>/<outil>.go,
// renommer le package, et implémenter une des 4 interfaces de
// internal/providers/interfaces.go.
//
// Ce squelette corrige les lacunes connues du driver GitLab de référence
// (voir references/known-gaps-methodology.md) : erreurs JSON vérifiées explicitement.

package outil // TODO: renommer (ex: harbor, jenkins, k8s)

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yourorg/symphony/internal/providers"
)

// Provider implémente <NomInterface> pour <NomOutil>.
// TODO: ajouter "var _ providers.XxxProvider = (*Provider)(nil)" pour
// forcer la vérification de conformité à l'interface au moment de la
// compilation.
type Provider struct {
	BaseURL string
	Token   string // TODO: adapter au mode d'auth réel de l'outil cible
	client  *http.Client
}

func New(baseURL, token string) *Provider {
	return &Provider{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

// api centralise tout appel HTTP vers l'outil tiers.
// TODO: adapter le préfixe de chemin (ex: "/api/v4" pour GitLab) et le
// header d'auth (ex: "PRIVATE-TOKEN" vs "Authorization: Bearer ...")
// au format réel de l'outil cible.
func (p *Provider) api(method, path string, body any) ([]byte, int, error) {
	var buf io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("encode payload: %w", err)
		}
		buf = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, p.BaseURL+path, buf)
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.Token) // TODO: adapter
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("appel réseau: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("lecture réponse: %w", err)
	}
	return data, resp.StatusCode, nil
}

// Exemple de méthode publique — à dupliquer/adapter pour chaque méthode
// de l'interface ciblée.
//
// func (p *Provider) CreateRepo(req providers.RepoRequest) (*providers.RepoResult, error) {
// 	payload := map[string]any{
// 		"name": req.Name,
// 		// TODO: champs spécifiques à l'outil cible
// 	}
//
// 	data, status, err := p.api("POST", "/chemin/api", payload)
// 	if err != nil {
// 		return nil, err
// 	}
// 	if status != 201 { // TODO: vérifier le code de succès réel de l'API
// 		return nil, fmt.Errorf("outil createRepo: %d — %s", status, string(data))
// 	}
//
// 	var resultatExterne struct {
// 		// TODO: uniquement les champs dont Symphony a besoin
// 	}
// 	if err := json.Unmarshal(data, &resultatExterne); err != nil {
// 		return nil, fmt.Errorf("outil createRepo: parse réponse: %w", err)
// 	}
//
// 	return &providers.RepoResult{
// 		// TODO: mapping explicite
// 	}, nil
// }
