#!/usr/bin/env bash
# Pousse les golden paths d'exemple qui conviennent à la forge (FORGE de .env)
# dans le repo TEMPLATES_REPO, sous templates/ : ceux de GitLab (sans celui de
# Kubernetes, qui demande Argo CD) ou ceux de GitHub (*-github).
#
#   ./push-templates.sh --dry-run   # ce qui serait poussé, sans rien pousser
#   ./push-templates.sh             # premier commit d'un repo vide (main)
#
# Le jeton passe par l'environnement de git, jamais dans une URL ni sur la
# ligne de commande.
set -euo pipefail
cd "$(dirname "$0")"
dry_run=false
[ "${1:-}" = "--dry-run" ] && dry_run=true

[ -f .env ] || { echo "pas de .env : cp .env.example .env, puis le remplir" >&2; exit 1; }
set -a; . ./.env; set +a
: "${FORGE_TOKEN:?FORGE_TOKEN vide dans .env}" "${TEMPLATES_REPO:?TEMPLATES_REPO vide dans .env}"

src=../demo/gitlab/symphony-templates/templates
case "${FORGE:-gitlab}" in
  gitlab)
    host=${FORGE_URL:-https://gitlab.com}; user=oauth2
    templates=$(cd "$src" && ls -d */ | tr -d / | grep -v -e '-github$' -e '-k8s$') ;;
  github)
    host=https://github.com; user=x-access-token
    templates=$(cd "$src" && ls -d *-github/ | tr -d /) ;;
  *) echo "FORGE=$FORGE : gitlab ou github" >&2; exit 1 ;;
esac
remote="${host%/}/$TEMPLATES_REPO.git"

echo "Repo : $remote (branche main, dossier templates/)"
echo "Golden paths : $(echo $templates)"
if $dry_run; then
  (cd "$src" && for t in $templates; do find "$t" -type f; done) | sed 's|^|  templates/|'
  echo "(dry-run : rien n'a été poussé)"
  exit 0
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/templates"
for t in $templates; do cp -r "$src/$t" "$work/templates/"; done
cd "$work"
git init -q -b main
git add .
git -c user.name=Symphony -c user.email=symphony@localhost commit -q -m "Golden paths d'exemple de Symphony"
auth=$(printf '%s:%s' "$user" "$FORGE_TOKEN" | base64 | tr -d '\n')
GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=http.extraHeader GIT_CONFIG_VALUE_0="Authorization: Basic $auth" \
  git push -q "$remote" main
echo "Poussé. Symphony les charge dans les 15 s (page Administration)."
