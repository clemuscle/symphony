#!/usr/bin/env bash
# Symphony demo — infra + bootstrap GitLab, entièrement automatisé.
#
# Ce script fait tout ce qui est mécanique et sans intérêt pédagogique :
#   1. Vérifie les prérequis (docker, docker compose, go, node/npm, jq, RAM, ports)
#   2. Construit le frontend une fois si besoin (assets embarqués absents)
#   3. Démarre PostgreSQL + GitLab CE + GitLab Runner (docker compose)
#   4. Attend que GitLab CE soit prêt, désactive Auto DevOps
#   5. Crée le groupe symphony-demo + le projet infra, un token SCM, et
#      enregistre le runner — idempotent, rejouable sans tout redémarrer.
#
# Il n'y a plus qu'une seule étape volontairement manuelle : coller le token
# SCM affiché en fin de script dans le wizard Symphony (DEMO.md, étape
# "Wizard"). C'est le seul endroit où faire remplir le formulaire soi-même a
# une vraie valeur pédagogique (comprendre la config providers de
# Symphony) — le reste (groupe/projet/tokens/runner GitLab) n'apprend rien
# sur Symphony, d'où l'automatisation complète ici.
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
command -v jq >/dev/null || fail "jq est requis pour le bootstrap GitLab (https://jqlang.org)"
ok "docker, docker compose, go, node, npm, jq présents"

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

# ── 5. Auto DevOps ────────────────────────────────────────────────────────────
# gitlab_rails['auto_devops_enabled'] = false dans le GITLAB_OMNIBUS_CONFIG du
# compose ne seed pas fiablement ce réglage en base sur un volume neuf — sans
# ce correctif, GitLab injecte son propre job "build" (buildpacks,
# gl-auto-build-variables.env) à côté de celui du golden path, qui échoue.
# Pur réglage d'infra sans valeur pédagogique — jamais quelque chose que le
# guide demande de faire à la main.
info "Désactivation d'Auto DevOps (collisionne avec le .gitlab-ci.yml du golden path)…"
$COMPOSE exec -T gitlab gitlab-rails runner "ApplicationSetting.current.update!(auto_devops_enabled: false)" >/dev/null 2>&1 \
  && ok "Auto DevOps désactivé" \
  || warn "Impossible de désactiver Auto DevOps automatiquement — si le stage 'build' d'un projet échoue avec des artefacts gl-auto-build-variables.env, relancer ce script"

# ── 6. Bootstrap GitLab (groupe, projet infra, token SCM, runner) ─────────────
# Rien ici n'apprend quoi que ce soit sur Symphony (c'est de la plomberie
# GitLab pure) — entièrement automatisé, contrairement au wizard Symphony qui
# reste volontairement manuel (voir DEMO.md).

info "Bootstrap GitLab (groupe, projet infra, token, runner)…"

# Jeton root généré via la console Rails (jamais affiché, usage interne à ce
# script uniquement) — même mécanisme que la désactivation d'Auto DevOps
# ci-dessus, pas de nouvelle surface de confiance.
ROOT_TOKEN=$($COMPOSE exec -T gitlab gitlab-rails runner \
  "puts User.find_by(username: 'root').personal_access_tokens.create!(scopes: ['api'], name: 'symphony-bootstrap', expires_at: 1.day.from_now).token" \
  2>/dev/null | tail -n1)
[ -n "$ROOT_TOKEN" ] || fail "Impossible de générer un token root pour le bootstrap GitLab"

# Pas de curl -f ici, volontairement : -f fait perdre le corps de la
# réponse (donc le message d'erreur GitLab) et, combiné à set -e/pipefail,
# tue le script sans un mot au premier 404 rencontré — exactement le cas
# normal d'un lookup d'existence sur une ressource pas encore créée. Chaque
# appel est donc vérifié explicitement ci-dessous, avec le corps affiché en
# cas d'échec réel.
gl() { curl -s -H "PRIVATE-TOKEN: $ROOT_TOKEN" "$@"; }

# Groupe symphony-demo — idempotent : réutilise s'il existe déjà.
GROUP_ID=$(gl "$GITLAB_URL/api/v4/groups/symphony-demo" | jq -r '.id // empty')
if [ -z "$GROUP_ID" ]; then
  RESP=$(gl -X POST "$GITLAB_URL/api/v4/groups" \
    -d "name=symphony-demo" -d "path=symphony-demo" -d "visibility=private")
  GROUP_ID=$(echo "$RESP" | jq -r '.id // empty')
  [ -n "$GROUP_ID" ] || fail "Échec de création du groupe symphony-demo : $RESP"
  ok "Groupe symphony-demo créé"
else
  ok "Groupe symphony-demo déjà présent"
fi

# Projet infra (avec README — la synchro GitOps de Symphony lit le dernier
# commit dès le démarrage, un repo vide ferait échouer cette lecture).
PROJECT_EXISTS=$(gl -o /dev/null -w '%{http_code}' "$GITLAB_URL/api/v4/projects/symphony-demo%2Finfra")
if [ "$PROJECT_EXISTS" = "200" ]; then
  ok "Projet symphony-demo/infra déjà présent"
else
  RESP=$(gl -X POST "$GITLAB_URL/api/v4/projects" \
    -d "name=infra" -d "namespace_id=$GROUP_ID" -d "initialize_with_readme=true" -d "visibility=private")
  echo "$RESP" | jq -e '.id' >/dev/null || fail "Échec de création du projet infra : $RESP"
  ok "Projet symphony-demo/infra créé"
fi

# Token SCM (group access token, scope api, rôle Owner) — révoqué et
# recréé à chaque run : GitLab n'affiche la valeur d'un token qu'à sa
# création, impossible de le récupérer plus tard pour rester idempotent.
EXISTING_GAT_ID=$(gl "$GITLAB_URL/api/v4/groups/$GROUP_ID/access_tokens" | jq -r '.[]? | select(.name=="symphony-scm") | .id')
if [ -n "$EXISTING_GAT_ID" ]; then
  gl -X DELETE "$GITLAB_URL/api/v4/groups/$GROUP_ID/access_tokens/$EXISTING_GAT_ID" >/dev/null 2>&1 || true
fi
RESP=$(gl -X POST "$GITLAB_URL/api/v4/groups/$GROUP_ID/access_tokens" \
  -d "name=symphony-scm" -d "scopes[]=api" -d "access_level=50" \
  -d "expires_at=$(date -d '+1 year' +%Y-%m-%d 2>/dev/null || date -v+1y +%Y-%m-%d)")
SCM_TOKEN=$(echo "$RESP" | jq -r '.token // empty')
[ -n "$SCM_TOKEN" ] || fail "Échec de création du token SCM : $RESP"
ok "Token SCM (re)généré"

# Runner — idempotent : ne recrée que si aucun runner en ligne n'existe déjà
# (le token de runner n'est lisible qu'à la création, donc pas de retry
# silencieux possible si un run précédent a déjà un runner fonctionnel).
RUNNER_ONLINE=$(gl "$GITLAB_URL/api/v4/runners/all" | jq -r 'any(.[]?; .online)')
if [ "$RUNNER_ONLINE" = "true" ]; then
  ok "Runner déjà enregistré et en ligne"
else
  RESP=$(gl -X POST "$GITLAB_URL/api/v4/user/runners" \
    -d "runner_type=instance_type" -d "tag_list=docker" -d "description=symphony-demo-runner")
  RUNNER_TOKEN=$(echo "$RESP" | jq -r '.token // empty')
  [ -n "$RUNNER_TOKEN" ] || fail "Échec de création du runner : $RESP"
  $COMPOSE exec -T gitlab-runner gitlab-runner register --non-interactive \
    --url http://gitlab:8929 \
    --token "$RUNNER_TOKEN" \
    --executor docker \
    --docker-image docker:latest \
    --docker-volumes /var/run/docker.sock:/var/run/docker.sock \
    --docker-network-mode host \
    --description symphony-demo-runner >/dev/null 2>&1 \
    && ok "Runner enregistré" \
    || fail "Échec d'enregistrement du runner — voir $COMPOSE logs gitlab-runner"
fi

echo ""
echo -e "${GREEN}╔═══════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║              Infra démo prête                         ║${NC}"
echo -e "${GREEN}╚═══════════════════════════════════════════════════════╝${NC}"
echo ""
echo "  Groupe GitLab : symphony-demo (repo config : symphony-demo/infra)"
echo -e "  Token SCM à coller dans le wizard : ${YELLOW}${SCM_TOKEN}${NC}"
echo ""
echo "  Suite :"
echo "   1. cp .env.demo.example .env && make demo-start"
echo "   2. Ouvrir http://localhost:8090 → wizard providers :"
echo "        SCM url=$GITLAB_URL, token=(ci-dessus)"
echo "        CI config_repo=symphony-demo/infra"
echo "        Registry (laisser vide) · Deploy socket=/var/run/docker.sock"
echo "   3. make demo-seed  (peuple 4 projets à différents stades — optionnel)"
echo ""
