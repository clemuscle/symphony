BIN := symphony

.PHONY: build run dev clean

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
