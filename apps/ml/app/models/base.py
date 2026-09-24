"""Model abstraction layer for VisionForge CV models."""
from __future__ import annotations

from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from typing import Any

import numpy as np


@dataclass
class BoundingBox:
    x: int
    y: int
    width: int
    height: int


@dataclass
class Detection:
    label: str
    confidence: float
    bbox: BoundingBox

    def to_dict(self) -> dict[str, Any]:
        return {
            "label": self.label,
            "confidence": round(float(self.confidence), 4),
            "bbox": {
                "x": self.bbox.x, "y": self.bbox.y,
                "width": self.bbox.width, "height": self.bbox.height,
            },
        }


@dataclass
class Prediction:
    label: str
    confidence: float

    def to_dict(self) -> dict[str, Any]:
        return {"label": self.label, "confidence": round(float(self.confidence), 4)}


@dataclass
class InferenceResult:
    task_type: str
    detections: list[Detection] = field(default_factory=list)
    predictions: list[Prediction] = field(default_factory=list)
    preprocessing_ms: int = 0
    inference_ms: int = 0
    postprocessing_ms: int = 0

    @property
    def total_ms(self) -> int:
        return self.preprocessing_ms + self.inference_ms + self.postprocessing_ms

    def to_dict(self) -> dict[str, Any]:
        return {
            "task_type": self.task_type,
            "detections": [d.to_dict() for d in self.detections],
            "predictions": [p.to_dict() for p in self.predictions],
            "inference_time_ms": self.inference_ms,
            "processing_time_ms": self.total_ms,
        }


@dataclass
class ModelMetadata:
    name: str
    version: str
    task_type: str
    runtime: str
    input_shape: tuple[int, ...] = ()
    labels: list[str] = field(default_factory=list)

    def to_dict(self) -> dict[str, Any]:
        return {
            "name": self.name,
            "version": self.version,
            "task_type": self.task_type,
            "runtime": self.runtime,
            "input_shape": list(self.input_shape),
            "labels": self.labels,
        }


class VisionModel(ABC):
    """Abstract base class for all CV models."""

    def __init__(self, artifact_uri: str, metadata: ModelMetadata):
        self.artifact_uri = artifact_uri
        self.metadata = metadata
        self._loaded = False

    @abstractmethod
    def load(self) -> None:
        """Load model weights (idempotent)."""

    @abstractmethod
    def predict(self, image: np.ndarray) -> InferenceResult:
        """Run inference on a BGR image (as loaded by OpenCV)."""

    def is_loaded(self) -> bool:
        return self._loaded
