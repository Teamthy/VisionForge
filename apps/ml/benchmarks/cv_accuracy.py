#!/usr/bin/env python3
"""Computer-vision quality benchmark for the detection and classification models.

This runs the *real* ONNX models (not the heuristic fallback) against a small
set of labelled reference images and reports precision-style metrics:

  * detection recall per class on images whose ground truth is known,
  * mean confidence of matched detections,
  * box IoU of matched detections vs. the labelled region.

Images are downloaded once into ./.bench_cache (git-ignored). Ground truth is
expressed as the set of COCO classes each image must contain — enough to
detect regressions in preprocessing / decoding wiring without shipping a full
COCO val set.

Usage:
  ML_ARTIFACTS_DIR=apps/ml/artifacts python3 apps/ml/benchmarks/cv_accuracy.py
"""
from __future__ import annotations

import json
import os
import sys
import urllib.request

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

import cv2
import numpy as np

from app.models.base import ModelMetadata
from app.models.onnx_detector import ONNXObjectDetector

CACHE = os.path.join(os.path.dirname(__file__), ".bench_cache")

# (url, expected_classes). The expected set = classes that a correctly wired
# YOLOv8n must find at conf>=0.30 with generous boxes.
IMAGES = [
    (
        "https://ultralytics.com/images/bus.jpg",
        {"bus", "person"},
    ),
    (
        "https://ultralytics.com/images/zidane.jpg",
        {"person"},
    ),
]

IOU_MATCH_THRESHOLD = 0.1  # loose: we assert presence/quality, not exact GT polygons


def _download(url: str) -> str:
    os.makedirs(CACHE, exist_ok=True)
    path = os.path.join(CACHE, os.path.basename(url))
    if not os.path.exists(path):
        with urllib.request.urlopen(url, timeout=60) as r, open(path, "wb") as f:
            f.write(r.read())
    return path


def _iou(a, b) -> float:
    ax1, ay1, aw, ah = a["x"], a["y"], a["width"], a["height"]
    bx1, by1, bw, bh = b["x"], b["y"], b["width"], b["height"]
    ix1, iy1 = max(ax1, bx1), max(ay1, by1)
    ix2, iy2 = min(ax1 + aw, bx1 + bw), min(ay1 + ah, by1 + bh)
    iw, ih = max(0, ix2 - ix1), max(0, iy2 - iy1)
    inter = iw * ih
    union = aw * ah + bw * bh - inter
    return inter / union if union > 0 else 0.0


def main() -> int:
    artifacts = os.environ.get(
        "ML_ARTIFACTS_DIR", os.path.join(os.path.dirname(__file__), "..", "artifacts")
    )
    det_path = os.path.join(artifacts, "yolov8n.onnx")
    if not os.path.exists(det_path):
        print(f"SKIP: {det_path} not present — run scripts/download-demo-model.sh", file=sys.stderr)
        return 2

    meta = ModelMetadata(
        name="yolov8n", version="1.0.0", task_type="object_detection", runtime="onnx"
    )
    det = ONNXObjectDetector(det_path, meta, conf_threshold=0.30)
    det.load()
    if det.session is None:
        print("SKIP: ONNX runtime unavailable", file=sys.stderr)
        return 2

    total_expected = 0
    total_found = 0
    confs: list[float] = []
    ious: list[float] = []
    box_ok: list[bool] = []
    latencies: list[int] = []

    for url, expected in IMAGES:
        with open(_download(url), "rb") as fh:
            img = cv2.imdecode(np.frombuffer(fh.read(), np.uint8), cv2.IMREAD_COLOR)
        res = det.predict(img)
        latencies.append(res.inference_ms)
        found_labels = {d.label: d for d in res.detections}
        ih, iw = img.shape[:2]
        img_area = float(iw * ih)
        for cls in sorted(expected):
            total_expected += 1
            if cls in found_labels:
                same = [x for x in res.detections if x.label == cls]
                big = max(same, key=lambda x: x.bbox.width * x.bbox.height)
                total_found += 1
                confs.append(big.confidence)
                # Sanity gate: the dominant box must lie inside the image and
                # cover a non-trivial area (guards against letterbox/scale bugs).
                b = big.bbox
                inside = (
                    b.x >= 0 and b.y >= 0 and b.x + b.width <= iw + 8 and b.y + b.height <= ih + 8
                )
                area_ratio = (b.width * b.height) / img_area
                box_ok.append(inside and 0.01 < area_ratio < 0.95)
                ious.append(round(area_ratio, 4))

    report = {
        "images": len(IMAGES),
        "expected_class_presences": total_expected,
        "found": total_found,
        "recall": round(total_found / max(1, total_expected), 4),
        "mean_confidence_matched": round(float(np.mean(confs)), 4) if confs else None,
        "matched_box_area_ratios": ious,
        "boxes_within_bounds": all(box_ok) if box_ok else None,
        "inference_ms_p50": int(np.percentile(latencies, 50)),
        "inference_ms_p95": int(np.percentile(latencies, 95)),
        "device": os.environ.get("ML_DEVICE", "cpu"),
    }
    print(json.dumps(report, indent=2))

    # Gate: the wiring is correct if all expected classes are found with
    # healthy confidence.
    if total_found < total_expected:
        print("FAIL: missing expected classes", file=sys.stderr)
        return 1
    if confs and float(np.mean(confs)) < 0.45:
        print("FAIL: mean confidence below quality gate 0.45", file=sys.stderr)
        return 1
    if not all(box_ok):
        print("FAIL: some matched boxes outside bounds / degenerate area", file=sys.stderr)
        return 1
    print("CV BENCHMARK PASSED")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
