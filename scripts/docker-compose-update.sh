#!/usr/bin/env bash
set -euo pipefail

IMAGE="${MEDIASTATION_IMAGE:-ghcr.io/shukebta/mediastation-go}"
SERVICE="${MEDIASTATION_SERVICE:-mediastation-go}"
CONTAINER="${MEDIASTATION_CONTAINER:-mediastation-go}"
PRUNE_DANGLING="${PRUNE_DANGLING:-1}"
PRUNE_ALL_UNUSED="${PRUNE_ALL_UNUSED:-0}"

if docker compose version >/dev/null 2>&1; then
  COMPOSE=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
  COMPOSE=(docker-compose)
else
  echo "docker compose / docker-compose not found" >&2
  exit 1
fi

echo "==> Pulling latest configured image"
"${COMPOSE[@]}" pull "$SERVICE"

echo "==> Recreating service"
"${COMPOSE[@]}" up -d "$SERVICE"

running_image_id="$(docker inspect -f '{{.Image}}' "$CONTAINER" 2>/dev/null || true)"
if [[ -z "$running_image_id" ]]; then
  echo "WARN: container '$CONTAINER' not found, skip project image cleanup" >&2
else
  echo "==> Removing old unused $IMAGE images"
  while IFS= read -r image_id; do
    [[ -z "$image_id" || "$image_id" == "$running_image_id" ]] && continue
    docker rmi "$image_id" >/dev/null 2>&1 || true
  done < <(docker image ls "$IMAGE" --format '{{.ID}}' | sort -u)
fi

if [[ "$PRUNE_DANGLING" == "1" ]]; then
  echo "==> Pruning dangling image layers"
  docker image prune -f >/dev/null
fi

if [[ "$PRUNE_ALL_UNUSED" == "1" ]]; then
  echo "==> Pruning all unused images"
  docker image prune -a -f >/dev/null
fi

echo "Done. Current image:"
docker image ls "$IMAGE" --format '  {{.Repository}}:{{.Tag}}  {{.ID}}  {{.Size}}' | head -n 20
