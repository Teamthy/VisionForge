/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  poweredByHeader: false,
  images: {
    remotePatterns: [
      { protocol: "http", hostname: "localhost" },
      { protocol: "http", hostname: "minio" },
      { protocol: "http", hostname: "127.0.0.1" },
    ],
  },
  // Same-origin proxy mode (NEXT_PUBLIC_PROXY_API=true): the browser calls
  // /api/v1/* on this server and Next forwards to the API. Useful when the
  // API has no public hostname (sandboxed/preview environments) or you want
  // one origin. NOTE: rewrites are evaluated at BUILD time into the routes
  // manifest, so all three env vars below must be set when running build.
  async rewrites() {
    if (process.env.NEXT_PUBLIC_PROXY_API !== "true") return [];
    // Where the API actually lives from the web SERVER's point of view
    // (inside compose this is http://api:8080).
    const target = process.env.API_INTERNAL_URL || "http://localhost:8080";
    return [{ source: "/api/v1/:path*", destination: `${target}/api/v1/:path*` }];
  },
};

export default nextConfig;
