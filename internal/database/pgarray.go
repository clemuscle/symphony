package database

import (
	"fmt"
	"strings"
)

// pgArrayScan lit une colonne text[] retournée par le driver pgx stdlib.
// L'insertion d'un []string Go fonctionne nativement (le driver l'encode
// tout seul en paramètre), mais la lecture ne passe pas par le même
// chemin : database/sql reçoit le littéral tableau Postgres brut
// ("{a,b,c}") et ne sait pas le convertir vers *[]string sans ce
// scanner — vérifié empiriquement, pas une supposition.
type pgArrayScanner struct{ dst *[]string }

func pgArrayScan(dst *[]string) *pgArrayScanner {
	return &pgArrayScanner{dst: dst}
}

func (s *pgArrayScanner) Scan(src any) error {
	if src == nil {
		*s.dst = []string{}
		return nil
	}
	switch v := src.(type) {
	case string:
		*s.dst = parsePGArray(v)
		return nil
	case []byte:
		*s.dst = parsePGArray(string(v))
		return nil
	default:
		return fmt.Errorf("pgArrayScan: type inattendu %T", src)
	}
}

// parsePGArray décode un littéral tableau Postgres simple ("{a,b,c}").
// Les tags sont des identifiants courts sans virgule ni guillemet
// attendus en pratique — pas un parseur d'échappement complet.
func parsePGArray(raw string) []string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "{")
	raw = strings.TrimSuffix(raw, "}")
	if raw == "" {
		return []string{}
	}
	parts := strings.Split(raw, ",")
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = strings.Trim(strings.TrimSpace(p), `"`)
	}
	return out
}
