"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { useAuth } from "@/lib/auth";
import Sidebar from "@/components/Sidebar";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { staleTime: 15_000, refetchOnWindowFocus: false, retry: 1 },
  },
});

const PUBLIC_PATHS = ["/login", "/register"];

export default function Providers({ children }: { children: React.ReactNode }) {
  const hydrate = useAuth((s) => s.hydrate);
  const initialized = useAuth((s) => s.initialized);
  const user = useAuth((s) => s.user);
  const fetchMe = useAuth((s) => s.fetchMe);
  const pathname = usePathname();
  const router = useRouter();

  useEffect(() => { hydrate(); }, [hydrate]);
  useEffect(() => {
    if (!initialized) return;
    const isPublic = PUBLIC_PATHS.some((p) => pathname?.startsWith(p));
    if (!user && !isPublic) {
      router.replace("/login");
    } else if (user && isPublic) {
      router.replace("/dashboard");
    }
    if (user) fetchMe().catch(() => {});
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialized, user, pathname]);

  if (!initialized) return null;

  const isPublic = PUBLIC_PATHS.some((p) => pathname?.startsWith(p));

  return (
    <QueryClientProvider client={queryClient}>
      {isPublic ? (
        <main className="min-h-screen flex items-center justify-center p-6">{children}</main>
      ) : (
        <div className="min-h-screen flex">
          <Sidebar />
          <main className="flex-1 min-w-0 p-6 lg:p-8">{children}</main>
        </div>
      )}
    </QueryClientProvider>
  );
}
