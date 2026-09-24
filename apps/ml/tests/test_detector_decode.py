"""Unit tests for YOLOv8 output decoding (pure numpy; no ONNX runtime needed).

Regression coverage for the two failure modes fixed during bring-up:
  1. boxes are in *input pixel space* (0..640), not normalised 0..1;
  2. candidates must be selected globally, not from a truncated prefix.
"""
from __future__ import annotations

import os
import sys

import numpy as np

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from app.models.base import ModelMetadata
from app.models.onnx_detector import COCO_LABELS, ONNXObjectDetector


def _detector() -> ONNXObjectDetector:
    meta = ModelMetadata(name="t", version="1", task_type="object_detection", runtime="onnx")
    d = ONNXObjectDetector("/nonexistent.onnx", meta, conf_threshold=0.30)
    d._loaded = True  # skip load(); session stays None
    return d


def _make_raw(bus_row: int = 5000) -> np.ndarray:
    """Synthetic YOLOv8 tensor [1, 84, 8400] with one strong 'bus' anchor."""
    raw = np.zeros((1, 4 + len(COCO_LABELS), 8400), dtype=np.float32)
    # cx, cy, w, h in 640-px input space: a big box around the left half.
    raw[0, 0, bus_row] = 150.0  # cx
    raw[0, 1, bus_row] = 180.0  # cy
    raw[0, 2, bus_row] = 300.0  # w
    raw[0, 3, bus_row] = 200.0  # h
    bus_idx = COCO_LABELS.index("bus")
    raw[0, 4 + bus_idx, bus_row] = 0.9
    # a degenerate anchor far past row 200 that a truncated scan would miss
    person_idx = COCO_LABELS.index("person")
    raw[0, 0, 7000] = 500.0
    raw[0, 1, 7000] = 500.0
    raw[0, 2, 7000] = 100.0
    raw[0, 3, 7000] = 150.0
    raw[0, 4 + person_idx, 7000] = 0.8
    # noise that must be filtered: tiny box (3x3) with high score
    raw[0, 0, 3000] = 400.0
    raw[0, 1, 3000] = 400.0
    raw[0, 2, 3000] = 3.0
    raw[0, 3, 3000] = 3.0
    raw[0, 4, 3000] = 0.99
    return raw


def test_decode_maps_pixel_space_through_letterbox():
    d = _detector()
    # 810x1080 image letterboxed into 640x640 -> ratio = 1080/640 = 1.6875
    orig_wh = (810, 1080)
    ratio = 1080 / 640
    dets = d._postprocess(_make_raw(), orig_wh, ratio)
    by_label = {x.label: x for x in dets}
    assert "bus" in by_label, sorted(by_label)
    bus = by_label["bus"]
    # expected original-space box: (cx-w/2)/ratio ...
    assert abs(bus.bbox.x - (150 - 150) / ratio) < 2  # left edge ~0
    assert abs(bus.bbox.y - (180 - 100) / ratio) < 2  # ~80
    assert abs(bus.bbox.width - 300 / ratio) < 2       # ~178
    assert abs(bus.bbox.height - 200 / ratio) < 2      # ~119


def test_decode_finds_strong_anchor_beyond_first_rows():
    d = _detector()
    dets = d._postprocess(_make_raw(), (640, 640), 1.0)
    labels = {x.label for x in dets}
    assert "person" in labels, "global top-k selection must see all 8400 anchors"


def test_degenerate_small_boxes_are_filtered():
    d = _detector()
    dets = d._postprocess(_make_raw(), (640, 640), 1.0)
    assert all(x.bbox.width >= 4 and x.bbox.height >= 4 for x in dets)


def test_logit_range_scores_are_squashed():
    """Pre-sigmoid logits (max > 1.5) must be squashed before thresholding."""
    d = _detector()
    raw = _make_raw()
    person_idx = COCO_LABELS.index("person")
    raw[0, 4 + person_idx, 7000] = 6.0  # logit, not probability
    dets = d._postprocess(raw, (640, 640), 1.0)
    person = next(x for x in dets if x.label == "person")
    assert 0.0 < person.confidence <= 1.0
