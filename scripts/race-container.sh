#!/usr/bin/env sh
set -eu

repository_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
container_runtime="${CONTAINER_RUNTIME:-docker}"
race_image="${VERICOPY_RACE_IMAGE:-docker.io/library/golang:1.26.5-alpine}"
mount_suffix=""

case "$container_runtime" in
  podman) mount_suffix=":Z" ;;
esac

if ! command -v "$container_runtime" >/dev/null 2>&1; then
  printf 'container runtime not found: %s\n' "$container_runtime" >&2
  exit 127
fi

"$container_runtime" run --rm \
  -v "$repository_dir:/src${mount_suffix}" \
  -w /src \
  "$race_image" \
  sh -ec 'apk add --no-cache build-base >/dev/null && go test -race -count=1 ./...'

