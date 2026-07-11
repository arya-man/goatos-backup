/** @type {import('next').NextConfig} */
const nextConfig = {
  devIndicators: false,
  output: "standalone",
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
