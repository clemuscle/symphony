#!/usr/bin/env bash
# Projets de démo : créés et déployés par l'API de Symphony, comme depuis
# l'UI, au nom de bob (developer). Chacun est un vrai projet : repo dans le
# groupe apps, pipeline, image dans le registry, service joignable, et sa
# fiche catalogue (seed/<nom>.yaml) commitée dans son repo, comme le ferait
# son équipe.
#
# Appelé par up.sh (étape 7) ; rejouable seul une fois la démo démarrée :
#   ./demo/seed.sh [true|false]   (false : sans Kubernetes, tout sur Docker)
# Un projet déjà créé ou déjà déployé n'est pas refait.

set -euo pipefail
cd "$(dirname "$0")"

K8S=${1:-true}

GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; RED='\033[0;31m'; NC='\033[0m'
info() { echo -e "${BLUE}[démo]${NC} $*"; }
ok()   { echo -e "${GREEN}[démo]${NC} ✓ $*"; }
warn() { echo -e "${YELLOW}[démo]${NC} ⚠ $*"; }
fail() { echo -e "${RED}[démo]${NC} ✗ $*" >&2; exit 1; }

[ -f .env ] || fail "demo/.env absent : lancer d'abord ./demo/up.sh"
saved() { sed -n "s/^$1=//p" .env | tail -n1; }
GITLAB_URL="http://localhost:$(saved GITLAB_PORT)"
SYMPHONY_URL="http://localhost:$(saved SYMPHONY_PORT)"
KEYCLOAK_URL="http://localhost:$(saved KEYCLOAK_PORT)"
ARGOCD_URL="http://localhost:$(saved ARGOCD_PORT)"
GITLAB_TOKEN=$(saved GITLAB_TOKEN)

# Un jeton par appel : ceux de Keycloak expirent au bout de 5 min
token() {
  curl -sf -d grant_type=password -d client_id=symphony -d username=bob -d password=bob \
    "$KEYCLOAK_URL/realms/symphony/protocol/openid-connect/token" | jq -r .access_token
}
sym() { curl -sf -H "Authorization: Bearer $(token)" -H "Content-Type: application/json" "$@"; }
urlenc() { jq -rn --arg s "$1" '$s|@uri'; }

# wait_task <id> <libellé> : attend la fin d'une tâche, renvoie son statut
wait_task() {
  local id=$1 label=$2 task status
  for _ in $(seq 180); do
    task=$(sym "$SYMPHONY_URL/api/v1/tasks/$id" || true)
    status=$(echo "$task" | jq -r '.status // empty' 2>/dev/null || true)
    case $status in
      SUCCESS) return 0 ;;
      FAILED) warn "$label : $(echo "$task" | jq -r .error)"; return 1 ;;
    esac
    sleep 5
  done
  warn "$label : toujours en cours après 15 min"
  return 1
}

projects=$(jq -c '.[]' seed/projects.json)
if [ "$K8S" != true ]; then
  # Sans Kubernetes, ni runtime argocd-k3s ni golden path api-rest-go-k8s
  projects=$(echo "$projects" | jq -c '.runtime_provider = "docker-local"
    | if .golden_path_id == "api-rest-go-k8s" then .golden_path_id = "api-rest-go" else . end')
fi

# 1. Création : les trois tâches partent ensemble, leurs pipelines passent
# l'une après l'autre sur le runner
existing=$(sym "$SYMPHONY_URL/api/v1/projects" | jq -r '.[].name')
declare -A created=()
while read -r p; do
  name=$(echo "$p" | jq -r .name)
  if grep -qx "$name" <<<"$existing"; then
    ok "$name déjà créé"
    continue
  fi
  id=$(echo "$p" | sym -X POST --data @- "$SYMPHONY_URL/api/v1/projects" | jq -r '.id // empty')
  [ -n "$id" ] || fail "création de $name refusée par Symphony"
  created[$name]=$id
done <<<"$projects"

if [ ${#created[@]} -gt 0 ]; then
  info "Création de ${!created[*]} : repo, squelette, première pipeline (3 à 5 min)…"
  for name in "${!created[@]}"; do
    wait_task "${created[$name]}" "création de $name" && ok "$name créé, image dans le registry"
  done
fi

# 2. Déploiement de l'image construite, sur le runtime prévu
deployments=$(sym "$SYMPHONY_URL/api/v1/deployments" | jq -r '.[].name')
declare -A deploying=()
while read -r p; do
  name=$(echo "$p" | jq -r .name)
  runtime=$(echo "$p" | jq -r .runtime_provider)
  if grep -qx "$name" <<<"$deployments"; then
    ok "$name déjà déployé"
    continue
  fi
  image=$(sym "$SYMPHONY_URL/api/v1/projects/$name/image" || true)
  tag=$(echo "$image" | jq -r '[.tags[]? | select(. != "latest")][0] // "latest"' 2>/dev/null || true)
  if [ -z "$image" ] || [ -z "$tag" ]; then
    warn "$name : pas d'image dans le registry, déploiement sauté"
    continue
  fi
  id=$(jq -n --arg n "$name" --arg i "$(echo "$image" | jq -r .image)" --arg t "$tag" --arg r "$runtime" \
      '{project_name: $n, image: $i, tag: $t, port: 8080, runtime_provider: $r}' \
    | sym -X POST --data @- "$SYMPHONY_URL/api/v1/deployments" | jq -r '.id // empty')
  [ -n "$id" ] || { warn "déploiement de $name refusé par Symphony"; continue; }
  deploying[$name]=$id
done <<<"$projects"

if [ ${#deploying[@]} -gt 0 ]; then
  info "Déploiement de ${!deploying[*]}…"
  for name in "${!deploying[@]}"; do
    wait_task "${deploying[$name]}" "déploiement de $name" && ok "$name déployé"
  done
fi

# 3. Fiche catalogue complète (équipe, SLO, dépendances, liens), commitée
# dans le repo du projet comme le ferait son équipe ; [skip ci] : pas de
# nouvelle pipeline pour une fiche
deployments=$(sym "$SYMPHONY_URL/api/v1/deployments")
projects_api=$(sym "$SYMPHONY_URL/api/v1/projects")
while read -r p; do
  name=$(echo "$p" | jq -r .name)
  repo=$(echo "$projects_api" | jq -r --arg n "$name" '.[] | select(.name == $n) | .repo_path')
  [ -n "$repo" ] || continue
  # Fiche déjà complétée : une fiche générée par Symphony n'a pas de SLO
  current=$(curl -sf -H "PRIVATE-TOKEN: $GITLAB_TOKEN" \
    "$GITLAB_URL/api/v4/projects/$(urlenc "$repo")/repository/files/catalog-info.yaml/raw?ref=main" || true)
  grep -q '^  slo:' <<<"$current" && continue

  app_url=$(echo "$deployments" | jq -r --arg n "$name" '[.[] | select(.name == $n) | .url][0] // ""')
  fiche=$(sed -e "s|__REPO_URL__|$GITLAB_URL/$repo|g" -e "s|__APP_URL__|$app_url|g" \
    -e "s|__ARGOCD_URL__|$ARGOCD_URL|g" "seed/$name.yaml")
  [ "$K8S" = true ] || fiche=$(sed '/title: Application Argo CD/,+2d' <<<"$fiche")
  jq -n --arg c "$fiche" \
    '{branch: "main", content: $c, commit_message: "docs: fiche catalogue complète (équipe, SLO, dépendances) [skip ci]"}' \
    | curl -sf -X PUT -H "PRIVATE-TOKEN: $GITLAB_TOKEN" -H "Content-Type: application/json" --data @- \
      "$GITLAB_URL/api/v4/projects/$(urlenc "$repo")/repository/files/catalog-info.yaml" >/dev/null \
    && ok "$name : fiche catalogue complétée dans $repo" \
    || warn "$name : fiche catalogue non mise à jour"
done <<<"$projects"

echo
echo "$deployments" | jq -r '.[] | select(.url != "") | "  \(.name)\t\(.url)"' | sort
