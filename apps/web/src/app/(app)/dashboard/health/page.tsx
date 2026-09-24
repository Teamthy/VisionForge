"use client";

export default function HealthPage() {
  const endpoints = [
    { name: "API", url: "/health", port: ":8080" },
    { name: "ML Service", url: "/health", port: ":8090" },
    { name: "Prometheus", url: "/-/healthy", port: ":9090" },
    { name: "Grafana", url: "/api/health", port: ":3001" },
    { name: "MinIO", url: "/minio/health/live", port: ":9000" },
  ];
  return (
    <div className="space-y-6">
      <header>
        <h1 className="text-2xl font-semibold">Observability & Metrics</h1>
        <p className="text-muted text-sm mt-1">Dashboards and endpoints for operational visibility.</p>
      </header>
      <div className="grid sm:grid-cols-2 gap-3">
        <div className="card">
          <h3 className="font-semibold mb-2">Direct Links</h3>
          <ul className="space-y-2 text-sm">
            <li><a className="link" href="http://localhost:3001" target="_blank" rel="noreferrer">Grafana →</a> <span className="text-muted text-xs">dashboards at :3001 (admin/admin)</span></li>
            <li><a className="link" href="http://localhost:9090" target="_blank" rel="noreferrer">Prometheus →</a> <span className="text-muted text-xs">at :9090</span></li>
            <li><a className="link" href="http://localhost:9001" target="_blank" rel="noreferrer">MinIO Console →</a> <span className="text-muted text-xs">at :9001 (visionforge / change_me_in_dev)</span></li>
          </ul>
        </div>
        <div className="card">
          <h3 className="font-semibold mb-2">Service Endpoints</h3>
          <ul className="text-sm space-y-1 font-mono">
            {endpoints.map((e) => (
              <li key={e.name}><span className="text-muted">{e.name}:</span> http://localhost{e.port}{e.url}</li>
            ))}
          </ul>
        </div>
      </div>
    </div>
  );
}
