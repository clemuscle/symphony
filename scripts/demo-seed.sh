#!/usr/bin/env bash
# Symphony demo — seed de plusieurs projets à différents stades d'avancement.
#
# Utilise exclusivement l'API publique de Symphony (POST /api/v1/projects,
# /deployments, /projects/{name}/recettes) — aucune insertion SQL directe.
# Les statuts observés dans l'UI sont donc de vrais états traversés par la
# state machine PENDING → RUNNING → SUCCESS/FAILED
# (internal/database/deployments.go, pipelines.go), pas des valeurs
# cosmétiques.
#
# Garde-fou de sécurité (voir section Sécurité de .claude/CLAUDE.md) : ce
# script exige que les providers soient déjà configurés et refuse de
# démarrer sinon — SYMPHONY_DEV_MODE relâche l'authentification, jamais la
# configuration, et ce script ne doit jamais dispenser du wizard.
#
# Prérequis : DEMO.md jusqu'à l'étape 9 incluse (wizard rempli par toi).
#
# Usage : ./scripts/demo-seed.sh   (depuis la racine du repo)
# Variables : SYMPHONY_URL (défaut http://localhost:8090), NAMESPACE (défaut
# symphony-demo, le groupe créé à l'étape 4 de DEMO.md), GOLDEN_PATH (défaut
# go — l'image golang:1.22-alpine est la plus légère/rapide à builder pour
# une démo sur une machine aux ressources limitées).

set -euo pipefail

SYMPHONY_URL="${SYMPHONY_URL:-http://localhost:8090}"
NAMESPACE="${NAMESPACE:-symphony-demo}"
GOLDEN_PATH="${GOLDEN_PATH:-go}"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
info()  { echo -e "${BLUE}[seed]${NC} $*"; }
ok()    { echo -e "${GREEN}[seed]${NC} ✓ $*"; }
warn()  { echo -e "${YELLOW}[seed]${NC} ⚠ $*"; }
fail()  { echo -e "${RED}[seed]${NC} ✗ $*" >&2; exit 1; }

command -v curl >/dev/null || fail "curl est requis"
command -v jq   >/dev/null || fail "jq est requis (https://jqlang.org)"

# ── 0. Garde-fou — providers déjà configurés par toi, jamais par ce script ──

configured=$(curl -sf "$SYMPHONY_URL/api/v1/setup/status" | jq -r '.configured // false')
if [ "$configured" != "true" ]; then
  fail "Providers non configurés. Termine le wizard Symphony (DEMO.md, étape 9) toi-même avant de lancer ce script — il ne configure jamais les providers à ta place (voir section Sécurité de .claude/CLAUDE.md)."
fi
ok "Providers déjà configurés — c'est toi qui as rempli le wizard, pas ce script"

# ── Aides ─────────────────────────────────────────────────────────────────

create_project() {
  local name="$1" desc="$2"
  info "Création du projet « $name » ($desc)…"
  local resp status
  resp=$(curl -sf -w '\n%{http_code}' -X POST "$SYMPHONY_URL/api/v1/projects" \
    -H 'Content-Type: application/json' \
    -d "$(jq -nc --arg name "$name" --arg desc "$desc" --arg ns "$NAMESPACE" --arg lang "$GOLDEN_PATH" \
      '{name:$name, description:$desc, namespace:$ns, language:$lang, type:"rest-api"}')")
  status=$(echo "$resp" | tail -n1)
  if [ "$status" != "201" ]; then
    echo "$resp" | head -n -1
    fail "création de $name: HTTP $status"
  fi
  ok "$name créé"
}

project_status() {
  curl -sf "$SYMPHONY_URL/api/v1/projects" | jq -r --arg n "$1" '.[] | select(.name==$n) | .status'
}

# wait_for_image attend que le pipeline automatique déclenché par le push
# initial (GitLab, hors du suivi de pipelines de Symphony — voir note dans
# DEMO.md §11) ait poussé une image :latest. C'est la seule preuve côté
# Symphony qu'un build a réellement réussi : Symphony ne trace dans sa
# propre table pipelines que les pipelines qu'il déclenche lui-même
# (déploiements/recettes), jamais les pipelines déclenchés par un push git.
wait_for_image() {
  local name="$1" timeout="${2:-900}" waited=0
  info "Attente de l'image :latest de « $name » (build automatique GitLab, jusqu'à ${timeout}s)…"
  while true; do
    local has_latest
    has_latest=$(curl -sf "$SYMPHONY_URL/api/v1/projects/$name/images" | jq -r 'any(.[]; .tag=="latest")')
    if [ "$has_latest" = "true" ]; then
      ok "$name: image :latest disponible"
      return 0
    fi
    if [ "$waited" -ge "$timeout" ]; then
      fail "$name: pas d'image :latest après ${timeout}s — vérifie le pipeline dans GitLab (CI/CD → Pipelines) pour ce projet"
    fi
    printf "."
    sleep 15
    waited=$((waited+15))
  done
}

wait_for_deployment_status() {
  local name="$1" want="$2" timeout="${3:-300}" waited=0
  info "Attente que le déploiement de « $name » atteigne « $want » (jusqu'à ${timeout}s)…"
  while true; do
    local status
    status=$(curl -sf "$SYMPHONY_URL/api/v1/deployments" | jq -r --arg n "$name" '[.[] | select(.project_name==$n)] | first | .status // "pending"')
    if [ "$status" = "$want" ]; then
      ok "$name: déploiement → $want"
      return 0
    fi
    if [ "$waited" -ge "$timeout" ]; then
      warn "$name: déploiement toujours « $status » après ${timeout}s (attendu « $want ») — état affiché tel quel, pas bloquant pour la suite"
      return 0
    fi
    printf "."
    sleep 10
    waited=$((waited+10))
  done
}

wait_for_recette_status() {
  local name="$1" recette="$2" want="$3" timeout="${4:-300}" waited=0
  info "Attente que la recette « $recette » de « $name » atteigne « $want » (jusqu'à ${timeout}s)…"
  while true; do
    local status
    status=$(curl -sf "$SYMPHONY_URL/api/v1/projects/$name/recettes" | jq -r --arg r "$recette" '.[] | select(.recette_name==$r) | .status')
    if [ "$status" = "$want" ]; then
      ok "$name/$recette: recette → $want"
      return 0
    fi
    if [ "$waited" -ge "$timeout" ]; then
      warn "$name/$recette: recette toujours « $status » après ${timeout}s (attendu « $want ») — état affiché tel quel, pas bloquant pour la suite"
      return 0
    fi
    printf "."
    sleep 10
    waited=$((waited+10))
  done
}

# ── 1. demo-fresh — juste provisionné, aucun pipeline déclenché via Symphony ─
#
# Symphony ne trace jamais le pipeline automatique du push initial (voir
# wait_for_image ci-dessus) : ce projet affichera durablement "aucun
# pipeline lancé" dans sa fiche Symphony, quel que soit le temps écoulé —
# ce n'est pas une fenêtre de course, c'est l'état stable réel.

create_project "demo-fresh" "Fraîchement créé, rien déclenché depuis Symphony pour l'instant"

# ── 2. demo-built — build automatique réussi, aucun déploiement ─────────────

create_project "demo-built" "Build automatique réussi (image disponible), pas encore déployé"
wait_for_image "demo-built"

# ── 3. demo-recette — recette déployée et active ─────────────────────────────

create_project "demo-recette" "Recette de test déployée et active"
wait_for_image "demo-recette"
info "Déploiement d'une recette pour « demo-recette »…"
curl -sf -X POST "$SYMPHONY_URL/api/v1/projects/demo-recette/recettes" \
  -H 'Content-Type: application/json' \
  -d '{"recette_name":"demo","port":9101}' > /dev/null
wait_for_recette_status "demo-recette" "demo" "running"

# ── 4. demo-failed — déploiement déclenché avant tout build : échec réel ────
#
# Aucune image n'existe encore pour ce projet à cet instant (contrairement à
# demo-built/demo-recette, on ne l'attend pas) — le job "deploy" du golden
# path exécute "docker pull $CI_REGISTRY_IMAGE:latest", qui échoue faute
# d'image, faisant échouer le pipeline puis le déploiement pour de vrai.

create_project "demo-failed" "Déploiement tenté avant qu'un build ait eu le temps de finir — échec réel"
info "Déploiement immédiat de « demo-failed » (avant tout build)…"
curl -sf -X POST "$SYMPHONY_URL/api/v1/deployments" \
  -H 'Content-Type: application/json' \
  -d '{"project_name":"demo-failed","environment":"prod"}' > /dev/null
wait_for_deployment_status "demo-failed" "failed" 300

echo ""
ok "Seed terminé — 4 projets créés : demo-fresh, demo-built, demo-recette, demo-failed"
info "Ouvre $SYMPHONY_URL pour les observer, ou relance ce script à tout moment (les noms sont fixes — le supprimer d'abord dans Symphony pour repartir de zéro sur un projet donné)."
