#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

PUSH_REGISTRY="${ACR_PUSH_REGISTRY:-bluecity-bigdata-registry-registry.ap-southeast-1.cr.aliyuncs.com}"
DEPLOY_REGISTRY="${ACR_DEPLOY_REGISTRY:-bluecity-bigdata-registry-registry-vpc.ap-southeast-1.cr.aliyuncs.com}"
ACR_NAMESPACE="${ACR_NAMESPACE:-algo}"
ACR_REPOSITORY="${ACR_REPOSITORY:-mlops}"
BACKEND_REPOSITORY="${BACKEND_REPOSITORY:-$ACR_REPOSITORY}"
WEB_REPOSITORY="${WEB_REPOSITORY:-$ACR_REPOSITORY}"
ACR_USERNAME="${ACR_USERNAME:-algo-dev-acr-image@1018288888205211}"
PLATFORM="${PLATFORM:-linux/amd64}"
REMOTE_API_URL="${REMOTE_API_URL:-http://backend:8080}"
NEXT_PUBLIC_WS_URL="${NEXT_PUBLIC_WS_URL:-}"
DOCKER_BIN="${DOCKER_BIN:-docker}"

TAG="${TAG:-}"
LOGIN=false
PUSH=true
DRY_RUN=false
WRITE_COMPOSE_OVERRIDE=""

usage() {
  cat <<'EOF'
Usage:
  bash scripts/publish-acr-images.sh [options]

Options:
  --tag <version>                  Release tag, for example v0.1.0-company.1
  --login                          Run docker login before building/pushing
  --no-push                        Build images locally without pushing
  --dry-run                        Print commands without executing them
  --platform <platform>            Docker target platform, default linux/amd64
  --push-registry <registry>       Registry used for docker login/build/push
  --deploy-registry <registry>     Registry printed/written for deployment
  --namespace <namespace>          ACR namespace, default algo
  --repository <repository>        Shared repository, default mlops
  --backend-repository <repo>      Backend repository, default same as --repository
  --web-repository <repo>          Web repository, default same as --repository
  --username <username>            Docker login username
  --remote-api-url <url>           Dockerfile.web REMOTE_API_URL build arg
  --next-public-ws-url <url>       Dockerfile.web NEXT_PUBLIC_WS_URL build arg
  --write-compose-override <path>  Write a compose override with deploy images
  -h, --help                       Show this help

Environment overrides:
  ACR_PUSH_REGISTRY, ACR_DEPLOY_REGISTRY, ACR_NAMESPACE, ACR_REPOSITORY,
  BACKEND_REPOSITORY, WEB_REPOSITORY, ACR_USERNAME, TAG, PLATFORM,
  REMOTE_API_URL, NEXT_PUBLIC_WS_URL, DOCKER_BIN
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --tag)
      TAG="${2:?Missing value for --tag}"
      shift 2
      ;;
    --login)
      LOGIN=true
      shift
      ;;
    --no-push)
      PUSH=false
      shift
      ;;
    --dry-run)
      DRY_RUN=true
      shift
      ;;
    --platform)
      PLATFORM="${2:?Missing value for --platform}"
      shift 2
      ;;
    --push-registry)
      PUSH_REGISTRY="${2:?Missing value for --push-registry}"
      shift 2
      ;;
    --deploy-registry)
      DEPLOY_REGISTRY="${2:?Missing value for --deploy-registry}"
      shift 2
      ;;
    --namespace)
      ACR_NAMESPACE="${2:?Missing value for --namespace}"
      shift 2
      ;;
    --repository)
      ACR_REPOSITORY="${2:?Missing value for --repository}"
      BACKEND_REPOSITORY="$ACR_REPOSITORY"
      WEB_REPOSITORY="$ACR_REPOSITORY"
      shift 2
      ;;
    --backend-repository)
      BACKEND_REPOSITORY="${2:?Missing value for --backend-repository}"
      shift 2
      ;;
    --web-repository)
      WEB_REPOSITORY="${2:?Missing value for --web-repository}"
      shift 2
      ;;
    --username)
      ACR_USERNAME="${2:?Missing value for --username}"
      shift 2
      ;;
    --remote-api-url)
      REMOTE_API_URL="${2:?Missing value for --remote-api-url}"
      shift 2
      ;;
    --next-public-ws-url)
      NEXT_PUBLIC_WS_URL="${2:?Missing value for --next-public-ws-url}"
      shift 2
      ;;
    --write-compose-override)
      WRITE_COMPOSE_OVERRIDE="${2:?Missing value for --write-compose-override}"
      shift 2
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

# shellcheck disable=SC2206
DOCKER=($DOCKER_BIN)

sanitize_tag() {
  printf '%s' "$1" | sed -E 's#[/:@[:space:]]+#-#g; s/[^A-Za-z0-9_.-]+/-/g; s/^-+//; s/-+$//'
}

if [ -z "$TAG" ]; then
  if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    TAG="$(git describe --tags --always --dirty 2>/dev/null || git rev-parse --short HEAD)"
  else
    TAG="manual-$(date +%Y%m%d%H%M%S)"
  fi
fi

TAG="$(sanitize_tag "$TAG")"
if [ -z "$TAG" ]; then
  echo "Tag is empty after sanitization." >&2
  exit 1
fi

COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"

if [ "$BACKEND_REPOSITORY" = "$WEB_REPOSITORY" ]; then
  BACKEND_TAG="${BACKEND_TAG:-multica-backend-$TAG}"
  WEB_TAG="${WEB_TAG:-multica-web-$TAG}"
else
  BACKEND_TAG="${BACKEND_TAG:-$TAG}"
  WEB_TAG="${WEB_TAG:-$TAG}"
fi

PUSH_BACKEND_IMAGE="${PUSH_REGISTRY}/${ACR_NAMESPACE}/${BACKEND_REPOSITORY}:${BACKEND_TAG}"
PUSH_WEB_IMAGE="${PUSH_REGISTRY}/${ACR_NAMESPACE}/${WEB_REPOSITORY}:${WEB_TAG}"
DEPLOY_BACKEND_IMAGE="${DEPLOY_REGISTRY}/${ACR_NAMESPACE}/${BACKEND_REPOSITORY}:${BACKEND_TAG}"
DEPLOY_WEB_IMAGE="${DEPLOY_REGISTRY}/${ACR_NAMESPACE}/${WEB_REPOSITORY}:${WEB_TAG}"

run() {
  printf '+'
  printf ' %q' "$@"
  printf '\n'
  if [ "$DRY_RUN" != true ]; then
    "$@"
  fi
}

echo "==> Multica ACR image release"
echo "    tag:             $TAG"
echo "    commit:          $COMMIT"
echo "    platform:        $PLATFORM"
echo "    push registry:   $PUSH_REGISTRY"
echo "    deploy registry: $DEPLOY_REGISTRY"
echo "    backend image:   $PUSH_BACKEND_IMAGE"
echo "    web image:       $PUSH_WEB_IMAGE"
echo ""

run "${DOCKER[@]}" buildx version

if [ "$LOGIN" = true ]; then
  if [ "$DRY_RUN" = true ]; then
    echo "+ ${DOCKER_BIN} login --username ${ACR_USERNAME} --password-stdin ${PUSH_REGISTRY}"
  else
    read -r -s -p "ACR password for ${ACR_USERNAME}: " ACR_PASSWORD
    echo ""
    printf '%s' "$ACR_PASSWORD" | "${DOCKER[@]}" login \
      --username "$ACR_USERNAME" \
      --password-stdin "$PUSH_REGISTRY"
  fi
fi

OUTPUT_FLAG="--push"
if [ "$PUSH" != true ]; then
  OUTPUT_FLAG="--load"
fi

run "${DOCKER[@]}" buildx build \
  --platform "$PLATFORM" \
  --file Dockerfile \
  --build-arg "VERSION=$TAG" \
  --build-arg "COMMIT=$COMMIT" \
  --tag "$PUSH_BACKEND_IMAGE" \
  "$OUTPUT_FLAG" \
  .

run "${DOCKER[@]}" buildx build \
  --platform "$PLATFORM" \
  --file Dockerfile.web \
  --build-arg "REMOTE_API_URL=$REMOTE_API_URL" \
  --build-arg "NEXT_PUBLIC_WS_URL=$NEXT_PUBLIC_WS_URL" \
  --build-arg "NEXT_PUBLIC_APP_VERSION=$TAG" \
  --tag "$PUSH_WEB_IMAGE" \
  "$OUTPUT_FLAG" \
  .

if [ -n "$WRITE_COMPOSE_OVERRIDE" ]; then
  echo "==> Writing compose override: $WRITE_COMPOSE_OVERRIDE"
  if [ "$DRY_RUN" = true ]; then
    cat <<EOF
services:
  backend:
    image: $DEPLOY_BACKEND_IMAGE
  frontend:
    image: $DEPLOY_WEB_IMAGE
EOF
  else
    mkdir -p "$(dirname "$WRITE_COMPOSE_OVERRIDE")"
    cat > "$WRITE_COMPOSE_OVERRIDE" <<EOF
services:
  backend:
    image: $DEPLOY_BACKEND_IMAGE
  frontend:
    image: $DEPLOY_WEB_IMAGE
EOF
  fi
fi

RESULT_LABEL="Pushed"
if [ "$DRY_RUN" = true ]; then
  RESULT_LABEL="Dry-run images"
elif [ "$PUSH" != true ]; then
  RESULT_LABEL="Built locally"
fi

COMPOSE_OVERRIDE_HINT="${WRITE_COMPOSE_OVERRIDE:-docker-compose.images.yml}"

cat <<EOF

==> Release images
$RESULT_LABEL:
  $PUSH_BACKEND_IMAGE
  $PUSH_WEB_IMAGE

Use for deployment:
  $DEPLOY_BACKEND_IMAGE
  $DEPLOY_WEB_IMAGE

Deploy with compose override:
  docker compose -f docker-compose.selfhost.yml -f $COMPOSE_OVERRIDE_HINT up -d
EOF
