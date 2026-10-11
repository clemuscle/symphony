#!/usr/bin/env bash
# Démo Symphony, de zéro à une instance utilisable, en une commande :
#   1. vérifie les prérequis
#   2. démarre PostgreSQL, GitLab CE et le runner
#   3. initialise GitLab : Auto DevOps désactivé, token API, repos
#      root/symphony-config et root/symphony-templates (contenu de demo/gitlab/),
#      runner enregistré
#   4. démarre Keycloak (realm symphony : alice admin, bob developer,
#      carol viewer, dave sans rôle)
#   5. démarre Symphony et attend qu'il ait chargé golden paths et catalogue
#
# Rejouable : chaque étape vérifie ce qui existe déjà.
# Ports réglables : GITLAB_PORT=18929 REGISTRY_PORT=15050 GITLAB_SSH_PORT=12224 \
#   SYMPHONY_PORT=18080 KEYCLOAK_PORT=18180 ./demo/up.sh
# Arrêt : ./demo/down.sh

set -euo pipefail
cd "$(dirname "$0")"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
info() { echo -e "${BLUE}[démo]${NC} $*"; }
ok()   { echo -e "${GREEN}[démo]${NC} ✓ $*"; }
warn() { echo -e "${YELLOW}[démo]${NC} ⚠ $*"; }
fail() { echo -e "${RED}[démo]${NC} ✗ $*" >&2; exit 1; }

# Réglages : environnement > demo/.env (run précédent) > défauts
saved() { [ -f .env ] && sed -n "s/^$1=//p" .env | tail -n1 || true; }
GITLAB_PORT=${GITLAB_PORT:-$(saved GITLAB_PORT)}; GITLAB_PORT=${GITLAB_PORT:-8929}
REGISTRY_PORT=${REGISTRY_PORT:-$(saved REGISTRY_PORT)}; REGISTRY_PORT=${REGISTRY_PORT:-5050}
GITLAB_SSH_PORT=${GITLAB_SSH_PORT:-$(saved GITLAB_SSH_PORT)}; GITLAB_SSH_PORT=${GITLAB_SSH_PORT:-2224}
SYMPHONY_PORT=${SYMPHONY_PORT:-$(saved SYMPHONY_PORT)}; SYMPHONY_PORT=${SYMPHONY_PORT:-8080}
KEYCLOAK_PORT=${KEYCLOAK_PORT:-$(saved KEYCLOAK_PORT)}; KEYCLOAK_PORT=${KEYCLOAK_PORT:-8180}
SYMPHONY_IMAGE=${SYMPHONY_IMAGE:-$(saved SYMPHONY_IMAGE)}; SYMPHONY_IMAGE=${SYMPHONY_IMAGE:-ghcr.io/clemuscle/symphony:latest}
GITLAB_TOKEN=$(saved GITLAB_TOKEN)
GITLAB_ROOT_PASSWORD=$(saved GITLAB_ROOT_PASSWORD)
random() { head -c 64 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c "$1"; }
GITLAB_ROOT_PASSWORD=${GITLAB_ROOT_PASSWORD:-Sy-$(random 20)}
KEYCLOAK_ADMIN_PASSWORD=$(saved KEYCLOAK_ADMIN_PASSWORD); KEYCLOAK_ADMIN_PASSWORD=${KEYCLOAK_ADMIN_PASSWORD:-$(random 20)}
SESSION_SECRET=$(saved SESSION_SECRET); SESSION_SECRET=${SESSION_SECRET:-$(random 48)}

GITLAB_URL="http://localhost:${GITLAB_PORT}"
SYMPHONY_URL="http://localhost:${SYMPHONY_PORT}"
KEYCLOAK_URL="http://localhost:${KEYCLOAK_PORT}"

write_env() {
  cat > .env <<EOF
# Écrit par demo/up.sh — ne pas versionner (contient un token)
GITLAB_PORT=${GITLAB_PORT}
REGISTRY_PORT=${REGISTRY_PORT}
GITLAB_SSH_PORT=${GITLAB_SSH_PORT}
SYMPHONY_PORT=${SYMPHONY_PORT}
KEYCLOAK_PORT=${KEYCLOAK_PORT}
SYMPHONY_IMAGE=${SYMPHONY_IMAGE}
KEYCLOAK_ADMIN_PASSWORD=${KEYCLOAK_ADMIN_PASSWORD}
SESSION_SECRET=${SESSION_SECRET}
GITLAB_ROOT_PASSWORD=${GITLAB_ROOT_PASSWORD}
GITLAB_TOKEN=${GITLAB_TOKEN}
EOF
  chmod 600 .env
}

COMPOSE="docker compose"

# ── 1. Prérequis ──────────────────────────────────────────────────────────────

command -v docker >/dev/null || fail "docker est requis"
docker compose version >/dev/null 2>&1 || fail "docker compose v2 est requis"
command -v curl >/dev/null || fail "curl est requis"
command -v jq >/dev/null || fail "jq est requis (https://jqlang.org)"
ok "docker, docker compose, curl, jq"

if command -v free >/dev/null 2>&1; then
  avail=$(free -m | awk '/^Mem:/ {print $7}')
  [ "${avail:-0}" -lt 4000 ] && warn "RAM disponible : ${avail} Mo ; GitLab CE en demande ~4 Go, ça peut être lent"
fi

# Un port pris par autre chose que la démo elle-même = conflit
running=$($COMPOSE ps --services --status running 2>/dev/null || true)
if [ -z "$running" ] && command -v ss >/dev/null 2>&1; then
  for p in "$GITLAB_PORT" "$REGISTRY_PORT" "$GITLAB_SSH_PORT" "$SYMPHONY_PORT" "$KEYCLOAK_PORT"; do
    ss -ltn | awk '{print $4}' | grep -q ":${p}\$" \
      && fail "port ${p} déjà utilisé ; choisir d'autres ports, ex. GITLAB_PORT=18929 REGISTRY_PORT=15050 GITLAB_SSH_PORT=12224 SYMPHONY_PORT=18080 KEYCLOAK_PORT=18180 ./demo/up.sh"
  done
fi

write_env

# Realm Keycloak : les URL de retour dépendent du port de Symphony
mkdir -p keycloak/import
sed "s|__SYMPHONY_URL__|${SYMPHONY_URL}|g" keycloak/realm-symphony.template.json > keycloak/import/realm-symphony.json

# ── 2. Infra ──────────────────────────────────────────────────────────────────

info "Démarrage de PostgreSQL, GitLab CE, du runner et de Keycloak…"
$COMPOSE up -d postgres gitlab gitlab-runner keycloak >/dev/null

info "Attente de GitLab (3 à 5 min au premier démarrage)…"
for i in $(seq 90); do
  curl -sf -o /dev/null "$GITLAB_URL/users/sign_in" && break
  [ "$i" = 90 ] && fail "GitLab ne répond pas après 15 min : $COMPOSE logs gitlab"
  printf "."; sleep 10
done
echo
ok "GitLab prêt : $GITLAB_URL"

# ── 3. Initialisation de GitLab ──────────────────────────────────────────────

api() { curl -s -H "PRIVATE-TOKEN: $GITLAB_TOKEN" "$@"; }

if [ -n "$GITLAB_TOKEN" ] && [ "$(api -o /dev/null -w '%{http_code}' "$GITLAB_URL/api/v4/user")" = 200 ]; then
  ok "Token API déjà en place"
else
  info "Auto DevOps désactivé et token API de root créé (console Rails, ~1 min)…"
  # Auto DevOps : le réglage du compose ne suffit pas sur un volume neuf
  GITLAB_TOKEN=$($COMPOSE exec -T gitlab gitlab-rails runner '
    ApplicationSetting.current.update!(auto_devops_enabled: false)
    root = User.find_by_username("root")
    puts root.personal_access_tokens.create!(name: "symphony-demo", scopes: ["api"], expires_at: 365.days.from_now).token
  ' 2>/dev/null | tail -n1)
  [ -n "$GITLAB_TOKEN" ] || fail "création du token impossible : $COMPOSE logs gitlab"
  write_env
  ok "Token API créé (dans demo/.env)"
fi

# Pousse le contenu de demo/gitlab/<repo>/ dans root/<repo>, en un commit
seed_repo() {
  local repo=$1 dir=gitlab/$1
  if [ "$(api -o /dev/null -w '%{http_code}' "$GITLAB_URL/api/v4/projects/root%2F$repo")" = 200 ]; then
    ok "root/$repo déjà présent"
    return
  fi
  api -X POST "$GITLAB_URL/api/v4/projects" -d "name=$repo" -d "visibility=private" \
    | jq -e .id >/dev/null || fail "création de root/$repo impossible"
  local actions
  actions=$(cd "$dir" && find . -type f | sort | while read -r f; do
    jq -n --arg p "${f#./}" --rawfile c "$f" '{action: "create", file_path: $p, content: $c}'
  done | jq -s .)
  jq -n --argjson a "$actions" \
    '{branch: "main", commit_message: "feat: contenu initial de la démo [symphony]", actions: $a}' \
    | api -X POST -H "Content-Type: application/json" --data @- \
      "$GITLAB_URL/api/v4/projects/root%2F$repo/repository/commits" \
    | jq -e .id >/dev/null || fail "push du contenu de root/$repo impossible"
  ok "root/$repo créé ($(echo "$actions" | jq length) fichiers)"
}
seed_repo symphony-config
seed_repo symphony-templates

if [ "$(api "$GITLAB_URL/api/v4/runners/all" | jq 'any(.[]?; .online)')" = true ]; then
  ok "Runner déjà enregistré"
else
  info "Enregistrement du runner…"
  runner_token=$(api -X POST "$GITLAB_URL/api/v4/user/runners" \
    -d "runner_type=instance_type" -d "run_untagged=true" -d "description=symphony-demo" | jq -r '.token // empty')
  [ -n "$runner_token" ] || fail "création du runner impossible"
  # Réseau hôte : les jobs joignent GitLab et le registry sur localhost,
  # comme le daemon Docker qui construit et pousse les images
  $COMPOSE exec -T gitlab-runner gitlab-runner register --non-interactive \
    --url "http://gitlab:${GITLAB_PORT}" --token "$runner_token" \
    --executor docker --docker-image alpine:latest \
    --docker-volumes /var/run/docker.sock:/var/run/docker.sock \
    --docker-network-mode host >/dev/null \
    || fail "enregistrement du runner impossible : $COMPOSE logs gitlab-runner"
  ok "Runner enregistré"
fi

# ── 4. Keycloak ───────────────────────────────────────────────────────────────

for i in $(seq 60); do
  curl -sf -o /dev/null "$KEYCLOAK_URL/realms/symphony/.well-known/openid-configuration" && break
  [ "$i" = 60 ] && fail "Keycloak ne répond pas : $COMPOSE logs keycloak"
  sleep 3
done
ok "Keycloak prêt : $KEYCLOAK_URL (realm symphony)"

# ── 5. Symphony ───────────────────────────────────────────────────────────────

info "Démarrage de Symphony ($SYMPHONY_IMAGE)…"
$COMPOSE --profile symphony up -d symphony >/dev/null
# L'API exige une session : on vérifie avec un jeton de carol (viewer)
token=$(curl -sf -d grant_type=password -d client_id=symphony -d username=carol -d password=carol \
  "$KEYCLOAK_URL/realms/symphony/protocol/openid-connect/token" | jq -r .access_token)
[ -n "$token" ] && [ "$token" != null ] || fail "jeton de démo impossible à obtenir auprès de Keycloak"
count() { curl -sf -H "Authorization: Bearer $token" "$SYMPHONY_URL/api/v1/$1" 2>/dev/null | jq length 2>/dev/null || echo 0; }
for i in $(seq 60); do
  n=$(count golden-paths); m=$(count services)
  [ "${n:-0}" -gt 0 ] && [ "${m:-0}" -gt 0 ] && break
  [ "$i" = 60 ] && fail "Symphony n'a pas chargé golden paths et catalogue : $COMPOSE logs symphony"
  sleep 2
done
ok "Symphony prêt : $n golden paths, $m services au catalogue"

cat <<EOF

$(echo -e "${GREEN}Démo prête${NC}")

  Symphony   $SYMPHONY_URL
             alice (admin) · bob (developer) · carol (viewer) · dave (sans rôle)
             mot de passe = identifiant
  GitLab     $GITLAB_URL   (root / $GITLAB_ROOT_PASSWORD)
  Keycloak   $KEYCLOAK_URL/admin   (admin / $KEYCLOAK_ADMIN_PASSWORD)
  Métriques  $SYMPHONY_URL/metrics

  Scénario : demo/SCENARIO.md      Arrêt : ./demo/down.sh

EOF
