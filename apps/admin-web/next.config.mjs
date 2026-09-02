import { fileURLToPath } from "node:url";

const repoRoot = fileURLToPath(new URL("../..", import.meta.url));

/** @type {import('next').NextConfig} */
const nextConfig = {
  devIndicators: false,
  output: "standalone",
  outputFileTracingRoot: repoRoot,
  transpilePackages: ["@goatos/api-client"],
  async rewrites() {
    return [
      {
        source: "/__/auth/action",
        destination: "/auth/action",
      },
    ];
  },
};

export default nextConfig;
