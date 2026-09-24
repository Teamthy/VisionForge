"use client";

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useParams } from "next/navigation";
import Link from "next/link";
import { api, apiError } from "@/lib/api";
import type { Asset, InferenceJob, JobResult, ModelVersion } from "@/types";
import JobBadge from "@/components/JobBadge";
import { useEffect, useRef, useState } from "react";
import ResultCanvas from "@/components/ResultCanvas";

export default function JobDetailPage() {
  const { id } = useParams<{ id: string }>();
  const qc = useQueryClient();
  const jobQ = useQuery({ queryKey: ["job", id], queryFn: async () => (await api.get(`/jobs/${id}`)).data.data as InferenceJob, refetchInterval: (q) => q.state.data?.status === "SUCCESS" || q.state.data?.status === "DEAD" || q.state.data?.status === "CANCELLED" ? false : 3000 });
  const resultQ = useQuery({ queryKey: ["job-result", id], queryFn: async () => (await api.get(`/jobs/${id}/result`)).data.data as JobResult, enabled: jobQ.data?.status === "SUCCESS" });
  const assetQ = useQuery({ queryKey: ["asset", jobQ.data?.asset_id], queryFn: async () => (await api.get(`/assets/${jobQ.data!.asset_id}`)).data.data as Asset, enabled: !!jobQ.data?.asset_id });
  const versionQ = useQuery({ queryKey: ["model-version", jobQ.data?.model_version_id], queryFn: async () => (await api.get(`/model-versions/${jobQ.data!.model_version_id}`)).data.data as ModelVersion, enabled: !!jobQ.data?.model_version_id });

  const retry = useMutation({ mutationFn: async () => (await api.post(`/jobs/${id}/retry`, {})), onSuccess: () => qc.invalidateQueries({ queryKey: ["job", id] }) });

  const job = jobQ.data;
  if (!job) return <div className="text-muted">Loading…</div>;
  const errMsg = job.error_message ? <div className="text-danger text-sm mt-2">Error: {job.error_message}</div> : null;

  return (
    <div className="space-y-6">
      <header>
        <div className="text-xs text-muted"><Link href="/jobs" className="link">Inference Jobs</Link> / <span className="font-mono">{job.id.slice(0,12)}…</span></div>
        <div className="flex items-center gap-3 mt-2">
          <h1 className="text-2xl font-semibold font-mono">{job.id}</h1>
          <JobBadge status={job.status} />
        </div>
      </header>

      <section className="grid md:grid-cols-3 gap-4">
        <div className="card">
          <h3 className="text-sm text-muted mb-2">Details</h3>
          <dl className="text-sm space-y-1">
            <div className="flex justify-between"><dt className="text-muted">Priority</dt><dd>{job.priority}</dd></div>
            <div className="flex justify-between"><dt className="text-muted">Attempts</dt><dd>{job.attempts}/{job.max_attempts}</dd></div>
            <div className="flex justify-between"><dt className="text-muted">Created</dt><dd className="text-xs">{new Date(job.created_at).toLocaleString()}</dd></div>
            <div className="flex justify-between"><dt className="text-muted">Queued</dt><dd className="text-xs">{job.queued_at ? new Date(job.queued_at).toLocaleString() : "—"}</dd></div>
            <div className="flex justify-between"><dt className="text-muted">Started</dt><dd className="text-xs">{job.started_at ? new Date(job.started_at).toLocaleString() : "—"}</dd></div>
            <div className="flex justify-between"><dt className="text-muted">Completed</dt><dd className="text-xs">{job.completed_at ? new Date(job.completed_at).toLocaleString() : "—"}</dd></div>
          </dl>
          {(job.status === "FAILED" || job.status === "DEAD") && (
            <button className="btn btn-primary mt-4" disabled={retry.isPending} onClick={() => retry.mutate()}>
              {retry.isPending ? "Retrying…" : "Retry job"}
            </button>
          )}
          {errMsg}
        </div>
        <div className="card">
          <h3 className="text-sm text-muted mb-2">Model</h3>
          {versionQ.data ? (
            <div>
              <div className="font-medium">{versionQ.data.id.slice(0,8)}…</div>
              <div className="text-xs text-muted mt-1">Runtime: {versionQ.data.runtime}</div>
              <div className="text-xs text-muted">Version: {versionQ.data.version}</div>
              <div className="text-xs mt-2"><span className="badge">{versionQ.data.status}</span></div>
            </div>
          ) : <div className="text-muted text-sm">…</div>}
        </div>
        <div className="card">
          <h3 className="text-sm text-muted mb-2">Timing</h3>
          {resultQ.data ? (
            <dl className="text-sm space-y-1">
              <div className="flex justify-between"><dt className="text-muted">Processing</dt><dd>{resultQ.data.processing_time_ms} ms</dd></div>
              <div className="flex justify-between"><dt className="text-muted">Inference</dt><dd>{resultQ.data.inference_time_ms} ms</dd></div>
            </dl>
          ) : <div className="text-muted text-sm">Not yet available.</div>}
        </div>
      </section>

      {resultQ.data && assetQ.data && (
        <ResultVisualization asset={assetQ.data} result={resultQ.data} />
      )}
    </div>
  );
}

function ResultVisualization({ asset, result }: { asset: Asset; result: JobResult }) {
  const [imageUrl, setImageUrl] = useState<string | null>(null);
  const [confThreshold, setConfThreshold] = useState(0.3);
  const [imgDims, setImgDims] = useState<{ w: number; h: number } | null>(null);
  const imgRef = useRef<HTMLImageElement>(null);

  useEffect(() => {
    let cancel = false;
    (async () => {
      try {
        const resp = await api.get(`/assets/${asset.id}/download`);
        if (!cancel) setImageUrl(resp.data.data.url);
      } catch {
        if (!cancel) setImageUrl(null);
      }
    })();
    return () => { cancel = true; };
  }, [asset.id]);

  const detections = (result.detections || []).filter((d) => d.confidence >= confThreshold);
  const predictions = result.predictions || [];

  return (
    <section className="space-y-4">
      <header className="flex items-center justify-between">
        <h2 className="font-semibold">Result</h2>
        {detections.length > 0 && (
          <label className="flex items-center gap-2 text-sm">
            Confidence ≥ {confThreshold.toFixed(2)}
            <input type="range" min={0} max={1} step={0.05} value={confThreshold} onChange={(e) => setConfThreshold(parseFloat(e.target.value))} />
          </label>
        )}
      </header>
      {imageUrl && (
        <div className="card">
          {detections.length > 0 ? (
            <ResultCanvas src={imageUrl} detections={detections} />
          ) : (
            <img ref={imgRef} src={imageUrl} alt={asset.filename} className="max-w-full max-h-[70vh] rounded-md mx-auto block" onLoad={(e) => setImgDims({ w: (e.target as HTMLImageElement).naturalWidth, h: (e.target as HTMLImageElement).naturalHeight })} crossOrigin="anonymous" />
          )}
        </div>
      )}
      {predictions.length > 0 && (
        <div className="card">
          <h3 className="font-medium mb-2">Predictions</h3>
          <ul className="space-y-1">
            {predictions.map((p, i) => (
              <li key={i} className="flex items-center justify-between text-sm">
                <span>{p.label}</span>
                <span className="text-muted font-mono">{(p.confidence * 100).toFixed(1)}%</span>
              </li>
            ))}
          </ul>
        </div>
      )}
      {detections.length > 0 && (
        <div className="card">
          <h3 className="font-medium mb-2">Detections ({detections.length})</h3>
          <div className="overflow-x-auto">
            <table className="table">
              <thead><tr><th>Label</th><th>Confidence</th><th>BBox (x,y,w,h)</th></tr></thead>
              <tbody>
                {detections.map((d, i) => (
                  <tr key={i}>
                    <td>{d.label}</td>
                    <td>{(d.confidence * 100).toFixed(1)}%</td>
                    <td className="font-mono text-xs text-muted">{d.bbox.x}, {d.bbox.y}, {d.bbox.width}, {d.bbox.height}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </section>
  );
}
