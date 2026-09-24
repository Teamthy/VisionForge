export type Role = "USER" | "ADMIN";

export interface User {
  id: string;
  email: string;
  name: string;
  role: Role;
  created_at: string;
  last_login_at?: string;
}

export interface Project {
  id: string;
  owner_id: string;
  name: string;
  description: string;
  created_at: string;
  updated_at: string;
}

export type JobStatus =
  | "CREATED"
  | "QUEUED"
  | "RUNNING"
  | "SUCCESS"
  | "FAILED"
  | "RETRYING"
  | "DEAD"
  | "CANCELLED";

export interface BBox { x: number; y: number; width: number; height: number; }
export interface Detection { label: string; confidence: number; bbox: BBox; }
export interface Prediction { label: string; confidence: number; }

export interface Asset {
  id: string;
  project_id: string;
  owner_id: string;
  filename: string;
  content_type: string;
  size_bytes: number;
  storage_key: string;
  checksum: string;
  created_at: string;
}

export interface Model {
  id: string;
  name: string;
  description: string;
  task_type: "object_detection" | "classification";
  created_at: string;
}

export type ModelVersionStatus = "ACTIVE" | "STAGING" | "INACTIVE" | "DEPRECATED";
export type Runtime = "pytorch" | "onnx";

export interface ModelVersion {
  id: string;
  model_id: string;
  version: string;
  artifact_uri: string;
  runtime: Runtime;
  status: ModelVersionStatus;
  metadata?: Record<string, any>;
  created_at: string;
}

export interface InferenceJob {
  id: string;
  project_id: string;
  asset_id: string;
  model_version_id: string;
  status: JobStatus;
  priority: number;
  attempts: number;
  max_attempts: number;
  idempotency_key?: string;
  error_code?: string;
  error_message?: string;
  queued_at?: string;
  started_at?: string;
  completed_at?: string;
  created_at: string;
  updated_at: string;
}

export interface JobResult {
  job_id: string;
  model_version_id: string;
  status: JobStatus;
  processing_time_ms: number;
  inference_time_ms: number;
  detections?: Detection[];
  predictions?: Prediction[];
  error?: { code: string; message: string };
  completed_at: string;
}

export interface Paginated<T> {
  items: T[];
  meta: { next_cursor?: string; has_more: boolean };
}

export interface DashboardStats {
  total_projects: number;
  total_assets: number;
  jobs_processed: number;
  jobs_succeeded: number;
  jobs_failed: number;
  queue_depth: number;
  ml_service_ok: boolean;
  object_storage_ok: boolean;
}

export interface Envelope<T> {
  data: T;
  meta?: { next_cursor?: string; has_more: boolean; request_id?: string };
}

export interface ErrorEnvelope {
  error: { code: string; message: string; request_id?: string };
}
