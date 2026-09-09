#!/usr/bin/env bash
set -Eeuo pipefail

config_file=${CONFIG_FILE:-syncer.json}
binary=${SYNCER_BINARY:-./bin/docker-syncer}
summary_path=${SUMMARY_PATH:-${GITHUB_STEP_SUMMARY:-docker-syncer-summary.md}}
input_mode=${INPUT_MODE:-}
input_image=${INPUT_IMAGE:-}
input_platform=${INPUT_PLATFORM:-}
input_target=${INPUT_TARGET_NAME:-}
input_force=${INPUT_FORCE:-}
input_dry_run=${INPUT_DRY_RUN:-}

work_dir=$(mktemp -d "${RUNNER_TEMP:-${TMPDIR:-/tmp}}/docker-syncer.XXXXXX")
auth_file="$work_dir/auth.json"
printf '{}\n' >"$auth_file"
chmod 600 "$auth_file"

notify() {
  local status=$1 result title payload
  [[ -n ${WEBHOOK_URL:-} ]] || return 0

  if ((status == 0)); then
    result=success
    title="✅ Docker 镜像同步成功"
  else
    result=failure
    title="❌ Docker 镜像同步失败"
  fi

  if ! payload=$(jq -cn \
    --arg title "$title" \
    --arg repository "${GITHUB_REPOSITORY:-local}" \
    --arg status "$result" \
    --arg image "${input_image:-batch}" \
    --arg url "${GITHUB_SERVER_URL:-https://github.com}/${GITHUB_REPOSITORY:-local}/actions/runs/${GITHUB_RUN_ID:-unknown}" \
    '{msg_type:"text",content:{text:($title+"\n仓库: "+$repository+"\n镜像: "+$image+"\n状态: "+$status+"\n链接: "+$url)}}'); then
    printf '%s\n' '::warning::Could not construct webhook notification.' >&2
    return 0
  fi

  if ! curl --silent --show-error --fail \
    --connect-timeout 5 --max-time 10 --retry 1 \
    -H 'Content-Type: application/json' \
    --data-binary "$payload" "$WEBHOOK_URL" >/dev/null 2>&1; then
    printf '%s\n' '::warning::Webhook notification failed.' >&2
  fi
}

finish() {
  local status=$?
  trap - EXIT INT TERM
  notify "$status" || true
  rm -rf -- "$work_dir"
  exit "$status"
}
trap finish EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

[[ -x $binary ]] || {
  printf 'docker-syncer binary is not executable: %s\n' "$binary" >&2
  exit 1
}
[[ -f $config_file ]] || {
  printf 'Configuration file not found: %s\n' "$config_file" >&2
  exit 1
}

config_args=(config --config "$config_file")
if [[ -n $input_mode && $input_mode != config ]]; then
  config_args+=(--mode "$input_mode")
fi
resolved_mode=$($binary "${config_args[@]}")
resolved_mode=${resolved_mode//$'\r'/}
resolved_mode=${resolved_mode//$'\n'/}
case "$resolved_mode" in
  aliyun | ghcr | double | none) ;;
  *)
    printf 'docker-syncer returned an invalid resolved mode.\n' >&2
    exit 1
    ;;
esac

is_true() {
  [[ ${1,,} == true ]]
}

login_registry() {
  local label=$1 registry=$2 username=$3 password=$4 login_log="$work_dir/login.log"
  if ! printf '%s' "$password" | skopeo login \
    --authfile "$auth_file" \
    --username "$username" \
    --password-stdin "$registry" >"$login_log" 2>&1; then
    printf 'Registry authentication failed for %s.\n' "$label" >&2
    return 1
  fi
  : >"$login_log"
}

# A dry run is intentionally offline; mode=none needs no credentials either.
if ! is_true "$input_dry_run" && [[ $resolved_mode != none ]]; then
  if [[ -n ${DOCKERHUB_USERNAME:-} || -n ${DOCKERHUB_TOKEN:-} ]]; then
    [[ -n ${DOCKERHUB_USERNAME:-} && -n ${DOCKERHUB_TOKEN:-} ]] || {
      printf '%s\n' 'Docker Hub credentials are incomplete.' >&2
      exit 1
    }
    login_registry DockerHub docker.io "$DOCKERHUB_USERNAME" "$DOCKERHUB_TOKEN"
  fi

  # This also authenticates GHCR sources when the destination is only Aliyun.
  if [[ -n ${GITHUB_TOKEN:-} ]]; then
    [[ -n ${GITHUB_ACTOR:-} ]] || {
      printf '%s\n' 'GITHUB_ACTOR is required when GITHUB_TOKEN is configured.' >&2
      exit 1
    }
    login_registry GHCR ghcr.io "$GITHUB_ACTOR" "$GITHUB_TOKEN"
  elif [[ $resolved_mode == ghcr || $resolved_mode == double ]]; then
    printf '%s\n' 'GITHUB_TOKEN is required for the resolved mode.' >&2
    exit 1
  fi

  if [[ $resolved_mode == aliyun || $resolved_mode == double ]]; then
    [[ -n ${ALIYUN_REGISTRY:-} && -n ${ALIYUN_NAMESPACE:-} ]] || {
      printf '%s\n' 'ALIYUN_REGISTRY and ALIYUN_NAMESPACE are required for the resolved mode.' >&2
      exit 1
    }
    [[ -n ${ALIYUN_USERNAME:-} && -n ${ALIYUN_PASSWORD:-} ]] || {
      printf '%s\n' 'ALIYUN_USERNAME and ALIYUN_PASSWORD are required for the resolved mode.' >&2
      exit 1
    }
    login_registry Aliyun "$ALIYUN_REGISTRY" "$ALIYUN_USERNAME" "$ALIYUN_PASSWORD"
  fi
fi

sync_args=(sync --config "$config_file" --authfile "$auth_file" --summary "$summary_path")
if [[ -n $input_image ]]; then
  sync_args+=(--image "$input_image")
else
  sync_args+=(--file "${IMAGES_FILE:-images.txt}")
fi
[[ -z $input_mode || $input_mode == config ]] || sync_args+=(--mode "$input_mode")
[[ -z $input_platform ]] || sync_args+=(--platform "$input_platform")
[[ -z $input_target ]] || sync_args+=(--target-name "$input_target")
[[ -z $input_force ]] || sync_args+=(--force="$input_force")
[[ -z $input_dry_run ]] || sync_args+=(--dry-run="$input_dry_run")

"$binary" "${sync_args[@]}"
