package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5/middleware"
)

// validRequestID : identifiant fourni par l'appelant (proxy, script), gardé
// s'il est court et sans caractère spécial
var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// requestID donne à chaque requête un identifiant court (16 caractères
// hexadécimaux) : renvoyé dans X-Request-ID, cité dans les erreurs et les logs.
// Remplace middleware.RequestID de chi, dont l'identifiant contient le nom de
// la machine. Lu ensuite avec middleware.GetReqID.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if !validRequestID.MatchString(id) {
			b := make([]byte, 8)
			rand.Read(b)
			id = hex.EncodeToString(b)
		}
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), middleware.RequestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
