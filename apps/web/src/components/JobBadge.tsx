"use client";

import clsx from "clsx";
import type { JobStatus } from "@/types";

const COLORS: Record<JobStatus, string> = {
  CREATED: "badge",
  QUEUED: "badge badge-info",
  RUNNING: "badge badge-info",
  SUCCESS: "badge badge-success",
  FAILED: "badge badge-danger",
  RETRYING: "badge badge-warn",
  DEAD: "badge badge-danger",
  CANCELLED: "badge",
};

export default function JobBadge({ status }: { status: JobStatus }) {
  return <span className={clsx(COLORS[status] || "badge")}>{status}</span>;
}
