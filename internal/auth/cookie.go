package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Codec signe des valeurs de cookie (HMAC-SHA256) : le contenu est lisible
// mais infalsifiable. Il ne contient que l'identité et les groupes, jamais
// de jeton ; le rôle est recalculé à chaque requête depuis la configuration.
type Codec struct{ key []byte }

// NewCodec : secret d'au moins 32 octets. Vide : secret aléatoire, les
// sessions ne survivent alors pas à un redémarrage.
func NewCodec(secret string) (*Codec, bool, error) {
	if secret == "" {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, false, err
		}
		return &Codec{key: key}, false, nil
	}
	if len(secret) < 32 {
		return nil, false, errors.New("secret de session trop court (32 caractères minimum)")
	}
	return &Codec{key: []byte(secret)}, true, nil
}

var errInvalidCookie = errors.New("cookie invalide")

// Encode sérialise v avec une date d'expiration
func (c *Codec) Encode(v any, ttl time.Duration) (string, error) {
	payload, err := json.Marshal(struct {
		Exp  int64 `json:"exp"`
		Data any   `json:"data"`
	}{time.Now().Add(ttl).Unix(), v})
	if err != nil {
		return "", err
	}
	p := base64.RawURLEncoding.EncodeToString(payload)
	return p + "." + base64.RawURLEncoding.EncodeToString(c.mac(p)), nil
}

// Decode vérifie la signature et l'expiration puis remplit v
func (c *Codec) Decode(value string, v any) error {
	p, sig, ok := strings.Cut(value, ".")
	if !ok {
		return errInvalidCookie
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, c.mac(p)) {
		return errInvalidCookie
	}
	payload, err := base64.RawURLEncoding.DecodeString(p)
	if err != nil {
		return errInvalidCookie
	}
	var env struct {
		Exp  int64           `json:"exp"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return errInvalidCookie
	}
	if time.Now().Unix() > env.Exp {
		return errors.New("cookie expiré")
	}
	return json.Unmarshal(env.Data, v)
}

func (c *Codec) mac(s string) []byte {
	h := hmac.New(sha256.New, c.key)
	h.Write([]byte(s))
	return h.Sum(nil)
}

func randomString() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
