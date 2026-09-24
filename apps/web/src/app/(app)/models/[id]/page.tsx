"use client";

import { useQuery } from "@tanstack/react-query";
import { useParams } from "next/navigation";
import Link from "next/link";
import { api } from "@/lib/api";
import type { Model, ModelVersion } from "@/types";

export default function ModelDetailPage() {
  const { id } = useParams<{ id: string }>();
  const mQ = useQuery({ queryKey: ["model", id], queryFn: async () => (await api.get(`/models/${id}`)).data.data as Model });
  const vQ = useQuery({ queryKey: ["model-versions", id], queryFn: async () => (await api.get(`/models/${id}/versions`)).data.data as ModelVersion[] });
  const m = mQ.data;
  if (!m) return <div className="text-muted">Loading…</div>;
  return (
    <div className="space-y-6">
      <header>
        <div className="text-xs text-muted"><Link href="/models" className="link">Models</Link> / {m.name}</div>
        <h1 className="text-2xl font-semibold mt-1">{m.name}</h1>
        <p className="text-muted text-sm mt-1">{m.description}</p>
      </header>
      <section>
        <h2 className="font-semibold mb-3">Versions</h2>
        <div className="card p-0 overflow-x-auto">
          {!vQ.data?.length ? <div className="p-4 text-muted">No versions registered.</div> : (
            <table className="table">
              <thead><tr><th>Version</th><th>Runtime</th><th>Status</th><th>Artifact</th><th>Created</th></tr></thead>
              <tbody>
                {vQ.data.map((v) => (
                  <tr key={v.id}>
                    <td className="font-mono">{v.version}</td>
                    <td><span className="badge">{v.runtime}</span></td>
                    <td><span className={`badge ${v.status === "ACTIVE" ? "badge-success" : v.status === "DEPRECATED" ? "badge-danger" : "badge-info"}`}>{v.status}</span></td>
                    <td className="text-xs text-muted font-mono truncate max-w-[30ch]">{v.artifact_uri}</td>
                    <td className="text-muted text-xs">{new Date(v.created_at).toLocaleDateString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </section>
    </div>
  );
}
