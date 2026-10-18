#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CFG="$ROOT/pipeline/deepstream/configs/config_det_pgie.txt.template"
# Keep model, class count and labels together. The supplied deployment contains
# a two-class forklift/person model; never overwrite it with COCO-80 settings.
MODEL_ENGINE="${MODEL_ENGINE:-$ROOT/model_stage/forklift.fp16.plan}"
NUM_CLASSES="$(awk 'NF {n++} END {print n+0}' "$ROOT/pipeline/deepstream/labels/config_det_pgie_labels.txt")"
[[ -f "$MODEL_ENGINE" && "$NUM_CLASSES" -gt 0 ]] || { echo 'Missing engine or labels' >&2; exit 2; }
export VIDEO_OUT="${VIDEO_OUT:-$ROOT/output/output.mp4}"
export JSON_OUT="${JSON_OUT:-$ROOT/output/metadata.json}"
export VIDEO_URI="${VIDEO_URI:-file:///workspace/Camera1.mp4}"
mkdir -p "$ROOT/output"
# DeepStream config expects absolute paths; materialize from this package's location.
sed -e "s|MODEL_PLAN_PLACEHOLDER|$MODEL_ENGINE|g" \
    -e "s|NUM_CLASSES_PLACEHOLDER|$NUM_CLASSES|g" \
    -e "s|LABELS_PLACEHOLDER|$ROOT/pipeline/deepstream/labels/config_det_pgie_labels.txt|g" \
    -e "s|PARSER_PLACEHOLDER|$ROOT/pipeline/deepstream/build/libconfig_det_pgie_parser.so|g" \
    "$CFG" > "$ROOT/output/active_detector_config.txt"
cp "$ROOT/output/active_detector_config.txt" "$ROOT/pipeline/deepstream/configs/config_det_pgie.txt"
echo "[navforge] using VIDEO_URI=$VIDEO_URI" >&2
export JSON_SOURCE="$VIDEO_URI"
export JSON_MIN_CONF=0.0
exec bash "$ROOT/pipeline/deepstream/run.sh"
