"""In-memory model registry keyed by model_version_id.

Resolution order for a prediction request:

1. Exact ``model_version_id`` hit (pre-registered demo entries).
2. Dynamic resolution from ``artifact_uri`` + ``task_type`` supplied by the
   caller (the Go API/worker reads these from the model_versions table). The
   artifact path is looked up under ``ML_ARTIFACTS_DIR`` and the resolved
   model is cached under the requested version id.
3. If the artifact file is absent the underlying models fall back to their
   documented heuristic mode (used for demo/offline environments).

In a larger deployment the registry would be re-hydrated from the database
model_versions table at startup; dynamic resolution keeps that optional.
"""
from __future__ import annotations

import logging
import os
import threading

from .base import ModelMetadata, VisionModel
from .onnx_classifier import ONNXClassifier
from .onnx_detector import ONNXObjectDetector

logger = logging.getLogger(__name__)

_DETECTION_HINTS = ("yolo", "detect", "ssd", "rtdetr", "rt-detr")
_CLASSIFIER_HINTS = ("mobilenet", "resnet", "efficientnet", "classif", "vit", "clip")


class ModelRegistry:
    def __init__(self, artifacts_dir: str = "./artifacts") -> None:
        self._models: dict[str, VisionModel] = {}
        self._meta: dict[str, ModelMetadata] = {}
        self._lock = threading.RLock()
        self._artifacts_dir = artifacts_dir

    def register(self, model_id: str, model: VisionModel, metadata: ModelMetadata) -> None:
        with self._lock:
            self._models[model_id] = model
            self._meta[model_id] = metadata
            logger.info(
                "registered model version %s (%s %s)", model_id, metadata.name, metadata.version
            )

    def get(self, model_id: str) -> VisionModel | None:
        with self._lock:
            m = self._models.get(model_id)
        if m is not None and not m.is_loaded():
            try:
                m.load()
            except Exception:  # pragma: no cover - defensive
                logger.exception("failed to load model %s", model_id)
                return None
        return m

    def metadata(self, model_id: str) -> ModelMetadata | None:
        with self._lock:
            return self._meta.get(model_id)

    def list_loaded(self) -> list[str]:
        with self._lock:
            return [k for k, m in self._models.items() if m.is_loaded()]

    def load_all(self) -> None:
        with self._lock:
            items = list(self._models.items())
        for mid, m in items:
            try:
                m.load()
                logger.info("loaded %s", mid)
            except Exception:  # pragma: no cover - defensive
                logger.exception("failed to load %s", mid)

    # ── dynamic resolution ───────────────────────────────
    def artifact_path(self, artifact_uri: str) -> str:
        """Resolve an artifact URI to a local file path.

        Supported forms: absolute paths, paths relative to the artifacts dir,
        and paths relative to the process cwd. A ``s3://bucket/key`` or
        ``minio://bucket/key`` URI is fetched from object storage into the
        artifacts cache first.
        """
        if artifact_uri.startswith(("s3://", "minio://")):
            return self._fetch_remote_artifact(artifact_uri)
        candidates = [artifact_uri]
        if not os.path.isabs(artifact_uri):
            base = os.path.basename(artifact_uri)
            # Normalise a redundant "artifacts/" prefix so DB URIs like
            # "artifacts/yolov8n.onnx" resolve under ML_ARTIFACTS_DIR too.
            trimmed = base if artifact_uri.startswith("artifacts/") else artifact_uri
            candidates = [
                os.path.join(self._artifacts_dir, trimmed),
                artifact_uri,
                os.path.join(self._artifacts_dir, base),
            ]
        for c in candidates:
            if os.path.exists(c):
                return c
        # Nothing on disk: return the canonical location so callers can log it.
        return os.path.join(self._artifacts_dir, os.path.basename(artifact_uri))

    def _fetch_remote_artifact(self, uri: str) -> str:
        """Download ``s3://bucket/key`` (or minio://) into the artifacts dir."""
        without_scheme = uri.split("://", 1)[1]
        bucket, _, key = without_scheme.partition("/")
        local = os.path.join(self._artifacts_dir, os.path.basename(key))
        if os.path.exists(local):
            return local
        endpoint = os.environ.get("S3_ENDPOINT", "http://localhost:9000")
        try:
            import boto3
            from botocore.config import Config as BotoConfig

            s3 = boto3.client(
                "s3",
                endpoint_url=endpoint,
                aws_access_key_id=os.environ.get("S3_ACCESS_KEY", ""),
                aws_secret_access_key=os.environ.get("S3_SECRET_KEY", ""),
                config=BotoConfig(signature_version="s3v4"),
                region_name=os.environ.get("S3_REGION", "us-east-1"),
            )
            os.makedirs(self._artifacts_dir, exist_ok=True)
            s3.download_file(bucket, key, local + ".part")
            os.replace(local + ".part", local)
            logger.info("downloaded artifact %s -> %s", uri, local)
        except Exception as exc:
            logger.warning("could not fetch artifact %s: %s", uri, exc)
            return local
        return local

    @staticmethod
    def _guess_task(artifact_uri: str, task_type: str | None) -> str | None:
        if task_type in ("object_detection", "classification"):
            return task_type
        # Scan the whole URI (filename plus path segments): models are commonly
        # registered under paths like models/yolo-2026/model.onnx.
        hay = artifact_uri.lower()
        if any(h in hay for h in _DETECTION_HINTS):
            return "object_detection"
        if any(h in hay for h in _CLASSIFIER_HINTS):
            return "classification"
        return None

    def resolve(
        self,
        model_version_id: str,
        artifact_uri: str | None = None,
        task_type: str | None = None,
    ) -> tuple[VisionModel | None, ModelMetadata | None]:
        """Resolve a model for the request, registering dynamic entries."""
        existing = self.get(model_version_id)
        if existing is not None:
            return existing, self.metadata(model_version_id)
        if not artifact_uri:
            return None, None
        task = self._guess_task(artifact_uri, task_type)
        if task is None:
            logger.warning("cannot infer task type for artifact %s", artifact_uri)
            return None, None
        path = self.artifact_path(artifact_uri)
        stem = os.path.splitext(os.path.basename(artifact_uri))[0]
        # Try to read a semver-ish version segment from the URI (e.g. models/yolo/1.0.0/model.onnx)
        import re as _re
        version = "1.0.0"
        for seg in reversed(artifact_uri.replace("\\", "/").split("/")[:-1]):
            if _re.fullmatch(r"\d+\.\d+(\.\d+)?([-._\w]*)?", seg):
                version = seg
                break
        meta = ModelMetadata(
            name=stem,
            version=version,
            task_type=task,
            runtime="onnx",
            input_shape=(1, 3, 640, 640) if task == "object_detection" else (1, 3, 224, 224),
            labels=self._labels_for(path, task),
        )
        model: VisionModel
        if task == "object_detection":
            model = ONNXObjectDetector(path, meta)
        else:
            model = ONNXClassifier(path, meta, labels_file=self._labels_file_for(path))
        model.load()
        with self._lock:
            # Re-check under the lock in case of a concurrent resolution.
            if model_version_id in self._models:
                return self._models[model_version_id], self._meta[model_version_id]
            self._models[model_version_id] = model
            self._meta[model_version_id] = meta
        logger.info(
            "dynamically registered model %s from %s (task=%s)", model_version_id, path, task
        )
        return model, meta

    @staticmethod
    def _labels_file_for(artifact_path: str) -> str | None:
        """Sidecar labels: <artifact>.labels.json|txt next to the artifact."""
        stem = os.path.splitext(artifact_path)[0]
        for ext in (".labels.json", ".labels.txt", "_labels.txt"):
            p = stem + ext
            if os.path.exists(p):
                return p
        return None

    @staticmethod
    def _labels_for(artifact_path: str, task: str) -> list[str]:
        if task != "object_detection":
            return []
        from .onnx_detector import COCO_LABELS
        return list(COCO_LABELS)


def build_default_registry(artifacts_dir: str) -> ModelRegistry:
    """Create a registry with the built-in demo models.

    IDs use a placeholder (``demo-*``) that seeds can reference. Dynamic
    resolution (see ModelRegistry.resolve) handles database model version
    UUIDs when the API passes ``artifact_uri``/``task_type``.
    """
    from .onnx_detector import COCO_LABELS as _COCO

    reg = ModelRegistry(artifacts_dir=artifacts_dir)
    det_meta = ModelMetadata(
        name="YOLOv8n-COCO",
        version="1.0.0",
        task_type="object_detection",
        runtime="onnx",
        input_shape=(1, 3, 640, 640),
        labels=list(_COCO),
    )
    det_path = os.path.join(artifacts_dir, "yolov8n.onnx")
    reg.register("demo-yolov8n", ONNXObjectDetector(det_path, det_meta), det_meta)

    cls_meta = ModelMetadata(
        name="MobileNetV3-ImageNet",
        version="1.0.0",
        task_type="classification",
        runtime="onnx",
        input_shape=(1, 3, 224, 224),
    )
    cls_path = os.path.join(artifacts_dir, "mobilenetv3.onnx")
    reg.register("demo-mobilenetv3", ONNXClassifier(cls_path, cls_meta), cls_meta)
    return reg
