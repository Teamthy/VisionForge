"use client";

import { useAuth } from "@/lib/auth";

export default function SettingsPage() {
  const user = useAuth((s) => s.user);
  const logout = useAuth((s) => s.logout);
  return (
    <div className="space-y-6 max-w-2xl">
      <header>
        <h1 className="text-2xl font-semibold">Settings</h1>
        <p className="text-muted text-sm mt-1">Account and platform configuration.</p>
      </header>
      <section className="card">
        <h2 className="font-semibold">Profile</h2>
        {user && (
          <dl className="mt-3 text-sm space-y-2">
            <div className="flex justify-between max-w-md"><dt className="text-muted">Name</dt><dd>{user.name}</dd></div>
            <div className="flex justify-between max-w-md"><dt className="text-muted">Email</dt><dd>{user.email}</dd></div>
            <div className="flex justify-between max-w-md"><dt className="text-muted">Role</dt><dd><span className="badge">{user.role}</span></dd></div>
            <div className="flex justify-between max-w-md"><dt className="text-muted">Joined</dt><dd>{new Date(user.created_at).toLocaleDateString()}</dd></div>
          </dl>
        )}
        <button className="btn btn-danger mt-5" onClick={() => logout()}>Sign out</button>
      </section>
      <section className="card">
        <h2 className="font-semibold">System</h2>
        <ul className="text-sm text-muted mt-3 space-y-1">
          <li>API endpoint: <code className="text-fg">{process.env.NEXT_PUBLIC_API_URL}</code></li>
          <li>Observability: Prometheus at <code>:9090</code>, Grafana at <code>:3001</code></li>
          <li>Storage: MinIO at <code>:9000</code> / <code>:9001</code></li>
        </ul>
      </section>
    </div>
  );
}
