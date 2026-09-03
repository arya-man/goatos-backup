import { fileURLToPath } from "node:url";

const repoRoot = fileURLToPath(new URL("../..", import.meta.url));

/** @type {import('next').NextConfig} */
const nextConfig = {
  devIndicators: false,
  // Dev-only: Next 16 treats its dev origin as `localhost` and BLOCKS dev resources
  // (client chunks' HMR socket included) for a page opened via 127.0.0.1 — the page then
  // server-renders fine but React mounts no renderers, so every click is dead. The two hosts
  // are the same loopback on this stack; allowing 127.0.0.1 makes both URLs behave.
  allowedDevOrigins: ["127.0.0.1"],
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
