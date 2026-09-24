#!/usr/bin/env bash
# Download the demo ONNX model weights used by the seeded registry entries.
#
# YOLOv8n (COCO object detection) is fetched from Hugging Face and verified
# with a SHA-256 check. Place it where the ML service expects it:
#   - local dev: apps/ml/artifacts/yolov8n.onnx
#   - containers: mounted into the ml service at $ML_ARTIFACTS_DIR/yolov8n.onnx
#
# The seed data references artifact_uri "artifacts/yolov8n.onnx"; the ML
# service resolves that path relative to ML_ARTIFACTS_DIR (or the app cwd).
set -euo pipefail

HASH="65158dad735be799c2466fa15e260c09558080bd530b42a8d0c3d1b419afd8b5"
URL="https://huggingface.co/Kalray/yolov8/resolve/main/yolov8n.onnx"
# DEST_DIR may be overridden (e.g. to a mounted container volume).
DEST_DIR="${DEST_DIR:-$(cd "$(dirname "$0")/.." && pwd)/apps/ml/artifacts}"
DEST="$DEST_DIR/yolov8n.onnx"

if [ -f "$DEST" ]; then
  echo "already present: $DEST"
  exit 0
fi

mkdir -p "$DEST_DIR"
echo "downloading $URL"
curl -fsSL -o "$DEST" "$URL"

echo "verifying sha256"
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$DEST" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
  actual=$(shasum -a 256 "$DEST" | awk '{print $1}')
else
  echo "no sha256 tool found; skipping verification" >&2
  exit 1
fi

if [ "$actual" != "$HASH" ]; then
  echo "checksum mismatch: got $actual" >&2
  rm -f "$DEST"
  exit 1
fi

echo "ok: $DEST ($actual)"
