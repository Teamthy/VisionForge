"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import clsx from "clsx";
import {
  LayoutDashboard,
  FolderKanban,
  Image as ImageIcon,
  PlayCircle,
  Boxes,
  Activity,
  Settings,
  LogOut,
  Eye,
} from "lucide-react";
import { useAuth } from "@/lib/auth";
import { useRouter } from "next/navigation";

const NAV = [
  { href: "/dashboard", label: "Dashboard", icon: LayoutDashboard },
  { href: "/projects", label: "Projects", icon: FolderKanban },
  { href: "/assets", label: "Assets", icon: ImageIcon },
  { href: "/jobs", label: "Inference Jobs", icon: PlayCircle },
  { href: "/models", label: "Models", icon: Boxes },
  { href: "/dashboard/health", label: "Metrics", icon: Activity },
  { href: "/settings", label: "Settings", icon: Settings },
];

export default function Sidebar() {
  const pathname = usePathname();
  const user = useAuth((s) => s.user);
  const logout = useAuth((s) => s.logout);
  const router = useRouter();

  return (
    <aside className="w-64 shrink-0 border-r border-border bg-panel/40 flex flex-col min-h-screen">
      <div className="px-5 py-5 flex items-center gap-2 border-b border-border">
        <div className="w-8 h-8 rounded-md bg-accent/15 text-accent flex items-center justify-center">
          <Eye className="w-5 h-5" />
        </div>
        <div>
          <div className="font-semibold leading-tight">VisionForge</div>
          <div className="text-xs text-muted">CV Inference Platform</div>
        </div>
      </div>
      <nav className="flex-1 p-3 space-y-1">
        {NAV.map(({ href, label, icon: Icon }) => (
          <Link
            key={href}
            href={href}
            className={clsx("nav-link", { active: pathname === href || pathname?.startsWith(href + "/") })}
          >
            <Icon className="w-4 h-4" />
            {label}
          </Link>
        ))}
      </nav>
      <div className="border-t border-border p-3">
        {user && (
          <div className="flex items-center justify-between gap-2 px-2 py-2">
            <div className="min-w-0">
              <div className="text-sm font-medium truncate">{user.name}</div>
              <div className="text-xs text-muted truncate">{user.email}</div>
            </div>
            <button
              onClick={async () => { await logout(); router.push("/login"); }}
              className="btn-ghost p-2 rounded-md text-muted hover:text-danger"
              aria-label="Logout"
            >
              <LogOut className="w-4 h-4" />
            </button>
          </div>
        )}
      </div>
    </aside>
  );
}
