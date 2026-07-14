BIN     := symphony
COMPOSE := docker compose -f docker-compose.demo.yml --project-name symphony

.PHONY: build run dev clean demo-up demo-start demo-down

## build : compile le frontend puis le binaire Go (produit un binaire autonome)
build:
	cd frontend && npm run build
	go build -o $(BIN) ./cmd/symphony

## run : démarre le backend (SYMPHONY_DEV_MODE=1 si pas d'OIDC)
run:
	go run ./cmd/symphony

## dev : frontend Vite (dev server) + backend en parallèle
dev:
	@trap 'kill 0' SIGINT; \
	(cd frontend && npm run dev) & \
	go run ./cmd/symphony & \
	wait

## clean : supprime le binaire et le dist embarqué
clean:
	rm -f $(BIN)
	rm -rf internal/web/static/*

# ── Démo ──────────────────────────────────────────────────────────────────────
# Parcours complet : make demo-up, puis suivre DEMO.md (connexion GitLab,
# création des tokens, wizard Symphony) jusqu'à make demo-start.

## demo-up : vérifie les prérequis, construit le frontend si besoin, démarre
##           PostgreSQL + GitLab CE + GitLab Runner, attend que GitLab soit prêt
demo-up:
	@./scripts/demo-up.sh

## demo-start : démarre Symphony (config via config/integrations.yaml + .env,
##              voir DEMO.md — le wizard providers s'ouvre au premier lancement)
demo-start:
	@[ -f .env ] || { echo "Lancer 'cp .env.demo.example .env' d'abord (voir DEMO.md)"; exit 1; }
	@go run ./cmd/symphony

## demo-down : arrête et supprime tous les volumes de démo (destructif)
demo-down:
	$(COMPOSE) down -v
