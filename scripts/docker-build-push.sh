#!/usr/bin/env bash
set -euo pipefail

IMAGE="${IMAGE:-shukbet/mediastationgo}"
TAG="${TAG:-latest}"
PLATFORMS="${PLATFORMS:-linux/amd64,linux/arm64}"
PUSH="${PUSH:-1}"
BUILDER="${BUILDER:-mediastation-builder}"

if ! docker buildx inspect "$BUILDER" >/dev/null 2>&1; then
  docker buildx create --name "$BUILDER" --use >/dev/null
else
  docker buildx use "$BUILDER" >/dev/null
fi

args=(
  buildx build
  --platform "$PLATFORMS"
  -t "$IMAGE:$TAG"
)

if [[ "$PUSH" == "1" ]]; then
  args+=(--push)
else
  args+=(--load)
fi

args+=(.)

echo "Building $IMAGE:$TAG for $PLATFORMS"
docker "${args[@]}"
