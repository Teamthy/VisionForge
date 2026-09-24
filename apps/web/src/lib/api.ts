"use client";

import axios, { AxiosError, AxiosInstance } from "axios";

const API_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080/api/v1";

export const api: AxiosInstance = axios.create({
  baseURL: API_URL,
  withCredentials: false,
  headers: { "Content-Type": "application/json" },
});

// Attach bearer token from storage.
api.interceptors.request.use((config) => {
  if (typeof window !== "undefined") {
    const tok = localStorage.getItem("vf_access_token");
    if (tok) {
      config.headers = config.headers || {};
      config.headers.Authorization = `Bearer ${tok}`;
    }
  }
  const idemKey = (config as any)._idemKey as string | undefined;
  if (idemKey) {
    config.headers = config.headers || {};
    config.headers["Idempotency-Key"] = idemKey;
  }
  return config;
});

api.interceptors.response.use(
  (r) => r,
  async (err: AxiosError<any>) => {
    // On 401, try refreshing the access token.
    if (err.response?.status === 401 && typeof window !== "undefined") {
      const refresh = localStorage.getItem("vf_refresh_token");
      if (refresh && !err.config?.url?.endsWith("/auth/refresh")) {
        try {
          const resp = await axios.post(`${API_URL}/auth/refresh`, { refresh_token: refresh });
          const { access_token, refresh_token } = resp.data.data;
          localStorage.setItem("vf_access_token", access_token);
          localStorage.setItem("vf_refresh_token", refresh_token);
          if (err.config) {
            err.config.headers.Authorization = `Bearer ${access_token}`;
            return api.request(err.config);
          }
        } catch {
          localStorage.removeItem("vf_access_token");
          localStorage.removeItem("vf_refresh_token");
          if (!window.location.pathname.startsWith("/login")) {
            window.location.href = "/login";
          }
        }
      }
    }
    return Promise.reject(err);
  }
);

export function apiError(e: unknown): string {
  const err = e as AxiosError<any>;
  const data = err.response?.data as any;
  return data?.error?.message || err.message || "Unknown error";
}

export function withIdem<T>(config: T, key?: string): T {
  if (!key) return config;
  return { ...(config as any), _idemKey: key } as T;
}
