#!/usr/bin/env bash

set -Eeuo pipefail
IFS=$'\n\t'
umask 077

usage() {
  echo "Usage: $0 <tag> <backend-image> <web-image> <docker-hub-user>" >&2
}

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

if [[ $# -ne 4 ]]; then
  usage
  exit 2
fi

tag=$1
backend_image=$2
web_image=$3
docker_hub_user=$4
deploy_dir=/srv/stein-ai
compose_file=$deploy_dir/docker-compose.selfhost.yml
runtime_env=$deploy_dir/.env
deploy_env=$deploy_dir/deploy.env

[[ $tag =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || fail "Invalid release tag."
[[ $docker_hub_user =~ ^[a-z0-9]+([._-][a-z0-9]+)*$ ]] || fail "Invalid Docker Hub namespace."
[[ $backend_image == "docker.io/$docker_hub_user/multica-backend" ]] || fail "Unexpected backend image."
[[ $web_image == "docker.io/$docker_hub_user/multica-web" ]] || fail "Unexpected web image."

for command_name in awk curl docker install mktemp; do
  command -v "$command_name" >/dev/null 2>&1 || fail "Required command is missing: $command_name"
done

[[ -f $compose_file ]] || fail "Missing Compose file: $compose_file"
[[ -f $runtime_env ]] || fail "Missing production environment file: $runtime_env"

if ! IFS= read -r docker_hub_token || [[ -z $docker_hub_token ]]; then
  fail "A Docker Hub token must be provided on standard input."
fi

docker_config=$(mktemp -d "${TMPDIR:-/tmp}/stein-ai-docker.XXXXXX")
cleanup() {
  unset docker_hub_token
  rm -f -- "$docker_config/config.json"
  rmdir -- "$docker_config" 2>/dev/null || true
}
trap cleanup EXIT
export DOCKER_CONFIG=$docker_config

printf '%s' "$docker_hub_token" | docker login docker.io --username "$docker_hub_user" --password-stdin >/dev/null
unset docker_hub_token

read_deploy_value() {
  local key=$1
  local file=$2

  [[ -f $file ]] || return 0
  awk -F= -v key="$key" '$1 == key { print substr($0, index($0, "=") + 1); exit }' "$file" | tr -d '\r'
}

write_deploy_env() {
  local current_tag=$1
  local previous_tag=$2
  local temporary_file

  temporary_file=$(mktemp "$deploy_dir/.deploy.env.XXXXXX")
  {
    printf 'MULTICA_IMAGE_TAG=%s\n' "$current_tag"
    printf 'MULTICA_BACKEND_IMAGE=%s\n' "$backend_image"
    printf 'MULTICA_WEB_IMAGE=%s\n' "$web_image"
    printf 'MULTICA_PREVIOUS_IMAGE_TAG=%s\n' "$previous_tag"
  } >"$temporary_file"
  chmod 0640 "$temporary_file"
  mv -f -- "$temporary_file" "$deploy_env"
}

wait_until_ready() {
  local attempt

  for ((attempt = 1; attempt <= 60; attempt++)); do
    if curl --fail --silent --connect-timeout 2 --max-time 5 \
      http://127.0.0.1:8080/readyz >/dev/null \
      && curl --fail --silent --connect-timeout 2 --max-time 5 \
        http://127.0.0.1:3000/ >/dev/null; then
      return 0
    fi
    sleep 2
  done

  return 1
}

deploy_stack() {
  local -a compose=(
    docker compose
    --env-file "$runtime_env"
    --env-file "$deploy_env"
    -f "$compose_file"
  )

  "${compose[@]}" pull || return 1
  "${compose[@]}" up -d || return 1
  wait_until_ready || return 1
}

current_tag=$(read_deploy_value MULTICA_IMAGE_TAG "$deploy_env")
current_previous_tag=$(read_deploy_value MULTICA_PREVIOUS_IMAGE_TAG "$deploy_env")

if [[ -n $current_tag && ! $current_tag =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  fail "Existing deploy.env contains an invalid image tag."
fi
if [[ -n $current_previous_tag && ! $current_previous_tag =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]; then
  fail "Existing deploy.env contains an invalid previous image tag."
fi

if [[ -n $current_tag && $current_tag != "$tag" ]]; then
  rollback_tag=$current_tag
  rollback_previous_tag=$current_previous_tag
elif [[ $current_tag == "$tag" ]]; then
  rollback_tag=$current_previous_tag
  rollback_previous_tag=
else
  rollback_tag=
  rollback_previous_tag=
fi

write_deploy_env "$tag" "$rollback_tag"
echo "Deploying Stein-AI release $tag."

if deploy_stack; then
  echo "Stein-AI release $tag is ready."
  exit 0
fi

echo "ERROR: Stein-AI release $tag did not become ready." >&2
echo "WARNING: Container rollback does not reverse database migrations; any migrations already applied by $tag remain in the database." >&2

if [[ -z $rollback_tag ]]; then
  echo "ERROR: No previous release tag is available for rollback." >&2
  exit 1
fi

echo "Rolling containers back to $rollback_tag." >&2
write_deploy_env "$rollback_tag" "$rollback_previous_tag"

if deploy_stack; then
  echo "Containers were rolled back to $rollback_tag, but the deployment is marked failed." >&2
else
  echo "ERROR: Rollback to $rollback_tag also failed its readiness check." >&2
fi

exit 1
