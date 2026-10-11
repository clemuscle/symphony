// Package web sert le front Vue compilé, embarqué dans le binaire.
// Le build du front (cd frontend && npm run build) écrit dans dist/ ;
// sans lui, le binaire se compile quand même et / renvoie une page d'aide.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

// Handler sert les fichiers du front. Une route inconnue sans extension
// renvoie index.html : le routeur Vue (mode history) la résout côté client.
func Handler() http.Handler {
	files, _ := fs.Sub(dist, "dist")
	return handler(files)
}

func handler(files fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(files))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if name == "index.html" {
			serveIndex(w, files)
			return
		}
		if _, err := fs.Stat(files, name); err != nil {
			if path.Ext(name) != "" {
				http.NotFound(w, r) // asset absent : pas de index.html à la place
				return
			}
			serveIndex(w, files)
			return
		}
		// Les assets de Vite ont un hash dans leur nom : cache long
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fileServer.ServeHTTP(w, r)
	})
}

// serveIndex sert index.html sans cache, pour qu'un nouveau déploiement soit
// vu tout de suite
func serveIndex(w http.ResponseWriter, files fs.FS) {
	index, err := fs.ReadFile(files, "index.html")
	if err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Front non compilé : cd frontend && npm install && npm run build, puis recompiler Symphony.\n"))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(index)
}
