#!/usr/bin/env bash
# Symphony demo — infra uniquement.
#
# Ce script fait ce qui est purement mécanique et sans intérêt pédagogique :
#   1. Vérifie les prérequis (docker, docker compose, go, node/npm, RAM, ports)
#   2. Construit le frontend une fois si besoin (assets embarqués absents)
#   3. Démarre PostgreSQL + GitLab CE + GitLab Runner (docker compose)
#   4. Attend que GitLab CE soit prêt
#
# Tout le reste (créer le groupe/projet, les tokens, enregistrer le runner,
# configurer Symphony) se fait à la main en suivant DEMO.md — c'est
# volontairement pas automatisé ici.
#
# Appel : make demo-up  (ou ./scripts/demo-up.sh depuis la racine du repo)

set -euo pipefail

COMPOSE="docker compose -f docker-compose.demo.yml --project-name symphony"
GITLAB_URL="http://localhost:8929"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
info()  { echo -e "${BLUE}[demo-up]${NC} $*"; }
ok()    { echo -e "${GREEN}[demo-up]${NC} ✓ $*"; }
warn()  { echo -e "${YELLOW}[demo-up]${NC} ⚠ $*"; }
fail()  { echo -e "${RED}[demo-up]${NC} ✗ $*" >&2; exit 1; }

# ── 1. Prérequis ─────────────────────────────────────────────────────────────

info "Vérification des prérequis…"

command -v docker >/dev/null || fail "docker est requis : https://docs.docker.com/engine/install/"
docker compose version >/dev/null 2>&1 || fail "docker compose (v2, plugin) est requis — 'docker compose version' doit fonctionner"
command -v go >/dev/null || fail "go est requis pour lancer Symphony (make demo-start)"
command -v node >/dev/null || fail "node est requis pour construire le frontend"
command -v npm >/dev/null || fail "npm est requis pour construire le frontend"
ok "docker, docker compose, go, node, npm présents"

if command -v free >/dev/null 2>&1; then
  AVAIL_MB=$(free -m | awk '/^Mem:/ {print $7}')
  if [ -n "$AVAIL_MB" ] && [ "$AVAIL_MB" -lt 4000 ]; then
    warn "RAM disponible : ${AVAIL_MB} Mo — GitLab CE est gourmand (~2.5 Go), 4 Go+ disponibles recommandés. Ça peut fonctionner mais lentement."
  else
    ok "RAM disponible : ${AVAIL_MB} Mo"
  fi
else
  warn "Impossible de vérifier la RAM disponible sur cet OS — GitLab CE recommande ~2.5 Go+ libres"
fi

PORTS="8929 2224 5050 5432 8090"
BUSY=""
for p in $PORTS; do
  if command -v ss >/dev/null 2>&1 && ss -ltn 2>/dev/null | awk '{print $4}' | grep -q ":${p}\$"; then
    BUSY="$BUSY $p"
  fi
done
if [ -n "$BUSY" ]; then
  warn "Port(s) déjà occupé(s) :$BUSY — GitLab/Postgres/Symphony pourraient déjà tourner (relancer la démo ?) ou entrer en conflit avec un autre service"
else
  ok "Ports libres (8929, 2224, 5050, 5432, 8090)"
fi

# ── 2. Frontend ───────────────────────────────────────────────────────────────

if [ ! -d internal/web/static/assets ]; then
  info "Assets frontend absents — build initial (cd frontend && npm install && npm run build)…"
  (cd frontend && npm install && npm run build)
  ok "Frontend construit"
else
  ok "Assets frontend déjà présents (internal/web/static/assets)"
fi

# ── 3. Infra ──────────────────────────────────────────────────────────────────

info "Démarrage de PostgreSQL + GitLab CE + GitLab Runner…"
$COMPOSE up -d

# ── 4. Attente GitLab ─────────────────────────────────────────────────────────

info "Attente de GitLab CE (peut prendre 3-5 min au premier démarrage)…"
MAX=60; i=0
until curl -sf "$GITLAB_URL/users/sign_in" >/dev/null 2>&1; do
  i=$((i+1))
  [ $i -ge $MAX ] && fail "GitLab n'a pas démarré après $((MAX*10))s. Vérifiez : $COMPOSE logs -f gitlab"
  printf "."
  sleep 10
done
echo ""
ok "GitLab est prêt ($GITLAB_URL)"

echo ""
echo -e "${GREEN}╔═══════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║              Infra démo prête                         ║${NC}"
echo -e "${GREEN}╚═══════════════════════════════════════════════════════╝${NC}"
echo ""
echo "  Suite : ouvrir DEMO.md et suivre le parcours guidé"
echo "  (connexion GitLab, tokens, wizard Symphony, premier déploiement)."
echo ""
