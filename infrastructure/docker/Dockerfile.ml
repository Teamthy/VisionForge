# syntax=docker/dockerfile:1.6
# Build context: repository root.
FROM python:3.11-slim AS builder
ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1
RUN apt-get update && apt-get install -y --no-install-recommends build-essential \
    libglib2.0-0 libsm6 libxext6 libgl1 \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY apps/ml/requirements.txt .
RUN pip install --prefix=/install -r requirements.txt

FROM python:3.11-slim
ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PYTHONPATH=/app
RUN apt-get update && apt-get install -y --no-install-recommends \
    libglib2.0-0 libsm6 libxext6 libgl1 libgomp1 curl \
    && rm -rf /var/lib/apt/lists/* \
    && useradd -m -u 10001 ml
COPY --from=builder /install /usr/local
WORKDIR /app
COPY apps/ml /app
RUN mkdir -p /app/artifacts && chown -R ml:ml /app
USER ml
EXPOSE 8090
HEALTHCHECK --interval=15s --timeout=5s --retries=3 CMD curl -sf http://localhost:8090/health || exit 1
CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8090", "--workers", "2"]
