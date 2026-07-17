#!/usr/bin/env sh
set -eu

repository_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
state_dir=$(mktemp -d "${TMPDIR:-/tmp}/vericopy-integration.XXXXXX")
container_name="vericopy-integration-$$"
host_port="${VERICOPY_INTEGRATION_PORT:-22345}"
container_runtime="${CONTAINER_RUNTIME:-docker}"
go_binary="${VERICOPY_GO_BIN:-go}"

if ! command -v "$container_runtime" >/dev/null 2>&1; then
  printf 'container runtime not found: %s\n' "$container_runtime" >&2
  exit 127
fi
if ! command -v "$go_binary" >/dev/null 2>&1 && [ ! -x "$go_binary" ]; then
  printf 'Go executable not found: %s\n' "$go_binary" >&2
  exit 127
fi

cleanup() {
  "$container_runtime" rm -f "$container_name" >/dev/null 2>&1 || true
  rm -rf "$state_dir"
}
trap cleanup EXIT INT TERM

ssh-keygen -q -t ed25519 -N '' -f "$state_dir/id_ed25519"
"$container_runtime" build \
  --build-arg "TEST_PUBLIC_KEY=$(cat "$state_dir/id_ed25519.pub")" \
  -t vericopy-integration:local \
  -f "$repository_dir/integration/Dockerfile" \
  "$repository_dir/integration"
"$container_runtime" run -d --name "$container_name" -p "127.0.0.1:${host_port}:2222" vericopy-integration:local >/dev/null

attempt=0
while ! ssh-keyscan -p "$host_port" 127.0.0.1 >"$state_dir/known_hosts" 2>/dev/null; do
  attempt=$((attempt + 1))
  if [ "$attempt" -ge 30 ]; then
    "$container_runtime" logs "$container_name"
    exit 1
  fi
  sleep 1
done

VERICOPY_INTEGRATION=1 \
VERICOPY_INTEGRATION_HOST=127.0.0.1 \
VERICOPY_INTEGRATION_PORT="$host_port" \
VERICOPY_INTEGRATION_IDENTITY="$state_dir/id_ed25519" \
VERICOPY_INTEGRATION_KNOWN_HOSTS="$state_dir/known_hosts" \
"$go_binary" test -tags=integration -count=1 ./integration
