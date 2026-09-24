"""ONNX image classification (MobileNet-style) with heuristic fallback."""
from __future__ import annotations

import json
import logging
import os
import time

import numpy as np

from .base import InferenceResult, ModelMetadata, Prediction, VisionModel

logger = logging.getLogger(__name__)

IMAGENET_SAMPLE = [
    "tabby cat", "tiger cat", "golden retriever", "labrador retriever",
    "coffee mug", "espresso", "laptop", "keyboard", "desktop computer",
    "ballpoint pen", "water bottle", "banana", "orange", "lemon", "pizza",
    "hamburger", "coffee", "dog", "cat", "bird",
]


class ONNXClassifier(VisionModel):
    def __init__(self, artifact_uri: str, metadata: ModelMetadata, top_k: int = 5,
                 labels_file: str | None = None):
        super().__init__(artifact_uri, metadata)
        self.top_k = top_k
        self.session = None
        self.input_name = None
        self._input_shape = (224, 224)
        self._labels = self._load_labels(labels_file)
        # Preprocessing defaults for torchvision-style exports:
        # input is RGB, scaled to [0,1], then ImageNet normalisation.
        self._input_format = "RGB"
        self._norm_mean = np.array([0.485, 0.456, 0.406], dtype=np.float32)
        self._norm_std = np.array([0.229, 0.224, 0.225], dtype=np.float32)
        self._scale_to_unit = True  # torchvision-style exports expect [0,1] input

    @staticmethod
    def _load_labels(labels_file: str | None) -> list[str] | None:
        """Load full ImageNet class names from a sidecar file when provided.

        Accepted formats: JSON list, JSON dict {"0": "name", ...} (as shipped
        with ONNX Model Zoo classification models), or one label per line.
        """
        if not labels_file or not os.path.exists(labels_file):
            return None
        try:
            with open(labels_file, encoding="utf-8") as fh:
                text = fh.read()
            if labels_file.endswith(".json"):
                data = json.loads(text)
                if isinstance(data, dict):
                    return [str(data[str(i)]) for i in range(len(data))]
                return [str(x) for x in data]
            return [ln.strip() for ln in text.splitlines() if ln.strip()]
        except Exception as exc:
            logger.warning("could not read labels file %s: %s", labels_file, exc)
            return None

    def load(self) -> None:
        if self._loaded:
            return
        try:
            import onnxruntime as ort
            if os.path.exists(self.artifact_uri):
                opts = ort.SessionOptions()
                opts.intra_op_num_threads = 2
                self.session = ort.InferenceSession(
                    self.artifact_uri, sess_options=opts, providers=["CPUExecutionProvider"]
                )
                inp = self.session.get_inputs()[0]
                self.input_name = inp.name
                shape = inp.shape
                if isinstance(shape, (list, tuple)) and len(shape) == 4:
                    h = shape[2] if isinstance(shape[2], int) else 224
                    w = shape[3] if isinstance(shape[3], int) else 224
                    self._input_shape = (h, w)
                self._configure_preprocessing()
                logger.info("loaded ONNX classifier %s", self.artifact_uri)
            else:
                logger.warning("classifier artifact %s missing; using fallback", self.artifact_uri)
        except Exception as exc:
            logger.warning("could not load ONNX %s (%s); using fallback", self.artifact_uri, exc)
        self._loaded = True

    def _configure_preprocessing(self) -> None:
        """Honour ONNX Model Zoo ``preprocessing`` metadata when present.

        Model-zoo classification models declare e.g.
        ``{"format":"BGR","mean":[123.68,116.9,103.94],"normalize":{"std":[58.8,58.6,57.4]}}``
        meaning they expect raw 0-255 pixels and normalise in the graph or via
        these stats. We reproduce the documented preprocessing on the client
        side so any zoo model or torchvision export gets fed correctly.
        """
        try:
            meta = self.session.get_modelmeta()  # type: ignore[union-attr]
            props = dict(meta.custom_metadata_map or {})
        except Exception:  # pragma: no cover - metadata is optional
            return
        raw = props.get("preprocessing")
        if not raw:
            return
        try:
            pp = json.loads(raw)
        except (TypeError, ValueError):
            return
        fmt = str(pp.get("format", "RGB")).upper()
        self._input_format = fmt
        mean = pp.get("mean")
        norm = pp.get("normalize") if isinstance(pp.get("normalize"), dict) else {}
        std = norm.get("std") or pp.get("std")
        scale = pp.get("scale")
        if isinstance(mean, (list, tuple)) and len(mean) == 3:
            self._norm_mean = np.array(mean, dtype=np.float32)
        if isinstance(std, (list, tuple)) and len(std) == 3:
            self._norm_std = np.array(std, dtype=np.float32)
        elif scale in ("255", 255):
            self._norm_mean = np.zeros(3, dtype=np.float32)
            self._norm_std = np.full(3, 255.0, dtype=np.float32)
        if isinstance(scale, (list, tuple)) and len(scale) == 3:
            self._norm_mean = np.zeros(3, dtype=np.float32)
            self._norm_std = np.array(scale, dtype=np.float32)
        if float(np.max(self._norm_mean)) > 1.5 or float(np.max(self._norm_std)) > 1.5:
            # Stats are in 0-255 pixel space (ONNX Model Zoo convention).
            self._scale_to_unit = False
        logger.info("classifier preprocessing from metadata: format=%s mean=%s std=%s",
                    self._input_format, self._norm_mean.tolist(), self._norm_std.tolist())

    def predict(self, image: np.ndarray) -> InferenceResult:
        t0 = time.perf_counter()
        blob = self._preprocess(image)
        t1 = time.perf_counter()
        logits = self._infer(blob, image)
        t2 = time.perf_counter()
        preds = self._postprocess(logits)
        t3 = time.perf_counter()
        return InferenceResult(
            task_type="classification",
            predictions=preds,
            preprocessing_ms=int((t1 - t0) * 1000),
            inference_ms=int((t2 - t1) * 1000),
            postprocessing_ms=int((t3 - t2) * 1000),
        )

    def _preprocess(self, image: np.ndarray) -> np.ndarray:
        import cv2
        th, tw = self._input_shape
        arr = image  # OpenCV loads BGR
        if self._input_format == "RGB":
            arr = cv2.cvtColor(arr, cv2.COLOR_BGR2RGB)
        resized = cv2.resize(arr, (tw, th)).astype(np.float32)
        if self._scale_to_unit:
            resized = resized / 255.0
        resized = (resized - self._norm_mean) / self._norm_std
        blob = resized.transpose(2, 0, 1)[None, ...]
        return np.ascontiguousarray(blob)

    def _infer(self, blob: np.ndarray, orig_image: np.ndarray) -> np.ndarray:
        if self.session is not None:
            out = self.session.run(None, {self.input_name: blob})
            return out[0].flatten()
        return self._heuristic_logits(orig_image)

    def _heuristic_logits(self, image: np.ndarray) -> np.ndarray:
        # Use image color averages to deterministically pick 2-3 top labels.
        avg = image.reshape(-1, 3).mean(axis=0)
        seed = int(avg.sum() * 1000) % (2**32)
        rng = np.random.default_rng(seed)
        n_classes = 1000
        logits = rng.normal(-3, 1, size=n_classes).astype(np.float32)
        # bias toward a few ImageNet labels for nicer demos.
        for i, _lbl in enumerate(IMAGENET_SAMPLE):
            logits[i] += 1.5 + rng.random()
        return logits

    def _postprocess(self, logits: np.ndarray) -> list[Prediction]:
        # softmax
        m = logits.max()
        ex = np.exp(logits - m)
        probs = ex / ex.sum()
        k = min(self.top_k, len(probs))
        idx = np.argpartition(-probs, k - 1)[:k]
        idx = idx[np.argsort(-probs[idx])]
        out: list[Prediction] = []
        labels = self._labels or []
        for i in idx:
            label = labels[i] if labels and i < len(labels) else f"class_{i}"
            out.append(Prediction(label=label, confidence=float(probs[i])))
        return out
