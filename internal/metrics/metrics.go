// Package metrics expose les métriques Prometheus de Symphony sur /metrics :
// API HTTP, appels aux providers, rechargements depuis Git.
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry propre à Symphony (pas le registre global) : seules ces métriques,
// plus celles du runtime Go et du processus
var Registry = prometheus.NewRegistry()

var (
	httpRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "symphony_http_requests_total",
		Help: "Requêtes HTTP reçues, par méthode, route (motif chi) et code.",
	}, []string{"method", "route", "status"})

	httpDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "symphony_http_request_duration_seconds",
		Help:    "Durée de traitement des requêtes HTTP.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route"})

	providerCalls = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "symphony_provider_calls_total",
		Help: "Appels aux providers, par type, id, méthode et résultat (ok, error).",
	}, []string{"kind", "provider", "method", "result"})

	providerDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "symphony_provider_call_duration_seconds",
		Help:    "Durée des appels aux providers (API GitLab, GitHub, Docker…).",
		Buckets: []float64{.01, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60},
	}, []string{"kind", "provider", "method"})

	reloads = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "symphony_reloads_total",
		Help: "Rechargements depuis Git, par source (catalogue, templates, config) et résultat (ok, error, refused).",
	}, []string{"source", "result"})

	lastReload = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "symphony_last_successful_reload_timestamp_seconds",
		Help: "Horodatage du dernier rechargement réussi, par source.",
	}, []string{"source"})

	// Services du catalogue et golden paths chargés
	CatalogueServices = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "symphony_catalogue_services",
		Help: "Services chargés dans le catalogue.",
	})
	GoldenPaths = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "symphony_golden_paths",
		Help: "Golden paths chargés (valides).",
	})
)

func init() {
	Registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		httpRequests, httpDuration,
		providerCalls, providerDuration,
		reloads, lastReload,
		CatalogueServices, GoldenPaths,
	)
}

// Handler sert /metrics
func Handler() http.Handler {
	return promhttp.HandlerFor(Registry, promhttp.HandlerOpts{})
}

// Middleware mesure les requêtes HTTP. La route est le motif chi
// (/api/v1/projects/{name}), pas le chemin : le nombre de séries reste borné.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)

		route := "inconnue"
		if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
			route = rc.RoutePattern()
		}
		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}
		httpRequests.WithLabelValues(r.Method, route, strconv.Itoa(status)).Inc()
		httpDuration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
	})
}

// ObserveProviderCall mesure un appel de provider
func ObserveProviderCall(kind, provider, method string, start time.Time, err error) {
	result := "ok"
	if err != nil {
		result = "error"
	}
	providerCalls.WithLabelValues(kind, provider, method, result).Inc()
	providerDuration.WithLabelValues(kind, provider, method).Observe(time.Since(start).Seconds())
}

// Reload compte un rechargement : result vaut ok, error (source illisible) ou
// refused (contenu invalide)
func Reload(source, result string) {
	reloads.WithLabelValues(source, result).Inc()
	if result == "ok" {
		lastReload.WithLabelValues(source).SetToCurrentTime()
	}
}
