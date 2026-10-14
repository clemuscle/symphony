#!/usr/bin/env bash
# Démo Symphony, de zéro à une instance utilisable, en une commande :
#   1. vérifie les prérequis
#   2. démarre PostgreSQL, GitLab CE et le runner
#   3. initialise GitLab : Auto DevOps désactivé, token API, groupes platform
#      (symphony-config, symphony-templates, symphony-deployments : contenu de
#      demo/gitlab/) et apps (les projets), comptes du realm Keycloak et leurs
#      droits, runner enregistré
#   4. démarre Kubernetes (k3s) et y installe Argo CD, avec un compte pour
#      Symphony et l'accès au repo platform/symphony-deployments et au registry
#   5. démarre Keycloak (realm symphony : alice admin, bob developer,
#      carol viewer, dave sans rôle), qui sert aussi à se connecter à GitLab
#   6. démarre Symphony et attend qu'il ait chargé les golden paths
#   7. crée et déploie, via l'API de Symphony, les projets de demo/seed/ :
#      de vrais repos, pipelines et services joignables (~5 min)
#
# Rejouable : chaque étape vérifie ce qui existe déjà.
# --no-k8s : sans Kubernetes (~1,5 Go de RAM en moins) ; les projets de démo
#   prévus sur Kubernetes sont alors déployés sur Docker.
# --no-seed : sans les projets de démo (on les crée pendant la démo).
# Ports réglables : GITLAB_PORT=18929 REGISTRY_PORT=15050 GITLAB_SSH_PORT=12224 \
#   SYMPHONY_PORT=18080 KEYCLOAK_PORT=18180 K8S_HTTP_PORT=18081 ARGOCD_PORT=18088 \
#   K8S_API_PORT=16443 ./demo/up.sh
# Arrêt : ./demo/down.sh

set -euo pipefail
cd "$(dirname "$0")"

K8S=true
SEED=true
for arg in "$@"; do
  case $arg in
    --no-k8s) K8S=false ;;
    --no-seed) SEED=false ;;
    *) echo "usage : $0 [--no-k8s] [--no-seed]" >&2; exit 2 ;;
  esac
done
ARGOCD_VERSION=v3.1.8

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
K8S_HTTP_PORT=${K8S_HTTP_PORT:-$(saved K8S_HTTP_PORT)}; K8S_HTTP_PORT=${K8S_HTTP_PORT:-8081}
ARGOCD_PORT=${ARGOCD_PORT:-$(saved ARGOCD_PORT)}; ARGOCD_PORT=${ARGOCD_PORT:-8088}
K8S_API_PORT=${K8S_API_PORT:-$(saved K8S_API_PORT)}; K8S_API_PORT=${K8S_API_PORT:-6443}
SYMPHONY_IMAGE=${SYMPHONY_IMAGE:-$(saved SYMPHONY_IMAGE)}; SYMPHONY_IMAGE=${SYMPHONY_IMAGE:-ghcr.io/clemuscle/symphony:latest}
GITLAB_TOKEN=$(saved GITLAB_TOKEN)
GITLAB_ROOT_PASSWORD=$(saved GITLAB_ROOT_PASSWORD)
K8S_GITLAB_TOKEN=$(saved K8S_GITLAB_TOKEN)
ARGOCD_TOKEN=$(saved ARGOCD_TOKEN)
random() { head -c 64 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c "$1"; }
GITLAB_ROOT_PASSWORD=${GITLAB_ROOT_PASSWORD:-Sy-$(random 20)}
KEYCLOAK_ADMIN_PASSWORD=$(saved KEYCLOAK_ADMIN_PASSWORD); KEYCLOAK_ADMIN_PASSWORD=${KEYCLOAK_ADMIN_PASSWORD:-$(random 20)}
SESSION_SECRET=$(saved SESSION_SECRET); SESSION_SECRET=${SESSION_SECRET:-$(random 48)}
WEBHOOK_SECRET=$(saved WEBHOOK_SECRET); WEBHOOK_SECRET=${WEBHOOK_SECRET:-$(random 32)}
GITLAB_OIDC_SECRET=$(saved GITLAB_OIDC_SECRET); GITLAB_OIDC_SECRET=${GITLAB_OIDC_SECRET:-$(random 32)}

GITLAB_URL="http://localhost:${GITLAB_PORT}"
SYMPHONY_URL="http://localhost:${SYMPHONY_PORT}"
KEYCLOAK_URL="http://localhost:${KEYCLOAK_PORT}"
ARGOCD_URL="http://localhost:${ARGOCD_PORT}"

write_env() {
  cat > .env <<EOF
# Écrit par demo/up.sh — ne pas versionner (contient un token)
GITLAB_PORT=${GITLAB_PORT}
REGISTRY_PORT=${REGISTRY_PORT}
GITLAB_SSH_PORT=${GITLAB_SSH_PORT}
SYMPHONY_PORT=${SYMPHONY_PORT}
KEYCLOAK_PORT=${KEYCLOAK_PORT}
K8S_HTTP_PORT=${K8S_HTTP_PORT}
ARGOCD_PORT=${ARGOCD_PORT}
K8S_API_PORT=${K8S_API_PORT}
SYMPHONY_IMAGE=${SYMPHONY_IMAGE}
KEYCLOAK_ADMIN_PASSWORD=${KEYCLOAK_ADMIN_PASSWORD}
SESSION_SECRET=${SESSION_SECRET}
WEBHOOK_SECRET=${WEBHOOK_SECRET}
GITLAB_OIDC_SECRET=${GITLAB_OIDC_SECRET}
GITLAB_ROOT_PASSWORD=${GITLAB_ROOT_PASSWORD}
GITLAB_TOKEN=${GITLAB_TOKEN}
K8S_GITLAB_TOKEN=${K8S_GITLAB_TOKEN}
ARGOCD_TOKEN=${ARGOCD_TOKEN}
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
  vars="GITLAB_PORT REGISTRY_PORT GITLAB_SSH_PORT SYMPHONY_PORT KEYCLOAK_PORT"
  [ "$K8S" = true ] && vars="$vars K8S_HTTP_PORT ARGOCD_PORT K8S_API_PORT"
  for v in $vars; do
    p=${!v}
    ss -ltn | awk '{print $4}' | grep -q ":${p}\$" \
      && fail "port ${p} déjà utilisé ; en choisir un autre, ex. ${v}=1${p: -4} ./demo/up.sh"
  done
fi

write_env

# Realm Keycloak : les URL de retour dépendent des ports des applications
mkdir -p keycloak/import
# (Symphony, GitLab), comme le secret du client gitlab
sed -e "s|__SYMPHONY_URL__|${SYMPHONY_URL}|g" -e "s|__GITLAB_URL__|${GITLAB_URL}|g" \
  -e "s|__GITLAB_OIDC_SECRET__|${GITLAB_OIDC_SECRET}|g" \
  keycloak/realm-symphony.template.json > keycloak/import/realm-symphony.json
chmod 600 keycloak/import/realm-symphony.json

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

# Webhooks vers Symphony (http://symphony:8080, adresse privée) : GitLab les
# refuse par défaut
api -X PUT "$GITLAB_URL/api/v4/application/settings" \
  -d "allow_local_requests_from_web_hooks_and_services=true" | jq -e .id >/dev/null \
  || fail "réglage des webhooks de GitLab impossible"
ok "Webhooks de GitLab vers Symphony autorisés"

urlenc() { jq -rn --arg s "$1" '$s|@uri'; }

# Groupes : platform pour les repos de la plateforme (équipe plateforme),
# apps pour les projets créés par Symphony (ouverts aux développeurs)
group_id() { api "$GITLAB_URL/api/v4/groups/$1" | jq -r '.id // empty'; }
ensure_group() {
  local path=$1 description=$2
  if [ -n "$(group_id "$path")" ]; then
    ok "Groupe $path déjà présent"
    return
  fi
  api -X POST "$GITLAB_URL/api/v4/groups" -d "name=$path" -d "path=$path" \
    -d "visibility=private" --data-urlencode "description=$description" \
    | jq -e .id >/dev/null || fail "création du groupe $path impossible"
  ok "Groupe $path créé"
}
ensure_group platform "Configuration de Symphony : golden paths, repo de déploiement"
ensure_group apps "Projets créés par Symphony"

# Pousse le contenu de demo/gitlab/<repo>/ dans platform/<repo>, en un commit
seed_repo() {
  local repo=$1 dir=gitlab/$1 path=platform/$1
  if [ "$(api -o /dev/null -w '%{http_code}' "$GITLAB_URL/api/v4/projects/$(urlenc "$path")")" = 200 ]; then
    ok "$path déjà présent"
    return
  fi
  api -X POST "$GITLAB_URL/api/v4/projects" -d "name=$repo" -d "namespace_id=$(group_id platform)" \
    -d "visibility=private" | jq -e .id >/dev/null || fail "création de $path impossible"
  local actions
  actions=$(cd "$dir" && find . -type f | sort | while read -r f; do
    jq -n --arg p "${f#./}" --rawfile c "$f" '{action: "create", file_path: $p, content: $c}'
  done | jq -s .)
  jq -n --argjson a "$actions" \
    '{branch: "main", commit_message: "feat: contenu initial de la démo [symphony]", actions: $a}' \
    | api -X POST -H "Content-Type: application/json" --data @- \
      "$GITLAB_URL/api/v4/projects/$(urlenc "$path")/repository/commits" \
    | jq -e .id >/dev/null || fail "push du contenu de $path impossible"
  ok "$path créé ($(echo "$actions" | jq length) fichiers)"
}
seed_repo symphony-config
seed_repo symphony-templates
seed_repo symphony-deployments

# Comptes du realm Keycloak : créés ici avec leur e-mail, GitLab les lie à
# Keycloak à la première connexion (bouton « Keycloak »). Droits : alice
# (admin) tient la plateforme, bob (developer) travaille dans apps, carol
# (viewer) y lit ; dave n'a accès à rien.
# Niveaux GitLab : 20 Reporter, 30 Developer, 40 Maintainer, 50 Owner
ensure_member() {
  local group=$1 user_id=$2 level=$3
  api -X POST "$GITLAB_URL/api/v4/groups/$group/members" -d "user_id=$user_id" -d "access_level=$level" >/dev/null
  api -X PUT "$GITLAB_URL/api/v4/groups/$group/members/$user_id" -d "access_level=$level" >/dev/null
}
for u in alice:Alice_Admin bob:Bob_Developer carol:Carol_Viewer dave:Dave_SansRole; do
  username=${u%%:*} name=${u#*:}
  id=$(api "$GITLAB_URL/api/v4/users?username=$username" | jq -r '.[0].id // empty')
  if [ -z "$id" ]; then
    id=$(api -X POST "$GITLAB_URL/api/v4/users" -d "username=$username" -d "name=${name/_/ }" \
      -d "email=$username@demo.symphony" -d "password=Sy-$(random 24)" -d "skip_confirmation=true" \
      | jq -r '.id // empty')
    [ -n "$id" ] || fail "création du compte GitLab $username impossible"
  fi
  case $username in
    alice) ensure_member platform "$id" 50; ensure_member apps "$id" 50 ;;
    bob)   ensure_member apps "$id" 30 ;;
    carol) ensure_member apps "$id" 20 ;;
  esac
done
ok "Comptes GitLab alice, bob, carol, dave (connexion par Keycloak) et leurs droits"

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

# ── 4. Kubernetes : k3s et Argo CD ────────────────────────────────────────────

kube() { $COMPOSE exec -T k3s kubectl "$@"; }

if [ "$K8S" = true ]; then
  # Jeton en lecture seule pour le cluster : containerd tire les images du
  # registry, Argo CD lit platform/symphony-deployments
  if [ -n "$K8S_GITLAB_TOKEN" ] && curl -sf -o /dev/null -u "root:$K8S_GITLAB_TOKEN" \
      "$GITLAB_URL/platform/symphony-deployments.git/info/refs?service=git-upload-pack"; then
    ok "Jeton de lecture du cluster déjà en place"
  else
    root_id=$(api "$GITLAB_URL/api/v4/user" | jq -r .id)
    K8S_GITLAB_TOKEN=$(api -X POST "$GITLAB_URL/api/v4/users/$root_id/personal_access_tokens" \
      -d "name=symphony-k8s" -d "scopes[]=read_registry" -d "scopes[]=read_repository" \
      -d "expires_at=$(date -d '+364 days' +%F)" | jq -r '.token // empty')
    [ -n "$K8S_GITLAB_TOKEN" ] || fail "création du jeton de lecture du cluster impossible"
    write_env
    ok "Jeton de lecture du cluster créé (read_registry, read_repository)"
  fi

  # Registry de GitLab vu par containerd : en HTTP, avec ce jeton
  mkdir -p k3s/output
  registries="# Écrit par demo/up.sh — ne pas versionner (contient un jeton)
mirrors:
  \"localhost:${REGISTRY_PORT}\":
    endpoint: [\"http://localhost:${REGISTRY_PORT}\"]
configs:
  \"localhost:${REGISTRY_PORT}\":
    auth:
      username: root
      password: ${K8S_GITLAB_TOKEN}"
  restart_k3s=false
  if [ "$(cat k3s/registries.yaml 2>/dev/null)" != "$registries" ]; then
    echo "$registries" > k3s/registries.yaml
    chmod 600 k3s/registries.yaml
    restart_k3s=true
  fi

  info "Démarrage de Kubernetes (k3s)…"
  $COMPOSE --profile k8s up -d k3s gitlab-proxy >/dev/null
  [ "$restart_k3s" = true ] && $COMPOSE --profile k8s restart k3s >/dev/null
  for i in $(seq 60); do
    kube get --raw=/readyz >/dev/null 2>&1 && break
    [ "$i" = 60 ] && fail "k3s ne répond pas : $COMPOSE logs k3s"
    sleep 3
  done
  sed "s|https://127.0.0.1:6443|https://localhost:${K8S_API_PORT}|" k3s/output/kubeconfig.yaml > k3s/kubeconfig.yaml
  ok "Kubernetes prêt (KUBECONFIG=demo/k3s/kubeconfig.yaml)"

  info "Installation d'Argo CD ${ARGOCD_VERSION} (1 à 2 min)…"
  kube create namespace argocd --dry-run=client -o yaml | kube apply -f - >/dev/null
  curl -sfL "https://raw.githubusercontent.com/argoproj/argo-cd/${ARGOCD_VERSION}/manifests/install.yaml" \
    | kube apply -n argocd --server-side --force-conflicts -f - >/dev/null \
    || fail "installation d'Argo CD impossible"
  kube apply --server-side --force-conflicts --field-manager=symphony-demo -f - < k3s/argocd-config.yaml >/dev/null
  # Accès au repo de déploiement (le jeton reste dans le cluster, pas dans Git)
  kube -n argocd create secret generic symphony-deployments-repo \
    --from-literal=type=git \
    --from-literal=url="http://k3s:${GITLAB_PORT}/platform/symphony-deployments.git" \
    --from-literal=username=root --from-literal=password="$K8S_GITLAB_TOKEN" \
    --dry-run=client -o yaml \
    | kube label --local -f - argocd.argoproj.io/secret-type=repository -o yaml \
    | kube apply -f - >/dev/null
  # server.insecure n'est lu qu'au démarrage
  kube -n argocd rollout restart deployment/argocd-server >/dev/null
  for d in deployment/argocd-server deployment/argocd-repo-server statefulset/argocd-application-controller; do
    kube -n argocd rollout status "$d" --timeout=300s >/dev/null || fail "Argo CD ne démarre pas ($d)"
  done
  for i in $(seq 60); do
    curl -sf -o /dev/null "$ARGOCD_URL/healthz" && break
    [ "$i" = 60 ] && fail "Argo CD ne répond pas sur $ARGOCD_URL"
    sleep 2
  done

  if [ -n "$ARGOCD_TOKEN" ] && curl -sf -o /dev/null -H "Authorization: Bearer $ARGOCD_TOKEN" "$ARGOCD_URL/api/v1/applications"; then
    ok "Jeton Argo CD de Symphony déjà en place"
  else
    admin_pw=$(kube -n argocd get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d)
    session=$(jq -n --arg p "$admin_pw" '{username: "admin", password: $p}' \
      | curl -sf -H "Content-Type: application/json" --data @- "$ARGOCD_URL/api/v1/session" | jq -r '.token // empty')
    [ -n "$session" ] || fail "connexion admin à Argo CD impossible"
    ARGOCD_TOKEN=$(curl -sf -X POST -H "Authorization: Bearer $session" -H "Content-Type: application/json" \
      -d '{"id": "symphony-demo"}' "$ARGOCD_URL/api/v1/account/symphony/token" | jq -r '.token // empty')
    [ -n "$ARGOCD_TOKEN" ] || fail "création du jeton Argo CD de Symphony impossible"
    write_env
    ok "Compte Argo CD de Symphony et son jeton créés"
  fi
  ok "Argo CD prêt : $ARGOCD_URL"
else
  warn "Sans Kubernetes (--no-k8s) : seul le runtime docker-local est utilisable"
fi

# ── 5. Keycloak ───────────────────────────────────────────────────────────────

for i in $(seq 60); do
  curl -sf -o /dev/null "$KEYCLOAK_URL/realms/symphony/.well-known/openid-configuration" && break
  [ "$i" = 60 ] && fail "Keycloak ne répond pas : $COMPOSE logs keycloak"
  sleep 3
done
ok "Keycloak prêt : $KEYCLOAK_URL (realm symphony)"

# ── 6. Symphony ───────────────────────────────────────────────────────────────

info "Démarrage de Symphony ($SYMPHONY_IMAGE)…"
$COMPOSE --profile symphony up -d symphony >/dev/null
# L'API exige une session : on vérifie avec un jeton de carol (viewer)
token=$(curl -sf -d grant_type=password -d client_id=symphony -d username=carol -d password=carol \
  "$KEYCLOAK_URL/realms/symphony/protocol/openid-connect/token" | jq -r .access_token)
[ -n "$token" ] && [ "$token" != null ] || fail "jeton de démo impossible à obtenir auprès de Keycloak"
count() { curl -sf -H "Authorization: Bearer $token" "$SYMPHONY_URL/api/v1/$1" 2>/dev/null | jq length 2>/dev/null || echo 0; }
for i in $(seq 60); do
  n=$(count golden-paths)
  [ "${n:-0}" -gt 0 ] && break
  [ "$i" = 60 ] && fail "Symphony n'a pas chargé les golden paths : $COMPOSE logs symphony"
  sleep 2
done
ok "Symphony prêt : $n golden paths au catalogue"

# ── 7. Projets de démo ────────────────────────────────────────────────────────

if [ "$SEED" = true ]; then
  ./seed.sh "$K8S" || warn "projets de démo incomplets : voir la page Activité de Symphony, puis relancer ./demo/up.sh"
else
  warn "Sans projets de démo (--no-seed)"
fi

cat <<EOF

$(echo -e "${GREEN}Démo prête${NC}")

  Symphony   $SYMPHONY_URL
             alice (admin) · bob (developer) · carol (viewer) · dave (sans rôle)
             mot de passe = identifiant
  GitLab     $GITLAB_URL   bouton « Keycloak », mêmes comptes ; root / $GITLAB_ROOT_PASSWORD
             groupes platform (config de Symphony) et apps (projets)
  Keycloak   $KEYCLOAK_URL/admin   (admin / $KEYCLOAK_ADMIN_PASSWORD)
EOF
if [ "$K8S" = true ]; then
  cat <<EOF
  Argo CD    $ARGOCD_URL   (admin / $(kube -n argocd get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d))
  Services   http://<service>.localhost:$K8S_HTTP_PORT   (runtime argocd-k3s)
  kubectl    KUBECONFIG=demo/k3s/kubeconfig.yaml kubectl get pods -n symphony-apps
EOF
fi
cat <<EOF
  Métriques  $SYMPHONY_URL/metrics

  Scénario : demo/SCENARIO.md      Arrêt : ./demo/down.sh

EOF
