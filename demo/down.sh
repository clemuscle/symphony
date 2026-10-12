#!/usr/bin/env bash
# Arrête la démo Symphony.
#   ./demo/down.sh          arrête les conteneurs, garde les données (GitLab, base)
#   ./demo/down.sh --purge  supprime aussi les volumes et demo/.env : prochain up.sh = démo neuve
#
# Les services déployés par la démo (images du registry de la démo) sont
# supprimés ; les déploiements d'une autre instance de Symphony ne sont pas touchés.
# Ceux du runtime argocd vivent dans k3s : arrêtés avec lui, effacés par --purge.

set -euo pipefail
cd "$(dirname "$0")"

REGISTRY_PORT=$( [ -f .env ] && sed -n 's/^REGISTRY_PORT=//p' .env | tail -n1 || true)
REGISTRY_PORT=${REGISTRY_PORT:-5050}

docker ps -a --filter label=symphony=true --format '{{.ID}} {{.Image}}' \
  | awk -v prefix="localhost:${REGISTRY_PORT}/" 'index($2, prefix) == 1 {print $1}' \
  | xargs -r docker rm -f >/dev/null

if [ "${1:-}" = "--purge" ]; then
  docker compose --profile symphony --profile k8s down -v
  rm -rf .env k3s/output k3s/registries.yaml k3s/kubeconfig.yaml
  echo "Démo supprimée (volumes compris)."
else
  docker compose --profile symphony --profile k8s down
  echo "Démo arrêtée ; ./demo/up.sh la relance avec ses données."
fi
