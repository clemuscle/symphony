# ── Stage 1 : build du frontend ───────────────────────────────────────────────
FROM node:22-alpine AS frontend
WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm ci --quiet
COPY frontend/ .
# outDir est '../internal/web/static' dans vite.config.js
RUN npm run build

# ── Stage 2 : build du binaire Go ─────────────────────────────────────────────
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Injection du frontend compilé (écrase le .gitkeep)
COPY --from=frontend /app/internal/web/static/ ./internal/web/static/
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o symphony ./cmd/symphony

# ── Stage 3 : image runtime minimale ──────────────────────────────────────────
FROM scratch
COPY --from=builder /app/symphony /symphony
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
EXPOSE 8080
ENTRYPOINT ["/symphony"]
