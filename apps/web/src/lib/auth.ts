"use client";

import { create } from "zustand";
import { api } from "./api";
import type { User } from "@/types";

interface AuthState {
  user: User | null;
  accessToken: string | null;
  refreshToken: string | null;
  initialized: boolean;
  hydrate: () => void;
  setSession: (access: string, refresh: string, user: User) => void;
  logout: () => Promise<void>;
  fetchMe: () => Promise<User | null>;
}

export const useAuth = create<AuthState>((set, get) => ({
  user: null,
  accessToken: null,
  refreshToken: null,
  initialized: false,

  hydrate: () => {
    if (typeof window === "undefined") return;
    const access = localStorage.getItem("vf_access_token");
    const refresh = localStorage.getItem("vf_refresh_token");
    const userRaw = localStorage.getItem("vf_user");
    set({
      accessToken: access,
      refreshToken: refresh,
      user: userRaw ? JSON.parse(userRaw) : null,
      initialized: true,
    });
  },

  setSession: (access, refresh, user) => {
    localStorage.setItem("vf_access_token", access);
    localStorage.setItem("vf_refresh_token", refresh);
    localStorage.setItem("vf_user", JSON.stringify(user));
    set({ accessToken: access, refreshToken: refresh, user });
  },

  logout: async () => {
    const refresh = get().refreshToken;
    if (refresh) {
      try {
        await api.post("/auth/logout", { refresh_token: refresh });
      } catch {}
    }
    localStorage.removeItem("vf_access_token");
    localStorage.removeItem("vf_refresh_token");
    localStorage.removeItem("vf_user");
    set({ user: null, accessToken: null, refreshToken: null });
  },

  fetchMe: async () => {
    try {
      const resp = await api.get("/auth/me");
      const user = resp.data.data as User;
      localStorage.setItem("vf_user", JSON.stringify(user));
      set({ user });
      return user;
    } catch {
      set({ user: null });
      return null;
    }
  },
}));
