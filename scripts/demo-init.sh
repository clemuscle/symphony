#!/usr/bin/env bash
# Symphony demo — initialisation de GitLab CE
#
# Ce script :
#   1. Attend que GitLab CE soit prêt
#   2. Crée un Personal Access Token root
#   3. Crée le groupe symphony-demo + le projet infra (config_repo)
#   4. Crée deux utilisateurs de démo (lead + dev)
#   5. Enregistre le GitLab Runner
#   6. Affiche les valeurs à saisir dans le wizard Symphony
#
# Prérequis : docker compose v2, jq
# Appel : make demo-init  (ou ./scripts/demo-init.sh depuis la racine du repo)

set -euo pipefail

COMPOSE="docker compose -f docker-compose.demo.yml --project-name symphony"
GITLAB_URL="http://localhost:8929"
GITLAB_ROOT_PASS="SymphonyDemo2024!"
CONFIG_REPO="symphony-demo/infra"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
info()    { echo -e "${BLUE}[demo-init]${NC} $*"; }
success() { echo -e "${GREEN}[demo-init]${NC} ✓ $*"; }
warn()    { echo -e "${YELLOW}[demo-init]${NC} ⚠ $*"; }
fail()    { echo -e "${RED}[demo-init]${NC} ✗ $*" >&2; exit 1; }

# ── Prérequis ────────────────────────────────────────────────────────────────

command -v jq  >/dev/null || fail "jq est requis : sudo apt install jq  (ou brew install jq)"
command -v docker >/dev/null || fail "docker est requis"

# ── 1. Attente de GitLab ─────────────────────────────────────────────────────

info "Attente de GitLab CE (peut prendre 3-5 min au premier démarrage)…"
MAX=60; i=0
until curl -sf "$GITLAB_URL/-/health" >/dev/null 2>&1; do
  i=$((i+1))
  [ $i -ge $MAX ] && fail "GitLab n'a pas démarré après $((MAX*10))s. Vérifiez : docker compose -f docker-compose.demo.yml logs gitlab"
  printf "."
  sleep 10
done
echo ""
success "GitLab est prêt ($GITLAB_URL)"

# ── 2. Personal Access Token root ────────────────────────────────────────────

info "Création du Personal Access Token (via gitlab-rails runner)…"
PAT=$($COMPOSE exec -T gitlab gitlab-rails runner "
token = User.find_by_username('root').personal_access_tokens.create!(
  name:       'symphony-demo',
  scopes:     ['api', 'read_repository', 'write_repository', 'read_user'],
  expires_at: Date.today + 365
)
puts token.token
" 2>/dev/null | grep -E '^glpat-' | head -1)

[ -z "$PAT" ] && fail "Impossible de créer le PAT. GitLab est-il bien démarré ?"
success "PAT root créé"

gl() {
  curl -sf -H "PRIVATE-TOKEN: $PAT" "$@"
}
gl_post() {
  curl -sf -X POST -H "PRIVATE-TOKEN: $PAT" -H "Content-Type: application/json" "$@"
}

# ── 3. Groupe + projet infra ─────────────────────────────────────────────────

info "Création du groupe symphony-demo…"
GROUP=$(gl_post "$GITLAB_URL/api/v4/groups" \
  -d '{"name":"Symphony Demo","path":"symphony-demo","visibility":"internal"}' 2>/dev/null || true)
GROUP_ID=$(echo "$GROUP" | jq -r '.id // empty')

if [ -z "$GROUP_ID" ]; then
  warn "Groupe déjà existant ou erreur — récupération de l'ID existant…"
  GROUP_ID=$(gl "$GITLAB_URL/api/v4/groups?search=symphony-demo" | jq -r '.[0].id // empty')
fi
[ -z "$GROUP_ID" ] && fail "Impossible de créer/trouver le groupe symphony-demo"
success "Groupe symphony-demo (id=$GROUP_ID)"

info "Création du projet infra (config_repo)…"
INFRA=$(gl_post "$GITLAB_URL/api/v4/projects" \
  -d "{\"name\":\"infra\",\"path\":\"infra\",\"namespace_id\":$GROUP_ID,\"initialize_with_readme\":true,\"visibility\":\"internal\"}" 2>/dev/null || true)
INFRA_ID=$(echo "$INFRA" | jq -r '.id // empty')

if [ -z "$INFRA_ID" ]; then
  warn "Projet infra déjà existant — récupération…"
  INFRA_ID=$(gl "$GITLAB_URL/api/v4/projects?search=infra" | jq -r '.[] | select(.path_with_namespace=="symphony-demo/infra") | .id')
fi
[ -z "$INFRA_ID" ] && fail "Impossible de créer/trouver le projet infra"
success "Projet infra créé (id=$INFRA_ID)"

# ── 4. Utilisateurs de démo ──────────────────────────────────────────────────

info "Création des utilisateurs de démo…"

create_user() {
  local username=$1 name=$2 email=$3
  local result
  result=$(gl_post "$GITLAB_URL/api/v4/users" \
    -d "{\"username\":\"$username\",\"name\":\"$name\",\"email\":\"$email\",\"password\":\"SymphonyDemo2024!\",\"skip_confirmation\":true}" 2>/dev/null || true)
  local uid
  uid=$(echo "$result" | jq -r '.id // empty')
  if [ -z "$uid" ]; then
    uid=$(gl "$GITLAB_URL/api/v4/users?username=$username" | jq -r '.[0].id // empty')
  fi
  echo "$uid"
}

LEAD_ID=$(create_user "alice" "Alice (Lead)" "alice@symphony-demo.local")
DEV_ID=$(create_user  "bob"   "Bob (Dev)"   "bob@symphony-demo.local")

[ -n "$LEAD_ID" ] && success "Utilisateur alice (lead) créé/trouvé (id=$LEAD_ID)" || warn "alice non créé"
[ -n "$DEV_ID"  ] && success "Utilisateur bob (dev) créé/trouvé (id=$DEV_ID)"    || warn "bob non créé"

# Ajouter alice et bob au groupe
if [ -n "$LEAD_ID" ]; then
  gl_post "$GITLAB_URL/api/v4/groups/$GROUP_ID/members" \
    -d "{\"user_id\":$LEAD_ID,\"access_level\":40}" >/dev/null 2>&1 || true
fi
if [ -n "$DEV_ID" ]; then
  gl_post "$GITLAB_URL/api/v4/groups/$GROUP_ID/members" \
    -d "{\"user_id\":$DEV_ID,\"access_level\":30}" >/dev/null 2>&1 || true
fi

# ── 5. Runner ────────────────────────────────────────────────────────────────

info "Enregistrement du GitLab Runner…"
RUNNER_RESP=$(gl_post "$GITLAB_URL/api/v4/user/runners" \
  -d '{"runner_type":"instance_type","run_untagged":true,"description":"symphony-demo-runner"}' 2>/dev/null || true)
RUNNER_TOKEN=$(echo "$RUNNER_RESP" | jq -r '.token // empty')

if [ -n "$RUNNER_TOKEN" ]; then
  $COMPOSE exec -T gitlab-runner gitlab-runner register \
    --non-interactive \
    --url "http://gitlab:8929" \
    --token "$RUNNER_TOKEN" \
    --executor "docker" \
    --docker-image "docker:latest" \
    --docker-volumes "/var/run/docker.sock:/var/run/docker.sock" \
    --docker-network-mode "host" \
    --description "symphony-demo-runner" \
    2>/dev/null && success "Runner enregistré" || warn "Enregistrement runner échoué — à faire manuellement"
else
  warn "Impossible de créer le runner token (API GitLab 16+ requise)"
fi

# ── 6. Écriture de .env.demo ─────────────────────────────────────────────────

cat > .env.demo << EOF
# Généré par scripts/demo-init.sh — NE PAS COMMITTER
GITLAB_TOKEN=$PAT
PORT=8090
DB_HOST=localhost
DB_PORT=5432
DB_NAME=symphony
DB_USER=symphony
DB_PASSWORD=symphony123
SYMPHONY_DEV_MODE=1
GOLDEN_PATHS_DIR=./config/golden-paths
EOF

success ".env.demo écrit"

# ── Résumé ───────────────────────────────────────────────────────────────────

echo ""
echo -e "${GREEN}╔═══════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║         Symphony Demo — configuration prête           ║${NC}"
echo -e "${GREEN}╚═══════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e " ${YELLOW}GitLab CE${NC}"
echo    "   URL       : $GITLAB_URL"
echo    "   root      : root / SymphonyDemo2024!"
echo    "   alice     : alice / SymphonyDemo2024!  (lead)"
echo    "   bob       : bob   / SymphonyDemo2024!  (developer)"
echo ""
echo -e " ${YELLOW}Symphony${NC}"
echo    "   1. Charger la config demo :"
echo    "      source .env.demo"
echo    "      make run          # ou PORT=8090 go run ./cmd/symphony"
echo ""
echo    "   2. Ouvrir http://localhost:8090"
echo    "      → Le wizard vous demande les valeurs ci-dessous :"
echo ""
echo    "      GitLab URL    : $GITLAB_URL"
echo -e "      GitLab Token  : ${YELLOW}$PAT${NC}"
echo    "      Config repo   : $CONFIG_REPO"
echo    "      Docker socket : /var/run/docker.sock"
echo ""
echo -e " ${YELLOW}Note${NC} : SYMPHONY_DEV_MODE=1 est actif — auth OIDC désactivée."
echo    "   Pour activer l'OIDC en production, configurer OIDC_ISSUER."
echo ""
