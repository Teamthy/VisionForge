"use client";

import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";
import { api, apiError } from "@/lib/api";
import type { Project } from "@/types";
import { FolderKanban, Plus } from "lucide-react";
import { useRouter } from "next/navigation";

export default function ProjectsPage() {
  const router = useRouter();
  const q = useQuery({ queryKey: ["projects"], queryFn: async () => (await api.get("/projects")).data.data as Project[] });
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const create = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr(null); setBusy(true);
    try {
      const resp = await api.post("/projects", { name, description: desc });
      setName(""); setDesc("");
      q.refetch();
      router.push(`/projects/${resp.data.data.id}`);
    } catch (e) { setErr(apiError(e)); }
    finally { setBusy(false); }
  };

  return (
    <div className="space-y-6">
      <header className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold">Projects</h1>
          <p className="text-muted text-sm mt-1">Group your assets and inference jobs by project.</p>
        </div>
      </header>

      <section className="card">
        <h2 className="font-semibold mb-3 flex items-center gap-2"><Plus className="w-4 h-4" /> New Project</h2>
        <form onSubmit={create} className="grid sm:grid-cols-[1fr_1fr_auto] gap-3">
          <input className="input" placeholder="Project name" value={name} onChange={(e) => setName(e.target.value)} required />
          <input className="input" placeholder="Description (optional)" value={desc} onChange={(e) => setDesc(e.target.value)} />
          <button className="btn btn-primary" disabled={busy}>{busy ? "Creating..." : "Create"}</button>
        </form>
        {err && <div className="text-danger text-sm mt-2">{err}</div>}
      </section>

      <section className="card">
        {q.isLoading ? <div className="text-muted">Loading…</div> : !q.data?.length ? <div className="text-muted">No projects yet.</div> : (
          <div className="grid sm:grid-cols-2 lg:grid-cols-3 gap-3">
            {q.data.map((p) => (
              <Link key={p.id} href={`/projects/${p.id}`} className="border border-border rounded-lg p-4 hover:border-accent/40 transition-colors">
                <div className="flex items-center gap-2 text-muted mb-2"><FolderKanban className="w-4 h-4" /> Project</div>
                <div className="font-semibold">{p.name}</div>
                <div className="text-sm text-muted mt-1 line-clamp-2">{p.description || "No description."}</div>
                <div className="text-xs text-muted mt-3">Created {new Date(p.created_at).toLocaleDateString()}</div>
              </Link>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
