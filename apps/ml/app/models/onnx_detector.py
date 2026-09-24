"""ONNX-based object detection (YOLOv8-style), with heuristic fallback."""
from __future__ import annotations

import logging
import os
import time

import numpy as np

from .base import BoundingBox, Detection, InferenceResult, ModelMetadata, VisionModel

logger = logging.getLogger(__name__)

COCO_LABELS = [
    "person", "bicycle", "car", "motorcycle", "airplane", "bus", "train", "truck",
    "boat", "traffic light", "fire hydrant", "stop sign", "parking meter", "bench",
    "bird", "cat", "dog", "horse", "sheep", "cow", "elephant", "bear", "zebra",
    "giraffe", "backpack", "umbrella", "handbag", "tie", "suitcase", "frisbee",
    "skis", "snowboard", "sports ball", "kite", "baseball bat", "baseball glove",
    "skateboard", "surfboard", "tennis racket", "bottle", "wine glass", "cup",
    "fork", "knife", "spoon", "bowl", "banana", "apple", "sandwich", "orange",
    "broccoli", "carrot", "hot dog", "pizza", "donut", "cake", "chair", "couch",
    "potted plant", "bed", "dining table", "toilet", "tv", "laptop", "mouse",
    "remote", "keyboard", "cell phone", "microwave", "oven", "toaster", "sink",
    "refrigerator", "book", "clock", "vase", "scissors", "teddy bear",
    "hair drier", "toothbrush",
]


class ONNXObjectDetector(VisionModel):
    def __init__(self, artifact_uri: str, metadata: ModelMetadata, conf_threshold: float = 0.35):
        super().__init__(artifact_uri, metadata)
        self.conf_threshold = conf_threshold
        self.session = None
        self.input_name = None
        self._input_shape = (640, 640)

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
                    h = shape[2] if isinstance(shape[2], int) else 640
                    w = shape[3] if isinstance(shape[3], int) else 640
                    self._input_shape = (h, w)
                logger.info("loaded ONNX detector %s", self.artifact_uri)
            else:
                logger.warning("artifact %s not found; using heuristic fallback", self.artifact_uri)
        except Exception as exc:
            logger.warning("could not load ONNX %s (%s); using fallback", self.artifact_uri, exc)
        self._loaded = True

    def predict(self, image: np.ndarray) -> InferenceResult:
        t0 = time.perf_counter()
        h, w = image.shape[:2]
        blob, ratio = self._preprocess(image)
        t1 = time.perf_counter()
        raw = self._infer(blob, (h, w))
        t2 = time.perf_counter()
        dets = self._postprocess(raw, (w, h), ratio)
        t3 = time.perf_counter()
        return InferenceResult(
            task_type="object_detection",
            detections=dets,
            preprocessing_ms=int((t1 - t0) * 1000),
            inference_ms=int((t2 - t1) * 1000),
            postprocessing_ms=int((t3 - t2) * 1000),
        )

    def _preprocess(self, image: np.ndarray):
        """Letterbox-resize to the model input, returning the downscale ratio.

        YOLOv8 ONNX exports emit box coordinates in *input pixel space*
        (0..input_dim), so mapping back to original pixels is a division by
        the same ratio used to shrink the image here.
        """
        import cv2
        th, tw = self._input_shape
        ratio = max(image.shape[0] / th, image.shape[1] / tw)
        new_h, new_w = round(image.shape[0] / ratio), round(image.shape[1] / ratio)
        resized = cv2.resize(image, (new_w, new_h))
        canvas = np.full((th, tw, 3), 114, dtype=np.float32)  # ultralytics pad value
        canvas[:new_h, :new_w] = resized.astype(np.float32) / 255.0
        blob = canvas.transpose(2, 0, 1)[None, ...]
        return np.ascontiguousarray(blob), ratio

    def _infer(self, blob: np.ndarray, orig_hw):
        if self.session is not None:
            out = self.session.run(None, {self.input_name: blob})
            return out[0]
        return self._heuristic_output(blob, orig_hw)

    def _heuristic_output(self, blob: np.ndarray, orig_hw):
        """Deterministic fallback used when no ONNX artifact is present.

        Emits boxes in the same 640-pixel input space as the real export so
        downstream post-processing is identical for both paths.
        """
        arr = blob[0]
        mean_val = float(arr.mean())
        std_val = float(arr.std())
        rng = np.random.default_rng(seed=int(mean_val * 1000) % (2**32))
        num = int(np.clip(1 + rng.integers(0, 3), 1, 3))
        dim = self._input_shape[1]
        out = np.zeros((1, 4 + len(COCO_LABELS), 8400), dtype=np.float32)
        for i in range(num):
            cx = rng.uniform(0.1, 0.9) * dim
            cy = rng.uniform(0.1, 0.9) * dim
            bw = rng.uniform(0.1, 0.4) * dim
            bh = rng.uniform(0.15, 0.5) * dim
            out[0, 0, i] = cx
            out[0, 1, i] = cy
            out[0, 2, i] = bw
            out[0, 3, i] = bh
            cls_scores = rng.dirichlet(np.ones(len(COCO_LABELS)) * 0.3) * (0.6 + std_val)
            out[0, 4:, i] = cls_scores.astype(np.float32)
        return out

    def _postprocess(self, raw: np.ndarray, orig_wh, ratio: float):
        """Decode a YOLOv8 [1, 84, 8400] tensor (boxes in input-pixel space)."""
        dets: list[Detection] = []
        if raw is None:
            return dets
        if raw.ndim != 3:
            return dets
        # Normalise layout to [N, 84]: rows of [cx, cy, w, h, cls...].
        arr = raw[0]
        if arr.shape[0] == 4 + len(COCO_LABELS) and arr.shape[1] != 4 + len(COCO_LABELS):
            arr = arr.T
        elif arr.shape[1] != 4 + len(COCO_LABELS):
            logger.warning("unexpected detector output shape %s; skipping decode", raw.shape)
            return dets
        boxes = arr[:, :4]
        scores = arr[:, 4:]
        cls_ids = scores.argmax(axis=1)
        cls_scores = scores.max(axis=1)
        if float(cls_scores.max(initial=0.0)) > 1.5:
            # Some exports emit pre-sigmoid logits; normalise so the
            # confidence threshold means the same thing for both variants.
            cls_scores = 1.0 / (1.0 + np.exp(-np.clip(cls_scores, -30, 30)))
        keep = cls_scores >= self.conf_threshold
        # Reject degenerate anchors that are far too small in input space.
        wh = arr[:, 2:4]
        keep &= (wh[:, 0] >= 4.0) & (wh[:, 1] >= 4.0)
        if not keep.any():
            return dets
        cand = np.flatnonzero(keep)
        if cand.size > 300:  # only top 300 candidates by score
            cand = cand[np.argsort(-cls_scores[cand])[:300]]
        for i in cand:
            cx, cy, bw, bh = boxes[i]
            # input-pixel -> original-pixel
            x = int(cx / ratio - (bw / 2) / ratio)
            y = int(cy / ratio - (bh / 2) / ratio)
            w = int(bw / ratio)
            h = int(bh / ratio)
            x = max(0, min(x, orig_wh[0] - 1))
            y = max(0, min(y, orig_wh[1] - 1))
            w = max(1, min(w, orig_wh[0] - x))
            h = max(1, min(h, orig_wh[1] - y))
            cid = int(cls_ids[i])
            label = COCO_LABELS[cid] if 0 <= cid < len(COCO_LABELS) else f"class_{cid}"
            dets.append(Detection(
                label=label, confidence=float(cls_scores[i]), bbox=BoundingBox(x, y, w, h)
            ))
        return _nms(dets, iou_threshold=0.5)


def _iou(a: BoundingBox, b: BoundingBox) -> float:
    ax1, ay1 = a.x, a.y
    ax2, ay2 = a.x + a.width, a.y + a.height
    bx1, by1 = b.x, b.y
    bx2, by2 = b.x + b.width, b.y + b.height
    ix1, iy1 = max(ax1, bx1), max(ay1, by1)
    ix2, iy2 = min(ax2, bx2), min(ay2, by2)
    iw, ih = max(0, ix2 - ix1), max(0, iy2 - iy1)
    inter = iw * ih
    union = a.width * a.height + b.width * b.height - inter
    return inter / union if union > 0 else 0.0


def _nms(dets: list[Detection], iou_threshold: float = 0.5) -> list[Detection]:
    dets.sort(key=lambda d: d.confidence, reverse=True)
    kept: list[Detection] = []
    while dets:
        d = dets.pop(0)
        kept.append(d)
        dets = [x for x in dets if x.label != d.label or _iou(d.bbox, x.bbox) < iou_threshold]
    return kept
