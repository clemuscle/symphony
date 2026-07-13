BIN     := symphony
COMPOSE := docker compose -f docker-compose.demo.yml --project-name symphony

.PHONY: build run dev clean demo-up demo-init demo-down

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

## demo-up : démarre PostgreSQL + GitLab CE + GitLab Runner (attend la santé de GitLab)
demo-up:
	$(COMPOSE) up -d
	@echo "GitLab CE démarre (~3-5 min). Suivre avec : $(COMPOSE) logs -f gitlab"
	@echo "Quand GitLab est prêt, lancer : make demo-init"

## demo-init : initialise GitLab (PAT, groupe, runner) et affiche la config Symphony
demo-init:
	@./scripts/demo-init.sh

## demo-start : démarre Symphony avec la config générée par demo-init
demo-start:
	@[ -f .env.demo ] || { echo "Lancer 'make demo-init' d'abord"; exit 1; }
	@set -a && . ./.env.demo && set +a && go run ./cmd/symphony

## demo-down : arrête et supprime tous les volumes de démo (destructif)
demo-down:
	$(COMPOSE) down -v
