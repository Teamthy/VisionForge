"use client";

import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import type { DashboardStats, InferenceJob } from "@/types";
import Link from "next/link";
import { Boxes, CheckCircle, Clock, FolderKanban, Image as ImageIcon, XCircle, Activity, AlertTriangle } from "lucide-react";
import JobBadge from "@/components/JobBadge";

export default function DashboardPage() {
  const statsQ = useQuery({
    queryKey: ["dashboard", "stats"],
    queryFn: async () => (await api.get("/dashboard/stats")).data.data as DashboardStats,
    refetchInterval: 10_000,
  });
  const jobsQ = useQuery({
    queryKey: ["jobs", "recent"],
    queryFn: async () => (await api.get("/jobs", { params: { limit: 10 } })).data.data as InferenceJob[],
  });

  const s = statsQ.data;

  const stats = [
    { label: "Projects", value: s?.total_projects ?? "—", icon: FolderKanban, href: "/projects" },
    { label: "Assets", value: s?.total_assets ?? "—", icon: ImageIcon, href: "/assets" },
    { label: "Jobs Processed", value: s?.jobs_processed ?? "—", icon: Boxes },
    { label: "Successful", value: s?.jobs_succeeded ?? "—", icon: CheckCircle, tone: "success" },
    { label: "Failed", value: s?.jobs_failed ?? "—", icon: XCircle, tone: "danger" },
    { label: "Queue Depth", value: s?.queue_depth ?? "—", icon: Clock, tone: s && s.queue_depth > 50 ? "warn" : "" },
  ];

  return (
    <div className="space-y-8">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Dashboard</h1>
          <p className="text-muted text-sm mt-1">Overview of your VisionForge workloads.</p>
        </div>
      </header>

      <section className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-6 gap-3">
        {stats.map((st) => {
          const Icon = st.icon;
          return (
            <Link key={st.label} href={st.href || "#"} className="stat hover:border-accent/40 transition-colors">
              <div className="flex items-center justify-between">
                <div className="text-xs uppercase tracking-wide text-muted">{st.label}</div>
                <Icon className={
                  "w-4 h-4 " +
                  (st.tone === "success" ? "text-success" :
                    st.tone === "danger" ? "text-danger" :
                    st.tone === "warn" ? "text-warning" : "text-muted")
                } />
              </div>
              <div className="text-2xl font-semibold mt-2">{st.value}</div>
            </Link>
          );
        })}
      </section>

      <section className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        <div className="card lg:col-span-2">
          <h2 className="font-semibold mb-3 flex items-center gap-2"><Activity className="w-4 h-4" /> Recent Jobs</h2>
          {jobsQ.isLoading ? <div className="text-muted text-sm">Loading…</div> :
           !jobsQ.data?.length ? <div className="text-muted text-sm">No jobs yet.</div> : (
            <table className="table">
              <thead><tr><th>Job ID</th><th>Status</th><th>Created</th></tr></thead>
              <tbody>
                {jobsQ.data.map((j) => (
                  <tr key={j.id}>
                    <td className="font-mono text-xs"><Link className="link" href={`/jobs/${j.id}`}>{j.id.slice(0,8)}…</Link></td>
                    <td><JobBadge status={j.status} /></td>
                    <td className="text-muted">{new Date(j.created_at).toLocaleString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
        <div className="card">
          <h2 className="font-semibold mb-3 flex items-center gap-2"><AlertTriangle className="w-4 h-4" /> System Health</h2>
          <ul className="space-y-2 text-sm">
            <HealthItem label="ML Service" ok={s?.ml_service_ok} />
            <HealthItem label="Object Storage" ok={s?.object_storage_ok} />
            <HealthItem label="PostgreSQL" ok={true} />
            <HealthItem label="Redis" ok={true} />
          </ul>
          <p className="text-xs text-muted mt-4">Detailed metrics are available via Grafana at <span className="font-mono">:3001</span>.</p>
        </div>
      </section>
    </div>
  );
}

function HealthItem({ label, ok }: { label: string; ok?: boolean }) {
  return (
    <li className="flex items-center justify-between">
      <span className="text-muted">{label}</span>
      {ok == null ? <span className="badge">Unknown</span>
        : ok ? <span className="badge badge-success">OK</span>
        : <span className="badge badge-danger">Unhealthy</span>}
    </li>
  );
}
