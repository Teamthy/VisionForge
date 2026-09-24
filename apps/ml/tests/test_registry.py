"""Registry dynamic-resolution tests: task inference and artifact path normalisation."""
from __future__ import annotations

import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from app.models.registry import ModelRegistry


def test_guess_task_from_filename():
    r = ModelRegistry(artifacts_dir="/tmp")
    assert r._guess_task("models/yolo-2026/model.onnx", None) == "object_detection"
    assert r._guess_task("models/resnet-50/model.onnx", None) == "classification"
    assert r._guess_task("models/custom/model.onnx", None) is None
    # explicit request wins over guessing
    assert r._guess_task("yolov8n.onnx", "classification") == "classification"


def test_artifact_path_normalises_artifacts_prefix(tmp_path=None):
    import tempfile
    d = tempfile.mkdtemp()
    with open(os.path.join(d, "yolov8n.onnx"), "wb") as fh:
        fh.write(b"x")
    r = ModelRegistry(artifacts_dir=d)
    resolved = r.artifact_path("artifacts/yolov8n.onnx")
    assert resolved == os.path.join(d, "yolov8n.onnx")


def test_resolve_unknown_without_artifact_returns_none():
    r = ModelRegistry(artifacts_dir="/tmp")
    model, meta = r.resolve("no-such-id")
    assert model is None and meta is None


def test_resolve_falls_back_to_heuristic_when_file_missing():
    """No file on disk => model still resolves in heuristic mode (documented)."""
    import tempfile
    d = tempfile.mkdtemp()
    r = ModelRegistry(artifacts_dir=d)
    model, meta = r.resolve("vid-1", "yolo-missing.onnx", "object_detection")
    assert model is not None, "heuristic fallback should keep the demo runnable"
    assert meta.task_type == "object_detection"
