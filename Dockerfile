# Front : compilé dans internal/web/dist, embarqué ensuite dans le binaire Go
FROM node:24-alpine AS front
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# Backend
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=front /app/internal/web/dist ./internal/web/dist
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o symphony ./cmd/symphony

# Runtime — image ultra-légère : un binaire, front compris
FROM scratch
COPY --from=builder /app/symphony /symphony
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

EXPOSE 8080
ENTRYPOINT ["/symphony"]
