#!/usr/bin/env bash
set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONTAINER="${PIPEGEN_CONTAINER:-craftifai-amd64-v3}"
VIDEO="${1:-}"
if [[ -z "$VIDEO" || ! -f "$VIDEO" ]]; then
  echo 'Usage: ./deploy-wsl.sh /mnt/c/Users/Atharva/Downloads/Camera1.mp4' >&2
  exit 2
fi
# Copy the files into the existing GPU/DeepStream container; this does not replace it.
docker exec "$CONTAINER" mkdir -p /opt/navforge-gpu /workspace
# Deploy only runtime inputs, not saved results, documentation or local caches.
docker cp "$DIR/pipeline" "$CONTAINER:/opt/navforge-gpu/"
docker cp "$DIR/model_stage" "$CONTAINER:/opt/navforge-gpu/"
docker cp "$DIR/run-in-container.sh" "$CONTAINER:/opt/navforge-gpu/run-in-container.sh"
docker cp "$VIDEO" "$CONTAINER:/workspace/navforge_input.mp4"
# A directory mounted from the host makes the flushed metadata available to the
# Go follower during inference. Otherwise this launcher supports replay after copy.
if [[ -n "${NAVFORGE_SHARED_OUTPUT:-}" ]]; then
  docker exec -e VIDEO_URI=file:///workspace/navforge_input.mp4 \
    -e JSON_OUT="$NAVFORGE_SHARED_OUTPUT/metadata.json" \
    -e VIDEO_OUT="$NAVFORGE_SHARED_OUTPUT/output.mp4" \
    "$CONTAINER" bash /opt/navforge-gpu/run-in-container.sh
else
  docker exec -e VIDEO_URI=file:///workspace/navforge_input.mp4 "$CONTAINER" bash /opt/navforge-gpu/run-in-container.sh
fi
mkdir -p "$DIR/results"
docker cp "$CONTAINER:${NAVFORGE_SHARED_OUTPUT:-/opt/navforge-gpu/output}/." "$DIR/results/"
echo "Outputs copied to $DIR/results"
