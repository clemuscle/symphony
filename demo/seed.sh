#!/usr/bin/env bash
# Projets de démo : créés et déployés par l'API de Symphony, comme depuis
# l'UI, au nom d'alice (admin : l'équipe plateforme amorce la démo, pour trois
# équipes différentes). Chacun est un vrai projet : repo dans le
# groupe apps, pipeline, image dans le registry, service joignable, et sa
# fiche catalogue (seed/<nom>.yaml) commitée dans son repo, comme le ferait
# son équipe.
#
# Un quatrième, invoice-service, est un service « d'avant Symphony » : son
# repo est poussé directement dans le groupe services de la GitLab, avec une
# fiche Backstage ; Symphony le découvre (catalogue.discovery), puis le déploie.
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
  curl -sf -d grant_type=password -d client_id=symphony -d username=alice -d password=alice \
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
  # Sans Kubernetes, pas de stacks *-k8s (la production est alors sur Docker,
  # voir up.sh) : la stack officielle à la place, sans base PostgreSQL (son
  # infra demande Kubernetes)
  projects=$(echo "$projects" | jq -c 'if (.stack | endswith("-k8s")) then .stack |= rtrimstr("-k8s") | del(.parameters.postgres) else . end')
fi

# 0. Un service existant, sans Symphony : repo dans le groupe services, code
# Go (squelette de la stack officielle, sans métriques ni chart), sa CI et sa
# fiche Backstage, en un commit ; sa pipeline construit l'image
EXISTING=invoice-service
EXISTING_REPO=services/$EXISTING
gitlab() { curl -sf -H "PRIVATE-TOKEN: $GITLAB_TOKEN" -H "Content-Type: application/json" "$@"; }
if gitlab "$GITLAB_URL/api/v4/projects/$(urlenc "$EXISTING_REPO")" >/dev/null; then
  ok "$EXISTING_REPO déjà poussé"
else
  group=$(gitlab "$GITLAB_URL/api/v4/groups/services" | jq -r .id)
  gitlab -X POST --data "$(jq -n --arg n "$EXISTING" --argjson g "$group" '{name: $n, path: $n, namespace_id: $g, visibility: "private"}')"     "$GITLAB_URL/api/v4/projects" >/dev/null || fail "création de $EXISTING_REPO impossible"
  skeleton=../stacks/api-rest-go/skeleton
  render() { # placeholders de la stack, remplis comme le ferait une équipe
    sed -e "s|{{SERVICE_NAME}}|$EXISTING|g" -e 's|{{params.go_version}}|1.22|g' -e 's|{{params.log_level}}|info|g' "$1"
  }
  actions=$(
    {
      (cd "$skeleton" && find . -type f ! -path './deploy/*' ! -name 'metrics*' | sed 's|^\./||') | while read -r f; do
        jq -n --arg p "${f%.tmpl}" --arg c "$(render "$skeleton/$f")" '{action: "create", file_path: $p, content: $c}'
      done
      jq -n --arg c "$(render ../stacks/api-rest-go/ci.gitlab-ci.yml)" '{action: "create", file_path: ".gitlab-ci.yml", content: $c}'
      jq -n --arg c "$(sed "s|__REPO_URL__|$GITLAB_URL/$EXISTING_REPO|g" seed/$EXISTING.yaml)"         '{action: "create", file_path: "catalog-info.yaml", content: $c}'
      jq -n --arg c "# $EXISTING"$'\n\nFacturation et avoirs. Service écrit avant Symphony, qui le découvre par sa fiche (catalog-info.yaml).\n'         '{action: "create", file_path: "README.md", content: $c}'
    } | jq -s .
  )
  jq -n --argjson a "$actions" '{branch: "main", commit_message: "feat: service de facturation", actions: $a}'     | gitlab -X POST --data @- "$GITLAB_URL/api/v4/projects/$(urlenc "$EXISTING_REPO")/repository/commits" >/dev/null     && ok "$EXISTING_REPO poussé sans Symphony (code, CI, fiche Backstage)"     || fail "push de $EXISTING_REPO impossible"
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

# 2. Fiche catalogue complète (équipe, SLO, dépendances, liens, variables par
# environnement), commitée dans le repo du projet comme le ferait son équipe,
# avant le déploiement : les variables et secrets de l'appli en viennent.
# [skip ci] : pas de nouvelle pipeline pour une fiche.
projects_api=$(sym "$SYMPHONY_URL/api/v1/projects")
while read -r p; do
  name=$(echo "$p" | jq -r .name)
  repo=$(echo "$projects_api" | jq -r --arg n "$name" '.[] | select(.name == $n) | .repo_path')
  [ -n "$repo" ] || continue
  # Fiche déjà complétée : une fiche générée par Symphony n'a pas de SLO
  current=$(curl -sf -H "PRIVATE-TOKEN: $GITLAB_TOKEN" \
    "$GITLAB_URL/api/v4/projects/$(urlenc "$repo")/repository/files/catalog-info.yaml/raw?ref=main" || true)
  grep -q '^  slo:' <<<"$current" && continue

  fiche=$(sed -e "s|__REPO_URL__|$GITLAB_URL/$repo|g" -e "s|__ARGOCD_URL__|$ARGOCD_URL|g" "seed/$name.yaml")
  [ "$K8S" = true ] || fiche=$(sed -e '/title: Application Argo CD/,+2d' -e '/# k8s:début/,/# k8s:fin/d' \
    -e 's/^\(    stack: .*\)-k8s$/\1/' <<<"$fiche")
  jq -n --arg c "$fiche" \
    '{branch: "main", content: $c, commit_message: "docs: fiche catalogue complète (équipe, SLO, dépendances) [skip ci]"}' \
    | curl -sf -X PUT -H "PRIVATE-TOKEN: $GITLAB_TOKEN" -H "Content-Type: application/json" --data @- \
      "$GITLAB_URL/api/v4/projects/$(urlenc "$repo")/repository/files/catalog-info.yaml" >/dev/null \
    && ok "$name : fiche catalogue complétée dans $repo" \
    || warn "$name : fiche catalogue non mise à jour"
done <<<"$projects"

# Les fiches sont relues toutes les 15 s : on force la relecture, puis on
# attend qu'elles soient chargées (le SLO n'est que dans les fiches complètes)
sym -X POST "$SYMPHONY_URL/api/v1/sync" >/dev/null || true
for _ in $(seq 30); do
  loaded=$(sym "$SYMPHONY_URL/api/v1/projects" | jq '[.[] | select(.service.spec.slo.availability != "")] | length')
  [ "$loaded" -ge "$(echo "$projects" | wc -l)" ] && break
  sleep 2
done

# 3. Déploiement en staging de l'image construite par la pipeline
deployed() { # <projet> <environnement>
  sym "$SYMPHONY_URL/api/v1/deployments" \
    | jq -e --arg n "$1" --arg e "$2" 'any(.[]; .project == $n and .environment == $e and .status == "running")' >/dev/null
}
declare -A deploying=()
while read -r p; do
  name=$(echo "$p" | jq -r .name)
  if deployed "$name" staging; then
    ok "$name déjà déployé en staging"
    continue
  fi
  image=$(sym "$SYMPHONY_URL/api/v1/projects/$name/image" || true)
  tag=$(echo "$image" | jq -r '[.tags[]? | select(. != "latest")][0] // "latest"' 2>/dev/null || true)
  if [ -z "$image" ] || [ -z "$tag" ]; then
    warn "$name : pas d'image dans le registry, déploiement sauté"
    continue
  fi
  id=$(jq -n --arg n "$name" --arg i "$(echo "$image" | jq -r .image)" --arg t "$tag" \
      '{project_name: $n, environment: "staging", image: $i, tag: $t, port: 8080}' \
    | sym -X POST --data @- "$SYMPHONY_URL/api/v1/deployments" | jq -r '.id // empty')
  [ -n "$id" ] || { warn "déploiement de $name refusé par Symphony"; continue; }
  deploying[$name]=$id
done <<<"$projects"

if [ ${#deploying[@]} -gt 0 ]; then
  info "Déploiement en staging de ${!deploying[*]}…"
  for name in "${!deploying[@]}"; do
    wait_task "${deploying[$name]}" "déploiement de $name" && ok "$name déployé en staging"
  done
fi

# 4. Promotion en production (environnement protégé) : la même image, saine
# en staging
while read -r p; do
  name=$(echo "$p" | jq -r .name)
  [ "$(echo "$p" | jq -r '.promote // false')" = true ] || continue
  if deployed "$name" production; then
    ok "$name déjà en production"
    continue
  fi
  id=$(sym -X POST --data '{"to":"production"}' "$SYMPHONY_URL/api/v1/projects/$name/promote" | jq -r '.id // empty')
  [ -n "$id" ] || { warn "promotion de $name refusée par Symphony"; continue; }
  wait_task "$id" "promotion de $name" && ok "$name promu en production"
done <<<"$projects"

# 5. Le service existant : découvert par sa fiche une fois sa pipeline finie
# (image dans le registry), puis déployé en staging comme les autres
info "$EXISTING : attente de sa pipeline, puis de sa découverte…"
for _ in $(seq 120); do
  # La dernière pipeline qui a tourné (un commit [skip ci] en laisse une « skipped »)
  status=$(gitlab "$GITLAB_URL/api/v4/projects/$(urlenc "$EXISTING_REPO")/pipelines?ref=main&per_page=20" \
    | jq -r '[.[] | select(.status != "skipped")][0].status // empty')
  case $status in success|failed|canceled) break ;; esac
  sleep 5
done
[ "$status" = success ] || warn "$EXISTING : pipeline $status"
sym -X POST "$SYMPHONY_URL/api/v1/sync" >/dev/null || true
for _ in $(seq 30); do
  sym "$SYMPHONY_URL/api/v1/projects" | jq -e --arg n "$EXISTING" 'any(.[]; .name == $n and .service != null)' >/dev/null && break
  sleep 2
done
if ! sym "$SYMPHONY_URL/api/v1/projects" | jq -e --arg n "$EXISTING" 'any(.[]; .name == $n)' >/dev/null; then
  warn "$EXISTING pas découvert : voir la page Administration de Symphony"
elif deployed "$EXISTING" staging; then
  ok "$EXISTING déjà déployé en staging"
else
  ok "$EXISTING découvert dans le groupe services"
  image=$(sym "$SYMPHONY_URL/api/v1/projects/$EXISTING/image" || true)
  tag=$(echo "$image" | jq -r '[.tags[]? | select(. != "latest")][0] // empty' 2>/dev/null || true)
  if [ -n "$tag" ]; then
    id=$(jq -n --arg n "$EXISTING" --arg i "$(echo "$image" | jq -r .image)" --arg t "$tag"         '{project_name: $n, environment: "staging", image: $i, tag: $t}'       | sym -X POST --data @- "$SYMPHONY_URL/api/v1/deployments" | jq -r '.id // empty')
    [ -n "$id" ] && wait_task "$id" "déploiement de $EXISTING" && ok "$EXISTING déployé en staging"
  else
    warn "$EXISTING : pas d'image dans le registry, déploiement sauté"
  fi
fi

echo
sym "$SYMPHONY_URL/api/v1/deployments" | jq -r '.[] | select(.url != "") | "  \(.project) (\(.environment))\t\(.url)"' | sort
