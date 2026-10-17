#!/usr/bin/env bash
# Données du mode visite (frontend/src/visit/fixtures.json) : réponses de l'API
# d'une démo lancée (./demo/up.sh), lues en carol (viewer). Ports ramenés à ceux
# par défaut ; l'utilisateur affiché est « Visiteur » (viewer).
#   ./demo/visit/capture-fixtures.sh
set -euo pipefail
cd "$(dirname "$0")/.."
saved() { sed -n "s/^$1=//p" .env | tail -n1; }
S="http://localhost:$(saved SYMPHONY_PORT)"
token=$(curl -sf -d grant_type=password -d client_id=symphony -d username=carol -d password=carol \
  "http://localhost:$(saved KEYCLOAK_PORT)/realms/symphony/protocol/openid-connect/token" | jq -r .access_token)
get() { curl -sf -H "Authorization: Bearer $token" "$S/api/v1$1"; }

projects=$(get /projects)
images=$(for p in $(echo "$projects" | jq -r '.[].name'); do
  jq -n --arg n "$p" --argjson i "$(get "/projects/$p/image")" '{($n): $i}'
done | jq -s 'add // {}')

jq -n \
  --argjson projects "$projects" \
  --argjson deployments "$(get /deployments)" \
  --argjson stacks "$(get /stacks)" \
  --argjson environments "$(get /environments)" \
  --argjson tasks "$(get '/tasks?limit=200')" \
  --argjson groups "$(get /identity/groups)" \
  --argjson providers "$(get /providers)" \
  --argjson images "$images" \
  '{
    "/me": {auth_enabled: true, user: {subject: "visite", name: "Visiteur", email: "", groups: [], teams: [], role: "viewer"}},
    "/projects": $projects, "/deployments": $deployments, "/stacks": $stacks,
    "/environments": $environments, "/tasks": $tasks, "/identity/groups": $groups,
    "/providers": $providers, images: $images
  }' \
  | sed -e "s/:$(saved GITLAB_PORT)/:8929/g" -e "s/:$(saved K8S_HTTP_PORT)/:8081/g" \
        -e "s/:$(saved ARGOCD_PORT)/:8088/g" -e "s/:$(saved REGISTRY_PORT)/:5050/g" \
        -e "s/\"port\": $(saved K8S_HTTP_PORT)/\"port\": 8081/g" \
  > ../frontend/src/visit/fixtures.json
echo "frontend/src/visit/fixtures.json : $(jq '."/projects" | length' ../frontend/src/visit/fixtures.json) projets"
