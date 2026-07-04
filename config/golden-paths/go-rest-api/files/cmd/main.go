package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "service": "{{.ServiceName}}"})
	})

	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("# HELP http_requests_total Total HTTP requests\nhttp_requests_total 0\n"))
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		slog.Info("request", "method", r.Method, "path", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"service": "{{.ServiceName}}", "status": "running"})
	})

	srv := &http.Server{
		Addr:         ":{{.Port}}",
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	slog.Info("starting", "addr", ":{{.Port}}")
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("failed", "error", err)
		os.Exit(1)
	}
}
