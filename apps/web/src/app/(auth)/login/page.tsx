"use client";

import { useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { api, apiError } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import type { User } from "@/types";

const schema = z.object({
  email: z.string().email(),
  password: z.string().min(1),
});

export default function LoginPage() {
  const router = useRouter();
  const setSession = useAuth((s) => s.setSession);
  const [err, setErr] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const form = useForm<z.infer<typeof schema>>({ resolver: zodResolver(schema) });

  const onSubmit = form.handleSubmit(async (values) => {
    setErr(null); setLoading(true);
    try {
      const resp = await api.post("/auth/login", values);
      const data = resp.data.data;
      setSession(data.access_token, data.refresh_token, data.user as User);
      router.push("/dashboard");
    } catch (e) { setErr(apiError(e)); }
    finally { setLoading(false); }
  });

  return (
    <div className="w-full max-w-sm">
      <div className="mb-8 text-center">
        <h1 className="text-2xl font-semibold">Sign in to VisionForge</h1>
        <p className="text-muted text-sm mt-1">Production computer vision inference.</p>
      </div>
      <form onSubmit={onSubmit} className="card space-y-3">
        <div>
          <label className="label" htmlFor="email">Email</label>
          <input id="email" className="input" {...form.register("email")} placeholder="you@company.com" />
          {form.formState.errors.email && <p className="text-danger text-xs mt-1">{form.formState.errors.email.message}</p>}
        </div>
        <div>
          <label className="label" htmlFor="password">Password</label>
          <input id="password" type="password" className="input" {...form.register("password")} placeholder="••••••••" />
        </div>
        {err && <div className="text-danger text-sm">{err}</div>}
        <button disabled={loading} className="btn btn-primary w-full">{loading ? "Signing in..." : "Sign in"}</button>
        <div className="text-center text-sm text-muted">
          Don&apos;t have an account? <Link href="/register" className="link">Create one</Link>
        </div>
        <div className="text-center text-xs text-muted border-t border-border pt-3 mt-2">
          Demo credentials: demo@visionforge.local / demo12345!
        </div>
      </form>
    </div>
  );
}
