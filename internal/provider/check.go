package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Les méthodes Check des providers vérifient qu'ils répondent et que leurs
// identifiants sont acceptés, sans rien modifier (page Administration). Elles
// renvoient un détail lisible quand tout va bien (« connecté en tant que
// root »), une erreur qui dit quoi corriger sinon.

// CheckStatus traduit le statut HTTP d'une vérification en erreur : un jeton
// refusé ou sans droits se dit autrement qu'un service en panne
func CheckStatus(service string, status int, body []byte) error {
	switch {
	case status >= 200 && status < 300:
		return nil
	case status == http.StatusUnauthorized:
		return fmt.Errorf("%s : jeton refusé (401), expiré ou invalide", service)
	case status == http.StatusForbidden:
		return fmt.Errorf("%s : droits insuffisants (403)", service)
	}
	msg := strings.TrimSpace(string(body))
	if len(msg) > 200 {
		msg = msg[:200] + "…"
	}
	if msg == "" {
		return fmt.Errorf("%s : status %d", service, status)
	}
	return fmt.Errorf("%s : status %d — %s", service, status, msg)
}

// CheckOIDC lit la découverte OIDC (DiscoveryURL, sinon Issuer) et vérifie
// qu'elle annonce l'issuer attendu : sinon la connexion échouerait à la
// vérification des jetons
func CheckOIDC(client *http.Client, s OIDCSettings) (string, error) {
	base := s.Issuer
	if s.DiscoveryURL != "" {
		base = s.DiscoveryURL
	}
	wellKnown := strings.TrimRight(base, "/") + "/.well-known/openid-configuration"
	resp, err := client.Get(wellKnown)
	if err != nil {
		return "", fmt.Errorf("découverte OIDC %s : %w", wellKnown, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err := CheckStatus("découverte OIDC "+wellKnown, resp.StatusCode, body); err != nil {
		return "", err
	}
	var doc struct {
		Issuer string `json:"issuer"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", fmt.Errorf("découverte OIDC %s : réponse illisible", wellKnown)
	}
	if s.DiscoveryURL == "" && strings.TrimRight(doc.Issuer, "/") != strings.TrimRight(s.Issuer, "/") {
		return "", fmt.Errorf("découverte OIDC : issuer annoncé %q, configuré %q", doc.Issuer, s.Issuer)
	}
	return "issuer " + s.Issuer, nil
}
