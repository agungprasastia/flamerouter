const backendUrl = process.env.BACKEND_URL || "http://127.0.0.1:20130";

/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: false,
  typescript: {
    ignoreBuildErrors: false,
  },
  serverExternalPackages: ["better-sqlite3", "sql.js", "node:sqlite", "bun:sqlite", "open"],
  turbopack: {},
  images: {
    unoptimized: true,
  },
  webpack: (config, { isServer }) => {
    if (!isServer) {
      config.resolve.fallback = {
        ...config.resolve.fallback,
        fs: false,
        path: false,
        "better-sqlite3": false,
      };
    } else {
      config.externals = [...(config.externals || []), "better-sqlite3"];
    }
    return config;
  },
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination: `${backendUrl}/api/:path*`,
      },
      {
        source: "/v1/:path*",
        destination: `${backendUrl}/v1/:path*`,
      },
      {
        source: "/v1beta/:path*",
        destination: `${backendUrl}/v1beta/:path*`,
      },
      {
        source: "/codex/:path*",
        destination: `${backendUrl}/codex/:path*`,
      },
    ];
  },
};

export default nextConfig;
