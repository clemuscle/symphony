# {{.ServiceName}}

> {{.ServiceDescription}}

## Stack
- **Langage** : {{.Language}}
- **Type** : {{.Type}}  
- **Port** : {{.Port}}

## Démarrage rapide

```bash
# Lancer en local
docker build -t {{.ServiceName}} .
docker run -p {{.Port}}:{{.Port}} {{.ServiceName}}

# Healthcheck
curl http://localhost:{{.Port}}/healthz

# Métriques
curl http://localhost:{{.Port}}/metrics
```

## Endpoints
| Endpoint | Description |
|----------|-------------|
| GET / | Info service |
| GET /healthz | Healthcheck |
| GET /metrics | Métriques Prometheus |

## Créé avec [Symphony IDP](http://localhost:8080)
