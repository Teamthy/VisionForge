"use client";

import { useEffect, useRef, useState } from "react";
import type { Detection } from "@/types";

export default function ResultCanvas({ src, detections }: { src: string; detections: Detection[] }) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const [dims, setDims] = useState<{ w: number; h: number } | null>(null);

  useEffect(() => {
    const img = new Image();
    img.crossOrigin = "anonymous";
    img.onload = () => setDims({ w: img.naturalWidth, h: img.naturalHeight });
    img.src = src;
  }, [src]);

  useEffect(() => {
    if (!dims || !canvasRef.current || !containerRef.current) return;
    const canvas = canvasRef.current;
    const container = containerRef.current;
    const maxWidth = Math.min(container.clientWidth, 1200);
    const scale = Math.min(1, maxWidth / dims.w);
    const w = dims.w * scale;
    const h = dims.h * scale;
    const dpr = window.devicePixelRatio || 1;
    canvas.width = w * dpr;
    canvas.height = h * dpr;
    canvas.style.width = `${w}px`;
    canvas.style.height = `${h}px`;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, w, h);
    const img = new Image();
    img.crossOrigin = "anonymous";
    img.onload = () => {
      ctx.drawImage(img, 0, 0, w, h);
      ctx.lineWidth = 2;
      ctx.font = "12px ui-monospace, monospace";
      const palette = ["#4f8cff", "#3fb950", "#d29922", "#f85149", "#a371f7", "#39c5cf"];
      detections.forEach((d, i) => {
        const color = palette[i % palette.length];
        const x = d.bbox.x * scale, y = d.bbox.y * scale, bw = d.bbox.width * scale, bh = d.bbox.height * scale;
        ctx.strokeStyle = color;
        ctx.strokeRect(x, y, bw, bh);
        const label = `${d.label} ${(d.confidence * 100).toFixed(0)}%`;
        const tw = ctx.measureText(label).width + 8;
        ctx.fillStyle = color;
        ctx.fillRect(x, y - 18, tw, 18);
        ctx.fillStyle = "#fff";
        ctx.fillText(label, x + 4, y - 5);
      });
    };
    img.src = src;
  }, [src, detections, dims]);

  return (
    <div ref={containerRef} className="w-full flex justify-center bg-black/20 rounded-md overflow-hidden">
      <canvas ref={canvasRef} className="block max-w-full" />
    </div>
  );
}
