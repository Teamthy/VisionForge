// VisionForge API load test (k6).
//
// Run against a live stack:
//   k6 run -e BASE=http://localhost:8080/api/v1 loadtest/api_load.js
//
// Scenarios:
//   1. burst registration + login (auth path under load)
//   2. steady read load on project/model lists (cache-less DB reads)
//   3. inference submission fan-out (queue pressure + idempotency)
//   4. health/metrics endpoints (should stay flat while the queue drains)
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';
import encoding from 'k6/encoding';

const BASE = __ENV.BASE || 'http://localhost:8080/api/v1';
const errors = new Rate('errors');
const jobLatency = new Trend('job_completion_seconds', true);

// 16x8 white JPEG sufficient for the ML service.
function tinyJpeg() {
  const b64 =
    '/9j/4AAQSkZJRgABAQEAYABgAAD/2wBDAAgGBgcGBQgHBwcJCQgKDBQNDAsLDBkSEw8UHRofHh0a' +
    'HBwgJC4nICIsIxwcKDcpLDAxNDQ0Hyc5PTgyPC4zNDL/wAALCAAAQBABAREA/8QAHwAAAQUBAQEBAQ' +
    'AAAAAAAAAAAQIDBAUGBwgJCgv/xAC1EAACAQMDAgQDBQUEBAAAAX0BAgMABBEFEiExQQYTUWEHInEU' +
    'MoGRoQgjQlKxFVLB0eLh8//aAAwDAQACEQMRAD8A+zKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKK' +
    'KKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKK' +
    'KKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKK' +
    'KKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKK' +
    'KKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKK' +
    'KKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKK' +
    'KKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKK' +
    'KKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKKK' +
    'KKKKKKKKKKKKKKKKKKKKK//2Q==';
  return encoding.b64decode(b64, 'std', 's');
}

export const options = {
  scenarios: {
    auth_burst: { executor: 'ramping-vus', startVUs: 0, stages: [
      { duration: '20s', target: 20 }, { duration: '40s', target: 20 }, { duration: '10s', target: 0 }] },
    reads: { executor: 'constant-arrival-rate', rate: 30, timeUnit: '1s', duration: '60s', preAllocatedVUs: 10, maxVUs: 40 },
    inference: { executor: 'constant-arrival-rate', rate: 5, timeUnit: '1s', duration: '60s', preAllocatedVUs: 5, maxVUs: 25 },
    health: { executor: 'constant-arrival-rate', rate: 1, timeUnit: '1s', duration: '70s', preAllocatedVUs: 2 },
  },
  thresholds: {
    http_req_failed: ['rate<0.02'],          // <2% HTTP failures under load
    errors: ['rate<0.02'],
    job_completion_seconds: ['p95<60'],       // jobs must drain within a minute
    http_req_duration: ['p95<500'],           // read-path p95 under 0.5s
  },
};

const state = { tokens: [], projectId: null, assetId: null, modelVersionId: null };

function newEmail() {
  return `k6${Math.random().toString(36).slice(2, 10)}@loadtest.local`;
}

function login() {
  const email = newEmail();
  const r = http.post(`${BASE}/auth/register`,
    JSON.stringify({ email, password: 'L0adTest!pw', name: 'K6 User' }),
    { headers: { 'Content-Type': 'application/json' } });
  const ok = check(r, { 'register 201/429': (x) => x.status === 201 || x.status === 429 });
  if (!ok || r.status !== 201) { errors.add(1); return null; }
  return JSON.parse(r.body).data.access_token;
}

export function setup() {
  const token = login();
  if (!token) throw new Error('setup register failed');
  const p = http.post(`${BASE}/projects`, JSON.stringify({ name: 'k6 project', description: 'load' }),
    { headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` } });
  check(p, { 'project created': (r) => r.status === 201 });
  const projectId = JSON.parse(p.body).data.id;

  const m = http.get(`${BASE}/models`, { headers: { Authorization: `Bearer ${token}` } });
  const models = JSON.parse(m.body).data;
  const det = models.find((x) => x.task_type === 'object_detection');
  const v = http.get(`${BASE}/models/${det.id}/versions`, { headers: { Authorization: `Bearer ${token}` } });
  const mv = JSON.parse(v.body).data[0];

  return { token, projectId, modelVersionId: mv.id };
}

export function auth_burst(data) {
  const t = login();
  errors.add(t === null ? 1 : 0);
  sleep(1);
}

export function reads(data) {
  const token = state.tokens.length ? state.tokens[Math.floor(Math.random() * state.tokens.length)] : data.token;
  const r = http.batch([
    ['GET', `${BASE}/projects`, null, { headers: { Authorization: `Bearer ${token}` } }],
    ['GET', `${BASE}/models`, null, { headers: { Authorization: `Bearer ${token}` } }],
    ['GET', `${BASE}/dashboard/stats`, null, { headers: { Authorization: `Bearer ${token}` } }],
  ]);
  check(r, { 'reads ok': (x) => x.every((v) => v.status === 200) });
}

export function inference(data) {
  const jpeg = tinyJpeg();
  const presign = http.post(`${BASE}/projects/${data.projectId}/assets`,
    JSON.stringify({ filename: 'k6.jpg', content_type: 'image/jpeg', size_bytes: jpeg.byteLength }),
    { headers: { Content-Type: 'application/json', Authorization: `Bearer ${data.token}` } });
  if (presign.status !== 201) { errors.add(1); return; }
  const body = JSON.parse(presign.body);

  const put = http.request('PUT', body.data.upload_url, jpeg,
    { headers: { 'Content-Type': 'image/jpeg' } });
  if (put.status >= 400) { errors.add(1); return; }

  const confirm = http.post(`${BASE}/assets/${body.data.asset.id}/confirm`,
    JSON.stringify({ asset_id: body.data.asset.id }),
    { headers: { Content-Type: 'application/json', Authorization: `Bearer ${data.token}` } });

  const start = Date.now();
  const job = http.post(`${BASE}/projects/${data.projectId}/inference/jobs`,
    JSON.stringify({ asset_id: body.data.asset.id, model_version_id: data.modelVersionId }),
    { headers: { Content-Type: 'application/json', Authorization: `Bearer ${data.token}` } });
  const okj = check(job, { 'job accepted': (r) => r.status === 202 });
  if (okj) {
    const jobId = JSON.parse(job.body).data.id;
    // Poll with backoff up to 90s (async pipeline through worker).
    for (let i = 0; i < 60; i++) {
      const st = http.get(`${BASE}/jobs/${jobId}`, { headers: { Authorization: `Bearer ${data.token}` } });
      const s = JSON.parse(st.body).data.status;
      if (s === 'SUCCESS') { jobLatency.add((Date.now() - start) / 1000); errors.add(0); return; }
      if (s === 'FAILED' || s === 'DEAD') { errors.add(1); return; }
      sleep(1.5);
    }
    errors.add(1);
  }
}

export function health() {
  const r = http.get(`${BASE.replace('/api/v1', '')}/ready`);
  check(r, { 'ready 200': (x) => x.status === 200 });
}
