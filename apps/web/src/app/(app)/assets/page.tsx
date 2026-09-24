"use client";

import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import Link from "next/link";
import type { Project, Asset } from "@/types";
import { useState } from "react";
import { Image as ImageIcon } from "lucide-react";

export default function AssetsPage() {
  const projectsQ = useQuery({ queryKey: ["projects"], queryFn: async () => (await api.get("/projects")).data.data as Project[] });
  const [projectId, setProjectId] = useState<string>("");
  const assetsQ = useQuery({
    queryKey: ["assets", projectId],
    queryFn: async () => projectId ? (await api.get(`/projects/${projectId}/assets`)).data.data as Asset[] : [],
    enabled: !!projectId,
  });

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-2xl font-semibold">Assets</h1>
        <p className="text-muted text-sm mt-1">Images and videos uploaded across projects.</p>
      </header>
      <div className="card">
        <label className="label">Project</label>
        <select className="input" value={projectId} onChange={(e) => setProjectId(e.target.value)}>
          <option value="">Select a project…</option>
          {projectsQ.data?.map((p) => <option key={p.id} value={p.id}>{p.name}</option>)}
        </select>
      </div>
      {projectId && assetsQ.data && (
        <div className="grid sm:grid-cols-2 lg:grid-cols-3 gap-3">
          {assetsQ.data.map((a) => (
            <div key={a.id} className="card">
              <div className="flex items-center gap-2 text-muted mb-1"><ImageIcon className="w-4 h-4" /> Asset</div>
              <div className="font-medium truncate">{a.filename}</div>
              <div className="text-xs text-muted mt-1">{a.content_type} · {(a.size_bytes/1024).toFixed(1)} KB</div>
              <div className="text-xs text-muted mt-1">{new Date(a.created_at).toLocaleString()}</div>
              <div className="mt-3"><Link className="link text-xs" href={`/projects/${a.project_id}`}>Go to project</Link></div>
            </div>
          ))}
          {assetsQ.data.length === 0 && <div className="text-muted text-sm col-span-full">No assets in this project.</div>}
        </div>
      )}
    </div>
  );
}
