"use client";

import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useParams } from "next/navigation";
import { api, apiError } from "@/lib/api";
import type { Asset, InferenceJob, Model, ModelVersion, Project } from "@/types";
import Link from "next/link";
import JobBadge from "@/components/JobBadge";
import { useState } from "react";
import { Upload, Play, Image as ImageIcon, Boxes } from "lucide-react";

export default function ProjectDetail() {
  const { id } = useParams<{ id: string }>();
  const qc = useQueryClient();
  const projQ = useQuery({ queryKey: ["project", id], queryFn: async () => (await api.get(`/projects/${id}`)).data.data as Project });
  const assetsQ = useQuery({ queryKey: ["project-assets", id], queryFn: async () => (await api.get(`/projects/${id}/assets`)).data.data as Asset[] });
  const jobsQ = useQuery({ queryKey: ["project-jobs", id], queryFn: async () => (await api.get(`/projects/${id}/jobs`)).data.data as InferenceJob[], refetchInterval: 5000 });
  const modelsQ = useQuery({ queryKey: ["models"], queryFn: async () => (await api.get("/models")).data.data as Model[] });

  const [file, setFile] = useState<File | null>(null);
  const [uploadStatus, setUploadStatus] = useState<string>("");
  const [modelVersionId, setModelVersionId] = useState<string>("");
  const [versionsForSelectedModel, setVersionsForSelectedModel] = useState<ModelVersion[]>([]);
  const [versionsMap, setVersionsMap] = useState<Record<string, ModelVersion[]>>({});

  const loadVersions = async (modelId: string) => {
    if (versionsMap[modelId]) { setVersionsForSelectedModel(versionsMap[modelId]); return; }
    const resp = await api.get(`/models/${modelId}/versions`);
    const vs = resp.data.data as ModelVersion[];
    setVersionsMap((m) => ({ ...m, [modelId]: vs }));
    setVersionsForSelectedModel(vs);
  };

  const upload = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!file) return;
    setUploadStatus("Uploading…");
    try {
      const fd = new FormData();
      fd.append("file", file);
      const resp = await api.post(`/projects/${id}/assets`, fd, { headers: { "Content-Type": "multipart/form-data" } });
      setFile(null); setUploadStatus("Upload complete.");
      assetsQ.refetch();
      void resp;
    } catch (e) { setUploadStatus(apiError(e)); }
  };

  const runInfer = useMutation({
    mutationFn: async (assetId: string) => {
      if (!modelVersionId) throw new Error("Select a model version first");
      const idem = `${id}-${assetId}-${modelVersionId}-${Date.now()}`;
      await api.post(`/projects/${id}/inference/jobs`,
        { asset_id: assetId, model_version_id: modelVersionId },
        { headers: { "Idempotency-Key": idem } } as any);
    },
    onSuccess: () => jobsQ.refetch(),
    onError: (e) => setUploadStatus(apiError(e)),
  });

  if (projQ.isLoading) return <div className="text-muted">Loading project…</div>;
  const proj = projQ.data;
  if (!proj) return <div className="text-danger">Project not found.</div>;

  return (
    <div className="space-y-8">
      <header>
        <div className="text-xs text-muted"><Link href="/projects" className="link">Projects</Link> / {proj.name}</div>
        <h1 className="text-2xl font-semibold mt-1">{proj.name}</h1>
        <p className="text-muted text-sm mt-1">{proj.description || "No description."}</p>
      </header>

      <div className="grid lg:grid-cols-2 gap-4">
        <section className="card">
          <h2 className="font-semibold mb-3 flex items-center gap-2"><Upload className="w-4 h-4" /> Upload Asset</h2>
          <form onSubmit={upload} className="space-y-3">
            <input type="file" accept="image/*" onChange={(e) => setFile(e.target.files?.[0] ?? null)} className="block w-full text-sm" />
            <button className="btn btn-primary" disabled={!file}>Upload</button>
            {uploadStatus && <div className="text-sm text-muted">{uploadStatus}</div>}
          </form>
        </section>

        <section className="card">
          <h2 className="font-semibold mb-3 flex items-center gap-2"><Boxes className="w-4 h-4" /> Run Inference</h2>
          <div className="space-y-2">
            <label className="label">Model</label>
            <select className="input" onChange={async (e) => loadVersions(e.target.value)} defaultValue="">
              <option value="" disabled>Select model…</option>
              {modelsQ.data?.map((m) => <option key={m.id} value={m.id}>{m.name} — {m.task_type}</option>)}
            </select>
            <label className="label">Version</label>
            <select className="input" value={modelVersionId} onChange={(e) => setModelVersionId(e.target.value)} disabled={!versionsForSelectedModel.length}>
              <option value="">Select version…</option>
              {versionsForSelectedModel.map((v) => <option key={v.id} value={v.id}>{v.version} ({v.status}, {v.runtime})</option>)}
            </select>
          </div>
        </section>
      </div>

      <section>
        <h2 className="font-semibold mb-3 flex items-center gap-2"><ImageIcon className="w-4 h-4" /> Assets</h2>
        {assetsQ.isLoading ? <div className="text-muted">Loading…</div> : !assetsQ.data?.length ? <div className="text-muted">No assets yet.</div> : (
          <div className="grid sm:grid-cols-2 lg:grid-cols-3 gap-3">
            {assetsQ.data.map((a) => (
              <div key={a.id} className="card">
                <div className="text-sm font-medium truncate">{a.filename}</div>
                <div className="text-xs text-muted">{a.content_type} · {(a.size_bytes/1024).toFixed(1)} KB · {new Date(a.created_at).toLocaleString()}</div>
                <div className="flex gap-2 mt-3">
                  <button className="btn btn-primary" disabled={!modelVersionId || runInfer.isPending} onClick={() => runInfer.mutate(a.id)}>
                    <Play className="w-4 h-4" /> Run
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </section>

      <section>
        <h2 className="font-semibold mb-3">Inference Jobs</h2>
        {jobsQ.isLoading ? <div className="text-muted">Loading…</div> : !jobsQ.data?.length ? <div className="text-muted">No jobs yet.</div> : (
          <div className="card p-0 overflow-x-auto">
            <table className="table">
              <thead><tr><th>Job</th><th>Asset</th><th>Status</th><th>Attempts</th><th>Created</th><th></th></tr></thead>
              <tbody>
                {jobsQ.data.map((j) => (
                  <tr key={j.id}>
                    <td className="font-mono text-xs"><Link className="link" href={`/jobs/${j.id}`}>{j.id.slice(0,8)}…</Link></td>
                    <td className="text-xs text-muted">{j.asset_id.slice(0,8)}…</td>
                    <td><JobBadge status={j.status} /></td>
                    <td>{j.attempts}/{j.max_attempts}</td>
                    <td className="text-muted">{new Date(j.created_at).toLocaleString()}</td>
                    <td><Link className="link text-xs" href={`/jobs/${j.id}`}>View</Link></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}
