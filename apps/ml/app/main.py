"""VisionForge ML inference service.

Exposes:

* GET  /health           - liveness + loaded models
* GET  /metrics          - prometheus metrics
* POST /predict          - sync inference from raw body (image bytes)
* POST /predict_url      - inference where the service downloads the asset
"""
from __future__ import annotations

import logging
import os
import time

import cv2
import httpx
import numpy as np
from fastapi import FastAPI, File, Form, HTTPException, Request, Response, UploadFile
from fastapi.responses import JSONResponse
from prometheus_client import CONTENT_TYPE_LATEST, Counter, Histogram, generate_latest
from pydantic import BaseModel, Field

from .models.base import InferenceResult
from .models.registry import ModelRegistry, build_default_registry

logging.basicConfig(
    level=os.environ.get("LOG_LEVEL", "INFO").upper(),
    format="%(asctime)s %(levelname)s %(name)s %(message)s",
)
logger = logging.getLogger("ml")

ARTIFACTS_DIR = os.environ.get("ML_ARTIFACTS_DIR", "./artifacts")
MAX_IMAGE_BYTES = int(os.environ.get("ML_MAX_IMAGE_BYTES", str(20 * 1024 * 1024)))
MAX_INFERENCE_MS = int(os.environ.get("ML_MAX_INFERENCE_MS", "60000"))
OBJECT_STORAGE_ENDPOINT = os.environ.get("S3_ENDPOINT", "http://minio:9000")

# App + registry
app = FastAPI(title="VisionForge ML Service", version="1.0.0")
registry: ModelRegistry = build_default_registry(ARTIFACTS_DIR)

# Metrics
PREDICT_COUNTER = Counter(
    "visionforge_ml_predictions_total",
    "Total predictions",
    ["model", "version", "task_type", "status"],
)
PREDICT_DURATION = Histogram(
    "visionforge_ml_predict_duration_seconds",
    "Prediction duration",
    ["model", "version"],
)


@app.on_event("startup")
def _startup() -> None:
    os.makedirs(ARTIFACTS_DIR, exist_ok=True)
    logger.info("warming models (lazy load)")


@app.get("/health")
def health() -> dict:
    loaded = registry.list_loaded()
    return {"status": "ok", "loaded_models": loaded, "version": "1.0.0"}


@app.get("/metrics")
def metrics() -> Response:
    return Response(content=generate_latest(), media_type=CONTENT_TYPE_LATEST)


# ── Request models ───────────────────────────────────────

class PredictURLRequest(BaseModel):
    model_version_id: str = Field(..., min_length=1)
    artifact_uri: str | None = None
    task_type: str | None = None
    storage_key: str | None = None
    asset_url: str | None = None


# ── Inference endpoints ─────────────────────────────────

@app.post("/predict")
async def predict(
    request: Request,
    model_version_id: str = Form(...),
    file: UploadFile = File(...),
    artifact_uri: str | None = Form(None),
    task_type: str | None = Form(None),
):
    start = time.perf_counter()
    model, meta = registry.resolve(model_version_id, artifact_uri, task_type)
    if model is None or meta is None:
        raise HTTPException(status_code=404, detail=f"unknown model version {model_version_id}")
    data = await file.read()
    if len(data) > MAX_IMAGE_BYTES:
        raise HTTPException(status_code=413, detail="payload too large")
    img = _decode_image(data)
    if img is None:
        raise HTTPException(status_code=400, detail="invalid image")
    try:
        result = model.predict(img)
    except Exception:
        logger.exception("inference failed")
        PREDICT_COUNTER.labels(meta.name, meta.version, meta.task_type, "error").inc()
        raise HTTPException(status_code=500, detail="inference error") from None
    dur = time.perf_counter() - start
    PREDICT_COUNTER.labels(meta.name, meta.version, meta.task_type, "ok").inc()
    PREDICT_DURATION.labels(meta.name, meta.version).observe(dur)
    return _response(model_version_id, result)


@app.post("/predict_url")
async def predict_url(req: PredictURLRequest):
    start = time.perf_counter()
    model, meta = registry.resolve(req.model_version_id, req.artifact_uri, req.task_type)
    if model is None or meta is None:
        raise HTTPException(status_code=404, detail=f"unknown model version {req.model_version_id}")
    data = await _fetch_asset(req.asset_url, req.storage_key)
    img = _decode_image(data)
    if img is None:
        raise HTTPException(status_code=400, detail="invalid image")
    try:
        result = model.predict(img)
    except Exception:
        logger.exception("inference failed")
        PREDICT_COUNTER.labels(meta.name, meta.version, meta.task_type, "error").inc()
        raise HTTPException(status_code=500, detail="inference error") from None
    dur = time.perf_counter() - start
    PREDICT_COUNTER.labels(meta.name, meta.version, meta.task_type, "ok").inc()
    PREDICT_DURATION.labels(meta.name, meta.version).observe(dur)
    return _response(req.model_version_id, result)


# ── Helpers ──────────────────────────────────────────────

def _response(model_version_id: str, result: InferenceResult) -> dict:
    body = result.to_dict()
    body["model_version_id"] = model_version_id
    return body


def _decode_image(data: bytes) -> np.ndarray | None:
    arr = np.frombuffer(data, dtype=np.uint8)
    img = cv2.imdecode(arr, cv2.IMREAD_COLOR)
    if img is None:
        return None
    # Safety: refuse enormous images.
    h, w = img.shape[:2]
    max_dim = int(os.environ.get("ML_MAX_IMAGE_DIMENSION", "4096"))
    if h > max_dim or w > max_dim:
        scale = max_dim / float(max(h, w))
        img = cv2.resize(img, (int(w * scale), int(h * scale)))
    return img


async def _fetch_asset(asset_url: str | None, storage_key: str | None) -> bytes:
    url = asset_url
    if url is None and storage_key:
        # In-cluster fallback: hit MinIO directly.
        url = f"{OBJECT_STORAGE_ENDPOINT}/visionforge/{storage_key}"
    if not url:
        raise HTTPException(status_code=400, detail="asset_url or storage_key required")
    timeout = httpx.Timeout(30.0, connect=5.0)
    async with httpx.AsyncClient(timeout=timeout, follow_redirects=True) as client:
        resp = await client.get(url)
        if resp.status_code >= 400:
            raise HTTPException(
                status_code=502, detail=f"could not fetch asset: status {resp.status_code}"
            )
        data = resp.content
        if len(data) > MAX_IMAGE_BYTES:
            raise HTTPException(status_code=413, detail="asset too large")
        return data


@app.exception_handler(Exception)
async def _unhandled(_: Request, exc: Exception):
    logger.exception("unhandled error")
    return JSONResponse(
        status_code=500,
        content={"error": {"code": "INTERNAL_ERROR", "message": "internal error"}},
    )
