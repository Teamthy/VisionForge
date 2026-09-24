import type { Metadata } from "next";
import "@/styles/globals.css";
import Providers from "./providers";

export const metadata: Metadata = {
  title: "VisionForge — Computer Vision Inference Platform",
  description: "Production-grade computer vision inference at scale.",
  icons: { icon: "/favicon.svg" },
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className="dark">
      <body className="min-h-screen">
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}
