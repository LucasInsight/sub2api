#!/usr/bin/env bash
# Build and optionally push Sub2API Docker images.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

DEFAULT_IMAGE="xeasydata.cn:5005/sub2api:latest"

IMAGE="${IMAGE:-${DEFAULT_IMAGE}}"
PLATFORM="${PLATFORM:-linux/amd64}"
DOCKERFILE="${DOCKERFILE:-${SCRIPT_DIR}/Dockerfile}"
CONTEXT="${CONTEXT:-${SCRIPT_DIR}}"
GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"
GOSUMDB="${GOSUMDB:-sum.golang.google.cn}"
NPM_REGISTRY="${NPM_REGISTRY:-https://registry.npmmirror.com}"
PNPM_VERSION="${PNPM_VERSION:-9.15.9}"
PROGRESS="${PROGRESS:-auto}"

PUSH=false
PUSH_ONLY=false
USE_BUILDX=false
LOAD=false
NO_CACHE=false
PULL=false
DRY_RUN=false
TARGET=""
VERSION="${VERSION:-}"
COMMIT="${COMMIT:-}"
DATE="${DATE:-}"

EXTRA_TAGS=()
EXTRA_BUILD_ARGS=()
EXTRA_LABELS=()

usage() {
  cat <<USAGE
Usage:
  ./docker_image.sh [options]

Defaults:
  image       ${DEFAULT_IMAGE}
  platform    linux/amd64
  dockerfile  ./Dockerfile
  context     .

Options:
  -i, --image IMAGE          Primary image tag.
  -t, --tag IMAGE            Add another image tag. Can be repeated.
      --platform PLATFORM    Build platform, e.g. linux/amd64 or linux/amd64,linux/arm64.
      --dockerfile FILE      Dockerfile path.
      --context DIR          Docker build context.
      --target STAGE         Build a specific Dockerfile target.
      --goproxy VALUE        GOPROXY build arg.
      --gosumdb VALUE        GOSUMDB build arg.
      --npm-registry VALUE   NPM_REGISTRY build arg.
      --pnpm-version VALUE   PNPM_VERSION build arg.
      --version VALUE        VERSION build arg.
      --commit VALUE         COMMIT build arg. Defaults to current git commit.
      --date VALUE           DATE build arg. Defaults to current UTC time.
      --build-arg KEY=VALUE  Add an extra Docker build arg. Can be repeated.
      --label KEY=VALUE      Add an image label. Can be repeated.
      --progress VALUE       Docker build progress mode: auto, plain, tty, quiet.
      --no-cache             Build without cache.
      --pull                 Always attempt to pull newer base images.
      --buildx               Use docker buildx.
      --load                 Load buildx single-platform output into local Docker.
      --push                 Push after a successful build.
      --push-only            Skip build and push the configured tags.
      --dry-run              Print commands without executing them.
  -h, --help                 Show this help.

Examples:
  ./docker_image.sh
  ./docker_image.sh --push
  ./docker_image.sh --image xeasydata.cn:5005/sub2api:dev --push
  ./docker_image.sh --tag xeasydata.cn:5005/sub2api:dev-20260627 --push
  ./docker_image.sh --platform linux/amd64,linux/arm64 --push
  ./docker_image.sh --push-only
USAGE
}

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

require_value() {
  local option="$1"
  shift
  [[ $# -gt 0 ]] || die "${option} requires a value"
  printf '%s' "$1"
}

print_cmd() {
  printf '+'
  printf ' %q' "$@"
  printf '\n'
}

run_cmd() {
  print_cmd "$@"
  if [[ "${DRY_RUN}" != "true" ]]; then
    "$@"
  fi
}

is_multi_platform() {
  [[ "${PLATFORM}" == *,* ]]
}

default_commit() {
  local commit=""
  commit="$(git -C "${SCRIPT_DIR}" rev-parse --short=12 HEAD 2>/dev/null || true)"
  if [[ -n "${commit}" ]] && git -C "${SCRIPT_DIR}" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    if ! git -C "${SCRIPT_DIR}" diff --quiet --ignore-submodules -- 2>/dev/null ||
      ! git -C "${SCRIPT_DIR}" diff --cached --quiet --ignore-submodules -- 2>/dev/null; then
      commit="${commit}-dirty"
    fi
  fi
  printf '%s' "${commit:-docker}"
}

add_optional_args() {
  local item

  if [[ ${#EXTRA_BUILD_ARGS[@]} -gt 0 ]]; then
    for item in "${EXTRA_BUILD_ARGS[@]}"; do
      BUILD_ARGS+=(--build-arg "${item}")
    done
  fi

  if [[ ${#EXTRA_LABELS[@]} -gt 0 ]]; then
    for item in "${EXTRA_LABELS[@]}"; do
      BUILD_ARGS+=(--label "${item}")
    done
  fi
}

push_local_tag() {
  local tag="$1"

  if [[ "${DRY_RUN}" == "true" ]]; then
    run_cmd docker image inspect "${tag}"
  else
    docker image inspect "${tag}" >/dev/null || die "local image not found: ${tag}"
  fi
  run_cmd docker push "${tag}"
}

while [[ $# -gt 0 ]]; do
  option="$1"
  case "${option}" in
    -i|--image)
      shift
      IMAGE="$(require_value "${option}" "$@")"
      ;;
    -t|--tag)
      shift
      EXTRA_TAGS+=("$(require_value "${option}" "$@")")
      ;;
    --platform)
      shift
      PLATFORM="$(require_value "${option}" "$@")"
      ;;
    --dockerfile)
      shift
      DOCKERFILE="$(require_value "${option}" "$@")"
      ;;
    --context)
      shift
      CONTEXT="$(require_value "${option}" "$@")"
      ;;
    --target)
      shift
      TARGET="$(require_value "${option}" "$@")"
      ;;
    --goproxy)
      shift
      GOPROXY="$(require_value "${option}" "$@")"
      ;;
    --gosumdb)
      shift
      GOSUMDB="$(require_value "${option}" "$@")"
      ;;
    --npm-registry)
      shift
      NPM_REGISTRY="$(require_value "${option}" "$@")"
      ;;
    --pnpm-version)
      shift
      PNPM_VERSION="$(require_value "${option}" "$@")"
      ;;
    --version)
      shift
      VERSION="$(require_value "${option}" "$@")"
      ;;
    --commit)
      shift
      COMMIT="$(require_value "${option}" "$@")"
      ;;
    --date)
      shift
      DATE="$(require_value "${option}" "$@")"
      ;;
    --build-arg)
      shift
      EXTRA_BUILD_ARGS+=("$(require_value "${option}" "$@")")
      ;;
    --label)
      shift
      EXTRA_LABELS+=("$(require_value "${option}" "$@")")
      ;;
    --progress)
      shift
      PROGRESS="$(require_value "${option}" "$@")"
      ;;
    --no-cache)
      NO_CACHE=true
      ;;
    --pull)
      PULL=true
      ;;
    --buildx)
      USE_BUILDX=true
      ;;
    --load)
      LOAD=true
      USE_BUILDX=true
      ;;
    --push)
      PUSH=true
      ;;
    --push-only)
      PUSH_ONLY=true
      PUSH=true
      ;;
    --dry-run)
      DRY_RUN=true
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown option: ${option}"
      ;;
  esac
  shift
done

[[ -n "${IMAGE}" ]] || die "image tag cannot be empty"
[[ -f "${DOCKERFILE}" ]] || die "Dockerfile not found: ${DOCKERFILE}"
[[ -d "${CONTEXT}" ]] || die "build context not found: ${CONTEXT}"

if is_multi_platform; then
  USE_BUILDX=true
  [[ "${PUSH}" == "true" ]] || die "multi-platform builds require --push"
  [[ "${LOAD}" != "true" ]] || die "--load is only supported for single-platform buildx builds"
fi

if [[ "${PUSH}" == "true" && "${LOAD}" == "true" ]]; then
  die "--push and --load cannot be used together"
fi

require_cmd docker
if [[ "${USE_BUILDX}" == "true" ]]; then
  docker buildx version >/dev/null 2>&1 || die "docker buildx is not available"
fi

if [[ -z "${COMMIT}" ]]; then
  COMMIT="$(default_commit)"
fi
if [[ -z "${DATE}" ]]; then
  DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
fi

TAGS=("${IMAGE}")
if [[ ${#EXTRA_TAGS[@]} -gt 0 ]]; then
  for tag in "${EXTRA_TAGS[@]}"; do
    TAGS+=("${tag}")
  done
fi

if [[ "${PUSH_ONLY}" == "true" ]]; then
  for tag in "${TAGS[@]}"; do
    push_local_tag "${tag}"
  done
  exit 0
fi

BUILD_ARGS=(
  --platform "${PLATFORM}"
  --progress "${PROGRESS}"
  -f "${DOCKERFILE}"
)

for tag in "${TAGS[@]}"; do
  BUILD_ARGS+=(-t "${tag}")
done

BUILD_ARGS+=(
  --build-arg "GOPROXY=${GOPROXY}"
  --build-arg "GOSUMDB=${GOSUMDB}"
  --build-arg "NPM_REGISTRY=${NPM_REGISTRY}"
  --build-arg "PNPM_VERSION=${PNPM_VERSION}"
  --build-arg "COMMIT=${COMMIT}"
  --build-arg "DATE=${DATE}"
)

if [[ -n "${VERSION}" ]]; then
  BUILD_ARGS+=(--build-arg "VERSION=${VERSION}")
fi
if [[ -n "${TARGET}" ]]; then
  BUILD_ARGS+=(--target "${TARGET}")
fi
if [[ "${NO_CACHE}" == "true" ]]; then
  BUILD_ARGS+=(--no-cache)
fi
if [[ "${PULL}" == "true" ]]; then
  BUILD_ARGS+=(--pull)
fi
add_optional_args

if [[ "${USE_BUILDX}" == "true" ]]; then
  if [[ "${PUSH}" == "true" ]]; then
    BUILD_ARGS+=(--push)
  else
    BUILD_ARGS+=(--load)
  fi
  run_cmd docker buildx build "${BUILD_ARGS[@]}" "${CONTEXT}"
else
  run_cmd docker build "${BUILD_ARGS[@]}" "${CONTEXT}"
  if [[ "${PUSH}" == "true" ]]; then
    for tag in "${TAGS[@]}"; do
      run_cmd docker push "${tag}"
    done
  fi
fi
