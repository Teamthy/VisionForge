"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { api } from "@/lib/api";
import type { InferenceJob } from "@/types";
import JobBadge from "@/components/JobBadge";

export default function JobsPage() {
  const q = useQuery({
    queryKey: ["jobs"],
    queryFn: async () => (await api.get("/jobs", { params: { limit: 50 } })).data.data as InferenceJob[],
    refetchInterval: 5000,
  });

  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-2xl font-semibold">Inference Jobs</h1>
        <p className="text-muted text-sm mt-1">Track sync and async inference across all projects.</p>
      </header>
      <div className="card p-0 overflow-x-auto">
        {q.isLoading ? <div className="p-4 text-muted">Loading…</div> : !q.data?.length ? <div className="p-4 text-muted">No jobs.</div> : (
          <table className="table">
            <thead>
              <tr><th>Job ID</th><th>Status</th><th>Attempts</th><th>Queued</th><th>Completed</th><th></th></tr>
            </thead>
            <tbody>
              {q.data.map((j) => (
                <tr key={j.id}>
                  <td className="font-mono text-xs">{j.id.slice(0,8)}…</td>
                  <td><JobBadge status={j.status} /></td>
                  <td>{j.attempts}/{j.max_attempts}</td>
                  <td className="text-muted text-xs">{j.queued_at ? new Date(j.queued_at).toLocaleString() : "—"}</td>
                  <td className="text-muted text-xs">{j.completed_at ? new Date(j.completed_at).toLocaleString() : "—"}</td>
                  <td><Link className="link text-xs" href={`/jobs/${j.id}`}>View</Link></td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
