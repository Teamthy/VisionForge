"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { api } from "@/lib/api";
import type { Model } from "@/types";
import { Boxes } from "lucide-react";

export default function ModelsPage() {
  const q = useQuery({ queryKey: ["models"], queryFn: async () => (await api.get("/models")).data.data as Model[] });
  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-2xl font-semibold">Models</h1>
        <p className="text-muted text-sm mt-1">Registered model families and versions.</p>
      </header>
      <div className="card p-0 overflow-x-auto">
        {q.isLoading ? <div className="p-4 text-muted">Loading…</div> : !q.data?.length ? <div className="p-4 text-muted">No models registered.</div> : (
          <table className="table">
            <thead><tr><th>Name</th><th>Task</th><th>Description</th><th>Created</th><th></th></tr></thead>
            <tbody>
              {q.data.map((m) => (
                <tr key={m.id}>
                  <td className="font-medium"><Boxes className="inline w-4 h-4 mr-2 text-muted" />{m.name}</td>
                  <td><span className="badge">{m.task_type}</span></td>
                  <td className="text-muted">{m.description}</td>
                  <td className="text-muted text-xs">{new Date(m.created_at).toLocaleDateString()}</td>
                  <td><Link className="link text-xs" href={`/models/${m.id}`}>View versions</Link></td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
